package tokenfactory

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
)

func NewProposalHandler(_ keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized tokenfactory proposal content type")
	}
}
