package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func MigrateEip1559Params(ctx sdk.Context, k *keeper.Keeper) error {
	keeperParams := k.GetParamsIfExists(ctx)
	keeperParams.MaxDynamicBaseFeeUpwardAdjustment = types.DefaultParams().MaxDynamicBaseFeeUpwardAdjustment
	keeperParams.MaxDynamicBaseFeeDownwardAdjustment = types.DefaultParams().MaxDynamicBaseFeeDownwardAdjustment
	keeperParams.TargetGasUsedPerBlock = types.DefaultParams().TargetGasUsedPerBlock
	keeperParams.MinimumFeePerGas = types.DefaultParams().MinimumFeePerGas
	k.SetParams(ctx, keeperParams)
	return nil
}
