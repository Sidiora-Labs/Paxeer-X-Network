package pricefeed

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// MidBlocker updates the aggregate of every enabled market; it does nothing
// while the store holds no params.
func MidBlocker(ctx sdk.Context, k keeper.Keeper) {
	k.Aggregate(ctx)
}
