package keeper

import sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"

func (k Keeper) Set(ctx sdk.Context, key []byte, value interface{}) { k.set(ctx, key, value) }

func (k Keeper) Get(ctx sdk.Context, key []byte, value interface{}) bool {
	return k.get(ctx, key, value)
}
func (k Keeper) StoreKey() sdk.StoreKey { return k.storeKey }
