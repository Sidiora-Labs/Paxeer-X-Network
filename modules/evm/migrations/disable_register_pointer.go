package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func MigrateDisableRegisterPointer(ctx sdk.Context, k *keeper.Keeper) error {
	params := k.GetParams(ctx)
	params.RegisterPointerDisabled = true
	k.SetParams(ctx, params)
	return nil
}
