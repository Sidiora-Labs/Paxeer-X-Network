package main

import (
	"fmt"
	"reflect"
	"time"

	ecommon "github.com/ethereum/go-ethereum/common"

	app "github.com/sidiora-labs/paxeer-network/node"
	upgradekeeper "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"

	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	layerxanchortypes "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
)

// checks records the outcome of the replay's assertions as it prints them.
type checks struct {
	failures int
	passes   int
}

func (c *checks) pass(format string, args ...interface{}) {
	c.passes++
	fmt.Printf("  ok   %s\n", fmt.Sprintf(format, args...))
}

func (c *checks) fail(format string, args ...interface{}) {
	c.failures++
	fmt.Printf("  FAIL %s\n", fmt.Sprintf(format, args...))
}

// assert records one claim about the replayed state.
func (c *checks) assert(held bool, format string, args ...interface{}) bool {
	if held {
		c.pass(format, args...)
	} else {
		c.fail(format, args...)
	}
	return held
}

// note prints an observation that is not an assertion.
func (c *checks) note(format string, args ...interface{}) {
	fmt.Printf("  note %s\n", fmt.Sprintf(format, args...))
}

// guard runs fn and records a failure when it panics, so one panicking read
// does not cost the rest of the report.
func (c *checks) guard(what string, fn func()) bool {
	panicked := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = true
				c.fail("%s panicked: %v", what, recovered)
			}
		}()
		fn()
	}()
	return !panicked
}

// nonZero reports whether value differs from the zero value of its type.
func nonZero(value interface{}) bool {
	return !reflect.DeepEqual(value, reflect.Zero(reflect.TypeOf(value)).Interface())
}

// writeUpgradeInfo writes the upgrade-info.json a node's old binary leaves
// behind when it halts for a plan, through the upgrade keeper that writes it,
// so the application's store loader reads exactly what a node would read.
func writeUpgradeInfo(home string, height int64, plan string) error {
	cdc := app.MakeEncodingConfig().Marshaler
	keeper := upgradekeeper.NewKeeper(map[int64]bool{}, sdk.NewKVStoreKey(upgradetypes.StoreKey), cdc, home, nil)
	return keeper.DumpUpgradeInfoToDisk(height, plan)
}

// replay applies plan to the state under home as the block at the height after
// the committed one, asserts what the fork owes the chain, commits once and
// reopens the store. It returns the number of failed assertions.
func replay(home string, opts appOptions, plan, chainID string, pre *preState) int {
	c := &checks{}
	upgradeHeight := pre.Height + 1
	blockTime := time.Now().UTC()

	fmt.Printf("plan %s replays over the state at height %d, as the block at height %d\n",
		plan, pre.Height, upgradeHeight)

	if err := writeUpgradeInfo(home, upgradeHeight, plan); err != nil {
		c.fail("writing the plan's upgrade-info.json: %v", err)
		return c.failures
	}
	c.pass("the plan is named in upgrade-info.json at height %d, as a halted node leaves it", upgradeHeight)

	fmt.Println("opening the application over the state; a plan that does not declare every store the state lacks ends the process here")
	a := openApp(home, opts)
	opened := true
	defer func() {
		if opened {
			_ = a.Close()
		}
	}()
	c.pass("the application loaded the state with the plan's store upgrades mounted")

	c.assert(a.LastBlockHeight() == pre.Height,
		"the loaded state is at height %d (loaded %d)", pre.Height, a.LastBlockHeight())
	if !c.assert(a.UpgradeKeeper.HasHandler(plan), "the application registers a handler for plan %s", plan) {
		c.note("without a handler the upgrade keeper would panic, so the plan is not applied")
		return c.failures
	}

	cms := a.CommitMultiStore()
	mounted := 0
	for _, name := range allStoreKeys() {
		key := a.GetKey(name)
		if key != nil && cms.GetCommitKVStore(key) != nil {
			mounted++
		}
	}
	c.assert(mounted == len(allStoreKeys()),
		"the load mounted all %d module stores (%d mounted)", len(allStoreKeys()), mounted)
	for _, name := range pre.Missing {
		key := a.GetKey(name)
		c.assert(key != nil && cms.GetCommitKVStore(key) != nil,
			"the plan added the store %s the state did not carry", name)
	}

	// A node applies a plan on the cache-wrapped state of the block, where the
	// writes a module makes are visible to the reads that follow them; on the
	// committed store itself a write is only visible after the commit. The replay
	// applies the plan the same way and writes the block through below, so the
	// handler and the assertions read what a node's block would read.
	ctx, writeBlock := blockContext(a, upgradeHeight, chainID, blockTime).CacheContext()
	before := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	fmt.Printf("version map before the plan: %d modules\n", len(before))
	for _, name := range forkModules {
		if version, known := before[name]; known {
			c.note("the state already carries module %s at consensus version %d", name, version)
		}
	}

	if !c.guard("applying plan "+plan, func() {
		a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: plan, Height: upgradeHeight})
	}) {
		return c.failures
	}
	c.pass("the plan handler ran over the state without a panic")

	after := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	fmt.Printf("version map after the plan: %d modules\n", len(after))
	for _, name := range forkModules {
		if version, known := after[name]; known {
			c.pass("the version map carries module %s at consensus version %d", name, version)
		} else {
			c.fail("the version map carries module %s", name)
		}
	}
	done := a.UpgradeKeeper.GetDoneHeight(ctx, plan)
	c.assert(done == upgradeHeight, "plan %s is recorded done at height %d (recorded %d)", plan, upgradeHeight, done)

	assertForkGenesis(c, a, ctx)
	assertPrecompiles(c, a, ctx, upgradeHeight)

	writeBlock()
	c.pass("the block wrote the plan's state through to the committed store")
	commitID := cms.Commit(true)
	c.assert(commitID.Version == upgradeHeight,
		"the single commit wrote version %d (wrote %d)", upgradeHeight, commitID.Version)
	// The composite commit store hands every child store out through an adapter
	// whose version is the whole store's version and whose root hash fails
	// closed, so a per-store initial version cannot be read through the
	// application. What the added stores carry at the committed version is read
	// instead, through the same store the modules read.
	c.note("the store backend exposes no per-store version, so each added store is read at the committed version instead")
	committed := blockContext(a, upgradeHeight, chainID, blockTime)
	for _, state := range forkModuleStates {
		key := a.GetKey(state.StoreKey)
		if key == nil {
			c.fail("the added store %s is mounted after the commit", state.StoreKey)
			continue
		}
		c.assert(committed.KVStore(key).Has(state.ParamsKey),
			"the added store %s holds the %s parameters at committed version %d",
			state.StoreKey, state.Module, commitID.Version)
	}

	if err := a.Close(); err != nil {
		c.fail("closing the application after the commit: %v", err)
	}
	opened = false

	fmt.Println("reopening the application over the committed state")
	b := openApp(home, opts)
	defer func() { _ = b.Close() }()
	c.pass("the application reopened the committed state, so the commit left the store loadable")
	c.assert(b.LastBlockHeight() == upgradeHeight,
		"the reopened state is at height %d (loaded %d)", upgradeHeight, b.LastBlockHeight())
	reopened := blockContext(b, upgradeHeight, chainID, blockTime)
	reopenedDone := b.UpgradeKeeper.GetDoneHeight(reopened, plan)
	c.assert(reopenedDone == upgradeHeight,
		"the reopened state records plan %s done at height %d (records %d)", plan, upgradeHeight, reopenedDone)
	persisted := b.UpgradeKeeper.GetModuleVersionMap(reopened)
	kept := 0
	for _, name := range forkModules {
		if _, known := persisted[name]; known {
			kept++
		}
	}
	c.assert(kept == len(forkModules),
		"the reopened version map carries all %d fork modules (carries %d)", len(forkModules), kept)
	assertPrecompilesServed(c, b, reopened, upgradeHeight)

	return c.failures
}

// forkModuleState names a module the fork brings, the store it writes and the
// entry its own InitGenesis leaves behind.
type forkModuleState struct {
	Module    string
	StoreKey  string
	ParamsKey []byte
}

// forkModuleStates is the initialisation evidence each fork module owes, taken
// from each module's own types package.
var forkModuleStates = []forkModuleState{
	{layerxcustodytypes.ModuleName, layerxcustodytypes.StoreKey, layerxcustodytypes.ParamsKey},
	{layerxanchortypes.ModuleName, layerxanchortypes.StoreKey, layerxanchortypes.ParamsKey},
	{layerxexchangetypes.ModuleName, layerxexchangetypes.StoreKey, layerxexchangetypes.ParamsKey},
	{layerxbridgetypes.ModuleName, layerxbridgetypes.StoreKey, layerxbridgetypes.ParamsKey},
	{launchpadtypes.ModuleName, launchpadtypes.StoreKey, launchpadtypes.ParamsKey},
	{xwebtypes.ModuleName, xwebtypes.StoreKey, xwebtypes.ParamsKey},
}

// assertForkGenesis reads the genesis state the plan wrote for every module the
// fork brings, through each module's own store entry and keeper.
func assertForkGenesis(c *checks, a *app.App, ctx sdk.Context) {
	for _, state := range forkModuleStates {
		key := a.GetKey(state.StoreKey)
		if key == nil {
			c.fail("the %s store is mounted", state.StoreKey)
			continue
		}
		c.assert(ctx.KVStore(key).Has(state.ParamsKey),
			"the plan initialised the %s module: its store carries the params entry", state.Module)
	}
	c.guard("reading the xweb state", func() {
		c.assert(a.XWebKeeper.IsPaused(ctx), "the xweb module is paused")
		attestors := a.XWebKeeper.GetAttestorSet(ctx).Attestors
		c.assert(len(attestors) == 0, "the xweb attestor set is empty (carries %d)", len(attestors))
		params := a.XWebKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the xweb params are readable and valid")
		c.assert(nonZero(params), "the xweb params are not the zero value")
		c.note("the xweb authority is %s", params.Authority)
	})
	c.guard("reading the layerxcustody params", func() {
		params := a.LayerXCustodyKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxcustody params are readable and valid")
		c.assert(nonZero(params), "the layerxcustody params are not the zero value")
		c.note("the layerxcustody params are the module's own defaults: %t",
			reflect.DeepEqual(params, layerxcustodytypes.DefaultParams()))
	})
	c.guard("reading the layerxanchor params", func() {
		params := a.LayerXAnchorKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxanchor params are readable and valid")
		c.assert(nonZero(params), "the layerxanchor params are not the zero value")
		c.note("the layerxanchor authority is %s", params.Authority)
	})
	c.guard("reading the layerxexchange params", func() {
		params := a.LayerXExchangeKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxexchange params are readable and valid")
		c.note("the layerxexchange module's own DefaultParams is the zero value, so its params carry no non-zero claim; the params entry above is its initialisation evidence")
		c.note("the layerxexchange params are the module's own defaults: %t",
			reflect.DeepEqual(params, layerxexchangetypes.DefaultParams()))
	})
	c.guard("reading the layerxbridge params", func() {
		params := a.LayerXBridgeKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxbridge params are readable and valid")
		c.assert(nonZero(params), "the layerxbridge params are not the zero value")
		c.note("the layerxbridge authority is %s", params.Authority)
	})
	c.guard("reading the launchpad params", func() {
		params := a.LaunchpadKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the launchpad params are readable and valid")
		c.assert(nonZero(params), "the launchpad params are not the zero value")
		c.note("the launchpad params are the module's own defaults: %t",
			reflect.DeepEqual(params, launchpadtypes.DefaultParams()))
	})
	c.guard("reading the fee-token params of the EVM module", func() {
		params := a.EvmKeeper.GetParams(ctx)
		c.assert(params.MaxFeeTokenRateAge > 0,
			"the fee-token rate age bound is set (%d blocks)", params.MaxFeeTokenRateAge)
		c.assert(!params.MaxFeeTokenSpread.IsNil() && params.MaxFeeTokenSpread.IsPositive(),
			"the fee-token spread bound is set (%s)", params.MaxFeeTokenSpread)
	})
}

// assertPrecompiles checks that the fork's precompile addresses are reachable
// through the EVM keeper at the upgrade height and were not reachable at the
// height before it.
func assertPrecompiles(c *checks, a *app.App, ctx sdk.Context, upgradeHeight int64) {
	at := a.EvmKeeper.CustomPrecompiles(ctx)
	earlier := a.EvmKeeper.CustomPrecompiles(ctx.WithBlockHeight(upgradeHeight - 1))
	reached := 0
	for _, precompile := range forkPrecompiles {
		address := ecommon.HexToAddress(precompile.Address)
		_, served := at[address]
		_, servedEarlier := earlier[address]
		now := c.assert(served, "the %s precompile at %s is served at height %d",
			precompile.Name, precompile.Address, upgradeHeight)
		before := c.assert(!servedEarlier, "the %s precompile at %s is not served at height %d",
			precompile.Name, precompile.Address, upgradeHeight-1)
		if now && before {
			reached++
		}
	}
	fmt.Printf("precompile reachability at the fork: %d of %d addresses served at %d and not at %d\n",
		reached, len(forkPrecompiles), upgradeHeight, upgradeHeight-1)
}

// assertPrecompilesServed checks that the fork's precompile addresses are still
// reachable after the state was committed and reopened.
func assertPrecompilesServed(c *checks, a *app.App, ctx sdk.Context, height int64) {
	served := a.EvmKeeper.CustomPrecompiles(ctx)
	count := 0
	for _, precompile := range forkPrecompiles {
		if _, ok := served[ecommon.HexToAddress(precompile.Address)]; ok {
			count++
		}
	}
	c.assert(count == len(forkPrecompiles),
		"the reopened state serves all %d fork precompiles at height %d (serves %d)",
		len(forkPrecompiles), height, count)
}
