package types

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// AbiError is a spot error that an EVM caller receives as ABI custom error
// data: the 4-byte selector of its Solidity signature followed by its packed
// arguments. Each one is also a registered module error, so it carries a
// codespace and code when it fails a Cosmos message.
type AbiError struct {
	code      *sdkerrors.Error
	signature string
	selector  [4]byte
	args      []byte
}

func newAbiError(code uint32, signature string) *AbiError {
	e := &AbiError{code: sdkerrors.Register(ModuleName, code, signature), signature: signature}
	copy(e.selector[:], crypto.Keccak256([]byte(signature))[:4])
	return e
}

// with returns a copy of e carrying the packed static arguments words.
func (e *AbiError) with(words ...[32]byte) *AbiError {
	out := *e
	out.args = make([]byte, 0, 32*len(words))
	for _, word := range words {
		out.args = append(out.args, word[:]...)
	}
	return &out
}

func addressWord(address common.Address) [32]byte {
	var word [32]byte
	copy(word[12:], address.Bytes())
	return word
}

func (e *AbiError) Error() string { return e.code.Error() }

// Unwrap exposes the registered module error, so errors.Is and the ABCI
// code mapping see the codespace and code.
func (e *AbiError) Unwrap() error { return e.code }

// Is matches any AbiError of the same selector, whatever its arguments.
func (e *AbiError) Is(target error) bool {
	other, ok := target.(*AbiError)
	return ok && other.selector == e.selector
}

// Signature is the canonical Solidity signature, for example
// "OwnableUnauthorizedAccount(address)".
func (e *AbiError) Signature() string { return e.signature }

// Selector is the first four bytes of keccak256 of the signature.
func (e *AbiError) Selector() [4]byte { return e.selector }

// Data is the revert data: the selector followed by the packed arguments.
func (e *AbiError) Data() []byte {
	out := make([]byte, 0, 4+len(e.args))
	out = append(out, e.selector[:]...)
	return append(out, e.args...)
}

// ErrInvalidGenesis is a module error with no ABI form: a genesis state that
// breaks a storage invariant.
var ErrInvalidGenesis = sdkerrors.Register(ModuleName, 2, "invalid genesis")

// The errors of contracts/spot.md#errors, Added error entries included. An
// error with arguments is the template that its constructor fills.
var (
	ErrCannotCancel                 = newAbiError(3, "CannotCancel()")
	ErrERC20InsufficientAllowance   = newAbiError(4, "ERC20InsufficientAllowance(address,uint256,uint256)")
	ErrERC20InsufficientBalance     = newAbiError(5, "ERC20InsufficientBalance(address,uint256,uint256)")
	ErrERC20InvalidApprover         = newAbiError(6, "ERC20InvalidApprover(address)")
	ErrERC20InvalidReceiver         = newAbiError(7, "ERC20InvalidReceiver(address)")
	ErrERC20InvalidSender           = newAbiError(8, "ERC20InvalidSender(address)")
	ErrERC20InvalidSpender          = newAbiError(9, "ERC20InvalidSpender(address)")
	ErrEnforcedPause                = newAbiError(10, "EnforcedPause()")
	ErrExcessiveInput               = newAbiError(11, "ExcessiveInput()")
	ErrExpectedPause                = newAbiError(12, "ExpectedPause()")
	ErrExpired                      = newAbiError(13, "Expired()")
	ErrFeeExceedsMax                = newAbiError(14, "FeeExceedsMax()")
	ErrFeeTooHigh                   = newAbiError(15, "FeeTooHigh()")
	ErrInsufficientLiquidity        = newAbiError(16, "InsufficientLiquidity()")
	ErrInsufficientOutput           = newAbiError(17, "InsufficientOutput()")
	ErrInvalidArrayLength           = newAbiError(18, "InvalidArrayLength()")
	ErrInvalidExpiry                = newAbiError(19, "InvalidExpiry()")
	ErrInvalidOracle                = newAbiError(20, "InvalidOracle()")
	ErrInvalidPrice                 = newAbiError(21, "InvalidPrice()")
	ErrInvalidPrices                = newAbiError(22, "InvalidPrices()")
	ErrInvalidTWAPPeriod            = newAbiError(23, "InvalidTWAPPeriod()")
	ErrInvalidVault                 = newAbiError(24, "InvalidVault()")
	ErrLimitMustBeAboveStopBuy      = newAbiError(25, "LimitMustBeAboveStopBuy()")
	ErrLimitMustBeBelowStopSell     = newAbiError(26, "LimitMustBeBelowStopSell()")
	ErrMulticallFailed              = newAbiError(27, "MulticallFailed(uint256)")
	ErrNativeTransferFailed         = newAbiError(28, "NativeTransferFailed()")
	ErrNoFeesToCollect              = newAbiError(29, "NoFeesToCollect()")
	ErrNotAStablecoin               = newAbiError(30, "NotAStablecoin()")
	ErrNotActivated                 = newAbiError(31, "NotActivated()")
	ErrNotAuthorized                = newAbiError(32, "NotAuthorized()")
	ErrNotFeeCollector              = newAbiError(33, "NotFeeCollector()")
	ErrNotOrderOwner                = newAbiError(34, "NotOrderOwner()")
	ErrNotPending                   = newAbiError(35, "NotPending()")
	ErrOrderBelowMinimumNotional    = newAbiError(36, "OrderBelowMinimumNotional()")
	ErrOrderExpired                 = newAbiError(37, "OrderExpired()")
	ErrOrderNotPending              = newAbiError(38, "OrderNotPending()")
	ErrOwnableInvalidOwner          = newAbiError(39, "OwnableInvalidOwner(address)")
	ErrOwnableUnauthorizedAccount   = newAbiError(40, "OwnableUnauthorizedAccount(address)")
	ErrPriceAboveLimit              = newAbiError(41, "PriceAboveLimit()")
	ErrPriceAboveTarget             = newAbiError(42, "PriceAboveTarget()")
	ErrPriceAboveTrigger            = newAbiError(43, "PriceAboveTrigger()")
	ErrPriceBelowLimit              = newAbiError(44, "PriceBelowLimit()")
	ErrPriceBelowTarget             = newAbiError(45, "PriceBelowTarget()")
	ErrReentrancyGuardReentrantCall = newAbiError(46, "ReentrancyGuardReentrantCall()")
	ErrSafeERC20FailedOperation     = newAbiError(47, "SafeERC20FailedOperation(address)")
	ErrSameToken                    = newAbiError(48, "SameToken()")
	ErrScalarTooHigh                = newAbiError(49, "ScalarTooHigh()")
	ErrStalePrice                   = newAbiError(50, "StalePrice()")
	ErrTier2BelowTier1              = newAbiError(51, "Tier2BelowTier1()")
	ErrTokenAlreadyRegistered       = newAbiError(52, "TokenAlreadyRegistered()")
	ErrTokenIsStablecoin            = newAbiError(53, "TokenIsStablecoin()")
	ErrTokenNotRegistered           = newAbiError(54, "TokenNotRegistered()")
	ErrTokenPaused                  = newAbiError(55, "TokenPaused()")
	ErrTooManyActiveOrders          = newAbiError(56, "TooManyActiveOrders()")
	ErrTwapWindowInvalid            = newAbiError(57, "TwapWindowInvalid()")
	ErrUnsupportedInputTransfer     = newAbiError(58, "UnsupportedInputTransfer()")
	ErrUnsupportedToken             = newAbiError(59, "UnsupportedToken()")
	ErrUnsupportedTransfer          = newAbiError(60, "UnsupportedTransfer()")
	ErrUseWethDeposit               = newAbiError(61, "UseWethDeposit()")
	ErrUseWethWithdraw              = newAbiError(62, "UseWethWithdraw()")
	ErrWethOnly                     = newAbiError(63, "WethOnly()")
	ErrZeroAddress                  = newAbiError(64, "ZeroAddress()")
	ErrZeroAmount                   = newAbiError(65, "ZeroAmount()")
)

func ERC20InsufficientAllowance(spender common.Address, allowance, needed *uint256.Int) *AbiError {
	return ErrERC20InsufficientAllowance.with(addressWord(spender), allowance.Bytes32(), needed.Bytes32())
}

func ERC20InsufficientBalance(sender common.Address, balance, needed *uint256.Int) *AbiError {
	return ErrERC20InsufficientBalance.with(addressWord(sender), balance.Bytes32(), needed.Bytes32())
}

func ERC20InvalidApprover(approver common.Address) *AbiError {
	return ErrERC20InvalidApprover.with(addressWord(approver))
}

func ERC20InvalidReceiver(receiver common.Address) *AbiError {
	return ErrERC20InvalidReceiver.with(addressWord(receiver))
}

func ERC20InvalidSender(sender common.Address) *AbiError {
	return ErrERC20InvalidSender.with(addressWord(sender))
}

func ERC20InvalidSpender(spender common.Address) *AbiError {
	return ErrERC20InvalidSpender.with(addressWord(spender))
}

func MulticallFailed(index *uint256.Int) *AbiError {
	return ErrMulticallFailed.with(index.Bytes32())
}

func OwnableInvalidOwner(owner common.Address) *AbiError {
	return ErrOwnableInvalidOwner.with(addressWord(owner))
}

func OwnableUnauthorizedAccount(account common.Address) *AbiError {
	return ErrOwnableUnauthorizedAccount.with(addressWord(account))
}

func SafeERC20FailedOperation(token common.Address) *AbiError {
	return ErrSafeERC20FailedOperation.with(addressWord(token))
}
