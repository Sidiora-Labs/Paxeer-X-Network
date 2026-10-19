package graphql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/graph-gophers/graphql-go/relay"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/pnl"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

const operationsDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"

var (
	opTrader     = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	opCounter    = common.HexToAddress("0x0000000000000000000000000000000000000b0b")
	opPAX        = common.HexToAddress("0x00000000000000000000000000000000000000fa")
	opSpotLimit  = common.HexToAddress("0x0000000000000000000000000000000000001022")
	opSpotVault  = common.HexToAddress("0x0000000000000000000000000000000000001020")
	opIndexFeed  = common.HexToAddress("0x00000000000000000000000000000000000000fe")
	opTraderHex  = strings.ToLower(opTrader.Hex())
	opPAXHex     = strings.ToLower(opPAX.Hex())
	opOperations = []string{
		"GetPositions", "GetClosedPositions", "GetTrades", "GetOrders", "GetMarketInfo", "GetMarkets", "GetGlobalStats",
		"GetUserStats", "GetUserVault", "GetFilledOrders", "GetAllOrders", "GetLiquidations", "GetLatestPrices",
		"GetVaultEvents", "GetAccountLedger", "GetPositionLedger", "GetDelegates", "GetTradingAccountEvents",
		"GetKeeperCycles", "GetTPSLOrders", "DashboardV2", "GetAllOrdersV2", "GetLeaderboard", "GetFees",
		"GetTradeSettlements", "GetOiSnapshots", "GetMarkPriceHistory", "GetVaultBalanceHistory", "GetFundingPayments",
		"GetMarketSnapshots", "GetProtocolSnapshots", "GetEnrichedGlobalStats", "GetEnrichedMarketStats",
		"GetOrderlyTickers", "GetOrderlyFundingRates", "GetOrderlyPriceChanges", "GetOrderlyOpenInterests",
		"GetSpotMarkets", "GetSpotMarket", "GetSpotOrders", "GetSpotOrder", "GetSpotOrderFills", "GetSpotBatchClearings",
		"GetSpotEpochSettlements", "GetSpotFastSettlements", "GetSpotCollateralMovements", "GetSpotMarketModeChanges",
		"GetSpotPofqUpdates", "GetSpotPLVs", "GetSpotKeeperCycles", "GetSpotBatchClearFailures",
	}
	opPerfdb = []string{"User", "UserBalances", "UserDailyPnl", "UserStats", "UserAlerts", "UserReferrals", "ReferralTiers",
		"Competitions", "Competition"}
	opOrigins = []string{
		"Hyperpax src/hooks/perps/useSubgraph.ts", "Hyperpax src/hooks/perps/usePerfdbUser.ts",
		"Hyperpax src/hooks/perps/usePerfdbBalances.ts", "Hyperpax src/hooks/perps/useDailyPnl.ts",
		"Hyperpax src/hooks/perps/usePerfdbTraderStats.ts", "Hyperpax src/hooks/perps/usePerfdbAlerts.ts",
		"Hyperpax src/hooks/perps/usePerfdbReferrals.ts", "Hyperpax src/hooks/perps/usePerfdbCompetitions.ts",
	}
	operationName = regexp.MustCompile(`^\s*query\s+(\w+)`)
	operationHead = regexp.MustCompile(`(?m)^\s*query\s`)
)

// operationCase is one document with the variables the application sends,
// the roots the seed makes non-empty and the roots that must answer [].
type operationCase struct {
	vars     map[string]any
	nonEmpty []string
	empty    []string
}

func operationCases(competitionID string) map[string]operationCase {
	user := map[string]any{"userAddress": opTraderHex}
	page := map[string]any{"userAddress": opTraderHex, "limit": 50, "offset": 0}
	market := map[string]any{"marketId": 0}
	address := map[string]any{"address": opTraderHex}
	none := map[string]any{}
	return map[string]operationCase{
		"GetPositions":               {user, []string{"positions"}, nil},
		"GetClosedPositions":         {page, []string{"positions"}, nil},
		"GetTrades":                  {page, []string{"trades"}, nil},
		"GetOrders":                  {user, []string{"orders"}, nil},
		"GetMarketInfo":              {market, []string{"market", "marketStats"}, nil},
		"GetMarkets":                 {none, []string{"markets"}, nil},
		"GetGlobalStats":             {none, []string{"globalStats"}, nil},
		"GetUserStats":               {user, []string{"userStats"}, nil},
		"GetUserVault":               {user, []string{"userVault"}, nil},
		"GetFilledOrders":            {page, []string{"orders"}, nil},
		"GetAllOrders":               {page, []string{"orders"}, nil},
		"GetLiquidations":            {page, []string{"liquidations"}, nil},
		"GetLatestPrices":            {none, []string{"latestPrices"}, nil},
		"GetVaultEvents":             {page, []string{"vaultEvents"}, nil},
		"GetAccountLedger":           {page, []string{"accountLedger"}, nil},
		"GetPositionLedger":          {map[string]any{"positionId": "1", "limit": 50}, []string{"accountLedger"}, nil},
		"GetDelegates":               {user, nil, []string{"delegates"}},
		"GetTradingAccountEvents":    {page, []string{"tradingAccountEvents"}, nil},
		"GetKeeperCycles":            {map[string]any{"limit": 50}, []string{"keeperCycles"}, nil},
		"GetTPSLOrders":              {user, []string{"takeProfits", "stopLosses"}, nil},
		"DashboardV2":                {user, []string{"userVault", "positions", "orders", "userStats"}, []string{"delegates"}},
		"GetAllOrdersV2":             {map[string]any{"userAddress": opTraderHex, "limit": 50, "offset": 0}, []string{"orders"}, nil},
		"GetLeaderboard":             {map[string]any{"limit": 50, "offset": 0}, []string{"leaderboard"}, nil},
		"GetFees":                    {user, []string{"fees"}, nil},
		"GetTradeSettlements":        {user, []string{"tradeSettlements"}, nil},
		"GetOiSnapshots":             {market, []string{"oiSnapshots"}, nil},
		"GetMarkPriceHistory":        {market, []string{"markPriceHistory"}, nil},
		"GetVaultBalanceHistory":     {none, []string{"vaultBalanceHistory"}, nil},
		"GetFundingPayments":         {user, []string{"fundingPayments"}, nil},
		"GetMarketSnapshots":         {market, []string{"marketSnapshots"}, nil},
		"GetProtocolSnapshots":       {none, []string{"protocolSnapshots"}, nil},
		"GetEnrichedGlobalStats":     {none, []string{"enrichedGlobalStats"}, nil},
		"GetEnrichedMarketStats":     {market, []string{"enrichedMarketStats"}, nil},
		"GetOrderlyTickers":          {none, []string{"orderlyTickers"}, nil},
		"GetOrderlyFundingRates":     {none, []string{"orderlyFundingRates"}, nil},
		"GetOrderlyPriceChanges":     {none, []string{"orderlyPriceChanges"}, nil},
		"GetOrderlyOpenInterests":    {none, []string{"orderlyOpenInterests"}, nil},
		"GetSpotMarkets":             {none, []string{"spotMarkets"}, nil},
		"GetSpotMarket":              {map[string]any{"marketId": opPAXHex}, []string{"spotMarket"}, nil},
		"GetSpotOrders":              {map[string]any{"traderAddress": opTraderHex}, []string{"spotOrders"}, nil},
		"GetSpotOrder":               {map[string]any{"orderId": "20"}, []string{"spotOrder"}, nil},
		"GetSpotOrderFills":          {map[string]any{"traderAddress": opTraderHex}, []string{"spotOrderFills"}, nil},
		"GetSpotBatchClearings":      {none, nil, []string{"spotBatchClearings"}},
		"GetSpotEpochSettlements":    {none, nil, []string{"spotEpochSettlements"}},
		"GetSpotFastSettlements":     {none, nil, []string{"spotFastSettlements"}},
		"GetSpotCollateralMovements": {map[string]any{"userAddress": opTraderHex}, []string{"spotCollateralMovements"}, nil},
		"GetSpotMarketModeChanges":   {none, nil, []string{"spotMarketModeChanges"}},
		"GetSpotPofqUpdates":         {none, nil, []string{"spotPofqUpdates"}},
		"GetSpotPLVs":                {none, nil, []string{"spotPLVs"}},
		"GetSpotKeeperCycles":        {none, nil, []string{"spotKeeperCycles"}},
		"GetSpotBatchClearFailures":  {none, nil, []string{"spotBatchClearFailures"}},
		"User":                       {address, []string{"user"}, nil},
		"UserBalances":               {address, []string{"userBalances"}, nil},
		"UserDailyPnl":               {map[string]any{"address": opTraderHex, "limit": 90}, []string{"userDailyPnl"}, nil},
		"UserStats":                  {address, []string{"userStats"}, nil},
		"UserAlerts":                 {address, []string{"userAlerts"}, nil},
		"UserReferrals":              {address, []string{"userReferrals", "referralTiers"}, nil},
		"ReferralTiers":              {none, []string{"referralTiers"}, nil},
		"Competitions":               {none, []string{"competitions"}, nil},
		"Competition":                {map[string]any{"id": competitionID}, []string{"competition"}, nil},
	}
}

func TestOperations(t *testing.T) {
	documents, order := loadOperations(t)
	if want := append(slices.Clone(opOperations), opPerfdb...); !slices.Equal(order, want) {
		t.Fatalf("testdata operations = %v, want %v", order, want)
	}
	db := operationsDB(t)
	competitionID := seedOperations(t, db)
	schema, err := NewSchema(db)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	handler := &relay.Handler{Schema: schema}
	mux := http.NewServeMux()
	mux.Handle("POST /graphql", handler)
	mux.Handle("POST /api/subgraph", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cases := operationCases(competitionID)
	for _, name := range order {
		tc, ok := cases[name]
		if !ok {
			t.Fatalf("no variables for operation %s", name)
		}
		subtest := name
		if slices.Contains(opPerfdb, name) {
			subtest = "perfdb_" + name
		}
		t.Run(subtest, func(t *testing.T) {
			document := documents[name]
			selected, err := parseSelection(document)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			data := postOperation(t, server.URL+"/graphql", document, tc.vars)
			assertSelected(t, name, selected, data)
			for _, root := range tc.nonEmpty {
				switch v := data[root].(type) {
				case nil:
					t.Errorf("%s.%s is null, want the seeded rows", name, root)
				case []any:
					if len(v) == 0 {
						t.Errorf("%s.%s is [], want the seeded rows", name, root)
					}
				}
			}
			for _, root := range tc.empty {
				if v, ok := data[root].([]any); !ok || len(v) != 0 {
					t.Errorf("%s.%s = %v, want []", name, root, data[root])
				}
			}
		})
	}

	t.Run("tpsl_aliases", func(t *testing.T) {
		data := postOperation(t, server.URL+"/graphql", documents["GetTPSLOrders"], cases["GetTPSLOrders"].vars)
		for alias, want := range map[string]string{"takeProfits": "10", "stopLosses": "11"} {
			rows, ok := data[alias].([]any)
			if !ok || len(rows) != 1 {
				t.Fatalf("%s = %v, want one active order", alias, data[alias])
			}
			row := rows[0].(map[string]any)
			if got := scalar(row["orderId"]); got != want {
				t.Errorf("%s orderId = %s, want %s", alias, got, want)
			}
			if positionID, ok := row["positionId"]; !ok || positionID != nil {
				t.Errorf("%s positionId = %v (present %t), want null until OrderExecuted", alias, positionID, ok)
			}
			if got := scalar(row["marketId"]); got != "0" {
				t.Errorf("%s marketId = %s, want 0", alias, got)
			}
		}
	})

	t.Run("dashboard_margin_mode", func(t *testing.T) {
		data := postOperation(t, server.URL+"/graphql", documents["DashboardV2"], cases["DashboardV2"].vars)
		vault, ok := data["userVault"].(map[string]any)
		if !ok {
			t.Fatalf("userVault = %v, want an object", data["userVault"])
		}
		if got := scalar(vault["marginMode"]); got != "0" {
			t.Errorf("userVault.marginMode = %s, want 0", got)
		}
	})

	t.Run("alias_and_errors", func(t *testing.T) {
		vars := cases["GetMarkets"].vars
		status, viaGraphQL := postRaw(t, server.URL+"/graphql", documents["GetMarkets"], vars)
		if status != http.StatusOK {
			t.Fatalf("/graphql status %d", status)
		}
		status, viaAlias := postRaw(t, server.URL+"/api/subgraph", documents["GetMarkets"], vars)
		if status != http.StatusOK {
			t.Fatalf("/api/subgraph status %d", status)
		}
		if !bytes.Equal(viaGraphQL, viaAlias) {
			t.Errorf("/api/subgraph answered %s, /graphql answered %s", viaAlias, viaGraphQL)
		}
		refusals := []struct {
			document string
			names    string
		}{
			{`query Refused { nosuch }`, `"nosuch"`},
			{`query RefusedField($userAddress: String!) { positions(userAddress: $userAddress) { positionId nosuchField } }`, `"nosuchField"`},
		}
		for _, endpoint := range []string{"/api/subgraph", "/graphql"} {
			for _, r := range refusals {
				status, body := postRaw(t, server.URL+endpoint, r.document, map[string]any{"userAddress": opTraderHex})
				if status != http.StatusOK {
					t.Fatalf("%s refused document status %d, want 200: %s", endpoint, status, body)
				}
				var res operationResponse
				if err := json.Unmarshal(body, &res); err != nil {
					t.Fatalf("decode %s: %v", body, err)
				}
				if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, r.names) {
					t.Errorf("%s errors = %+v, want errors[0].message naming %s", endpoint, res.Errors, r.names)
				}
				if len(res.Data) != 0 && string(res.Data) != "null" {
					t.Errorf("%s refused document answered data %s", endpoint, res.Data)
				}
			}
		}
		status, body := postRaw(t, server.URL+"/api/subgraph",
			`query Both($a: String) { userStats(userAddress: $a, address: $a) { address } }`, map[string]any{"a": opTraderHex})
		if status != http.StatusOK {
			t.Fatalf("resolver refusal status %d, want 200: %s", status, body)
		}
		var res operationResponse
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if len(res.Errors) != 1 || res.Errors[0].Message != "exactly one of userAddress or address is required" {
			t.Errorf("resolver refusal errors = %+v", res.Errors)
		}
	})
}

// loadOperations splits testdata/operations.graphql into its documents keyed
// by operation name, in file order, and checks the origin lines.
func loadOperations(t *testing.T) (map[string]string, []string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/operations.graphql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	var origins []string
	for _, line := range strings.Split(text, "\n") {
		if origin, ok := strings.CutPrefix(line, "# origin: "); ok {
			origins = append(origins, origin)
		}
	}
	if !slices.Equal(origins, opOrigins) {
		t.Fatalf("origins = %v, want %v", origins, opOrigins)
	}
	starts := operationHead.FindAllStringIndex(text, -1)
	documents := make(map[string]string, len(starts))
	order := make([]string, 0, len(starts))
	for i, s := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		document := text[s[0]:end]
		m := operationName.FindStringSubmatch(document)
		if m == nil {
			t.Fatalf("unnamed operation at offset %d", s[0])
		}
		if _, dup := documents[m[1]]; dup {
			t.Fatalf("duplicate operation %s", m[1])
		}
		documents[m[1]] = document
		order = append(order, m[1])
	}
	return documents, order
}

type operationResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func postRaw(t *testing.T, url, document string, vars map[string]any) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": document, "variables": vars})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode == http.StatusOK && ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	return resp.StatusCode, out
}

// postOperation posts document the way the application client does and
// returns the data object of an answer without errors.
func postOperation(t *testing.T, url, document string, vars map[string]any) map[string]any {
	t.Helper()
	status, body := postRaw(t, url, document, vars)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	var res operationResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	dec := json.NewDecoder(bytes.NewReader(res.Data))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil || data == nil {
		t.Fatalf("data %s: %v", res.Data, err)
	}
	return data
}

// scalar renders a JSON scalar decoded with UseNumber as text.
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

// selection is the response-key tree of a selection set; a leaf is nil.
type selection map[string]selection

// parseSelection reads the selection set of a single operation without
// fragments, keyed by alias when one is given.
func parseSelection(document string) (selection, error) {
	toks := tokenize(document)
	depth := 0
	for i, tok := range toks {
		switch tok {
		case "(":
			depth++
		case ")":
			depth--
		case "{":
			if depth == 0 {
				sel, next, err := parseSet(toks, i)
				if err != nil {
					return nil, err
				}
				if next != len(toks) {
					return nil, fmt.Errorf("trailing tokens %v", toks[next:])
				}
				return sel, nil
			}
		}
	}
	return nil, fmt.Errorf("no selection set")
}

func parseSet(toks []string, i int) (selection, int, error) {
	sel := selection{}
	i++
	for i < len(toks) && toks[i] != "}" {
		key := toks[i]
		i++
		if i < len(toks) && toks[i] == ":" {
			i += 2
		}
		if i < len(toks) && toks[i] == "(" {
			for depth := 0; i < len(toks); i++ {
				if toks[i] == "(" {
					depth++
				} else if toks[i] == ")" {
					if depth--; depth == 0 {
						break
					}
				}
			}
			i++
		}
		var child selection
		if i < len(toks) && toks[i] == "{" {
			var err error
			if child, i, err = parseSet(toks, i); err != nil {
				return nil, 0, err
			}
		}
		if _, dup := sel[key]; dup {
			return nil, 0, fmt.Errorf("duplicate response key %s", key)
		}
		sel[key] = child
	}
	if i >= len(toks) {
		return nil, 0, fmt.Errorf("unterminated selection set")
	}
	return sel, i + 1, nil
}

func tokenize(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			i++
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			toks = append(toks, s[i:min(j+1, len(s))])
			i = j + 1
		case strings.IndexByte("{}():!$[]=@", c) >= 0:
			toks = append(toks, string(c))
			i++
		default:
			j := i
			for j < len(s) && (('0' <= s[j] && s[j] <= '9') || ('a' <= s[j] && s[j] <= 'z') || ('A' <= s[j] && s[j] <= 'Z') ||
				s[j] == '_' || s[j] == '.' || s[j] == '-') {
				j++
			}
			if j == i {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks
}

// assertSelected checks that every object of v carries exactly the response
// keys of sel, recursively through lists; null stands for a nullable field.
func assertSelected(t *testing.T, path string, sel selection, v any) {
	t.Helper()
	switch x := v.(type) {
	case nil:
	case []any:
		for i, e := range x {
			assertSelected(t, fmt.Sprintf("%s[%d]", path, i), sel, e)
		}
	case map[string]any:
		if sel == nil {
			t.Errorf("%s is an object, want a scalar", path)
			return
		}
		got, want := slices.Sorted(maps.Keys(x)), slices.Sorted(maps.Keys(sel))
		if !slices.Equal(got, want) {
			t.Errorf("%s fields = %v, want %v", path, got, want)
		}
		for key, child := range sel {
			assertSelected(t, path+"."+key, child, x[key])
		}
	default:
		if sel != nil {
			t.Errorf("%s = %v, want an object", path, v)
		}
	}
}

func operationsDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(operationsDatabaseEnv)
	if url == "" {
		t.Fatal(operationsDatabaseEnv + " is unset")
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

// opSeed writes the fact rows of consecutive blocks the way the indexer
// stores them and keeps the decoded rows of the current block for
// store.Project.
type opSeed struct {
	t       *testing.T
	ctx     context.Context
	tx      *sql.Tx
	block   uint64
	at      time.Time
	txIndex int64
	log     int64
	rows    []decode.Row
}

func (s *opSeed) next(n uint64, at time.Time) {
	s.t.Helper()
	s.block, s.at, s.txIndex, s.log, s.rows = n, at.UTC(), 0, 0, nil
	hash := crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n)))
	parent := crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n-1)))
	s.exec(`INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, 1)`,
		int64(n), hash.Bytes(), parent.Bytes(), s.at, strings.Repeat("AB", 20))
}

func (s *opSeed) endBlock() { s.txIndex = 1 }

func (s *opSeed) fact(kind string, account, emitter common.Address, sub int64) decode.Fact {
	rk := decode.ReceiptPerpsEndBlock
	var from *common.Address
	if s.txIndex == 0 {
		rk = decode.ReceiptTx
		sender := account
		from = &sender
	}
	f := decode.Fact{
		Block:       s.block,
		BlockHash:   crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", s.block))),
		TxIndex:     s.txIndex,
		LogIndex:    s.log,
		Sub:         sub,
		TxHash:      crypto.Keccak256Hash([]byte(fmt.Sprintf("tx/%d/%d", s.block, s.txIndex))),
		ReceiptKind: rk,
		BlockTime:   s.at,
		Emitter:     emitter,
		Kind:        kind,
		Account:     account,
		TxFrom:      from,
	}
	if sub == 0 {
		s.log++
	}
	return f
}

func (s *opSeed) exec(query string, args ...any) {
	s.t.Helper()
	if _, err := s.tx.ExecContext(s.ctx, query, args...); err != nil {
		s.t.Fatalf("%s: %v", query, err)
	}
}

func (s *opSeed) insert(table string, f decode.Fact, cols string, vals ...any) {
	s.t.Helper()
	var from any
	if f.TxFrom != nil {
		from = f.TxFrom.Bytes()
	}
	args := append([]any{int64(f.Block), f.BlockHash.Bytes(), f.TxIndex, f.LogIndex, f.Sub, f.TxHash.Bytes(), string(f.ReceiptKind),
		f.BlockTime, f.Emitter.Bytes(), f.Kind, f.Account.Bytes(), from}, vals...)
	marks := make([]string, len(args))
	for i := range marks {
		marks[i] = fmt.Sprintf("$%d", i+1)
	}
	s.exec(`INSERT INTO perps.`+table+` (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind,
    account, tx_from, `+cols+`) VALUES (`+strings.Join(marks, ", ")+`)`, args...)
}

// put writes a decoded row into its fact table and, as store.WriteFacts does
// for each decoded log, its perps.protocol_events row.
func (s *opSeed) put(row decode.Row, cols string, vals ...any) {
	s.t.Helper()
	f := row.Common()
	s.insert(row.Table(), f, cols, vals...)
	if f.Sub == 0 {
		s.insert("protocol_events", f, "event_name, args", strings.TrimSuffix(reflect.TypeOf(row).Name(), "Row"), "{}")
	}
	s.rows = append(s.rows, row)
}

func (s *opSeed) project() {
	s.t.Helper()
	if err := store.Project(s.ctx, s.tx, s.rows); err != nil {
		s.t.Fatalf("Project block %d: %v", s.block, err)
	}
	s.rows = nil
}

func (s *opSeed) opened(id string, a common.Address, size, entry, collateral string) {
	f := s.fact("opened", a, decode.PerpsAddress, 0)
	s.put(decode.PositionOpenedRow{Fact: f, PositionID: id, User: a, MarketID: "0", IsLong: true, SizeUSD: size,
		Leverage: "10000000000000000000", EntryPrice: entry, CollateralToken: store.USDL, CollateralAmount: collateral},
		`position_id, "user", market_id, is_long, size_usd, leverage, entry_price, collateral_token, collateral_amount`,
		id, a.Bytes(), "0", true, size, "10000000000000000000", entry, store.USDL.Bytes(), collateral)
}

func (s *opSeed) closed(id string, a common.Address, size, pnl string) {
	f := s.fact("closed", a, decode.PerpsAddress, 0)
	s.put(decode.PositionClosedRow{Fact: f, PositionID: id, User: a, MarketID: "0", ClosedSizeUSD: size,
		ExitPrice: "61000000000000000000000", RealizedPnl: pnl, IsFullClose: true},
		`position_id, "user", market_id, closed_size_usd, exit_price, realized_pnl, is_full_close`,
		id, a.Bytes(), "0", size, "61000000000000000000000", pnl, true)
}

func (s *opSeed) settled(id string, a common.Address, tradeType uint8, gross, payout string) {
	f := s.fact("settled", a, decode.PerpsAddress, 0)
	s.put(decode.TradeSettledRow{Fact: f, PositionID: id, User: a, MarketID: "0", TradeType: tradeType,
		SizeUSD: "1000000000000000000000", ExecutionPrice: "60000000000000000000000", GrossPnl: gross,
		TotalFeesUSD: "500000000000000000", FundingPaidUSD: "0", NetPayoutTokens: payout},
		`position_id, "user", market_id, trade_type, size_usd, execution_price, gross_pnl, total_fees_usd, funding_paid_usd, net_payout_tokens`,
		id, a.Bytes(), "0", int16(tradeType), "1000000000000000000000", "60000000000000000000000", gross, "500000000000000000", "0", payout)
}

func (s *opSeed) placed(id string, a common.Address, orderType uint8, trigger string) {
	f := s.fact("placed", a, decode.PerpsAddress, 0)
	s.put(decode.OrderPlacedRow{Fact: f, OrderID: id, User: a, MarketID: "0", OrderType: orderType, IsLong: false,
		TriggerPrice: trigger, SizeUSD: "1000000000000000000000"},
		`order_id, "user", market_id, order_type, is_long, trigger_price, size_usd`,
		id, a.Bytes(), "0", int16(orderType), false, trigger, "1000000000000000000000")
}

// seedOperations writes two blocks of perps and spot facts for opTrader,
// folds them through store.Project, the hourly snapshot and the leaderboard,
// and fills the account tables; it returns the competition id.
func seedOperations(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				t.Error(err)
			}
		}
	})
	hour := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	t0, t1 := hour.Add(-30*time.Minute), hour.Add(5*time.Minute)
	s := &opSeed{t: t, ctx: ctx, tx: tx}

	s.next(100, t0)
	f := s.fact("created", opTrader, decode.PerpsAddress, 0)
	s.put(decode.VaultCreatedRow{Fact: f, User: opTrader, Vault: decode.PerpsAddress}, `"user", vault`, opTrader.Bytes(), decode.PerpsAddress.Bytes())
	f = s.fact("deposited", opTrader, decode.PerpsAddress, 0)
	s.put(decode.CollateralDepositedRow{Fact: f, AccountArg: opTrader, Amount: "5000000000", NewBalance: "5000000000"},
		"account_arg, amount, new_balance", opTrader.Bytes(), "5000000000", "5000000000")
	s.opened("1", opTrader, "1000000000000000000000", "60000000000000000000000", "100000000")
	f = s.fact("tpsl_set", opTrader, decode.PerpsAddress, 0)
	s.put(decode.PositionTpSlSetRow{Fact: f, PositionID: "1", TakeProfitPrice: "66000000000000000000000", StopLossPrice: "57000000000000000000000"},
		"position_id, take_profit_price, stop_loss_price", "1", "66000000000000000000000", "57000000000000000000000")
	s.settled("1", opTrader, decode.TradeOpen, "0", "0")
	f = s.fact("collected", opTrader, decode.PerpsAddress, 0)
	s.put(decode.FeeCollectedRow{Fact: f, PositionID: "1", User: opTrader, MarketID: "0", FeeType: decode.FeeTrading,
		FeeUSD: "500000000000000000", FeeTokens: "500000", Token: store.USDL},
		`position_id, "user", market_id, fee_type, fee_usd, fee_tokens, token`,
		"1", opTrader.Bytes(), "0", int16(decode.FeeTrading), "500000000000000000", "500000", store.USDL.Bytes())
	s.opened("2", opTrader, "1000000000000000000000", "60000000000000000000000", "100000000")
	s.opened("3", opTrader, "1000000000000000000000", "60000000000000000000000", "100000000")
	s.placed("10", opTrader, 2, "66000000000000000000000")
	s.placed("11", opTrader, 3, "57000000000000000000000")
	s.placed("12", opTrader, 0, "0")
	f = s.fact("token_registered", opSpotVault, opSpotVault, 0)
	s.insert("spot_token_registry", f, "token, decimals, is_stablecoin", opPAX.Bytes(), int16(18), false)
	f = s.fact("token_registered", opSpotVault, opSpotVault, 0)
	s.insert("spot_token_registry", f, "token, decimals, is_stablecoin", store.USDL.Bytes(), int16(6), true)
	f = s.fact("limit_created", opTrader, opSpotLimit, 0)
	s.insert("spot_order_events", f, `order_id, "user", order_type, amount, target_price, token, stablecoin`,
		"20", opTrader.Bytes(), int16(1), "700", "60", opPAX.Bytes(), store.USDL.Bytes())
	f = s.fact("deposit", opTrader, opSpotVault, 0)
	s.insert("spot_vault_events", f, "token, depositor, amount, new_reserve", opPAX.Bytes(), opTrader.Bytes(), "500", "500")
	s.endBlock()
	f = s.fact("mark", decode.PerpsAddress, decode.PerpsAddress, 0)
	s.put(decode.MarkPriceUpdatedRow{Fact: f, MarketID: "0", MarkPrice: "60100000000000000000000", IndexPrice: "60000000000000000000000",
		MidPrice: "60050000000000000000000"}, "market_id, mark_price, index_price, mid_price",
		"0", "60100000000000000000000", "60000000000000000000000", "60050000000000000000000")
	f = s.fact("rate", decode.PerpsAddress, decode.PerpsAddress, 0)
	s.put(decode.FundingRateUpdatedRow{Fact: f, MarketID: "0", NewRatePerSecond: "1000", FundingRate24h: "86400000"},
		"market_id, new_rate_per_second, funding_rate24h", "0", "1000", "86400000")
	f = s.fact("oi", decode.PerpsAddress, decode.PerpsAddress, 0)
	s.put(decode.OpenInterestChangedRow{Fact: f, MarketID: "0", LongOI: "3000000000000000000000", ShortOI: "0",
		DeltaUSD: "1000000000000000000000", IsIncrease: true}, "market_id, long_oi, short_oi, delta_usd, is_increase",
		"0", "3000000000000000000000", "0", "1000000000000000000000", true)
	f = s.fact("applied", opTrader, decode.PerpsAddress, 0)
	s.put(decode.PositionFundingAppliedRow{Fact: f, PositionID: "1", User: opTrader, MarketID: "0", FundingPaymentUSD: "-1000000000000000000",
		NewCollateralUSD: "99000000000000000000", NewCollateralAmount: "99000000"},
		`position_id, "user", market_id, funding_payment_usd, new_collateral_usd, new_collateral_amount`,
		"1", opTrader.Bytes(), "0", "-1000000000000000000", "99000000000000000000", "99000000")
	f = s.fact("fill", opTrader, decode.PerpsAddress, 0)
	s.put(decode.FillRow{Fact: f, MarketID: "0", MakerOrderID: "0", TakerOrderID: "12", Maker: opCounter, Taker: opTrader,
		TakerIsLong: false, Price: "60000000000000000000000", SizeUSD: "1000000000000000000000", MakerFeeUSD: "0", TakerFeeUSD: "0"},
		"market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd, taker_fee_usd",
		"0", "0", "12", opCounter.Bytes(), opTrader.Bytes(), false, "60000000000000000000000", "1000000000000000000000", "0", "0")
	f = s.fact("executed", opTrader, decode.PerpsAddress, 0)
	s.put(decode.OrderExecutedRow{Fact: f, OrderID: "12", PositionID: "2", ExecutionPrice: "60000000000000000000000"},
		"order_id, position_id, execution_price", "12", "2", "60000000000000000000000")
	f = s.fact("draw", decode.PerpsAddress, decode.PerpsAddress, 0)
	s.put(decode.InsuranceDrawRow{Fact: f, MarketID: "0", Amount: "1000000", NewBalance: "9000000"},
		"market_id, amount, new_balance", "0", "1000000", "9000000")
	f = s.fact("aggregate", decode.PerpsAddress, opIndexFeed, 0)
	s.insert("index_prices", f, "market_id, index_price, mark_price, basis, timestamp_ms, sources, required",
		"0", "60000000000000000000000", "60100000000000000000000", "100000000000000000000", fmt.Sprint(t0.UnixMilli()), "3", "2")
	s.project()

	s.next(101, t1)
	s.closed("2", opTrader, "1000000000000000000000", "16500000000000000000")
	s.settled("2", opTrader, decode.TradeClose, "16500000000000000000", "116000000")
	f = s.fact("withdrawn", opTrader, decode.PerpsAddress, 0)
	s.put(decode.CollateralWithdrawnRow{Fact: f, AccountArg: opTrader, Amount: "1000000000", NewBalance: "4116000000"},
		"account_arg, amount, new_balance", opTrader.Bytes(), "1000000000", "4116000000")
	f = s.fact("limit_executed", opTrader, opSpotLimit, 0)
	s.insert("spot_order_events", f, "order_id, token, stablecoin, execution_price", "20", opPAX.Bytes(), store.USDL.Bytes(), "61")
	s.endBlock()
	f = s.fact("liquidated", opTrader, decode.PerpsAddress, 0)
	s.put(decode.LiquidationRow{Fact: f, PositionID: "3", User: opTrader, MarketID: "0", LiquidationPrice: "59000000000000000000000",
		Penalty: "1000000", Keeper: opCounter}, `position_id, "user", market_id, liquidation_price, penalty, keeper`,
		"3", opTrader.Bytes(), "0", "59000000000000000000000", "1000000", opCounter.Bytes())
	s.closed("3", opTrader, "1000000000000000000000", "-90000000000000000000")
	s.settled("3", opTrader, decode.TradeLiquidation, "-90000000000000000000", "0")
	s.project()
	if err := (store.Snapshotter{}).Snapshot(ctx, tx, 101, t1); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	s.exec(`INSERT INTO perps.cursors (name, height, hash) VALUES ('head', 101, $1)`, crypto.Keccak256Hash([]byte("block/101")).Bytes())
	if err := (pnl.Leaderboard{}).Rebuild(ctx, tx, t1); err != nil {
		t.Fatalf("Leaderboard: %v", err)
	}

	s.exec(`INSERT INTO perps.spot_orders (order_id, account, kind, order_type, side, token, stablecoin, amount, target_price, status,
    execution_price, placed_block, closed_block) VALUES ('20', $1, 'limit', 1, 'sell', $2, $3, 700, 60, 'executed', 61, 100, 101)`,
		opTrader.Bytes(), opPAX.Bytes(), store.USDL.Bytes())
	s.exec(`INSERT INTO perps.fee_config (id, taker_fee_ppm, maker_fee_ppm, liquidation_fee_ppm, insurance_share_ppm, refreshed_block)
VALUES (1, 500, 200, 10000, 300000, 101)`)
	s.exec(`INSERT INTO perps.referral_tiers (tier, min_referrals, min_volume, reward_bps, fee_discount_bps)
VALUES ('bronze', 0, 0, 500, 0), ('silver', 5, 1000000000000000000000000, 1000, 500)`)
	s.exec(`INSERT INTO perps.referrals (address, referral_code, total_earned) VALUES ($1, 'ALPHA1', 2500000)`, opTrader.Bytes())
	s.exec(`INSERT INTO perps.referrals (address, referral_code, referred_by, applied_at) VALUES ($1, 'BRAVO2', $2, $3)`,
		opCounter.Bytes(), opTrader.Bytes(), t0)
	s.exec(`INSERT INTO perps.user_alerts (address, alert_type, market_id, threshold, enabled) VALUES ($1, 'price_above', 0, 70000, true)`,
		opTrader.Bytes())
	var competitionID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO perps.competitions (name, description, metric, start_at, end_at, prize_pool, status)
VALUES ('Weekly PnL', 'Highest realized PnL', 'pnl', $1, $2, 1000000000, 'active') RETURNING id`,
		t0.Add(-time.Hour), time.Now().UTC().Add(7*24*time.Hour)).Scan(&competitionID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	s.exec(`INSERT INTO perps.competition_entries (competition_id, address, joined_at) VALUES ($1, $2, $3)`, competitionID, opTrader.Bytes(), t0)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	committed = true
	return fmt.Sprint(competitionID)
}
