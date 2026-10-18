// Package pricefeed carries validator-signed index and mark prices on chain:
// each bonded validator's feed account submits observations, and the
// mid-block hook aggregates the fresh observations of the bonded set into
// one price per enabled market. State and genesis are plain JSON.
package pricefeed

import (
	"encoding/json"
	"fmt"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
)

var (
	_ module.AppModule         = AppModule{}
	_ module.AppModuleBasic    = AppModuleBasic{}
	_ module.MidBlockAppModule = AppModule{}
)

type AppModuleBasic struct{}

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) { types.RegisterCodec(cdc) }

func (AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	encoded, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(err)
	}
	return encoded
}

// ValidateGenesis checks the params and refuses a feed key repeated for an
// operator or a feed account registered to two operators.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genesis types.GenesisState
	if err := json.Unmarshal(bz, &genesis); err != nil {
		return types.ErrInvalidGenesis.Wrapf("failed to unmarshal %s genesis state: %v", types.ModuleName, err)
	}
	if err := genesis.Params.Validate(); err != nil {
		return err
	}
	operators := make(map[string]struct{}, len(genesis.FeedKeys))
	feedAccounts := make(map[string]struct{}, len(genesis.FeedKeys))
	for _, entry := range genesis.FeedKeys {
		operator := entry.Operator.String()
		if _, seen := operators[operator]; seen {
			return types.ErrInvalidGenesis.Wrapf("duplicate feed key for operator %s", operator)
		}
		operators[operator] = struct{}{}
		if _, seen := feedAccounts[entry.FeedKey.FeedAccount]; seen {
			return types.ErrInvalidGenesis.Wrapf("duplicate feed account %s", entry.FeedKey.FeedAccount)
		}
		feedAccounts[entry.FeedKey.FeedAccount] = struct{}{}
	}
	return nil
}

func (am AppModuleBasic) ValidateGenesisStream(cdc codec.JSONCodec, config client.TxEncodingConfig, genesisCh <-chan json.RawMessage) error {
	for genesis := range genesisCh {
		if err := am.ValidateGenesis(cdc, config, genesis); err != nil {
			return err
		}
	}
	return nil
}

func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

func (AppModuleBasic) GetTxCmd() *cobra.Command { return nil }

func (AppModuleBasic) GetQueryCmd() *cobra.Command { return nil }

type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule { return AppModule{keeper: k} }

func (AppModule) Route() sdk.Route { return sdk.Route{} }

func (AppModule) QuerierRoute() string { return types.ModuleName }

func (AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier { return nil }

// RegisterServices routes MsgSubmitPrices and MsgSetParams to the keeper.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
}

func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	var genesis types.GenesisState
	if err := json.Unmarshal(gs, &genesis); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(err)
	}
	return []abci.ValidatorUpdate{}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	encoded, err := json.Marshal(genesis)
	if err != nil {
		panic(err)
	}
	return encoded
}

func (am AppModule) ExportGenesisStream(ctx sdk.Context, cdc codec.JSONCodec) <-chan json.RawMessage {
	ch := make(chan json.RawMessage)
	go func() {
		ch <- am.ExportGenesis(ctx, cdc)
		close(ch)
	}()
	return ch
}

func (AppModule) ConsensusVersion() uint64 { return 1 }

// MidBlock aggregates the block's observations after every transaction and
// before the end block.
func (am AppModule) MidBlock(ctx sdk.Context, _ int64) {
	MidBlocker(ctx, am.keeper)
}
