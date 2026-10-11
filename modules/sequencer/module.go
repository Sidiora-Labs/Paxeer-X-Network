// Package sequencer pins the sequencer's receipt keys on chain. The list lives
// in the sequencer param subspace, is set at genesis and changes only through
// a governance param change.
package sequencer

import (
	"bytes"
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

// genesisState is the JSON form of the sequencer genesis. A genesis with a
// leader list is the activated module: it decodes to types.GenesisState, is
// written to the module store and pins the same receipt keys in the
// ReceiptKeys param. A genesis with no leader is the module before
// activation: it pins only the ReceiptKeys param. An absent lastSeqHash is
// the zero hash.
type genesisState struct {
	Leaders     [][]byte           `json:"leaders,omitempty"`
	ReceiptKeys keeper.ReceiptKeys `json:"receipt_keys"`
	LastSeq     uint64             `json:"last_seq,omitempty"`
	LastSeqHash []byte             `json:"last_seq_hash,omitempty"`
}

// decodeGenesis decodes bz and returns the receipt key list for the param and,
// when the genesis carries a leader list, the validated activation state.
func decodeGenesis(bz json.RawMessage) (keeper.ReceiptKeys, *types.GenesisState, error) {
	var genesis genesisState
	if err := json.Unmarshal(bz, &genesis); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	if err := genesis.ReceiptKeys.Validate(); err != nil {
		return nil, nil, err
	}
	if len(genesis.Leaders) == 0 {
		if genesis.LastSeq != 0 || len(genesis.LastSeqHash) != 0 {
			return nil, nil, types.ErrInvalidGenesis.Wrapf("lastSeq %d or lastSeqHash %X with no leader list", genesis.LastSeq, genesis.LastSeqHash)
		}
		return genesis.ReceiptKeys, nil, nil
	}
	if len(genesis.LastSeqHash) != 0 && len(genesis.LastSeqHash) != types.HashLength {
		return nil, nil, types.ErrInvalidGenesis.Wrapf("lastSeqHash is %d bytes, want %d", len(genesis.LastSeqHash), types.HashLength)
	}
	state := types.GenesisState{
		Leaders:     make([][32]byte, len(genesis.Leaders)),
		ReceiptKeys: make([]types.ReceiptKey, len(genesis.ReceiptKeys)),
		LastSeq:     genesis.LastSeq,
	}
	copy(state.LastSeqHash[:], genesis.LastSeqHash)
	for i, leader := range genesis.Leaders {
		if len(leader) != len(state.Leaders[i]) {
			return nil, nil, types.ErrInvalidGenesis.Wrapf("leader %d is %d bytes, want %d", i, len(leader), len(state.Leaders[i]))
		}
		copy(state.Leaders[i][:], leader)
	}
	for i, entry := range genesis.ReceiptKeys {
		key := &state.ReceiptKeys[i]
		copy(key.KeyId[:], entry.KeyId)
		copy(key.PublicKey[:], entry.PublicKey)
		key.FirstHeight = entry.FirstHeight
		key.LastHeight = entry.LastHeight
	}
	if err := state.Validate(); err != nil {
		return nil, nil, err
	}
	return genesis.ReceiptKeys, &state, nil
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
	_, _, err := decodeGenesis(bz)
	return err
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

// AppModule holds the sequencer keeper and param subspace. It routes no
// message: the batch reaches the keeper only as tx 0 of a block, and the
// receipt key list has no Msg write path.
type AppModule struct {
	AppModuleBasic
	keeper     keeper.Keeper
	paramSpace paramtypes.Subspace
}

// NewAppModule registers the ReceiptKeys param on the subspace.
func NewAppModule(k keeper.Keeper, paramSpace paramtypes.Subspace) AppModule {
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(keeper.ParamKeyTable())
	}
	return AppModule{keeper: k, paramSpace: paramSpace}
}

func (AppModule) Route() sdk.Route { return sdk.Route{} }

func (AppModule) QuerierRoute() string { return types.ModuleName }

func (AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier { return nil }

func (AppModule) RegisterServices(_ module.Configurator) {}

func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// InitGenesis writes the activation state to the module store and the same
// receipt key list to the ReceiptKeys param, so the batch signature check and
// the param read one key set.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	keys, state, err := decodeGenesis(gs)
	if err != nil {
		panic(err)
	}
	if state != nil {
		if err := am.keeper.InitGenesis(ctx, *state); err != nil {
			panic(err)
		}
	}
	if err := keeper.InitReceiptKeys(ctx, am.paramSpace, keys); err != nil {
		panic(err)
	}
	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the leader list, lastSeq and lastSeqHash from the
// module store and the governed ReceiptKeys param; the zero lastSeqHash is
// left out.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	state, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	keys := keeper.GetReceiptKeys(ctx, am.paramSpace)
	if keys == nil {
		keys = keeper.ReceiptKeys{}
	}
	genesis := genesisState{ReceiptKeys: keys, LastSeq: state.LastSeq}
	if state.LastSeqHash != ([types.HashLength]byte{}) {
		genesis.LastSeqHash = bytes.Clone(state.LastSeqHash[:])
	}
	for _, leader := range state.Leaders {
		genesis.Leaders = append(genesis.Leaders, bytes.Clone(leader[:]))
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

// NewParamChangeProposalHandler wraps the params proposal handler next so a
// change to the sequencer ReceiptKeys reaches the store only when the new list
// keeps every height at or below the current block pinned as before; the
// accepted list is written to the keeper store with the param, so the batch
// signature check and the param hold one key set. Any other change passes
// through to next unchanged.
func NewParamChangeProposalHandler(k keeper.Keeper, paramSpace paramtypes.Subspace, next govtypes.Handler) govtypes.Handler {
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
		rotated := keeper.GetReceiptKeys(cacheCtx, paramSpace)
		if err := keeper.ValidateRotation(current, rotated, ctx.BlockHeight()); err != nil {
			return err
		}
		if err := k.SetReceiptKeys(cacheCtx, rotated); err != nil {
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
