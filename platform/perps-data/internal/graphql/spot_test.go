package graphql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	graphql "github.com/graph-gophers/graphql-go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

var (
	spotTrader  = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	spotPAX     = common.HexToAddress("0x00000000000000000000000000000000000000fa")
	spotSID     = common.HexToAddress("0x00000000000000000000000000000000000000fb")
	spotAdmin   = common.HexToAddress("0x0000000000000000000000000000000000001021")
	spotLimits  = common.HexToAddress("0x0000000000000000000000000000000000001022")
	spotStops   = common.HexToAddress("0x0000000000000000000000000000000000001023")
	spotVaultID = common.HexToAddress("0x0000000000000000000000000000000000001025")
	spotEpoch   = time.Unix(1700000000, 0).UTC()
)

const (
	spotPAXHex  = "0x00000000000000000000000000000000000000fa"
	spotSIDHex  = "0x00000000000000000000000000000000000000fb"
	spotUSDLHex = "0x85fcd13735f4309833a503ee804ea32395851479"
	spotA1Hex   = "0x00000000000000000000000000000000000000a1"
)

func spotTxHash(block, logIndex int64) []byte {
	return crypto.Keccak256([]byte(fmt.Sprintf("spot/%d/%d", block, logIndex)))
}

func spotTxHex(block, logIndex int64) string {
	return "0x" + hex.EncodeToString(spotTxHash(block, logIndex))
}

// spotAt is the block time of block n in the seeded chain: one second per block.
func spotAt(n int64) string {
	return spotEpoch.Add(time.Duration(n) * time.Second).Format(time.RFC3339Nano)
}

type spotFact struct {
	table, kind      string
	block, logIndex  int64
	emitter, account common.Address
	cols             string
	vals             []any
}

func insertSpotFact(t *testing.T, db *sql.DB, f spotFact) {
	t.Helper()
	args := append([]any{f.block, crypto.Keccak256([]byte(fmt.Sprintf("block/%d", f.block))), 0, f.logIndex, 0,
		spotTxHash(f.block, f.logIndex), "tx", spotEpoch.Add(time.Duration(f.block) * time.Second), f.emitter.Bytes(), f.kind,
		f.account.Bytes(), spotTrader.Bytes()}, f.vals...)
	marks := make([]string, len(args))
	for i := range marks {
		marks[i] = fmt.Sprintf("$%d", i+1)
	}
	q := `INSERT INTO perps.` + f.table + ` (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time,
    emitter, kind, account, tx_from, ` + f.cols + `) VALUES (` + strings.Join(marks, ", ") + `)`
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("insert %s %s: %v", f.table, f.kind, err)
	}
}

// seedSpot writes the registry, admin, order and vault facts of the spot venue
// and the perps.spot_orders rows the store projection folds from those order
// events (contracts/data-services.md section 2).
func seedSpot(t *testing.T, db *sql.DB) {
	t.Helper()
	usdl, pax, sid, a1 := store.USDL.Bytes(), spotPAX.Bytes(), spotSID.Bytes(), spotTrader.Bytes()
	for i, tok := range []struct {
		token  []byte
		stable bool
	}{{usdl, true}, {pax, false}, {sid, false}} {
		insertSpotFact(t, db, spotFact{table: "spot_token_registry", kind: "token_registered", block: 1, logIndex: int64(i),
			emitter: spotVaultID, account: spotVaultID, cols: "token, decimals, is_stablecoin", vals: []any{tok.token, 6, tok.stable}})
	}
	insertSpotFact(t, db, spotFact{table: "spot_admin", kind: "paused", block: 2, emitter: spotAdmin, account: spotAdmin,
		cols: "account_arg", vals: []any{a1}})
	insertSpotFact(t, db, spotFact{table: "spot_admin", kind: "token_pause_updated", block: 3, emitter: spotAdmin, account: spotAdmin,
		cols: "token, paused", vals: []any{pax, false}})

	orderCols := `order_id, "user", order_type, amount, target_price, stop_price, limit_price, trigger_price, execution_price, token, stablecoin`
	for _, f := range []spotFact{
		{kind: "limit_created", block: 4, emitter: spotLimits, account: spotTrader,
			vals: []any{1, a1, 0, "100000000", "3000000000000000000", nil, nil, nil, nil, pax, usdl}},
		{kind: "limit_executed", block: 5, emitter: spotLimits, account: spotLimits,
			vals: []any{1, nil, nil, nil, nil, nil, nil, nil, "2500000000000000000", pax, usdl}},
		{kind: "stop_loss_created", block: 6, emitter: spotStops, account: spotTrader,
			vals: []any{2, a1, nil, "7000000", nil, nil, nil, "1000000000000000000", nil, sid, usdl}},
		{kind: "stop_limit_created", block: 7, emitter: spotStops, account: spotTrader,
			vals: []any{3, a1, 4, "5000000", nil, "2000000000000000000", "1900000000000000000", nil, nil, pax, usdl}},
		{kind: "stop_limit_cancelled", block: 8, emitter: spotStops, account: spotStops,
			vals: []any{3, nil, nil, nil, nil, nil, nil, nil, nil, pax, usdl}},
	} {
		f.table, f.cols = "spot_order_events", orderCols
		insertSpotFact(t, db, f)
	}
	for _, o := range [][]any{
		{1, a1, "limit", 0, "buy", pax, usdl, "100000000", "3000000000000000000", nil, nil, nil, "executed", "2500000000000000000", 4, 5},
		{2, a1, "stop_loss", nil, "sell", sid, usdl, "7000000", nil, nil, nil, "1000000000000000000", "open", nil, 6, nil},
		{3, a1, "stop_limit", 4, "sell", pax, usdl, "5000000", nil, "2000000000000000000", "1900000000000000000", nil, "cancelled", nil, 7, 8},
	} {
		if _, err := db.Exec(`INSERT INTO perps.spot_orders (order_id, account, kind, order_type, side, token, stablecoin, amount,
    target_price, stop_price, limit_price, trigger_price, status, execution_price, placed_block, closed_block)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`, o...); err != nil {
			t.Fatalf("insert spot order %v: %v", o[0], err)
		}
	}

	for _, f := range []spotFact{
		{kind: "deposit", block: 9, cols: "token, depositor, amount, new_reserve", vals: []any{pax, a1, "40000000", "40000000"}},
		{kind: "native_deposit", block: 10, cols: "depositor, amount, new_reserve", vals: []any{a1, "11000000", "51000000"}},
		{kind: "withdrawal", block: 11, cols: "token, recipient, amount, new_reserve", vals: []any{usdl, a1, "3000000", "9000000"}},
		{kind: "lp_withdrawal", block: 12, cols: "token, provider, shares, amount", vals: []any{sid, a1, "500", "2000000"}},
		{kind: "reserves_sync", block: 13, cols: "token, old_reserves, new_reserves, recovered", vals: []any{pax, "1", "2", "1"}},
	} {
		f.table, f.emitter, f.account = "spot_vault_events", spotVaultID, spotTrader
		if f.kind == "reserves_sync" {
			f.account = spotVaultID
		}
		insertSpotFact(t, db, f)
	}
}

// spotSchema parses the spot query roots and spot types of SDL with the
// Resolver, so the spot resolvers run through the graphql-go executor with
// the options of NewSchema.
func spotSchema(t *testing.T, db *sql.DB) *graphql.Schema {
	t.Helper()
	start := strings.Index(SDL, "  spotMarket(marketId")
	types := strings.Index(SDL, "type SpotMarket {")
	end := strings.Index(SDL, "type LeaderboardEntry {")
	if start < 0 || types < start || end < types {
		t.Fatal("spot roots and types not found in SDL")
	}
	roots := strings.TrimSuffix(strings.TrimSpace(SDL[start:types]), "}")
	sdl := "scalar BigDecimal\n\nscalar DateTime\n\ntype Query {\n" + roots + "}\n\n" + SDL[types:end]
	schema, err := graphql.ParseSchema(sdl, &Resolver{DB: db},
		graphql.UseFieldResolvers(), graphql.MaxDepth(10), graphql.MaxParallelism(20))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// assertSpot runs query and compares data with the JSON document want.
func assertSpot(t *testing.T, schema *graphql.Schema, query string, vars map[string]any, want string) {
	t.Helper()
	response := schema.Exec(context.Background(), query, "", vars)
	if len(response.Errors) != 0 {
		t.Fatalf("%s: errors %v", query, response.Errors)
	}
	var got, expected any
	if err := json.Unmarshal(response.Data, &got); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatalf("want: %v", err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("%s %v:\n got %s\nwant %s", query, vars, response.Data, want)
	}
}

const (
	spotMarketFields = `marketId baseToken quoteToken mode modeName status createdAt createdBlock createdTxHash
    lastModeChangedAt lastModeChangeBlock lastModeChangeTx`
	spotOrderFields = `orderId traderAddress marketId side sideName orderType orderTypeName offsetBps size filledSize status
    placedAt placedBlock placedTxHash resolvedAt resolvedBlock resolvedTxHash`
	spotFillFields = `id orderId traderAddress marketId fillSize fillPrice fillScore blockNumber txHash logIndex blockTimestamp`
)

func spotMarketJSON(token string, logIndex int64, status string) string {
	return fmt.Sprintf(`{"marketId":%q,"baseToken":%q,"quoteToken":%q,"mode":0,"modeName":"pecor","status":%q,
        "createdAt":%q,"createdBlock":1,"createdTxHash":%q,"lastModeChangedAt":null,"lastModeChangeBlock":null,"lastModeChangeTx":null}`,
		token, token, spotUSDLHex, status, spotAt(1), spotTxHex(1, logIndex))
}

var (
	spotPAXMarket = spotMarketJSON(spotPAXHex, 1, "active")
	spotSIDMarket = spotMarketJSON(spotSIDHex, 2, "paused")
	spotFill1     = fmt.Sprintf(`{"id":2,"orderId":"1","traderAddress":%q,"marketId":%q,"fillSize":"100000000",
        "fillPrice":"2500000000000000000","fillScore":"0","blockNumber":5,"txHash":%q,"logIndex":0,"blockTimestamp":%q}`,
		spotA1Hex, spotPAXHex, spotTxHex(5, 0), spotAt(5))
	spotOrder1 = fmt.Sprintf(`{"orderId":"1","traderAddress":%q,"marketId":%q,"side":0,"sideName":"buy","orderType":0,
        "orderTypeName":"limit","offsetBps":0,"size":"100000000","filledSize":"100000000","status":"executed",
        "placedAt":%q,"placedBlock":4,"placedTxHash":%q,"resolvedAt":%q,"resolvedBlock":5,"resolvedTxHash":%q}`,
		spotA1Hex, spotPAXHex, spotAt(4), spotTxHex(4, 0), spotAt(5), spotTxHex(5, 0))
	spotOrder2 = fmt.Sprintf(`{"orderId":"2","traderAddress":%q,"marketId":%q,"side":1,"sideName":"sell","orderType":2,
        "orderTypeName":"stop_loss","offsetBps":0,"size":"7000000","filledSize":"0","status":"open",
        "placedAt":%q,"placedBlock":6,"placedTxHash":%q,"resolvedAt":null,"resolvedBlock":null,"resolvedTxHash":null}`,
		spotA1Hex, spotSIDHex, spotAt(6), spotTxHex(6, 0))
	spotOrder3 = fmt.Sprintf(`{"orderId":"3","traderAddress":%q,"marketId":%q,"side":1,"sideName":"sell","orderType":1,
        "orderTypeName":"stop_limit","offsetBps":0,"size":"5000000","filledSize":"0","status":"cancelled",
        "placedAt":%q,"placedBlock":7,"placedTxHash":%q,"resolvedAt":%q,"resolvedBlock":8,"resolvedTxHash":%q}`,
		spotA1Hex, spotPAXHex, spotAt(7), spotTxHex(7, 0), spotAt(8), spotTxHex(8, 0))
)

func spotMovementJSON(id, block int64, kind, token, amount string) string {
	return fmt.Sprintf(`{"id":%d,"eventType":%q,"userAddress":%q,"tokenAddress":%q,"amount":%q,"blockNumber":%d,
        "txHash":%q,"logIndex":0,"blockTimestamp":%q}`, id, kind, spotA1Hex, token, amount, block, spotTxHex(block, 0), spotAt(block))
}

func TestSpotResolvers(t *testing.T) {
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TestSpotResolvers requires PERPS_DATA_TEST_DATABASE_URL")
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	seedSpot(t, db)
	schema := spotSchema(t, db)

	t.Run("spotMarket", func(t *testing.T) {
		query := `query GetSpotMarket($marketId: String!) { spotMarket(marketId: $marketId) { ` + spotMarketFields + ` } }`
		assertSpot(t, schema, query, map[string]any{"marketId": spotPAXHex}, `{"spotMarket":`+spotPAXMarket+`}`)
		assertSpot(t, schema, query, map[string]any{"marketId": strings.ToUpper(spotSIDHex[2:])}, `{"spotMarket":`+spotSIDMarket+`}`)
		for _, unknown := range []string{spotUSDLHex, spotA1Hex, "nope"} {
			assertSpot(t, schema, query, map[string]any{"marketId": unknown}, `{"spotMarket":null}`)
		}
	})
	t.Run("spotMarkets", func(t *testing.T) {
		query := `query GetSpotMarkets($status: String, $limit: Int, $offset: Int) {
            spotMarkets(status: $status, limit: $limit, offset: $offset) { ` + spotMarketFields + ` } }`
		assertSpot(t, schema, query, nil, `{"spotMarkets":[`+spotPAXMarket+`,`+spotSIDMarket+`]}`)
		assertSpot(t, schema, query, map[string]any{"status": "paused"}, `{"spotMarkets":[`+spotSIDMarket+`]}`)
		assertSpot(t, schema, query, map[string]any{"status": "active"}, `{"spotMarkets":[`+spotPAXMarket+`]}`)
		assertSpot(t, schema, query, map[string]any{"limit": 1, "offset": 1}, `{"spotMarkets":[`+spotSIDMarket+`]}`)
		assertSpot(t, schema, query, map[string]any{"limit": 0, "offset": -5}, `{"spotMarkets":[`+spotPAXMarket+`]}`)
		assertSpot(t, schema, query, map[string]any{"offset": 2}, `{"spotMarkets":[]}`)
	})
	t.Run("spotOrder", func(t *testing.T) {
		query := `query GetSpotOrder($orderId: String!) { spotOrder(orderId: $orderId) { ` + spotOrderFields + `
            fills { ` + spotFillFields + ` } market { marketId status } } }`
		withNested := func(order, fills, market string) string {
			return `{"spotOrder":` + strings.TrimSuffix(strings.TrimSpace(order), "}") + `,"fills":[` + fills + `],"market":` + market + `}}`
		}
		assertSpot(t, schema, query, map[string]any{"orderId": "1"},
			withNested(spotOrder1, spotFill1, fmt.Sprintf(`{"marketId":%q,"status":"active"}`, spotPAXHex)))
		assertSpot(t, schema, query, map[string]any{"orderId": "2"},
			withNested(spotOrder2, "", fmt.Sprintf(`{"marketId":%q,"status":"paused"}`, spotSIDHex)))
		assertSpot(t, schema, query, map[string]any{"orderId": "3"},
			withNested(spotOrder3, "", fmt.Sprintf(`{"marketId":%q,"status":"active"}`, spotPAXHex)))
		for _, unknown := range []string{"99", "-1", "one"} {
			assertSpot(t, schema, query, map[string]any{"orderId": unknown}, `{"spotOrder":null}`)
		}
	})
	t.Run("spotOrders", func(t *testing.T) {
		query := `query GetSpotOrders($traderAddress: String, $marketId: String, $status: String, $limit: Int, $offset: Int) {
            spotOrders(traderAddress: $traderAddress, marketId: $marketId, status: $status, limit: $limit, offset: $offset) { ` +
			spotOrderFields + ` } }`
		all := `{"spotOrders":[` + spotOrder3 + `,` + spotOrder2 + `,` + spotOrder1 + `]}`
		assertSpot(t, schema, query, nil, all)
		assertSpot(t, schema, query, map[string]any{"traderAddress": "0x00000000000000000000000000000000000000A1"}, all)
		assertSpot(t, schema, query, map[string]any{"marketId": spotPAXHex}, `{"spotOrders":[`+spotOrder3+`,`+spotOrder1+`]}`)
		assertSpot(t, schema, query, map[string]any{"status": "open"}, `{"spotOrders":[`+spotOrder2+`]}`)
		assertSpot(t, schema, query, map[string]any{"limit": 1, "offset": 2}, `{"spotOrders":[`+spotOrder1+`]}`)
		for _, vars := range []map[string]any{
			{"traderAddress": spotPAXHex}, {"traderAddress": "nope"}, {"marketId": spotUSDLHex}, {"status": "filled"},
		} {
			assertSpot(t, schema, query, vars, `{"spotOrders":[]}`)
		}
	})
	t.Run("spotOrderFills", func(t *testing.T) {
		query := `query GetSpotOrderFills($orderId: String, $traderAddress: String, $marketId: String, $limit: Int, $offset: Int) {
            spotOrderFills(orderId: $orderId, traderAddress: $traderAddress, marketId: $marketId, limit: $limit, offset: $offset) { ` +
			spotFillFields + ` } }`
		one := `{"spotOrderFills":[` + spotFill1 + `]}`
		assertSpot(t, schema, query, nil, one)
		assertSpot(t, schema, query, map[string]any{"orderId": "1", "traderAddress": spotA1Hex, "marketId": spotPAXHex}, one)
		for _, vars := range []map[string]any{
			{"orderId": "2"}, {"orderId": "3"}, {"orderId": "x"}, {"marketId": spotSIDHex}, {"traderAddress": spotPAXHex}, {"offset": 1},
		} {
			assertSpot(t, schema, query, vars, `{"spotOrderFills":[]}`)
		}
	})
	t.Run("spotCollateralMovements", func(t *testing.T) {
		query := `query GetSpotCollateralMovements($userAddress: String, $tokenAddress: String, $eventType: String, $limit: Int, $offset: Int) {
            spotCollateralMovements(userAddress: $userAddress, tokenAddress: $tokenAddress, eventType: $eventType, limit: $limit, offset: $offset) {
                id eventType userAddress tokenAddress amount blockNumber txHash logIndex blockTimestamp } }`
		deposit := spotMovementJSON(1, 9, "deposit", spotPAXHex, "40000000")
		native := spotMovementJSON(2, 10, "native_deposit", spotPAXHex, "11000000")
		withdrawal := spotMovementJSON(3, 11, "withdrawal", spotUSDLHex, "3000000")
		lp := spotMovementJSON(4, 12, "lp_withdrawal", spotSIDHex, "2000000")
		assertSpot(t, schema, query, nil, `{"spotCollateralMovements":[`+lp+`,`+withdrawal+`,`+native+`,`+deposit+`]}`)
		assertSpot(t, schema, query, map[string]any{"tokenAddress": spotPAXHex}, `{"spotCollateralMovements":[`+native+`,`+deposit+`]}`)
		assertSpot(t, schema, query, map[string]any{"eventType": "native_deposit", "userAddress": spotA1Hex},
			`{"spotCollateralMovements":[`+native+`]}`)
		assertSpot(t, schema, query, map[string]any{"limit": 2, "offset": 1}, `{"spotCollateralMovements":[`+withdrawal+`,`+native+`]}`)
		for _, vars := range []map[string]any{
			{"eventType": "reserves_sync"}, {"userAddress": spotPAXHex}, {"tokenAddress": "nope"},
		} {
			assertSpot(t, schema, query, vars, `{"spotCollateralMovements":[]}`)
		}
	})
	for _, tc := range []struct{ root, document string }{
		{"spotBatchClearings", `{ spotBatchClearings(marketId: "` + spotPAXHex + `", limit: 10, offset: 0) {
            id marketId clearingOffsetBps clearingPrice matchedVolume numBuysFilled numSellsFilled blockNumber txHash logIndex blockTimestamp } }`},
		{"spotEpochSettlements", `{ spotEpochSettlements(limit: 10, offset: 0) {
            epochNumber usersProcessed onchainBlock blockNumber txHash logIndex blockTimestamp } }`},
		{"spotFastSettlements", `{ spotFastSettlements(userAddress: "` + spotA1Hex + `", tokenAddress: "` + spotPAXHex + `") {
            id userAddress tokenAddress amount fee blockNumber txHash logIndex blockTimestamp } }`},
		{"spotMarketModeChanges", `{ spotMarketModeChanges(marketId: "` + spotPAXHex + `") {
            id marketId oldMode newMode batchModeUntilBlock blockNumber txHash logIndex blockTimestamp } }`},
		{"spotPofqUpdates", `{ spotPofqUpdates(entityAddress: "` + spotA1Hex + `", isVault: false) {
            id entityAddress newScore newWeight isVault blockNumber txHash logIndex blockTimestamp } }`},
		{"spotPLVs", `{ spotPLVs(isRegistered: true) {
            vaultAddress isRegistered registeredAt registeredBlock registeredTxHash deregisteredAt deregisteredBlock deregisteredTxHash } }`},
		{"spotKeeperCycles", `{ spotKeeperCycles(limit: 5) {
            id onchainTimestamp marketsEvaluated batchesCleared epochSettled blockNumber txHash blockTimestamp } }`},
		{"spotBatchClearFailures", `{ spotBatchClearFailures(marketId: "` + spotSIDHex + `") {
            id marketId reason blockNumber txHash logIndex blockTimestamp } }`},
	} {
		t.Run(tc.root, func(t *testing.T) {
			assertSpot(t, schema, tc.document, nil, `{"`+tc.root+`":[]}`)
		})
	}
}
