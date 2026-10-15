package types_test

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func usd(amount int64) sdk.Int { return sdk.NewIntWithDecimal(amount, types.USDDecimals) }

func TestParamsDefaults(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())

	require.Equal(t, common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479"), p.CollateralToken)

	wantFees := []struct {
		volume int64
		taker  uint64
		maker  int64
	}{
		{0, 450, 150}, {5_000_000, 400, 120}, {25_000_000, 350, 80}, {100_000_000, 300, 40},
		{500_000_000, 280, 0}, {2_000_000_000, 260, 0}, {7_000_000_000, 240, 0},
	}
	require.Len(t, p.FeeTiers, len(wantFees))
	for i, want := range wantFees {
		require.True(t, p.FeeTiers[i].MinVolumeUsd.Equal(usd(want.volume)), "tier %d volume", i)
		require.Equal(t, want.taker, p.FeeTiers[i].TakerFeePpm, "tier %d taker", i)
		require.Equal(t, want.maker, p.FeeTiers[i].MakerFeePpm, "tier %d maker", i)
	}
	require.Equal(t, []types.RebateTier{
		{MinMakerSharePpm: 5_000, MakerFeePpm: -10},
		{MinMakerSharePpm: 15_000, MakerFeePpm: -20},
		{MinMakerSharePpm: 30_000, MakerFeePpm: -30},
	}, p.RebateTiers)
	require.Equal(t, uint64(0), p.InsuranceSharePpm)

	require.Equal(t, uint64(100), p.FundingInterestPpm8h)
	require.Equal(t, uint64(500), p.FundingPremiumClampPpm)
	require.Equal(t, uint64(40_000), p.FundingCapPpmPerHour)
	require.Equal(t, uint64(5), p.PremiumIntervalSeconds)
	require.Equal(t, uint64(150), p.MarkEmaSeconds)

	wantCaps := []struct {
		age uint64
		cap int64
	}{{0, 2_000_000}, {30, 5_000_000}, {90, 10_000_000}, {180, 25_000_000}}
	require.Len(t, p.ClassEOiCaps, len(wantCaps))
	for i, want := range wantCaps {
		require.Equal(t, want.age, p.ClassEOiCaps[i].MinAgeDays, "cap row %d age", i)
		require.True(t, p.ClassEOiCaps[i].CapUsd.Equal(usd(want.cap)), "cap row %d", i)
	}

	require.Equal(t, uint64(100_000), p.WithdrawFloorPpm)
	require.True(t, p.LiquidationSliceThresholdUsd.Equal(usd(100_000)))
	require.Equal(t, uint64(200_000), p.LiquidationSlicePpm)
	require.Equal(t, uint64(30), p.LiquidationCooldownSeconds)
	require.Equal(t, uint64(2), p.BackstopRatioNumerator)
	require.Equal(t, uint64(3), p.BackstopRatioDenominator)

	require.Equal(t, uint64(512), p.SweepPositionsPerBlock)
	require.Equal(t, uint64(1_024), p.LiquidationScanPerBlock)
	require.Equal(t, uint64(64), p.LiquidationsPerBlock)
	require.Equal(t, uint64(256), p.SlicesPerBlock)
	require.Equal(t, uint64(64), p.AdlPositionsPerBlock)
	require.Equal(t, uint64(256), p.TriggersPerBlock)
	require.Equal(t, uint64(256), p.ExpiriesPerBlock)

	require.True(t, p.MinOrderValueUsd.Equal(usd(10)))
	wantBounds := []struct {
		leverage uint64
		value    int64
	}{{0, 500_000}, {10, 2_000_000}, {20, 5_000_000}, {25, 30_000_000}}
	require.Len(t, p.MarketOrderMaxValues, len(wantBounds))
	for i, want := range wantBounds {
		require.Equal(t, want.leverage, p.MarketOrderMaxValues[i].MinMaxLeverage, "bound %d leverage", i)
		require.True(t, p.MarketOrderMaxValues[i].MaxValueUsd.Equal(usd(want.value)), "bound %d", i)
	}
	require.Equal(t, uint64(10), p.LimitOrderValueMultiple)
	require.Equal(t, uint64(5_000), p.DefaultSlippagePpm)
	require.Equal(t, uint64(1_000), p.MaxOpenOrdersPerAccount)
	require.Equal(t, uint64(32), p.MaxFillsPerOrder)
	require.False(t, p.Paused)

	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var decoded types.Params
	require.NoError(t, json.Unmarshal(raw, &decoded))
	again, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(again))
	require.NoError(t, decoded.Validate())
}

func TestParamsValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"maker above taker", func(p *types.Params) { p.FeeTiers[0].MakerFeePpm = 500 }},
		{"zero collateral token", func(p *types.Params) { p.CollateralToken = common.Address{} }},
		{"no fee tiers", func(p *types.Params) { p.FeeTiers = nil }},
		{"tier 0 above zero volume", func(p *types.Params) { p.FeeTiers[0].MinVolumeUsd = usd(1) }},
		{"inverted volume thresholds", func(p *types.Params) { p.FeeTiers[2].MinVolumeUsd = usd(5_000_000) }},
		{"inverted taker tiers", func(p *types.Params) { p.FeeTiers[3].TakerFeePpm = 360 }},
		{"inverted maker tiers", func(p *types.Params) { p.FeeTiers[2].MakerFeePpm = 130 }},
		{"negative maker in fee tier", func(p *types.Params) { p.FeeTiers[6].MakerFeePpm = -1 }},
		{"taker fee out of range", func(p *types.Params) { p.FeeTiers[0].TakerFeePpm = 1_000_000 }},
		{"positive rebate", func(p *types.Params) { p.RebateTiers[0].MakerFeePpm = 0 }},
		{"rebate above lowest taker", func(p *types.Params) { p.RebateTiers[2].MakerFeePpm = -241 }},
		{"inverted rebate shares", func(p *types.Params) { p.RebateTiers[1].MinMakerSharePpm = 5_000 }},
		{"inverted rebate rates", func(p *types.Params) { p.RebateTiers[2].MakerFeePpm = -15 }},
		{"rebate share out of range", func(p *types.Params) { p.RebateTiers[2].MinMakerSharePpm = 1_000_000 }},
		{"insurance share out of range", func(p *types.Params) { p.InsuranceSharePpm = 1_000_001 }},
		{"zero funding cap", func(p *types.Params) { p.FundingCapPpmPerHour = 0 }},
		{"funding cap out of range", func(p *types.Params) { p.FundingCapPpmPerHour = 1_000_001 }},
		{"funding interest out of range", func(p *types.Params) { p.FundingInterestPpm8h = 1_000_001 }},
		{"zero premium interval", func(p *types.Params) { p.PremiumIntervalSeconds = 0 }},
		{"zero mark EMA", func(p *types.Params) { p.MarkEmaSeconds = 0 }},
		{"no class E caps", func(p *types.Params) { p.ClassEOiCaps = nil }},
		{"class E first age nonzero", func(p *types.Params) { p.ClassEOiCaps[0].MinAgeDays = 1 }},
		{"class E ages not ascending", func(p *types.Params) { p.ClassEOiCaps[2].MinAgeDays = 30 }},
		{"class E caps not ascending", func(p *types.Params) { p.ClassEOiCaps[2].CapUsd = usd(5_000_000) }},
		{"negative class E cap", func(p *types.Params) { p.ClassEOiCaps[0].CapUsd = usd(-1) }},
		{"withdraw floor out of range", func(p *types.Params) { p.WithdrawFloorPpm = 1_000_001 }},
		{"zero slice threshold", func(p *types.Params) { p.LiquidationSliceThresholdUsd = sdk.ZeroInt() }},
		{"zero slice", func(p *types.Params) { p.LiquidationSlicePpm = 0 }},
		{"slice out of range", func(p *types.Params) { p.LiquidationSlicePpm = 1_000_001 }},
		{"zero cooldown", func(p *types.Params) { p.LiquidationCooldownSeconds = 0 }},
		{"backstop ratio of one", func(p *types.Params) { p.BackstopRatioNumerator = 3 }},
		{"zero sweep bound", func(p *types.Params) { p.SweepPositionsPerBlock = 0 }},
		{"zero scan bound", func(p *types.Params) { p.LiquidationScanPerBlock = 0 }},
		{"zero liquidation bound", func(p *types.Params) { p.LiquidationsPerBlock = 0 }},
		{"zero slice bound", func(p *types.Params) { p.SlicesPerBlock = 0 }},
		{"zero ADL bound", func(p *types.Params) { p.AdlPositionsPerBlock = 0 }},
		{"zero trigger bound", func(p *types.Params) { p.TriggersPerBlock = 0 }},
		{"zero expiry bound", func(p *types.Params) { p.ExpiriesPerBlock = 0 }},
		{"zero minimum order value", func(p *types.Params) { p.MinOrderValueUsd = sdk.ZeroInt() }},
		{"no market order bounds", func(p *types.Params) { p.MarketOrderMaxValues = nil }},
		{"market bound below minimum", func(p *types.Params) { p.MarketOrderMaxValues[0].MaxValueUsd = usd(10) }},
		{"market bounds not ascending", func(p *types.Params) { p.MarketOrderMaxValues[3].MinMaxLeverage = 20 }},
		{"zero limit multiple", func(p *types.Params) { p.LimitOrderValueMultiple = 0 }},
		{"zero slippage", func(p *types.Params) { p.DefaultSlippagePpm = 0 }},
		{"zero open orders", func(p *types.Params) { p.MaxOpenOrdersPerAccount = 0 }},
		{"zero fills per order", func(p *types.Params) { p.MaxFillsPerOrder = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			require.ErrorIs(t, p.Validate(), types.ErrInvalidParams)
		})
	}
}

func TestParamsValidateTakerBelowMaker(t *testing.T) {
	p := types.DefaultParams()
	p.FeeTiers[0].TakerFeePpm = 149
	require.ErrorIs(t, p.Validate(), types.ErrInvalidParams)

	p.FeeTiers[0].TakerFeePpm = 450
	p.FeeTiers[0].MakerFeePpm = 450
	require.NoError(t, p.Validate())
}
