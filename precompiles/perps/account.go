package perps

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

var (
	accountBalanceOf    = crypto.Keccak256([]byte("balanceOf(address)"))[:4]
	accountAllowance    = crypto.Keccak256([]byte("allowance(address,address)"))[:4]
	accountTransferFrom = crypto.Keccak256([]byte("transferFrom(address,address,uint256)"))[:4]
	accountTransfer     = crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	accountTrue         = common.LeftPadBytes([]byte{1}, 32)
)

// accountSummary is the AccountSummary tuple of getAccountSummary.
type accountSummary struct {
	Idle          *big.Int
	TotalLocked   *big.Int
	PositionCount *big.Int
}

func init() {
	for name, serve := range map[string]handler{
		"createVault":        createVault,
		"deposit":            deposit,
		"withdraw":           withdraw,
		"emergencyWithdraw":  emergencyWithdraw,
		"transferCollateral": transferCollateral,
		"setMarginMode":      setMarginMode,
		"topUpPosition":      topUpPosition,
		"transferMargin":     transferMargin,
		"getBalance":         getBalance,
		"getBalance0":        getBalance,
		"getLockedBalance":   getLockedBalance,
		"getLockedBalance0":  getLockedBalance,
		"getAccountSummary":  getAccountSummary,
		"getAccountSummary0": getAccountSummary,
		"getMarginMode":      getMarginMode,
		"getMarginMode0":     getMarginMode,
	} {
		handlers[name] = serve
	}
}

// createVault creates the caller's empty cross account on its first call,
// emitting VaultCreated(caller, 0x1015), and returns the precompile address
// every vault-address call of the application targets.
func createVault(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 0); err != nil {
		return nil, err
	}
	_, created, err := p.keeper.EnsureAccount(ctx, caller)
	if err != nil {
		return nil, err
	}
	if created {
		record := types.EventRecord{Name: types.EventVaultCreated, Args: []any{caller, p.address}}
		if err := logFromEvent(evm, p.address, record); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack(p.address)
}

// deposit pulls amount USDL units from the caller by the deposit leg and
// books them as cross collateral.
func deposit(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 2); err != nil {
		return nil, err
	}
	token, err := accountAddressArg(args[0], "token")
	if err != nil {
		return nil, err
	}
	amount, ok := args[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: amount", types.ErrInvalidParams)
	}
	events, err := p.depositLeg(ctx, evm, caller, token, amount)
	if err != nil {
		return nil, err
	}
	return p.packAccountEvents(method, evm, events)
}

// withdraw books the withdrawal of amount USDL units within the caller's
// withdrawable collateral, then pays them by the withdrawal leg.
func withdraw(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 2); err != nil {
		return nil, err
	}
	token, err := accountAddressArg(args[0], "token")
	if err != nil {
		return nil, err
	}
	if err := p.requireCollateral(ctx, token); err != nil {
		return nil, err
	}
	amount, err := accountAmountArg(args[1], "amount")
	if err != nil {
		return nil, err
	}
	events, err := p.withdrawalLeg(ctx, evm, caller, amount)
	if err != nil {
		return nil, err
	}
	return p.packAccountEvents(method, evm, events)
}

// emergencyWithdraw cancels every open order of a caller without positions
// and pays out all of its withdrawable collateral.
func emergencyWithdraw(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 1); err != nil {
		return nil, err
	}
	token, err := accountAddressArg(args[0], "token")
	if err != nil {
		return nil, err
	}
	if err := p.requireCollateral(ctx, token); err != nil {
		return nil, err
	}
	amount, events, err := p.keeper.EmergencyWithdraw(ctx, caller)
	if err != nil {
		return nil, err
	}
	if err := p.payLeg(evm, token, caller, amount); err != nil {
		return nil, err
	}
	return p.packAccountEvents(method, evm, events)
}

// transferCollateral moves amount USDL units of collateral from the caller to
// another account as store entries; no token moves.
func transferCollateral(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 2); err != nil {
		return nil, err
	}
	to, err := accountAddressArg(args[0], "to")
	if err != nil {
		return nil, err
	}
	amount, err := accountAmountArg(args[1], "amount")
	if err != nil {
		return nil, err
	}
	events, err := p.keeper.TransferCollateral(ctx, caller, to, amount)
	if err != nil {
		return nil, err
	}
	return p.packAccountEvents(method, evm, events)
}

// setMarginMode accepts cross margin (0) for the caller's account and refuses
// every other mode with ErrCrossMarginOnly.
func setMarginMode(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 1); err != nil {
		return nil, err
	}
	mode, ok := args[0].(uint8)
	if !ok {
		return nil, fmt.Errorf("%w: mode", types.ErrInvalidParams)
	}
	if err := p.keeper.SetMarginMode(ctx, caller, mode); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

// topUpPosition deposits amount USDL units into the caller's cross account by
// the deposit leg; the position id only proves that the caller holds the
// position.
func topUpPosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 2); err != nil {
		return nil, err
	}
	positionID, err := accountAmountArg(args[0], "positionId")
	if err != nil {
		return nil, err
	}
	amount, ok := args[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: amount", types.ErrInvalidParams)
	}
	if _, err := p.keeper.ClosePosition(ctx, caller, positionID); err != nil {
		return nil, err
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	events, err := p.depositLeg(ctx, evm, caller, params.CollateralToken, amount)
	if err != nil {
		return nil, err
	}
	return p.packAccountEvents(method, evm, events)
}

// transferMargin is refused under cross margin: every position already draws
// on the one cross account.
func transferMargin(_ PrecompileExecutor, _ sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, 3); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: transferMargin", types.ErrCrossMarginOnly)
}

// getBalance returns the collateral of the account argument, or of the caller
// in the token-only overload; an account never created holds 0.
func getBalance(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	account, err := p.accountView(ctx, method, caller, args)
	if err != nil {
		return nil, err
	}
	collateral, err := p.accountCollateral(ctx, account)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(collateral)
}

// getLockedBalance returns the initial margin of the account in USDL units,
// rounded up.
func getLockedBalance(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	account, err := p.accountView(ctx, method, caller, args)
	if err != nil {
		return nil, err
	}
	locked, _, err := p.accountLocked(ctx, account)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(locked)
}

// getAccountSummary returns the idle collateral (collateral less locked
// initial margin, floored at 0), the locked initial margin and the position
// count of the account.
func getAccountSummary(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	account, err := p.accountView(ctx, method, caller, args)
	if err != nil {
		return nil, err
	}
	collateral, err := p.accountCollateral(ctx, account)
	if err != nil {
		return nil, err
	}
	locked, positions, err := p.accountLocked(ctx, account)
	if err != nil {
		return nil, err
	}
	idle := new(big.Int).Sub(collateral, locked)
	if idle.Sign() < 0 {
		idle.SetUint64(0)
	}
	return method.Outputs.Pack(accountSummary{Idle: idle, TotalLocked: locked,
		PositionCount: new(big.Int).SetUint64(positions)})
}

// getMarginMode returns cross margin (0) for the caller or any account.
func getMarginMode(_ PrecompileExecutor, _ sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := accountArgCount(method, args, len(method.Inputs)); err != nil {
		return nil, err
	}
	if len(args) == 1 {
		if _, err := accountAddressArg(args[0], "account"); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack(uint8(perpskeeper.MarginModeCross))
}

// depositLeg refuses a token other than Params.CollateralToken and a zero
// amount, checks the caller's USDL balance and allowance to the precompile,
// runs transferFrom(caller, perps module EVM address, amount) as a nested
// call from the precompile and books the amount with Deposit once the module
// balance moved by exactly amount. It returns the Deposit events, which the
// caller emits after the token leg's Transfer log.
func (p PrecompileExecutor) depositLeg(ctx sdk.Context, evm *vm.EVM, caller, token common.Address,
	amount *big.Int) ([]types.EventRecord, error) {
	if err := p.requireCollateral(ctx, token); err != nil {
		return nil, err
	}
	value, err := accountAmountArg(amount, "amount")
	if err != nil {
		return nil, err
	}
	if value.IsZero() {
		return nil, fmt.Errorf("%w: deposit", types.ErrZeroAmount)
	}
	leg := accountLeg{evm: evm, token: token, from: p.address, readGas: accountReadGas}
	held, err := leg.balanceOf(caller)
	if err != nil {
		return nil, err
	}
	allowed, err := leg.read(accountAllowance, common.LeftPadBytes(caller.Bytes(), 32),
		common.LeftPadBytes(p.address.Bytes(), 32))
	if err != nil {
		return nil, err
	}
	if held.Cmp(amount) < 0 || allowed.Cmp(amount) < 0 {
		return nil, fmt.Errorf("%w: %s holds %s USDL units with allowance %s, deposit of %s", types.ErrInsufficientCollateral,
			caller, held, allowed, amount)
	}
	module := p.keeper.ModuleEVMAddress()
	if err := leg.move(module, value, accountTransferFrom, common.LeftPadBytes(caller.Bytes(), 32),
		common.LeftPadBytes(module.Bytes(), 32), common.LeftPadBytes(amount.Bytes(), 32)); err != nil {
		return nil, err
	}
	return p.keeper.Deposit(accountResume(ctx, evm), caller, value)
}

// withdrawalLeg books the withdrawal of amount USDL units within the
// account's withdrawable collateral with Withdraw, then pays them to the
// account by the payLeg transfer of Params.CollateralToken from the perps
// module EVM address. It returns the Withdraw events, which the caller emits
// after the token leg's Transfer log.
func (p PrecompileExecutor) withdrawalLeg(ctx sdk.Context, evm *vm.EVM, account common.Address,
	amount uint256.Int) ([]types.EventRecord, error) {
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	events, err := p.keeper.Withdraw(ctx, account, amount)
	if err != nil {
		return nil, err
	}
	if err := p.payLeg(evm, params.CollateralToken, account, amount); err != nil {
		return nil, err
	}
	return events, nil
}

// payLeg runs transfer(to, amount) on the USDL contract as a nested call from
// the perps module EVM address and requires the receiving balance to move by
// exactly amount.
func (p PrecompileExecutor) payLeg(evm *vm.EVM, token, to common.Address, amount uint256.Int) error {
	leg := accountLeg{evm: evm, token: token, from: p.keeper.ModuleEVMAddress(), readGas: accountReadGas}
	return leg.move(to, amount, accountTransfer, common.LeftPadBytes(to.Bytes(), 32),
		common.LeftPadBytes(amount.Bytes(), 32))
}

// requireCollateral refuses every token other than Params.CollateralToken.
func (p PrecompileExecutor) requireCollateral(ctx sdk.Context, token common.Address) error {
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return err
	}
	if token == (common.Address{}) || token != params.CollateralToken {
		return fmt.Errorf("%w: %s", types.ErrUnsupportedCollateral, token)
	}
	return nil
}

// accountView validates the arguments of a two-form account view and returns
// the account it reads: the leading account argument, or the caller in the
// token-only overload. The token must be Params.CollateralToken.
func (p PrecompileExecutor) accountView(ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}) (common.Address, error) {
	if err := accountArgCount(method, args, len(method.Inputs)); err != nil {
		return common.Address{}, err
	}
	account := caller
	if len(args) == 2 {
		var err error
		if account, err = accountAddressArg(args[0], "account"); err != nil {
			return common.Address{}, err
		}
	}
	token, err := accountAddressArg(args[len(args)-1], "token")
	if err != nil {
		return common.Address{}, err
	}
	return account, p.requireCollateral(ctx, token)
}

// accountCollateral is the collateral of account in USDL units, 0 for an
// account never created.
func (p PrecompileExecutor) accountCollateral(ctx sdk.Context, account common.Address) (*big.Int, error) {
	stored, err := p.keeper.GetAccount(ctx, account)
	if errors.Is(err, types.ErrNoAccount) {
		return new(big.Int), nil
	}
	if err != nil {
		return nil, err
	}
	return stored.Collateral.ToBig(), nil
}

// accountLocked is the initial margin of account at mark in USDL units,
// rounded up, and its position count; both are 0 for an account never
// created.
func (p PrecompileExecutor) accountLocked(ctx sdk.Context, account common.Address) (*big.Int, uint64, error) {
	summary, err := p.keeper.AccountMargin(ctx, account)
	if errors.Is(err, types.ErrNoAccount) {
		return new(big.Int), 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	locked, _, err := types.UsdToUsdlUp(summary.InitialMargin)
	if err != nil {
		return nil, 0, err
	}
	return locked.BigInt(), summary.Positions, nil
}

// packAccountEvents emits records as EVM logs of the precompile in order and
// packs the method's outputs.
func (p PrecompileExecutor) packAccountEvents(method *abi.Method, evm *vm.EVM, records []types.EventRecord) ([]byte, error) {
	for _, record := range records {
		if err := logFromEvent(evm, p.address, record); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack()
}

// accountReadGas is the budget the USDL view reads of one token leg share;
// it is the part of a token leg's surcharge above TokenCallGas.
const accountReadGas uint64 = 30_000

// accountLeg runs the nested USDL calls of one token leg from one address:
// the view reads share readGas and the token call gets TokenCallGas.
type accountLeg struct {
	evm     *vm.EVM
	token   common.Address
	from    common.Address
	readGas uint64
}

// read static-calls a one-word USDL view.
func (l *accountLeg) read(selector []byte, words ...[]byte) (*big.Int, error) {
	ret, left, err := l.evm.StaticCall(l.from, l.token, bytes.Join(append([][]byte{selector}, words...), nil),
		l.readGas)
	l.readGas = left
	if err != nil {
		return nil, accountLegError(err)
	}
	if len(ret) != 32 {
		return nil, fmt.Errorf("%w: USDL view returned %d bytes", types.ErrTokenTransferFailed, len(ret))
	}
	return new(big.Int).SetBytes(ret), nil
}

func (l *accountLeg) balanceOf(holder common.Address) (*big.Int, error) {
	return l.read(accountBalanceOf, common.LeftPadBytes(holder.Bytes(), 32))
}

// move calls selector with words on the USDL contract and accepts it only
// when it does not revert, returns true or no data and raises the balance of
// receiver by exactly amount.
func (l *accountLeg) move(receiver common.Address, amount uint256.Int, selector []byte, words ...[]byte) error {
	before, err := l.balanceOf(receiver)
	if err != nil {
		return err
	}
	ret, _, err := l.evm.Call(l.from, l.token, bytes.Join(append([][]byte{selector}, words...), nil), TokenCallGas,
		uint256.NewInt(0))
	if err != nil {
		return accountLegError(err)
	}
	if len(ret) != 0 && !bytes.Equal(ret, accountTrue) {
		return fmt.Errorf("%w: USDL returned %x", types.ErrTokenTransferFailed, ret)
	}
	after, err := l.balanceOf(receiver)
	if err != nil {
		return err
	}
	if moved := new(big.Int).Sub(after, before); moved.Cmp(amount.ToBig()) != 0 {
		return fmt.Errorf("%w: balance of %s moved by %s, want %s", types.ErrTokenTransferFailed, receiver, moved,
			amount.Dec())
	}
	return nil
}

// accountLegError keeps an abort of the whole execution bare and refuses
// every other failure of a nested USDL call with ErrTokenTransferFailed.
func accountLegError(err error) error {
	if abortErr, ok := err.(vm.AbortError); ok && abortErr.IsAbortError() {
		return err
	}
	return fmt.Errorf("%w: %v", types.ErrTokenTransferFailed, err)
}

// accountResume is ctx on the state branch the EVM continues with after a
// nested call, keeping the call's context, event manager and gas meter.
func accountResume(ctx sdk.Context, evm *vm.EVM) sdk.Context {
	return state.GetDBImpl(evm.StateDB).Ctx().WithContext(ctx.Context()).WithEventManager(ctx.EventManager()).
		WithGasMeter(ctx.GasMeter())
}

func accountArgCount(method *abi.Method, args []interface{}, want int) error {
	if len(args) != want {
		return fmt.Errorf("%w: %s takes %d arguments, got %d", types.ErrInvalidParams, method.Name, want, len(args))
	}
	return nil
}

func accountAddressArg(arg interface{}, name string) (common.Address, error) {
	address, ok := arg.(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("%w: %s", types.ErrInvalidParams, name)
	}
	return address, nil
}

func accountAmountArg(arg interface{}, name string) (uint256.Int, error) {
	value, ok := arg.(*big.Int)
	if !ok || value == nil || value.Sign() < 0 {
		return uint256.Int{}, fmt.Errorf("%w: %s", types.ErrInvalidParams, name)
	}
	word, overflow := uint256.FromBig(value)
	if overflow {
		return uint256.Int{}, fmt.Errorf("%w: %s", types.ErrOverflow, name)
	}
	return *word, nil
}
