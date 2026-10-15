package typesv610_test

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	types "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	launchpad "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad/legacy/v610"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestLaunchpadErrorStrings(t *testing.T) {
	cases := []struct {
		err    *sdkerrors.Error
		code   uint32
		reason string
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
			remoteSpace, remoteCode, _ := sdkerrors.ABCIInfo(sdkerrors.ABCIError("launchpad", tc.code, "remote call"), false)
			require.Equal(t, "launchpad", remoteSpace)
			require.Equal(t, tc.code, remoteCode)

			revert := pcommon.RevertReason(tc.err)
			require.GreaterOrEqual(t, len(revert), 4)
			require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, revert[:4])
			reason, err := abi.UnpackRevert(revert)
			require.NoError(t, err)
			require.Equal(t, tc.reason, reason)
		})
	}
	require.Len(t, seen, 29)

	t.Run("paused_sell", func(t *testing.T) {
		app := testkeeper.EVMTestApp
		ctx := app.GetContextForDeliverTx(nil)
		ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
		key := sdk.NewKVStoreKey(types.StoreKey)
		k := keeper.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper), &app.EvmKeeper)
		ctx = launchpadtest.WithStore(ctx, key)
		k.InitGenesis(ctx, *types.DefaultGenesis())
		caller := common.BytesToAddress([]byte{1})
		account := app.EvmKeeper.GetPaxAddressOrDefault(ctx, caller)
		params := k.GetParams(ctx)
		coins := sdk.NewCoins(sdk.NewCoin(params.QuoteDenom, params.CreationFee))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", account, coins))
		market, err := k.CreateMarket(ctx, nil, account, "Error Test Token", "ERR", types.FeeStrategyClaim)
		require.NoError(t, err)
		require.NoError(t, k.Pause(ctx, account, market.Denom))
		before, found := k.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.True(t, before.Paused)
		p, err := launchpad.NewPrecompileWithKeeper(k, &app.EvmKeeper)
		require.NoError(t, err)
		input, err := p.GetABI().Pack(launchpad.SellMethod, common.HexToAddress(market.Pointer),
			big.NewInt(1), big.NewInt(0), caller, big.NewInt(0))
		require.NoError(t, err)
		evm := &vm.EVM{StateDB: state.NewDBImpl(ctx, &app.EvmKeeper, false)}
		result, err := p.Run(evm, caller, caller, input, nil, false, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		reason, err := abi.UnpackRevert(result)
		require.NoError(t, err)
		require.Equal(t, "LXP_LAUNCHPAD_PAUSED", reason)
		after, found := k.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.Equal(t, before, after)
	})
}
