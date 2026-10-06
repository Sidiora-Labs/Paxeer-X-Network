package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/keeper"
	keepertest "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetupMsgServer(t *testing.T) {
	k, _ := keepertest.EpochKeeper(t)
	msg := keeper.NewMsgServerImpl(*k)
	require.NotNil(t, msg)
}
