package migrations_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/migrations"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestMigrateBlockBloom(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	k.PrefixStore(ctx, types.BlockBloomPrefix).Set([]byte{1, 2, 3}, []byte{4, 5, 6})
	k.SetBlockBloom(ctx, []ethtypes.Bloom{})
	require.Nil(t, migrations.MigrateBlockBloom(ctx, &k))
	require.Nil(t, k.PrefixStore(ctx, types.BlockBloomPrefix).Get([]byte{1, 2, 3}))
	require.NotNil(t, k.GetBlockBloom(ctx))
	require.Equal(t, int64(8), k.GetLegacyBlockBloomCutoffHeight(ctx))
}
