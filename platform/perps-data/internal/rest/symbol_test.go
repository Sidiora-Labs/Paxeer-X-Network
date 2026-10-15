package rest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/migrations"
)

var (
	perpsAddr = addr(0x15, 0x10)
	spotAddr  = addr(0x21, 0x10)
	vaultAddr = addr(0x25, 0x10)
	usdlToken = addr(0x01, 0xa1)
	paxToken  = addr(0x02, 0xa2)
	sidToken  = addr(0x03, 0xa3)
	trader    = addr(0x04, 0xb1)
	maker     = addr(0x05, 0xb2)
)

var seededTables = []string{
	"fills", "mark_prices", "index_prices", "funding_rates", "funding_settlements", "open_interest", "liquidations",
	"spot_swaps", "spot_order_events", "spot_token_registry", "cursors",
}

func addr(lo, hi byte) []byte {
	b := make([]byte, 20)
	b[18], b[19] = hi, lo
	return b
}

func hash32(block int64, txIndex, logIndex int) []byte {
	return []byte(fmt.Sprintf("%016d%08d%08d", block, txIndex+1, logIndex))
}

// e18 scales a decimal string by 10^18 into a raw integer string.
func e18(t *testing.T, v string) string { return scaled(t, v, 18) }

// e6 scales a decimal string by 10^6 into a raw integer string.
func e6(t *testing.T, v string) string { return scaled(t, v, 6) }

func scaled(t *testing.T, v string, decimals int64) string {
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
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	for _, name := range files {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v", name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func reset(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := make([]string, len(seededTables))
	for i, name := range seededTables {
		tables[i] = "perps." + name
	}
	if _, err := db.Exec("TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
}

// row is one fact row: the common columns of contracts/data-services.md
// section 2 plus its argument columns.
type row struct {
	table, kind       string
	block             int64
	txIndex, logIndex int
	at                time.Time
	emitter, account  []byte
	args              []string
	values            []any
}

func insert(t *testing.T, db *sql.DB, r row) {
	t.Helper()
	cols := append([]string{"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}, r.args...)
	vals := append([]any{r.block, hash32(r.block, -1, 0), r.txIndex, r.logIndex, 0, hash32(r.block, r.txIndex, r.logIndex), "tx", r.at, r.emitter, r.kind, r.account, trader}, r.values...)
	marks := make([]string, len(cols))
	quoted := make([]string, len(cols))
	for i, c := range cols {
		marks[i] = fmt.Sprintf("$%d", i+1)
		quoted[i] = `"` + c + `"`
	}
	q := fmt.Sprintf("INSERT INTO perps.%s (%s) VALUES (%s)", r.table, strings.Join(quoted, ", "), strings.Join(marks, ", "))
	if _, err := db.Exec(q, vals...); err != nil {
		t.Fatalf("insert %s: %v", r.table, err)
	}
}

func fill(t *testing.T, db *sql.DB, market int64, block int64, logIndex int, at time.Time, takerLong bool, price, sizeUSD string) {
	t.Helper()
	insert(t, db, row{table: "fills", kind: "fill", block: block, logIndex: logIndex, at: at, emitter: perpsAddr, account: trader,
		args:   []string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long", "price", "size_usd", "maker_fee_usd", "taker_fee_usd"},
		values: []any{market, 1, 2, maker, trader, takerLong, e18(t, price), e18(t, sizeUSD), "0", "0"}})
}

func markPrice(t *testing.T, db *sql.DB, market, block int64, at time.Time, mark, index string) {
	t.Helper()
	insert(t, db, row{table: "mark_prices", kind: "mark", block: block, at: at, emitter: perpsAddr, account: perpsAddr,
		args: []string{"market_id", "mark_price", "index_price", "mid_price"}, values: []any{market, e18(t, mark), e18(t, index), e18(t, mark)}})
}

func registerTokens(t *testing.T, db *sql.DB, at time.Time) {
	t.Helper()
	for i, tok := range []struct {
		token  []byte
		stable bool
	}{{usdlToken, true}, {paxToken, false}, {sidToken, false}} {
		insert(t, db, row{table: "spot_token_registry", kind: "token_registered", block: 1, logIndex: i, at: at, emitter: vaultAddr, account: vaultAddr,
			args: []string{"token", "decimals", "is_stablecoin"}, values: []any{tok.token, 6, tok.stable}})
	}
}

func marketOrder(t *testing.T, db *sql.DB, block int64, at time.Time, token []byte, buy bool, stable, amount string) {
	t.Helper()
	insert(t, db, row{table: "spot_swaps", kind: "market_order", block: block, at: at, emitter: spotAddr, account: trader,
		args:   []string{"user", "stablecoin", "token", "stablecoin_amount", "token_amount", "is_buy"},
		values: []any{trader, usdlToken, token, e6(t, stable), e6(t, amount), buy}})
}

func get(t *testing.T, s *Symbol, path string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	mux := http.NewServeMux()
	s.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s: Content-Type %q", path, ct)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
	}
	return rec, body
}

func getOK(t *testing.T, s *Symbol, path string) map[string]json.RawMessage {
	t.Helper()
	rec, body := get(t, s, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
	}
	return body
}

func keys(body map[string]json.RawMessage) []string {
	out := make([]string, 0, len(body))
	for k := range body {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func wantKeys(t *testing.T, body map[string]json.RawMessage, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := keys(body); !slices.Equal(got, want) {
		t.Fatalf("fields %v, want %v", got, want)
	}
}

func field[T any](t *testing.T, body map[string]json.RawMessage, name string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body[name], &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func wantNumber(t *testing.T, body map[string]json.RawMessage, name string, want float64) {
	t.Helper()
	if got := field[*float64](t, body, name); got == nil || *got != want {
		t.Fatalf("%s = %s, want %v", name, body[name], want)
	}
}

func wantNull(t *testing.T, body map[string]json.RawMessage, name string) {
	t.Helper()
	if string(body[name]) != "null" {
		t.Fatalf("%s = %s, want null", name, body[name])
	}
}

type spotTradeJSON struct {
	ID        string  `json:"id"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	TradeTime int64   `json:"trade_time"`
}

func spotTrades(t *testing.T, s *Symbol, path string, symbol string) []spotTradeJSON {
	t.Helper()
	body := getOK(t, s, path)
	wantKeys(t, body, "symbol", "count", "trades")
	if got := field[string](t, body, "symbol"); got != symbol {
		t.Fatalf("symbol %q, want %q", got, symbol)
	}
	trades := field[[]spotTradeJSON](t, body, "trades")
	if trades == nil {
		t.Fatalf("%s: trades is %s, want a list", path, body["trades"])
	}
	if got := field[int](t, body, "count"); got != len(trades) {
		t.Fatalf("count %d with %d trades", got, len(trades))
	}
	return trades
}

func perpTradeCount(t *testing.T, s *Symbol, path string) int {
	t.Helper()
	body := getOK(t, s, path)
	count := field[int](t, body, "count")
	if n := len(field[[]json.RawMessage](t, body, "trades")); n != count {
		t.Fatalf("count %d with %d trades", count, n)
	}
	return count
}

func TestSymbolREST(t *testing.T) {
	db := openDB(t)
	s, err := NewSymbol(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSymbol(nil); err == nil {
		t.Fatal("NewSymbol(nil) accepted a nil database")
	}
	now := time.Now().UTC().Truncate(time.Second)

	// seedBTC writes the BTC market of the stats scenarios: a fill outside the
	// 24 hour window, three fills inside it (the last one liquidating its
	// taker), mark prices, open interest and funding.
	seedBTC := func(t *testing.T) {
		reset(t, db)
		fill(t, db, 0, 90, 0, now.Add(-25*time.Hour), true, "70000", "9999")
		fill(t, db, 0, 100, 0, now.Add(-3*time.Hour), true, "76800", "1000")
		fill(t, db, 0, 101, 0, now.Add(-2*time.Hour), true, "77310", "2000")
		fill(t, db, 0, 102, 1, now.Add(-time.Hour), false, "76590", "500")
		insert(t, db, row{table: "liquidations", kind: "liquidated", block: 102, logIndex: 2, at: now.Add(-time.Hour), emitter: perpsAddr, account: trader,
			args:   []string{"position_id", "user", "market_id", "liquidation_price", "penalty", "keeper"},
			values: []any{7, trader, 0, e18(t, "76590"), "0", maker}})
		markPrice(t, db, 0, 101, now.Add(-2*time.Hour), "77000", "76990")
		markPrice(t, db, 0, 103, now.Add(-30*time.Minute), "77182.4", "77173")
		insert(t, db, row{table: "open_interest", kind: "oi", block: 101, at: now.Add(-2 * time.Hour), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "long_oi", "short_oi", "delta_usd", "is_increase"}, values: []any{0, e18(t, "1"), e18(t, "1"), e18(t, "1"), true}})
		insert(t, db, row{table: "open_interest", kind: "oi", block: 102, at: now.Add(-time.Hour), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "long_oi", "short_oi", "delta_usd", "is_increase"}, values: []any{0, e18(t, "600000"), e18(t, "400000"), e18(t, "500"), false}})
		insert(t, db, row{table: "funding_rates", kind: "rate", block: 103, at: now.Add(-30 * time.Minute), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "new_rate_per_second", "funding_rate24h"}, values: []any{0, "3472222222", e18(t, "0.0003")}})
		insert(t, db, row{table: "funding_settlements", kind: "settled", block: 103, logIndex: 1, at: now.Add(-30 * time.Minute), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "funding_rate", "long_payment", "short_payment"}, values: []any{0, "12500000000000", e18(t, "-1.5"), e18(t, "1.5")}})
	}

	t.Run("price", func(t *testing.T) {
		seedBTC(t)
		rec, body := get(t, s, "/api/btc/price")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "max-age=1" {
			t.Fatalf("Cache-Control %q", cc)
		}
		wantKeys(t, body, "symbol", "price", "timestamp", "open", "high", "low")
		if got := field[string](t, body, "symbol"); got != "BTC" {
			t.Fatalf("symbol %q", got)
		}
		wantNumber(t, body, "price", 77173)
		if got, want := field[string](t, body, "timestamp"), now.Add(-30*time.Minute).Format("2006-01-02T15:04:05.000Z"); got != want {
			t.Fatalf("timestamp %q, want %q", got, want)
		}
		wantNumber(t, body, "open", 76800)
		wantNumber(t, body, "high", 77310)
		wantNumber(t, body, "low", 76590)

		body = getOK(t, s, "/api/eth/price")
		for _, name := range []string{"price", "timestamp", "open", "high", "low"} {
			wantNull(t, body, name)
		}
	})

	t.Run("stats", func(t *testing.T) {
		seedBTC(t)
		rec, body := get(t, s, "/api/btc/stats")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "max-age=1" {
			t.Fatalf("Cache-Control %q", cc)
		}
		wantKeys(t, body, "symbol", "volume_24h", "open_interest", "est_funding_rate", "last_funding_rate")
		wantNumber(t, body, "volume_24h", 3500)
		wantNumber(t, body, "open_interest", 1000000)
		wantNumber(t, body, "est_funding_rate", 0.0000124999999992)
		wantNumber(t, body, "last_funding_rate", 0.0000125)

		body = getOK(t, s, "/api/eth/stats")
		wantNumber(t, body, "volume_24h", 0)
		wantNumber(t, body, "open_interest", 0)
		wantNull(t, body, "est_funding_rate")
		wantNull(t, body, "last_funding_rate")
	})

	t.Run("perp_stats", func(t *testing.T) {
		seedBTC(t)
		rec, body := get(t, s, "/api/btc/perp_stats")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		wantKeys(t, body, "symbol", "volume_24h", "open_interest", "est_funding_rate", "last_funding_rate",
			"mark_price", "index_price", "basis_bps", "long_short_ratio", "liq_volume_24h")
		wantNumber(t, body, "volume_24h", 3500)
		wantNumber(t, body, "open_interest", 1000000)
		wantNumber(t, body, "mark_price", 77182.4)
		wantNumber(t, body, "index_price", 77173)
		wantNumber(t, body, "basis_bps", 1.218)
		wantNumber(t, body, "long_short_ratio", 1.5)
		wantNumber(t, body, "liq_volume_24h", 500)
	})

	t.Run("index_only_update", func(t *testing.T) {
		reset(t, db)
		aggregate := func(block int64, at time.Time, index, mark string) {
			insert(t, db, row{table: "index_prices", kind: "aggregate", block: block, at: at, emitter: addr(0x20, 0x10), account: addr(0x20, 0x10),
				args:   []string{"market_id", "index_price", "mark_price", "basis", "timestamp_ms", "sources", "required"},
				values: []any{0, e18(t, index), e18(t, mark), "0", at.UnixMilli(), 4, 3}})
		}
		aggregate(99, now.Add(-4*time.Minute), "59000", "59000")
		markPrice(t, db, 0, 100, now.Add(-3*time.Minute), "60000", "60000")
		aggregate(101, now.Add(-2*time.Minute), "60001", "60000")
		insert(t, db, row{table: "index_prices", kind: "skipped", block: 102, at: now.Add(-time.Minute), emitter: addr(0x20, 0x10), account: addr(0x20, 0x10),
			args: []string{"market_id", "sources", "required"}, values: []any{0, 2, 3}})

		body := getOK(t, s, "/api/btc/price")
		wantNumber(t, body, "price", 60001)
		if got, want := field[string](t, body, "timestamp"), now.Add(-2*time.Minute).Format("2006-01-02T15:04:05.000Z"); got != want {
			t.Fatalf("timestamp %q, want %q", got, want)
		}
		body = getOK(t, s, "/api/btc/perp_stats")
		wantNumber(t, body, "mark_price", 60000)
		wantNumber(t, body, "index_price", 60001)
		wantNumber(t, body, "basis_bps", -0.167)

		reset(t, db)
		aggregate(99, now.Add(-4*time.Minute), "59000", "59000")
		markPrice(t, db, 0, 100, now.Add(-3*time.Minute), "60000", "60000")
		body = getOK(t, s, "/api/btc/perp_stats")
		wantNumber(t, body, "index_price", 60000)
		wantNumber(t, body, "basis_bps", 0)
	})

	t.Run("trades", func(t *testing.T) {
		reset(t, db)
		registerTokens(t, db, now.Add(-48*time.Hour))
		marketOrder(t, db, 10, now.Add(-2*time.Minute), paxToken, true, "50", "100")
		marketOrder(t, db, 11, now.Add(-time.Minute), paxToken, false, "30.6", "60")
		fill(t, db, 0, 12, 0, now, true, "77000", "1540")

		trades := spotTrades(t, s, "/api/pax/trades/PAX", "PAX")
		want := []spotTradeJSON{
			{ID: "11-0", Side: "SELL", Price: 0.51, Size: 60, TradeTime: now.Add(-time.Minute).UnixMilli()},
			{ID: "10-0", Side: "BUY", Price: 0.5, Size: 100, TradeTime: now.Add(-2 * time.Minute).UnixMilli()},
		}
		if !slices.Equal(trades, want) {
			t.Fatalf("trades %+v, want %+v", trades, want)
		}
		if trades := spotTrades(t, s, "/api/btc/trades/BTC", "BTC"); len(trades) != 0 {
			t.Fatalf("BTC trades %+v, want none", trades)
		}
		rec, body := get(t, s, "/api/pax/trades/SID")
		if rec.Code != http.StatusNotFound || field[string](t, body, "error") != "Unknown symbol: SID" {
			t.Fatalf("mismatched trade symbol: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("spot_trades_pax_sid", func(t *testing.T) {
		reset(t, db)
		registerTokens(t, db, now.Add(-48*time.Hour))
		at := func(block int64) time.Time { return now.Add(time.Duration(block-100) * time.Minute) }
		insert(t, db, row{table: "spot_swaps", kind: "simple_swap", block: 20, at: at(20), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_in", "token_out", "amount_in", "amount_out", "execution_price"},
			values: []any{trader, usdlToken, sidToken, e6(t, "10"), e6(t, "40"), e18(t, "0.25")}})
		insert(t, db, row{table: "spot_order_events", kind: "limit_created", block: 21, at: at(21), emitter: addr(0x22, 0x10), account: trader,
			args: []string{"order_id", "user", "order_type", "amount", "target_price", "token", "stablecoin"}, values: []any{1, trader, 1, e6(t, "20"), e18(t, "0.3"), sidToken, usdlToken}})
		insert(t, db, row{table: "spot_order_events", kind: "stop_limit_created", block: 21, logIndex: 1, at: at(21), emitter: addr(0x23, 0x10), account: trader,
			args: []string{"order_id", "user", "order_type", "amount", "stop_price", "limit_price", "token", "stablecoin"}, values: []any{2, trader, 3, e6(t, "9"), e18(t, "0.29"), e18(t, "0.31"), sidToken, usdlToken}})
		insert(t, db, row{table: "spot_order_events", kind: "limit_executed", block: 22, at: at(22), emitter: addr(0x22, 0x10), account: trader,
			args: []string{"order_id", "execution_price", "token", "stablecoin"}, values: []any{1, e18(t, "0.3"), sidToken, usdlToken}})
		insert(t, db, row{table: "spot_order_events", kind: "stop_limit_executed", block: 23, at: at(23), emitter: addr(0x23, 0x10), account: trader,
			args: []string{"order_id", "execution_price", "token", "stablecoin"}, values: []any{2, e18(t, "0.3"), sidToken, usdlToken}})
		insert(t, db, row{table: "spot_swaps", kind: "native_swap", block: 24, at: at(24), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_other", "native_amount", "token_amount", "native_is_input"},
			values: []any{trader, usdlToken, e6(t, "10"), e6(t, "5"), true}})
		insert(t, db, row{table: "spot_swaps", kind: "simple_swap", block: 25, at: at(25), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_in", "token_out", "amount_in", "amount_out", "execution_price"},
			values: []any{trader, paxToken, usdlToken, e6(t, "8"), e6(t, "4.4"), e18(t, "1")}})
		insert(t, db, row{table: "spot_swaps", kind: "simple_swap", block: 26, at: at(26), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_in", "token_out", "amount_in", "amount_out", "execution_price"},
			values: []any{trader, sidToken, paxToken, e6(t, "4"), e6(t, "2"), e18(t, "0.5")}})
		insert(t, db, row{table: "spot_swaps", kind: "native_swap", block: 27, at: at(27), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_other", "native_amount", "token_amount", "native_is_input"},
			values: []any{trader, sidToken, e6(t, "6"), e6(t, "24"), true}})
		insert(t, db, row{table: "spot_swaps", kind: "native_swap", block: 28, at: at(28), emitter: spotAddr, account: trader,
			args:   []string{"user", "token_other", "native_amount", "token_amount", "native_is_input"},
			values: []any{trader, sidToken, e6(t, "5"), e6(t, "25"), false}})

		sid := spotTrades(t, s, "/api/sid/trades/SID", "SID")
		wantSID := []spotTradeJSON{
			{ID: "28-0", Side: "SELL", Price: 0.2, Size: 25, TradeTime: at(28).UnixMilli()},
			{ID: "27-0", Side: "BUY", Price: 0.25, Size: 24, TradeTime: at(27).UnixMilli()},
			{ID: "23-0", Side: "BUY", Price: 0.3, Size: 30, TradeTime: at(23).UnixMilli()},
			{ID: "22-0", Side: "SELL", Price: 0.3, Size: 20, TradeTime: at(22).UnixMilli()},
			{ID: "20-0", Side: "BUY", Price: 0.25, Size: 40, TradeTime: at(20).UnixMilli()},
		}
		if !slices.Equal(sid, wantSID) {
			t.Fatalf("SID trades %+v, want %+v", sid, wantSID)
		}
		pax := spotTrades(t, s, "/api/pax/trades/PAX", "PAX")
		wantPAX := []spotTradeJSON{
			{ID: "28-0", Side: "BUY", Price: 5, Size: 5, TradeTime: at(28).UnixMilli()},
			{ID: "27-0", Side: "SELL", Price: 4, Size: 6, TradeTime: at(27).UnixMilli()},
			{ID: "26-0", Side: "BUY", Price: 0.5, Size: 2, TradeTime: at(26).UnixMilli()},
			{ID: "25-0", Side: "SELL", Price: 0.55, Size: 8, TradeTime: at(25).UnixMilli()},
			{ID: "24-0", Side: "SELL", Price: 0.5, Size: 10, TradeTime: at(24).UnixMilli()},
		}
		if !slices.Equal(pax, wantPAX) {
			t.Fatalf("PAX trades %+v, want %+v", pax, wantPAX)
		}
		for _, sym := range []string{"eth", "btc", "xau"} {
			if trades := spotTrades(t, s, "/api/"+sym+"/trades/"+strings.ToUpper(sym), strings.ToUpper(sym)); len(trades) != 0 {
				t.Fatalf("%s trades %+v, want none", sym, trades)
			}
		}
		if trades := spotTrades(t, s, "/api/sid/trades", "SID"); len(trades) != 5 {
			t.Fatalf("SID trades without path symbol: %+v", trades)
		}
	})

	t.Run("perp_trades", func(t *testing.T) {
		reset(t, db)
		fill(t, db, 0, 100, 0, now.Add(-2*time.Minute), true, "77000", "1540")
		fill(t, db, 0, 101, 3, now.Add(-time.Minute), false, "77100", "3855")
		fill(t, db, 1, 101, 4, now.Add(-time.Minute), true, "3000", "300")
		insert(t, db, row{table: "liquidations", kind: "liquidated", block: 101, logIndex: 5, at: now.Add(-time.Minute), emitter: perpsAddr, account: trader,
			args:   []string{"position_id", "user", "market_id", "liquidation_price", "penalty", "keeper"},
			values: []any{7, trader, 0, e18(t, "77100"), "0", maker}})

		body := getOK(t, s, "/api/btc/perp_trades/BTC")
		wantKeys(t, body, "symbol", "count", "trades")
		if field[int](t, body, "count") != 2 {
			t.Fatalf("count %s", body["count"])
		}
		raw := field[[]map[string]json.RawMessage](t, body, "trades")
		for _, tr := range raw {
			wantKeys(t, tr, "id", "side", "price", "contracts", "notional", "liquidation", "trade_time")
		}
		type perpTradeJSON struct {
			ID          string  `json:"id"`
			Side        string  `json:"side"`
			Price       float64 `json:"price"`
			Contracts   float64 `json:"contracts"`
			Notional    float64 `json:"notional"`
			Liquidation bool    `json:"liquidation"`
			TradeTime   int64   `json:"trade_time"`
		}
		got := field[[]perpTradeJSON](t, body, "trades")
		want := []perpTradeJSON{
			{ID: "101-3", Side: "SELL", Price: 77100, Contracts: 0.05, Notional: 3855, Liquidation: true, TradeTime: now.Add(-time.Minute).UnixMilli()},
			{ID: "100-0", Side: "BUY", Price: 77000, Contracts: 0.02, Notional: 1540, Liquidation: false, TradeTime: now.Add(-2 * time.Minute).UnixMilli()},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("trades %+v, want %+v", got, want)
		}
		eth := field[[]perpTradeJSON](t, getOK(t, s, "/api/eth/perp_trades/ETH"), "trades")
		if len(eth) != 1 || eth[0].Liquidation || eth[0].Contracts != 0.1 {
			t.Fatalf("ETH trades %+v", eth)
		}
	})

	t.Run("perp_trades_liquidation_log_group", func(t *testing.T) {
		reset(t, db)
		takerFill := func(logIndex int, takerOrder int64, price, sizeUSD string) {
			insert(t, db, row{table: "fills", kind: "fill", block: 200, logIndex: logIndex, at: now.Add(-time.Minute), emitter: perpsAddr, account: trader,
				args:   []string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long", "price", "size_usd", "maker_fee_usd", "taker_fee_usd"},
				values: []any{0, 1, takerOrder, maker, trader, false, e18(t, price), e18(t, sizeUSD), "0", "0"}})
		}
		takerFill(10, 7, "60000", "6000")
		takerFill(30, 8, "60000", "54000")
		insert(t, db, row{table: "liquidations", kind: "liquidated", block: 200, logIndex: 40, at: now.Add(-time.Minute), emitter: perpsAddr, account: trader,
			args:   []string{"position_id", "user", "market_id", "liquidation_price", "penalty", "keeper"},
			values: []any{7, trader, 0, e18(t, "60000"), "0", addr(0, 0)}})

		type flag struct {
			ID          string `json:"id"`
			Liquidation bool   `json:"liquidation"`
		}
		got := field[[]flag](t, getOK(t, s, "/api/btc/perp_trades/BTC"), "trades")
		want := []flag{{ID: "200-30", Liquidation: true}, {ID: "200-10", Liquidation: false}}
		if !slices.Equal(got, want) {
			t.Fatalf("trades %+v, want %+v", got, want)
		}
		wantNumber(t, getOK(t, s, "/api/btc/perp_stats"), "liq_volume_24h", 54000)
	})

	t.Run("health", func(t *testing.T) {
		reset(t, db)
		fillAt := time.Unix(1760000000, 0).UTC()
		fill(t, db, 0, 100, 0, fillAt, true, "60000", "600")
		if _, err := db.Exec(`INSERT INTO perps.cursors (name, height, hash) VALUES ('head', 100, $1)`, hash32(100, -1, 0)); err != nil {
			t.Fatal(err)
		}
		body := getOK(t, s, "/api/btc/health")
		wantKeys(t, body, "status", "backfill", "lastFill", "symbol")
		if got := field[string](t, body, "lastFill"); got != "2025-10-09T08:53:20.000Z" {
			t.Fatalf("lastFill %q", got)
		}
		if field[string](t, body, "status") != "ok" || field[string](t, body, "backfill") != "complete" || field[string](t, body, "symbol") != "BTC" {
			t.Fatalf("health %v", body)
		}
		body = getOK(t, s, "/api/eth/health")
		wantNull(t, body, "lastFill")
		if field[string](t, body, "symbol") != "ETH" {
			t.Fatalf("symbol %s", body["symbol"])
		}
	})

	t.Run("limit_clamp", func(t *testing.T) {
		reset(t, db)
		if _, err := db.Exec(`INSERT INTO perps.fills (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account,
			market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd, taker_fee_usd)
			SELECT g, decode(lpad(to_hex(g), 64, '0'), 'hex'), 0, 0, 0, decode(lpad(to_hex(g + 100000), 64, '0'), 'hex'), 'perps_endblock', now(), $1, 'fill', $2,
				0, 1, 2, $3, $2, true, $4::numeric, $4::numeric, 0, 0
			FROM generate_series(1, 1200) AS g`, perpsAddr, trader, maker, e18(t, "60000")); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			query string
			want  int
		}{
			{"?limit=5000", 1000}, {"?limit=0", 1}, {"", 100}, {"?limit=-5", 1}, {"?limit=7", 7},
			{"?limit=1000", 1000}, {"?limit=99999999999999999999", 1000}, {"?limit=-99999999999999999999", 1},
		}
		for _, c := range cases {
			if got := perpTradeCount(t, s, "/api/btc/perp_trades/BTC"+c.query); got != c.want {
				t.Fatalf("limit %q: count %d, want %d", c.query, got, c.want)
			}
		}
		rec, body := get(t, s, "/api/btc/perp_trades/BTC?limit=ten")
		if rec.Code != http.StatusBadRequest || field[string](t, body, "error") != "invalid limit" {
			t.Fatalf("limit=ten: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown_symbol", func(t *testing.T) {
		for _, path := range []string{"/api/doge/price", "/api/doge/trades/DOGE", "/api/doge/config"} {
			rec, body := get(t, s, path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status %d", path, rec.Code)
			}
			wantKeys(t, body, "error")
			if got := field[string](t, body, "error"); got != "Unknown symbol: DOGE" {
				t.Fatalf("%s: error %q", path, got)
			}
		}
		for _, sym := range []string{"btc", "eth", "sol", "avax", "link", "hype", "xrp", "aster", "trump", "bnb", "sid", "pax", "tsla", "nvda", "googl", "xau", "nas100", "spx500"} {
			getOK(t, s, "/api/"+sym+"/health")
		}
		if rec, _ := get(t, s, "/api/BTC/price"); rec.Code != http.StatusNotFound {
			t.Fatalf("upper-case mount answered %d", rec.Code)
		}
	})

	for _, route := range []string{"config", "time", "symbols", "search", "history", "quotes", "marks", "timescale_marks"} {
		t.Run("no_udf_"+route, func(t *testing.T) {
			rec, body := get(t, s, "/api/btc/"+route+"?symbol=BTC")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("/api/btc/%s: status %d", route, rec.Code)
			}
			if got := field[string](t, body, "error"); got != "Not found" {
				t.Fatalf("/api/btc/%s: error %q", route, got)
			}
		})
	}
}
