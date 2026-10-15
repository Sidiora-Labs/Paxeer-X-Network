package launchpad

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

// ERC721ReceivedSelector is IERC721Receiver.onERC721Received.selector, the
// only return value a fee-right receiver hook accepts.
var ERC721ReceivedSelector = [4]byte{0x15, 0x0b, 0x7a, 0x02}

var receiverArguments = func() abi.Arguments {
	address, _ := abi.NewType("address", "", nil)
	word, _ := abi.NewType("uint256", "", nil)
	data, _ := abi.NewType("bytes", "", nil)
	return abi.Arguments{{Type: address}, {Type: address}, {Type: word}, {Type: data}}
}()

// nestedKind selects the revert rule of a nested call.
type nestedKind uint8

const (
	// nestedPlain reverts the precompile call with the nested revert data.
	nestedPlain nestedKind = iota
	// nestedPermit ignores the failure, as Router._executePermit does.
	nestedPermit
	// nestedReceiver follows the onERC721Received rule of ERC721Base.
	nestedReceiver
)

// NestedRevertError is a nested call that failed: the precompile call
// reverts with Data, the revert data of the nested call, unchanged.
type NestedRevertError struct {
	Target common.Address
	Data   []byte
	Err    error
}

func (e *NestedRevertError) Error() string {
	return fmt.Sprintf("launchpad: nested call to %s failed: %v (revert data %x)", e.Target.Hex(), e.Err, e.Data)
}

func (e *NestedRevertError) Unwrap() error { return e.Err }

// Revert is the EVM revert data of the refusal: the nested revert data.
func (e *NestedRevertError) Revert() ([]byte, error) { return append([]byte{}, e.Data...), nil }

// RunCaller serves the keeper's NestedCaller inside one precompile run. The
// keeper writes every change of the run straight into the context of the EVM
// state branch the run executes on, and continues on the context each Call
// returns, so when a nested call snapshots that branch every keeper change of
// the run so far is already written below it and the nested call, and every
// view it makes, reads the committed outer state. Every nested call is
// metered by the EVM out of the gas the run has left.
type RunCaller struct {
	evm *vm.EVM
	gas *uint64
}

var _ launchpadkeeper.NestedCaller = (*RunCaller)(nil)

// NewRunCaller is the nested caller of the run on evm; gas is the gas the
// run has left for nested calls, lowered by what each call uses.
func NewRunCaller(evm *vm.EVM, gas *uint64) (*RunCaller, error) {
	if evm == nil || gas == nil {
		return nil, errors.New("launchpad: nested caller needs the EVM and the gas of the run")
	}
	if state.GetDBImpl(evm.StateDB) == nil {
		return nil, errors.New("launchpad: nested caller needs the EVM state of the run")
	}
	return &RunCaller{evm: evm, gas: gas}, nil
}

// Call runs input on to with caller as msg.sender and no value; a failure
// reverts with the nested revert data.
func (c *RunCaller) Call(ctx sdk.Context, caller common.Address, to common.Address,
	input []byte) (sdk.Context, []byte, error) {
	return c.callNested(ctx, nestedPlain, caller, to, input, false)
}

// StaticCall runs input on to as a staticcall with caller as msg.sender; a
// failure reverts with the nested revert data.
func (c *RunCaller) StaticCall(ctx sdk.Context, caller common.Address, to common.Address,
	input []byte) ([]byte, error) {
	_, ret, err := c.callNested(ctx, nestedPlain, caller, to, input, true)
	return ret, err
}

// Permit calls permit(owner, spender, value, deadline, v, r, s) on token
// with caller as msg.sender and ignores its failure, as the *WithPermit
// variants do (Launch-x:contracts/contracts/periphery/Router.sol:292-314):
// a failed permit leaves no state behind and the following pull refuses when
// the allowance does not cover it.
func (c *RunCaller) Permit(ctx sdk.Context, caller, token, owner, spender common.Address, value, deadline *big.Int,
	v uint8, r, s [32]byte) (sdk.Context, error) {
	input, err := roleABIs["SidioraERC20"].Pack("permit", owner, spender, value, deadline, v, r, s)
	if err != nil {
		return ctx, fmt.Errorf("%w: permit arguments: %w", types.ErrInvalidPermit, err)
	}
	next, _, err := c.callNested(ctx, nestedPermit, caller, token, input, false)
	return next, err
}

// OnERC721Received runs the receiver hook of a fee-right mint or
// safeTransferFrom from collection: a receiver without code is not called;
// otherwise it must return 0x150b7a02, a wrong return value or an empty
// revert gives NonERC721Receiver() and any other revert data is returned
// unchanged (Launch-x:contracts/contracts/base/ERC721Base.sol:81-92,
// Launch-x:contracts/contracts/core/SidioraNFT.sol:71-77).
func (c *RunCaller) OnERC721Received(ctx sdk.Context, collection, operator, from, to common.Address,
	tokenID *big.Int, data []byte) (sdk.Context, error) {
	if c.evm.StateDB.GetCodeSize(to) == 0 {
		return ctx, nil
	}
	packed, err := receiverArguments.Pack(operator, from, tokenID, data)
	if err != nil {
		return ctx, fmt.Errorf("%w: onERC721Received arguments: %w", types.ErrNonERC721Receiver, err)
	}
	input := append(append([]byte{}, ERC721ReceivedSelector[:]...), packed...)
	next, ret, err := c.callNested(ctx, nestedReceiver, collection, to, input, false)
	if err != nil {
		return next, err
	}
	// A return word that is not a bytes4 fails the abi decoding of the
	// collection, which reverts with empty data.
	if len(ret) < 32 || !bytes.Equal(ret[4:32], make([]byte, 28)) {
		return next, &NestedRevertError{Target: to, Data: []byte{},
			Err: fmt.Errorf("%w: onERC721Received of %s returned %x", types.ErrInvalidHookResponse, to.Hex(), ret)}
	}
	if !bytes.Equal(ret[:4], ERC721ReceivedSelector[:]) {
		return next, fmt.Errorf("%w: onERC721Received of %s returned %x", types.ErrNonERC721Receiver, to.Hex(), ret[:4])
	}
	return next, nil
}

// callNested calls to through evm.Call, or evm.StaticCall, as the xweb
// precompile delivers its callback (precompiles/xweb/xweb.go:482), after
// every keeper change of the run so far has been written into the EVM state
// branch the call snapshots. It returns the context the run continues on:
// the branch the EVM continues with after the call, carrying the event
// manager and settings of ctx. Only an abort of the whole execution is
// returned as it is; every other failure follows nestedRevert.
func (c *RunCaller) callNested(ctx sdk.Context, kind nestedKind, caller, to common.Address, input []byte,
	static bool) (sdk.Context, []byte, error) {
	db := state.GetDBImpl(c.evm.StateDB)
	if db == nil {
		return ctx, nil, errors.New("launchpad: nested call outside the EVM state of the run")
	}
	var (
		ret  []byte
		left uint64
		err  error
	)
	if static {
		ret, left, err = c.evm.StaticCall(caller, to, input, *c.gas)
	} else {
		ret, left, err = c.evm.Call(caller, to, input, *c.gas, uint256.NewInt(0))
	}
	*c.gas = left
	if abortErr, ok := err.(vm.AbortError); ok && abortErr.IsAbortError() {
		return ctx, nil, err
	}
	next := ctx.WithMultiStore(db.Ctx().MultiStore())
	if err != nil {
		return next, nil, nestedRevert(kind, to, ret, err)
	}
	return next, ret, nil
}

// nestedRevert is the outcome of a failed nested call: nil for a permit,
// whose failure is ignored; NonERC721Receiver() for a receiver hook that
// reverts with empty data; otherwise the nested revert data unchanged.
func nestedRevert(kind nestedKind, to common.Address, ret []byte, err error) error {
	switch kind {
	case nestedPermit:
		return nil
	case nestedReceiver:
		if len(ret) == 0 {
			return fmt.Errorf("%w: onERC721Received of %s reverted without data: %w", types.ErrNonERC721Receiver,
				to.Hex(), err)
		}
	}
	return &NestedRevertError{Target: to, Data: append([]byte{}, ret...), Err: err}
}
