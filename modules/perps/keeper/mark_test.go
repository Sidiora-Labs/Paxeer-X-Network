package keeper_test

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const markNowMs int64 = 1_700_000_010_000

type markEnv struct {
	k     *keeper.Keeper
	ctx   sdk.Context
	pfKey sdk.StoreKey
}

// newMarkEnv builds the perps keeper over the real pricefeed keeper, each on
// its own in-memory store, with BTC (0) and SOL (2) listed and the block at
// height 10 and markNowMs.
func newMarkEnv(t *testing.T) markEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(markNowMs).UTC())
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := keeper.NewKeeper(perpsKey, &testApp.EvmKeeper, pricefeed)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(2, "SOL", 2, 3, types.TierClassB, 10_000, 100_000_000)))
	return markEnv{k: k, ctx: ctx, pfKey: pfKey}
}

func listedMarket(id uint64, symbol string, sz, px uint8, class types.TierClass, impactUsd, capUsd int64) types.Market {
	return types.Market{
		ID:                   id,
		Symbol:               symbol,
		Name:                 symbol,
		OracleSymbol:         symbol,
		SzDecimals:           sz,
		PxDecimals:           px,
		MaxLeverage:          1000,
		TierClass:            class,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(impactUsd, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(capUsd, 18),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}
}

// setAggregate stores the pricefeed aggregate GetPrice reads for a market.
func (env markEnv) setAggregate(t *testing.T, marketID uint64, index, mark int64, timestampMs int64) {
	t.Helper()
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(index, 18),
		MarkPrice:   sdk.NewIntWithDecimal(mark, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(timestampMs),
		Height:      env.ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(t, err)
	env.ctx.KVStore(env.pfKey).Set(pricefeedtypes.AggregateKey(marketID), bz)
}

func (env markEnv) atBlock(height int64, nowMs int64) markEnv {
	env.ctx = env.ctx.WithBlockHeight(height).WithBlockTime(time.UnixMilli(nowMs).UTC())
	return env
}

func e18Dec(n int64) string {
	value := e18(n)
	return value.Dec()
}

func e18(n int64) uint256.Int {
	var value uint256.Int
	value.Mul(uint256.NewInt(uint64(n)), uint256.NewInt(1_000_000_000_000_000_000))
	return value
}

func negative(value uint256.Int) uint256.Int {
	var out uint256.Int
	out.Neg(&value)
	return out
}

func wordOf(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value, err := uint256.FromDecimal(decimal)
	require.NoError(t, err)
	return *value
}

func eventStrings(event types.EventRecord) []string {
	out := []string{event.Name}
	for _, arg := range event.Args {
		out = append(out, arg.(*big.Int).String())
	}
	return out
}

func TestMarkReadsPricefeed(t *testing.T) {
	env := newMarkEnv(t)
	env.setAggregate(t, 0, 60_000, 60_010, markNowMs-1_000)

	events, err := env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)

	state, err := env.k.GetMarketState(env.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, e18(60_000), state.IndexPrice)
	require.Equal(t, e18(60_010), state.PricefeedMark)
	require.Equal(t, uint64(markNowMs-1_000), state.PriceTimestampMs)
	require.True(t, state.Fresh)
	// An empty book and no trade set bid, ask, last and mid to the index:
	// median(60,000 + 0, 60,000, 60,010) = 60,000.
	require.Equal(t, e18(60_000), state.MarkPrice)
	require.True(t, state.PremiumEma.IsZero())

	// SOL has no aggregate: zero prices, not fresh, no mark.
	sol, err := env.k.GetMarketState(env.ctx, 2)
	require.NoError(t, err)
	require.Equal(t, types.MarketState{MarketID: 2}, sol)

	require.Len(t, events, 1)
	require.Equal(t, []string{types.EventMarkPriceUpdated, "0", "60000000000000000000000", "60000000000000000000000",
		"60000000000000000000000"}, eventStrings(events[0]))
}

func TestMarkMedianOfThree(t *testing.T) {
	cases := []struct {
		name                                 string
		index, basis, bid, ask, last, pfMark uint256.Int
		want                                 uint256.Int
	}{
		{"acceptance", e18(60_000), e18(20), e18(59_990), e18(60_030), e18(60_040), e18(60_010), e18(60_020)},
		{"details example", e18(60_000), e18(12), e18(59_990), e18(60_010), e18(60_000), e18(60_030), e18(60_012)},
		{"negative basis", e18(60_000), negative(e18(30)), e18(59_990), e18(60_030), e18(60_040), e18(60_010), e18(60_010)},
		{"no bid and no trade take the index", e18(60_000), e18(20), uint256.Int{}, e18(60_030), uint256.Int{}, e18(60_010), e18(60_010)},
		{"empty book and no trade", e18(60_000), e18(20), uint256.Int{}, uint256.Int{}, uint256.Int{}, e18(60_030), e18(60_020)},
		{"basis below zero price", e18(1), negative(e18(5)), e18(3), e18(4), e18(2), e18(6), e18(3)},
		{"large basis takes the higher component", e18(60_000), wordOf(t, "57896044618658097711785492504343953926634992332820282019728792003956564819967"),
			e18(59_990), e18(60_030), e18(60_040), e18(60_010), e18(60_030)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keeper.ComputeMark(tc.index, tc.basis, tc.bid, tc.ask, tc.last, tc.pfMark)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMarkStaleRefusesTrading(t *testing.T) {
	env := newMarkEnv(t)
	env.setAggregate(t, 0, 60_000, 60_010, markNowMs-1_000)
	// 16 seconds old is beyond max_price_age_ms of 15,000.
	env.setAggregate(t, 2, 150, 150, markNowMs-16_000)

	require.NoError(t, env.k.RequireFresh(env.ctx, 0))
	err := env.k.RequireFresh(env.ctx, 2)
	require.ErrorIs(t, err, types.ErrPriceStale)
	require.Equal(t, uint32(5), types.ErrPriceStale.ABCICode())

	// The stale market keeps its mark and EMA basis while storing the price.
	env.k.SetMarketState(env.ctx, types.MarketState{MarketID: 2, MarkPrice: e18(149), PremiumEma: e18(1), Fresh: true})
	events, err := env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, types.EventMarkPriceUpdated, events[0].Name)
	require.Equal(t, "0", events[0].Args[0].(*big.Int).String())
	sol, err := env.k.GetMarketState(env.ctx, 2)
	require.NoError(t, err)
	require.False(t, sol.Fresh)
	require.Equal(t, e18(150), sol.IndexPrice)
	require.Equal(t, uint64(markNowMs-16_000), sol.PriceTimestampMs)
	require.Equal(t, e18(149), sol.MarkPrice)
	require.Equal(t, e18(1), sol.PremiumEma)
}

func TestMarkEventOnChangeOnly(t *testing.T) {
	env := newMarkEnv(t)
	aggregateMs := markNowMs - 1_000
	env.setAggregate(t, 0, 60_000, 60_030, aggregateMs)
	env.k.SetMarketState(env.ctx, types.MarketState{
		MarketID: 0, MarkPrice: e18(60_000), PremiumEma: e18(20), LastTradePrice: e18(60_040),
		PriceTimestampMs: uint64(aggregateMs), Fresh: true,
	})

	// Block 10: median(60,000 + 20, median(60,000, 60,000, 60,040), 60,030) = 60,020.
	events, err := env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, []string{types.EventMarkPriceUpdated, "0", "60020000000000000000000", "60000000000000000000000",
		"60000000000000000000000"}, eventStrings(events[0]))
	_, _, err = perpsabi.PackEvent(events[0].Name, events[0].Args...)
	require.NoError(t, err)

	// Block 11 with the same inputs: the mark stays 60,020 and nothing is emitted.
	env = env.atBlock(11, markNowMs+400)
	events, err = env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)
	require.Empty(t, events)
	state, err := env.k.GetMarketState(env.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, e18(60_020), state.MarkPrice)
	require.Equal(t, e18(20), state.PremiumEma)

	// Block 12: the index moves to 60,100 one second later; the basis decays
	// toward mid - index = 0 as 20 + (0 - 20) x 1000 / 151000, truncated.
	env = env.atBlock(12, markNowMs+1_000)
	env.setAggregate(t, 0, 60_100, 60_130, aggregateMs+1_000)
	events, err = env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, []string{types.EventMarkPriceUpdated, "0", "60119867549668874172186", "60100000000000000000000",
		"60100000000000000000000"}, eventStrings(events[0]))
	state, err = env.k.GetMarketState(env.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, wordOf(t, "19867549668874172186"), state.PremiumEma)
	require.Equal(t, wordOf(t, "60119867549668874172186"), state.MarkPrice)
}
