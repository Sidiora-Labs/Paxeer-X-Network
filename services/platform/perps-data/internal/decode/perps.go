package decode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
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

func registerPerps(r *Registry) error {
	a, err := perpsabi.ABI()
	if err != nil {
		return fmt.Errorf("decode: perps ABI: %w", err)
	}
	if r.orderOwner, err = newStateGetter(a, "order", orderUserQuery, orderStoredQuery, "PerpsUnknownOrder", "getOrderDetails"); err != nil {
		return err
	}
	if r.positionOwner, err = newStateGetter(a, "position", positionUserQuery, positionStoredQuery, "PerpsUnknownPosition", "getPosition", "getPositionDetails"); err != nil {
		return err
	}
	return r.register(PerpsAddress, r.opts.PerpsModule, a, perpsEvents)
}

// stateGetter reads an order or position: its account from its stored rows
// through query, whether a projection row or a projection seed of it is
// stored through stored, else its state through methods on 0x1015 in order,
// whose revert with unknown reports an id the precompile does not know.
type stateGetter struct {
	what    string
	query   string
	stored  string
	methods []abi.Method
	unknown []byte
}

func newStateGetter(a abi.ABI, what, query, stored, unknown string, methods ...string) (stateGetter, error) {
	g := stateGetter{what: what, query: query, stored: stored}
	for _, name := range methods {
		m, ok := a.Methods[name]
		if !ok || len(m.Inputs) != 1 || len(m.Outputs) == 0 {
			return stateGetter{}, fmt.Errorf("decode: perps ABI declares no %s(uint256)", name)
		}
		g.methods = append(g.methods, m)
	}
	e, ok := a.Errors[unknown]
	if !ok {
		return stateGetter{}, fmt.Errorf("decode: perps ABI declares no error %s", unknown)
	}
	g.unknown = e.ID[:4]
	return g, nil
}

// batch carries the account sources of one Decode call: the order and
// position accounts decoded or looked up so far, then the stored rows read
// through q, then the perps getters read through State; the spot orders and
// share pointers resolved so far; the token of the share pointer whose log
// is being decoded; and the account lookups made through State.
type batch struct {
	ctx         context.Context
	q           Querier
	reg         *Registry
	orders      map[string]common.Address
	positions   map[string]common.Address
	spotOrders  map[spotOrderKey]spotOrder
	created     map[common.Address]common.Address
	shareToken  common.Address
	unknownLogs int
	enumUnknown int
	lookups     []AccountLookup
	projected   map[string]bool
	seeds       []Seed
}

// Every stored row of an order or position carries its account, the placed
// or opened user or the account an id-only row resolved, so the earliest row
// answers, else the projection seed read from state.
const (
	orderUserQuery = `SELECT COALESCE((SELECT account FROM perps.order_events WHERE order_id = $1 ORDER BY block, tx_index, log_index, sub LIMIT 1),
    (SELECT account FROM perps.projection_seeds WHERE kind = 'order' AND id = $1))`
	positionUserQuery = `SELECT COALESCE((SELECT account FROM perps.position_events WHERE position_id = $1 ORDER BY block, tx_index, log_index, sub LIMIT 1),
    (SELECT account FROM perps.projection_seeds WHERE kind = 'position' AND id = $1))`
	orderStoredQuery    = `SELECT EXISTS (SELECT 1 FROM perps.orders WHERE order_id = $1) OR EXISTS (SELECT 1 FROM perps.projection_seeds WHERE kind = 'order' AND id = $1)`
	positionStoredQuery = `SELECT EXISTS (SELECT 1 FROM perps.positions_current WHERE position_id = $1) OR EXISTS (SELECT 1 FROM perps.projection_seeds WHERE kind = 'position' AND id = $1)`
)

// orderUser and positionUser resolve the account of an id-only event after
// seeding its projection, so an id the precompile does not know is an
// account_lookup raw row.
func (b *batch) orderUser(r *reader, f Fact, orderID string) common.Address {
	b.seed(r, f, b.orders, b.reg.orderOwner, orderID, FailureAccountLookup)
	return b.user(r, b.orders, b.reg.orderOwner, orderID, f.Block)
}

func (b *batch) positionUser(r *reader, f Fact, positionID string) common.Address {
	b.seed(r, f, b.positions, b.reg.positionOwner, positionID, FailureAccountLookup)
	return b.user(r, b.positions, b.reg.positionOwner, positionID, f.Block)
}

func (b *batch) user(r *reader, seen map[string]common.Address, g stateGetter, id string, height uint64) common.Address {
	if r.err != nil {
		return common.Address{}
	}
	if u, ok := seen[id]; ok {
		return u
	}
	if b.q != nil {
		var raw []byte
		err := b.q.QueryRowContext(b.ctx, g.query, id).Scan(&raw)
		switch {
		case err == nil && raw == nil:
		case err == nil && len(raw) == common.AddressLength:
			u := common.BytesToAddress(raw)
			seen[id] = u
			return u
		case err == nil:
			r.fail(fmt.Errorf("stored %s %s user has %d bytes", g.what, id, len(raw)))
			return common.Address{}
		case err != nil:
			r.fail(fmt.Errorf("read %s %s user: %w", g.what, id, err))
			return common.Address{}
		}
	}
	if b.reg.opts.State == nil {
		r.fail(fmt.Errorf("%w: %s %s", ErrAccountUnresolved, g.what, id))
		return common.Address{}
	}
	return b.lookupState(r, seen, g, id, height, FailureAccountLookup)
}

// lookupState reads an order or position no stored row carries through g at
// the height before the event's block, where an id that predates every
// stored row still exists although the event's own block may execute or
// close it, and at the latest height when every node pruned that height. It
// records the account and the projection seed of what it read; an id the
// precompile does not know turns the log into a raw row of class.
func (b *batch) lookupState(r *reader, seen map[string]common.Address, g stateGetter, id string, height uint64, class string) common.Address {
	n, ok := new(big.Int).SetString(id, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 {
		r.fail(fmt.Errorf("%s: %s id %q", g.methods[0].Name, g.what, id))
		return common.Address{}
	}
	at := height
	if at > 0 {
		at--
	}
	state := b.reg.opts.State
	path := LookupAtHeight
	outputs := make([][]any, len(g.methods))
	for i, m := range g.methods {
		data := append(append([]byte(nil), m.ID...), common.LeftPadBytes(n.Bytes(), 32)...)
		var (
			out []byte
			err error
		)
		if path == LookupAtHeight {
			out, err = state.CallAt(b.ctx, PerpsAddress, data, at)
			if errors.Is(err, ErrStatePruned) {
				path = LookupLatest
			}
		}
		if path == LookupLatest {
			out, err = state.Call(b.ctx, PerpsAddress, data)
		}
		var revert *RevertError
		if errors.As(err, &revert) && bytes.Equal(revert.Data, g.unknown) {
			b.lookups = append(b.lookups, AccountLookup{What: g.what, ID: id, Height: at, Path: path, Missing: true})
			r.fail(&lookupFailure{class, fmt.Sprintf("%s(%s) at %s %d: unknown %s", m.Name, id, path, at, g.what)})
			return common.Address{}
		}
		if err != nil {
			r.fail(fmt.Errorf("%s(%s) at %s %d: %w", m.Name, id, path, at, err))
			return common.Address{}
		}
		if outputs[i], err = m.Outputs.Unpack(out); err != nil {
			r.fail(fmt.Errorf("%s(%s) at %s %d: %w", m.Name, id, path, at, err))
			return common.Address{}
		}
	}
	seed, err := newSeed(g, outputs)
	if err != nil {
		r.fail(fmt.Errorf("%s %s at %s %d: %w", g.what, id, path, at, err))
		return common.Address{}
	}
	seed.Kind, seed.ID, seed.Block, seed.Height, seed.Path = g.what, id, height, at, path
	b.lookups = append(b.lookups, AccountLookup{What: g.what, ID: id, Height: at, Path: path, Account: seed.Account})
	b.seeds = append(b.seeds, seed)
	b.projected[g.what+" "+id] = true
	seen[id] = seed.Account
	return seed.Account
}

// seed makes sure the store can project the order or position id of the
// event f: an id placed or opened in this batch, seeded in it or with a
// stored projection row or seed needs nothing; any other id predates the
// indexed blocks and is read through lookupState, whose unknown id turns the
// log into a raw row of class. Without stored facts or State it does nothing,
// and the store reports the missing projection row.
func (b *batch) seed(r *reader, f Fact, seen map[string]common.Address, g stateGetter, id, class string) {
	if r.err != nil || b.q == nil || b.reg.opts.State == nil || id == "0" || b.projected[g.what+" "+id] {
		return
	}
	var stored bool
	if err := b.q.QueryRowContext(b.ctx, g.stored, id).Scan(&stored); err != nil {
		r.fail(fmt.Errorf("read %s %s projection: %w", g.what, id, err))
		return
	}
	if stored {
		b.projected[g.what+" "+id] = true
		return
	}
	b.lookupState(r, seen, g, id, f.Block, class)
}

// seedOrder and seedPosition seed the order or position of an event that
// carries its account, whose unknown id is a projection_lookup raw row.
func (b *batch) seedOrder(r *reader, f Fact, id string) {
	b.seed(r, f, b.orders, b.reg.orderOwner, id, FailureProjectionLookup)
}

func (b *batch) seedPosition(r *reader, f Fact, id string) {
	b.seed(r, f, b.positions, b.reg.positionOwner, id, FailureProjectionLookup)
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
			b.projected["order "+id] = true
		}
		return []Row{OrderPlacedRow{f, id, f.Account, r.u256("marketId"), r.u8("orderType"), r.flag("isLong"), r.u256("triggerPrice"), r.u256("sizeUsd")}}
	}},
	{"OrderOptions", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "options"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, f, id)
		return []Row{OrderOptionsRow{f, id, r.u8("timeInForce"), r.flag("reduceOnly"), r.u256("limitPrice"), r.u256("acceptablePrice"), r.u64("deadline"), r.b16("cloid")}}
	}},
	{"OrderCancelled", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "cancelled"
		f.Account = r.addr("user")
		id := r.u256("orderId")
		b.seedOrder(r, f, id)
		return []Row{OrderCancelledRow{f, id, f.Account}}
	}},
	{"OrderExpired", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "expired"
		f.Account = r.addr("user")
		id := r.u256("orderId")
		b.seedOrder(r, f, id)
		return []Row{OrderExpiredRow{f, id, f.Account, r.u256("marketId")}}
	}},
	{"OrderTriggered", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "triggered"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, f, id)
		return []Row{OrderTriggeredRow{f, id, r.u256("marketId"), r.u256("markPrice")}}
	}},
	{"OrderExecuted", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "executed"
		id := r.u256("orderId")
		f.Account = b.orderUser(r, f, id)
		return []Row{OrderExecutedRow{f, id, r.u256("positionId"), r.u256("executionPrice")}}
	}},
	{"Fill", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "fill"
		f.Account = r.addr("taker")
		maker, taker := r.u256("makerOrderId"), r.u256("takerOrderId")
		b.seedOrder(r, f, maker)
		b.seedOrder(r, f, taker)
		return []Row{FillRow{f, r.u256("marketId"), maker, taker, r.addr("maker"), f.Account,
			r.flag("takerIsLong"), r.u256("price"), r.u256("sizeUsd"), r.i256("makerFeeUsd"), r.u256("takerFeeUsd")}}
	}},
	{"PositionOpened", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "opened"
		id := r.u256("positionId")
		f.Account = r.addr("user")
		if r.err == nil {
			b.positions[id] = f.Account
			b.projected["position "+id] = true
		}
		return []Row{PositionOpenedRow{f, id, f.Account, r.u256("marketId"), r.flag("isLong"), r.u256("sizeUsd"), r.u256("leverage"),
			r.u256("entryPrice"), r.addr("collateralToken"), r.u256("collateralAmount")}}
	}},
	{"PositionModified", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "modified"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, f, id)
		return []Row{PositionModifiedRow{f, id, r.u256("newSizeUsd"), r.u256("newCollateralUsd"), r.u256("newCollateralAmount")}}
	}},
	{"PositionEntryPriceChanged", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "entry_price_changed"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, f, id)
		return []Row{PositionEntryPriceChangedRow{f, id, r.u256("oldEntryPrice"), r.u256("newEntryPrice")}}
	}},
	{"PositionClosed", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "closed"
		f.Account = r.addr("user")
		id := r.u256("positionId")
		b.seedPosition(r, f, id)
		return []Row{PositionClosedRow{f, id, f.Account, r.u256("marketId"), r.u256("closedSizeUsd"), r.u256("exitPrice"),
			r.i256("realizedPnl"), r.flag("isFullClose")}}
	}},
	{"PositionTpSlSet", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "tpsl_set"
		id := r.u256("positionId")
		f.Account = b.positionUser(r, f, id)
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
	{"PositionFundingApplied", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "applied"
		f.Account = r.addr("user")
		id := r.u256("positionId")
		b.seedPosition(r, f, id)
		return []Row{PositionFundingAppliedRow{f, id, f.Account, r.u256("marketId"), r.i256("fundingPaymentUsd"),
			r.u256("newCollateralUsd"), r.u256("newCollateralAmount")}}
	}},
	{"OpenInterestChanged", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "oi"
		return []Row{OpenInterestChangedRow{f, r.u256("marketId"), r.u256("longOI"), r.u256("shortOI"), r.u256("deltaUsd"), r.flag("isIncrease")}}
	}},
	{"Liquidation", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "liquidated"
		f.Account = r.addr("user")
		id := r.u256("positionId")
		b.seedPosition(r, f, id)
		return []Row{LiquidationRow{f, id, f.Account, r.u256("marketId"), r.u256("liquidationPrice"), r.u256("penalty"), r.addr("keeper")}}
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
		f.Account = b.positionUser(r, f, id)
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
