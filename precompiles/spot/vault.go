package spot

import (
	"errors"
	"fmt"
	"math/big"

	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// vaultOperators are the rails authorizedOperators reports, the addresses
// that hold OPERATOR_ROLE on the source vault.
var vaultOperators = map[common.Address]bool{
	common.HexToAddress(types.PECORAddress):           true,
	common.HexToAddress(types.PECOROrdersAddress):     true,
	common.HexToAddress(types.PECORStopOrdersAddress): true,
}

// vaultAdminMethods revert OwnableUnauthorizedAccount(caller) for every EVM
// caller: their state changes only through governance messages.
var vaultAdminMethods = map[string]bool{
	"emergencyWithdraw":     true,
	"pullTokens":            true,
	"pushTokens":            true,
	"registerToken":         true,
	"renounceOwnership":     true,
	"setMaxUtilizationBps":  true,
	"setOperator":           true,
	"setStablecoinStatus":   true,
	"setTokenPaused":        true,
	"setTransactionTracker": true,
	"syncAllReserves":       true,
	"syncReserves":          true,
	"transferOwnership":     true,
	"updateReserves":        true,
}

// vaultExecutor serves the PECORVault interface at 0x1025 from the vault
// keeper.
type vaultExecutor struct {
	keeper    *spotkeeper.Keeper
	evmKeeper utils.EVMKeeper
}

func newVaultExecutor(keeper *spotkeeper.Keeper, evmKeeper utils.EVMKeeper) *vaultExecutor {
	return &vaultExecutor{keeper: keeper, evmKeeper: evmKeeper}
}

func (e *vaultExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, !method.IsConstant())
}

func (e *vaultExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address,
	args []interface{}, value *big.Int, _ bool, evm *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if method.Type == abi.Receive {
		return nil, types.ErrWethOnly
	}
	if vaultAdminMethods[method.Name] {
		return nil, types.OwnableUnauthorizedAccount(caller)
	}
	switch method.Name {
	case "deposit":
		return e.mutate(ctx, evm, method, func(ctx sdk.Context) ([]*ethtypes.Log, error) {
			_, logs, err := e.keeper.Deposit(ctx, caller, args[0].(common.Address), sdk.NewIntFromBigInt(args[1].(*big.Int)))
			return logs, err
		})
	case "depositBatch":
		return e.mutate(ctx, evm, method, func(ctx sdk.Context) ([]*ethtypes.Log, error) {
			raw := args[1].([]*big.Int)
			amounts := make([]sdk.Int, len(raw))
			for i, amount := range raw {
				amounts[i] = sdk.NewIntFromBigInt(amount)
			}
			_, logs, err := e.keeper.DepositBatch(ctx, caller, args[0].([]common.Address), amounts)
			return logs, err
		})
	case "depositNative":
		return e.mutate(ctx, evm, method, func(ctx sdk.Context) ([]*ethtypes.Log, error) {
			_, logs, err := e.keeper.DepositNative(ctx, caller, value)
			return logs, err
		})
	case "withdraw":
		var amount sdk.Int
		ret, err := e.mutate(ctx, evm, nil, func(ctx sdk.Context) ([]*ethtypes.Log, error) {
			paid, logs, err := e.keeper.Withdraw(ctx, caller, args[0].(common.Address), sdk.NewIntFromBigInt(args[1].(*big.Int)))
			amount = paid
			return logs, err
		})
		if err != nil {
			return ret, err
		}
		return method.Outputs.Pack(amount.BigInt())
	case "withdrawNative":
		return e.mutate(ctx, evm, method, func(ctx sdk.Context) ([]*ethtypes.Log, error) {
			_, logs, err := e.keeper.WithdrawNative(ctx, caller, sdk.NewIntFromBigInt(args[0].(*big.Int)), args[1].(common.Address))
			return logs, err
		})
	case "BPS_DENOMINATOR":
		return method.Outputs.Pack(big.NewInt(types.BpsDenominator))
	case "DEFAULT_MAX_UTILIZATION_BPS":
		return method.Outputs.Pack(big.NewInt(types.DefaultMaxUtilizationBps))
	case "WETH":
		denom := e.evmKeeper.GetBaseDenom(ctx)
		pointer, _, exists := e.evmKeeper.GetERC20NativePointer(ctx, denom)
		if !exists {
			return nil, fmt.Errorf("spot: %s has no ERC20 pointer", denom)
		}
		return method.Outputs.Pack(pointer)
	case "authorizedOperators":
		return method.Outputs.Pack(vaultOperators[args[0].(common.Address)])
	case "owner":
		return method.Outputs.Pack(types.GovernanceEVMAddress())
	case "transactionTracker":
		return method.Outputs.Pack(common.Address{})
	case "getRegisteredTokens", "getRegisteredStablecoins", "getRegisteredTokenCount",
		"registeredTokens", "registeredStablecoins", "getAllReserves":
		return e.registry(ctx, method, args)
	case "getTokenInfo", "tokens":
		return e.tokenInfo(ctx, method, args[0].(common.Address))
	case "getReserves":
		book, err := e.keeper.GetBook(ctx, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(book.Reserves.BigInt())
	case "getTokenDecimals", "isStablecoin", "isTokenPaused", "pausedTokens", "maxUtilizationBps":
		params, _, err := e.tokenParams(ctx, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		switch method.Name {
		case "getTokenDecimals":
			return method.Outputs.Pack(uint8(params.Decimals)) //nolint:gosec // registration caps decimals at 36
		case "isStablecoin":
			return method.Outputs.Pack(params.IsStablecoin)
		case "maxUtilizationBps":
			return method.Outputs.Pack(new(big.Int).SetUint64(spotkeeper.MaxUtilizationBps(params)))
		default:
			return method.Outputs.Pack(params.Paused)
		}
	case "getUntrackedFunds":
		untracked, err := e.keeper.UntrackedFunds(ctx, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(untracked.BigInt())
	case "hasLiquidity":
		ok, err := e.keeper.HasLiquidity(ctx, args[0].(common.Address), sdk.NewIntFromBigInt(args[1].(*big.Int)))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(ok)
	case "shareTokens":
		pointer, _, _ := e.evmKeeper.GetERC20NativePointer(ctx, e.keeper.ShareDenom(args[0].(common.Address)))
		return method.Outputs.Pack(pointer)
	}
	return nil, fmt.Errorf("spot: vault method %s is not served", method.Name)
}

// mutate runs fn under the spot re-entry guard, emits the logs it returns at
// their addresses and returns the empty outputs of method; a nil method
// returns no data for the caller to pack.
func (e *vaultExecutor) mutate(ctx sdk.Context, evm *vm.EVM, method *abi.Method,
	fn func(sdk.Context) ([]*ethtypes.Log, error)) ([]byte, error) {
	var logs []*ethtypes.Log
	if _, err := e.keeper.NonReentrant(ctx, func(ctx sdk.Context) (sdk.Context, error) {
		var err error
		logs, err = fn(ctx)
		return ctx, err
	}); err != nil {
		return nil, err
	}
	for _, log := range logs {
		if err := pcommon.EmitEVMLog(evm, log.Address, log.Topics, log.Data); err != nil {
			return nil, err
		}
	}
	if method == nil {
		return nil, nil
	}
	return method.Outputs.Pack()
}

// tokenParams returns the registered params of token and whether the tokens
// parameter lists it; an unregistered token has zero params.
func (e *vaultExecutor) tokenParams(ctx sdk.Context, token common.Address) (types.TokenParams, bool, error) {
	params, err := e.keeper.GetParams(ctx)
	if err != nil {
		return types.TokenParams{}, false, err
	}
	for _, registered := range params.Tokens {
		if registered.Address == token {
			return registered, true, nil
		}
	}
	return types.TokenParams{}, false, nil
}

// tokenInfo packs isRegistered, isStablecoin, decimals, reserves,
// totalDeposited and totalWithdrawn of token.
func (e *vaultExecutor) tokenInfo(ctx sdk.Context, method *abi.Method, token common.Address) ([]byte, error) {
	params, registered, err := e.tokenParams(ctx, token)
	if err != nil {
		return nil, err
	}
	book, err := e.keeper.GetBook(ctx, token)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(registered, params.IsStablecoin, uint8(params.Decimals), //nolint:gosec // registration caps decimals at 36
		book.Reserves.BigInt(), book.TotalDeposited.BigInt(), book.TotalWithdrawn.BigInt())
}

// registry serves the views over the registered tokens, in the order of the
// tokens parameter, which is registration order.
func (e *vaultExecutor) registry(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	params, err := e.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	tokens := make([]common.Address, 0, len(params.Tokens))
	stablecoins := make([]common.Address, 0, len(params.Tokens))
	for _, registered := range params.Tokens {
		tokens = append(tokens, registered.Address)
		if registered.IsStablecoin {
			stablecoins = append(stablecoins, registered.Address)
		}
	}
	switch method.Name {
	case "getRegisteredTokens":
		return method.Outputs.Pack(tokens)
	case "getRegisteredStablecoins":
		return method.Outputs.Pack(stablecoins)
	case "getRegisteredTokenCount":
		return method.Outputs.Pack(big.NewInt(int64(len(tokens))))
	case "registeredTokens":
		return packIndexed(method, tokens, args[0].(*big.Int))
	case "registeredStablecoins":
		return packIndexed(method, stablecoins, args[0].(*big.Int))
	}
	reserves := make([]*big.Int, len(tokens))
	for i, token := range tokens {
		book, err := e.keeper.GetBook(ctx, token)
		if err != nil {
			return nil, err
		}
		reserves[i] = book.Reserves.BigInt()
	}
	return method.Outputs.Pack(tokens, reserves)
}

// errIndexOutOfRange is the public array getter read past its end.
var errIndexOutOfRange = errors.New("spot: index out of range")

func packIndexed(method *abi.Method, list []common.Address, index *big.Int) ([]byte, error) {
	if !index.IsInt64() || index.Sign() < 0 || index.Int64() >= int64(len(list)) {
		return nil, errIndexOutOfRange
	}
	return method.Outputs.Pack(list[index.Int64()])
}
