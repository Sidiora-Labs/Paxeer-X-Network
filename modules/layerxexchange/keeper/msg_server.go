package keeper

import (
	"context"
	"errors"

	custodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
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

// SetMarket enables or disables an existing market listing. Only the Enabled
// flag of msg.Market is read; markets are created through createPerpsMarket.
func (s msgServer) SetMarket(goCtx context.Context, msg *types.MsgSetMarket) (*types.MsgSetMarketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := s.k.requireAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if _, err := custodytypes.ParseNonZeroHash32(msg.Market.MarketId); err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidMarket, "market id: %s", err)
	}
	params := s.k.GetParams(ctx)
	for index := range params.Markets {
		if params.Markets[index].MarketId == msg.Market.MarketId {
			params.Markets[index].Enabled = msg.Market.Enabled
			return &types.MsgSetMarketResponse{}, s.k.SetParams(ctx, params)
		}
	}
	return nil, sdkerrors.Wrapf(types.ErrUnknownMarket, "market %s", msg.Market.MarketId)
}

// PauseCheck refuses a state-changing exchange method with LXP_PAUSED_SCOPE
// while Params.Paused is set. The exit paths cancelOrder, cancelSpotOrder,
// closePosition and withdrawMargin always pass.
func (k *Keeper) PauseCheck(ctx sdk.Context, method string) error {
	err := govtypes.CheckPauseScope(types.ModuleName, k.GetParams(ctx).Paused, method)
	if errors.Is(err, govtypes.ErrPausedScope) {
		return govtypes.ErrPausedScope
	}
	return err
}
