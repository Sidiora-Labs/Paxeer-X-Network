package perps

import (
	"errors"
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
	// usdlToUsdScale converts 6-decimal USDL units to 18-decimal USD.
	usdlToUsdScale = big.NewInt(1_000_000_000_000)
	// ppmScale is the scale of fee rates.
	ppmScale = big.NewInt(1_000_000)
	// compatDenominatorScale is 1e18 x 1e6, the scale of 1 in the
	// denominator 1 + leverage x taker rate of the compatibility notional.
	compatDenominatorScale = new(big.Int).Mul(big.NewInt(1_000_000_000_000_000_000), ppmScale)
)

// orderParamsArg is the OrderParams tuple of contracts/perps.md section 2 as
// go-ethereum decodes it.
type orderParamsArg struct {
	MarketId         *big.Int
	IsLong           bool
	OrderType        uint8
	TimeInForce      uint8
	SizeUsd          *big.Int
	Leverage         *big.Int
	LimitPrice       *big.Int
	TriggerPrice     *big.Int
	AcceptablePrice  *big.Int
	CollateralAmount *big.Int
	ReduceOnly       bool
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	Deadline         uint64
	Cloid            [16]byte
}

func init() {
	handlers["placeOrder"] = placeOrder
	handlers["placeLimitOrder"] = placeLimitOrder
	handlers["placeStopLimitOrder"] = placeStopLimitOrder
	handlers["cancelOrder"] = cancelOrder
	handlers["cancelByCloid"] = cancelByCloid
	handlers["placeTakeProfitOrder"] = placeTakeProfitOrder
	handlers["placeStopLossOrder"] = placeStopLossOrder
	handlers["setPositionTpSl"] = setPositionTpSl
	handlers["executeOrder"] = executeOrder
}

// placeOrder serves placeOrder(OrderParams): the order of the caller through
// the keeper's validation, matching and resting, replacing the place command
// of lx_perps_order_command and execute_order_place.
func placeOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%w: placeOrder takes 1 argument, got %d", types.ErrInvalidParams, len(args))
	}
	arg, ok := abi.ConvertType(args[0], new(orderParamsArg)).(*orderParamsArg)
	if !ok {
		return nil, fmt.Errorf("%w: placeOrder argument is not an OrderParams tuple", types.ErrInvalidParams)
	}
	params, err := orderParamsOf(*arg)
	if err != nil {
		return nil, err
	}
	return p.placeAndPack(ctx, method, caller, params, nil, evm)
}

// placeLimitOrder serves the compatibility method of the application's limit
// order: the deposit leg for collateralAmount, then a GTC limit order at
// triggerPrice of the compatibility notional.
func placeLimitOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 7 {
		return nil, fmt.Errorf("%w: placeLimitOrder takes 7 arguments, got %d", types.ErrInvalidParams, len(args))
	}
	marketID, err := ordersMarketID(args[0])
	if err != nil {
		return nil, err
	}
	isLong, ok := args[1].(bool)
	if !ok {
		return nil, fmt.Errorf("%w: isLong", types.ErrInvalidParams)
	}
	limitPrice, err := ordersWord(args[2], "triggerPrice")
	if err != nil {
		return nil, err
	}
	return p.placeCompat(ctx, method, caller, args[3:], evm, perpskeeper.OrderParams{
		MarketID:    marketID,
		IsLong:      isLong,
		OrderType:   uint8(types.OrderTypeLimit),
		TimeInForce: uint8(types.TimeInForceGTC),
		LimitPrice:  limitPrice,
	})
}

// placeStopLimitOrder serves the compatibility method of the application's
// stop-limit order: the deposit leg for collateralAmount, then a GTC
// STOP_LIMIT order at triggerPrice and limitPrice of the compatibility
// notional.
func placeStopLimitOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 8 {
		return nil, fmt.Errorf("%w: placeStopLimitOrder takes 8 arguments, got %d", types.ErrInvalidParams, len(args))
	}
	marketID, err := ordersMarketID(args[0])
	if err != nil {
		return nil, err
	}
	isLong, ok := args[1].(bool)
	if !ok {
		return nil, fmt.Errorf("%w: isLong", types.ErrInvalidParams)
	}
	triggerPrice, err := ordersWord(args[2], "triggerPrice")
	if err != nil {
		return nil, err
	}
	limitPrice, err := ordersWord(args[3], "limitPrice")
	if err != nil {
		return nil, err
	}
	return p.placeCompat(ctx, method, caller, args[4:], evm, perpskeeper.OrderParams{
		MarketID:     marketID,
		IsLong:       isLong,
		OrderType:    uint8(types.OrderTypeStopLimit),
		TimeInForce:  uint8(types.TimeInForceGTC),
		TriggerPrice: triggerPrice,
		LimitPrice:   limitPrice,
	})
}

// placeCompat runs the deposit leg of the trailing (sizeUsd, leverage,
// collateralToken, collateralAmount) arguments of a compatibility method,
// then places order with notional min(sizeUsd, collateralAmount x leverage /
// (1 + leverage x taker rate)) in 18-decimal USD, rounded down, so the
// deposited collateral covers the initial margin and the fee reserve.
func (p PrecompileExecutor) placeCompat(ctx sdk.Context, method *abi.Method, caller common.Address,
	tail []interface{}, evm *vm.EVM, order perpskeeper.OrderParams) ([]byte, error) {
	sizeUsd, err := ordersWord(tail[0], "sizeUsd")
	if err != nil {
		return nil, err
	}
	leverage, err := ordersWord(tail[1], "leverage")
	if err != nil {
		return nil, err
	}
	token, ok := tail[2].(common.Address)
	if !ok {
		return nil, fmt.Errorf("%w: collateralToken", types.ErrInvalidParams)
	}
	amount, ok := tail[3].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: collateralAmount", types.ErrInvalidParams)
	}
	deposited, err := p.depositLeg(ctx, evm, caller, token, amount)
	if err != nil {
		return nil, err
	}
	ctx = accountResume(ctx, evm)
	tier, err := p.keeper.FeeTier(ctx, caller)
	if err != nil {
		return nil, err
	}
	order.SizeUsd = compatNotional(sizeUsd, leverage, amount, tier.TakerFeePpm)
	order.Leverage = leverage
	return p.placeAndPack(ctx, method, caller, order, deposited, evm)
}

// compatNotional is min(sizeUsd, collateralAmount x 1e12 x leverage x 1e6 /
// (1e24 + leverage x takerPpm)), the compatibility notional of
// contracts/perps.md section 5 with leverage in 18-decimal fixed point.
func compatNotional(sizeUsd, leverage uint256.Int, collateralAmount *big.Int, takerPpm uint64) uint256.Int {
	lev := leverage.ToBig()
	numerator := new(big.Int).Mul(collateralAmount, usdlToUsdScale)
	numerator.Mul(numerator, lev)
	numerator.Mul(numerator, ppmScale)
	denominator := new(big.Int).Mul(lev, new(big.Int).SetUint64(takerPpm))
	denominator.Add(denominator, compatDenominatorScale)
	bound := numerator.Quo(numerator, denominator)
	if bound.Cmp(sizeUsd.ToBig()) >= 0 {
		return sizeUsd
	}
	var notional uint256.Int
	notional.SetFromBig(bound)
	return notional
}

// placeAndPack places order for caller, emits the records of earlier legs
// followed by the placement's records and returns the packed order id.
func (p PrecompileExecutor) placeAndPack(ctx sdk.Context, method *abi.Method, caller common.Address,
	order perpskeeper.OrderParams, earlier []types.EventRecord, evm *vm.EVM) ([]byte, error) {
	placed, err := p.keeper.PlaceOrder(ctx, caller, order)
	if err != nil {
		return nil, err
	}
	if err := p.emitOrderEvents(evm, append(earlier, placed.Events...)); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(placed.OrderID.ToBig())
}

// cancelOrder serves cancelOrder(orderId) for the placer of the order,
// replacing the cancel command of lx_perps_order_command and
// execute_order_cancel.
func cancelOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%w: cancelOrder takes 1 argument, got %d", types.ErrInvalidParams, len(args))
	}
	orderID, err := ordersWord(args[0], "orderId")
	if err != nil {
		return nil, err
	}
	cancelled, err := p.keeper.CancelOrder(ctx, caller, orderID)
	if err != nil {
		return nil, err
	}
	if err := p.emitOrderEvents(evm, []types.EventRecord{cancelled}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// cancelByCloid serves cancelByCloid(marketId, cloid) for the placer of the
// open order carrying cloid in the market.
func cancelByCloid(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%w: cancelByCloid takes 2 arguments, got %d", types.ErrInvalidParams, len(args))
	}
	cloid, ok := args[1].([16]byte)
	if !ok {
		return nil, fmt.Errorf("%w: cloid", types.ErrInvalidParams)
	}
	raw, ok := args[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: marketId", types.ErrInvalidParams)
	}
	if !raw.IsUint64() {
		return nil, fmt.Errorf("%w: no order with cloid %x in market %s", types.ErrUnknownOrder, cloid, raw)
	}
	cancelled, err := p.keeper.CancelByCloid(ctx, caller, raw.Uint64(), cloid)
	if err != nil {
		return nil, err
	}
	if err := p.emitOrderEvents(evm, []types.EventRecord{cancelled}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// placeTakeProfitOrder serves placeTakeProfitOrder(positionId, triggerPrice):
// a reduce-only TAKE_PROFIT order for the whole position, attached to it
// beside its open stop-loss leg.
func placeTakeProfitOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	return p.placePositionLeg(ctx, method, caller, args, evm, types.OrderTypeTakeProfit)
}

// placeStopLossOrder serves placeStopLossOrder(positionId, triggerPrice): a
// reduce-only STOP_LOSS order for the whole position, attached to it beside
// its open take-profit leg.
func placeStopLossOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	return p.placePositionLeg(ctx, method, caller, args, evm, types.OrderTypeStopLoss)
}

// placePositionLeg sets one leg of the position's attached pair through
// SetPositionTpSl, keeping the trigger price of the other leg while that leg
// is still open, and returns the id of the new leg's order.
func (p PrecompileExecutor) placePositionLeg(ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM, kind types.OrderType) ([]byte, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%w: %s takes 2 arguments, got %d", types.ErrInvalidParams, method.Name, len(args))
	}
	positionID, err := ordersWord(args[0], "positionId")
	if err != nil {
		return nil, err
	}
	triggerPrice, err := ordersWord(args[1], "triggerPrice")
	if err != nil {
		return nil, err
	}
	if triggerPrice.IsZero() {
		return nil, fmt.Errorf("%w: zero trigger price", types.ErrInvalidTriggerPrice)
	}
	pair, err := p.keeper.GetAttachedTpSl(ctx, positionID)
	if err != nil {
		return nil, err
	}
	otherID := pair.StopLossOrderID
	if kind == types.OrderTypeStopLoss {
		otherID = pair.TakeProfitOrderID
	}
	var otherPrice uint256.Int
	if !otherID.IsZero() {
		other, err := p.keeper.GetOrder(ctx, otherID)
		if err != nil && !errors.Is(err, types.ErrUnknownOrder) {
			return nil, err
		}
		if err == nil && other.Status == types.OrderStatusUntriggered {
			otherPrice = other.TriggerPrice
		}
	}
	takeProfit, stopLoss := triggerPrice, otherPrice
	if kind == types.OrderTypeStopLoss {
		takeProfit, stopLoss = otherPrice, triggerPrice
	}
	events, err := p.keeper.SetPositionTpSl(ctx, caller, positionID, takeProfit, stopLoss)
	if err != nil {
		return nil, err
	}
	if pair, err = p.keeper.GetAttachedTpSl(ctx, positionID); err != nil {
		return nil, err
	}
	orderID := pair.TakeProfitOrderID
	if kind == types.OrderTypeStopLoss {
		orderID = pair.StopLossOrderID
	}
	if orderID.IsZero() {
		return nil, fmt.Errorf("%w: position %s holds no %s leg after placement", types.ErrInvalidParams,
			positionID.Dec(), method.Name)
	}
	if err := p.emitOrderEvents(evm, events); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(orderID.ToBig())
}

// setPositionTpSl serves setPositionTpSl(positionId, takeProfitPrice,
// stopLossPrice): it replaces the attached pair of the caller's position, a
// zero price leaving that leg empty.
func setPositionTpSl(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%w: setPositionTpSl takes 3 arguments, got %d", types.ErrInvalidParams, len(args))
	}
	positionID, err := ordersWord(args[0], "positionId")
	if err != nil {
		return nil, err
	}
	takeProfit, err := ordersWord(args[1], "takeProfitPrice")
	if err != nil {
		return nil, err
	}
	stopLoss, err := ordersWord(args[2], "stopLossPrice")
	if err != nil {
		return nil, err
	}
	events, err := p.keeper.SetPositionTpSl(ctx, caller, positionID, takeProfit, stopLoss)
	if err != nil {
		return nil, err
	}
	if err := p.emitOrderEvents(evm, events); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// executeOrder serves executeOrder(orderId) for any caller: it triggers the
// order when its condition holds at the current mark and returns the id of
// the position it fills into.
func executeOrder(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%w: executeOrder takes 1 argument, got %d", types.ErrInvalidParams, len(args))
	}
	orderID, err := ordersWord(args[0], "orderId")
	if err != nil {
		return nil, err
	}
	positionID, events, err := p.keeper.ExecuteOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if err := p.emitOrderEvents(evm, events); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(positionID.ToBig())
}

// emitOrderEvents logs every record from the precompile address in order.
func (p PrecompileExecutor) emitOrderEvents(evm *vm.EVM, events []types.EventRecord) error {
	for _, record := range events {
		if err := logFromEvent(evm, p.address, record); err != nil {
			return err
		}
	}
	return nil
}

// orderParamsOf converts the decoded OrderParams tuple to the keeper's
// order parameters; a market id beyond uint64 names no market.
func orderParamsOf(arg orderParamsArg) (perpskeeper.OrderParams, error) {
	if arg.MarketId == nil || !arg.MarketId.IsUint64() {
		return perpskeeper.OrderParams{}, fmt.Errorf("%w: market %v", types.ErrUnknownMarket, arg.MarketId)
	}
	out := perpskeeper.OrderParams{
		MarketID:    arg.MarketId.Uint64(),
		IsLong:      arg.IsLong,
		OrderType:   arg.OrderType,
		TimeInForce: arg.TimeInForce,
		ReduceOnly:  arg.ReduceOnly,
		Deadline:    arg.Deadline,
		Cloid:       arg.Cloid,
	}
	words := []struct {
		name  string
		value *big.Int
		into  *uint256.Int
	}{
		{"sizeUsd", arg.SizeUsd, &out.SizeUsd},
		{"leverage", arg.Leverage, &out.Leverage},
		{"limitPrice", arg.LimitPrice, &out.LimitPrice},
		{"triggerPrice", arg.TriggerPrice, &out.TriggerPrice},
		{"acceptablePrice", arg.AcceptablePrice, &out.AcceptablePrice},
		{"takeProfitPrice", arg.TakeProfitPrice, &out.TakeProfitPrice},
		{"stopLossPrice", arg.StopLossPrice, &out.StopLossPrice},
	}
	for _, word := range words {
		value, err := ordersWord(word.value, word.name)
		if err != nil {
			return perpskeeper.OrderParams{}, err
		}
		*word.into = value
	}
	return out, nil
}

// ordersMarketID reads a uint256 market id argument; a value beyond uint64
// names no market.
func ordersMarketID(arg interface{}) (uint64, error) {
	raw, ok := arg.(*big.Int)
	if !ok {
		return 0, fmt.Errorf("%w: marketId", types.ErrInvalidParams)
	}
	if !raw.IsUint64() {
		return 0, fmt.Errorf("%w: market %s", types.ErrUnknownMarket, raw)
	}
	return raw.Uint64(), nil
}

// ordersWord reads a uint256 argument.
func ordersWord(arg interface{}, name string) (uint256.Int, error) {
	raw, ok := arg.(*big.Int)
	if !ok || raw == nil {
		return uint256.Int{}, fmt.Errorf("%w: %s", types.ErrInvalidParams, name)
	}
	var word uint256.Int
	if raw.Sign() < 0 || word.SetFromBig(raw) {
		return uint256.Int{}, fmt.Errorf("%w: %s %s is not a uint256", types.ErrOverflow, name, raw)
	}
	return word, nil
}
