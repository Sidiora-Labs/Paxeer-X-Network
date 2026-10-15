package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// InitV6120 is the pricefeed step of the v6.12.0 upgrade. It stores the
// default params with the eighteen markets under the governance authority
// when the store holds no params, and returns nil without a change when it
// does, so a governance change is never overwritten. It registers no feed key
// and writes no aggregate, so every market reads not fresh until the bonded
// majority submits.
func InitV6120(ctx sdk.Context, k Keeper) error {
	if ctx.KVStore(k.storeKey).Has(types.ParamsKey) {
		return nil
	}
	return k.SetParams(ctx, types.DefaultParams(types.DefaultAuthority()))
}
