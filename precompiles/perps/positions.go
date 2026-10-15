package perps

import (
	"fmt"
	"math/big"

	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

var (
	positionWad      = big.NewInt(1_000_000_000_000_000_000)
	positionUsdlUnit = big.NewInt(1_000_000_000_000)
	positionPpm      = big.NewInt(int64(types.PpmDenominator))
)

func init() {
	handlers["openPosition"] = handleOpenPosition
	handlers["addSize"] = handleAddSize
	handlers["addCollateral"] = handleAddCollateral
	handlers["removeCollateral"] = handleRemoveCollateral
	handlers["closePosition"] = handleClosePosition
	handlers["partialClose"] = handlePartialClose
	handlers["closePositionWithParams"] = handleClosePositionWithParams
	handlers["liquidate"] = handleLiquidate
}

// handleOpenPosition serves openPosition(marketId, collateralToken,
// collateralAmount, leverage, isLong): the deposit leg pulls the collateral,
// then a MARKET IOC order sized by the compatibility rule of contracts/perps.md
// section 8 opens the position, whose id is returned.
func handleOpenPosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 5); err != nil {
		return nil, err
	}
	marketID, err := positionMarketArg(args[0])
	if err != nil {
		return nil, err
	}
	token, ok := args[1].(common.Address)
	if !ok {
		return nil, positionArgError(method, 1)
	}
	amount, err := positionWordArg(method, args, 2)
	if err != nil {
		return nil, err
	}
	leverage, err := positionWordArg(method, args, 3)
	if err != nil {
		return nil, err
	}
	isLong, ok := args[4].(bool)
	if !ok {
		return nil, positionArgError(method, 4)
	}
	result, err := p.compatibilityOpen(ctx, evm, caller, token, marketID, isLong, amount, leverage)
	if err != nil {
		return nil, err
	}
	fills := result.Fills
	if len(fills) == 0 {
		return nil, fmt.Errorf("%w: market %d", types.ErrNoLiquidity, marketID)
	}
	return method.Outputs.Pack(fills[len(fills)-1].TakerPositionID.ToBig())
}

// handleAddSize serves addSize(positionId, additionalCollateral, leverage):
// the openPosition sizing applied to additionalCollateral in the direction of
// the caller's position.
func handleAddSize(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 3); err != nil {
		return nil, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return nil, err
	}
	amount, err := positionWordArg(method, args, 1)
	if err != nil {
		return nil, err
	}
	leverage, err := positionWordArg(method, args, 2)
	if err != nil {
		return nil, err
	}
	closing, err := p.keeper.ClosePosition(ctx, caller, positionID)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := p.compatibilityOpen(ctx, evm, caller, params.CollateralToken, closing.MarketID, !closing.IsLong,
		amount, leverage); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handleAddCollateral serves addCollateral(positionId, amount): the deposit
// leg moves amount from the caller's wallet into its cross account; the
// position's size and entry are unchanged.
func handleAddCollateral(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	positionID, amount, err := p.collateralArgs(ctx, method, caller, args)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.depositLeg(ctx, evm, caller, params.CollateralToken, amount); err != nil {
		return nil, err
	}
	records, err := p.keeper.Deposit(ctx, caller, amount)
	if err != nil {
		return nil, fmt.Errorf("position %s: %w", positionID.Dec(), err)
	}
	if err := p.emitPositionRecords(evm, records); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handleRemoveCollateral serves removeCollateral(positionId, amount): amount
// is debited within Withdrawable, ErrInsufficientMargin above it, and paid to
// the caller by the withdrawal leg.
func handleRemoveCollateral(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	positionID, amount, err := p.collateralArgs(ctx, method, caller, args)
	if err != nil {
		return nil, err
	}
	records, err := p.keeper.Withdraw(ctx, caller, amount)
	if err != nil {
		return nil, fmt.Errorf("position %s: %w", positionID.Dec(), err)
	}
	if err := p.withdrawalLeg(ctx, evm, caller, amount); err != nil {
		return nil, err
	}
	if err := p.emitPositionRecords(evm, records); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handleClosePosition serves closePosition(positionId): a reduce-only MARKET
// IOC order for the whole position at the default slippage bound.
func handleClosePosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 1); err != nil {
		return nil, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return nil, err
	}
	closing, err := p.keeper.ClosePosition(ctx, caller, positionID)
	if err != nil {
		return nil, err
	}
	if err := p.placeClose(ctx, evm, caller, closing, uint256.Int{}, 0); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handlePartialClose serves partialClose(positionId, closeSizeUsd): a
// reduce-only MARKET IOC order for closeSizeUsd at mark rounded down to the
// lot, at the default slippage bound.
func handlePartialClose(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 2); err != nil {
		return nil, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return nil, err
	}
	closeSizeUsd, err := positionWordArg(method, args, 1)
	if err != nil {
		return nil, err
	}
	closing, err := p.keeper.PartialClose(ctx, caller, positionID, closeSizeUsd)
	if err != nil {
		return nil, err
	}
	if err := p.placeClose(ctx, evm, caller, closing, uint256.Int{}, 0); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handleClosePositionWithParams serves closePositionWithParams(positionId,
// closeSizeUsd, acceptablePrice, deadline): closeSizeUsd 0 closes the whole
// position; the slippage bound and the deadline are the caller's.
func handleClosePositionWithParams(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 4); err != nil {
		return nil, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return nil, err
	}
	closeSizeUsd, err := positionWordArg(method, args, 1)
	if err != nil {
		return nil, err
	}
	acceptablePrice, err := positionWordArg(method, args, 2)
	if err != nil {
		return nil, err
	}
	deadline, ok := args[3].(uint64)
	if !ok {
		return nil, positionArgError(method, 3)
	}
	var closing types.Order
	if closeSizeUsd.IsZero() {
		closing, err = p.keeper.ClosePosition(ctx, caller, positionID)
	} else {
		closing, err = p.keeper.PartialClose(ctx, caller, positionID, closeSizeUsd)
	}
	if err != nil {
		return nil, err
	}
	if err := p.placeClose(ctx, evm, caller, closing, acceptablePrice, deadline); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// handleLiquidate serves liquidate(positionId): permissionless liquidation of
// the account holding the position, the caller recorded as keeper and paid
// nothing.
func handleLiquidate(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := positionArgCount(method, args, 1); err != nil {
		return nil, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return nil, err
	}
	records, err := p.keeper.Liquidate(ctx, caller, positionID)
	if err != nil {
		return nil, err
	}
	if err := p.emitPositionRecords(evm, records); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// compatibilityOpen runs the deposit leg for amount, books it, and places the
// MARKET IOC order of the compatibility opens: notional
// N = c x L / (1 + L x f) rounded down, base N / P rounded down to the lot,
// where P is the highest price the order can fill at, the default bound for a
// buy and the best bid for a sell.
func (p PrecompileExecutor) compatibilityOpen(ctx sdk.Context, evm *vm.EVM, caller, token common.Address,
	marketID uint64, isLong bool, amount, leverage uint256.Int) (perpskeeper.PlaceResult, error) {
	if amount.IsZero() {
		return perpskeeper.PlaceResult{}, fmt.Errorf("%w: collateral", types.ErrZeroAmount)
	}
	if err := p.depositLeg(ctx, evm, caller, token, amount); err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	records, err := p.keeper.Deposit(ctx, caller, amount)
	if err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	if err := p.emitPositionRecords(evm, records); err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	sizeUsd, err := p.compatibilitySize(ctx, caller, marketID, isLong, amount, leverage)
	if err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	result, err := p.keeper.PlaceOrder(ctx, caller, perpskeeper.OrderParams{
		MarketID:    marketID,
		IsLong:      isLong,
		OrderType:   uint8(types.OrderTypeMarket),
		TimeInForce: uint8(types.TimeInForceIOC),
		SizeUsd:     sizeUsd,
		Leverage:    leverage,
	})
	if err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	if err := p.emitPositionRecords(evm, result.Events); err != nil {
		return perpskeeper.PlaceResult{}, err
	}
	return result, nil
}

// compatibilitySize returns the sizeUsd that PlaceOrder, sizing a MARKET
// order at mark, turns into the compatibility base size: base x mark / 1e18
// rounded up, so the conversion back at mark lands on that base before the
// lot rounding.
func (p PrecompileExecutor) compatibilitySize(ctx sdk.Context, caller common.Address, marketID uint64, isLong bool,
	amount, leverage uint256.Int) (uint256.Int, error) {
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return uint256.Int{}, err
	}
	market, err := p.keeper.GetMarket(ctx, marketID)
	if err != nil {
		return uint256.Int{}, err
	}
	if market.PxDecimals > types.USDDecimals || market.SzDecimals > types.USDDecimals {
		return uint256.Int{}, fmt.Errorf("%w: market %d decimals", types.ErrInvalidParams, marketID)
	}
	if params.DefaultSlippagePpm >= types.PpmDenominator {
		return uint256.Int{}, fmt.Errorf("%w: default slippage %d ppm", types.ErrInvalidParams, params.DefaultSlippagePpm)
	}
	tier, err := p.keeper.FeeTier(ctx, caller)
	if err != nil {
		return uint256.Int{}, err
	}
	collateralUsd := new(big.Int).Mul(amount.ToBig(), positionUsdlUnit)
	notional, err := perpskeeper.CompatibilityNotional(sdk.NewIntFromBigInt(collateralUsd), leverage, tier.TakerFeePpm)
	if err != nil {
		return uint256.Int{}, err
	}
	state, err := p.keeper.GetMarketState(ctx, marketID)
	if err != nil {
		return uint256.Int{}, err
	}
	if state.MarkPrice.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, marketID)
	}
	mark := state.MarkPrice.ToBig()
	var price *big.Int
	if isLong {
		tick := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals-market.PxDecimals)), nil)
		bound := new(big.Int).Mul(mark, new(big.Int).SetUint64(types.PpmDenominator+params.DefaultSlippagePpm))
		bound.Quo(bound, new(big.Int).Mul(positionPpm, tick))
		price = bound.Mul(bound, tick)
	} else {
		bid, ok, err := p.keeper.BestBid(ctx, marketID)
		if err != nil {
			return uint256.Int{}, err
		}
		if !ok {
			return uint256.Int{}, fmt.Errorf("%w: market %d has no bid", types.ErrNoLiquidity, marketID)
		}
		price = bid.ToBig()
	}
	if price.Sign() == 0 {
		return uint256.Int{}, fmt.Errorf("%w: market %d sizing price is zero", types.ErrPriceStale, marketID)
	}
	lot := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals-market.SzDecimals)), nil)
	base := new(big.Int).Quo(new(big.Int).Mul(notional.BigInt(), positionWad), price)
	base.Sub(base, new(big.Int).Mod(base, lot))
	if base.Sign() == 0 {
		return uint256.Int{}, fmt.Errorf("%w: %s USD at %s is below one lot", types.ErrLotSize, notional, price)
	}
	sizeUsd := new(big.Int).Mul(base, mark)
	sizeUsd.Add(sizeUsd, new(big.Int).Sub(positionWad, big.NewInt(1)))
	sizeUsd.Quo(sizeUsd, positionWad)
	word, overflow := uint256.FromBig(sizeUsd)
	if overflow {
		return uint256.Int{}, fmt.Errorf("%w: compatibility size of market %d", types.ErrOverflow, marketID)
	}
	return *word, nil
}

// placeClose places the reduce-only closing order the keeper built, with the
// caller's slippage bound (0 for the default) and deadline (0 for none).
func (p PrecompileExecutor) placeClose(ctx sdk.Context, evm *vm.EVM, caller common.Address, closing types.Order,
	acceptablePrice uint256.Int, deadline uint64) error {
	result, err := p.keeper.PlaceOrder(ctx, caller, perpskeeper.OrderParams{
		MarketID:        closing.MarketID,
		IsLong:          closing.IsLong,
		OrderType:       uint8(closing.Type),
		TimeInForce:     uint8(closing.TimeInForce),
		SizeUsd:         closing.SizeUsd,
		Leverage:        closing.Leverage,
		AcceptablePrice: acceptablePrice,
		ReduceOnly:      true,
		Deadline:        deadline,
	})
	if err != nil {
		return err
	}
	return p.emitPositionRecords(evm, result.Events)
}

// collateralArgs decodes (positionId, amount) of addCollateral and
// removeCollateral, refusing a zero amount and a position the caller does not
// hold.
func (p PrecompileExecutor) collateralArgs(ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}) (uint256.Int, uint256.Int, error) {
	if err := positionArgCount(method, args, 2); err != nil {
		return uint256.Int{}, uint256.Int{}, err
	}
	positionID, err := positionWordArg(method, args, 0)
	if err != nil {
		return uint256.Int{}, uint256.Int{}, err
	}
	amount, err := positionWordArg(method, args, 1)
	if err != nil {
		return uint256.Int{}, uint256.Int{}, err
	}
	if amount.IsZero() {
		return uint256.Int{}, uint256.Int{}, fmt.Errorf("%w: %s", types.ErrZeroAmount, method.Name)
	}
	if _, err := p.keeper.ClosePosition(ctx, caller, positionID); err != nil {
		return uint256.Int{}, uint256.Int{}, err
	}
	return positionID, amount, nil
}

// emitPositionRecords emits the records the keeper collected, in order, as
// logs of the precompile.
func (p PrecompileExecutor) emitPositionRecords(evm *vm.EVM, records []types.EventRecord) error {
	for _, record := range records {
		if err := logFromEvent(evm, p.address, record); err != nil {
			return err
		}
	}
	return nil
}

func positionArgCount(method *abi.Method, args []interface{}, want int) error {
	if len(args) != want {
		return fmt.Errorf("%w: %s takes %d arguments, got %d", types.ErrInvalidParams, method.Name, want, len(args))
	}
	return nil
}

func positionArgError(method *abi.Method, index int) error {
	return fmt.Errorf("%w: %s argument %d has the wrong type", types.ErrInvalidParams, method.Name, index)
}

func positionWordArg(method *abi.Method, args []interface{}, index int) (uint256.Int, error) {
	value, ok := args[index].(*big.Int)
	if !ok || value == nil || value.Sign() < 0 {
		return uint256.Int{}, positionArgError(method, index)
	}
	word, overflow := uint256.FromBig(value)
	if overflow {
		return uint256.Int{}, fmt.Errorf("%w: %s argument %d", types.ErrOverflow, method.Name, index)
	}
	return *word, nil
}

// positionMarketArg is the uint256 market id as the keeper's uint64; an id
// above 2^64-1 names no market.
func positionMarketArg(arg interface{}) (uint64, error) {
	value, ok := arg.(*big.Int)
	if !ok || value == nil || value.Sign() < 0 || !value.IsUint64() {
		return 0, fmt.Errorf("%w: %v", types.ErrUnknownMarket, arg)
	}
	return value.Uint64(), nil
}
