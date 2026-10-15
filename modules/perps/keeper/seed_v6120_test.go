package keeper

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const (
	seedActivationTime   = int64(1_800_000_000)
	seedActivationHeight = int64(1000)
)

func newSeedKeeper(t *testing.T) (*Keeper, sdk.Context) {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_perps_seed")).
		WithBlockHeight(seedActivationHeight).
		WithBlockTime(time.Unix(seedActivationTime, 0))
	return NewKeeper(key, nil, nil), ctx
}

func seedUsd(amount int64) sdk.Int { return sdk.NewIntWithDecimal(amount, types.USDDecimals) }

func requireSameJSON(t *testing.T, want, got any) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	require.Equal(t, wantJSON, gotJSON)
}

// seedStoreDump is every key and value of the perps store in key order.
func seedStoreDump(t *testing.T, k *Keeper, ctx sdk.Context) [][2][]byte {
	t.Helper()
	iterator := k.store(ctx).Iterator(nil, nil)
	defer iterator.Close()
	var dump [][2][]byte
	for ; iterator.Valid(); iterator.Next() {
		dump = append(dump, [2][]byte{append([]byte(nil), iterator.Key()...), append([]byte(nil), iterator.Value()...)})
	}
	return dump
}

func TestSeedWritesEighteenMarkets(t *testing.T) {
	k, ctx := newSeedKeeper(t)
	require.False(t, k.store(ctx).Has(types.ParamsKey))
	require.NoError(t, SeedV6120(ctx, *k))

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	requireSameJSON(t, types.DefaultParams(), params)
	require.Equal(t, common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479"), params.CollateralToken)
	stored, err := json.Marshal(types.DefaultParams())
	require.NoError(t, err)
	require.Equal(t, stored, k.store(ctx).Get(types.ParamsKey))

	for _, address := range []common.Address{k.FeesAccountAddress(), k.InsuranceAccountAddress()} {
		account, err := k.GetAccount(ctx, address)
		require.NoError(t, err)
		require.Equal(t, types.Account{Address: address, Created: true}, account)
	}

	want := []struct {
		symbol, name string
		sz, px       uint8
		class        types.TierClass
		impactUsd    int64
		oiCapUsd     int64
	}{
		{"BTC", "Bitcoin", 5, 1, types.TierClassA, 20_000, 500_000_000},
		{"ETH", "Ethereum", 4, 2, types.TierClassA, 20_000, 500_000_000},
		{"SOL", "Solana", 2, 3, types.TierClassB, 6_000, 50_000_000},
		{"AVAX", "Avalanche", 2, 3, types.TierClassB, 6_000, 50_000_000},
		{"LINK", "Chainlink", 1, 3, types.TierClassF, 6_000, 20_000_000},
		{"TSLA", "Tesla", 3, 2, types.TierClassD, 6_000, 25_000_000},
		{"NVDA", "NVIDIA", 3, 2, types.TierClassD, 6_000, 25_000_000},
		{"NAS100", "Nasdaq 100", 4, 1, types.TierClassC, 6_000, 100_000_000},
		{"XAU", "Gold", 4, 1, types.TierClassC, 6_000, 100_000_000},
		{"SPX500", "S&P 500", 4, 1, types.TierClassC, 6_000, 100_000_000},
		{"GOOGL", "Alphabet", 3, 2, types.TierClassD, 6_000, 25_000_000},
		{"PAX", "Paxeer Network", 0, 5, types.TierClassE, 6_000, 0},
		{"SID", "Sidiora Markets", 0, 5, types.TierClassE, 6_000, 0},
		{"HYPE", "Hyperliquid", 2, 3, types.TierClassB, 6_000, 50_000_000},
		{"XRP", "XRP", 0, 4, types.TierClassB, 6_000, 50_000_000},
		{"ASTER", "Aster", 0, 5, types.TierClassE, 6_000, 0},
		{"TRUMP", "Trump", 1, 3, types.TierClassE, 6_000, 0},
		{"BNB", "Binance Smart Chain", 3, 2, types.TierClassB, 6_000, 50_000_000},
	}

	var markets []types.Market
	require.NoError(t, k.IterateMarkets(ctx, func(market types.Market) bool {
		markets = append(markets, market)
		return false
	}))
	require.Len(t, markets, 18)
	for i, row := range want {
		expected := types.Market{
			ID:                   uint64(i),
			Symbol:               row.symbol,
			Name:                 row.name,
			OracleSymbol:         row.symbol,
			SzDecimals:           row.sz,
			PxDecimals:           row.px,
			MaxLeverage:          1000,
			TierClass:            row.class,
			ImpactNotionalUsd:    seedUsd(row.impactUsd),
			OiCapUsd:             seedUsd(row.oiCapUsd),
			FundingCapPpmPerHour: 40_000,
			ListedAt:             uint64(seedActivationTime),
			Enabled:              true,
		}
		requireSameJSON(t, expected, markets[i])
		market, err := k.GetMarket(ctx, uint64(i))
		require.NoError(t, err)
		requireSameJSON(t, expected, market)
		require.True(t, market.ImpactNotionalUsd.Equal(seedUsd(row.impactUsd)), "market %d", i)
		require.True(t, market.OiCapUsd.Equal(seedUsd(row.oiCapUsd)), "market %d", i)

		tiers, err := types.TiersFor(market)
		require.NoError(t, err)
		require.Equal(t, uint64(1000), tiers[0].MaxLeverage)
		require.Equal(t, uint64(5), tiers[0].MaintenanceBps)

		capUsd, err := types.OiCapInForce(market, params, ctx.BlockTime())
		require.NoError(t, err)
		wantCap := seedUsd(row.oiCapUsd)
		if row.class == types.TierClassE {
			wantCap = seedUsd(2_000_000)
		}
		require.True(t, wantCap.Equal(capUsd), "market %d cap %s", i, capUsd)
	}
	require.Equal(t, "BTC", markets[0].Symbol)
	require.Equal(t, "LINK", markets[4].Symbol)
	require.Equal(t, "BNB", markets[17].Symbol)

	_, err = k.GetMarket(ctx, 18)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}

func TestSeedIsIdempotent(t *testing.T) {
	k, ctx := newSeedKeeper(t)
	require.NoError(t, SeedV6120(ctx, *k))
	firstDump := seedStoreDump(t, k, ctx)
	firstExport, err := json.Marshal(k.ExportGenesis(ctx))
	require.NoError(t, err)

	require.NoError(t, SeedV6120(ctx, *k))
	require.Equal(t, firstDump, seedStoreDump(t, k, ctx))
	secondExport, err := json.Marshal(k.ExportGenesis(ctx))
	require.NoError(t, err)
	require.Equal(t, firstExport, secondExport)

	later := ctx.WithBlockHeight(seedActivationHeight + 1).WithBlockTime(time.Unix(seedActivationTime+types.SecondsPerDay, 0))
	require.NoError(t, SeedV6120(later, *k))
	require.Equal(t, firstDump, seedStoreDump(t, k, later))
}

func TestSeedRefusesInvalidMarket(t *testing.T) {
	k, ctx := newSeedKeeper(t)
	markets := seedV6120MarketList()
	markets[4].MaxLeverage = 1001

	err := seedV6120(ctx, k, markets)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_PARAMS")
	require.Empty(t, seedStoreDump(t, k, ctx))

	markets = seedV6120MarketList()
	markets[11].OiCapUsd = seedUsd(2_000_000)
	err = seedV6120(ctx, k, markets)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_PARAMS")
	require.Empty(t, seedStoreDump(t, k, ctx))
}
