package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func MigrateRemoveCurrBlockBaseFee(ctx sdk.Context, k *keeper.Keeper) error {
	currBlockBaseFee := k.GetCurrBaseFeePerGas(ctx)
	k.SetNextBaseFeePerGas(ctx, currBlockBaseFee)
	// just store min base fee in curr block base fee
	k.SetCurrBaseFeePerGas(ctx, types.DefaultMinFeePerGas)
	return nil
}
