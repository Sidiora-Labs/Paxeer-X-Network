package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func (k Keeper) IsVoteTarget(ctx sdk.Context, denom string) bool {
	_, err := k.GetVoteTarget(ctx, denom)
	return err == nil
}

func (k Keeper) GetVoteTargets(ctx sdk.Context) (voteTargets []string) {
	k.IterateVoteTargets(ctx, func(denom string, denomInfo types.Denom) bool {
		voteTargets = append(voteTargets, denom)
		return false
	})

	return voteTargets
}
