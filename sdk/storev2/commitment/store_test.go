package commitment

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree)
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

func TestGetWorkingHashRequiresRootMultiStore(t *testing.T) {
	store := NewStore(memiavl.New(100))
	hash, err := store.GetWorkingHash()
	require.Nil(t, hash)
	require.ErrorIs(t, err, ErrWorkingHashUnavailable)
}
