package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// GetParams returns the stored params, or the defaults under the governance
// authority before any params are stored.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	var params types.Params
	if !k.get(ctx, types.ParamsKey, &params) {
		return types.DefaultParams(types.DefaultAuthority())
	}
	return params
}

// SetParams validates params and stores them.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	k.set(ctx, types.ParamsKey, params)
	return nil
}

func (k Keeper) requireAuthority(ctx sdk.Context, authority string) error {
	if authority == "" || k.GetParams(ctx).Authority != authority {
		return types.ErrUnauthorized.Wrapf("%s is not the pricefeed authority", authority)
	}
	return nil
}

// UpdateParams replaces the params for the authority. A listed market keeps
// its id and symbol and is never removed; a listing appends the next id.
func (k Keeper) UpdateParams(ctx sdk.Context, authority string, params types.Params) error {
	if err := k.requireAuthority(ctx, authority); err != nil {
		return err
	}
	listed := k.GetParams(ctx).Markets
	if len(params.Markets) < len(listed) {
		return types.ErrInvalidParams.Wrapf("%d markets for %d listed, a listed market is never removed",
			len(params.Markets), len(listed))
	}
	for i, market := range listed {
		if params.Markets[i].ID != market.ID || params.Markets[i].Symbol != market.Symbol {
			return types.ErrInvalidParams.Wrapf("market %d %s is listed, its id and symbol never change",
				market.ID, market.Symbol)
		}
	}
	if err := k.SetParams(ctx, params); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeParams,
		sdk.NewAttribute(types.AttributeAuthority, authority)))
	return nil
}
