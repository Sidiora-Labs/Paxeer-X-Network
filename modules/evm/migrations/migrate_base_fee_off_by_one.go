package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func MigrateBaseFeeOffByOne(ctx sdk.Context, k *keeper.Keeper) error {
	baseFee := k.GetCurrBaseFeePerGas(ctx)
	k.SetNextBaseFeePerGas(ctx, baseFee)
	return nil
}
