package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type sourceEvent struct {
	envelope delivery.Envelope
	wire     []byte
}
type sample struct {
	ID              int    `json:"id"`
	Scheduled       int64  `json:"scheduled_utc_ns"`
	Attempted       int64  `json:"attempted_utc_ns"`
	Completed       int64  `json:"completed_utc_ns"`
	EventID         string `json:"event_id"`
	Status          string `json:"status"`
	Stream          string `json:"stream"`
	Sequence        uint64 `json:"stream_sequence"`
	SourceAuthority string `json:"source_authority"`
	Boundary        string `json:"boundary"`
}

func execute() error {
	stream := flag.String("stream", "", "")
	sourceStream := flag.String("source-stream", "", "")
	schema := flag.String("delivery-schema", "", "")
	rate := flag.Int("rate", 0, "")
	duration := flag.Int("duration", 0, "")
	host := flag.Int("host-index", -1, "")
	startUTC := flag.Int64("start-utc-ns", 0, "")
	flag.Parse()
	identifier := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	if !identifier.MatchString(*stream) || !identifier.MatchString(*sourceStream) || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(*schema) || *rate < 1 || *rate > 2500 || *duration < 60 || *duration > 86400 || *host < 0 || *host > 3 || *startUTC <= 0 {
		return errors.New("exact bounded actual event driver configuration required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	pg, err := pgx.Connect(ctx, "")
	if err != nil {
		return err
	}
	defer pg.Close(context.Background())
	var primary bool
	if err = pg.QueryRow(ctx, "SELECT NOT pg_is_in_recovery()").Scan(&primary); err != nil || !primary {
		return errors.New("canonical PostgreSQL primary required")
	}
	endpoint, err := url.Parse(os.Getenv("NATS_URL"))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || (endpoint.Scheme != "nats" && endpoint.Scheme != "tls") {
		return errors.New("actual NATS endpoint required")
	}
	options := []nats.Option{nats.Timeout(5 * time.Second), nats.NoReconnect()}
	if endpoint.Scheme == "nats" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "::1" {
		return errors.New("plaintext broker confined to owned loopback")
	}
	if endpoint.Scheme == "tls" {
		if os.Getenv("NATS_CA") == "" {
			return errors.New("actual broker CA required")
		}
		options = append(options, nats.RootCAs(os.Getenv("NATS_CA")))
		if os.Getenv("NATS_CERT") != "" || os.Getenv("NATS_KEY") != "" {
			options = append(options, nats.ClientCert(os.Getenv("NATS_CERT"), os.Getenv("NATS_KEY")))
		}
	}
	if os.Getenv("NATS_USER") == "" || os.Getenv("NATS_PASSWORD") == "" {
		return errors.New("actual restricted broker credentials required")
	}
	options = append(options, nats.UserInfo(os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")))
	nc, err := nats.Connect(endpoint.String(), options...)
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	actualStream, err := js.Stream(ctx, *stream)
	if err != nil {
		return err
	}
	info, err := actualStream.Info(ctx)
	if err != nil || info.Config.Storage != jetstream.FileStorage {
		return errors.New("actual file backed stream required")
	}
	publisher, err := natsdriver.NewPublisher(nc, *stream)
	if err != nil {
		return err
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{accounting.MarketSubject: true}}
	count := *rate * *duration
	table := pgx.Identifier{*schema, "delivery_outbox"}.Sanitize()
	query := `SELECT o.envelope FROM ` + table + ` o JOIN indexer.indexer_accounting_publications_v1 p ON p.publication_id=o.event_id JOIN indexer.indexer_accounting_fills_v1 f ON f.source_fill_id=p.payload->>'sourceFillId' JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=f.chain_id AND r.transaction_hash=f.transaction_hash AND r.log_index=f.log_index JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.height=r.height AND c.stream_id=$1 WHERE o.subject='accounting.market.v1' AND p.payload->>'facet'='market' AND p.payload->'fill'=f.payload AND p.payload->>'sourceFactDigest'=f.source_fact_digest AND c.block_hash=r.block_hash AND c.fetch_complete AND c.required_decode_complete AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$1 AND a.previous_height<r.height AND a.last_complete_height>=r.height) AND mod(abs(hashtextextended(p.payload->'positions'->0->>'partitionKey',0)::numeric),4)=$2 ORDER BY r.height,r.transaction_index,r.log_index,o.event_id LIMIT $3`
	events := make(chan sourceEvent, 64)
	sourceResult := make(chan error, 1)
	go func() {
		defer close(events)
		rows, queryErr := pg.Query(ctx, query, *sourceStream, *host, count)
		if queryErr != nil {
			sourceResult <- queryErr
			return
		}
		defer rows.Close()
		observed := 0
		for rows.Next() {
			var wire []byte
			if queryErr = rows.Scan(&wire); queryErr != nil {
				break
			}
			envelope, decodeErr := delivery.Decode(wire, policy, accounting.MarketSubject)
			if decodeErr != nil {
				queryErr = decodeErr
				break
			}
			publication, decodeErr := accounting.DecodePublicationV1(envelope.Payload)
			if decodeErr != nil || publication.Fill == nil || publication.Facet != "market" || envelope.EventID != publication.PublicationID {
				queryErr = errors.New("canonical publication envelope identity differs")
				break
			}
			canonical, encodeErr := accounting.CanonicalPublicationV1(publication)
			if encodeErr != nil || !bytes.Equal(canonical, envelope.Payload) {
				queryErr = errors.New("authentic canonical wire differs")
				break
			}
			height := publication.Fill.Position.Height
			if envelope.SourceOrder.Height != height || envelope.SourceOrder.TransactionIndex != publication.Fill.Position.TransactionIndex || envelope.SourceOrder.LogIndex != publication.Fill.Position.LogIndex {
				queryErr = delivery.ErrIntegrity
				break
			}
			select {
			case events <- sourceEvent{envelope: envelope, wire: append([]byte(nil), wire...)}:
				observed++
			case <-ctx.Done():
				queryErr = ctx.Err()
			}
			if queryErr != nil {
				break
			}
		}
		if queryErr == nil {
			queryErr = rows.Err()
		}
		if queryErr == nil && observed != count {
			queryErr = errors.New("actual unique canonical source inventory insufficient")
		}
		sourceResult <- queryErr
	}()
	start := time.Unix(0, *startUTC)
	if time.Now().Before(start) {
		timer := time.NewTimer(time.Until(start))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	output := json.NewEncoder(os.Stdout)
	var writeMu sync.Mutex
	var wait sync.WaitGroup
	slots := make(chan struct{}, 128)
	var failures atomic.Int64
	write := func(row sample) {
		writeMu.Lock()
		err := output.Encode(row)
		writeMu.Unlock()
		if err != nil {
			cancel()
		}
	}
	for index := 0; index < count; index++ {
		scheduled := start.Add(time.Duration(int64(index) * int64(time.Second) / int64(*rate)))
		if time.Now().Before(scheduled) {
			timer := time.NewTimer(time.Until(scheduled))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		row := sample{ID: index, Scheduled: scheduled.UnixNano(), Status: "missed", SourceAuthority: "canonical_postgres_delivery_envelope", Boundary: "file_jetstream_ack"}
		var event sourceEvent
		select {
		case candidate, ok := <-events:
			if !ok {
				row.Completed = time.Now().UnixNano()
				write(row)
				failures.Add(1)
				continue
			}
			event = candidate
		default:
			row.Completed = time.Now().UnixNano()
			write(row)
			failures.Add(1)
			continue
		}
		row.EventID = event.envelope.EventID
		select {
		case slots <- struct{}{}:
		default:
			row.Completed = time.Now().UnixNano()
			write(row)
			failures.Add(1)
			continue
		}
		wait.Add(1)
		go func(row sample, event sourceEvent) {
			defer wait.Done()
			defer func() { <-slots }()
			row.Attempted = time.Now().UnixNano()
			ack, publishErr := publisher.Publish(ctx, accounting.MarketSubject, event.envelope.EventID, event.wire)
			row.Completed = time.Now().UnixNano()
			if publishErr != nil {
				row.Status = "error"
				if errors.Is(publishErr, context.DeadlineExceeded) {
					row.Status = "timeout"
				}
				failures.Add(1)
			} else {
				row.Status = "received"
				row.Stream = ack.Stream
				row.Sequence = ack.Sequence
			}
			write(row)
		}(row, event)
	}
	wait.Wait()
	cancel()
	if err := <-sourceResult; err != nil {
		return err
	}
	if failures.Load() > 0 {
		return errors.New("scheduled event publications missing or failed")
	}
	return nil
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "actual event adapter failed: "+strconv.Quote(fmt.Sprintf("%T", err)))
		os.Exit(1)
	}
}
