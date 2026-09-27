package app

import (
	"encoding/json"
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
	require.Positive(t, ForkHeight)

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

func TestActivationSchedulesThePlanOneBlockBeforeTheForkHeight(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	before := ctx.WithBlockHeight(ForkHeight - 1)

	// A chain whose version map already carries the fork modules never schedules.
	require.True(t, a.activationModulesPresent(ctx))
	a.scheduleActivationUpgrade(before)
	_, found := a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.False(t, found)

	rewindBeforeActivation(t, a, ctx)

	// Only the block before the fork height schedules the plan, and never a trace.
	for _, height := range []int64{1, ForkHeight - 2, ForkHeight, ForkHeight + 1} {
		a.scheduleActivationUpgrade(ctx.WithBlockHeight(height))
		_, found = a.UpgradeKeeper.GetUpgradePlan(ctx)
		require.False(t, found, height)
	}
	a.scheduleActivationUpgrade(before.WithIsTracing(true))
	_, found = a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.False(t, found)

	a.scheduleActivationUpgrade(before)
	plan, found := a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.True(t, found)
	require.Equal(t, ActivationUpgrade, plan.Name)
	require.Equal(t, ForkHeight, plan.Height)
	require.False(t, plan.ShouldExecute(before))
	require.True(t, plan.ShouldExecute(ctx.WithBlockHeight(ForkHeight)))

	// The scheduled plan is never rewritten by the blocks that follow.
	a.scheduleActivationUpgrade(before)
	again, found := a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.True(t, found)
	require.Equal(t, plan, again)

	// A chain that has applied the plan never schedules it again.
	a.UpgradeKeeper.ClearUpgradePlan(ctx)
	a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(ForkHeight), ActivationUpgrade)
	a.scheduleActivationUpgrade(before)
	_, found = a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.False(t, found)
}

func TestActivationHaltsTheNodeUntilItsStoreUpgradeInfoIsWritten(t *testing.T) {
	testWrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	require.Zero(t, a.activationUpgradeInfoHeight)

	// With no plan due, the block runs.
	require.NotPanics(t, func() { a.haltForActivationStoreUpgrade(ctx) })

	plan := upgradetypes.Plan{Name: ActivationUpgrade, Height: ctx.BlockHeight()}
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
	require.True(t, plan.ShouldExecute(ctx))
	require.Panics(t, func() { a.haltForActivationStoreUpgrade(ctx) })

	info, err := a.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Equal(t, plan.Name, info.Name)
	require.Equal(t, plan.Height, info.Height)
	upgrades, ok := layerxStoreUpgrades(info.Name)
	require.True(t, ok)
	require.Equal(t, activationStoreUpgrades(), upgrades)

	// A process that started with that file mounts the plan's stores through the
	// upgrade store loader, which SetStoreUpgradeHandlers records as this height.
	a.activationUpgradeInfoHeight = info.Height
	require.NotPanics(t, func() { a.haltForActivationStoreUpgrade(ctx) })

	// A trace of the height never stops the node.
	a.activationUpgradeInfoHeight = 0
	require.NotPanics(t, func() { a.haltForActivationStoreUpgrade(ctx.WithIsTracing(true)) })
}
