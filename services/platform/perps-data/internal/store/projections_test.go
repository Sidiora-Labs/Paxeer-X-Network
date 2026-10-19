package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

var (
	traderA    = common.HexToAddress("0x00000000000000000000000000000000000a11ce")
	traderB    = common.HexToAddress("0x0000000000000000000000000000000000000b0b")
	perpsVenue = common.HexToAddress("0x00000000000000000000000000000000000c0057")
	spotStop   = common.HexToAddress("0x0000000000000000000000000000000000001023")
	spotLimit  = common.HexToAddress("0x0000000000000000000000000000000000001022")
	tokenPAX   = common.HexToAddress("0x00000000000000000000000000000000000000fa")
	usdlBytes  = USDL.Bytes()
)

// chain writes fact rows of consecutive blocks into one transaction, the way
// WriteBlock stores them before it calls Project, and keeps the decoded rows of
// the current block for Project.
type chain struct {
	t       *testing.T
	ctx     context.Context
	tx      *sql.Tx
	block   uint64
	at      time.Time
	txIndex int64
	log     int64
	rows    []decode.Row
}

func newChain(t *testing.T, db *sql.DB) *chain {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil {
			t.Errorf("rollback: %v", err)
		}
	})
	return &chain{t: t, ctx: context.Background(), tx: tx}
}

// next starts block n at time at in its first transaction receipt.
func (c *chain) next(n uint64, at time.Time) {
	c.block, c.at, c.txIndex, c.log, c.rows = n, at.UTC(), 0, 0, nil
}

// endBlock moves to the perps EndBlock receipt of the current block.
func (c *chain) endBlock() { c.txIndex = 1 }

func (c *chain) fact(kind string, account, emitter common.Address, sub int64) decode.Fact {
	rk := decode.ReceiptTx
	var from *common.Address
	switch c.txIndex {
	case 0:
		sender := account
		from = &sender
	default:
		rk = decode.ReceiptPerpsEndBlock
		if emitter == spotStop || emitter == spotLimit {
			rk = decode.ReceiptSpotEndBlock
		}
	}
	f := decode.Fact{
		Block:       c.block,
		BlockHash:   crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", c.block))),
		TxIndex:     c.txIndex,
		LogIndex:    c.log,
		Sub:         sub,
		TxHash:      crypto.Keccak256Hash([]byte(fmt.Sprintf("tx/%d/%d", c.block, c.txIndex))),
		ReceiptKind: rk,
		BlockTime:   c.at,
		Emitter:     emitter,
		Kind:        kind,
		Account:     account,
		TxFrom:      from,
	}
	if sub == 0 {
		c.log++
	}
	return f
}

// insert stores one fact row of table with the common columns of f and the
// named argument columns.
func (c *chain) insert(table string, f decode.Fact, cols string, vals ...any) {
	c.t.Helper()
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
	q := `INSERT INTO perps.` + table + ` (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind,
    account, tx_from, ` + cols + `) VALUES (` + strings.Join(marks, ", ") + `)`
	if _, err := c.tx.ExecContext(c.ctx, q, args...); err != nil {
		c.t.Fatalf("insert %s %s: %v", table, f.Kind, err)
	}
}

func (c *chain) put(row decode.Row, cols string, vals ...any) {
	c.t.Helper()
	c.insert(row.Table(), row.Common(), cols, vals...)
	c.rows = append(c.rows, row)
}

func (c *chain) project() {
	c.t.Helper()
	if err := Project(c.ctx, c.tx, c.rows); err != nil {
		c.t.Fatalf("Project block %d: %v", c.block, err)
	}
	c.rows = nil
}

func (c *chain) deposited(a common.Address, amount, balance string) {
	f := c.fact("deposited", a, decode.PerpsAddress, 0)
	c.put(decode.CollateralDepositedRow{Fact: f, AccountArg: a, Amount: amount, NewBalance: balance},
		"account_arg, amount, new_balance", a.Bytes(), amount, balance)
}

func (c *chain) withdrawn(a common.Address, amount, balance string) {
	f := c.fact("withdrawn", a, decode.PerpsAddress, 0)
	c.put(decode.CollateralWithdrawnRow{Fact: f, AccountArg: a, Amount: amount, NewBalance: balance},
		"account_arg, amount, new_balance", a.Bytes(), amount, balance)
}

func (c *chain) transferred(from, to common.Address, amount string) {
	in := c.fact("transferred_in", to, decode.PerpsAddress, 1)
	out := c.fact("transferred_out", from, decode.PerpsAddress, 0)
	for _, f := range []decode.Fact{out, in} {
		c.put(decode.CollateralTransferredRow{Fact: f, FromAccount: from, ToAccount: to, Amount: amount},
			"from_account, to_account, amount", from.Bytes(), to.Bytes(), amount)
	}
}

func (c *chain) opened(id string, a common.Address, market, size, entry, collateral string) {
	f := c.fact("opened", a, decode.PerpsAddress, 0)
	c.put(decode.PositionOpenedRow{Fact: f, PositionID: id, User: a, MarketID: market, IsLong: true, SizeUSD: size,
		Leverage: "10000000000000000000", EntryPrice: entry, CollateralToken: USDL, CollateralAmount: collateral},
		`position_id, "user", market_id, is_long, size_usd, leverage, entry_price, collateral_token, collateral_amount`,
		id, a.Bytes(), market, true, size, "10000000000000000000", entry, usdlBytes, collateral)
}

func (c *chain) modified(id string, a common.Address, size, collateralUSD, collateral string) {
	f := c.fact("modified", a, decode.PerpsAddress, 0)
	c.put(decode.PositionModifiedRow{Fact: f, PositionID: id, NewSizeUSD: size, NewCollateralUSD: collateralUSD, NewCollateralAmount: collateral},
		"position_id, new_size_usd, new_collateral_usd, new_collateral_amount", id, size, collateralUSD, collateral)
}

func (c *chain) entryChanged(id string, a common.Address, oldPrice, newPrice string) {
	f := c.fact("entry_price_changed", a, decode.PerpsAddress, 0)
	c.put(decode.PositionEntryPriceChangedRow{Fact: f, PositionID: id, OldEntryPrice: oldPrice, NewEntryPrice: newPrice},
		"position_id, old_entry_price, new_entry_price", id, oldPrice, newPrice)
}

func (c *chain) tpsl(id string, a common.Address, tp, sl string) {
	f := c.fact("tpsl_set", a, decode.PerpsAddress, 0)
	c.put(decode.PositionTpSlSetRow{Fact: f, PositionID: id, TakeProfitPrice: tp, StopLossPrice: sl},
		"position_id, take_profit_price, stop_loss_price", id, tp, sl)
}

func (c *chain) closed(id string, a common.Address, size, pnl string, full bool) {
	f := c.fact("closed", a, decode.PerpsAddress, 0)
	c.put(decode.PositionClosedRow{Fact: f, PositionID: id, User: a, MarketID: "0", ClosedSizeUSD: size, ExitPrice: "61000",
		RealizedPnl: pnl, IsFullClose: full},
		`position_id, "user", market_id, closed_size_usd, exit_price, realized_pnl, is_full_close`, id, a.Bytes(), "0", size, "61000", pnl, full)
}

func (c *chain) funding(id string, a common.Address, payment, collateralUSD, collateral string) {
	f := c.fact("applied", a, decode.PerpsAddress, 0)
	c.put(decode.PositionFundingAppliedRow{Fact: f, PositionID: id, User: a, MarketID: "0", FundingPaymentUSD: payment,
		NewCollateralUSD: collateralUSD, NewCollateralAmount: collateral},
		`position_id, "user", market_id, funding_payment_usd, new_collateral_usd, new_collateral_amount`,
		id, a.Bytes(), "0", payment, collateralUSD, collateral)
}

func (c *chain) adl(id string, a common.Address, size string) {
	f := c.fact("adl", a, decode.PerpsAddress, 0)
	c.put(decode.ADLExecutedRow{Fact: f, PositionID: id, DeleveragedSizeUSD: size}, "position_id, deleveraged_size_usd", id, size)
}

func (c *chain) liquidation(id string, a common.Address) {
	f := c.fact("liquidated", a, decode.PerpsAddress, 0)
	c.put(decode.LiquidationRow{Fact: f, PositionID: id, User: a, MarketID: "0", LiquidationPrice: "59000", Penalty: "0", Keeper: common.Address{}},
		`position_id, "user", market_id, liquidation_price, penalty, keeper`, id, a.Bytes(), "0", "59000", "0", make([]byte, 20))
}

func (c *chain) settled(id string, a common.Address, size, gross, fees, funding, payout string) {
	f := c.fact("settled", a, decode.PerpsAddress, 0)
	c.put(decode.TradeSettledRow{Fact: f, PositionID: id, User: a, MarketID: "0", TradeType: decode.TradeOpen, SizeUSD: size,
		ExecutionPrice: "60000", GrossPnl: gross, TotalFeesUSD: fees, FundingPaidUSD: funding, NetPayoutTokens: payout},
		`position_id, "user", market_id, trade_type, size_usd, execution_price, gross_pnl, total_fees_usd, funding_paid_usd, net_payout_tokens`,
		id, a.Bytes(), "0", int16(decode.TradeOpen), size, "60000", gross, fees, funding, payout)
}

func (c *chain) fee(id string, a common.Address, feeType uint8, usd, tokens string) {
	f := c.fact("collected", a, decode.PerpsAddress, 0)
	c.put(decode.FeeCollectedRow{Fact: f, PositionID: id, User: a, MarketID: "0", FeeType: feeType, FeeUSD: usd, FeeTokens: tokens, Token: USDL},
		`position_id, "user", market_id, fee_type, fee_usd, fee_tokens, token`, id, a.Bytes(), "0", int16(feeType), usd, tokens, usdlBytes)
}

func (c *chain) placed(id string, a common.Address, size string) {
	f := c.fact("placed", a, decode.PerpsAddress, 0)
	c.put(decode.OrderPlacedRow{Fact: f, OrderID: id, User: a, MarketID: "0", OrderType: 0, IsLong: true, TriggerPrice: "0", SizeUSD: size},
		`order_id, "user", market_id, order_type, is_long, trigger_price, size_usd`, id, a.Bytes(), "0", int16(0), true, "0", size)
}

func (c *chain) options(id string, a common.Address, limit string) {
	f := c.fact("options", a, decode.PerpsAddress, 0)
	cloid := [16]byte{0xc1, 0x01, 0xd0}
	c.put(decode.OrderOptionsRow{Fact: f, OrderID: id, TimeInForce: 0, ReduceOnly: false, LimitPrice: limit, AcceptablePrice: "0", Deadline: 0, Cloid: cloid},
		"order_id, time_in_force, reduce_only, limit_price, acceptable_price, deadline, cloid", id, int16(0), false, limit, "0", int64(0), cloid[:])
}

func (c *chain) triggered(id string, a common.Address) {
	f := c.fact("triggered", a, decode.PerpsAddress, 0)
	c.put(decode.OrderTriggeredRow{Fact: f, OrderID: id, MarketID: "0", MarkPrice: "61050"}, "order_id, market_id, mark_price", id, "0", "61050")
}

func (c *chain) executed(id string, a common.Address, position, price string) {
	f := c.fact("executed", a, decode.PerpsAddress, 0)
	c.put(decode.OrderExecutedRow{Fact: f, OrderID: id, PositionID: position, ExecutionPrice: price},
		"order_id, position_id, execution_price", id, position, price)
}

func (c *chain) cancelled(id string, a common.Address) {
	f := c.fact("cancelled", a, decode.PerpsAddress, 0)
	c.put(decode.OrderCancelledRow{Fact: f, OrderID: id, User: a}, `order_id, "user"`, id, a.Bytes())
}

func (c *chain) expired(id string, a common.Address) {
	f := c.fact("expired", a, decode.PerpsAddress, 0)
	c.put(decode.OrderExpiredRow{Fact: f, OrderID: id, User: a, MarketID: "0"}, `order_id, "user", market_id`, id, a.Bytes(), "0")
}

func (c *chain) fill(market, makerOrder, takerOrder string, maker, taker common.Address, price, size string) {
	f := c.fact("fill", taker, decode.PerpsAddress, 0)
	c.put(decode.FillRow{Fact: f, MarketID: market, MakerOrderID: makerOrder, TakerOrderID: takerOrder, Maker: maker, Taker: taker,
		TakerIsLong: true, Price: price, SizeUSD: size, MakerFeeUSD: "0", TakerFeeUSD: "0"},
		"market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd, taker_fee_usd",
		market, makerOrder, takerOrder, maker.Bytes(), taker.Bytes(), true, price, size, "0", "0")
}

func (c *chain) openInterest(market, long, short string) {
	f := c.fact("oi", perpsVenue, decode.PerpsAddress, 0)
	c.put(decode.OpenInterestChangedRow{Fact: f, MarketID: market, LongOI: long, ShortOI: short, DeltaUSD: "1", IsIncrease: true},
		"market_id, long_oi, short_oi, delta_usd, is_increase", market, long, short, "1", true)
}

func (c *chain) mark(market, price string) {
	f := c.fact("mark", perpsVenue, decode.PerpsAddress, 0)
	c.put(decode.MarkPriceUpdatedRow{Fact: f, MarketID: market, MarkPrice: price, IndexPrice: price, MidPrice: price},
		"market_id, mark_price, index_price, mid_price", market, price, price, price)
}

// spotOrder stores one perps.spot_order_events row and folds it through the
// spot order and spot token projections.
func (c *chain) spotOrder(kind string, emitter common.Address, orderID string, cols string, vals ...any) {
	c.t.Helper()
	f := c.fact(kind, traderA, emitter, 0)
	c.insert(spotOrderTable, f, "order_id, token, stablecoin"+cols, append([]any{orderID, tokenPAX.Bytes(), usdlBytes}, vals...)...)
	if err := projectSpotOrder(c.ctx, c.tx, f); err != nil {
		c.t.Fatalf("projectSpotOrder %s %s: %v", kind, orderID, err)
	}
	if err := projectSpotTokens(c.ctx, c.tx, spotOrderTable, f); err != nil {
		c.t.Fatalf("projectSpotTokens %s %s: %v", kind, orderID, err)
	}
}

func (c *chain) blockRow(n uint64, at time.Time) {
	c.t.Helper()
	hash := crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n)))
	parent := crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n-1)))
	if _, err := c.tx.ExecContext(c.ctx, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count)
VALUES ($1, $2, $3, $4, $5, 1)`, int64(n), hash.Bytes(), parent.Bytes(), at.UTC(), strings.Repeat("AB", 20)); err != nil {
		c.t.Fatalf("insert block %d: %v", n, err)
	}
}

// values reads one row as text, NULL as "null".
func (c *chain) values(query string, args ...any) []string {
	c.t.Helper()
	rows, err := c.tx.QueryContext(c.ctx, query, args...)
	if err != nil {
		c.t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		c.t.Fatalf("columns: %v", err)
	}
	if !rows.Next() {
		c.t.Fatalf("%s %v: no row", query, args)
	}
	cells := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range cells {
		ptrs[i] = &cells[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		c.t.Fatalf("scan: %v", err)
	}
	if rows.Next() {
		c.t.Fatalf("%s %v: more than one row", query, args)
	}
	out := make([]string, len(cells))
	for i, s := range cells {
		out[i] = "null"
		if s.Valid {
			out[i] = s.String
		}
	}
	return out
}

func (c *chain) want(query string, want []string, args ...any) {
	c.t.Helper()
	if got := c.values(query, args...); !slices.Equal(got, want) {
		c.t.Fatalf("%s %v\n got  %q\n want %q", query, args, got, want)
	}
}

func TestProjections(t *testing.T) {
	db := testDB(t)
	migrate(t, db)
	t0 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

	t.Run("position_lifecycle", func(t *testing.T) {
		c := newChain(t, db)
		const q = `SELECT size_usd::text, collateral_usd::text, collateral_amount::text, entry_price::text, realized_pnl::text, status,
    opened_block::text, closed_block::text, take_profit_price::text, stop_loss_price::text FROM perps.positions_current WHERE position_id = $1`
		c.next(10, t0)
		c.opened("5", traderA, "0", "990000000000000000000", "60000000000000000000000", "100000000")
		c.tpsl("5", traderA, "66000000000000000000000", "57000000000000000000000")
		c.project()
		c.want(q, []string{"990000000000000000000", "100000000000000000000", "100000000", "60000000000000000000000", "0", "open", "10", "null",
			"66000000000000000000000", "57000000000000000000000"}, "5")
		c.want(`SELECT account = $2, collateral_token = $3, leverage::text, is_long::text FROM perps.positions_current WHERE position_id = $1`,
			[]string{"true", "true", "10000000000000000000", "true"}, "5", traderA.Bytes(), usdlBytes)

		c.next(11, t0.Add(time.Minute))
		c.modified("5", traderA, "1517760000000000000000", "99000000000000000000", "99000000")
		c.closed("5", traderA, "1517760000000000000000", "16500000000000000000", true)
		c.project()
		c.want(q, []string{"1517760000000000000000", "99000000000000000000", "99000000", "60000000000000000000000", "16500000000000000000",
			"closed", "10", "11", "66000000000000000000000", "57000000000000000000000"}, "5")

		c.next(12, t0.Add(2*time.Minute))
		c.opened("6", traderB, "0", "6000", "60000", "1000")
		c.entryChanged("6", traderB, "60000", "60651")
		c.funding("6", traderB, "-75", "925", "925")
		c.endBlock()
		c.adl("6", traderB, "2500")
		c.project()
		c.want(q, []string{"3500", "925", "925", "60651", "0", "open", "12", "null", "null", "null"}, "6")

		c.next(13, t0.Add(3*time.Minute))
		c.opened("7", traderA, "0", "600000", "60000", "5000")
		c.endBlock()
		c.closed("7", traderA, "120000", "-40", false)
		c.liquidation("7", traderA)
		c.project()
		c.want(q, []string{"600000", "5000000000000000", "5000", "60000", "-40", "open", "13", "null", "null", "null"}, "7")

		c.next(14, t0.Add(4*time.Minute))
		c.endBlock()
		c.liquidation("7", traderA)
		c.closed("7", traderA, "480000", "-160", true)
		c.project()
		c.want(q, []string{"600000", "5000000000000000", "5000", "60000", "-200", "liquidated", "13", "14", "null", "null"}, "7")

		c.next(15, t0.Add(5*time.Minute))
		c.modified("99", traderA, "1", "1", "1")
		if err := Project(c.ctx, c.tx, c.rows); !errors.Is(err, ErrNotProjected) {
			t.Fatalf("Project of an unknown position: %v, want ErrNotProjected", err)
		}
	})

	t.Run("order_lifecycle", func(t *testing.T) {
		c := newChain(t, db)
		const q = `SELECT remaining_size_usd::text, status, limit_price::text, acceptable_price::text, time_in_force::text, reduce_only::text,
    deadline::text, encode(cloid, 'hex'), position_id::text, execution_price::text, placed_block::text, closed_block::text
FROM perps.orders WHERE order_id = $1`
		c.next(20, t0)
		c.placed("8", traderA, "2000000")
		c.options("8", traderA, "50000")
		c.placed("9", traderB, "1000000")
		c.options("9", traderB, "50000")
		c.project()
		c.want(q, []string{"1000000", "active", "50000", "0", "0", "false", "0", "c101d000000000000000000000000000", "null", "null", "20", "null"}, "9")

		c.next(21, t0.Add(time.Second))
		c.fill("0", "8", "9", traderA, traderB, "50000", "400000")
		c.project()
		c.want(q, []string{"600000", "active", "50000", "0", "0", "false", "0", "c101d000000000000000000000000000", "null", "null", "20", "null"}, "9")

		c.next(22, t0.Add(2*time.Second))
		c.fill("0", "8", "9", traderA, traderB, "50000", "600000")
		c.executed("9", traderB, "3", "50000")
		c.project()
		c.want(q, []string{"0", "filled", "50000", "0", "0", "false", "0", "c101d000000000000000000000000000", "3", "50000", "20", "22"}, "9")
		c.want(`SELECT remaining_size_usd::text, status, closed_block::text FROM perps.orders WHERE order_id = $1`,
			[]string{"1000000", "active", "null"}, "8")

		c.next(23, t0.Add(3*time.Second))
		c.placed("10", traderA, "500")
		c.placed("11", traderA, "700")
		c.endBlock()
		c.triggered("10", traderA)
		c.project()
		c.want(`SELECT status, closed_block::text FROM perps.orders WHERE order_id = $1`, []string{"triggered", "null"}, "10")

		c.next(24, t0.Add(4*time.Second))
		c.cancelled("10", traderA)
		c.endBlock()
		c.expired("11", traderA)
		c.project()
		c.want(`SELECT status, closed_block::text FROM perps.orders WHERE order_id = $1`, []string{"cancelled", "24"}, "10")
		c.want(`SELECT status, closed_block::text, remaining_size_usd::text FROM perps.orders WHERE order_id = $1`,
			[]string{"expired", "24", "700"}, "11")

		c.next(25, t0.Add(5*time.Second))
		c.fill("0", "0", "77", traderA, traderB, "50000", "1")
		if err := Project(c.ctx, c.tx, c.rows); !errors.Is(err, ErrNotProjected) {
			t.Fatalf("Project of a fill naming an unknown order: %v, want ErrNotProjected", err)
		}
	})

	t.Run("margin_fold", func(t *testing.T) {
		c := newChain(t, db)
		const q = `SELECT balance::text, locked::text, margin_mode::text, updated_block::text FROM perps.account_margin WHERE account = $1 AND token = $2`
		c.next(30, t0)
		c.deposited(traderA, "10000000", "10000000")
		c.transferred(traderA, traderB, "2000000")
		c.opened("1", traderA, "0", "30000000000000000000", "60000", "3000000")
		c.project()
		c.want(q, []string{"8000000", "3000000", "0", "30"}, traderA.Bytes(), usdlBytes)
		c.want(q, []string{"2000000", "0", "0", "30"}, traderB.Bytes(), usdlBytes)

		c.next(31, t0.Add(time.Minute))
		c.withdrawn(traderA, "1000000", "7000000")
		c.closed("1", traderA, "30000000000000000000", "0", true)
		c.project()
		c.want(q, []string{"7000000", "0", "0", "31"}, traderA.Bytes(), usdlBytes)
	})

	t.Run("stats_rolling", func(t *testing.T) {
		c := newChain(t, db)
		const q = `SELECT volume_24h::text, volume_7d::text, volume_30d::text, trades_count::text, open_interest_long::text,
    open_interest_short::text, high_24h::text, low_24h::text, open_24h::text, last_price::text, updated_block::text
FROM perps.market_stats_rolling WHERE market_id = $1`
		c.next(40, t0.Add(-48*time.Hour))
		c.fill("0", "0", "0", traderA, traderB, "59000", "500000")
		c.openInterest("0", "500000", "500000")
		c.project()
		c.want(q, []string{"500000", "500000", "500000", "1", "500000", "500000", "59000", "59000", "59000", "59000", "40"}, "0")

		c.next(41, t0)
		c.fill("0", "0", "0", traderA, traderB, "60000", "1000000")
		c.settled("2", traderA, "1000000", "0", "300", "0", "0")
		c.settled("3", traderB, "1000000", "0", "450", "0", "0")
		c.openInterest("0", "1500000", "1500000")
		c.project()
		c.want(q, []string{"1000000", "1500000", "1500000", "2", "1500000", "1500000", "60000", "60000", "60000", "60000", "41"}, "0")
		for _, a := range []common.Address{traderA, traderB} {
			c.want(`SELECT trades_count::text, volume_usd::text FROM perps.user_stats WHERE account = $1`, []string{"1", "1000000"}, a.Bytes())
		}
		c.want(`SELECT trading_pnl::text, fees::text, funding::text, realized_pnl::text, volume::text, trades::text
FROM perps.user_pnl_daily WHERE account = $1 AND day = $2::date`, []string{"0", "300", "0", "-300", "1000000", "1"}, traderA.Bytes(), "2026-03-02")

		c.next(42, t0.Add(time.Hour))
		c.fill("0", "0", "0", traderA, traderB, "61000", "2000000")
		c.project()
		c.want(q, []string{"3000000", "3500000", "3500000", "3", "1500000", "1500000", "61000", "60000", "60000", "61000", "42"}, "0")
		c.want(`SELECT trades_count::text FROM perps.user_stats WHERE account = $1`, []string{"1"}, traderA.Bytes())
	})

	t.Run("hourly_snapshots", func(t *testing.T) {
		c := newChain(t, db)
		var s Snapshotter
		before := time.Date(2026, 3, 2, 10, 59, 58, 0, time.UTC)
		after := time.Date(2026, 3, 2, 11, 0, 1, 0, time.UTC)
		hour := time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC)
		markets := c.values(`SELECT count(*)::text FROM perps.markets`)[0]

		c.blockRow(100, before)
		c.next(100, before)
		ids, err := c.tx.QueryContext(c.ctx, `SELECT market_id::text FROM perps.markets ORDER BY market_id`)
		if err != nil {
			t.Fatalf("markets: %v", err)
		}
		var marketIDs []string
		for ids.Next() {
			var id string
			if err := ids.Scan(&id); err != nil {
				t.Fatalf("scan market: %v", err)
			}
			marketIDs = append(marketIDs, id)
		}
		if err := errors.Join(ids.Err(), ids.Close()); err != nil {
			t.Fatalf("markets: %v", err)
		}
		c.fill("0", "0", "0", traderA, traderB, "60000", "700")
		c.openInterest("0", "700", "700")
		in := c.fact("in", traderA, USDL, 0)
		c.insert("usdl_transfers", in, "from_account, to_account, value, venue", traderA.Bytes(), perpsVenue.Bytes(), "5000000", perpsVenue.Bytes())
		out := c.fact("out", traderB, USDL, 0)
		c.insert("usdl_transfers", out, "from_account, to_account, value, venue", perpsVenue.Bytes(), traderB.Bytes(), "1500000", perpsVenue.Bytes())
		contribution := c.fact("contribution", perpsVenue, decode.PerpsAddress, 0)
		c.insert("insurance_events", contribution, "token, amount, source", usdlBytes, "300", int16(1))
		c.endBlock()
		for _, id := range marketIDs {
			c.mark(id, "1000"+id)
		}
		c.project()
		if err := s.Snapshot(c.ctx, c.tx, 100, before); err != nil {
			t.Fatalf("Snapshot 100: %v", err)
		}
		c.want(`SELECT count(*)::text FROM perps.protocol_snapshots`, []string{"0"})

		c.blockRow(101, after)
		c.next(101, after)
		c.fill("0", "0", "0", traderA, traderB, "60000", "9")
		c.project()
		for range 2 {
			if err := s.Snapshot(c.ctx, c.tx, 101, after); err != nil {
				t.Fatalf("Snapshot 101: %v", err)
			}
		}
		c.want(`SELECT count(*)::text, count(*) FILTER (WHERE hour = $1)::text FROM perps.market_snapshots`, []string{markets, markets}, hour)
		c.want(`SELECT mark_price::text, index_price::text, funding_rate::text, volume_usd::text, trades_count::text
FROM perps.market_snapshots WHERE market_id = 0 AND hour = $1`, []string{"10000", "10000", "null", "700", "1"}, hour)
		c.want(`SELECT count(*)::text FROM perps.protocol_snapshots`, []string{"1"})
		c.want(`SELECT total_volume_usd::text, total_oi_usd::text, total_fees_usd::text, tvl_usd::text, insurance_total_usd::text,
    accounts::text, trades_count::text FROM perps.protocol_snapshots WHERE hour = $1`,
			[]string{"700", "1400", "0", "3500000", "300", "2", "1"}, hour)
	})

	t.Run("keeper_cycles", func(t *testing.T) {
		c := newChain(t, db)
		at := t0.Add(7 * time.Hour)
		c.next(420, at)
		for _, id := range []string{"31", "32", "33"} {
			c.placed(id, traderA, "100")
		}
		c.opened("40", traderB, "0", "100", "60000", "10")
		c.endBlock()
		for m := range 18 {
			c.mark(fmt.Sprint(m), "5000")
		}
		for _, id := range []string{"31", "32", "33"} {
			c.executed(id, traderA, "41", "60000")
		}
		c.liquidation("40", traderB)
		c.project()
		c.want(`SELECT block_time = $2, markets_updated::text, orders_executed::text, liquidations_executed::text, orders_failed::text,
    liquidations_failed::text FROM perps.keeper_cycles WHERE block = $1`, []string{"true", "18", "3", "1", "0", "0"}, int64(420), at)

		c.next(421, at.Add(time.Second))
		c.placed("34", traderA, "100")
		c.project()
		c.want(`SELECT count(*)::text FROM perps.keeper_cycles`, []string{"1"})
	})

	t.Run("account_ledger_view", func(t *testing.T) {
		c := newChain(t, db)
		c.next(50, t0)
		c.opened("8", traderA, "0", "1000", "60000", "100")
		c.deposited(traderA, "250000000", "250000000")
		c.fee("8", traderA, decode.FeeTrading, "270000000000000000", "270000")
		c.endBlock()
		c.funding("8", traderA, "-750000000000000000", "99", "99")
		c.next(51, t0.Add(time.Second))
		c.settled("8", traderA, "1000", "16500000000000000000", "452925000000000000", "-50000000000000000", "15997075")
		c.project()
		rows, err := c.tx.QueryContext(c.ctx, `SELECT entry_type, amount::text, position_id::text, is_debit, token = $2, block, tx_index, log_index
FROM perps.account_ledger WHERE account = $1 ORDER BY block, tx_index, log_index`, traderA.Bytes(), usdlBytes)
		if err != nil {
			t.Fatalf("account_ledger: %v", err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var (
				entry, block, txIndex, logIndex int64
				amount, position                string
				debit, usdl                     bool
			)
			if err := rows.Scan(&entry, &amount, &position, &debit, &usdl, &block, &txIndex, &logIndex); err != nil {
				t.Fatalf("scan account_ledger: %v", err)
			}
			got = append(got, fmt.Sprintf("%d %s %s %t %t %d/%d/%d", entry, amount, position, debit, usdl, block, txIndex, logIndex))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("account_ledger: %v", err)
		}
		want := []string{"0 250000000 0 false true 50/0/1", "4 270000 8 true true 50/0/2", "5 750000000000000000 8 true true 50/1/3",
			"6 15997075 8 false true 51/0/0"}
		if !slices.Equal(got, want) {
			t.Fatalf("account_ledger\n got  %q\n want %q", got, want)
		}
	})

	t.Run("user_stats_win_loss", func(t *testing.T) {
		c := newChain(t, db)
		c.next(60, t0)
		for _, id := range []string{"11", "12", "13"} {
			c.opened(id, traderA, "0", "1000", "60000", "100")
		}
		c.fee("11", traderA, decode.FeeTrading, "30", "0")
		c.funding("11", traderA, "-5", "99", "99")
		c.closed("11", traderA, "1000", "500", true)
		c.closed("12", traderA, "1000", "-200", true)
		c.closed("13", traderA, "1000", "0", true)
		c.project()
		c.want(`SELECT win_count::text, loss_count::text, realized_pnl::text, fees_usd::text, funding_usd::text, total_pnl::text,
    trades_count::text, liquidations::text, updated_block::text FROM perps.user_stats WHERE account = $1`,
			[]string{"1", "1", "300", "30", "-5", "265", "0", "0", "60"}, traderA.Bytes())

		c.next(61, t0.Add(time.Second))
		c.fee("11", traderA, decode.FeeMakerRebate, "10", "0")
		c.project()
		c.want(`SELECT fees_usd::text, total_pnl::text, updated_block::text FROM perps.user_stats WHERE account = $1`,
			[]string{"20", "275", "61"}, traderA.Bytes())
	})

	t.Run("spot_orders_lifecycle", func(t *testing.T) {
		c := newChain(t, db)
		const q = `SELECT account = $3, side, order_type::text, amount::text, stop_price::text, limit_price::text, target_price::text,
    trigger_price::text, expires_at::text, status, execution_price::text, placed_block::text, closed_block::text
FROM perps.spot_orders WHERE kind = $1 AND order_id = $2`
		a := traderA.Bytes()
		c.next(70, t0)
		c.spotOrder("stop_limit_created", spotStop, "4", `, "user", order_type, amount, stop_price, limit_price`, a, int16(spotStopLimitBuy), "1000", "45", "50")
		c.want(q, []string{"true", "buy", "3", "1000", "45", "50", "null", "null", "null", "open", "null", "70", "null"}, "stop_limit", "4", a)
		c.want(`SELECT first_block::text FROM perps.spot_token_seen WHERE token = $1`, []string{"70"}, tokenPAX.Bytes())
		c.want(`SELECT first_block::text FROM perps.spot_token_seen WHERE token = $1`, []string{"70"}, usdlBytes)

		c.next(71, t0.Add(time.Second))
		c.endBlock()
		c.spotOrder("stop_limit_activated", spotStop, "4", ", activation_price", "46")
		c.want(q, []string{"true", "buy", "3", "1000", "45", "50", "null", "null", "null", "activated", "null", "70", "null"}, "stop_limit", "4", a)

		c.next(72, t0.Add(2*time.Second))
		c.endBlock()
		c.spotOrder("stop_limit_executed", spotStop, "4", ", execution_price", "48")
		c.want(q, []string{"true", "buy", "3", "1000", "45", "50", "null", "null", "null", "executed", "48", "70", "72"}, "stop_limit", "4", a)
		c.want(`SELECT first_block::text FROM perps.spot_token_seen WHERE token = $1`, []string{"70"}, tokenPAX.Bytes())

		c.next(73, t0.Add(3*time.Second))
		c.spotOrder("limit_created", spotLimit, "5", `, "user", order_type, amount, target_price`, a, int16(spotLimitSell), "700", "60")
		c.spotOrder("limit_modified", spotLimit, "5", ", target_price, expires_at", "61", "1800000000")
		c.spotOrder("stop_loss_created", spotStop, "6", `, "user", amount, trigger_price`, a, "300", "40")
		c.spotOrder("limit_created", spotLimit, "7", `, "user", order_type, amount, target_price`, a, int16(spotLimitBuy), "900", "55")
		c.spotOrder("limit_cancelled", spotLimit, "7", "")
		c.want(q, []string{"true", "sell", "1", "700", "null", "null", "61", "null", "1800000000", "open", "null", "73", "null"}, "limit", "5", a)
		c.want(q, []string{"true", "sell", "null", "300", "null", "null", "null", "40", "null", "open", "null", "73", "null"}, "stop_loss", "6", a)
		c.want(q, []string{"true", "buy", "0", "900", "null", "null", "55", "null", "null", "cancelled", "null", "73", "73"}, "limit", "7", a)

		c.next(74, t0.Add(4*time.Second))
		c.endBlock()
		c.spotOrder("stop_loss_cancelled", spotStop, "6", "")
		c.spotOrder("limit_expired", spotLimit, "5", "")
		c.want(`SELECT status, closed_block::text FROM perps.spot_orders WHERE kind = $1 AND order_id = $2`, []string{"expired", "74"}, "stop_loss", "6")
		c.want(`SELECT status, closed_block::text FROM perps.spot_orders WHERE kind = $1 AND order_id = $2`, []string{"expired", "74"}, "limit", "5")

		missing := c.fact("limit_executed", traderA, spotLimit, 0)
		if err := projectSpotOrder(c.ctx, c.tx, missing); !errors.Is(err, ErrNotProjected) {
			t.Fatalf("projectSpotOrder of an unstored row: %v, want ErrNotProjected", err)
		}
	})
}
