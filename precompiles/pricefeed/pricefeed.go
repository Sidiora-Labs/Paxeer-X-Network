package pricefeed

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	pfabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	PriceFeedAddress                       = "0x0000000000000000000000000000000000001020"
	PrecompileName                         = "pricefeed"
	ViewBaseGas                     uint64 = 3000
	WriteBaseGas                    uint64 = 30000
	GasPerByte                      uint64 = 16
	RegisterFeedKeyMethod                  = "registerFeedKey"
	RevokeFeedKeyMethod                    = "revokeFeedKey"
	GetPriceMethod                         = "getPrice"
	GetPriceBySymbolMethod                 = "getPriceBySymbol"
	GetPricesMethod                        = "getPrices"
	GetMarketMethod                        = "getMarket"
	GetMarketCountMethod                   = "getMarketCount"
	MarketIDOfMethod                       = "marketIdOf"
	GetObservationMethod                   = "getObservation"
	GetFeedKeyMethod                       = "getFeedKey"
	GetValidatorByFeedAccountMethod        = "getValidatorByFeedAccount"
	GetParamsMethod                        = "getParams"
)

type PriceFeedKeepers interface {
	PriceFeedK() *pricefeedkeeper.Keeper
}

type PrecompileExecutor struct {
	abi       abi.ABI
	address   common.Address
	keeper    *pricefeedkeeper.Keeper
	evmKeeper utils.EVMKeeper
}

type Precompile struct {
	*pcommon.Precompile
	executor *PrecompileExecutor
}

var _ vm.PrecompiledContract = (*Precompile)(nil)

func NewPrecompile(keepers utils.Keepers) (*Precompile, error) {
	var keeper *pricefeedkeeper.Keeper
	if wired, ok := keepers.(PriceFeedKeepers); ok {
		keeper = wired.PriceFeedK()
	}
	return NewPrecompileWithKeeper(keeper, keepers.EVMK())
}

func NewPrecompileWithKeeper(keeper *pricefeedkeeper.Keeper, evmKeeper utils.EVMKeeper) (*Precompile, error) {
	parsed, err := abi.JSON(strings.NewReader(pfabi.JSON))
	if err != nil {
		return nil, err
	}
	executor := &PrecompileExecutor{abi: parsed, address: common.HexToAddress(PriceFeedAddress), keeper: keeper, evmKeeper: evmKeeper}
	return &Precompile{Precompile: pcommon.NewPrecompile(parsed, executor, executor.address, PrecompileName).WithRevertReasons(), executor: executor}, nil
}

func isTransaction(method string) bool {
	return method == RegisterFeedKeyMethod || method == RevokeFeedKeyMethod
}

func (PrecompileExecutor) IsTransaction(method string) bool { return isTransaction(method) }

func (PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	base := ViewBaseGas
	if isTransaction(method.Name) {
		base = WriteBaseGas
	}
	return base + GasPerByte*(uint64(len(input))+4)
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte,
	value *big.Int, readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) ([]byte, error) {
	if len(input) >= 4 {
		if method, err := p.MethodById(input[:4]); err == nil {
			if _, err := method.Inputs.Unpack(input[4:]); err != nil {
				refusal := fmt.Errorf("LXP_PRECOMPILE_ARG_COUNT: %w", err)
				pcommon.HandlePrecompileError(refusal, evm, method.Name)
				return pcommon.RevertReason(refusal), vm.ErrExecutionReverted
			}
		}
	}
	return p.Precompile.Run(evm, caller, callingContract, input, value, readOnly, isFromDelegateCall, hooks)
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address,
	args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) (ret []byte, err error) {
	defer func() { err = lxpError(err) }()
	if err := p.guard(ctx.EVMPrecompileCalledFromDelegateCall(), method.Name, readOnly); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, fmt.Errorf("LXP_PRECOMPILE_NON_PAYABLE: %w", err)
	}
	if err := pcommon.ValidateArgsLength(args, len(method.Inputs)); err != nil {
		return nil, fmt.Errorf("LXP_PRECOMPILE_ARG_COUNT: %w", err)
	}
	switch method.Name {
	case RegisterFeedKeyMethod:
		return p.registerFeedKey(ctx, method, caller, args, evm)
	case RevokeFeedKeyMethod:
		return p.revokeFeedKey(ctx, method, caller, evm)
	case GetPriceMethod:
		return p.getPrice(ctx, method, args)
	case GetPriceBySymbolMethod:
		return p.getPriceBySymbol(ctx, method, args)
	case GetPricesMethod:
		return p.getPrices(ctx, method, args)
	case GetMarketMethod:
		return p.getMarket(ctx, method, args)
	case GetMarketCountMethod:
		return method.Outputs.Pack(new(big.Int).SetUint64(uint64(len(p.keeper.GetParams(ctx).Markets))))
	case MarketIDOfMethod:
		return p.marketIdOf(ctx, method, args)
	case GetObservationMethod:
		return p.getObservation(ctx, method, args)
	case GetFeedKeyMethod:
		return p.getFeedKey(ctx, method, args)
	case GetValidatorByFeedAccountMethod:
		operator, _ := p.keeper.GetValidatorByFeedAccount(ctx, sdk.AccAddress(args[0].(common.Address).Bytes()))
		return method.Outputs.Pack(common.BytesToAddress(operator))
	case GetParamsMethod:
		return p.getParams(ctx, method)
	default:
		return nil, fmt.Errorf("LXP_PRECOMPILE_UNKNOWN_METHOD: pricefeed: unknown method %s", method.Name)
	}
}

func lxpError(err error) error {
	if name, mapped := types.LXPName(err); mapped {
		return sdkerrors.Wrap(err, name)
	}
	return err
}

func (p PrecompileExecutor) guard(delegate bool, method string, readOnly bool) error {
	if delegate {
		return errors.New("LXP_PRICEFEED_DELEGATECALL: cannot delegatecall pricefeed")
	}
	if readOnly && isTransaction(method) {
		return fmt.Errorf("LXP_PRICEFEED_STATICCALL: cannot call pricefeed %s from staticcall", method)
	}
	if p.keeper == nil || (isTransaction(method) && p.evmKeeper == nil) {
		return errors.New("LXP_PRICEFEED_KEEPER_MISSING: pricefeed: the pricefeed keeper is not wired into this application")
	}
	return nil
}
