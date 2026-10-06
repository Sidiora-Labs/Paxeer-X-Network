package mint

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func HandleUpdateMinterProposal(ctx sdk.Context, k *keeper.Keeper, p *types.UpdateMinterProposal) error {
	err := types.ValidateMinter(*p.Minter)
	if err != nil {
		return err
	}
	k.SetMinter(ctx, *p.Minter)
	return nil
}
