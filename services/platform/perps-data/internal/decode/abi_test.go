package decode

import (
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

const repoRoot = "../../../../../"

type tableRow struct {
	name    string
	inputs  []string // "type [indexed] name"
	sources string
	table   string
	kinds   []string
}

// decodingTable reads the table of contracts/data-services.md section 6.
func decodingTable(t *testing.T) []tableRow {
	t.Helper()
	raw, err := os.ReadFile(repoRoot + "spec/release-v6-12-0/contracts/data-services.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## 6. Event decoding")
	end := strings.Index(doc, "## 7.")
	if start < 0 || end < start {
		t.Fatal("contracts/data-services.md has no section 6")
	}
	sig := regexp.MustCompile(`^(\w+)\s*\((.*)\)$`)
	var rows []tableRow
	for _, line := range strings.Split(doc[start:end], "\n") {
		cells := strings.Split(line, "|")
		if len(cells) != 7 || strings.HasPrefix(strings.TrimSpace(cells[1]), "---") || strings.HasPrefix(strings.TrimSpace(cells[1]), "event (") {
			continue
		}
		m := sig.FindStringSubmatch(strings.TrimSpace(cells[1]))
		if m == nil {
			t.Fatalf("row %q has no signature", line)
		}
		row := tableRow{name: m[1], sources: strings.TrimSpace(cells[2]), table: strings.TrimSpace(cells[3])}
		for _, k := range strings.Split(cells[4], ",") {
			row.kinds = append(row.kinds, strings.TrimSpace(k))
		}
		for _, in := range strings.Split(m[2], ",") {
			if in = strings.Join(strings.Fields(strings.ReplaceAll(in, "`", "")), " "); in != "" {
				row.inputs = append(row.inputs, in)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("section 6 holds no rows")
	}
	return rows
}

func signature(ev abi.Event) []string {
	var out []string
	for _, in := range ev.Inputs {
		s := in.Type.String()
		if in.Indexed {
			s += " indexed"
		}
		out = append(out, s+" "+in.Name)
	}
	return out
}

// tableColumns reads the columns of every perps table of the fact migration.
func tableColumns(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(repoRoot + "services/platform/perps-data/migrations/0001_facts.sql")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]bool{}
	var cols map[string]bool
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "CREATE TABLE perps."); ok {
			cols = map[string]bool{}
			out[strings.TrimSuffix(name, " (")] = cols
			continue
		}
		if cols == nil || line == "" {
			continue
		}
		if strings.HasPrefix(line, ");") {
			cols = nil
			continue
		}
		f := strings.Fields(line)[0]
		if f == "PRIMARY" || f == "CHECK" || f == "UNIQUE" || f == "FOREIGN" || f == "CONSTRAINT" {
			continue
		}
		cols[strings.Trim(f, `"`)] = true
	}
	return out
}

func rowColumns(typ reflect.Type) []string {
	var out []string
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Anonymous {
			out = append(out, rowColumns(f.Type)...)
			continue
		}
		out = append(out, ColumnName(f.Name))
	}
	return out
}

// flatten lists the static words of an ABI output type.
func flatten(typ abi.Type) []abi.Type {
	if typ.T != abi.TupleTy {
		return []abi.Type{typ}
	}
	var out []abi.Type
	for _, e := range typ.TupleElems {
		out = append(out, flatten(*e)...)
	}
	return out
}

func TestDecoderColumnsMatchABI(t *testing.T) {
	m, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(m, Options{PerpsModule: ModuleAddress("perps"), SpotModule: ModuleAddress("spot"), PricefeedModule: ModuleAddress("pricefeed")})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[[2]string]bool{}
	for _, k := range KindRows() {
		kinds[[2]string{k.Table, k.Kind}] = true
	}

	t.Run("table_matches_abi", func(t *testing.T) {
		covered := map[key]bool{}
		shareCovered := map[common.Hash]bool{}
		feeCovered := map[common.Hash]bool{}
		for _, row := range decodingTable(t) {
			for _, k := range row.kinds {
				if !kinds[[2]string{row.table, k}] {
					t.Errorf("%s: no decoder row type for %s/%s", row.name, row.table, k)
				}
			}
			if row.sources == "Cosmos block event" {
				continue
			}
			var addresses []common.Address
			share, fee := false, false
			for _, s := range strings.Split(row.sources, ",") {
				switch s = strings.TrimSpace(s); {
				case s == "share pointers":
					share = true
				case s == "any of 0x1021 to 0x1025 by topic0" || s == "any of 0x1021 to 0x1025":
					fee = true
				case s == "by topic0":
				case strings.HasPrefix(s, "USDL "):
					addresses = append(addresses, common.HexToAddress(strings.TrimPrefix(s, "USDL ")))
				case strings.HasPrefix(s, "0x10"):
					addresses = append(addresses, common.HexToAddress(s))
				default:
					t.Fatalf("%s: unknown source %q", row.name, s)
				}
			}
			check := func(where string, e entry) {
				if e.event.Name != row.name || !slices.Equal(signature(e.event), row.inputs) {
					t.Errorf("%s at %s: decoder %s(%s), table %s(%s)", row.name, where, e.event.Name, strings.Join(signature(e.event), ", "), row.name, strings.Join(row.inputs, ", "))
				}
			}
			for _, a := range addresses {
				found := false
				for k, e := range r.entries {
					if k.address == a && e.event.Name == row.name {
						check(a.Hex(), e)
						covered[k], found = true, true
					}
				}
				if !found {
					t.Errorf("%s: no decoder at %s", row.name, a.Hex())
				}
			}
			if fee {
				found := false
				for topic, e := range r.fees {
					if e.event.Name == row.name {
						check("any of 0x1021 to 0x1025", e)
						feeCovered[topic], found = true, true
					}
				}
				if !found {
					t.Errorf("%s: no fee decoder", row.name)
				}
			}
			if share {
				found := false
				for topic, e := range r.share {
					if e.event.Name == row.name {
						check("share pointer", e)
						shareCovered[topic], found = true, true
					}
				}
				if !found {
					t.Errorf("%s: no share pointer decoder", row.name)
				}
			}
		}
		for k, e := range r.entries {
			if !covered[k] {
				t.Errorf("decoder %s at %s has no table row", e.event.Name, k.address.Hex())
			}
		}
		for topic, e := range r.share {
			if !shareCovered[topic] {
				t.Errorf("share decoder %s has no table row", e.event.Name)
			}
		}
		for topic, e := range r.fees {
			if !feeCovered[topic] {
				t.Errorf("fee decoder %s has no table row", e.event.Name)
			}
		}
	})

	t.Run("selectors_match_abi", func(t *testing.T) {
		cases := []struct {
			load     func() (abi.ABI, error)
			method   string
			selector []byte
			address  common.Address
			want     common.Address
		}{
			{spotabi.Orders, "getLimitOrder", limitRail.selector, limitRail.address, SpotOrdersAddress},
			{spotabi.StopOrders, "getStopLimitOrder", stopLimitRail.selector, stopLimitRail.address, SpotStopAddress},
			{spotabi.StopOrders, "getStopLossOrder", stopLossRail.selector, stopLossRail.address, SpotStopAddress},
			{spotabi.Vault, "getRegisteredTokens", getRegisteredTokensSelector, SpotVaultAddress, SpotVaultAddress},
			{spotabi.Vault, "shareTokens", shareTokensSelector, SpotVaultAddress, SpotVaultAddress},
		}
		for _, c := range cases {
			a, err := c.load()
			if err != nil {
				t.Fatal(err)
			}
			m, ok := a.Methods[c.method]
			if !ok || !slices.Equal(m.ID, c.selector) || c.address != c.want {
				t.Errorf("%s: selector %x at %s, ABI %x", c.method, c.selector, c.address.Hex(), m.ID)
			}
		}
		for _, rail := range []orderRail{limitRail, stopLimitRail, stopLossRail} {
			load := spotabi.Orders
			if rail.address == SpotStopAddress {
				load = spotabi.StopOrders
			}
			a, err := load()
			if err != nil {
				t.Fatal(err)
			}
			words := 0
			for _, o := range a.Methods[rail.name].Outputs {
				words += len(flatten(o.Type))
			}
			if words != rail.words {
				t.Errorf("%s returns %d words, rail reads %d", rail.name, words, rail.words)
			}
		}
	})

	t.Run("columns_match_migration", func(t *testing.T) {
		tables := tableColumns(t)
		for _, k := range KindRows() {
			cols, ok := tables[k.Table]
			if !ok {
				t.Errorf("%s: no table in 0001_facts.sql", k.Table)
				continue
			}
			if k.Row.Table() != k.Table {
				t.Errorf("%s/%s: row type writes %s", k.Table, k.Kind, k.Row.Table())
			}
			for _, c := range rowColumns(reflect.TypeOf(k.Row)) {
				if !cols[c] {
					t.Errorf("%s/%s: column %s not in perps.%s", k.Table, k.Kind, c, k.Table)
				}
			}
		}
	})

	t.Run("leaf_imports_only", func(t *testing.T) {
		out, err := exec.Command("go", "list", "-deps", ".").Output()
		if err != nil {
			t.Fatal(err)
		}
		allowed := []string{"precompiles/perps/abi", "precompiles/pricefeed/abi", "precompiles/spot/abi"}
		for _, pkg := range strings.Fields(string(out)) {
			_, rest, ok := strings.Cut(pkg, "/Paxeer-X-Network/")
			if !ok {
				continue
			}
			if strings.HasPrefix(rest, "modules/lxphost") || (strings.HasPrefix(rest, "precompiles/") && !slices.Contains(allowed, rest)) {
				t.Errorf("decode depends on %s", pkg)
			}
		}
	})
}
