package app

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	appparams "github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	paramstypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/common/keys"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// mergeHeight is H_act, the height the merge plan applies at in these tests.
const mergeHeight = int64(42)

var (
	mergePrimary = [32]byte(bytes.Repeat([]byte{0x51}, 32))
	mergeStandby = [32]byte(bytes.Repeat([]byte{0x52}, 32))
)

// mergeReceiptKey is the receipt key the plan pins, from firstHeight on.
func mergeReceiptKey(firstHeight uint64) seqkeeper.ReceiptKeyEntry {
	return seqkeeper.ReceiptKeyEntry{
		KeyId:       bytes.Repeat([]byte{0x44}, seqtypes.KeyIDLength),
		PublicKey:   ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize)).Public().(ed25519.PublicKey),
		FirstHeight: firstHeight,
		LastHeight:  math.MaxUint64,
	}
}

// mergePlanInfo is the sequencer genesis a merge plan carries in its info.
func mergePlanInfo(t *testing.T, genesis map[string]any) string {
	t.Helper()
	bz, err := json.Marshal(genesis)
	require.NoError(t, err)
	return string(bz)
}

// activationInfo pins L = [primary, standby] and one receipt key from H_act on.
func activationInfo(t *testing.T) string {
	return mergePlanInfo(t, map[string]any{
		"leaders":      [][]byte{mergePrimary[:], mergeStandby[:]},
		"receipt_keys": seqkeeper.ReceiptKeys{mergeReceiptKey(uint64(mergeHeight))},
	})
}

// rewindBeforeMerge turns the test application into a chain that has not applied
// the merge plan: the sequencer store holds nothing, the ReceiptKeys param is
// unset and the module version map does not know the sequencer module.
func rewindBeforeMerge(t *testing.T, a *App, ctx sdk.Context) {
	t.Helper()
	store := ctx.KVStore(a.GetKey(keys.SequencerStoreKey))
	iter := store.Iterator(nil, nil)
	var stale [][]byte
	for ; iter.Valid(); iter.Next() {
		stale = append(stale, iter.Key())
	}
	require.NoError(t, iter.Close())
	for _, key := range stale {
		store.Delete(key)
	}
	params := ctx.KVStore(a.GetKey(paramstypes.StoreKey))
	params.Delete(append([]byte(seqtypes.ModuleName+"/"), seqkeeper.KeyReceiptKeys...))
	ctx.KVStore(a.GetKey(upgradetypes.StoreKey)).Delete(append([]byte{upgradetypes.VersionMapByte}, seqtypes.ModuleName...))
	require.NotContains(t, a.UpgradeKeeper.GetModuleVersionMap(ctx), seqtypes.ModuleName)
	require.Nil(t, seqkeeper.GetReceiptKeys(ctx, a.GetSubspace(seqtypes.ModuleName)))
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, MergeUpgrade))
}

// snapshotStores copies every pair of every KV store the merge plan must leave
// untouched: every store but the sequencer store it adds and the upgrade store
// that records it, with the params store taken without the sequencer subspace.
func snapshotStores(t *testing.T, a *App, ctx sdk.Context) map[string]map[string][]byte {
	t.Helper()
	snapshot := make(map[string]map[string][]byte, len(kvStoreKeyNames))
	for _, name := range kvStoreKeyNames {
		if name == keys.SequencerStoreKey || name == upgradetypes.StoreKey {
			continue
		}
		pairs := map[string][]byte{}
		iter := ctx.KVStore(a.GetKey(name)).Iterator(nil, nil)
		for ; iter.Valid(); iter.Next() {
			if name == paramstypes.StoreKey && bytes.HasPrefix(iter.Key(), []byte(seqtypes.ModuleName+"/")) {
				continue
			}
			pairs[string(iter.Key())] = bytes.Clone(iter.Value())
		}
		require.NoError(t, iter.Close())
		snapshot[name] = pairs
	}
	return snapshot
}

func TestUpgradesSequencerAddsStoreOnly(t *testing.T) {
	upgrades, ok := layerxStoreUpgrades(MergeUpgrade)
	require.True(t, ok)
	require.Equal(t, []string{keys.SequencerStoreKey}, upgrades.Added)
	require.Empty(t, upgrades.Deleted)
	require.Empty(t, upgrades.Renamed)
	require.Contains(t, knownUpgradePlans(), MergeUpgrade)

	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx.WithBlockHeight(mergeHeight)
	require.True(t, a.UpgradeKeeper.HasHandler(MergeUpgrade))
	rewindBeforeMerge(t, a, ctx)

	// An account with a balance, a nonce and code, so the comparison covers each.
	contract := common.HexToAddress("0x00000000000000000000000000000000000c0de1")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	a.EvmKeeper.SetCode(ctx, contract, code)
	a.EvmKeeper.SetNonce(ctx, contract, 7)
	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseCoinUnit, 1_000_000))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "evm", funding))
	holder := a.AccountKeeper.GetModuleAddress("evm")
	balances := a.BankKeeper.GetAllBalances(ctx, holder)
	require.True(t, balances.IsAllGTE(funding))
	before := snapshotStores(t, a, ctx)

	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: MergeUpgrade, Height: mergeHeight, Info: activationInfo(t)})

	require.Equal(t, mergeHeight, a.UpgradeKeeper.GetDoneHeight(ctx, MergeUpgrade))
	after := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.Equal(t, a.mm.Modules[seqtypes.ModuleName].ConsensusVersion(), after[seqtypes.ModuleName])
	delete(after, seqtypes.ModuleName)
	require.Equal(t, versions, after)

	require.Equal(t, before, snapshotStores(t, a, ctx))
	require.Equal(t, balances, a.BankKeeper.GetAllBalances(ctx, holder))
	require.Equal(t, uint64(7), a.EvmKeeper.GetNonce(ctx, contract))
	require.Equal(t, crypto.Keccak256Hash(code), a.EvmKeeper.GetCodeHash(ctx, contract))
}

func TestUpgradesSequencerGenesisAtActivation(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx.WithBlockHeight(mergeHeight)
	rewindBeforeMerge(t, a, ctx)
	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)

	refusals := []struct {
		name           string
		info           string
		invalidGenesis bool
	}{
		{"not a genesis", "v7.0", false},
		{"no plan info", "", false},
		{"no leader list", mergePlanInfo(t, map[string]any{
			"receipt_keys": seqkeeper.ReceiptKeys{mergeReceiptKey(uint64(mergeHeight))},
		}), true},
		{"lastSeq above 0", mergePlanInfo(t, map[string]any{
			"leaders":       [][]byte{mergePrimary[:]},
			"receipt_keys":  seqkeeper.ReceiptKeys{mergeReceiptKey(uint64(mergeHeight))},
			"last_seq":      5,
			"last_seq_hash": bytes.Repeat([]byte{0x77}, seqtypes.HashLength),
		}), true},
		{"no receipt key for H_act", mergePlanInfo(t, map[string]any{
			"leaders":      [][]byte{mergePrimary[:]},
			"receipt_keys": seqkeeper.ReceiptKeys{mergeReceiptKey(uint64(mergeHeight) + 1)},
		}), true},
		{"zero leader", mergePlanInfo(t, map[string]any{
			"leaders":      [][]byte{make([]byte, 32)},
			"receipt_keys": seqkeeper.ReceiptKeys{mergeReceiptKey(uint64(mergeHeight))},
		}), true},
	}
	for _, refusal := range refusals {
		cacheCtx, _ := ctx.CacheContext()
		plan := upgradetypes.Plan{Name: MergeUpgrade, Height: mergeHeight, Info: refusal.info}
		_, err := a.mergeUpgradePlan(cacheCtx, plan, versions)
		require.Error(t, err, refusal.name)
		require.Equal(t, refusal.invalidGenesis, errors.Is(err, seqtypes.ErrInvalidGenesis), refusal.name)
		require.Zero(t, a.UpgradeKeeper.GetDoneHeight(cacheCtx, MergeUpgrade), refusal.name)
	}

	info := activationInfo(t)
	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: MergeUpgrade, Height: mergeHeight, Info: info})

	state, err := a.SequencerKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, [][32]byte{mergePrimary, mergeStandby}, state.Leaders)
	pinned := mergeReceiptKey(uint64(mergeHeight))
	want := seqtypes.ReceiptKey{
		KeyId:       [seqtypes.KeyIDLength]byte(pinned.KeyId),
		PublicKey:   [32]byte(pinned.PublicKey),
		FirstHeight: uint64(mergeHeight),
		LastHeight:  math.MaxUint64,
	}
	require.Equal(t, []seqtypes.ReceiptKey{want}, state.ReceiptKeys)
	require.Zero(t, state.LastSeq)
	require.Equal(t, [seqtypes.HashLength]byte{}, state.LastSeqHash)
	require.Equal(t, seqkeeper.ReceiptKeys{pinned}, seqkeeper.GetReceiptKeys(ctx, a.GetSubspace(seqtypes.ModuleName)))

	exported := a.mm.Modules[seqtypes.ModuleName].ExportGenesis(ctx, a.appCodec)
	require.JSONEq(t, info, string(exported))
}

func TestUpgradesSequencerSwitchAtActivation(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx.WithBlockHeight(mergeHeight)
	rewindBeforeMerge(t, a, ctx)

	// Before the plan applies no height is pinned, whatever the round.
	for _, height := range []int64{mergeHeight - 1, mergeHeight, mergeHeight + 1000} {
		for _, round := range []int32{0, 1, 2} {
			leader, pinned, err := a.sequencerPinnedLeader(ctx, height, round)
			require.NoError(t, err)
			require.False(t, pinned)
			require.Equal(t, [32]byte{}, leader)
		}
		require.False(t, a.Sequenced(ctx, height))
	}

	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: MergeUpgrade, Height: mergeHeight, Info: activationInfo(t)})

	// H_act-1 keeps the proposer rotation.
	leader, pinned, err := a.sequencerPinnedLeader(ctx, mergeHeight-1, 0)
	require.NoError(t, err)
	require.False(t, pinned)
	require.Equal(t, [32]byte{}, leader)

	// From H_act the leader is L[r mod len(L)], read from the module store. The
	// block format reads the same state: H_act applies the plan unsequenced and
	// every block after it carries the sequence batch.
	require.False(t, a.Sequenced(ctx, mergeHeight))
	require.True(t, a.Sequenced(ctx, mergeHeight+1))
	cases := []struct {
		height int64
		round  int32
		want   [32]byte
	}{
		{mergeHeight, 0, mergePrimary},
		{mergeHeight, 1, mergeStandby},
		{mergeHeight, 2, mergePrimary},
		{mergeHeight, 5, mergeStandby},
		{mergeHeight + 10, 0, mergePrimary},
		{mergeHeight + 10, 3, mergeStandby},
	}
	for _, c := range cases {
		leader, pinned, err := a.sequencerPinnedLeader(ctx, c.height, c.round)
		require.NoError(t, err)
		require.True(t, pinned, "height %d round %d", c.height, c.round)
		require.Equal(t, c.want, leader, "height %d round %d", c.height, c.round)
	}

	// A store that lost its leader list after the plan applied is refused rather
	// than read as unpinned.
	cacheCtx, _ := ctx.CacheContext()
	cacheCtx.KVStore(a.GetKey(keys.SequencerStoreKey)).Delete(seqtypes.LeaderListKey)
	_, pinned, err = a.sequencerPinnedLeader(cacheCtx, mergeHeight, 0)
	require.ErrorIs(t, err, seqtypes.ErrInvalidGenesis)
	require.False(t, pinned)
}
