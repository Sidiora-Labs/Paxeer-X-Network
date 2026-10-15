// Package keeper holds the pricefeed state: the parameters, each validator's
// feed key and its reverse entry, the latest observation per validator and
// market, the per-market aggregate and each validator's last submission
// height.
package keeper

import (
	"encoding/json"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type Keeper struct {
	storeKey      sdk.StoreKey
	stakingKeeper types.StakingKeeper
	accountKeeper types.AccountKeeper
	evmKeeper     types.EVMKeeper
}

// NewKeeper builds the pricefeed keeper.
func NewKeeper(storeKey sdk.StoreKey, stakingKeeper types.StakingKeeper, accountKeeper types.AccountKeeper,
	evmKeeper types.EVMKeeper) Keeper {
	return Keeper{storeKey: storeKey, stakingKeeper: stakingKeeper, accountKeeper: accountKeeper, evmKeeper: evmKeeper}
}

func (k Keeper) set(ctx sdk.Context, key []byte, value interface{}) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	ctx.KVStore(k.storeKey).Set(key, encoded)
}

func (k Keeper) get(ctx sdk.Context, key []byte, value interface{}) bool {
	encoded := ctx.KVStore(k.storeKey).Get(key)
	if encoded == nil {
		return false
	}
	if err := json.Unmarshal(encoded, value); err != nil {
		panic(err)
	}
	return true
}
