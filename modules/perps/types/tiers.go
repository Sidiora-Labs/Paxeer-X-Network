package types

import (
	"time"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// TierClass is the margin tier class of a market (contracts/perps-markets.md
// section 3).
type TierClass uint8

const (
	TierClassA TierClass = iota // BTC and ETH
	TierClassB                  // major alternative assets
	TierClassC                  // indices and gold
	TierClassD                  // single equities
	TierClassE                  // native and new listings
	TierClassF                  // LINK
)

const (
	// BpsDenominator is the basis-point scale of maintenance rates.
	BpsDenominator = 10_000

	// SecondsPerDay is the length of one listing-age day.
	SecondsPerDay = 86_400

	// maintenanceNumeratorBps is MMR = 0.5 / max leverage in basis points.
	maintenanceNumeratorBps = BpsDenominator / 2
)

// MarginTier is one row of a market's tier table: inside [LowerBoundUsd, next
// lower bound) leverage is at most MaxLeverage and maintenance margin is
// notional x MaintenanceBps / 10,000 - DeductionUsd.
type MarginTier struct {
	LowerBoundUsd  sdk.Int `json:"lower_bound_usd"`
	MaxLeverage    uint64  `json:"max_leverage"`
	MaintenanceBps uint64  `json:"maintenance_bps"`
	DeductionUsd   sdk.Int `json:"deduction_usd"`
}

type tierBound struct {
	lowerBoundUsd int64
	maxLeverage   uint64
}

// tierSheets are the lower bounds and leverages of section 4 per class.
var tierSheets = map[TierClass][]tierBound{
	TierClassA: {{0, 1000}, {1_000_000, 500}, {5_000_000, 200}, {20_000_000, 100}, {50_000_000, 40}, {150_000_000, 20}},
	TierClassB: {{0, 1000}, {500_000, 200}, {2_000_000, 50}, {10_000_000, 20}, {30_000_000, 10}},
	TierClassC: {{0, 1000}, {1_000_000, 200}, {5_000_000, 50}, {20_000_000, 20}},
	TierClassD: {{0, 1000}, {250_000, 100}, {1_000_000, 20}, {5_000_000, 10}},
	TierClassE: {{0, 1000}, {10_000, 100}, {50_000, 20}, {250_000, 10}, {1_000_000, 5}},
	TierClassF: {{0, 1000}, {250_000, 50}, {1_000_000, 20}, {5_000_000, 10}},
}

func maintenanceBps(maxLeverage uint64) uint64 { return maintenanceNumeratorBps / maxLeverage }

// nextDeduction is deduction(n-1) + lowerBound(n) x (MMR(n) - MMR(n-1)).
func nextDeduction(prev MarginTier, lowerBoundUsd sdk.Int, bps uint64) sdk.Int {
	step := sdk.NewIntFromUint64(bps - prev.MaintenanceBps)
	return prev.DeductionUsd.Add(lowerBoundUsd.Mul(step).QuoRaw(BpsDenominator))
}

// TiersFor is the tier table of the market's class with MMR = 0.5 / max
// leverage and deductions by the recurrence of contracts/perps-markets.md
// section 1, so maintenance margin is continuous at every boundary.
func TiersFor(market Market) ([]MarginTier, error) {
	sheet, ok := tierSheets[market.TierClass]
	if !ok {
		return nil, invalid("market %d has unknown tier class %d", market.ID, market.TierClass)
	}
	tiers := make([]MarginTier, len(sheet))
	for i, row := range sheet {
		lower := usd(row.lowerBoundUsd)
		bps := maintenanceBps(row.maxLeverage)
		deduction := sdk.ZeroInt()
		if i > 0 {
			deduction = nextDeduction(tiers[i-1], lower, bps)
		}
		tiers[i] = MarginTier{LowerBoundUsd: lower, MaxLeverage: row.maxLeverage, MaintenanceBps: bps, DeductionUsd: deduction}
	}
	return tiers, nil
}

// ValidateTiers refuses a table that does not start at 0 and 1000x, whose
// bounds do not strictly increase, whose leverages do not strictly decrease,
// whose MMR is not 0.5 / max leverage, or whose deductions break the
// recurrence.
func ValidateTiers(tiers []MarginTier) error {
	if len(tiers) == 0 {
		return invalid("empty tier table")
	}
	for i, tier := range tiers {
		if tier.LowerBoundUsd.IsNil() || tier.LowerBoundUsd.IsNegative() || tier.DeductionUsd.IsNil() {
			return invalid("tier %d bound and deduction must be set and non-negative", i)
		}
		if tier.MaxLeverage == 0 || maintenanceNumeratorBps%tier.MaxLeverage != 0 {
			return invalid("tier %d max leverage %d does not give a whole-bps MMR", i, tier.MaxLeverage)
		}
		if tier.MaintenanceBps != maintenanceBps(tier.MaxLeverage) {
			return invalid("tier %d MMR %d bps is not 0.5 / %d", i, tier.MaintenanceBps, tier.MaxLeverage)
		}
		if i == 0 {
			if !tier.LowerBoundUsd.IsZero() || !tier.DeductionUsd.IsZero() || tier.MaxLeverage != VenueMaxLeverage {
				return invalid("tier 0 must start at 0 USD and %dx with no deduction", VenueMaxLeverage)
			}
			continue
		}
		prev := tiers[i-1]
		if !tier.LowerBoundUsd.GT(prev.LowerBoundUsd) || tier.MaxLeverage >= prev.MaxLeverage {
			return invalid("tier %d not strictly above tier %d", i, i-1)
		}
		if !tier.DeductionUsd.Equal(nextDeduction(prev, tier.LowerBoundUsd, tier.MaintenanceBps)) {
			return invalid("tier %d deduction breaks the recurrence", i)
		}
	}
	return nil
}

// OiCapInForce is the per-side open-interest cap of the market at blockTime:
// the fixed cap, or for class E the row of params.ClassEOiCaps whose age range
// contains floor((blockTime - listedAt) / 86400) days.
func OiCapInForce(market Market, params Params, blockTime time.Time) (sdk.Int, error) {
	if _, ok := tierSheets[market.TierClass]; !ok {
		return sdk.Int{}, invalid("market %d has unknown tier class %d", market.ID, market.TierClass)
	}
	if market.TierClass != TierClassE {
		return market.OiCapUsd, nil
	}
	now := blockTime.Unix()
	if now < 0 || uint64(now) < market.ListedAt {
		return sdk.Int{}, invalid("block time %d before market %d listing %d", now, market.ID, market.ListedAt)
	}
	ageDays := (uint64(now) - market.ListedAt) / SecondsPerDay
	var capUsd sdk.Int
	for _, row := range params.ClassEOiCaps {
		if row.MinAgeDays > ageDays {
			break
		}
		capUsd = row.CapUsd
	}
	if !positive(capUsd) {
		return sdk.Int{}, invalid("no class E cap row covers listing age %d days", ageDays)
	}
	return capUsd, nil
}
