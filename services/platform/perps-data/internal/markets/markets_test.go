package markets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/migrations"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testRPCEnv      = "PERPS_DATA_TEST_RPC"
	seedFile        = "0005_markets_seed.sql"
)

type row struct {
	id                   uint64
	symbol               string
	name                 string
	oracleSymbol         string
	szDecimals           sql.NullInt16
	pxDecimals           sql.NullInt16
	maxLeverage          string
	maintenanceMarginBps string
	maxOpenInterest      string
	enabled              bool
	source               string
	refreshedBlock       sql.NullInt64
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestMarkets requires " + testDatabaseEnv)
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func testClient(t *testing.T) *ethclient.Client {
	t.Helper()
	url := os.Getenv(testRPCEnv)
	if url == "" {
		t.Fatal("TestMarkets requires " + testRPCEnv)
	}
	client, err := ethclient.Dial(url)
	if err != nil {
		t.Fatalf("dial %s: %v", testRPCEnv, err)
	}
	t.Cleanup(client.Close)
	return client
}

// reseed restores perps.markets to the content of the embedded seed migration
// and empties perps.fee_config.
func reseed(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, seedFile)
	if err != nil {
		t.Fatalf("read %s: %v", seedFile, err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, stmt := range []string{`DELETE FROM perps.markets`, `DELETE FROM perps.fee_config`, string(body)} {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("reseed: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("reseed commit: %v", err)
	}
}

func readRows(t *testing.T, db *sql.DB) []row {
	t.Helper()
	rows, err := db.Query(`SELECT market_id::bigint, symbol, name, oracle_symbol, sz_decimals, px_decimals, max_leverage::text, maintenance_margin_bps::text, max_open_interest::text, enabled, source, refreshed_block FROM perps.markets ORDER BY market_id`)
	if err != nil {
		t.Fatalf("read perps.markets: %v", err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.symbol, &r.name, &r.oracleSymbol, &r.szDecimals, &r.pxDecimals, &r.maxLeverage, &r.maintenanceMarginBps, &r.maxOpenInterest, &r.enabled, &r.source, &r.refreshedBlock); err != nil {
			t.Fatalf("scan perps.markets: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read perps.markets: %v", err)
	}
	return out
}

func seedRow(m Market) row {
	return row{
		id:                   m.ID,
		symbol:               m.Symbol,
		name:                 m.Name,
		oracleSymbol:         m.OracleSymbol,
		szDecimals:           sql.NullInt16{Int16: int16(m.SzDecimals), Valid: true},
		pxDecimals:           sql.NullInt16{Int16: int16(m.PxDecimals), Valid: true},
		maxLeverage:          big.NewInt(int64(m.MaxLeverage)).String(),
		maintenanceMarginBps: big.NewInt(int64(m.MaintenanceMarginBps)).String(),
		maxOpenInterest:      m.MaxOpenInterest,
		enabled:              m.Enabled,
		source:               "seed",
	}
}

func requireSeeded(t *testing.T, db *sql.DB) {
	t.Helper()
	got := readRows(t, db)
	if len(got) != len(Seed) {
		t.Fatalf("perps.markets holds %d rows, want %d", len(got), len(Seed))
	}
	for i, m := range Seed {
		if want := seedRow(m); got[i] != want {
			t.Fatalf("market %d = %+v, want %+v", i, got[i], want)
		}
	}
}

func height(t *testing.T, client *ethclient.Client) int64 {
	t.Helper()
	h, err := client.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	return int64(h)
}

func TestMarkets(t *testing.T) {
	t.Run("seed_eighteen", func(t *testing.T) {
		db := testDB(t)
		reseed(t, db)
		want := []string{"BTC", "ETH", "SOL", "AVAX", "LINK", "TSLA", "NVDA", "NAS100", "XAU", "SPX500", "GOOGL", "PAX", "SID", "HYPE", "XRP", "ASTER", "TRUMP", "BNB"}
		got := readRows(t, db)
		if len(got) != 18 {
			t.Fatalf("perps.markets holds %d rows, want 18", len(got))
		}
		symbols := make([]string, len(got))
		for i, r := range got {
			if r.id != uint64(i) {
				t.Fatalf("row %d has market_id %d", i, r.id)
			}
			if r.maxLeverage != "1000" || r.source != "seed" || r.refreshedBlock.Valid {
				t.Fatalf("market %d max_leverage %s source %s refreshed_block %v, want 1000 seed null", i, r.maxLeverage, r.source, r.refreshedBlock)
			}
			symbols[i] = r.symbol
		}
		if !slices.Equal(symbols, want) {
			t.Fatalf("symbols %v, want %v", symbols, want)
		}
		requireSeeded(t, db)
		if got[0].name != "Bitcoin" || got[0].szDecimals.Int16 != 5 || got[0].pxDecimals.Int16 != 1 || got[0].maxOpenInterest != "500000000000000000000000000" {
			t.Fatalf("BTC row %+v", got[0])
		}
		if got[11].name != "Paxeer Network" || got[11].oracleSymbol != "PAX" || got[11].szDecimals.Int16 != 0 || got[11].pxDecimals.Int16 != 5 || got[11].maxOpenInterest != "2000000000000000000000000" {
			t.Fatalf("PAX row %+v", got[11])
		}
	})

	t.Run("refresh_from_chain", func(t *testing.T) {
		db := testDB(t)
		client := testClient(t)
		reseed(t, db)
		before := height(t, client)
		if err := Refresh(context.Background(), db, client); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		after := height(t, client)
		c, _, err := latest(context.Background(), client)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		out, err := c.call(context.Background(), "totalMarkets")
		if err != nil {
			t.Fatalf("totalMarkets: %v", err)
		}
		count, err := field[*big.Int](out, 0, "totalMarkets")
		if err != nil {
			t.Fatal(err)
		}
		if count.Cmp(big.NewInt(18)) != 0 {
			t.Fatalf("totalMarkets %s, want 18", count)
		}
		got := readRows(t, db)
		if len(got) != 18 {
			t.Fatalf("perps.markets holds %d rows, want 18", len(got))
		}
		block := got[0].refreshedBlock
		if !block.Valid || block.Int64 < before || block.Int64 > after {
			t.Fatalf("refreshed_block %v, want the latest height in [%d, %d]", block, before, after)
		}
		c.at = big.NewInt(block.Int64)
		for i, r := range got {
			m, err := c.market(context.Background(), uint64(i))
			if err != nil {
				t.Fatalf("getMarket(%d): %v", i, err)
			}
			seed := seedRow(Seed[i])
			want := seed
			want.name, want.maxLeverage, want.maintenanceMarginBps, want.maxOpenInterest, want.enabled = m.name, m.maxLeverage, m.maintenanceMarginBps, m.maxOpenInterest, m.enabled
			want.source, want.refreshedBlock = "chain", block
			if r != want {
				t.Fatalf("market %d = %+v, want %+v", i, r, want)
			}
			if r.szDecimals != seed.szDecimals || r.pxDecimals != seed.pxDecimals {
				t.Fatalf("market %d decimals changed to %v/%v", i, r.szDecimals, r.pxDecimals)
			}
		}
		if btc := got[0]; btc.name != "Bitcoin" || btc.symbol != "BTC" || btc.maxLeverage != "1000" || btc.maintenanceMarginBps != "5" || btc.maxOpenInterest != "500000000000000000000000000" || !btc.enabled {
			t.Fatalf("BTC after refresh %+v, want (Bitcoin, BTC, 1000, 5, 500000000e18, true)", btc)
		}
	})

	t.Run("fee_config", func(t *testing.T) {
		db := testDB(t)
		client := testClient(t)
		reseed(t, db)
		readFees := func() (vals [4]string, block int64) {
			t.Helper()
			err := db.QueryRow(`SELECT taker_fee_ppm::text, maker_fee_ppm::text, liquidation_fee_ppm::text, insurance_share_ppm::text, refreshed_block FROM perps.fee_config WHERE id = 1`).
				Scan(&vals[0], &vals[1], &vals[2], &vals[3], &block)
			if err != nil {
				t.Fatalf("read perps.fee_config: %v", err)
			}
			return vals, block
		}
		want := [4]string{"450", "150", "0", "0"}

		before := height(t, client)
		if err := FeeConfig(context.Background(), db, client); err != nil {
			t.Fatalf("FeeConfig: %v", err)
		}
		after := height(t, client)
		vals, block := readFees()
		if vals != want || block < before || block > after {
			t.Fatalf("fee_config %v at %d, want %v in [%d, %d]", vals, block, want, before, after)
		}

		if _, err := db.Exec(`UPDATE perps.fee_config SET taker_fee_ppm = 1, maker_fee_ppm = 1, liquidation_fee_ppm = 1, insurance_share_ppm = 1, refreshed_block = 0 WHERE id = 1`); err != nil {
			t.Fatalf("overwrite fee_config: %v", err)
		}
		before = height(t, client)
		if err := Refresh(context.Background(), db, client); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		after = height(t, client)
		vals, block = readFees()
		if vals != want || block < before || block > after {
			t.Fatalf("fee_config after Refresh %v at %d, want %v in [%d, %d]", vals, block, want, before, after)
		}
		var rows int
		if err := db.QueryRow(`SELECT count(*) FROM perps.fee_config`).Scan(&rows); err != nil {
			t.Fatalf("count fee_config: %v", err)
		}
		if rows != 1 {
			t.Fatalf("perps.fee_config holds %d rows, want 1", rows)
		}
	})

	t.Run("refresh_retry", func(t *testing.T) {
		db := testDB(t)
		reseed(t, db)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().String()
		if err := ln.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		client, err := ethclient.Dial("http://" + addr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(client.Close)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type failure struct {
			err     error
			retryIn time.Duration
		}
		failures := make(chan failure, 1)
		done := make(chan error, 1)
		go func() {
			done <- Retry(ctx, db, client, func(err error, retryIn time.Duration) {
				failures <- failure{err, retryIn}
			})
		}()
		var f failure
		select {
		case f = <-failures:
		case <-time.After(30 * time.Second):
			t.Fatal("Retry reported no failure")
		}
		if f.err == nil || f.retryIn != 60*time.Second || RetryInterval != 60*time.Second {
			t.Fatalf("failure %v, retry in %s, want an error and 60s", f.err, f.retryIn)
		}
		requireSeeded(t, db)

		mux := http.NewServeMux()
		httpx.RegisterHealth(mux, httpx.HealthDeps{Postgres: db.PingContext})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("ready %d while the refresh is failing: %s", rec.Code, rec.Body)
		}
		var body struct {
			Checks map[string]string `json:"checks"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("ready body: %v", err)
		}
		names := make([]string, 0, len(body.Checks))
		for name := range body.Checks {
			names = append(names, name)
		}
		slices.Sort(names)
		if !slices.Equal(names, []string{"memory_alloc_mb", "postgres"}) {
			t.Fatalf("readiness checks %v name more than postgres", names)
		}

		select {
		case err := <-done:
			t.Fatalf("Retry returned %v before the 60s wait elapsed", err)
		default:
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Retry after cancel %v, want context.Canceled", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Retry did not stop on cancel")
		}
		requireSeeded(t, db)
	})
}
