package bank

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils/metrics"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	SendMethod        = "send"
	SendContextMethod = "send0"
	SendNativeMethod  = "sendNative"
	BalanceMethod     = "balance"
	AllBalancesMethod = "all_balances"
	NameMethod        = "name"
	SymbolMethod      = "symbol"
	DecimalsMethod    = "decimals"
	SupplyMethod      = "supply"
)

const (
	BankAddress = "0x0000000000000000000000000000000000001001"
)

// v6120Upgrade is the plan from whose height a pointer send over the
// sender's balance returns false instead of reverting.
const v6120Upgrade = "v6.12.0"

// LayerXReceiptEvent is the log the context-hash send overload emits.
const LayerXReceiptEvent = "LayerXReceipt"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	accountKeeper putils.AccountKeeper
	bankKeeper    putils.BankKeeper
	bankMsgServer putils.BankMsgServer
	evmKeeper     putils.EVMKeeper
	address       common.Address

	SendID        []byte
	SendContextID []byte
	SendNativeID  []byte
	BalanceID     []byte
	AllBalancesID []byte
	NameID        []byte
	SymbolID      []byte
	DecimalsID    []byte
	SupplyID      []byte
}

type CoinBalance struct {
	Amount *big.Int
	Denom  string
}

func GetABI() abi.ABI {
	return pcommon.MustGetABI(f, "abi.json")
}

func NewPrecompile(keepers putils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := GetABI()
	p := &PrecompileExecutor{
		bankKeeper:    keepers.BankK(),
		bankMsgServer: keepers.BankMS(),
		evmKeeper:     keepers.EVMK(),
		accountKeeper: keepers.AccountK(),
		address:       common.HexToAddress(BankAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SendMethod:
			p.SendID = m.ID
		case SendContextMethod:
			p.SendContextID = m.ID
		case SendNativeMethod:
			p.SendNativeID = m.ID
		case BalanceMethod:
			p.BalanceID = m.ID
		case AllBalancesMethod:
			p.AllBalancesID = m.ID
		case NameMethod:
			p.NameID = m.ID
		case SymbolMethod:
			p.SymbolID = m.ID
		case DecimalsMethod:
			p.DecimalsID = m.ID
		case SupplyMethod:
			p.SupplyID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "bank").WithRevertReasons(), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	ret, remainingGas, err = p.dispatch(ctx, method, caller, callingContract, args, value, readOnly, evm, hooks)
	var refused refusal
	if errors.As(err, &refused) && p.mergePlanActive(ctx) {
		err = refused.lxpError()
	}
	return ret, remainingGas, err
}

func (p PrecompileExecutor) dispatch(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, uint64, error) {
	switch method.Name {
	case SendMethod:
		return p.send(ctx, caller, method, args, value, readOnly)
	case SendContextMethod:
		return p.sendWithContext(ctx, caller, method, args, value, readOnly, evm)
	case SendNativeMethod:
		return p.sendNative(ctx, method, args, caller, callingContract, value, readOnly, hooks, evm)
	case BalanceMethod:
		return p.balance(ctx, method, args, value)
	case AllBalancesMethod:
		return p.all_balances(ctx, method, args, value)
	case NameMethod:
		return p.name(ctx, method, args, value)
	case SymbolMethod:
		return p.symbol(ctx, method, args, value)
	case DecimalsMethod:
		return p.decimals(ctx, method, args, value)
	case SupplyMethod:
		return p.totalSupply(ctx, method, args, value)
	}
	return nil, 0, nil
}

// refusal is a framework refusal of a bank call. Its text is the text the
// call reverted with before the merge plan; from the plan's height Execute
// replaces it with the registry text of its code.
type refusal struct {
	code   string
	detail string
	err    error
}

func (r refusal) Error() string {
	return r.err.Error()
}

func (r refusal) Unwrap() error {
	return r.err
}

// lxpError returns the error whose revert reason is LXPRevert of the
// refusal's code and detail.
func (r refusal) lxpError() error {
	reason, err := abi.UnpackRevert(pcommon.LXPRevert(r.code, r.detail))
	if err != nil {
		return r.err
	}
	return errors.New(reason)
}

func argCountRefusal(err error) error {
	return refusal{code: "LXP_PRECOMPILE_ARG_COUNT", detail: err.Error(), err: err}
}

func nonPayableRefusal(err error) error {
	return refusal{code: "LXP_PRECOMPILE_NON_PAYABLE", err: err}
}

func nonCanonicalRefusal(err error) error {
	return refusal{code: "LXP_NON_CANONICAL", detail: err.Error(), err: err}
}

func assetPausedRefusal(err error) error {
	return refusal{code: "LXP_ASSET_PAUSED", detail: err.Error(), err: err}
}

func invalidAddrRefusal() error {
	return refusal{code: "LXP_PRECOMPILE_INVALID_ADDR", err: errors.New("invalid addr")}
}

// mergePlanActive reports whether the merge plan is active at the height of
// the call's block. It reads the plan without charging the call.
func (p PrecompileExecutor) mergePlanActive(ctx sdk.Context) bool {
	getter, ok := p.evmKeeper.(interface {
		UpgradeKeeper() *upgradekeeper.Keeper
	})
	if !ok {
		return false
	}
	return getter.UpgradeKeeper().IsUpgradeActiveAtHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), pcommon.MergeUpgrade, ctx.BlockHeight())
}

// v6120Active reports whether the v6.12.0 plan is active at the height of the
// call's block. It reads the plan without charging the call.
func (p PrecompileExecutor) v6120Active(ctx sdk.Context) bool {
	getter, ok := p.evmKeeper.(interface {
		UpgradeKeeper() *upgradekeeper.Keeper
	})
	if !ok {
		return false
	}
	return getter.UpgradeKeeper().IsUpgradeActiveAtHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), v6120Upgrade, ctx.BlockHeight())
}

func (p PrecompileExecutor) send(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, value *big.Int, readOnly bool) ([]byte, uint64, error) {
	if readOnly {
		return nil, 0, errors.New("cannot call send from staticcall")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, 0, argCountRefusal(err)
	}
	if !p.v6120Active(ctx) {
		if _, err := p.pointerSend(ctx, caller, args); err != nil {
			return nil, 0, err
		}
		bz, err := method.Outputs.Pack(true)
		return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
	}
	cache, write := ctx.CacheContext()
	if _, err := p.pointerSend(cache, caller, args); err != nil {
		if !errors.Is(err, sdkerrors.ErrInsufficientFunds) {
			return nil, 0, err
		}
		bz, err := method.Outputs.Pack(false)
		return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
	}
	write()
	ctx.EventManager().EmitEvents(cache.EventManager().Events())
	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

// sendWithContext is the five-argument send overload served from the merge
// plan height. It runs the checks and the bank move of send and, for a nonzero
// amount, emits LayerXReceipt carrying the caller's context hash.
func (p PrecompileExecutor) sendWithContext(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) ([]byte, uint64, error) {
	if !p.mergePlanActive(ctx) {
		return nil, 0, fmt.Errorf("no method with id: %#x", method.ID)
	}
	if readOnly {
		return nil, 0, nonCanonicalRefusal(errors.New("cannot call send from staticcall"))
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonCanonicalRefusal(err)
	}
	if err := pcommon.ValidateArgsLength(args, 5); err != nil {
		return nil, 0, argCountRefusal(err)
	}
	moved, err := p.pointerSend(ctx, caller, args)
	if errors.Is(err, banktypes.ErrSendDisabled) {
		return nil, 0, assetPausedRefusal(err)
	}
	if err != nil {
		return nil, 0, err
	}
	if moved {
		event := GetABI().Events[LayerXReceiptEvent]
		data, err := event.Inputs.NonIndexed().Pack(args[2], args[3], args[4])
		if err != nil {
			return nil, 0, err
		}
		from := args[0].(common.Address)
		to := args[1].(common.Address)
		topics := []common.Hash{event.ID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}
		if err := pcommon.EmitEVMLog(evm, p.address, topics, data); err != nil {
			return nil, 0, err
		}
	}
	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

// pointerSend moves args[3] of denom args[2] from args[0] to args[1] through
// the bank MsgSend path once the caller is proven to be the denom's ERC20
// native pointer. It reports whether coins moved: a zero amount moves none.
func (p PrecompileExecutor) pointerSend(ctx sdk.Context, caller common.Address, args []interface{}) (bool, error) {
	denom := args[2].(string)
	if denom == "" {
		return false, errors.New("invalid denom")
	}
	pointer, _, exists := p.evmKeeper.GetERC20NativePointer(ctx, denom)
	if !exists || pointer.Cmp(caller) != 0 {
		return false, fmt.Errorf("only pointer %s can send %s but got %s", pointer.Hex(), denom, caller.Hex())
	}
	amount := args[3].(*big.Int)
	if amount.Cmp(utils.Big0) == 0 {
		// short circuit
		return false, nil
	}
	senderPaxAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return false, err
	}
	receiverPaxAddr, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return false, err
	}

	msg := &banktypes.MsgSend{
		FromAddress: senderPaxAddr.String(),
		ToAddress:   receiverPaxAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount))),
	}

	if err := msg.ValidateBasic(); err != nil {
		return false, err
	}

	if _, err := p.bankMsgServer.Send(sdk.WrapSDKContext(ctx), msg); err != nil {
		return false, err
	}
	return true, nil
}

func (p PrecompileExecutor) sendNative(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address, callingContract common.Address, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, uint64, error) {
	if readOnly {
		return nil, 0, errors.New("cannot call sendNative from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall sendNative")
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, argCountRefusal(err)
	}
	if value == nil || value.Sign() == 0 {
		return nil, 0, errors.New("set `value` field to non-zero to send")
	}

	senderPaxAddr, ok := p.evmKeeper.GetPaxAddress(ctx, caller)
	if !ok {
		return nil, 0, invalidAddrRefusal()
	}

	receiverAddr, ok := (args[0]).(string)
	if !ok || receiverAddr == "" {
		return nil, 0, invalidAddrRefusal()
	}

	receiverPaxAddr, err := sdk.AccAddressFromBech32(receiverAddr)
	if err != nil {
		return nil, 0, err
	}

	uhpx, wei, err := pcommon.HandlePaymentUhpxWei(ctx, p.evmKeeper.GetPaxAddressOrDefault(ctx, p.address), senderPaxAddr, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
	if err != nil {
		return nil, 0, err
	}

	if err := p.bankKeeper.SendCoinsAndWei(ctx, senderPaxAddr, receiverPaxAddr, uhpx, wei); err != nil {
		return nil, 0, err
	}
	accExists := p.accountKeeper.HasAccount(ctx, receiverPaxAddr)
	if !accExists {
		defer metrics.SafeTelemetryIncrCounter(1, "new", "account")
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, receiverPaxAddr))
	}

	if hooks != nil {
		newCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
		remainingGas := pcommon.GetRemainingGas(newCtx, p.evmKeeper)
		if hooks.OnEnter != nil {
			hooks.OnEnter(evm.GetDepth()+1, byte(vm.CALL), caller, p.evmKeeper.GetEVMAddressOrDefault(newCtx, receiverPaxAddr), []byte{}, remainingGas, value)
		}
		defer func() {
			if hooks.OnExit != nil {
				hooks.OnExit(evm.GetDepth()+1, []byte{}, 0, nil, false)
			}
		}()
	}

	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) balance(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, argCountRefusal(err)
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}
	denom := args[1].(string)
	if denom == "" {
		return nil, 0, errors.New("invalid denom")
	}

	bz, err := method.Outputs.Pack(p.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt())
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) all_balances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, argCountRefusal(err)
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}

	coins := p.bankKeeper.GetAllBalances(ctx, addr)

	// convert to coin balance structs
	coinBalances := make([]CoinBalance, 0, len(coins))

	for _, coin := range coins {
		coinBalances = append(coinBalances, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}

	bz, err := method.Outputs.Pack(coinBalances)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) name(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, argCountRefusal(err)
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, 0, fmt.Errorf("denom %s not found", denom)
	}
	bz, err := method.Outputs.Pack(metadata.Name)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) symbol(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, argCountRefusal(err)
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, 0, fmt.Errorf("denom %s not found", denom)
	}
	bz, err := method.Outputs.Pack(metadata.Symbol)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) decimals(ctx sdk.Context, method *abi.Method, _ []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	// all native tokens are integer-based, returns decimals for microdenom (uhpx)
	bz, err := method.Outputs.Pack(uint8(0))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) totalSupply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, nonPayableRefusal(err)
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, argCountRefusal(err)
	}

	denom := args[0].(string)
	coin := p.bankKeeper.GetSupply(ctx, denom)
	bz, err := method.Outputs.Pack(coin.Amount.BigInt())
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, invalidAddrRefusal()
	}
	paxAddr, found := p.evmKeeper.GetPaxAddress(ctx, addr)
	if !found {
		// return the casted version instead
		return sdk.AccAddress(addr[:]), nil
	}
	return paxAddr, nil
}

func (PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case SendMethod:
		return true
	case SendContextMethod:
		return true
	case SendNativeMethod:
		return true
	default:
		return false
	}
}

func (p PrecompileExecutor) EVMKeeper() putils.EVMKeeper {
	return p.evmKeeper
}
