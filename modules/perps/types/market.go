package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	// VenueMaxLeverage is the venue leverage cap; every market lists at it.
	VenueMaxLeverage = 1000

	// CollateralDecimals are the decimals of the USDL ERC20 collateral.
	CollateralDecimals = 6
)

// Market is the venue configuration of one perpetual market: the fields of
// lx_perps_market the venue keeps, with ticks and lots expressed as decimals.
// USD amounts are 18-decimal fixed point; ListedAt is the listing block time
// in Unix seconds.
type Market struct {
	ID                   uint64    `json:"id"`
	Symbol               string    `json:"symbol"`
	Name                 string    `json:"name"`
	OracleSymbol         string    `json:"oracle_symbol"`
	SzDecimals           uint8     `json:"sz_decimals"`
	PxDecimals           uint8     `json:"px_decimals"`
	MaxLeverage          uint64    `json:"max_leverage"`
	TierClass            TierClass `json:"tier_class"`
	ImpactNotionalUsd    sdk.Int   `json:"impact_notional_usd"`
	OiCapUsd             sdk.Int   `json:"oi_cap_usd"`
	FundingCapPpmPerHour uint64    `json:"funding_cap_ppm_per_hour"`
	ListedAt             uint64    `json:"listed_at"`
	Enabled              bool      `json:"enabled"`
}

// Validate applies the common rules of contracts/perps-markets.md section 1.
// A class E market carries no fixed cap: its cap follows Params.ClassEOiCaps.
func (m Market) Validate() error {
	if m.Symbol == "" || m.Name == "" || m.OracleSymbol == "" {
		return invalid("market %d needs a symbol, a name and an oracle symbol", m.ID)
	}
	if uint(m.SzDecimals)+uint(m.PxDecimals) > CollateralDecimals {
		return invalid("market %d szDecimals + pxDecimals above %d", m.ID, CollateralDecimals)
	}
	if m.MaxLeverage == 0 || m.MaxLeverage > VenueMaxLeverage {
		return invalid("market %d max leverage must be in [1, %d]", m.ID, VenueMaxLeverage)
	}
	if !positive(m.ImpactNotionalUsd) {
		return invalid("market %d impact notional must be positive", m.ID)
	}
	if m.FundingCapPpmPerHour == 0 || m.FundingCapPpmPerHour > PpmDenominator {
		return invalid("market %d funding cap must be in (0, %d] ppm", m.ID, PpmDenominator)
	}
	if m.TierClass == TierClassE {
		if !m.OiCapUsd.IsNil() && !m.OiCapUsd.IsZero() {
			return invalid("market %d is class E and takes its cap from the listing-age table", m.ID)
		}
	} else if !positive(m.OiCapUsd) {
		return invalid("market %d open-interest cap must be positive", m.ID)
	}
	tiers, err := TiersFor(m)
	if err != nil {
		return err
	}
	if err := ValidateTiers(tiers); err != nil {
		return err
	}
	if tiers[0].MaxLeverage != m.MaxLeverage {
		return invalid("market %d max leverage %d differs from its tier 0 leverage %d", m.ID, m.MaxLeverage, tiers[0].MaxLeverage)
	}
	return nil
}
