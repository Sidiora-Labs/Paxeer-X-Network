package rest

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var recordFields = []string{
	"symbol", "symbol_full", "price", "change_24h", "volume_24h", "open_interest", "funding_8h", "last_funding_rate_hourly",
	"volume_7d", "volume_30d", "trades_count", "last_updated",
	"perp_mark_price", "perp_index_price", "perp_basis_bps", "perp_funding_rate", "perp_next_funding_ms",
	"perp_long_short_ratio", "perp_liq_volume_24h", "perp_oi_contracts", "perp_oi_usd",
	"perp_volume_24h_contracts", "perp_volume_24h_usd",
}

var seedSymbols = []string{
	"BTC", "ETH", "SOL", "AVAX", "LINK", "TSLA", "NVDA", "NAS100", "XAU",
	"SPX500", "GOOGL", "PAX", "SID", "HYPE", "XRP", "ASTER", "TRUMP", "BNB",
}

func rollingStats(t *testing.T, db *sql.DB, market int64, vol24, vol7, vol30 string, trades int64, longOI, shortOI, open24 string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO perps.market_stats_rolling (market_id, volume_24h, volume_7d, volume_30d, trades_count,
		open_interest_long, open_interest_short, open_24h, updated_block) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1)`,
		market, e18(t, vol24), e18(t, vol7), e18(t, vol30), trades, e18(t, longOI), e18(t, shortOI), e18(t, open24)); err != nil {
		t.Fatal(err)
	}
}

func serve(t *testing.T, mux *http.ServeMux, path string) (int, map[string]json.RawMessage) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s: Content-Type %q", path, ct)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
	}
	return rec.Code, body
}

func serveOK(t *testing.T, mux *http.ServeMux, path string) map[string]json.RawMessage {
	t.Helper()
	code, body := serve(t, mux, path)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", path, code)
	}
	return body
}

func TestMarketsREST(t *testing.T) {
	db := openDB(t)
	if _, err := NewMarkets(nil, nil); err == nil {
		t.Fatal("NewMarkets(nil) accepted a nil database")
	}
	now := time.Now().UTC().Truncate(time.Second)
	offset := time.Duration(0)
	m, err := NewMarkets(db, func() time.Time { return now.Add(offset) })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	m.Register(mux)

	// seed writes BTC with fills, a liquidation, mark prices, funding and its
	// rolling stats, and ETH with rolling stats only.
	seed := func(t *testing.T) {
		reset(t, db)
		if _, err := db.Exec(`TRUNCATE perps.market_stats_rolling, perps.positions_current`); err != nil {
			t.Fatal(err)
		}
		fill(t, db, 0, 90, 0, now.Add(-25*time.Hour), true, "70000", "9999")
		fill(t, db, 0, 100, 0, now.Add(-3*time.Hour), true, "80000", "1000")
		fill(t, db, 0, 101, 0, now.Add(-2*time.Hour), true, "50000", "2000")
		fill(t, db, 0, 102, 1, now.Add(-time.Hour), false, "62500", "500")
		insert(t, db, row{table: "liquidations", kind: "liquidated", block: 102, logIndex: 2, at: now.Add(-time.Hour), emitter: perpsAddr, account: trader,
			args:   []string{"position_id", "user", "market_id", "liquidation_price", "penalty", "keeper"},
			values: []any{7, trader, 0, e18(t, "62500"), "0", maker}})
		markPrice(t, db, 0, 101, now.Add(-2*time.Hour), "77000", "76990")
		markPrice(t, db, 0, 103, now.Add(-30*time.Minute), "77182.4", "77173")
		insert(t, db, row{table: "funding_rates", kind: "rate", block: 101, at: now.Add(-2 * time.Hour), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "new_rate_per_second", "funding_rate24h"}, values: []any{0, "1000000000", e18(t, "0.0001")}})
		insert(t, db, row{table: "funding_rates", kind: "rate", block: 103, at: now.Add(-30 * time.Minute), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "new_rate_per_second", "funding_rate24h"}, values: []any{0, "3472222222", e18(t, "0.0003")}})
		insert(t, db, row{table: "funding_settlements", kind: "settled", block: 101, logIndex: 1, at: now.Add(-2 * time.Hour), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "funding_rate", "long_payment", "short_payment"}, values: []any{0, "9000000000000", e18(t, "-1"), e18(t, "1")}})
		insert(t, db, row{table: "funding_settlements", kind: "settled", block: 103, logIndex: 1, at: now.Add(-30 * time.Minute), emitter: perpsAddr, account: perpsAddr,
			args: []string{"market_id", "funding_rate", "long_payment", "short_payment"}, values: []any{0, "12345678901234", e18(t, "-1.5"), e18(t, "1.5")}})
		rollingStats(t, db, 0, "100", "700", "3000", 4, "600000", "400000", "80000")
		rollingStats(t, db, 1, "300", "900", "5000", 6, "50", "0", "2000")
		if _, err := db.Exec(`UPDATE perps.market_stats_rolling SET last_price = $1 WHERE market_id = 0`, e18(t, "62500")); err != nil {
			t.Fatal(err)
		}
		// BTC open positions: 600000 USD long at 50000 and 400000 USD short at
		// 80000, 12 + 5 = 17 contracts; the closed one is not counted.
		for _, pos := range []struct {
			id          int
			long        bool
			size, entry string
			status      string
		}{{1, true, "600000", "50000", "open"}, {2, false, "400000", "80000", "open"}, {3, true, "999999", "10000", "closed"}} {
			if _, err := db.Exec(`INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd,
				collateral_usd, collateral_amount, collateral_token, entry_price, leverage, realized_pnl, status, opened_block)
				VALUES ($1, $2, 0, $3, $4, 1, 1, $2, $5, 1, 0, $6, 1)`,
				pos.id, trader, pos.long, e18(t, pos.size), e18(t, pos.entry), pos.status); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("markets_all", func(t *testing.T) {
		seed(t)
		body := serveOK(t, mux, "/api/markets")
		wantKeys(t, body, "ts", "markets")
		if got := field[int64](t, body, "ts"); got != now.UnixMilli() {
			t.Fatalf("ts %d, want %d", got, now.UnixMilli())
		}
		records := field[[]map[string]json.RawMessage](t, body, "markets")
		if len(records) != 18 {
			t.Fatalf("%d records, want 18", len(records))
		}
		symbols := make([]string, len(records))
		for i, rec := range records {
			wantKeys(t, rec, slices.Clone(recordFields)...)
			symbols[i] = field[string](t, rec, "symbol")
		}
		if !slices.Equal(symbols, seedSymbols) {
			t.Fatalf("symbols %v, want %v", symbols, seedSymbols)
		}
		if got := field[string](t, records[0], "symbol_full"); got != "Bitcoin" {
			t.Fatalf("BTC symbol_full %q", got)
		}
		eth := records[1]
		wantNumber(t, eth, "volume_24h", 300)
		wantNumber(t, eth, "volume_7d", 900)
		wantNumber(t, eth, "volume_30d", 5000)
		wantNumber(t, eth, "open_interest", 50)
		if got := field[int64](t, eth, "trades_count"); got != 6 {
			t.Fatalf("ETH trades_count %d", got)
		}
		for _, name := range []string{"price", "change_24h", "funding_8h", "last_funding_rate_hourly", "last_updated", "perp_mark_price",
			"perp_index_price", "perp_basis_bps", "perp_funding_rate", "perp_long_short_ratio"} {
			wantNull(t, eth, name)
		}
		wantNumber(t, eth, "perp_oi_contracts", 0)
		sol := records[2]
		for _, name := range []string{"volume_24h", "open_interest", "perp_oi_usd", "perp_volume_24h_usd", "perp_liq_volume_24h"} {
			wantNumber(t, sol, name, 0)
		}
		if got := field[int64](t, sol, "trades_count"); got != 0 {
			t.Fatalf("SOL trades_count %d", got)
		}
	})

	t.Run("market_by_symbol", func(t *testing.T) {
		seed(t)
		for _, path := range []string{"/api/markets/btc", "/api/markets/BTC", "/api/markets/Btc"} {
			body := serveOK(t, mux, path)
			wantKeys(t, body, "ts", "market")
			rec := field[map[string]json.RawMessage](t, body, "market")
			wantKeys(t, rec, slices.Clone(recordFields)...)
			if got := field[string](t, rec, "symbol"); got != "BTC" {
				t.Fatalf("%s: symbol %q", path, got)
			}
			wantNumber(t, rec, "volume_24h", 100)
		}
		rec := field[map[string]json.RawMessage](t, serveOK(t, mux, "/api/markets/nas100"), "market")
		if got := field[string](t, rec, "symbol"); got != "NAS100" {
			t.Fatalf("symbol %q, want NAS100", got)
		}
	})

	t.Run("unknown_symbol_404", func(t *testing.T) {
		seed(t)
		for path, want := range map[string]string{"/api/markets/DOGE": "Unknown symbol: DOGE", "/api/markets/doge": "Unknown symbol: DOGE"} {
			code, body := serve(t, mux, path)
			if code != http.StatusNotFound {
				t.Fatalf("%s: status %d, want 404", path, code)
			}
			wantKeys(t, body, "error")
			if got := field[string](t, body, "error"); got != want {
				t.Fatalf("%s: error %q, want %q", path, got, want)
			}
		}
	})

	t.Run("totals", func(t *testing.T) {
		seed(t)
		body := serveOK(t, mux, "/api/totals")
		wantKeys(t, body, "ts", "totals")
		totals := field[map[string]json.RawMessage](t, body, "totals")
		wantKeys(t, totals, "total_trades", "volume_24h", "volume_7d", "volume_30d", "total_oi")
		if got := field[int64](t, totals, "total_trades"); got != 10 {
			t.Fatalf("total_trades %d, want 10", got)
		}
		wantNumber(t, totals, "volume_24h", 400)
		wantNumber(t, totals, "volume_7d", 1600)
		wantNumber(t, totals, "volume_30d", 8000)
		wantNumber(t, totals, "total_oi", 1000050)

		if _, err := db.Exec(`TRUNCATE perps.market_stats_rolling`); err != nil {
			t.Fatal(err)
		}
		got, err := m.Totals(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got != (Totals{}) {
			t.Fatalf("empty totals %+v, want zero", got)
		}
	})

	t.Run("health", func(t *testing.T) {
		seed(t)
		offset = 12*time.Second + 400*time.Millisecond
		defer func() { offset = 0 }()
		body := serveOK(t, mux, "/api/markets/health")
		wantKeys(t, body, "ok", "service", "symbols", "uptimeSec")
		if !field[bool](t, body, "ok") {
			t.Fatal("ok is false")
		}
		if got := field[string](t, body, "service"); got != "perps-gateway" {
			t.Fatalf("service %q", got)
		}
		if got := field[int64](t, body, "symbols"); got != 18 {
			t.Fatalf("symbols %d, want 18", got)
		}
		if got := field[int64](t, body, "uptimeSec"); got != 12 {
			t.Fatalf("uptimeSec %d, want 12", got)
		}
	})

	t.Run("derived_fields", func(t *testing.T) {
		seed(t)
		rec := field[map[string]json.RawMessage](t, serveOK(t, mux, "/api/markets/btc"), "market")
		wantNumber(t, rec, "perp_basis_bps", 1.218)
		wantNumber(t, rec, "perp_mark_price", 77182.4)
		wantNumber(t, rec, "perp_index_price", 77173)
		wantNumber(t, rec, "price", 77173)
		// change_24h compares the last fill (62500) with the window's first
		// fill (80000), not the index price (77173).
		wantNumber(t, rec, "change_24h", -0.21875)
		// last_funding_rate_hourly is the latest FundingSettled (block 103),
		// exact, not the FundingRateUpdated estimate.
		if got := field[string](t, rec, "last_funding_rate_hourly"); got != "0.000012345678901234" {
			t.Fatalf("last_funding_rate_hourly %q, want 0.000012345678901234", got)
		}
		wantNumber(t, rec, "funding_8h", 0.0000999999999936)
		wantNumber(t, rec, "perp_funding_rate", 0.0000999999999936)
		if got, want := field[int64](t, rec, "perp_next_funding_ms"), now.Truncate(time.Hour).Add(time.Hour).UnixMilli(); got != want {
			t.Fatalf("perp_next_funding_ms %d, want %d", got, want)
		}
		if got, want := field[int64](t, rec, "last_updated"), now.Add(-30*time.Minute).UnixMilli(); got != want {
			t.Fatalf("last_updated %d, want %d", got, want)
		}
		wantNumber(t, rec, "open_interest", 1000000)
		wantNumber(t, rec, "perp_oi_usd", 1000000)
		wantNumber(t, rec, "perp_oi_contracts", 17)
		wantNumber(t, rec, "perp_long_short_ratio", 1.5)
		wantNumber(t, rec, "perp_volume_24h_usd", 3500)
		wantNumber(t, rec, "perp_volume_24h_contracts", 0.0605)
		wantNumber(t, rec, "perp_liq_volume_24h", 500)
		wantNumber(t, rec, "volume_24h", 100)
		wantNumber(t, rec, "volume_7d", 700)
		wantNumber(t, rec, "volume_30d", 3000)
		if got := field[int64](t, rec, "trades_count"); got != 4 {
			t.Fatalf("trades_count %d, want 4", got)
		}

		offset = 30 * time.Minute
		defer func() { offset = 0 }()
		next, ok, err := m.Record(t.Context(), "btc")
		if err != nil || !ok {
			t.Fatalf("Record(btc) = %v, %v", ok, err)
		}
		if want := now.Add(offset).Truncate(time.Hour).Add(time.Hour).UnixMilli(); next.PerpNextFundingMs != want {
			t.Fatalf("perp_next_funding_ms %d, want %d", next.PerpNextFundingMs, want)
		}
	})

	t.Run("fixed_routes_first", func(t *testing.T) {
		seed(t)
		symbol, err := NewSymbol(db)
		if err != nil {
			t.Fatal(err)
		}
		gateway := http.NewServeMux()
		symbol.Register(gateway)
		m.Register(gateway)

		wantKeys(t, serveOK(t, gateway, "/api/markets/health"), "ok", "service", "symbols", "uptimeSec")
		wantKeys(t, serveOK(t, gateway, "/api/markets"), "ts", "markets")
		wantKeys(t, serveOK(t, gateway, "/api/totals"), "ts", "totals")
		rec := field[map[string]json.RawMessage](t, serveOK(t, gateway, "/api/markets/eth"), "market")
		if got := field[string](t, rec, "symbol"); got != "ETH" {
			t.Fatalf("symbol %q, want ETH", got)
		}
		wantKeys(t, serveOK(t, gateway, "/api/btc/stats"), "symbol", "volume_24h", "open_interest", "last_funding_rate_hourly", "last_funding_rate")
		for pattern, path := range map[string]string{
			"GET /api/markets/health":      "/api/markets/health",
			"GET /api/markets/{symbol}":    "/api/markets/btc",
			"GET /api/{symbol}/{route...}": "/api/btc/health",
		} {
			if _, got := gateway.Handler(httptest.NewRequest(http.MethodGet, path, nil)); got != pattern {
				t.Fatalf("%s routed to %q, want %q", path, got, pattern)
			}
		}
	})
}

// TestNoEstimatedFundingEmitter fails while any file of the service source
// tree, testdata excepted, still names the removed estimated funding field.
func TestNoEstimatedFundingEmitter(t *testing.T) {
	needle := []byte("est_" + "funding_rate")
	root := filepath.Join("..", "..")
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, needle) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("%s still named in %v", needle, found)
	}
}
