package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func stopKey(prefix []byte, ids ...uint64) []byte {
	key := append([]byte(nil), prefix...)
	for _, id := range ids {
		key = binary.BigEndian.AppendUint64(key, id)
	}
	return key
}

func stopRead(store sdk.KVStore, key []byte, value interface{}) error {
	if raw := store.Get(key); raw != nil {
		if err := json.Unmarshal(raw, value); err != nil {
			return fmt.Errorf("spot: corrupt record %x: %w", key, err)
		}
	}
	return nil
}

func stopWrite(store sdk.KVStore, key []byte, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	store.Set(key, raw)
	return nil
}

func stopUint(store sdk.KVStore, key []byte) (uint64, error) {
	raw := store.Get(key)
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("spot: corrupt counter %x", key)
	}
	return binary.BigEndian.Uint64(raw), nil
}

func stopSetUint(store sdk.KVStore, key []byte, value uint64) {
	store.Set(key, binary.BigEndian.AppendUint64(nil, value))
}

func stopValidAmount(amount sdk.Int) bool {
	return !amount.IsNil() && amount.IsPositive() && amount.BigInt().BitLen() <= 256
}

func stopExpired(ctx sdk.Context, expiry uint64) bool {
	return expiry != 0 && ctx.BlockTime().Unix() >= 0 && expiry <= uint64(ctx.BlockTime().Unix())
}

func (k *Keeper) GetStopLimitOrder(ctx sdk.Context, id uint64) (types.StopLimitOrder, error) {
	order := types.StopLimitOrder{Amount: sdk.ZeroInt(), StopPrice: sdk.ZeroInt(), LimitPrice: sdk.ZeroInt()}
	err := stopRead(ctx.KVStore(k.storeKey), stopKey(types.StopLimitOrderPrefix, id), &order)
	if err == nil && order.ID != 0 {
		if order.ID != id {
			return order, fmt.Errorf("spot: stop-limit record id mismatch")
		}
		err = order.Validate()
	}
	return order, err
}

func (k *Keeper) GetStopLossOrder(ctx sdk.Context, id uint64) (types.StopLossOrder, error) {
	order := types.StopLossOrder{Amount: sdk.ZeroInt(), TriggerPrice: sdk.ZeroInt()}
	err := stopRead(ctx.KVStore(k.storeKey), stopKey(types.StopLossOrderPrefix, id), &order)
	if err == nil && order.ID != 0 {
		if order.ID != id {
			return order, fmt.Errorf("spot: stop-loss record id mismatch")
		}
		err = order.Validate()
	}
	return order, err
}

func (k *Keeper) stopPlacement(ctx sdk.Context, user, stablecoin, token common.Address, amount sdk.Int, expiry uint64) error {
	params := k.GetParams(ctx)
	if params.OrdersPaused {
		return types.ErrEnforcedPause
	}
	if user == (common.Address{}) || stablecoin == (common.Address{}) || token == (common.Address{}) {
		return types.ErrZeroAddress
	}
	if stablecoin == token {
		return types.ErrSameToken
	}
	if !stopValidAmount(amount) {
		return types.ErrZeroAmount
	}
	if ctx.BlockTime().Unix() < 0 || stopExpired(ctx, expiry) {
		return types.ErrInvalidExpiry
	}
	var stable, base *types.TokenParams
	for i := range params.Tokens {
		registered := &params.Tokens[i]
		if registered.Address == stablecoin {
			stable = registered
		}
		if registered.Address == token {
			base = registered
		}
	}
	if stable == nil || base == nil {
		return types.ErrTokenNotRegistered
	}
	if !stable.IsStablecoin {
		return types.ErrNotAStablecoin
	}
	if base.IsStablecoin {
		return types.ErrTokenIsStablecoin
	}
	if stable.Paused || base.Paused {
		return types.ErrTokenPaused
	}
	if base.MarketID == 0 {
		return types.ErrInvalidOracle
	}
	if _, err := k.tokenOf(ctx, stablecoin); err != nil {
		return err
	}
	if _, err := k.tokenOf(ctx, token); err != nil {
		return err
	}
	price, err := k.IndexPrice(ctx, token)
	if err != nil {
		return err
	}
	if !stopValidAmount(price) {
		return types.ErrInvalidPrice
	}
	count, err := stopUint(ctx.KVStore(k.storeKey), append(append([]byte(nil), types.ActiveCountPrefix...), user.Bytes()...))
	if err != nil {
		return err
	}
	if count >= params.MaxActiveOrdersPerUser {
		return types.ErrTooManyActiveOrders
	}
	return nil
}

func (k *Keeper) stopNotional(ctx sdk.Context, token common.Address, amount sdk.Int) error {
	price, err := k.IndexPrice(ctx, token)
	if err != nil {
		return err
	}
	if !stopValidAmount(price) {
		return types.ErrInvalidPrice
	}
	params := k.GetParams(ctx)
	for _, registered := range params.Tokens {
		if registered.Address != token {
			continue
		}
		if registered.Decimals > types.MaxTokenDecimals {
			return types.ErrUnsupportedToken
		}
		volume := new(big.Int).Mul(amount.BigInt(), price.BigInt())
		volume.Quo(volume, new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(registered.Decimals)), nil))
		if params.MinOrderNotional.IsNil() || params.MinOrderNotional.IsNegative() {
			return fmt.Errorf("spot: invalid minimum notional")
		}
		if volume.Cmp(params.MinOrderNotional.BigInt()) < 0 {
			return types.ErrOrderBelowMinimumNotional
		}
		return nil
	}
	return types.ErrTokenNotRegistered
}

func stopEscrow(store sdk.KVStore, token common.Address, change sdk.Int) error {
	key := append(append([]byte(nil), types.EscrowPrefix...), token.Bytes()...)
	escrow := types.TokenEscrow{Token: token, Amount: sdk.ZeroInt()}
	if err := stopRead(store, key, &escrow); err != nil {
		return err
	}
	if escrow.Token != token || escrow.Amount.IsNil() || escrow.Amount.IsNegative() {
		return fmt.Errorf("spot: corrupt escrow")
	}
	amount := new(big.Int).Add(escrow.Amount.BigInt(), change.BigInt())
	if amount.Sign() < 0 || amount.BitLen() > 256 {
		return fmt.Errorf("spot: escrow outside uint256")
	}
	escrow.Amount = sdk.NewIntFromBigInt(amount)
	return stopWrite(store, key, escrow)
}

func stopActiveSlot(prefix []byte, index uint64) []byte {
	return stopKey(append(append([]byte(nil), prefix...), 0), index)
}

func stopAddActive(store sdk.KVStore, prefix []byte, id uint64) error {
	length, err := stopUint(store, prefix)
	if err != nil {
		return err
	}
	if length == math.MaxUint64 || store.Has(stopKey(prefix, id)) {
		return fmt.Errorf("spot: invalid active index insertion")
	}
	stopSetUint(store, stopActiveSlot(prefix, length), id)
	stopSetUint(store, stopKey(prefix, id), length+1)
	stopSetUint(store, prefix, length+1)
	return nil
}

func stopRemoveActive(store sdk.KVStore, prefix []byte, id uint64) error {
	length, err := stopUint(store, prefix)
	if err != nil {
		return err
	}
	position, err := stopUint(store, stopKey(prefix, id))
	if err != nil {
		return err
	}
	if length == 0 || position == 0 || position > length {
		return fmt.Errorf("spot: missing active order %d", id)
	}
	current, err := stopUint(store, stopActiveSlot(prefix, position-1))
	if err != nil {
		return err
	}
	if current != id {
		return fmt.Errorf("spot: corrupt active position")
	}
	last, err := stopUint(store, stopActiveSlot(prefix, length-1))
	if err != nil {
		return err
	}
	if last == 0 {
		return fmt.Errorf("spot: missing active tail")
	}
	tailPosition, err := stopUint(store, stopKey(prefix, last))
	if err != nil {
		return err
	}
	if tailPosition != length {
		return fmt.Errorf("spot: corrupt active tail")
	}
	if position != length {
		stopSetUint(store, stopActiveSlot(prefix, position-1), last)
		stopSetUint(store, stopKey(prefix, last), position)
	}
	store.Delete(stopActiveSlot(prefix, length-1))
	store.Delete(stopKey(prefix, id))
	stopSetUint(store, prefix, length-1)
	return nil
}

func (k *Keeper) stopNextID(ctx sdk.Context) (uint64, error) {
	id, err := stopUint(ctx.KVStore(k.storeKey), types.NextOrderIDKey)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		id = 1
	}
	if id == math.MaxUint64 {
		return 0, fmt.Errorf("spot: order id exhausted")
	}
	for _, prefix := range [][]byte{types.LimitOrderPrefix, types.StopLimitOrderPrefix, types.StopLossOrderPrefix} {
		if ctx.KVStore(k.storeKey).Has(stopKey(prefix, id)) {
			return 0, fmt.Errorf("spot: order id already allocated")
		}
	}
	return id, nil
}

func (k *Keeper) stopPlaced(ctx sdk.Context, id uint64, user, input common.Address, amount sdk.Int, expiry uint64, kind types.OrderType, active []byte) error {
	store := ctx.KVStore(k.storeKey)
	if err := stopEscrow(store, input, amount); err != nil {
		return err
	}
	if err := stopAddActive(store, active, id); err != nil {
		return err
	}
	userKey := stopKey(append(append([]byte(nil), types.UserOrderPrefix...), user.Bytes()...), id)
	if err := stopWrite(store, userKey, types.UserOrder{User: user, OrderID: id, OrderType: kind}); err != nil {
		return err
	}
	if err := stopWrite(store, stopKey(types.OrderMinOutputPrefix, id), types.OrderMinOutput{OrderID: id, MinOutput: sdk.ZeroInt()}); err != nil {
		return err
	}
	if expiry != 0 {
		if err := stopWrite(store, stopKey(types.ExpiryIndexPrefix, expiry, id), types.OrderExpiry{ExpiresAt: expiry, OrderID: id}); err != nil {
			return err
		}
	}
	key := append(append([]byte(nil), types.ActiveCountPrefix...), user.Bytes()...)
	count, err := stopUint(store, key)
	if err != nil {
		return err
	}
	if count == math.MaxUint64 {
		return types.ErrTooManyActiveOrders
	}
	stopSetUint(store, key, count+1)
	stopSetUint(store, types.NextOrderIDKey, id+1)
	return nil
}

func (k *Keeper) stopFinished(ctx sdk.Context, id uint64, user, input common.Address, amount sdk.Int, expiry uint64, active []byte) error {
	store := ctx.KVStore(k.storeKey)
	if err := stopEscrow(store, input, amount.Neg()); err != nil {
		return err
	}
	if err := stopRemoveActive(store, active, id); err != nil {
		return err
	}
	key := append(append([]byte(nil), types.ActiveCountPrefix...), user.Bytes()...)
	count, err := stopUint(store, key)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("spot: missing active user count")
	}
	stopSetUint(store, key, count-1)
	if expiry != 0 {
		store.Delete(stopKey(types.ExpiryIndexPrefix, expiry, id))
	}
	store.Delete(stopKey(types.FillBackoffPrefix, id))
	return nil
}

func stopEvent(ctx sdk.Context, name string, id uint64, attributes ...sdk.Attribute) {
	attrs := []sdk.Attribute{sdk.NewAttribute("rail", types.PECORStopOrdersAddress), sdk.NewAttribute("orderId", strconv.FormatUint(id, 10))}
	ctx.EventManager().EmitEvent(sdk.NewEvent(name, append(attrs, attributes...)...))
}

func (k *Keeper) PlaceStopLimit(ctx sdk.Context, user, stablecoin, token common.Address, amount, stopPrice, limitPrice sdk.Int, expiresAt uint64, kind types.OrderType) (uint64, error) {
	if kind != types.OrderTypeStopLimitBuy && kind != types.OrderTypeStopLimitSell {
		return 0, types.ErrInvalidPrices
	}
	if !stopValidAmount(stopPrice) || !stopValidAmount(limitPrice) {
		return 0, types.ErrInvalidPrices
	}
	if kind.IsBuy() && limitPrice.LT(stopPrice) {
		return 0, types.ErrLimitMustBeAboveStopBuy
	}
	if !kind.IsBuy() && limitPrice.GT(stopPrice) {
		return 0, types.ErrLimitMustBeBelowStopSell
	}
	if err := k.stopPlacement(ctx, user, stablecoin, token, amount, expiresAt); err != nil {
		return 0, err
	}
	input := token
	if kind.IsBuy() {
		input = stablecoin
	}
	if err := k.stopNotional(ctx, input, amount); err != nil {
		return 0, err
	}
	id, err := k.stopNextID(ctx)
	if err != nil {
		return 0, err
	}
	cache, write := ctx.CacheContext()
	received, err := k.pullTokens(cache, common.HexToAddress(types.PECORStopOrdersAddress), input, user, amount)
	if err != nil {
		return 0, err
	}
	if !stopValidAmount(received) {
		return 0, types.ErrZeroAmount
	}
	if err := k.stopNotional(cache, input, received); err != nil {
		return 0, err
	}
	order := types.StopLimitOrder{ID: id, User: user, Stablecoin: stablecoin, Token: token, Amount: received, StopPrice: stopPrice, LimitPrice: limitPrice, OrderType: kind, Status: types.OrderStatusPending, CreatedAt: uint64(ctx.BlockTime().Unix()), ExpiresAt: expiresAt}
	if err := order.Validate(); err != nil {
		return 0, err
	}
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLimitOrderPrefix, id), order); err != nil {
		return 0, err
	}
	if err := k.stopPlaced(cache, id, user, input, received, expiresAt, kind, types.ActiveStopLimitPrefix); err != nil {
		return 0, err
	}
	stopEvent(cache, "StopLimitOrderCreated", id, sdk.NewAttribute("user", user.Hex()), sdk.NewAttribute("orderType", strconv.Itoa(int(kind))), sdk.NewAttribute("amount", received.String()), sdk.NewAttribute("stopPrice", stopPrice.String()), sdk.NewAttribute("limitPrice", limitPrice.String()))
	write()
	return id, nil
}

func (k *Keeper) PlaceStopLoss(ctx sdk.Context, user, token, stablecoin common.Address, amount, triggerPrice sdk.Int, expiresAt uint64) (uint64, error) {
	if !stopValidAmount(triggerPrice) {
		return 0, types.ErrInvalidPrice
	}
	if err := k.stopPlacement(ctx, user, stablecoin, token, amount, expiresAt); err != nil {
		return 0, err
	}
	if err := k.stopNotional(ctx, token, amount); err != nil {
		return 0, err
	}
	id, err := k.stopNextID(ctx)
	if err != nil {
		return 0, err
	}
	cache, write := ctx.CacheContext()
	received, err := k.pullTokens(cache, common.HexToAddress(types.PECORStopOrdersAddress), token, user, amount)
	if err != nil {
		return 0, err
	}
	if !stopValidAmount(received) {
		return 0, types.ErrZeroAmount
	}
	if err := k.stopNotional(cache, token, received); err != nil {
		return 0, err
	}
	order := types.StopLossOrder{ID: id, User: user, Stablecoin: stablecoin, Token: token, Amount: received, TriggerPrice: triggerPrice, Status: types.OrderStatusPending, CreatedAt: uint64(ctx.BlockTime().Unix()), ExpiresAt: expiresAt}
	if err := order.Validate(); err != nil {
		return 0, err
	}
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLossOrderPrefix, id), order); err != nil {
		return 0, err
	}
	if err := k.stopPlaced(cache, id, user, token, received, expiresAt, types.OrderTypeStopLoss, types.ActiveStopLossPrefix); err != nil {
		return 0, err
	}
	stopEvent(cache, "StopLossOrderCreated", id, sdk.NewAttribute("user", user.Hex()), sdk.NewAttribute("amount", received.String()), sdk.NewAttribute("triggerPrice", triggerPrice.String()))
	write()
	return id, nil
}

func (k *Keeper) ActivateStopLimit(ctx sdk.Context, id uint64) (bool, error) {
	if k.GetParams(ctx).OrdersPaused {
		return false, types.ErrEnforcedPause
	}
	order, err := k.GetStopLimitOrder(ctx, id)
	if err != nil {
		return false, err
	}
	if order.ID == 0 || order.Status != types.OrderStatusPending {
		return false, types.ErrOrderNotPending
	}
	if stopExpired(ctx, order.ExpiresAt) {
		return false, types.ErrOrderExpired
	}
	price, err := k.IndexPrice(ctx, order.Token)
	if err != nil {
		return false, err
	}
	if !stopValidAmount(price) {
		return false, types.ErrInvalidPrice
	}
	crossed := price.LTE(order.StopPrice)
	if order.OrderType.IsBuy() {
		crossed = price.GTE(order.StopPrice)
	}
	if !crossed {
		return false, nil
	}
	cache, write := ctx.CacheContext()
	order.Status = types.OrderStatusActivated
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLimitOrderPrefix, id), order); err != nil {
		return false, err
	}
	stopEvent(cache, "StopLimitActivated", id, sdk.NewAttribute("activationPrice", price.String()))
	write()
	return true, nil
}

func (k *Keeper) stopFillQuote(ctx sdk.Context, id uint64, input, output common.Address, amount sdk.Int) (sdk.Int, sdk.Int, error) {
	gross, net, _, _, _, _, err := k.Price(ctx, types.FeeRailOrder, input, output, amount)
	if err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	minimum := types.OrderMinOutput{OrderID: id, MinOutput: sdk.ZeroInt()}
	if err := stopRead(ctx.KVStore(k.storeKey), stopKey(types.OrderMinOutputPrefix, id), &minimum); err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	if minimum.OrderID != id || minimum.MinOutput.IsNil() || minimum.MinOutput.IsNegative() {
		return sdk.Int{}, sdk.Int{}, fmt.Errorf("spot: invalid order minimum")
	}
	if !stopValidAmount(gross) || !stopValidAmount(net) || net.LT(minimum.MinOutput) {
		return sdk.Int{}, sdk.Int{}, types.ErrInsufficientOutput
	}
	if net.GT(gross) {
		return sdk.Int{}, sdk.Int{}, fmt.Errorf("spot: net output exceeds gross")
	}
	liquid, err := k.HasLiquidity(ctx, output, gross)
	if err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	if !liquid {
		return sdk.Int{}, sdk.Int{}, types.ErrInsufficientLiquidity
	}
	return gross, net, nil
}

func stopBook(store sdk.KVStore, token common.Address, change sdk.Int) (sdk.Int, error) {
	key := append(append([]byte(nil), types.BookPrefix...), token.Bytes()...)
	var book types.TokenBook
	if err := stopRead(store, key, &book); err != nil {
		return sdk.Int{}, err
	}
	if book.Token != token || book.Reserves.IsNil() || book.Reserves.IsNegative() {
		return sdk.Int{}, fmt.Errorf("spot: missing token book")
	}
	reserves := new(big.Int).Add(book.Reserves.BigInt(), change.BigInt())
	if reserves.Sign() < 0 || reserves.BitLen() > 256 {
		return sdk.Int{}, fmt.Errorf("spot: reserves outside uint256")
	}
	book.Reserves = sdk.NewIntFromBigInt(reserves)
	return book.Reserves, stopWrite(store, key, book)
}

func (k *Keeper) stopSettle(ctx sdk.Context, id uint64, user, input, output common.Address, amount sdk.Int) error {
	gross, net, err := k.stopFillQuote(ctx, id, input, output, amount)
	if err != nil {
		return err
	}
	if err := k.pushTokens(ctx, output, user, net); err != nil {
		return err
	}
	if err := k.AccrueFees(ctx, types.FeeRailOrder, output, gross.Sub(net)); err != nil {
		return err
	}
	inBook, err := stopBook(ctx.KVStore(k.storeKey), input, amount)
	if err != nil {
		return err
	}
	outBook, err := stopBook(ctx.KVStore(k.storeKey), output, net.Neg())
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		token    common.Address
		amount   sdk.Int
		previous sdk.Int
	}{{input, inBook, inBook.Sub(amount)}, {output, outBook, outBook.Add(net)}} {
		ctx.EventManager().EmitEvent(sdk.NewEvent("ReservesUpdated", sdk.NewAttribute("rail", types.PECORVaultAddress), sdk.NewAttribute("token", entry.token.Hex()), sdk.NewAttribute("oldReserve", entry.previous.String()), sdk.NewAttribute("newReserve", entry.amount.String())))
	}
	return nil
}

func (k *Keeper) ExecuteStopLimit(ctx sdk.Context, id uint64) error {
	if k.GetParams(ctx).OrdersPaused {
		return types.ErrEnforcedPause
	}
	order, err := k.GetStopLimitOrder(ctx, id)
	if err != nil {
		return err
	}
	if order.ID == 0 || order.Status != types.OrderStatusActivated {
		return types.ErrNotActivated
	}
	if stopExpired(ctx, order.ExpiresAt) {
		return types.ErrOrderExpired
	}
	price, err := k.IndexPrice(ctx, order.Token)
	if err != nil {
		return err
	}
	if !stopValidAmount(price) {
		return types.ErrInvalidPrice
	}
	input, output := order.Token, order.Stablecoin
	if order.OrderType.IsBuy() {
		if price.GT(order.LimitPrice) {
			return types.ErrPriceAboveLimit
		}
		input, output = output, input
	} else if price.LT(order.LimitPrice) {
		return types.ErrPriceBelowLimit
	}
	cache, write := ctx.CacheContext()
	if err := k.stopSettle(cache, id, order.User, input, output, order.Amount); err != nil {
		return err
	}
	if err := k.stopFinished(cache, id, order.User, input, order.Amount, order.ExpiresAt, types.ActiveStopLimitPrefix); err != nil {
		return err
	}
	order.Status = types.OrderStatusExecuted
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLimitOrderPrefix, id), order); err != nil {
		return err
	}
	stopEvent(cache, "StopLimitExecuted", id, sdk.NewAttribute("executionPrice", price.String()))
	write()
	return nil
}

func (k *Keeper) ExecuteStopLoss(ctx sdk.Context, id uint64) error {
	if k.GetParams(ctx).OrdersPaused {
		return types.ErrEnforcedPause
	}
	order, err := k.GetStopLossOrder(ctx, id)
	if err != nil {
		return err
	}
	if order.ID == 0 || order.Status != types.OrderStatusPending {
		return types.ErrNotPending
	}
	if stopExpired(ctx, order.ExpiresAt) {
		return types.ErrOrderExpired
	}
	price, err := k.IndexPrice(ctx, order.Token)
	if err != nil {
		return err
	}
	if !stopValidAmount(price) {
		return types.ErrInvalidPrice
	}
	if price.GT(order.TriggerPrice) {
		return types.ErrPriceAboveTrigger
	}
	cache, write := ctx.CacheContext()
	if err := k.stopSettle(cache, id, order.User, order.Token, order.Stablecoin, order.Amount); err != nil {
		return err
	}
	if err := k.stopFinished(cache, id, order.User, order.Token, order.Amount, order.ExpiresAt, types.ActiveStopLossPrefix); err != nil {
		return err
	}
	order.Status = types.OrderStatusExecuted
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLossOrderPrefix, id), order); err != nil {
		return err
	}
	stopEvent(cache, "StopLossOrderExecuted", id, sdk.NewAttribute("executionPrice", price.String()))
	write()
	return nil
}

func (k *Keeper) CancelStopLimit(ctx sdk.Context, caller common.Address, id uint64) error {
	return k.refundStopLimit(ctx, caller, id, false)
}

func (k *Keeper) ExpireStopLimit(ctx sdk.Context, id uint64) error {
	return k.refundStopLimit(ctx, common.Address{}, id, true)
}

func (k *Keeper) refundStopLimit(ctx sdk.Context, caller common.Address, id uint64, expired bool) error {
	order, err := k.GetStopLimitOrder(ctx, id)
	if err != nil {
		return err
	}
	if !expired && caller != order.User {
		return types.ErrNotOrderOwner
	}
	if order.ID == 0 || !order.IsActive() {
		return types.ErrCannotCancel
	}
	if expired && !stopExpired(ctx, order.ExpiresAt) {
		return types.ErrInvalidExpiry
	}
	cache, write := ctx.CacheContext()
	if err := k.pushTokens(cache, order.EscrowToken(), order.User, order.Amount); err != nil {
		return err
	}
	if err := k.stopFinished(cache, id, order.User, order.EscrowToken(), order.Amount, order.ExpiresAt, types.ActiveStopLimitPrefix); err != nil {
		return err
	}
	order.Status = types.OrderStatusCancelled
	if expired {
		order.Status = types.OrderStatusExpired
	}
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLimitOrderPrefix, id), order); err != nil {
		return err
	}
	stopEvent(cache, "StopLimitCancelled", id)
	write()
	return nil
}

func (k *Keeper) CancelStopLoss(ctx sdk.Context, caller common.Address, id uint64) error {
	return k.refundStopLoss(ctx, caller, id, false)
}

func (k *Keeper) ExpireStopLoss(ctx sdk.Context, id uint64) error {
	return k.refundStopLoss(ctx, common.Address{}, id, true)
}

func (k *Keeper) refundStopLoss(ctx sdk.Context, caller common.Address, id uint64, expired bool) error {
	order, err := k.GetStopLossOrder(ctx, id)
	if err != nil {
		return err
	}
	if !expired && caller != order.User {
		return types.ErrNotOrderOwner
	}
	if order.ID == 0 || !order.IsActive() {
		return types.ErrNotPending
	}
	if expired && !stopExpired(ctx, order.ExpiresAt) {
		return types.ErrInvalidExpiry
	}
	cache, write := ctx.CacheContext()
	if err := k.pushTokens(cache, order.Token, order.User, order.Amount); err != nil {
		return err
	}
	if err := k.stopFinished(cache, id, order.User, order.Token, order.Amount, order.ExpiresAt, types.ActiveStopLossPrefix); err != nil {
		return err
	}
	order.Status = types.OrderStatusCancelled
	if expired {
		order.Status = types.OrderStatusExpired
	}
	if err := stopWrite(cache.KVStore(k.storeKey), stopKey(types.StopLossOrderPrefix, id), order); err != nil {
		return err
	}
	stopEvent(cache, "StopLossOrderCancelled", id)
	write()
	return nil
}

func (k *Keeper) stopAuthorized(ctx sdk.Context, caller common.Address) error {
	params := k.GetParams(ctx)
	if params.OrdersPaused {
		return types.ErrEnforcedPause
	}
	if params.KeeperOpen {
		return nil
	}
	for _, keeper := range params.Keepers {
		if keeper == caller {
			return nil
		}
	}
	return types.ErrNotAuthorized
}

func (k *Keeper) BatchCheckAndExecuteStopLimits(ctx sdk.Context, caller common.Address, ids []uint64) (uint64, uint64, error) {
	if err := k.stopAuthorized(ctx, caller); err != nil {
		return 0, 0, err
	}
	var activated, executed uint64
	for _, id := range ids {
		order, err := k.GetStopLimitOrder(ctx, id)
		if err != nil {
			continue
		}
		if order.ID != 0 && order.Status == types.OrderStatusPending {
			changed, err := k.ActivateStopLimit(ctx, id)
			if err != nil {
				continue
			}
			if changed {
				activated++
			}
		}
		if err := k.ExecuteStopLimit(ctx, id); err == nil {
			executed++
		}
	}
	return activated, executed, nil
}

func (k *Keeper) BatchExecuteStopLossOrders(ctx sdk.Context, caller common.Address, ids []uint64) (uint64, error) {
	if err := k.stopAuthorized(ctx, caller); err != nil {
		return 0, err
	}
	var executed uint64
	for _, id := range ids {
		if err := k.ExecuteStopLoss(ctx, id); err == nil {
			executed++
		}
	}
	return executed, nil
}

func (k *Keeper) GetActiveStopLimitOrderCount(ctx sdk.Context) (uint64, error) {
	return stopUint(ctx.KVStore(k.storeKey), types.ActiveStopLimitPrefix)
}

func (k *Keeper) GetActiveStopLossOrderCount(ctx sdk.Context) (uint64, error) {
	return stopUint(ctx.KVStore(k.storeKey), types.ActiveStopLossPrefix)
}

func (k *Keeper) stopActiveAt(ctx sdk.Context, prefix []byte, index uint64) (uint64, error) {
	store := ctx.KVStore(k.storeKey)
	length, err := stopUint(store, prefix)
	if err != nil {
		return 0, err
	}
	if index >= length {
		return 0, fmt.Errorf("spot: active index out of bounds")
	}
	id, err := stopUint(store, stopActiveSlot(prefix, index))
	if err == nil && id == 0 {
		return 0, fmt.Errorf("spot: missing active index entry")
	}
	return id, err
}

func (k *Keeper) ActiveStopLimitOrderIDs(ctx sdk.Context, index uint64) (uint64, error) {
	return k.stopActiveAt(ctx, types.ActiveStopLimitPrefix, index)
}

func (k *Keeper) ActiveStopLossOrderIDs(ctx sdk.Context, index uint64) (uint64, error) {
	return k.stopActiveAt(ctx, types.ActiveStopLossPrefix, index)
}

func (k *Keeper) stopUserOrders(ctx sdk.Context, user common.Address, loss bool) ([]uint64, error) {
	ids := []uint64{}
	prefix := append(append([]byte(nil), types.UserOrderPrefix...), user.Bytes()...)
	iterator := sdk.KVStorePrefixIterator(ctx.KVStore(k.storeKey), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var order types.UserOrder
		if err := json.Unmarshal(iterator.Value(), &order); err != nil {
			return nil, err
		}
		if order.User != user || order.OrderID == 0 {
			return nil, fmt.Errorf("spot: corrupt user order index")
		}
		if loss && order.OrderType == types.OrderTypeStopLoss || !loss && (order.OrderType == types.OrderTypeStopLimitBuy || order.OrderType == types.OrderTypeStopLimitSell) {
			ids = append(ids, order.OrderID)
		}
	}
	return ids, nil
}

func (k *Keeper) GetUserStopLimitOrders(ctx sdk.Context, user common.Address) ([]uint64, error) {
	return k.stopUserOrders(ctx, user, false)
}

func (k *Keeper) GetUserStopLossOrders(ctx sdk.Context, user common.Address) ([]uint64, error) {
	return k.stopUserOrders(ctx, user, true)
}

func (k *Keeper) GetActivatableStopLimits(ctx sdk.Context, maxCount uint64) ([]uint64, error) {
	return k.stopCandidates(ctx, maxCount, false)
}

func (k *Keeper) GetExecutableStopLimits(ctx sdk.Context, maxCount uint64) ([]uint64, error) {
	return k.stopCandidates(ctx, maxCount, true)
}

func (k *Keeper) stopCandidates(ctx sdk.Context, maxCount uint64, activated bool) ([]uint64, error) {
	if maxCount > 100 {
		maxCount = 100
	}
	ids := []uint64{}
	if maxCount == 0 || k.GetParams(ctx).OrdersPaused {
		return ids, nil
	}
	length, err := k.GetActiveStopLimitOrderCount(ctx)
	if err != nil {
		return nil, err
	}
	for i := uint64(0); i < length && uint64(len(ids)) < maxCount; i++ {
		id, err := k.ActiveStopLimitOrderIDs(ctx, i)
		if err != nil {
			return nil, err
		}
		order, err := k.GetStopLimitOrder(ctx, id)
		if err != nil {
			return nil, err
		}
		expected := types.OrderStatusPending
		if activated {
			expected = types.OrderStatusActivated
		}
		if order.ID == 0 || order.Status != expected || stopExpired(ctx, order.ExpiresAt) {
			continue
		}
		price, err := k.IndexPrice(ctx, order.Token)
		if err != nil || !stopValidAmount(price) {
			continue
		}
		matches := price.LTE(order.StopPrice)
		if order.OrderType.IsBuy() {
			matches = price.GTE(order.StopPrice)
		}
		if activated {
			matches = price.GTE(order.LimitPrice)
			input, output := order.Token, order.Stablecoin
			if order.OrderType.IsBuy() {
				matches = price.LTE(order.LimitPrice)
				input, output = output, input
			}
			if matches {
				_, _, err = k.stopFillQuote(ctx, id, input, output, order.Amount)
				matches = err == nil
			}
		}
		if matches {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
