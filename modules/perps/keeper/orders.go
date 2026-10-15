package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// fixedPointOne is 1e18, the scale of prices, USD amounts, base sizes and
// leverage.
var fixedPointOne = uint256.NewInt(1_000_000_000_000_000_000)

// OrderParams are the fields of the OrderParams tuple of contracts/perps.md
// section 2 that the keeper reads; the collateral leg runs in the precompile.
// Prices, sizeUsd and leverage are 18-decimal fixed point, Deadline is Unix
// seconds of block time (0 for none) and a zero Cloid means none.
type OrderParams struct {
	MarketID        uint64
	IsLong          bool
	OrderType       uint8
	TimeInForce     uint8
	SizeUsd         uint256.Int
	Leverage        uint256.Int
	LimitPrice      uint256.Int
	TriggerPrice    uint256.Int
	AcceptablePrice uint256.Int
	ReduceOnly      bool
	TakeProfitPrice uint256.Int
	StopLossPrice   uint256.Int
	Deadline        uint64
	Cloid           [16]byte
}

// PlaceResult is the outcome of a placement: the order id, the OrderPlaced
// and OrderOptions records that precede every fill, and the fills of the
// order when it took liquidity.
type PlaceResult struct {
	OrderID uint256.Int
	Events  []types.EventRecord
	Fills   []FillResult
}

// PlaceOrder validates an order of account in the order of contracts/perps.md
// section 5 (pause, account, market, fresh price, order type and time in
// force, deadline, tick, lot, order value, leverage, open-order limit, cloid,
// reduce-only, margin), the checks of order_validate, then stores it: a
// trigger order waits untriggered in the trigger index, a crossing or
// immediate order is matched against the book and a GTC or POST_ONLY
// remainder rests at its limit price, reserving its initial margin.
func (k *Keeper) PlaceOrder(ctx sdk.Context, account common.Address, p OrderParams) (PlaceResult, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return PlaceResult{}, err
	}
	if params.Paused && !p.ReduceOnly {
		return PlaceResult{}, fmt.Errorf("%w: opening orders are stopped", types.ErrPaused)
	}
	holder, exists, err := loadBookedAccount(k.store(ctx), account)
	if err != nil {
		return PlaceResult{}, err
	}
	if !exists || !holder.Created {
		return PlaceResult{}, fmt.Errorf("%w: %s", types.ErrNoAccount, account)
	}
	market, err := k.GetMarket(ctx, p.MarketID)
	if err != nil {
		return PlaceResult{}, err
	}
	if !market.Enabled && !p.ReduceOnly {
		return PlaceResult{}, fmt.Errorf("%w: market %d", types.ErrMarketDisabled, p.MarketID)
	}
	orderType := types.OrderType(p.OrderType)
	if !p.ReduceOnly || orderType == types.OrderTypeMarket {
		if err := k.RequireFresh(ctx, p.MarketID); err != nil {
			return PlaceResult{}, err
		}
	}
	timeInForce := types.TimeInForce(p.TimeInForce)
	if err := validateOrderKind(orderType, timeInForce); err != nil {
		return PlaceResult{}, err
	}
	now, err := blockTimeSeconds(ctx)
	if err != nil {
		return PlaceResult{}, err
	}
	if p.Deadline != 0 && p.Deadline < now {
		return PlaceResult{}, fmt.Errorf("%w: deadline %d below block time %d", types.ErrDeadlineExpired, p.Deadline, now)
	}

	order := types.Order{
		Account:         account,
		MarketID:        p.MarketID,
		IsLong:          p.IsLong,
		Type:            orderType,
		TimeInForce:     timeInForce,
		ReduceOnly:      p.ReduceOnly,
		AcceptablePrice: p.AcceptablePrice,
		Leverage:        p.Leverage,
		TakeProfitPrice: p.TakeProfitPrice,
		StopLossPrice:   p.StopLossPrice,
		Deadline:        p.Deadline,
		Cloid:           p.Cloid,
	}
	sizingPrice, err := k.orderPrices(ctx, market, p, &order)
	if err != nil {
		return PlaceResult{}, err
	}
	if err := sizeOrder(market, p.SizeUsd, sizingPrice, &order); err != nil {
		return PlaceResult{}, err
	}
	if err := checkOrderValue(params, market, order); err != nil {
		return PlaceResult{}, err
	}
	if err := checkOrderLeverage(market, order); err != nil {
		return PlaceResult{}, err
	}
	if holder.OpenOrderCount >= params.MaxOpenOrdersPerAccount {
		return PlaceResult{}, fmt.Errorf("%w: %s holds %d open orders", types.ErrTooManyOpenOrders, account,
			holder.OpenOrderCount)
	}
	if order.Cloid != ([16]byte{}) && k.store(ctx).Has(types.CloidKey(account, p.MarketID, order.Cloid)) {
		return PlaceResult{}, fmt.Errorf("%w: %x in market %d", types.ErrDuplicateCloid, order.Cloid, p.MarketID)
	}
	if order.ReduceOnly {
		if err := k.checkReduceOnly(ctx, order); err != nil {
			return PlaceResult{}, err
		}
	} else {
		tier, err := k.FeeTier(ctx, account)
		if err != nil {
			return PlaceResult{}, err
		}
		if err := k.CheckOpen(ctx, OpenRequest{
			Account: account, MarketID: order.MarketID, IsLong: order.IsLong,
			NotionalUsd: sdk.NewIntFromBigInt(order.SizeUsd.ToBig()), Leverage: order.Leverage,
			TakerFeePpm: tier.TakerFeePpm,
		}); err != nil {
			return PlaceResult{}, err
		}
	}

	if err := k.admitOrder(ctx, &order); err != nil {
		return PlaceResult{}, err
	}
	result := PlaceResult{OrderID: order.ID, Events: []types.EventRecord{orderPlacedEvent(order), orderOptionsEvent(order)}}
	if isTriggerOrder(order.Type) {
		order.Status = types.OrderStatusUntriggered
		k.setOrder(ctx, order)
		k.store(ctx).Set(triggerKey(order), wordBytes(order.ID))
		return result, nil
	}
	order.Status = types.OrderStatusResting
	k.setOrder(ctx, order)
	takes, err := k.takesLiquidity(ctx, order)
	if err != nil {
		return PlaceResult{}, err
	}
	if !takes {
		if err := k.BookInsert(ctx, order); err != nil {
			return PlaceResult{}, err
		}
		return result, nil
	}
	matched, err := k.Match(ctx, market, order)
	if err != nil {
		return PlaceResult{}, err
	}
	result.Fills = matched.Fills
	result.Events = append(result.Events, matched.Events...)
	return result, nil
}

// validateOrderKind refuses an order type above STOP_MARKET and a time in
// force above POST_ONLY or one the type does not allow: a market order and
// a triggered market order fill at once, so they take IOC or FOK only.
func validateOrderKind(orderType types.OrderType, timeInForce types.TimeInForce) error {
	if orderType > types.OrderTypeStopMarket {
		return fmt.Errorf("%w: %d", types.ErrInvalidOrderType, orderType)
	}
	if timeInForce > types.TimeInForcePostOnly {
		return fmt.Errorf("%w: %d", types.ErrInvalidTimeInForce, timeInForce)
	}
	if isMarketKind(orderType) && timeInForce != types.TimeInForceIOC && timeInForce != types.TimeInForceFOK {
		return fmt.Errorf("%w: order type %d takes IOC or FOK, not %d", types.ErrInvalidTimeInForce, orderType, timeInForce)
	}
	return nil
}

// isMarketKind is true for the order types that fill at once at mark when
// placed or triggered.
func isMarketKind(orderType types.OrderType) bool {
	switch orderType {
	case types.OrderTypeMarket, types.OrderTypeStopMarket, types.OrderTypeTakeProfit, types.OrderTypeStopLoss:
		return true
	default:
		return false
	}
}

// isTriggerOrder is true for the order types that wait in the trigger index
// until mark reaches their trigger price.
func isTriggerOrder(orderType types.OrderType) bool {
	return orderType != types.OrderTypeLimit && orderType != types.OrderTypeMarket
}

// orderPrices checks the limit and trigger prices of the order against the
// market tick, stores them on the order and returns the sizing price: the
// limit price of a LIMIT or STOP_LIMIT order, mark for every other type. A
// LIMIT order carries its limit price as its trigger price, as the
// application reads it. A trigger price must be non-zero and on the side of
// mark from which its condition is still to be reached.
func (k *Keeper) orderPrices(ctx sdk.Context, market types.Market, p OrderParams, order *types.Order) (uint256.Int, error) {
	tick := decimalStep(market.PxDecimals)
	if order.Type == types.OrderTypeLimit || order.Type == types.OrderTypeStopLimit {
		if p.LimitPrice.IsZero() {
			return uint256.Int{}, fmt.Errorf("%w: zero limit price", types.ErrZeroAmount)
		}
		if err := checkTick(p.LimitPrice, tick, "limit"); err != nil {
			return uint256.Int{}, err
		}
		order.LimitPrice = p.LimitPrice
	}
	if order.Type == types.OrderTypeLimit {
		order.TriggerPrice = p.LimitPrice
		return p.LimitPrice, nil
	}
	state, err := k.GetMarketState(ctx, market.ID)
	if err != nil {
		return uint256.Int{}, err
	}
	mark := state.MarkPrice
	if mark.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, market.ID)
	}
	if order.Type == types.OrderTypeMarket {
		return mark, nil
	}
	if p.TriggerPrice.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: zero trigger price", types.ErrInvalidTriggerPrice)
	}
	if err := checkTick(p.TriggerPrice, tick, "trigger"); err != nil {
		return uint256.Int{}, err
	}
	order.TriggerPrice = p.TriggerPrice
	if rising := triggerDirection(*order) == types.TriggerRising; (rising && !mark.Lt(&p.TriggerPrice)) ||
		(!rising && !mark.Gt(&p.TriggerPrice)) {
		return uint256.Int{}, fmt.Errorf("%w: trigger %s already reached at mark %s", types.ErrInvalidTriggerPrice,
			p.TriggerPrice.Dec(), mark.Dec())
	}
	if order.Type == types.OrderTypeStopLimit {
		return p.LimitPrice, nil
	}
	return mark, nil
}

// decimalStep is 10^(18 - decimals), the 18-decimal value of one unit of the
// last decimal: the tick for pxDecimals and the lot for szDecimals.
// Market.Validate bounds szDecimals + pxDecimals by 18.
func decimalStep(decimals uint8) uint256.Int {
	var step uint256.Int
	step.Exp(uint256.NewInt(10), uint256.NewInt(uint64(types.USDDecimals)-uint64(decimals)))
	return step
}

func checkTick(price, tick uint256.Int, name string) error {
	var residue uint256.Int
	if !residue.Mod(&price, &tick).IsZero() {
		return fmt.Errorf("%w: %s price %s is not a multiple of %s", types.ErrTickSize, name, price.Dec(), tick.Dec())
	}
	return nil
}

// sizeOrder converts sizeUsd to base at the sizing price, rounds it down to
// the lot and stores the base size and the order notional, base x price /
// 1e18 rounded down.
func sizeOrder(market types.Market, sizeUsd, price uint256.Int, order *types.Order) error {
	base, overflow := new(uint256.Int).MulDivOverflow(&sizeUsd, fixedPointOne, &price)
	if overflow {
		return fmt.Errorf("%w: size %s USD at %s", types.ErrOverflow, sizeUsd.Dec(), price.Dec())
	}
	lot := decimalStep(market.SzDecimals)
	var residue uint256.Int
	base.Sub(base, residue.Mod(base, &lot))
	if base.IsZero() {
		return fmt.Errorf("%w: %s USD at %s is below one lot of %s", types.ErrLotSize, sizeUsd.Dec(), price.Dec(), lot.Dec())
	}
	notional, overflow := new(uint256.Int).MulDivOverflow(base, &price, fixedPointOne)
	if overflow {
		return fmt.Errorf("%w: notional of %s base at %s", types.ErrOverflow, base.Dec(), price.Dec())
	}
	order.SizeBase = *base
	order.SizeUsd = *notional
	return nil
}

// checkOrderValue bounds the order notional below by the minimum order value
// and above by the market-order maximum of the market's leverage band, ten
// times it (LimitOrderValueMultiple) for LIMIT and STOP_LIMIT orders.
func checkOrderValue(params types.Params, market types.Market, order types.Order) error {
	notional := order.SizeUsd.ToBig()
	if notional.Cmp(params.MinOrderValueUsd.BigInt()) < 0 {
		return fmt.Errorf("%w: %s USD below %s", types.ErrMinOrderValue, notional, params.MinOrderValueUsd)
	}
	var maxValue *big.Int
	for _, row := range params.MarketOrderMaxValues {
		if row.MinMaxLeverage <= market.MaxLeverage {
			maxValue = row.MaxValueUsd.BigInt()
		}
	}
	if maxValue == nil {
		return fmt.Errorf("%w: no order value bound for max leverage %d", types.ErrInvalidParams, market.MaxLeverage)
	}
	if !isMarketKind(order.Type) {
		maxValue = new(big.Int).Mul(maxValue, new(big.Int).SetUint64(params.LimitOrderValueMultiple))
	}
	if notional.Cmp(maxValue) > 0 {
		return fmt.Errorf("%w: %s USD above %s", types.ErrMaxOrderValue, notional, maxValue)
	}
	return nil
}

// checkOrderLeverage refuses leverage below 1x or above the maximum of the
// tier that contains the order notional with ErrLeverageTooHigh, the one
// leverage refusal of contracts/perps.md section 19; CheckOpen applies the
// tier of the resulting position.
func checkOrderLeverage(market types.Market, order types.Order) error {
	if order.Leverage.Lt(fixedPointOne) {
		return fmt.Errorf("%w: leverage %s below 1e18", types.ErrLeverageTooHigh, order.Leverage.Dec())
	}
	tiers, err := types.TiersFor(market)
	if err != nil {
		return err
	}
	notional := order.SizeUsd.ToBig()
	maxLeverage := tiers[0].MaxLeverage
	for _, tier := range tiers {
		if notional.Cmp(tier.LowerBoundUsd.BigInt()) >= 0 {
			maxLeverage = tier.MaxLeverage
		}
	}
	var limit uint256.Int
	limit.Mul(uint256.NewInt(maxLeverage), fixedPointOne)
	if order.Leverage.Gt(&limit) {
		return fmt.Errorf("%w: leverage %s above %dx for %s USD", types.ErrLeverageTooHigh, order.Leverage.Dec(),
			maxLeverage, order.SizeUsd.Dec())
	}
	return nil
}

// checkReduceOnly accepts a reduce-only order only against an open position
// of the opposite side at least as large as the order.
func (k *Keeper) checkReduceOnly(ctx sdk.Context, order types.Order) error {
	position, exists, err := k.orderPosition(ctx, order.Account, order.MarketID)
	if err != nil {
		return err
	}
	if !exists || position.IsLong == order.IsLong || order.SizeBase.Gt(&position.SizeBase) {
		return fmt.Errorf("%w: order %s base against the position of %s in market %d", types.ErrReduceOnlyViolation,
			order.SizeBase.Dec(), order.Account, order.MarketID)
	}
	return nil
}

// orderPosition reads the account's position in the market through the
// AccountMarketPosition index.
func (k *Keeper) orderPosition(ctx sdk.Context, account common.Address, marketID uint64) (types.Position, bool, error) {
	store := k.store(ctx)
	idBytes := store.Get(types.AccountMarketPositionKey(account, marketID))
	if idBytes == nil {
		return types.Position{}, false, nil
	}
	if len(idBytes) != 32 {
		return types.Position{}, false, fmt.Errorf("%w: position index of %s in market %d holds %d bytes",
			types.ErrInvalidParams, account, marketID, len(idBytes))
	}
	var positionID uint256.Int
	positionID.SetBytes32(idBytes)
	bz := store.Get(types.PositionKey(positionID))
	if bz == nil {
		return types.Position{}, false, fmt.Errorf("%w: position index of %s in market %d names missing position %s",
			types.ErrInvalidParams, account, marketID, positionID.Dec())
	}
	var position types.Position
	if err := position.Unmarshal(bz); err != nil {
		return types.Position{}, false, fmt.Errorf("%w: corrupt position %s: %v", types.ErrInvalidParams, positionID.Dec(), err)
	}
	return position, true, nil
}

// admitOrder allocates the order id and placement sequence, writes the
// account order, cloid and expiry index entries and counts the order open.
func (k *Keeper) admitOrder(ctx sdk.Context, order *types.Order) error {
	id, err := k.NextOrderID(ctx)
	if err != nil {
		return err
	}
	sequence, err := k.nextID(ctx, types.SequencePlacement)
	if err != nil {
		return err
	}
	if !sequence.IsUint64() {
		return fmt.Errorf("%w: placement sequence above 2^64-1", types.ErrOverflow)
	}
	order.ID = id
	order.Sequence = sequence.Uint64()
	store := k.store(ctx)
	holder, _, err := loadBookedAccount(store, order.Account)
	if err != nil {
		return err
	}
	holder.OpenOrderCount++
	store.Set(types.AccountKey(order.Account), holder.Marshal())
	store.Set(types.AccountOrderKey(order.Account, id), []byte{})
	if order.Cloid != ([16]byte{}) {
		store.Set(types.CloidKey(order.Account, order.MarketID, order.Cloid), wordBytes(id))
	}
	if order.Deadline != 0 {
		store.Set(types.ExpiryKey(order.Deadline, id), []byte{})
	}
	return nil
}

// takesLiquidity is true for an IOC or FOK order and for a GTC or POST_ONLY
// limit that crosses the best opposite price; a POST_ONLY order that crosses
// is refused by Match.
func (k *Keeper) takesLiquidity(ctx sdk.Context, order types.Order) (bool, error) {
	if order.TimeInForce == types.TimeInForceIOC || order.TimeInForce == types.TimeInForceFOK {
		return true, nil
	}
	if order.IsLong {
		ask, ok, err := k.BestAsk(ctx, order.MarketID)
		return ok && !ask.Gt(&order.LimitPrice), err
	}
	bid, ok, err := k.BestBid(ctx, order.MarketID)
	return ok && !bid.Lt(&order.LimitPrice), err
}

// CancelOrder cancels an open order of account, the role of
// lx_perps_order_cancel_execute: only the placer may cancel, the order leaves
// the book or the trigger index, its reserved margin is released and its
// cloid and expiry entries are freed. Cancelling stays open while the venue
// is paused.
func (k *Keeper) CancelOrder(ctx sdk.Context, account common.Address, orderID uint256.Int) (types.EventRecord, error) {
	order, err := k.openOrder(ctx, orderID)
	if err != nil {
		return types.EventRecord{}, err
	}
	if order.Account != account {
		return types.EventRecord{}, fmt.Errorf("%w: order %s", types.ErrNotOrderHolder, orderID.Dec())
	}
	if err := k.withdrawOrder(ctx, order, types.OrderStatusCancelled); err != nil {
		return types.EventRecord{}, err
	}
	return types.EventRecord{Name: types.EventOrderCancelled, Args: []any{order.ID.ToBig(), order.Account}}, nil
}

// CancelByCloid cancels the open order of account that carries cloid in the
// market, as CancelOrder.
func (k *Keeper) CancelByCloid(ctx sdk.Context, account common.Address, marketID uint64, cloid [16]byte) (types.EventRecord, error) {
	if cloid == ([16]byte{}) {
		return types.EventRecord{}, fmt.Errorf("%w: zero cloid", types.ErrUnknownOrder)
	}
	idBytes := k.store(ctx).Get(types.CloidKey(account, marketID, cloid))
	if idBytes == nil {
		return types.EventRecord{}, fmt.Errorf("%w: cloid %x in market %d", types.ErrUnknownOrder, cloid, marketID)
	}
	if len(idBytes) != 32 {
		return types.EventRecord{}, fmt.Errorf("%w: cloid %x holds %d bytes", types.ErrInvalidParams, cloid, len(idBytes))
	}
	var orderID uint256.Int
	orderID.SetBytes32(idBytes)
	return k.CancelOrder(ctx, account, orderID)
}

// ExpireOrders is the expiry step of EndBlock: it expires open orders whose
// deadline is below the block time in Expiry key order, at most
// Params.ExpiriesPerBlock (256) per block, the rest carried to the next block,
// and returns one OrderExpired record per order.
func (k *Keeper) ExpireOrders(ctx sdk.Context) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	now, err := blockTimeSeconds(ctx)
	if err != nil {
		return nil, err
	}
	store := k.store(ctx)
	iterator := store.Iterator(types.ExpiryPrefix, types.ExpiryKey(now, uint256.Int{}))
	var due []uint256.Int
	for ; iterator.Valid() && uint64(len(due)) < params.ExpiriesPerBlock; iterator.Next() {
		key := iterator.Key()
		if len(key) != len(types.ExpiryPrefix)+8+32 {
			iterator.Close()
			return nil, fmt.Errorf("%w: expiry key %x", types.ErrInvalidParams, key)
		}
		var orderID uint256.Int
		orderID.SetBytes32(key[len(types.ExpiryPrefix)+8:])
		due = append(due, orderID)
	}
	iterator.Close()

	events := make([]types.EventRecord, 0, len(due))
	for _, orderID := range due {
		order, err := k.openOrder(ctx, orderID)
		if err != nil {
			return nil, fmt.Errorf("%w: expiry entry of order %s: %v", types.ErrInvalidParams, orderID.Dec(), err)
		}
		if err := k.withdrawOrder(ctx, order, types.OrderStatusExpired); err != nil {
			return nil, err
		}
		events = append(events, types.EventRecord{
			Name: types.EventOrderExpired,
			Args: []any{order.ID.ToBig(), order.Account, new(big.Int).SetUint64(order.MarketID)},
		})
	}
	return events, nil
}

// openOrder reads an UNTRIGGERED or RESTING order, ErrUnknownOrder for any
// other id.
func (k *Keeper) openOrder(ctx sdk.Context, orderID uint256.Int) (types.Order, error) {
	bz := k.store(ctx).Get(types.OrderKey(orderID))
	if bz == nil {
		return types.Order{}, fmt.Errorf("%w: %s", types.ErrUnknownOrder, orderID.Dec())
	}
	var order types.Order
	if err := order.Unmarshal(bz); err != nil {
		return types.Order{}, fmt.Errorf("%w: corrupt order %s: %v", types.ErrInvalidParams, orderID.Dec(), err)
	}
	if order.Status != types.OrderStatusResting && order.Status != types.OrderStatusUntriggered {
		return types.Order{}, fmt.Errorf("%w: order %s is closed", types.ErrUnknownOrder, orderID.Dec())
	}
	return order, nil
}

// withdrawOrder takes an open order out of the book or the trigger index and
// closes it with status.
func (k *Keeper) withdrawOrder(ctx sdk.Context, order types.Order, status types.OrderStatus) error {
	if order.Status == types.OrderStatusResting {
		if err := k.BookRemove(ctx, order); err != nil {
			return err
		}
	} else {
		k.store(ctx).Delete(triggerKey(order))
	}
	return k.closeOrder(ctx, order, status)
}

// closeOrder stores the order with its final status, frees its account
// order, cloid and expiry entries and counts it out of the account's open
// orders.
func (k *Keeper) closeOrder(ctx sdk.Context, order types.Order, status types.OrderStatus) error {
	store := k.store(ctx)
	holder, exists, err := loadBookedAccount(store, order.Account)
	if err != nil {
		return err
	}
	if !exists || holder.OpenOrderCount == 0 {
		return fmt.Errorf("%w: order %s of %s is not counted open", types.ErrInvalidParams, order.ID.Dec(), order.Account)
	}
	holder.OpenOrderCount--
	store.Set(types.AccountKey(order.Account), holder.Marshal())
	order.Status = status
	k.setOrder(ctx, order)
	store.Delete(types.AccountOrderKey(order.Account, order.ID))
	if order.Cloid != ([16]byte{}) {
		store.Delete(types.CloidKey(order.Account, order.MarketID, order.Cloid))
	}
	if order.Deadline != 0 {
		store.Delete(types.ExpiryKey(order.Deadline, order.ID))
	}
	return nil
}

// triggerDirection is the trigger index direction of a trigger order: a
// STOP_MARKET or STOP_LIMIT buy, a take-profit sell (of a long) and a
// stop-loss buy (of a short) fire as mark rises to the trigger; the mirrored
// orders fire as it falls.
func triggerDirection(order types.Order) byte {
	rising := order.IsLong
	if order.Type == types.OrderTypeTakeProfit {
		rising = !order.IsLong
	}
	if rising {
		return types.TriggerRising
	}
	return types.TriggerFalling
}

// triggerKey is the trigger index key of an untriggered order: a rising
// trigger keyed by its price, a falling one by 2^256-1 minus its price, then
// the placement sequence.
func triggerKey(order types.Order) []byte {
	direction := triggerDirection(order)
	priceKey := wordBytes(order.TriggerPrice)
	if direction == types.TriggerFalling {
		var inverted uint256.Int
		priceKey = wordBytes(*inverted.Not(&order.TriggerPrice))
	}
	return types.TriggerKey(order.MarketID, direction, priceKey, order.Sequence)
}

func wordBytes(value uint256.Int) []byte {
	word := value.Bytes32()
	return word[:]
}

func orderPlacedEvent(order types.Order) types.EventRecord {
	return types.EventRecord{
		Name: types.EventOrderPlaced,
		Args: []any{order.ID.ToBig(), order.Account, new(big.Int).SetUint64(order.MarketID), uint8(order.Type),
			order.IsLong, order.TriggerPrice.ToBig(), order.SizeUsd.ToBig()},
	}
}

func orderOptionsEvent(order types.Order) types.EventRecord {
	return types.EventRecord{
		Name: types.EventOrderOptions,
		Args: []any{order.ID.ToBig(), uint8(order.TimeInForce), order.ReduceOnly, order.LimitPrice.ToBig(),
			order.AcceptablePrice.ToBig(), order.Deadline, order.Cloid},
	}
}
