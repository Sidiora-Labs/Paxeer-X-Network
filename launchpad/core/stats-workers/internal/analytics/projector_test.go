package analytics_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestPhase2CanonicalPublicationIdempotencyAndExpiry(t *testing.T) {
	a := phase2Authority(t)
	path := os.Getenv("L15_CANONICAL_PUBLICATION_FILE")
	if path == "" {
		t.Fatal("real admitted L15_CANONICAL_PUBLICATION_FILE required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := accounting.DecodePublicationV1(raw)
	if err != nil || publication.Fill == nil || publication.Facet != "market" {
		t.Fatal(publication, err)
	}
	fill := publication.Fill
	ctx := context.Background()
	projector, err := analytics.NewProjector(a, risk.ProjectionModelV1{}, analytics.PostgresRiskInputs{Authority: a})
	if err != nil {
		t.Fatal(err)
	}
	envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: "accounting.market.v1", ChainID: 125, SourceOrder: delivery.SourceOrder{Height: fill.Position.Height, TransactionIndex: fill.Position.TransactionIndex, LogIndex: fill.Position.LogIndex}, EmittedAt: time.Now().UTC(), Payload: raw, PayloadHash: delivery.Hash(raw)}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = projector.Effect(ctx, tx, envelope); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	original, err := a.ReadSnapshot(ctx, fill.PoolAddress, "pressure-v1", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	tx, err = a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = projector.Effect(ctx, tx, envelope); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var outboxCount int
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_outbox").Scan(&outboxCount); err != nil || outboxCount != 2 {
		t.Fatal(outboxCount, err)
	}
	tx, err = a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = projector.RecomputeExpiryTx(ctx, tx, fill.PoolAddress); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := a.ReadSnapshot(ctx, fill.PoolAddress, "pressure-v1", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, _ := strconv.ParseInt(original.Snapshot.Watermark.ProjectionRevision, 10, 64)
	newRevision, _ := strconv.ParseInt(next.Snapshot.Watermark.ProjectionRevision, 10, 64)
	if newRevision != oldRevision+1 || next.Snapshot.Watermark.BlockHash != original.Snapshot.Watermark.BlockHash || next.Snapshot.Watermark.ObservedAt != original.Snapshot.Watermark.ObservedAt {
		t.Fatal(original, next)
	}
	page, err := a.ReplaySnapshots(ctx, fill.PoolAddress, "0", 100)
	if err != nil || len(page.Items) != 4 {
		t.Fatal(page, err)
	}
}

type phase3CrashDelivery struct {
	*natsdriver.Message
	marker, stage string
}

func (d phase3CrashDelivery) Ack(ctx context.Context) error {
	if d.stage == "before_ack" {
		phase3CrashPoint(d.marker)
	}
	return d.Message.Ack(ctx)
}
func phase3CrashPoint(path string) {
	if err := os.WriteFile(path, []byte("reached"), 0600); err != nil {
		panic(err)
	}
	select {}
}
func phase3Policy(subject string) delivery.Policy {
	return delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{"accounting.market.v1": true}}
}
func phase3ConsumerConfig(stream, subject string) natsdriver.ConsumerConfig {
	return natsdriver.ConsumerConfig{Stream: stream, Durable: "analytics", Subject: subject, Batch: 1, MaxAckPending: 1, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 500 * time.Millisecond, AckWait: time.Second}
}
func TestPhase3WorkerCrashHelper(t *testing.T) {
	if os.Getenv("L15_CRASH_HELPER") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("L15_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	a := analytics.ReactionAuthority{Pool: pool, Schema: os.Getenv("L15_CRASH_SCHEMA")}
	p, err := analytics.NewProjector(a, risk.ProjectionModelV1{}, analytics.PostgresRiskInputs{Authority: a})
	if err != nil {
		t.Fatal(err)
	}
	nc, err := nats.Connect(os.Getenv("L15_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	subject := os.Getenv("L15_CRASH_SUBJECT")
	c, err := natsdriver.NewConsumer(ctx, nc, phase3ConsumerConfig(os.Getenv("L15_CRASH_STREAM"), subject))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := delivery.NewStore(pool, a.Schema, phase3Policy(subject))
	if err != nil {
		t.Fatal(err)
	}
	var messages []*natsdriver.Message
	for len(messages) == 0 && ctx.Err() == nil {
		messages, err = c.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(messages) != 1 {
		t.Fatal("actual broker message required")
	}
	marker, stage := os.Getenv("L15_CRASH_MARKER"), os.Getenv("L15_CRASH_STAGE")
	err = store.Consume(ctx, "analytics", "v1", subject, messages[0].Wire, phase3CrashDelivery{Message: messages[0], marker: marker, stage: stage}, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		if err := p.Effect(ctx, tx, e); err != nil {
			return err
		}
		if stage == "before_commit" {
			phase3CrashPoint(marker)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stage != "after_ack" {
		t.Fatal("missing interruption point")
	}
	phase3CrashPoint(marker)
}
func TestPhase3RealWorkerSIGKILLAndBrokerRedelivery(t *testing.T) {
	broker := os.Getenv("L15_NATS_URL")
	endpoint, err := url.Parse(broker)
	if err != nil || endpoint.Scheme != "nats" || endpoint.User != nil || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		t.Fatal("explicit private loopback L15_NATS_URL required")
	}
	publicationPath := os.Getenv("L15_CANONICAL_PUBLICATION_FILE")
	raw, err := os.ReadFile(publicationPath)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := accounting.DecodePublicationV1(raw)
	if err != nil || publication.Fill == nil {
		t.Fatal("real admitted canonical market fixture required", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"before_commit", "before_ack", "after_ack"} {
		t.Run(stage, func(t *testing.T) {
			a := phase2Authority(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ddl, err := delivery.SchemaDDL(a.Schema)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = a.Pool.Exec(ctx, ddl); err != nil {
				t.Fatal(err)
			}
			nc, err := nats.Connect(broker)
			if err != nil {
				t.Fatal(err)
			}
			defer nc.Close()
			js, err := jetstream.New(nc)
			if err != nil {
				t.Fatal(err)
			}
			name, subject := "l15_"+a.Schema, "l15."+a.Schema+".market"
			stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{subject}, Storage: jetstream.FileStorage})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				if err := js.DeleteStream(cleanup, name); err != nil {
					t.Error(err)
				}
			}()
			cfg := phase3ConsumerConfig(name, subject)
			config, err := cfg.BrokerConsumerConfig()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = stream.CreateConsumer(ctx, config); err != nil {
				t.Fatal(err)
			}
			f := publication.Fill
			envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: "accounting.market.v1", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: f.Position.Height, TransactionIndex: f.Position.TransactionIndex, LogIndex: f.Position.LogIndex}, EmittedAt: time.Now().UTC(), TraceID: "l15-crash", Payload: raw, PayloadHash: delivery.Hash(raw)}
			wire, err := envelope.Encode(phase3Policy(subject), subject)
			if err != nil {
				t.Fatal(err)
			}
			publisher, err := natsdriver.NewPublisher(nc, name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = publisher.Publish(ctx, subject, envelope.EventID, wire); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			marker := filepath.Join(dir, "reached")
			logPath := filepath.Join(dir, "worker.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			child := exec.Command(executable, "-test.run=^TestPhase3WorkerCrashHelper$", "-test.timeout=25s")
			child.Env = append(os.Environ(), "L15_CRASH_HELPER=1", "L15_CRASH_SCHEMA="+a.Schema, "L15_CRASH_SUBJECT="+subject, "L15_CRASH_STREAM="+name, "L15_CRASH_MARKER="+marker, "L15_CRASH_STAGE="+stage)
			child.Stdout, child.Stderr = log, log
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if child.ProcessState == nil {
					child.Process.Kill()
					child.Wait()
				}
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err = os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					data, _ := os.ReadFile(logPath)
					t.Fatalf("worker did not reach %s: %s", stage, data)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err = child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			err = child.Wait()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || !exited.ProcessState.Sys().(syscall.WaitStatus).Signaled() || exited.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatal("actual worker SIGKILL required", err)
			}
			wantBefore := 1
			if stage == "before_commit" {
				wantBefore = 0
			}
			var trades, inbox int
			if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_trades").Scan(&trades); err != nil || trades != wantBefore {
				t.Fatal(trades, wantBefore, err)
			}
			if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".delivery_inbox").Scan(&inbox); err != nil || inbox != wantBefore {
				t.Fatal(inbox, wantBefore, err)
			}
			consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			store, err := delivery.NewStore(a.Pool, a.Schema, phase3Policy(subject))
			if err != nil {
				t.Fatal(err)
			}
			projector, err := analytics.NewProjector(a, risk.ProjectionModelV1{}, analytics.PostgresRiskInputs{Authority: a})
			if err != nil {
				t.Fatal(err)
			}
			if stage != "after_ack" {
				var messages []*natsdriver.Message
				for len(messages) == 0 && ctx.Err() == nil {
					messages, err = consumer.Fetch(ctx)
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(messages) != 1 || messages[0].Deliveries < 2 {
					t.Fatal("actual broker redelivery required", messages)
				}
				if err = store.Consume(ctx, "analytics", "v1", subject, messages[0].Wire, messages[0], projector.Effect); err != nil {
					t.Fatal(err)
				}
			}
			var outbox int
			if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_trades").Scan(&trades); err != nil || trades != 1 {
				t.Fatal(trades, err)
			}
			if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_outbox").Scan(&outbox); err != nil || outbox != 2 {
				t.Fatal(outbox, err)
			}
			current, err := a.ReadSnapshot(ctx, f.PoolAddress, "pressure-v1", time.Now().Unix())
			if err != nil {
				t.Fatal(err)
			}
			var pressure analytics.PressureWindows
			if err = json.Unmarshal(current.Snapshot.Payload, &pressure); err != nil {
				t.Fatal(err)
			}
			timestamp, err := strconv.ParseInt(f.BlockTimestampSeconds, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			for _, window := range []struct {
				duration int64
				value    analytics.Pressure
			}{{3600, pressure.OneHour}, {86400, pressure.TwentyFourHours}} {
				expectedCount := int64(0)
				if timestamp > current.Snapshot.CalculatedAt-window.duration && timestamp <= current.Snapshot.CalculatedAt {
					expectedCount = 1
				}
				buy, sell := int64(0), int64(0)
				buyRaw, sellRaw := "0", "0"
				if f.Side == "buy" {
					buy = expectedCount
					if buy == 1 {
						buyRaw = f.USDLAmountRaw
					}
				} else {
					sell = expectedCount
					if sell == 1 {
						sellRaw = f.USDLAmountRaw
					}
				}
				if window.value.BuyCount != buy || window.value.SellCount != sell || window.value.BuyUSDLRaw != buyRaw || window.value.SellUSDLRaw != sellRaw {
					t.Fatal("independent canonical window mismatch", window.value)
				}
				if expectedCount == 0 {
					if window.value.BuyCountShare != nil || window.value.BuyNotionalShare != nil || window.value.Direction != "unavailable" || !reflect.DeepEqual(window.value.Flags, []string{"no_activity", "low_activity"}) {
						t.Fatal("independent empty-window pressure mismatch", window.value)
					}
				} else {
					expectedShare, expectedDirection := int64(0), "bearish"
					if f.Side == "buy" {
						expectedShare, expectedDirection = 10000, "bullish"
					}
					if window.value.BuyCountShare == nil || *window.value.BuyCountShare != expectedShare || window.value.BuyNotionalShare == nil || *window.value.BuyNotionalShare != expectedShare || window.value.Direction != expectedDirection || !reflect.DeepEqual(window.value.Flags, []string{"low_activity"}) {
						t.Fatal("independent rational pressure mismatch", window.value)
					}
				}
			}
			info, err := stream.Consumer(ctx, cfg.Durable)
			if err != nil {
				t.Fatal(err)
			}
			state, err := info.Info(ctx)
			if err != nil || state.NumAckPending != 0 {
				t.Fatal("acknowledgement did not persist", state, err)
			}
			replay, err := a.ReplaySnapshots(ctx, f.PoolAddress, "0", 100)
			if err != nil || len(replay.Items) != 2 {
				t.Fatal(replay, err)
			}
			router := chi.NewRouter()
			httpapi.RegisterDurableAnalytics(router, a)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/stats/"+f.PoolAddress+"/pressure", nil))
			var served analytics.SnapshotRead
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &served) != nil || served.Snapshot.Watermark != current.Snapshot.Watermark || served.Snapshot.CalculatedAt != current.Snapshot.CalculatedAt {
				t.Fatal(response.Code, response.Body.String())
			}
			var publicPressure analytics.PressureWindows
			if json.Unmarshal(served.Snapshot.Payload, &publicPressure) != nil || !reflect.DeepEqual(publicPressure, pressure) {
				t.Fatal("public pressure differs from persisted canonical pressure")
			}
		})
	}
}

func TestPhase3CanonicalHostileInputsHaveNoDatabaseEffect(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	raw, err := os.ReadFile(os.Getenv("L15_CANONICAL_PUBLICATION_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	publication, err := accounting.DecodePublicationV1(raw)
	if err != nil || publication.Fill == nil {
		t.Fatal("real canonical fixture required", err)
	}
	projector, err := analytics.NewProjector(a, risk.ProjectionModelV1{}, analytics.PostgresRiskInputs{Authority: a})
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC()
	apply := func(fill accounting.FillV1) error {
		tx, err := a.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err = projector.ApplyFillTx(ctx, tx, fill, observed); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	original := *publication.Fill
	for _, mutate := range []func(*accounting.FillV1){
		func(f *accounting.FillV1) { f.USDLAmountRaw = "abc" },
		func(f *accounting.FillV1) { f.USDLAmountRaw = "-1" },
		func(f *accounting.FillV1) { f.USDLAmountRaw = "01" },
		func(f *accounting.FillV1) { f.Provenance.ReceiptSHA256 = "" },
		func(f *accounting.FillV1) { f.Provenance.DeploymentID = "" },
		func(f *accounting.FillV1) { f.BlockTimestampSeconds = strconv.FormatInt(observed.Unix()+1, 10) },
		func(f *accounting.FillV1) { f.SourceFillID = "invalid" },
	} {
		hostile := original
		mutate(&hostile)
		if err = apply(hostile); err == nil {
			t.Fatal("hostile fill accepted", hostile.SourceFillID)
		}
		for _, table := range []string{"analytics_trades", "analytics_progress", "analytics_snapshots", "analytics_outbox"} {
			var count int
			if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+"."+table).Scan(&count); err != nil || count != 0 {
				t.Fatal(table, count, err)
			}
		}
	}
	if err = apply(original); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		if err = apply(original); err != nil {
			t.Fatal(err)
		}
	}
	hostile := original
	hostile.BlockTimestampSeconds = strconv.FormatInt(observed.Unix(), 10)
	if hostile.BlockTimestampSeconds == original.BlockTimestampSeconds {
		hostile.BlockTimestampSeconds = strconv.FormatInt(observed.Unix()-1, 10)
	}
	if err = apply(hostile); !errors.Is(err, analytics.ErrConflict) {
		t.Fatal("conflicting immutable source accepted", err)
	}
	envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: "accounting.market.v1", ChainID: 125, Payload: raw, PayloadHash: delivery.Hash(raw), SourceOrder: delivery.SourceOrder{Height: original.Position.Height, TransactionIndex: original.Position.TransactionIndex, LogIndex: "999999"}, EmittedAt: observed}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = projector.Effect(ctx, tx, envelope); !errors.Is(err, analytics.ErrConflict) {
		t.Fatal("conflicting delivery identity accepted", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var trades, outbox int
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_trades").Scan(&trades); err != nil || trades != 1 {
		t.Fatal(trades, err)
	}
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".analytics_outbox").Scan(&outbox); err != nil || outbox != 2 {
		t.Fatal(outbox, err)
	}
}
