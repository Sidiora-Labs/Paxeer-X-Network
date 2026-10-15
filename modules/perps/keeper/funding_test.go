package keeper

import (
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const fundingHour = uint64(500_000)

func fundingKeeper(t *testing.T, at uint64) (*Keeper, sdk.Context) {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	tkey := sdk.NewTransientStoreKey("transient_" + types.StoreKey)
	ctx := testutil.DefaultContext(key, tkey).WithBlockTime(time.Unix(int64(at), 0).UTC())
	return NewKeeper(key, nil, nil), ctx
}

func fundingMarket() types.Market {
	return types.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: 1000, TierClass: types.TierClassA,
		ImpactNotionalUsd: sdk.NewIntWithDecimal(20_000, 18), OiCapUsd: sdk.NewIntWithDecimal(50_000_000, 18),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}
}

func usd18(units int64) uint256.Int {
	return *uint256.MustFromBig(new(big.Int).Mul(big.NewInt(units), fixedOne))
}

func word(t *testing.T, value *big.Int) uint256.Int {
	t.Helper()
	out, err := signedWord(value, 0)
	require.NoError(t, err)
	return out
}

func restingOrder(id uint64, isLong bool, price, size uint256.Int) types.Order {
	return types.Order{
		ID: *uint256.NewInt(id), Account: common.BigToAddress(big.NewInt(int64(100 + id))), MarketID: 0,
		IsLong: isLong, Type: types.OrderTypeLimit, Status: types.OrderStatusResting,
		LimitPrice: price, SizeBase: size, Leverage: usd18(1), Sequence: id,
	}
}

func TestFundingPremiumSample(t *testing.T) {
	at := fundingHour*secondsPerHour + 10
	k, ctx := fundingKeeper(t, at)
	market := fundingMarket()
	k.setFundingMarketState(ctx, types.MarketState{MarketID: 0, IndexPrice: usd18(60_000), Fresh: true, LastIndexHour: fundingHour})

	sampled, err := k.SamplePremium(ctx, market)
	require.NoError(t, err)
	require.True(t, sampled)
	state, err := k.fundingMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.PremiumCount)
	require.Equal(t, 0, signedOf(state.PremiumSum).Sign(), "empty book prices both sides at the oracle")

	require.NoError(t, k.BookInsert(ctx, restingOrder(1, true, usd18(60_030), usd18(1))))
	require.NoError(t, k.BookInsert(ctx, restingOrder(2, false, usd18(60_060), usd18(1))))

	ctx = ctx.WithBlockTime(time.Unix(int64(at+4), 0).UTC())
	sampled, err = k.SamplePremium(ctx, market)
	require.NoError(t, err)
	require.False(t, sampled, "4 seconds after the last sample")

	ctx = ctx.WithBlockTime(time.Unix(int64(at+5), 0).UTC())
	sampled, err = k.SamplePremium(ctx, market)
	require.NoError(t, err)
	require.True(t, sampled)
	state, err = k.fundingMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(2), state.PremiumCount)
	require.Equal(t, big.NewInt(500_000_000_000_000), signedOf(state.PremiumSum), "premium +0.0005")
	require.Equal(t, at+5, state.LastSampleTime)

	require.Equal(t, big.NewInt(500_000_000_000_000),
		premiumOf(usd18(60_000).ToBig(), usd18(60_030).ToBig(), usd18(60_060).ToBig()))
	require.Equal(t, big.NewInt(-1_000_000_000_000_000),
		premiumOf(usd18(60_000).ToBig(), usd18(59_900).ToBig(), usd18(59_940).ToBig()))
}

func TestFundingRateFormula(t *testing.T) {
	params := types.DefaultParams()
	market := fundingMarket()
	for _, tc := range []struct {
		name    string
		premium int64
		hourly  int64
	}{
		{"interest alone", 0, 12_500_000_000_000},
		{"premium inside the clamp", 300_000_000_000_000, 12_500_000_000_000},
		{"premium above the clamp", 1_000_000_000_000_000, 62_500_000_000_000},
		{"negative premium below the clamp", -1_000_000_000_000_000, -62_500_000_000_000},
		{"capped at 4 percent", 1_000_000_000_000_000_000, 40_000_000_000_000_000},
		{"capped at -4 percent", -1_000_000_000_000_000_000, -40_000_000_000_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, big.NewInt(tc.hourly), hourlyRate(big.NewInt(tc.premium), params, market))
		})
	}

	k, ctx := fundingKeeper(t, (fundingHour+1)*secondsPerHour)
	k.setFundingMarketState(ctx, types.MarketState{
		MarketID: 0, IndexPrice: usd18(60_000), LastIndexHour: fundingHour, PremiumCount: 720,
	})
	events, err := k.SettleFunding(ctx, market)
	require.NoError(t, err)
	state, err := k.fundingMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(12_500_000_000_000), signedOf(state.HourlyRate), "0.0001 / 8")
	require.Equal(t, uint64(0), state.PremiumCount)
	require.Equal(t, fundingHour+1, state.LastIndexHour)
	require.Equal(t, types.EventFundingRateUpdated, events[0].Name)
	require.Equal(t, []any{big.NewInt(0), big.NewInt(3_472_222_222), big.NewInt(300_000_000_000_000)}, events[0].Args)
}

func TestFundingSettlementIndex(t *testing.T) {
	k, ctx := fundingKeeper(t, (fundingHour+1)*secondsPerHour+2)
	market := fundingMarket()
	long := types.Position{ID: *uint256.NewInt(7), Account: common.HexToAddress("0xa1"), IsLong: true, SizeBase: usd18(1)}
	short := types.Position{ID: *uint256.NewInt(8), Account: common.HexToAddress("0xb2"), IsLong: false, SizeBase: usd18(1)}
	k.store(ctx).Set(types.PositionKey(long.ID), long.Marshal())
	k.store(ctx).Set(types.PositionKey(short.ID), short.Marshal())
	k.setFundingMarketState(ctx, types.MarketState{
		MarketID: 0, IndexPrice: usd18(60_000), LastIndexHour: fundingHour, PremiumCount: 720,
		LongOpenInterest: usd18(1), ShortOpenInterest: usd18(1),
	})

	events, err := k.SettleFunding(ctx, market)
	require.NoError(t, err)
	require.Len(t, events, 2, "no position event at the hour")
	require.Equal(t, types.EventFundingSettled, events[1].Name)
	require.Equal(t, []any{big.NewInt(0), big.NewInt(12_500_000_000_000),
		big.NewInt(-750_000_000_000_000_000), big.NewInt(750_000_000_000_000_000)}, events[1].Args)
	require.Equal(t, long.Marshal(), k.store(ctx).Get(types.PositionKey(long.ID)), "long not visited")
	require.Equal(t, short.Marshal(), k.store(ctx).Get(types.PositionKey(short.ID)), "short not visited")

	state, err := k.fundingMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(750_000_000_000_000_000), signedOf(state.FundingIndex))

	again, err := k.SettleFunding(ctx, market)
	require.NoError(t, err)
	require.Nil(t, again, "the index advances once per hour")

	for _, tc := range []struct {
		position types.Position
		usd      int64
		usdl     int64
	}{{long, -750_000_000_000_000_000, 99_250_000}, {short, 750_000_000_000_000_000, 100_750_000}} {
		account := types.Account{Address: tc.position.Account, Collateral: *uint256.NewInt(100_000_000), Created: true}
		position := tc.position
		event, err := k.SettlePositionFunding(ctx, &account, &position, state.FundingIndex)
		require.NoError(t, err)
		require.NotNil(t, event)
		require.Equal(t, types.EventPositionFundingApplied, event.Name)
		require.Equal(t, []any{position.ID.ToBig(), position.Account, big.NewInt(0), big.NewInt(tc.usd),
			new(big.Int).Mul(big.NewInt(tc.usdl), usdlUnitInUsd), big.NewInt(tc.usdl)}, event.Args)
		require.Equal(t, uint64(tc.usdl), account.Collateral.Uint64())
		require.Equal(t, big.NewInt(750_000_000_000_000_000), signedOf(position.FundingBasis))

		owed, err := FundingOwed(position, state.FundingIndex)
		require.NoError(t, err)
		require.True(t, owed.Usd.IsZero(), "settled funding is not owed twice")
	}
}

func TestFundingRoundingTowardVenue(t *testing.T) {
	k, ctx := fundingKeeper(t, fundingHour*secondsPerHour)
	index := word(t, big.NewInt(333_333_333_333_333_333))
	three := new(big.Int).Add(new(big.Int).Mul(big.NewInt(3), fixedOne), big.NewInt(1))
	positions := []types.Position{
		{ID: *uint256.NewInt(1), Account: common.HexToAddress("0x01"), IsLong: true, SizeBase: usd18(1)},
		{ID: *uint256.NewInt(2), Account: common.HexToAddress("0x02"), IsLong: true, SizeBase: usd18(2)},
		{ID: *uint256.NewInt(3), Account: common.HexToAddress("0x03"), IsLong: true, SizeBase: *uint256.NewInt(1)},
		{ID: *uint256.NewInt(4), Account: common.HexToAddress("0x04"), IsLong: false, SizeBase: *uint256.MustFromBig(three)},
	}
	wantUsd := []int64{-333_333_333_333_333_333, -666_666_666_666_666_666, -1, 999_999_999_999_999_999}
	wantCollateral := []uint64{9_666_666, 9_333_333, 9_999_999, 10_999_999}
	wantResidue := []int64{666_666_666_667, 333_333_333_334, 999_999_999_999, 999_999_999_999}

	paid, received, residue := new(big.Int), new(big.Int), new(big.Int)
	for i := range positions {
		account := types.Account{Address: positions[i].Account, Collateral: *uint256.NewInt(10_000_000), Created: true}
		owed, err := FundingOwed(positions[i], index)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(wantUsd[i]), owed.Usd.BigInt())
		require.Equal(t, big.NewInt(wantResidue[i]), owed.ResidueUsd.BigInt())
		event, err := k.SettlePositionFunding(ctx, &account, &positions[i], index)
		require.NoError(t, err)
		require.NotNil(t, event)
		require.Equal(t, wantCollateral[i], account.Collateral.Uint64())
		if owed.Usdl.IsNegative() {
			paid.Sub(paid, owed.Usdl.BigInt())
		} else {
			received.Add(received, owed.Usdl.BigInt())
		}
		residue.Add(residue, owed.ResidueUsd.BigInt())
	}
	require.Equal(t, big.NewInt(1_000_002), paid)
	require.Equal(t, big.NewInt(999_999), received)
	require.True(t, paid.Cmp(received) >= 0, "the venue never pays out more than it collects")

	books, err := k.fundingVenueBooks(ctx)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(2_999_999_999_999), books.ResidueUsd.ToBig())
	require.Equal(t, residue, books.ResidueUsd.ToBig())
	require.True(t, books.DeficitSumUsd.IsZero())
	// The USDL difference is the residue plus the 1e-18 USD the rounded
	// bases kept: 1,000,002 - 999,999 units = 2,999,999,999,999 + 1.
	gap := new(big.Int).Mul(new(big.Int).Sub(paid, received), usdlUnitInUsd)
	require.Equal(t, new(big.Int).Add(residue, big.NewInt(1)), gap)
	require.Nil(t, k.store(ctx).Get(types.AccountKey(k.InsuranceAccountAddress())), "nothing reaches perps_insurance")
}

func TestFundingBeforeLiquidation(t *testing.T) {
	at := (fundingHour + 1) * secondsPerHour
	k, ctx := fundingKeeper(t, at)
	market := fundingMarket()
	require.NoError(t, k.SetMarket(ctx, k.Authority(), market))

	trader := common.HexToAddress("0xa1")
	position := types.Position{
		ID: *uint256.NewInt(7), Account: trader, MarketID: 0, IsLong: true, SizeBase: usd18(1),
		ReferencePrice: usd18(60_000), CostBasisUsd: usd18(60_000), EntryPrice: usd18(60_000),
		Leverage: usd18(1000), OpenedAt: at - secondsPerHour,
	}
	account := types.Account{Address: trader, Collateral: *uint256.NewInt(87_000_000), PositionCount: 1, Created: true}
	id := position.ID.Bytes32()
	k.store(ctx).Set(types.AccountKey(trader), account.Marshal())
	k.store(ctx).Set(types.PositionKey(position.ID), position.Marshal())
	k.store(ctx).Set(types.AccountMarketPositionKey(trader, 0), id[:])
	k.store(ctx).Set(types.MarginRatioIndexKey(145, trader), []byte{})
	k.setFundingMarketState(ctx, types.MarketState{
		MarketID: 0, IndexPrice: usd18(60_000), PricefeedMark: usd18(60_000), MarkPrice: usd18(60_000), Fresh: true,
		LastIndexHour: fundingHour, PremiumCount: 1, PremiumSum: word(t, fixedOne),
		LongOpenInterest: usd18(1), ShortOpenInterest: usd18(1),
	})

	before, err := k.IsLiquidatable(ctx, trader)
	require.NoError(t, err)
	require.False(t, before, "87 USD of collateral covers the 30 USD maintenance of 60,000 USD at 1000x")

	events, err := k.SettleFunding(ctx, market)
	require.NoError(t, err)
	require.Len(t, events, 2)
	state, err := k.fundingMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(40_000_000_000_000_000), signedOf(state.HourlyRate), "capped at 4 percent")
	require.Equal(t, usd18(2_400).ToBig(), signedOf(state.FundingIndex), "one capped step: 2,400 USD per BTC")

	after, err := k.IsLiquidatable(ctx, trader)
	require.NoError(t, err)
	require.True(t, after, "equity counts the funding owed by the index without a settlement")

	liquidations, err := k.LiquidationScan(ctx)
	require.NoError(t, err)
	funding, liquidation := -1, -1
	for i, event := range liquidations {
		switch event.Name {
		case types.EventPositionFundingApplied:
			if funding < 0 {
				funding = i
			}
			require.Equal(t, new(big.Int).Neg(usd18(2_400).ToBig()), event.Args[3])
		case types.EventLiquidation:
			if liquidation < 0 {
				liquidation = i
			}
		}
	}
	require.GreaterOrEqual(t, funding, 0, "the liquidation settles the funding")
	require.Greater(t, liquidation, funding, "PositionFundingApplied precedes Liquidation")
}
