package keeper

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	oiMarketBTC uint64 = 0
	oiMarketPAX uint64 = 11
)

var oiListedAt = time.Unix(1_800_000_000, 0).UTC()

type oiEnv struct {
	k   *Keeper
	ctx sdk.Context
}

func newOiEnv(t *testing.T) oiEnv {
	t.Helper()
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(storeKey, sdk.NewTransientStoreKey("perps_transient_test")).WithBlockTime(oiListedAt)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := &Keeper{storeKey: storeKey, authority: authority}
	require.NoError(t, k.UpdateParams(ctx, authority, types.DefaultParams()))
	for _, market := range []types.Market{
		{ID: oiMarketBTC, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
			MaxLeverage: 1000, TierClass: types.TierClassA, ImpactNotionalUsd: oiUsd(20_000),
			OiCapUsd: oiUsd(500_000_000), FundingCapPpmPerHour: 40_000, Enabled: true},
		{ID: oiMarketPAX, Symbol: "PAX", Name: "Paxeer", OracleSymbol: "PAX", SzDecimals: 0, PxDecimals: 6,
			MaxLeverage: 1000, TierClass: types.TierClassE, ImpactNotionalUsd: oiUsd(1_000),
			OiCapUsd: sdk.ZeroInt(), FundingCapPpmPerHour: 40_000, Enabled: true},
	} {
		require.NoError(t, k.SetMarket(ctx, authority, market))
	}
	return oiEnv{k: k, ctx: ctx}
}

// seed writes the market state with equal sides of sideBase at mark.
func (e oiEnv) seed(t *testing.T, marketID uint64, sideBase, mark *uint256.Int) {
	t.Helper()
	state := types.MarketState{MarketID: marketID, MarkPrice: *mark, LongOpenInterest: *sideBase, ShortOpenInterest: *sideBase}
	e.ctx.KVStore(e.k.storeKey).Set(types.MarketStateKey(marketID), state.Marshal())
}

func (e oiEnv) storeDump() [][2][]byte {
	iterator := e.ctx.KVStore(e.k.storeKey).Iterator(nil, nil)
	defer iterator.Close()
	var out [][2][]byte
	for ; iterator.Valid(); iterator.Next() {
		out = append(out, [2][]byte{bytes.Clone(iterator.Key()), bytes.Clone(iterator.Value())})
	}
	return out
}

func oiUsd(whole int64) sdk.Int { return sdk.NewIntWithDecimal(whole, types.USDDecimals) }

func oiE18(whole uint64) *uint256.Int {
	return new(uint256.Int).Mul(uint256.NewInt(whole), uint256.NewInt(1_000_000_000_000_000_000))
}

func oiBig(value *uint256.Int) *big.Int { return value.ToBig() }

func oiFill(marketID uint64, size, price, buyerOpens, sellerOpens *uint256.Int) OpenInterestFill {
	return OpenInterestFill{MarketID: marketID, SizeBase: *size, PriceUsd: *price,
		BuyerOpensBase: *buyerOpens, SellerOpensBase: *sellerOpens}
}

func requireSides(t *testing.T, e oiEnv, marketID uint64, base, usd *uint256.Int) {
	t.Helper()
	oi, err := e.k.OpenInterest(e.ctx, marketID)
	require.NoError(t, err)
	require.Equal(t, base.Dec(), oi.LongBase.Dec())
	require.Equal(t, base.Dec(), oi.ShortBase.Dec())
	require.Equal(t, usd.Dec(), oi.LongUsd.Dec())
	require.Equal(t, usd.Dec(), oi.ShortUsd.Dec())
}

func TestOpenInterestEqualSides(t *testing.T) {
	e := newOiEnv(t)
	mark := oiE18(60_000)
	zero := uint256.NewInt(0)
	e.seed(t, oiMarketBTC, oiE18(10), mark)
	requireSides(t, e, oiMarketBTC, oiE18(10), oiE18(600_000))

	record, err := e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), mark, oiE18(1), oiE18(1)))
	require.NoError(t, err)
	require.NotNil(t, record)
	requireSides(t, e, oiMarketBTC, oiE18(11), oiE18(660_000))

	record, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), mark, zero, zero))
	require.NoError(t, err)
	require.NotNil(t, record)
	requireSides(t, e, oiMarketBTC, oiE18(10), oiE18(600_000))

	// An opening buy against a closing sell moves size between longs.
	record, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), mark, oiE18(1), zero))
	require.NoError(t, err)
	require.Nil(t, record)
	requireSides(t, e, oiMarketBTC, oiE18(10), oiE18(600_000))

	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(11), mark, zero, zero))
	require.ErrorIs(t, err, types.ErrOverflow)
	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), mark, oiE18(2), zero))
	require.ErrorIs(t, err, types.ErrInvalidParams)
	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, zero, mark, zero, zero))
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, err = e.k.OpenInterest(e.ctx, 99)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
	requireSides(t, e, oiMarketBTC, oiE18(10), oiE18(600_000))
}

func TestOpenInterestCapInForce(t *testing.T) {
	e := newOiEnv(t)
	before := e.storeDump()
	day := time.Duration(types.SecondsPerDay) * time.Second
	for _, tc := range []struct {
		age     time.Duration
		paxCap  int64
		btcCaps int64
	}{
		{29 * day, 2_000_000, 500_000_000},
		{30 * day, 5_000_000, 500_000_000},
		{90 * day, 10_000_000, 500_000_000},
		{180 * day, 25_000_000, 500_000_000},
	} {
		ctx := e.ctx.WithBlockTime(oiListedAt.Add(tc.age))
		paxCap, err := e.k.OpenInterestCap(ctx, oiMarketPAX)
		require.NoError(t, err)
		require.Equal(t, oiUsd(tc.paxCap).String(), paxCap.String())
		btcCap, err := e.k.OpenInterestCap(ctx, oiMarketBTC)
		require.NoError(t, err)
		require.Equal(t, oiUsd(tc.btcCaps).String(), btcCap.String())
	}
	// Reading the cap writes nothing: no borrowing index or other state.
	require.Equal(t, before, e.storeDump())
	_, err := e.k.OpenInterestCap(e.ctx, 99)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}

func TestOpenInterestCap(t *testing.T) {
	e := newOiEnv(t)
	e.ctx = e.ctx.WithBlockTime(oiListedAt.Add(10 * 24 * time.Hour))
	mark := oiE18(1)
	zero := uint256.NewInt(0)
	e.seed(t, oiMarketPAX, oiE18(1_999_000), mark)
	before := e.storeDump()

	_, err := e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(2_000), mark, oiE18(2_000), oiE18(2_000)))
	require.ErrorIs(t, err, types.ErrOpenInterestCap)
	require.Equal(t, before, e.storeDump())

	record, err := e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(1_000), mark, oiE18(1_000), oiE18(1_000)))
	require.NoError(t, err)
	require.NotNil(t, record)
	requireSides(t, e, oiMarketPAX, oiE18(2_000_000), oiE18(2_000_000))

	// At the cap, a reducing sell against an opening buy leaves OI unchanged
	// and one against a closing buy lowers it; both pass.
	record, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(2_000), mark, oiE18(2_000), zero))
	require.NoError(t, err)
	require.Nil(t, record)
	record, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(2_000), mark, zero, zero))
	require.NoError(t, err)
	require.NotNil(t, record)
	requireSides(t, e, oiMarketPAX, oiE18(1_998_000), oiE18(1_998_000))

	// The cap is per side in USD at mark: at a mark of 2 USD the side is
	// 3,996,000 USD, above the 2,000,000 cap, and only reduces pass.
	e.seed(t, oiMarketPAX, oiE18(1_998_000), oiE18(2))
	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(1), mark, oiE18(1), oiE18(1)))
	require.ErrorIs(t, err, types.ErrOpenInterestCap)
	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(1), mark, zero, zero))
	require.NoError(t, err)

	e.seed(t, oiMarketPAX, oiE18(1), zero)
	_, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketPAX, oiE18(1), mark, oiE18(1), oiE18(1)))
	require.ErrorIs(t, err, types.ErrPriceStale)
}

func TestOpenInterestEvent(t *testing.T) {
	e := newOiEnv(t)
	mark := oiE18(60_000)
	zero := uint256.NewInt(0)
	e.seed(t, oiMarketBTC, oiE18(10), mark)

	record, err := e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), mark, oiE18(1), oiE18(1)))
	require.NoError(t, err)
	require.Equal(t, &types.EventRecord{
		Name: types.EventOpenInterestChanged,
		Args: []any{big.NewInt(0), oiBig(oiE18(660_000)), oiBig(oiE18(660_000)), oiBig(oiE18(60_000)), true},
	}, record)
	topics, data, err := perpsabi.PackEvent(record.Name, record.Args...)
	require.NoError(t, err)
	require.Len(t, topics, 2)
	require.Len(t, data, 4*32)

	// deltaUsd is the changed size at the fill price; the sides are at mark.
	record, err = e.k.CheckOpenInterest(e.ctx, oiFill(oiMarketBTC, oiE18(1), oiE18(59_000), zero, zero))
	require.NoError(t, err)
	require.Equal(t, &types.EventRecord{
		Name: types.EventOpenInterestChanged,
		Args: []any{big.NewInt(0), oiBig(oiE18(600_000)), oiBig(oiE18(600_000)), oiBig(oiE18(59_000)), false},
	}, record)
}
