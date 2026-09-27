package app

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	evmkeeper "github.com/sidiora-labs/paxeer-network/modules/evm/keeper"
	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	layerxanchortypes "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	"github.com/sidiora-labs/paxeer-network/modules/xweb"
	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	"github.com/sidiora-labs/paxeer-network/precompiles"
	feetokenprecompile "github.com/sidiora-labs/paxeer-network/precompiles/feetoken"
	anchorprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxanchor"
	verifyprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxverify"
	putils "github.com/sidiora-labs/paxeer-network/precompiles/utils"
	storetypes "github.com/sidiora-labs/paxeer-network/sdk/store/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	"github.com/sidiora-labs/paxeer-network/sdk/types/module"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
	"golang.org/x/mod/semver"
)

//go:embed tags
var f embed.FS

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
var upgradesList []string

var LatestUpgrade string

func init() {
	content, err := f.ReadFile("tags")
	if err != nil {
		panic(err)
	}
	upgradesList = parseUpgradesList(string(content))
	LatestUpgrade = upgradesList[len(upgradesList)-1]
}

func parseUpgradesList(list string) []string {
	upgrades := strings.FieldsFunc(list, func(r rune) bool {
		return r == '\n' || r == ','
	})
	// Upgrades names must be in alphabetical order
	// https://github.com/cosmos/cosmos-sdk/issues/11707
	semver.Sort(upgrades)
	return upgrades
}

// if there is an override list, use that instead, for integration tests
func overrideList() {
	// if there is an override list, use that instead, for integration tests
	envList := os.Getenv("UPGRADE_VERSION_LIST")
	if envList != "" {
		upgradesList = parseUpgradesList(envList)
	}
}

// sidioraFeeTokenUpgrade gates the Sidiora fee token: it runs the x/evm
// fee-token parameter migration and creates the Sidiora denom and its bank
// metadata under the bridge module account.
const sidioraFeeTokenUpgrade = "v6.7"

// xwebUpgrade adds the xweb store, initialises the xweb module paused with its
// documented default parameters and no attestor, and from its height on serves
// the xweb precompile.
const xwebUpgrade = precompiles.XWebUpgrade

// ActivationUpgrade is the plan that brings the Paxeer X fork online on a chain
// whose state predates it: it mounts the store of every fork module the chain
// never mounted, initialises those modules from their own default genesis, and
// from its height on serves the eight fork precompiles. It carries no entry in
// the embedded tag list, so the latest upgrade and every custom precompile
// version map stay exactly as they are.
const ActivationUpgrade = "v6.9"

func (app *App) RegisterUpgradeHandlers() {
	// if there is an override list, use that instead, for integration tests
	overrideList()
	for _, upgradeName := range upgradesList {
		app.UpgradeKeeper.SetUpgradeHandler(upgradeName, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Set params to Distribution here when migrating
			if upgradeName == "1.2.3beta" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				params := app.DistrKeeper.GetParams(ctx)
				params.CommunityTax = sdk.NewDec(0)
				app.DistrKeeper.SetParams(ctx, params)

				return newVM, err
			}

			if upgradeName == "v6.0.2" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				cp := app.GetConsensusParams(ctx)
				cp.Block.MinTxsInBlock = 10
				app.StoreConsensusParams(ctx, cp)
				return newVM, err
			}

			if upgradeName == "v6.0.5" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				cp := app.GetConsensusParams(ctx)
				cp.Block.MaxGasWanted = 50000000 // 50 mil
				app.StoreConsensusParams(ctx, cp)
				return newVM, err
			}

			if upgradeName == sidioraFeeTokenUpgrade {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				denom := layerxbridgetypes.SidioraDenom()
				asset, found := app.LayerXBridgeKeeper.GetAssetByDenom(ctx, denom)
				if !found {
					return newVM, fmt.Errorf("upgrade %s requires the registered Sidiora remote asset of %s", upgradeName, denom)
				}
				if _, err := app.LayerXBridgeKeeper.EnsureSidioraDenom(ctx, asset.ChainID); err != nil {
					return newVM, err
				}
				return newVM, nil
			}

			if upgradeName == xwebUpgrade {
				return app.runXWebUpgrade(ctx, fromVM)
			}

			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}

	// The activation plan is registered under its own name rather than a tag, so
	// that the tag list, the latest upgrade and the existing plans are untouched.
	app.UpgradeKeeper.SetUpgradeHandler(ActivationUpgrade, app.runActivationUpgrade)
}

// runXWebUpgrade runs the module migrations with xweb taken as present, so the
// migrations never initialise it, then initialises it from the default genesis
// unless its state already exists, and serves the xweb precompile from this
// block on.
func (app *App) runXWebUpgrade(ctx sdk.Context, fromVM module.VersionMap) (module.VersionMap, error) {
	initialised := app.xwebInitialised(ctx)
	versions := make(module.VersionMap, len(fromVM)+1)
	for name, version := range fromVM {
		versions[name] = version
	}
	versions[xwebtypes.ModuleName] = xweb.AppModule{}.ConsensusVersion()
	newVM, err := app.mm.RunMigrations(ctx, app.configurator, versions)
	if err != nil {
		return newVM, err
	}
	if !initialised {
		genesis := xwebtypes.DefaultGenesis()
		if err := genesis.Validate(); err != nil {
			return newVM, fmt.Errorf("upgrade %s: %w", xwebUpgrade, err)
		}
		app.XWebKeeper.InitGenesis(ctx, *genesis)
	}
	app.setXWebPrecompile(true)
	return newVM, nil
}

// v68StoreUpgrades mounts the xweb store at the xweb upgrade height.
func v68StoreUpgrades() storetypes.StoreUpgrades {
	return storetypes.StoreUpgrades{
		Added: []string{xwebtypes.StoreKey},
	}
}

// xwebInitialised reports whether the xweb module has state: its genesis or the
// xweb upgrade stored its parameters.
func (app *App) xwebInitialised(ctx sdk.Context) bool {
	return ctx.KVStore(app.GetKey(xwebtypes.StoreKey)).Has(xwebtypes.ParamsKey)
}

// xwebLive reports whether the xweb module is live in the state of ctx: the
// xweb upgrade is done, or the chain started with xweb in its module versions
// and its genesis initialised it.
func (app *App) xwebLive(ctx sdk.Context) bool {
	if app.UpgradeKeeper.GetDoneHeight(ctx, xwebUpgrade) > 0 {
		return true
	}
	if _, known := app.UpgradeKeeper.GetModuleVersionMap(ctx)[xwebtypes.ModuleName]; !known {
		return false
	}
	return app.xwebInitialised(ctx)
}

// refreshXWebPrecompile serves the xweb precompile exactly when the xweb
// module is live in the state of ctx.
func (app *App) refreshXWebPrecompile(ctx sdk.Context) {
	app.setXWebPrecompile(app.xwebLive(ctx))
}

// setXWebPrecompile hands the EVM keeper the custom precompile set with or
// without the xweb entry. An application built without custom precompiles
// keeps none.
func (app *App) setXWebPrecompile(live bool) {
	if app.customPrecompiles == nil {
		return
	}
	app.EvmKeeper.SetCustomPrecompiles(xwebPrecompileSet(app.customPrecompiles, live), LatestUpgrade)
}

// xwebPrecompileSet copies the custom precompile set, leaving out the xweb
// entry unless the xweb module is live.
func xwebPrecompileSet(all map[common.Address]putils.VersionedPrecompiles, live bool) map[common.Address]putils.VersionedPrecompiles {
	address := common.HexToAddress(xwebtypes.PrecompileAddress)
	set := make(map[common.Address]putils.VersionedPrecompiles, len(all))
	for addr, versioned := range all {
		if addr == address && !live {
			continue
		}
		set[addr] = versioned
	}
	return set
}

const v606UpgradeHeight = 151573570

// runActivationUpgrade initialises every module the chain's version map lacks
// from that module's own default genesis, exactly once, and serves the xweb
// precompile from this block on when the module's state says it is live. The
// stores those modules write to are mounted by the activation store loader
// before the block that runs this handler.
func (app *App) runActivationUpgrade(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
	if err != nil {
		return newVM, err
	}
	// The module version map the handler reads predates the upgrade, so the xweb
	// precompile is served on the state the migration just wrote instead.
	if !app.xwebInitialised(ctx) {
		return newVM, fmt.Errorf("upgrade %s: the xweb module holds no parameters", ActivationUpgrade)
	}
	app.setXWebPrecompile(true)
	return newVM, nil
}

// activationStoreUpgrades mounts the store of every module the activation plan
// initialises: the five stores the v6.6 plan mounts and the xweb store the v6.8
// plan mounts, none of which a chain that applied neither plan ever mounted.
func activationStoreUpgrades() storetypes.StoreUpgrades {
	return storetypes.StoreUpgrades{
		Added: append(v66StoreUpgrades().Added, v68StoreUpgrades().Added...),
	}
}

// activationModules lists the modules the activation plan initialises, in the
// order their stores are mounted.
func activationModules() []string {
	return []string{
		layerxcustodytypes.ModuleName, layerxanchortypes.ModuleName,
		layerxexchangetypes.ModuleName, layerxbridgetypes.ModuleName,
		launchpadtypes.ModuleName, xwebtypes.ModuleName,
	}
}

// activationPrecompileGate declares the custom precompiles the activation plan
// brings online and, for each, the fork module whose state it serves: the
// stateless evidence precompile is gated with the anchor module whose batch
// headers and receipts it verifies, and the fee-token precompile with the bridge
// module that holds the Sidiora asset it prices.
func activationPrecompileGate() evmkeeper.CustomPrecompileActivation {
	return evmkeeper.CustomPrecompileActivation{
		Upgrade: ActivationUpgrade,
		Modules: map[common.Address][]string{
			common.HexToAddress(verifyprecompile.LayerXVerifyAddress): {layerxanchortypes.ModuleName},
			common.HexToAddress(layerxcustodytypes.CustodyAddress):    {layerxcustodytypes.ModuleName},
			common.HexToAddress(anchorprecompile.LayerXAnchorAddress): {layerxanchortypes.ModuleName},
			common.HexToAddress(layerxexchangetypes.ExchangeAddress):  {layerxexchangetypes.ModuleName},
			common.HexToAddress(layerxbridgetypes.BridgeAddress):      {layerxbridgetypes.ModuleName},
			common.HexToAddress(launchpadtypes.LaunchpadAddress):      {launchpadtypes.ModuleName},
			common.HexToAddress(feetokenprecompile.FeeTokenAddress):   {layerxbridgetypes.ModuleName},
			common.HexToAddress(xwebtypes.PrecompileAddress):          {xwebtypes.ModuleName},
		},
	}
}

// applyActivationUpgrade applies the activation plan in the block whose height
// the upgrade info file on disk names, which is the same file the store loader
// reads to mount the stores the plan adds. That file is an operator's, not a
// chain record, so the upgrade module's own begin blocker never sees the plan and
// the application applies it here, before the module begin blockers, so that
// every module the plan initialises is live for the rest of its own block. The
// keeper's apply path runs the handler, writes the module version map, records
// the done height and clears any plan the store carries, and the done height and
// the version map it writes are what refuse a second application while the file
// stays on disk. A file that names another plan or another height, a chain that
// has applied the plan, and a chain whose version map already carries every
// module the plan initialises are all left untouched.
func (app *App) applyActivationUpgrade(ctx sdk.Context) {
	if ctx.IsTracing() {
		return
	}
	// The two state reads come before the file, so a chain that carries the fork
	// reads no file in any of its blocks.
	if app.UpgradeKeeper.GetDoneHeight(ctx, ActivationUpgrade) != 0 {
		return
	}
	if app.activationModulesPresent(ctx) {
		return
	}
	info, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Errorf("unable to read the upgrade info of the %s upgrade from filesystem: %w", ActivationUpgrade, err))
	}
	if info.Name != ActivationUpgrade || info.Height != ctx.BlockHeight() {
		return
	}
	if missing := app.unmountedActivationStores(); len(missing) > 0 {
		panic(fmt.Errorf("upgrade %s is due at height %d but the store loader did not add the activation stores %s: the upgrade info height must equal the last committed height plus one when the process starts",
			ActivationUpgrade, info.Height, strings.Join(missing, ", ")))
	}
	plan := upgradetypes.Plan{Name: ActivationUpgrade, Height: ctx.BlockHeight()}
	logger.Info("applying upgrade", "name", plan.Name, "at", plan.DueAt())
	app.UpgradeKeeper.ApplyUpgrade(ctx, plan)
}

// activationModulesPresent reports whether the module version map of ctx already
// carries every module the activation plan initialises.
func (app *App) activationModulesPresent(ctx sdk.Context) bool {
	versions := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	for _, name := range activationModules() {
		if _, known := versions[name]; !known {
			return false
		}
	}
	return true
}

// unmountedActivationStores names the stores the activation plan adds that the
// commit multistore does not carry. Only a process that read the plan's upgrade
// info file at one above its last committed height mounts them, through the
// upgrade store loader the store-loader wiring installs for the plan's name.
func (app *App) unmountedActivationStores() []string {
	cms := app.CommitMultiStore()
	var missing []string
	for _, name := range activationStoreUpgrades().Added {
		key := app.GetKey(name)
		if key == nil || cms.GetCommitKVStore(key) == nil {
			missing = append(missing, name)
		}
	}
	return missing
}
