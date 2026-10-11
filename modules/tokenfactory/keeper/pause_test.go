package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func TestDenomPauseStore(t *testing.T) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(storeKey, sdk.NewTransientStoreKey("transient_test"))
	k := Keeper{storeKey: storeKey}
	denom := "factory/creator/bitcoin"
	other := "factory/creator/ether"

	require.Equal(t, []byte("paused|"+denom), types.GetDenomPausedKey(denom))

	require.False(t, k.IsDenomPaused(ctx, denom))
	require.True(t, k.SendEnabledChecker(ctx, denom))

	require.NoError(t, k.setDenomPaused(ctx, denom, true))
	require.True(t, k.IsDenomPaused(ctx, denom))
	require.False(t, k.SendEnabledChecker(ctx, denom))
	require.False(t, k.IsDenomPaused(ctx, other))
	require.True(t, k.SendEnabledChecker(ctx, other))

	err := k.setDenomPaused(ctx, denom, true)
	require.ErrorIs(t, err, types.ErrPauseUnchanged)
	require.Contains(t, err.Error(), "LXP_PAUSED_SCOPE")
	require.True(t, k.IsDenomPaused(ctx, denom))

	require.NoError(t, k.setDenomPaused(ctx, denom, false))
	require.False(t, k.IsDenomPaused(ctx, denom))
	require.True(t, k.SendEnabledChecker(ctx, denom))

	err = k.setDenomPaused(ctx, denom, false)
	require.ErrorIs(t, err, types.ErrPauseUnchanged)
	require.Contains(t, err.Error(), "LXP_PAUSED_SCOPE")
}
