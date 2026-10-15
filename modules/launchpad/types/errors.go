package types

import (
	"errors"
	"fmt"
	"strings"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
)

func LaunchpadCode(name string) string {
	return "LXP_LAUNCHPAD_" + name
}

var (
	ErrInvalidParams           = sdkerrors.Register(ModuleName, 2, LaunchpadCode("INVALID_PARAMS"))
	ErrOverflow                = sdkerrors.Register(ModuleName, 3, LaunchpadCode("OVERFLOW"))
	ErrDivisionByZero          = sdkerrors.Register(ModuleName, 4, LaunchpadCode("DIVISION_BY_ZERO"))
	ErrInsufficientInput       = sdkerrors.Register(ModuleName, 5, LaunchpadCode("INSUFFICIENT_INPUT"))
	ErrInsufficientLiquidity   = sdkerrors.Register(ModuleName, 6, LaunchpadCode("INSUFFICIENT_LIQUIDITY"))
	ErrVirtualFloorBreached    = sdkerrors.Register(ModuleName, 7, LaunchpadCode("VIRTUAL_FLOOR_BREACHED"))
	ErrSlippageExceeded        = sdkerrors.Register(ModuleName, 8, LaunchpadCode("SLIPPAGE_EXCEEDED"))
	ErrDeadlineExpired         = sdkerrors.Register(ModuleName, 9, LaunchpadCode("DEADLINE_EXPIRED"))
	ErrZeroAddress             = sdkerrors.Register(ModuleName, 10, LaunchpadCode("ZERO_ADDRESS"))
	ErrZeroAmount              = sdkerrors.Register(ModuleName, 11, LaunchpadCode("ZERO_AMOUNT"))
	ErrUnknownMarket           = sdkerrors.Register(ModuleName, 12, LaunchpadCode("UNKNOWN_MARKET"))
	ErrPaused                  = sdkerrors.Register(ModuleName, 13, LaunchpadCode("PAUSED"))
	ErrNotPaused               = sdkerrors.Register(ModuleName, 14, LaunchpadCode("NOT_PAUSED"))
	ErrNotGuardian             = sdkerrors.Register(ModuleName, 15, LaunchpadCode("NOT_GUARDIAN"))
	ErrNotFeeRightsHolder      = sdkerrors.Register(ModuleName, 16, LaunchpadCode("NOT_FEE_RIGHTS_HOLDER"))
	ErrWrongStrategy           = sdkerrors.Register(ModuleName, 17, LaunchpadCode("WRONG_STRATEGY"))
	ErrInvalidStrategy         = sdkerrors.Register(ModuleName, 18, LaunchpadCode("INVALID_STRATEGY"))
	ErrNoFeesAccumulated       = sdkerrors.Register(ModuleName, 19, LaunchpadCode("NO_FEES_ACCUMULATED"))
	ErrAirdropNotTriggered     = sdkerrors.Register(ModuleName, 20, LaunchpadCode("AIRDROP_NOT_TRIGGERED"))
	ErrAlreadyClaimed          = sdkerrors.Register(ModuleName, 21, LaunchpadCode("ALREADY_CLAIMED"))
	ErrInvalidMarket           = sdkerrors.Register(ModuleName, 22, LaunchpadCode("INVALID_MARKET"))
	ErrUnauthorized            = sdkerrors.Register(ModuleName, 23, LaunchpadCode("UNAUTHORIZED"))
	ErrInvalidGenesis          = sdkerrors.Register(ModuleName, 24, LaunchpadCode("INVALID_GENESIS"))
	ErrHoldingHistory          = sdkerrors.Register(ModuleName, 25, LaunchpadCode("HOLDING_HISTORY"))
	ErrUnsupportedHoldingEpoch = sdkerrors.Register(ModuleName, 26, LaunchpadCode("UNSUPPORTED_HOLDING_EPOCH"))
	ErrInvalidAirdropEpoch     = sdkerrors.Register(ModuleName, 27, LaunchpadCode("INVALID_AIRDROP_EPOCH"))
	ErrLegacyAirdropEpoch      = sdkerrors.Register(ModuleName, 28, LaunchpadCode("LEGACY_AIRDROP_EPOCH"))
	ErrAirdropBasis            = sdkerrors.Register(ModuleName, 29, LaunchpadCode("AIRDROP_BASIS"))
	ErrInvalidRange            = sdkerrors.Register(ModuleName, 30, LaunchpadCode("INVALID_RANGE"))

	ErrAlreadyInitialized               = sdkerrors.Register(ModuleName, 31, LaunchpadCode("ALREADY_INITIALIZED"))
	ErrAlreadyMinted                    = sdkerrors.Register(ModuleName, 32, LaunchpadCode("ALREADY_MINTED"))
	ErrApproveFailed                    = sdkerrors.Register(ModuleName, 33, LaunchpadCode("APPROVE_FAILED"))
	ErrDuplicatePool                    = sdkerrors.Register(ModuleName, 34, LaunchpadCode("DUPLICATE_POOL"))
	ErrDuplicateToken                   = sdkerrors.Register(ModuleName, 35, LaunchpadCode("DUPLICATE_TOKEN"))
	ErrFeeOutOfRange                    = sdkerrors.Register(ModuleName, 36, LaunchpadCode("FEE_OUT_OF_RANGE"))
	ErrIndexOutOfBounds                 = sdkerrors.Register(ModuleName, 37, LaunchpadCode("INDEX_OUT_OF_BOUNDS"))
	ErrInsufficientBalance              = sdkerrors.Register(ModuleName, 38, LaunchpadCode("INSUFFICIENT_BALANCE"))
	ErrInsufficientCreationFee          = sdkerrors.Register(ModuleName, 39, LaunchpadCode("INSUFFICIENT_CREATION_FEE"))
	ErrInsufficientSurplus              = sdkerrors.Register(ModuleName, 40, LaunchpadCode("INSUFFICIENT_SURPLUS"))
	ErrInvalidBalance                   = sdkerrors.Register(ModuleName, 41, LaunchpadCode("INVALID_BALANCE"))
	ErrInvalidConfiguration             = sdkerrors.Register(ModuleName, 42, LaunchpadCode("INVALID_CONFIGURATION"))
	ErrInvalidHookResponse              = sdkerrors.Register(ModuleName, 43, LaunchpadCode("INVALID_HOOK_RESPONSE"))
	ErrInvalidImplementation            = sdkerrors.Register(ModuleName, 44, LaunchpadCode("INVALID_IMPLEMENTATION"))
	ErrInvalidRoleAccount               = sdkerrors.Register(ModuleName, 45, LaunchpadCode("INVALID_ROLE_ACCOUNT"))
	ErrLastDefaultAdmin                 = sdkerrors.Register(ModuleName, 46, LaunchpadCode("LAST_DEFAULT_ADMIN"))
	ErrMissingRole                      = sdkerrors.Register(ModuleName, 47, LaunchpadCode("MISSING_ROLE"))
	ErrMulticallFailed                  = sdkerrors.Register(ModuleName, 48, LaunchpadCode("MULTICALL_FAILED"))
	ErrNonERC721Receiver                = sdkerrors.Register(ModuleName, 49, LaunchpadCode("NON_ERC721_RECEIVER"))
	ErrNonPayableUpgrade                = sdkerrors.Register(ModuleName, 50, LaunchpadCode("NON_PAYABLE_UPGRADE"))
	ErrNotApproved                      = sdkerrors.Register(ModuleName, 51, LaunchpadCode("NOT_APPROVED"))
	ErrNotInitializing                  = sdkerrors.Register(ModuleName, 52, LaunchpadCode("NOT_INITIALIZING"))
	ErrNotTokenOwner                    = sdkerrors.Register(ModuleName, 53, LaunchpadCode("NOT_TOKEN_OWNER"))
	ErrOpticalQuoteUnsupported          = sdkerrors.Register(ModuleName, 54, LaunchpadCode("OPTICAL_QUOTE_UNSUPPORTED"))
	ErrPoolIndexMigrationRequired       = sdkerrors.Register(ModuleName, 55, LaunchpadCode("POOL_INDEX_MIGRATION_REQUIRED"))
	ErrReentrancyGuardReentrantCall     = sdkerrors.Register(ModuleName, 56, LaunchpadCode("REENTRANCY_GUARD_REENTRANT_CALL"))
	ErrSameToken                        = sdkerrors.Register(ModuleName, 57, LaunchpadCode("SAME_TOKEN"))
	ErrTokenNotFound                    = sdkerrors.Register(ModuleName, 58, LaunchpadCode("TOKEN_NOT_FOUND"))
	ErrTransferFailed                   = sdkerrors.Register(ModuleName, 59, LaunchpadCode("TRANSFER_FAILED"))
	ErrTransferFromFailed               = sdkerrors.Register(ModuleName, 60, LaunchpadCode("TRANSFER_FROM_FAILED"))
	ErrUUPSNotThroughActiveProxy        = sdkerrors.Register(ModuleName, 61, LaunchpadCode("UUPS_NOT_THROUGH_ACTIVE_PROXY"))
	ErrUUPSNotThroughProxy              = sdkerrors.Register(ModuleName, 62, LaunchpadCode("UUPS_NOT_THROUGH_PROXY"))
	ErrUnauthorizedUpgrade              = sdkerrors.Register(ModuleName, 63, LaunchpadCode("UNAUTHORIZED_UPGRADE"))
	ErrUnexecutableQuote                = sdkerrors.Register(ModuleName, 64, LaunchpadCode("UNEXECUTABLE_QUOTE"))
	ErrUnsupportedSnapshotToken         = sdkerrors.Register(ModuleName, 65, LaunchpadCode("UNSUPPORTED_SNAPSHOT_TOKEN"))
	ErrUpgradeInitializationFailed      = sdkerrors.Register(ModuleName, 66, LaunchpadCode("UPGRADE_INITIALIZATION_FAILED"))
	ErrInvalidToken                     = sdkerrors.Register(ModuleName, 67, LaunchpadCode("INVALID_TOKEN"))
	ErrReentrantCall                    = sdkerrors.Register(ModuleName, 68, LaunchpadCode("REENTRANT_CALL"))
	ErrAccessControlUnauthorizedAccount = sdkerrors.Register(ModuleName, 69, LaunchpadCode("ACCESS_CONTROL_UNAUTHORIZED_ACCOUNT"))
	ErrRetired                          = sdkerrors.Register(ModuleName, 70, LaunchpadCode("RETIRED"))
)

// ContractError is the Launch-x custom error a launchpad keeper error reverts
// with at a contract surface: its selector and the ABI arguments it carries.
type ContractError struct {
	Err      *sdkerrors.Error
	Name     string
	Selector [4]byte
	Inputs   abi.Arguments
}

// Signature is the canonical error signature the selector hashes.
func (c ContractError) Signature() string {
	types := make([]string, len(c.Inputs))
	for i, input := range c.Inputs {
		types[i] = input.Type.String()
	}
	return c.Name + "(" + strings.Join(types, ",") + ")"
}

// Revert is the EVM revert data of the custom error: the selector followed by
// the ABI encoding of values, one per input.
func (c ContractError) Revert(values ...interface{}) ([]byte, error) {
	data, err := c.Inputs.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s arguments: %w", ErrInvalidParams, c.Signature(), err)
	}
	revert := make([]byte, 0, len(c.Selector)+len(data))
	revert = append(revert, c.Selector[:]...)
	return append(revert, data...), nil
}

// ContractErrorOf is the custom error of the launchpad keeper error err wraps.
func ContractErrorOf(err error) (ContractError, bool) {
	for _, contractError := range contractErrors {
		if errors.Is(err, contractError.Err) {
			return contractError, true
		}
	}
	return ContractError{}, false
}

// newContractError builds the table entry once at package initialisation; its
// inputs are constant Solidity type names, so a parse failure is a programming
// error and stops the binary as sdkerrors.Register does for duplicate codes.
func newContractError(err *sdkerrors.Error, name string, inputs ...string) ContractError {
	args := make(abi.Arguments, len(inputs))
	for i, input := range inputs {
		typ, typeErr := abi.NewType(input, "", nil)
		if typeErr != nil {
			panic(fmt.Sprintf("launchpad contract error %s: %v", name, typeErr))
		}
		args[i] = abi.Argument{Type: typ}
	}
	var selector [4]byte
	copy(selector[:], crypto.Keccak256([]byte(name+"("+strings.Join(inputs, ",")+")")))
	return ContractError{Err: err, Name: name, Selector: selector, Inputs: args}
}

var contractErrors = []ContractError{
	newContractError(ErrInvalidParams, "FeeOutOfRange"),
	newContractError(ErrOverflow, "Overflow"),
	newContractError(ErrDivisionByZero, "DivisionByZero"),
	newContractError(ErrInsufficientInput, "InsufficientInput"),
	newContractError(ErrInsufficientLiquidity, "InsufficientLiquidity"),
	newContractError(ErrVirtualFloorBreached, "VirtualFloorBreached"),
	newContractError(ErrSlippageExceeded, "SlippageExceeded"),
	newContractError(ErrDeadlineExpired, "DeadlineExpired"),
	newContractError(ErrZeroAddress, "ZeroAddress"),
	newContractError(ErrZeroAmount, "ZeroAmount"),
	newContractError(ErrUnknownMarket, "PoolNotFound"),
	newContractError(ErrPaused, "Paused"),
	newContractError(ErrNotPaused, "NotPaused"),
	newContractError(ErrNotGuardian, "Unauthorized"),
	newContractError(ErrNotFeeRightsHolder, "NotNftOwner"),
	newContractError(ErrWrongStrategy, "WrongStrategy"),
	newContractError(ErrInvalidStrategy, "InvalidStrategy"),
	newContractError(ErrNoFeesAccumulated, "NoFeesAccumulated"),
	newContractError(ErrAirdropNotTriggered, "AirdropNotTriggered"),
	newContractError(ErrAlreadyClaimed, "AlreadyClaimed"),
	newContractError(ErrInvalidMarket, "InvalidConfiguration"),
	newContractError(ErrUnauthorized, "Unauthorized"),
	newContractError(ErrInvalidGenesis, "InvalidConfiguration"),
	newContractError(ErrHoldingHistory, "UnsupportedSnapshotToken"),
	newContractError(ErrUnsupportedHoldingEpoch, "UnsupportedSnapshotToken"),
	newContractError(ErrInvalidAirdropEpoch, "AirdropNotTriggered"),
	newContractError(ErrLegacyAirdropEpoch, "UnsupportedSnapshotToken"),
	newContractError(ErrAirdropBasis, "UnsupportedSnapshotToken"),
	newContractError(ErrInvalidRange, "IndexOutOfBounds"),
	newContractError(ErrAlreadyInitialized, "AlreadyInitialized"),
	newContractError(ErrAlreadyMinted, "AlreadyMinted"),
	newContractError(ErrApproveFailed, "ApproveFailed"),
	newContractError(ErrDuplicatePool, "DuplicatePool"),
	newContractError(ErrDuplicateToken, "DuplicateToken"),
	newContractError(ErrFeeOutOfRange, "FeeOutOfRange"),
	newContractError(ErrIndexOutOfBounds, "IndexOutOfBounds"),
	newContractError(ErrInsufficientBalance, "InsufficientBalance"),
	newContractError(ErrInsufficientCreationFee, "InsufficientCreationFee"),
	newContractError(ErrInsufficientSurplus, "InsufficientSurplus"),
	newContractError(ErrInvalidBalance, "InvalidBalance"),
	newContractError(ErrInvalidConfiguration, "InvalidConfiguration"),
	newContractError(ErrInvalidHookResponse, "InvalidHookResponse"),
	newContractError(ErrInvalidImplementation, "InvalidImplementation"),
	newContractError(ErrInvalidRoleAccount, "InvalidRoleAccount"),
	newContractError(ErrLastDefaultAdmin, "LastDefaultAdmin"),
	newContractError(ErrMissingRole, "MissingRole", "address", "bytes32"),
	newContractError(ErrMulticallFailed, "MulticallFailed", "uint256", "bytes"),
	newContractError(ErrNonERC721Receiver, "NonERC721Receiver"),
	newContractError(ErrNonPayableUpgrade, "NonPayableUpgrade"),
	newContractError(ErrNotApproved, "NotApproved"),
	newContractError(ErrNotInitializing, "NotInitializing"),
	newContractError(ErrNotTokenOwner, "NotTokenOwner"),
	newContractError(ErrOpticalQuoteUnsupported, "OpticalQuoteUnsupported"),
	newContractError(ErrPoolIndexMigrationRequired, "PoolIndexMigrationRequired"),
	newContractError(ErrReentrancyGuardReentrantCall, "ReentrancyGuardReentrantCall"),
	newContractError(ErrSameToken, "SameToken"),
	newContractError(ErrTokenNotFound, "TokenNotFound"),
	newContractError(ErrTransferFailed, "TransferFailed"),
	newContractError(ErrTransferFromFailed, "TransferFromFailed"),
	newContractError(ErrUUPSNotThroughActiveProxy, "UUPSNotThroughActiveProxy"),
	newContractError(ErrUUPSNotThroughProxy, "UUPSNotThroughProxy"),
	newContractError(ErrUnauthorizedUpgrade, "UnauthorizedUpgrade"),
	newContractError(ErrUnexecutableQuote, "UnexecutableQuote"),
	newContractError(ErrUnsupportedSnapshotToken, "UnsupportedSnapshotToken"),
	newContractError(ErrUpgradeInitializationFailed, "UpgradeInitializationFailed"),
	newContractError(ErrInvalidToken, "InvalidToken"),
	newContractError(ErrReentrantCall, "ReentrantCall"),
	newContractError(ErrAccessControlUnauthorizedAccount, "AccessControlUnauthorizedAccount", "address", "bytes32"),
	newContractError(ErrRetired, "Retired", "address"),
}
