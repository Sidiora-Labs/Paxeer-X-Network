package app

import (
	"fmt"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	launchpadprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
)

// V6120Upgrade is the plan from whose height the chain executes the custody
// module's governance proposal, moves the usid mint authority to the bridge
// module account, migrates the legacy SID ERC-20 balances of the embedded
// holder snapshot into usid, runs the pointers, launchpad, pricefeed, perps and
// spot hooks in that order over the stores the plan adds, and then serves the
// v6.12.0 precompile set. It carries no entry in the embedded tag list.
const V6120Upgrade = "v6.12.0"

// v6120Hook is one named step of the v6.12.0 plan; a nil run is unbound.
type v6120Hook struct {
	name string
	run  func(sdk.Context) error
}

// v6120Hooks returns the hook table the v6.12.0 plan runs, in order. An
// application whose table was never bound gets the five hooks, each unbound.
func (app *App) v6120Hooks() []v6120Hook {
	if app.v6120HookTable != nil {
		return app.v6120HookTable
	}
	return []v6120Hook{
		{name: "pointers"},
		{name: "launchpad"},
		{name: "pricefeed"},
		{name: "perps"},
		{name: "spot"},
	}
}

// bindV6120Hooks binds the five hooks of the v6.12.0 plan to the pointer step
// and the launchpad, pricefeed, perps and spot keepers of the application.
func (app *App) bindV6120Hooks() {
	app.v6120HookTable = []v6120Hook{
		{name: "pointers", run: app.createV6120Pointers},
		{name: "launchpad", run: func(ctx sdk.Context) error {
			return launchpadkeeper.MigrateV6120(ctx, *app.LaunchpadKeeper, v6120LaunchpadEVM{&app.EvmKeeper})
		}},
		{name: "pricefeed", run: func(ctx sdk.Context) error {
			return pricefeedkeeper.InitV6120(ctx, app.PricefeedKeeper)
		}},
		{name: "perps", run: func(ctx sdk.Context) error {
			return perpskeeper.SeedV6120(ctx, *app.PerpsKeeper)
		}},
		{name: "spot", run: func(ctx sdk.Context) error {
			return spotkeeper.InitV6120(ctx, *app.SpotKeeper)
		}},
	}
}

// v6120LaunchpadEVM is the EVM keeper as the launchpad migration reads it,
// installing the retirement runtime at a retired launchpad address.
type v6120LaunchpadEVM struct{ *evmkeeper.Keeper }

func (e v6120LaunchpadEVM) InstallRetired(ctx sdk.Context, retired, replacement common.Address) error {
	return launchpadprecompile.InstallRetired(ctx, e.Keeper, retired, replacement)
}

// runV6120Upgrade refuses the plan while any hook is unbound, before writing
// any state; otherwise it runs the module migrations, moves the usid mint
// authority to the bridge module account, migrates the legacy SID balances,
// runs every hook in order and serves the v6.12.0 precompile set. A hook
// failure returns no version map, so the upgrade keeper records no done height.
func (app *App) runV6120Upgrade(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	hooks := app.v6120Hooks()
	for _, hook := range hooks {
		if hook.run == nil {
			return nil, fmt.Errorf("upgrade %s: hook %s is not bound", V6120Upgrade, hook.name)
		}
	}
	newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
	if err != nil {
		return newVM, err
	}
	if err := app.moveSidioraMintAuthority(ctx); err != nil {
		return newVM, fmt.Errorf("upgrade %s: %w", V6120Upgrade, err)
	}
	if err := app.migrateSIDHolders(ctx, sidHoldersJSON); err != nil {
		return newVM, fmt.Errorf("upgrade %s: %w", V6120Upgrade, err)
	}
	for _, hook := range hooks {
		if err := hook.run(ctx); err != nil {
			return nil, fmt.Errorf("upgrade %s: %s: %w", V6120Upgrade, hook.name, err)
		}
	}
	app.setV6120Live(true)
	return newVM, nil
}
