package decode

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

// PerpsAddress is the perpetuals precompile, the emitter of every perps event.
var PerpsAddress = common.HexToAddress("0x0000000000000000000000000000000000001015")

// TradeType values of contracts/perps.md section 2.
const (
	TradeOpen        uint8 = 0
	TradeClose       uint8 = 1
	TradeLiquidation uint8 = 2
	TradeReduce      uint8 = 3
	TradeIncrease    uint8 = 4
	TradeADL         uint8 = 5
	TradeBackstop    uint8 = 6
)

// FeeType values of contracts/perps.md section 2 that the release emits.
const (
	FeeTrading     uint8 = 0
	FeeMakerRebate uint8 = 4
)

type eventDecoder struct {
	name  string
	build func(*batch, Fact, *reader) []Row
}

func registerPerps(r *Registry, module common.Address) error {
	a, err := perpsabi.ABI()
	if err != nil {
		return fmt.Errorf("decode: perps ABI: %w", err)
	}
	return r.register(PerpsAddress, module, a, perpsEvents)
}

// batch carries the account sources of one Decode call: the placed and opened
// rows decoded so far, then the stored rows read through q.
type batch struct {
	ctx         context.Context
	q           Querier
	orders      map[string]common.Address
	positions   map[string]common.Address
	unknownLogs int
	enumUnknown int
}

const (
	orderUserQuery    = `SELECT "user" FROM perps.order_events WHERE kind = 'placed' AND order_id = $1 ORDER BY block, tx_index, log_index LIMIT 1`
	positionUserQuery = `SELECT "user" FROM perps.position_events WHERE kind = 'opened' AND position_id = $1 ORDER BY block, tx_index, log_index LIMIT 1`
)

func (b *batch) orderUser(r *reader, orderID string) common.Address {
	return b.user(r, b.orders, orderUserQuery, "order", orderID)
}

func (b *batch) positionUser(r *reader, positionID string) common.Address {
	return b.user(r, b.positions, positionUserQuery, "position", positionID)
}

func (b *batch) user(r *reader, seen map[string]common.Address, query, what, id string) common.Address {
	if r.err != nil {
		return common.Address{}
	}
	if u, ok := seen[id]; ok {
		return u
	}
	if b.q == nil {
		r.fail(fmt.Errorf("%w: %s %s", ErrAccountUnresolved, what, id))
		return common.Address{}
	}
	var raw []byte
	err := b.q.QueryRowContext(b.ctx, query, id).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		r.fail(fmt.Errorf("%w: %s %s", ErrAccountUnresolved, what, id))
		return common.Address{}
	case err != nil:
		r.fail(fmt.Errorf("read %s %s user: %w", what, id, err))
		return common.Address{}
	case len(raw) != common.AddressLength:
		r.fail(fmt.Errorf("stored %s %s user has %d bytes", what, id, len(raw)))
		return common.Address{}
	}
	u := common.BytesToAddress(raw)
	seen[id] = u
	return u
}

func (b *batch) tradeType(v uint8) uint8 {
	if v > TradeBackstop {
		b.enumUnknown++
	}
	return v
}

func (b *batch) feeType(v uint8) uint8 {
	if v != FeeTrading && v != FeeMakerRebate {
		b.enumUnknown++
	}
	return v
}

// perpsEvents is the decoding table of contracts/data-services.md section 6
// for 0x1015: every event contracts/perps.md section 18 lists as emitted.
var perpsEvents = []eventDecoder{
	{"VaultCreated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "created"
		f.Account = r.addr("user")
		return []Row{VaultCreatedRow{f, f.Account, r.addr("vault")}}
	}},
	{"CollateralDeposited", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "deposited"
		f.Account = r.addr("account")
		return []Row{CollateralDepositedRow{f, f.Account, r.u256("amount"), r.u256("newBalance")}}
	}},
	{"CollateralWithdrawn", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "withdrawn"
		f.Account = r.addr("account")
		return []Row{CollateralWithdrawnRow{f, f.Account, r.u256("amount"), r.u256("newBalance")}}
	}},
	{"CollateralTransferred", func(_ *batch, f Fact, r *reader) []Row {
		from, to, amount := r.addr("from"), r.addr("to"), r.u256("amount")
		out, in := f, f
		out.Kind, out.Sub, out.Account = "transferred_out", 0, from
		in.Kind, in.Sub, in.Account = "transferred_in", 1, to
		return []Row{
			CollateralTransferredRow{out, from, to, amount},
			CollateralTransferredRow{in, from, to, amount},
		}
	}},
	{"OrderPlaced", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "placed"
		id := r.u256("orderId")
		f.Account = r.addr("user")
		if r.err == nil {
			b.orders[id] = f.Account
		}
		return []Row{OrderPlacedRow{f, id, f.Account, r.u256("marketId"), r.u8("orderType"), r.flag("isLong"), r.u256("triggerPrice"), r.u256("sizeUsd")}}
	}},
	{"OrderOptions", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "options"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, id)
		return []Row{OrderOptionsRow{f, id, r.u8("timeInForce"), r.flag("reduceOnly"), r.u256("limitPrice"), r.u256("acceptablePrice"), r.u64("deadline"), r.b16("cloid")}}
	}},
	{"OrderCancelled", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "cancelled"
		f.Account = r.addr("user")
		return []Row{OrderCancelledRow{f, r.u256("orderId"), f.Account}}
	}},
	{"OrderExpired", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "expired"
		f.Account = r.addr("user")
		return []Row{OrderExpiredRow{f, r.u256("orderId"), f.Account, r.u256("marketId")}}
	}},
	{"OrderTriggered", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "triggered"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, id)
		return []Row{OrderTriggeredRow{f, id, r.u256("marketId"), r.u256("markPrice")}}
	}},
	{"OrderExecuted", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "executed"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, id)
		return []Row{OrderExecutedRow{f, id, r.u256("positionId"), r.u256("executionPrice")}}
	}},
	{"Fill", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "fill"
		f.Account = r.addr("taker")
		return []Row{FillRow{f, r.u256("marketId"), r.u256("makerOrderId"), r.u256("takerOrderId"), r.addr("maker"), f.Account,
			r.flag("takerIsLong"), r.u256("price"), r.u256("sizeUsd"), r.i256("makerFeeUsd"), r.u256("takerFeeUsd")}}
	}},
	{"PositionOpened", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "opened"
		id := r.u256("positionId")
		f.Account = r.addr("user")
		if r.err == nil {
			b.positions[id] = f.Account
		}
		return []Row{PositionOpenedRow{f, id, f.Account, r.u256("marketId"), r.flag("isLong"), r.u256("sizeUsd"), r.u256("leverage"),
			r.u256("entryPrice"), r.addr("collateralToken"), r.u256("collateralAmount")}}
	}},
	{"PositionModified", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "modified"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, id)
		return []Row{PositionModifiedRow{f, id, r.u256("newSizeUsd"), r.u256("newCollateralUsd"), r.u256("newCollateralAmount")}}
	}},
	{"PositionEntryPriceChanged", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "entry_price_changed"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, id)
		return []Row{PositionEntryPriceChangedRow{f, id, r.u256("oldEntryPrice"), r.u256("newEntryPrice")}}
	}},
	{"PositionClosed", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "closed"
		f.Account = r.addr("user")
		return []Row{PositionClosedRow{f, r.u256("positionId"), f.Account, r.u256("marketId"), r.u256("closedSizeUsd"), r.u256("exitPrice"),
			r.i256("realizedPnl"), r.flag("isFullClose")}}
	}},
	{"PositionTpSlSet", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "tpsl_set"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, id)
		return []Row{PositionTpSlSetRow{f, id, r.u256("takeProfitPrice"), r.u256("stopLossPrice")}}
	}},
	{"TradeSettled", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "settled"
		f.Account = r.addr("user")
		return []Row{TradeSettledRow{f, r.u256("positionId"), f.Account, r.u256("marketId"), b.tradeType(r.u8("tradeType")), r.u256("sizeUsd"),
			r.u256("executionPrice"), r.i256("grossPnl"), r.u256("totalFeesUsd"), r.i256("fundingPaidUsd"), r.u256("netPayoutTokens")}}
	}},
	{"FeeCollected", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "collected"
		f.Account = r.addr("user")
		return []Row{FeeCollectedRow{f, r.u256("positionId"), f.Account, r.u256("marketId"), b.feeType(r.u8("feeType")), r.u256("feeUsd"),
			r.u256("feeTokens"), r.addr("token")}}
	}},
	{"MarkPriceUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "mark"
		return []Row{MarkPriceUpdatedRow{f, r.u256("marketId"), r.u256("markPrice"), r.u256("indexPrice"), r.u256("midPrice")}}
	}},
	{"FundingRateUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "rate"
		return []Row{FundingRateUpdatedRow{f, r.u256("marketId"), r.i256("newRatePerSecond"), r.i256("fundingRate24h")}}
	}},
	{"FundingSettled", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "settled"
		return []Row{FundingSettledRow{f, r.u256("marketId"), r.i256("fundingRate"), r.i256("longPayment"), r.i256("shortPayment")}}
	}},
	{"PositionFundingApplied", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "applied"
		f.Account = r.addr("user")
		return []Row{PositionFundingAppliedRow{f, r.u256("positionId"), f.Account, r.u256("marketId"), r.i256("fundingPaymentUsd"),
			r.u256("newCollateralUsd"), r.u256("newCollateralAmount")}}
	}},
	{"OpenInterestChanged", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "oi"
		return []Row{OpenInterestChangedRow{f, r.u256("marketId"), r.u256("longOI"), r.u256("shortOI"), r.u256("deltaUsd"), r.flag("isIncrease")}}
	}},
	{"Liquidation", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "liquidated"
		f.Account = r.addr("user")
		return []Row{LiquidationRow{f, r.u256("positionId"), f.Account, r.u256("marketId"), r.u256("liquidationPrice"), r.u256("penalty"), r.addr("keeper")}}
	}},
	{"BackstopLiquidation", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "backstop"
		f.Account = r.addr("account")
		return []Row{BackstopLiquidationRow{f, f.Account, r.u256("marketId"), r.u256("sizeUsd"), r.u256("markPrice"), r.u256("marginTakenUsd")}}
	}},
	{"InsuranceContribution", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "contribution"
		return []Row{InsuranceContributionRow{f, r.addr("token"), r.u256("amount"), r.u8("source")}}
	}},
	{"InsuranceDraw", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "draw"
		return []Row{InsuranceDrawRow{f, r.u256("marketId"), r.u256("amount"), r.u256("newBalance")}}
	}},
	{"VaultDeficit", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "vault_deficit"
		return []Row{VaultDeficitRow{f, r.addr("token"), r.u256("deficit")}}
	}},
	{"ADLExecuted", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "adl"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, id)
		return []Row{ADLExecutedRow{f, id, r.u256("deleveragedSizeUsd")}}
	}},
	{"ADLMatched", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "matched"
		f.Account = r.addr("account")
		return []Row{ADLMatchedRow{f, r.u256("marketId"), r.u256("positionId"), f.Account, r.u256("sizeUsd"), r.u256("price"), r.u256("rankScore")}}
	}},
}

// VaultCreatedRow is a perps.vaults row.
type VaultCreatedRow struct {
	Fact
	User  common.Address
	Vault common.Address
}

// CollateralDepositedRow is a perps.collateral_events row of kind deposited.
type CollateralDepositedRow struct {
	Fact
	AccountArg common.Address
	Amount     string
	NewBalance string
}

// CollateralWithdrawnRow is a perps.collateral_events row of kind withdrawn.
type CollateralWithdrawnRow struct {
	Fact
	AccountArg common.Address
	Amount     string
	NewBalance string
}

// CollateralTransferredRow is a perps.collateral_events row of kind
// transferred_out (sub 0) or transferred_in (sub 1).
type CollateralTransferredRow struct {
	Fact
	FromAccount common.Address
	ToAccount   common.Address
	Amount      string
}

// OrderPlacedRow is a perps.order_events row of kind placed.
type OrderPlacedRow struct {
	Fact
	OrderID      string
	User         common.Address
	MarketID     string
	OrderType    uint8
	IsLong       bool
	TriggerPrice string
	SizeUSD      string
}

// OrderOptionsRow is a perps.order_events row of kind options.
type OrderOptionsRow struct {
	Fact
	OrderID         string
	TimeInForce     uint8
	ReduceOnly      bool
	LimitPrice      string
	AcceptablePrice string
	Deadline        uint64
	Cloid           [16]byte
}

// OrderCancelledRow is a perps.order_events row of kind cancelled.
type OrderCancelledRow struct {
	Fact
	OrderID string
	User    common.Address
}

// OrderExpiredRow is a perps.order_events row of kind expired.
type OrderExpiredRow struct {
	Fact
	OrderID  string
	User     common.Address
	MarketID string
}

// OrderTriggeredRow is a perps.order_events row of kind triggered.
type OrderTriggeredRow struct {
	Fact
	OrderID   string
	MarketID  string
	MarkPrice string
}

// OrderExecutedRow is a perps.order_events row of kind executed.
type OrderExecutedRow struct {
	Fact
	OrderID        string
	PositionID     string
	ExecutionPrice string
}

// FillRow is a perps.fills row; MakerFeeUSD is signed.
type FillRow struct {
	Fact
	MarketID     string
	MakerOrderID string
	TakerOrderID string
	Maker        common.Address
	Taker        common.Address
	TakerIsLong  bool
	Price        string
	SizeUSD      string
	MakerFeeUSD  string
	TakerFeeUSD  string
}

// PositionOpenedRow is a perps.position_events row of kind opened.
type PositionOpenedRow struct {
	Fact
	PositionID       string
	User             common.Address
	MarketID         string
	IsLong           bool
	SizeUSD          string
	Leverage         string
	EntryPrice       string
	CollateralToken  common.Address
	CollateralAmount string
}

// PositionModifiedRow is a perps.position_events row of kind modified.
type PositionModifiedRow struct {
	Fact
	PositionID          string
	NewSizeUSD          string
	NewCollateralUSD    string
	NewCollateralAmount string
}

// PositionEntryPriceChangedRow is a perps.position_events row of kind
// entry_price_changed.
type PositionEntryPriceChangedRow struct {
	Fact
	PositionID    string
	OldEntryPrice string
	NewEntryPrice string
}

// PositionClosedRow is a perps.position_events row of kind closed;
// RealizedPnl is signed.
type PositionClosedRow struct {
	Fact
	PositionID    string
	User          common.Address
	MarketID      string
	ClosedSizeUSD string
	ExitPrice     string
	RealizedPnl   string
	IsFullClose   bool
}

// PositionTpSlSetRow is a perps.position_events row of kind tpsl_set.
type PositionTpSlSetRow struct {
	Fact
	PositionID      string
	TakeProfitPrice string
	StopLossPrice   string
}

// TradeSettledRow is a perps.trade_settlements row; GrossPnl and
// FundingPaidUSD are signed.
type TradeSettledRow struct {
	Fact
	PositionID      string
	User            common.Address
	MarketID        string
	TradeType       uint8
	SizeUSD         string
	ExecutionPrice  string
	GrossPnl        string
	TotalFeesUSD    string
	FundingPaidUSD  string
	NetPayoutTokens string
}

// FeeCollectedRow is a perps.fees row.
type FeeCollectedRow struct {
	Fact
	PositionID string
	User       common.Address
	MarketID   string
	FeeType    uint8
	FeeUSD     string
	FeeTokens  string
	Token      common.Address
}

// MarkPriceUpdatedRow is a perps.mark_prices row.
type MarkPriceUpdatedRow struct {
	Fact
	MarketID   string
	MarkPrice  string
	IndexPrice string
	MidPrice   string
}

// FundingRateUpdatedRow is a perps.funding_rates row; both rates are signed.
type FundingRateUpdatedRow struct {
	Fact
	MarketID         string
	NewRatePerSecond string
	FundingRate24h   string
}

// FundingSettledRow is a perps.funding_settlements row; the rate and both
// payments are signed.
type FundingSettledRow struct {
	Fact
	MarketID     string
	FundingRate  string
	LongPayment  string
	ShortPayment string
}

// PositionFundingAppliedRow is a perps.funding_payments row;
// FundingPaymentUSD is signed.
type PositionFundingAppliedRow struct {
	Fact
	PositionID          string
	User                common.Address
	MarketID            string
	FundingPaymentUSD   string
	NewCollateralUSD    string
	NewCollateralAmount string
}

// OpenInterestChangedRow is a perps.open_interest row.
type OpenInterestChangedRow struct {
	Fact
	MarketID   string
	LongOI     string
	ShortOI    string
	DeltaUSD   string
	IsIncrease bool
}

// LiquidationRow is a perps.liquidations row.
type LiquidationRow struct {
	Fact
	PositionID       string
	User             common.Address
	MarketID         string
	LiquidationPrice string
	Penalty          string
	Keeper           common.Address
}

// BackstopLiquidationRow is a perps.backstop_liquidations row.
type BackstopLiquidationRow struct {
	Fact
	AccountArg     common.Address
	MarketID       string
	SizeUSD        string
	MarkPrice      string
	MarginTakenUSD string
}

// InsuranceContributionRow is a perps.insurance_events row of kind
// contribution.
type InsuranceContributionRow struct {
	Fact
	Token  common.Address
	Amount string
	Source uint8
}

// InsuranceDrawRow is a perps.insurance_events row of kind draw.
type InsuranceDrawRow struct {
	Fact
	MarketID   string
	Amount     string
	NewBalance string
}

// VaultDeficitRow is a perps.insurance_events row of kind vault_deficit.
type VaultDeficitRow struct {
	Fact
	Token   common.Address
	Deficit string
}

// ADLExecutedRow is a perps.adl_events row of kind adl.
type ADLExecutedRow struct {
	Fact
	PositionID         string
	DeleveragedSizeUSD string
}

// ADLMatchedRow is a perps.adl_events row of kind matched.
type ADLMatchedRow struct {
	Fact
	MarketID   string
	PositionID string
	AccountArg common.Address
	SizeUSD    string
	Price      string
	RankScore  string
}

func (VaultCreatedRow) Table() string              { return "vaults" }
func (CollateralDepositedRow) Table() string       { return "collateral_events" }
func (CollateralWithdrawnRow) Table() string       { return "collateral_events" }
func (CollateralTransferredRow) Table() string     { return "collateral_events" }
func (OrderPlacedRow) Table() string               { return "order_events" }
func (OrderOptionsRow) Table() string              { return "order_events" }
func (OrderCancelledRow) Table() string            { return "order_events" }
func (OrderExpiredRow) Table() string              { return "order_events" }
func (OrderTriggeredRow) Table() string            { return "order_events" }
func (OrderExecutedRow) Table() string             { return "order_events" }
func (FillRow) Table() string                      { return "fills" }
func (PositionOpenedRow) Table() string            { return "position_events" }
func (PositionModifiedRow) Table() string          { return "position_events" }
func (PositionEntryPriceChangedRow) Table() string { return "position_events" }
func (PositionClosedRow) Table() string            { return "position_events" }
func (PositionTpSlSetRow) Table() string           { return "position_events" }
func (TradeSettledRow) Table() string              { return "trade_settlements" }
func (FeeCollectedRow) Table() string              { return "fees" }
func (MarkPriceUpdatedRow) Table() string          { return "mark_prices" }
func (FundingRateUpdatedRow) Table() string        { return "funding_rates" }
func (FundingSettledRow) Table() string            { return "funding_settlements" }
func (PositionFundingAppliedRow) Table() string    { return "funding_payments" }
func (OpenInterestChangedRow) Table() string       { return "open_interest" }
func (LiquidationRow) Table() string               { return "liquidations" }
func (BackstopLiquidationRow) Table() string       { return "backstop_liquidations" }
func (InsuranceContributionRow) Table() string     { return "insurance_events" }
func (InsuranceDrawRow) Table() string             { return "insurance_events" }
func (VaultDeficitRow) Table() string              { return "insurance_events" }
func (ADLExecutedRow) Table() string               { return "adl_events" }
func (ADLMatchedRow) Table() string                { return "adl_events" }
