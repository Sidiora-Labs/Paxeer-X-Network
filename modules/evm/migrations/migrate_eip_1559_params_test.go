package migrations_test

import (
	"testing"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/migrations"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestMigrateEip1559Params(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{})

	keeperParams := k.GetParams(ctx)
	keeperParams.BaseFeePerGas = sdk.NewDec(123)

	// Perform the migration
	err := migrations.MigrateEip1559Params(ctx, &k)
	require.NoError(t, err)

	// Ensure that the new EIP-1559 parameters were migrated and the old ones were not changed
	require.Equal(t, keeperParams.BaseFeePerGas, sdk.NewDec(123))
	require.Equal(t, keeperParams.MaxDynamicBaseFeeUpwardAdjustment, types.DefaultParams().MaxDynamicBaseFeeUpwardAdjustment)
	require.Equal(t, keeperParams.MaxDynamicBaseFeeDownwardAdjustment, types.DefaultParams().MaxDynamicBaseFeeDownwardAdjustment)
	require.Equal(t, keeperParams.TargetGasUsedPerBlock, types.DefaultParams().TargetGasUsedPerBlock)
	require.Equal(t, keeperParams.MinimumFeePerGas, types.DefaultParams().MinimumFeePerGas)
}
