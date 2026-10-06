package types_test

import (
	"testing"

	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	storev2rootmulti "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/storev2/rootmulti"
	paxdbconfig "github.com/Sidiora-Labs/Paxeer-X-Network/storage/config"
	"github.com/stretchr/testify/suite"
)

type MerkleTestSuite struct {
	suite.Suite

	store     *storev2rootmulti.Store
	storeKey  *storetypes.KVStoreKey
	iavlStore storetypes.KVStore
}

func (suite *MerkleTestSuite) SetupTest() {
	scConfig := paxdbconfig.DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.AsyncCommitBuffer = 0
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 0
	ssConfig := paxdbconfig.StateStoreConfig{}

	suite.store = storev2rootmulti.NewStore(suite.T().TempDir(), scConfig, ssConfig, nil)
	suite.storeKey = storetypes.NewKVStoreKey("iavlStoreKey")

	suite.store.MountStoreWithDB(suite.storeKey, storetypes.StoreTypeIAVL, nil)
	suite.store.LoadLatestVersion()

	suite.iavlStore = suite.store.GetCommitKVStore(suite.storeKey)
}

func TestMerkleTestSuite(t *testing.T) {
	suite.Run(t, new(MerkleTestSuite))
}
