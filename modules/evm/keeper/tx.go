package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/prefix"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const DefaultTxHashesToRemove = 100

func (k *Keeper) RemoveFirstNTxHashes(ctx sdk.Context, n int) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.TxHashesPrefix)
	iter := store.Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	keysToDelete := make([][]byte, 0, n)
	for ; n > 0 && iter.Valid(); iter.Next() {
		keysToDelete = append(keysToDelete, iter.Key())
		n--
	}
	for _, k := range keysToDelete {
		store.Delete(k)
	}
}
