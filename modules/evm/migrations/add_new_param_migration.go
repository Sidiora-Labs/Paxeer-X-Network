package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// Note that such migration would nuke any param changes that deviate
// from the defaults. If such changes need to be preserved, a fine-grained
// migration handler should be used instead
func AddNewParamsAndSetAllToDefaults(ctx sdk.Context, k *keeper.Keeper) error {
	defaultParams := types.DefaultParams()
	k.SetParams(ctx, defaultParams)
	return nil
}
