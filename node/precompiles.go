package app

import (
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	launchpadv610keeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	layerxbridgekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/keeper"
	layerxcustodykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/keeper"
	layerxexchangekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/keeper"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	xwebkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	perpsprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	pricefeedprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	spotprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	govkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/keeper"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	wasmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/keeper"
)

type PrecompileKeepers struct {
	putils.BankKeeper
	putils.BankMsgServer
	putils.EVMKeeper
	putils.AccountKeeper
	putils.OracleKeeper
	putils.WasmdKeeper
	putils.WasmdViewKeeper
	putils.StakingKeeper
	putils.StakingQuerier
	putils.GovKeeper
	putils.GovMsgServer
	putils.DistributionKeeper
	putils.TransferKeeper
	putils.ClientKeeper
	putils.ConnectionKeeper
	putils.ChannelKeeper
	putils.AnchorKeeper
	txConf          client.TxConfig
	layerxCustody   *layerxcustodykeeper.Keeper
	layerxExchange  *layerxexchangekeeper.Keeper
	layerxBridge    *layerxbridgekeeper.Keeper
	launchpad       *launchpadkeeper.Keeper
	launchpadV610   *launchpadv610keeper.Keeper
	xweb            *xwebkeeper.Keeper
	PerpsKeeper     *perpskeeper.Keeper
	SpotKeeper      *spotkeeper.Keeper
	PricefeedKeeper *pricefeedkeeper.Keeper
}

func NewPrecompileKeepers(a *App) *PrecompileKeepers {
	return &PrecompileKeepers{
		BankKeeper:         a.BankKeeper,
		BankMsgServer:      bankkeeper.NewMsgServerImpl(a.BankKeeper),
		EVMKeeper:          &a.EvmKeeper,
		AccountKeeper:      a.AccountKeeper,
		OracleKeeper:       a.OracleKeeper,
		WasmdKeeper:        wasmkeeper.NewDefaultPermissionKeeper(a.WasmKeeper),
		WasmdViewKeeper:    a.WasmKeeper,
		StakingKeeper:      stakingkeeper.NewMsgServerImpl(a.StakingKeeper),
		StakingQuerier:     stakingkeeper.Querier{Keeper: a.StakingKeeper},
		GovKeeper:          a.GovKeeper,
		GovMsgServer:       govkeeper.NewMsgServerImpl(a.GovKeeper),
		DistributionKeeper: a.DistrKeeper,
		TransferKeeper:     a.TransferKeeper,
		ClientKeeper:       a.IBCKeeper.ClientKeeper,
		ConnectionKeeper:   a.IBCKeeper.ConnectionKeeper,
		ChannelKeeper:      a.IBCKeeper.ChannelKeeper,
		AnchorKeeper:       a.LayerXAnchorKeeper,
		txConf:             a.GetTxConfig(),
		layerxCustody:      a.LayerXCustodyKeeper,
		layerxExchange:     a.LayerXExchangeKeeper,
		layerxBridge:       &a.LayerXBridgeKeeper,
		launchpad:          a.LaunchpadKeeper,
		launchpadV610: launchpadv610keeper.NewKeeper(a.GetKey(launchpadtypes.StoreKey), a.BankKeeper,
			tokenfactorykeeper.NewMsgServerImpl(a.TokenFactoryKeeper), &a.EvmKeeper),
		xweb:            &a.XWebKeeper,
		PerpsKeeper:     a.PerpsKeeper,
		SpotKeeper:      a.SpotKeeper,
		PricefeedKeeper: &a.PricefeedKeeper,
	}
}

func (pk *PrecompileKeepers) BankK() putils.BankKeeper                 { return pk.BankKeeper }
func (pk *PrecompileKeepers) BankMS() putils.BankMsgServer             { return pk.BankMsgServer }
func (pk *PrecompileKeepers) EVMK() putils.EVMKeeper                   { return pk.EVMKeeper }
func (pk *PrecompileKeepers) AccountK() putils.AccountKeeper           { return pk.AccountKeeper }
func (pk *PrecompileKeepers) OracleK() putils.OracleKeeper             { return pk.OracleKeeper }
func (pk *PrecompileKeepers) WasmdK() putils.WasmdKeeper               { return pk.WasmdKeeper }
func (pk *PrecompileKeepers) WasmdVK() putils.WasmdViewKeeper          { return pk.WasmdViewKeeper }
func (pk *PrecompileKeepers) StakingK() putils.StakingKeeper           { return pk.StakingKeeper }
func (pk *PrecompileKeepers) StakingQ() putils.StakingQuerier          { return pk.StakingQuerier }
func (pk *PrecompileKeepers) GovK() putils.GovKeeper                   { return pk.GovKeeper }
func (pk *PrecompileKeepers) GovMS() putils.GovMsgServer               { return pk.GovMsgServer }
func (pk *PrecompileKeepers) DistributionK() putils.DistributionKeeper { return pk.DistributionKeeper }
func (pk *PrecompileKeepers) TransferK() putils.TransferKeeper         { return pk.TransferKeeper }
func (pk *PrecompileKeepers) ClientK() putils.ClientKeeper             { return pk.ClientKeeper }
func (pk *PrecompileKeepers) ConnectionK() putils.ConnectionKeeper     { return pk.ConnectionKeeper }
func (pk *PrecompileKeepers) ChannelK() putils.ChannelKeeper           { return pk.ChannelKeeper }
func (pk *PrecompileKeepers) AnchorK() putils.AnchorKeeper             { return pk.AnchorKeeper }
func (pk *PrecompileKeepers) TxConfig() client.TxConfig                { return pk.txConf }
func (pk *PrecompileKeepers) LayerXCustodyK() *layerxcustodykeeper.Keeper {
	return pk.layerxCustody
}
func (pk *PrecompileKeepers) LayerXExchangeK() *layerxexchangekeeper.Keeper {
	return pk.layerxExchange
}
func (pk *PrecompileKeepers) LayerXBridgeK() *layerxbridgekeeper.Keeper {
	return pk.layerxBridge
}
func (pk *PrecompileKeepers) LaunchpadK() *launchpadkeeper.Keeper {
	return pk.launchpad
}

// LaunchpadV610K is the v6.10 launchpad keeper over the launchpad store key,
// which serves the v6.10 launchpad precompile below the v6.12.0 height.
func (pk *PrecompileKeepers) LaunchpadV610K() *launchpadv610keeper.Keeper {
	return pk.launchpadV610
}

// XWebK hands the xweb precompile the application's xweb keeper.
func (pk *PrecompileKeepers) XWebK() *xwebkeeper.Keeper {
	return pk.xweb
}

// PerpsK hands the perps precompile the application's perps keeper.
func (pk *PrecompileKeepers) PerpsK() *perpskeeper.Keeper {
	return pk.PerpsKeeper
}

// SpotK hands the spot precompiles the application's spot keeper.
func (pk *PrecompileKeepers) SpotK() *spotkeeper.Keeper {
	return pk.SpotKeeper
}

// PriceFeedK hands the pricefeed precompile the application's pricefeed keeper.
func (pk *PrecompileKeepers) PriceFeedK() *pricefeedkeeper.Keeper {
	return pk.PricefeedKeeper
}

var _ putils.XWebKeepers = (*PrecompileKeepers)(nil)
var _ putils.PriceFeedKeepers = (*PrecompileKeepers)(nil)
var _ perpsprecompile.PerpsKeepers = (*PrecompileKeepers)(nil)
var _ spotprecompile.SpotKeepers = (*PrecompileKeepers)(nil)
var _ pricefeedprecompile.PriceFeedKeepers = (*PrecompileKeepers)(nil)
