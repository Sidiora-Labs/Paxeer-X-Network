package publish

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	chain           = 125
	market          = 7
)

var (
	hashA = "0x" + strings.Repeat("aa", 32)
	hashB = "0x" + strings.Repeat("bb", 32)
)

func subjects(t *testing.T) (fill, rewind string) {
	t.Helper()
	fill, err := stream.MarketSubject(chain, market, "fill")
	if err != nil {
		t.Fatal(err)
	}
	rewind, err = stream.ChainSubject(chain, "rewind")
	if err != nil {
		t.Fatal(err)
	}
	return fill, rewind
}

func testPolicy(t *testing.T) delivery.Policy {
	fill, rewind := subjects(t)
	return delivery.Policy{
		Subjects:   map[string]bool{fill: true, rewind: true},
		EventTypes: map[string]bool{"fill": true, "rewind": true},
	}
}

func testConfig() Config {
	return Config{Owner: "perps-publisher", Batch: 100, MaxAttempts: 2, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, IdleInterval: 20 * time.Millisecond, MaxHeadAge: 30 * time.Second}
}

// openRows counts the outbox rows still to be published or announced.
const openRows = `SELECT count(*) FROM perps.delivery_outbox WHERE state='pending' OR (state='quarantined' AND broker_sequence IS NULL)`

// controlID is the Nats-Msg-Id of the control message of the row at seq
// with event id id.
func controlID(seq int64, id string) string {
	sum := sha256.Sum256([]byte(id))
	return "control-" + strconv.FormatInt(seq, 10) + "-" + hex.EncodeToString(sum[:16])
}

// metricValue reads the value of a counter or gauge collector.
func metricValue(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()
	var out dto.Metric
	if err := m.Write(&out); err != nil {
		t.Fatal(err)
	}
	if out.Counter != nil {
		return out.Counter.GetValue()
	}
	return out.Gauge.GetValue()
}

// drainOpen runs p until no outbox row is open, then stops it.
func drainOpen(t *testing.T, ctx context.Context, db *sql.DB, p *Publisher) {
	t.Helper()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- p.Run(runCtx) }()
	deadline := time.Now().Add(30 * time.Second)
	for count(t, db, openRows) != 0 {
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("outbox not drained; Run = %v", <-done)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

type event struct {
	subject string
	env     delivery.Envelope
}

// newEvent builds the envelope of one log; txIndex -1 marks a block event.
func newEvent(t *testing.T, kind string, block uint64, hash string, txIndex, logIndex int, payload string) event {
	t.Helper()
	fill, rewind := subjects(t)
	subject := fill
	if kind == "rewind" {
		subject = rewind
	}
	id, err := natsdriver.MsgID(fmt.Sprintf("%d:%d:%s:%d:%d:0", chain, block, hash, txIndex, logIndex))
	if err != nil {
		t.Fatal(err)
	}
	tx := txIndex
	if tx < 0 {
		tx = 0
	}
	p := json.RawMessage(payload)
	return event{subject: subject, env: delivery.Envelope{
		EventID: id, EventType: kind, SchemaVersion: 1, ChainID: chain,
		SourceOrder: delivery.SourceOrder{Height: strconv.FormatUint(block, 10), TransactionIndex: strconv.Itoa(tx), LogIndex: strconv.Itoa(logIndex)},
		EmittedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		TraceID:     id, PayloadHash: delivery.Hash(p), Payload: p,
	}}
}

func fillEvent(t *testing.T, block uint64, hash string, txIndex, logIndex int) event {
	t.Helper()
	row := fmt.Sprintf(`{"block_height":"%d","block_hash":"%s","tx_index":"%d","log_index":"%d","market_id":"%d","price_raw":"115792089237316195423570985008687907853269984665640564039457584007913129639935","size_raw":"9007199254740993123456789"}`, block, hash, txIndex, logIndex, market)
	return newEvent(t, "fill", block, hash, txIndex, logIndex, row)
}

// startCluster runs three in-process JetStream servers, the replica count of stream.Config.
func startCluster(t *testing.T) *nats.Conn {
	t.Helper()
	ports := make([]int, 3)
	routes := make([]string, 3)
	for i := range ports {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = l.Addr().(*net.TCPAddr).Port
		if err = l.Close(); err != nil {
			t.Fatal(err)
		}
		routes[i] = "nats://127.0.0.1:" + strconv.Itoa(ports[i])
	}
	servers := make([]*server.Server, 3)
	urls := make([]string, 3)
	for i := range servers {
		ns, err := server.NewServer(&server.Options{
			ServerName: "perps-" + strconv.Itoa(i), Host: "127.0.0.1", Port: server.RANDOM_PORT,
			JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
			Cluster: server.ClusterOpts{Name: "perps-test", Host: "127.0.0.1", Port: ports[i]},
			Routes:  server.RoutesFromStr(strings.Join(routes, ",")),
		})
		if err != nil {
			t.Fatal(err)
		}
		go ns.Start()
		t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
		servers[i] = ns
	}
	for i, ns := range servers {
		if !ns.ReadyForConnections(10 * time.Second) {
			t.Fatalf("server %d not ready", i)
		}
		urls[i] = ns.ClientURL()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		formed := false
		for _, ns := range servers {
			if ns.JetStreamIsLeader() && len(ns.JetStreamClusterPeers()) == 3 {
				formed = true
			}
		}
		if formed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("JetStream cluster has no meta leader with three peers")
		}
		time.Sleep(50 * time.Millisecond)
	}
	nc, err := nats.Connect(strings.Join(urls, ","))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// deleteStream deletes the perps stream and waits until the cluster answers
// it not found, so the next create of the name does not race the removal of
// the stream assignment across the three servers.
func deleteStream(t *testing.T, ctx context.Context, js jetstream.JetStream) {
	t.Helper()
	if err := js.DeleteStream(ctx, stream.Name); err != nil {
		t.Fatalf("delete %s: %v", stream.Name, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := js.Stream(attempt, stream.Name)
		cancel()
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s still answers after delete: %v", stream.Name, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func reset(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox, perps.delivery_inbox, perps.delivery_quarantine RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
}

func enqueue(t *testing.T, ctx context.Context, db *sql.DB, s *delivery.Store, events ...event) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if _, err = s.Enqueue(ctx, tx, e.subject, e.env); err != nil {
			_ = tx.Rollback()
			t.Fatalf("Enqueue %s: %v", e.env.EventID, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// drain runs the publisher until the outbox holds no pending row.
func drain(t *testing.T, p *Publisher, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	deadline := time.Now().Add(30 * time.Second)
	for count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE state='pending'`) != 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("outbox not drained; Run = %v", <-done)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

type outboxRow struct {
	seq            int64
	eventID, state string
	failure        string
	brokerStream   string
	brokerSequence int64
	attempts       int
}

func outbox(t *testing.T, db *sql.DB) []outboxRow {
	t.Helper()
	rows, err := db.Query(`SELECT perps_seq,event_id,state,COALESCE(failure_class,''),COALESCE(broker_stream,''),COALESCE(broker_sequence,0),attempts FROM perps.delivery_outbox ORDER BY perps_seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err = rows.Scan(&r.seq, &r.eventID, &r.state, &r.failure, &r.brokerStream, &r.brokerSequence, &r.attempts); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func streamState(t *testing.T, ctx context.Context, s jetstream.Stream) jetstream.StreamState {
	t.Helper()
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info.State
}

func wireOf(t *testing.T, policy delivery.Policy, e event) []byte {
	t.Helper()
	wire, err := e.env.Encode(policy, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// padded builds a fill envelope whose encoded size is exactly size bytes.
func padded(t *testing.T, policy delivery.Policy, block uint64, size int) event {
	t.Helper()
	build := func(n int) event {
		return newEvent(t, "fill", block, hashA, 0, 0, `{"block_height":"`+strconv.FormatUint(block, 10)+`","pad":"`+strings.Repeat("a", n)+`"}`)
	}
	// The payload is appended verbatim, so each pad byte adds one wire byte.
	e := build(size - len(wireOf(t, policy, build(0))))
	if size <= delivery.MaxEnvelopeBytes && len(wireOf(t, policy, e)) != size {
		t.Fatalf("padded envelope is not %d bytes", size)
	}
	return e
}

// testDatabase creates a database of its own on the server of
// PERPS_DATA_TEST_DATABASE_URL and drops it after the test, so the outbox
// resets of this package never race the tests of another package that share
// the server.
func testDatabase(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(testDatabaseEnv)
	if dsn == "" {
		t.Fatal("TestPublisher requires " + testDatabaseEnv)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		if dsn, err = pq.ParseURL(dsn); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := store.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "publish_test_" + strings.ToLower(rand.Text())
	if _, err = admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + pq.QuoteIdentifier(name)); err != nil {
			t.Error(err)
		}
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	return dsn + " dbname=" + name
}

func TestPublisher(t *testing.T) {
	url := testDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err = store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	policy := testPolicy(t)
	outboxStore, err := delivery.NewStore(db, "perps", policy)
	if err != nil {
		t.Fatal(err)
	}
	nc := startCluster(t)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureStream(ctx, nc, 0); err == nil || err.Error() != "publish: context, open connection and chain required" {
		t.Fatalf("EnsureStream chain 0: %v", err)
	}
	invalid := testConfig()
	invalid.PublishTimeout = invalid.LeaseDuration
	if _, err = New(db, nc, policy, invalid); err == nil || err.Error() != "publish: invalid publisher configuration" {
		t.Fatalf("New with publish timeout at the lease: %v", err)
	}
	if _, err = New(db, nil, policy, testConfig()); err == nil || err.Error() != "publish: open connection required" {
		t.Fatalf("New without connection: %v", err)
	}
	if _, err = New(nil, nc, policy, testConfig()); err == nil || err.Error() != "delivery: invalid store configuration" {
		t.Fatalf("New without database: %v", err)
	}
	publisher, err := New(db, nc, policy, testConfig())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ensure_stream", func(t *testing.T) {
		if _, err := js.Stream(ctx, stream.Name); !errors.Is(err, jetstream.ErrStreamNotFound) {
			t.Fatalf("stream before EnsureStream: %v", err)
		}
		if err := EnsureStream(ctx, nc, chain); err != nil {
			t.Fatal(err)
		}
		s, err := js.Stream(ctx, stream.Name)
		if err != nil {
			t.Fatal(err)
		}
		info, err := s.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := stream.Config(chain)
		got := info.Config
		if got.Name != stream.Name || len(got.Subjects) != 1 || got.Subjects[0] != "perps.125.>" || got.Storage != jetstream.FileStorage || got.Retention != jetstream.LimitsPolicy || got.Replicas != 3 || got.MaxAge != 72*time.Hour || got.Discard != jetstream.DiscardNew || !got.DenyDelete || !got.DenyPurge || got.Duplicates != 2*time.Minute || got.MaxMsgSize != 1048576 {
			t.Fatalf("created stream config = %+v, want %+v", got, want)
		}
		if info.Cluster == nil || len(info.Cluster.Replicas)+1 != 3 {
			t.Fatalf("stream placement = %+v", info.Cluster)
		}
		if err = EnsureStream(ctx, nc, chain); err != nil {
			t.Fatalf("EnsureStream on the matching stream: %v", err)
		}
	})

	t.Run("refuses_drift", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			mutate func(*jetstream.StreamConfig)
			want   string
		}{
			{"replicas", func(c *jetstream.StreamConfig) { c.Replicas = 1 }, "stream drift: replicas 1 want 3"},
			{"storage", func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage }, "stream drift: storage Memory want File"},
			{"retention", func(c *jetstream.StreamConfig) { c.Retention = jetstream.InterestPolicy }, "stream drift: retention Interest want Limits"},
			{"subjects", func(c *jetstream.StreamConfig) { c.Subjects = []string{"perps.126.>"} }, "stream drift: subjects [perps.126.>] want [perps.125.>]"},
			{"max_age", func(c *jetstream.StreamConfig) { c.MaxAge = time.Hour }, "stream drift: max_age 1h0m0s want 72h0m0s"},
			{"discard", func(c *jetstream.StreamConfig) { c.Discard = jetstream.DiscardOld }, "stream drift: discard DiscardOld want DiscardNew"},
			{"deny_delete", func(c *jetstream.StreamConfig) { c.DenyDelete = false }, "stream drift: deny_delete false want true"},
			{"deny_purge", func(c *jetstream.StreamConfig) { c.DenyPurge = false }, "stream drift: deny_purge false want true"},
			{"duplicates", func(c *jetstream.StreamConfig) { c.Duplicates = time.Minute }, "stream drift: duplicates 1m0s want 2m0s"},
			{"max_msg_size", func(c *jetstream.StreamConfig) { c.MaxMsgSize = 1024 }, "stream drift: max_msg_size 1024 want 1048576"},
			{"max_bytes", func(c *jetstream.StreamConfig) { c.MaxBytes = 1 << 30 }, "stream drift: max_bytes 1073741824 want -1"},
			{"max_msgs", func(c *jetstream.StreamConfig) { c.MaxMsgs = 1000 }, "stream drift: max_msgs 1000 want -1"},
			{"max_msgs_per_subject", func(c *jetstream.StreamConfig) { c.MaxMsgsPerSubject = 10 }, "stream drift: max_msgs_per_subject 10 want -1"},
		} {
			deleteStream(t, ctx, js)
			cfg := stream.Config(chain)
			c.mutate(&cfg)
			if _, err := js.CreateStream(ctx, cfg); err != nil {
				t.Fatalf("%s: create: %v", c.name, err)
			}
			err := EnsureStream(ctx, nc, chain)
			if !errors.Is(err, ErrDrift) || err.Error() != c.want {
				t.Fatalf("%s: EnsureStream = %v, want %s", c.name, err, c.want)
			}
		}
		deleteStream(t, ctx, js)
		if err := EnsureStream(ctx, nc, chain); err != nil {
			t.Fatalf("EnsureStream after the drifted stream was removed: %v", err)
		}
	})

	perps, err := js.Stream(ctx, stream.Name)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("monotonic_seq", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		var events []event
		for block := uint64(400); block <= 409; block++ {
			var inBlock []event
			for txIndex := 0; txIndex < 5; txIndex++ {
				inBlock = append(inBlock, fillEvent(t, block, hashA, txIndex, 0))
			}
			enqueue(t, ctx, db, outboxStore, inBlock...)
			events = append(events, inBlock...)
		}
		drain(t, publisher, db)
		rows := outbox(t, db)
		if len(rows) != 50 {
			t.Fatalf("outbox holds %d rows, want 50", len(rows))
		}
		var last int64
		for i, r := range rows {
			e := events[i]
			if r.seq != int64(i+1) || r.eventID != e.env.EventID || r.state != "published" || r.brokerStream != stream.Name || r.brokerSequence != int64(base)+int64(i)+1 || r.brokerSequence <= last || r.attempts != 1 {
				t.Fatalf("row %d = %+v", i, r)
			}
			last = r.brokerSequence
			msg, err := perps.GetMsg(ctx, uint64(r.brokerSequence))
			if err != nil {
				t.Fatal(err)
			}
			if msg.Subject != e.subject || msg.Header.Get(jetstream.MsgIDHeader) != e.env.EventID || msg.Header.Get(HeaderSeq) != strconv.Itoa(i+1) || msg.Header.Get(HeaderBlock) != e.env.SourceOrder.Height || msg.Header.Get(HeaderKind) != "fill" || !bytes.Equal(msg.Data, wireOf(t, policy, e)) {
				t.Fatalf("message %d = %s %v", r.brokerSequence, msg.Subject, msg.Header)
			}
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+50 {
			t.Fatalf("stream last sequence %d, want %d", state.LastSeq, base+50)
		}
	})

	t.Run("dedup_msg_id", func(t *testing.T) {
		reset(t, ctx, db)
		before := streamState(t, ctx, perps)
		first := fillEvent(t, 500, hashA, 2, 0)
		rewritten := fillEvent(t, 500, hashB, 2, 0)
		if first.env.EventID == rewritten.env.EventID {
			t.Fatal("a rewritten block hash maps to the same event id")
		}
		enqueue(t, ctx, db, outboxStore, first)
		drain(t, publisher, db)
		// A replay rewrites the outbox with the first event id inside the duplicate window.
		reset(t, ctx, db)
		enqueue(t, ctx, db, outboxStore, first)
		drain(t, publisher, db)
		rows := outbox(t, db)
		if len(rows) != 1 || rows[0].state != "published" || rows[0].brokerSequence != int64(before.LastSeq)+1 {
			t.Fatalf("republished row = %+v", rows)
		}
		if state := streamState(t, ctx, perps); state.Msgs != before.Msgs+1 || state.LastSeq != before.LastSeq+1 {
			t.Fatalf("stream after republish holds %d messages, last %d", state.Msgs, state.LastSeq)
		}
		enqueue(t, ctx, db, outboxStore, rewritten)
		drain(t, publisher, db)
		rows = outbox(t, db)
		if len(rows) != 2 || rows[1].eventID != rewritten.env.EventID || rows[1].state != "published" || rows[1].brokerSequence != int64(before.LastSeq)+2 {
			t.Fatalf("rewritten row = %+v", rows)
		}
		if state := streamState(t, ctx, perps); state.Msgs != before.Msgs+2 {
			t.Fatalf("stream holds %d new entries, want 2", state.Msgs-before.Msgs)
		}
		msg, err := perps.GetMsg(ctx, before.LastSeq+2)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Header.Get(jetstream.MsgIDHeader) != rewritten.env.EventID || msg.Header.Get(HeaderSeq) != "2" || !bytes.Equal(msg.Data, wireOf(t, policy, rewritten)) {
			t.Fatalf("second entry carries %v", msg.Header)
		}
	})

	t.Run("rewind_message", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		for block := uint64(100); block <= 108; block++ {
			enqueue(t, ctx, db, outboxStore, fillEvent(t, block, hashA, 0, 0))
		}
		drain(t, publisher, db)
		// The fork at 106 enqueues the rewind and the rewritten heights in one transaction.
		rewind := newEvent(t, "rewind", 106, hashB, -1, 0, `{"from":106}`)
		forked := []event{rewind}
		for block := uint64(106); block <= 108; block++ {
			forked = append(forked, fillEvent(t, block, hashB, 0, 0))
		}
		enqueue(t, ctx, db, outboxStore, forked...)
		drain(t, publisher, db)
		rows := outbox(t, db)
		if len(rows) != 13 {
			t.Fatalf("outbox holds %d rows, want 13", len(rows))
		}
		rewindRow := rows[9]
		if rewindRow.eventID != rewind.env.EventID || rewindRow.state != "published" || rewindRow.brokerSequence != int64(base)+10 {
			t.Fatalf("rewind row = %+v", rewindRow)
		}
		for i, r := range rows[10:] {
			if r.eventID != forked[i+1].env.EventID || r.state != "published" || r.brokerSequence <= rewindRow.brokerSequence {
				t.Fatalf("rewritten row %d = %+v not above rewind %d", i, r, rewindRow.brokerSequence)
			}
		}
		msg, err := perps.GetMsg(ctx, uint64(rewindRow.brokerSequence))
		if err != nil {
			t.Fatal(err)
		}
		if msg.Subject != "perps.125.chain.rewind" || msg.Header.Get(HeaderKind) != "rewind" || msg.Header.Get(HeaderBlock) != "106" || msg.Header.Get(HeaderSeq) != "10" || !bytes.Equal(msg.Data, wireOf(t, policy, rewind)) {
			t.Fatalf("rewind message = %s %v", msg.Subject, msg.Header)
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+13 {
			t.Fatalf("stream last sequence %d, want %d", state.LastSeq, base+13)
		}
	})

	t.Run("envelope_limit", func(t *testing.T) {
		reset(t, ctx, db)
		before := streamState(t, ctx, perps)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		sibling := fillEvent(t, 600, hashA, 1, 0)
		if _, err = outboxStore.Enqueue(ctx, tx, sibling.subject, sibling.env); err != nil {
			t.Fatal(err)
		}
		over := padded(t, policy, 600, delivery.MaxEnvelopeBytes+1)
		if _, err = outboxStore.Enqueue(ctx, tx, over.subject, over.env); err == nil || err.Error() != "delivery: oversized envelope" {
			t.Fatalf("Enqueue of a %d byte envelope: %v", delivery.MaxEnvelopeBytes+1, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox`); n != 0 {
			t.Fatalf("rolled back block transaction left %d rows", n)
		}

		// An envelope at the limit is stored, but with its headers the broker
		// refuses it for size. That can never succeed: the row is quarantined
		// permanently on its first attempt and announced in its place.
		limit := padded(t, policy, 601, delivery.MaxEnvelopeBytes)
		after := fillEvent(t, 602, hashA, 0, 0)
		enqueue(t, ctx, db, outboxStore, limit, after)
		var logs bytes.Buffer
		cfg := testConfig()
		cfg.MaxHeadAge = time.Second
		cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		p, err := New(db, nc, policy, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// The head row stuck past the bound turns readiness off.
		time.Sleep(1100 * time.Millisecond)
		if err := p.Ready(ctx); err == nil || !strings.Contains(err.Error(), "oldest open outbox row") {
			t.Fatalf("Ready with a stuck head row = %v", err)
		}
		drainOpen(t, ctx, db, p)
		rows := outbox(t, db)
		if len(rows) != 2 {
			t.Fatalf("outbox = %+v", rows)
		}
		if r := rows[0]; r.eventID != limit.env.EventID || r.state != "quarantined" || r.failure != "oversized" || r.attempts != 2 || r.brokerStream != stream.Name || r.brokerSequence != int64(before.LastSeq)+1 {
			t.Fatalf("refused row = %+v", r)
		}
		if r := rows[1]; r.state != "published" || r.brokerSequence != int64(before.LastSeq)+2 {
			t.Fatalf("row behind the refused row = %+v", r)
		}
		msg, err := perps.GetMsg(ctx, before.LastSeq+1)
		if err != nil {
			t.Fatal(err)
		}
		seq := strconv.FormatInt(rows[0].seq, 10)
		if msg.Subject != "perps.125.control" || msg.Header.Get(delivery.HeaderControl) != delivery.ControlSkipped || msg.Header.Get(HeaderSeq) != seq || msg.Header.Get(jetstream.MsgIDHeader) != controlID(rows[0].seq, limit.env.EventID) || len(msg.Data) >= delivery.MaxEnvelopeBytes {
			t.Fatalf("control message = %s %v", msg.Subject, msg.Header)
		}
		if state := streamState(t, ctx, perps); state.LastSeq != before.LastSeq+2 {
			t.Fatalf("stream last sequence %d, want %d: one control message and one row", state.LastSeq, before.LastSeq+2)
		}
		// The announced row is resolved: readiness returns once the head drains.
		if n, err := outboxStore.Quarantined(ctx); err != nil || n != 0 {
			t.Fatalf("quarantined = %d, %v", n, err)
		}
		if err := p.Ready(ctx); err != nil {
			t.Fatalf("Ready after drain = %v", err)
		}
		if n := metricValue(t, p.quarantined); n != 1 {
			t.Fatalf("perps_outbox_quarantined_total = %v, want 1", n)
		}
		if age := metricValue(t, p.headAge); age != 0 {
			t.Fatalf("perps_outbox_head_age_seconds = %v, want 0", age)
		}
		line := logs.String()
		for _, want := range []string{"level=ERROR", `msg="perps outbox row quarantined permanently"`, "perps_seq=" + seq, limit.env.EventID, "class=oversized"} {
			if !strings.Contains(line, want) {
				t.Fatalf("log %q lacks %q", line, want)
			}
		}
	})

	t.Run("refused_message_skipped_in_order", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		first := fillEvent(t, 700, hashA, 0, 0)
		refused := padded(t, policy, 701, delivery.MaxEnvelopeBytes)
		later := []event{fillEvent(t, 702, hashA, 0, 0), fillEvent(t, 703, hashA, 0, 0)}
		enqueue(t, ctx, db, outboxStore, append([]event{first, refused}, later...)...)
		drainOpen(t, ctx, db, publisher)
		rows := outbox(t, db)
		if len(rows) != 4 || rows[0].state != "published" || rows[0].brokerSequence != int64(base)+1 {
			t.Fatalf("first row = %+v", rows)
		}
		if r := rows[1]; r.state != "quarantined" || r.failure != "oversized" || r.brokerSequence != int64(base)+2 {
			t.Fatalf("refused row = %+v", r)
		}
		msg, err := perps.GetMsg(ctx, base+2)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Subject != "perps.125.control" || msg.Header.Get(HeaderSeq) != strconv.FormatInt(rows[1].seq, 10) {
			t.Fatalf("control message = %s %v", msg.Subject, msg.Header)
		}
		for i, r := range rows[2:] {
			if r.state != "published" || r.brokerSequence != int64(base)+int64(i)+3 {
				t.Fatalf("row behind the refused row = %+v", r)
			}
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+4 {
			t.Fatalf("stream last sequence %d, want %d", state.LastSeq, base+4)
		}
		if n, err := outboxStore.Quarantined(ctx); err != nil || n != 0 {
			t.Fatalf("quarantined = %d, %v", n, err)
		}
	})

	t.Run("skipped_row_control_message", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		fill, _ := subjects(t)
		// The publishers admit no rewind, so the rewind row fails to decode
		// and is quarantined permanently.
		narrow := delivery.Policy{Subjects: map[string]bool{fill: true}, EventTypes: map[string]bool{"fill": true, "rewind": true}}
		events := []event{fillEvent(t, 950, hashA, 0, 0), newEvent(t, "rewind", 951, hashA, -1, 0, `{"from":951}`), fillEvent(t, 952, hashA, 0, 0), fillEvent(t, 953, hashA, 0, 0)}
		enqueue(t, ctx, db, outboxStore, events...)
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 2)
		// Two replicas drain the outbox at once.
		for _, name := range []string{"perps-publisher-a", "perps-publisher-b"} {
			cfg := testConfig()
			cfg.Owner = name
			cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			p, err := New(db, nc, narrow, cfg)
			if err != nil {
				t.Fatal(err)
			}
			go func() { done <- p.Run(runCtx) }()
		}
		deadline := time.Now().Add(30 * time.Second)
		for count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE state='pending' OR (state='quarantined' AND failure_class<>'transient' AND broker_sequence IS NULL)`) != 0 {
			if time.Now().After(deadline) {
				stop()
				t.Fatalf("outbox not closed; Run = %v", <-done)
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		for range 2 {
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("Run = %v", err)
			}
		}
		rows := outbox(t, db)
		if len(rows) != 4 {
			t.Fatalf("outbox holds %d rows, want 4", len(rows))
		}
		for i, r := range rows {
			want := "published"
			if i == 1 {
				want = "quarantined"
				if r.failure != "schema_violation" {
					t.Fatalf("skipped row = %+v", r)
				}
			}
			if r.seq != int64(i+1) || r.state != want || r.brokerStream != stream.Name || r.brokerSequence != int64(base)+int64(i)+1 {
				t.Fatalf("row %d = %+v", i, r)
			}
			msg, err := perps.GetMsg(ctx, uint64(r.brokerSequence))
			if err != nil {
				t.Fatal(err)
			}
			if msg.Header.Get(HeaderSeq) != strconv.Itoa(i+1) {
				t.Fatalf("message %d carries Perps-Seq %q", r.brokerSequence, msg.Header.Get(HeaderSeq))
			}
			if i == 1 {
				if msg.Subject != "perps.125.control" || msg.Header.Get(delivery.HeaderControl) != delivery.ControlSkipped || msg.Header.Get(jetstream.MsgIDHeader) != controlID(2, events[1].env.EventID) {
					t.Fatalf("control message = %s %v", msg.Subject, msg.Header)
				}
				continue
			}
			if msg.Subject != events[i].subject || msg.Header.Get(jetstream.MsgIDHeader) != events[i].env.EventID || msg.Header.Get(delivery.HeaderControl) != "" {
				t.Fatalf("message %d = %s %v", r.brokerSequence, msg.Subject, msg.Header)
			}
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+4 {
			t.Fatalf("stream last sequence %d, want %d: one control message and three rows", state.LastSeq, base+4)
		}
	})

	t.Run("pipelined_batch_in_order", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		leases := make([]delivery.Lease, 0, 20)
		for i := 0; i < 20; i++ {
			e := fillEvent(t, 800+uint64(i), hashA, 0, 0)
			leases = append(leases, delivery.Lease{EventID: e.env.EventID, Subject: e.subject, Wire: wireOf(t, policy, e), PerpsSeq: int64(i + 1)})
		}
		s := &sink{js: jsPub(t, nc), policy: policy}
		// A batch whose first message is already persisted is answered as a
		// duplicate and the rest follow it.
		if res := s.PublishBatch(ctx, leases[:1]); res[0].Err != nil || res[0].Ack.Sequence != base+1 {
			t.Fatalf("first message = %+v", res[0])
		}
		for i, res := range s.PublishBatch(ctx, leases) {
			if res.Err != nil || res.Ack.Sequence != base+uint64(i)+1 {
				t.Fatalf("message %d = %+v", i, res)
			}
		}
		// A message expected after an unpersisted one is refused.
		stale := fillEvent(t, 900, hashA, 0, 0)
		f, err := s.send(delivery.Lease{EventID: stale.env.EventID, Subject: stale.subject, Wire: wireOf(t, policy, stale), PerpsSeq: 21}, jetstream.WithExpectLastSequence(base+19))
		if err != nil {
			t.Fatal(err)
		}
		if res := await(ctx, f, true); res.Err == nil {
			t.Fatalf("message expecting sequence %d was persisted at %d", base+19, res.Ack.Sequence)
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+20 {
			t.Fatalf("stream last sequence %d, want %d", state.LastSeq, base+20)
		}
	})

	t.Run("broker_outage_spends_no_attempt", func(t *testing.T) {
		reset(t, ctx, db)
		base := streamState(t, ctx, perps).LastSeq
		g := openGate(t, nc.ConnectedAddr())
		gc, err := nats.Connect("nats://"+g.addr, nats.SetCustomDialer(g), nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(gc.Close)
		var logs bytes.Buffer
		cfg := testConfig()
		cfg.PublishTimeout = time.Second
		cfg.MaxHeadAge = time.Second
		cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		p, err := New(db, gc, policy, cfg)
		if err != nil {
			t.Fatal(err)
		}
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- p.Run(runCtx) }()
		waitFor := func(what string, cond func() bool) {
			t.Helper()
			deadline := time.Now().Add(30 * time.Second)
			for !cond() {
				if time.Now().After(deadline) {
					stop()
					t.Fatalf("%s; Run = %v", what, <-done)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		first := fillEvent(t, 1100, hashA, 0, 0)
		enqueue(t, ctx, db, outboxStore, first)
		waitFor("first row not published", func() bool { return count(t, db, openRows) == 0 })

		// The broker port closes and stays closed far longer than the whole
		// backoff budget of MaxAttempts attempts.
		g.close()
		head, next := fillEvent(t, 1101, hashA, 0, 0), fillEvent(t, 1102, hashA, 0, 0)
		enqueue(t, ctx, db, outboxStore, head, next)
		waitFor("perps_outbox_broker_down not set", func() bool { return metricValue(t, p.brokerDown) == 1 })
		time.Sleep(6 * time.Second)
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE state<>'pending' AND event_id<>$1 OR failure_class IS NOT NULL`, first.env.EventID); n != 0 {
			t.Fatalf("%d rows failed during the outage: %+v", n, outbox(t, db))
		}
		if err := p.Ready(ctx); err == nil || !strings.Contains(err.Error(), "oldest open outbox row") {
			t.Fatalf("Ready during the outage = %v", err)
		}
		if metricValue(t, p.brokerDown) != 1 {
			t.Fatal("perps_outbox_broker_down cleared during the outage")
		}

		// The broker returns: publishing resumes at the head row.
		g.open(t)
		waitFor("outbox not drained after the broker returned", func() bool { return count(t, db, openRows) == 0 })
		waitFor("perps_outbox_broker_down not cleared", func() bool { return metricValue(t, p.brokerDown) == 0 })
		stop()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v", err)
		}
		rows := outbox(t, db)
		if len(rows) != 3 {
			t.Fatalf("outbox = %+v", rows)
		}
		events := []event{first, head, next}
		for i, r := range rows {
			// The one attempt of each row is the one that persisted it.
			if r.eventID != events[i].env.EventID || r.state != "published" || r.failure != "" || r.attempts != 1 || r.brokerSequence != int64(base)+int64(i)+1 {
				t.Fatalf("row %d = %+v; log %q", i, r, logs.String())
			}
			msg, err := perps.GetMsg(ctx, uint64(r.brokerSequence))
			if err != nil {
				t.Fatal(err)
			}
			if msg.Subject != events[i].subject || msg.Header.Get(delivery.HeaderControl) != "" || msg.Header.Get(jetstream.MsgIDHeader) != events[i].env.EventID {
				t.Fatalf("message %d = %s %v", r.brokerSequence, msg.Subject, msg.Header)
			}
		}
		if state := streamState(t, ctx, perps); state.LastSeq != base+3 {
			t.Fatalf("stream last sequence %d, want %d: three rows and no control message", state.LastSeq, base+3)
		}
		if n := metricValue(t, p.quarantined); n != 0 {
			t.Fatalf("perps_outbox_quarantined_total = %v, want 0", n)
		}
		line := logs.String()
		if n := strings.Count(line, "level=WARN"); n != 1 || !strings.Contains(line, "cannot reach the broker") || strings.Contains(line, "level=ERROR") {
			t.Fatalf("outage logged %d Warn lines, want one and no Error: %q", n, line)
		}
	})

	t.Run("refused_message_exhausted", func(t *testing.T) {
		reset(t, ctx, db)
		state := streamState(t, ctx, perps)
		if state.Msgs == 0 {
			t.Fatal("stream holds no message to fill up to")
		}
		base := state.LastSeq
		// The reachable broker refuses every new message: the stream is full.
		full := stream.Config(chain)
		full.MaxMsgs = int64(state.Msgs)
		if _, err := js.UpdateStream(ctx, full); err != nil {
			t.Fatal(err)
		}
		restored := false
		restore := func() {
			if _, err := js.UpdateStream(ctx, stream.Config(chain)); err != nil {
				t.Fatal(err)
			}
			restored = true
		}
		defer func() {
			if !restored {
				restore()
			}
		}()
		head, next := fillEvent(t, 1200, hashA, 0, 0), fillEvent(t, 1201, hashA, 0, 0)
		enqueue(t, ctx, db, outboxStore, head, next)
		var logs bytes.Buffer
		cfg := testConfig()
		cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		p, err := New(db, nc, policy, cfg)
		if err != nil {
			t.Fatal(err)
		}
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- p.Run(runCtx) }()
		deadline := time.Now().Add(30 * time.Second)
		for count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE event_id=$1 AND state='quarantined' AND failure_class='exhausted'`, head.env.EventID) != 1 {
			if time.Now().After(deadline) {
				stop()
				t.Fatalf("refused row not exhausted: %+v; Run = %v", outbox(t, db), <-done)
			}
			time.Sleep(20 * time.Millisecond)
		}
		if metricValue(t, p.brokerDown) != 0 {
			t.Fatal("a refusal by a reachable broker set perps_outbox_broker_down")
		}
		restore()
		drainOpen(t, ctx, db, p)
		stop()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v", err)
		}
		rows := outbox(t, db)
		if len(rows) != 2 {
			t.Fatalf("outbox = %+v", rows)
		}
		if r := rows[0]; r.eventID != head.env.EventID || r.state != "quarantined" || r.failure != "exhausted" || r.brokerSequence != int64(base)+1 {
			t.Fatalf("refused row = %+v", r)
		}
		if r := rows[1]; r.state != "published" || r.brokerSequence != int64(base)+2 {
			t.Fatalf("row behind the refused row = %+v", r)
		}
		msg, err := perps.GetMsg(ctx, base+1)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Subject != "perps.125.control" || msg.Header.Get(jetstream.MsgIDHeader) != controlID(rows[0].seq, head.env.EventID) {
			t.Fatalf("control message = %s %v", msg.Subject, msg.Header)
		}
		if n := metricValue(t, p.quarantined); n != 1 {
			t.Fatalf("perps_outbox_quarantined_total = %v, want 1", n)
		}
		if line := logs.String(); strings.Contains(line, "cannot reach the broker") {
			t.Fatalf("a refusal was logged as an outage: %q", line)
		}
	})
}

// gate is a TCP path to one broker whose port can be closed and reopened,
// so a client meets a broker that is unreachable and then returns. Its
// Dial, the client dialer, always dials the gate.
type gate struct {
	addr, target string
	mu           sync.Mutex
	l            net.Listener
	conns        []net.Conn
}

func openGate(t *testing.T, target string) *gate {
	t.Helper()
	g := &gate{addr: "127.0.0.1:0", target: target}
	g.open(t)
	t.Cleanup(g.close)
	return g
}

func (g *gate) Dial(network, _ string) (net.Conn, error) {
	g.mu.Lock()
	addr := g.addr
	g.mu.Unlock()
	return net.Dial(network, addr)
}

func (g *gate) open(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", g.addr)
	if err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	g.addr, g.l = l.Addr().String(), l
	g.mu.Unlock()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			b, err := net.Dial("tcp", g.target)
			if err != nil {
				_ = c.Close()
				continue
			}
			g.mu.Lock()
			g.conns = append(g.conns, c, b)
			g.mu.Unlock()
			go func() { _, _ = io.Copy(b, c); _ = b.Close() }()
			go func() { _, _ = io.Copy(c, b); _ = c.Close() }()
		}
	}()
}

// close closes the gate port and every connection through it.
func (g *gate) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.l != nil {
		_ = g.l.Close()
		g.l = nil
	}
	for _, c := range g.conns {
		_ = c.Close()
	}
	g.conns = nil
}

func jsPub(t *testing.T, nc *nats.Conn) jetstream.JetStream {
	t.Helper()
	js, err := jetstream.New(nc, jetstream.WithPublishAsyncTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return js
}
