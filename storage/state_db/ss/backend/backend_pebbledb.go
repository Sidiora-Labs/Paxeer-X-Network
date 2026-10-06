package backend

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/pebbledb/mvcc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/types"
)

func openPebbleDB(dbHome string, cfg config.StateStoreConfig) (types.StateStore, error) {
	return mvcc.OpenDB(dbHome, cfg)
}
