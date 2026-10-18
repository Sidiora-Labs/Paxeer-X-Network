package spot_test

import (
	"testing"

	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
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
