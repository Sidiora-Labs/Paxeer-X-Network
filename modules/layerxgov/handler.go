package layerxgov

import (
	"github.com/sidiora-labs/paxeer-network/modules/layerxgov/types"
	"github.com/sidiora-labs/paxeer-network/sdk/baseapp"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
)

func NewProposalHandler(router *baseapp.MsgServiceRouter) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		proposal, ok := content.(*types.LayerXProposal)
		if !ok {
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized LayerX proposal content: %T", content)
		}
		if err := proposal.ValidateBasic(); err != nil {
			return err
		}
		msgs, err := proposal.GetMessages()
		if err != nil {
			return err
		}
		cache, write := ctx.CacheContext()
		for _, msg := range msgs {
			handler := router.HandlerByTypeURL(sdk.MsgTypeURL(msg))
			if handler == nil {
				return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "no handler for %s", sdk.MsgTypeURL(msg))
			}
			result, err := handler(cache, msg)
			if err != nil {
				return err
			}
			for _, event := range result.Events {
				cache.EventManager().EmitEvent(sdk.Event(event))
			}
		}
		write()
		ctx.EventManager().EmitEvents(cache.EventManager().Events())
		return nil
	}
}
