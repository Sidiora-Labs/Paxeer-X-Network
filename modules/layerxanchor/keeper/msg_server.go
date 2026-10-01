package keeper

import (
	"context"
	"github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
)

type msgServer struct{ keeper Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{keeper: k} }
func (s msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, types.ErrUnauthorized.Wrap("missing message")
	}
	authority, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("authority: %v", err)
	}

	if err := s.keeper.UpdateParams(sdk.UnwrapSDKContext(ctx), authority, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
