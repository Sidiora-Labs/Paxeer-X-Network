package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
)

const (
	DefaultStalenessWindowMs            uint64 = 10000
	DefaultMaxClockSkewMs               uint64 = 2000
	DefaultMinSources                   uint32 = 1
	DefaultMaxDivergenceBps             uint32 = 50
	DefaultMaxDeviationBps              uint32 = 1000
	DefaultMaxObservationsPerSubmission uint32 = 32
	DefaultMaxSubmissionBytes           uint32 = 8192
	DefaultMarkEmaTauMs                 uint64 = 150000
	DefaultMaxMarkBasisBps              uint32 = 500
	DefaultMaxPriceAgeMs                uint64 = 15000

	// BpsDenominator is 100% in basis points, the upper bound of every bps field.
	BpsDenominator uint32 = 10000
	// MinSubmissionBytesLimit and MaxSubmissionBytesLimit bound max_submission_bytes.
	MinSubmissionBytesLimit uint32 = 512
	MaxSubmissionBytesLimit uint32 = 65536
)

// Params is the whole pricefeed parameter set; MsgSetParams replaces it.
type Params struct {
	Authority                    string   `json:"authority"`
	Markets                      []Market `json:"markets"`
	StalenessWindowMs            uint64   `json:"staleness_window_ms"`
	MaxClockSkewMs               uint64   `json:"max_clock_skew_ms"`
	MinSources                   uint32   `json:"min_sources"`
	MaxDivergenceBps             uint32   `json:"max_divergence_bps"`
	MaxDeviationBps              uint32   `json:"max_deviation_bps"`
	MaxObservationsPerSubmission uint32   `json:"max_observations_per_submission"`
	MaxSubmissionBytes           uint32   `json:"max_submission_bytes"`
	MarkEmaTauMs                 uint64   `json:"mark_ema_tau_ms"`
	MaxMarkBasisBps              uint32   `json:"max_mark_basis_bps"`
	MaxPriceAgeMs                uint64   `json:"max_price_age_ms"`
}

// DefaultAuthority is the gov module account.
func DefaultAuthority() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

// DefaultParams carries the eighteen markets of the market table and the
// defaults of the contract parameter table.
func DefaultParams(authority string) Params {
	return Params{
		Authority:                    authority,
		Markets:                      append([]Market(nil), Markets...),
		StalenessWindowMs:            DefaultStalenessWindowMs,
		MaxClockSkewMs:               DefaultMaxClockSkewMs,
		MinSources:                   DefaultMinSources,
		MaxDivergenceBps:             DefaultMaxDivergenceBps,
		MaxDeviationBps:              DefaultMaxDeviationBps,
		MaxObservationsPerSubmission: DefaultMaxObservationsPerSubmission,
		MaxSubmissionBytes:           DefaultMaxSubmissionBytes,
		MarkEmaTauMs:                 DefaultMarkEmaTauMs,
		MaxMarkBasisBps:              DefaultMaxMarkBasisBps,
		MaxPriceAgeMs:                DefaultMaxPriceAgeMs,
	}
}

// Validate checks every bound of the parameter table and the market rules:
// ids dense from 0, symbols unique and 0 < min_price < max_price.
func (p Params) Validate() error {
	if _, err := sdk.AccAddressFromBech32(p.Authority); err != nil {
		return ErrInvalidParams.Wrapf("authority: %v", err)
	}
	if err := validateMarkets(p.Markets); err != nil {
		return err
	}
	if p.StalenessWindowMs == 0 {
		return ErrInvalidParams.Wrap("staleness_window_ms is zero")
	}
	if p.MaxClockSkewMs > p.StalenessWindowMs {
		return ErrInvalidParams.Wrapf("max_clock_skew_ms %d above staleness_window_ms %d", p.MaxClockSkewMs, p.StalenessWindowMs)
	}
	if p.MinSources == 0 {
		return ErrInvalidParams.Wrap("min_sources is zero")
	}
	if err := validateBps("max_divergence_bps", p.MaxDivergenceBps); err != nil {
		return err
	}
	if err := validateBps("max_deviation_bps", p.MaxDeviationBps); err != nil {
		return err
	}
	if p.MaxObservationsPerSubmission == 0 || p.MaxObservationsPerSubmission > MaxObservationsPerMessage {
		return ErrInvalidParams.Wrapf("max_observations_per_submission %d outside 1..%d",
			p.MaxObservationsPerSubmission, MaxObservationsPerMessage)
	}
	if p.MaxSubmissionBytes < MinSubmissionBytesLimit || p.MaxSubmissionBytes > MaxSubmissionBytesLimit {
		return ErrInvalidParams.Wrapf("max_submission_bytes %d outside %d..%d",
			p.MaxSubmissionBytes, MinSubmissionBytesLimit, MaxSubmissionBytesLimit)
	}
	if p.MarkEmaTauMs == 0 {
		return ErrInvalidParams.Wrap("mark_ema_tau_ms is zero")
	}
	if err := validateBps("max_mark_basis_bps", p.MaxMarkBasisBps); err != nil {
		return err
	}
	if p.MaxPriceAgeMs < p.StalenessWindowMs {
		return ErrInvalidParams.Wrapf("max_price_age_ms %d below staleness_window_ms %d", p.MaxPriceAgeMs, p.StalenessWindowMs)
	}
	return nil
}

func validateBps(field string, value uint32) error {
	if value == 0 || value > BpsDenominator {
		return ErrInvalidParams.Wrapf("%s %d outside 1..%d", field, value, BpsDenominator)
	}
	return nil
}

func validateMarkets(markets []Market) error {
	symbols := make(map[string]struct{}, len(markets))
	for i, market := range markets {
		if market.ID != uint64(i) {
			return ErrInvalidParams.Wrapf("market %d has id %d, ids are dense from 0", i, market.ID)
		}
		if market.Symbol == "" || market.ChainflowSymbol == "" {
			return ErrInvalidParams.Wrapf("market %d has an empty symbol", market.ID)
		}
		if _, duplicate := symbols[market.Symbol]; duplicate {
			return ErrInvalidParams.Wrapf("market %d repeats symbol %s", market.ID, market.Symbol)
		}
		symbols[market.Symbol] = struct{}{}
		if market.MinPrice.IsNil() || market.MaxPrice.IsNil() || !market.MinPrice.IsPositive() ||
			market.MinPrice.GTE(market.MaxPrice) {
			return ErrInvalidParams.Wrapf("market %d needs 0 < min_price < max_price", market.ID)
		}
	}
	return nil
}
