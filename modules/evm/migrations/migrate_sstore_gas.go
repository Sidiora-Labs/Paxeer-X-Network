package migrations

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// MigrateSstoreGas updates the PaxSstoreSetGasEip2200 parameter to the default value.
func MigrateSstoreGas(ctx sdk.Context, k *keeper.Keeper) error {
	params := k.GetParams(ctx)
	params.PaxSstoreSetGasEip2200 = types.DefaultPaxSstoreSetGasEIP2200
	k.SetParams(ctx, params)
	return nil
}
