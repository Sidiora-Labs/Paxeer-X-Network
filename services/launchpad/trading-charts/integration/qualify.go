package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	Pool                                                                  *pgxpool.Pool
	NATS                                                                  *nats.Conn
	RPC                                                                   *ethclient.Client
	Stream, Schema, SourceSchema, SourceStream                            string
	ConsumerID, Generation, Owner, ProjectionVersion, Region, PoolAddress string
	Epoch                                                                 int64
	MaxReplayBlocks, MaxReplayFills                                       int
}

type recordedPublication struct {
	Publication accounting.PublicationV1
	Wire        []byte
}

type durableState struct {
	DeliveryInbox, ChartInbox, Effects, Outbox, Commits, Next, Quarantine int64
}

func RunFaultScenario(ctx context.Context, scenario string, config Config) error {
	suffixes := map[string]string{"stale-partition-owner": "stale", "ordered-gap": "gap", "poison-quarantine": "poison"}
	suffix, ok := suffixes[scenario]
	if !ok || ctx == nil || config.Pool == nil || config.NATS == nil || config.NATS.IsClosed() || config.RPC == nil || !strings.HasPrefix(config.Schema, "kd_") || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,45}$`).MatchString(config.Schema) || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(config.SourceSchema) || config.SourceStream == "" || config.ConsumerID == "" || config.Generation == "" || config.Owner == "" || config.ProjectionVersion == "" || config.Epoch < 1 || config.Region == "" || !regexp.MustCompile(`^0x[0-9a-f]{40}$`).MatchString(config.PoolAddress) || config.MaxReplayBlocks < 1 || config.MaxReplayBlocks > 4096 || config.MaxReplayFills < 2 || config.MaxReplayFills > 10000 {
		return errors.New("charts fault: actual owned resources, canonical source, RPC and bounded configuration required")
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	schema := config.Schema + "_" + suffix
	consumerID := config.ConsumerID + "_" + suffix
	durable := consumerID + "_broker"
	if len(consumerID) > 100 || len(durable) > 128 {
		return errors.New("charts fault: bounded owned durable name required")
	}
	q := pgx.Identifier{schema}.Sanitize()
	sourceQ := pgx.Identifier{config.SourceSchema}.Sanitize()
	rows, err := config.Pool.Query(bounded, `SELECT p.position,o.envelope FROM `+sourceQ+`.indexer_market_partition_positions_v1 p JOIN `+sourceQ+`.indexer_accounting_publications_v1 a ON a.payload->'positions'->0->>'effectIdentity'=convert_from(p.position,'UTF8')::jsonb->>'effectIdentity' JOIN `+sourceQ+`.delivery_outbox o ON o.event_id=a.publication_id WHERE p.projection_version=$1 AND p.partition_kind='market' AND p.partition_key=$2 AND p.sequence IN(1,2) ORDER BY p.sequence`, config.ProjectionVersion, "125:"+config.PoolAddress)
	if err != nil {
		return fmt.Errorf("charts fault: source publication authority: %w", err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{accounting.MarketSubject: true}}
	var records []recordedPublication
	for rows.Next() {
		var positionWire, wire []byte
		if err = rows.Scan(&positionWire, &wire); err != nil {
			rows.Close()
			return err
		}
		envelope, err := delivery.Decode(wire, policy, accounting.MarketSubject)
		if err != nil {
			rows.Close()
			return err
		}
		publication, err := accounting.DecodePublicationV1(envelope.Payload)
		if err != nil {
			rows.Close()
			return err
		}
		var position accounting.PartitionPosition
		if err = json.Unmarshal(positionWire, &position); err != nil {
			rows.Close()
			return err
		}
		if publication.Fill == nil || publication.Fill.PoolAddress != config.PoolAddress || len(publication.Positions) != 1 || publication.Positions[0] != position || publication.PublicationID != envelope.EventID {
			rows.Close()
			return errors.New("charts fault: source outbox/publication/position disagreement")
		}
		records = append(records, recordedPublication{Publication: publication, Wire: wire})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(records) != 2 || records[0].Publication.Positions[0].Sequence != "1" || records[1].Publication.Positions[0].Sequence != "2" || records[1].Publication.Positions[0].PreviousEffectIdentity != records[0].Publication.Positions[0].EffectIdentity {
		return errors.New("charts fault: two actual contiguous committed source publications required")
	}
	if _, err = config.Pool.Exec(bounded, "CREATE SCHEMA "+q); err != nil {
		return fmt.Errorf("charts fault: fresh exact owned schema required: %w", err)
	}
	for _, ddl := range []func(string) (string, error){delivery.SchemaDDL, delivery.PartitionSchemaDDL, store.ExactSchemaDDL} {
		sql, err := ddl(schema)
		if err != nil {
			return err
		}
		if _, err = config.Pool.Exec(bounded, sql); err != nil {
			return err
		}
	}
	guard, err := queue.NewEpochGuard("recovery", config.Epoch, config.Region)
	if err != nil {
		return err
	}
	deliveries, err := delivery.NewStoreWithEpochGuard(config.Pool, schema, policy, guard.Assert)
	if err != nil {
		return err
	}
	partitions, err := delivery.NewPartitionStore(config.Pool, schema)
	if err != nil {
		return err
	}
	exact, err := store.NewExact(config.Pool, schema)
	if err != nil {
		return err
	}
	accountingConsumer, err := consumer.NewSourceVerifiedAccountingConsumer(consumer.SourceVerifiedAccountingConfig{Pool: config.Pool, Deliveries: deliveries, Partitions: partitions, Exact: exact, ConsumerID: consumerID, Generation: config.Generation, ProjectionVersion: config.ProjectionVersion, Owner: config.Owner, SourceStream: config.SourceStream, SourcePartitionSchema: config.SourceSchema, RPC: config.RPC, MaxReplayBlocks: config.MaxReplayBlocks, MaxReplayFills: config.MaxReplayFills})
	if err != nil {
		return err
	}
	js, err := jetstream.New(config.NATS)
	if err != nil {
		return err
	}
	stream, err := js.Stream(bounded, config.Stream)
	if err != nil {
		return err
	}
	info, err := stream.Info(bounded)
	if err != nil {
		return err
	}
	if info.Config.Storage != jetstream.FileStorage {
		return errors.New("charts fault: actual file-backed stream required")
	}
	transportConfig := natsdriver.ConsumerConfig{Stream: config.Stream, Durable: durable, Subject: accounting.MarketSubject, Batch: 1, MaxAckPending: 10000, MaxWaiting: 1, MaxDeliver: 20, FetchWait: 100 * time.Millisecond, AckWait: time.Second}
	brokerConfig, err := transportConfig.BrokerConsumerConfig()
	if err != nil {
		return err
	}
	brokerConsumer, err := stream.CreateConsumer(bounded, brokerConfig)
	if err != nil {
		return err
	}
	transport, err := natsdriver.NewConsumer(bounded, config.NATS, transportConfig)
	if err != nil {
		return err
	}
	defer transport.Close()
	publisher, err := natsdriver.NewPublisher(config.NATS, config.Stream)
	if err != nil {
		return err
	}
	for _, record := range records {
		if _, err = publisher.Publish(bounded, accounting.MarketSubject, record.Publication.PublicationID, record.Wire); err != nil {
			return err
		}
	}
	fetch := func(eventID string) (*natsdriver.Message, error) {
		for count := 0; count < config.MaxReplayFills; {
			messages, err := transport.Fetch(bounded)
			if err != nil {
				return nil, err
			}
			for _, message := range messages {
				count++
				if message.EventID == eventID {
					return message, nil
				}
			}
			if err = bounded.Err(); err != nil {
				return nil, err
			}
		}
		return nil, errors.New("charts fault: bounded retained broker replay exhausted")
	}
	inspect := func() (durableState, error) {
		var result durableState
		err := config.Pool.QueryRow(bounded, `SELECT (SELECT count(*) FROM `+q+`.delivery_inbox),(SELECT count(*) FROM `+q+`.chart_inbox),(SELECT count(*) FROM `+q+`.chart_exact),(SELECT count(*) FROM `+q+`.chart_outbox),(SELECT count(*) FROM `+q+`.delivery_partition_commits),(SELECT COALESCE(max(next_sequence),0) FROM `+q+`.delivery_partition_offsets),(SELECT count(*) FROM `+q+`.delivery_ordered_quarantine)`).Scan(&result.DeliveryInbox, &result.ChartInbox, &result.Effects, &result.Outbox, &result.Commits, &result.Next, &result.Quarantine)
		return result, err
	}
	assertUnacked := func(before *jetstream.ConsumerInfo) error {
		after, err := brokerConsumer.Info(bounded)
		if err != nil {
			return err
		}
		if before.NumAckPending < 1 || after.NumAckPending != before.NumAckPending || after.AckFloor.Consumer != before.AckFloor.Consumer || after.AckFloor.Stream != before.AckFloor.Stream {
			return fmt.Errorf("charts fault: refused delivery changed broker acknowledgment state pending=%d/%d consumer_floor=%d/%d stream_floor=%d/%d", before.NumAckPending, after.NumAckPending, before.AckFloor.Consumer, after.AckFloor.Consumer, before.AckFloor.Stream, after.AckFloor.Stream)
		}
		return nil
	}
	switch scenario {
	case "stale-partition-owner":
		first, err := fetch(records[0].Publication.PublicationID)
		if err != nil {
			return err
		}
		if err = accountingConsumer.Consume(bounded, first); err != nil {
			return fmt.Errorf("charts fault: initial real chart consume: %w", err)
		}
		before, err := inspect()
		if err != nil {
			return err
		}
		if before.DeliveryInbox != 1 || before.ChartInbox != 1 || before.Effects != 7 || before.Outbox != 7 || before.Commits != 1 || before.Next != 2 || before.Quarantine != 0 {
			return fmt.Errorf("charts fault: initial committed state %+v", before)
		}
		key := delivery.ConsumerKey{ConsumerID: consumerID, ProjectionGeneration: config.Generation, PartitionKind: "market", PartitionKey: "125:" + config.PoolAddress}
		old, err := partitions.Acquire(bounded, key, config.Owner, time.Second)
		if err != nil {
			return err
		}
		timer := time.NewTimer(1100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-bounded.Done():
			return bounded.Err()
		case <-timer.C:
		}
		replacement, err := partitions.Acquire(bounded, key, config.Owner+"_replacement", 30*time.Second)
		if err != nil {
			return err
		}
		if replacement.Epoch <= old.Epoch {
			return errors.New("charts fault: real lease takeover did not increase fence")
		}
		second, err := fetch(records[1].Publication.PublicationID)
		if err != nil {
			return err
		}
		brokerBefore, err := brokerConsumer.Info(bounded)
		if err != nil {
			return err
		}
		err = accountingConsumer.Consume(bounded, second)
		if !errors.Is(err, delivery.ErrLease) {
			return fmt.Errorf("charts fault: old actual consumer did not receive stale lease refusal: %v", err)
		}
		after, err := inspect()
		if err != nil {
			return err
		}
		if before != after {
			return fmt.Errorf("charts fault: stale owner changed durable state before=%+v after=%+v", before, after)
		}
		if err = assertUnacked(brokerBefore); err != nil {
			return err
		}
	case "ordered-gap":
		second, err := fetch(records[1].Publication.PublicationID)
		if err != nil {
			return err
		}
		brokerBefore, err := brokerConsumer.Info(bounded)
		if err != nil {
			return err
		}
		err = accountingConsumer.Consume(bounded, second)
		if !errors.Is(err, delivery.ErrGap) {
			return fmt.Errorf("charts fault: actual out-of-order consumer did not reject missing predecessor: %v", err)
		}
		observed, err := inspect()
		if err != nil {
			return err
		}
		if observed != (durableState{Next: 1}) {
			return fmt.Errorf("charts fault: gap altered durable domain/inbox/offset/outbox %+v", observed)
		}
		if err = assertUnacked(brokerBefore); err != nil {
			return err
		}
	case "poison-quarantine":
		sourceEnvelope, err := delivery.Decode(records[0].Wire, policy, accounting.MarketSubject)
		if err != nil {
			return err
		}
		poisonPayload := json.RawMessage(`{"version":"accounting-publication-v1"}`)
		poisonID := delivery.Hash([]byte(schema + "/malformed-inner/" + sourceEnvelope.EventID))
		sourceEnvelope.EventID = poisonID
		sourceEnvelope.TraceID = poisonID
		sourceEnvelope.Payload = poisonPayload
		sourceEnvelope.PayloadHash = delivery.Hash(poisonPayload)
		wire, err := sourceEnvelope.Encode(policy, accounting.MarketSubject)
		if err != nil {
			return err
		}
		if _, err = publisher.Publish(bounded, accounting.MarketSubject, poisonID, wire); err != nil {
			return err
		}
		poison, err := fetch(poisonID)
		if err != nil {
			return err
		}
		brokerBefore, err := brokerConsumer.Info(bounded)
		if err != nil {
			return err
		}
		err = accountingConsumer.Consume(bounded, poison)
		var failure *delivery.IntegrityFailure
		if !errors.As(err, &failure) || failure.Class != delivery.InnerSchema {
			return fmt.Errorf("charts fault: actual malformed broker message did not reach inner schema refusal: %v", err)
		}
		observed, err := inspect()
		if err != nil {
			return err
		}
		if observed != (durableState{Quarantine: 1}) {
			return fmt.Errorf("charts fault: poison changed accepted business state %+v", observed)
		}
		var retained bool
		err = config.Pool.QueryRow(bounded, `SELECT envelope=$4 AND publication_id=$5 AND failure_class='inner_schema' FROM `+q+`.delivery_ordered_quarantine WHERE consumer_id=$1 AND projection_generation=$2 AND envelope_hash=$3`, consumerID, config.Generation, delivery.Hash(wire), wire, poisonID).Scan(&retained)
		if err != nil {
			return err
		}
		if !retained {
			return errors.New("charts fault: actual poisoned wire evidence not retained")
		}
		if err = assertUnacked(brokerBefore); err != nil {
			return err
		}
	}
	fmt.Printf("charts fault qualified scenario=%s schema=%s durable=%s source_first=%s source_second=%s\n", scenario, schema, durable, records[0].Publication.PublicationID, records[1].Publication.PublicationID)
	return nil
}
