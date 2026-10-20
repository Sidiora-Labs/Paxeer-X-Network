package indexer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testRPCEnv      = "PERPS_DATA_TEST_RPC"
	testCometEnv    = "PERPS_DATA_TEST_COMET"
	// scanWindow and scanWindows bound the search for venue logs on the node.
	scanWindow  = 500
	scanWindows = 200
	waitTimeout = 2 * time.Minute
)

var chainSubject = regexp.MustCompile(`^perps\.[0-9]+\.chain\.[a-z]+$`)

type env struct {
	db     *sql.DB
	client *ethclient.Client
	nc     *nats.Conn
	js     jetstream.JetStream
	chain  uint64
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatal("TestIndexer requires " + name)
	}
	return v
}

// setup empties schema perps, applies the migrations and starts an in-process
// JetStream server holding PERPS_V1 for the chain of the node.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(requireEnv(t, testDatabaseEnv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	client, err := ethclient.DialContext(ctx, requireEnv(t, testRPCEnv))
	if err != nil {
		t.Fatalf("dial %s: %v", testRPCEnv, err)
	}
	t.Cleanup(client.Close)
	id, err := client.ChainID(ctx)
	if err != nil {
		t.Fatalf("chain id: %v", err)
	}
	if !id.IsUint64() || id.Sign() == 0 {
		t.Fatalf("chain id %s", id)
	}
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	// One in-process server cannot hold three replicas; every other setting
	// is the PERPS_V1 configuration.
	cfg := stream.Config(id.Uint64())
	cfg.Replicas = 1
	if _, err := js.CreateStream(ctx, cfg); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	return &env{db: db, client: client, nc: nc, js: js, chain: id.Uint64()}
}

// newIndexer builds an Indexer over the real follower, decoders, writers and
// publisher of e.
func (e *env) newIndexer(t *testing.T, holder string, start uint64) *Indexer {
	t.Helper()
	return e.newIndexerVia(t, holder, start, requireEnv(t, testRPCEnv))
}

// newIndexerVia is newIndexer with the EVM JSON-RPC of the indexer at rpcURL.
func (e *env) newIndexerVia(t *testing.T, holder string, start uint64, rpcURL string) *Indexer {
	t.Helper()
	ctx := context.Background()
	metrics, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	module := func(name string) common.Address {
		sum := sha256.Sum256([]byte(name))
		return common.BytesToAddress(sum[:common.AddressLength])
	}
	rpc, err := source.NewRPC(ctx, source.RPCOptions{URLs: []string{rpcURL}})
	if err != nil {
		t.Fatalf("NewRPC: %v", err)
	}
	t.Cleanup(rpc.Close)
	registry, err := decode.NewRegistry(metrics, decode.Options{PerpsModule: module("perps"), SpotModule: module("spot"),
		PricefeedModule: module("pricefeed"), State: rpc})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	comet, err := source.NewComet([]string{requireEnv(t, testCometEnv)}, 2*time.Second)
	if err != nil {
		t.Fatalf("NewComet: %v", err)
	}
	outbox, err := delivery.NewStore(e.db, "perps", store.Policy(e.chain))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	publisher, err := publish.New(e.db, e.nc, store.Policy(e.chain), publish.Config{
		Owner: holder, Batch: 100, MaxAttempts: 20, LeaseDuration: 30 * time.Second,
		PublishTimeout: 5 * time.Second, RetryDelay: time.Second, IdleInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("publish.New: %v", err)
	}
	ix, err := New(ctx, Deps{DB: e.db, RPC: rpc, Comet: comet, Outbox: outbox, ChainID: e.chain, Registry: registry, Publisher: publisher,
		Metrics: metrics, Holder: holder, StartHeight: int64(start)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ix
}

// runUntil runs ix until done reports true, then stops it and checks that Run
// returned the cancellation.
func (e *env) runUntil(t *testing.T, ix *Indexer, what string, done func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- ix.Run(ctx) }()
	deadline := time.Now().Add(waitTimeout)
	for !done() {
		select {
		case err := <-result:
			cancel()
			t.Fatalf("Run returned before %s: %v", what, err)
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			cancel()
			<-result
			t.Fatalf("timed out waiting for %s", what)
		}
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

func (e *env) cursor(t *testing.T) (uint64, bool) {
	t.Helper()
	h, ok, err := store.Cursor(context.Background(), e.db)
	if err != nil {
		t.Fatalf("Cursor: %v", err)
	}
	return h, ok
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// indexedThrough reports whether the cursor reached height and every outbox
// row is published.
func (e *env) indexedThrough(t *testing.T, height uint64) func() bool {
	return func() bool {
		h, ok := e.cursor(t)
		return ok && h >= height && e.count(t, `SELECT count(*) FROM perps.delivery_outbox WHERE state <> 'published'`) == 0
	}
}

// latestLogs returns the last n logs of q in the latest node window holding
// at least n of them, scanning back from the head.
func (e *env) latestLogs(t *testing.T, q ethereum.FilterQuery, n int) []ethtypes.Log {
	t.Helper()
	ctx := context.Background()
	head, err := e.client.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	to := head
	for range scanWindows {
		from := uint64(1)
		if to > scanWindow {
			from = to - scanWindow + 1
		}
		q.FromBlock, q.ToBlock = new(big.Int).SetUint64(from), new(big.Int).SetUint64(to)
		logs, err := e.client.FilterLogs(ctx, q)
		if err != nil {
			t.Fatalf("eth_getLogs [%d, %d]: %v", from, to, err)
		}
		if len(logs) >= n {
			return logs[len(logs)-n:]
		}
		if from == 1 {
			break
		}
		to = from - 1
	}
	t.Fatalf("the node of %s holds fewer than %d matching logs in the last %d blocks", testRPCEnv, n, scanWindow*scanWindows)
	return nil
}

// logsCall is one eth_getLogs request seen by sizeRefusingNode.
type logsCall struct {
	from, to  uint64
	addresses []string
	refused   bool
}

func (c logsCall) span() uint64 { return c.to - c.from + 1 }

// sizeRefusingNode forwards every JSON-RPC request to the node at upstream and
// records each eth_getLogs range; the first eth_getLogs spanning exactly
// refuseSpan blocks is answered with the node's size refusal instead.
type sizeRefusingNode struct {
	upstream   string
	refuseSpan uint64
	mu         sync.Mutex
	calls      []logsCall
	refusedOne bool
}

func (n *sizeRefusingNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params []struct {
			FromBlock string   `json:"fromBlock"`
			ToBlock   string   `json:"toBlock"`
			Address   []string `json:"address"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &req) == nil && req.Method == "eth_getLogs" && len(req.Params) == 1 {
		from, errFrom := strconv.ParseUint(strings.TrimPrefix(req.Params[0].FromBlock, "0x"), 16, 64)
		to, errTo := strconv.ParseUint(strings.TrimPrefix(req.Params[0].ToBlock, "0x"), 16, 64)
		if errFrom != nil || errTo != nil || to < from {
			http.Error(w, "eth_getLogs range", http.StatusBadRequest)
			return
		}
		call := logsCall{from: from, to: to, addresses: req.Params[0].Address}
		n.mu.Lock()
		if !n.refusedOne && call.span() == n.refuseSpan {
			n.refusedOne, call.refused = true, true
		}
		n.calls = append(n.calls, call)
		n.mu.Unlock()
		if call.refused {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{
				"code": -32000, "message": "block range too large (" + strconv.FormatUint(call.span(), 10) + "), maximum allowed is 250 blocks"}})
			return
		}
	}
	resp, err := http.Post(n.upstream, "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// venueCalls returns the recorded eth_getLogs calls whose filter holds the
// perps module, the first pass of each follower window.
func (n *sizeRefusingNode) venueCalls() []logsCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	perps := strings.ToLower(decode.PerpsAddress.Hex())
	var out []logsCall
	for _, c := range n.calls {
		if slices.Contains(c.addresses, perps) {
			out = append(out, c)
		}
	}
	return out
}

func (n *sizeRefusingNode) allCalls() []logsCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.calls)
}

type streamMsg struct {
	seq      uint64
	subject  string
	perpsSeq int64
	block    string
	envelope delivery.Envelope
}

func (e *env) streamMessages(t *testing.T) []streamMsg {
	t.Helper()
	ctx := context.Background()
	s, err := e.js.Stream(ctx, stream.Name)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	var out []streamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
		raw, err := s.GetMsg(ctx, seq)
		if err != nil {
			t.Fatalf("get message %d: %v", seq, err)
		}
		m := streamMsg{seq: raw.Sequence, subject: raw.Subject, block: raw.Header.Get(publish.HeaderBlock)}
		if m.perpsSeq, err = strconv.ParseInt(raw.Header.Get(publish.HeaderSeq), 10, 64); err != nil {
			t.Fatalf("message %d Perps-Seq %q: %v", seq, raw.Header.Get(publish.HeaderSeq), err)
		}
		if err := json.Unmarshal(raw.Data, &m.envelope); err != nil {
			t.Fatalf("message %d envelope: %v", seq, err)
		}
		out = append(out, m)
	}
	return out
}

func sourceOrder(t *testing.T, m streamMsg) [3]uint64 {
	t.Helper()
	var o [3]uint64
	for i, v := range []string{m.envelope.SourceOrder.Height, m.envelope.SourceOrder.TransactionIndex, m.envelope.SourceOrder.LogIndex} {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("message %d source order %q: %v", m.seq, v, err)
		}
		o[i] = n
	}
	return o
}

func less(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func TestIndexer(t *testing.T) {
	perps, err := perpsabi.ABI()
	if err != nil {
		t.Fatalf("perps ABI: %v", err)
	}

	t.Run("indexes_venue_block", func(t *testing.T) {
		e := setup(t)
		placed := e.latestLogs(t, ethereum.FilterQuery{Addresses: []common.Address{decode.PerpsAddress}, Topics: [][]common.Hash{{perps.Events["OrderPlaced"].ID}}}, 1)[0]
		orderID := new(big.Int).SetBytes(placed.Topics[1].Bytes())
		user := common.BytesToAddress(placed.Topics[2].Bytes())
		marketID := new(big.Int).SetBytes(placed.Topics[3].Bytes())
		ix := e.newIndexer(t, "indexes-venue-block", placed.BlockNumber)
		e.runUntil(t, ix, "the OrderPlaced block", e.indexedThrough(t, placed.BlockNumber))

		if n := e.count(t, `SELECT count(*) FROM perps.order_events WHERE kind = 'placed' AND block = $1 AND tx_hash = $2 AND log_index = $3 AND order_id = $4::numeric AND "user" = $5 AND market_id = $6::numeric`,
			placed.BlockNumber, placed.TxHash.Bytes(), placed.Index, orderID.String(), user.Bytes(), marketID.String()); n != 1 {
			t.Fatalf("perps.order_events placed rows = %d, want 1", n)
		}
		if n := e.count(t, `SELECT count(*) FROM perps.orders WHERE order_id = $1::numeric AND account = $2 AND market_id = $3::numeric AND placed_block = $4`,
			orderID.String(), user.Bytes(), marketID.String(), placed.BlockNumber); n != 1 {
			t.Fatalf("perps.orders rows of order %s = %d, want 1", orderID, n)
		}
		subject, err := stream.MarketSubject(e.chain, marketID.Uint64(), "order")
		if err != nil {
			t.Fatalf("MarketSubject: %v", err)
		}
		want := [3]uint64{placed.BlockNumber, uint64(placed.TxIndex), uint64(placed.Index)}
		var matched int
		for _, m := range e.streamMessages(t) {
			if m.subject == subject && sourceOrder(t, m) == want {
				matched++
				if m.block != strconv.FormatUint(placed.BlockNumber, 10) {
					t.Fatalf("Perps-Block = %q, want %d", m.block, placed.BlockNumber)
				}
			}
		}
		if matched != 1 {
			t.Fatalf("stream messages of the OrderPlaced log on %s = %d, want 1", subject, matched)
		}
	})

	t.Run("resumes_from_cursor", func(t *testing.T) {
		e := setup(t)
		head, err := e.client.BlockNumber(context.Background())
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		if head < 10 {
			t.Fatalf("node head %d below 10", head)
		}
		start := head - 5
		first := e.newIndexer(t, "resumes-first", start)
		next, err := first.resume(context.Background())
		if err != nil {
			t.Fatalf("resume on an empty cursor: %v", err)
		}
		if next != start {
			t.Fatalf("resume on an empty cursor = %d, want PERPS_START_HEIGHT %d", next, start)
		}
		e.runUntil(t, first, "three blocks", e.indexedThrough(t, start+2))
		cursor, ok := e.cursor(t)
		if !ok {
			t.Fatal("no cursor after the first run")
		}
		blocks := e.count(t, `SELECT count(*) FROM perps.blocks`)
		if blocks != int(cursor-start+1) {
			t.Fatalf("perps.blocks rows = %d, want %d (heights %d to %d)", blocks, cursor-start+1, start, cursor)
		}
		messages := len(e.streamMessages(t))

		second := e.newIndexer(t, "resumes-second", 0)
		next, err = second.resume(context.Background())
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if next != cursor+1 {
			t.Fatalf("resume after cursor %d = %d, want %d", cursor, next, cursor+1)
		}
		e.runUntil(t, second, "the block after the cursor", e.indexedThrough(t, cursor+1))
		if n := e.count(t, `SELECT count(*) FROM perps.blocks WHERE number = $1`, cursor+1); n != 1 {
			t.Fatalf("perps.blocks rows of height %d = %d, want 1", cursor+1, n)
		}
		if n := e.count(t, `SELECT count(*) FROM perps.blocks WHERE number < $1`, start); n != 0 {
			t.Fatalf("perps.blocks rows below the first start height = %d, want 0", n)
		}
		for _, m := range e.streamMessages(t)[messages:] {
			if o := sourceOrder(t, m); o[0] <= cursor {
				t.Fatalf("message %d of the second run at height %d, at or below cursor %d", m.seq, o[0], cursor)
			}
		}
	})

	t.Run("catch_up_batches", func(t *testing.T) {
		e := setup(t)
		ctx := context.Background()
		const behind = 1200
		head, err := e.client.BlockNumber(ctx)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		if head <= behind {
			t.Fatalf("node head %d is not above %d", head, behind)
		}
		cursor := head - behind
		header, err := e.client.HeaderByNumber(ctx, new(big.Int).SetUint64(cursor))
		if err != nil {
			t.Fatalf("header %d: %v", cursor, err)
		}
		if _, err := e.db.Exec(`INSERT INTO perps.cursors (name, height, hash, updated_at) VALUES ('head', $1, $2, now())`, int64(cursor), header.Hash().Bytes()); err != nil {
			t.Fatalf("insert cursor: %v", err)
		}
		node := &sizeRefusingNode{upstream: requireEnv(t, testRPCEnv), refuseSpan: source.MaxLogRange}
		proxy := httptest.NewServer(node)
		t.Cleanup(proxy.Close)
		ix := e.newIndexerVia(t, "catch-up-batches", 0, proxy.URL)
		e.runUntil(t, ix, "the catch-up to the head", e.indexedThrough(t, head))

		for _, c := range node.allCalls() {
			if c.span() > source.MaxLogRange {
				t.Fatalf("eth_getLogs [%d, %d] spans %d blocks, above %d", c.from, c.to, c.span(), source.MaxLogRange)
			}
		}
		calls := node.venueCalls()
		if len(calls) < 3 {
			t.Fatalf("venue eth_getLogs calls = %d, want at least the refused window and its two halves: %+v", len(calls), calls)
		}
		first := logsCall{from: cursor + 1, to: cursor + source.MaxLogRange, refused: true}
		lowHalf := logsCall{from: cursor + 1, to: cursor + source.MaxLogRange/2}
		highHalf := logsCall{from: cursor + source.MaxLogRange/2 + 1, to: cursor + source.MaxLogRange}
		for i, want := range []logsCall{first, lowHalf, highHalf} {
			got := calls[i]
			if got.from != want.from || got.to != want.to || got.refused != want.refused {
				t.Fatalf("venue eth_getLogs call %d = [%d, %d] refused %t, want [%d, %d] refused %t", i, got.from, got.to, got.refused, want.from, want.to, want.refused)
			}
		}
		for _, c := range calls[3:] {
			if c.refused {
				t.Fatalf("eth_getLogs [%d, %d] refused after the first window", c.from, c.to)
			}
		}
		next := cursor + 1
		for _, c := range calls {
			if c.refused {
				continue
			}
			if c.from > next {
				t.Fatalf("venue eth_getLogs calls skip heights %d to %d", next, c.from-1)
			}
			next = max(next, c.to+1)
		}
		if next <= head {
			t.Fatalf("venue eth_getLogs calls end at %d, below the head %d", next-1, head)
		}
		stored, ok := e.cursor(t)
		if !ok || stored < head {
			t.Fatalf("cursor after the catch-up = %d (%t), want at least %d", stored, ok, head)
		}
		if n := e.count(t, `SELECT count(*) FROM perps.blocks WHERE number > $1 AND number <= $2`, int64(cursor), int64(stored)); n != int(stored-cursor) {
			t.Fatalf("perps.blocks rows above the cursor %d through %d = %d, want %d", cursor, stored, n, stored-cursor)
		}
		if n := e.count(t, `SELECT count(*) FROM perps.blocks WHERE number <= $1`, int64(cursor)); n != 0 {
			t.Fatalf("perps.blocks rows at or below the starting cursor %d = %d, want 0", cursor, n)
		}
	})

	t.Run("readiness_lag", func(t *testing.T) {
		e := setup(t)
		ix := e.newIndexer(t, "readiness-lag", 0)
		ctx := context.Background()
		if err := ix.acquire(ctx); err != nil {
			t.Fatalf("acquire: %v", err)
		}
		mux := http.NewServeMux()
		httpx.RegisterHealth(mux, httpx.HealthDeps{Postgres: e.db.PingContext, Custom: ix.Checks()})
		ready := func(wantCode int, want map[string]string) {
			t.Helper()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
			if rec.Code != wantCode {
				t.Fatalf("/healthz/ready = %d, want %d: %s", rec.Code, wantCode, rec.Body)
			}
			var body struct {
				Checks map[string]string `json:"checks"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("ready body: %v", err)
			}
			for name, status := range want {
				if body.Checks[name] != status {
					t.Fatalf("check %s = %q, want %q (%v)", name, body.Checks[name], status, body.Checks)
				}
			}
		}

		ready(http.StatusServiceUnavailable, map[string]string{"lag": "failed", "lease": "ok", "outbox": "ok"})
		ix.setCursor(1000)
		ix.setHead(1031)
		ready(http.StatusServiceUnavailable, map[string]string{"lag": "failed", "lease": "ok", "outbox": "ok", "postgres": "ok"})
		ix.setHead(1030)
		ready(http.StatusOK, map[string]string{"lag": "ok", "lease": "ok", "outbox": "ok", "postgres": "ok"})

		if _, err := e.db.Exec(`INSERT INTO perps.delivery_outbox (event_id, schema_version, subject, payload_hash, envelope, created_at) VALUES ('stale', 1, $1, '', '\x7b7d', clock_timestamp() - interval '31 seconds')`, "perps."+strconv.FormatUint(e.chain, 10)+".chain.block"); err != nil {
			t.Fatalf("insert stale outbox row: %v", err)
		}
		ready(http.StatusServiceUnavailable, map[string]string{"lag": "ok", "lease": "ok", "outbox": "failed"})
		if _, err := e.db.Exec(`DELETE FROM perps.delivery_outbox WHERE event_id = 'stale'`); err != nil {
			t.Fatalf("delete stale outbox row: %v", err)
		}
		ix.release(ctx)
		ready(http.StatusServiceUnavailable, map[string]string{"lag": "ok", "lease": "failed", "outbox": "ok"})
	})

	t.Run("publishes_in_order", func(t *testing.T) {
		e := setup(t)
		logs := e.latestLogs(t, ethereum.FilterQuery{Addresses: []common.Address{decode.PerpsAddress}}, 3)
		from, to := logs[0].BlockNumber, logs[len(logs)-1].BlockNumber
		ix := e.newIndexer(t, "publishes-in-order", from)
		e.runUntil(t, ix, "the venue logs", e.indexedThrough(t, to))

		rows, err := e.db.Query(`SELECT perps_seq, broker_sequence FROM perps.delivery_outbox ORDER BY perps_seq`)
		if err != nil {
			t.Fatalf("read outbox: %v", err)
		}
		defer rows.Close()
		var lastSeq, lastBroker int64
		var published int
		for rows.Next() {
			var seq, broker int64
			if err := rows.Scan(&seq, &broker); err != nil {
				t.Fatalf("scan outbox: %v", err)
			}
			if seq <= lastSeq || broker <= lastBroker {
				t.Fatalf("outbox perps_seq %d broker_sequence %d after %d %d, want both increasing", seq, broker, lastSeq, lastBroker)
			}
			lastSeq, lastBroker = seq, broker
			published++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read outbox: %v", err)
		}

		msgs := e.streamMessages(t)
		if len(msgs) != published {
			t.Fatalf("stream holds %d messages, outbox published %d", len(msgs), published)
		}
		first := map[[3]uint64]uint64{}
		var prev streamMsg
		var prevOrder [3]uint64
		ordered := 0
		for i, m := range msgs {
			if m.block != m.envelope.SourceOrder.Height {
				t.Fatalf("message %d Perps-Block %q, source height %q", m.seq, m.block, m.envelope.SourceOrder.Height)
			}
			if i > 0 && (m.seq <= prev.seq || m.perpsSeq <= prev.perpsSeq) {
				t.Fatalf("message %d stream seq %d Perps-Seq %d after %d %d, want both increasing", i, m.seq, m.perpsSeq, prev.seq, prev.perpsSeq)
			}
			prev = m
			if chainSubject.MatchString(m.subject) {
				continue
			}
			o := sourceOrder(t, m)
			if ordered > 0 && less(o, prevOrder) {
				t.Fatalf("message %d on %s at %v after %v", m.seq, m.subject, o, prevOrder)
			}
			prevOrder = o
			ordered++
			if _, ok := first[o]; !ok {
				first[o] = m.seq
			}
		}
		var last uint64
		for _, l := range logs {
			seq, ok := first[[3]uint64{l.BlockNumber, uint64(l.TxIndex), uint64(l.Index)}]
			if !ok {
				t.Fatalf("no stream message of the log at block %d (%d, %d)", l.BlockNumber, l.TxIndex, l.Index)
			}
			if seq <= last {
				t.Fatalf("log at block %d (%d, %d) first published at stream seq %d, after %d", l.BlockNumber, l.TxIndex, l.Index, seq, last)
			}
			last = seq
		}
	})
}
