package store

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/cachekv"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/cachemulti"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/dbadapter"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	dbm "github.com/tendermint/tm-db"
)

func NewTestKVStore() types.KVStore {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	return cachekv.NewStore(mem, storetypes.NewKVStoreKey("test"), storetypes.DefaultCacheSizeLimit)
}

func NewTestCacheMultiStore(stores map[types.StoreKey]types.CacheWrapper) types.CacheMultiStore {
	return cachemulti.NewStore(dbm.NewMemDB(), stores, map[string]types.StoreKey{}, nil, nil, nil, 0)
}
