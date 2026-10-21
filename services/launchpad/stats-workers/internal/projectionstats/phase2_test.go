package projectionstats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPhase2ActualReceiptProjectionAgainstIntegerReference(t *testing.T) {
	ctx, repo := repository(t)
	authority, labels := historicalAuthority(t)
	if _, err := repo.pool.Exec(ctx, repo.Phase2MigrationSQL()); err != nil {
		t.Fatal(err)
	}
	volume := new(big.Int)
	fees := map[string]*big.Int{}
	count := int64(0)
	var observed int64
	for _, label := range []string{"buy", "sell", "direct-swap", "rounding-buy", "multihop"} {
		for _, trade := range authority.Trades(labels[label]) {
			value, ok := new(big.Int).SetString(trade.USDLAmountRaw, 10)
			if !ok {
				t.Fatal("actual receipt amount")
			}
			volume.Add(volume, value)
			key := trade.FeeAssetAddress
			if fees[key] == nil {
				fees[key] = new(big.Int)
			}
			fee, ok := new(big.Int).SetString(trade.FeeRaw, 10)
			if !ok {
				t.Fatal("actual receipt fee")
			}
			fees[key].Add(fees[key], fee)
			original := authority.admitted[trade.EventID]
			if original.timestamp > observed {
				observed = original.timestamp
			}
			applied, err := repo.ApplyAuthorized(ctx, authority, trade, "phase2-actual", "phase2-actual")
			if err != nil || !applied.Accepted {
				t.Fatal(applied, err)
			}
			replay, err := repo.ApplyAuthorized(ctx, authority, trade, "phase2-actual", "phase2-actual")
			if err != nil || replay.Accepted {
				t.Fatal(replay, err)
			}
			count++
		}
	}
	aggregate, err := repo.AggregateWindow(ctx, "phase2-actual", "999999999999999999", observed, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.VenueVolumeRaw != volume.String() || aggregate.PoolFillCount != count || aggregate.TraderPopulation != "sender_actor" || aggregate.UserActionCount != nil {
		t.Fatal(aggregate, volume, count)
	}
	if len(aggregate.Fees) != len(fees) {
		t.Fatal(aggregate.Fees, fees)
	}
	for _, group := range aggregate.Fees {
		if fees[group.Asset] == nil || group.Raw != fees[group.Asset].String() {
			t.Fatal(group, fees)
		}
	}
	t.Logf("actual receipt SQL reference: volume=%s fills=%d fees=%v", volume, count, aggregate.Fees)
}

func phase3Driver(t *testing.T, ctx context.Context, nc *nats.Conn, stream, subject string) *natsdriver.Consumer {
	t.Helper()
	cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: "phase3_worker", Subject: subject, Batch: 1, MaxAckPending: 1, MaxWaiting: 1, MaxDeliver: 10, FetchWait: time.Second, AckWait: time.Second}
	driver, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func phase3Fetch(t *testing.T, ctx context.Context, driver *natsdriver.Consumer) *natsdriver.Message {
	t.Helper()
	for {
		messages, err := driver.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) == 1 {
			return messages[0]
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
}

func TestPhase3CrashWorker(t *testing.T) {
	stage := os.Getenv("L13_PHASE3_CRASH_STAGE")
	if stage == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("L13_PHASE3_CRASH_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(pool, os.Getenv("L13_PHASE3_CRASH_SCHEMA"))
	if err != nil {
		t.Fatal(err)
	}
	nc, err := nats.Connect(os.Getenv("L13_PHASE3_NATS_URL"), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	driver := phase3Driver(t, ctx, nc, os.Getenv("L13_PHASE3_CRASH_STREAM"), os.Getenv("L13_PHASE3_CRASH_SUBJECT"))
	message := phase3Fetch(t, ctx, driver)
	authority, labels := historicalAuthority(t)
	trade := authority.Trades(labels["buy"])[0]
	var received Trade
	if json.Unmarshal(message.Wire, &received) != nil || Digest(received) != Digest(trade) {
		t.Fatal("broker payload differs from actual canonical receipt")
	}
	if stage == "before_commit" {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		amounts, err := authority.Account(trade)
		if err != nil {
			t.Fatal(err)
		}
		admitted := authority.admitted[trade.EventID]
		in := ApplyInput{Trade: trade, Assets: admitted.assets, BlockTimestamp: admitted.timestamp, WatermarkTimestamp: admitted.timestamp, ConsumerVersion: "phase3-crash", ProjectionVersion: "phase3-crash"}
		if _, err = repo.applyAccountedTx(ctx, tx, in, amounts, nil, nil); err != nil {
			t.Fatal(err)
		}
		os.Exit(73)
	}
	result, err := repo.ApplyAuthorized(ctx, authority, trade, "phase3-crash", "phase3-crash")
	if err != nil || !result.Accepted {
		t.Fatal(result, err)
	}
	if stage == "after_commit" {
		os.Exit(74)
	}
	if stage == "before_ack" {
		var volume string
		if err = pool.QueryRow(ctx, "SELECT volume::text FROM "+repo.schema+".trade_facts WHERE event_id=$1", trade.EventID).Scan(&volume); err != nil || volume != trade.USDLAmountRaw {
			t.Fatal(volume, err)
		}
		os.Exit(75)
	}
	t.Fatal("unknown crash stage")
}

func TestPhase3ActualReceiptProcessCrashAndBrokerRedelivery(t *testing.T) {
	brokerURL := os.Getenv("L13_PHASE3_NATS_URL")
	if brokerURL == "" {
		t.Fatal("L13_PHASE3_NATS_URL must name the admitted three-replica JetStream fixture")
	}
	for _, stage := range []string{"before_commit", "after_commit", "before_ack"} {
		t.Run(stage, func(t *testing.T) {
			ctx, repo := repository(t)
			nc, err := nats.Connect(brokerURL, nats.Timeout(5*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(nc.Close)
			js, err := jetstream.New(nc)
			if err != nil {
				t.Fatal(err)
			}
			stream := "L13_" + digestBytes([]byte(repo.schema))[:24]
			subject := "l13.phase3." + stream
			created, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{subject}, Storage: jetstream.FileStorage, Replicas: 3, MaxMsgs: 8, MaxBytes: 8 * delivery.MaxEnvelopeBytes, MaxAge: time.Minute, Duplicates: time.Minute})
			if err != nil {
				t.Fatal("real R3 file-backed transport required", err)
			}
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := js.DeleteStream(cleanup, stream); err != nil {
					t.Error(err)
				}
			})
			info, err := created.Info(ctx)
			if err != nil || info.Config.Replicas != 3 || info.Cluster == nil || len(info.Cluster.Replicas) != 2 {
				t.Fatal(info, err)
			}
			cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: "phase3_worker", Subject: subject, Batch: 1, MaxAckPending: 1, MaxWaiting: 1, MaxDeliver: 10, FetchWait: time.Second, AckWait: time.Second}
			brokerConfig, err := cfg.BrokerConsumerConfig()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = js.CreateConsumer(ctx, stream, brokerConfig); err != nil {
				t.Fatal(err)
			}
			authority, labels := historicalAuthority(t)
			trade := authority.Trades(labels["buy"])[0]
			wire, err := json.Marshal(trade)
			if err != nil {
				t.Fatal(err)
			}
			publisher, err := natsdriver.NewPublisher(nc, stream)
			if err != nil {
				t.Fatal(err)
			}
			ack, err := publisher.Publish(ctx, subject, digestBytes(wire), wire)
			if err != nil || ack.Sequence != 1 {
				t.Fatal(ack, err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, binary, "-test.run=^TestPhase3CrashWorker$", "-test.timeout=25s")
			child.Env = append(os.Environ(), "L13_PHASE3_CRASH_STAGE="+stage, "L13_PHASE3_CRASH_DSN="+repo.pool.Config().ConnString(), "L13_PHASE3_CRASH_SCHEMA="+strings.Trim(repo.schema, `"`), "L13_PHASE3_CRASH_STREAM="+stream, "L13_PHASE3_CRASH_SUBJECT="+subject)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			expectedExit := map[string]int{"before_commit": 73, "after_commit": 74, "before_ack": 75}[stage]
			if !errors.As(err, &exited) || exited.ExitCode() != expectedExit {
				t.Fatalf("crash stage=%s exit=%v output=%s", stage, err, output)
			}
			driver := phase3Driver(t, ctx, nc, stream, subject)
			message := phase3Fetch(t, ctx, driver)
			if message.Sequence != ack.Sequence || message.Deliveries < 2 || !bytes.Equal(message.Wire, wire) {
				t.Fatal("durable redelivery mismatch", message.Sequence, message.Deliveries)
			}
			persistedBefore := 1
			if stage == "before_commit" {
				persistedBefore = 0
			}
			for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
				if got := count(t, ctx, repo, table); got != persistedBefore {
					t.Fatalf("%s survived stage=%s count=%d", table, stage, got)
				}
			}
			result, err := repo.ApplyAuthorized(ctx, authority, trade, "phase3-crash", "phase3-crash")
			if err != nil || result.Accepted != (stage == "before_commit") || result.Revision != 1 {
				t.Fatal(result, err)
			}
			if err = message.Ack(ctx); err != nil {
				t.Fatal(err)
			}
			aggregate, err := repo.AggregateWindow(ctx, "phase3-crash", trade.Position.Height, authority.admitted[trade.EventID].timestamp, 3600)
			if err != nil || aggregate.PoolFillCount != 1 || aggregate.UniqueActors != 1 || aggregate.VenueVolumeRaw != "5000000000" || len(aggregate.Fees) != 1 || aggregate.Fees[0].Raw != "15000000" {
				t.Fatal(aggregate, err)
			}
			for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
				if got := count(t, ctx, repo, table); got != 1 {
					t.Fatalf("%s duplicated after restart: %d", table, got)
				}
			}
			consumer, err := js.Consumer(ctx, stream, cfg.Durable)
			if err != nil {
				t.Fatal(err)
			}
			state, err := consumer.Info(ctx)
			if err != nil || state.NumAckPending != 0 || state.AckFloor.Stream != ack.Sequence {
				t.Fatal(state, err)
			}
			t.Logf("real worker crash stage=%s exit=%d SQL=%+v broker_ack=%d", stage, expectedExit, aggregate, state.AckFloor.Stream)
		})
	}
}
