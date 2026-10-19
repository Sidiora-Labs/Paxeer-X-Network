package rest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func venueRequest(t *testing.T, handler http.Handler, path string, status int) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != status {
		t.Fatalf("%s: status %d, want %d: %s", path, rec.Code, status, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%s: response is not JSON", path)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func venueData(t *testing.T, mux http.Handler, path string) json.RawMessage {
	t.Helper()
	body := venueRequest(t, mux, path, http.StatusOK)
	wantKeys(t, body, "success", "data")
	if !field[bool](t, body, "success") {
		t.Fatal("success is false")
	}
	return body["data"]
}

func venueMux(t *testing.T, db *sql.DB) *http.ServeMux {
	t.Helper()
	v, err := NewVenue(db)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSymbol(db)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	v.Register(mux)
	return mux
}

func venueOrder(t *testing.T, db *sql.DB, id, market int, long bool, price, size, remaining, status string) {
	t.Helper()
	accountExec(t, db, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long,
        trigger_price, size_usd, limit_price, remaining_size_usd, status, placed_block)
        VALUES ($1, $2, $3, 1, $4, 0, $5, $6, $7, $8, 1)`,
		id, trader, market, long, e18(t, size), e18(t, price), e18(t, remaining), status)
}

func venueMarketFacts(t *testing.T, db *sql.DB) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	markPrice(t, db, 0, 40, at, "70000", "69999")
	insert(t, db, row{table: "mark_prices", kind: "mark", block: 40, txIndex: 1, logIndex: 1,
		at: at.Add(-time.Second), emitter: perpsAddr, account: perpsAddr,
		args:   []string{"market_id", "mark_price", "index_price", "mid_price"},
		values: []any{0, e18(t, "77182.4"), e18(t, "77173"), e18(t, "77180")}})
	for _, oi := range []struct {
		market, block     int64
		txIndex, logIndex int
		long, short       string
	}{{0, 39, 0, 0, "5", "6"}, {0, 40, 0, 0, "10", "20"},
		{0, 40, 1, 0, "100", "200"}, {0, 40, 1, 1, "1250.5", "750.25"},
		{1, 40, 0, 2, "200", "300"}} {
		insert(t, db, row{table: "open_interest", kind: "oi", block: oi.block, txIndex: oi.txIndex,
			logIndex: oi.logIndex, at: at, emitter: perpsAddr, account: perpsAddr,
			args:   []string{"market_id", "long_oi", "short_oi", "delta_usd", "is_increase"},
			values: []any{oi.market, e18(t, oi.long), e18(t, oi.short), e18(t, "1"), true}})
	}
	for i, rate := range []struct{ perSecond, daily string }{
		{"0.000001", "0.0864"}, {"-0.00000001", "-0.000864"},
	} {
		insert(t, db, row{table: "funding_rates", kind: "rate", block: 40, txIndex: i,
			at: at, emitter: perpsAddr, account: perpsAddr,
			args:   []string{"market_id", "new_rate_per_second", "funding_rate24h"},
			values: []any{0, e18(t, rate.perSecond), e18(t, rate.daily)}})
	}
	accountExec(t, db, `INSERT INTO perps.market_stats_rolling (market_id, volume_24h, volume_7d,
        volume_30d, trades_count, open_interest_long, open_interest_short, high_24h, low_24h,
        open_24h, last_price, updated_block)
        VALUES (0, $1, $1, $1, 2, 0, 0, $2, $3, $3, $2, 40)`,
		e18(t, "2500"), e18(t, "125"), e18(t, "100"))
	fill(t, db, 0, 38, 0, at.Add(-time.Hour), true, "100", "1000")
	fill(t, db, 0, 39, 0, at, false, "125", "1500")
	fill(t, db, 0, 37, 0, at.Add(-25*time.Hour), true, "10", "10000")
	fill(t, db, 1, 39, 1, at, true, "5", "500")
}

func TestVenueREST(t *testing.T) {
	db := openDB(t)
	clean := func(t *testing.T) {
		t.Helper()
		reset(t, db)
		accountExec(t, db, `TRUNCATE perps.orders, perps.market_stats_rolling`)
	}
	readBook := func(t *testing.T, mux http.Handler, path string) map[string]json.RawMessage {
		t.Helper()
		var data map[string]json.RawMessage
		if err := json.Unmarshal(venueData(t, mux, path), &data); err != nil {
			t.Fatal(err)
		}
		wantKeys(t, data, "asks", "bids")
		return data
	}

	t.Run("orderbook_shape", func(t *testing.T) {
		clean(t)
		venueOrder(t, db, 1, 0, true, "100", "5", "1.25", "active")
		venueOrder(t, db, 2, 0, true, "100", "0.75", "0.75", "active")
		venueOrder(t, db, 3, 0, false, "101", "4", "3", "active")
		for i, status := range []string{"cancelled", "expired", "filled", "triggered"} {
			venueOrder(t, db, 4+i, 0, false, "99", "100", "100", status)
		}
		venueOrder(t, db, 8, 0, true, "0", "100", "100", "active")
		venueOrder(t, db, 9, 0, true, "100", "1", "0", "active")
		venueOrder(t, db, 10, 1, true, "100", "100", "100", "active")
		data := readBook(t, venueMux(t, db), "/api/orderbook/BTC?depth=33")
		for _, side := range []struct {
			name            string
			price, quantity float64
		}{{"asks", 101, 3}, {"bids", 100, 2}} {
			levels := field[[]map[string]json.RawMessage](t, data, side.name)
			if len(levels) != 1 {
				t.Fatalf("%s: %s", side.name, data[side.name])
			}
			wantKeys(t, levels[0], "price", "quantity")
			wantNumber(t, levels[0], "price", side.price)
			wantNumber(t, levels[0], "quantity", side.quantity)
		}
	})

	t.Run("orderbook_depth_clamp", func(t *testing.T) {
		clean(t)
		accountExec(t, db, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long,
            trigger_price, size_usd, limit_price, remaining_size_usd, status, placed_block)
            SELECT n + side * 150, $1, 0, 1, side = 0, 0, 2 * $2::numeric,
                (CASE side WHEN 0 THEN 1000 - n ELSE 1000 + n END) * $2::numeric,
                2 * $2::numeric, 'active', 1
            FROM generate_series(1, 150) n CROSS JOIN generate_series(0, 1) side`, trader, usdScale)
		mux := venueMux(t, db)
		for _, tc := range []struct {
			query string
			count int
		}{
			{"", 20}, {"?depth=", 20}, {"?depth=500", 100}, {"?depth=100", 100},
			{"?depth=33", 33}, {"?depth=0", 1}, {"?depth=-5", 1}, {"?depth=1", 1},
			{"?depth=999999999999999999999999", 100}, {"?depth=-999999999999999999999999", 1},
		} {
			t.Run(tc.query, func(t *testing.T) {
				data := readBook(t, mux, "/api/orderbook/BTC"+tc.query)
				for _, side := range []string{"asks", "bids"} {
					levels := field[[]map[string]json.RawMessage](t, data, side)
					if len(levels) != tc.count {
						t.Fatalf("%s: %d levels, want %d", side, len(levels), tc.count)
					}
					for i, level := range levels {
						price := 1001 + i
						if side == "bids" {
							price = 999 - i
						}
						wantNumber(t, level, "price", float64(price))
						wantNumber(t, level, "quantity", 2)
					}
				}
			})
		}
		for _, value := range []string{"nope", "2.5"} {
			body := venueRequest(t, mux, "/api/orderbook/BTC?depth="+value, http.StatusBadRequest)
			wantKeys(t, body, "success", "error")
			if field[bool](t, body, "success") || field[string](t, body, "error") != "invalid depth" {
				t.Fatalf("invalid depth: %v", body)
			}
		}
	})

	t.Run("empty_book", func(t *testing.T) {
		clean(t)
		data := readBook(t, venueMux(t, db), "/api/orderbook/BTC")
		for _, side := range []string{"asks", "bids"} {
			if string(data[side]) != "[]" {
				t.Fatalf("%s: %s, want []", side, data[side])
			}
		}
	})

	t.Run("open_interest", func(t *testing.T) {
		clean(t)
		venueMarketFacts(t, db)
		mux := venueMux(t, db)
		for _, path := range []string{"/api/open-interest", "/api/markets/open-interest"} {
			var entries []map[string]json.RawMessage
			if err := json.Unmarshal(venueData(t, mux, path), &entries); err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(marketIDs) {
				t.Fatalf("%d markets, want %d", len(entries), len(marketIDs))
			}
			seen := map[string]bool{}
			for i, entry := range entries {
				wantKeys(t, entry, "paxeerSymbol", "long_oi", "short_oi")
				symbol := field[string](t, entry, "paxeerSymbol")
				id, ok := marketIDs[symbol]
				if !ok || id != int64(i) || seen[symbol] {
					t.Fatalf("unexpected market %q at %d", symbol, i)
				}
				seen[symbol] = true
				long, short := 0.0, 0.0
				switch symbol {
				case "BTC":
					long, short = 1250.5, 750.25
				case "ETH":
					long, short = 200, 300
				}
				wantNumber(t, entry, "long_oi", long)
				wantNumber(t, entry, "short_oi", short)
			}
		}
	})

	t.Run("ticker", func(t *testing.T) {
		clean(t)
		venueMarketFacts(t, db)
		mux := venueMux(t, db)
		for _, path := range []string{"/api/ticker/BTC", "/api/markets/ticker/BTC", "/api/ticker/btc"} {
			var entry map[string]json.RawMessage
			if err := json.Unmarshal(venueData(t, mux, path), &entry); err != nil {
				t.Fatal(err)
			}
			wantKeys(t, entry, "symbol", "index_price", "mark_price", "est_funding_rate", "last_funding_rate", "open_interest", "volume_24h")
			if field[string](t, entry, "symbol") != "BTC" {
				t.Fatalf("symbol: %s", entry["symbol"])
			}
			for name, want := range map[string]float64{"index_price": 77173, "mark_price": 77182.4,
				"est_funding_rate": -0.000864, "last_funding_rate": -0.000036,
				"open_interest": 2000.75, "volume_24h": 2500} {
				wantNumber(t, entry, name, want)
			}
		}
	})

	t.Run("tickers", func(t *testing.T) {
		clean(t)
		venueMarketFacts(t, db)
		for _, id := range marketIDs {
			if id != 0 {
				markPrice(t, db, id, id, time.Now().UTC(), "50.5", "50")
			}
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(venueData(t, venueMux(t, db), "/api/markets/tickers"), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(marketIDs) {
			t.Fatalf("%d markets, want %d", len(entries), len(marketIDs))
		}
		seen := map[string]bool{}
		for i, entry := range entries {
			wantKeys(t, entry, "symbol", "paxeerSymbol", "mark_price", "index_price", "open_interest", "est_funding_rate",
				"last_funding_rate", "24h_open", "24h_close", "24h_high", "24h_low", "24h_amount", "24h_volume")
			symbol := field[string](t, entry, "paxeerSymbol")
			id, ok := marketIDs[symbol]
			if !ok || id != int64(i) || seen[symbol] {
				t.Fatalf("unexpected market %q at %d", symbol, i)
			}
			seen[symbol] = true
			if field[string](t, entry, "symbol") != "PERP_"+symbol+"_USDC" {
				t.Fatalf("symbol: %s", entry["symbol"])
			}
			want := map[string]float64{"mark_price": 50.5, "index_price": 50, "open_interest": 0,
				"est_funding_rate": 0, "last_funding_rate": 0, "24h_open": 0, "24h_close": 0,
				"24h_high": 0, "24h_low": 0, "24h_amount": 0, "24h_volume": 0}
			if symbol == "BTC" {
				want = map[string]float64{"mark_price": 77182.4, "index_price": 77173, "open_interest": 2000.75,
					"est_funding_rate": -0.000864, "last_funding_rate": -0.000036, "24h_open": 100,
					"24h_close": 125, "24h_high": 125, "24h_low": 100, "24h_amount": 22, "24h_volume": 2500}
			} else if symbol == "ETH" {
				want["open_interest"], want["24h_amount"] = 500, 100
			}
			for name, value := range want {
				wantNumber(t, entry, name, value)
			}
		}
	})

	t.Run("dual_mount", func(t *testing.T) {
		clean(t)
		venueMarketFacts(t, db)
		venueOrder(t, db, 1, 0, true, "100", "2", "2", "active")
		mux := venueMux(t, db)
		for _, route := range []string{"/orderbook/BTC?depth=1", "/open-interest", "/ticker/BTC", "/tickers"} {
			a := venueRequest(t, mux, "/api"+route, http.StatusOK)
			b := venueRequest(t, mux, "/api/markets"+route, http.StatusOK)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s: bodies differ: %v vs %v", route, a, b)
			}
		}
		venueRequest(t, mux, "/api/btc/price", http.StatusOK)
	})

	t.Run("unknown_symbol", func(t *testing.T) {
		clean(t)
		mux := venueMux(t, db)
		for _, prefix := range []string{"/api", "/api/markets"} {
			for _, route := range []string{"/orderbook/DOGE", "/ticker/DOGE"} {
				body := venueRequest(t, mux, prefix+route, http.StatusNotFound)
				wantKeys(t, body, "success", "error")
				if field[bool](t, body, "success") || field[string](t, body, "error") != "Unknown symbol: DOGE" {
					t.Fatalf("unknown symbol: %v", body)
				}
			}
		}
	})

	t.Run("database_failure", func(t *testing.T) {
		clean(t)
		venueMarketFacts(t, db)
		mux := venueMux(t, db)
		for _, tc := range []struct{ table, route string }{
			{"orders", "/orderbook/BTC"}, {"open_interest", "/open-interest"},
			{"mark_prices", "/ticker/BTC"}, {"funding_rates", "/tickers"},
		} {
			t.Run(tc.table, func(t *testing.T) {
				accountExec(t, db, fmt.Sprintf("ALTER TABLE perps.%s RENAME TO venue_unavailable", tc.table))
				t.Cleanup(func() {
					accountExec(t, db, fmt.Sprintf("ALTER TABLE perps.venue_unavailable RENAME TO %s", tc.table))
				})
				for _, prefix := range []string{"/api", "/api/markets"} {
					body := venueRequest(t, mux, prefix+tc.route, http.StatusServiceUnavailable)
					wantKeys(t, body, "success")
					if field[bool](t, body, "success") {
						t.Fatal("database failure returned success")
					}
				}
			})
		}
	})

	t.Run("nil_database", func(t *testing.T) {
		if v, err := NewVenue(nil); err == nil || v != nil {
			t.Fatal("nil database accepted")
		}
	})
}
