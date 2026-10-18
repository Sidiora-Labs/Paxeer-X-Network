package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

type LimitOrderEvent struct {
	Address common.Address
	Name    string
	Args    []interface{}
}

func limitEvent(name string, args ...interface{}) LimitOrderEvent {
	return LimitOrderEvent{Address: common.HexToAddress(types.PECOROrdersAddress), Name: name, Args: args}
}

func orderUint(store sdk.KVStore, key []byte) (uint64, error) {
	raw := store.Get(key)
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("spot: invalid integer at %x", key)
	}
	return binary.BigEndian.Uint64(raw), nil
}

func putOrderRecord(store sdk.KVStore, key []byte, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	store.Set(key, raw)
	return nil
}

func orderUint256(value sdk.Int, positive bool) bool {
	return !value.IsNil() && !value.IsNegative() && value.BigInt().BitLen() <= 256 && (!positive || value.IsPositive())
}

func orderAdd(a, b sdk.Int) (sdk.Int, error) {
	if !orderUint256(a, false) || !orderUint256(b, false) {
		return sdk.Int{}, fmt.Errorf("spot: invalid amount")
	}
	sum := new(big.Int).Add(a.BigInt(), b.BigInt())
	if sum.BitLen() > 256 {
		return sdk.Int{}, fmt.Errorf("spot: amount overflow")
	}
	return sdk.NewIntFromBigInt(sum), nil
}

func orderNow(ctx sdk.Context) (uint64, error) {
	now := ctx.BlockTime().Unix()
	if now < 0 {
		return 0, types.ErrInvalidExpiry
	}
	return uint64(now), nil
}

func (k *Keeper) NextOrderID(ctx sdk.Context) (uint64, error) {
	store := ctx.KVStore(k.storeKey)
	if !store.Has(types.NextOrderIDKey) {
		return 1, nil
	}
	id, err := orderUint(store, types.NextOrderIDKey)
	if err != nil {
		return 0, err
	}
	if id == 0 || id == math.MaxUint64 {
		return 0, fmt.Errorf("spot: order id exhausted or invalid")
	}
	return id, nil
}

func (k *Keeper) GetLimitOrder(ctx sdk.Context, id uint64) (types.LimitOrder, error) {
	order := types.LimitOrder{Amount: sdk.ZeroInt(), TargetPrice: sdk.ZeroInt()}
	raw := ctx.KVStore(k.storeKey).Get(recordKey(types.LimitOrderPrefix, orderIDBytes(id)))
	if raw == nil {
		return order, nil
	}
	if err := json.Unmarshal(raw, &order); err != nil {
		return types.LimitOrder{}, err
	}
	if err := order.Validate(); err != nil {
		return types.LimitOrder{}, err
	}
	if order.ID != id {
		return types.LimitOrder{}, fmt.Errorf("spot: limit order id mismatch")
	}
	return order, nil
}

func (k *Keeper) GetOrderMinOutput(ctx sdk.Context, id uint64) (sdk.Int, error) {
	entry := types.OrderMinOutput{MinOutput: sdk.ZeroInt()}
	if _, err := getRecord(ctx.KVStore(k.storeKey), recordKey(types.OrderMinOutputPrefix, orderIDBytes(id)), &entry); err != nil {
		return sdk.Int{}, err
	}
	if entry.OrderID != 0 && entry.OrderID != id || !orderUint256(entry.MinOutput, false) {
		return sdk.Int{}, fmt.Errorf("spot: invalid order min output %d", id)
	}
	return entry.MinOutput, nil
}

func setOrderMinOutput(store sdk.KVStore, id uint64, minimum sdk.Int) error {
	if !orderUint256(minimum, false) {
		return fmt.Errorf("spot: amount out of range")
	}
	return setRecord(store, recordKey(types.OrderMinOutputPrefix, orderIDBytes(id)),
		types.OrderMinOutput{OrderID: id, MinOutput: minimum})
}

func (k *Keeper) GetEscrow(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	escrow, err := k.tokenEscrow(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	if !orderUint256(escrow, false) {
		return sdk.Int{}, fmt.Errorf("spot: invalid escrow of %s", token.Hex())
	}
	return escrow, nil
}

func setEscrow(store sdk.KVStore, token common.Address, amount sdk.Int) error {
	if !orderUint256(amount, false) {
		return fmt.Errorf("spot: amount out of range")
	}
	return setRecord(store, recordKey(types.EscrowPrefix, token.Bytes()), types.TokenEscrow{Token: token, Amount: amount})
}

func (k *Keeper) GetActiveOrderCount(ctx sdk.Context, user common.Address) (uint64, error) {
	var entry types.ActiveOrderCount
	if _, err := getRecord(ctx.KVStore(k.storeKey), recordKey(types.ActiveCountPrefix, user.Bytes()), &entry); err != nil {
		return 0, err
	}
	return entry.Count, nil
}

// setActiveOrderCount writes the active order count of user, removing the
// entry at zero.
func setActiveOrderCount(store sdk.KVStore, user common.Address, count uint64) error {
	key := recordKey(types.ActiveCountPrefix, user.Bytes())
	if count == 0 {
		store.Delete(key)
		return nil
	}
	return setRecord(store, key, types.ActiveOrderCount{User: user, Count: count})
}

func (k *Keeper) GetUserLimitOrders(ctx sdk.Context, user common.Address) ([]uint64, error) {
	prefix := recordKey(types.UserOrderPrefix, user.Bytes())
	it := sdk.KVStorePrefixIterator(ctx.KVStore(k.storeKey), prefix)
	defer it.Close()
	ids := []uint64{}
	for ; it.Valid(); it.Next() {
		var entry types.UserOrder
		if err := json.Unmarshal(it.Value(), &entry); err != nil {
			return nil, err
		}
		if len(it.Key()) != len(prefix)+8 || entry.User != user || entry.OrderID != binary.BigEndian.Uint64(it.Key()[len(prefix):]) {
			return nil, fmt.Errorf("spot: invalid user order index")
		}
		if entry.OrderType == types.OrderTypeLimitBuy || entry.OrderType == types.OrderTypeLimitSell {
			ids = append(ids, entry.OrderID)
		}
	}
	return ids, nil
}

// activeCount is the number of active order ids under prefix, each indexed
// as prefix + order id.
func activeCount(store sdk.KVStore, prefix []byte) (uint64, error) {
	ids, err := exportIDs(store, prefix)
	return uint64(len(ids)), err
}

// activePosition decodes the 8-byte big-endian list position stored as the
// value of an active index entry.
func activePosition(key, value []byte) (uint64, error) {
	if len(value) != 8 {
		return 0, fmt.Errorf("spot: corrupt active index entry %x", key)
	}
	return binary.BigEndian.Uint64(value), nil
}

// activeAt is the active order id stored at position of prefix.
// ponytail: linear scan of the index; keep a position index if lists grow large.
func activeAt(store sdk.KVStore, prefix []byte, position uint64) (uint64, error) {
	count, err := activeCount(store, prefix)
	if err != nil {
		return 0, err
	}
	if position >= count {
		return 0, fmt.Errorf("spot: active index out of bounds")
	}
	it := sdk.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	var id uint64
	found := false
	for ; it.Valid(); it.Next() {
		pos, err := activePosition(it.Key(), it.Value())
		if err != nil {
			return 0, err
		}
		if pos != position {
			continue
		}
		if found {
			return 0, fmt.Errorf("spot: duplicate active index position %d", position)
		}
		id, found = binary.BigEndian.Uint64(it.Key()[len(prefix):]), true
	}
	if !found {
		return 0, fmt.Errorf("spot: missing active index position %d", position)
	}
	return id, nil
}

// activeOrdered is the active order ids of prefix ordered by stored position.
func activeOrdered(store sdk.KVStore, prefix []byte) ([]uint64, error) {
	count, err := activeCount(store, prefix)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, count)
	seen := make([]bool, count)
	it := sdk.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		pos, err := activePosition(it.Key(), it.Value())
		if err != nil {
			return nil, err
		}
		if pos >= count || seen[pos] {
			return nil, fmt.Errorf("spot: corrupt active index entry %x", it.Key())
		}
		ids[pos], seen[pos] = binary.BigEndian.Uint64(it.Key()[len(prefix):]), true
	}
	return ids, nil
}

func activeAdd(store sdk.KVStore, prefix []byte, id uint64) error {
	key := recordKey(prefix, orderIDBytes(id))
	if id == 0 || store.Has(key) {
		return fmt.Errorf("spot: invalid active index insertion")
	}
	count, err := activeCount(store, prefix)
	if err != nil {
		return err
	}
	store.Set(key, orderIDBytes(count))
	return nil
}

// activeRemove swaps the last active entry of prefix into the position of id
// and pops id.
func activeRemove(store sdk.KVStore, prefix []byte, id uint64) error {
	key := recordKey(prefix, orderIDBytes(id))
	value := store.Get(key)
	if value == nil {
		return fmt.Errorf("spot: missing active order %d", id)
	}
	pos, err := activePosition(key, value)
	if err != nil {
		return err
	}
	count, err := activeCount(store, prefix)
	if err != nil {
		return err
	}
	last, err := activeAt(store, prefix, count-1)
	if err != nil {
		return err
	}
	if last != id {
		store.Set(recordKey(prefix, orderIDBytes(last)), orderIDBytes(pos))
	}
	store.Delete(key)
	return nil
}

func (k *Keeper) GetActiveLimitOrderCount(ctx sdk.Context) (uint64, error) {
	return activeCount(ctx.KVStore(k.storeKey), types.ActiveLimitPrefix)
}

func (k *Keeper) ActiveLimitOrderID(ctx sdk.Context, position uint64) (uint64, error) {
	return activeAt(ctx.KVStore(k.storeKey), types.ActiveLimitPrefix, position)
}

func (k *Keeper) appendActiveLimit(ctx sdk.Context, id uint64) error {
	return activeAdd(ctx.KVStore(k.storeKey), types.ActiveLimitPrefix, id)
}

func (k *Keeper) removeActiveLimit(ctx sdk.Context, id uint64) error {
	return activeRemove(ctx.KVStore(k.storeKey), types.ActiveLimitPrefix, id)
}

func limitExpiryKey(order types.LimitOrder) []byte {
	return recordKey(types.ExpiryIndexPrefix, orderIDBytes(order.ExpiresAt), orderIDBytes(order.ID))
}

func (k *Keeper) PlaceLimit(ctx sdk.Context, user, stablecoin, token common.Address, amount, target sdk.Int, expiry uint64, kind types.OrderType) (uint64, []LimitOrderEvent, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, nil, err
	}
	if params.OrdersPaused {
		return 0, nil, types.ErrEnforcedPause
	}
	if kind != types.OrderTypeLimitBuy && kind != types.OrderTypeLimitSell {
		return 0, nil, types.ErrOrderNotPending
	}
	if user == (common.Address{}) {
		return 0, nil, types.ErrZeroAddress
	}
	stable, err := k.tokenOf(ctx, stablecoin)
	if err != nil {
		return 0, nil, err
	}
	base, err := k.tokenOf(ctx, token)
	if err != nil {
		return 0, nil, err
	}
	if !stable.IsStablecoin {
		return 0, nil, types.ErrNotAStablecoin
	}
	if base.IsStablecoin {
		return 0, nil, types.ErrTokenIsStablecoin
	}
	if stable.Paused || base.Paused {
		return 0, nil, types.ErrTokenPaused
	}
	if base.MarketID == 0 {
		return 0, nil, types.ErrInvalidOracle
	}
	if !orderUint256(amount, true) {
		return 0, nil, types.ErrZeroAmount
	}
	if !orderUint256(target, true) {
		return 0, nil, types.ErrInvalidPrice
	}
	now, err := orderNow(ctx)
	if err != nil {
		return 0, nil, err
	}
	if expiry != 0 && expiry <= now {
		return 0, nil, types.ErrInvalidExpiry
	}
	input := base
	if kind.IsBuy() {
		input = stable
	}
	price, err := k.IndexPrice(ctx, input.Address)
	if err != nil {
		return 0, nil, err
	}
	notional, err := VolumeUSD(amount, price, input.Decimals)
	if err != nil {
		return 0, nil, err
	}
	if notional.LT(params.MinOrderNotional) {
		return 0, nil, types.ErrOrderBelowMinimumNotional
	}
	count, err := k.GetActiveOrderCount(ctx, user)
	if err != nil {
		return 0, nil, err
	}
	if count >= params.MaxActiveOrdersPerUser {
		return 0, nil, types.ErrTooManyActiveOrders
	}
	id, err := k.NextOrderID(ctx)
	if err != nil {
		return 0, nil, err
	}
	for _, prefix := range [][]byte{types.LimitOrderPrefix, types.StopLimitOrderPrefix, types.StopLossOrderPrefix} {
		if ctx.KVStore(k.storeKey).Has(recordKey(prefix, orderIDBytes(id))) {
			return 0, nil, fmt.Errorf("spot: order id already allocated")
		}
	}
	branch, write := ctx.CacheContext()
	branch, received, err := k.pullTokens(branch, common.HexToAddress(types.PECOROrdersAddress), input.Address, user, amount)
	if err != nil {
		return 0, nil, err
	}
	if !orderUint256(received, true) {
		return 0, nil, types.ErrZeroAmount
	}
	receivedNotional, err := VolumeUSD(received, price, input.Decimals)
	if err != nil {
		return 0, nil, err
	}
	if receivedNotional.LT(params.MinOrderNotional) {
		return 0, nil, types.ErrOrderBelowMinimumNotional
	}
	escrow, err := k.GetEscrow(branch, input.Address)
	if err != nil {
		return 0, nil, err
	}
	escrow, err = orderAdd(escrow, received)
	if err != nil {
		return 0, nil, err
	}
	order := types.LimitOrder{ID: id, User: user, Stablecoin: stablecoin, Token: token, Amount: received,
		TargetPrice: target, OrderType: kind, Status: types.OrderStatusPending, CreatedAt: now, ExpiresAt: expiry}
	if err := order.Validate(); err != nil {
		return 0, nil, err
	}
	store := branch.KVStore(k.storeKey)
	if err := putOrderRecord(store, recordKey(types.LimitOrderPrefix, orderIDBytes(id)), order); err != nil {
		return 0, nil, err
	}
	if err := putOrderRecord(store, recordKey(types.UserOrderPrefix, user.Bytes(), orderIDBytes(id)), types.UserOrder{User: user, OrderID: id, OrderType: kind}); err != nil {
		return 0, nil, err
	}
	if err := setEscrow(store, input.Address, escrow); err != nil {
		return 0, nil, err
	}
	if err := setOrderMinOutput(store, id, sdk.ZeroInt()); err != nil {
		return 0, nil, err
	}
	if err := k.appendActiveLimit(branch, id); err != nil {
		return 0, nil, err
	}
	if expiry != 0 {
		store.Set(limitExpiryKey(order), []byte{})
	}
	if err := setActiveOrderCount(store, user, count+1); err != nil {
		return 0, nil, err
	}
	store.Set(types.NextOrderIDKey, orderIDBytes(id+1))
	write()
	ctx.EventManager().EmitEvents(branch.EventManager().Events())
	return id, []LimitOrderEvent{limitEvent("LimitOrderCreated", new(big.Int).SetUint64(id), user, uint8(kind), received.BigInt(), target.BigInt())}, nil
}

func (k *Keeper) ModifyLimit(ctx sdk.Context, user common.Address, id uint64, target sdk.Int, expiry uint64) ([]LimitOrderEvent, error) {
	order, err := k.GetLimitOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if order.ID == 0 || order.User != user {
		return nil, types.ErrNotOrderOwner
	}
	if !order.IsActive() {
		return nil, types.ErrOrderNotPending
	}
	if !orderUint256(target, true) {
		return nil, types.ErrInvalidPrice
	}
	now, err := orderNow(ctx)
	if err != nil {
		return nil, err
	}
	if expiry != 0 && expiry <= now {
		return nil, types.ErrInvalidExpiry
	}
	branch, write := ctx.CacheContext()
	store := branch.KVStore(k.storeKey)
	if order.ExpiresAt != 0 {
		store.Delete(limitExpiryKey(order))
	}
	order.TargetPrice, order.ExpiresAt = target, expiry
	if err := order.Validate(); err != nil {
		return nil, err
	}
	if err := putOrderRecord(store, recordKey(types.LimitOrderPrefix, orderIDBytes(id)), order); err != nil {
		return nil, err
	}
	if expiry != 0 {
		store.Set(limitExpiryKey(order), []byte{})
	}
	write()
	return []LimitOrderEvent{limitEvent("LimitOrderModified", new(big.Int).SetUint64(id), target.BigInt(), new(big.Int).SetUint64(expiry))}, nil
}

func (k *Keeper) finishLimit(ctx sdk.Context, order types.LimitOrder, status types.OrderStatus) error {
	if !order.IsActive() || (status != types.OrderStatusExecuted && status != types.OrderStatusCancelled && status != types.OrderStatusExpired) {
		return types.ErrOrderNotPending
	}
	escrow, err := k.GetEscrow(ctx, order.EscrowToken())
	if err != nil {
		return err
	}
	if escrow.LT(order.Amount) {
		return types.ErrInsufficientLiquidity
	}
	count, err := k.GetActiveOrderCount(ctx, order.User)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("spot: missing active order count")
	}
	if err := k.removeActiveLimit(ctx, order.ID); err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	if err := setEscrow(store, order.EscrowToken(), escrow.Sub(order.Amount)); err != nil {
		return err
	}
	if err := setActiveOrderCount(store, order.User, count-1); err != nil {
		return err
	}
	if order.ExpiresAt != 0 {
		store.Delete(limitExpiryKey(order))
	}
	store.Delete(recordKey(types.FillBackoffPrefix, orderIDBytes(order.ID)))
	order.Status = status
	return putOrderRecord(store, recordKey(types.LimitOrderPrefix, orderIDBytes(order.ID)), order)
}

func (k *Keeper) refundLimit(ctx sdk.Context, order types.LimitOrder, status types.OrderStatus, event string) ([]LimitOrderEvent, error) {
	if err := k.limitEscrowAvailable(ctx, order); err != nil {
		return nil, err
	}
	branch, write := ctx.CacheContext()
	if err := k.finishLimit(branch, order, status); err != nil {
		return nil, err
	}
	if _, err := k.pushTokens(branch, order.EscrowToken(), order.User, order.Amount); err != nil {
		return nil, err
	}
	write()
	ctx.EventManager().EmitEvents(branch.EventManager().Events())
	return []LimitOrderEvent{limitEvent(event, new(big.Int).SetUint64(order.ID))}, nil
}

func (k *Keeper) CancelLimit(ctx sdk.Context, user common.Address, id uint64) ([]LimitOrderEvent, error) {
	order, err := k.GetLimitOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if order.ID == 0 || order.User != user {
		return nil, types.ErrNotOrderOwner
	}
	if !order.IsActive() {
		return nil, types.ErrCannotCancel
	}
	return k.refundLimit(ctx, order, types.OrderStatusCancelled, "LimitOrderCancelled")
}

func (k *Keeper) ExpireLimit(ctx sdk.Context, id uint64) ([]LimitOrderEvent, error) {
	order, err := k.GetLimitOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if order.ID == 0 || !order.IsActive() {
		return nil, types.ErrOrderNotPending
	}
	now, err := orderNow(ctx)
	if err != nil {
		return nil, err
	}
	if order.ExpiresAt == 0 || order.ExpiresAt > now {
		return nil, types.ErrInvalidExpiry
	}
	return k.refundLimit(ctx, order, types.OrderStatusExpired, "LimitOrderExpired")
}

func (k *Keeper) BatchExpire(ctx sdk.Context, ids []uint64) (uint64, []LimitOrderEvent) {
	var count uint64
	var events []LimitOrderEvent
	for _, id := range ids {
		next, err := k.ExpireLimit(ctx, id)
		if err == nil {
			count++
			events = append(events, next...)
		}
	}
	return count, events
}

func limitOutput(order types.LimitOrder) common.Address {
	if order.OrderType.IsBuy() {
		return order.Token
	}
	return order.Stablecoin
}

func (k *Keeper) limitEscrowAvailable(ctx sdk.Context, order types.LimitOrder) error {
	escrow, err := k.GetEscrow(ctx, order.EscrowToken())
	if err != nil {
		return err
	}
	_, custody, err := k.CustodyBalance(ctx, order.EscrowToken())
	if err != nil {
		return err
	}
	if escrow.LT(order.Amount) || custody.LT(escrow) {
		return types.ErrInsufficientLiquidity
	}
	return nil
}

func (k *Keeper) limitQuote(ctx sdk.Context, order types.LimitOrder) (sdk.Int, sdk.Int, error) {
	quote, err := k.Price(ctx, common.HexToAddress(types.PECOROrdersAddress), order.EscrowToken(), limitOutput(order), order.Amount)
	if err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	gross, net := quote.Gross, quote.Net
	if !orderUint256(gross, false) || !orderUint256(net, false) || net.GT(gross) {
		return sdk.Int{}, sdk.Int{}, fmt.Errorf("spot: invalid order quote")
	}
	return gross, net, nil
}

func (k *Keeper) limitMinimum(ctx sdk.Context, id uint64, net sdk.Int) error {
	minimum, err := k.GetOrderMinOutput(ctx, id)
	if err != nil {
		return err
	}
	if net.IsZero() || net.LT(minimum) {
		return types.ErrInsufficientOutput
	}
	return nil
}

func (k *Keeper) CanExecuteLimitOrder(ctx sdk.Context, id uint64) (bool, string, error) {
	order, err := k.GetLimitOrder(ctx, id)
	if err != nil {
		return false, "", err
	}
	if order.ID == 0 {
		return false, "Order not found", nil
	}
	if !order.IsActive() {
		return false, "Order not pending", nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, "", err
	}
	if params.OrdersPaused {
		return false, "Paused", nil
	}
	now, err := orderNow(ctx)
	if err != nil {
		return false, "", err
	}
	if order.ExpiresAt != 0 && order.ExpiresAt <= now {
		return false, "Order expired", nil
	}
	price, err := k.IndexPrice(ctx, order.Token)
	if err != nil {
		return false, "Price unavailable", nil
	}
	if order.OrderType.IsBuy() && price.GT(order.TargetPrice) {
		return false, "Price above target", nil
	}
	if !order.OrderType.IsBuy() && price.LT(order.TargetPrice) {
		return false, "Price below target", nil
	}
	gross, net, err := k.limitQuote(ctx, order)
	if err != nil {
		return false, "Price unavailable", nil
	}
	if err := k.limitEscrowAvailable(ctx, order); err != nil {
		return false, "Escrow deficit", nil
	}
	if err := k.limitMinimum(ctx, id, net); err != nil {
		if err == types.ErrInsufficientOutput {
			return false, "Insufficient output", nil
		}
		return false, "", err
	}
	liquid, err := k.HasLiquidity(ctx, limitOutput(order), gross)
	if err != nil {
		return false, "", err
	}
	if !liquid {
		return false, "Insufficient liquidity", nil
	}
	return true, "Executable", nil
}

func (k *Keeper) GetExecutableLimitOrders(ctx sdk.Context, maxCount uint64) ([]uint64, error) {
	if maxCount > 100 {
		maxCount = 100
	}
	ids := make([]uint64, 0, maxCount)
	count, err := k.GetActiveLimitOrderCount(ctx)
	if err != nil {
		return nil, err
	}
	for position := uint64(0); position < count && uint64(len(ids)) < maxCount; position++ {
		id, err := k.ActiveLimitOrderID(ctx, position)
		if err != nil {
			return nil, err
		}
		executable, _, err := k.CanExecuteLimitOrder(ctx, id)
		if err != nil {
			return nil, err
		}
		if executable {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (k *Keeper) limitBook(ctx sdk.Context, token common.Address, amount sdk.Int, add bool) (LimitOrderEvent, error) {
	store := ctx.KVStore(k.storeKey)
	key := recordKey(types.BookPrefix, token.Bytes())
	raw := store.Get(key)
	if raw == nil {
		return LimitOrderEvent{}, types.ErrInsufficientLiquidity
	}
	var book types.TokenBook
	if err := json.Unmarshal(raw, &book); err != nil {
		return LimitOrderEvent{}, err
	}
	if book.Token != token || !orderUint256(book.Reserves, false) {
		return LimitOrderEvent{}, fmt.Errorf("spot: invalid token book")
	}
	oldReserve := book.Reserves
	if add {
		next, err := orderAdd(book.Reserves, amount)
		if err != nil {
			return LimitOrderEvent{}, err
		}
		book.Reserves = next
	} else {
		if book.Reserves.LT(amount) {
			return LimitOrderEvent{}, types.ErrInsufficientLiquidity
		}
		book.Reserves = book.Reserves.Sub(amount)
	}
	if err := putOrderRecord(store, key, book); err != nil {
		return LimitOrderEvent{}, err
	}
	return LimitOrderEvent{Address: common.HexToAddress(types.PECORVaultAddress), Name: "ReservesUpdated", Args: []interface{}{token, oldReserve.BigInt(), book.Reserves.BigInt()}}, nil
}

func (k *Keeper) ExecuteLimit(ctx sdk.Context, id uint64) ([]LimitOrderEvent, error) {
	order, err := k.GetLimitOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if order.ID == 0 || !order.IsActive() {
		return nil, types.ErrOrderNotPending
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if params.OrdersPaused {
		return nil, types.ErrEnforcedPause
	}
	now, err := orderNow(ctx)
	if err != nil {
		return nil, err
	}
	if order.ExpiresAt != 0 && order.ExpiresAt <= now {
		return nil, types.ErrOrderExpired
	}
	price, err := k.IndexPrice(ctx, order.Token)
	if err != nil {
		return nil, err
	}
	if order.OrderType.IsBuy() && price.GT(order.TargetPrice) {
		return nil, types.ErrPriceAboveTarget
	}
	if !order.OrderType.IsBuy() && price.LT(order.TargetPrice) {
		return nil, types.ErrPriceBelowTarget
	}
	gross, net, err := k.limitQuote(ctx, order)
	if err != nil {
		return nil, err
	}
	if err := k.limitEscrowAvailable(ctx, order); err != nil {
		return nil, err
	}
	if err := k.limitMinimum(ctx, id, net); err != nil {
		return nil, err
	}
	liquid, err := k.HasLiquidity(ctx, limitOutput(order), gross)
	if err != nil {
		return nil, err
	}
	if !liquid {
		return nil, types.ErrInsufficientLiquidity
	}
	branch, write := ctx.CacheContext()
	if err := k.finishLimit(branch, order, types.OrderStatusExecuted); err != nil {
		return nil, err
	}
	incoming, err := k.limitBook(branch, order.EscrowToken(), order.Amount, true)
	if err != nil {
		return nil, err
	}
	outgoing, err := k.limitBook(branch, limitOutput(order), net, false)
	if err != nil {
		return nil, err
	}
	if err := k.AccrueFees(branch, common.HexToAddress(types.PECOROrdersAddress), limitOutput(order), gross.Sub(net)); err != nil {
		return nil, err
	}
	if _, err := k.pushTokens(branch, limitOutput(order), order.User, net); err != nil {
		return nil, err
	}
	write()
	ctx.EventManager().EmitEvents(branch.EventManager().Events())
	return []LimitOrderEvent{incoming, outgoing, limitEvent("LimitOrderExecuted", new(big.Int).SetUint64(id), price.BigInt())}, nil
}

func (k *Keeper) BatchExecuteLimit(ctx sdk.Context, ids []uint64) (uint64, []LimitOrderEvent) {
	var count uint64
	var events []LimitOrderEvent
	for _, id := range ids {
		next, err := k.ExecuteLimit(ctx, id)
		if err == nil {
			count++
			events = append(events, next...)
		}
	}
	return count, events
}
