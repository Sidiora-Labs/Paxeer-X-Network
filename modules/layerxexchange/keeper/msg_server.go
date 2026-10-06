package keeper

import (
	"context"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type msgServer struct{ k *Keeper }

// NewMsgServerImpl returns the exchange Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer { return msgServer{k} }

var _ types.MsgServer = msgServer{}

func (s msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := s.k.requireAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, s.k.SetParams(ctx, msg.Params)
}

func (s msgServer) SetMarket(goCtx context.Context, msg *types.MsgSetMarket) (*types.MsgSetMarketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := s.k.requireAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	return &types.MsgSetMarketResponse{}, s.k.SetMarket(ctx, msg.Market)
}
