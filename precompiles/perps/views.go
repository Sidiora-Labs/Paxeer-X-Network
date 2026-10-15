package perps

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

const (
	viewSecondsPerHour = 3600
	viewFundingPeriods = 8
	viewFixedDecimals  = 18
	viewLiquidationFee = 0
)

var (
	viewWad        = new(big.Int).Exp(big.NewInt(10), big.NewInt(viewFixedDecimals), nil)
	viewPpmToFixed = big.NewInt(1_000_000_000_000)
	viewBps        = big.NewInt(types.BpsDenominator)
	viewMaxWord    = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

type viewAccountState struct {
	Collateral           *big.Int
	EquityUsd            *big.Int
	InitialMarginUsd     *big.Int
	MaintenanceMarginUsd *big.Int
	Withdrawable         *big.Int
	FeeTier              uint8
	Volume14dUsd         *big.Int
	PositionCount        *big.Int
	OpenOrderCount       *big.Int
}

type viewPositionMargin struct {
	Token        common.Address
	LockedAmount *big.Int
	MarketId     *big.Int
	IsLong       bool
}

type viewMarginTier struct {
	LowerBoundUsd  *big.Int
	MaxLeverage    *big.Int
	MaintenanceBps *big.Int
	DeductionUsd   *big.Int
}

type viewOrderDetails struct {
	OrderId         *big.Int
	User            common.Address
	MarketId        *big.Int
	IsLong          bool
	OrderType       uint8
	TimeInForce     uint8
	Status          uint8
	ReduceOnly      bool
	LimitPrice      *big.Int
	TriggerPrice    *big.Int
	AcceptablePrice *big.Int
	SizeUsd         *big.Int
	SizeBase        *big.Int
	FilledBase      *big.Int
	Leverage        *big.Int
	Deadline        uint64
	Cloid           [16]byte
	Sequence        uint64
}

type viewPositionDetails struct {
	PositionId       *big.Int
	User             common.Address
	MarketId         *big.Int
	IsLong           bool
	SizeBase         *big.Int
	SizeUsdAtMark    *big.Int
	EntryPrice       *big.Int
	Leverage         *big.Int
	UnrealizedPnlUsd *big.Int
	FundingOwedUsd   *big.Int
	LiquidationPrice *big.Int
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	OpenedAt         *big.Int
}

type viewMarketConfig struct {
	MarketId             *big.Int
	Symbol               string
	Name                 string
	OracleSymbol         string
	SzDecimals           uint8
	PxDecimals           uint8
	TickSize             *big.Int
	LotSize              *big.Int
	MaxLeverage          *big.Int
	TierClass            uint8
	ImpactNotionalUsd    *big.Int
	OiCapUsd             *big.Int
	FundingCapPpmPerHour *big.Int
	Enabled              bool
	ListedAt             uint64
}

func init() {
	for name, serve := range map[string]handler{
		"getVault":              viewGetVault,
		"getAccount":            viewGetAccount,
		"getPositionMargin":     viewGetPositionMargin,
		"getMarginTiers":        viewGetMarginTiers,
		"checkLiquidatable":     viewCheckLiquidatable,
		"getOrder":              viewGetOrder,
		"getOrderDetails":       viewGetOrderDetails,
		"getUserOrderIds":       viewGetUserOrderIds,
		"getPosition":           viewGetPosition,
		"getPositionDetails":    viewGetPositionDetails,
		"getUserPositionIds":    viewGetUserPositionIds,
		"getUserMarketPosition": viewGetUserMarketPosition,
		"getMarket":             viewGetMarket,
		"getMarketConfig":       viewGetMarketConfig,
		"getActiveMarketIds":    viewGetActiveMarketIds,
		"totalMarkets":          viewTotalMarkets,
		"isMarketActive":        viewIsMarketActive,
		"getFees":               viewGetFees,
		"getFeeTier":            viewGetFeeTier,
		"getProtocolAccounts":   viewGetProtocolAccounts,
		"getMarkPrice":          viewGetMarkPrice,
		"getMarkPriceDetails":   viewGetMarkPriceDetails,
		"getCurrentFundingRate": viewGetCurrentFundingRate,
		"getFundingState":       viewGetFundingState,
		"getFundingDetails":     viewGetFundingDetails,
		"getPositionFunding":    viewGetPositionFunding,
		"getOpenInterest":       viewGetOpenInterest,
		"getLiquidationPrice":   viewGetLiquidationPrice,
		"getInsuranceBalance":   viewGetInsuranceBalance,
		"getADLRank":            viewGetADLRank,
	} {
		handlers[name] = serve
	}
}

func viewArg[T any](args []interface{}, i int) (T, error) {
	var zero T
	if i >= len(args) {
		return zero, fmt.Errorf("%w: argument %d missing", types.ErrInvalidParams, i)
	}
	value, ok := args[i].(T)
	if !ok {
		return zero, fmt.Errorf("%w: argument %d has type %T", types.ErrInvalidParams, i, args[i])
	}
	return value, nil
}

func viewMarketID(args []interface{}, i int) (uint64, error) {
	value, err := viewArg[*big.Int](args, i)
	if err != nil {
		return 0, err
	}
	if value == nil || !value.IsUint64() {
		return 0, fmt.Errorf("%w: %v", types.ErrUnknownMarket, value)
	}
	return value.Uint64(), nil
}

func viewWord(args []interface{}, i int) (uint256.Int, error) {
	value, err := viewArg[*big.Int](args, i)
	if err != nil {
		return uint256.Int{}, err
	}
	var word uint256.Int
	if value == nil || value.Sign() < 0 || word.SetFromBig(value) {
		return uint256.Int{}, fmt.Errorf("%w: argument %d outside uint256", types.ErrInvalidParams, i)
	}
	return word, nil
}

func viewSigned(word uint256.Int) *big.Int {
	if word.Sign() >= 0 {
		return word.ToBig()
	}
	var magnitude uint256.Int
	magnitude.Neg(&word)
	return new(big.Int).Neg(magnitude.ToBig())
}

func viewUint(value uint64) *big.Int { return new(big.Int).SetUint64(value) }

func viewMark(state types.MarketState) (*big.Int, error) {
	if state.MarkPrice.IsZero() {
		return nil, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, state.MarketID)
	}
	return state.MarkPrice.ToBig(), nil
}

func viewNotional(position types.Position, mark *big.Int) *big.Int {
	notional := new(big.Int).Mul(position.SizeBase.ToBig(), mark)
	return notional.Quo(notional, viewWad)
}

func viewMarket(p PrecompileExecutor, ctx sdk.Context, marketID uint64) (types.Market, types.MarketState, error) {
	market, err := p.keeper.GetMarket(ctx, marketID)
	if err != nil {
		return types.Market{}, types.MarketState{}, err
	}
	state, err := p.keeper.GetMarketState(ctx, marketID)
	if err != nil {
		return types.Market{}, types.MarketState{}, err
	}
	return market, state, nil
}

func viewMarketArg(p PrecompileExecutor, ctx sdk.Context, args []interface{}) (types.Market, types.MarketState, error) {
	marketID, err := viewMarketID(args, 0)
	if err != nil {
		return types.Market{}, types.MarketState{}, err
	}
	return viewMarket(p, ctx, marketID)
}

func viewPosition(p PrecompileExecutor, ctx sdk.Context, args []interface{}) (types.Position, types.MarketState, *big.Int, error) {
	positionID, err := viewWord(args, 0)
	if err != nil {
		return types.Position{}, types.MarketState{}, nil, err
	}
	position, err := p.keeper.GetPosition(ctx, positionID)
	if err != nil {
		return types.Position{}, types.MarketState{}, nil, err
	}
	_, state, err := viewMarket(p, ctx, position.MarketID)
	if err != nil {
		return types.Position{}, types.MarketState{}, nil, err
	}
	mark, err := viewMark(state)
	if err != nil {
		return types.Position{}, types.MarketState{}, nil, err
	}
	return position, state, mark, nil
}

func viewGetVault(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	user, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	_, err = p.keeper.GetAccount(ctx, user)
	switch {
	case errors.Is(err, types.ErrNoAccount):
		return method.Outputs.Pack(common.Address{})
	case err != nil:
		return nil, err
	}
	return method.Outputs.Pack(p.address)
}

func viewGetAccount(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	address, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	tier, err := p.keeper.FeeTier(ctx, address)
	if err != nil {
		return nil, err
	}
	state := viewAccountState{
		Collateral: new(big.Int), EquityUsd: new(big.Int), InitialMarginUsd: new(big.Int),
		MaintenanceMarginUsd: new(big.Int), Withdrawable: new(big.Int), FeeTier: tier.Tier,
		Volume14dUsd: tier.Volume14dUsd.ToBig(), PositionCount: new(big.Int), OpenOrderCount: new(big.Int),
	}
	account, err := p.keeper.GetAccount(ctx, address)
	switch {
	case errors.Is(err, types.ErrNoAccount):
		return method.Outputs.Pack(state)
	case err != nil:
		return nil, err
	}
	margin, err := p.keeper.AccountMargin(ctx, address)
	if err != nil {
		return nil, err
	}
	withdrawable, err := p.keeper.Withdrawable(ctx, address)
	if err != nil {
		return nil, err
	}
	state.Collateral = account.Collateral.ToBig()
	state.EquityUsd = margin.Equity.BigInt()
	state.InitialMarginUsd = margin.InitialMargin.BigInt()
	state.MaintenanceMarginUsd = margin.MaintenanceMargin.BigInt()
	state.Withdrawable = withdrawable.ToBig()
	state.PositionCount = viewUint(account.PositionCount)
	state.OpenOrderCount = viewUint(account.OpenOrderCount)
	return method.Outputs.Pack(state)
}

func viewGetPositionMargin(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	position, _, mark, err := viewPosition(p, ctx, args)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	initial, err := perpskeeper.InitialMargin(sdk.NewIntFromBigInt(viewNotional(position, mark)), position.Leverage)
	if err != nil {
		return nil, err
	}
	locked, _, err := types.UsdToUsdlUp(initial)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewPositionMargin{
		Token: params.CollateralToken, LockedAmount: locked.BigInt(), MarketId: viewUint(position.MarketID), IsLong: position.IsLong,
	})
}

func viewGetMarginTiers(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, _, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	tiers, err := types.TiersFor(market)
	if err != nil {
		return nil, err
	}
	rows := make([]viewMarginTier, len(tiers))
	for i, tier := range tiers {
		rows[i] = viewMarginTier{
			LowerBoundUsd: tier.LowerBoundUsd.BigInt(), MaxLeverage: viewUint(tier.MaxLeverage),
			MaintenanceBps: viewUint(tier.MaintenanceBps), DeductionUsd: tier.DeductionUsd.BigInt(),
		}
	}
	return method.Outputs.Pack(rows)
}

func viewCheckLiquidatable(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	positionID, err := viewWord(args, 0)
	if err != nil {
		return nil, err
	}
	position, err := p.keeper.GetPosition(ctx, positionID)
	if err != nil {
		return nil, err
	}
	margin, err := p.keeper.AccountMargin(ctx, position.Account)
	if err != nil {
		return nil, err
	}
	ratio := new(big.Int)
	switch {
	case !margin.Equity.IsPositive():
	case !margin.MaintenanceMargin.IsPositive():
		ratio.Set(viewMaxWord)
	default:
		ratio.Mul(margin.Equity.BigInt(), viewBps)
		ratio.Quo(ratio, margin.MaintenanceMargin.BigInt())
	}
	return method.Outputs.Pack(margin.Liquidatable(), ratio)
}

func viewOrderArg(p PrecompileExecutor, ctx sdk.Context, args []interface{}) (types.Order, error) {
	orderID, err := viewWord(args, 0)
	if err != nil {
		return types.Order{}, err
	}
	return p.keeper.GetOrder(ctx, orderID)
}

func viewGetOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	order, err := viewOrderArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	initial, err := perpskeeper.InitialMargin(sdk.NewIntFromBigInt(order.SizeUsd.ToBig()), order.Leverage)
	if err != nil {
		return nil, err
	}
	collateral, _, err := types.UsdToUsdlUp(initial)
	if err != nil {
		return nil, err
	}
	trigger := order.TriggerPrice
	if trigger.IsZero() {
		trigger = order.LimitPrice
	}
	active := order.Status == types.OrderStatusUntriggered || order.Status == types.OrderStatusResting
	return method.Outputs.Pack(order.Account, viewUint(order.MarketID), order.IsLong, uint8(order.Type),
		trigger.ToBig(), order.LimitPrice.ToBig(), order.SizeUsd.ToBig(), order.Leverage.ToBig(),
		params.CollateralToken, collateral.BigInt(), active)
}

func viewGetOrderDetails(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	order, err := viewOrderArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewOrderDetails{
		OrderId: order.ID.ToBig(), User: order.Account, MarketId: viewUint(order.MarketID), IsLong: order.IsLong,
		OrderType: uint8(order.Type), TimeInForce: uint8(order.TimeInForce), Status: uint8(order.Status),
		ReduceOnly: order.ReduceOnly, LimitPrice: order.LimitPrice.ToBig(), TriggerPrice: order.TriggerPrice.ToBig(),
		AcceptablePrice: order.AcceptablePrice.ToBig(), SizeUsd: order.SizeUsd.ToBig(), SizeBase: order.SizeBase.ToBig(),
		FilledBase: order.FilledBase.ToBig(), Leverage: order.Leverage.ToBig(), Deadline: order.Deadline,
		Cloid: order.Cloid, Sequence: order.Sequence,
	})
}

func viewGetUserOrderIds(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	user, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	ids, err := p.keeper.AccountOpenOrders(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]*big.Int, len(ids))
	for i := range ids {
		out[i] = ids[i].ToBig()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmp(out[j]) < 0 })
	return method.Outputs.Pack(out)
}

func viewGetPosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	position, _, mark, err := viewPosition(p, ctx, args)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	notional := viewNotional(position, mark)
	positions, err := p.keeper.AccountPositions(ctx, position.Account)
	if err != nil {
		return nil, err
	}
	total := new(big.Int)
	for _, held := range positions {
		_, state, err := viewMarket(p, ctx, held.MarketID)
		if err != nil {
			return nil, err
		}
		heldMark, err := viewMark(state)
		if err != nil {
			return nil, err
		}
		total.Add(total, viewNotional(held, heldMark))
	}
	margin, err := p.keeper.AccountMargin(ctx, position.Account)
	if err != nil {
		return nil, err
	}
	share := new(big.Int)
	if margin.Equity.IsPositive() && total.Sign() > 0 {
		share.Mul(margin.Equity.BigInt(), notional)
		share.Quo(share, total)
	}
	amount, _, err := types.UsdToUsdlDown(sdk.NewIntFromBigInt(share))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(position.Account, viewUint(position.MarketID), position.IsLong, notional, share,
		params.CollateralToken, amount.BigInt(), position.EntryPrice.ToBig(), viewUint(position.OpenedAt), true)
}

func viewTriggerPrice(p PrecompileExecutor, ctx sdk.Context, orderID uint256.Int) (*big.Int, error) {
	if orderID.IsZero() {
		return new(big.Int), nil
	}
	order, err := p.keeper.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return order.TriggerPrice.ToBig(), nil
}

func viewGetPositionDetails(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	position, state, mark, err := viewPosition(p, ctx, args)
	if err != nil {
		return nil, err
	}
	pnl := new(big.Int).Mul(position.SizeBase.ToBig(), mark)
	pnl.Sub(pnl, new(big.Int).Mul(position.CostBasisUsd.ToBig(), viewWad))
	if !position.IsLong {
		pnl.Neg(pnl)
	}
	pnl.Div(pnl, viewWad)
	funding, err := perpskeeper.FundingOwed(position, state.FundingIndex)
	if err != nil {
		return nil, err
	}
	liquidation, err := p.keeper.LiquidationPrice(ctx, position.ID)
	if err != nil {
		return nil, err
	}
	attached, err := p.keeper.GetAttachedTpSl(ctx, position.ID)
	if err != nil {
		return nil, err
	}
	takeProfit, err := viewTriggerPrice(p, ctx, attached.TakeProfitOrderID)
	if err != nil {
		return nil, err
	}
	stopLoss, err := viewTriggerPrice(p, ctx, attached.StopLossOrderID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewPositionDetails{
		PositionId: position.ID.ToBig(), User: position.Account, MarketId: viewUint(position.MarketID),
		IsLong: position.IsLong, SizeBase: position.SizeBase.ToBig(), SizeUsdAtMark: viewNotional(position, mark),
		EntryPrice: position.EntryPrice.ToBig(), Leverage: position.Leverage.ToBig(), UnrealizedPnlUsd: pnl,
		FundingOwedUsd: funding.Usd.BigInt(), LiquidationPrice: liquidation.ToBig(), TakeProfitPrice: takeProfit,
		StopLossPrice: stopLoss, OpenedAt: viewUint(position.OpenedAt),
	})
}

func viewGetUserPositionIds(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	user, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	positions, err := p.keeper.AccountPositions(ctx, user)
	if err != nil {
		return nil, err
	}
	ids := make([]*big.Int, len(positions))
	for i := range positions {
		ids[i] = positions[i].ID.ToBig()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Cmp(ids[j]) < 0 })
	return method.Outputs.Pack(ids)
}

func viewGetUserMarketPosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	user, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	marketID, err := viewMarketID(args, 1)
	if err != nil {
		return nil, err
	}
	if _, err := p.keeper.GetMarket(ctx, marketID); err != nil {
		return nil, err
	}
	positions, err := p.keeper.AccountPositions(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, position := range positions {
		if position.MarketID == marketID {
			return method.Outputs.Pack(position.ID.ToBig())
		}
	}
	return method.Outputs.Pack(new(big.Int))
}

func viewGetMarket(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, _, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	tiers, err := types.TiersFor(market)
	if err != nil {
		return nil, err
	}
	capUsd, err := p.keeper.OpenInterestCap(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(market.Name, market.Symbol, viewUint(market.MaxLeverage),
		viewUint(tiers[0].MaintenanceBps), capUsd.BigInt(), market.Enabled)
}

func viewStep(decimals uint8) (*big.Int, error) {
	if decimals > viewFixedDecimals {
		return nil, fmt.Errorf("%w: %d decimals above %d", types.ErrInvalidParams, decimals, viewFixedDecimals)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(viewFixedDecimals-int(decimals))), nil), nil
}

func viewGetMarketConfig(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, _, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	tick, err := viewStep(market.PxDecimals)
	if err != nil {
		return nil, err
	}
	lot, err := viewStep(market.SzDecimals)
	if err != nil {
		return nil, err
	}
	capUsd, err := p.keeper.OpenInterestCap(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewMarketConfig{
		MarketId: viewUint(market.ID), Symbol: market.Symbol, Name: market.Name, OracleSymbol: market.OracleSymbol,
		SzDecimals: market.SzDecimals, PxDecimals: market.PxDecimals, TickSize: tick, LotSize: lot,
		MaxLeverage: viewUint(market.MaxLeverage), TierClass: uint8(market.TierClass),
		ImpactNotionalUsd: market.ImpactNotionalUsd.BigInt(), OiCapUsd: capUsd.BigInt(),
		FundingCapPpmPerHour: viewUint(market.FundingCapPpmPerHour), Enabled: market.Enabled, ListedAt: market.ListedAt,
	})
}

func viewGetActiveMarketIds(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, _ []interface{}, _ *vm.EVM) ([]byte, error) {
	ids := []*big.Int{}
	if err := p.keeper.IterateMarkets(ctx, func(market types.Market) bool {
		if market.Enabled {
			ids = append(ids, viewUint(market.ID))
		}
		return false
	}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(ids)
}

func viewTotalMarkets(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, _ []interface{}, _ *vm.EVM) ([]byte, error) {
	var count uint64
	if err := p.keeper.IterateMarkets(ctx, func(types.Market) bool {
		count++
		return false
	}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewUint(count))
}

func viewIsMarketActive(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, state, err := viewMarketArg(p, ctx, args)
	switch {
	case errors.Is(err, types.ErrUnknownMarket):
		return method.Outputs.Pack(false)
	case err != nil:
		return nil, err
	}
	return method.Outputs.Pack(market.Enabled && state.Fresh)
}

func viewGetFees(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, _ []interface{}, _ *vm.EVM) ([]byte, error) {
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if len(params.FeeTiers) == 0 || params.FeeTiers[0].MakerFeePpm < 0 {
		return nil, fmt.Errorf("%w: tier 0 fee row", types.ErrInvalidParams)
	}
	tier := params.FeeTiers[0]
	return method.Outputs.Pack(viewUint(tier.TakerFeePpm), viewUint(uint64(tier.MakerFeePpm)),
		viewUint(viewLiquidationFee), viewUint(params.InsuranceSharePpm))
}

func viewGetFeeTier(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	address, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	tier, err := p.keeper.FeeTier(ctx, address)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(tier.Tier, viewUint(tier.TakerFeePpm), big.NewInt(tier.MakerFeePpm), tier.Volume14dUsd.ToBig())
}

func viewGetProtocolAccounts(p PrecompileExecutor, _ sdk.Context, method *abi.Method, _ common.Address, _ []interface{}, _ *vm.EVM) ([]byte, error) {
	return method.Outputs.Pack(p.keeper.FeesAccountAddress(), p.keeper.InsuranceAccountAddress())
}

func viewGetMarkPrice(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	_, state, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	mark, err := viewMark(state)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(mark)
}

func viewGetMarkPriceDetails(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, state, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	mark, err := viewMark(state)
	if err != nil {
		return nil, err
	}
	mid, ok, err := p.keeper.MidPrice(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		mid = state.IndexPrice
	}
	return method.Outputs.Pack(mark, state.IndexPrice.ToBig(), mid.ToBig(), state.Fresh)
}

func viewHourlyRate(state types.MarketState, params types.Params, market types.Market) *big.Int {
	mean := new(big.Int)
	if state.PremiumCount > 0 {
		mean.Quo(viewSigned(state.PremiumSum), viewUint(state.PremiumCount))
	}
	interest := new(big.Int).Mul(viewUint(params.FundingInterestPpm8h), viewPpmToFixed)
	bound := new(big.Int).Mul(viewUint(params.FundingPremiumClampPpm), viewPpmToFixed)
	rate := viewClamp(new(big.Int).Sub(interest, mean), bound)
	rate.Add(rate, mean)
	rate.Quo(rate, big.NewInt(viewFundingPeriods))
	capPpm := params.FundingCapPpmPerHour
	if market.FundingCapPpmPerHour < capPpm {
		capPpm = market.FundingCapPpmPerHour
	}
	return viewClamp(rate, new(big.Int).Mul(viewUint(capPpm), viewPpmToFixed))
}

func viewClamp(value, bound *big.Int) *big.Int {
	if value.Cmp(bound) > 0 {
		return new(big.Int).Set(bound)
	}
	if lower := new(big.Int).Neg(bound); value.Cmp(lower) < 0 {
		return lower
	}
	return value
}

func viewPerSecond(hourly *big.Int) *big.Int {
	return new(big.Int).Quo(hourly, big.NewInt(viewSecondsPerHour))
}

func viewGetCurrentFundingRate(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	market, state, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewPerSecond(viewHourlyRate(state, params, market)))
}

func viewGetFundingState(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	_, state, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	index := viewSigned(state.FundingIndex)
	return method.Outputs.Pack(index, new(big.Int).Neg(index), viewSettleTime(state),
		viewPerSecond(viewSigned(state.HourlyRate)))
}

func viewSettleTime(state types.MarketState) *big.Int {
	return new(big.Int).Mul(viewUint(state.LastIndexHour), big.NewInt(viewSecondsPerHour))
}

func viewGetFundingDetails(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	_, state, err := viewMarketArg(p, ctx, args)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(viewSigned(state.FundingIndex), viewSettleTime(state), viewSigned(state.HourlyRate),
		viewSigned(state.PremiumSum), viewUint(state.PremiumCount))
}

func viewGetPositionFunding(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	position, state, _, err := viewPosition(p, ctx, args)
	if err != nil {
		return nil, err
	}
	funding, err := perpskeeper.FundingOwed(position, state.FundingIndex)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(funding.Usd.BigInt())
}

func viewGetOpenInterest(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	marketID, err := viewMarketID(args, 0)
	if err != nil {
		return nil, err
	}
	interest, err := p.keeper.OpenInterest(ctx, marketID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(interest.LongUsd.ToBig(), interest.ShortUsd.ToBig())
}

func viewGetLiquidationPrice(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	positionID, err := viewWord(args, 0)
	if err != nil {
		return nil, err
	}
	price, err := p.keeper.LiquidationPrice(ctx, positionID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(price.ToBig())
}

func viewGetInsuranceBalance(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	token, err := viewArg[common.Address](args, 0)
	if err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if token != params.CollateralToken {
		return method.Outputs.Pack(new(big.Int))
	}
	fund, err := p.keeper.GetAccount(ctx, p.keeper.InsuranceAccountAddress())
	switch {
	case errors.Is(err, types.ErrNoAccount):
		return method.Outputs.Pack(new(big.Int))
	case err != nil:
		return nil, err
	}
	return method.Outputs.Pack(fund.Collateral.ToBig())
}

func viewGetADLRank(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address, args []interface{}, _ *vm.EVM) ([]byte, error) {
	position, _, mark, err := viewPosition(p, ctx, args)
	if err != nil {
		return nil, err
	}
	entry := position.EntryPrice.ToBig()
	profitable := mark.Cmp(entry) > 0
	if !position.IsLong {
		profitable = mark.Cmp(entry) < 0
	}
	return method.Outputs.Pack(position.RankScore.ToBig(), profitable)
}
