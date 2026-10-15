package launchpad

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestRevertDataMatchesContractErrors(t *testing.T) {
	caller := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	replacement := common.HexToAddress("0x00000000000000000000000000000000000000d2")
	role := [32]byte{1, 2, 3}
	cases := []struct {
		err       *sdkerrors.Error
		signature string
		selector  string
		args      []interface{}
	}{
		{types.ErrAirdropNotTriggered, "AirdropNotTriggered()", "0xceadfa6d", nil},
		{types.ErrAlreadyClaimed, "AlreadyClaimed()", "0x646cf558", nil},
		{types.ErrAlreadyInitialized, "AlreadyInitialized()", "0x0dc149f0", nil},
		{types.ErrAlreadyMinted, "AlreadyMinted()", "0xddefae28", nil},
		{types.ErrApproveFailed, "ApproveFailed()", "0x3e3f8f73", nil},
		{types.ErrDeadlineExpired, "DeadlineExpired()", "0x1ab7da6b", nil},
		{types.ErrDivisionByZero, "DivisionByZero()", "0x23d359a3", nil},
		{types.ErrDuplicatePool, "DuplicatePool()", "0xb82a4746", nil},
		{types.ErrDuplicateToken, "DuplicateToken()", "0x464e3f6a", nil},
		{types.ErrFeeOutOfRange, "FeeOutOfRange()", "0x0c8249ad", nil},
		{types.ErrFutureLookup, "FutureLookup()", "0x0297ecc2", nil},
		{types.ErrIndexOutOfBounds, "IndexOutOfBounds()", "0x4e23d035", nil},
		{types.ErrInsufficientAllowance, "InsufficientAllowance()", "0x13be252b", nil},
		{types.ErrInsufficientBalance, "InsufficientBalance()", "0xf4d678b8", nil},
		{types.ErrInsufficientCreationFee, "InsufficientCreationFee()", "0x535b7470", nil},
		{types.ErrInsufficientInput, "InsufficientInput()", "0xf8b3bb61", nil},
		{types.ErrInsufficientLiquidity, "InsufficientLiquidity()", "0xbb55fd27", nil},
		{types.ErrInsufficientSurplus, "InsufficientSurplus()", "0x94290ab9", nil},
		{types.ErrInvalidBalance, "InvalidBalance()", "0xc52e3eff", nil},
		{types.ErrInvalidConfiguration, "InvalidConfiguration()", "0xc52a9bd3", nil},
		{types.ErrInvalidHookResponse, "InvalidHookResponse()", "0x1e048e1d", nil},
		{types.ErrInvalidImplementation, "InvalidImplementation()", "0x68155f9a", nil},
		{types.ErrInvalidPermit, "InvalidPermit()", "0xddafbaef", nil},
		{types.ErrInvalidPoolBinding, "InvalidPoolBinding()", "0xf77cf96c", nil},
		{types.ErrInvalidRoleAccount, "InvalidRoleAccount()", "0xeccda526", nil},
		{types.ErrInvalidStrategy, "InvalidStrategy()", "0x4e236e9a", nil},
		{types.ErrLastDefaultAdmin, "LastDefaultAdmin()", "0x9c88180d", nil},
		{types.ErrMissingRole, "MissingRole(address,bytes32)", "0x0161a64a", []interface{}{caller, role}},
		{types.ErrMulticallFailed, "MulticallFailed(uint256,bytes)", "0x6cf73d14", []interface{}{big.NewInt(2), []byte{0xde, 0xad, 0xbe, 0xef}}},
		{types.ErrNoFeesAccumulated, "NoFeesAccumulated()", "0xbe6fbbaf", nil},
		{types.ErrNonERC721Receiver, "NonERC721Receiver()", "0x80526d0c", nil},
		{types.ErrNonPayableUpgrade, "NonPayableUpgrade()", "0x93738d49", nil},
		{types.ErrNotApproved, "NotApproved()", "0xc19f17a9", nil},
		{types.ErrNotInitializing, "NotInitializing()", "0xd7e6bcf8", nil},
		{types.ErrNotFeeRightsHolder, "NotNftOwner()", "0x4b64ae47", nil},
		{types.ErrNotPaused, "NotPaused()", "0x6cd60201", nil},
		{types.ErrNotTokenOwner, "NotTokenOwner()", "0x59dc379f", nil},
		{types.ErrOpticalQuoteUnsupported, "OpticalQuoteUnsupported()", "0x41d4e1d1", nil},
		{types.ErrOverflow, "Overflow()", "0x35278d12", nil},
		{types.ErrPaused, "Paused()", "0x9e87fac8", nil},
		{types.ErrPermitExpired, "PermitExpired()", "0x1a15a3cc", nil},
		{types.ErrPoolIndexMigrationRequired, "PoolIndexMigrationRequired()", "0x9aee8140", nil},
		{types.ErrUnknownMarket, "PoolNotFound()", "0x76ecffc0", nil},
		{types.ErrReentrancyGuardReentrantCall, "ReentrancyGuardReentrantCall()", "0x3ee5aeb5", nil},
		{types.ErrSameToken, "SameToken()", "0x201b580a", nil},
		{types.ErrSlippageExceeded, "SlippageExceeded()", "0x8199f5f3", nil},
		{types.ErrTokenNotFound, "TokenNotFound()", "0xcbdb7b30", nil},
		{types.ErrTransferFailed, "TransferFailed()", "0x90b8ec18", nil},
		{types.ErrTransferFromFailed, "TransferFromFailed()", "0x7939f424", nil},
		{types.ErrUUPSNotThroughActiveProxy, "UUPSNotThroughActiveProxy()", "0x45a93fc9", nil},
		{types.ErrUUPSNotThroughProxy, "UUPSNotThroughProxy()", "0x4f083858", nil},
		{types.ErrUnauthorized, "Unauthorized()", "0x82b42900", nil},
		{types.ErrUnauthorizedUpgrade, "UnauthorizedUpgrade()", "0x3a617a54", nil},
		{types.ErrUnexecutableQuote, "UnexecutableQuote()", "0xc1becfdb", nil},
		{types.ErrUnsupportedSnapshotToken, "UnsupportedSnapshotToken()", "0xe3aa7e40", nil},
		{types.ErrUpgradeInitializationFailed, "UpgradeInitializationFailed()", "0xcc1cf440", nil},
		{types.ErrVirtualFloorBreached, "VirtualFloorBreached()", "0x04af467f", nil},
		{types.ErrWrongStrategy, "WrongStrategy()", "0xe181295c", nil},
		{types.ErrZeroAddress, "ZeroAddress()", "0xd92e233d", nil},
		{types.ErrZeroAmount, "ZeroAmount()", "0x1f2a2005", nil},
		{types.ErrInvalidToken, "InvalidToken()", "0xc1ab6dc1", nil},
		{types.ErrReentrantCall, "ReentrantCall()", "0x37ed32e8", nil},
		{types.ErrAccessControlUnauthorizedAccount, "AccessControlUnauthorizedAccount(address,bytes32)", "0xe2517d3f", []interface{}{caller, [32]byte{}}},
		{types.ErrRetired, "Retired(address)", "0xda9d2e31", []interface{}{replacement}},
	}
	signatures := make(map[string]bool)
	for _, tc := range cases {
		t.Run(tc.signature, func(t *testing.T) {
			signatures[tc.signature] = true
			carried, err := newCustomRevert(tc.err, tc.args...)
			require.NoError(t, err)
			require.ErrorIs(t, carried, tc.err)
			require.Equal(t, tc.err.Error(), carried.Error())
			require.True(t, strings.HasPrefix(carried.Error(), "LXP_LAUNCHPAD_"))
			space, code, message := sdkerrors.ABCIInfo(carried, false)
			require.Equal(t, types.ModuleName, space)
			require.Equal(t, tc.err.ABCICode(), code)
			require.Equal(t, tc.err.Error(), message)
			want := common.FromHex(tc.selector)
			switch tc.signature {
			case "MissingRole(address,bytes32)":
				want = append(want, common.LeftPadBytes(caller.Bytes(), 32)...)
				want = append(want, role[:]...)
			case "AccessControlUnauthorizedAccount(address,bytes32)":
				want = append(want, common.LeftPadBytes(caller.Bytes(), 32)...)
				want = append(want, make([]byte, 32)...)
			case "Retired(address)":
				want = append(want, common.LeftPadBytes(replacement.Bytes(), 32)...)
			case "MulticallFailed(uint256,bytes)":
				want = common.FromHex("0x6cf73d14" +
					"0000000000000000000000000000000000000000000000000000000000000002" +
					"0000000000000000000000000000000000000000000000000000000000000040" +
					"0000000000000000000000000000000000000000000000000000000000000004" +
					"deadbeef00000000000000000000000000000000000000000000000000000000")
			}
			for _, wrapped := range []error{carried, fmt.Errorf("call: %w", carried), sdkerrors.Wrap(carried, "call")} {
				data, err := revertData(wrapped)
				require.NoError(t, err)
				require.Equal(t, want, data)
				require.ErrorIs(t, wrapped, tc.err)
			}
			if len(tc.args) == 0 {
				for _, wrapped := range []error{tc.err, tc.err.Wrap("call"), fmt.Errorf("call: %w", tc.err)} {
					data, err := revertData(wrapped)
					require.NoError(t, err)
					require.Equal(t, want, data)
				}
			}
			data, err := carried.Revert()
			require.NoError(t, err)
			data[0] ^= 0xff
			data, err = revertData(carried)
			require.NoError(t, err)
			require.Equal(t, want, data)
			found := false
			for _, artifact := range roleABIs {
				for _, declared := range artifact.Errors {
					if declared.Sig != tc.signature {
						continue
					}
					found = true
					require.Equal(t, tc.selector, hexutil.Encode(declared.ID[:4]))
					packed, err := declared.Inputs.Pack(tc.args...)
					require.NoError(t, err)
					require.Equal(t, want, append(declared.ID[:4:4], packed...))
				}
			}
			require.Equal(t, tc.err != types.ErrRetired && tc.err != types.ErrAccessControlUnauthorizedAccount, found)
		})
	}
	require.Len(t, signatures, 64)
	for role, artifact := range roleABIs {
		for _, declared := range artifact.Errors {
			require.True(t, signatures[declared.Sig], "%s: %s is not covered", role, declared.Sig)
		}
	}

	t.Run("condition aliases", func(t *testing.T) {
		for _, tc := range []struct {
			err  error
			want string
		}{
			{types.ErrNotGuardian, "82b42900"},
			{types.ErrInvalidAirdropEpoch, "ceadfa6d"},
			{types.ErrLegacyAirdropEpoch, "ceadfa6d"},
		} {
			data, err := revertData(fmt.Errorf("call: %w", tc.err))
			require.NoError(t, err)
			require.Equal(t, tc.want, hex.EncodeToString(data))
		}
	})

	t.Run("keeper argument carriers", func(t *testing.T) {
		for _, tc := range []struct {
			err  error
			want string
			code uint32
		}{
			{&launchpadkeeper.RoleError{Err: types.ErrMissingRole, Account: caller, Role: role},
				"0161a64a" + "00000000000000000000000000000000000000000000000000000000000000c1" +
					"0102030000000000000000000000000000000000000000000000000000000000", 47},
			{launchpadkeeper.AdminRefusal(caller),
				"e2517d3f" + "00000000000000000000000000000000000000000000000000000000000000c1" +
					"0000000000000000000000000000000000000000000000000000000000000000", 69},
			{&launchpadkeeper.AddressTakenError{Address: replacement}, "", 22},
		} {
			for _, wrapped := range []error{tc.err, fmt.Errorf("call: %w", tc.err), sdkerrors.Wrap(tc.err, "call")} {
				data, err := revertData(wrapped)
				require.NoError(t, err)
				require.Equal(t, tc.want, hex.EncodeToString(data))
			}
			space, code, _ := sdkerrors.ABCIInfo(tc.err, false)
			require.Equal(t, types.ModuleName, space)
			require.Equal(t, tc.code, code)
		}
	})

	t.Run("outer transfer refusal wins", func(t *testing.T) {
		nested := launchpadkeeper.AdminRefusal(caller)
		for _, tc := range []struct {
			err  error
			want string
		}{
			{fmt.Errorf("%w: %w", types.ErrTransferFromFailed, nested), "7939f424"},
			{fmt.Errorf("%w: %w", types.ErrTransferFailed, nested), "90b8ec18"},
			{fmt.Errorf("%w: %w", types.ErrTransferFailed, types.ErrInsufficientInput), "90b8ec18"},
			{fmt.Errorf("%w: %w", types.ErrApproveFailed, types.ErrZeroAddress), "3e3f8f73"},
			{errors.Join(types.ErrInvalidToken, nested), "c1ab6dc1"},
			{fmt.Errorf("%w: %w", types.ErrInvalidBalance, nested), "c52e3eff"},
		} {
			data, err := revertData(tc.err)
			require.NoError(t, err)
			require.Equal(t, tc.want, hex.EncodeToString(data))
		}
	})

	t.Run("missing or invalid arguments", func(t *testing.T) {
		for _, err := range []*sdkerrors.Error{types.ErrMissingRole, types.ErrMulticallFailed, types.ErrRetired, types.ErrAccessControlUnauthorizedAccount} {
			data, packErr := revertData(err)
			require.Nil(t, data)
			require.ErrorIs(t, packErr, types.ErrInvalidParams)
		}
		for _, tc := range []struct {
			err  *sdkerrors.Error
			args []interface{}
		}{
			{types.ErrMissingRole, []interface{}{caller, "role"}},
			{types.ErrMissingRole, []interface{}{nil, role}},
			{types.ErrRetired, []interface{}{[]byte{1}}},
			{types.ErrMulticallFailed, []interface{}{big.NewInt(-1), []byte{1}}},
			{types.ErrMulticallFailed, []interface{}{new(big.Int).Lsh(big.NewInt(1), 256), []byte{1}}},
			{types.ErrMulticallFailed, []interface{}{(*big.Int)(nil), []byte{1}}},
			{types.ErrPaused, []interface{}{caller}},
			{types.ErrInvalidParams, nil},
			{nil, nil},
		} {
			carried, err := newCustomRevert(tc.err, tc.args...)
			require.Nil(t, carried)
			require.ErrorIs(t, err, types.ErrInvalidParams)
		}
		invalid := &launchpadkeeper.RoleError{Err: types.ErrPaused, Account: caller, Role: role}
		data, err := revertData(invalid)
		require.Nil(t, data)
		require.ErrorIs(t, err, types.ErrInvalidParams)
	})

	t.Run("keeper only and foreign reasons", func(t *testing.T) {
		for _, keeperOnly := range types.KeeperOnlyErrors {
			wrapped := fmt.Errorf("call: %w", keeperOnly)
			data, err := revertData(wrapped)
			require.NoError(t, err)
			reason, err := abi.UnpackRevert(data)
			require.NoError(t, err)
			require.Equal(t, wrapped.Error(), reason)
		}
		for _, foreign := range []error{errors.New("LXP_LAUNCHPAD_PAUSED"), sdkerrors.ErrUnauthorized} {
			data, err := revertData(foreign)
			require.NoError(t, err)
			reason, err := abi.UnpackRevert(data)
			require.NoError(t, err)
			require.Equal(t, foreign.Error(), reason)
		}
		data, err := revertData(nil)
		require.Nil(t, data)
		require.NoError(t, err)
	})

	t.Run("multicall captures immutable bytes", func(t *testing.T) {
		index := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		reason := common.FromHex("0x90b8ec18")
		carried, err := newCustomRevert(types.ErrMulticallFailed, index, reason)
		require.NoError(t, err)
		want := "6cf73d14" + strings.Repeat("ff", 32) +
			"0000000000000000000000000000000000000000000000000000000000000040" +
			"0000000000000000000000000000000000000000000000000000000000000004" +
			"90b8ec1800000000000000000000000000000000000000000000000000000000"
		index.SetInt64(0)
		reason[0] = 0
		data, err := revertData(carried)
		require.NoError(t, err)
		require.Equal(t, want, hex.EncodeToString(data))
	})
}
