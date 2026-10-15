package migrations

import (
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	pow255        = "57896044618658097711785492504343953926634992332820282019728792003956564819968"
	receiptKinds  = "receipt_kind text NOT NULL CHECK (receipt_kind IN ('tx', 'perps_endblock', 'spot_endblock', 'activation', 'block_event'))"
	txFromCheck   = "CHECK (receipt_kind = 'tx' OR tx_from IS NULL)"
	factPK        = "PRIMARY KEY (block, tx_index, log_index, sub)"
	factsFile     = "0001_facts.sql"
	projectsFile  = "0002_projections.sql"
	usdlByteaText = `'\x85fcd13735f4309833a503ee804ea32395851479'::bytea`
)

var commonColumns = []string{"row_id", "block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}

var commonDefs = map[string]string{
	"row_id":       "bigint GENERATED ALWAYS AS IDENTITY",
	"block":        "bigint NOT NULL CHECK (block >= 0)",
	"block_hash":   "bytea NOT NULL CHECK (octet_length(block_hash) = 32)",
	"tx_index":     "integer NOT NULL CHECK (tx_index >= -1)",
	"log_index":    "integer NOT NULL CHECK (log_index >= 0)",
	"sub":          "integer NOT NULL CHECK (sub >= 0)",
	"tx_hash":      "bytea NOT NULL CHECK (octet_length(tx_hash) = 32)",
	"receipt_kind": strings.TrimPrefix(receiptKinds, "receipt_kind "),
	"block_time":   "timestamptz NOT NULL",
	"emitter":      "bytea NOT NULL CHECK (octet_length(emitter) = 20)",
	"kind":         "text NOT NULL",
	"account":      "bytea NOT NULL CHECK (octet_length(account) = 20)",
	"tx_from":      "bytea CHECK (octet_length(tx_from) = 20)",
}

// factTable is one fact table of contracts/data-services.md section 2: its
// kinds, its argument columns as name:type (u uint256, s int256, u8 uint8,
// u64 uint64, b bool, a address, b16 bytes16, bytes bytes, raw a literal
// definition prefix) and its indexes beyond the common ones.
type factTable struct {
	name    string
	kinds   []string
	args    []string
	indexes []string
}

var factTables = []factTable{
	{"vaults", []string{"created"}, []string{"user:a", "vault:a"}, []string{"unique (user)"}},
	{"collateral_events", []string{"deposited", "withdrawn", "transferred_out", "transferred_in"},
		[]string{"account_arg:a", "amount:u", "new_balance:u", "from_account:a", "to_account:a"}, []string{"(kind, block)"}},
	{"order_events", []string{"placed", "options", "cancelled", "expired", "triggered", "executed"},
		[]string{"order_id:u", "user:a", "market_id:u", "order_type:u8", "is_long:b", "trigger_price:u", "size_usd:u", "time_in_force:u8", "reduce_only:b", "limit_price:u", "acceptable_price:u", "deadline:u64", "cloid:b16", "mark_price:u", "position_id:u", "execution_price:u"},
		[]string{"(order_id, block)", "(market_id, block)", "(user, block)"}},
	{"fills", []string{"fill"},
		[]string{"market_id:u", "maker_order_id:u", "taker_order_id:u", "maker:a", "taker:a", "taker_is_long:b", "price:u", "size_usd:u", "maker_fee_usd:s", "taker_fee_usd:u"},
		[]string{"(market_id, block)", "(maker, block)", "(taker, block)"}},
	{"position_events", []string{"opened", "modified", "entry_price_changed", "closed", "tpsl_set"},
		[]string{"position_id:u", "user:a", "market_id:u", "is_long:b", "size_usd:u", "leverage:u", "entry_price:u", "collateral_token:a", "collateral_amount:u", "new_size_usd:u", "new_collateral_usd:u", "new_collateral_amount:u", "old_entry_price:u", "new_entry_price:u", "closed_size_usd:u", "exit_price:u", "realized_pnl:s", "is_full_close:b", "take_profit_price:u", "stop_loss_price:u"},
		[]string{"(position_id, block)", "(market_id, block)", "(user, block)"}},
	{"trade_settlements", []string{"settled"},
		[]string{"position_id:u", "user:a", "market_id:u", "trade_type:u8", "size_usd:u", "execution_price:u", "gross_pnl:s", "total_fees_usd:u", "funding_paid_usd:s", "net_payout_tokens:u"},
		[]string{"(position_id, block)", "(market_id, block)", "(user, block)"}},
	{"fees", []string{"collected"},
		[]string{"position_id:u", "user:a", "market_id:u", "fee_type:u8", "fee_usd:u", "fee_tokens:u", "token:a"},
		[]string{"(market_id, block)", "(position_id, block)", "(user, block)"}},
	{"mark_prices", []string{"mark"}, []string{"market_id:u", "mark_price:u", "index_price:u", "mid_price:u"}, []string{"(market_id, block)"}},
	{"funding_rates", []string{"rate"}, []string{"market_id:u", "new_rate_per_second:s", "funding_rate24h:s"}, []string{"(market_id, block)"}},
	{"funding_settlements", []string{"settled"}, []string{"market_id:u", "funding_rate:s", "long_payment:s", "short_payment:s"}, []string{"(market_id, block)"}},
	{"funding_payments", []string{"applied"},
		[]string{"position_id:u", "user:a", "market_id:u", "funding_payment_usd:s", "new_collateral_usd:u", "new_collateral_amount:u"},
		[]string{"(position_id, block)", "(market_id, block)", "(user, block)"}},
	{"open_interest", []string{"oi"}, []string{"market_id:u", "long_oi:u", "short_oi:u", "delta_usd:u", "is_increase:b"}, []string{"(market_id, block)"}},
	{"liquidations", []string{"liquidated"},
		[]string{"position_id:u", "user:a", "market_id:u", "liquidation_price:u", "penalty:u", "keeper:a"},
		[]string{"(position_id, block)", "(market_id, block)", "(user, block)"}},
	{"backstop_liquidations", []string{"backstop"},
		[]string{"account_arg:a", "market_id:u", "size_usd:u", "mark_price:u", "margin_taken_usd:u"}, []string{"(market_id, block)"}},
	{"insurance_events", []string{"contribution", "draw", "vault_deficit"},
		[]string{"token:a", "amount:u", "source:u8", "market_id:u", "new_balance:u", "deficit:u"}, []string{"(kind, block)"}},
	{"adl_events", []string{"adl", "matched"},
		[]string{"position_id:u", "deleveraged_size_usd:u", "market_id:u", "account_arg:a", "size_usd:u", "price:u", "rank_score:u"},
		[]string{"(position_id, block)", "(market_id, block)"}},
	{"feed_keys", []string{"registered", "revoked"},
		[]string{"validator:a", "feed_account:a", "pub_key:bytes", "previous_feed_account:a"}, []string{"(validator, block)"}},
	{"index_prices", []string{"aggregate", "skipped"},
		[]string{"market_id:u", "index_price:u", "mark_price:u", "basis:s", "timestamp_ms:u", "sources:u", "required:u"}, []string{"(market_id, block)"}},
	{"spot_swaps", []string{"market_order", "native_swap", "simple_swap"},
		[]string{"user:a", "stablecoin:a", "token:a", "stablecoin_amount:u", "token_amount:u", "is_buy:b", "token_other:a", "native_amount:u", "native_is_input:b", "token_in:a", "token_out:a", "amount_in:u", "amount_out:u", "execution_price:u"},
		[]string{"(token, block)", "(user, block)"}},
	{"spot_fee_events", []string{"price_impact", "tiered_fee"},
		[]string{"user:a", "token_out:a", "impact_bps:u", "deducted_amount:u", "volume_usd:u", "fee_bps:u", "fee_amount:u"}, []string{"(user, block)"}},
	{"spot_order_events", []string{"limit_created", "limit_cancelled", "limit_executed", "limit_expired", "limit_modified", "stop_limit_created", "stop_limit_activated", "stop_limit_cancelled", "stop_limit_executed", "stop_loss_created", "stop_loss_cancelled", "stop_loss_executed"},
		[]string{"order_id:u", "user:a", "order_type:u8", "amount:u", "target_price:u", "token:a", "stablecoin:a", "execution_price:u", "expires_at:u", "stop_price:u", "limit_price:u", "activation_price:u", "trigger_price:u"},
		[]string{"(order_id, block)", "(user, block)", "(token, block)"}},
	{"spot_vault_events", []string{"deposit", "lp_withdrawal", "native_deposit", "native_withdrawal", "withdrawal", "reserves_sync", "reserves_updated"},
		[]string{"token:a", "depositor:a", "amount:u", "new_reserve:u", "provider:a", "shares:u", "recipient:a", "old_reserves:u", "new_reserves:u", "recovered:u", "old_reserve:u"},
		[]string{"(token, block)"}},
	{"spot_token_registry", []string{"token_registered", "share_token_created"},
		[]string{"token:a", "decimals:u8", "is_stablecoin:b", "share_token:a"}, []string{"(token)", "unique (share_token)"}},
	{"spot_admin", []string{"ownership_transferred", "paused", "unpaused", "twap_sanity_updated", "operator_updated", "token_pause_updated", "tracker_updated", "utilization_cap_updated"},
		[]string{"previous_holder:a", "new_holder:a", "account_arg:a", "enabled:b", "period:u", "max_deviation_bps:u", "operator:a", "authorized:b", "token:a", "paused:b", "tracker:a", "max_utilization_bps:u"},
		[]string{"(emitter, block)", "(token, block)"}},
	{"spot_share_transfers", []string{"transfer", "approval"},
		[]string{"share_token:a", "token:a", "from_account:a", "to_account:a", "value:u", "holder:a", "spender:a"},
		[]string{"(share_token, block)", "(token, block)"}},
	{"usdl_transfers", []string{"in", "out"}, []string{"from_account:a", "to_account:a", "value:u", "venue:a"}, []string{"(venue, block)"}},
	{"raw_logs", nil,
		[]string{"topics:raw:bytea[] NOT NULL CHECK (cardinality(topics) <= 4)", "data:raw:bytea NOT NULL", "decoded:raw:boolean NOT NULL", "failure_class:raw:text NOT NULL CHECK (failure_class IN ('unknown_topic', 'abi_decode', 'order_lookup', 'share_lookup'))"},
		[]string{"(decoded, block)", "(failure_class, block)"}},
	{"protocol_events", nil,
		[]string{"event_name:raw:text NOT NULL", "market_id:u", "args:raw:jsonb NOT NULL"},
		[]string{"(event_name, block)", "(market_id, block)"}},
	{"tx_actions", nil,
		[]string{"action:raw:text NOT NULL", "symbol:raw:text", "side:raw:text", "price:u", "size:u"},
		[]string{"(block DESC, tx_index DESC, log_index DESC)", "(account, block DESC)", "(symbol, block DESC)"}},
}

var signedColumns = []string{"gross_pnl", "funding_paid_usd", "realized_pnl", "maker_fee_usd", "funding_rate", "long_payment", "short_payment", "new_rate_per_second", "funding_rate24h", "funding_payment_usd", "basis"}

type column struct{ name, def string }

type table struct {
	name        string
	file        string
	columns     []column
	constraints []string
}

func (t *table) column(name string) (column, bool) {
	for _, c := range t.columns {
		if c.name == name {
			return c, true
		}
	}
	return column{}, false
}

func (t *table) columnNames() []string {
	names := make([]string, 0, len(t.columns))
	for _, c := range t.columns {
		names = append(names, c.name)
	}
	return names
}

type index struct {
	name, table, columns string
	unique               bool
}

type schema struct {
	files   []string
	tables  map[string]*table
	indexes []index
	views   map[string]string
}

func (s *schema) indexSet(tableName string) []string {
	var out []string
	for _, ix := range s.indexes {
		if ix.table != tableName {
			continue
		}
		entry := ix.columns
		if ix.unique {
			entry = "unique " + entry
		}
		out = append(out, entry)
	}
	return out
}

var (
	createTable  = regexp.MustCompile(`(?s)^CREATE TABLE perps\.(\w+) \((.*)\)$`)
	createIndex  = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX (\w+) ON perps\.(\w+) (\(.*\))$`)
	createView   = regexp.MustCompile(`(?s)^CREATE VIEW perps\.(\w+) AS\n(.*)$`)
	createSchema = "CREATE SCHEMA IF NOT EXISTS perps"
)

func splitTopLevel(body string) []string {
	var parts []string
	depth, start := 0, 0
	inString := false
	for i, r := range body {
		switch {
		case r == '\'':
			inString = !inString
		case inString:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(body[start:]))
}

func parseTable(file, name, body string) (*table, error) {
	t := &table{name: name, file: file}
	for _, item := range splitTopLevel(body) {
		if strings.HasPrefix(item, "PRIMARY KEY ") || strings.HasPrefix(item, "CHECK ") || strings.HasPrefix(item, "UNIQUE ") {
			t.constraints = append(t.constraints, strings.ReplaceAll(item, `"`, ""))
			continue
		}
		colName, def, ok := strings.Cut(item, " ")
		if !ok {
			return nil, fmt.Errorf("table %s: column item %q has no type", name, item)
		}
		t.columns = append(t.columns, column{name: strings.Trim(colName, `"`), def: strings.ReplaceAll(def, `"`, "")})
	}
	return t, nil
}

func loadSchema(t *testing.T) *schema {
	t.Helper()
	files, err := fs.Glob(FS, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	s := &schema{files: files, tables: map[string]*table{}, views: map[string]string{}}
	for _, file := range files {
		raw, err := fs.ReadFile(FS, file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, stmt := range strings.Split(string(raw), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || stmt == createSchema {
				continue
			}
			if m := createTable.FindStringSubmatch(stmt); m != nil {
				if _, dup := s.tables[m[1]]; dup {
					t.Fatalf("%s: table perps.%s created twice", file, m[1])
				}
				tb, err := parseTable(file, m[1], m[2])
				if err != nil {
					t.Fatalf("%s: %v", file, err)
				}
				s.tables[m[1]] = tb
				continue
			}
			if m := createIndex.FindStringSubmatch(stmt); m != nil {
				s.indexes = append(s.indexes, index{name: m[2], table: m[3], columns: strings.ReplaceAll(m[4], `"`, ""), unique: m[1] != ""})
				continue
			}
			if m := createView.FindStringSubmatch(stmt); m != nil {
				s.views[m[1]] = m[2]
				continue
			}
			t.Fatalf("%s: unrecognised statement %q", file, stmt)
		}
	}
	return s
}

func expectedArgDef(name, typ string) (prefix string, check string) {
	switch typ {
	case "u":
		return "numeric(78,0)", fmt.Sprintf("CHECK (%[1]s = trunc(%[1]s) AND %[1]s >= 0)", name)
	case "s":
		return "numeric(78,0)", fmt.Sprintf("CHECK (%[1]s = trunc(%[1]s) AND %[1]s >= -%[2]s AND %[1]s < %[2]s)", name, pow255)
	case "u8":
		return "smallint", fmt.Sprintf("CHECK (%[1]s >= 0 AND %[1]s <= 255)", name)
	case "u64":
		return "bigint", fmt.Sprintf("CHECK (%s >= 0)", name)
	case "b":
		return "boolean", ""
	case "a":
		return "bytea", fmt.Sprintf("CHECK (octet_length(%s) = 20)", name)
	case "b16":
		return "bytea", fmt.Sprintf("CHECK (octet_length(%s) = 16)", name)
	case "bytes":
		return "bytea", ""
	}
	return "", ""
}

func quoteList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + v + "'"
	}
	return strings.Join(quoted, ", ")
}

func TestSchemaFiles(t *testing.T) {
	s := loadSchema(t)
	for _, want := range []string{factsFile, projectsFile} {
		if !slices.Contains(s.files, want) {
			t.Fatalf("embedded files %v lack %s", s.files, want)
		}
	}
	if !slices.IsSorted(s.files) {
		t.Fatalf("embedded files %v are not in lexical order", s.files)
	}

	t.Run("fact_columns", func(t *testing.T) {
		for _, ft := range factTables {
			tb, ok := s.tables[ft.name]
			if !ok {
				t.Errorf("fact table perps.%s missing", ft.name)
				continue
			}
			if tb.file != factsFile {
				t.Errorf("perps.%s is created in %s, want %s", ft.name, tb.file, factsFile)
			}
			wantNames := slices.Clone(commonColumns)
			for _, arg := range ft.args {
				name, _, _ := strings.Cut(arg, ":")
				wantNames = append(wantNames, name)
			}
			if got := tb.columnNames(); !slices.Equal(got, wantNames) {
				t.Errorf("perps.%s columns\n got %v\nwant %v", ft.name, got, wantNames)
				continue
			}
			for i, name := range commonColumns {
				if got := tb.columns[i].def; got != commonDefs[name] {
					t.Errorf("perps.%s.%s = %q, want %q", ft.name, name, got, commonDefs[name])
				}
			}
			for i, arg := range ft.args {
				name, typ, _ := strings.Cut(arg, ":")
				got := tb.columns[len(commonColumns)+i].def
				if raw, ok := strings.CutPrefix(typ, "raw:"); ok {
					if got != raw {
						t.Errorf("perps.%s.%s = %q, want %q", ft.name, name, got, raw)
					}
					continue
				}
				if (typ == "s") != slices.Contains(signedColumns, name) {
					t.Errorf("perps.%s.%s has type %s, but the signed set of section 6 says otherwise", ft.name, name, typ)
				}
				prefix, check := expectedArgDef(name, typ)
				rest, ok := strings.CutPrefix(got, prefix)
				if !ok || (rest != "" && !strings.HasPrefix(rest, " ")) {
					t.Errorf("perps.%s.%s = %q, want type %s", ft.name, name, got, prefix)
					continue
				}
				rest = strings.TrimPrefix(strings.TrimPrefix(rest, " "), "NOT NULL")
				if strings.TrimSpace(rest) != check {
					t.Errorf("perps.%s.%s check = %q, want %q", ft.name, name, strings.TrimSpace(rest), check)
				}
			}
			if ft.kinds != nil {
				want := fmt.Sprintf("CHECK (kind IN (%s))", quoteList(ft.kinds))
				if !slices.Contains(tb.constraints, want) {
					t.Errorf("perps.%s constraints %v lack %q", ft.name, tb.constraints, want)
				}
			}
		}
		amount, _ := s.tables["trade_settlements"].column("size_usd")
		if amount.def != "numeric(78,0) NOT NULL CHECK (size_usd = trunc(size_usd) AND size_usd >= 0)" {
			t.Errorf("perps.trade_settlements.size_usd = %q", amount.def)
		}
		nullable, _ := s.tables["index_prices"].column("index_price")
		if strings.Contains(nullable.def, "NOT NULL") {
			t.Errorf("perps.index_prices.index_price must be null for skipped rows: %q", nullable.def)
		}
		shared, _ := s.tables["collateral_events"].column("amount")
		if !strings.Contains(shared.def, "NOT NULL") {
			t.Errorf("perps.collateral_events.amount is carried by every kind and must be NOT NULL: %q", shared.def)
		}
	})

	t.Run("fact_keys", func(t *testing.T) {
		for _, ft := range factTables {
			tb, ok := s.tables[ft.name]
			if !ok {
				t.Errorf("fact table perps.%s missing", ft.name)
				continue
			}
			if !slices.Contains(tb.constraints, factPK) {
				t.Errorf("perps.%s constraints %v lack %q", ft.name, tb.constraints, factPK)
			}
			want := []string{"unique (tx_hash, log_index, sub)", "unique (row_id)", "(block_time)"}
			if ft.name != "tx_actions" {
				want = append(want, "(account, block)")
			}
			for _, ix := range ft.indexes {
				if !slices.Contains(want, ix) {
					want = append(want, ix)
				}
			}
			if got := s.indexSet(ft.name); !slices.Equal(got, want) {
				t.Errorf("perps.%s indexes\n got %v\nwant %v", ft.name, got, want)
			}
		}
		names := map[string]bool{}
		for _, ix := range s.indexes {
			if len(ix.name) > 63 {
				t.Errorf("index name %s exceeds the 63-byte identifier limit", ix.name)
			}
			if names[ix.name] {
				t.Errorf("index name %s declared twice", ix.name)
			}
			names[ix.name] = true
		}
	})

	t.Run("receipt_kind_check", func(t *testing.T) {
		for _, ft := range factTables {
			tb, ok := s.tables[ft.name]
			if !ok {
				t.Errorf("fact table perps.%s missing", ft.name)
				continue
			}
			rk, _ := tb.column("receipt_kind")
			if "receipt_kind "+rk.def != receiptKinds {
				t.Errorf("perps.%s.receipt_kind = %q", ft.name, rk.def)
			}
			if !slices.Contains(tb.constraints, txFromCheck) {
				t.Errorf("perps.%s constraints %v lack %q", ft.name, tb.constraints, txFromCheck)
			}
			from, _ := tb.column("tx_from")
			if strings.Contains(from.def, "NOT NULL") {
				t.Errorf("perps.%s.tx_from must be nullable: %q", ft.name, from.def)
			}
		}
	})

	t.Run("chain_tables", func(t *testing.T) {
		want := map[string][]string{
			"blocks":       {"number", "hash", "parent_hash", "time", "proposer", "tx_count"},
			"cursors":      {"name", "height", "hash", "updated_at"},
			"writer_lease": {"name", "holder", "expires_at"},
			"markets":      {"market_id", "symbol", "name", "oracle_symbol", "sz_decimals", "px_decimals", "max_leverage", "maintenance_margin_bps", "max_open_interest", "enabled", "source", "refreshed_block"},
			"fee_config":   {"id", "taker_fee_ppm", "maker_fee_ppm", "liquidation_fee_ppm", "insurance_share_ppm", "refreshed_block"},
		}
		defs := map[string]string{
			"blocks.number":                  "bigint PRIMARY KEY CHECK (number >= 0)",
			"blocks.hash":                    "bytea NOT NULL UNIQUE CHECK (octet_length(hash) = 32)",
			"blocks.parent_hash":             "bytea NOT NULL CHECK (octet_length(parent_hash) = 32)",
			"blocks.time":                    "timestamptz NOT NULL",
			"blocks.proposer":                "text NOT NULL CHECK (proposer ~ '^[0-9A-F]{40}$')",
			"blocks.tx_count":                "integer NOT NULL CHECK (tx_count >= 0)",
			"cursors.name":                   "text PRIMARY KEY",
			"cursors.height":                 "bigint NOT NULL CHECK (height >= 0)",
			"cursors.hash":                   "bytea NOT NULL CHECK (octet_length(hash) = 32)",
			"cursors.updated_at":             "timestamptz NOT NULL DEFAULT now()",
			"writer_lease.name":              "text PRIMARY KEY",
			"markets.market_id":              "numeric(78,0) PRIMARY KEY CHECK (market_id = trunc(market_id) AND market_id >= 0)",
			"markets.symbol":                 "text NOT NULL UNIQUE",
			"markets.source":                 "text NOT NULL CHECK (source IN ('seed', 'chain'))",
			"markets.sz_decimals":            "smallint NOT NULL CHECK (sz_decimals >= 0)",
			"fee_config.id":                  "smallint PRIMARY KEY CHECK (id = 1)",
			"fee_config.taker_fee_ppm":       "numeric(78,0) NOT NULL CHECK (taker_fee_ppm = trunc(taker_fee_ppm) AND taker_fee_ppm >= 0)",
			"fee_config.insurance_share_ppm": "numeric(78,0) NOT NULL CHECK (insurance_share_ppm = trunc(insurance_share_ppm) AND insurance_share_ppm >= 0)",
		}
		for name, cols := range want {
			tb, ok := s.tables[name]
			if !ok {
				t.Errorf("chain table perps.%s missing", name)
				continue
			}
			if tb.file != factsFile {
				t.Errorf("perps.%s is created in %s, want %s", name, tb.file, factsFile)
			}
			if got := tb.columnNames(); !slices.Equal(got, cols) {
				t.Errorf("perps.%s columns\n got %v\nwant %v", name, got, cols)
			}
			if len(tb.constraints) != 0 {
				t.Errorf("perps.%s has table constraints %v, want keys inline", name, tb.constraints)
			}
			if got := s.indexSet(name); len(got) != 0 {
				t.Errorf("perps.%s has extra indexes %v", name, got)
			}
		}
		for key, def := range defs {
			tableName, colName, _ := strings.Cut(key, ".")
			tb, ok := s.tables[tableName]
			if !ok {
				continue
			}
			if c, _ := tb.column(colName); c.def != def {
				t.Errorf("perps.%s = %q, want %q", key, c.def, def)
			}
		}
		for _, tb := range s.tables {
			isFact := slices.ContainsFunc(factTables, func(ft factTable) bool { return ft.name == tb.name })
			if tb.file == factsFile && !isFact && want[tb.name] == nil {
				t.Errorf("%s creates perps.%s, which is neither a fact nor a chain table", factsFile, tb.name)
			}
		}
	})

	t.Run("projections_and_view", func(t *testing.T) {
		want := []struct {
			name    string
			columns []string
			keys    []string
			indexes []string
		}{
			{"positions_current", []string{"position_id", "account", "market_id", "is_long", "size_usd", "collateral_usd", "collateral_amount", "collateral_token", "entry_price", "leverage", "realized_pnl", "status", "opened_block", "closed_block", "take_profit_price", "stop_loss_price"},
				[]string{"PRIMARY KEY (position_id)"}, []string{"(account, status)", "(market_id, status)"}},
			{"orders", []string{"order_id", "account", "market_id", "order_type", "is_long", "trigger_price", "size_usd", "limit_price", "acceptable_price", "time_in_force", "reduce_only", "deadline", "cloid", "remaining_size_usd", "status", "position_id", "execution_price", "placed_block", "closed_block"},
				[]string{"PRIMARY KEY (order_id)"}, []string{"(account, status)", "(market_id, status)"}},
			{"spot_orders", []string{"order_id", "account", "kind", "order_type", "side", "token", "stablecoin", "amount", "target_price", "stop_price", "limit_price", "trigger_price", "expires_at", "status", "execution_price", "placed_block", "closed_block"},
				[]string{"PRIMARY KEY (kind, order_id)"}, []string{"(account, status)", "(token, status)"}},
			{"account_margin", []string{"account", "token", "balance", "locked", "margin_mode", "updated_block"}, []string{"PRIMARY KEY (account, token)"}, nil},
			{"user_stats", []string{"account", "trades_count", "volume_usd", "fees_usd", "funding_usd", "realized_pnl", "total_pnl", "win_count", "loss_count", "liquidations", "updated_block"}, []string{"PRIMARY KEY (account)"}, nil},
			{"user_pnl_daily", []string{"account", "day", "trading_pnl", "fees", "funding", "realized_pnl", "volume", "trades"}, []string{"PRIMARY KEY (account, day)"}, nil},
			{"market_stats_rolling", []string{"market_id", "volume_24h", "volume_7d", "volume_30d", "trades_count", "open_interest_long", "open_interest_short", "high_24h", "low_24h", "open_24h", "last_price", "updated_block"}, []string{"PRIMARY KEY (market_id)"}, nil},
			{"leaderboard", []string{"window", "rank", "account", "realized_pnl", "volume_usd", "trades_count", "win_rate", "avg_hold_time_seconds", "open_positions"}, []string{"PRIMARY KEY (window, rank)", "UNIQUE (window, account)"}, nil},
			{"spot_token_seen", []string{"token", "first_block"}, []string{"PRIMARY KEY (token)"}, nil},
			{"market_snapshots", []string{"market_id", "hour", "mark_price", "index_price", "funding_rate", "volume_usd", "trades_count"}, []string{"PRIMARY KEY (market_id, hour)"}, nil},
			{"protocol_snapshots", []string{"hour", "total_volume_usd", "total_oi_usd", "total_fees_usd", "tvl_usd", "insurance_total_usd", "accounts", "trades_count"}, []string{"PRIMARY KEY (hour)"}, nil},
			{"keeper_cycles", []string{"block", "block_time", "markets_updated", "orders_executed", "liquidations_executed", "orders_failed", "liquidations_failed"}, []string{"PRIMARY KEY (block)"}, nil},
		}
		created := 0
		for _, tb := range s.tables {
			if tb.file == projectsFile {
				created++
			}
		}
		if created != len(want) {
			t.Errorf("%s creates %d tables, want %d", projectsFile, created, len(want))
		}
		for _, w := range want {
			tb, ok := s.tables[w.name]
			if !ok {
				t.Errorf("projection perps.%s missing", w.name)
				continue
			}
			if tb.file != projectsFile {
				t.Errorf("perps.%s is created in %s, want %s", w.name, tb.file, projectsFile)
			}
			if got := tb.columnNames(); !slices.Equal(got, w.columns) {
				t.Errorf("perps.%s columns\n got %v\nwant %v", w.name, got, w.columns)
			}
			if !slices.Equal(tb.constraints, w.keys) {
				t.Errorf("perps.%s keys %v, want %v", w.name, tb.constraints, w.keys)
			}
			if got := s.indexSet(w.name); !slices.Equal(got, w.indexes) {
				t.Errorf("perps.%s indexes %v, want %v", w.name, got, w.indexes)
			}
			for _, c := range tb.columns {
				if strings.HasPrefix(c.def, "numeric(78,0)") && !strings.Contains(c.def, fmt.Sprintf("CHECK (%[1]s = trunc(%[1]s)", c.name)) {
					t.Errorf("perps.%s.%s amount column lacks the fraction check: %q", w.name, c.name, c.def)
				}
				if c.name == "account" && c.def != "bytea NOT NULL CHECK (octet_length(account) = 20)" {
					t.Errorf("perps.%s.account = %q", w.name, c.def)
				}
			}
		}
		if c, _ := s.tables["positions_current"].column("status"); c.def != "text NOT NULL CHECK (status IN ('open', 'closed', 'liquidated'))" {
			t.Errorf("perps.positions_current.status = %q", c.def)
		}
		if c, _ := s.tables["orders"].column("status"); c.def != "text NOT NULL CHECK (status IN ('active', 'triggered', 'filled', 'cancelled', 'expired'))" {
			t.Errorf("perps.orders.status = %q", c.def)
		}
		if c, _ := s.tables["leaderboard"].column("win_rate"); c.def != "numeric(9,4) NOT NULL CHECK (win_rate >= 0 AND win_rate <= 100)" {
			t.Errorf("perps.leaderboard.win_rate = %q", c.def)
		}

		view, ok := s.views["account_ledger"]
		if !ok {
			t.Fatalf("view perps.account_ledger missing")
		}
		branches := strings.Split(view, "\nUNION ALL\n")
		sources := []string{"collateral_events", "fees", "funding_payments", "trade_settlements"}
		if len(branches) != len(sources) {
			t.Fatalf("perps.account_ledger has %d branches, want %d", len(branches), len(sources))
		}
		entryTypes := []string{
			"CASE kind WHEN 'deposited' THEN 0 WHEN 'withdrawn' THEN 1 WHEN 'transferred_in' THEN 2 WHEN 'transferred_out' THEN 3 END::smallint AS entry_type",
			"4::smallint", "5::smallint", "6::smallint",
		}
		for i, b := range branches {
			if !strings.HasSuffix(b, "FROM perps."+sources[i]) {
				t.Errorf("account_ledger branch %d does not read perps.%s: %q", i, sources[i], b)
			}
			if !strings.Contains(b, entryTypes[i]) {
				t.Errorf("account_ledger branch %d lacks entry type %q", i, entryTypes[i])
			}
			if !strings.HasPrefix(b, "SELECT account, "+usdlByteaText) {
				t.Errorf("account_ledger branch %d does not start with account and the USDL token", i)
			}
			if !strings.Contains(b, "block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time") {
				t.Errorf("account_ledger branch %d lacks the common keys", i)
			}
		}
		if !strings.Contains(branches[0], "amount, 0::numeric(78,0) AS position_id, kind IN ('withdrawn', 'transferred_out') AS is_debit") {
			t.Errorf("account_ledger collateral branch columns differ: %q", branches[0])
		}
	})

	t.Run("no_candle_table", func(t *testing.T) {
		for name := range s.tables {
			if strings.Contains(name, "candle") || name == "oi_snapshots" {
				t.Errorf("table perps.%s must not exist", name)
			}
		}
		for name := range s.views {
			if strings.Contains(name, "candle") || name == "oi_snapshots" {
				t.Errorf("view perps.%s must not exist", name)
			}
		}
		if tb, ok := s.tables["open_interest"]; !ok || tb.file != factsFile {
			t.Errorf("open interest history must be the fact table perps.open_interest of %s", factsFile)
		}
	})
}
