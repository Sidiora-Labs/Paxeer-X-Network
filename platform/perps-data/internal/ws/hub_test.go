package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/feeds"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/stream"
)

const hubSnapshotID = "hub-7c1e4a90"

// hubFeeds serves the hub from the topic providers of internal/feeds.
type hubFeeds struct{ *feeds.Providers }

func (p hubFeeds) Message(ctx context.Context, subject string, env delivery.Envelope) ([]Frame, []string, error) {
	frames, resnap, err := p.Providers.Message(ctx, subject, env)
	out := make([]Frame, len(frames))
	for i, f := range frames {
		out[i] = Frame(f)
	}
	return out, resnap, err
}

func (p hubFeeds) Run(ctx context.Context, emit func(Frame)) error {
	return p.Providers.Run(ctx, func(f feeds.Frame) { emit(Frame(f)) })
}

func hubDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func hubExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func hubReset(t *testing.T, db *sql.DB) {
	t.Helper()
	hubExec(t, db, `TRUNCATE perps.fills, perps.blocks, perps.orders, perps.mark_prices, perps.cursors RESTART IDENTITY`)
}

func hubAddr(lo, hi byte) []byte {
	b := make([]byte, 20)
	b[18], b[19] = hi, lo
	return b
}

var (
	hubPerps  = hubAddr(0x15, 0x10)
	hubTrader = hubAddr(0x04, 0xb1)
	hubMaker  = hubAddr(0x05, 0xb2)
)

func hubHash(block int64, txIndex, logIndex int) []byte {
	return []byte(fmt.Sprintf("%016d%08d%08d", block, txIndex+1, logIndex))
}

const e18 = "000000000000000000"

// hubFact inserts one fact row of market 0 at block with its common columns.
func hubFact(t *testing.T, db *sql.DB, table, kind string, block int64, args []string, values []any) {
	t.Helper()
	cols := append([]string{"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}, args...)
	vals := append([]any{block, hubHash(block, -1, 0), 0, 0, 0, hubHash(block, 0, 0), "tx", time.Now(), hubPerps, kind, hubTrader, hubTrader}, values...)
	marks := make([]string, len(cols))
	for i := range cols {
		marks[i] = "$" + strconv.Itoa(i+1)
		cols[i] = `"` + cols[i] + `"`
	}
	hubExec(t, db, fmt.Sprintf("INSERT INTO perps.%s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(marks, ", ")), vals...)
}

// hubFill inserts a BTC fill of 100 USD at price 100 in block.
func hubFill(t *testing.T, db *sql.DB, block int64) {
	t.Helper()
	hubFact(t, db, "fills", "fill", block,
		[]string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long", "price", "size_usd", "maker_fee_usd", "taker_fee_usd"},
		[]any{0, 1, 2, hubMaker, hubTrader, true, "100" + e18, "100" + e18, "0", "0"})
}

func hubBlock(t *testing.T, db *sql.DB, number int64) {
	t.Helper()
	hubExec(t, db, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, 0)`,
		number, hubHash(number, -1, 0), hubHash(number-1, -1, 0), time.Now(), strings.Repeat("AB", 20))
}

// hubSeedBook writes a BTC book with one bid of 10 contracts at 100 and a
// mark price.
func hubSeedBook(t *testing.T, db *sql.DB) {
	t.Helper()
	hubExec(t, db, `INSERT INTO perps.cursors (name, height, hash) VALUES ('head', 10, $1)`, hubHash(10, -1, 0))
	hubFact(t, db, "mark_prices", "mark", 9, []string{"market_id", "mark_price", "index_price", "mid_price"}, []any{0, "100" + e18, "100" + e18, "100" + e18})
	hubExec(t, db, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price, size_usd, limit_price,
		remaining_size_usd, status, placed_block) VALUES (1, $1, 0, 1, true, 0, $2, $3, $2, 'active', 1)`, hubTrader, "1000"+e18, "100"+e18)
}

// hubBroker starts an in-process JetStream broker holding PERPS_V1 for chain
// 125 on one replica; maxMsgs > 0 bounds the stream and discards the oldest.
func hubBroker(t *testing.T, maxMsgs int64) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("in-process broker not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := stream.Config(125)
	cfg.Replicas = 1
	if maxMsgs > 0 {
		cfg.MaxMsgs, cfg.Discard = maxMsgs, jetstream.DiscardOld
	}
	if _, err := js.CreateStream(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return nc, js
}

// hubPublisher publishes delivery envelopes with the headers of the perps
// publisher, numbering Perps-Seq from 1.
type hubPublisher struct {
	js  jetstream.JetStream
	seq int64
}

func (p *hubPublisher) publish(t *testing.T, subject, kind string, height int64, payload string) (uint64, string) {
	t.Helper()
	p.seq++
	id, err := natsdriver.MsgID(fmt.Sprintf("125:%d:0x%064x:0:%d:0", height, p.seq, p.seq))
	if err != nil {
		t.Fatal(err)
	}
	env := delivery.Envelope{
		EventID: id, EventType: kind, SchemaVersion: 1, ChainID: 125,
		SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(height, 10), TransactionIndex: "0", LogIndex: "0"},
		EmittedAt:   time.Now().UTC().Truncate(time.Microsecond), TraceID: id,
		PayloadHash: delivery.Hash([]byte(payload)), Payload: json.RawMessage(payload),
	}
	wire, err := env.Encode(delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{kind: true}}, subject)
	if err != nil {
		t.Fatal(err)
	}
	msg := &nats.Msg{Subject: subject, Data: wire, Header: nats.Header{}}
	msg.Header.Set(publish.HeaderSeq, strconv.FormatInt(p.seq, 10))
	msg.Header.Set(publish.HeaderBlock, env.SourceOrder.Height)
	msg.Header.Set(publish.HeaderKind, kind)
	ack, err := p.js.PublishMsg(context.Background(), msg, jetstream.WithMsgID(id), jetstream.WithExpectStream(stream.Name))
	if err != nil {
		t.Fatal(err)
	}
	return ack.Sequence, id
}

// hubBlockRow is the payload of a block message stamped now.
func hubBlockRow() string {
	return `{"time":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
}

func blockTime(at time.Time) string {
	return `{"block_time":"` + at.UTC().Format(time.RFC3339Nano) + `"}`
}

// hubRig is one hub with its broker, providers and socket server.
type hubRig struct {
	hub  *Hub
	nc   *nats.Conn
	pub  *hubPublisher
	url  string
	stop func()
}

func newHubRig(t *testing.T, db *sql.DB, maxMsgs int64) *hubRig {
	t.Helper()
	nc, js := hubBroker(t, maxMsgs)
	p, err := feeds.NewProviders(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := NewHub(hubFeeds{p}, 125, hubSnapshotID, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(hubSnapshotID, p.Served, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	srv.OnSubscribe, srv.OnUnsubscribe, srv.OnClose = hub.Subscribe, hub.Unsubscribe, hub.Leave
	httpServer := httptest.NewServer(srv)
	t.Cleanup(httpServer.Close)
	r := &hubRig{hub: hub, nc: nc, pub: &hubPublisher{js: js}, url: "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"}
	r.start(t)
	t.Cleanup(func() { r.stop() })
	return r
}

// start runs the hub until stop and waits until it is attached.
func (r *hubRig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.hub.Run(ctx, r.nc) }()
	var once sync.Once
	r.stop = func() {
		once.Do(func() {
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("hub run: %v", err)
			}
		})
	}
	hubWait(t, "attach", func() bool {
		r.hub.mu.Lock()
		defer r.hub.mu.Unlock()
		return r.hub.attached
	})
}

func (r *hubRig) applied() uint64 {
	r.hub.mu.Lock()
	defer r.hub.mu.Unlock()
	return r.hub.applied
}

func hubWait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sequenceGuard is the per-topic guard of the application client
// (Hyperpax:src/core/net/realtime.ts:29-47): it reports false on a gap.
func sequenceGuard() func(DataFrame) bool {
	type mark struct {
		seq   uint64
		epoch string
	}
	topics := map[string]mark{}
	return func(f DataFrame) bool {
		old, ok := topics[f.Topic]
		if f.Snapshot || !ok {
			if !ok || old.epoch != f.SnapshotID || f.Seq >= old.seq {
				topics[f.Topic] = mark{f.Seq, f.SnapshotID}
			}
			return true
		}
		if old.epoch != f.SnapshotID || f.Seq > old.seq+1 {
			return false
		}
		if f.Seq <= old.seq {
			return true
		}
		topics[f.Topic] = mark{f.Seq, f.SnapshotID}
		return true
	}
}

type hubClient struct {
	conn  *websocket.Conn
	guard func(DataFrame) bool
}

// dial opens a socket and subscribes it to topic, returning the snapshot.
func (r *hubRig) dial(t *testing.T, topic string) (*hubClient, DataFrame) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(r.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &hubClient{conn: conn, guard: sequenceGuard()}
	if w := c.control(t); w.Event != controlWelcome || w.SnapshotID != hubSnapshotID {
		t.Fatalf("welcome %+v", w)
	}
	if err := conn.WriteJSON(ClientFrame{Event: EventSubscribe, Topic: topic}); err != nil {
		t.Fatal(err)
	}
	if s := c.control(t); s.Event != controlSubscribed || s.Topic != topic || s.SnapshotID != hubSnapshotID {
		t.Fatalf("subscribed %+v", s)
	}
	snap := c.frame(t)
	if !snap.Snapshot || snap.Topic != topic {
		t.Fatalf("first frame after subscribed %+v", snap)
	}
	return c, snap
}

func (c *hubClient) read(t *testing.T) []byte {
	t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return raw
}

func (c *hubClient) control(t *testing.T) Control {
	t.Helper()
	var ctl Control
	if err := json.Unmarshal(c.read(t), &ctl); err != nil {
		t.Fatal(err)
	}
	return ctl
}

// frame reads one data frame, which the application guard must accept.
func (c *hubClient) frame(t *testing.T) DataFrame {
	t.Helper()
	raw := c.read(t)
	var f DataFrame
	if err := json.Unmarshal(raw, &f); err != nil || f.Topic == "" {
		t.Fatalf("data frame %s: %v", raw, err)
	}
	if f.SnapshotID != hubSnapshotID {
		t.Fatalf("snapshotId %q", f.SnapshotID)
	}
	if !c.guard(f) {
		t.Fatalf("guard refused %s", raw)
	}
	return f
}

// delta reads the next frame and requires a live frame with seq.
func (c *hubClient) delta(t *testing.T, seq uint64) DataFrame {
	t.Helper()
	f := c.frame(t)
	if f.Snapshot || f.Seq != seq {
		t.Fatalf("frame snapshot=%v seq %d, want live seq %d", f.Snapshot, f.Seq, seq)
	}
	return f
}

type hubTrades struct {
	Symbol string           `json:"symbol"`
	Trades []map[string]any `json:"trades"`
}

func tradeID(t *testing.T, f DataFrame) string {
	t.Helper()
	var trade map[string]any
	if err := json.Unmarshal(f.Data, &trade); err != nil {
		t.Fatal(err)
	}
	id, _ := trade["id"].(string)
	return id
}

const btcTrade = "BTC@perp_trade"

func TestHub(t *testing.T) {
	db := hubDB(t)

	t.Run("snapshot_then_deltas", func(t *testing.T) {
		hubReset(t, db)
		r := newHubRig(t, db, 0)
		a, snap := r.dial(t, btcTrade)
		if snap.Seq != 0 {
			t.Fatalf("first snapshot seq %d", snap.Seq)
		}
		for n := int64(1); n <= 41; n++ {
			hubFill(t, db, n)
			r.pub.publish(t, "perps.125.0.fill", "fill", n, blockTime(time.Now()))
			if id := tradeID(t, a.delta(t, uint64(n))); id != fmt.Sprintf("%d-0", n) {
				t.Fatalf("delta %d carried %s", n, id)
			}
		}
		b, snap := r.dial(t, btcTrade)
		var trades hubTrades
		if err := json.Unmarshal(snap.Data, &trades); err != nil {
			t.Fatal(err)
		}
		if snap.Seq != 41 || trades.Symbol != "BTC" || len(trades.Trades) != 41 || trades.Trades[0]["id"] != "41-0" {
			t.Fatalf("snapshot seq %d with %d trades, newest %v", snap.Seq, len(trades.Trades), trades.Trades[0]["id"])
		}
		hubFill(t, db, 42)
		r.pub.publish(t, "perps.125.0.fill", "fill", 42, blockTime(time.Now()))
		for _, c := range []*hubClient{a, b} {
			if id := tradeID(t, c.delta(t, 42)); id != "42-0" {
				t.Fatalf("delta 42 carried %s", id)
			}
		}
	})

	t.Run("dense_seq", func(t *testing.T) {
		hubReset(t, db)
		r := newHubRig(t, db, 0)
		c, snap := r.dial(t, btcTrade)
		for n := int64(1); n <= 1000; n++ {
			hubFill(t, db, n)
			r.pub.publish(t, "perps.125.0.fill", "fill", n, blockTime(time.Now()))
			if id := tradeID(t, c.delta(t, snap.Seq+uint64(n))); id != fmt.Sprintf("%d-0", n) {
				t.Fatalf("delta %d carried %s", n, id)
			}
		}
	})

	t.Run("resume_from_stream_seq", func(t *testing.T) {
		hubReset(t, db)
		r := newHubRig(t, db, 0)
		var (
			mu      sync.Mutex
			applied []string
		)
		defer r.hub.Listen(func(_ string, env delivery.Envelope) {
			mu.Lock()
			applied = append(applied, env.EventID)
			mu.Unlock()
		})()
		var published []string
		for n := int64(1); n <= 500; n++ {
			seq, id := r.pub.publish(t, "perps.125.chain.block", "block", n, hubBlockRow())
			if seq != uint64(n) {
				t.Fatalf("stream sequence %d, want %d", seq, n)
			}
			published = append(published, id)
		}
		hubWait(t, "stream sequence 500", func() bool { return r.applied() == 500 })
		s, err := r.pub.js.Stream(context.Background(), stream.Name)
		if err != nil {
			t.Fatal(err)
		}
		consumers := s.ConsumerNames(context.Background())
		deleted := 0
		for name := range consumers.Name() {
			if err := r.pub.js.DeleteConsumer(context.Background(), stream.Name, name); err != nil {
				t.Fatal(err)
			}
			deleted++
		}
		if err := consumers.Err(); err != nil || deleted != 1 {
			t.Fatalf("deleted %d consumers: %v", deleted, err)
		}
		for n := int64(501); n <= 510; n++ {
			_, id := r.pub.publish(t, "perps.125.chain.block", "block", n, hubBlockRow())
			published = append(published, id)
		}
		hubWait(t, "stream sequence 510", func() bool { return r.applied() == 510 })
		mu.Lock()
		defer mu.Unlock()
		if !slices.Equal(applied, published) {
			t.Fatalf("applied %d messages, want the 510 published once each in stream order", len(applied))
		}
	})

	t.Run("rewind_resnapshot", func(t *testing.T) {
		hubReset(t, db)
		hubSeedBook(t, db)
		r := newHubRig(t, db, 0)
		c, snap := r.dial(t, "BTC@perp_orderbook")
		if snap.Seq != 0 {
			t.Fatalf("first snapshot seq %d", snap.Seq)
		}
		// The first tick after the subscribe sends a frame with checksum;
		// every mark change after it sends one coalesced book frame.
		c.delta(t, 1)
		for seq := uint64(2); seq <= 9; seq++ {
			r.pub.publish(t, "perps.125.0.mark", "mark", 10, blockTime(time.Now()))
			c.delta(t, seq)
		}
		r.pub.publish(t, "perps.125.chain.rewind", "rewind", 5, `{"from":5}`)
		f := c.frame(t)
		var book struct {
			Symbol string      `json:"symbol"`
			Bids   [][]float64 `json:"bids"`
		}
		if err := json.Unmarshal(f.Data, &book); err != nil {
			t.Fatal(err)
		}
		if !f.Snapshot || f.Seq != 10 || f.Topic != "BTC@perp_orderbook" || book.Symbol != "BTC" || len(book.Bids) != 1 || book.Bids[0][0] != 100 || book.Bids[0][1] != 10 {
			t.Fatalf("frame after rewind %+v data %s", f, f.Data)
		}
	})

	t.Run("gap_closes_1013", func(t *testing.T) {
		hubReset(t, db)
		r := newHubRig(t, db, 5)
		c, snap := r.dial(t, feeds.TopicChainBlocks)
		hubBlock(t, db, 1)
		r.pub.publish(t, "perps.125.chain.block", "block", 1, hubBlockRow())
		c.delta(t, snap.Seq+1)
		r.stop()
		for n := int64(2); n <= 11; n++ {
			r.pub.publish(t, "perps.125.chain.block", "block", n, hubBlockRow())
		}
		r.start(t)
		if err := c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			t.Fatal(err)
		}
		_, raw, err := c.conn.ReadMessage()
		var closed *websocket.CloseError
		if !errors.As(err, &closed) {
			t.Fatalf("read %s, %v; want a close", raw, err)
		}
		m := regexp.MustCompile(`^retry-after=(\d+)$`).FindStringSubmatch(closed.Text)
		if closed.Code != websocket.CloseTryAgainLater || m == nil {
			t.Fatalf("close %d %q", closed.Code, closed.Text)
		}
		if n, _ := strconv.Atoi(m[1]); n < 5 || n > 30 {
			t.Fatalf("retry-after %d outside [5, 30]", n)
		}
		hubWait(t, "stream sequence 11", func() bool { return r.applied() == 11 })
	})

	t.Run("stale_frame_dropped", func(t *testing.T) {
		hubReset(t, db)
		r := newHubRig(t, db, 0)
		c, snap := r.dial(t, btcTrade)
		hubFill(t, db, 1)
		r.pub.publish(t, "perps.125.0.fill", "fill", 1, blockTime(time.Now().Add(-61*time.Second)))
		f := c.frame(t)
		var trades hubTrades
		if err := json.Unmarshal(f.Data, &trades); err != nil {
			t.Fatal(err)
		}
		if !f.Snapshot || f.Seq != snap.Seq+1 || len(trades.Trades) != 1 || trades.Trades[0]["id"] != "1-0" {
			t.Fatalf("frame after a stale message %+v data %s", f, f.Data)
		}
		if age := time.Since(time.UnixMilli(f.TS)); age > 5*time.Second || age < -time.Second {
			t.Fatalf("fresh snapshot ts is %v old", age)
		}
		hubFill(t, db, 2)
		at := time.Now()
		r.pub.publish(t, "perps.125.0.fill", "fill", 2, blockTime(at))
		live := c.delta(t, snap.Seq+2)
		if id := tradeID(t, live); id != "2-0" || live.TS != at.UnixMilli() {
			t.Fatalf("live frame %s ts %d, want 2-0 at %d", id, live.TS, at.UnixMilli())
		}
	})
}
