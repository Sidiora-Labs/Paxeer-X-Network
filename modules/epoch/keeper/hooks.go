package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func (k Keeper) AfterEpochEnd(ctx sdk.Context, epoch types.Epoch) {
	k.hooks.AfterEpochEnd(ctx, epoch)
}

func (k Keeper) BeforeEpochStart(ctx sdk.Context, epoch types.Epoch) {
	k.hooks.BeforeEpochStart(ctx, epoch)
}
