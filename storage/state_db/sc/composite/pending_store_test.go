package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/common/keys"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/proto"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/state_db/sc/types"
)

// forkStoreKeys are the module stores a state that predates the fork does not
// carry. A binary that mounts them has to be able to run on that state, which
// is what a pending store is for.
var forkStoreKeys = []string{
	keys.LayerXCustodyStoreKey,
	keys.LayerXAnchorStoreKey,
	keys.LayerXExchangeStoreKey,
	keys.LayerXBridgeStoreKey,
	keys.LaunchpadStoreKey,
	keys.XWebStoreKey,
}

// legacyStoreKeys is the canonical mount list without the fork stores, which
// is the mount list of the binary a pre-fork chain runs.
func legacyStoreKeys(t *testing.T) []string {
	t.Helper()
	legacy, err := keys.AllModulesExcept(forkStoreKeys...)
	require.NoError(t, err)
	return legacy
}

// mountAlso returns the mount list plus one more store, without disturbing
// the list it was given.
func mountAlso(mounted []string, extra string) []string {
	out := make([]string, 0, len(mounted)+1)
	out = append(out, mounted...)
	return append(out, extra)
}

// openPendingTestStore opens dir with the given mount list, exactly as the root
// multistore does: record the mounted stores, then load the latest version.
func openPendingTestStore(t *testing.T, dir string, mounted []string) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, config.DefaultStateCommitConfig())
	require.NoError(t, err)
	require.NoError(t, cs.Initialize(mounted))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	return cs
}

// bankWrite is the ordinary write every block in these tests makes to a store
// the database does carry, so that a block is never empty and the commit info
// under comparison is a real one.
func bankWrite() []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name: keys.BankStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("balances/holder"), Value: []byte("1000")},
		}},
	}}
}

// launchpadWrite is a write aimed at a store the pre-fork database lacks.
func launchpadWrite() *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: keys.LaunchpadStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("params"), Value: []byte("paused")},
		}},
	}
}

// seedPreForkState leaves dir holding one committed block written by a binary
// that mounts only the legacy stores.
func seedPreForkState(t *testing.T, dir string) {
	t.Helper()
	cs := openPendingTestStore(t, dir, legacyStoreKeys(t))
	require.NoError(t, cs.ApplyChangeSets(bankWrite()))
	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.NoError(t, cs.Close())
}

// cloneCommitInfo detaches a commit info from the store that produced it so it
// survives a later commit, reload or close.
func cloneCommitInfo(ci *proto.CommitInfo) proto.CommitInfo {
	clone := proto.CommitInfo{
		Version:    ci.Version,
		StoreInfos: make([]proto.StoreInfo, len(ci.StoreInfos)),
	}
	for i, info := range ci.StoreInfos {
		clone.StoreInfos[i] = proto.StoreInfo{
			Name: info.Name,
			CommitId: proto.CommitID{
				Version: info.CommitId.Version,
				Hash:    append([]byte(nil), info.CommitId.Hash...),
			},
		}
	}
	return clone
}

// storeInfo returns the commit info entry for name, or nil when the commit
// info does not carry one.
func storeInfo(ci proto.CommitInfo, name string) *proto.StoreInfo {
	for i := range ci.StoreInfos {
		if ci.StoreInfos[i].Name == name {
			return &ci.StoreInfos[i]
		}
	}
	return nil
}

// TestPendingStoreServesAMountedStoreTheDatabaseLacks is the load a binary
// whose mount list has grown performs on a state that predates the growth: the
// load succeeds, the extra store is reported pending, and it reads as empty
// while every store the database does carry reads its committed value.
func TestPendingStoreServesAMountedStoreTheDatabaseLacks(t *testing.T) {
	dir := t.TempDir()
	seedPreForkState(t, dir)

	legacy := legacyStoreKeys(t)
	cs := openPendingTestStore(t, dir, mountAlso(legacy, keys.LaunchpadStoreKey))
	defer func() { require.NoError(t, cs.Close()) }()

	require.Equal(t, int64(1), cs.Version())
	require.Equal(t, []string{keys.LaunchpadStoreKey}, cs.PendingStores())
	require.True(t, cs.IsPendingStore(keys.LaunchpadStoreKey))
	for _, name := range legacy {
		require.False(t, cs.IsPendingStore(name), "store %q is carried by the database", name)
	}

	var launchpad types.CommitKVStore
	require.NotPanics(t, func() {
		launchpad = cs.GetChildStoreByName(keys.LaunchpadStoreKey)
	})
	require.NotNil(t, launchpad)
	require.Nil(t, launchpad.Get([]byte("params")))
	require.False(t, launchpad.Has([]byte("params")))

	iter := launchpad.Iterator(nil, nil, true)
	require.NotNil(t, iter)
	require.False(t, iter.Valid())
	require.NoError(t, iter.Close())

	bank := cs.GetChildStoreByName(keys.BankStoreKey)
	require.Equal(t, []byte("1000"), bank.Get([]byte("balances/holder")))
}

// TestPendingStoreDoesNotCoverAnUnmountedName pins the boundary: only a store
// the caller mounted and the canonical key list names can be pending. A
// canonical store nobody mounted, and any name outside the canonical list,
// keep the behaviour they have always had.
func TestPendingStoreDoesNotCoverAnUnmountedName(t *testing.T) {
	dir := t.TempDir()
	seedPreForkState(t, dir)

	cs := openPendingTestStore(t, dir, mountAlso(legacyStoreKeys(t), keys.LaunchpadStoreKey))
	defer func() { require.NoError(t, cs.Close()) }()

	require.False(t, cs.IsPendingStore(keys.XWebStoreKey))
	require.Panics(t, func() { cs.GetChildStoreByName(keys.XWebStoreKey) })

	require.False(t, cs.IsPendingStore("not-a-real-store"))
	require.Panics(t, func() { cs.GetChildStoreByName("not-a-real-store") })
}

// TestPendingStoreWritesLeaveTheCommitHashUntouched runs the same block twice
// over the same pre-fork state: once on a binary that does not mount the extra
// store, and once on a binary that mounts it and writes to it. The working
// commit info before the commit, the version the commit produces, the commit
// info afterwards and the state both leave on disk are identical, so the two
// binaries agree on the app hash and the writes aimed at the pending store
// never become state.
func TestPendingStoreWritesLeaveTheCommitHashUntouched(t *testing.T) {
	legacy := legacyStoreKeys(t)
	mounted := mountAlso(legacy, keys.LaunchpadStoreKey)

	controlDir := t.TempDir()
	forkDir := t.TempDir()
	seedPreForkState(t, controlDir)
	seedPreForkState(t, forkDir)

	control := openPendingTestStore(t, controlDir, legacy)
	require.Empty(t, control.PendingStores())
	require.NoError(t, control.ApplyChangeSets(bankWrite()))
	controlWorking := cloneCommitInfo(control.WorkingCommitInfo())
	controlVersion, err := control.Commit()
	require.NoError(t, err)
	controlCommitted := cloneCommitInfo(control.LastCommitInfo())
	require.NoError(t, control.Close())

	fork := openPendingTestStore(t, forkDir, mounted)
	require.True(t, fork.IsPendingStore(keys.LaunchpadStoreKey))
	require.NoError(t, fork.ApplyChangeSets(append(bankWrite(), launchpadWrite())))

	held := fork.HeldPendingWrites(keys.LaunchpadStoreKey)
	require.Len(t, held, 1)
	require.Equal(t, []byte("params"), held[0].Key)

	forkWorking := cloneCommitInfo(fork.WorkingCommitInfo())
	forkVersion, err := fork.Commit()
	require.NoError(t, err)
	forkCommitted := cloneCommitInfo(fork.LastCommitInfo())

	require.Nil(t, fork.HeldPendingWrites(keys.LaunchpadStoreKey),
		"the block that held the writes has committed, so they must be gone")
	require.Equal(t, controlVersion, forkVersion)
	require.Equal(t, controlWorking, forkWorking)
	require.Equal(t, controlCommitted, forkCommitted)
	require.Nil(t, storeInfo(forkCommitted, keys.LaunchpadStoreKey),
		"a pending store contributes no entry to the commit info")
	require.True(t, fork.IsPendingStore(keys.LaunchpadStoreKey))
	require.NoError(t, fork.Close())

	controlReopened := openPendingTestStore(t, controlDir, legacy)
	defer func() { require.NoError(t, controlReopened.Close()) }()
	forkReopened := openPendingTestStore(t, forkDir, mounted)
	defer func() { require.NoError(t, forkReopened.Close()) }()

	require.Equal(t,
		cloneCommitInfo(controlReopened.LastCommitInfo()),
		cloneCommitInfo(forkReopened.LastCommitInfo()),
	)
	require.True(t, forkReopened.IsPendingStore(keys.LaunchpadStoreKey))
	value, ok, err := forkReopened.Get(keys.LaunchpadStoreKey, []byte("params"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, value)
}

// TestPendingStoreStopsPendingWhenAnUpgradeAddsTheTree is the load the upgrade
// store loader performs: the same mount list, this time with the store named
// in the load's tree upgrades. The tree is created, the store stops being
// pending, its writes persist across the commit and a reload, and it now
// carries an entry in the commit info that the pre-upgrade commit info did not
// have, which is the change of app hash the upgrade is expected to make.
func TestPendingStoreStopsPendingWhenAnUpgradeAddsTheTree(t *testing.T) {
	dir := t.TempDir()
	seedPreForkState(t, dir)
	mounted := mountAlso(legacyStoreKeys(t), keys.LaunchpadStoreKey)

	pending := openPendingTestStore(t, dir, mounted)
	require.True(t, pending.IsPendingStore(keys.LaunchpadStoreKey))
	pendingCommitted := cloneCommitInfo(pending.LastCommitInfo())
	require.Nil(t, storeInfo(pendingCommitted, keys.LaunchpadStoreKey))
	require.NoError(t, pending.Close())

	cs := openPendingTestStore(t, dir, mounted)
	require.True(t, cs.IsPendingStore(keys.LaunchpadStoreKey))
	require.NoError(t, cs.ApplyUpgrades([]*proto.TreeNameUpgrade{
		{Name: keys.LaunchpadStoreKey},
	}))
	require.False(t, cs.IsPendingStore(keys.LaunchpadStoreKey))
	require.Empty(t, cs.PendingStores())
	require.NotNil(t, cs.memIAVL.GetChildStoreByName(keys.LaunchpadStoreKey),
		"the upgrade creates the real tree")

	require.NoError(t, cs.ApplyChangeSets(append(bankWrite(), launchpadWrite())))
	require.Nil(t, cs.HeldPendingWrites(keys.LaunchpadStoreKey),
		"the store is no longer pending, so nothing is held")

	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	committed := cloneCommitInfo(cs.LastCommitInfo())
	added := storeInfo(committed, keys.LaunchpadStoreKey)
	require.NotNil(t, added, "the added store now contributes to the commit info")
	require.Equal(t, version, added.CommitId.Version)
	require.NotEmpty(t, added.CommitId.Hash)
	require.NotEqual(t, pendingCommitted.StoreInfos, committed.StoreInfos)
	require.NoError(t, cs.Close())

	reopened := openPendingTestStore(t, dir, mounted)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.False(t, reopened.IsPendingStore(keys.LaunchpadStoreKey))
	value, ok, err := reopened.Get(keys.LaunchpadStoreKey, []byte("params"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("paused"), value)
}
