package keeper

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestPauseExitSet(t *testing.T) {
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(key, sdk.NewTransientStoreKey("perps_transient"))
	k := &Keeper{storeKey: key}

	exits := []string{
		"closePosition",
		"partialClose",
		"closePositionWithParams",
		"cancelOrder",
		"cancelByCloid",
		"deposit",
		"addCollateral",
		"topUpPosition",
	}
	blocked := []string{
		"placeOrder",
		"placeLimitOrder",
		"placeStopLimitOrder",
		"placeTakeProfitOrder",
		"placeStopLossOrder",
		"setPositionTpSl",
		"executeOrder",
		"openPosition",
		"addSize",
		"withdraw",
		"emergencyWithdraw",
		"removeCollateral",
		"transferCollateral",
		"createVault",
		"setMarginMode",
		"transferMargin",
		"liquidate",
		"unknownMethod",
		"",
		"Deposit",
		"deposit ",
		"deposit(address,uint256)",
	}

	params := types.DefaultParams()
	current, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.False(t, current.Paused)
	for _, method := range append(append([]string(nil), exits...), blocked...) {
		require.NoError(t, k.CheckPause(ctx, method), method)
	}
	require.Nil(t, ctx.KVStore(key).Get(types.ParamsKey))

	for _, phase := range []struct {
		name   string
		paused bool
	}{
		{"paused", true},
		{"resumed", false},
		{"paused_again", true},
	} {
		t.Run(phase.name, func(t *testing.T) {
			params.Paused = phase.paused
			require.NoError(t, k.setParams(ctx, params))
			current, err := k.GetParams(ctx)
			require.NoError(t, err)
			require.Equal(t, phase.paused, current.Paused)
			stored := append([]byte(nil), ctx.KVStore(key).Get(types.ParamsKey)...)
			events := append(sdk.Events(nil), ctx.EventManager().Events()...)

			for _, method := range exits {
				t.Run("exit/"+method, func(t *testing.T) {
					require.NoError(t, k.CheckPause(ctx, method))
				})
			}
			for _, method := range blocked {
				t.Run("other/"+method, func(t *testing.T) {
					err := k.CheckPause(ctx, method)
					if !phase.paused {
						require.NoError(t, err)
						return
					}
					require.ErrorIs(t, err, types.ErrPaused)
					codespace, code, _ := sdkerrors.ABCIInfo(err, false)
					require.Equal(t, "perps", codespace)
					require.Equal(t, uint32(45), code)
					require.EqualError(t, err, "LXP_PERPS_PAUSED")
				})
			}

			require.Equal(t, stored, ctx.KVStore(key).Get(types.ParamsKey))
			require.Equal(t, events, append(sdk.Events(nil), ctx.EventManager().Events()...))
		})
	}
}
