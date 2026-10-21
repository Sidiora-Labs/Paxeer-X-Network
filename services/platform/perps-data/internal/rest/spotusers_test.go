package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

var (
	spotUser      = addr(0xa1, 0x00)
	spotLimitAddr = addr(0x22, 0x10)
	spotUserHex   = "0x00000000000000000000000000000000000000a1"
)

// spotTokenSet is the stablecoin and the two base tokens of a scenario, in
// registration order.
type spotTokenSet struct{ usdl, pax, sid []byte }

// spotFactRow is a stored fact row handed to store.Project.
type spotFactRow struct {
	decode.Fact
	table string
}

func (r spotFactRow) Table() string { return r.table }

func spotPtr[T any](v T) *T { return &v }

func spotHexOf(b []byte) string { return spotHex(common.BytesToAddress(b)) }

func spotTx(block int64) string { return common.BytesToHash(hash32(block, 0, 0)).Hex() }

func spotReset(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE perps.spot_swaps, perps.spot_fee_events, perps.spot_order_events, perps.spot_token_registry,
		perps.spot_orders, perps.spot_token_seen, perps.index_prices RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
}

// spotProjected inserts a spot fact row and folds it through store.Project.
func spotProjected(t *testing.T, db *sql.DB, r row) {
	t.Helper()
	insert(t, db, r)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	f := decode.Fact{Block: uint64(r.block), TxHash: common.BytesToHash(hash32(r.block, r.txIndex, r.logIndex)), LogIndex: int64(r.logIndex)}
	if err := store.Project(context.Background(), tx, []decode.Row{spotFactRow{Fact: f, table: r.table}}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("project %s: %v", r.table, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func spotRegister(t *testing.T, db *sql.DB, tk spotTokenSet, at time.Time) {
	t.Helper()
	for i, tok := range []struct {
		token  []byte
		stable bool
	}{{tk.usdl, true}, {tk.pax, false}, {tk.sid, false}} {
		spotProjected(t, db, row{table: "spot_token_registry", kind: "token_registered", block: 1, logIndex: i, at: at, emitter: vaultAddr,
			account: vaultAddr, args: []string{"token", "decimals", "is_stablecoin"}, values: []any{tok.token, 6, tok.stable}})
	}
}

func spotMarket(t *testing.T, db *sql.DB, block int64, at time.Time, user, stable, token []byte, buy bool, stableAmt, tokenAmt string) {
	t.Helper()
	spotProjected(t, db, row{table: "spot_swaps", kind: "market_order", block: block, at: at, emitter: spotAddr, account: user,
		args:   []string{"user", "stablecoin", "token", "stablecoin_amount", "token_amount", "is_buy"},
		values: []any{user, stable, token, e6(t, stableAmt), e6(t, tokenAmt), buy}})
}

func spotOrderEvent(t *testing.T, db *sql.DB, block int64, at time.Time, kind string, id int, tk spotTokenSet, token []byte, args []string, values []any) {
	t.Helper()
	spotProjected(t, db, row{table: "spot_order_events", kind: kind, block: block, at: at, emitter: spotLimitAddr, account: spotUser,
		args:   append([]string{"order_id", "user", "token", "stablecoin"}, args...),
		values: append([]any{id, spotUser, token, tk.usdl}, values...)})
}

// spotScenario seeds the trades of spotUser at base + block seconds:
// block 10 buys 50 PAX for 100 USDL, block 11 sells 20 PAX for 60 USDL, limit
// buy order 1 of 30 USDL of SID placed at 12 fills at 13 at 1.5, limit sell
// order 2 of 10 PAX placed at 14 stays open, block 15 swaps 20 SID for 40 USDL
// at 2 with a 30 bps fee of 0.12 USDL, and PAX is indexed at 2.5.
func spotScenario(t *testing.T, db *sql.DB, tk spotTokenSet, base time.Time) {
	t.Helper()
	at := func(block int64) time.Time { return base.Add(time.Duration(block) * time.Second) }
	spotRegister(t, db, tk, at(1))
	spotMarket(t, db, 10, at(10), spotUser, tk.usdl, tk.pax, true, "100", "50")
	spotMarket(t, db, 11, at(11), spotUser, tk.usdl, tk.pax, false, "60", "20")
	spotOrderEvent(t, db, 12, at(12), "limit_created", 1, tk, tk.sid, []string{"order_type", "amount", "target_price"},
		[]any{0, e6(t, "30"), e18(t, "1.5")})
	spotOrderEvent(t, db, 13, at(13), "limit_executed", 1, tk, tk.sid, []string{"execution_price"}, []any{e18(t, "1.5")})
	spotOrderEvent(t, db, 14, at(14), "limit_created", 2, tk, tk.pax, []string{"order_type", "amount", "target_price"},
		[]any{1, e6(t, "10"), e18(t, "3")})
	spotProjected(t, db, row{table: "spot_swaps", kind: "simple_swap", block: 15, at: at(15), emitter: spotAddr, account: spotUser,
		args:   []string{"user", "token_in", "token_out", "amount_in", "amount_out", "execution_price"},
		values: []any{spotUser, tk.sid, tk.usdl, e6(t, "20"), e6(t, "40"), e18(t, "2")}})
	insert(t, db, row{table: "spot_fee_events", kind: "tiered_fee", block: 15, at: at(15), emitter: spotAddr, account: spotUser,
		args: []string{"user", "token_out", "volume_usd", "fee_bps", "fee_amount"}, values: []any{spotUser, tk.usdl, e18(t, "40"), 30, e6(t, "0.12")}})
	insert(t, db, row{table: "index_prices", kind: "aggregate", block: 16, at: at(16), emitter: addr(0x20, 0x10), account: addr(0x20, 0x10),
		args:   []string{"market_id", "index_price", "mark_price", "basis", "timestamp_ms", "sources", "required"},
		values: []any{11, e18(t, "2.5"), e18(t, "2.5"), "0", at(16).UnixMilli(), 3, 2}})
}

// spotGet serves path and decodes the JSON body into out, failing on another
// status or a Content-Type other than application/json; it returns the fields.
func spotGet(t *testing.T, mux http.Handler, path string, status int, out any) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != status {
		t.Fatalf("%s: status %d, want %d: %s", path, rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s: Content-Type %q", path, ct)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
		}
	}
	return fields
}

func spotRefused(t *testing.T, mux http.Handler, path string, status int, msg string) {
	t.Helper()
	body := spotGet(t, mux, path, status, nil)
	wantKeys(t, body, "error")
	if got := field[string](t, body, "error"); got != msg {
		t.Fatalf("%s: error %q, want %q", path, got, msg)
	}
}

// spotBoardAsOf checks that as_of is the ISO millisecond time of a fold that
// ran within the last minute.
func spotBoardAsOf(t *testing.T, asOf string) {
	t.Helper()
	at, err := time.Parse(records.ISOMillis, asOf)
	if err != nil {
		t.Fatalf("as_of %q: %v", asOf, err)
	}
	if age := time.Since(at); age < -time.Second || age > time.Minute {
		t.Fatalf("as_of %s is %s old", asOf, age)
	}
}

func spotEqual(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Fatalf("%s:\n got %s\nwant %s", name, g, w)
	}
}

func spotMux(s *SpotUsers) *http.ServeMux {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

// spotNode dials the test node of PERPS_DATA_TEST_RPC and reads the registered
// spot tokens of 0x1025 (getRegisteredTokens): USDL, PAX and SID.
func spotNode(t *testing.T) (*ethclient.Client, spotTokenSet) {
	t.Helper()
	url := os.Getenv("PERPS_DATA_TEST_RPC")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_RPC is unset")
	}
	client, err := ethclient.DialContext(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	vault := common.BytesToAddress(vaultAddr)
	out, err := client.CallContract(context.Background(), ethereum.CallMsg{To: &vault, Data: []byte{0x14, 0xcb, 0xd9, 0x93}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	typ, err := abi.NewType("address[]", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	vals, err := abi.Arguments{{Type: typ}}.Unpack(out)
	if err != nil {
		t.Fatal(err)
	}
	tokens, ok := vals[0].([]common.Address)
	if !ok || len(tokens) < 3 {
		t.Fatalf("getRegisteredTokens = %v, want USDL, PAX and SID", vals)
	}
	return client, spotTokenSet{usdl: tokens[0].Bytes(), pax: tokens[1].Bytes(), sid: tokens[2].Bytes()}
}

func spotNodeBalance(t *testing.T, client *ethclient.Client, token []byte) string {
	t.Helper()
	to := common.BytesToAddress(token)
	data := append([]byte{0x70, 0xa0, 0x82, 0x31}, common.LeftPadBytes(spotUser, 32)...)
	out, err := client.CallContract(context.Background(), ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 32 {
		t.Fatalf("balanceOf %s returned %d bytes", spotHexOf(token), len(out))
	}
	return new(big.Int).SetBytes(out).String()
}

func TestSpotUsersREST(t *testing.T) {
	db := openDB(t)
	dbOnly := spotMux(&SpotUsers{db: db})
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	at := func(block int64) string { return spotISO(base.Add(time.Duration(block) * time.Second)) }
	synthetic := spotTokenSet{usdl: store.USDL.Bytes(), pax: paxToken, sid: sidToken}
	usdlHex, paxHex, sidHex := spotHexOf(synthetic.usdl), spotHexOf(paxToken), spotHexOf(sidToken)
	userPath := "/v1/users/" + spotUserHex

	if _, err := NewSpotUsers(nil, &ethclient.Client{}); err == nil || err.Error() != "rest: nil database" {
		t.Fatalf("NewSpotUsers(nil db) = %v", err)
	}
	if _, err := NewSpotUsers(db, nil); err == nil || err.Error() != "rest: nil node" {
		t.Fatalf("NewSpotUsers(nil node) = %v", err)
	}

	t.Run("orders", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var body spotOrdersBody
		fields := spotGet(t, dbOnly, userPath+"/orders", http.StatusOK, &body)
		wantKeys(t, fields, "address", "count", "items")
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(fields["items"], &items); err != nil || len(items) != 2 {
			t.Fatalf("items %s: %v", fields["items"], err)
		}
		wantKeys(t, items[0], "id", "orderId", "type", "status", "token", "stablecoin", "amount", "targetPrice", "stopPrice", "limitPrice",
			"triggerPrice", "executionPrice", "isBuy", "placedAt", "executedAt", "cancelledAt", "placedTxHash", "executedTxHash", "symbol", "decimals")
		open := spotOrderJSON{ID: "2", OrderID: "2", Type: "limit", Status: "open", Token: &paxHex, Stablecoin: &usdlHex,
			Amount: spotPtr(e6(t, "10")), TargetPrice: spotPtr(e18(t, "3")), IsBuy: spotPtr(false), PlacedAt: spotPtr(at(14)),
			PlacedTxHash: spotPtr(spotTx(14)), Symbol: spotPtr("PAX"), Decimals: spotPtr(6)}
		executed := spotOrderJSON{ID: "1", OrderID: "1", Type: "limit", Status: "executed", Token: &sidHex, Stablecoin: &usdlHex,
			Amount: spotPtr(e6(t, "30")), TargetPrice: spotPtr(e18(t, "1.5")), ExecutionPrice: spotPtr(e18(t, "1.5")), IsBuy: spotPtr(true),
			PlacedAt: spotPtr(at(12)), ExecutedAt: spotPtr(at(13)), PlacedTxHash: spotPtr(spotTx(12)), ExecutedTxHash: spotPtr(spotTx(13)),
			Symbol: spotPtr("SID"), Decimals: spotPtr(6)}
		spotEqual(t, "orders", body, spotOrdersBody{Address: spotUserHex, Count: 2, Items: []spotOrderJSON{open, executed}})

		body = spotOrdersBody{}
		spotGet(t, dbOnly, userPath+"/orders?status=open", http.StatusOK, &body)
		spotEqual(t, "open orders", body, spotOrdersBody{Address: spotUserHex, Count: 1, Items: []spotOrderJSON{open}})
		body = spotOrdersBody{}
		spotGet(t, dbOnly, userPath+"/orders?status=all&limit=1", http.StatusOK, &body)
		spotEqual(t, "limited orders", body, spotOrdersBody{Address: spotUserHex, Count: 1, Items: []spotOrderJSON{open}})
		spotRefused(t, dbOnly, userPath+"/orders?status=closed", http.StatusBadRequest, "invalid status")
		spotRefused(t, dbOnly, userPath+"/orders?limit=x", http.StatusBadRequest, "invalid limit")
	})

	t.Run("stream_order_item", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		users := &SpotUsers{db: db}
		env := func(payload string) delivery.Envelope {
			return delivery.Envelope{EventID: "e1", Payload: json.RawMessage(payload)}
		}
		items, err := users.StreamItems(context.Background(), env(`{"table":"spot_order_events","kind":"limit_created","block":"14","log_index":0,"order_id":"2"}`))
		if err != nil || len(items) != 1 {
			t.Fatalf("StreamItems(limit_created) = %v, %v", items, err)
		}
		if items[0].Stream != "order" || items[0].Address != spotUserHex {
			t.Fatalf("item = %s %s", items[0].Stream, items[0].Address)
		}
		var got spotOrderJSON
		if err := json.Unmarshal(items[0].Data, &got); err != nil {
			t.Fatal(err)
		}
		spotEqual(t, "order item", got, spotOrderJSON{ID: "2", OrderID: "2", Type: "limit", Status: "open", Token: &paxHex, Stablecoin: &usdlHex,
			Amount: spotPtr(e6(t, "10")), TargetPrice: spotPtr(e18(t, "3")), IsBuy: spotPtr(false), PlacedAt: spotPtr(at(14)),
			PlacedTxHash: spotPtr(spotTx(14)), Symbol: spotPtr("PAX"), Decimals: spotPtr(6)})

		if items, err := users.StreamItems(context.Background(), env(`{"table":"fills","block":"14","log_index":0}`)); err != nil || items != nil {
			t.Fatalf("StreamItems(fills) = %v, %v", items, err)
		}
		if _, err := users.StreamItems(context.Background(), env(`{"table":"spot_swaps","kind":"simple_swap","block":"15","log_index":0}`)); err == nil ||
			err.Error() != "rest: spot swap e1 without a user" {
			t.Fatalf("StreamItems(swap without user) = %v", err)
		}
		if _, err := users.StreamItems(context.Background(), env(`{"table":"spot_order_events","kind":"limit_cancelled","block":"16","log_index":0,"order_id":"9"}`)); err == nil ||
			!strings.HasPrefix(err.Error(), "rest: account of spot order 9: ") {
			t.Fatalf("StreamItems(unknown order) = %v", err)
		}
	})

	t.Run("trades", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var body spotTradesBody
		fields := spotGet(t, dbOnly, userPath+"/trades", http.StatusOK, &body)
		wantKeys(t, fields, "address", "count", "limit", "items", "nextCursor")
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(fields["items"], &items); err != nil || len(items) != 4 {
			t.Fatalf("items %s: %v", fields["items"], err)
		}
		wantKeys(t, items[0], "id", "txHash", "logIndex", "blockNumber", "blockTimestamp", "user", "tokenIn", "tokenOut", "amountIn", "amountOut",
			"executionPrice", "volumeUsd", "source", "contract", "createdAt", "feeAmount", "feeBps", "hops", "priceImpactAmount", "priceImpactBps",
			"routeAdapterId", "twapPrice")
		trade := func(block int64, source string, contract []byte, tin, tout, ain, aout, price, volume string) spotTradeItemJSON {
			return spotTradeItemJSON{ID: tradeID(block, 0), TxHash: spotTx(block), BlockNumber: strconv.FormatInt(block, 10),
				BlockTimestamp: at(block), User: spotUserHex, TokenIn: tin, TokenOut: tout, AmountIn: e6(t, ain), AmountOut: e6(t, aout),
				ExecutionPrice: e18(t, price), VolumeUsd: e6(t, volume), Source: source, Contract: spotHexOf(contract), CreatedAt: at(block)}
		}
		swap := trade(15, "simple_swap", spotAddr, sidHex, usdlHex, "20", "40", "2", "40")
		swap.FeeAmount, swap.FeeBps = spotPtr(e6(t, "0.12")), spotPtr(int64(30))
		spotEqual(t, "trades", body, spotTradesBody{Address: spotUserHex, Count: 4, Limit: defaultLimit, Items: []spotTradeItemJSON{
			swap,
			trade(13, "limit_executed", spotLimitAddr, usdlHex, sidHex, "30", "20", "1.5", "30"),
			trade(11, "market_order", spotAddr, paxHex, usdlHex, "20", "60", "3", "60"),
			trade(10, "market_order", spotAddr, usdlHex, paxHex, "100", "50", "2", "100"),
		}})
		spotRefused(t, dbOnly, userPath+"/trades?afterBlock=11", http.StatusBadRequest, "afterBlock and afterIndex go together")
		spotRefused(t, dbOnly, userPath+"/trades?afterBlock=-1&afterIndex=0", http.StatusBadRequest, "invalid afterBlock")
		spotRefused(t, dbOnly, userPath+"/trades?afterBlock=11&afterIndex=x", http.StatusBadRequest, "invalid afterIndex")
	})

	t.Run("trades_stats", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var body spotTradesStatsBody
		fields := spotGet(t, dbOnly, userPath+"/trades/stats", http.StatusOK, &body)
		wantKeys(t, fields, "address", "totalTrades", "bySource", "byContract")
		spotEqual(t, "trades/stats", body, spotTradesStatsBody{Address: spotUserHex, TotalTrades: 4,
			BySource:   map[string]int64{"market_order": 2, "limit_executed": 1, "simple_swap": 1},
			ByContract: map[string]int64{spotHexOf(spotAddr): 3, spotHexOf(spotLimitAddr): 1}})
	})

	closedSID := spotClosedLegJSON{ID: 2, Token: sidHex, Qty: e6(t, "20"), EntryPrice: e18(t, "1.5"), ExitPrice: e18(t, "2"),
		Pnl: e6(t, "10"), PnlUsd: e6(t, "10"), PnlPct: spotPtr("33.3333"), HoldingSeconds: spotPtr(int64(2)), OpenedAt: spotPtr(at(13)),
		ClosedAt: at(15), CloseTxHash: spotTx(15), FullyClosed: true, Symbol: spotPtr("SID"), Decimals: spotPtr(6)}
	closedPAX := spotClosedLegJSON{ID: 1, Token: paxHex, Qty: e6(t, "20"), EntryPrice: e18(t, "2"), ExitPrice: e18(t, "3"),
		Pnl: e6(t, "20"), PnlUsd: e6(t, "20"), PnlPct: spotPtr("50.0000"), HoldingSeconds: spotPtr(int64(1)), OpenedAt: spotPtr(at(10)),
		ClosedAt: at(11), CloseTxHash: spotTx(11), FullyClosed: false, Symbol: spotPtr("PAX"), Decimals: spotPtr(6)}

	t.Run("closed_legs", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var body spotClosedLegsBody
		fields := spotGet(t, dbOnly, userPath+"/closed-legs", http.StatusOK, &body)
		wantKeys(t, fields, "address", "count", "limit", "items", "nextCursor")
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(fields["items"], &items); err != nil || len(items) != 2 {
			t.Fatalf("items %s: %v", fields["items"], err)
		}
		wantKeys(t, items[0], "id", "token", "qty", "entryPrice", "exitPrice", "pnl", "pnlUsd", "pnlPct", "holdingSeconds", "openedAt",
			"closedAt", "closeTxHash", "fullyClosed", "symbol", "name", "decimals")
		spotEqual(t, "closed-legs", body, spotClosedLegsBody{Address: spotUserHex, Count: 2, Limit: defaultLimit,
			Items: []spotClosedLegJSON{closedSID, closedPAX}})
		spotRefused(t, dbOnly, userPath+"/closed-legs?afterId=0", http.StatusBadRequest, "invalid afterId")
	})

	wantPnl := spotPnlBody{Address: spotUserHex, RealizedUsd: e6(t, "30"), UnrealizedUsd: spotPtr(e6(t, "15")), TotalUsd: spotPtr(e6(t, "45")),
		TradeCount: 4, ClosedLegCount: 2}
	for name, route := range map[string]string{"pnl_summary": "/pnl/summary", "pnl": "/pnl"} {
		t.Run(name, func(t *testing.T) {
			spotReset(t, db)
			spotScenario(t, db, synthetic, base)
			var body spotPnlBody
			fields := spotGet(t, dbOnly, userPath+route, http.StatusOK, &body)
			wantKeys(t, fields, "address", "realizedUsd", "unrealizedUsd", "totalUsd", "tradeCount", "closedLegCount")
			spotEqual(t, route, body, wantPnl)
			if _, err := db.Exec(`TRUNCATE perps.index_prices`); err != nil {
				t.Fatal(err)
			}
			body = spotPnlBody{}
			fields = spotGet(t, dbOnly, userPath+route, http.StatusOK, &body)
			wantKeys(t, fields, "address", "realizedUsd", "tradeCount", "closedLegCount")
			spotEqual(t, route+" without a mark", body, spotPnlBody{Address: spotUserHex, RealizedUsd: e6(t, "30"), TradeCount: 4, ClosedLegCount: 2})
		})
	}

	t.Run("identity", func(t *testing.T) {
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var body spotIdentityBody
		fields := spotGet(t, dbOnly, userPath+"/identity", http.StatusOK, &body)
		wantKeys(t, fields, "address", "paxscan", "trading24h", "latestSnapshot")
		spotEqual(t, "identity", body, spotIdentityBody{Address: spotUserHex,
			Paxscan:        spotPaxscanJSON{FirstSeenBlock: spotPtr("10"), TxCount: 4},
			Trading24h:     spotTrading24hJSON{Trades: 4, VolumeUsd: e6(t, "230")},
			LatestSnapshot: spotSnapshotJSON{RealizedUsd: e6(t, "30"), At: spotPtr(at(15))}})
	})

	t.Run("trades_cursor", func(t *testing.T) {
		spotReset(t, db)
		spotRegister(t, db, synthetic, base)
		for b := int64(100); b < 130; b++ {
			spotMarket(t, db, b, base.Add(time.Duration(b)*time.Second), spotUser, synthetic.usdl, paxToken, true, "1", "1")
		}
		seen := map[string]bool{}
		var first spotTradesBody
		spotGet(t, dbOnly, userPath+"/trades?limit=20", http.StatusOK, &first)
		if first.Count != 20 || first.Limit != 20 || len(first.Items) != 20 {
			t.Fatalf("first page count %d limit %d items %d", first.Count, first.Limit, len(first.Items))
		}
		spotEqual(t, "first nextCursor", first.NextCursor, &spotTradeCursor{AfterBlock: "110", AfterIndex: 0})
		var second spotTradesBody
		spotGet(t, dbOnly, fmt.Sprintf("%s/trades?limit=20&afterBlock=%s&afterIndex=%d", userPath, first.NextCursor.AfterBlock,
			first.NextCursor.AfterIndex), http.StatusOK, &second)
		if second.Count != 10 || len(second.Items) != 10 || second.NextCursor != nil {
			t.Fatalf("second page count %d items %d nextCursor %v", second.Count, len(second.Items), second.NextCursor)
		}
		for _, item := range append(first.Items, second.Items...) {
			seen[item.ID] = true
		}
		if len(seen) != 30 {
			t.Fatalf("%d distinct trades, want 30", len(seen))
		}
		if first.Items[0].ID != "129-0" || second.Items[9].ID != "100-0" {
			t.Fatalf("pages run %s to %s", first.Items[0].ID, second.Items[9].ID)
		}
	})

	t.Run("closed_legs_cursor", func(t *testing.T) {
		spotReset(t, db)
		spotRegister(t, db, synthetic, base)
		spotMarket(t, db, 200, base.Add(200*time.Second), spotUser, synthetic.usdl, paxToken, true, "12", "12")
		for b := int64(201); b <= 212; b++ {
			spotMarket(t, db, b, base.Add(time.Duration(b)*time.Second), spotUser, synthetic.usdl, paxToken, false, "2", "1")
		}
		seen := map[int]bool{}
		path := userPath + "/closed-legs?limit=5"
		for _, want := range []struct {
			ids  []int
			next *spotLegCursor
		}{{[]int{12, 11, 10, 9, 8}, &spotLegCursor{AfterID: 8}}, {[]int{7, 6, 5, 4, 3}, &spotLegCursor{AfterID: 3}}, {[]int{2, 1}, nil}} {
			var page spotClosedLegsBody
			spotGet(t, dbOnly, path, http.StatusOK, &page)
			ids := make([]int, len(page.Items))
			for i, leg := range page.Items {
				ids[i] = leg.ID
				seen[leg.ID] = true
			}
			spotEqual(t, path+" ids", ids, want.ids)
			spotEqual(t, path+" nextCursor", page.NextCursor, want.next)
			if page.Count != len(want.ids) || page.Limit != 5 {
				t.Fatalf("%s: count %d limit %d", path, page.Count, page.Limit)
			}
			if page.NextCursor != nil {
				path = fmt.Sprintf("%s/closed-legs?limit=5&afterId=%d", userPath, page.NextCursor.AfterID)
			}
		}
		if len(seen) != 12 {
			t.Fatalf("%d distinct legs, want 12", len(seen))
		}
	})

	traders := [][]byte{addr(0xc1, 0x00), addr(0xc2, 0x00), addr(0xc3, 0x00)}
	seedTraders := func(t *testing.T) {
		t.Helper()
		spotReset(t, db)
		spotRegister(t, db, synthetic, base)
		for i, legs := range [][]struct {
			buy            bool
			stable, tokens string
		}{
			{{true, "10", "10"}, {false, "30", "10"}},
			{{true, "10", "10"}, {false, "10", "5"}, {false, "2", "5"}},
			{{true, "100", "100"}, {false, "40", "50"}, {false, "45", "50"}},
		} {
			for j, leg := range legs {
				block := int64(300 + 10*i + j)
				spotMarket(t, db, block, base.Add(time.Duration(block)*time.Second), traders[i], synthetic.usdl, paxToken, leg.buy, leg.stable, leg.tokens)
			}
		}
	}
	for _, c := range []struct {
		name, kind string
		order      []int
		values     []string
	}{
		{"leaderboard_pnl", "pnl", []int{0, 1, 2}, []string{e6(t, "20"), e6(t, "2"), e6(t, "-15")}},
		{"leaderboard_volume", "volume", []int{2, 0, 1}, []string{e6(t, "185"), e6(t, "40"), e6(t, "22")}},
		{"leaderboard_win_rate", "win-rate", []int{0, 1, 2}, []string{"100.0000", "50.0000", "0.0000"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			seedTraders(t)
			var body spotLeaderboardBody
			fields := spotGet(t, dbOnly, "/v1/leaderboards/"+c.kind, http.StatusOK, &body)
			wantKeys(t, fields, "kind", "as_of", "omitted_accounts", "items")
			spotBoardAsOf(t, body.AsOf)
			want := spotLeaderboardBody{Kind: c.kind, AsOf: body.AsOf}
			for rank, i := range c.order {
				want.Items = append(want.Items, spotRankJSON{Rank: rank + 1, Address: spotHexOf(traders[i]), Value: c.values[rank]})
			}
			spotEqual(t, c.kind, body, want)
			body = spotLeaderboardBody{}
			spotGet(t, dbOnly, "/v1/leaderboards/"+c.kind+"?limit=1", http.StatusOK, &body)
			spotEqual(t, c.kind+" limit 1", body, spotLeaderboardBody{Kind: c.kind, AsOf: want.AsOf, Items: want.Items[:1]})
		})
	}

	t.Run("leaderboard_survives_unfoldable_account", func(t *testing.T) {
		seedTraders(t)
		// An oversell (a sell of tokens received by transfer) cannot be folded;
		// that account is left off and everyone else is still ranked.
		oversold := addr(0xc4, 0x00)
		spotMarket(t, db, 400, base.Add(400*time.Second), oversold, synthetic.usdl, paxToken, false, "50", "5")
		users := &SpotUsers{db: db}
		mux := spotMux(users)
		want := spotLeaderboardBody{Kind: "pnl", Items: []spotRankJSON{
			{Rank: 1, Address: spotHexOf(traders[0]), Value: e6(t, "20")},
			{Rank: 2, Address: spotHexOf(traders[1]), Value: e6(t, "2")},
			{Rank: 3, Address: spotHexOf(traders[2]), Value: e6(t, "-15")},
		}}
		var body spotLeaderboardBody
		spotGet(t, mux, "/v1/leaderboards/pnl", http.StatusOK, &body)
		// The board says it left one account out.
		spotBoardAsOf(t, body.AsOf)
		want.AsOf, want.OmittedAccounts = body.AsOf, 1
		spotEqual(t, "pnl with an unfoldable account", body, want)
		spotRefused(t, mux, "/v1/users/"+spotHexOf(oversold)+"/pnl/summary", http.StatusUnprocessableEntity,
			"pnl: oversell attribution anomaly; reconcile unknown inventory")

		// Within the TTL the board is served from the last fold; past it the
		// next request folds again.
		late := addr(0xc5, 0x00)
		spotMarket(t, db, 410, base.Add(410*time.Second), late, synthetic.usdl, paxToken, true, "10", "10")
		spotMarket(t, db, 411, base.Add(411*time.Second), late, synthetic.usdl, paxToken, false, "110", "10")
		body = spotLeaderboardBody{}
		spotGet(t, mux, "/v1/leaderboards/pnl", http.StatusOK, &body)
		spotEqual(t, "pnl within the TTL", body, want)
		users.board.at = users.board.at.Add(-spotBoardTTL)
		body = spotLeaderboardBody{}
		spotGet(t, mux, "/v1/leaderboards/pnl", http.StatusOK, &body)
		spotBoardAsOf(t, body.AsOf)
		if body.AsOf < want.AsOf {
			t.Fatalf("rebuilt as_of %s before %s", body.AsOf, want.AsOf)
		}
		rebuilt := spotLeaderboardBody{Kind: "pnl", AsOf: body.AsOf, OmittedAccounts: 1,
			Items: []spotRankJSON{{Rank: 1, Address: spotHexOf(late), Value: e6(t, "100")}}}
		for _, it := range want.Items {
			it.Rank++
			rebuilt.Items = append(rebuilt.Items, it)
		}
		spotEqual(t, "pnl past the TTL", body, rebuilt)
	})

	t.Run("leaderboard_one_fold_for_concurrent_callers", func(t *testing.T) {
		seedTraders(t)
		users := &SpotUsers{db: db}
		// Hold the fold on a table lock so the callers overlap it.
		lock, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback()
		if _, err := lock.Exec(`LOCK TABLE perps.spot_swaps IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal(err)
		}
		type result struct {
			body spotLeaderboardBody
			err  error
		}
		call := func(ctx context.Context) chan result {
			done := make(chan result, 1)
			go func() {
				body, err := users.rank(ctx, "pnl", maxLimit)
				done <- result{body, err}
			}()
			return done
		}
		// The first caller starts the fold, then goes away.
		ctx, cancel := context.WithCancel(context.Background())
		first := call(ctx)
		for deadline := time.Now().Add(10 * time.Second); users.board.folds.Load() == 0; time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("the fold never started")
			}
		}
		cancel()
		select {
		case r := <-first:
			if !errors.Is(r.err, context.Canceled) {
				t.Fatalf("cancelled caller: %v", r.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the cancelled caller is still waiting")
		}
		// The others wait on the same fold, which the cancellation left running.
		waiters := []chan result{call(context.Background()), call(context.Background())}
		select {
		case <-waiters[0]:
			t.Fatal("a waiter returned while the fold was held")
		case <-waiters[1]:
			t.Fatal("a waiter returned while the fold was held")
		case <-time.After(200 * time.Millisecond):
		}
		if err := lock.Rollback(); err != nil {
			t.Fatal(err)
		}
		var got []spotLeaderboardBody
		for _, w := range waiters {
			select {
			case r := <-w:
				if r.err != nil {
					t.Fatalf("waiter: %v", r.err)
				}
				got = append(got, r.body)
			case <-time.After(30 * time.Second):
				t.Fatal("a waiter never got the fold")
			}
		}
		spotBoardAsOf(t, got[0].AsOf)
		want := spotLeaderboardBody{Kind: "pnl", AsOf: got[0].AsOf, Items: []spotRankJSON{
			{Rank: 1, Address: spotHexOf(traders[0]), Value: e6(t, "20")},
			{Rank: 2, Address: spotHexOf(traders[1]), Value: e6(t, "2")},
			{Rank: 3, Address: spotHexOf(traders[2]), Value: e6(t, "-15")},
		}}
		spotEqual(t, "first waiter", got[0], want)
		spotEqual(t, "second waiter", got[1], want)
		if n := users.board.folds.Load(); n != 1 {
			t.Fatalf("%d folds, want 1", n)
		}
	})

	t.Run("unknown_kind_404", func(t *testing.T) {
		spotRefused(t, dbOnly, "/v1/leaderboards/streak", http.StatusNotFound, "unknown leaderboard streak")
	})

	t.Run("holdings", func(t *testing.T) {
		client, tk := spotNode(t)
		s, err := NewSpotUsers(db, client)
		if err != nil {
			t.Fatal(err)
		}
		mux := spotMux(s)
		spotReset(t, db)
		spotRegister(t, db, tk, base)
		head, err := client.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var body spotHoldingsBody
		fields := spotGet(t, mux, userPath+"/holdings", http.StatusOK, &body)
		wantKeys(t, fields, "address", "native", "erc20")
		native, err := client.BalanceAt(context.Background(), common.BytesToAddress(spotUser), nil)
		if err != nil {
			t.Fatal(err)
		}
		want := spotHoldingsBody{Address: spotUserHex, Native: spotNativeJSON{Symbol: "PAX", Decimals: 18, Balance: native.String()},
			ERC20: []spotHoldingJSON{}}
		symbols := []*string{nil, spotPtr("PAX"), spotPtr("SID")}
		if common.BytesToAddress(tk.usdl) == store.USDL {
			symbols[0] = spotPtr("USDL")
		}
		for i, token := range [][]byte{tk.usdl, tk.pax, tk.sid} {
			balance := spotNodeBalance(t, client, token)
			if balance == "0" {
				continue
			}
			want.ERC20 = append(want.ERC20, spotHoldingJSON{Token: spotHexOf(token), Balance: balance, Symbol: symbols[i],
				Decimals: spotPtr(6), Type: "ERC-20"})
		}
		for i := range body.ERC20 {
			n, err := strconv.ParseUint(*body.ERC20[i].BlockNumber, 10, 64)
			if err != nil || n < head {
				t.Fatalf("blockNumber %s below head %d", *body.ERC20[i].BlockNumber, head)
			}
			body.ERC20[i].BlockNumber = nil
		}
		spotEqual(t, "holdings", body, want)
		if len(body.ERC20) != 1 || body.ERC20[0].Token != spotHexOf(tk.sid) || body.ERC20[0].Balance == "0" {
			t.Fatalf("holdings erc20 = %+v, want one non-zero entry for SID %s", body.ERC20, spotHexOf(tk.sid))
		}
	})

	t.Run("positions", func(t *testing.T) {
		client, tk := spotNode(t)
		s, err := NewSpotUsers(db, client)
		if err != nil {
			t.Fatal(err)
		}
		spotReset(t, db)
		spotScenario(t, db, tk, base)
		var body spotPositionsBody
		fields := spotGet(t, spotMux(s), userPath+"/positions", http.StatusOK, &body)
		wantKeys(t, fields, "address", "positions")
		var positions []map[string]json.RawMessage
		if err := json.Unmarshal(fields["positions"], &positions); err != nil || len(positions) != 2 {
			t.Fatalf("positions %s: %v", fields["positions"], err)
		}
		wantKeys(t, positions[0], "token", "symbol", "name", "decimals", "isStablecoin", "derived", "paxscan", "externalActivity", "externalQtyDelta")
		position := func(token []byte, symbol string, d spotDerivedJSON) spotPositionJSON {
			node := spotNodeBalance(t, client, token)
			nodeInt, _ := new(big.Int).SetString(node, 10)
			qty, _ := new(big.Int).SetString(d.Qty, 10)
			delta := new(big.Int).Sub(nodeInt, qty)
			return spotPositionJSON{Token: spotHexOf(token), Symbol: spotPtr(symbol), Decimals: spotPtr(6), Derived: d,
				Paxscan: spotQtyJSON{Qty: node}, ExternalActivity: delta.Sign() != 0, ExternalQtyDelta: delta.String()}
		}
		spotEqual(t, "positions", body, spotPositionsBody{Address: spotUserHex, Positions: []spotPositionJSON{
			position(tk.pax, "PAX", spotDerivedJSON{Qty: e6(t, "30"), AvgEntryPrice: e18(t, "2"), AvgExitPrice: spotPtr(e18(t, "3")),
				TotalBought: e6(t, "50"), TotalSold: e6(t, "20"), TotalCostBasis: e6(t, "100"), TotalProceeds: e6(t, "60"),
				RealizedPnl: e6(t, "20"), RealizedPnlUsd: e6(t, "20"), TradeCount: 2, FirstTradeAt: spotPtr(at(10)), LastTradeAt: spotPtr(at(11))}),
			position(tk.sid, "SID", spotDerivedJSON{Qty: "0", AvgEntryPrice: "0", AvgExitPrice: spotPtr(e18(t, "2")),
				TotalBought: e6(t, "20"), TotalSold: e6(t, "20"), TotalCostBasis: e6(t, "30"), TotalProceeds: e6(t, "40"),
				RealizedPnl: e6(t, "10"), RealizedPnlUsd: e6(t, "10"), TradeCount: 2, FirstTradeAt: spotPtr(at(13)), LastTradeAt: spotPtr(at(15))}),
		}})
	})

	t.Run("address_case", func(t *testing.T) {
		upperPath := "/v1/users/" + strings.Replace(spotUserHex, "a1", "A1", 1)
		spotReset(t, db)
		spotScenario(t, db, synthetic, base)
		var lower, upper spotClosedLegsBody
		spotGet(t, dbOnly, userPath+"/closed-legs", http.StatusOK, &lower)
		spotGet(t, dbOnly, upperPath+"/closed-legs", http.StatusOK, &upper)
		spotEqual(t, "closed-legs by case", upper, lower)
		spotEqual(t, "closed-legs address", upper.Address, spotUserHex)
		spotRefused(t, dbOnly, "/v1/users/0x00a1/holdings", http.StatusBadRequest, "invalid address")
		spotRefused(t, dbOnly, "/v1/users/"+strings.Replace(spotUserHex, "a1", "g1", 1)+"/holdings", http.StatusBadRequest, "invalid address")

		client, tk := spotNode(t)
		s, err := NewSpotUsers(db, client)
		if err != nil {
			t.Fatal(err)
		}
		mux := spotMux(s)
		spotReset(t, db)
		spotRegister(t, db, tk, base)
		var lowerH, upperH spotHoldingsBody
		spotGet(t, mux, userPath+"/holdings", http.StatusOK, &lowerH)
		spotGet(t, mux, upperPath+"/holdings", http.StatusOK, &upperH)
		for _, body := range []*spotHoldingsBody{&lowerH, &upperH} {
			for i := range body.ERC20 {
				body.ERC20[i].BlockNumber = nil
			}
		}
		spotEqual(t, "holdings by case", upperH, lowerH)
		spotEqual(t, "holdings address", upperH.Address, spotUserHex)
	})
}
