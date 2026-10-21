package keeper_test

import (
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

const priceTimestampMs uint64 = 1_700_000_000_000

func atMs(ctx sdk.Context, ms int64) sdk.Context {
	return ctx.WithBlockTime(time.UnixMilli(ms).UTC())
}

func e18(units int64) sdk.Int {
	return sdk.NewInt(units).Mul(sdk.NewIntWithDecimal(1, 18))
}

func storeAggregate(k keeper.Keeper, ctx sdk.Context, marketID uint64, aggregate types.Aggregate) {
	k.Set(ctx, types.AggregateKey(marketID), aggregate)
}

func btcAggregate() types.Aggregate {
	return types.Aggregate{
		IndexPrice:  e18(65000),
		MarkPrice:   e18(65010),
		Basis:       e18(10),
		TimestampMs: priceTimestampMs,
		Height:      300,
		Sources:     4,
	}
}

func TestGetPriceFresh(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	storeAggregate(k, ctx, 0, btcAggregate())

	price, err := k.GetPrice(atMs(ctx, 1_700_000_005_000), 0)
	require.NoError(t, err)
	require.Equal(t, keeper.Price{
		Index:       e18(65000),
		Mark:        e18(65010),
		TimestampMs: priceTimestampMs,
		Height:      300,
		Sources:     4,
		Fresh:       true,
	}, price)
}

func TestGetPriceAges(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	params := k.GetParams(ctx)
	require.Equal(t, uint64(15000), params.MaxPriceAgeMs)
	require.Equal(t, uint64(2000), params.MaxClockSkewMs)
	storeAggregate(k, ctx, 0, btcAggregate())

	cases := []struct {
		blockMs int64
		fresh   bool
	}{
		{1_700_000_015_000, true},
		{1_700_000_015_001, false},
		{1_700_000_000_000, true},
		{1_699_999_998_000, true},
		{1_699_999_997_999, false},
	}
	for _, c := range cases {
		price, err := k.GetPrice(atMs(ctx, c.blockMs), 0)
		require.NoError(t, err)
		require.Equal(t, c.fresh, price.Fresh, "block time %d", c.blockMs)
		require.Equal(t, e18(65000), price.Index)
	}

	params.MaxPriceAgeMs = 20000
	require.NoError(t, k.SetParams(ctx, params))
	price, err := k.GetPrice(atMs(ctx, 1_700_000_015_001), 0)
	require.NoError(t, err)
	require.True(t, price.Fresh)
}

func TestGetPriceDisabledAndEmpty(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	storeAggregate(k, ctx, 5, btcAggregate())
	params := k.GetParams(ctx)
	params.Markets[5].Enabled = false
	require.NoError(t, k.SetParams(ctx, params))
	ctx = atMs(ctx, 1_700_000_005_000)

	disabled, err := k.GetPrice(ctx, 5)
	require.NoError(t, err)
	require.False(t, disabled.Fresh)
	require.Equal(t, e18(65000), disabled.Index)
	require.Equal(t, e18(65010), disabled.Mark)
	require.Equal(t, int64(300), disabled.Height)

	empty, err := k.GetPrice(ctx, 6)
	require.NoError(t, err)
	require.Equal(t, keeper.Price{Index: sdk.ZeroInt(), Mark: sdk.ZeroInt()}, empty)
	require.True(t, empty.Index.IsZero())
	require.True(t, empty.Mark.IsZero())
	require.False(t, empty.Fresh)
}

func TestGetPriceUnknownMarket(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	require.Len(t, k.GetParams(ctx).Markets, 18)
	for _, id := range []uint64{18, 1 << 63, ^uint64(0)} {
		_, err := k.GetPrice(ctx, id)
		requireCode(t, err, types.ErrUnknownMarket, "LXP_PRICEFEED_UNKNOWN_MARKET")
	}
}

func TestGetPriceBySymbol(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	eth := btcAggregate()
	eth.IndexPrice, eth.MarkPrice, eth.Basis = e18(3500), e18(3501), e18(1)
	storeAggregate(k, ctx, 1, eth)
	ctx = atMs(ctx, 1_700_000_005_000)

	byID, err := k.GetPrice(ctx, 1)
	require.NoError(t, err)
	require.True(t, byID.Fresh)
	bySymbol, err := k.GetPriceBySymbol(ctx, "ETH")
	require.NoError(t, err)
	require.Equal(t, byID, bySymbol)

	for _, symbol := range []string{"eth", "PERP_ETH_USDC", ""} {
		_, err = k.GetPriceBySymbol(ctx, symbol)
		requireCode(t, err, types.ErrUnknownMarket, "LXP_PRICEFEED_UNKNOWN_MARKET")
	}

	for _, market := range types.Markets {
		byID, err := k.GetPrice(ctx, market.ID)
		require.NoError(t, err)
		bySymbol, err := k.GetPriceBySymbol(ctx, market.Symbol)
		require.NoError(t, err)
		require.Equal(t, byID, bySymbol, market.Symbol)
	}
}

func TestGetPriceFreshAtContextBlockTime(t *testing.T) {
	k, ctx, _ := newParamsKeeper(t)
	storeAggregate(k, ctx, 0, btcAggregate())
	pinned := atMs(ctx, int64(priceTimestampMs)+1_000)

	price, err := k.GetPrice(pinned, 0)
	require.NoError(t, err)
	require.True(t, price.Fresh)

	later := atMs(ctx, int64(priceTimestampMs)+20_000).WithBlockHeight(ctx.BlockHeight() + 1)
	price, err = k.GetPrice(later, 0)
	require.NoError(t, err)
	require.False(t, price.Fresh)

	price, err = k.GetPrice(pinned, 0)
	require.NoError(t, err)
	require.True(t, price.Fresh)
}
