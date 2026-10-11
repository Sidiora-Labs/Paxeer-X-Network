// Package sequencer pins the sequencer's receipt keys on chain. The list lives
// in the sequencer param subspace, is set at genesis and changes only through
// a governance param change.
package sequencer

import (
	"encoding/json"
	"fmt"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types/proposal"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// genesisState is the receipt key part of the sequencer genesis.
type genesisState struct {
	ReceiptKeys keeper.ReceiptKeys `json:"receipt_keys"`
}

type AppModuleBasic struct{}

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

func (AppModuleBasic) RegisterInterfaces(_ cdctypes.InterfaceRegistry) {}

// DefaultGenesis pins no receipt key: activation sets the list.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	encoded, err := json.Marshal(genesisState{ReceiptKeys: keeper.ReceiptKeys{}})
	if err != nil {
		panic(err)
	}
	return encoded
}

func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genesis genesisState
	if err := json.Unmarshal(bz, &genesis); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genesis.ReceiptKeys.Validate()
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

// AppModule holds the sequencer param subspace. It routes no message: the
// receipt key list has no Msg write path.
type AppModule struct {
	AppModuleBasic
	paramSpace paramtypes.Subspace
}

// NewAppModule registers the ReceiptKeys param on the subspace.
func NewAppModule(paramSpace paramtypes.Subspace) AppModule {
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(keeper.ParamKeyTable())
	}
	return AppModule{paramSpace: paramSpace}
}

func (AppModule) Route() sdk.Route { return sdk.Route{} }

func (AppModule) QuerierRoute() string { return types.ModuleName }

func (AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier { return nil }

func (AppModule) RegisterServices(_ module.Configurator) {}

func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	var genesis genesisState
	if err := json.Unmarshal(gs, &genesis); err != nil {
		panic(err)
	}
	if err := keeper.InitReceiptKeys(ctx, am.paramSpace, genesis.ReceiptKeys); err != nil {
		panic(err)
	}
	return []abci.ValidatorUpdate{}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	keys := keeper.GetReceiptKeys(ctx, am.paramSpace)
	if keys == nil {
		keys = keeper.ReceiptKeys{}
	}
	encoded, err := json.Marshal(genesisState{ReceiptKeys: keys})
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

// NewParamChangeProposalHandler wraps the params proposal handler next so a
// change to the sequencer ReceiptKeys reaches the store only when the new list
// keeps every height at or below the current block pinned as before. Any
// other change passes through to next unchanged.
func NewParamChangeProposalHandler(paramSpace paramtypes.Subspace, next govtypes.Handler) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		p, ok := content.(*proposal.ParameterChangeProposal)
		if !ok || !changesReceiptKeys(p) {
			return next(ctx, content)
		}
		cacheCtx, write := ctx.CacheContext()
		if err := next(cacheCtx, content); err != nil {
			return err
		}
		current := keeper.GetReceiptKeys(ctx, paramSpace)
		if err := keeper.ValidateRotation(current, keeper.GetReceiptKeys(cacheCtx, paramSpace), ctx.BlockHeight()); err != nil {
			return err
		}
		write()
		return nil
	}
}

func changesReceiptKeys(p *proposal.ParameterChangeProposal) bool {
	for _, c := range p.Changes {
		if c.Subspace == types.ModuleName && c.Key == string(keeper.KeyReceiptKeys) {
			return true
		}
	}
	return false
}
