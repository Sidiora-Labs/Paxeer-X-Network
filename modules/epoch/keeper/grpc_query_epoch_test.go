package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestEpochQuery(t *testing.T) {
	keeper, ctx := testkeeper.EpochKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	epoch := types.DefaultEpoch()
	keeper.SetEpoch(ctx, epoch)

	response, err := keeper.Epoch(wctx, &types.QueryEpochRequest{})
	require.NoError(t, err)
	require.Equal(t, &types.QueryEpochResponse{Epoch: epoch}, response)
}
