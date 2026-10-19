package keeper_test

import (
	"errors"
	"math/big"
	"testing"
	"time"

	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

const twapBase = uint64(1_700_000_000)

// twapFeedKey resolves to the fixture's pricefeed store, which the store
// overlay serves by key name.
var twapFeedKey = sdk.NewKVStoreKey(pricefeedtypes.StoreKey)

func atSecond(ctx sdk.Context, second uint64) sdk.Context {
	return ctx.WithBlockTime(time.Unix(int64(second), 0))
}

func e18(milli int64) sdk.Int { return sdk.NewIntWithDecimal(milli, 15) }

// setTwapIndex publishes index as the fresh PAX index price at ctx's block.
func setTwapIndex(t *testing.T, ctx sdk.Context, index sdk.Int) {
	t.Helper()
	putSwapJSON(t, ctx.KVStore(twapFeedKey), pricefeedtypes.AggregateKey(types.PAXMarketID), pricefeedtypes.Aggregate{
		IndexPrice: index, MarkPrice: index, Basis: sdk.ZeroInt(), TimestampMs: uint64(ctx.BlockTime().UnixMilli()),
		Height: ctx.BlockHeight(), Sources: 1,
	})
}

func recordTwapSample(t *testing.T, f swapFixture, second uint64, index sdk.Int) {
	t.Helper()
	ctx := atSecond(f.ctx, second)
	setTwapIndex(t, ctx, index)
	require.NoError(t, f.keeper.RecordSample(ctx, f.pax))
}

func updateTwapParams(t *testing.T, f swapFixture, update func(*types.Params)) {
	t.Helper()
	params, err := f.keeper.GetParams(f.ctx)
	require.NoError(t, err)
	update(&params)
	require.NoError(t, f.keeper.UpdateParams(f.ctx, f.keeper.Authority(), params))
}

func requireAbiSelector(t *testing.T, err error, want *types.AbiError, selector string) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var abiErr *types.AbiError
	require.True(t, errors.As(err, &abiErr))
	require.Equal(t, selector, hexutil.Encode(abiErr.Data()))
}

func TestSampleRing(t *testing.T) {
	f := newSwapFixture(t, 2000_000000, 10000)
	for block := uint64(1); block <= types.TwapHistoryDepth+1; block++ {
		recordTwapSample(t, f, twapBase+block, sdk.NewIntFromUint64(block))
	}
	gs := f.keeper.ExportGenesis(f.ctx)
	require.Equal(t, []types.TwapRing{{Token: f.pax, Head: 1, Count: types.TwapHistoryDepth}}, gs.TwapRings)
	require.Len(t, gs.TwapSamples, types.TwapHistoryDepth)

	for i := uint32(0); i < types.TwapHistoryDepth; i++ {
		slot := (uint32(1) + i) % types.TwapHistoryDepth
		sample := gs.TwapSamples[slot]
		want := uint64(i) + 2
		require.Equal(t, f.pax, sample.Token)
		require.Equal(t, slot, sample.Slot)
		require.Equal(t, twapBase+want, sample.Time)
		require.Equal(t, sdk.NewIntFromUint64(want), sample.Index)
	}

	// The first sample was overwritten: the ring covers the latest 1000 only.
	now := atSecond(f.ctx, twapBase+types.TwapHistoryDepth+1)
	_, err := f.keeper.TWAP(now, f.pax, types.TwapHistoryDepth)
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	twap, err := f.keeper.TWAP(now, f.pax, types.TwapHistoryDepth-1)
	require.NoError(t, err)
	// Samples 2..1000 each hold one second over the window of 999 seconds.
	require.Equal(t, sdk.NewIntFromUint64(501), twap)

	_, err = f.keeper.TWAP(now, f.stable, 1)
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
}

func TestTWAPWindow(t *testing.T) {
	f := newSwapFixture(t, 2000_000000, 10000)
	now := twapBase + 300
	nowCtx := atSecond(f.ctx, now)

	_, err := f.keeper.TWAP(nowCtx, f.pax, 300)
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")

	recordTwapSample(t, f, twapBase, e18(500))
	recordTwapSample(t, f, twapBase+200, e18(600))

	cases := []struct {
		period uint64
		want   string
	}{
		{300, "533333333333333333"},
		{250, "540000000000000000"},
		{100, "600000000000000000"},
		{1, "600000000000000000"},
	}
	for _, tc := range cases {
		twap, err := f.keeper.TWAP(nowCtx, f.pax, tc.period)
		require.NoError(t, err)
		require.Equal(t, tc.want, twap.String(), "period %d", tc.period)
	}

	for _, period := range []uint64{0, 301, now + 1} {
		_, err := f.keeper.TWAP(nowCtx, f.pax, period)
		requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	}
}

func TestTwapSanity(t *testing.T) {
	f := newSwapFixture(t, 2000_000000, 10000)
	updateTwapParams(t, f, func(params *types.Params) {
		params.TwapSanityEnabled = true
		params.TwapSanityPeriod = 300
		params.MaxTwapDeviationBps = 200
		params.FillQualityTwapPeriod = 300
	})
	recordTwapSample(t, f, twapBase, e18(1000))
	nowCtx := atSecond(f.ctx, twapBase+300)

	cases := []struct {
		index   sdk.Int
		refused bool
	}{
		{e18(1030), true},
		{e18(970), true},
		{e18(1020), false},
		{e18(980), false},
		{e18(1000), false},
	}
	for _, tc := range cases {
		err := f.keeper.CheckTwapSanity(nowCtx, f.pax, tc.index)
		ok, fillErr := f.keeper.FillQualityOK(nowCtx, f.pax, tc.index)
		require.NoError(t, fillErr)
		require.Equal(t, !tc.refused, ok, "index %s", tc.index)
		if tc.refused {
			requireAbiSelector(t, err, types.ErrStalePrice, "0x19abf40e")
		} else {
			require.NoError(t, err, "index %s", tc.index)
		}
	}

	// A swap at an index 3% above its TWAP is refused without state changes.
	setTwapIndex(t, nowCtx, e18(1030))
	store := nowCtx.KVStore(f.key)
	before := swapStoreSnapshot(t, store)
	bank := testkeeper.EVMTestApp.BankKeeper
	userBefore := bank.GetAllBalances(nowCtx, f.account)
	custodyBefore := bank.GetAllBalances(nowCtx, spotkeeper.CustodyAddress())
	for name, swap := range map[string]func() (spotkeeper.SwapResult, error){
		"market_buy": func() (spotkeeper.SwapResult, error) {
			return f.keeper.MarketBuy(nowCtx, f.user, f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
		},
		"exact_out": func() (spotkeeper.SwapResult, error) {
			return f.keeper.SwapExactOut(nowCtx, f.user, f.stable, f.pax, big.NewInt(100_000000), big.NewInt(1000_000000), new(big.Int))
		},
	} {
		result, err := swap()
		requireAbiSelector(t, err, types.ErrStalePrice, "0x19abf40e")
		require.Equal(t, spotkeeper.SwapResult{}, result, name)
		require.Equal(t, before, swapStoreSnapshot(t, store), name)
		require.Equal(t, userBefore, bank.GetAllBalances(nowCtx, f.account), name)
		require.Equal(t, custodyBefore, bank.GetAllBalances(nowCtx, spotkeeper.CustodyAddress()), name)
	}

	// Within maxTwapDeviationBps the same swap settles.
	setTwapIndex(t, nowCtx, e18(1020))
	result, err := f.keeper.MarketBuy(nowCtx, f.user, f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	require.NoError(t, err)
	require.Equal(t, "1000000000", result.AmountIn.String())
	require.Equal(t, "0", bank.GetBalance(nowCtx, f.account, "uquote").Amount.String())

	// A window the ring does not cover refuses the trade and holds the fill.
	early := atSecond(f.ctx, twapBase+299)
	requireAbiSelector(t, f.keeper.CheckTwapSanity(early, f.pax, e18(1000)), types.ErrTwapWindowInvalid, "0xb307c2d2")
	ok, err := f.keeper.FillQualityOK(early, f.pax, e18(1000))
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	require.False(t, ok)

	updateTwapParams(t, f, func(params *types.Params) {
		params.TwapSanityEnabled = false
		params.FillQualityTwapPeriod = 0
	})
	require.NoError(t, f.keeper.CheckTwapSanity(nowCtx, f.pax, e18(1030)))
	ok, err = f.keeper.FillQualityOK(nowCtx, f.pax, e18(1030))
	require.NoError(t, err)
	require.True(t, ok)
}
