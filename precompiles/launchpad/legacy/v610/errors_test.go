package launchpadv610_test

import (
	"testing"

	types "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	livetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestLegacyErrorsRegistered(t *testing.T) {
	require.Equal(t, "launchpad_legacy_v610", types.Codespace)
	require.NotEqual(t, livetypes.ModuleName, types.Codespace)
	cases := []struct {
		err  *sdkerrors.Error
		code uint32
		text string
	}{
		{types.ErrInvalidParams, 2, "LXP_LAUNCHPAD_INVALID_PARAMS"},
		{types.ErrOverflow, 3, "LXP_LAUNCHPAD_OVERFLOW"},
		{types.ErrDivisionByZero, 4, "LXP_LAUNCHPAD_DIVISION_BY_ZERO"},
		{types.ErrInsufficientInput, 5, "LXP_LAUNCHPAD_INSUFFICIENT_INPUT"},
		{types.ErrInsufficientLiquidity, 6, "LXP_LAUNCHPAD_INSUFFICIENT_LIQUIDITY"},
		{types.ErrVirtualFloorBreached, 7, "LXP_LAUNCHPAD_VIRTUAL_FLOOR_BREACHED"},
		{types.ErrSlippageExceeded, 8, "LXP_LAUNCHPAD_SLIPPAGE_EXCEEDED"},
		{types.ErrDeadlineExpired, 9, "LXP_LAUNCHPAD_DEADLINE_EXPIRED"},
		{types.ErrZeroAddress, 10, "LXP_LAUNCHPAD_ZERO_ADDRESS"},
		{types.ErrZeroAmount, 11, "LXP_LAUNCHPAD_ZERO_AMOUNT"},
		{types.ErrUnknownMarket, 12, "LXP_LAUNCHPAD_UNKNOWN_MARKET"},
		{types.ErrPaused, 13, "LXP_LAUNCHPAD_PAUSED"},
		{types.ErrNotPaused, 14, "LXP_LAUNCHPAD_NOT_PAUSED"},
		{types.ErrNotGuardian, 15, "LXP_LAUNCHPAD_NOT_GUARDIAN"},
		{types.ErrNotFeeRightsHolder, 16, "LXP_LAUNCHPAD_NOT_FEE_RIGHTS_HOLDER"},
		{types.ErrWrongStrategy, 17, "LXP_LAUNCHPAD_WRONG_STRATEGY"},
		{types.ErrInvalidStrategy, 18, "LXP_LAUNCHPAD_INVALID_STRATEGY"},
		{types.ErrNoFeesAccumulated, 19, "LXP_LAUNCHPAD_NO_FEES_ACCUMULATED"},
		{types.ErrAirdropNotTriggered, 20, "LXP_LAUNCHPAD_AIRDROP_NOT_TRIGGERED"},
		{types.ErrAlreadyClaimed, 21, "LXP_LAUNCHPAD_ALREADY_CLAIMED"},
		{types.ErrInvalidMarket, 22, "LXP_LAUNCHPAD_INVALID_MARKET"},
		{types.ErrUnauthorized, 23, "LXP_LAUNCHPAD_UNAUTHORIZED"},
		{types.ErrInvalidGenesis, 24, "LXP_LAUNCHPAD_INVALID_GENESIS"},
		{types.ErrHoldingHistory, 25, "LXP_LAUNCHPAD_HOLDING_HISTORY"},
		{types.ErrUnsupportedHoldingEpoch, 26, "LXP_LAUNCHPAD_UNSUPPORTED_HOLDING_EPOCH"},
		{types.ErrInvalidAirdropEpoch, 27, "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH"},
		{types.ErrLegacyAirdropEpoch, 28, "LXP_LAUNCHPAD_LEGACY_AIRDROP_EPOCH"},
		{types.ErrAirdropBasis, 29, "LXP_LAUNCHPAD_AIRDROP_BASIS"},
		{types.ErrInvalidRange, 30, "LXP_LAUNCHPAD_INVALID_RANGE"},
	}
	require.Len(t, cases, 29)
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			require.Equal(t, types.Codespace, tc.err.Codespace())
			require.Equal(t, tc.code, tc.err.ABCICode())
			require.Equal(t, tc.text, tc.err.Error())

			looked := sdkerrors.ABCIError(types.Codespace, tc.code, "lookup")
			require.True(t, tc.err.Is(looked))
			require.ErrorIs(t, looked, tc.err)
			space, code, log := sdkerrors.ABCIInfo(looked, false)
			require.Equal(t, types.Codespace, space)
			require.Equal(t, tc.code, code)
			require.Equal(t, "lookup: "+tc.text, log)

			require.Panics(t, func() { sdkerrors.Register(types.Codespace, tc.code, tc.text) })
		})
	}
	unregistered := sdkerrors.ABCIError(types.Codespace, 1, "lookup")
	for _, tc := range cases {
		require.False(t, tc.err.Is(unregistered))
	}
}
