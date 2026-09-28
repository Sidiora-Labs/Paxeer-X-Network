package app

import (
	"embed"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	evmconfig "github.com/sidiora-labs/paxeer-network/modules/evm/config"
	evmkeeper "github.com/sidiora-labs/paxeer-network/modules/evm/keeper"
	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	layerxanchortypes "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	"github.com/sidiora-labs/paxeer-network/modules/xweb"
	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	appparams "github.com/sidiora-labs/paxeer-network/node/params"
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

// V610Upgrade is the plan that writes the chain's operating values: the anchor
// module's chain and network identifiers and the custody module's network
// identifier from the EVM chain identifier the application derives for its own
// chain id, the launchpad module's quote denomination from the base coin unit,
// the shortened governance voting periods and the initial web-search attestor
// set with its threshold and its unpause. It adds, deletes and renames no store
// and, like the activation plan, carries no entry in the embedded tag list.
const V610Upgrade = "v6.10"

// The governance voting periods the v6.10 plan writes. Every other governance
// parameter, the deposit and the tally parameters among them, is left as the
// plan finds it.
const (
	V610VotingPeriod          = time.Hour
	V610ExpeditedVotingPeriod = 20 * time.Minute
)

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
	// The v6.10 plan is registered the same way, beside the activation plan and
	// the handlers the tag list registers.
	app.UpgradeKeeper.SetUpgradeHandler(V610Upgrade, app.runV610Upgrade)
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

// runV610Upgrade writes the chain's operating values in the block the plan
// applies: the anchor module's chain and network identifiers and the custody
// module's network identifier from the EVM chain identifier the application
// derives for its own chain id, the launchpad module's quote denomination from
// the base coin unit, the governance voting periods and the initial web-search
// attestor set. Every other field of those parameter sets is written back as it
// was read, and the plan adds no store, so the module migrations it runs first
// are the only thing that can change a module's version.
func (app *App) runV610Upgrade(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
	if err != nil {
		return newVM, err
	}
	evmChainID := evmconfig.GetEVMChainID(app.ChainID)
	if !evmChainID.IsUint64() || evmChainID.Uint64() > math.MaxUint32 {
		return newVM, fmt.Errorf("upgrade %s: the EVM chain id %s does not fit a network id", V610Upgrade, evmChainID)
	}
	chainID := evmChainID.Uint64()

	anchor := app.LayerXAnchorKeeper.GetParams(ctx)
	anchor.PaxeerChainID = chainID
	anchor.NetworkID = uint32(chainID)
	if err := app.LayerXAnchorKeeper.SetParams(ctx, anchor); err != nil {
		return newVM, fmt.Errorf("upgrade %s: anchor parameters: %w", V610Upgrade, err)
	}

	custody := app.LayerXCustodyKeeper.GetParams(ctx)
	custody.NetworkId = uint32(chainID)
	if err := app.LayerXCustodyKeeper.SetParams(ctx, custody); err != nil {
		return newVM, fmt.Errorf("upgrade %s: custody parameters: %w", V610Upgrade, err)
	}

	launchpad := app.LaunchpadKeeper.GetParams(ctx)
	launchpad.QuoteDenom = appparams.BaseCoinUnit
	if err := app.LaunchpadKeeper.UpdateParams(ctx, app.LaunchpadKeeper.Authority(), launchpad); err != nil {
		return newVM, fmt.Errorf("upgrade %s: launchpad parameters: %w", V610Upgrade, err)
	}

	voting := app.GovKeeper.GetVotingParams(ctx)
	voting.VotingPeriod = V610VotingPeriod
	voting.ExpeditedVotingPeriod = V610ExpeditedVotingPeriod
	app.GovKeeper.SetVotingParams(ctx, voting)

	if err := app.registerXWebAttestors(ctx); err != nil {
		return newVM, err
	}
	return newVM, nil
}

// registerXWebAttestors registers the initial web-search attestor set, sets the
// threshold over it and lifts the module's pause, through the same keeper
// methods a governance message reaches and with the module's own authority, so
// every authority check runs exactly as it does for a proposal. A list with no
// entry writes none of the three and leaves the module as its own genesis left
// it: paused, with an empty set and no threshold.
func (app *App) registerXWebAttestors(ctx sdk.Context) error {
	if len(XWebAttestors) == 0 {
		return nil
	}
	authority := app.XWebKeeper.GetParams(ctx).Authority
	for _, attestor := range XWebAttestors {
		msg := xwebtypes.MsgRegisterAttestor{Authority: authority, Attestor: attestor}
		if err := app.XWebKeeper.RegisterAttestor(ctx, msg); err != nil {
			return fmt.Errorf("upgrade %s: web-search attestor %s: %w", V610Upgrade, attestor.Signer.Hex(), err)
		}
	}
	if err := app.XWebKeeper.SetThreshold(ctx, xwebtypes.MsgSetThreshold{Authority: authority, Threshold: XWebThreshold}); err != nil {
		return fmt.Errorf("upgrade %s: web-search threshold: %w", V610Upgrade, err)
	}
	if err := app.XWebKeeper.Unpause(ctx, xwebtypes.MsgUnpause{Authority: authority}); err != nil {
		return fmt.Errorf("upgrade %s: web-search unpause: %w", V610Upgrade, err)
	}
	return nil
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

// knownUpgradePlans names, in the order they apply, the plans the application
// applies from the upgrade info file an operator leaves on disk.
func knownUpgradePlans() []string {
	return []string{ActivationUpgrade, V610Upgrade}
}

// upgradePlanPending reports whether the named plan is one of those the
// application applies from an upgrade info file and this chain is still to
// apply. The activation plan is refused as well on a chain whose module version
// map already carries every module it initialises, which is every chain started
// from a full genesis and no chain that must still apply it.
func (app *App) upgradePlanPending(ctx sdk.Context, name string) bool {
	known := false
	for _, plan := range knownUpgradePlans() {
		if plan == name {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	if app.UpgradeKeeper.GetDoneHeight(ctx, name) != 0 {
		return false
	}
	if name == ActivationUpgrade && app.activationModulesPresent(ctx) {
		return false
	}
	return true
}

// applyNamedUpgrade applies the plan the upgrade info file on disk names, in the
// block whose height that file names, when the plan is one the application knows
// and this chain is still to apply. It is the same file the store loader reads to
// mount the stores a plan adds. That file is an operator's, not a chain record, so
// the upgrade module's own begin blocker never sees the plan and the application
// applies it here, before the module begin blockers, so that every module the plan
// initialises is live for the rest of its own block. The keeper's apply path runs
// the handler, writes the module version map, records the done height and clears
// any plan the store carries, and the done height and the version map it writes
// are what refuse a second application while the file stays on disk. A tracing
// context, a file that names another plan or another height, a chain that has
// applied the plan the file names, and, for the activation plan, a chain whose
// version map already carries every module it initialises are all left untouched.
func (app *App) applyNamedUpgrade(ctx sdk.Context) {
	if ctx.IsTracing() {
		return
	}
	// The state reads come before the file, so a chain that has applied every plan
	// the application knows reads no file in any of its blocks.
	pending := false
	for _, name := range knownUpgradePlans() {
		if app.upgradePlanPending(ctx, name) {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	info, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Errorf("unable to read the upgrade info from filesystem: %w", err))
	}
	if !app.upgradePlanPending(ctx, info.Name) || info.Height != ctx.BlockHeight() {
		return
	}
	if missing := app.unmountedActivationStores(); len(missing) > 0 {
		panic(fmt.Errorf("upgrade %s is due at height %d but the store loader did not add the activation stores %s: the upgrade info height must equal the last committed height plus one when the process starts",
			info.Name, info.Height, strings.Join(missing, ", ")))
	}
	plan := upgradetypes.Plan{Name: info.Name, Height: ctx.BlockHeight()}
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
