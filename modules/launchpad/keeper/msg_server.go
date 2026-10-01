package keeper

import (
	"context"
	"github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
)

type msgServer struct{ keeper *Keeper }

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k *Keeper) types.MsgServer { return msgServer{keeper: k} }
func (s msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, types.ErrUnauthorized.Wrap("missing message")
	}

	if err := s.keeper.UpdateParams(sdk.UnwrapSDKContext(ctx), msg.Authority, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
