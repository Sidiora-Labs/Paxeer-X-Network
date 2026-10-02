package layerxcustody

import (
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"

	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/keeper"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
)

// NewProposalHandler executes a passed CustodyProposal: every message it
// carries, in order, through the module's Msg service and so through the
// keeper, which applies it only for the custody authority. The governance
// module account is the authority a proposal executes with, so a proposal
// carrying a message for any other authority, or a malformed proposal, is
// refused before any message runs. The messages execute together: if one
// fails, none of them changes the state. Proposal content executes only from
// the recorded v6.11 activation height on; before it the handler refuses
// without touching state.
func NewProposalHandler(k *keeper.Keeper) govtypes.Handler {
	msgServer := keeper.NewMsgServerImpl(k)
	return func(ctx sdk.Context, content govtypes.Content) error {
		proposal, ok := content.(*types.CustodyProposal)
		if !ok {
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized %s proposal content type: %T", types.ModuleName, content)
		}
		if err := proposal.ValidateBasic(); err != nil {
			return err
		}
		msgs, err := proposal.GetMessages()
		if err != nil {
			return err
		}
		if err := k.GovernanceExecutionActive(ctx); err != nil {
			return err
		}
		cacheCtx, write := ctx.CacheContext()
		for i, msg := range msgs {
			if err := execute(cacheCtx, msgServer, msg); err != nil {
				return sdkerrors.Wrapf(err, "message %d (%s)", i, sdk.MsgTypeURL(msg))
			}
		}
		write()
		ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
		return nil
	}
}

func execute(ctx sdk.Context, msgServer types.MsgServer, msg sdk.Msg) error {
	goCtx := sdk.WrapSDKContext(ctx)
	var err error
	switch m := msg.(type) {
	case *types.MsgUpdateParams:
		_, err = msgServer.UpdateParams(goCtx, m)
	case *types.MsgSetAsset:
		_, err = msgServer.SetAsset(goCtx, m)
	case *types.MsgRegisterCheckpoint:
		_, err = msgServer.RegisterCheckpoint(goCtx, m)
	case *types.MsgSetEmergency:
		_, err = msgServer.SetEmergency(goCtx, m)
	case *types.MsgCancelClaim:
		_, err = msgServer.CancelClaim(goCtx, m)
	default:
		err = sdkerrors.Wrapf(govtypes.ErrInvalidProposalContent, "%T is not a %s governance message", msg, types.ModuleName)
	}
	return err
}
