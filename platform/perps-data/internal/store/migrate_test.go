package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/migrations"
)

const testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"

var commonColumns = []string{"row_id", "block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}

var factTables = []string{
	"vaults", "collateral_events", "order_events", "fills", "position_events", "trade_settlements", "fees",
	"mark_prices", "funding_rates", "funding_settlements", "funding_payments", "open_interest", "liquidations",
	"backstop_liquidations", "insurance_events", "adl_events", "feed_keys", "index_prices", "spot_swaps",
	"spot_fee_events", "spot_order_events", "spot_vault_events", "spot_token_registry", "spot_admin",
	"spot_share_transfers", "usdl_transfers", "raw_logs", "protocol_events", "tx_actions",
}

type column struct {
	name string
	typ  string
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestMigrate requires " + testDatabaseEnv)
	}
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return db
}

func migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func embeddedFiles(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	return names
}

func embeddedSum(t *testing.T, file string) []byte {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	sum := sha256.Sum256(body)
	return sum[:]
}

type record struct {
	file      string
	sum       []byte
	appliedAt string
}

func records(t *testing.T, db *sql.DB) []record {
	t.Helper()
	rows, err := db.Query(`SELECT file, sha256, applied_at::text FROM perps.schema_migrations ORDER BY file`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var out []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.file, &r.sum, &r.appliedAt); err != nil {
			t.Fatalf("scan schema_migrations: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return out
}

func queryStrings(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

func columns(t *testing.T, db *sql.DB, table string) []column {
	t.Helper()
	rows, err := db.Query(`SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'perps' AND table_name = $1 ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	defer rows.Close()
	var out []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ); err != nil {
			t.Fatalf("scan columns of %s: %v", table, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	return out
}

func columnNames(cols []column) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	return names
}

const indexColumnsSQL = `SELECT string_agg(a.attname, ',' ORDER BY k.ord)
FROM pg_index i
CROSS JOIN LATERAL unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
WHERE i.indrelid = $1::regclass AND %s
GROUP BY i.indexrelid
ORDER BY 1`

func primaryKey(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	keys := queryStrings(t, db, fmt.Sprintf(indexColumnsSQL, "i.indisprimary"), "perps."+table)
	if len(keys) != 1 {
		t.Fatalf("perps.%s primary keys %v, want exactly one", table, keys)
	}
	return keys[0]
}

func uniqueKeys(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	return queryStrings(t, db, fmt.Sprintf(indexColumnsSQL, "i.indisunique AND NOT i.indisprimary"), "perps."+table)
}

func indexedColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	return queryStrings(t, db, fmt.Sprintf(indexColumnsSQL, "NOT i.indisprimary"), "perps."+table)
}

func tablesInPerps(t *testing.T, db *sql.DB, tableType string) []string {
	t.Helper()
	return queryStrings(t, db, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'perps' AND table_type = $1 ORDER BY table_name`, tableType)
}

// execRolledBack runs one statement in its own transaction and rolls it back,
// so a refused insert leaves no aborted transaction and an accepted one leaves
// no row behind.
func execRolledBack(t *testing.T, db *sql.DB, query string, args ...any) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			t.Errorf("rollback: %v", err)
		}
	}()
	_, err = tx.Exec(query, args...)
	return err
}

func wantCheckViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("got %v, want check violation of %s", err, constraint)
	}
	if pqErr.Code != "23514" || pqErr.Constraint != constraint {
		t.Fatalf("got code %s constraint %q (%s), want 23514 %q", pqErr.Code, pqErr.Constraint, pqErr.Message, constraint)
	}
}

const fillInsert = `INSERT INTO perps.fills (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account, tx_from,
    market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd, taker_fee_usd)
VALUES (7, decode(repeat('11', 32), 'hex'), 0, 0, 0, decode(repeat('22', 32), 'hex'), $1, now(), decode(repeat('33', 20), 'hex'), 'fill',
    decode(repeat('44', 20), 'hex'), $2, 1, 2, 3, decode(repeat('55', 20), 'hex'), decode(repeat('66', 20), 'hex'), true, 100, 10, -1, 1)`

const settlementInsert = `INSERT INTO perps.trade_settlements (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account, tx_from,
    position_id, "user", market_id, trade_type, size_usd, execution_price, gross_pnl, total_fees_usd, funding_paid_usd, net_payout_tokens)
VALUES (7, decode(repeat('11', 32), 'hex'), 0, 0, 0, decode(repeat('22', 32), 'hex'), 'tx', now(), decode(repeat('33', 20), 'hex'), 'settled',
    decode(repeat('44', 20), 'hex'), decode(repeat('44', 20), 'hex'), 9, decode(repeat('44', 20), 'hex'), 1, 0, $1::numeric, 100, -5, 1, 0, 3)`

func TestMigrate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("applies_all", func(t *testing.T) {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
			t.Fatalf("drop schema perps: %v", err)
		}
		migrate(t, db)
		files := embeddedFiles(t)
		if len(files) < 3 || files[0] != "0001_facts.sql" || files[1] != "0002_projections.sql" || files[2] != "0003_delivery.sql" {
			t.Fatalf("embedded files %v do not start with 0001_facts.sql, 0002_projections.sql, 0003_delivery.sql", files)
		}
		got := records(t, db)
		if len(got) != len(files) {
			t.Fatalf("recorded %d migrations, want %d (%v)", len(got), len(files), files)
		}
		for i, file := range files {
			if want := fmt.Sprintf("%04d_", i+1); !strings.HasPrefix(file, want) {
				t.Fatalf("embedded file %d is %s, want prefix %s", i, file, want)
			}
			if got[i].file != file {
				t.Fatalf("record %d is %s, want %s", i, got[i].file, file)
			}
			if want := embeddedSum(t, file); string(got[i].sum) != string(want) {
				t.Fatalf("record %s sha256 %x, want %x", file, got[i].sum, want)
			}
		}
		if got := columns(t, db, "schema_migrations"); !slices.Equal(got, []column{{"file", "text"}, {"sha256", "bytea"}, {"applied_at", "timestamp with time zone"}}) {
			t.Fatalf("schema_migrations columns %v", got)
		}
		if got := primaryKey(t, db, "schema_migrations"); got != "file" {
			t.Fatalf("schema_migrations primary key %s, want file", got)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		migrate(t, db)
		before := records(t, db)
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("second Migrate: %v, want nil", err)
		}
		if after := records(t, db); !slices.EqualFunc(before, after, func(a, b record) bool {
			return a.file == b.file && string(a.sum) == string(b.sum) && a.appliedAt == b.appliedAt
		}) {
			t.Fatalf("second Migrate changed schema_migrations: before %v after %v", before, after)
		}
	})

	t.Run("refuses_changed_hash", func(t *testing.T) {
		migrate(t, db)
		files := embeddedFiles(t)
		last := files[len(files)-1]
		t.Cleanup(func() {
			if _, err := db.Exec(`UPDATE perps.schema_migrations SET sha256 = $1 WHERE file = '0001_facts.sql'`, embeddedSum(t, "0001_facts.sql")); err != nil {
				t.Errorf("restore 0001_facts.sql hash: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO perps.schema_migrations (file, sha256) VALUES ($1, $2) ON CONFLICT (file) DO NOTHING`, last, embeddedSum(t, last)); err != nil {
				t.Errorf("restore %s record: %v", last, err)
			}
		})
		if _, err := db.Exec(`UPDATE perps.schema_migrations SET sha256 = '\x00' WHERE file = '0001_facts.sql'`); err != nil {
			t.Fatalf("tamper 0001_facts.sql hash: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM perps.schema_migrations WHERE file = $1`, last); err != nil {
			t.Fatalf("forget %s: %v", last, err)
		}
		err := Migrate(ctx, db)
		if err == nil || err.Error() != "migration 0001_facts.sql changed" || !errors.Is(err, ErrMigrationChanged) {
			t.Fatalf("Migrate: %v, want migration 0001_facts.sql changed", err)
		}
		for _, r := range records(t, db) {
			if r.file == last {
				t.Fatalf("Migrate applied %s after refusing a changed hash", last)
			}
		}
	})

	t.Run("tables_and_indexes", func(t *testing.T) {
		migrate(t, db)
		tables := tablesInPerps(t, db, "BASE TABLE")
		for _, table := range append(slices.Clone(factTables), "blocks", "cursors", "writer_lease", "markets", "fee_config", "delivery_outbox", "delivery_inbox", "delivery_quarantine", "schema_migrations") {
			if !slices.Contains(tables, table) {
				t.Fatalf("perps.%s missing from %v", table, tables)
			}
		}
		for _, table := range factTables {
			indexed := indexedColumns(t, db, table)
			for _, want := range []string{"tx_hash,log_index,sub", "row_id", "block_time", "account,block"} {
				if !slices.Contains(indexed, want) {
					t.Fatalf("perps.%s indexes %v lack (%s)", table, indexed, want)
				}
			}
		}
		for table, want := range map[string]string{
			"delivery_outbox":     "event_id",
			"delivery_inbox":      "consumer_id,projection_generation,event_id",
			"delivery_quarantine": "consumer_id,projection_generation,envelope_hash",
		} {
			if got := primaryKey(t, db, table); got != want {
				t.Fatalf("perps.%s primary key %s, want %s", table, got, want)
			}
		}
		var def string
		if err := db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE schemaname = 'perps' AND indexname = 'delivery_pending'`).Scan(&def); err != nil {
			t.Fatalf("delivery_pending index: %v", err)
		}
		if want := "CREATE INDEX delivery_pending ON perps.delivery_outbox USING btree (available_at, created_at) WHERE (state = 'pending'::text)"; def != want {
			t.Fatalf("delivery_pending is %q, want %q", def, want)
		}
		var trigger string
		if err := db.QueryRow(`SELECT action_timing || ' ' || event_manipulation || ' ' || action_orientation || ' ' || action_statement FROM information_schema.triggers WHERE trigger_schema = 'perps' AND trigger_name = 'delivery_immutable' AND event_object_table = 'delivery_outbox'`).Scan(&trigger); err != nil {
			t.Fatalf("delivery_immutable trigger: %v", err)
		}
		if want := "BEFORE UPDATE ROW EXECUTE FUNCTION perps.delivery_immutable()"; trigger != want {
			t.Fatalf("delivery_immutable trigger %q, want %q", trigger, want)
		}
		var publicSelect bool
		if err := db.QueryRow(`SELECT has_table_privilege('public', 'perps.delivery_quarantine', 'SELECT')`).Scan(&publicSelect); err != nil {
			t.Fatalf("quarantine privilege: %v", err)
		}
		if publicSelect {
			t.Fatal("PUBLIC may select perps.delivery_quarantine")
		}
	})

	t.Run("fact_keys", func(t *testing.T) {
		migrate(t, db)
		for _, table := range factTables {
			if got := primaryKey(t, db, table); got != "block,tx_index,log_index,sub" {
				t.Fatalf("perps.%s primary key %s, want block,tx_index,log_index,sub", table, got)
			}
			unique := uniqueKeys(t, db, table)
			for _, want := range []string{"tx_hash,log_index,sub", "row_id"} {
				if !slices.Contains(unique, want) {
					t.Fatalf("perps.%s unique keys %v lack (%s)", table, unique, want)
				}
			}
			names := columnNames(columns(t, db, table))
			if len(names) < len(commonColumns) || !slices.Equal(names[:len(commonColumns)], commonColumns) {
				t.Fatalf("perps.%s columns %v do not start with the common columns %v", table, names, commonColumns)
			}
		}
	})

	t.Run("blocks_and_cursors", func(t *testing.T) {
		migrate(t, db)
		if got := columns(t, db, "blocks"); !slices.Equal(got, []column{
			{"number", "bigint"}, {"hash", "bytea"}, {"parent_hash", "bytea"}, {"time", "timestamp with time zone"}, {"proposer", "text"}, {"tx_count", "integer"},
		}) {
			t.Fatalf("blocks columns %v", got)
		}
		if got := columns(t, db, "cursors"); !slices.Equal(got, []column{
			{"name", "text"}, {"height", "bigint"}, {"hash", "bytea"}, {"updated_at", "timestamp with time zone"},
		}) {
			t.Fatalf("cursors columns %v", got)
		}
		if got := primaryKey(t, db, "blocks"); got != "number" {
			t.Fatalf("blocks primary key %s, want number", got)
		}
		if got := primaryKey(t, db, "cursors"); got != "name" {
			t.Fatalf("cursors primary key %s, want name", got)
		}
		insert := `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES (1, decode(repeat('aa', 32), 'hex'), decode(repeat('bb', 32), 'hex'), now(), $1, 0)`
		if err := execRolledBack(t, db, insert, strings.Repeat("AB", 20)); err != nil {
			t.Fatalf("upper-case hex proposer refused: %v", err)
		}
		wantCheckViolation(t, execRolledBack(t, db, insert, strings.Repeat("ab", 20)), "blocks_proposer_check")
	})

	t.Run("chain_tables", func(t *testing.T) {
		migrate(t, db)
		if got := columnNames(columns(t, db, "markets")); !slices.Equal(got, []string{
			"market_id", "symbol", "name", "oracle_symbol", "sz_decimals", "px_decimals", "max_leverage",
			"maintenance_margin_bps", "max_open_interest", "enabled", "source", "refreshed_block",
		}) {
			t.Fatalf("markets columns %v", got)
		}
		if got := primaryKey(t, db, "markets"); got != "market_id" {
			t.Fatalf("markets primary key %s, want market_id", got)
		}
		if got := uniqueKeys(t, db, "markets"); !slices.Equal(got, []string{"symbol"}) {
			t.Fatalf("markets unique keys %v, want [symbol]", got)
		}
		market := `INSERT INTO perps.markets (market_id, symbol, name, oracle_symbol, sz_decimals, px_decimals, max_leverage, maintenance_margin_bps, max_open_interest, enabled, source, refreshed_block)
VALUES (4096, 'UNSEEDED', 'UNSEEDED', 'UNSEEDED', $1, $2, 10, 50, 0, true, 'chain', 9)`
		if err := execRolledBack(t, db, market, nil, nil); err != nil {
			t.Fatalf("chain-discovered market with unknown decimals refused: %v", err)
		}
		wantCheckViolation(t, execRolledBack(t, db, market, -1, nil), "markets_sz_decimals_check")
		wantCheckViolation(t, execRolledBack(t, db, market, nil, -1), "markets_px_decimals_check")
		if got := columns(t, db, "fee_config"); !slices.Equal(got, []column{
			{"id", "smallint"}, {"taker_fee_ppm", "numeric"}, {"maker_fee_ppm", "numeric"}, {"liquidation_fee_ppm", "numeric"},
			{"insurance_share_ppm", "numeric"}, {"refreshed_block", "bigint"},
		}) {
			t.Fatalf("fee_config columns %v", got)
		}
		if got := primaryKey(t, db, "fee_config"); got != "id" {
			t.Fatalf("fee_config primary key %s, want id", got)
		}
		insert := `INSERT INTO perps.fee_config (id, taker_fee_ppm, maker_fee_ppm, liquidation_fee_ppm, insurance_share_ppm, refreshed_block) VALUES ($1, 1, 2, 3, 4, 5)`
		if err := execRolledBack(t, db, insert+` ON CONFLICT (id) DO NOTHING`, 1); err != nil {
			t.Fatalf("fee_config row 1 refused: %v", err)
		}
		wantCheckViolation(t, execRolledBack(t, db, insert, 2), "fee_config_id_check")
	})

	t.Run("amount_columns_exact", func(t *testing.T) {
		migrate(t, db)
		if err := execRolledBack(t, db, settlementInsert, "1"); err != nil {
			t.Fatalf("integral size_usd refused: %v", err)
		}
		wantCheckViolation(t, execRolledBack(t, db, settlementInsert, "1.5"), "uint256_check")
		var typ, domainSchema, domainName string
		if err := db.QueryRow(`SELECT data_type, domain_schema, domain_name FROM information_schema.columns WHERE table_schema = 'perps' AND table_name = 'trade_settlements' AND column_name = 'size_usd'`).Scan(&typ, &domainSchema, &domainName); err != nil {
			t.Fatalf("size_usd type: %v", err)
		}
		if typ != "numeric" || domainSchema != "perps" || domainName != "uint256" {
			t.Fatalf("size_usd is %s over domain %s.%s, want numeric over domain perps.uint256", typ, domainSchema, domainName)
		}
	})

	t.Run("projection_tables", func(t *testing.T) {
		migrate(t, db)
		for table, want := range map[string]string{
			"positions_current":    "position_id",
			"orders":               "order_id",
			"spot_orders":          "kind,order_id",
			"account_margin":       "account,token",
			"user_stats":           "account",
			"user_pnl_daily":       "account,day",
			"market_stats_rolling": "market_id",
			"leaderboard":          "window,rank",
			"spot_token_seen":      "token",
			"market_snapshots":     "market_id,hour",
			"protocol_snapshots":   "hour",
			"keeper_cycles":        "block",
		} {
			if got := primaryKey(t, db, table); got != want {
				t.Fatalf("perps.%s primary key %s, want %s", table, got, want)
			}
		}
		if got := tablesInPerps(t, db, "VIEW"); !slices.Equal(got, []string{"account_ledger"}) {
			t.Fatalf("views of perps %v, want [account_ledger]", got)
		}
		if slices.Contains(tablesInPerps(t, db, "BASE TABLE"), "oi_snapshots") {
			t.Fatal("perps.oi_snapshots exists")
		}
	})

	t.Run("account_ledger_row_id", func(t *testing.T) {
		migrate(t, db)
		if got := columnNames(columns(t, db, "account_ledger")); !slices.Equal(got, []string{
			"account", "token", "entry_type", "amount", "position_id", "is_debit",
			"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "row_id",
		}) {
			t.Fatalf("account_ledger columns %v", got)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				t.Errorf("rollback: %v", err)
			}
		}()
		const keys = `block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account, tx_from`
		const common = `decode(repeat('11', 32), 'hex'), 0, $1, 0, decode(repeat('22', 32), 'hex'), 'tx', now(), decode(repeat('33', 20), 'hex')`
		const account = `decode(repeat('44', 20), 'hex')`
		sources := []struct {
			entryType int
			insert    string
		}{
			{0, `INSERT INTO perps.collateral_events (` + keys + `, account_arg, amount, new_balance)
VALUES (7, ` + common + `, 'deposited', ` + account + `, ` + account + `, ` + account + `, 10, 10) RETURNING row_id`},
			{4, `INSERT INTO perps.fees (` + keys + `, position_id, "user", market_id, fee_type, fee_usd, fee_tokens, token)
VALUES (7, ` + common + `, 'collected', ` + account + `, ` + account + `, 9, ` + account + `, 1, 0, 2, 2, decode(repeat('85', 20), 'hex')) RETURNING row_id`},
			{5, `INSERT INTO perps.funding_payments (` + keys + `, position_id, "user", market_id, funding_payment_usd, new_collateral_usd, new_collateral_amount)
VALUES (7, ` + common + `, 'applied', ` + account + `, ` + account + `, 9, ` + account + `, 1, -3, 50, 50) RETURNING row_id`},
			{6, `INSERT INTO perps.trade_settlements (` + keys + `, position_id, "user", market_id, trade_type, size_usd, execution_price, gross_pnl, total_fees_usd, funding_paid_usd, net_payout_tokens)
VALUES (7, ` + common + `, 'settled', ` + account + `, ` + account + `, 9, ` + account + `, 1, 0, 100, 100, -5, 1, 0, 3) RETURNING row_id`},
		}
		want := map[int]int64{}
		for i, src := range sources {
			var rowID int64
			if err := tx.QueryRow(src.insert, i).Scan(&rowID); err != nil {
				t.Fatalf("insert source of entry type %d: %v", src.entryType, err)
			}
			want[src.entryType] = rowID
		}
		rows, err := tx.Query(`SELECT entry_type, row_id, log_index FROM perps.account_ledger WHERE block = 7 ORDER BY log_index`)
		if err != nil {
			t.Fatalf("SELECT row_id FROM perps.account_ledger: %v", err)
		}
		defer rows.Close()
		got := map[int]int64{}
		for rows.Next() {
			var entryType, logIndex int
			var rowID int64
			if err := rows.Scan(&entryType, &rowID, &logIndex); err != nil {
				t.Fatalf("scan account_ledger: %v", err)
			}
			if sources[logIndex].entryType != entryType {
				t.Fatalf("account_ledger row at log index %d has entry type %d, want %d", logIndex, entryType, sources[logIndex].entryType)
			}
			got[entryType] = rowID
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read account_ledger: %v", err)
		}
		if !maps.Equal(got, want) {
			t.Fatalf("account_ledger row_id by entry type %v, want the source row_id %v", got, want)
		}
	})

	t.Run("delivery_tables", func(t *testing.T) {
		migrate(t, db)
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				t.Errorf("rollback: %v", err)
			}
		}()
		if _, err := tx.Exec(`INSERT INTO perps.delivery_outbox (event_id, schema_version, subject, payload_hash, envelope) VALUES ('e1', 1, 'perps.fills', 'h1', '\x0102')`); err != nil {
			t.Fatalf("insert outbox row: %v", err)
		}
		if _, err := tx.Exec(`UPDATE perps.delivery_outbox SET state = 'published', published_at = now(), attempts = 1, broker_sequence = 1 WHERE event_id = 'e1'`); err != nil {
			t.Fatalf("update of delivery state refused: %v", err)
		}
		var state string
		var attempts int
		if err := tx.QueryRow(`SELECT state, attempts FROM perps.delivery_outbox WHERE event_id = 'e1'`).Scan(&state, &attempts); err != nil {
			t.Fatalf("read outbox row: %v", err)
		}
		if state != "published" || attempts != 1 {
			t.Fatalf("outbox row state %s attempts %d, want published 1", state, attempts)
		}
		_, err = tx.Exec(`UPDATE perps.delivery_outbox SET envelope = '\x03' WHERE event_id = 'e1'`)
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "P0001" || pqErr.Message != "immutable delivery envelope" {
			t.Fatalf("envelope update: %v, want immutable delivery envelope", err)
		}
	})

	t.Run("delivery_sequence", func(t *testing.T) {
		migrate(t, db)
		var typ, nullable, identity, generation, start, increment string
		if err := db.QueryRow(`SELECT data_type, is_nullable, is_identity, identity_generation, identity_start, identity_increment
 FROM information_schema.columns WHERE table_schema='perps' AND table_name='delivery_outbox' AND column_name='perps_seq'`).Scan(&typ, &nullable, &identity, &generation, &start, &increment); err != nil {
			t.Fatal(err)
		}
		if typ != "bigint" || nullable != "NO" || identity != "YES" || generation != "ALWAYS" || start != "1" || increment != "1" {
			t.Fatalf("perps_seq definition: %s %s %s %s %s %s", typ, nullable, identity, generation, start, increment)
		}
		if got := uniqueKeys(t, db, "delivery_outbox"); !slices.Equal(got, []string{"perps_seq"}) {
			t.Fatalf("delivery_outbox unique keys %v, want [perps_seq]", got)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				t.Error(err)
			}
		}()
		if _, err = tx.Exec(`TRUNCATE perps.delivery_outbox RESTART IDENTITY`); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 3; i++ {
			var sequence int64
			if err = tx.QueryRow(`INSERT INTO perps.delivery_outbox (event_id, schema_version, subject, payload_hash, envelope)
 VALUES ($1, 1, 'perps.fills', 'h1', '\x0102') RETURNING perps_seq`, fmt.Sprintf("sequence-%d", i)).Scan(&sequence); err != nil || sequence != int64(i) {
				t.Fatalf("insert %d: sequence %d, error %v", i, sequence, err)
			}
		}
		for _, check := range []struct {
			name, query, code, constraint, message string
		}{
			{"generated_always", `INSERT INTO perps.delivery_outbox (event_id, perps_seq, schema_version, subject, payload_hash, envelope) VALUES ('explicit-sequence', 4, 1, 'perps.fills', 'h1', '\x0102')`, "428C9", "", `cannot insert a non-DEFAULT value into column "perps_seq"`},
			{"unique", `INSERT INTO perps.delivery_outbox (event_id, perps_seq, schema_version, subject, payload_hash, envelope) OVERRIDING SYSTEM VALUE VALUES ('duplicate-sequence', 1, 1, 'perps.fills', 'h1', '\x0102')`, "23505", "delivery_outbox_perps_seq_key", `duplicate key value violates unique constraint "delivery_outbox_perps_seq_key"`},
			{"immutable", `UPDATE perps.delivery_outbox SET perps_seq=DEFAULT WHERE event_id='sequence-1'`, "P0001", "", "immutable delivery envelope"},
		} {
			t.Run(check.name, func(t *testing.T) {
				if _, err := tx.Exec(`SAVEPOINT rejected_write`); err != nil {
					t.Fatal(err)
				}
				_, err := tx.Exec(check.query)
				if _, rollbackErr := tx.Exec(`ROLLBACK TO SAVEPOINT rejected_write`); rollbackErr != nil {
					t.Fatal(rollbackErr)
				}
				var pqErr *pq.Error
				if !errors.As(err, &pqErr) || pqErr.Code != pq.ErrorCode(check.code) || pqErr.Constraint != check.constraint || pqErr.Message != check.message {
					t.Fatalf("%s: %v, want %s %q %q", check.name, err, check.code, check.constraint, check.message)
				}
			})
		}
		var sequence int64
		if err := tx.QueryRow(`SELECT perps_seq FROM perps.delivery_outbox WHERE event_id='sequence-1'`).Scan(&sequence); err != nil || sequence != 1 {
			t.Fatalf("original sequence %d, want 1: %v", sequence, err)
		}
	})

	t.Run("receipt_kind_check", func(t *testing.T) {
		migrate(t, db)
		txFrom := []byte(strings.Repeat("\x77", 20))
		if err := execRolledBack(t, db, fillInsert, "tx", txFrom); err != nil {
			t.Fatalf("tx fill with tx_from refused: %v", err)
		}
		for _, kind := range []string{"perps_endblock", "spot_endblock", "activation", "block_event"} {
			if err := execRolledBack(t, db, fillInsert, kind, nil); err != nil {
				t.Fatalf("%s fill without tx_from refused: %v", kind, err)
			}
			wantCheckViolation(t, execRolledBack(t, db, fillInsert, kind, txFrom), "fills_check")
		}
		wantCheckViolation(t, execRolledBack(t, db, fillInsert, "other", nil), "fills_receipt_kind_check")
	})

	t.Run("no_candle_table", func(t *testing.T) {
		migrate(t, db)
		if got := queryStrings(t, db, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'perps' AND table_name LIKE '%candle%'`); len(got) != 0 {
			t.Fatalf("candle tables in perps: %v", got)
		}
		root := filepath.Join("..", "..")
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && strings.Contains(d.Name(), "candle") {
				return fmt.Errorf("candle package %s exists", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
