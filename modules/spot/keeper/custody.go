package keeper

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// weiPerUhpx is the wei value of one uhpx.
var weiPerUhpx = big.NewInt(1_000_000_000_000)

var (
	erc20Transfer     = crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	erc20TransferFrom = crypto.Keccak256([]byte("transferFrom(address,address,uint256)"))[:4]
	erc20BalanceOf    = crypto.Keccak256([]byte("balanceOf(address)"))[:4]
)

// reentrancySlot is the ReentrancyGuardTransient slot of PECOR, held in the
// transient storage of the PECOR rail for every spot rail.
var reentrancySlot = common.HexToHash("0x9b779b17422d0df92223018b32b4d1fa46e071723d6817e2486d003becc55f00")

var (
	pecorRail   = common.HexToAddress(types.PECORAddress)
	slotEntered = common.BigToHash(big.NewInt(1))
)

// Run is the EVM execution a spot rail serves. A context carrying a Run is
// inside a transaction: token calls are nested calls of that EVM, charged to
// Gas. A context without one is outside a transaction and uses CallEVM and
// StaticCallEVM.
type Run struct {
	EVM *vm.EVM
	Gas uint64
}

type runKey struct{}

type guardKey struct{}

// WithRun returns ctx carrying run.
func WithRun(ctx sdk.Context, run *Run) sdk.Context { return ctx.WithValue(runKey{}, run) }

func runOf(ctx sdk.Context) *Run {
	run, _ := ctx.Value(runKey{}).(*Run)
	return run
}

func (r *Run) stateDB() (*state.DBImpl, error) {
	db := state.GetDBImpl(r.EVM.StateDB)
	if db == nil {
		return nil, errors.New("spot: EVM state has no context")
	}
	return db, nil
}

// resume returns the context the run continues on after a nested call or a
// snapshot, which may leave the state on a new branch, keeping the context
// values, event manager and gas meter of ctx.
func (r *Run) resume(ctx sdk.Context) (sdk.Context, error) {
	db, err := r.stateDB()
	if err != nil {
		return ctx, err
	}
	return db.Ctx().WithContext(ctx.Context()).WithEventManager(ctx.EventManager()).WithGasMeter(ctx.GasMeter()), nil
}

// CustodyAddress is the spot module account, which holds every registered
// token.
func CustodyAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// CustodyEVMAddress is the EVM address of the spot module account, which
// holds every ERC20 token as its ERC20 balance.
func (k *Keeper) CustodyEVMAddress(ctx sdk.Context) common.Address {
	return k.evmKeeper.GetEVMAddressOrDefault(ctx, CustodyAddress())
}

// custodyToken is a registered token with the bank denom of a NATIVE token.
type custodyToken struct {
	types.TokenParams
	Denom string
}

// tokenOf returns the registered token at address token. An address with no
// registered kind, a CW20 pointer, or a NATIVE token that is not the native
// pointer of its denom is refused with UnsupportedTransfer().
func (k *Keeper) tokenOf(ctx sdk.Context, token common.Address) (custodyToken, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return custodyToken{}, err
	}
	pointee, _, isPointer := k.evmKeeper.GetNativePointee(ctx, token.Hex())
	if isPointer {
		if cw20, _, exists := k.evmKeeper.GetERC20CW20Pointer(ctx, pointee); exists && cw20 == token {
			return custodyToken{}, types.ErrUnsupportedTransfer
		}
	}
	for _, registered := range params.Tokens {
		if registered.Address != token {
			continue
		}
		switch registered.Kind {
		case types.TokenKindERC20:
			return custodyToken{TokenParams: registered}, nil
		case types.TokenKindNative:
			if !isPointer {
				return custodyToken{}, types.ErrUnsupportedTransfer
			}
			if pointer, _, exists := k.evmKeeper.GetERC20NativePointer(ctx, pointee); !exists || pointer != token {
				return custodyToken{}, types.ErrUnsupportedTransfer
			}
			return custodyToken{TokenParams: registered, Denom: pointee}, nil
		default:
			return custodyToken{}, types.ErrUnsupportedTransfer
		}
	}
	return custodyToken{}, types.ErrUnsupportedTransfer
}

func isAbort(err error) bool {
	abortErr, ok := err.(vm.AbortError)
	return ok && abortErr.IsAbortError()
}

// call runs input on to with caller as msg.sender: a nested call inside a
// transaction, CallEVM outside one. Only an abort of the whole execution is
// returned bare; every other failure is the call's error.
func (k *Keeper) call(ctx sdk.Context, caller, to common.Address, input []byte) (sdk.Context, []byte, error) {
	run := runOf(ctx)
	if run == nil {
		ret, err := k.evmKeeper.CallEVM(ctx, caller, &to, nil, input)
		return ctx, ret, err
	}
	ret, left, callErr := run.EVM.Call(caller, to, input, run.Gas, uint256.NewInt(0))
	run.Gas = left
	next, err := run.resume(ctx)
	if err != nil {
		return ctx, nil, err
	}
	return next, ret, callErr
}

// staticCall runs input on to read-only with caller as msg.sender.
func (k *Keeper) staticCall(ctx sdk.Context, caller, to common.Address, input []byte) (sdk.Context, []byte, error) {
	run := runOf(ctx)
	if run == nil {
		ret, err := k.evmKeeper.StaticCallEVM(ctx, k.evmKeeper.GetPaxAddressOrDefault(ctx, caller), &to, input)
		return ctx, ret, err
	}
	ret, left, callErr := run.EVM.StaticCall(caller, to, input, run.Gas)
	run.Gas = left
	next, err := run.resume(ctx)
	if err != nil {
		return ctx, nil, err
	}
	return next, ret, callErr
}

func (k *Keeper) hasCode(ctx sdk.Context, token common.Address) bool {
	if run := runOf(ctx); run != nil {
		return run.EVM.StateDB.GetCodeSize(token) > 0
	}
	return len(k.evmKeeper.GetCode(ctx, token)) > 0
}

// safeCall runs one ERC20 call under the SafeERC20 rule: it fails with
// SafeERC20FailedOperation(token) when the call reverts, returns a first word
// other than 1, or returns nothing from an address without code.
func (k *Keeper) safeCall(ctx sdk.Context, caller, token common.Address, input []byte) (sdk.Context, error) {
	next, ret, err := k.call(ctx, caller, token, input)
	if err != nil {
		if isAbort(err) {
			return next, err
		}
		return next, types.SafeERC20FailedOperation(token)
	}
	if len(ret) == 0 {
		if !k.hasCode(next, token) {
			return next, types.SafeERC20FailedOperation(token)
		}
		return next, nil
	}
	if len(ret) < 32 || new(big.Int).SetBytes(ret[:32]).Cmp(big.NewInt(1)) != 0 {
		return next, types.SafeERC20FailedOperation(token)
	}
	return next, nil
}

// erc20Balance reads token.balanceOf(holder) with the custody EVM address as
// msg.sender.
func (k *Keeper) erc20Balance(ctx sdk.Context, token, holder common.Address) (sdk.Context, sdk.Int, error) {
	next, ret, err := k.staticCall(ctx, k.CustodyEVMAddress(ctx), token,
		append(append([]byte(nil), erc20BalanceOf...), common.LeftPadBytes(holder.Bytes(), 32)...))
	if err != nil {
		if isAbort(err) {
			return next, sdk.Int{}, err
		}
		return next, sdk.Int{}, types.SafeERC20FailedOperation(token)
	}
	if len(ret) != 32 {
		return next, sdk.Int{}, types.SafeERC20FailedOperation(token)
	}
	return next, sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret)), nil
}

func (k *Keeper) bankBalance(ctx sdk.Context, holder sdk.AccAddress, denom string) sdk.Int {
	return k.bankKeeper.GetBalance(ctx, holder, denom).Amount
}

func amountWord(amount sdk.Int) []byte { return common.LeftPadBytes(amount.BigInt().Bytes(), 32) }

func checkAmount(amount sdk.Int) error {
	if amount.IsNil() || amount.IsNegative() || amount.BigInt().BitLen() > 256 {
		return fmt.Errorf("%w: spot amount must be a uint256 integer", sdkerrors.ErrInvalidRequest)
	}
	return nil
}

// atomically runs fn so that a failure leaves no state change and no event:
// on a cache context outside a transaction, on a snapshot of the EVM state
// inside one.
func (k *Keeper) atomically(ctx sdk.Context, fn func(sdk.Context) (sdk.Context, error)) (sdk.Context, error) {
	parent := ctx.EventManager()
	run := runOf(ctx)
	if run == nil {
		cache, write := ctx.CacheContext()
		if _, err := fn(cache); err != nil {
			return ctx, err
		}
		write()
		parent.EmitEvents(cache.EventManager().Events())
		return ctx, nil
	}
	db, err := run.stateDB()
	if err != nil {
		return ctx, err
	}
	revision := db.Snapshot()
	inner, err := run.resume(ctx.WithEventManager(sdk.NewEventManager()))
	if err != nil {
		return ctx, err
	}
	next, err := fn(inner)
	if err != nil {
		db.RevertToSnapshot(revision)
		restored, resumeErr := run.resume(ctx)
		if resumeErr != nil {
			return ctx, resumeErr
		}
		return restored, err
	}
	parent.EmitEvents(next.EventManager().Events())
	return next.WithEventManager(parent), nil
}

// pullTokens moves amount of token from from into custody, called by the
// rail, and returns the amount custody received. An ERC20 token is pulled by
// transferFrom(from, custody, amount) with the rail as msg.sender, spending
// from's allowance to the rail, and credited by the rise of the custody
// balance; a NATIVE token is a bank send from from to the module account.
func (k *Keeper) pullTokens(ctx sdk.Context, rail, token, from common.Address, amount sdk.Int) (sdk.Context, sdk.Int, error) {
	if err := checkAmount(amount); err != nil {
		return ctx, sdk.Int{}, err
	}
	registered, err := k.tokenOf(ctx, token)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if amount.IsZero() {
		return ctx, sdk.Int{}, types.ErrZeroAmount
	}
	received := sdk.ZeroInt()
	next, err := k.atomically(ctx, func(ctx sdk.Context) (sdk.Context, error) {
		if registered.Kind == types.TokenKindNative {
			coins := sdk.NewCoins(sdk.NewCoin(registered.Denom, amount))
			if err := k.bankKeeper.SendCoins(ctx, k.evmKeeper.GetPaxAddressOrDefault(ctx, from), CustodyAddress(), coins); err != nil {
				return ctx, types.SafeERC20FailedOperation(token)
			}
			received = amount
			return ctx, nil
		}
		custody := k.CustodyEVMAddress(ctx)
		ctx, before, err := k.erc20Balance(ctx, token, custody)
		if err != nil {
			return ctx, err
		}
		input := append(append([]byte(nil), erc20TransferFrom...), common.LeftPadBytes(from.Bytes(), 32)...)
		input = append(append(input, common.LeftPadBytes(custody.Bytes(), 32)...), amountWord(amount)...)
		if ctx, err = k.safeCall(ctx, rail, token, input); err != nil {
			return ctx, err
		}
		ctx, after, err := k.erc20Balance(ctx, token, custody)
		if err != nil {
			return ctx, err
		}
		if !after.GT(before) {
			return ctx, types.ErrZeroAmount
		}
		received = after.Sub(before)
		return ctx, nil
	})
	if err != nil {
		return next, sdk.Int{}, err
	}
	return next, received, nil
}

// pushTokens pays exactly amount of token from custody to to: transfer(to,
// amount) from the custody EVM address for an ERC20 token, a bank send from
// the module address for a NATIVE token. Both the custody and the recipient
// balance must move by exactly amount, and a payout to custody itself is
// refused, each with UnsupportedTransfer().
func (k *Keeper) pushTokens(ctx sdk.Context, token, to common.Address, amount sdk.Int) (sdk.Context, error) {
	if err := checkAmount(amount); err != nil {
		return ctx, err
	}
	registered, err := k.tokenOf(ctx, token)
	if err != nil {
		return ctx, err
	}
	if to == (common.Address{}) {
		return ctx, types.ErrZeroAddress
	}
	custody := k.CustodyEVMAddress(ctx)
	recipient := k.evmKeeper.GetPaxAddressOrDefault(ctx, to)
	if to == custody || recipient.Equals(CustodyAddress()) {
		return ctx, types.ErrUnsupportedTransfer
	}
	return k.atomically(ctx, func(ctx sdk.Context) (sdk.Context, error) {
		if registered.Kind == types.TokenKindNative {
			beforeCustody := k.bankBalance(ctx, CustodyAddress(), registered.Denom)
			beforeRecipient := k.bankBalance(ctx, recipient, registered.Denom)
			coins := sdk.NewCoins(sdk.NewCoin(registered.Denom, amount))
			if err := k.bankKeeper.SendCoins(ctx, CustodyAddress(), recipient, coins); err != nil {
				return ctx, types.SafeERC20FailedOperation(token)
			}
			if !exactDeltas(beforeCustody, k.bankBalance(ctx, CustodyAddress(), registered.Denom),
				beforeRecipient, k.bankBalance(ctx, recipient, registered.Denom), amount) {
				return ctx, types.ErrUnsupportedTransfer
			}
			return ctx, nil
		}
		ctx, beforeCustody, err := k.erc20Balance(ctx, token, custody)
		if err != nil {
			return ctx, err
		}
		ctx, beforeRecipient, err := k.erc20Balance(ctx, token, to)
		if err != nil {
			return ctx, err
		}
		input := append(append([]byte(nil), erc20Transfer...), common.LeftPadBytes(to.Bytes(), 32)...)
		if ctx, err = k.safeCall(ctx, custody, token, append(input, amountWord(amount)...)); err != nil {
			return ctx, err
		}
		ctx, afterCustody, err := k.erc20Balance(ctx, token, custody)
		if err != nil {
			return ctx, err
		}
		ctx, afterRecipient, err := k.erc20Balance(ctx, token, to)
		if err != nil {
			return ctx, err
		}
		if !exactDeltas(beforeCustody, afterCustody, beforeRecipient, afterRecipient, amount) {
			return ctx, types.ErrUnsupportedTransfer
		}
		return ctx, nil
	})
}

func exactDeltas(beforeCustody, afterCustody, beforeRecipient, afterRecipient, amount sdk.Int) bool {
	return !afterCustody.GT(beforeCustody) && beforeCustody.Sub(afterCustody).Equal(amount) &&
		!afterRecipient.LT(beforeRecipient) && afterRecipient.Sub(beforeRecipient).Equal(amount)
}

// CustodyBalance is the custody balance of a registered token: balanceOf of
// the custody EVM address for an ERC20 token, the module bank balance of its
// denom for a NATIVE token.
func (k *Keeper) CustodyBalance(ctx sdk.Context, token common.Address) (sdk.Context, sdk.Int, error) {
	registered, err := k.tokenOf(ctx, token)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if registered.Kind == types.TokenKindNative {
		return ctx, k.bankBalance(ctx, CustodyAddress(), registered.Denom), nil
	}
	return k.erc20Balance(ctx, token, k.CustodyEVMAddress(ctx))
}

// pullNative moves a value in wei attached to a call of the rail into
// custody as uhpx at value / 1e12 through the native pointer of the base
// denom. A zero value is refused with ZeroAmount() and a value with a wei
// remainder, or a receipt other than the converted amount, with
// UnsupportedInputTransfer().
func (k *Keeper) pullNative(ctx sdk.Context, rail common.Address, value *big.Int) (sdk.Context, sdk.Int, error) {
	if value == nil || value.Sign() <= 0 {
		return ctx, sdk.Int{}, types.ErrZeroAmount
	}
	uhpx, remainder := new(big.Int).QuoRem(value, weiPerUhpx, new(big.Int))
	if remainder.Sign() != 0 {
		return ctx, sdk.Int{}, types.ErrUnsupportedInputTransfer
	}
	pointer, _, exists := k.evmKeeper.GetERC20NativePointer(ctx, k.evmKeeper.GetBaseDenom(ctx))
	if !exists {
		return ctx, sdk.Int{}, types.ErrUnsupportedTransfer
	}
	amount := sdk.NewIntFromBigInt(uhpx)
	next, received, err := k.pullTokens(ctx, rail, pointer, rail, amount)
	if err != nil {
		return next, sdk.Int{}, err
	}
	if !received.Equal(amount) {
		return next, sdk.Int{}, types.ErrUnsupportedInputTransfer
	}
	return next, received, nil
}

// NonReentrant runs fn under the spot re-entry flag and refuses a spot call
// nested inside another with ReentrancyGuardReentrantCall(). Inside a
// transaction the flag is the transient slot of the PECOR rail, which reverts
// with the frame that set it; outside one it is a context value, which the
// EVM run of CallEVM inherits.
func (k *Keeper) NonReentrant(ctx sdk.Context, fn func(sdk.Context) (sdk.Context, error)) (sdk.Context, error) {
	if ctx.Value(guardKey{}) != nil {
		return ctx, types.ErrReentrancyGuardReentrantCall
	}
	run := runOf(ctx)
	if run == nil {
		next, err := fn(ctx.WithValue(guardKey{}, true))
		return next.WithContext(ctx.Context()), err
	}
	db, err := run.stateDB()
	if err != nil {
		return ctx, err
	}
	if db.GetTransientState(pecorRail, reentrancySlot) != (common.Hash{}) {
		return ctx, types.ErrReentrancyGuardReentrantCall
	}
	db.SetTransientState(pecorRail, reentrancySlot, slotEntered)
	next, err := fn(ctx)
	db.SetTransientState(pecorRail, reentrancySlot, common.Hash{})
	return next, err
}
