package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	// PpmDenominator is the parts-per-million scale of every rate parameter.
	PpmDenominator = 1_000_000

	// USDDecimals is the fixed-point exponent of USD amounts.
	USDDecimals = 18

	// DefaultCollateralToken is the USDL ERC20 contract.
	DefaultCollateralToken = "0x85FcD13735F4309833A503EE804ea32395851479"
)

// FeeTier is one row of the 14-day volume fee table: the tier applies when
// the account's 14-day volume exceeds MinVolumeUsd (tier 0 from 0).
type FeeTier struct {
	MinVolumeUsd sdk.Int `json:"min_volume_usd"`
	TakerFeePpm  uint64  `json:"taker_fee_ppm"`
	MakerFeePpm  int64   `json:"maker_fee_ppm"`
}

// RebateTier replaces the maker rate when the account's 14-day maker volume
// exceeds MinMakerSharePpm of the venue's 14-day volume.
type RebateTier struct {
	MinMakerSharePpm uint64 `json:"min_maker_share_ppm"`
	MakerFeePpm      int64  `json:"maker_fee_ppm"`
}

// OiCapRow is one row of the class E open-interest cap table: CapUsd per side
// from listing age MinAgeDays.
type OiCapRow struct {
	MinAgeDays uint64  `json:"min_age_days"`
	CapUsd     sdk.Int `json:"cap_usd"`
}

// OrderValueRow bounds a market order's value to MaxValueUsd for markets
// whose maximum leverage is at least MinMaxLeverage.
type OrderValueRow struct {
	MinMaxLeverage uint64  `json:"min_max_leverage"`
	MaxValueUsd    sdk.Int `json:"max_value_usd"`
}

// Params are the venue parameters of contracts/perps-parameters.md. USD
// amounts are 18-decimal fixed point, rates are ppm. Params change only
// through UpdateParams, which accepts the governance module account alone.
type Params struct {
	CollateralToken   common.Address `json:"collateral_token"`
	FeeTiers          []FeeTier      `json:"fee_tiers"`
	RebateTiers       []RebateTier   `json:"rebate_tiers"`
	InsuranceSharePpm uint64         `json:"insurance_share_ppm"`

	FundingInterestPpm8h   uint64 `json:"funding_interest_ppm_8h"`
	FundingPremiumClampPpm uint64 `json:"funding_premium_clamp_ppm"`
	FundingCapPpmPerHour   uint64 `json:"funding_cap_ppm_per_hour"`
	PremiumIntervalSeconds uint64 `json:"premium_interval_seconds"`
	MarkEmaSeconds         uint64 `json:"mark_ema_seconds"`

	ClassEOiCaps []OiCapRow `json:"class_e_oi_caps"`

	WithdrawFloorPpm             uint64  `json:"withdraw_floor_ppm"`
	LiquidationSliceThresholdUsd sdk.Int `json:"liquidation_slice_threshold_usd"`
	LiquidationSlicePpm          uint64  `json:"liquidation_slice_ppm"`
	LiquidationCooldownSeconds   uint64  `json:"liquidation_cooldown_seconds"`
	BackstopRatioNumerator       uint64  `json:"backstop_ratio_numerator"`
	BackstopRatioDenominator     uint64  `json:"backstop_ratio_denominator"`

	SweepPositionsPerBlock  uint64 `json:"sweep_positions_per_block"`
	LiquidationScanPerBlock uint64 `json:"liquidation_scan_per_block"`
	LiquidationsPerBlock    uint64 `json:"liquidations_per_block"`
	SlicesPerBlock          uint64 `json:"slices_per_block"`
	AdlPositionsPerBlock    uint64 `json:"adl_positions_per_block"`
	TriggersPerBlock        uint64 `json:"triggers_per_block"`
	ExpiriesPerBlock        uint64 `json:"expiries_per_block"`

	MinOrderValueUsd        sdk.Int         `json:"min_order_value_usd"`
	MarketOrderMaxValues    []OrderValueRow `json:"market_order_max_values"`
	LimitOrderValueMultiple uint64          `json:"limit_order_value_multiple"`
	DefaultSlippagePpm      uint64          `json:"default_slippage_ppm"`
	MaxOpenOrdersPerAccount uint64          `json:"max_open_orders_per_account"`
	MaxFillsPerOrder        uint64          `json:"max_fills_per_order"`

	Paused bool `json:"paused"`
}

func usd(amount int64) sdk.Int { return sdk.NewIntWithDecimal(amount, USDDecimals) }

// DefaultParams are the activation figures of contracts/perps-parameters.md
// sections 1 to 8.
func DefaultParams() Params {
	return Params{
		CollateralToken: common.HexToAddress(DefaultCollateralToken),
		FeeTiers: []FeeTier{
			{MinVolumeUsd: sdk.ZeroInt(), TakerFeePpm: 450, MakerFeePpm: 150},
			{MinVolumeUsd: usd(5_000_000), TakerFeePpm: 400, MakerFeePpm: 120},
			{MinVolumeUsd: usd(25_000_000), TakerFeePpm: 350, MakerFeePpm: 80},
			{MinVolumeUsd: usd(100_000_000), TakerFeePpm: 300, MakerFeePpm: 40},
			{MinVolumeUsd: usd(500_000_000), TakerFeePpm: 280, MakerFeePpm: 0},
			{MinVolumeUsd: usd(2_000_000_000), TakerFeePpm: 260, MakerFeePpm: 0},
			{MinVolumeUsd: usd(7_000_000_000), TakerFeePpm: 240, MakerFeePpm: 0},
		},
		RebateTiers: []RebateTier{
			{MinMakerSharePpm: 5_000, MakerFeePpm: -10},
			{MinMakerSharePpm: 15_000, MakerFeePpm: -20},
			{MinMakerSharePpm: 30_000, MakerFeePpm: -30},
		},
		InsuranceSharePpm: 0,

		FundingInterestPpm8h:   100,
		FundingPremiumClampPpm: 500,
		FundingCapPpmPerHour:   40_000,
		PremiumIntervalSeconds: 5,
		MarkEmaSeconds:         150,

		ClassEOiCaps: []OiCapRow{
			{MinAgeDays: 0, CapUsd: usd(2_000_000)},
			{MinAgeDays: 30, CapUsd: usd(5_000_000)},
			{MinAgeDays: 90, CapUsd: usd(10_000_000)},
			{MinAgeDays: 180, CapUsd: usd(25_000_000)},
		},

		WithdrawFloorPpm:             100_000,
		LiquidationSliceThresholdUsd: usd(100_000),
		LiquidationSlicePpm:          200_000,
		LiquidationCooldownSeconds:   30,
		BackstopRatioNumerator:       2,
		BackstopRatioDenominator:     3,

		SweepPositionsPerBlock:  512,
		LiquidationScanPerBlock: 1_024,
		LiquidationsPerBlock:    64,
		SlicesPerBlock:          256,
		AdlPositionsPerBlock:    64,
		TriggersPerBlock:        256,
		ExpiriesPerBlock:        256,

		MinOrderValueUsd: usd(10),
		MarketOrderMaxValues: []OrderValueRow{
			{MinMaxLeverage: 0, MaxValueUsd: usd(500_000)},
			{MinMaxLeverage: 10, MaxValueUsd: usd(2_000_000)},
			{MinMaxLeverage: 20, MaxValueUsd: usd(5_000_000)},
			{MinMaxLeverage: 25, MaxValueUsd: usd(30_000_000)},
		},
		LimitOrderValueMultiple: 10,
		DefaultSlippagePpm:      5_000,
		MaxOpenOrdersPerAccount: 1_000,
		MaxFillsPerOrder:        32,

		Paused: false,
	}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidParams}, args...)...)
}

func positive(value sdk.Int) bool { return !value.IsNil() && value.IsPositive() }

func (p Params) Validate() error {
	if p.CollateralToken == (common.Address{}) {
		return invalid("zero collateral token")
	}
	if err := p.validateFees(); err != nil {
		return err
	}
	if p.FundingInterestPpm8h > PpmDenominator || p.FundingPremiumClampPpm > PpmDenominator {
		return invalid("funding interest and premium clamp must be at most %d ppm", PpmDenominator)
	}
	if p.FundingCapPpmPerHour == 0 || p.FundingCapPpmPerHour > PpmDenominator {
		return invalid("funding cap must be in (0, %d] ppm", PpmDenominator)
	}
	if p.PremiumIntervalSeconds == 0 || p.MarkEmaSeconds == 0 {
		return invalid("premium interval and mark EMA time constant must be positive")
	}
	if err := p.validateOiCaps(); err != nil {
		return err
	}
	if err := p.validateLiquidation(); err != nil {
		return err
	}
	for _, bound := range []uint64{
		p.SweepPositionsPerBlock, p.LiquidationScanPerBlock, p.LiquidationsPerBlock,
		p.SlicesPerBlock, p.AdlPositionsPerBlock, p.TriggersPerBlock, p.ExpiriesPerBlock,
	} {
		if bound == 0 {
			return invalid("per-block bounds must be positive")
		}
	}
	return p.validateOrderLimits()
}

func (p Params) validateFees() error {
	if len(p.FeeTiers) == 0 {
		return invalid("no fee tiers")
	}
	for i, tier := range p.FeeTiers {
		if tier.MinVolumeUsd.IsNil() || tier.MinVolumeUsd.IsNegative() {
			return invalid("fee tier %d volume threshold must be a non-negative integer", i)
		}
		if tier.TakerFeePpm >= PpmDenominator {
			return invalid("fee tier %d taker fee must be below %d ppm", i, PpmDenominator)
		}
		if tier.MakerFeePpm < 0 || uint64(tier.MakerFeePpm) > tier.TakerFeePpm {
			return invalid("fee tier %d maker fee must be in [0, taker fee]", i)
		}
		if i == 0 {
			if !tier.MinVolumeUsd.IsZero() {
				return invalid("fee tier 0 must start at volume 0")
			}
			continue
		}
		prev := p.FeeTiers[i-1]
		if !tier.MinVolumeUsd.GT(prev.MinVolumeUsd) {
			return invalid("fee tier %d volume threshold not above tier %d", i, i-1)
		}
		if tier.TakerFeePpm > prev.TakerFeePpm || tier.MakerFeePpm > prev.MakerFeePpm {
			return invalid("fee tier %d charges more than tier %d", i, i-1)
		}
	}
	lowestTaker := p.FeeTiers[len(p.FeeTiers)-1].TakerFeePpm
	for i, tier := range p.RebateTiers {
		if tier.MinMakerSharePpm == 0 || tier.MinMakerSharePpm >= PpmDenominator {
			return invalid("rebate tier %d maker share must be in (0, %d) ppm", i, PpmDenominator)
		}
		if tier.MakerFeePpm >= 0 || uint64(-tier.MakerFeePpm) > lowestTaker {
			return invalid("rebate tier %d maker rate must be negative and its rebate at most the lowest taker fee", i)
		}
		if i == 0 {
			continue
		}
		prev := p.RebateTiers[i-1]
		if tier.MinMakerSharePpm <= prev.MinMakerSharePpm || tier.MakerFeePpm >= prev.MakerFeePpm {
			return invalid("rebate tier %d not above rebate tier %d", i, i-1)
		}
	}
	if p.InsuranceSharePpm > PpmDenominator {
		return invalid("insurance share above %d ppm", PpmDenominator)
	}
	return nil
}

func (p Params) validateOiCaps() error {
	if len(p.ClassEOiCaps) == 0 {
		return invalid("no class E open-interest caps")
	}
	for i, row := range p.ClassEOiCaps {
		if !positive(row.CapUsd) {
			return invalid("class E cap row %d must be positive", i)
		}
		if i == 0 {
			if row.MinAgeDays != 0 {
				return invalid("class E cap table must start at age 0")
			}
			continue
		}
		prev := p.ClassEOiCaps[i-1]
		if row.MinAgeDays <= prev.MinAgeDays || !row.CapUsd.GT(prev.CapUsd) {
			return invalid("class E cap row %d not strictly above row %d", i, i-1)
		}
	}
	return nil
}

func (p Params) validateLiquidation() error {
	if p.WithdrawFloorPpm > PpmDenominator {
		return invalid("withdraw floor above %d ppm", PpmDenominator)
	}
	if !positive(p.LiquidationSliceThresholdUsd) {
		return invalid("liquidation slice threshold must be positive")
	}
	if p.LiquidationSlicePpm == 0 || p.LiquidationSlicePpm > PpmDenominator {
		return invalid("liquidation slice must be in (0, %d] ppm", PpmDenominator)
	}
	if p.LiquidationCooldownSeconds == 0 {
		return invalid("liquidation cooldown must be positive")
	}
	if p.BackstopRatioNumerator == 0 || p.BackstopRatioNumerator >= p.BackstopRatioDenominator {
		return invalid("backstop ratio must be in (0, 1)")
	}
	return nil
}

func (p Params) validateOrderLimits() error {
	if !positive(p.MinOrderValueUsd) {
		return invalid("minimum order value must be positive")
	}
	if len(p.MarketOrderMaxValues) == 0 {
		return invalid("no market order value bounds")
	}
	for i, row := range p.MarketOrderMaxValues {
		if row.MaxValueUsd.IsNil() || !row.MaxValueUsd.GT(p.MinOrderValueUsd) {
			return invalid("market order bound %d must exceed the minimum order value", i)
		}
		if i == 0 {
			if row.MinMaxLeverage != 0 {
				return invalid("market order bounds must start at leverage 0")
			}
			continue
		}
		prev := p.MarketOrderMaxValues[i-1]
		if row.MinMaxLeverage <= prev.MinMaxLeverage || !row.MaxValueUsd.GT(prev.MaxValueUsd) {
			return invalid("market order bound %d not strictly above bound %d", i, i-1)
		}
	}
	if p.LimitOrderValueMultiple == 0 {
		return invalid("limit order value multiple must be positive")
	}
	if p.DefaultSlippagePpm == 0 || p.DefaultSlippagePpm >= PpmDenominator {
		return invalid("default slippage must be in (0, %d) ppm", PpmDenominator)
	}
	if p.MaxOpenOrdersPerAccount == 0 || p.MaxFillsPerOrder == 0 {
		return invalid("order limits must be positive")
	}
	return nil
}
