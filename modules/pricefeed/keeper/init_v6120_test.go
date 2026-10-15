package keeper_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func storeKeys(ctx sdk.Context, k keeper.Keeper) [][]byte {
	iterator := ctx.KVStore(k.StoreKey()).Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, append([]byte(nil), iterator.Key()...))
	}
	return keys
}

func TestInitV6120Seeds(t *testing.T) {
	k, ctx := registryKeeper(t)
	require.Empty(t, storeKeys(ctx, k))

	require.NoError(t, keeper.InitV6120(ctx, k))

	params := k.GetParams(ctx)
	require.Equal(t, types.DefaultParams(types.DefaultAuthority()), params)
	require.Len(t, params.Markets, 18)
	for i, market := range params.Markets {
		require.Equal(t, uint64(i), market.ID)
		require.True(t, market.Enabled)
	}
	encoded, err := json.Marshal(types.DefaultParams(types.DefaultAuthority()))
	require.NoError(t, err)
	require.Equal(t, string(encoded), string(ctx.KVStore(k.StoreKey()).Get(types.ParamsKey)))
	require.Equal(t, [][]byte{types.ParamsKey}, storeKeys(ctx, k))

	bonded := testkeeper.EVMTestApp.StakingKeeper.GetBondedValidatorsByPower(ctx)
	require.NotEmpty(t, bonded)
	for _, validator := range bonded {
		feedKey, ok := k.GetFeedKey(ctx, validator.GetOperator())
		require.False(t, ok)
		require.Equal(t, types.FeedKey{}, feedKey)
	}
}

func TestInitV6120KeepsParams(t *testing.T) {
	k, ctx := registryKeeper(t)
	changed := types.DefaultParams(types.DefaultAuthority())
	changed.MaxPriceAgeMs = 20000
	require.NoError(t, k.SetParams(ctx, changed))
	before := ctx.KVStore(k.StoreKey()).Get(types.ParamsKey)

	require.NoError(t, keeper.InitV6120(ctx, k))
	require.Equal(t, uint64(20000), k.GetParams(ctx).MaxPriceAgeMs)
	require.Equal(t, changed, k.GetParams(ctx))
	require.Equal(t, before, ctx.KVStore(k.StoreKey()).Get(types.ParamsKey))
	require.Equal(t, [][]byte{types.ParamsKey}, storeKeys(ctx, k))
}

func TestInitV6120StartsNotFresh(t *testing.T) {
	k, ctx := registryKeeper(t)
	ctx = ctx.WithBlockTime(time.UnixMilli(1_800_000_000_000))
	require.NoError(t, keeper.InitV6120(ctx, k))

	for m := uint64(0); m < 18; m++ {
		price, err := k.GetPrice(ctx, m)
		require.NoError(t, err)
		require.False(t, price.Fresh, "market %d", m)
		require.True(t, price.Index.IsZero(), "market %d", m)
		require.True(t, price.Mark.IsZero(), "market %d", m)
		require.Equal(t, uint64(0), price.TimestampMs)
		require.Equal(t, int64(0), price.Height)
		require.Equal(t, uint32(0), price.Sources)
	}
}

func TestInitV6120GenesisRoundTrip(t *testing.T) {
	k, ctx := registryKeeper(t)
	require.NoError(t, keeper.InitV6120(ctx, k))

	v1 := setValidator(t, ctx, "genesis 1", stakingtypes.Bonded)
	v2 := setValidator(t, ctx, "genesis 2", stakingtypes.Bonded)
	f1, _, err := k.RegisterFeedKey(ctx, v1, feedPubKey("genesis 1"))
	require.NoError(t, err)
	f2, _, err := k.RegisterFeedKey(ctx, v2, feedPubKey("genesis 2"))
	require.NoError(t, err)

	price := sdk.NewIntWithDecimal(64250, 18)
	observe := func(market uint64, operator sdk.ValAddress, sequence uint64, delta int64) {
		k.Set(ctx, types.ObservationKey(market, operator), types.ObservationRecord{
			Sequence:          sequence,
			IndexPrice:        price.AddRaw(delta),
			MarkPrice:         price.AddRaw(delta + 3),
			SourceTimestampMs: 1_800_000_000_000 + sequence,
			Height:            8,
		})
	}
	observe(3, v2, 7, 2)
	observe(0, v1, 5, 0)
	observe(0, v2, 6, 1)
	k.Set(ctx, types.AggregateKey(0), types.Aggregate{
		IndexPrice:  price,
		MarkPrice:   price.AddRaw(3),
		Basis:       sdk.NewInt(-4),
		TimestampMs: 1_800_000_000_005,
		Height:      8,
		Sources:     2,
	})
	ctx.KVStore(k.StoreKey()).Set(types.LastSubmitKey(v1), sdk.Uint64ToBigEndian(8))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, k.GetParams(ctx), exported.Params)
	require.Len(t, exported.FeedKeys, 2)
	require.Len(t, exported.Observations, 3)
	require.Len(t, exported.Aggregates, 1)
	for i := 1; i < len(exported.Observations); i++ {
		prev, next := exported.Observations[i-1], exported.Observations[i]
		require.Less(t, string(types.ObservationKey(prev.MarketID, prev.Operator)),
			string(types.ObservationKey(next.MarketID, next.Operator)))
	}
	require.Equal(t, uint64(0), exported.Aggregates[0].MarketID)
	require.Equal(t, "-4", exported.Aggregates[0].Aggregate.Basis.String())
	first, err := json.Marshal(exported)
	require.NoError(t, err)

	fresh, freshCtx := pricefeedtestutil.NewKeeper(testkeeper.EVMTestApp, ctx)
	require.Empty(t, storeKeys(freshCtx, fresh))
	require.NoError(t, fresh.InitGenesis(freshCtx, exported))
	reexported, err := fresh.ExportGenesis(freshCtx)
	require.NoError(t, err)
	second, err := json.Marshal(reexported)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))

	require.Nil(t, freshCtx.KVStore(fresh.StoreKey()).Get(types.LastSubmitKey(v1)))
	for operator, account := range map[string]sdk.AccAddress{v1.String(): f1, v2.String(): f2} {
		holder, ok := fresh.GetValidatorByFeedAccount(freshCtx, account)
		require.True(t, ok)
		require.Equal(t, operator, holder.String())
	}

	shared := exported
	shared.FeedKeys = append([]types.GenesisFeedKey(nil), exported.FeedKeys...)
	shared.FeedKeys[1].FeedKey = shared.FeedKeys[0].FeedKey
	refused, refusedCtx := pricefeedtestutil.NewKeeper(testkeeper.EVMTestApp, ctx)
	err = refused.InitGenesis(refusedCtx, shared)
	requireRefusal(t, err, types.ErrInvalidGenesis, "LXP_PRICEFEED_INVALID_GENESIS")
	require.ErrorContains(t, err, "feed account "+shared.FeedKeys[0].FeedKey.FeedAccount+" is mapped to validators "+
		shared.FeedKeys[0].Operator.String()+" and "+shared.FeedKeys[1].Operator.String())
	require.Empty(t, storeKeys(refusedCtx, refused))
}
