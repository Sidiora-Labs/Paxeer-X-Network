package spot

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

type pecorExecutor struct {
	abi       abi.ABI
	keeper    *spotkeeper.Keeper
	evmKeeper utils.EVMKeeper
}

func NewPECORExecutor(contract abi.ABI, keeper *spotkeeper.Keeper, evmKeeper utils.EVMKeeper) (*pecorExecutor, error) {
	if keeper == nil || evmKeeper == nil {
		return nil, errors.New("spot: PECOR requires spot and EVM keepers")
	}
	return &pecorExecutor{abi: contract, keeper: keeper, evmKeeper: evmKeeper}, nil
}

func (p *pecorExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	base := uint64(3000)
	if method != nil {
		switch method.Name {
		case "marketBuy", "marketSell", "swapExactIn", "swapExactOut", "swapExactInNative", "swapExactInToNative":
			base = 60000
		case "collectFees":
			base = 30000
		case "getAccumulatedFees", "getDetailedQuote", "getQuoteExactIn", "getQuoteExactOut":
			base = 6000
		}
	}
	return base + 16*uint64(len(input))
}

func (p *pecorExecutor) Execute(ctx sdk.Context, method *abi.Method, caller, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (ret []byte, err error) {
	defer func() {
		if err != nil {
			var refusal *types.AbiError
			if errors.As(err, &refusal) {
				ret = refusal.Data()
			}
		}
	}()
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("spot: cannot delegatecall PECOR")
	}
	if method == nil || method.Type == abi.Receive {
		return nil, types.ErrNativeTransferFailed
	}
	if method.StateMutability != "payable" {
		if err := pcommon.ValidateNonPayable(value); err != nil {
			return nil, err
		}
	}
	if readOnly && method.StateMutability != "view" && method.StateMutability != "pure" {
		return nil, vm.ErrWriteProtection
	}
	if err := pcommon.ValidateArgsLength(args, len(method.Inputs)); err != nil {
		return nil, err
	}
	if evm == nil {
		return nil, errors.New("spot: PECOR requires an EVM execution")
	}
	db := state.GetDBImpl(evm.StateDB)
	if db == nil {
		return nil, errors.New("spot: EVM state has no context")
	}
	revision := db.Snapshot()
	completed := false
	defer func() {
		if !completed {
			db.RevertToSnapshot(revision)
		}
	}()
	ctx = db.Ctx().WithContext(ctx.Context()).WithGasMeter(ctx.GasMeter()).WithEventManager(sdk.NewEventManager())
	gas := pcommon.GetRemainingGas(ctx, p.evmKeeper)
	run := &spotkeeper.Run{EVM: evm, Gas: gas}
	ctx = spotkeeper.WithRun(ctx, run)
	ret, err = p.dispatch(ctx, method, caller, args, value, readOnly, evm, 0)
	ctx.GasMeter().ConsumeGas(p.evmKeeper.GetCosmosGasLimitFromEVMGas(ctx, gas-run.Gas), "PECOR nested calls")
	if err != nil {
		return ret, err
	}
	if readOnly {
		db.RevertToSnapshot(revision)
	} else {
		db.Ctx().EventManager().EmitEvents(ctx.EventManager().Events())
	}
	completed = true
	return ret, nil
}

func (p *pecorExecutor) dispatch(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, depth int) ([]byte, error) {
	if method.StateMutability != "payable" {
		if err := pcommon.ValidateNonPayable(value); err != nil {
			return nil, err
		}
	}
	if readOnly && method.StateMutability != "view" && method.StateMutability != "pure" {
		return nil, vm.ErrWriteProtection
	}
	if err := pcommon.ValidateArgsLength(args, len(method.Inputs)); err != nil {
		return nil, err
	}
	switch method.Name {
	case "renounceOwnership", "transferOwnership", "pause", "unpause", "setSwapFee", "setTieredFees", "setPriceImpact", "setTwapSanity", "setPriceOracle", "setTransactionTracker", "setFeeCollector":
		return nil, types.OwnableUnauthorizedAccount(caller)
	case "PRECISION":
		return method.Outputs.Pack(big.NewInt(1e18))
	case "BPS_DENOMINATOR":
		return method.Outputs.Pack(big.NewInt(types.BpsDenominator))
	case "MAX_FEE_BPS":
		return method.Outputs.Pack(big.NewInt(types.MaxFeeBps))
	case "MAX_IMPACT_BPS":
		return method.Outputs.Pack(big.NewInt(types.MaxImpactBps))
	case "TIER1_THRESHOLD":
		return method.Outputs.Pack(spotkeeper.Tier1Threshold().BigInt())
	case "TIER2_THRESHOLD":
		return method.Outputs.Pack(spotkeeper.Tier2Threshold().BigInt())
	case "priceOracle":
		return method.Outputs.Pack(common.HexToAddress("0x1020"))
	case "vault":
		return method.Outputs.Pack(common.HexToAddress(types.PECORVaultAddress))
	case "transactionTracker":
		return method.Outputs.Pack(common.Address{})
	case "owner":
		return method.Outputs.Pack(p.evmKeeper.GetEVMAddressOrDefault(ctx, authtypes.NewModuleAddress(govtypes.ModuleName)))
	case "WETH":
		pointer, _, found := p.evmKeeper.GetERC20NativePointer(ctx, "uhpx")
		if !found {
			return nil, types.ErrUnsupportedTransfer
		}
		return method.Outputs.Pack(pointer)
	case "multicall":
		return p.multicall(ctx, method, caller, args[0].([][]byte), readOnly, evm, depth)
	case "getQuoteExactIn", "getQuoteExactOut", "getDetailedQuote":
		return p.quote(ctx, method, args)
	case "accumulatedFees", "getAccumulatedFees":
		amount, err := p.keeper.AccumulatedFees(ctx, common.HexToAddress(types.PECORAddress), args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(amount.BigInt())
	case "marketBuy", "marketSell", "swapExactIn", "swapExactOut", "swapExactInNative", "swapExactInToNative", "collectFees":
		var result []byte
		_, err := p.keeper.NonReentrant(ctx, func(inner sdk.Context) (sdk.Context, error) {
			var callErr error
			result, callErr = p.trade(inner, method, caller, args, value, evm)
			next := state.GetDBImpl(evm.StateDB).Ctx().WithContext(inner.Context()).WithGasMeter(inner.GasMeter()).WithEventManager(inner.EventManager())
			return next, callErr
		})
		return result, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case "swapFeeBps":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.SwapFeeBps))
	case "tier1FeeBps":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.Tier1FeeBps))
	case "tier2FeeBps":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.Tier2FeeBps))
	case "impactScalarBps":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.PriceImpactScalarBps))
	case "priceImpactEnabled":
		return method.Outputs.Pack(params.PriceImpactEnabled)
	case "paused":
		return method.Outputs.Pack(params.SwapPaused)
	case "twapSanityEnabled":
		return method.Outputs.Pack(params.TwapSanityEnabled)
	case "twapSanityPeriod":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.TwapSanityPeriod))
	case "maxTwapDeviationBps":
		return method.Outputs.Pack(new(big.Int).SetUint64(params.MaxTwapDeviationBps))
	case "feeCollector":
		return method.Outputs.Pack(params.FeeCollector)
	}
	return nil, fmt.Errorf("spot: unknown PECOR method %s", method.Name)
}

func (p *pecorExecutor) quote(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	input, output, amount := args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int)
	if input == output {
		return nil, types.ErrSameToken
	}
	if amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 {
		return nil, types.ErrUnsupportedInputTransfer
	}
	if amount.Sign() == 0 {
		return nil, types.ErrZeroAmount
	}
	rail := common.HexToAddress(types.PECORAddress)
	if method.Name == "getQuoteExactOut" {
		required, err := p.keeper.QuoteExactOut(ctx, rail, input, output, sdk.NewIntFromBigInt(amount))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(required.BigInt())
	}
	result, err := p.keeper.Price(ctx, rail, input, output, sdk.NewIntFromBigInt(amount))
	if err != nil {
		return nil, err
	}
	if method.Name == "getQuoteExactIn" {
		return method.Outputs.Pack(result.Net.BigInt())
	}
	return method.Outputs.Pack(result.Gross.BigInt(), result.Net.BigInt(), new(big.Int).SetUint64(result.ImpactBps), new(big.Int).SetUint64(result.FeeBps), result.FeeAmount.BigInt())
}

func (p *pecorExecutor) trade(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM) ([]byte, error) {
	if method.Name == "collectFees" {
		params, err := p.keeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		if caller != params.FeeCollector {
			return nil, types.OwnableUnauthorizedAccount(caller)
		}
		token := args[0].(common.Address)
		accrued, err := p.keeper.AccumulatedFees(ctx, common.HexToAddress(types.PECORAddress), token)
		if err != nil {
			return nil, err
		}
		if accrued.IsZero() {
			return nil, types.ErrNoFeesToCollect
		}
		logs, err := p.keeper.CollectFees(ctx, common.HexToAddress(types.PECORAddress), caller, token)
		if err != nil {
			return nil, err
		}
		for _, log := range logs {
			evm.StateDB.AddLog(log)
		}
		return method.Outputs.Pack()
	}
	var result spotkeeper.SwapResult
	var err error
	switch method.Name {
	case "marketBuy":
		result, err = p.keeper.MarketBuy(ctx, caller, args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int), args[3].(*big.Int), args[4].(*big.Int))
	case "marketSell":
		result, err = p.keeper.MarketSell(ctx, caller, args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int), args[3].(*big.Int), args[4].(*big.Int))
	case "swapExactIn":
		result, err = p.keeper.SwapExactIn(ctx, caller, args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int), args[3].(*big.Int), args[4].(*big.Int))
	case "swapExactOut":
		result, err = p.keeper.SwapExactOut(ctx, caller, args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int), args[3].(*big.Int), args[4].(*big.Int))
	case "swapExactInNative":
		result, err = p.keeper.SwapNative(ctx, caller, args[0].(common.Address), value, args[1].(*big.Int), args[2].(*big.Int), true)
	case "swapExactInToNative":
		result, err = p.keeper.SwapNative(ctx, caller, args[0].(common.Address), args[1].(*big.Int), args[2].(*big.Int), args[3].(*big.Int), false)
	default:
		return nil, fmt.Errorf("spot: unknown PECOR trade %s", method.Name)
	}
	if err != nil {
		return nil, err
	}
	amount := result.AmountOut
	if method.Name == "swapExactOut" {
		amount = result.AmountIn
	}
	ret, err := method.Outputs.Pack(amount)
	if err != nil {
		return nil, err
	}
	for _, log := range result.Logs {
		evm.StateDB.AddLog(log)
	}
	return ret, nil
}

func (p *pecorExecutor) multicall(ctx sdk.Context, method *abi.Method, caller common.Address, calls [][]byte, readOnly bool, evm *vm.EVM, depth int) ([]byte, error) {
	if depth >= 1024 {
		return nil, vm.ErrDepth
	}
	results := make([][]byte, len(calls))
	for i, input := range calls {
		if len(input) == 0 {
			return nil, types.ErrNativeTransferFailed
		}
		if len(input) < 4 {
			return nil, types.MulticallFailed(uint256.NewInt(uint64(i)))
		}
		child, err := p.abi.MethodById(input[:4])
		if err != nil {
			return nil, types.MulticallFailed(uint256.NewInt(uint64(i)))
		}
		args, err := child.Inputs.Unpack(input[4:])
		if err != nil {
			return nil, types.MulticallFailed(uint256.NewInt(uint64(i)))
		}
		ctx.GasMeter().ConsumeGas(p.evmKeeper.GetCosmosGasLimitFromEVMGas(ctx, p.RequiredGas(input[4:], child)), "PECOR multicall element")
		next := state.GetDBImpl(evm.StateDB).Ctx().WithContext(ctx.Context()).WithGasMeter(ctx.GasMeter()).WithEventManager(ctx.EventManager())
		result, err := p.dispatch(next, child, caller, args, new(big.Int), readOnly, evm, depth+1)
		if err != nil {
			var refusal *types.AbiError
			if errors.As(err, &refusal) {
				return refusal.Data(), err
			}
			if len(result) != 0 {
				return result, err
			}
			return nil, types.MulticallFailed(uint256.NewInt(uint64(i)))
		}
		results[i] = result
	}
	return method.Outputs.Pack(results)
}
