package app

import (
	"fmt"
	"testing"
	"time"

	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/common/keys"
	"github.com/stretchr/testify/require"
)

func TestV6120StoreUpgradesAddOnlyTheNewModules(t *testing.T) {
	upgrades, ok := layerxStoreUpgrades(V6120Upgrade)
	require.True(t, ok)
	require.Equal(t, v6120StoreUpgrades(), upgrades)
	require.Equal(t, []string{perpstypes.StoreKey, spottypes.StoreKey, pricefeedtypes.StoreKey}, upgrades.Added)
	require.Equal(t, []string{"perps", "spot", "pricefeed"}, upgrades.Added)
	require.Empty(t, upgrades.Deleted)
	require.Empty(t, upgrades.Renamed)

	// No store the earlier plans mount is added again.
	for _, store := range upgrades.Added {
		require.NotContains(t, activationStoreUpgrades().Added, store)
		require.NotContains(t, mergeStoreUpgrades().Added, store)
		require.Contains(t, keys.MemIAVLStoreKeys, store)
		require.True(t, keys.IsMemIAVLStoreKey(store))
		require.Contains(t, kvStoreKeyNames, store)
	}
	require.Equal(t, keys.PerpsStoreKey, perpstypes.StoreKey)
	require.Equal(t, keys.SpotStoreKey, spottypes.StoreKey)
	require.Equal(t, keys.PriceFeedStoreKey, pricefeedtypes.StoreKey)

	// The memiavl list carries the new stores after launchpad, as the app does.
	launchpadAt := -1
	for i, name := range keys.MemIAVLStoreKeys {
		if name == keys.LaunchpadStoreKey {
			launchpadAt = i
		}
	}
	require.GreaterOrEqual(t, launchpadAt, 0)
	require.Equal(t, upgrades.Added, keys.MemIAVLStoreKeys[launchpadAt+1:launchpadAt+4])

	// A test application mounts every new store, so neither plan names one.
	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	for _, store := range upgrades.Added {
		key := a.GetKey(store)
		require.NotNil(t, key, store)
		require.NotNil(t, a.CommitMultiStore().GetCommitKVStore(key), store)
	}
	require.Empty(t, a.unmountedPlanStores(V6120Upgrade))
	require.Empty(t, a.unmountedPlanStores(ActivationUpgrade))
	require.Nil(t, a.unmountedPlanStores(V610Upgrade))
	require.Nil(t, a.unmountedPlanStores(MergeUpgrade))
}

func TestV6120RefusesUnmountedStores(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	w := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := w.App, w.Ctx
	ran := make([]string, 0, len(v6120HookNames))
	a.v6120HookTable = make([]v6120Hook, 0, len(v6120HookNames))
	for _, name := range v6120HookNames {
		a.v6120HookTable = append(a.v6120HookTable, v6120Hook{name: name, run: func(sdk.Context) error {
			ran = append(ran, name)
			return nil
		}})
	}

	const height = int64(42)
	writeV6120UpgradeInfo(t, a, height+1)
	at := ctx.WithBlockHeight(height + 1)
	recordPreviousProposer(a, at, valPub)
	before := a.UpgradeKeeper.GetModuleVersionMap(at)

	// A process the upgrade store loader never ran in carries the spot store key
	// without the commit multistore holding a store for it.
	mounted := a.keys[spottypes.StoreKey]
	a.keys[spottypes.StoreKey] = sdk.NewKVStoreKey(spottypes.StoreKey)
	require.Equal(t, []string{spottypes.StoreKey}, a.unmountedPlanStores(V6120Upgrade))
	require.Empty(t, a.unmountedPlanStores(ActivationUpgrade))

	recovered := func() (value any) {
		defer func() { value = recover() }()
		a.BeginBlock(at, height+1, nil, nil, false)
		return nil
	}()
	a.keys[spottypes.StoreKey] = mounted

	require.NotNil(t, recovered)
	require.Equal(t, fmt.Sprintf("upgrade v6.12.0 is due at height %d but the store loader did not add the v6.12.0 stores spot: "+
		"the upgrade info height must equal the last committed height plus one when the process starts", height+1), fmt.Sprint(recovered))

	// The handler has not run: no hook, no done height, no plan, no version map change.
	require.Empty(t, ran)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, V6120Upgrade))
	require.Equal(t, before, a.UpgradeKeeper.GetModuleVersionMap(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)
}
