package types_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestLaunchpadErrorStrings(t *testing.T) {
	caller := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	successor := common.HexToAddress("0x00000000000000000000000000000000000000d2")
	role := [32]byte{0x01, 0x02, 0x03}
	cases := []struct {
		err       *sdkerrors.Error
		code      uint32
		reason    string
		signature string
		selector  string
		args      []interface{}
	}{
		{types.ErrInvalidParams, 2, "LXP_LAUNCHPAD_INVALID_PARAMS", "", "", nil},
		{types.ErrOverflow, 3, "LXP_LAUNCHPAD_OVERFLOW", "Overflow()", "0x35278d12", nil},
		{types.ErrDivisionByZero, 4, "LXP_LAUNCHPAD_DIVISION_BY_ZERO", "DivisionByZero()", "0x23d359a3", nil},
		{types.ErrInsufficientInput, 5, "LXP_LAUNCHPAD_INSUFFICIENT_INPUT", "InsufficientInput()", "0xf8b3bb61", nil},
		{types.ErrInsufficientLiquidity, 6, "LXP_LAUNCHPAD_INSUFFICIENT_LIQUIDITY", "InsufficientLiquidity()", "0xbb55fd27", nil},
		{types.ErrVirtualFloorBreached, 7, "LXP_LAUNCHPAD_VIRTUAL_FLOOR_BREACHED", "VirtualFloorBreached()", "0x04af467f", nil},
		{types.ErrSlippageExceeded, 8, "LXP_LAUNCHPAD_SLIPPAGE_EXCEEDED", "SlippageExceeded()", "0x8199f5f3", nil},
		{types.ErrDeadlineExpired, 9, "LXP_LAUNCHPAD_DEADLINE_EXPIRED", "DeadlineExpired()", "0x1ab7da6b", nil},
		{types.ErrZeroAddress, 10, "LXP_LAUNCHPAD_ZERO_ADDRESS", "ZeroAddress()", "0xd92e233d", nil},
		{types.ErrZeroAmount, 11, "LXP_LAUNCHPAD_ZERO_AMOUNT", "ZeroAmount()", "0x1f2a2005", nil},
		{types.ErrUnknownMarket, 12, "LXP_LAUNCHPAD_UNKNOWN_MARKET", "PoolNotFound()", "0x76ecffc0", nil},
		{types.ErrPaused, 13, "LXP_LAUNCHPAD_PAUSED", "Paused()", "0x9e87fac8", nil},
		{types.ErrNotPaused, 14, "LXP_LAUNCHPAD_NOT_PAUSED", "NotPaused()", "0x6cd60201", nil},
		{types.ErrNotGuardian, 15, "LXP_LAUNCHPAD_NOT_GUARDIAN", "Unauthorized()", "0x82b42900", nil},
		{types.ErrNotFeeRightsHolder, 16, "LXP_LAUNCHPAD_NOT_FEE_RIGHTS_HOLDER", "NotNftOwner()", "0x4b64ae47", nil},
		{types.ErrWrongStrategy, 17, "LXP_LAUNCHPAD_WRONG_STRATEGY", "WrongStrategy()", "0xe181295c", nil},
		{types.ErrInvalidStrategy, 18, "LXP_LAUNCHPAD_INVALID_STRATEGY", "InvalidStrategy()", "0x4e236e9a", nil},
		{types.ErrNoFeesAccumulated, 19, "LXP_LAUNCHPAD_NO_FEES_ACCUMULATED", "NoFeesAccumulated()", "0xbe6fbbaf", nil},
		{types.ErrAirdropNotTriggered, 20, "LXP_LAUNCHPAD_AIRDROP_NOT_TRIGGERED", "AirdropNotTriggered()", "0xceadfa6d", nil},
		{types.ErrAlreadyClaimed, 21, "LXP_LAUNCHPAD_ALREADY_CLAIMED", "AlreadyClaimed()", "0x646cf558", nil},
		{types.ErrInvalidMarket, 22, "LXP_LAUNCHPAD_INVALID_MARKET", "", "", nil},
		{types.ErrUnauthorized, 23, "LXP_LAUNCHPAD_UNAUTHORIZED", "Unauthorized()", "0x82b42900", nil},
		{types.ErrInvalidGenesis, 24, "LXP_LAUNCHPAD_INVALID_GENESIS", "", "", nil},
		{types.ErrHoldingHistory, 25, "LXP_LAUNCHPAD_HOLDING_HISTORY", "", "", nil},
		{types.ErrUnsupportedHoldingEpoch, 26, "LXP_LAUNCHPAD_UNSUPPORTED_HOLDING_EPOCH", "", "", nil},
		{types.ErrInvalidAirdropEpoch, 27, "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH", "AirdropNotTriggered()", "0xceadfa6d", nil},
		{types.ErrLegacyAirdropEpoch, 28, "LXP_LAUNCHPAD_LEGACY_AIRDROP_EPOCH", "AirdropNotTriggered()", "0xceadfa6d", nil},
		{types.ErrAirdropBasis, 29, "LXP_LAUNCHPAD_AIRDROP_BASIS", "", "", nil},
		{types.ErrInvalidRange, 30, "LXP_LAUNCHPAD_INVALID_RANGE", "", "", nil},
		{types.ErrAlreadyInitialized, 31, "LXP_LAUNCHPAD_ALREADY_INITIALIZED", "AlreadyInitialized()", "0x0dc149f0", nil},
		{types.ErrAlreadyMinted, 32, "LXP_LAUNCHPAD_ALREADY_MINTED", "AlreadyMinted()", "0xddefae28", nil},
		{types.ErrApproveFailed, 33, "LXP_LAUNCHPAD_APPROVE_FAILED", "ApproveFailed()", "0x3e3f8f73", nil},
		{types.ErrDuplicatePool, 34, "LXP_LAUNCHPAD_DUPLICATE_POOL", "DuplicatePool()", "0xb82a4746", nil},
		{types.ErrDuplicateToken, 35, "LXP_LAUNCHPAD_DUPLICATE_TOKEN", "DuplicateToken()", "0x464e3f6a", nil},
		{types.ErrFeeOutOfRange, 36, "LXP_LAUNCHPAD_FEE_OUT_OF_RANGE", "FeeOutOfRange()", "0x0c8249ad", nil},
		{types.ErrIndexOutOfBounds, 37, "LXP_LAUNCHPAD_INDEX_OUT_OF_BOUNDS", "IndexOutOfBounds()", "0x4e23d035", nil},
		{types.ErrInsufficientBalance, 38, "LXP_LAUNCHPAD_INSUFFICIENT_BALANCE", "InsufficientBalance()", "0xf4d678b8", nil},
		{types.ErrInsufficientCreationFee, 39, "LXP_LAUNCHPAD_INSUFFICIENT_CREATION_FEE", "InsufficientCreationFee()", "0x535b7470", nil},
		{types.ErrInsufficientSurplus, 40, "LXP_LAUNCHPAD_INSUFFICIENT_SURPLUS", "InsufficientSurplus()", "0x94290ab9", nil},
		{types.ErrInvalidBalance, 41, "LXP_LAUNCHPAD_INVALID_BALANCE", "InvalidBalance()", "0xc52e3eff", nil},
		{types.ErrInvalidConfiguration, 42, "LXP_LAUNCHPAD_INVALID_CONFIGURATION", "InvalidConfiguration()", "0xc52a9bd3", nil},
		{types.ErrInvalidHookResponse, 43, "LXP_LAUNCHPAD_INVALID_HOOK_RESPONSE", "InvalidHookResponse()", "0x1e048e1d", nil},
		{types.ErrInvalidImplementation, 44, "LXP_LAUNCHPAD_INVALID_IMPLEMENTATION", "InvalidImplementation()", "0x68155f9a", nil},
		{types.ErrInvalidRoleAccount, 45, "LXP_LAUNCHPAD_INVALID_ROLE_ACCOUNT", "InvalidRoleAccount()", "0xeccda526", nil},
		{types.ErrLastDefaultAdmin, 46, "LXP_LAUNCHPAD_LAST_DEFAULT_ADMIN", "LastDefaultAdmin()", "0x9c88180d", nil},
		{types.ErrMissingRole, 47, "LXP_LAUNCHPAD_MISSING_ROLE", "MissingRole(address,bytes32)", "0x0161a64a", []interface{}{caller, role}},
		{types.ErrMulticallFailed, 48, "LXP_LAUNCHPAD_MULTICALL_FAILED", "MulticallFailed(uint256,bytes)", "0x6cf73d14", []interface{}{big.NewInt(2), []byte{0xde, 0xad, 0xbe, 0xef}}},
		{types.ErrNonERC721Receiver, 49, "LXP_LAUNCHPAD_NON_ERC721_RECEIVER", "NonERC721Receiver()", "0x80526d0c", nil},
		{types.ErrNonPayableUpgrade, 50, "LXP_LAUNCHPAD_NON_PAYABLE_UPGRADE", "NonPayableUpgrade()", "0x93738d49", nil},
		{types.ErrNotApproved, 51, "LXP_LAUNCHPAD_NOT_APPROVED", "NotApproved()", "0xc19f17a9", nil},
		{types.ErrNotInitializing, 52, "LXP_LAUNCHPAD_NOT_INITIALIZING", "NotInitializing()", "0xd7e6bcf8", nil},
		{types.ErrNotTokenOwner, 53, "LXP_LAUNCHPAD_NOT_TOKEN_OWNER", "NotTokenOwner()", "0x59dc379f", nil},
		{types.ErrOpticalQuoteUnsupported, 54, "LXP_LAUNCHPAD_OPTICAL_QUOTE_UNSUPPORTED", "OpticalQuoteUnsupported()", "0x41d4e1d1", nil},
		{types.ErrPoolIndexMigrationRequired, 55, "LXP_LAUNCHPAD_POOL_INDEX_MIGRATION_REQUIRED", "PoolIndexMigrationRequired()", "0x9aee8140", nil},
		{types.ErrReentrancyGuardReentrantCall, 56, "LXP_LAUNCHPAD_REENTRANCY_GUARD_REENTRANT_CALL", "ReentrancyGuardReentrantCall()", "0x3ee5aeb5", nil},
		{types.ErrSameToken, 57, "LXP_LAUNCHPAD_SAME_TOKEN", "SameToken()", "0x201b580a", nil},
		{types.ErrTokenNotFound, 58, "LXP_LAUNCHPAD_TOKEN_NOT_FOUND", "TokenNotFound()", "0xcbdb7b30", nil},
		{types.ErrTransferFailed, 59, "LXP_LAUNCHPAD_TRANSFER_FAILED", "TransferFailed()", "0x90b8ec18", nil},
		{types.ErrTransferFromFailed, 60, "LXP_LAUNCHPAD_TRANSFER_FROM_FAILED", "TransferFromFailed()", "0x7939f424", nil},
		{types.ErrUUPSNotThroughActiveProxy, 61, "LXP_LAUNCHPAD_UUPS_NOT_THROUGH_ACTIVE_PROXY", "UUPSNotThroughActiveProxy()", "0x45a93fc9", nil},
		{types.ErrUUPSNotThroughProxy, 62, "LXP_LAUNCHPAD_UUPS_NOT_THROUGH_PROXY", "UUPSNotThroughProxy()", "0x4f083858", nil},
		{types.ErrUnauthorizedUpgrade, 63, "LXP_LAUNCHPAD_UNAUTHORIZED_UPGRADE", "UnauthorizedUpgrade()", "0x3a617a54", nil},
		{types.ErrUnexecutableQuote, 64, "LXP_LAUNCHPAD_UNEXECUTABLE_QUOTE", "UnexecutableQuote()", "0xc1becfdb", nil},
		{types.ErrUnsupportedSnapshotToken, 65, "LXP_LAUNCHPAD_UNSUPPORTED_SNAPSHOT_TOKEN", "UnsupportedSnapshotToken()", "0xe3aa7e40", nil},
		{types.ErrUpgradeInitializationFailed, 66, "LXP_LAUNCHPAD_UPGRADE_INITIALIZATION_FAILED", "UpgradeInitializationFailed()", "0xcc1cf440", nil},
		{types.ErrInvalidToken, 67, "LXP_LAUNCHPAD_INVALID_TOKEN", "InvalidToken()", "0xc1ab6dc1", nil},
		{types.ErrReentrantCall, 68, "LXP_LAUNCHPAD_REENTRANT_CALL", "ReentrantCall()", "0x37ed32e8", nil},
		{types.ErrAccessControlUnauthorizedAccount, 69, "LXP_LAUNCHPAD_ACCESS_CONTROL_UNAUTHORIZED_ACCOUNT", "AccessControlUnauthorizedAccount(address,bytes32)", "0xe2517d3f", []interface{}{caller, [32]byte{}}},
		{types.ErrRetired, 70, "LXP_LAUNCHPAD_RETIRED", "Retired(address)", "0xda9d2e31", []interface{}{successor}},
		{types.ErrFutureLookup, 71, "LXP_LAUNCHPAD_FUTURE_LOOKUP", "FutureLookup()", "0x0297ecc2", nil},
		{types.ErrInsufficientAllowance, 72, "LXP_LAUNCHPAD_INSUFFICIENT_ALLOWANCE", "InsufficientAllowance()", "0x13be252b", nil},
		{types.ErrInvalidPermit, 73, "LXP_LAUNCHPAD_INVALID_PERMIT", "InvalidPermit()", "0xddafbaef", nil},
		{types.ErrInvalidPoolBinding, 74, "LXP_LAUNCHPAD_INVALID_POOL_BINDING", "InvalidPoolBinding()", "0xf77cf96c", nil},
		{types.ErrPermitExpired, 75, "LXP_LAUNCHPAD_PERMIT_EXPIRED", "PermitExpired()", "0x1a15a3cc", nil},
	}
	seen := make(map[string]uint32, len(cases))
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			require.Equal(t, tc.reason, tc.err.Error())
			require.Equal(t, "launchpad", tc.err.Codespace())
			require.Equal(t, tc.code, tc.err.ABCICode())
			require.Contains(t, tc.reason, "LXP_LAUNCHPAD_")
			previous, duplicate := seen[tc.err.Error()]
			require.False(t, duplicate, "codes %d and %d share a reason", previous, tc.code)
			seen[tc.err.Error()] = tc.code

			wrapped := tc.err.Wrap("call")
			space, code, log := sdkerrors.ABCIInfo(wrapped, false)
			require.Equal(t, "launchpad", space)
			require.Equal(t, tc.code, code)
			require.Equal(t, "call: "+tc.reason, log)
			require.True(t, tc.err.Is(wrapped))
			require.ErrorIs(t, wrapped, tc.err)
			require.True(t, tc.err.Is(sdkerrors.ABCIError("launchpad", tc.code, "remote call")))

			revert := pcommon.RevertReason(tc.err)
			require.GreaterOrEqual(t, len(revert), 4)
			require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, revert[:4])
			reason, err := abi.UnpackRevert(revert)
			require.NoError(t, err)
			require.Equal(t, tc.reason, reason)

			if tc.signature == "" {
				for _, carrier := range []error{
					tc.err,
					wrapped,
					fmt.Errorf("%w: detail", tc.err),
					sdkerrors.ABCIError("launchpad", tc.code, "remote call"),
				} {
					found, ok := types.ContractErrorOf(carrier)
					require.False(t, ok)
					require.Equal(t, types.ContractError{}, found)
				}
				return
			}

			contractError, ok := types.ContractErrorOf(tc.err)
			require.True(t, ok)
			require.Same(t, tc.err, contractError.Err)
			require.Equal(t, tc.signature, contractError.Signature())
			require.Equal(t, tc.selector, hexutil.Encode(contractError.Selector[:]))
			require.Len(t, contractError.Inputs, len(tc.args))
			for _, carrier := range []error{
				wrapped,
				fmt.Errorf("%w: detail", tc.err),
				sdkerrors.ABCIError("launchpad", tc.code, "remote call"),
			} {
				found, ok := types.ContractErrorOf(carrier)
				require.True(t, ok)
				require.Same(t, tc.err, found.Err)
				require.Equal(t, contractError.Selector, found.Selector)
			}

			data, err := contractError.Revert(tc.args...)
			require.NoError(t, err)
			require.Equal(t, tc.selector, hexutil.Encode(data[:4]))
			if len(tc.args) == 0 {
				require.Len(t, data, 4)
				_, err := contractError.Revert(caller)
				require.ErrorIs(t, err, types.ErrInvalidParams)
				return
			}
			values, err := contractError.Inputs.Unpack(data[4:])
			require.NoError(t, err)
			require.Equal(t, tc.args, values)
			_, err = contractError.Revert()
			require.ErrorIs(t, err, types.ErrInvalidParams)
		})
	}
	require.Len(t, seen, 74)

	t.Run("artifact_errors", func(t *testing.T) {
		conditionErrors := map[*sdkerrors.Error]string{
			types.ErrNotGuardian:         "Unauthorized()",
			types.ErrInvalidAirdropEpoch: "AirdropNotTriggered()",
			types.ErrLegacyAirdropEpoch:  "AirdropNotTriggered()",
		}
		chainDefined := map[string]string{
			"AccessControlUnauthorizedAccount(address,bytes32)": "0xe2517d3f",
			"Retired(address)": "0xda9d2e31",
		}

		artifacts, err := filepath.Glob(filepath.Join("..", "..", "..", "precompiles", "launchpad", "abi", "*.json"))
		require.NoError(t, err)
		require.Len(t, artifacts, 11)
		artifactErrors := map[string]string{}
		for _, path := range artifacts {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var entries []struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Inputs []struct {
					Type string `json:"type"`
				} `json:"inputs"`
			}
			require.NoError(t, json.Unmarshal(raw, &entries), path)
			for _, entry := range entries {
				if entry.Type != "error" {
					continue
				}
				inputs := make([]string, len(entry.Inputs))
				for i, input := range entry.Inputs {
					inputs[i] = input.Type
				}
				signature := entry.Name + "(" + strings.Join(inputs, ",") + ")"
				artifactErrors[signature] = hexutil.Encode(crypto.Keccak256([]byte(signature))[:4])
			}
		}
		require.Len(t, artifactErrors, 62)

		own := map[string][]string{}
		var keeperOnly []*sdkerrors.Error
		for _, tc := range cases {
			if tc.signature == "" {
				keeperOnly = append(keeperOnly, tc.err)
				continue
			}
			selector, declared := artifactErrors[tc.signature]
			if !declared {
				selector, declared = chainDefined[tc.signature]
			}
			require.True(t, declared, "%s maps to undeclared %s", tc.reason, tc.signature)
			require.Equal(t, selector, tc.selector, tc.signature)
			if condition, ok := conditionErrors[tc.err]; ok {
				require.Equal(t, condition, tc.signature, tc.reason)
				continue
			}
			own[tc.signature] = append(own[tc.signature], tc.reason)
		}
		for signature := range artifactErrors {
			require.Len(t, own[signature], 1, "%s keeper errors %v", signature, own[signature])
		}
		for signature := range chainDefined {
			require.Len(t, own[signature], 1, "%s keeper errors %v", signature, own[signature])
		}
		require.Len(t, own, len(artifactErrors)+len(chainDefined))
		require.Equal(t, keeperOnly, types.KeeperOnlyErrors)
	})

	t.Run("argument_encoding", func(t *testing.T) {
		admin, ok := types.ContractErrorOf(types.ErrAccessControlUnauthorizedAccount)
		require.True(t, ok)
		data, err := admin.Revert(caller, [32]byte{})
		require.NoError(t, err)
		require.Equal(t, "0xe2517d3f"+
			"00000000000000000000000000000000000000000000000000000000000000c1"+
			"0000000000000000000000000000000000000000000000000000000000000000", hexutil.Encode(data))

		retired, ok := types.ContractErrorOf(types.ErrRetired)
		require.True(t, ok)
		data, err = retired.Revert(successor)
		require.NoError(t, err)
		require.Equal(t, "0xda9d2e31"+
			"00000000000000000000000000000000000000000000000000000000000000d2", hexutil.Encode(data))

		missing, ok := types.ContractErrorOf(types.ErrMissingRole)
		require.True(t, ok)
		data, err = missing.Revert(caller, role)
		require.NoError(t, err)
		require.Equal(t, "0x0161a64a"+
			"00000000000000000000000000000000000000000000000000000000000000c1"+
			"0102030000000000000000000000000000000000000000000000000000000000", hexutil.Encode(data))

		multicall, ok := types.ContractErrorOf(types.ErrMulticallFailed)
		require.True(t, ok)
		data, err = multicall.Revert(big.NewInt(2), []byte{0xde, 0xad, 0xbe, 0xef})
		require.NoError(t, err)
		require.Equal(t, "0x6cf73d14"+
			"0000000000000000000000000000000000000000000000000000000000000002"+
			"0000000000000000000000000000000000000000000000000000000000000040"+
			"0000000000000000000000000000000000000000000000000000000000000004"+
			"deadbeef00000000000000000000000000000000000000000000000000000000", hexutil.Encode(data))

		_, err = missing.Revert(caller, "role")
		require.ErrorIs(t, err, types.ErrInvalidParams)
	})

	t.Run("foreign_errors", func(t *testing.T) {
		for _, err := range []error{
			nil,
			errors.New("LXP_LAUNCHPAD_PAUSED"),
			sdkerrors.ErrUnauthorized,
			sdkerrors.ABCIError("launchpad", 1, "unregistered"),
			sdkerrors.ABCIError("bank", 13, "other module"),
		} {
			found, ok := types.ContractErrorOf(err)
			require.False(t, ok)
			require.Equal(t, types.ContractError{}, found)
		}
	})
}
