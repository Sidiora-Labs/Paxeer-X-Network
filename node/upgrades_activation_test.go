package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	layerxanchortypes "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	anchorprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxanchor"
	verifyprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxverify"
	"github.com/sidiora-labs/paxeer-network/sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/sidiora-labs/paxeer-network/sdk/crypto/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
	"github.com/sidiora-labs/paxeer-network/storage/common/keys"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// preForkStoreCount is the number of stores a chain that applied none of the
// LayerX plans mounted. The activation plan mounts every store the application
// carries beyond them, so a store added to the application either joins the plan
// or is accounted for here.
const preForkStoreCount = 20

// protoState is the encoding a module defines for the state it stores. A module
// whose state carries no proto encoding keeps it as JSON, and is compared
// through that encoding instead.
type protoState interface {
	Marshal() ([]byte, error)
}

// requireSameState compares a module's stored state against the state its own
// default genesis carries, through the encoding that module stores.
func requireSameState(t *testing.T, name string, want, got any) {
	t.Helper()
	require.Equal(t, encodeState(t, want), encodeState(t, got), name)
}

func encodeState(t *testing.T, state any) []byte {
	t.Helper()
	if message, ok := state.(protoState); ok {
		encoded, err := message.Marshal()
		require.NoError(t, err)
		return encoded
	}
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	return encoded
}

// rewindBeforeActivation turns the test application into a chain that predates
// the fork: the stores of the modules the activation plan initialises hold
// nothing and the module version map knows none of them, exactly as on a chain
// that applied neither the v6.6 nor the v6.8 plan.
func rewindBeforeActivation(t *testing.T, a *App, ctx sdk.Context) {
	t.Helper()
	for _, name := range activationStoreUpgrades().Added {
		store := ctx.KVStore(a.GetKey(name))
		iter := store.Iterator(nil, nil)
		var stale [][]byte
		for ; iter.Valid(); iter.Next() {
			stale = append(stale, iter.Key())
		}
		require.NoError(t, iter.Close())
		require.NotEmpty(t, stale, name)
		for _, key := range stale {
			store.Delete(key)
		}
	}
	upgradeStore := ctx.KVStore(a.GetKey(upgradetypes.StoreKey))
	for _, name := range activationModules() {
		upgradeStore.Delete(append([]byte{upgradetypes.VersionMapByte}, name...))
	}
	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	for _, name := range activationModules() {
		require.NotContains(t, versions, name)
	}
	require.False(t, a.activationModulesPresent(ctx))
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, ActivationUpgrade))
}

func TestActivationUpgradeIsRegisteredBesideTheTagList(t *testing.T) {
	tags, err := f.ReadFile("tags")
	require.NoError(t, err)
	names := parseUpgradesList(string(tags))
	require.NotContains(t, names, ActivationUpgrade)
	require.Equal(t, xwebUpgrade, LatestUpgrade)
	require.Equal(t, xwebUpgrade, names[len(names)-1])
	require.Equal(t, 1, semver.Compare(ActivationUpgrade, LatestUpgrade))

	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	require.True(t, a.UpgradeKeeper.HasHandler(ActivationUpgrade))
	require.True(t, a.UpgradeKeeper.HasHandler(xwebUpgrade))
	require.True(t, a.UpgradeKeeper.HasHandler(sidioraFeeTokenUpgrade))
}

func TestActivationStoreUpgradesMountEveryForkStore(t *testing.T) {
	upgrades, ok := layerxStoreUpgrades(ActivationUpgrade)
	require.True(t, ok)
	require.Equal(t, activationStoreUpgrades(), upgrades)
	require.Empty(t, upgrades.Deleted)
	require.Empty(t, upgrades.Renamed)
	require.Equal(t, append(v66StoreUpgrades().Added, v68StoreUpgrades().Added...), upgrades.Added)
	require.Subset(t, upgrades.Added, v65StoreUpgrades().Added)

	forkModules := []struct {
		store  string
		module string
	}{
		{layerxcustodytypes.StoreKey, layerxcustodytypes.ModuleName},
		{layerxanchortypes.StoreKey, layerxanchortypes.ModuleName},
		{layerxexchangetypes.StoreKey, layerxexchangetypes.ModuleName},
		{layerxbridgetypes.StoreKey, layerxbridgetypes.ModuleName},
		{launchpadtypes.StoreKey, launchpadtypes.ModuleName},
		{xwebtypes.StoreKey, xwebtypes.ModuleName},
	}
	require.Len(t, upgrades.Added, len(forkModules))
	require.Len(t, activationModules(), len(forkModules))
	require.Len(t, kvStoreKeyNames, preForkStoreCount+len(forkModules))

	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	for i, fork := range forkModules {
		require.Equal(t, fork.store, upgrades.Added[i])
		require.Equal(t, fork.module, activationModules()[i])
		require.Contains(t, kvStoreKeyNames, fork.store)
		require.Contains(t, keys.MemIAVLStoreKeys, fork.store)
		require.NotNil(t, a.GetKey(fork.store))
		require.Contains(t, a.mm.Modules, fork.module)
		require.NotZero(t, a.mm.Modules[fork.module].ConsensusVersion())
	}
}

func TestActivationUpgradeInitialisesEveryForkModuleOnce(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	rewindBeforeActivation(t, a, ctx)

	plan := upgradetypes.Plan{Name: ActivationUpgrade, Height: ctx.BlockHeight()}
	require.True(t, a.UpgradeKeeper.HasHandler(plan.Name))
	a.UpgradeKeeper.ApplyUpgrade(ctx, plan)

	require.Equal(t, plan.Height, a.UpgradeKeeper.GetDoneHeight(ctx, ActivationUpgrade))
	require.Equal(t, a.mm.GetVersionMap(), a.UpgradeKeeper.GetModuleVersionMap(ctx))
	require.True(t, a.activationModulesPresent(ctx))

	custody := a.LayerXCustodyKeeper.GetParams(ctx)
	requireSameState(t, layerxcustodytypes.ModuleName, &layerxcustodytypes.DefaultGenesis().Params, &custody)
	anchor := a.LayerXAnchorKeeper.GetParams(ctx)
	requireSameState(t, layerxanchortypes.ModuleName, &layerxanchortypes.DefaultGenesis().Params, &anchor)
	exchange := a.LayerXExchangeKeeper.GetParams(ctx)
	requireSameState(t, layerxexchangetypes.ModuleName, &layerxexchangetypes.DefaultGenesis().Params, &exchange)
	bridge := a.LayerXBridgeKeeper.GetParams(ctx)
	requireSameState(t, layerxbridgetypes.ModuleName, &layerxbridgetypes.DefaultGenesis().Params, &bridge)
	launchpad := a.LaunchpadKeeper.GetParams(ctx)
	requireSameState(t, launchpadtypes.ModuleName, &launchpadtypes.DefaultGenesis().Params, &launchpad)
	xweb := a.XWebKeeper.GetParams(ctx)
	requireSameState(t, xwebtypes.ModuleName, &xwebtypes.DefaultGenesis().Params, &xweb)

	// The web module comes up paused, with no attestor and no threshold.
	require.True(t, a.xwebInitialised(ctx))
	require.True(t, a.XWebKeeper.IsPaused(ctx))
	require.Empty(t, a.XWebKeeper.GetAttestorSet(ctx).Attestors)
	require.Zero(t, a.XWebKeeper.Threshold(ctx))
	require.Zero(t, a.XWebKeeper.Nonce(ctx))
	require.True(t, a.xwebLive(ctx))

	// The plan registers no remote asset: the Sidiora pair stays an owner action
	// through the bridge module's own admin path.
	_, found := a.LayerXBridgeKeeper.GetAssetByDenom(ctx, layerxbridgetypes.SidioraDenom())
	require.False(t, found)
}

func TestActivationServesTheForkPrecompilesOnlyFromItsHeight(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx

	gate := activationPrecompileGate()
	require.Equal(t, ActivationUpgrade, gate.Upgrade)
	require.Len(t, gate.Modules, 8)
	require.Contains(t, gate.Modules, common.HexToAddress(verifyprecompile.LayerXVerifyAddress))
	require.Equal(t, common.Address(layerxanchortypes.AnchorPrecompileAddress), common.HexToAddress(anchorprecompile.LayerXAnchorAddress))
	for addr, names := range gate.Modules {
		require.NotEmpty(t, names)
		require.Subset(t, activationModules(), names)
		require.Contains(t, a.customPrecompiles, addr)
	}
	// The gate names the eight fork addresses and no legacy precompile.
	for addr := range a.customPrecompiles {
		_, gated := gate.Modules[addr]
		value := addr.Big().Int64()
		require.Equal(t, value >= 0x1012 && value <= 0x1019, gated, addr.Hex())
	}

	// A chain whose genesis carries the fork modules serves the eight already.
	for addr := range gate.Modules {
		require.Contains(t, a.EvmKeeper.CustomPrecompiles(ctx), addr)
	}

	rewindBeforeActivation(t, a, ctx)
	a.refreshXWebPrecompile(ctx)
	below := a.EvmKeeper.CustomPrecompiles(ctx)
	require.Len(t, below, len(a.customPrecompiles)-len(gate.Modules))
	for addr := range a.customPrecompiles {
		if _, gated := gate.Modules[addr]; gated {
			require.NotContains(t, below, addr)
			continue
		}
		require.Contains(t, below, addr)
	}
	require.Len(t, a.EvmKeeper.CustomPrecompiles(ctx.WithIsTracing(true)), len(below))

	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: ActivationUpgrade, Height: ctx.BlockHeight()})
	at := a.EvmKeeper.CustomPrecompiles(ctx)
	require.Len(t, at, len(a.customPrecompiles))
	for addr := range gate.Modules {
		require.Contains(t, at, addr)
	}
	require.Len(t, a.EvmKeeper.CustomPrecompiles(ctx.WithIsTracing(true)), len(at))

	// Below the height the plan was applied at, the eight are still absent.
	before := ctx.WithBlockHeight(ctx.BlockHeight() - 1)
	require.Len(t, a.EvmKeeper.CustomPrecompiles(before), len(below))
	require.Len(t, a.EvmKeeper.CustomPrecompiles(before.WithIsTracing(true)), len(below))
	for addr := range gate.Modules {
		require.NotContains(t, a.EvmKeeper.CustomPrecompiles(before), addr)
	}
}

// writeActivationUpgradeInfo writes the upgrade info file an operator leaves for
// the activation plan, through the keeper that writes it and into the home the
// application reads, so the begin blocker reads exactly what a node reads.
func writeActivationUpgradeInfo(t *testing.T, a *App, height int64) {
	t.Helper()
	require.NoError(t, a.UpgradeKeeper.DumpUpgradeInfoToDisk(height, ActivationUpgrade))
	info, err := a.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Equal(t, ActivationUpgrade, info.Name)
	require.Equal(t, height, info.Height)
}

// recordPreviousProposer records the proposer of the block before the one the
// test runs, which the distribution module's begin blocker reads in every block
// after the first.
func recordPreviousProposer(a *App, ctx sdk.Context, valPub cryptotypes.PubKey) {
	a.DistrKeeper.SetPreviousProposerConsAddr(ctx, sdk.ConsAddress(valPub.Address()))
}

func TestActivationAppliesThePlanInTheBlockTheUpgradeInfoNames(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	rewindBeforeActivation(t, a, ctx)
	require.Empty(t, a.unmountedActivationStores())

	const height = int64(42)
	writeActivationUpgradeInfo(t, a, height)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)

	gate := activationPrecompileGate()
	for addr := range gate.Modules {
		require.NotContains(t, a.EvmKeeper.CustomPrecompiles(at), addr, addr.Hex())
	}

	a.BeginBlock(at, height, nil, nil, false)

	// The plan ran on the standard path: the handler initialised every module the
	// version map lacked, the done height is the block's own and no plan is left.
	require.Equal(t, height, a.UpgradeKeeper.GetDoneHeight(at, ActivationUpgrade))
	versions := a.UpgradeKeeper.GetModuleVersionMap(at)
	require.Equal(t, a.mm.GetVersionMap(), versions)
	for _, name := range activationModules() {
		require.Contains(t, versions, name)
	}
	require.True(t, a.activationModulesPresent(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)

	// The web module comes up paused on an empty attestor set, as its own genesis
	// leaves it.
	require.True(t, a.xwebInitialised(at))
	require.True(t, a.XWebKeeper.IsPaused(at))
	require.Empty(t, a.XWebKeeper.GetAttestorSet(at).Attestors)

	served := a.EvmKeeper.CustomPrecompiles(at)
	for addr := range gate.Modules {
		require.Contains(t, served, addr, addr.Hex())
	}
	below := a.EvmKeeper.CustomPrecompiles(at.WithBlockHeight(height - 1))
	require.Len(t, below, len(served)-len(gate.Modules))
	for addr := range gate.Modules {
		require.NotContains(t, below, addr, addr.Hex())
	}

	// The file is still on disk in the blocks that follow, and none of them applies
	// the plan a second time.
	next := ctx.WithBlockHeight(height + 1)
	recordPreviousProposer(a, next, valPub)
	a.BeginBlock(next, height+1, nil, nil, false)
	require.Equal(t, height, a.UpgradeKeeper.GetDoneHeight(next, ActivationUpgrade))
	require.Equal(t, versions, a.UpgradeKeeper.GetModuleVersionMap(next))
	_, found = a.UpgradeKeeper.GetUpgradePlan(next)
	require.False(t, found)
}

func TestActivationIgnoresAnUpgradeInfoThatNamesAnotherHeight(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	rewindBeforeActivation(t, a, ctx)

	const height = int64(42)
	writeActivationUpgradeInfo(t, a, height+5)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)
	before := a.UpgradeKeeper.GetModuleVersionMap(at)

	a.BeginBlock(at, height, nil, nil, false)

	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, ActivationUpgrade))
	require.Equal(t, before, a.UpgradeKeeper.GetModuleVersionMap(at))
	require.False(t, a.activationModulesPresent(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)
	for addr := range activationPrecompileGate().Modules {
		require.NotContains(t, a.EvmKeeper.CustomPrecompiles(at), addr, addr.Hex())
	}
	for _, name := range activationStoreUpgrades().Added {
		iter := at.KVStore(a.GetKey(name)).Iterator(nil, nil)
		require.False(t, iter.Valid(), name)
		require.NoError(t, iter.Close())
	}
}

func TestActivationStopsTheBlockTheStoreLoaderMountedNoActivationStoresFor(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	rewindBeforeActivation(t, a, ctx)

	const height = int64(42)
	writeActivationUpgradeInfo(t, a, height)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)
	before := a.UpgradeKeeper.GetModuleVersionMap(at)

	// A process the upgrade store loader never ran in carries the plan's store keys
	// without the commit multistore holding a store for any of them.
	mounted := make(map[string]*sdk.KVStoreKey, len(activationStoreUpgrades().Added))
	for _, name := range activationStoreUpgrades().Added {
		mounted[name] = a.keys[name]
		a.keys[name] = sdk.NewKVStoreKey(name)
	}
	require.Equal(t, activationStoreUpgrades().Added, a.unmountedActivationStores())

	recovered := func() (value any) {
		defer func() { value = recover() }()
		a.BeginBlock(at, height, nil, nil, false)
		return nil
	}()
	for name, key := range mounted {
		a.keys[name] = key
	}

	require.NotNil(t, recovered)
	message := fmt.Sprint(recovered)
	require.Contains(t, message, "the store loader did not add the activation stores")
	require.Contains(t, message, "the upgrade info height must equal the last committed height plus one when the process starts")
	for _, name := range activationStoreUpgrades().Added {
		require.Contains(t, message, name)
	}

	// The block wrote nothing: no plan, no done height, no module and no store entry.
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, ActivationUpgrade))
	require.Equal(t, before, a.UpgradeKeeper.GetModuleVersionMap(at))
	require.False(t, a.activationModulesPresent(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)
	for _, name := range activationStoreUpgrades().Added {
		iter := at.KVStore(a.GetKey(name)).Iterator(nil, nil)
		require.False(t, iter.Valid(), name)
		require.NoError(t, iter.Close())
	}
}
