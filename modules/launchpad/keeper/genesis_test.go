package keeper_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// newSurfacedEnv is newEnv with the generation 2 FeeAccumulator registered as
// a surface, so the POOL_ROLE grants of CreateMarket export as valid role
// members.
func newSurfacedEnv(t *testing.T) *env {
	e := newEnv(t)
	require.NoError(t, e.k.SetSurface(e.ctx, keeper.Generation2Surfaces[types.RoleFeeAccumulator],
		keeper.Surface{Role: types.RoleFeeAccumulator, Generation: types.Generation2}))
	return e
}

// TestGenesisPoolMetadataRoundTrip creates two pools, exports the module,
// imports it into a fresh store and reads back the PoolRegistry record and
// token index of each pool unchanged.
func TestGenesisPoolMetadataRoundTrip(t *testing.T) {
	e := newSurfacedEnv(t)
	creator := e.account(200_000_000)
	first := e.create(creator, types.FeeStrategyClaim).Denom
	e.at(time.Minute)
	e.ctx = e.ctx.WithBlockHeight(9)
	second := e.create(creator, types.FeeStrategyAirdrop).Denom
	pools := []common.Address{e.pool(first), e.pool(second)}
	tokens := []common.Address{e.token(first), e.token(second)}

	created := make([]keeper.PoolMetadata, len(pools))
	for index, pool := range pools {
		metadata, found, err := e.k.GetPoolMetadata(e.ctx, pool)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, e.evmAddress(creator), metadata.Creator)
		require.Equal(t, tokens[index], metadata.Token)
		created[index] = metadata
	}
	require.Equal(t, genesisTime.Unix(), created[0].CreatedAt)
	require.Equal(t, int64(1), created[0].CreatedBlock)
	require.Equal(t, genesisTime.Add(time.Minute).Unix(), created[1].CreatedAt)
	require.Equal(t, int64(9), created[1].CreatedBlock)
	require.NotEqual(t, created[0].NFTID, created[1].NFTID)

	exported := e.k.ExportGenesis(e.ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.PoolMetadata, 2)
	raw, err := json.Marshal(exported)
	require.NoError(t, err)
	var decoded types.GenesisState
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NoError(t, decoded.Validate())

	fresh, ctx := launchpadtest.NewKeeper(testkeeper.EVMTestApp, e.ctx)
	for _, pool := range pools {
		_, found, err := fresh.GetPoolMetadata(ctx, pool)
		require.NoError(t, err)
		require.False(t, found)
	}
	fresh.InitGenesis(ctx, decoded)
	for index, pool := range pools {
		metadata, found, err := fresh.GetPoolMetadata(ctx, pool)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, created[index], metadata)
		registered, found := fresh.PoolByToken(ctx, tokens[index])
		require.True(t, found)
		require.Equal(t, pool, registered)
	}
	sameJSON(t, exported, fresh.ExportGenesis(ctx))
}

// TestGenesisRejectsPoolWithoutMetadata refuses an export whose pool
// metadata list lost an entry or the whole field.
func TestGenesisRejectsPoolWithoutMetadata(t *testing.T) {
	e := newSurfacedEnv(t)
	creator := e.account(200_000_000)
	pool := e.pool(e.create(creator, types.FeeStrategyClaim).Denom)
	exported := e.k.ExportGenesis(e.ctx)
	require.Len(t, exported.PoolMetadata, 1)

	raw, err := json.Marshal(exported)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	delete(fields, "pool_metadata")
	raw, err = json.Marshal(fields)
	require.NoError(t, err)
	var stripped types.GenesisState
	require.NoError(t, json.Unmarshal(raw, &stripped))
	err = stripped.Validate()
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.Contains(t, err.Error(), "pool metadata missing for pool "+pool.Hex())

	fresh, ctx := launchpadtest.NewKeeper(testkeeper.EVMTestApp, e.ctx)
	require.Panics(t, func() { fresh.InitGenesis(ctx, stripped) })
}
