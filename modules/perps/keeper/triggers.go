package keeper

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// triggerKeyLength is prefix, market id, direction, 32-byte price key and
// 8-byte placement sequence.
var triggerKeyLength = len(types.TriggerPrefix) + 8 + 1 + 32 + 8

var priceScale = uint256.NewInt(1_000_000_000_000_000_000)

// triggerRefusals are the refusals of a triggered order's execution that
// cancel the order in EndBlock instead of failing the market's pass.
var triggerRefusals = []error{
	types.ErrNoLiquidity, types.ErrSlippageExceeded, types.ErrInsufficientMargin, types.ErrOpenInterestCap,
	types.ErrReduceOnlyViolation, types.ErrLeverageTooHigh, types.ErrMinOrderValue, types.ErrMaxOrderValue,
	types.ErrLotSize, types.ErrFokNotFilled, types.ErrPostOnlyWouldCross, types.ErrUnknownPosition,
	types.ErrPriceStale, types.ErrMarketDisabled,
}

// AttachedTpSl is the value of prefix 0x11: the order ids of the take-profit
// and stop-loss orders attached to a position, 0 for none.
type AttachedTpSl struct {
	TakeProfitOrderID uint256.Int
	StopLossOrderID   uint256.Int
}

func (a AttachedTpSl) marshal() []byte {
	tp, sl := a.TakeProfitOrderID.Bytes32(), a.StopLossOrderID.Bytes32()
	return append(tp[:], sl[:]...)
}

// TriggerDirection is the trigger index direction of an order: STOP_MARKET
// and STOP_LIMIT buys rise and sells fall; TAKE_PROFIT closing a long (a
// sell) rises and closing a short falls; STOP_LOSS closing a long falls and
// closing a short rises.
func TriggerDirection(order types.Order) (byte, error) {
	switch order.Type {
	case types.OrderTypeStopMarket, types.OrderTypeStopLimit, types.OrderTypeStopLoss:
		if order.IsLong {
			return types.TriggerRising, nil
		}
		return types.TriggerFalling, nil
	case types.OrderTypeTakeProfit:
		if order.IsLong {
			return types.TriggerFalling, nil
		}
		return types.TriggerRising, nil
	default:
		return 0, fmt.Errorf("%w: order %s of type %d has no trigger", types.ErrInvalidOrderType, order.ID.Dec(), order.Type)
	}
}

// TriggerReached reports whether the trigger condition of contracts/perps.md
// section 7 holds at mark: a rising trigger at mark >= trigger, a falling
// trigger at mark <= trigger.
func TriggerReached(order types.Order, mark uint256.Int) (bool, error) {
	direction, err := TriggerDirection(order)
	if err != nil {
		return false, err
	}
	if direction == types.TriggerRising {
		return !mark.Lt(&order.TriggerPrice), nil
	}
	return !mark.Gt(&order.TriggerPrice), nil
}

// ValidateTriggerPrice refuses with ErrInvalidTriggerPrice a trigger price
// that is zero, off the market tick or on the wrong side of mark for its
// kind, so that the condition already holds.
func ValidateTriggerPrice(market types.Market, order types.Order, mark uint256.Int) error {
	if order.TriggerPrice.IsZero() {
		return fmt.Errorf("%w: zero trigger price", types.ErrInvalidTriggerPrice)
	}
	if market.PxDecimals > types.USDDecimals {
		return fmt.Errorf("%w: market %d pxDecimals %d", types.ErrInvalidParams, market.ID, market.PxDecimals)
	}
	var tick, remainder uint256.Int
	tick.Exp(uint256.NewInt(10), uint256.NewInt(uint64(types.USDDecimals-market.PxDecimals)))
	if !remainder.Mod(&order.TriggerPrice, &tick).IsZero() {
		return fmt.Errorf("%w: trigger price %s off tick %s", types.ErrInvalidTriggerPrice, order.TriggerPrice.Dec(), tick.Dec())
	}
	reached, err := TriggerReached(order, mark)
	if err != nil {
		return err
	}
	if reached {
		return fmt.Errorf("%w: trigger price %s on the wrong side of mark %s", types.ErrInvalidTriggerPrice,
			order.TriggerPrice.Dec(), mark.Dec())
	}
	return nil
}

// TriggerInsert adds an UNTRIGGERED order to the trigger index of prefix
// 0x0B under its market, direction, trigger price key and placement sequence.
func (k *Keeper) TriggerInsert(ctx sdk.Context, order types.Order) error {
	if order.Status != types.OrderStatusUntriggered {
		return fmt.Errorf("%w: order %s is not untriggered", types.ErrInvalidOrderType, order.ID.Dec())
	}
	if order.TriggerPrice.IsZero() {
		return fmt.Errorf("%w: order %s has no trigger price", types.ErrInvalidTriggerPrice, order.ID.Dec())
	}
	if _, err := TriggerDirection(order); err != nil {
		return err
	}
	key := triggerKey(order)
	id := order.ID.Bytes32()
	k.store(ctx).Set(key, id[:])
	return nil
}

// TriggerRemove deletes the trigger index entry of an order.
func (k *Keeper) TriggerRemove(ctx sdk.Context, order types.Order) error {
	if _, err := TriggerDirection(order); err != nil {
		return err
	}
	key := triggerKey(order)
	k.store(ctx).Delete(key)
	return nil
}

// crossedTriggers collects up to limit order ids of a market's trigger index
// that mark crossed, rising entries from the lowest trigger price up, then
// falling entries from the highest down, equal prices by placement sequence.
// While the venue is paused only reduce-only orders are collected.
func (k *Keeper) crossedTriggers(ctx sdk.Context, marketID uint64, mark uint256.Int, limit uint64, paused bool) ([]uint256.Int, error) {
	var ids []uint256.Int
	for _, direction := range []byte{types.TriggerRising, types.TriggerFalling} {
		more, err := k.crossedInDirection(ctx, marketID, direction, mark, limit-uint64(len(ids)), paused)
		if err != nil {
			return nil, err
		}
		ids = append(ids, more...)
		if uint64(len(ids)) == limit {
			break
		}
	}
	return ids, nil
}

func (k *Keeper) crossedInDirection(ctx sdk.Context, marketID uint64, direction byte, mark uint256.Int, limit uint64,
	paused bool) ([]uint256.Int, error) {
	var ids []uint256.Int
	prefix := types.TriggerDirectionPrefix(marketID, direction)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid() && uint64(len(ids)) < limit; iterator.Next() {
		key, value := iterator.Key(), iterator.Value()
		if len(key) != triggerKeyLength || len(value) != 32 {
			return nil, fmt.Errorf("%w: trigger entry %x", types.ErrInvalidParams, key)
		}
		var price uint256.Int
		price.SetBytes32(key[len(prefix) : len(prefix)+32])
		if direction == types.TriggerFalling {
			price.Not(&price)
			if price.Lt(&mark) {
				break
			}
		} else if price.Gt(&mark) {
			break
		}
		var id uint256.Int
		id.SetBytes32(value)
		if paused {
			order, err := k.triggerOrderRecord(ctx, id)
			if err != nil {
				return nil, err
			}
			if !order.ReduceOnly {
				continue
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// EvaluateTriggers is EndBlock step 4 for one market: it triggers at most
// limit orders that mark crossed, in the key order of the trigger index, and
// returns how many it evaluated with the logs of the pass. The rest stay in
// the index for the next block. STOP_MARKET, TAKE_PROFIT and STOP_LOSS
// orders run as IOC orders through Match; STOP_LIMIT orders rest as limit
// orders under a new placement sequence. An order whose execution is refused
// is cancelled after its OrderTriggered log. A disabled market or a market
// whose price is not fresh is skipped for the block.
func (k *Keeper) EvaluateTriggers(ctx sdk.Context, market types.Market, mark uint256.Int, limit uint64) (uint64, []types.EventRecord, error) {
	if limit == 0 || !market.Enabled {
		return 0, nil, nil
	}
	state, err := k.GetMarketState(ctx, market.ID)
	if err != nil {
		return 0, nil, err
	}
	if !state.Fresh {
		return 0, nil, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, nil, err
	}
	ids, err := k.crossedTriggers(ctx, market.ID, mark, limit, params.Paused)
	if err != nil {
		return 0, nil, err
	}
	var (
		evaluated uint64
		events    []types.EventRecord
	)
	for _, id := range ids {
		order, err := k.triggerOrderRecord(ctx, id)
		if err != nil {
			return evaluated, events, err
		}
		// A one-cancels-other step earlier in this pass may have closed it.
		if order.Status != types.OrderStatusUntriggered {
			continue
		}
		evaluated++
		orderCtx, write := ctx.CacheContext()
		_, orderEvents, err := k.triggerOrder(orderCtx, market, order, mark)
		if err == nil {
			write()
			events = append(events, orderEvents...)
			continue
		}
		if !isTriggerRefusal(err) {
			return evaluated, events, err
		}
		cancelCtx, writeCancel := ctx.CacheContext()
		cancelEvents, err := k.cancelUntriggered(cancelCtx, order)
		if err != nil {
			return evaluated, events, err
		}
		writeCancel()
		events = append(events, orderTriggered(order, mark))
		events = append(events, cancelEvents...)
	}
	return evaluated, events, nil
}

func isTriggerRefusal(err error) bool {
	for _, refusal := range triggerRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}

// ExecuteOrder serves executeOrder: any caller triggers an UNTRIGGERED order
// whose condition holds at the market's current mark, and the order executes
// as in EndBlock; it returns the position id of the order's account in the
// market after the fills, or the closed position's id. A condition that does
// not hold returns ErrTriggerNotReached.
func (k *Keeper) ExecuteOrder(ctx sdk.Context, orderID uint256.Int) (uint256.Int, []types.EventRecord, error) {
	order, err := k.triggerOrderRecord(ctx, orderID)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if order.Status != types.OrderStatusUntriggered {
		return uint256.Int{}, nil, fmt.Errorf("%w: order %s is not an open trigger order", types.ErrUnknownOrder, orderID.Dec())
	}
	market, err := k.GetMarket(ctx, order.MarketID)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if !market.Enabled {
		return uint256.Int{}, nil, fmt.Errorf("%w: %d", types.ErrMarketDisabled, market.ID)
	}
	mark, err := k.freshMark(ctx, market.ID)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	reached, err := TriggerReached(order, mark)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if !reached {
		return uint256.Int{}, nil, fmt.Errorf("%w: order %s trigger %s, mark %s", types.ErrTriggerNotReached,
			orderID.Dec(), order.TriggerPrice.Dec(), mark.Dec())
	}
	return k.triggerOrder(ctx, market, order, mark)
}

// freshMark is the stored mark of a market whose pricefeed price is fresh.
func (k *Keeper) freshMark(ctx sdk.Context, marketID uint64) (uint256.Int, error) {
	if err := k.RequireFresh(ctx, marketID); err != nil {
		return uint256.Int{}, err
	}
	state, err := k.GetMarketState(ctx, marketID)
	if err != nil {
		return uint256.Int{}, err
	}
	if state.MarkPrice.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, marketID)
	}
	return state.MarkPrice, nil
}

// triggerOrder removes the order from the trigger index, logs OrderTriggered
// and executes it: a STOP_LIMIT order takes a new placement sequence, matches
// what its limit crosses and rests the remainder in the book; the other
// trigger types run as IOC orders through Match. A filled attached TP/SL
// order cancels the other order of its pair.
func (k *Keeper) triggerOrder(ctx sdk.Context, market types.Market, order types.Order, mark uint256.Int) (uint256.Int, []types.EventRecord, error) {
	if err := k.TriggerRemove(ctx, order); err != nil {
		return uint256.Int{}, nil, err
	}
	events := []types.EventRecord{orderTriggered(order, mark)}
	positionBefore, err := k.triggerPositionID(ctx, order.Account, order.MarketID)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	order.Status = types.OrderStatusResting
	switch order.Type {
	case types.OrderTypeStopLimit:
		sequence, err := k.nextPlacementSequence(ctx)
		if err != nil {
			return uint256.Int{}, nil, err
		}
		order.Sequence = sequence
		crosses, err := k.limitCrossesBook(ctx, order)
		if err != nil {
			return uint256.Int{}, nil, err
		}
		if crosses {
			k.setTriggerOrderRecord(ctx, order)
			matched, err := k.Match(ctx, market, order)
			if err != nil {
				return uint256.Int{}, nil, err
			}
			order = matched.Taker
			events = append(events, matched.Events...)
		} else if err := k.BookInsert(ctx, order); err != nil {
			return uint256.Int{}, nil, err
		}
	case types.OrderTypeStopMarket, types.OrderTypeTakeProfit, types.OrderTypeStopLoss:
		order.TimeInForce = types.TimeInForceIOC
		k.setTriggerOrderRecord(ctx, order)
		matched, err := k.Match(ctx, market, order)
		if err != nil {
			return uint256.Int{}, nil, err
		}
		order = matched.Taker
		events = append(events, matched.Events...)
		if order.Type != types.OrderTypeStopMarket && !order.FilledBase.IsZero() && !positionBefore.IsZero() {
			ocoEvents, err := k.cancelOtherLeg(ctx, positionBefore, order.ID)
			if err != nil {
				return uint256.Int{}, nil, err
			}
			events = append(events, ocoEvents...)
		}
	default:
		return uint256.Int{}, nil, fmt.Errorf("%w: order %s of type %d has no trigger", types.ErrInvalidOrderType, order.ID.Dec(), order.Type)
	}
	positionAfter, err := k.triggerPositionID(ctx, order.Account, order.MarketID)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if positionAfter.IsZero() {
		return positionBefore, events, nil
	}
	return positionAfter, events, nil
}

// limitCrossesBook reports whether the best opposite resting price is at or
// inside the order's limit price.
func (k *Keeper) limitCrossesBook(ctx sdk.Context, order types.Order) (bool, error) {
	if order.IsLong {
		ask, ok, err := k.BestAsk(ctx, order.MarketID)
		if err != nil || !ok {
			return false, err
		}
		return !ask.Gt(&order.LimitPrice), nil
	}
	bid, ok, err := k.BestBid(ctx, order.MarketID)
	if err != nil || !ok {
		return false, err
	}
	return !bid.Lt(&order.LimitPrice), nil
}

// SetPositionTpSl serves setPositionTpSl: it replaces the TP/SL pair attached
// to the caller's position by reduce-only TAKE_PROFIT and STOP_LOSS trigger
// orders for the whole position at the given prices, 0 for none, stores
// their ids under prefix 0x11 and logs PositionTpSlSet. setPositionTpSl(id,
// 0, 0) clears the pair.
func (k *Keeper) SetPositionTpSl(ctx sdk.Context, caller common.Address, positionID, takeProfitPrice,
	stopLossPrice uint256.Int) ([]types.EventRecord, error) {
	position, err := k.triggerPositionRecord(ctx, positionID)
	if err != nil {
		return nil, err
	}
	if position.Account != caller {
		return nil, fmt.Errorf("%w: position %s", types.ErrNotPositionHolder, positionID.Dec())
	}
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return nil, err
	}
	if !market.Enabled {
		return nil, fmt.Errorf("%w: %d", types.ErrMarketDisabled, market.ID)
	}
	mark, err := k.freshMark(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return k.attachPositionTpSl(ctx, params, market, position, takeProfitPrice, stopLossPrice, mark)
}

// attachPositionTpSl validates the TP/SL pair against mark, cancels the pair
// attached to the position, places the new reduce-only trigger orders, stores
// their ids under prefix 0x11 and logs PositionTpSlSet.
func (k *Keeper) attachPositionTpSl(ctx sdk.Context, params types.Params, market types.Market,
	position types.Position, takeProfitPrice, stopLossPrice, mark uint256.Int) ([]types.EventRecord, error) {
	positionID := position.ID
	legs := []struct {
		orderType types.OrderType
		price     uint256.Int
	}{{types.OrderTypeTakeProfit, takeProfitPrice}, {types.OrderTypeStopLoss, stopLossPrice}}
	for _, leg := range legs {
		if leg.price.IsZero() {
			continue
		}
		if err := ValidateTriggerPrice(market, attachedOrder(position, leg.orderType, leg.price), mark); err != nil {
			return nil, err
		}
	}

	events, err := k.CancelPositionTpSl(ctx, positionID)
	if err != nil {
		return nil, err
	}
	var pair AttachedTpSl
	for _, leg := range legs {
		if leg.price.IsZero() {
			continue
		}
		order, placed, err := k.placeAttachedOrder(ctx, params, position, leg.orderType, leg.price, mark)
		if err != nil {
			return nil, err
		}
		events = append(events, placed...)
		if leg.orderType == types.OrderTypeTakeProfit {
			pair.TakeProfitOrderID = order.ID
		} else {
			pair.StopLossOrderID = order.ID
		}
	}
	if !pair.TakeProfitOrderID.IsZero() || !pair.StopLossOrderID.IsZero() {
		k.store(ctx).Set(types.AttachedTpSlKey(positionID), pair.marshal())
	}
	return append(events, types.EventRecord{
		Name: types.EventPositionTpSlSet,
		Args: []any{positionID.ToBig(), takeProfitPrice.ToBig(), stopLossPrice.ToBig()},
	}), nil
}

// attachOrderTpSl attaches the TakeProfitPrice/StopLossPrice of an order that
// is not reduce-only to the account's position once its fills leave one.
func (k *Keeper) attachOrderTpSl(ctx sdk.Context, order types.Order) ([]types.EventRecord, error) {
	if order.ReduceOnly || (order.TakeProfitPrice.IsZero() && order.StopLossPrice.IsZero()) {
		return nil, nil
	}
	positionID, err := k.triggerPositionID(ctx, order.Account, order.MarketID)
	if err != nil || positionID.IsZero() {
		return nil, err
	}
	position, err := k.triggerPositionRecord(ctx, positionID)
	if err != nil {
		return nil, err
	}
	if position.SizeBase.IsZero() {
		return nil, nil
	}
	market, err := k.GetMarket(ctx, order.MarketID)
	if err != nil {
		return nil, err
	}
	mark, err := k.freshMark(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return k.attachPositionTpSl(ctx, params, market, position, order.TakeProfitPrice, order.StopLossPrice, mark)
}

// GetAttachedTpSl returns the TP/SL pair attached to a position, the zero
// pair when none is attached.
func (k *Keeper) GetAttachedTpSl(ctx sdk.Context, positionID uint256.Int) (AttachedTpSl, error) {
	bz := k.store(ctx).Get(types.AttachedTpSlKey(positionID))
	if bz == nil {
		return AttachedTpSl{}, nil
	}
	if len(bz) != 64 {
		return AttachedTpSl{}, fmt.Errorf("%w: attached TP/SL of position %s holds %d bytes", types.ErrInvalidParams,
			positionID.Dec(), len(bz))
	}
	var pair AttachedTpSl
	pair.TakeProfitOrderID.SetBytes32(bz[:32])
	pair.StopLossOrderID.SetBytes32(bz[32:])
	return pair, nil
}

// CancelPositionTpSl removes the TP/SL pair of a position and cancels its
// orders that are still open; it runs when the pair is replaced and when the
// position closes.
func (k *Keeper) CancelPositionTpSl(ctx sdk.Context, positionID uint256.Int) ([]types.EventRecord, error) {
	pair, err := k.GetAttachedTpSl(ctx, positionID)
	if err != nil {
		return nil, err
	}
	k.store(ctx).Delete(types.AttachedTpSlKey(positionID))
	var events []types.EventRecord
	for _, id := range []uint256.Int{pair.TakeProfitOrderID, pair.StopLossOrderID} {
		cancelled, err := k.cancelOpenLeg(ctx, id)
		if err != nil {
			return nil, err
		}
		events = append(events, cancelled...)
	}
	return events, nil
}

// cancelOtherLeg is one-cancels-other: once the attached order filledID of
// the position fills, the pair is removed and its other order cancelled.
func (k *Keeper) cancelOtherLeg(ctx sdk.Context, positionID, filledID uint256.Int) ([]types.EventRecord, error) {
	pair, err := k.GetAttachedTpSl(ctx, positionID)
	if err != nil {
		return nil, err
	}
	var other uint256.Int
	switch {
	case !filledID.IsZero() && pair.TakeProfitOrderID.Eq(&filledID):
		other = pair.StopLossOrderID
	case !filledID.IsZero() && pair.StopLossOrderID.Eq(&filledID):
		other = pair.TakeProfitOrderID
	default:
		return nil, nil
	}
	k.store(ctx).Delete(types.AttachedTpSlKey(positionID))
	return k.cancelOpenLeg(ctx, other)
}

func (k *Keeper) cancelOpenLeg(ctx sdk.Context, orderID uint256.Int) ([]types.EventRecord, error) {
	if orderID.IsZero() {
		return nil, nil
	}
	order, err := k.triggerOrderRecord(ctx, orderID)
	if errors.Is(err, types.ErrUnknownOrder) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if order.Status != types.OrderStatusUntriggered {
		return nil, nil
	}
	return k.cancelUntriggered(ctx, order)
}

func attachedOrder(position types.Position, orderType types.OrderType, triggerPrice uint256.Int) types.Order {
	return types.Order{
		Account:      position.Account,
		MarketID:     position.MarketID,
		IsLong:       !position.IsLong,
		Type:         orderType,
		TimeInForce:  types.TimeInForceIOC,
		Status:       types.OrderStatusUntriggered,
		ReduceOnly:   true,
		TriggerPrice: triggerPrice,
		SizeBase:     position.SizeBase,
		Leverage:     position.Leverage,
	}
}

// placeAttachedOrder stores one reduce-only trigger order closing the whole
// position, sized at mark, and logs OrderPlaced and OrderOptions.
func (k *Keeper) placeAttachedOrder(ctx sdk.Context, params types.Params, position types.Position,
	orderType types.OrderType, triggerPrice, mark uint256.Int) (types.Order, []types.EventRecord, error) {
	order := attachedOrder(position, orderType, triggerPrice)
	if _, overflow := order.SizeUsd.MulDivOverflow(&position.SizeBase, &mark, priceScale); overflow {
		return types.Order{}, nil, fmt.Errorf("%w: notional of position %s", types.ErrOverflow, position.ID.Dec())
	}
	if err := k.adjustOpenOrders(ctx, order.Account, true, params.MaxOpenOrdersPerAccount); err != nil {
		return types.Order{}, nil, err
	}
	var err error
	if order.ID, err = k.NextOrderID(ctx); err != nil {
		return types.Order{}, nil, err
	}
	if order.Sequence, err = k.nextPlacementSequence(ctx); err != nil {
		return types.Order{}, nil, err
	}
	k.setTriggerOrderRecord(ctx, order)
	k.store(ctx).Set(types.AccountOrderKey(order.Account, order.ID), []byte{})
	if err := k.TriggerInsert(ctx, order); err != nil {
		return types.Order{}, nil, err
	}
	return order, []types.EventRecord{
		{Name: types.EventOrderPlaced, Args: []any{order.ID.ToBig(), order.Account, new(big.Int).SetUint64(order.MarketID),
			uint8(order.Type), order.IsLong, order.TriggerPrice.ToBig(), order.SizeUsd.ToBig()}},
		{Name: types.EventOrderOptions, Args: []any{order.ID.ToBig(), uint8(order.TimeInForce), order.ReduceOnly,
			order.LimitPrice.ToBig(), order.AcceptablePrice.ToBig(), order.Deadline, order.Cloid}},
	}, nil
}

// cancelUntriggered cancels an UNTRIGGERED order: it leaves the trigger,
// account, cloid and expiry indexes, the account's open order count drops by
// one and OrderCancelled is logged.
func (k *Keeper) cancelUntriggered(ctx sdk.Context, order types.Order) ([]types.EventRecord, error) {
	if err := k.TriggerRemove(ctx, order); err != nil {
		return nil, err
	}
	if err := k.adjustOpenOrders(ctx, order.Account, false, 0); err != nil {
		return nil, err
	}
	store := k.store(ctx)
	order.Status = types.OrderStatusCancelled
	k.setTriggerOrderRecord(ctx, order)
	store.Delete(types.AccountOrderKey(order.Account, order.ID))
	if order.Cloid != ([16]byte{}) {
		cloidKey := types.CloidKey(order.Account, order.MarketID, order.Cloid)
		if bz := store.Get(cloidKey); len(bz) == 32 {
			var id uint256.Int
			if id.SetBytes32(bz).Eq(&order.ID) {
				store.Delete(cloidKey)
			}
		}
	}
	if order.Deadline != 0 {
		store.Delete(types.ExpiryKey(order.Deadline, order.ID))
	}
	return []types.EventRecord{{Name: types.EventOrderCancelled, Args: []any{order.ID.ToBig(), order.Account}}}, nil
}

func orderTriggered(order types.Order, mark uint256.Int) types.EventRecord {
	return types.EventRecord{
		Name: types.EventOrderTriggered,
		Args: []any{order.ID.ToBig(), new(big.Int).SetUint64(order.MarketID), mark.ToBig()},
	}
}

// adjustOpenOrders adds or removes one open order of an account, refusing an
// account at max open orders with ErrTooManyOpenOrders.
func (k *Keeper) adjustOpenOrders(ctx sdk.Context, address common.Address, increment bool, max uint64) error {
	store := k.store(ctx)
	bz := store.Get(types.AccountKey(address))
	if bz == nil {
		return fmt.Errorf("%w: %s", types.ErrNoAccount, address.Hex())
	}
	var account types.Account
	if err := account.Unmarshal(bz); err != nil {
		return fmt.Errorf("%w: corrupt account %s: %v", types.ErrInvalidParams, address.Hex(), err)
	}
	switch {
	case increment && account.OpenOrderCount >= max:
		return fmt.Errorf("%w: account %s holds %d", types.ErrTooManyOpenOrders, address.Hex(), account.OpenOrderCount)
	case increment:
		account.OpenOrderCount++
	case account.OpenOrderCount == 0:
		return fmt.Errorf("%w: account %s has no open order to remove", types.ErrInvalidParams, address.Hex())
	default:
		account.OpenOrderCount--
	}
	store.Set(types.AccountKey(address), account.Marshal())
	return nil
}

// nextPlacementSequence allocates the next global placement sequence.
func (k *Keeper) nextPlacementSequence(ctx sdk.Context) (uint64, error) {
	sequence, err := k.nextID(ctx, types.SequencePlacement)
	if err != nil {
		return 0, err
	}
	if !sequence.IsUint64() {
		return 0, fmt.Errorf("%w: placement sequence above 2^64-1", types.ErrOverflow)
	}
	return sequence.Uint64(), nil
}

func (k *Keeper) triggerOrderRecord(ctx sdk.Context, orderID uint256.Int) (types.Order, error) {
	bz := k.store(ctx).Get(types.OrderKey(orderID))
	if bz == nil {
		return types.Order{}, fmt.Errorf("%w: %s", types.ErrUnknownOrder, orderID.Dec())
	}
	var order types.Order
	if err := order.Unmarshal(bz); err != nil {
		return types.Order{}, fmt.Errorf("%w: corrupt order %s: %v", types.ErrInvalidParams, orderID.Dec(), err)
	}
	return order, nil
}

func (k *Keeper) setTriggerOrderRecord(ctx sdk.Context, order types.Order) {
	k.store(ctx).Set(types.OrderKey(order.ID), order.Marshal())
}

func (k *Keeper) triggerPositionRecord(ctx sdk.Context, positionID uint256.Int) (types.Position, error) {
	bz := k.store(ctx).Get(types.PositionKey(positionID))
	if bz == nil {
		return types.Position{}, fmt.Errorf("%w: %s", types.ErrUnknownPosition, positionID.Dec())
	}
	var position types.Position
	if err := position.Unmarshal(bz); err != nil {
		return types.Position{}, fmt.Errorf("%w: corrupt position %s: %v", types.ErrInvalidParams, positionID.Dec(), err)
	}
	return position, nil
}

// triggerPositionID is the id of the account's position in the market, 0
// when it holds none.
func (k *Keeper) triggerPositionID(ctx sdk.Context, account common.Address, marketID uint64) (uint256.Int, error) {
	bz := k.store(ctx).Get(types.AccountMarketPositionKey(account, marketID))
	if bz == nil {
		return uint256.Int{}, nil
	}
	if len(bz) != 32 {
		return uint256.Int{}, fmt.Errorf("%w: position index of %s in market %d holds %d bytes", types.ErrInvalidParams,
			account.Hex(), marketID, len(bz))
	}
	var id uint256.Int
	id.SetBytes32(bz)
	return id, nil
}
