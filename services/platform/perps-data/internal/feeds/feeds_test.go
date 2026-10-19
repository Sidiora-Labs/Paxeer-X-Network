package feeds

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ws"
)

var (
	perpsAddr = perpsEmitter
	spotAddr  = addr(0x21, 0x10)
	vaultAddr = addr(0x25, 0x10)
	usdlToken = addr(0x01, 0xa1)
	paxToken  = addr(0x02, 0xa2)
	sidToken  = addr(0x03, 0xa3)
	trader    = addr(0x04, 0xb1)
	maker     = addr(0x05, 0xb2)
)

var touchedTables = []string{
	"orders", "order_events", "fills", "mark_prices", "index_prices", "funding_rates", "funding_settlements", "open_interest",
	"liquidations", "spot_orders", "spot_swaps", "spot_order_events", "spot_token_registry", "market_stats_rolling", "blocks",
	"tx_actions", "cursors",
}

func addr(lo, hi byte) []byte {
	b := make([]byte, 20)
	b[18], b[19] = hi, lo
	return b
}

func hash32(block int64, txIndex, logIndex int) []byte {
	return []byte(fmt.Sprintf("%016d%08d%08d", block, txIndex+1, logIndex))
}

func e18(t *testing.T, v string) string { return scaledInt(t, v, 18) }
func e6(t *testing.T, v string) string  { return scaledInt(t, v, 6) }

func scaledInt(t *testing.T, v string, decimals int64) string {
	t.Helper()
	r, ok := new(big.Rat).SetString(v)
	if !ok {
		t.Fatalf("bad decimal %q", v)
	}
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil)))
	if !r.IsInt() {
		t.Fatalf("%q has more than %d decimals", v, decimals)
	}
	return r.Num().String()
}

func openDB(t *testing.T) *sql.DB {
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

func reset(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := make([]string, len(touchedTables))
	for i, name := range touchedTables {
		tables[i] = "perps." + name
	}
	if _, err := db.Exec("TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// fact is one fact row: the common columns plus its argument columns.
type fact struct {
	table, kind       string
	block             int64
	txIndex, logIndex int
	emitter, account  []byte
	args              []string
	values            []any
}

func insert(t *testing.T, db *sql.DB, r fact) {
	t.Helper()
	cols := append([]string{"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}, r.args...)
	vals := append([]any{r.block, hash32(r.block, -1, 0), r.txIndex, r.logIndex, 0, hash32(r.block, r.txIndex, r.logIndex), "tx", time.Now(), r.emitter, r.kind, r.account, trader}, r.values...)
	marks := make([]string, len(cols))
	quoted := make([]string, len(cols))
	for i, c := range cols {
		marks[i] = fmt.Sprintf("$%d", i+1)
		quoted[i] = `"` + c + `"`
	}
	exec(t, db, fmt.Sprintf("INSERT INTO perps.%s (%s) VALUES (%s)", r.table, strings.Join(quoted, ", "), strings.Join(marks, ", ")), vals...)
}

func head(t *testing.T, db *sql.DB, height int64) {
	t.Helper()
	exec(t, db, `INSERT INTO perps.cursors (name, height, hash) VALUES ('head', $1, $2)
		ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height`, height, hash32(height, -1, 0))
}

func order(t *testing.T, db *sql.DB, id, market int64, long bool, limit *string, remaining, status string) {
	t.Helper()
	var lp any
	if limit != nil {
		lp = e18(t, *limit)
	}
	exec(t, db, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price, size_usd, limit_price,
		remaining_size_usd, status, placed_block) VALUES ($1, $2, $3, 1, $4, 0, $5, $6, $5, $7, 1)`,
		id, trader, market, long, e18(t, remaining), lp, status)
}

func str(s string) *string { return &s }

func fill(t *testing.T, db *sql.DB, market, block int64, logIndex int, makerID, takerID int64, takerLong bool, price, sizeUSD string) {
	t.Helper()
	insert(t, db, fact{table: "fills", kind: "fill", block: block, logIndex: logIndex, emitter: perpsAddr, account: trader,
		args:   []string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long", "price", "size_usd", "maker_fee_usd", "taker_fee_usd"},
		values: []any{market, makerID, takerID, maker, trader, takerLong, e18(t, price), e18(t, sizeUSD), "0", "0"}})
}

func markPrice(t *testing.T, db *sql.DB, market, block int64, mark, index string) {
	t.Helper()
	insert(t, db, fact{table: "mark_prices", kind: "mark", block: block, emitter: perpsAddr, account: perpsAddr,
		args: []string{"market_id", "mark_price", "index_price", "mid_price"}, values: []any{market, e18(t, mark), e18(t, index), e18(t, mark)}})
}

func registerTokens(t *testing.T, db *sql.DB) {
	t.Helper()
	for i, tok := range []struct {
		token  []byte
		stable bool
	}{{usdlToken, true}, {paxToken, false}, {sidToken, false}} {
		insert(t, db, fact{table: "spot_token_registry", kind: "token_registered", block: 1, logIndex: i, emitter: vaultAddr, account: vaultAddr,
			args: []string{"token", "decimals", "is_stablecoin"}, values: []any{tok.token, 6, tok.stable}})
	}
}

func spotOrder(t *testing.T, db *sql.DB, id int64, kind, side string, token []byte, amount string, target *string, status string) {
	t.Helper()
	var tp any
	if target != nil {
		tp = e18(t, *target)
	}
	exec(t, db, `INSERT INTO perps.spot_orders (order_id, account, kind, side, token, stablecoin, amount, target_price, status, placed_block)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1)`, id, trader, kind, side, token, usdlToken, e6(t, amount), tp, status)
}

func marketOrder(t *testing.T, db *sql.DB, block int64, logIndex int, token []byte, buy bool, stable, amount string) {
	t.Helper()
	insert(t, db, fact{table: "spot_swaps", kind: "market_order", block: block, logIndex: logIndex, emitter: spotAddr, account: trader,
		args:   []string{"user", "stablecoin", "token", "stablecoin_amount", "token_amount", "is_buy"},
		values: []any{trader, usdlToken, token, e6(t, stable), e6(t, amount), buy}})
}

func block(t *testing.T, db *sql.DB, number int64, txs int) {
	t.Helper()
	exec(t, db, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
		number, hash32(number, -1, 0), hash32(number-1, -1, 0), time.Unix(1_700_000_000+number, 0), strings.Repeat("AB", 20), txs)
}

func txAction(t *testing.T, db *sql.DB, number int64, logIndex int, action string, price, size *string) {
	t.Helper()
	var p, s any
	if price != nil {
		p = e18(t, *price)
	}
	if size != nil {
		s = e18(t, *size)
	}
	insert(t, db, fact{table: "tx_actions", kind: "fill", block: number, logIndex: logIndex, emitter: perpsAddr, account: trader,
		args: []string{"action", "symbol", "side", "price", "size"}, values: []any{action, "BTC", "buy", p, s}})
}

func providers(t *testing.T, db *sql.DB) *Providers {
	t.Helper()
	p, err := NewProviders(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func snapshot(t *testing.T, p *Providers, topic string) json.RawMessage {
	t.Helper()
	data, err := p.Snapshot(context.Background(), topic)
	if err != nil {
		t.Fatalf("snapshot %s: %v", topic, err)
	}
	return data
}

func subscribe(t *testing.T, p *Providers, topic string) json.RawMessage {
	t.Helper()
	p.Subscribed(topic)
	return snapshot(t, p, topic)
}

func at(height int64, logIndex int) delivery.Envelope {
	return delivery.Envelope{SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(height, 10), TransactionIndex: "0", LogIndex: strconv.Itoa(logIndex)}}
}

func message(t *testing.T, p *Providers, subject string, env delivery.Envelope) ([]Frame, []string) {
	t.Helper()
	frames, resnap, err := p.Message(context.Background(), subject, env)
	if err != nil {
		t.Fatalf("message %s: %v", subject, err)
	}
	return frames, resnap
}

func only(frames []Frame, topic string) []Frame {
	var out []Frame
	for _, f := range frames {
		if f.Topic == topic {
			out = append(out, f)
		}
	}
	return out
}

func decodeJSON[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return v
}

func wantJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// symbolBody writes the @stats or @perp_stats body of sym from records.Symbol.
func symbolBody(t *testing.T, db *sql.DB, sym, kind string) json.RawMessage {
	t.Helper()
	s, err := records.NewSymbol(db)
	if err != nil {
		t.Fatal(err)
	}
	var body any
	if kind == kindStats {
		body, err = s.Stats(context.Background(), sym)
	} else {
		body, err = s.PerpStats(context.Background(), sym)
	}
	if err != nil {
		t.Fatalf("%s %s: %v", sym, kind, err)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type bookJSON struct {
	Symbol   string   `json:"symbol"`
	Asks     []Level  `json:"asks"`
	Bids     []Level  `json:"bids"`
	Checksum *uint32  `json:"checksum"`
	Mark     *float64 `json:"mark_price"`
}

// seedBook writes the BTC book of the perp subtests: bids 15 contracts at
// 100 from two orders, asks 2 contracts at 101, and orders the book must
// leave out (no limit, zero limit, nothing remaining, cancelled, other
// market).
func seedBook(t *testing.T, db *sql.DB) {
	t.Helper()
	head(t, db, 10)
	markPrice(t, db, 0, 9, "100.5", "100")
	order(t, db, 1, 0, true, str("100"), "1000", "active")
	order(t, db, 2, 0, true, str("100"), "500", "active")
	order(t, db, 3, 0, false, str("101"), "202", "active")
	order(t, db, 4, 0, false, nil, "50", "active")
	order(t, db, 5, 0, false, str("0"), "50", "active")
	order(t, db, 6, 0, true, str("99"), "0", "active")
	order(t, db, 7, 0, true, str("98"), "70", "cancelled")
	order(t, db, 8, 1, true, str("100"), "70", "active")
}

func TestFeeds(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	t0 := time.Unix(1_800_000_000, 0)

	t.Run("perp_orderbook", func(t *testing.T) {
		reset(t, db)
		seedBook(t, db)
		p := providers(t, db)
		wantJSON(t, subscribe(t, p, "BTC@perp_orderbook"),
			`{"symbol":"BTC","mark_price":100.5,"index_price":100,"basis_bps":50,"asks":[[101,2]],"bids":[[100,15]]}`)
		// OrderPlaced and OrderOptions of a new ask, then a Fill of order 1.
		insert(t, db, fact{table: "order_events", kind: "placed", block: 11, logIndex: 0, emitter: perpsAddr, account: trader,
			args:   []string{"order_id", "user", "market_id", "order_type", "is_long", "trigger_price", "size_usd"},
			values: []any{9, trader, 0, 1, false, "0", e18(t, "303")}})
		insert(t, db, fact{table: "order_events", kind: "options", block: 11, logIndex: 1, emitter: perpsAddr, account: trader,
			args:   []string{"order_id", "time_in_force", "reduce_only", "limit_price", "acceptable_price", "deadline"},
			values: []any{9, 0, false, e18(t, "101"), "0", 0}})
		fill(t, db, 0, 12, 0, 1, 0, false, "100", "500")
		head(t, db, 12)
		for _, m := range []struct {
			kind string
			env  delivery.Envelope
		}{{"order", at(11, 0)}, {"order", at(11, 1)}, {"fill", at(12, 0)}} {
			if frames, _ := message(t, p, "perps.125.0."+m.kind, m.env); len(only(frames, "BTC@perp_orderbook")) != 0 {
				t.Fatalf("book frames are sent by Run, got %v", frames)
			}
		}
		frames := only(p.tick(ctx, t0), "BTC@perp_orderbook")
		if len(frames) != 1 {
			t.Fatalf("book frames %d, want 1", len(frames))
		}
		got := decodeJSON[bookJSON](t, frames[0].Data)
		if !reflect.DeepEqual(got.Asks, []Level{{101, 5}}) || !reflect.DeepEqual(got.Bids, []Level{{100, 10}}) {
			t.Fatalf("live book %s", frames[0].Data)
		}
		// A cancel ends order 2.
		insert(t, db, fact{table: "order_events", kind: "cancelled", block: 13, logIndex: 0, emitter: perpsAddr, account: trader,
			args: []string{"order_id", "user"}, values: []any{2, trader}})
		message(t, p, "perps.125.0.order", at(13, 0))
		frames = only(p.tick(ctx, t0.Add(BookInterval)), "BTC@perp_orderbook")
		if len(frames) != 1 || !reflect.DeepEqual(decodeJSON[bookJSON](t, frames[0].Data).Bids, []Level{{100, 5}}) {
			t.Fatalf("book after cancel %v", frames)
		}
	})

	t.Run("orderbook", func(t *testing.T) {
		reset(t, db)
		registerTokens(t, db)
		spotOrder(t, db, 1, "limit", "sell", paxToken, "2", str("1.5"), "open")
		spotOrder(t, db, 2, "limit", "sell", paxToken, "1", str("1.5"), "open")
		spotOrder(t, db, 3, "limit", "buy", paxToken, "3", str("1.2"), "open")
		spotOrder(t, db, 4, "limit", "sell", paxToken, "9", str("1.4"), "executed")
		spotOrder(t, db, 5, "stop_limit", "sell", paxToken, "9", nil, "open")
		spotOrder(t, db, 6, "limit", "sell", sidToken, "9", str("1.1"), "open")
		p := providers(t, db)
		wantJSON(t, subscribe(t, p, "PAX@orderbook"), `{"symbol":"PAX","asks":[[1.5,3]],"bids":[[1.2,2.5]]}`)
		spotOrder(t, db, 7, "limit", "buy", paxToken, "1.3", str("1.3"), "open")
		message(t, p, "perps.125.spot.0x"+fmt.Sprintf("%x", paxToken)+".order", at(5, 0))
		frames := only(p.tick(ctx, t0), "PAX@orderbook")
		if len(frames) != 1 {
			t.Fatalf("spot book frames %d", len(frames))
		}
		got := decodeJSON[bookJSON](t, frames[0].Data)
		if !reflect.DeepEqual(got.Bids, []Level{{1.3, 1}, {1.2, 2.5}}) || !reflect.DeepEqual(got.Asks, []Level{{1.5, 3}}) {
			t.Fatalf("spot live book %s", frames[0].Data)
		}
	})

	t.Run("book_ordering", func(t *testing.T) {
		reset(t, db)
		head(t, db, 1)
		for i := int64(0); i < 120; i++ {
			order(t, db, 100+i, 0, true, str(strconv.FormatInt(1000-i, 10)), "1000", "active")
			order(t, db, 300+i, 0, false, str(strconv.FormatInt(2000+i, 10)), "2000", "active")
		}
		p := providers(t, db)
		snap := decodeJSON[bookJSON](t, subscribe(t, p, "BTC@perp_orderbook"))
		if len(snap.Asks) != SnapshotLevels || len(snap.Bids) != SnapshotLevels {
			t.Fatalf("snapshot levels %d/%d", len(snap.Asks), len(snap.Bids))
		}
		for i := range SnapshotLevels {
			if snap.Asks[i][0] != float64(2000+i) || snap.Bids[i][0] != float64(1000-i) {
				t.Fatalf("level %d: ask %v bid %v", i, snap.Asks[i], snap.Bids[i])
			}
		}
		frames := only(p.tick(ctx, t0), "BTC@perp_orderbook")
		live := decodeJSON[bookJSON](t, frames[0].Data)
		if len(live.Asks) != LiveLevels || len(live.Bids) != LiveLevels || live.Asks[0][0] != 2000 || live.Bids[0][0] != 1000 ||
			live.Asks[19][0] != 2019 || live.Bids[19][0] != 981 {
			t.Fatalf("live levels %s", frames[0].Data)
		}
	})

	t.Run("book_rate_limit", func(t *testing.T) {
		reset(t, db)
		seedBook(t, db)
		p := providers(t, db)
		subscribe(t, p, "BTC@perp_orderbook")
		count := 0
		for i := range 100 {
			p.mu.Lock()
			p.books["BTC@perp_orderbook"].dirty = true
			p.mu.Unlock()
			count += len(only(p.bookFrames(ctx, t0.Add(time.Duration(i)*10*time.Millisecond)), "BTC@perp_orderbook"))
		}
		if count != 10 {
			t.Fatalf("frames in one second %d, want 10", count)
		}
		if n := len(only(p.bookFrames(ctx, t0.Add(1050*time.Millisecond)), "BTC@perp_orderbook")); n != 1 {
			t.Fatalf("change coalesced at 990ms sent %d frames, want 1", n)
		}
		if n := len(only(p.bookFrames(ctx, t0.Add(1300*time.Millisecond)), "BTC@perp_orderbook")); n != 0 {
			t.Fatalf("clean book sent %d frames", n)
		}
	})

	t.Run("book_checksum", func(t *testing.T) {
		reset(t, db)
		seedBook(t, db)
		p := providers(t, db)
		if snap := decodeJSON[bookJSON](t, subscribe(t, p, "BTC@perp_orderbook")); snap.Checksum != nil {
			t.Fatal("snapshot carries checksum")
		}
		want := crc32.ChecksumIEEE([]byte("101:2,100:15"))
		if got := p.books["BTC@perp_orderbook"].perp.Checksum(); got != want {
			t.Fatalf("Book.Checksum %d, want %d", got, want)
		}
		frames := only(p.bookFrames(ctx, t0), "BTC@perp_orderbook")
		if c := decodeJSON[bookJSON](t, frames[0].Data).Checksum; c == nil || *c != want {
			t.Fatalf("first frame %s", frames[0].Data)
		}
		p.books["BTC@perp_orderbook"].dirty = true
		frames = only(p.bookFrames(ctx, t0.Add(time.Second)), "BTC@perp_orderbook")
		if len(frames) != 1 || decodeJSON[bookJSON](t, frames[0].Data).Checksum != nil {
			t.Fatalf("dirty frame inside 5 seconds %v", frames)
		}
		if n := len(p.bookFrames(ctx, t0.Add(4999*time.Millisecond))); n != 0 {
			t.Fatalf("clean book before 5 seconds sent %d frames", n)
		}
		frames = only(p.bookFrames(ctx, t0.Add(ChecksumEvery)), "BTC@perp_orderbook")
		if len(frames) != 1 {
			t.Fatalf("checksum frames %d", len(frames))
		}
		if c := decodeJSON[bookJSON](t, frames[0].Data).Checksum; c == nil || *c != want {
			t.Fatalf("5 second frame %s", frames[0].Data)
		}
	})

	t.Run("perp_trade", func(t *testing.T) {
		reset(t, db)
		fill(t, db, 0, 5, 0, 1, 2, true, "100", "250")
		fill(t, db, 0, 6, 0, 1, 2, false, "101", "101")
		p := providers(t, db)
		snap := decodeJSON[struct {
			Symbol string           `json:"symbol"`
			Trades []map[string]any `json:"trades"`
		}](t, subscribe(t, p, "BTC@perp_trade"))
		if snap.Symbol != "BTC" || len(snap.Trades) != 2 || snap.Trades[0]["id"] != "6-0" || snap.Trades[1]["id"] != "5-0" {
			t.Fatalf("snapshot %+v", snap)
		}
		fill(t, db, 0, 7, 3, 1, 2, true, "100", "50")
		frames, resnap := message(t, p, "perps.125.0.fill", at(7, 3))
		if len(frames) != 1 || len(resnap) != 0 {
			t.Fatalf("frames %v resnap %v", frames, resnap)
		}
		got := decodeJSON[map[string]any](t, frames[0].Data)
		want := map[string]any{"symbol": "BTC", "id": "7-3", "side": "BUY", "price": 100.0, "contracts": 0.5, "notional": 50.0,
			"liquidation": false, "trade_time": got["trade_time"]}
		if !reflect.DeepEqual(got, want) || got["trade_time"] == nil {
			t.Fatalf("perp trade %v", got)
		}
	})

	t.Run("perp_trade_once_per_fill", func(t *testing.T) {
		reset(t, db)
		p := providers(t, db)
		subscribe(t, p, "BTC@perp_trade")
		fill(t, db, 0, 5, 0, 1, 2, true, "100", "100")
		fill(t, db, 0, 5, 1, 1, 2, true, "100", "200")
		frames, _ := message(t, p, "perps.125.0.fill", at(5, 0))
		frames2, _ := message(t, p, "perps.125.0.fill", at(5, 1))
		frames3, _ := message(t, p, "perps.125.0.fill", at(5, 1))
		ids := []string{}
		for _, f := range append(append(frames, frames2...), frames3...) {
			ids = append(ids, decodeJSON[map[string]any](t, f.Data)["id"].(string))
		}
		if !reflect.DeepEqual(ids, []string{"5-0", "5-1"}) {
			t.Fatalf("frames %v, want one per fill", ids)
		}
		if frames, _ := message(t, p, "perps.125.1.fill", at(5, 0)); len(frames) != 0 {
			t.Fatalf("unwatched market sent %v", frames)
		}
	})

	t.Run("trade", func(t *testing.T) {
		reset(t, db)
		registerTokens(t, db)
		marketOrder(t, db, 5, 0, paxToken, true, "3", "2")
		p := providers(t, db)
		snap := decodeJSON[struct {
			Symbol string           `json:"symbol"`
			Trades []map[string]any `json:"trades"`
		}](t, subscribe(t, p, "PAX@trade"))
		if snap.Symbol != "PAX" || len(snap.Trades) != 1 || snap.Trades[0]["price"] != 1.5 || snap.Trades[0]["size"] != 2.0 {
			t.Fatalf("snapshot %+v", snap)
		}
		marketOrder(t, db, 6, 0, paxToken, false, "1", "4")
		marketOrder(t, db, 6, 1, sidToken, false, "1", "4")
		frames, _ := message(t, p, "perps.125.spot.0x"+fmt.Sprintf("%x", paxToken)+".swap", at(6, 0))
		if len(frames) != 1 {
			t.Fatalf("frames %v", frames)
		}
		got := decodeJSON[map[string]any](t, frames[0].Data)
		if got["symbol"] != "PAX" || got["side"] != "SELL" || got["price"] != 0.25 || got["size"] != 4.0 {
			t.Fatalf("trade %v", got)
		}
		if frames, _ := message(t, p, "perps.125.spot.0x"+fmt.Sprintf("%x", sidToken)+".swap", at(6, 1)); len(frames) != 0 {
			t.Fatalf("unwatched SID tape sent %v", frames)
		}
	})

	t.Run("perp_stats", func(t *testing.T) {
		reset(t, db)
		markPrice(t, db, 0, 4, "100.5", "100")
		fill(t, db, 0, 5, 0, 1, 2, true, "100", "250")
		p := providers(t, db)
		wantJSON(t, subscribe(t, p, "BTC@perp_stats"), string(symbolBody(t, db, "BTC", kindPerpStats)))
		if n := len(only(p.tick(ctx, t0), "BTC@perp_stats")); n != 1 {
			t.Fatalf("first tick stats %d", n)
		}
		if n := len(only(p.tick(ctx, t0.Add(StatsInterval-time.Second)), "BTC@perp_stats")); n != 0 {
			t.Fatalf("stats inside 30 seconds %d", n)
		}
		frames := only(p.tick(ctx, t0.Add(StatsInterval)), "BTC@perp_stats")
		if len(frames) != 1 {
			t.Fatalf("stats after 30 seconds %d", len(frames))
		}
		wantJSON(t, frames[0].Data, string(symbolBody(t, db, "BTC", kindPerpStats)))
	})

	t.Run("stats", func(t *testing.T) {
		reset(t, db)
		fill(t, db, 11, 5, 0, 1, 2, true, "0.5", "10")
		p := providers(t, db)
		wantJSON(t, subscribe(t, p, "PAX@stats"), string(symbolBody(t, db, "PAX", kindStats)))
		frames := only(p.tick(ctx, t0), "PAX@stats")
		if len(frames) != 1 {
			t.Fatalf("stats frames %d", len(frames))
		}
		wantJSON(t, frames[0].Data, `{"symbol":"PAX","volume_24h":10,"open_interest":0,"est_funding_rate":null,"last_funding_rate":null}`)
		if p.Served("BTC@stats") {
			t.Fatal("BTC@stats served")
		}
	})

	t.Run("market", func(t *testing.T) {
		reset(t, db)
		block(t, db, 5, 1)
		markPrice(t, db, 0, 4, "100.5", "100")
		fill(t, db, 0, 5, 0, 1, 2, true, "100", "250")
		insert(t, db, fact{table: "open_interest", kind: "oi", block: 5, logIndex: 1, emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "long_oi", "short_oi", "delta_usd", "is_increase"}, values: []any{0, e18(t, "300"), e18(t, "100"), "0", true}})
		exec(t, db, `INSERT INTO perps.market_stats_rolling (market_id, volume_24h, volume_7d, volume_30d, trades_count, open_interest_long,
			open_interest_short, last_price, open_24h, updated_block) VALUES (0, $1, $2, $3, 7, 0, 0, $4, $5, 5)`,
			e18(t, "250"), e18(t, "700"), e18(t, "900"), e18(t, "110"), e18(t, "100"))
		p := providers(t, db)
		rec := decodeJSON[map[string]any](t, subscribe(t, p, "BTC@market"))
		if len(rec) != 23 {
			t.Fatalf("record has %d fields: %v", len(rec), rec)
		}
		next := float64(time.Now().UTC().Truncate(time.Hour).Add(time.Hour).UnixMilli())
		want := map[string]any{
			"symbol": "BTC", "symbol_full": "Bitcoin", "price": 100.0, "change_24h": 0.1, "volume_24h": 250.0, "open_interest": 400.0,
			"funding_8h": nil, "est_funding_rate": nil, "volume_7d": 700.0, "volume_30d": 900.0, "trades_count": 7.0,
			"last_updated": float64(time.Unix(1_700_000_005, 0).UnixMilli()), "perp_mark_price": 100.5, "perp_index_price": 100.0,
			"perp_basis_bps": 50.0, "perp_funding_rate": nil, "perp_next_funding_ms": next, "perp_long_short_ratio": 3.0,
			"perp_liq_volume_24h": 0.0, "perp_oi_contracts": 400 / 100.5, "perp_oi_usd": 400.0, "perp_volume_24h_contracts": 2.5,
			"perp_volume_24h_usd": 250.0,
		}
		if !reflect.DeepEqual(rec, want) {
			t.Fatalf("record\n got %v\nwant %v", rec, want)
		}
		if n := len(only(p.tick(ctx, t0), "BTC@market")); n != 1 {
			t.Fatalf("watched market frames %d", n)
		}
		if n := len(only(p.tick(ctx, t0.Add(MarketInterval-time.Second)), "BTC@market")); n != 0 {
			t.Fatalf("market inside 5 seconds %d", n)
		}
		if n := len(only(p.tick(ctx, t0.Add(MarketInterval)), "BTC@market")); n != 1 {
			t.Fatalf("market after 5 seconds %d", n)
		}
		p.Unsubscribed("BTC@market")
		frames := p.tick(ctx, t0.Add(2*MarketInterval))
		if n := len(only(frames, "BTC@market")); n != 0 {
			t.Fatalf("unwatched market frames %d", n)
		}
		if n := len(only(frames, "ETH@market")); n != 0 {
			t.Fatalf("never watched market frames %d", n)
		}
	})

	t.Run("markets_all", func(t *testing.T) {
		reset(t, db)
		p := providers(t, db)
		records := decodeJSON[[]map[string]any](t, subscribe(t, p, TopicMarketsAll))
		if len(records) != 18 || records[0]["symbol"] != "BTC" || records[17]["symbol"] != "BNB" {
			t.Fatalf("markets %d", len(records))
		}
		frames := only(p.tick(ctx, t0), TopicMarketsAll)
		if len(frames) != 1 || len(decodeJSON[[]map[string]any](t, frames[0].Data)) != 18 {
			t.Fatalf("markets@all frames %v", frames)
		}
		if n := len(only(p.tick(ctx, t0.Add(MarketInterval)), TopicMarketsAll)); n != 1 {
			t.Fatalf("markets@all after 5 seconds %d", n)
		}
	})

	t.Run("markets_totals", func(t *testing.T) {
		reset(t, db)
		fill(t, db, 0, 5, 0, 1, 2, true, "100", "250")
		fill(t, db, 1, 5, 1, 1, 2, true, "10", "50")
		for _, m := range []struct {
			id     int64
			trades int
		}{{0, 3}, {1, 4}} {
			exec(t, db, `INSERT INTO perps.market_stats_rolling (market_id, volume_24h, volume_7d, volume_30d, trades_count, open_interest_long,
				open_interest_short, updated_block) VALUES ($1, 0, $2, $3, $4, 0, 0, 5)`, m.id, e18(t, "100"), e18(t, "1000"), m.trades)
		}
		insert(t, db, fact{table: "open_interest", kind: "oi", block: 5, logIndex: 2, emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "long_oi", "short_oi", "delta_usd", "is_increase"}, values: []any{1, e18(t, "30"), e18(t, "20"), "0", true}})
		p := providers(t, db)
		wantJSON(t, subscribe(t, p, TopicMarketsTotals), `{"total_trades":7,"volume_24h":300,"volume_7d":200,"volume_30d":2000,"total_oi":50}`)
		if n := len(only(p.tick(ctx, t0), TopicMarketsTotals)); n != 1 {
			t.Fatalf("totals frames %d", n)
		}
	})

	t.Run("chain_blocks", func(t *testing.T) {
		reset(t, db)
		for n := int64(1); n <= 25; n++ {
			block(t, db, n, int(n))
		}
		p := providers(t, db)
		blocks := decodeJSON[[]map[string]any](t, subscribe(t, p, TopicChainBlocks))
		if len(blocks) != BlockSnapshot || blocks[0]["block_number"] != 25.0 || blocks[19]["block_number"] != 6.0 {
			t.Fatalf("blocks snapshot %v", blocks)
		}
		block(t, db, 26, 3)
		frames, _ := message(t, p, "perps.125.chain.block", at(26, 0))
		if len(frames) != 1 {
			t.Fatalf("block frames %v", frames)
		}
		wantJSON(t, frames[0].Data, fmt.Sprintf(`{"block_number":26,"time":"2023-11-14T22:13:46.000Z","proposer":"%s","hash":"0x%x","transactions":3}`,
			strings.Repeat("AB", 20), hash32(26, -1, 0)))
		_, resnap := message(t, p, "perps.125.chain.rewind", delivery.Envelope{})
		if !reflect.DeepEqual(resnap, []string{TopicChainBlocks}) {
			t.Fatalf("rewind resnapshot %v", resnap)
		}
	})

	t.Run("chain_transactions", func(t *testing.T) {
		reset(t, db)
		for i := range 60 {
			txAction(t, db, 1, i, "OROB Fill", str("100"), str("0.5"))
		}
		p := providers(t, db)
		txs := decodeJSON[[]map[string]any](t, subscribe(t, p, TopicChainTxs))
		if len(txs) != TxSnapshot {
			t.Fatalf("transactions snapshot %d", len(txs))
		}
		txAction(t, db, 2, 0, "Oracle Updated", nil, str("0.000000000000000003"))
		frames, _ := message(t, p, "perps.125.chain.tx", at(2, 0))
		if len(frames) != 1 {
			t.Fatalf("tx frames %v", frames)
		}
		got := decodeJSON[map[string]any](t, frames[0].Data)
		if got["action"] != "Oracle Updated" || got["size"] != 3.0 || got["price"] != nil || got["block"] != 2.0 ||
			!strings.HasPrefix(got["user"].(string), "pax1") || got["symbol"] != "BTC" || got["side"] != "buy" {
			t.Fatalf("tx frame %v", got)
		}
		if first := txs[0]; first["price"] != 100.0 || first["size"] != 0.5 {
			t.Fatalf("fill row %v", first)
		}
	})

	t.Run("spot_unknown_symbol", func(t *testing.T) {
		reset(t, db)
		p := providers(t, db)
		for _, topic := range []string{"BTC@orderbook", "ETH@trade", "BNB@stats", "pax@orderbook"} {
			if p.Served(topic) {
				t.Fatalf("%s served", topic)
			}
			if _, err := ws.ParseClientFrame([]byte(`{"event":"subscribe","topic":"`+topic+`"}`), p.Served); err != ws.ErrUnknownTopic {
				t.Fatalf("%s: %v, want unknown_topic", topic, err)
			}
		}
		for _, topic := range []string{"PAX@orderbook", "SID@trade", "SID@stats"} {
			if !p.Served(topic) {
				t.Fatalf("%s not served", topic)
			}
		}
	})

	t.Run("unknown_symbol_topic", func(t *testing.T) {
		reset(t, db)
		p := providers(t, db)
		for _, topic := range []string{"DOGE@perp_orderbook", "DOGE@perp_trade", "DOGE@market", "BTC@candles", "BTC", "markets@none", "chain@logs"} {
			if _, err := ws.ParseClientFrame([]byte(`{"event":"subscribe","topic":"`+topic+`"}`), p.Served); err != ws.ErrUnknownTopic {
				t.Fatalf("%s: %v, want unknown_topic", topic, err)
			}
		}
		if _, err := p.Snapshot(ctx, "DOGE@perp_orderbook"); err == nil {
			t.Fatal("snapshot of an unknown symbol")
		}
		for _, topic := range []string{"BTC@perp_orderbook", "BNB@perp_trade", "XAU@perp_stats", "SPX500@market", TopicMarketsAll, TopicChainTxs} {
			if !p.Served(topic) {
				t.Fatalf("%s not served", topic)
			}
		}
	})

	t.Run("no_kline_topic", func(t *testing.T) {
		reset(t, db)
		p := providers(t, db)
		for _, interval := range []string{"1m", "2m", "3m", "5m", "15m", "30m", "1h", "4h", "8h", "1D", "1W"} {
			for _, sym := range []string{"BTC", "PAX"} {
				topic := sym + "@kline_" + interval
				if _, err := ws.ParseClientFrame([]byte(`{"event":"subscribe","topic":"`+topic+`","limit":1000}`), p.Served); err != ws.ErrUnknownTopic {
					t.Fatalf("%s: %v, want unknown_topic", topic, err)
				}
			}
		}
	})

	t.Run("symbol_socket_autosubscribe", func(t *testing.T) {
		reset(t, db)
		registerTokens(t, db)
		p := providers(t, db)
		for path, want := range map[string][]string{
			"/ws/btc":     {"BTC@perp_orderbook", "BTC@perp_trade", "BTC@perp_stats"},
			"/ws/pax":     {"PAX@perp_orderbook", "PAX@perp_trade", "PAX@perp_stats", "PAX@orderbook", "PAX@trade", "PAX@stats"},
			"/ws/markets": {TopicMarketsAll, TopicMarketsTotals},
			"/ws":         nil,
			"/":           nil,
		} {
			got, ok := p.SocketTopics(path)
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: %v %v, want %v", path, got, ok, want)
			}
			for _, topic := range got {
				if !p.Served(topic) {
					t.Fatalf("%s: %s not served", path, topic)
				}
				subscribe(t, p, topic)
			}
		}
		for _, path := range []string{"/ws/doge", "/ws/BTC", "/ws/", "/api/btc"} {
			if _, ok := p.SocketTopics(path); ok {
				t.Fatalf("%s served", path)
			}
		}
	})

	t.Run("explorer_socket", func(t *testing.T) {
		reset(t, db)
		for n := int64(1); n <= 30; n++ {
			block(t, db, n, 1)
		}
		for i := range 70 {
			txAction(t, db, 3, i, "Place Order", str("100"), str("1"))
		}
		p := providers(t, db)
		topics, ok := p.SocketTopics("/ws/explorer")
		if !ok || !reflect.DeepEqual(topics, []string{TopicChainBlocks, TopicChainTxs}) {
			t.Fatalf("explorer topics %v %v", topics, ok)
		}
		if n := len(decodeJSON[[]json.RawMessage](t, subscribe(t, p, topics[0]))); n != BlockSnapshot {
			t.Fatalf("blocks snapshot %d", n)
		}
		if n := len(decodeJSON[[]json.RawMessage](t, subscribe(t, p, topics[1]))); n != TxSnapshot {
			t.Fatalf("transactions snapshot %d", n)
		}
	})
}
