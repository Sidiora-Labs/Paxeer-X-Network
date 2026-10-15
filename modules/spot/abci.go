package spot

import (
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func EndBlocker(ctx sdk.Context, k *keeper.Keeper) error {
	_, err := k.ExecuteResting(evmkeeper.SyntheticReceiptContext(ctx, evmkeeper.SyntheticReceiptSpot))
	return err
}
