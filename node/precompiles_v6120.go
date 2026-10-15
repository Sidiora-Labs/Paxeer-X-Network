package app

import (
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	layerxexchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// v6120PrecompileGate declares the custom precompiles the v6.12.0 plan brings
// online and, for each, the module whose state it serves: the price feed serves
// the pricefeed module and the five spot precompiles serve the spot module. The
// exchange and launchpad addresses stay gated by the activation plan, since one
// address cannot be named by two entries, and the vault share and router
// addresses of the spot domain carry no code at any height.
func v6120PrecompileGate() evmkeeper.CustomPrecompileActivation {
	return evmkeeper.CustomPrecompileActivation{
		Upgrade: V6120Upgrade,
		Modules: map[common.Address][]string{
			common.HexToAddress(gigaV6120PricefeedAddress):        {pricefeedtypes.ModuleName},
			common.HexToAddress(spottypes.PECORAddress):           {spottypes.ModuleName},
			common.HexToAddress(spottypes.PECOROrdersAddress):     {spottypes.ModuleName},
			common.HexToAddress(spottypes.PECORStopOrdersAddress): {spottypes.ModuleName},
			common.HexToAddress(spottypes.PECORQuoterAddress):     {spottypes.ModuleName},
			common.HexToAddress(spottypes.PECORVaultAddress):      {spottypes.ModuleName},
		},
	}
}

// exchangeVersionSwitch versions the exchange address: once the v6.12.0 set is
// live it serves the perpetuals contract and keeps the v6.9 exchange for the
// heights below the v6.12.0 plan; before, it serves the v6.9 exchange alone.
func exchangeVersionSwitch(perps, legacy vm.PrecompiledContract, live bool) putils.VersionedPrecompiles {
	return v6120VersionSwitch(perps, legacy, live)
}

// launchpadVersionSwitch versions the launchpad address: once the v6.12.0 set
// is live it serves the second-generation launchpad and keeps the first for the
// heights below the v6.12.0 plan; before, it serves the first alone.
func launchpadVersionSwitch(g2, g1 vm.PrecompiledContract, live bool) putils.VersionedPrecompiles {
	return v6120VersionSwitch(g2, g1, live)
}

func v6120VersionSwitch(next, legacy vm.PrecompiledContract, live bool) putils.VersionedPrecompiles {
	if live {
		return putils.VersionedPrecompiles{
			LatestUpgrade:     next,
			ActivationUpgrade: legacy,
			V6120Upgrade:      next,
		}
	}
	return putils.VersionedPrecompiles{
		LatestUpgrade:     legacy,
		ActivationUpgrade: legacy,
	}
}

// v6120PrecompileSet copies the custom precompile set through the xweb switch,
// versions the exchange and launchpad addresses that next carries through their
// switches, and, once the v6.12.0 set is live, adds every address the v6.12.0
// activation entry gates that next carries, keyed to the latest upgrade and to
// the v6.12.0 plan; the activation entry still withholds them below the done
// height. Each next entry holds the v6.12.0 contract first and, for the
// exchange and launchpad addresses, the contract it replaces second.
func v6120PrecompileSet(all map[common.Address]putils.VersionedPrecompiles, next map[common.Address][2]vm.PrecompiledContract, xwebLive, v6120Live bool) map[common.Address]putils.VersionedPrecompiles {
	set := xwebPrecompileSet(all, xwebLive)
	exchange := common.HexToAddress(layerxexchangetypes.ExchangeAddress)
	if contracts, ok := next[exchange]; ok {
		set[exchange] = exchangeVersionSwitch(contracts[0], contracts[1], v6120Live)
	}
	launchpad := common.HexToAddress(launchpadtypes.LaunchpadAddress)
	if contracts, ok := next[launchpad]; ok {
		set[launchpad] = launchpadVersionSwitch(contracts[0], contracts[1], v6120Live)
	}
	if !v6120Live {
		return set
	}
	for addr := range v6120PrecompileGate().Modules {
		contracts, ok := next[addr]
		if !ok {
			continue
		}
		set[addr] = putils.VersionedPrecompiles{
			LatestUpgrade: contracts[0],
			V6120Upgrade:  contracts[0],
		}
	}
	return set
}

// setV6120Live records whether the v6.12.0 set is live and hands the EVM keeper
// the custom precompile set it selects. The v6.12.0 handler's module migrations
// leave the xweb module initialised, so xweb is served exactly when the v6.12.0
// set is; a node that switches the set off reselects xweb from state through
// RefreshPrecompiles. An application built without custom precompiles keeps
// none.
func (app *App) setV6120Live(live bool) {
	app.v6120Live = live
	if app.customPrecompiles == nil {
		return
	}
	app.EvmKeeper.SetCustomPrecompiles(v6120PrecompileSet(app.customPrecompiles, app.v6120Next, live, live), LatestUpgrade)
}

// v6120LiveInState reports whether the state of ctx records the v6.12.0 plan
// as done. Only the load path and InitChainer read it; the handler runs before
// the upgrade keeper records the done height and sets the switch itself.
func (app *App) v6120LiveInState(ctx sdk.Context) bool {
	return app.UpgradeKeeper.GetDoneHeight(ctx, V6120Upgrade) > 0
}
