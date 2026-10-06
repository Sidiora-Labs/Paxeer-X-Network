//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/test"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/types"
)

func TestStorageTestSuite(t *testing.T) {
	rocksConfig := config.DefaultStateStoreConfig()
	rocksConfig.Backend = "rocksdb"
	s := &sstest.StorageTestSuite{
		BaseStorageTestSuite: sstest.BaseStorageTestSuite{
			NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
				return OpenDB(dir, config)
			},
			Config:         rocksConfig,
			EmptyBatchSize: 12,
		},
	}

	suite.Run(t, s)
}
