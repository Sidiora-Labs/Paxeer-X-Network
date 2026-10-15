package spot

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

// GasPerInputByte is charged for every call argument byte on top of the row
// (contracts/spot.md#gas).
const GasPerInputByte uint64 = 16

// SpotKeepers is implemented by application keepers that carry the spot
// keeper. NewPrecompileSet refuses to build without one.
type SpotKeepers interface {
	SpotK() *spotkeeper.Keeper
}

// GasRow is one row of contracts/spot.md#gas: Base, plus PerID for each id of
// the batch argument, plus PerToken for each registered token.
type GasRow struct {
	Base     uint64
	PerID    uint64
	PerToken uint64
}

var (
	pecorAddress      = common.HexToAddress(types.PECORAddress)
	ordersAddress     = common.HexToAddress(types.PECOROrdersAddress)
	stopOrdersAddress = common.HexToAddress(types.PECORStopOrdersAddress)
	quoterAddress     = common.HexToAddress(types.PECORQuoterAddress)
	vaultAddress      = common.HexToAddress(types.PECORVaultAddress)
)

var (
	rowLight  = GasRow{Base: 3000}
	rowRead   = GasRow{Base: 6000}
	rowChange = GasRow{Base: 30000}
	rowPlace  = GasRow{Base: 50000}
	rowFill   = GasRow{Base: 60000}
	rowBatch  = GasRow{Base: 20000, PerID: 60000}
)

// GasTable holds, per served address, the row of every served method.
var GasTable = map[common.Address]map[string]GasRow{
	pecorAddress: {
		"BPS_DENOMINATOR": rowLight, "MAX_FEE_BPS": rowLight, "MAX_IMPACT_BPS": rowLight, "PRECISION": rowLight,
		"TIER1_THRESHOLD": rowLight, "TIER2_THRESHOLD": rowLight, "WETH": rowLight, "accumulatedFees": rowLight,
		"collectFees": rowChange, "feeCollector": rowLight, "getAccumulatedFees": rowRead, "getDetailedQuote": rowRead,
		"getQuoteExactIn": rowRead, "getQuoteExactOut": rowRead, "impactScalarBps": rowLight, "marketBuy": rowFill,
		"marketSell": rowFill, "maxTwapDeviationBps": rowLight, "multicall": rowLight, "owner": rowLight, "pause": rowLight,
		"paused": rowLight, "priceImpactEnabled": rowLight, "priceOracle": rowLight, "renounceOwnership": rowLight,
		"setFeeCollector": rowLight, "setPriceImpact": rowLight, "setPriceOracle": rowLight, "setSwapFee": rowLight,
		"setTieredFees": rowLight, "setTransactionTracker": rowLight, "setTwapSanity": rowLight, "swapExactIn": rowFill,
		"swapExactInNative": rowFill, "swapExactInToNative": rowFill, "swapExactOut": rowFill, "swapFeeBps": rowLight,
		"tier1FeeBps": rowLight, "tier2FeeBps": rowLight, "transactionTracker": rowLight, "transferOwnership": rowLight,
		"twapSanityEnabled": rowLight, "twapSanityPeriod": rowLight, "unpause": rowLight, "vault": rowLight,
	},
	ordersAddress: {
		"BPS_DENOMINATOR": rowLight, "MAX_ORDER_FEE_BPS": rowLight, "accumulatedFees": rowLight, "activeLimitOrderIds": rowLight,
		"batchExecuteLimitOrders": rowBatch, "batchExpireOrders": rowBatch, "canExecuteLimitOrder": rowLight,
		"cancelLimitOrder": rowChange, "collectFees": rowChange, "executeLimitOrder": rowFill, "feeCollector": rowLight,
		"fillQualityTwapPeriod": rowLight, "getAccumulatedFees": rowRead, "getActiveLimitOrderCount": rowRead,
		"getExecutableLimitOrders": rowRead, "getLimitOrder": rowRead, "getUserLimitOrders": rowRead, "keepers": rowLight,
		"limitOrders": rowLight, "modifyLimitOrder": rowChange, "nextOrderId": rowLight, "orderFeeBps": rowLight, "owner": rowLight,
		"pause": rowLight, "paused": rowLight, "placeLimitBuy": rowPlace, "placeLimitSell": rowPlace, "priceOracle": rowLight,
		"renounceOwnership": rowLight, "setFeeCollector": rowLight, "setFillQualityTwapPeriod": rowLight, "setKeeper": rowLight,
		"setOrderFee": rowLight, "setPriceOracle": rowLight, "setTransactionTracker": rowLight, "transactionTracker": rowLight,
		"transferOwnership": rowLight, "unpause": rowLight, "userLimitOrders": rowLight, "vault": rowLight,
	},
	stopOrdersAddress: {
		"BPS_DENOMINATOR": rowLight, "MAX_ORDER_FEE_BPS": rowLight, "accumulatedFees": rowLight,
		"activeStopLimitOrderIds": rowLight, "activeStopLossOrderIds": rowLight, "batchCheckAndExecuteStopLimits": rowBatch,
		"batchExecuteStopLossOrders": rowBatch, "cancelStopLimitOrder": rowChange, "cancelStopLossOrder": rowChange,
		"checkStopLimitActivation": rowChange, "collectFees": rowChange, "executeStopLimitOrder": rowFill,
		"executeStopLossOrder": rowFill, "feeCollector": rowLight, "fillQualityTwapPeriod": rowLight,
		"getAccumulatedFees": rowRead, "getActivatableStopLimits": rowRead, "getActiveStopLimitOrderCount": rowRead,
		"getActiveStopLossOrderCount": rowRead, "getExecutableStopLimits": rowRead, "getStopLimitOrder": rowRead,
		"getStopLossOrder": rowRead, "getUserStopLimitOrders": rowRead, "getUserStopLossOrders": rowRead, "keepers": rowLight,
		"nextOrderId": rowLight, "orderFeeBps": rowLight, "owner": rowLight, "pause": rowLight, "paused": rowLight,
		"placeStopLimitBuy": rowPlace, "placeStopLimitSell": rowPlace, "placeStopLoss": rowPlace, "priceOracle": rowLight,
		"renounceOwnership": rowLight, "setFeeCollector": rowLight, "setFillQualityTwapPeriod": rowLight, "setKeeper": rowLight,
		"setOrderFee": rowLight, "setPriceOracle": rowLight, "setTransactionTracker": rowLight, "stopLimitOrders": rowLight,
		"stopLossOrders": rowLight, "transactionTracker": rowLight, "transferOwnership": rowLight, "unpause": rowLight,
		"userStopLimitOrders": rowLight, "userStopLossOrders": rowLight, "vault": rowLight,
	},
	quoterAddress: {
		"WETH": rowLight, "batchQuote": rowRead, "getAllLiquidityInfo": rowRead, "getLiquidityInfo": rowRead, "getTWAP": rowRead,
		"getTokenPrice": rowLight, "getTokenPrices": rowRead, "pecor": rowLight, "priceOracle": rowLight, "quoteExactIn": rowLight,
		"quoteExactInNative": rowLight, "quoteExactInToNative": rowLight, "quoteExactOut": rowLight, "quoteMarketBuy": rowLight,
		"quoteMarketSell": rowLight, "setPecor": rowLight, "vault": rowLight,
	},
	vaultAddress: {
		"BPS_DENOMINATOR": rowLight, "DEFAULT_MAX_UTILIZATION_BPS": rowLight, "WETH": rowLight, "authorizedOperators": rowLight,
		"deposit": rowPlace, "depositBatch": rowPlace, "depositNative": rowPlace, "emergencyWithdraw": rowLight,
		"getAllReserves": rowRead, "getRegisteredStablecoins": rowRead, "getRegisteredTokenCount": rowRead,
		"getRegisteredTokens": rowRead, "getReserves": rowRead, "getTokenDecimals": rowRead, "getTokenInfo": rowRead,
		"getUntrackedFunds": rowRead, "hasLiquidity": rowLight, "isStablecoin": rowLight, "isTokenPaused": rowLight,
		"maxUtilizationBps": rowLight, "owner": rowLight, "pausedTokens": rowLight, "pullTokens": rowChange, "pushTokens": rowChange,
		"registerToken": rowLight, "registeredStablecoins": rowLight, "registeredTokens": rowLight, "renounceOwnership": rowLight,
		"setMaxUtilizationBps": rowLight, "setOperator": rowLight, "setStablecoinStatus": rowLight, "setTokenPaused": rowLight,
		"setTransactionTracker": rowLight, "shareTokens": rowLight, "syncAllReserves": {PerToken: 20000},
		"syncReserves": {Base: 20000}, "tokens": rowLight, "transactionTracker": rowLight, "transferOwnership": rowLight,
		"updateReserves": rowChange, "withdraw": rowPlace, "withdrawNative": rowPlace,
	},
}

// rail is the executor every spot address is served through. It refuses a
// method without a gas row, value on a non-payable method and a write under a
// static call, runs the address executor atomically inside the EVM call with
// the call's EVM execution as its spotkeeper.Run, charges the nested calls to
// the context gas meter, and returns the custom error data of a refusal.
type rail struct {
	executor  pcommon.PrecompileExecutor
	keeper    *spotkeeper.Keeper
	evmKeeper utils.EVMKeeper
	gas       map[string]GasRow
}

var _ pcommon.PrecompileExecutor = (*rail)(nil)

func gasAdd(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return sum
}

func gasMul(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		return math.MaxUint64
	}
	return lo
}

// cost is the charge of row for args, whose encoding is size bytes long, with
// tokens registered tokens.
func cost(row GasRow, args []interface{}, size int, tokens uint64) uint64 {
	gas := gasAdd(row.Base, gasMul(GasPerInputByte, uint64(size)))
	if row.PerID != 0 && len(args) > 0 {
		if ids, ok := args[0].([]*big.Int); ok {
			gas = gasAdd(gas, gasMul(row.PerID, uint64(len(ids))))
		}
	}
	return gasAdd(gas, gasMul(row.PerToken, tokens))
}

// RequiredGas is the charge of method for the argument bytes input. It reads no
// state, so a PerToken row is charged for one token; RunAndCalculateGas charges
// every registered token.
func (r *rail) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil {
		return pcommon.UnknownMethodCallGas
	}
	row, ok := r.gas[method.Name]
	if !ok {
		return pcommon.UnknownMethodCallGas
	}
	var args []interface{}
	if row.PerID != 0 {
		args, _ = method.Inputs.Unpack(input)
	}
	return cost(row, args, len(input), 1)
}

// tokens is the number of registered tokens, read without charging the call.
func (r *rail) tokens(ctx sdk.Context) (uint64, error) {
	params, err := r.keeper.GetParams(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)))
	if err != nil {
		return 0, err
	}
	return uint64(len(params.Tokens)), nil
}

func (r *rail) Execute(ctx sdk.Context, method *abi.Method, caller, callingContract common.Address, args []interface{},
	value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (ret []byte, err error) {
	defer func() {
		if err != nil && !errors.Is(err, vm.ErrOutOfGas) {
			ret = packError(err)
		}
	}()
	if method == nil {
		return nil, errors.New("spot: missing method")
	}
	if _, ok := r.gas[method.Name]; !ok && method.Type != abi.Receive {
		return nil, fmt.Errorf("spot: %s has no gas row", method.Name)
	}
	if method.StateMutability != "payable" {
		if err := pcommon.ValidateNonPayable(value); err != nil {
			return nil, err
		}
	}
	if readOnly && !method.IsConstant() {
		return nil, vm.ErrWriteProtection
	}
	if evm == nil {
		return nil, errors.New("spot: call requires an EVM execution")
	}
	db := state.GetDBImpl(evm.StateDB)
	if db == nil {
		return nil, errors.New("spot: EVM state has no context")
	}
	revision := db.Snapshot()
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, outOfGas := recovered.(sdk.ErrorOutOfGas); !outOfGas {
				panic(recovered)
			}
			ret, err = nil, vm.ErrOutOfGas
		}
		if err != nil {
			db.RevertToSnapshot(revision)
		}
	}()
	ctx = db.Ctx().WithContext(ctx.Context()).WithGasMeter(ctx.GasMeter()).WithEventManager(ctx.EventManager()).
		WithEVMPrecompileCalledFromDelegateCall(ctx.EVMPrecompileCalledFromDelegateCall())
	gas := pcommon.GetRemainingGas(ctx, r.evmKeeper)
	run := &spotkeeper.Run{EVM: evm, Gas: gas}
	ret, err = r.executor.Execute(spotkeeper.WithRun(ctx, run), method, caller, callingContract, args, value, readOnly, evm, hooks)
	nested := r.evmKeeper.GetCosmosGasLimitFromEVMGas(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), gas-run.Gas)
	ctx.GasMeter().ConsumeGas(nested, "spot nested calls")
	return ret, err
}

// Precompile serves one spot address. The EVM dispatches it through
// RunAndCalculateGas, which charges the row from the supplied gas and the bank
// moves, store access and nested calls of the call from what remains.
type Precompile struct {
	*pcommon.Precompile
	rail *rail
}

var _ vm.DynamicGasPrecompiledContract = (*Precompile)(nil)

func newPrecompile(base *pcommon.Precompile) (*Precompile, error) {
	served, ok := base.GetExecutor().(*rail)
	if !ok {
		return nil, fmt.Errorf("spot: %s is not served by a spot rail", base.Address().Hex())
	}
	return &Precompile{Precompile: base, rail: served}, nil
}

// prepare is Prepare, which refuses a call without a method selector, except
// that a call without input reaches the receive entry of an ABI that has one.
func (p *Precompile) prepare(evm *vm.EVM, input []byte) (sdk.Context, *abi.Method, []interface{}, error) {
	if len(input) == 0 && p.HasReceive() {
		db := state.GetDBImpl(evm.StateDB)
		if db == nil {
			return sdk.Context{}, nil, nil, errors.New("spot: EVM state has no context")
		}
		return db.Ctx(), &p.Receive, nil, nil
	}
	return p.Prepare(evm, input)
}

// RunAndCalculateGas runs the call with suppliedGas. A refused call returns the
// selector and arguments of its ABI error, or Error(string) for an error
// without an ABI form, beside vm.ErrExecutionReverted and keeps the gas it did
// not use; a call that runs out of gas returns vm.ErrOutOfGas and no gas.
func (p *Precompile) RunAndCalculateGas(evm *vm.EVM, caller, callingContract common.Address, input []byte,
	suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, isFromDelegateCall bool,
) (ret []byte, remainingGas uint64, err error) {
	operation := p.GetName() + "_unknown"
	defer func() {
		pcommon.HandlePrecompileError(err, evm, operation)
		if err != nil && !errors.Is(err, vm.ErrOutOfGas) {
			ret, err = packError(err), vm.ErrExecutionReverted
		}
	}()
	ctx, method, args, err := p.prepare(evm, input)
	if err != nil {
		if suppliedGas < pcommon.UnknownMethodCallGas {
			return nil, 0, vm.ErrOutOfGas
		}
		return nil, suppliedGas - pcommon.UnknownMethodCallGas, err
	}
	operation = method.Name
	charge := pcommon.UnknownMethodCallGas
	if row, ok := p.rail.gas[method.Name]; ok {
		tokens := uint64(0)
		if row.PerToken != 0 {
			if tokens, err = p.rail.tokens(ctx); err != nil {
				return nil, 0, err
			}
		}
		charge = cost(row, args, len(input)-4, tokens)
	}
	if suppliedGas < charge {
		return nil, 0, vm.ErrOutOfGas
	}
	limit := p.rail.evmKeeper.GetCosmosGasLimitFromEVMGas(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), suppliedGas-charge)
	if limit == 0 {
		// A zero limit reads as an unmetered context; no gas is left to meter.
		return nil, 0, vm.ErrOutOfGas
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, limit))
	em := ctx.EventManager()
	ctx = ctx.WithEventManager(sdk.NewEventManager()).WithEVMPrecompileCalledFromDelegateCall(isFromDelegateCall)
	ret, err = p.rail.Execute(ctx, method, caller, callingContract, args, value, readOnly, evm, hooks)
	if errors.Is(err, vm.ErrOutOfGas) {
		return nil, 0, err
	}
	remainingGas = p.rail.evmKeeper.GetEVMGasLimitFromCtx(ctx)
	if err != nil {
		return ret, remainingGas, err
	}
	em.EmitEvents(ctx.EventManager().Events())
	return ret, remainingGas, nil
}

// NewPrecompileSet builds the PECOR set at 0x1021-0x1025 over the spot keeper
// of keepers. 0x1026 and 0x1027 carry no code and have no entry.
func NewPrecompileSet(keepers utils.Keepers) (map[common.Address]*pcommon.Precompile, error) {
	wired, ok := keepers.(SpotKeepers)
	if !ok || wired.SpotK() == nil {
		return nil, errors.New("spot: the application keepers carry no spot keeper")
	}
	k, evmKeeper := wired.SpotK(), keepers.EVMK()
	if evmKeeper == nil {
		return nil, errors.New("spot: the application keepers carry no EVM keeper")
	}
	pecor, err := NewPECORExecutor(railEventABIs[pecorAddress], k, evmKeeper)
	if err != nil {
		return nil, err
	}
	orders, err := NewOrdersExecutor(k)
	if err != nil {
		return nil, err
	}
	stopOrders, err := NewStopOrdersExecutor(k, evmKeeper)
	if err != nil {
		return nil, err
	}
	quoter, err := NewQuoterPrecompile(k, evmKeeper)
	if err != nil {
		return nil, err
	}
	executors := []struct {
		address  common.Address
		name     string
		executor pcommon.PrecompileExecutor
	}{
		{pecorAddress, "spot_pecor", pecor},
		{ordersAddress, "spot_orders", orders},
		{stopOrdersAddress, "spot_stoporders", stopOrders},
		{quoterAddress, quoter.GetName(), quoter.GetExecutor()},
		{vaultAddress, "spot_vault", newVaultExecutor(k, evmKeeper)},
	}
	set := make(map[common.Address]*pcommon.Precompile, len(executors))
	for _, entry := range executors {
		served := &rail{executor: entry.executor, keeper: k, evmKeeper: evmKeeper, gas: GasTable[entry.address]}
		set[entry.address] = pcommon.NewPrecompile(railEventABIs[entry.address], served, entry.address, entry.name)
	}
	return set, nil
}
