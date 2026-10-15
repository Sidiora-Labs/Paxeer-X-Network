package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

var exitPaths = map[string]bool{
	"closePosition":           true,
	"partialClose":            true,
	"closePositionWithParams": true,
	"cancelOrder":             true,
	"cancelByCloid":           true,
	"deposit":                 true,
	"addCollateral":           true,
	"topUpPosition":           true,
}

func (k *Keeper) CheckPause(ctx sdk.Context, method string) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if !params.Paused || exitPaths[method] {
		return nil
	}
	return types.ErrPaused
}
