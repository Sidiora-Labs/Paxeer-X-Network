package keeper

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const (
	// marginRatioBucketMax caps the MarginRatioIndex bucket.
	marginRatioBucketMax = 65535

	// marginRatioScale is the factor of floor(100 x equity / maintenance margin).
	marginRatioScale = 100
)

var (
	usdUnit       = new(big.Int).Exp(big.NewInt(10), big.NewInt(types.USDDecimals), nil)
	bpsDenom      = big.NewInt(types.BpsDenominator)
	usdUnitPerBps = new(big.Int).Mul(usdUnit, bpsDenom)
)

// LiquidationScanResult is the outcome of one EndBlock liquidation pass:
// accounts visited in the MarginRatioIndex, accounts liquidated, closing
// orders sent, the Liquidation events of those orders and the accounts whose
// liquidation failed and was rolled back, each error naming its account.
type LiquidationScanResult struct {
	Visited    uint64
	Liquidated uint64
	Slices     uint64
	Events     []types.EventRecord
	Failures   []error
}

// Liquidate liquidates the account holding positionID when its equity is below
// its maintenance margin: the account is settled, its open orders are
// cancelled and each position is closed by a reduce-only IOC market order
// against the book, a position above Params.LiquidationSliceThresholdUsd by a
// slice of Params.LiquidationSlicePpm of its notional at most once per
// Params.LiquidationCooldownSeconds. The caller is recorded as the keeper of
// each Liquidation event and is paid nothing.
func (k *Keeper) Liquidate(ctx sdk.Context, caller common.Address, positionID uint256.Int) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	named, err := k.liquidationPosition(ctx, positionID)
	if err != nil {
		return nil, err
	}
	positions, err := k.liquidationAccountPositions(ctx, named.Account)
	if err != nil {
		return nil, err
	}
	for _, position := range positions {
		if err := k.RequireFresh(ctx, position.MarketID); err != nil {
			return nil, err
		}
	}
	liquidatable, err := k.liquidationRebucket(ctx, named.Account)
	if err != nil {
		return nil, err
	}
	if !liquidatable {
		return nil, fmt.Errorf("%w: account %s", types.ErrNotLiquidatable, named.Account)
	}
	now, err := liquidationNow(ctx)
	if err != nil {
		return nil, err
	}
	large, _, _, err := k.liquidationCloseSize(ctx, params, named)
	if err != nil {
		return nil, err
	}
	if large {
		cooling, err := k.liquidationCooling(ctx, params, named.ID, now)
		if err != nil {
			return nil, err
		}
		if cooling {
			return nil, fmt.Errorf("%w: position %s", types.ErrLiquidationCooldown, named.ID.Dec())
		}
	}
	events, _, err := k.liquidateAccount(ctx, params, named.Account, caller, now, uint64(len(positions)))
	if err != nil {
		return nil, err
	}
	if _, err := k.liquidationRebucket(ctx, named.Account); err != nil {
		return nil, err
	}
	return events, nil
}

// LiquidationScan is the EndBlock liquidation pass: it walks the
// MarginRatioIndex from bucket 0 upward, visits at most
// Params.LiquidationScanPerBlock accounts, recomputes and re-buckets each at
// mark and liquidates the liquidatable ones, at most
// Params.LiquidationsPerBlock accounts and Params.SlicesPerBlock closing
// orders; the rest carry to the next block in index order. An account holding
// a position of a market without a fresh price is skipped for the block, and
// an account whose liquidation fails is rolled back and reported in Failures.
func (k *Keeper) LiquidationScan(ctx sdk.Context) (LiquidationScanResult, error) {
	var result LiquidationScanResult
	params, err := k.GetParams(ctx)
	if err != nil {
		return result, err
	}
	now, err := liquidationNow(ctx)
	if err != nil {
		return result, err
	}
	candidates, err := k.liquidationCandidates(ctx, params.LiquidationScanPerBlock)
	if err != nil {
		return result, err
	}
	for _, account := range candidates {
		if result.Liquidated >= params.LiquidationsPerBlock || result.Slices >= params.SlicesPerBlock {
			break
		}
		result.Visited++
		positions, err := k.liquidationAccountPositions(ctx, account)
		if err != nil {
			return result, err
		}
		fresh, err := k.liquidationFresh(ctx, positions)
		if err != nil {
			return result, err
		}
		if !fresh {
			continue
		}
		cacheCtx, write := ctx.CacheContext()
		liquidatable, err := k.liquidationRebucket(cacheCtx, account)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Errorf("account %s: %w", account, err))
			continue
		}
		if !liquidatable {
			write()
			continue
		}
		events, slices, err := k.liquidateAccount(cacheCtx, params, account, common.Address{}, now,
			params.SlicesPerBlock-result.Slices)
		if err == nil {
			_, err = k.liquidationRebucket(cacheCtx, account)
		}
		if err != nil {
			result.Failures = append(result.Failures, fmt.Errorf("account %s: %w", account, err))
			continue
		}
		write()
		if slices > 0 {
			result.Liquidated++
			result.Slices += slices
			result.Events = append(result.Events, events...)
		}
	}
	return result, nil
}

// LiquidationPrice is the mark at which the position's account reaches
// maintenance with its other positions held constant (contracts/perps-
// parameters.md section 6): price - side x marginAvailable / size / (1 - l x
// side), with marginAvailable = equity - maintenance margin and l the MMR of
// the position's tier at mark, rounded down to 1e-18 USD and floored at 0.
func (k *Keeper) LiquidationPrice(ctx sdk.Context, positionID uint256.Int) (uint256.Int, error) {
	position, err := k.liquidationPosition(ctx, positionID)
	if err != nil {
		return uint256.Int{}, err
	}
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return uint256.Int{}, err
	}
	mark, err := k.liquidationMark(ctx, position.MarketID)
	if err != nil {
		return uint256.Int{}, err
	}
	summary, err := k.AccountMargin(ctx, position.Account)
	if err != nil {
		return uint256.Int{}, err
	}
	equity, maintenance := summary.Equity, summary.MaintenanceMargin
	bps, err := liquidationTierBps(market, liquidationNotional(position.SizeBase, mark))
	if err != nil {
		return uint256.Int{}, err
	}
	return liquidationPriceFormula(position.IsLong, mark, position.SizeBase,
		new(big.Int).Sub(equity.BigInt(), maintenance.BigInt()), bps)
}

// liquidationPriceFormula evaluates price - side x available x 1e18 / size x
// 10,000 / (10,000 - side x bps) as one floor division of 256-bit operands.
func liquidationPriceFormula(isLong bool, mark, size uint256.Int, available *big.Int, bps uint64) (uint256.Int, error) {
	if size.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: position of zero size", types.ErrUnknownPosition)
	}
	if bps >= types.BpsDenominator {
		return uint256.Int{}, fmt.Errorf("%w: maintenance rate %d bps", types.ErrInvalidParams, bps)
	}
	factor := new(big.Int).SetUint64(types.BpsDenominator + bps)
	if isLong {
		factor.SetUint64(types.BpsDenominator - bps)
	}
	denominator := new(big.Int).Mul(size.ToBig(), factor)
	numerator := new(big.Int).Mul(mark.ToBig(), denominator)
	shift := new(big.Int).Mul(available, usdUnitPerBps)
	if isLong {
		numerator.Sub(numerator, shift)
	} else {
		numerator.Add(numerator, shift)
	}
	price := numerator.Div(numerator, denominator)
	if price.Sign() < 0 {
		return uint256.Int{}, nil
	}
	out, overflow := uint256.FromBig(price)
	if overflow {
		return uint256.Int{}, fmt.Errorf("%w: liquidation price %s", types.ErrOverflow, price)
	}
	return *out, nil
}

// liquidateAccount settles the account, cancels its open orders and sends one
// closing order per position, at most sliceBudget orders, skipping large
// positions inside their cooldown. It returns the Liquidation events and the
// number of closing orders sent.
func (k *Keeper) liquidateAccount(ctx sdk.Context, params types.Params, account, keeper common.Address,
	now, sliceBudget uint64) ([]types.EventRecord, uint64, error) {
	events, err := k.Settle(ctx, account)
	if err != nil {
		return nil, 0, err
	}
	orderIDs, err := k.liquidationOpenOrders(ctx, account)
	if err != nil {
		return nil, 0, err
	}
	for _, orderID := range orderIDs {
		cancelled, err := k.CancelOrder(ctx, account, orderID)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, cancelled)
	}
	positions, err := k.liquidationAccountPositions(ctx, account)
	if err != nil {
		return nil, 0, err
	}
	var slices uint64
	for _, position := range positions {
		if slices >= sliceBudget {
			break
		}
		large, closeBase, basis, err := k.liquidationCloseSize(ctx, params, position)
		if err != nil {
			return nil, 0, err
		}
		if large {
			cooling, err := k.liquidationCooling(ctx, params, position.ID, now)
			if err != nil {
				return nil, 0, err
			}
			if cooling {
				continue
			}
		}
		closed, err := k.liquidationClose(ctx, position, closeBase, basis, large, keeper, now)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, closed...)
		slices++
	}
	return events, slices, nil
}

// liquidationClose matches one reduce-only IOC market order of closeBase
// against the book, records the slice time and slice basis of a large
// position that stays open and drops the cooldown of a position the order
// closed.
func (k *Keeper) liquidationClose(ctx sdk.Context, position types.Position, closeBase, basis uint256.Int, large bool,
	keeper common.Address, now uint64) ([]types.EventRecord, error) {
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return nil, err
	}
	mark, err := k.liquidationMark(ctx, position.MarketID)
	if err != nil {
		return nil, err
	}
	order := types.Order{
		Account:     position.Account,
		MarketID:    position.MarketID,
		IsLong:      !position.IsLong,
		Type:        types.OrderTypeMarket,
		TimeInForce: types.TimeInForceIOC,
		Status:      types.OrderStatusResting,
		ReduceOnly:  true,
		SizeBase:    closeBase,
		Leverage:    position.Leverage,
	}
	if err := k.admitOrder(ctx, &order); err != nil {
		return nil, err
	}
	k.setOrder(ctx, order)
	matched, err := k.Match(ctx, market, order)
	if err != nil {
		return nil, err
	}
	k.liquidationRecordSlice(ctx, position.ID, large, now, basis)
	return append(matched.Events, types.EventRecord{
		Name: types.EventLiquidation,
		Args: []any{position.ID.ToBig(), position.Account, new(big.Int).SetUint64(position.MarketID),
			mark.ToBig(), new(big.Int), keeper},
	}), nil
}

// liquidationCloseSize reports whether the position's notional at mark is
// above the slice threshold, the base size its closing order sends and the
// slice basis: when large, the slice share of the basis rounded down to the
// lot and capped at the position's size, else its size. The basis is the size
// of the position at its first slice, read from its cooldown record, or its
// current size before any slice.
func (k *Keeper) liquidationCloseSize(ctx sdk.Context, params types.Params,
	position types.Position) (bool, uint256.Int, uint256.Int, error) {
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return false, uint256.Int{}, uint256.Int{}, err
	}
	mark, err := k.liquidationMark(ctx, position.MarketID)
	if err != nil {
		return false, uint256.Int{}, uint256.Int{}, err
	}
	if liquidationNotional(position.SizeBase, mark).Cmp(params.LiquidationSliceThresholdUsd.BigInt()) <= 0 {
		return false, position.SizeBase, position.SizeBase, nil
	}
	_, basis, found, err := k.liquidationCooldownRecord(ctx, position.ID)
	if err != nil {
		return false, uint256.Int{}, uint256.Int{}, err
	}
	if !found {
		basis = position.SizeBase
	}
	lot := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals)-int64(market.SzDecimals)), nil)
	slice := new(big.Int).Mul(basis.ToBig(), new(big.Int).SetUint64(params.LiquidationSlicePpm))
	slice.Quo(slice, big.NewInt(types.PpmDenominator))
	slice.Sub(slice, new(big.Int).Rem(slice, lot))
	if slice.Sign() == 0 {
		slice.Set(lot)
	}
	if slice.Cmp(position.SizeBase.ToBig()) >= 0 {
		return true, position.SizeBase, basis, nil
	}
	out, _ := uint256.FromBig(slice)
	return true, *out, basis, nil
}

// liquidationCooling reports whether the position's last slice lies less than
// Params.LiquidationCooldownSeconds of block time before now.
func (k *Keeper) liquidationCooling(ctx sdk.Context, params types.Params, positionID uint256.Int, now uint64) (bool, error) {
	last, _, found, err := k.liquidationCooldownRecord(ctx, positionID)
	if err != nil || !found {
		return false, err
	}
	return now < last || now-last < params.LiquidationCooldownSeconds, nil
}

// liquidationCooldownRecord reads the prefix 0x0F record of the position: the
// block time of its last slice and its slice basis in base units.
func (k *Keeper) liquidationCooldownRecord(ctx sdk.Context, positionID uint256.Int) (uint64, uint256.Int, bool, error) {
	bz := k.store(ctx).Get(types.LiquidationCooldownKey(positionID))
	if bz == nil {
		return 0, uint256.Int{}, false, nil
	}
	if len(bz) != types.LiquidationCooldownValueLength {
		return 0, uint256.Int{}, false, fmt.Errorf("%w: liquidation cooldown of position %s holds %d bytes",
			types.ErrInvalidParams, positionID.Dec(), len(bz))
	}
	var basis uint256.Int
	basis.SetBytes32(bz[8:])
	if basis.IsZero() {
		return 0, uint256.Int{}, false, fmt.Errorf("%w: liquidation cooldown of position %s holds a zero slice basis",
			types.ErrInvalidParams, positionID.Dec())
	}
	return binary.BigEndian.Uint64(bz[:8]), basis, true, nil
}

// liquidationRecordSlice keeps the slice time and slice basis of a large
// position that stays open after a slice and drops the cooldown record of a
// position that closed or was closed whole.
func (k *Keeper) liquidationRecordSlice(ctx sdk.Context, positionID uint256.Int, large bool, now uint64, basis uint256.Int) {
	store := k.store(ctx)
	cooldownKey := types.LiquidationCooldownKey(positionID)
	if store.Has(types.PositionKey(positionID)) && large {
		store.Set(cooldownKey, types.LiquidationCooldownValue(now, basis))
	} else {
		store.Delete(cooldownKey)
	}
}

// liquidationRebucket recomputes the account's equity and maintenance margin
// at mark, rewrites its MarginRatioIndex entry (none without a position) and
// reports whether equity is below maintenance.
func (k *Keeper) liquidationRebucket(ctx sdk.Context, address common.Address) (bool, error) {
	account, err := k.liquidationAccount(ctx, address)
	if err != nil {
		return false, err
	}
	store := k.store(ctx)
	store.Delete(types.MarginRatioIndexKey(account.MarginRatioBucket, address))
	if account.PositionCount == 0 {
		account.MarginRatioBucket = 0
		store.Set(types.AccountKey(address), account.Marshal())
		return false, nil
	}
	summary, err := k.AccountMargin(ctx, address)
	if err != nil {
		return false, err
	}
	equity, maintenance := summary.Equity, summary.MaintenanceMargin
	account.MarginRatioBucket = marginRatioBucket(equity.BigInt(), maintenance.BigInt())
	store.Set(types.AccountKey(address), account.Marshal())
	store.Set(types.MarginRatioIndexKey(account.MarginRatioBucket, address), []byte{})
	return equity.LT(maintenance), nil
}

// marginRatioBucket is min(65535, floor(100 x equity / maintenance margin)), 0
// for equity at or below 0 and 65535 without maintenance.
func marginRatioBucket(equity, maintenance *big.Int) uint16 {
	if equity.Sign() <= 0 {
		return 0
	}
	if maintenance.Sign() <= 0 {
		return marginRatioBucketMax
	}
	ratio := new(big.Int).Mul(equity, big.NewInt(marginRatioScale))
	ratio.Quo(ratio, maintenance)
	if !ratio.IsUint64() || ratio.Uint64() > marginRatioBucketMax {
		return marginRatioBucketMax
	}
	return uint16(ratio.Uint64())
}

// liquidationCandidates reads at most limit accounts of the MarginRatioIndex
// in key order, bucket 0 first.
func (k *Keeper) liquidationCandidates(ctx sdk.Context, limit uint64) ([]common.Address, error) {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.MarginRatioIndexPrefix)
	defer iterator.Close()
	keyLength := len(types.MarginRatioIndexPrefix) + 2 + common.AddressLength
	var accounts []common.Address
	for ; iterator.Valid() && uint64(len(accounts)) < limit; iterator.Next() {
		key := iterator.Key()
		if len(key) != keyLength {
			return nil, fmt.Errorf("%w: margin ratio key %x", types.ErrInvalidParams, key)
		}
		accounts = append(accounts, common.BytesToAddress(key[len(types.MarginRatioIndexPrefix)+2:]))
	}
	return accounts, nil
}

// liquidationFresh reports whether every market of the positions has a fresh
// price; any error other than ErrPriceStale is returned.
func (k *Keeper) liquidationFresh(ctx sdk.Context, positions []types.Position) (bool, error) {
	for _, position := range positions {
		err := k.RequireFresh(ctx, position.MarketID)
		if errors.Is(err, types.ErrPriceStale) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// liquidationOpenOrders lists the ids of the account's open orders from its
// AccountOrder index.
func (k *Keeper) liquidationOpenOrders(ctx sdk.Context, account common.Address) ([]uint256.Int, error) {
	prefix := types.AccountOrderPrefixFor(account)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	var ids []uint256.Int
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+32 {
			return nil, fmt.Errorf("%w: account order key %x", types.ErrInvalidParams, key)
		}
		var id uint256.Int
		id.SetBytes32(key[len(prefix):])
		ids = append(ids, id)
	}
	return ids, nil
}

// liquidationAccountPositions lists the account's positions in ascending
// market id from its AccountMarketPosition index.
func (k *Keeper) liquidationAccountPositions(ctx sdk.Context, account common.Address) ([]types.Position, error) {
	prefix := append(append([]byte(nil), types.AccountMarketPositionPrefix...), account.Bytes()...)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	var ids []uint256.Int
	for ; iterator.Valid(); iterator.Next() {
		value := iterator.Value()
		if len(iterator.Key()) != len(prefix)+8 || len(value) != 32 {
			return nil, fmt.Errorf("%w: account market position entry %x", types.ErrInvalidParams, iterator.Key())
		}
		var id uint256.Int
		id.SetBytes32(value)
		ids = append(ids, id)
	}
	positions := make([]types.Position, 0, len(ids))
	for _, id := range ids {
		position, err := k.liquidationPosition(ctx, id)
		if err != nil {
			return nil, err
		}
		positions = append(positions, position)
	}
	return positions, nil
}

func (k *Keeper) liquidationPosition(ctx sdk.Context, positionID uint256.Int) (types.Position, error) {
	bz := k.store(ctx).Get(types.PositionKey(positionID))
	if bz == nil {
		return types.Position{}, fmt.Errorf("%w: %s", types.ErrUnknownPosition, positionID.Dec())
	}
	var position types.Position
	if err := position.Unmarshal(bz); err != nil {
		return types.Position{}, fmt.Errorf("%w: position %s: %v", types.ErrInvalidParams, positionID.Dec(), err)
	}
	return position, nil
}

func (k *Keeper) liquidationAccount(ctx sdk.Context, address common.Address) (types.Account, error) {
	bz := k.store(ctx).Get(types.AccountKey(address))
	if bz == nil {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, address)
	}
	var account types.Account
	if err := account.Unmarshal(bz); err != nil {
		return types.Account{}, fmt.Errorf("%w: account %s: %v", types.ErrInvalidParams, address, err)
	}
	return account, nil
}

// liquidationMark is the stored mark of the market, ErrPriceStale before any.
func (k *Keeper) liquidationMark(ctx sdk.Context, marketID uint64) (uint256.Int, error) {
	bz := k.store(ctx).Get(types.MarketStateKey(marketID))
	if bz == nil {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no price", types.ErrPriceStale, marketID)
	}
	var state types.MarketState
	if err := state.Unmarshal(bz); err != nil {
		return uint256.Int{}, fmt.Errorf("%w: market state %d: %v", types.ErrInvalidParams, marketID, err)
	}
	if state.MarkPrice.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, marketID)
	}
	return state.MarkPrice, nil
}

// liquidationNotional is size x mark / 1e18 rounded down.
func liquidationNotional(size, mark uint256.Int) *big.Int {
	notional := new(big.Int).Mul(size.ToBig(), mark.ToBig())
	return notional.Quo(notional, usdUnit)
}

// liquidationTierBps is the maintenance rate of the market's tier holding the
// notional.
func liquidationTierBps(market types.Market, notional *big.Int) (uint64, error) {
	tiers, err := types.TiersFor(market)
	if err != nil {
		return 0, err
	}
	bps := tiers[0].MaintenanceBps
	for _, tier := range tiers[1:] {
		if notional.Cmp(tier.LowerBoundUsd.BigInt()) < 0 {
			break
		}
		bps = tier.MaintenanceBps
	}
	return bps, nil
}

func liquidationNow(ctx sdk.Context) (uint64, error) {
	seconds := ctx.BlockTime().Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("%w: block time %d before the epoch", types.ErrInvalidParams, seconds)
	}
	return uint64(seconds), nil
}
