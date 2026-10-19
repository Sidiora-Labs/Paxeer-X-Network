package publish

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

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
	return Config{Owner: "perps-publisher", Batch: 100, MaxAttempts: 2, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, IdleInterval: 20 * time.Millisecond}
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

func TestPublisher(t *testing.T) {
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestPublisher requires " + testDatabaseEnv)
	}
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
		// refuses it; Retry backs it off until the attempt budget quarantines it.
		limit := padded(t, policy, 601, delivery.MaxEnvelopeBytes)
		enqueue(t, ctx, db, outboxStore, limit)
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- publisher.Run(runCtx) }()
		var availableInMS int64
		deadline := time.Now().Add(30 * time.Second)
		for {
			err := db.QueryRowContext(ctx, `SELECT (EXTRACT(EPOCH FROM (available_at-clock_timestamp()))*1000)::bigint FROM perps.delivery_outbox WHERE event_id=$1 AND state='pending' AND failure_class='transient' AND attempts=1`, limit.env.EventID).Scan(&availableInMS)
			if err == nil {
				break
			}
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("refused publish was not put back for retry")
			}
			time.Sleep(5 * time.Millisecond)
		}
		if availableInMS <= 0 || availableInMS > 1000 {
			t.Fatalf("retry backoff %d ms, want within (0, 1000]", availableInMS)
		}
		for count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE state='pending'`) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("refused publish was never quarantined")
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		if err = <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v", err)
		}
		rows := outbox(t, db)
		if len(rows) != 1 || rows[0].state != "quarantined" || rows[0].failure != "transient" || rows[0].attempts != 2 || rows[0].brokerSequence != 0 || rows[0].brokerStream != "" {
			t.Fatalf("refused row = %+v", rows)
		}
		if state := streamState(t, ctx, perps); state.Msgs != before.Msgs || state.LastSeq != before.LastSeq {
			t.Fatalf("refused envelope reached the stream: %d messages, last %d", state.Msgs, state.LastSeq)
		}
	})
}
