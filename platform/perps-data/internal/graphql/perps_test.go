package graphql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	graphql "github.com/graph-gophers/graphql-go"
	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/pnl"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

var (
	traderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	traderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	keeper  = common.HexToAddress("0x00000000000000000000000000000000000000c3")
	vaultA  = common.HexToAddress("0x00000000000000000000000000000000000000d4")
)

const (
	usd1     = "1000000000000000000"
	usd20    = "20000000000000000000"
	usd98    = "98000000000000000000"
	usd500   = "500000000000000000000"
	usd1000  = "1000000000000000000000"
	usd2000  = "2000000000000000000000"
	px3000   = "3000000000000000000000"
	px3600   = "3600000000000000000000"
	px49000  = "49000000000000000000000"
	px50000  = "50000000000000000000000"
	px51000  = "51000000000000000000000"
	rate     = "1000000000"
	rate24h  = "86400000000000"
	settled  = "1000000000000000"
	usdLoss  = "-100000000000000000000"
	funding  = "-2000000000000000000"
	usdl1000 = "1000000000"
)

func lower(a common.Address) string { return strings.ToLower(a.Hex()) }

func blockHash(n uint64) common.Hash { return crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n))) }

func txHash(n uint64, txIndex int64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("tx/%d/%d", n, txIndex)))
}

func txHex(n uint64, txIndex int64) string { return txHash(n, txIndex).Hex() }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// block collects the decoded logs of one block in log order.
type block struct {
	n   uint64
	at  time.Time
	log int64
	res decode.Result
}

// fact is the common part of the next log of transaction txIndex; the last
// transaction index of a block is its perps EndBlock receipt.
func (b *block) fact(txIndex int64, endBlock bool, kind string, account common.Address) decode.Fact {
	f := decode.Fact{Block: b.n, BlockHash: blockHash(b.n), TxIndex: txIndex, LogIndex: b.log, TxHash: txHash(b.n, txIndex),
		ReceiptKind: decode.ReceiptTx, BlockTime: b.at, Emitter: decode.PerpsAddress, Kind: kind, Account: account}
	if endBlock {
		f.ReceiptKind = decode.ReceiptPerpsEndBlock
	} else {
		from := account
		f.TxFrom = &from
	}
	b.log++
	return f
}

func (b *block) add(name string, args map[string]any, row decode.Row) {
	f := row.Common()
	b.res.Decoded = append(b.res.Decoded, decode.Decoded{
		Event: decode.Event{EventName: name, Args: args, BlockNumber: f.Block, TxHash: f.TxHash.Hex(), LogIndex: uint(f.LogIndex)},
		Rows:  []decode.Row{row},
	})
}

func openPerpsDB(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

// seedPerps writes blocks 100 to 102 the way the follower commits them, the
// perps.blocks row, store.WriteBlock and the head cursor: trader A opens
// position 1 on ETH from a filled market order, receives funding, cancels a
// limit order and closes at a profit; trader B opens short position 2 on SOL
// and is liquidated in the EndBlock of block 101.
func seedPerps(t *testing.T, db *sql.DB, now time.Time) (b100, b101, b102 time.Time) {
	t.Helper()
	ctx := context.Background()
	b100, b101, b102 = now.Add(-20*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Minute)

	one := &block{n: 100, at: b100}
	f := one.fact(0, false, "created", traderA)
	one.add("VaultCreated", map[string]any{"user": lower(traderA), "vault": lower(vaultA)}, decode.VaultCreatedRow{Fact: f, User: traderA, Vault: vaultA})
	f = one.fact(0, false, "deposited", traderA)
	one.add("CollateralDeposited", map[string]any{"amount": usdl1000}, decode.CollateralDepositedRow{Fact: f, AccountArg: traderA, Amount: usdl1000, NewBalance: usdl1000})
	f = one.fact(0, false, "placed", traderA)
	one.add("OrderPlaced", map[string]any{"orderId": "7"}, decode.OrderPlacedRow{Fact: f, OrderID: "7", User: traderA, MarketID: "1", OrderType: 4,
		IsLong: true, TriggerPrice: "0", SizeUSD: usd1000})
	f = one.fact(0, false, "fill", traderA)
	one.add("Fill", map[string]any{"takerOrderId": "7"}, decode.FillRow{Fact: f, MarketID: "1", MakerOrderID: "0", TakerOrderID: "7", Maker: traderB,
		Taker: traderA, TakerIsLong: true, Price: px50000, SizeUSD: usd1000, MakerFeeUSD: "0", TakerFeeUSD: "0"})
	f = one.fact(0, false, "opened", traderA)
	one.add("PositionOpened", map[string]any{"positionId": "1"}, decode.PositionOpenedRow{Fact: f, PositionID: "1", User: traderA, MarketID: "1",
		IsLong: true, SizeUSD: usd1000, Leverage: "10000000000000000000", EntryPrice: px50000, CollateralToken: store.USDL, CollateralAmount: "100000000"})
	f = one.fact(0, false, "settled", traderA)
	one.add("TradeSettled", map[string]any{"positionId": "1"}, decode.TradeSettledRow{Fact: f, PositionID: "1", User: traderA, MarketID: "1",
		TradeType: 0, SizeUSD: usd1000, ExecutionPrice: px50000, GrossPnl: "0", TotalFeesUSD: usd1, FundingPaidUSD: "0", NetPayoutTokens: "0"})
	f = one.fact(0, false, "collected", traderA)
	one.add("FeeCollected", map[string]any{"positionId": "1"}, decode.FeeCollectedRow{Fact: f, PositionID: "1", User: traderA, MarketID: "1",
		FeeType: 0, FeeUSD: usd1, FeeTokens: "1000000", Token: store.USDL})
	f = one.fact(0, false, "oi", traderA)
	one.add("OpenInterestChanged", map[string]any{"marketId": "1"}, decode.OpenInterestChangedRow{Fact: f, MarketID: "1", LongOI: usd1000, ShortOI: "0",
		DeltaUSD: usd1000, IsIncrease: true})
	f = one.fact(1, false, "opened", traderB)
	one.add("PositionOpened", map[string]any{"positionId": "2"}, decode.PositionOpenedRow{Fact: f, PositionID: "2", User: traderB, MarketID: "2",
		IsLong: false, SizeUSD: usd500, Leverage: "20000000000000000000", EntryPrice: px3000, CollateralToken: store.USDL, CollateralAmount: "25000000"})
	f = one.fact(1, false, "oi", traderB)
	one.add("OpenInterestChanged", map[string]any{"marketId": "2"}, decode.OpenInterestChangedRow{Fact: f, MarketID: "2", LongOI: "0", ShortOI: usd500,
		DeltaUSD: usd500, IsIncrease: true})
	f = one.fact(2, true, "mark", decode.PerpsAddress)
	one.add("MarkPriceUpdated", map[string]any{"marketId": "1"}, decode.MarkPriceUpdatedRow{Fact: f, MarketID: "1", MarkPrice: px50000,
		IndexPrice: px50000, MidPrice: px50000})
	f = one.fact(2, true, "rate", decode.PerpsAddress)
	one.add("FundingRateUpdated", map[string]any{"marketId": "1"}, decode.FundingRateUpdatedRow{Fact: f, MarketID: "1", NewRatePerSecond: rate,
		FundingRate24h: rate24h})
	f = one.fact(2, true, "settled", decode.PerpsAddress)
	one.add("FundingSettled", map[string]any{"marketId": "1"}, decode.FundingSettledRow{Fact: f, MarketID: "1", FundingRate: settled,
		LongPayment: "0", ShortPayment: "0"})
	f = one.fact(2, true, "applied", traderA)
	one.add("PositionFundingApplied", map[string]any{"positionId": "1"}, decode.PositionFundingAppliedRow{Fact: f, PositionID: "1", User: traderA,
		MarketID: "1", FundingPaymentUSD: funding, NewCollateralUSD: usd98, NewCollateralAmount: "98000000"})

	two := &block{n: 101, at: b101}
	f = two.fact(0, false, "placed", traderA)
	two.add("OrderPlaced", map[string]any{"orderId": "8"}, decode.OrderPlacedRow{Fact: f, OrderID: "8", User: traderA, MarketID: "1", OrderType: 0,
		IsLong: true, TriggerPrice: px49000, SizeUSD: usd500})
	f = two.fact(0, false, "cancelled", traderA)
	two.add("OrderCancelled", map[string]any{"orderId": "8"}, decode.OrderCancelledRow{Fact: f, OrderID: "8", User: traderA})
	f = two.fact(0, false, "closed", traderA)
	two.add("PositionClosed", map[string]any{"positionId": "1"}, decode.PositionClosedRow{Fact: f, PositionID: "1", User: traderA, MarketID: "1",
		ClosedSizeUSD: usd1000, ExitPrice: px51000, RealizedPnl: usd20, IsFullClose: true})
	f = two.fact(0, false, "settled", traderA)
	two.add("TradeSettled", map[string]any{"positionId": "1"}, decode.TradeSettledRow{Fact: f, PositionID: "1", User: traderA, MarketID: "1",
		TradeType: 1, SizeUSD: usd1000, ExecutionPrice: px51000, GrossPnl: usd20, TotalFeesUSD: "0", FundingPaidUSD: "0", NetPayoutTokens: "119000000"})
	f = two.fact(0, false, "oi", traderA)
	two.add("OpenInterestChanged", map[string]any{"marketId": "1"}, decode.OpenInterestChangedRow{Fact: f, MarketID: "1", LongOI: "0", ShortOI: "0",
		DeltaUSD: usd1000, IsIncrease: false})
	f = two.fact(1, true, "mark", decode.PerpsAddress)
	two.add("MarkPriceUpdated", map[string]any{"marketId": "1"}, decode.MarkPriceUpdatedRow{Fact: f, MarketID: "1", MarkPrice: px51000,
		IndexPrice: px51000, MidPrice: px51000})
	f = two.fact(1, true, "closed", traderB)
	two.add("PositionClosed", map[string]any{"positionId": "2"}, decode.PositionClosedRow{Fact: f, PositionID: "2", User: traderB, MarketID: "2",
		ClosedSizeUSD: usd500, ExitPrice: px3600, RealizedPnl: usdLoss, IsFullClose: true})
	f = two.fact(1, true, "liquidated", traderB)
	two.add("Liquidation", map[string]any{"positionId": "2"}, decode.LiquidationRow{Fact: f, PositionID: "2", User: traderB, MarketID: "2",
		LiquidationPrice: px3600, Penalty: "5000000000000000000", Keeper: keeper})
	f = two.fact(1, true, "draw", decode.PerpsAddress)
	two.add("InsuranceDraw", map[string]any{"marketId": "2"}, decode.InsuranceDrawRow{Fact: f, MarketID: "2", Amount: "5000000", NewBalance: "95000000"})
	f = two.fact(1, true, "oi", decode.PerpsAddress)
	two.add("OpenInterestChanged", map[string]any{"marketId": "2"}, decode.OpenInterestChangedRow{Fact: f, MarketID: "2", LongOI: "0", ShortOI: "0",
		DeltaUSD: usd500, IsIncrease: false})

	three := &block{n: 102, at: b102}

	parent := common.Hash{}
	for _, b := range []*block{one, two, three} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		header := store.Block{Number: b.n, Hash: blockHash(b.n), ParentHash: parent, Time: b.at, Proposer: strings.Repeat("AB", 20), TxCount: 3}
		if _, err := tx.ExecContext(ctx, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
			int64(header.Number), header.Hash.Bytes(), header.ParentHash.Bytes(), header.Time, header.Proposer, header.TxCount); err != nil {
			_ = tx.Rollback()
			t.Fatalf("block row %d: %v", b.n, err)
		}
		if err := store.WriteBlock(ctx, tx, header, &b.res); err != nil {
			_ = tx.Rollback()
			t.Fatalf("WriteBlock %d: %v", b.n, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO perps.cursors (name, height, hash, updated_at) VALUES ($1, $2, $3, now())
ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash, updated_at = now()`,
			headCursor, int64(header.Number), header.Hash.Bytes()); err != nil {
			_ = tx.Rollback()
			t.Fatalf("head cursor %d: %v", b.n, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		parent = header.Hash
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO perps.index_prices (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind,
    block_time, emitter, kind, account, market_id, index_price, mark_price, basis, timestamp_ms, sources, required)
VALUES (100, $1, 3, 20, 0, $2, 'perps_endblock', $3, $4, 'aggregate', $4, 1, $5, $5, 0, $6, 4, 3)`,
		blockHash(100).Bytes(), txHash(100, 3).Bytes(), b100, decode.PerpsAddress.Bytes(), px50000, b100.UnixMilli()); err != nil {
		t.Fatalf("seed index_prices: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO perps.fee_config (id, taker_fee_ppm, maker_fee_ppm, liquidation_fee_ppm,
    insurance_share_ppm, refreshed_block) VALUES (1, 650, 200, 5000, 100000, 100)`); err != nil {
		t.Fatalf("seed fee_config: %v", err)
	}
	if err := (pnl.Leaderboard{}).Rebuild(ctx, tx, now); err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return b100, b101, b102
}

// countingConnector counts the queries database/sql sends to the server.
type countingConnector struct {
	driver.Connector
	queries *atomic.Int64
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	q, ok := conn.(driver.QueryerContext)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("connection %T has no QueryContext", conn)
	}
	return countingConn{Conn: conn, queryer: q, queries: c.queries}, nil
}

type countingConn struct {
	driver.Conn
	queryer driver.QueryerContext
	queries *atomic.Int64
}

func (c countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries.Add(1)
	return c.queryer.QueryContext(ctx, query, args)
}

func exec(t *testing.T, s *graphql.Schema, query string) (string, []string) {
	t.Helper()
	res := s.Exec(context.Background(), query, "", nil)
	errs := make([]string, len(res.Errors))
	for i, e := range res.Errors {
		errs[i] = e.Message
	}
	return string(res.Data), errs
}

// check runs query and compares its data with want as JSON values.
func check(t *testing.T, s *graphql.Schema, query, want string) {
	t.Helper()
	data, errs := exec(t, s, query)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	var got, exp any
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("data %s: %v", data, err)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatalf("want %s: %v", want, err)
	}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("got  %s\nwant %s", data, want)
	}
}

func TestPerpsResolvers(t *testing.T) {
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db := openPerpsDB(t, url)
	now := time.Now().UTC().Truncate(time.Second)
	b100, b101, b102 := seedPerps(t, db, now)
	s, err := NewSchema(db)
	if err != nil {
		t.Fatal(err)
	}
	a, b, k, usdl := lower(traderA), lower(traderB), lower(keeper), lower(store.USDL)
	hour := b102.Truncate(time.Hour)

	t.Run("leaderboard", func(t *testing.T) {
		check(t, s, `{ leaderboard { rank userAddress totalRealizedPnl totalTrades winRate avgHoldTime openPositions totalVolume } }`,
			fmt.Sprintf(`{"leaderboard":[
{"rank":1,"userAddress":%q,"totalRealizedPnl":%q,"totalTrades":2,"winRate":100,"avgHoldTime":64800,"openPositions":0,"totalVolume":%q},
{"rank":2,"userAddress":%q,"totalRealizedPnl":%q,"totalTrades":0,"winRate":0,"avgHoldTime":64800,"openPositions":0,"totalVolume":"0"}]}`,
				a, usd20, usd2000, b, usdLoss))
		check(t, s, `{ leaderboard(limit: 1, offset: 1) { rank } }`, `{"leaderboard":[{"rank":2}]}`)
	})

	positionWant := fmt.Sprintf(`{"positionId":"1","userAddress":%q,"marketId":1,"isLong":true,"sizeUsd":%q,"leverage":"10000000000000000000",
"entryPrice":%q,"collateralToken":%q,"collateralAmount":"98000000","collateralUsd":%q,"status":"closed","realizedPnl":%q,
"exitPrice":%q,"openedAt":%q,"closedAt":%q,"openBlock":100,"closeBlock":101,"openTxHash":%q,"closeTxHash":%q,"market":{"symbol":"ETH"}}`,
		a, usd1000, px50000, usdl, usd98, usd20, px51000, ts(b100), ts(b101), txHex(100, 0), txHex(101, 0))
	const positionFields = `positionId userAddress marketId isLong sizeUsd leverage entryPrice collateralToken collateralAmount collateralUsd
    status realizedPnl exitPrice openedAt closedAt openBlock closeBlock openTxHash closeTxHash market { symbol }`

	t.Run("position", func(t *testing.T) {
		check(t, s, `{ position(positionId: "1") { `+positionFields+` } }`, `{"position":`+positionWant+`}`)
		check(t, s, `{ position(positionId: "999") { positionId } }`, `{"position":null}`)
		if _, errs := exec(t, s, `{ position(positionId: "x1") { positionId } }`); len(errs) != 1 || errs[0] != `invalid positionId "x1"` {
			t.Fatalf("errors %v", errs)
		}
	})

	t.Run("positions", func(t *testing.T) {
		check(t, s, `{ positions(userAddress: "`+strings.ToUpper(a[2:])+`") { `+positionFields+` } }`, `{"positions":[`+positionWant+`]}`)
		check(t, s, `{ positions { positionId status market { symbol } } }`,
			`{"positions":[{"positionId":"2","status":"liquidated","market":{"symbol":"SOL"}},{"positionId":"1","status":"closed","market":{"symbol":"ETH"}}]}`)
		check(t, s, `{ positions(marketId: 2, status: "liquidated") { positionId } }`, `{"positions":[{"positionId":"2"}]}`)
		check(t, s, `{ positions(status: "open") { positionId } }`, `{"positions":[]}`)
	})

	t.Run("position_market_batched", func(t *testing.T) {
		base, err := pq.NewConnector(url)
		if err != nil {
			t.Fatal(err)
		}
		var queries atomic.Int64
		counted := sql.OpenDB(countingConnector{Connector: base, queries: &queries})
		t.Cleanup(func() { _ = counted.Close() })
		cs, err := NewSchema(counted)
		if err != nil {
			t.Fatal(err)
		}
		check(t, cs, `{ positions { positionId market { symbol } } }`,
			`{"positions":[{"positionId":"2","market":{"symbol":"SOL"}},{"positionId":"1","market":{"symbol":"ETH"}}]}`)
		if n := queries.Load(); n != 2 {
			t.Fatalf("GetPositions with market { symbol } sent %d queries, want 2", n)
		}
		queries.Store(0)
		if _, errs := exec(t, cs, `{ markets { marketId latestPrice { price } } }`); len(errs) > 0 {
			t.Fatal(errs)
		}
		if n := queries.Load(); n != 2 {
			t.Fatalf("markets with latestPrice sent %d queries, want 2", n)
		}
	})

	t.Run("limit_clamp", func(t *testing.T) {
		check(t, s, `{ markPriceHistory(marketId: 1, limit: 0) { blockNumber } }`, `{"markPriceHistory":[{"blockNumber":101}]}`)
		check(t, s, `{ markPriceHistory(marketId: 1, limit: 100000, offset: -5) { blockNumber } }`,
			`{"markPriceHistory":[{"blockNumber":101},{"blockNumber":100}]}`)
		check(t, s, `{ markPriceHistory(marketId: 1, limit: 1, offset: 1) { blockNumber } }`, `{"markPriceHistory":[{"blockNumber":100}]}`)
		check(t, s, `{ markPriceHistory(marketId: 1, offset: 200000) { blockNumber } }`, `{"markPriceHistory":[]}`)
		if got := len(mustList(t, s, `{ markets { marketId } }`, "markets")); got != 18 {
			t.Fatalf("markets default limit answered %d rows, want 18", got)
		}
	})

	t.Run("trades", func(t *testing.T) {
		check(t, s, `{ trades(userAddress: "`+a+`") { positionId userAddress marketId tradeType isLong sizeUsd price executionPrice realizedPnl feeUsd blockNumber txHash blockTimestamp } }`,
			fmt.Sprintf(`{"trades":[
{"positionId":"1","userAddress":%[1]q,"marketId":1,"tradeType":"close","isLong":true,"sizeUsd":%[2]q,"price":%[3]q,"executionPrice":%[3]q,"realizedPnl":%[4]q,"feeUsd":"0","blockNumber":101,"txHash":%[5]q,"blockTimestamp":%[6]q},
{"positionId":"1","userAddress":%[1]q,"marketId":1,"tradeType":"open","isLong":true,"sizeUsd":%[2]q,"price":%[7]q,"executionPrice":%[7]q,"realizedPnl":"0","feeUsd":%[8]q,"blockNumber":100,"txHash":%[9]q,"blockTimestamp":%[10]q}]}`,
				a, usd1000, px51000, usd20, txHex(101, 0), ts(b101), px50000, usd1, txHex(100, 0), ts(b100)))
		check(t, s, `{ trades(tradeType: "open") { blockNumber } }`, `{"trades":[{"blockNumber":100}]}`)
		check(t, s, `{ trades(tradeType: "swap") { blockNumber } }`, `{"trades":[]}`)
	})

	orderFields := `orderId userAddress marketId orderType orderTypeName isLong triggerPrice sizeUsd remainingSize status positionId executionPrice
    failureReason placedAt resolvedAt placedBlock resolvedBlock placedTxHash resolvedTxHash blockTimestamp`
	order7 := fmt.Sprintf(`{"orderId":"7","userAddress":%q,"marketId":1,"orderType":4,"orderTypeName":"market","isLong":true,"triggerPrice":"0",
"sizeUsd":%q,"remainingSize":"0","status":"filled","positionId":null,"executionPrice":null,"failureReason":null,"placedAt":%q,"resolvedAt":%q,
"placedBlock":100,"resolvedBlock":100,"placedTxHash":%q,"resolvedTxHash":%q,"blockTimestamp":%q}`,
		a, usd1000, ts(b100), ts(b100), txHex(100, 0), txHex(100, 0), ts(b100))
	order8 := fmt.Sprintf(`{"orderId":"8","userAddress":%q,"marketId":1,"orderType":0,"orderTypeName":"limit","isLong":true,"triggerPrice":%q,
"sizeUsd":%q,"remainingSize":%q,"status":"cancelled","positionId":null,"executionPrice":null,"failureReason":null,"placedAt":%q,"resolvedAt":%q,
"placedBlock":101,"resolvedBlock":101,"placedTxHash":%q,"resolvedTxHash":%q,"blockTimestamp":%q}`,
		a, px49000, usd500, usd500, ts(b101), ts(b101), txHex(101, 0), txHex(101, 0), ts(b101))

	t.Run("order", func(t *testing.T) {
		check(t, s, `{ order(orderId: "7") { `+orderFields+` } }`, `{"order":`+order7+`}`)
		check(t, s, `{ order(orderId: "70") { orderId } }`, `{"order":null}`)
	})

	t.Run("orders", func(t *testing.T) {
		check(t, s, `{ orders(userAddress: "`+a+`") { `+orderFields+` } }`, `{"orders":[`+order8+`,`+order7+`]}`)
		check(t, s, `{ orders(status: "filled") { orderId } }`, `{"orders":[{"orderId":"7"}]}`)
		check(t, s, `{ orders(orderType: 0) { orderId } }`, `{"orders":[{"orderId":"8"}]}`)
		check(t, s, `{ orders(userAddress: "`+b+`") { orderId } }`, `{"orders":[]}`)
	})

	t.Run("liquidations", func(t *testing.T) {
		check(t, s, `{ liquidations { positionId userAddress marketId price penalty keeper sizeUsd executionPrice blockNumber txHash blockTimestamp } }`,
			fmt.Sprintf(`{"liquidations":[{"positionId":"2","userAddress":%q,"marketId":2,"price":%q,"penalty":"5000000000000000000","keeper":%q,
"sizeUsd":%q,"executionPrice":%q,"blockNumber":101,"txHash":%q,"blockTimestamp":%q}]}`, b, px3600, k, usd500, px3600, txHex(101, 1), ts(b101)))
		check(t, s, `{ liquidations(userAddress: "`+a+`") { positionId } }`, `{"liquidations":[]}`)
	})

	t.Run("market", func(t *testing.T) {
		check(t, s, `{ market(marketId: 1) { marketId name symbol maxLeverage enabled createdAt
    latestPrice { marketId price onchainTimestamp blockNumber updatedAt }
    poolState { marketId baseReserve quoteReserve oraclePrice blockNumber }
    fundingRate { marketId ratePerSecond rate24h blockNumber txHash blockTimestamp } } }`,
			fmt.Sprintf(`{"market":{"marketId":1,"name":"Ethereum","symbol":"ETH","maxLeverage":"1000","enabled":true,"createdAt":null,
"latestPrice":{"marketId":1,"price":%[1]q,"onchainTimestamp":%[2]d,"blockNumber":101,"updatedAt":%[3]q},
"poolState":{"marketId":1,"baseReserve":"0","quoteReserve":"0","oraclePrice":%[1]q,"blockNumber":101},
"fundingRate":{"marketId":1,"ratePerSecond":%[4]q,"rate24h":%[5]q,"blockNumber":100,"txHash":%[6]q,"blockTimestamp":%[7]q}}}`,
				px51000, b101.Unix(), ts(b101), rate, rate24h, txHex(100, 2), ts(b100)))
		check(t, s, `{ market(marketId: 99) { marketId } }`, `{"market":null}`)
	})

	t.Run("markets", func(t *testing.T) {
		list := mustList(t, s, `{ markets { marketId symbol latestPrice { price } } }`, "markets")
		if len(list) != 18 {
			t.Fatalf("markets answered %d rows, want 18", len(list))
		}
		want := []map[string]any{
			{"marketId": 0.0, "symbol": "BTC", "latestPrice": nil},
			{"marketId": 1.0, "symbol": "ETH", "latestPrice": map[string]any{"price": px51000}},
			{"marketId": 2.0, "symbol": "SOL", "latestPrice": nil},
		}
		if !reflect.DeepEqual(list[:3], want) {
			t.Fatalf("got %v want %v", list[:3], want)
		}
	})

	t.Run("latestPrices", func(t *testing.T) {
		check(t, s, `{ latestPrices { marketId price onchainTimestamp blockNumber updatedAt } }`,
			fmt.Sprintf(`{"latestPrices":[{"marketId":1,"price":%q,"onchainTimestamp":%d,"blockNumber":101,"updatedAt":%q}]}`, px51000, b101.Unix(), ts(b101)))
	})

	t.Run("priceHistory", func(t *testing.T) {
		check(t, s, `{ priceHistory(marketId: 1) { marketId price onchainTimestamp timestamp blockNumber txHash blockTimestamp updatedAt } }`,
			fmt.Sprintf(`{"priceHistory":[{"marketId":1,"price":%q,"onchainTimestamp":%[2]d,"timestamp":%[2]d,"blockNumber":100,"txHash":%q,
"blockTimestamp":%[4]q,"updatedAt":%[4]q}]}`, px50000, b100.Unix(), blockHash(100).Hex(), ts(b100)))
		check(t, s, `{ priceHistory(marketId: 2) { price } }`, `{"priceHistory":[]}`)
	})

	t.Run("fundingRates", func(t *testing.T) {
		check(t, s, `{ fundingRates(marketId: 1) { marketId ratePerSecond rate24h blockNumber txHash blockTimestamp } }`,
			fmt.Sprintf(`{"fundingRates":[{"marketId":1,"ratePerSecond":%q,"rate24h":%q,"blockNumber":100,"txHash":%q,"blockTimestamp":%q}]}`,
				rate, rate24h, txHex(100, 2), ts(b100)))
	})

	t.Run("userVault", func(t *testing.T) {
		check(t, s, `{ userVault(userAddress: "`+strings.ToUpper(a[2:])+`") { userAddress vaultAddress createdAt blockNumber txHash } }`,
			fmt.Sprintf(`{"userVault":{"userAddress":%q,"vaultAddress":%q,"createdAt":%q,"blockNumber":100,"txHash":%q}}`,
				a, lower(vaultA), ts(b100), txHex(100, 0)))
		check(t, s, `{ userVault(userAddress: "`+b+`") { userAddress } }`, `{"userVault":null}`)
	})

	t.Run("vaultEvents", func(t *testing.T) {
		check(t, s, `{ vaultEvents(userAddress: "`+a+`") { eventType userAddress tokenAddress amount blockNumber txHash logIndex blockTimestamp } }`,
			fmt.Sprintf(`{"vaultEvents":[{"eventType":"deposited","userAddress":%q,"tokenAddress":%q,"amount":%q,"blockNumber":100,"txHash":%q,
"logIndex":1,"blockTimestamp":%q}]}`, a, usdl, usdl1000, txHex(100, 0), ts(b100)))
		check(t, s, `{ vaultEvents(eventType: "withdrawn") { amount } }`, `{"vaultEvents":[]}`)
	})

	t.Run("collateralTokens", func(t *testing.T) {
		check(t, s, `{ collateralTokens { tokenAddress decimals isActive addedAt } }`,
			fmt.Sprintf(`{"collateralTokens":[{"tokenAddress":%q,"decimals":6,"isActive":true,"addedAt":null}]}`, usdl))
	})

	t.Run("poolStates", func(t *testing.T) {
		check(t, s, `{ poolStates { marketId baseReserve quoteReserve oraclePrice updatedAt blockNumber } }`,
			fmt.Sprintf(`{"poolStates":[{"marketId":1,"baseReserve":"0","quoteReserve":"0","oraclePrice":%q,"updatedAt":%q,"blockNumber":101}]}`,
				px51000, ts(b101)))
	})

	t.Run("poolState", func(t *testing.T) {
		check(t, s, `{ poolState(marketId: 1) { marketId oraclePrice blockNumber } }`,
			fmt.Sprintf(`{"poolState":{"marketId":1,"oraclePrice":%q,"blockNumber":101}}`, px51000))
		check(t, s, `{ poolState(marketId: 5) { marketId } }`, `{"poolState":null}`)
	})

	t.Run("feeConfig", func(t *testing.T) {
		check(t, s, `{ feeConfig { takerFeeBps makerFeeBps liquidationFeeBps insuranceFeeBps updatedAt } }`,
			fmt.Sprintf(`{"feeConfig":{"takerFeeBps":6,"makerFeeBps":2,"liquidationFeeBps":50,"insuranceFeeBps":1000,"updatedAt":%q}}`, ts(b100)))
	})

	t.Run("protocolEvents", func(t *testing.T) {
		check(t, s, `{ protocolEvents(eventName: "Liquidation") { eventName eventData blockNumber txHash logIndex blockTimestamp } }`,
			fmt.Sprintf(`{"protocolEvents":[{"eventName":"Liquidation","eventData":"{\"positionId\": \"2\"}","blockNumber":101,"txHash":%q,
"logIndex":7,"blockTimestamp":%q}]}`, txHex(101, 1), ts(b101)))
		if got := len(mustList(t, s, `{ protocolEvents(limit: 500) { id } }`, "protocolEvents")); got != 24 {
			t.Fatalf("protocolEvents answered %d rows, want 24", got)
		}
	})

	t.Run("userStats", func(t *testing.T) {
		want := fmt.Sprintf(`{"userStats":{"address":%[1]q,"userAddress":%[1]q,"totalPnl":"17000000000000000000","totalVolume":%[2]q,
"totalFeesPaid":%[3]q,"winCount":1,"lossCount":0,"liquidationCount":0,"totalPositions":1,"openPositions":0,"closedPositions":1,
"liquidatedPositions":0,"totalTrades":2,"totalRealizedPnl":%[4]q,"totalOrders":2,"activeOrders":0}}`, a, usd2000, usd1, usd20)
		const fields = `address userAddress totalPnl totalVolume totalFeesPaid winCount lossCount liquidationCount totalPositions openPositions
    closedPositions liquidatedPositions totalTrades totalRealizedPnl totalOrders activeOrders`
		check(t, s, `{ userStats(userAddress: "`+a+`") { `+fields+` } }`, want)
		check(t, s, `{ userStats(address: "`+strings.ToUpper(a[2:])+`") { `+fields+` } }`, want)
		check(t, s, `{ userStats(userAddress: "`+lower(vaultA)+`") { userAddress } }`, `{"userStats":null}`)
		if _, errs := exec(t, s, `{ userStats { userAddress } }`); len(errs) != 1 || errs[0] != "exactly one of userAddress or address is required" {
			t.Fatalf("errors %v", errs)
		}
	})

	marketStats1 := fmt.Sprintf(`{"marketId":1,"symbol":"ETH","totalPositions":1,"openPositions":0,"totalTrades":1,"totalLiquidations":0,
"totalVolume":%q,"latestPrice":%q,"latestFundingRate":%q}`, usd1000, px50000, rate)
	const marketStatsFields = `marketId symbol totalPositions openPositions totalTrades totalLiquidations totalVolume latestPrice latestFundingRate`

	t.Run("marketStats", func(t *testing.T) {
		check(t, s, `{ marketStats(marketId: 1) { `+marketStatsFields+` } }`, `{"marketStats":`+marketStats1+`}`)
		check(t, s, `{ marketStats(marketId: 2) { totalPositions totalLiquidations totalVolume latestPrice } }`,
			`{"marketStats":{"totalPositions":1,"totalLiquidations":1,"totalVolume":"0","latestPrice":null}}`)
		check(t, s, `{ marketStats(marketId: 99) { marketId } }`, `{"marketStats":null}`)
	})

	globalStats := fmt.Sprintf(`{"totalMarkets":18,"totalPositions":2,"openPositions":0,"totalTrades":1,"totalLiquidations":1,
"totalVolume":%q,"totalUsers":2,"indexerBlock":102}`, usd1000)
	const globalStatsFields = `totalMarkets totalPositions openPositions totalTrades totalLiquidations totalVolume totalUsers indexerBlock`

	t.Run("globalStats", func(t *testing.T) {
		check(t, s, `{ globalStats { `+globalStatsFields+` } }`, `{"globalStats":`+globalStats+`}`)
	})

	t.Run("indexerStatus", func(t *testing.T) {
		check(t, s, `{ indexerStatus { lastIndexedBlock chainHead blocksScanned eventsProcessed isSynced } }`,
			`{"indexerStatus":{"lastIndexedBlock":102,"chainHead":null,"blocksScanned":3,"eventsProcessed":24,"isSynced":null}}`)
	})

	t.Run("keeperCycles", func(t *testing.T) {
		check(t, s, `{ keeperCycles { id onchainTimestamp marketsUpdated ordersExecuted liquidationsExecuted ordersFailed liquidationsFailed blockNumber txHash blockTimestamp } }`,
			fmt.Sprintf(`{"keeperCycles":[
{"id":101,"onchainTimestamp":%d,"marketsUpdated":1,"ordersExecuted":0,"liquidationsExecuted":1,"ordersFailed":0,"liquidationsFailed":0,"blockNumber":101,"txHash":%q,"blockTimestamp":%q},
{"id":100,"onchainTimestamp":%d,"marketsUpdated":1,"ordersExecuted":0,"liquidationsExecuted":0,"ordersFailed":0,"liquidationsFailed":0,"blockNumber":100,"txHash":%q,"blockTimestamp":%q}]}`,
				b101.Unix(), txHex(101, 1), ts(b101), b100.Unix(), txHex(100, 2), ts(b100)))
	})

	t.Run("accountLedger", func(t *testing.T) {
		check(t, s, `{ accountLedger(userAddress: "`+a+`") { userAddress entryType tokenAddress amount positionId isDebit blockNumber txHash } }`,
			fmt.Sprintf(`{"accountLedger":[
{"userAddress":%[1]q,"entryType":6,"tokenAddress":%[2]q,"amount":"119000000","positionId":"1","isDebit":false,"blockNumber":101,"txHash":%[3]q},
{"userAddress":%[1]q,"entryType":5,"tokenAddress":%[2]q,"amount":"2000000000000000000","positionId":"1","isDebit":true,"blockNumber":100,"txHash":%[4]q},
{"userAddress":%[1]q,"entryType":4,"tokenAddress":%[2]q,"amount":"1000000","positionId":"1","isDebit":true,"blockNumber":100,"txHash":%[5]q},
{"userAddress":%[1]q,"entryType":6,"tokenAddress":%[2]q,"amount":"0","positionId":"1","isDebit":false,"blockNumber":100,"txHash":%[5]q},
{"userAddress":%[1]q,"entryType":0,"tokenAddress":%[2]q,"amount":%[6]q,"positionId":"0","isDebit":false,"blockNumber":100,"txHash":%[5]q}]}`,
				a, usdl, txHex(101, 0), txHex(100, 2), txHex(100, 0), usdl1000))
		check(t, s, `{ accountLedger(positionId: "2") { amount } }`, `{"accountLedger":[]}`)
	})

	t.Run("delegates", func(t *testing.T) {
		check(t, s, `{ delegates(userAddress: "`+a+`") { delegateAddress } }`, `{"delegates":[]}`)
		if _, errs := exec(t, s, `{ delegates(userAddress: "0x12") { delegateAddress } }`); len(errs) != 1 || errs[0] != `invalid userAddress "0x12"` {
			t.Fatalf("errors %v", errs)
		}
	})

	t.Run("tradingAccountEvents", func(t *testing.T) {
		check(t, s, `{ tradingAccountEvents(userAddress: "`+a+`") { eventType userAddress positionId tokenAddress amount blockNumber } }`,
			fmt.Sprintf(`{"tradingAccountEvents":[
{"eventType":"closed","userAddress":%[1]q,"positionId":"1","tokenAddress":null,"amount":null,"blockNumber":101},
{"eventType":"opened","userAddress":%[1]q,"positionId":"1","tokenAddress":%[2]q,"amount":null,"blockNumber":100},
{"eventType":"deposited","userAddress":%[1]q,"positionId":null,"tokenAddress":%[2]q,"amount":%[3]q,"blockNumber":100}]}`, a, usdl, usdl1000))
		check(t, s, `{ tradingAccountEvents(userAddress: "`+a+`", eventType: "deposited") { extraData } }`,
			fmt.Sprintf(`{"tradingAccountEvents":[{"extraData":"{\"account\": \"%s\", \"new_balance\": \"%s\"}"}]}`, a, usdl1000))
	})

	t.Run("fees", func(t *testing.T) {
		check(t, s, `{ fees(userAddress: "`+a+`") { positionId userAddress marketId feeType feeTypeName feeUsd feeTokens tokenAddress blockNumber txHash logIndex } }`,
			fmt.Sprintf(`{"fees":[{"positionId":"1","userAddress":%q,"marketId":1,"feeType":0,"feeTypeName":"trading","feeUsd":%q,"feeTokens":"1000000",
"tokenAddress":%q,"blockNumber":100,"txHash":%q,"logIndex":6}]}`, a, usd1, usdl, txHex(100, 0)))
		check(t, s, `{ fees(feeType: 4) { feeUsd } }`, `{"fees":[]}`)
	})

	t.Run("tradeSettlements", func(t *testing.T) {
		check(t, s, `{ tradeSettlements(positionId: "1", tradeType: 1) { positionId userAddress marketId tradeType tradeTypeName sizeUsd executionPrice grossPnl totalFeesUsd borrowingFeeUsd fundingPaidUsd netPayoutTokens blockNumber txHash } }`,
			fmt.Sprintf(`{"tradeSettlements":[{"positionId":"1","userAddress":%q,"marketId":1,"tradeType":1,"tradeTypeName":"close","sizeUsd":%q,
"executionPrice":%q,"grossPnl":%q,"totalFeesUsd":"0","borrowingFeeUsd":"0","fundingPaidUsd":"0","netPayoutTokens":"119000000","blockNumber":101,"txHash":%q}]}`,
				a, usd1000, px51000, usd20, txHex(101, 0)))
	})

	t.Run("oiSnapshots", func(t *testing.T) {
		check(t, s, `{ oiSnapshots(marketId: 1) { marketId longOi shortOi deltaUsd isIncrease blockNumber txHash } }`,
			fmt.Sprintf(`{"oiSnapshots":[{"marketId":1,"longOi":"0","shortOi":"0","deltaUsd":%[1]q,"isIncrease":false,"blockNumber":101,"txHash":%[2]q},
{"marketId":1,"longOi":%[1]q,"shortOi":"0","deltaUsd":%[1]q,"isIncrease":true,"blockNumber":100,"txHash":%[3]q}]}`, usd1000, txHex(101, 0), txHex(100, 0)))
	})

	t.Run("markPriceHistory", func(t *testing.T) {
		check(t, s, `{ markPriceHistory(marketId: 1) { marketId markPrice indexPrice baseReserve quoteReserve blockNumber txHash blockTimestamp } }`,
			fmt.Sprintf(`{"markPriceHistory":[
{"marketId":1,"markPrice":%[1]q,"indexPrice":%[1]q,"baseReserve":"0","quoteReserve":"0","blockNumber":101,"txHash":%[2]q,"blockTimestamp":%[3]q},
{"marketId":1,"markPrice":%[4]q,"indexPrice":%[4]q,"baseReserve":"0","quoteReserve":"0","blockNumber":100,"txHash":%[5]q,"blockTimestamp":%[6]q}]}`,
				px51000, txHex(101, 1), ts(b101), px50000, txHex(100, 2), ts(b100)))
	})

	t.Run("vaultBalanceHistory", func(t *testing.T) {
		check(t, s, `{ vaultBalanceHistory { tokenAddress vaultType vaultTypeName newBalance delta isIncrease blockNumber txHash } }`,
			fmt.Sprintf(`{"vaultBalanceHistory":[
{"tokenAddress":%[1]q,"vaultType":1,"vaultTypeName":"insurance","newBalance":"95000000","delta":"5000000","isIncrease":false,"blockNumber":101,"txHash":%[2]q},
{"tokenAddress":%[1]q,"vaultType":0,"vaultTypeName":"collateral","newBalance":%[3]q,"delta":%[3]q,"isIncrease":true,"blockNumber":100,"txHash":%[4]q}]}`,
				usdl, txHex(101, 1), usdl1000, txHex(100, 0)))
		check(t, s, `{ vaultBalanceHistory(vaultType: 0) { vaultType } }`, `{"vaultBalanceHistory":[{"vaultType":0}]}`)
		check(t, s, `{ vaultBalanceHistory(tokenAddress: "`+lower(vaultA)+`") { vaultType } }`, `{"vaultBalanceHistory":[]}`)
	})

	t.Run("fundingPayments", func(t *testing.T) {
		check(t, s, `{ fundingPayments(userAddress: "`+a+`") { positionId userAddress marketId fundingPaymentUsd newCollateralUsd newCollateralAmount blockNumber txHash } }`,
			fmt.Sprintf(`{"fundingPayments":[{"positionId":"1","userAddress":%q,"marketId":1,"fundingPaymentUsd":%q,"newCollateralUsd":%q,
"newCollateralAmount":"98000000","blockNumber":100,"txHash":%q}]}`, a, funding, usd98, txHex(100, 2)))
	})

	t.Run("marketSnapshots", func(t *testing.T) {
		check(t, s, `{ marketSnapshots(marketId: 1, limit: 1) { id marketId longOi shortOi markPrice indexPrice fundingRatePerSecond fundingRate24h volume24hUsd onchainTimestamp blockNumber blockTimestamp } }`,
			fmt.Sprintf(`{"marketSnapshots":[{"id":%d,"marketId":1,"longOi":"0","shortOi":"0","markPrice":%[2]q,"indexPrice":%[2]q,
"fundingRatePerSecond":%q,"fundingRate24h":%q,"volume24hUsd":%q,"onchainTimestamp":%d,"blockNumber":102,"blockTimestamp":%q}]}`,
				hour.Unix()/3600, px51000, rate, rate24h, usd1000, hour.Unix(), ts(hour)))
	})

	t.Run("protocolSnapshots", func(t *testing.T) {
		check(t, s, `{ protocolSnapshots(limit: 1) { id totalPositions totalOpenPositions totalMarkets tvlUsd insuranceTotalUsd onchainTimestamp blockNumber blockTimestamp } }`,
			fmt.Sprintf(`{"protocolSnapshots":[{"id":%d,"totalPositions":"2","totalOpenPositions":"0","totalMarkets":"18","tvlUsd":"0",
"insuranceTotalUsd":"95000000","onchainTimestamp":%d,"blockNumber":102,"blockTimestamp":%q}]}`, hour.Unix()/3600, hour.Unix(), ts(hour)))
	})

	t.Run("enrichedGlobalStats", func(t *testing.T) {
		check(t, s, `{ enrichedGlobalStats { onchain { `+globalStatsFields+` } orderly { perp_volume_ytd } } }`,
			`{"enrichedGlobalStats":{"onchain":`+globalStats+`,"orderly":null}}`)
	})

	t.Run("enrichedMarketStats", func(t *testing.T) {
		check(t, s, `{ enrichedMarketStats(marketId: 1) { onchain { `+marketStatsFields+` } ticker { symbol } fundingRate { symbol } openInterest { symbol } priceChange { symbol } } }`,
			`{"enrichedMarketStats":{"onchain":`+marketStats1+`,"ticker":null,"fundingRate":null,"openInterest":null,"priceChange":null}}`)
		check(t, s, `{ enrichedMarketStats(marketId: 99) { onchain { marketId symbol totalPositions totalVolume } } }`,
			`{"enrichedMarketStats":{"onchain":{"marketId":99,"symbol":null,"totalPositions":0,"totalVolume":"0"}}}`)
	})

	t.Run("orderlyTickers", func(t *testing.T) {
		list := mustList(t, s, `{ orderlyTickers { symbol paxeerSymbol mark_price index_price sum_unitary_funding est_funding_rate last_funding_rate
    next_funding_time open_interest h24_open h24_close h24_high h24_low h24_amount h24_volume } }`, "orderlyTickers")
		if len(list) != 18 {
			t.Fatalf("orderlyTickers answered %d rows, want 18", len(list))
		}
		want := []map[string]any{
			{"symbol": "PERP_ETH_USDC", "paxeerSymbol": "ETH", "mark_price": 51000.0, "index_price": 51000.0, "sum_unitary_funding": nil,
				"est_funding_rate": 0.0000036, "last_funding_rate": 0.001, "next_funding_time": nil, "open_interest": 0.0, "h24_open": 50000.0,
				"h24_close": 50000.0, "h24_high": 50000.0, "h24_low": 50000.0, "h24_amount": 1000.0, "h24_volume": 0.02},
			{"symbol": "PERP_SOL_USDC", "paxeerSymbol": "SOL", "mark_price": nil, "index_price": nil, "sum_unitary_funding": nil,
				"est_funding_rate": nil, "last_funding_rate": nil, "next_funding_time": nil, "open_interest": 0.0, "h24_open": nil,
				"h24_close": nil, "h24_high": nil, "h24_low": nil, "h24_amount": nil, "h24_volume": nil},
		}
		for i, w := range want {
			if !reflect.DeepEqual(list[i+1], w) {
				t.Fatalf("ticker %d: got %v want %v", i+1, list[i+1], w)
			}
		}
		if list[0]["symbol"] != "PERP_BTC_USDC" || list[0]["open_interest"] != nil {
			t.Fatalf("ticker 0: %v", list[0])
		}
	})

	t.Run("orderlyFundingRates", func(t *testing.T) {
		check(t, s, `{ orderlyFundingRates { symbol paxeerSymbol est_funding_rate est_funding_rate_timestamp last_funding_rate last_funding_rate_timestamp next_funding_time sum_unitary_funding } }`,
			fmt.Sprintf(`{"orderlyFundingRates":[{"symbol":"PERP_ETH_USDC","paxeerSymbol":"ETH","est_funding_rate":0.0000036,"est_funding_rate_timestamp":%[1]d,
"last_funding_rate":0.001,"last_funding_rate_timestamp":%[1]d,"next_funding_time":null,"sum_unitary_funding":null}]}`, b100.UnixMilli()))
	})

	t.Run("orderlyPriceChanges", func(t *testing.T) {
		check(t, s, `{ orderlyPriceChanges { symbol paxeerSymbol last_price change_5m change_30m change_1h change_4h change_24h change_3d change_7d change_30d } }`,
			`{"orderlyPriceChanges":[{"symbol":"PERP_ETH_USDC","paxeerSymbol":"ETH","last_price":51000,"change_5m":0,"change_30m":0,"change_1h":0,
"change_4h":0.02,"change_24h":null,"change_3d":null,"change_7d":null,"change_30d":null}]}`)
	})

	t.Run("orderlyOpenInterests", func(t *testing.T) {
		check(t, s, `{ orderlyOpenInterests { symbol paxeerSymbol long_oi short_oi } }`,
			`{"orderlyOpenInterests":[{"symbol":"PERP_ETH_USDC","paxeerSymbol":"ETH","long_oi":0,"short_oi":0},
{"symbol":"PERP_SOL_USDC","paxeerSymbol":"SOL","long_oi":0,"short_oi":0}]}`)
	})
}

// mustList runs query and answers the list under root.
func mustList(t *testing.T, s *graphql.Schema, query, root string) []map[string]any {
	t.Helper()
	data, errs := exec(t, s, query)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	var out map[string][]map[string]any
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		t.Fatalf("data %s: %v", data, err)
	}
	return out[root]
}
