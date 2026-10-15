package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const listedAt uint64 = 1_700_000_000

func tierMarket(id uint64, symbol string, sz, px uint8, class types.TierClass, impactUsd, capUsd int64) types.Market {
	m := types.Market{
		ID:                   id,
		Symbol:               symbol,
		Name:                 symbol,
		OracleSymbol:         symbol,
		SzDecimals:           sz,
		PxDecimals:           px,
		MaxLeverage:          1000,
		TierClass:            class,
		ImpactNotionalUsd:    usd(impactUsd),
		FundingCapPpmPerHour: 40_000,
		ListedAt:             listedAt,
		Enabled:              true,
	}
	if capUsd > 0 {
		m.OiCapUsd = usd(capUsd)
	}
	return m
}

func btcMarket() types.Market {
	return tierMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)
}

func paxMarket() types.Market { return tierMarket(11, "PAX", 0, 5, types.TierClassE, 6_000, 0) }

// maintenanceAt is notional x MMR(tier) - deduction(tier) for the highest
// tier whose lower bound is at most the notional.
func maintenanceAt(tiers []types.MarginTier, notional sdk.Int) sdk.Int {
	tier := tiers[0]
	for _, row := range tiers {
		if row.LowerBoundUsd.GT(notional) {
			break
		}
		tier = row
	}
	return notional.Mul(sdk.NewIntFromUint64(tier.MaintenanceBps)).QuoRaw(types.BpsDenominator).Sub(tier.DeductionUsd)
}

func TestTiersDeductionRecurrence(t *testing.T) {
	type row struct {
		lower     int64
		leverage  uint64
		bps       uint64
		deduction int64
	}
	btc := []row{
		{0, 1000, 5, 0},
		{1_000_000, 500, 10, 500},
		{5_000_000, 200, 25, 8_000},
		{20_000_000, 100, 50, 58_000},
		{50_000_000, 40, 125, 433_000},
		{150_000_000, 20, 250, 2_308_000},
	}
	tiers, err := types.TiersFor(btcMarket())
	require.NoError(t, err)
	require.Len(t, tiers, len(btc))
	for i, want := range btc {
		require.True(t, tiers[i].LowerBoundUsd.Equal(usd(want.lower)), "tier %d bound", i)
		require.Equal(t, want.leverage, tiers[i].MaxLeverage, "tier %d leverage", i)
		require.Equal(t, want.bps, tiers[i].MaintenanceBps, "tier %d MMR", i)
		require.Equal(t, uint64(10_000)/want.leverage, 2*tiers[i].MaintenanceBps, "tier %d IMR", i)
		require.True(t, tiers[i].DeductionUsd.Equal(usd(want.deduction)), "tier %d deduction %s", i, tiers[i].DeductionUsd)
	}
	require.NoError(t, types.ValidateTiers(tiers))

	below := maintenanceAt(tiers, usd(999_999))
	at := maintenanceAt(tiers, usd(1_000_000))
	require.True(t, below.Equal(sdk.NewIntWithDecimal(4_999_995, types.USDDecimals-4)), below.String())
	require.True(t, at.Equal(usd(500)), at.String())
	require.True(t, at.Sub(below).LT(sdk.NewIntWithDecimal(1, types.USDDecimals-2)))
	for i := 1; i < len(tiers); i++ {
		boundary := tiers[i].LowerBoundUsd
		lowerSide := boundary.Mul(sdk.NewIntFromUint64(tiers[i-1].MaintenanceBps)).QuoRaw(types.BpsDenominator).Sub(tiers[i-1].DeductionUsd)
		require.True(t, lowerSide.Equal(maintenanceAt(tiers, boundary)), "discontinuity at tier %d", i)
	}

	classes := []struct {
		market     types.Market
		deductions []int64
	}{
		{tierMarket(1, "ETH", 4, 2, types.TierClassA, 20_000, 500_000_000), []int64{500, 8_000, 58_000, 433_000, 2_308_000}},
		{tierMarket(2, "SOL", 2, 3, types.TierClassB, 6_000, 50_000_000), []int64{1_000, 16_000, 166_000, 916_000}},
		{tierMarket(7, "NAS100", 4, 1, types.TierClassC, 6_000, 100_000_000), []int64{2_000, 39_500, 339_500}},
		{tierMarket(5, "TSLA", 3, 2, types.TierClassD, 6_000, 25_000_000), []int64{1_125, 21_125, 146_125}},
		{paxMarket(), []int64{45, 1_045, 7_295, 57_295}},
		{tierMarket(4, "LINK", 1, 3, types.TierClassF, 6_000, 20_000_000), []int64{2_375, 17_375, 142_375}},
	}
	for _, tc := range classes {
		got, err := types.TiersFor(tc.market)
		require.NoError(t, err, tc.market.Symbol)
		require.NoError(t, types.ValidateTiers(got), tc.market.Symbol)
		require.Len(t, got, len(tc.deductions)+1, tc.market.Symbol)
		require.Equal(t, uint64(1000), got[0].MaxLeverage, tc.market.Symbol)
		require.Equal(t, uint64(5), got[0].MaintenanceBps, tc.market.Symbol)
		require.True(t, got[0].DeductionUsd.IsZero(), tc.market.Symbol)
		for i, want := range tc.deductions {
			require.True(t, got[i+1].DeductionUsd.Equal(usd(want)), "%s tier %d deduction %s", tc.market.Symbol, i+1, got[i+1].DeductionUsd)
		}
	}

	broken, err := types.TiersFor(btcMarket())
	require.NoError(t, err)
	broken[3].DeductionUsd = usd(58_001)
	require.ErrorIs(t, types.ValidateTiers(broken), types.ErrInvalidParams)
	wrongRate, err := types.TiersFor(btcMarket())
	require.NoError(t, err)
	wrongRate[1].MaintenanceBps = 11
	require.ErrorIs(t, types.ValidateTiers(wrongRate), types.ErrInvalidParams)
	require.ErrorIs(t, types.ValidateTiers(nil), types.ErrInvalidParams)

	_, err = types.TiersFor(tierMarket(0, "BTC", 5, 1, types.TierClassF+1, 20_000, 500_000_000))
	require.ErrorIs(t, err, types.ErrInvalidParams)
}

func TestTiersMarketValidate(t *testing.T) {
	require.NoError(t, btcMarket().Validate())
	require.NoError(t, paxMarket().Validate())

	cases := []struct {
		name   string
		mutate func(*types.Market)
	}{
		{"pxDecimals 2 with szDecimals 5", func(m *types.Market) { m.PxDecimals = 2 }},
		{"zero OI cap", func(m *types.Market) { m.OiCapUsd = sdk.ZeroInt() }},
		{"unset OI cap", func(m *types.Market) { m.OiCapUsd = sdk.Int{} }},
		{"max leverage above 1000", func(m *types.Market) { m.MaxLeverage = 1001 }},
		{"max leverage below tier 0", func(m *types.Market) { m.MaxLeverage = 500 }},
		{"zero max leverage", func(m *types.Market) { m.MaxLeverage = 0 }},
		{"unknown tier class", func(m *types.Market) { m.TierClass = types.TierClassF + 1 }},
		{"empty symbol", func(m *types.Market) { m.Symbol = "" }},
		{"empty oracle symbol", func(m *types.Market) { m.OracleSymbol = "" }},
		{"zero impact notional", func(m *types.Market) { m.ImpactNotionalUsd = sdk.ZeroInt() }},
		{"zero funding cap", func(m *types.Market) { m.FundingCapPpmPerHour = 0 }},
		{"funding cap above 100 percent", func(m *types.Market) { m.FundingCapPpmPerHour = 1_000_001 }},
	}
	for _, tc := range cases {
		m := btcMarket()
		tc.mutate(&m)
		require.ErrorIs(t, m.Validate(), types.ErrInvalidParams, tc.name)
	}

	pax := paxMarket()
	pax.OiCapUsd = usd(2_000_000)
	require.ErrorIs(t, pax.Validate(), types.ErrInvalidParams, "class E with a fixed cap")
	pax = paxMarket()
	pax.SzDecimals = 2
	require.ErrorIs(t, pax.Validate(), types.ErrInvalidParams, "class E above six decimals")
}

func TestTiersClassEAgeCaps(t *testing.T) {
	params := types.DefaultParams()
	pax := paxMarket()
	at := func(seconds uint64) time.Time { return time.Unix(int64(listedAt+seconds), 0).UTC() }
	day := uint64(types.SecondsPerDay)

	cases := []struct {
		seconds uint64
		capUsd  int64
	}{
		{0, 2_000_000},
		{29 * day, 2_000_000},
		{30*day - 1, 2_000_000},
		{30 * day, 5_000_000},
		{89 * day, 5_000_000},
		{90 * day, 10_000_000},
		{179*day + day - 1, 10_000_000},
		{180 * day, 25_000_000},
		{3_650 * day, 25_000_000},
	}
	for _, tc := range cases {
		got, err := types.OiCapInForce(pax, params, at(tc.seconds))
		require.NoError(t, err)
		require.True(t, got.Equal(usd(tc.capUsd)), "age %d s: %s", tc.seconds, got)
	}

	_, err := types.OiCapInForce(pax, params, time.Unix(int64(listedAt)-1, 0))
	require.ErrorIs(t, err, types.ErrInvalidParams)
	empty := params
	empty.ClassEOiCaps = nil
	_, err = types.OiCapInForce(pax, empty, at(0))
	require.ErrorIs(t, err, types.ErrInvalidParams)

	btc := btcMarket()
	for _, seconds := range []uint64{0, 30 * day, 180 * day} {
		got, err := types.OiCapInForce(btc, params, at(seconds))
		require.NoError(t, err)
		require.True(t, got.Equal(usd(500_000_000)), got.String())
	}

	for name, rows := range map[string][]types.OiCapRow{
		"repeated age": {{MinAgeDays: 0, CapUsd: usd(2_000_000)}, {MinAgeDays: 0, CapUsd: usd(5_000_000)}},
		"falling age":  {{MinAgeDays: 0, CapUsd: usd(2_000_000)}, {MinAgeDays: 90, CapUsd: usd(5_000_000)}, {MinAgeDays: 30, CapUsd: usd(10_000_000)}},
		"equal cap":    {{MinAgeDays: 0, CapUsd: usd(2_000_000)}, {MinAgeDays: 30, CapUsd: usd(2_000_000)}},
		"falling cap":  {{MinAgeDays: 0, CapUsd: usd(5_000_000)}, {MinAgeDays: 30, CapUsd: usd(2_000_000)}},
		"late start":   {{MinAgeDays: 1, CapUsd: usd(2_000_000)}},
	} {
		p := types.DefaultParams()
		p.ClassEOiCaps = rows
		require.ErrorIs(t, p.Validate(), types.ErrInvalidParams, name)
	}
}
