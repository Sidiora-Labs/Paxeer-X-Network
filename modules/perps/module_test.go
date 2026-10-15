package perps_test

import (
	"encoding/json"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// serviceRecorder records the services RegisterServices registers.
type serviceRecorder struct{ services []string }

func (r *serviceRecorder) RegisterService(sd *grpc.ServiceDesc, _ interface{}) {
	r.services = append(r.services, sd.ServiceName)
}

// invariantRecorder records the routes RegisterInvariants registers.
type invariantRecorder struct{ routes []string }

func (r *invariantRecorder) RegisterRoute(moduleName, route string, _ sdk.Invariant) {
	r.routes = append(r.routes, moduleName+"/"+route)
}

func TestModuleShape(t *testing.T) {
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_perps")).
		WithBlockTime(time.Unix(1_800_000_000, 0))
	am := perps.NewAppModule(keeper.NewKeeper(key, nil, nil))

	var _ module.AppModule = am
	require.Equal(t, "perps", am.Name())
	require.Equal(t, "perps", am.AppModuleBasic.Name())
	require.Equal(t, uint64(1), am.ConsensusVersion())
	require.Equal(t, sdk.Route{}, am.Route())
	require.Equal(t, types.ModuleName, am.QuerierRoute())
	require.Nil(t, am.LegacyQuerierHandler(nil))
	require.Nil(t, am.GetTxCmd())
	require.Nil(t, am.GetQueryCmd())

	msgServer, queryServer := &serviceRecorder{}, &serviceRecorder{}
	am.RegisterServices(module.NewConfigurator(nil, msgServer, queryServer))
	require.Empty(t, msgServer.services)
	require.Empty(t, queryServer.services)

	invariants := &invariantRecorder{}
	am.RegisterInvariants(invariants)
	require.Equal(t, []string{
		"perps/" + keeper.SolvencyInvariantName, "perps/" + keeper.OpenInterestInvariantName,
		"perps/" + keeper.BookInvariantName, "perps/" + keeper.OrdersInvariantName,
		"perps/" + keeper.PositionsInvariantName, "perps/" + keeper.FundingInvariantName,
		"perps/" + keeper.CountersInvariantName, "perps/" + keeper.DeterminismInvariantName,
		"perps/" + keeper.IndexesInvariantName,
	}, invariants.routes)

	defaultGenesis := am.DefaultGenesis(nil)
	want, err := json.Marshal(types.DefaultGenesis())
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(defaultGenesis))
	require.NoError(t, am.ValidateGenesis(nil, nil, defaultGenesis))

	genesisCh := make(chan json.RawMessage, 2)
	genesisCh <- defaultGenesis
	genesisCh <- json.RawMessage(`{"params":`)
	close(genesisCh)
	require.ErrorIs(t, am.ValidateGenesisStream(nil, nil, genesisCh), types.ErrInvalidGenesis)

	err = am.ValidateGenesis(nil, nil, json.RawMessage(`{"params":`))
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_GENESIS")

	invalid := types.DefaultGenesis()
	invalid.Params.CollateralToken = common.Address{}
	encoded, err := json.Marshal(invalid)
	require.NoError(t, err)
	err = am.ValidateGenesis(nil, nil, encoded)
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.ErrorIs(t, err, types.ErrInvalidParams)

	require.Panics(t, func() { am.InitGenesis(ctx, nil, json.RawMessage(`{"params":`)) })
	require.Panics(t, func() { am.InitGenesis(ctx, nil, encoded) })

	require.Equal(t, []abci.ValidatorUpdate{}, am.InitGenesis(ctx, nil, defaultGenesis))
	exported := am.ExportGenesis(ctx, nil)
	require.JSONEq(t, string(defaultGenesis), string(exported))

	var streamed []json.RawMessage
	for chunk := range am.ExportGenesisStream(ctx, nil) {
		streamed = append(streamed, chunk)
	}
	require.Len(t, streamed, 1)
	require.JSONEq(t, string(exported), string(streamed[0]))
}
