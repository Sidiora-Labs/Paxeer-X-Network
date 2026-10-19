package spot_test

import (
	"encoding/json"
	"testing"

	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/stretchr/testify/require"
)

// invariantRecorder records the routes RegisterInvariants registers.
type invariantRecorder struct{ routes []string }

func (r *invariantRecorder) RegisterRoute(moduleName, route string, _ sdk.Invariant) {
	r.routes = append(r.routes, moduleName+"/"+route)
}

func TestModuleRegistersInvariants(t *testing.T) {
	am := spot.NewAppModule(keeper.NewKeeper(sdk.NewKVStoreKey(types.StoreKey), nil, nil, pricefeedkeeper.Keeper{}))

	invariants := &invariantRecorder{}
	am.RegisterInvariants(invariants)
	require.Equal(t, []string{
		"spot/" + keeper.SolvencyInvariantName,
		"spot/" + keeper.ShareBackingInvariantName,
		"spot/" + keeper.EscrowInvariantName,
	}, invariants.routes)
}

func TestModuleBasics(t *testing.T) {
	basic := spot.NewAppModuleBasic()
	require.Equal(t, types.ModuleName, basic.Name())
	require.Equal(t, "spot", basic.Name())

	bz := basic.DefaultGenesis(nil)
	var gs types.GenesisState
	require.NoError(t, json.Unmarshal(bz, &gs))
	require.Equal(t, *types.DefaultGenesis(), gs)
	require.NoError(t, basic.ValidateGenesis(nil, nil, bz))

	require.Error(t, basic.ValidateGenesis(nil, nil, json.RawMessage(`{`)))
	invalid := types.DefaultGenesis()
	invalid.NextOrderID = 0
	require.ErrorIs(t, basic.ValidateGenesis(nil, nil, mustJSON(t, invalid)), types.ErrInvalidGenesis)
}

func TestGenesisRoundTrip(t *testing.T) {
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(10)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pricefeed, ctx := pricefeedtestutil.NewKeeper(app, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, pricefeed)
	am := spot.NewAppModule(k)

	input := am.DefaultGenesis(nil)
	require.Empty(t, am.InitGenesis(ctx, nil, input))
	exported := am.ExportGenesis(ctx, nil)
	require.JSONEq(t, string(input), string(exported))
	require.Equal(t, types.DefaultGenesis(), k.ExportGenesis(ctx))
	require.NoError(t, am.ValidateGenesis(nil, nil, exported))
}

func mustJSON(t *testing.T, gs *types.GenesisState) json.RawMessage {
	t.Helper()
	bz, err := json.Marshal(gs)
	require.NoError(t, err)
	return bz
}
