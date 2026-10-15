package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// MarginModeCross is MarginMode.Cross of the application, the only margin
// mode of the venue.
const MarginModeCross uint8 = 0

// maxMarginRatioBucket is the highest MarginRatioIndex bucket.
const maxMarginRatioBucket = 65535

var (
	wad          = big.NewInt(1_000_000_000_000_000_000)
	usdlUnitUsd  = big.NewInt(1_000_000_000_000)
	ppmScale     = big.NewInt(types.PpmDenominator)
	bpsScale     = big.NewInt(types.BpsDenominator)
	wadPpm       = new(big.Int).Mul(wad, ppmScale)
	bucketsScale = big.NewInt(100)
)

// MarginSummary is the cross-margin state of one account at mark, in
// 18-decimal USD: equity with unrealised PnL and unsettled funding, the
// initial margin of its positions and resting orders and the maintenance
// margin of its positions.
type MarginSummary struct {
	Equity            sdk.Int
	InitialMargin     sdk.Int
	MaintenanceMargin sdk.Int
	Positions         uint64
}

// Liquidatable reports an account with a position whose equity is below its
// maintenance margin.
func (m MarginSummary) Liquidatable() bool {
	return m.Positions > 0 && m.Equity.LT(m.MaintenanceMargin)
}

// RatioBucket is min(65535, floor(100 x equity / maintenance margin)), 0 for
// equity at or below 0 and 65535 for positive equity without maintenance.
func (m MarginSummary) RatioBucket() uint16 {
	if !m.Equity.IsPositive() {
		return 0
	}
	if !m.MaintenanceMargin.IsPositive() {
		return maxMarginRatioBucket
	}
	ratio := new(big.Int).Mul(m.Equity.BigInt(), bucketsScale)
	ratio.Quo(ratio, m.MaintenanceMargin.BigInt())
	if !ratio.IsUint64() || ratio.Uint64() > maxMarginRatioBucket {
		return maxMarginRatioBucket
	}
	return uint16(ratio.Uint64())
}

// OpenRequest is an order or position increase checked by CheckOpen:
// NotionalUsd is the order notional at its limit price or slippage bound,
// Leverage the requested 1e18-scaled leverage and TakerFeePpm the account's
// taker rate, which reserves the fee whether the order fills as taker or
// maker.
type OpenRequest struct {
	Account     common.Address
	MarketID    uint64
	IsLong      bool
	NotionalUsd sdk.Int
	Leverage    uint256.Int
	TakerFeePpm uint64
}

// InitialMargin is notional / leverage for a 1e18-scaled leverage of at
// least 1x, rounded down to 1e-18 USD, so the compatibility sizing of
// CompatibilityNotional always passes CheckOpen.
func InitialMargin(notionalUsd sdk.Int, leverage uint256.Int) (sdk.Int, error) {
	notional, err := nonNegativeUsd(notionalUsd, "notional")
	if err != nil {
		return sdk.Int{}, err
	}
	im, err := initialMargin(notional, leverage)
	if err != nil {
		return sdk.Int{}, err
	}
	return usdInt(im)
}

// CompatibilityNotional is the notional N = c x L / (1 + L x f) of the
// compatibility opens for collateral c in USD, 1e18-scaled leverage L and
// taker rate f, rounded down to 1e-18 USD, so N / L + N x f <= c.
func CompatibilityNotional(collateralUsd sdk.Int, leverage uint256.Int, takerFeePpm uint64) (sdk.Int, error) {
	collateral, err := nonNegativeUsd(collateralUsd, "collateral")
	if err != nil {
		return sdk.Int{}, err
	}
	if err := checkLeverageFloor(leverage); err != nil {
		return sdk.Int{}, err
	}
	if err := checkTakerRate(takerFeePpm); err != nil {
		return sdk.Int{}, err
	}
	lev := leverage.ToBig()
	numerator := new(big.Int).Mul(collateral, lev)
	numerator.Mul(numerator, ppmScale)
	denominator := new(big.Int).Mul(lev, new(big.Int).SetUint64(takerFeePpm))
	denominator.Add(denominator, wadPpm)
	return usdInt(numerator.Quo(numerator, denominator))
}

// MaintenanceMargin is the maintenance margin of a position at mark:
// notional x MMR(tier) - deduction(tier) for the tier of TiersFor that
// contains the notional, rounded up to 1e-18 USD. It replaces the single-rate
// check of lx_perps_maintenance_check.
func (k *Keeper) MaintenanceMargin(ctx sdk.Context, position types.Position) (sdk.Int, error) {
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return sdk.Int{}, err
	}
	state, err := k.GetMarketState(ctx, position.MarketID)
	if err != nil {
		return sdk.Int{}, err
	}
	mark, err := markOf(state)
	if err != nil {
		return sdk.Int{}, err
	}
	mm, err := maintenanceAt(market, positionNotional(position, mark))
	if err != nil {
		return sdk.Int{}, err
	}
	return usdInt(mm)
}

// Equity is the account's collateral x 1e12 less its deficit plus the
// unrealised PnL and the unsettled funding of its positions at mark.
func (k *Keeper) Equity(ctx sdk.Context, account common.Address) (sdk.Int, error) {
	summary, err := k.AccountMargin(ctx, account)
	if err != nil {
		return sdk.Int{}, err
	}
	return summary.Equity, nil
}

// AccountMargin computes the account's margin summary at mark without
// writing state.
func (k *Keeper) AccountMargin(ctx sdk.Context, account common.Address) (MarginSummary, error) {
	stored, err := k.loadAccount(ctx, account)
	if err != nil {
		return MarginSummary{}, err
	}
	return k.accountMargin(ctx, stored)
}

// RefreshMarginRatio recomputes the account's margin at mark and rewrites its
// MarginRatioIndex entry and stored bucket: one entry while the account holds
// a position, none otherwise.
func (k *Keeper) RefreshMarginRatio(ctx sdk.Context, account common.Address) (MarginSummary, error) {
	stored, err := k.loadAccount(ctx, account)
	if err != nil {
		return MarginSummary{}, err
	}
	summary, err := k.accountMargin(ctx, stored)
	if err != nil {
		return MarginSummary{}, err
	}
	store := k.store(ctx)
	store.Delete(types.MarginRatioIndexKey(stored.MarginRatioBucket, account))
	stored.MarginRatioBucket = 0
	if summary.Positions > 0 {
		stored.MarginRatioBucket = summary.RatioBucket()
		store.Set(types.MarginRatioIndexKey(stored.MarginRatioBucket, account), []byte{})
	}
	store.Set(types.AccountKey(account), stored.Marshal())
	return summary, nil
}

// IsLiquidatable recomputes the account at mark, re-buckets it in the
// MarginRatioIndex and reports whether its equity is below its maintenance
// margin while it holds a position.
func (k *Keeper) IsLiquidatable(ctx sdk.Context, account common.Address) (bool, error) {
	summary, err := k.RefreshMarginRatio(ctx, account)
	if err != nil {
		return false, err
	}
	return summary.Liquidatable(), nil
}

// CheckOpen admits an order or position increase: leverage above the cap of
// the tier that contains the resulting notional of the account's position in
// the market fails with ErrLeverageTooHigh, and equity less the taker fee
// reserve on the order notional below the initial margin of every position
// and resting order after the change fails with ErrInsufficientMargin.
func (k *Keeper) CheckOpen(ctx sdk.Context, req OpenRequest) error {
	notional, err := nonNegativeUsd(req.NotionalUsd, "order notional")
	if err != nil {
		return err
	}
	if notional.Sign() == 0 {
		return fmt.Errorf("%w: order notional", types.ErrZeroAmount)
	}
	if err := checkLeverageFloor(req.Leverage); err != nil {
		return err
	}
	if err := checkTakerRate(req.TakerFeePpm); err != nil {
		return err
	}
	stored, err := k.loadAccount(ctx, req.Account)
	if err != nil {
		return err
	}
	market, err := k.GetMarket(ctx, req.MarketID)
	if err != nil {
		return err
	}
	tiers, err := types.TiersFor(market)
	if err != nil {
		return err
	}
	resulting, err := k.resultingNotional(ctx, req, notional)
	if err != nil {
		return err
	}
	tier := tierFor(tiers, resulting)
	limit := new(big.Int).Mul(new(big.Int).SetUint64(tier.MaxLeverage), wad)
	if req.Leverage.ToBig().Cmp(limit) > 0 {
		return fmt.Errorf("%w: leverage %s above %dx for notional %s in market %d",
			types.ErrLeverageTooHigh, req.Leverage.Dec(), tier.MaxLeverage, resulting, req.MarketID)
	}
	summary, err := k.accountMargin(ctx, stored)
	if err != nil {
		return err
	}
	orderMargin, err := initialMargin(notional, req.Leverage)
	if err != nil {
		return err
	}
	reserve := ceilDiv(new(big.Int).Mul(notional, new(big.Int).SetUint64(req.TakerFeePpm)), ppmScale)
	free := new(big.Int).Sub(summary.Equity.BigInt(), reserve)
	required := new(big.Int).Add(summary.InitialMargin.BigInt(), orderMargin)
	if free.Cmp(required) < 0 {
		return fmt.Errorf("%w: equity %s less fee reserve %s below initial margin %s",
			types.ErrInsufficientMargin, summary.Equity, reserve, required)
	}
	return nil
}

// SetMarginMode accepts MarginModeCross for an existing account and changes
// nothing; every other mode fails with ErrCrossMarginOnly.
func (k *Keeper) SetMarginMode(ctx sdk.Context, account common.Address, mode uint8) error {
	if _, err := k.loadAccount(ctx, account); err != nil {
		return err
	}
	if mode != MarginModeCross {
		return fmt.Errorf("%w: margin mode %d", types.ErrCrossMarginOnly, mode)
	}
	return nil
}

// resultingNotional is |position notional at mark + order notional| with
// signs by side, for the account's position in the requested market.
func (k *Keeper) resultingNotional(ctx sdk.Context, req OpenRequest, notional *big.Int) (*big.Int, error) {
	order := new(big.Int).Set(notional)
	if !req.IsLong {
		order.Neg(order)
	}
	position, found, err := k.marketPosition(ctx, req.Account, req.MarketID)
	if err != nil {
		return nil, err
	}
	if !found {
		return order.Abs(order), nil
	}
	state, err := k.GetMarketState(ctx, req.MarketID)
	if err != nil {
		return nil, err
	}
	mark, err := markOf(state)
	if err != nil {
		return nil, err
	}
	held := positionNotional(position, mark)
	if !position.IsLong {
		held.Neg(held)
	}
	return order.Abs(order.Add(order, held)), nil
}

func (k *Keeper) accountMargin(ctx sdk.Context, stored types.Account) (MarginSummary, error) {
	equity := new(big.Int).Mul(stored.Collateral.ToBig(), usdlUnitUsd)
	equity.Sub(equity, stored.DeficitUsd.ToBig())
	initial := new(big.Int)
	maintenance := new(big.Int)
	var positions uint64
	err := k.iterateAccountPositions(ctx, stored.Address, func(position types.Position) error {
		market, err := k.GetMarket(ctx, position.MarketID)
		if err != nil {
			return err
		}
		state, err := k.GetMarketState(ctx, position.MarketID)
		if err != nil {
			return err
		}
		mark, err := markOf(state)
		if err != nil {
			return err
		}
		notional := positionNotional(position, mark)
		im, err := initialMargin(notional, position.Leverage)
		if err != nil {
			return err
		}
		mm, err := maintenanceAt(market, notional)
		if err != nil {
			return err
		}
		equity.Add(equity, unrealisedPnl(position, mark))
		equity.Add(equity, unsettledFunding(position, state))
		initial.Add(initial, im)
		maintenance.Add(maintenance, mm)
		positions++
		return nil
	})
	if err != nil {
		return MarginSummary{}, err
	}
	if err := k.iterateRestingOrders(ctx, stored.Address, func(order types.Order) error {
		if order.FilledBase.Gt(&order.SizeBase) {
			return fmt.Errorf("%w: order %s filled above its size", types.ErrInvalidParams, order.ID.Dec())
		}
		remaining := new(big.Int).Sub(order.SizeBase.ToBig(), order.FilledBase.ToBig())
		notional := remaining.Mul(remaining, order.LimitPrice.ToBig())
		im, err := initialMargin(notional.Quo(notional, wad), order.Leverage)
		if err != nil {
			return err
		}
		initial.Add(initial, im)
		return nil
	}); err != nil {
		return MarginSummary{}, err
	}
	summary := MarginSummary{Positions: positions}
	if summary.Equity, err = usdInt(equity); err != nil {
		return MarginSummary{}, err
	}
	if summary.InitialMargin, err = usdInt(initial); err != nil {
		return MarginSummary{}, err
	}
	if summary.MaintenanceMargin, err = usdInt(maintenance); err != nil {
		return MarginSummary{}, err
	}
	return summary, nil
}

// loadAccount returns the stored account, ErrNoAccount when none was created.
func (k *Keeper) loadAccount(ctx sdk.Context, account common.Address) (types.Account, error) {
	bz := k.store(ctx).Get(types.AccountKey(account))
	if bz == nil {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, account.Hex())
	}
	var stored types.Account
	if err := stored.Unmarshal(bz); err != nil {
		return types.Account{}, fmt.Errorf("%w: corrupt account %s: %v", types.ErrInvalidParams, account.Hex(), err)
	}
	if stored.Address != account {
		return types.Account{}, fmt.Errorf("%w: account %s holds %s", types.ErrInvalidParams, account.Hex(), stored.Address.Hex())
	}
	if !stored.Created {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, account.Hex())
	}
	return stored, nil
}

func accountPositionsPrefix(account common.Address) []byte {
	return append(append([]byte(nil), types.AccountMarketPositionPrefix...), account.Bytes()...)
}

func (k *Keeper) loadPosition(ctx sdk.Context, account common.Address, marketID uint64, idBytes []byte) (types.Position, error) {
	if len(idBytes) != 32 {
		return types.Position{}, fmt.Errorf("%w: position index of %s in market %d holds %d bytes",
			types.ErrInvalidParams, account.Hex(), marketID, len(idBytes))
	}
	var id uint256.Int
	id.SetBytes32(idBytes)
	bz := k.store(ctx).Get(types.PositionKey(id))
	if bz == nil {
		return types.Position{}, fmt.Errorf("%w: indexed position %s missing", types.ErrInvalidParams, id.Dec())
	}
	var position types.Position
	if err := position.Unmarshal(bz); err != nil {
		return types.Position{}, fmt.Errorf("%w: corrupt position %s: %v", types.ErrInvalidParams, id.Dec(), err)
	}
	if position.ID != id || position.Account != account || position.MarketID != marketID {
		return types.Position{}, fmt.Errorf("%w: position %s does not match its index", types.ErrInvalidParams, id.Dec())
	}
	return position, nil
}

func (k *Keeper) marketPosition(ctx sdk.Context, account common.Address, marketID uint64) (types.Position, bool, error) {
	bz := k.store(ctx).Get(types.AccountMarketPositionKey(account, marketID))
	if bz == nil {
		return types.Position{}, false, nil
	}
	position, err := k.loadPosition(ctx, account, marketID, bz)
	if err != nil {
		return types.Position{}, false, err
	}
	return position, true, nil
}

func (k *Keeper) iterateAccountPositions(ctx sdk.Context, account common.Address, visit func(types.Position) error) error {
	prefix := accountPositionsPrefix(account)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+8 {
			return fmt.Errorf("%w: account position key %x", types.ErrInvalidParams, key)
		}
		position, err := k.loadPosition(ctx, account, sdk.BigEndianToUint64(key[len(prefix):]), iterator.Value())
		if err != nil {
			return err
		}
		if err := visit(position); err != nil {
			return err
		}
	}
	return nil
}

func (k *Keeper) iterateRestingOrders(ctx sdk.Context, account common.Address, visit func(types.Order) error) error {
	prefix := types.AccountOrderPrefixFor(account)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+32 {
			return fmt.Errorf("%w: account order key %x", types.ErrInvalidParams, key)
		}
		var id uint256.Int
		id.SetBytes32(key[len(prefix):])
		bz := k.store(ctx).Get(types.OrderKey(id))
		if bz == nil {
			return fmt.Errorf("%w: indexed order %s missing", types.ErrInvalidParams, id.Dec())
		}
		var order types.Order
		if err := order.Unmarshal(bz); err != nil {
			return fmt.Errorf("%w: corrupt order %s: %v", types.ErrInvalidParams, id.Dec(), err)
		}
		if order.ID != id || order.Account != account {
			return fmt.Errorf("%w: order %s does not match its index", types.ErrInvalidParams, id.Dec())
		}
		if order.Status != types.OrderStatusResting {
			continue
		}
		if err := visit(order); err != nil {
			return err
		}
	}
	return nil
}

// markOf is the stored mark of a market, ErrPriceStale before its first mark.
func markOf(state types.MarketState) (*big.Int, error) {
	if state.MarkPrice.IsZero() {
		return nil, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, state.MarketID)
	}
	return state.MarkPrice.ToBig(), nil
}

// positionNotional is size x mark / 1e18, rounded down.
func positionNotional(position types.Position, mark *big.Int) *big.Int {
	notional := new(big.Int).Mul(position.SizeBase.ToBig(), mark)
	return notional.Quo(notional, wad)
}

// unrealisedPnl is side x (size x mark / 1e18 - cost basis), rounded down.
func unrealisedPnl(position types.Position, mark *big.Int) *big.Int {
	pnl := new(big.Int).Mul(position.SizeBase.ToBig(), mark)
	pnl.Sub(pnl, new(big.Int).Mul(position.CostBasisUsd.ToBig(), wad))
	if !position.IsLong {
		pnl.Neg(pnl)
	}
	return pnl.Div(pnl, wad)
}

// unsettledFunding is the funding the position received since its last
// settlement, -side x (size x index / 1e18 - funding basis), negative when it
// pays; floor division rounds a payment up and a receipt down, toward the
// venue, as lx_perps_funding_owed does.
func unsettledFunding(position types.Position, state types.MarketState) *big.Int {
	accrued := new(big.Int).Mul(position.SizeBase.ToBig(), signedBig(state.FundingIndex))
	accrued.Sub(accrued, new(big.Int).Mul(signedBig(position.FundingBasis), wad))
	if position.IsLong {
		accrued.Neg(accrued)
	}
	return accrued.Div(accrued, wad)
}

func tierFor(tiers []types.MarginTier, notional *big.Int) types.MarginTier {
	tier := tiers[0]
	for _, row := range tiers[1:] {
		if row.LowerBoundUsd.BigInt().Cmp(notional) > 0 {
			break
		}
		tier = row
	}
	return tier
}

// maintenanceAt is notional x MMR(tier) - deduction(tier), the product
// rounded up and the result floored at 0.
func maintenanceAt(market types.Market, notional *big.Int) (*big.Int, error) {
	tiers, err := types.TiersFor(market)
	if err != nil {
		return nil, err
	}
	tier := tierFor(tiers, notional)
	mm := ceilDiv(new(big.Int).Mul(notional, new(big.Int).SetUint64(tier.MaintenanceBps)), bpsScale)
	mm.Sub(mm, tier.DeductionUsd.BigInt())
	if mm.Sign() < 0 {
		mm.SetInt64(0)
	}
	return mm, nil
}

func initialMargin(notional *big.Int, leverage uint256.Int) (*big.Int, error) {
	if err := checkLeverageFloor(leverage); err != nil {
		return nil, err
	}
	im := new(big.Int).Mul(notional, wad)
	return im.Quo(im, leverage.ToBig()), nil
}

func checkLeverageFloor(leverage uint256.Int) error {
	if leverage.ToBig().Cmp(wad) < 0 {
		return fmt.Errorf("%w: leverage %s below 1x", types.ErrInvalidParams, leverage.Dec())
	}
	return nil
}

func checkTakerRate(takerFeePpm uint64) error {
	if takerFeePpm >= types.PpmDenominator {
		return fmt.Errorf("%w: taker rate %d ppm", types.ErrInvalidParams, takerFeePpm)
	}
	return nil
}

func ceilDiv(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func nonNegativeUsd(amount sdk.Int, name string) (*big.Int, error) {
	if amount.IsNil() || amount.IsNegative() {
		return nil, fmt.Errorf("%w: %s must be set and non-negative", types.ErrInvalidParams, name)
	}
	return amount.BigInt(), nil
}

// usdInt converts a USD amount to sdk.Int, refusing one beyond 256 bits.
func usdInt(value *big.Int) (sdk.Int, error) {
	if value.BitLen() > 256 {
		return sdk.Int{}, fmt.Errorf("%w: USD amount %s beyond 256 bits", types.ErrOverflow, value)
	}
	return sdk.NewIntFromBigInt(value), nil
}
