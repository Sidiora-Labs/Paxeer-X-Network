// Package l17 implements exact portfolio primitives. It does not certify providers,
// chain ingestion, HTTP authorization, CDN purging, or production capacity.
package l17

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid portfolio input")
var ErrUnauthorized = errors.New("owner authorization required")
var address = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var integer = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)

func number(s string, signed bool, digits int) (*big.Int, error) {
	if !integer.MatchString(s) || len(strings.TrimPrefix(s, "-")) > digits || (!signed && strings.HasPrefix(s, "-")) {
		return nil, ErrInvalid
	}
	n, _ := new(big.Int).SetString(s, 10)
	return n, nil
}
func uint256(s string) (*big.Int, error) {
	n, e := number(s, false, 78)
	if e != nil {
		return nil, e
	}
	if n.BitLen() > 256 {
		return nil, ErrInvalid
	}
	return n, nil
}
func Wallet(s string) (string, error) {
	if !address.MatchString(s) {
		return "", ErrInvalid
	}
	return strings.ToLower(s), nil
}

const Algorithm = "proportional_floor_v1"

type Watermark struct {
	ChainID            string `json:"chainId"`
	Height             string `json:"height"`
	BlockHash          string `json:"blockHash"`
	ProjectionRevision string `json:"projectionRevision"`
	ObservedAt         string `json:"observedAt"`
	Integrity          string `json:"integrity"`
}
type Ratio struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}
type AssetRef struct {
	Kind         string  `json:"kind"`
	ChainID      *string `json:"chainId"`
	Address      *string `json:"address"`
	CurrencyCode *string `json:"currencyCode"`
	Decimals     *int    `json:"decimals"`
}
type ChainPosition struct {
	ChainID          string `json:"chainId"`
	Height           string `json:"height"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex         string `json:"logIndex"`
	TransactionHash  string `json:"transactionHash"`
	BlockHash        string `json:"blockHash"`
}
type PriceSource struct {
	Kind            string         `json:"kind"`
	AmountBasis     string         `json:"amountBasis"`
	SourceEventID   *string        `json:"sourceEventId"`
	Position        *ChainPosition `json:"position"`
	ProviderID      *string        `json:"providerId"`
	ObservedAt      string         `json:"observedAt"`
	SourceReference *string        `json:"sourceReference"`
}
type Price struct {
	Numerator   string      `json:"numerator"`
	Denominator string      `json:"denominator"`
	BaseAsset   AssetRef    `json:"baseAsset"`
	QuoteAsset  AssetRef    `json:"quoteAsset"`
	Source      PriceSource `json:"source"`
}
type Position struct {
	PositionID                   string    `json:"positionId"`
	ChainID                      string    `json:"chainId"`
	Wallet                       string    `json:"wallet"`
	TokenAddress                 string    `json:"tokenAddress"`
	BalanceRaw                   string    `json:"balanceRaw"`
	KnownQuantityRaw             string    `json:"knownQuantityRaw"`
	UnknownQuantityRaw           string    `json:"unknownQuantityRaw"`
	KnownRemainingCostUsdlRaw    string    `json:"knownRemainingCostUsdlRaw"`
	BasisCoverage                Ratio     `json:"basisCoverage"`
	RemainingCostUsdlRaw         *string   `json:"remainingCostUsdlRaw"`
	RealizedPnlRaw               *string   `json:"realizedPnlRaw"`
	KnownRealizedSubtotalUsdlRaw string    `json:"knownRealizedSubtotalUsdlRaw"`
	UnrealizedPnlRaw             *string   `json:"unrealizedPnlRaw"`
	BasisQuality                 string    `json:"basisQuality"`
	RealizedBasisQuality         string    `json:"realizedBasisQuality"`
	PricingQuality               string    `json:"pricingQuality"`
	MarkPrice                    *Price    `json:"markPrice"`
	SourceWatermark              Watermark `json:"sourceWatermark"`
}
type Summary struct {
	Wallet                       string    `json:"wallet"`
	PricedSubtotalUsdlRaw        string    `json:"pricedSubtotalUsdlRaw"`
	CompleteValueUsdlRaw         *string   `json:"completeValueUsdlRaw"`
	RealizedPnlRaw               *string   `json:"realizedPnlRaw"`
	KnownRealizedSubtotalUsdlRaw string    `json:"knownRealizedSubtotalUsdlRaw"`
	BasisQuality                 string    `json:"basisQuality"`
	PricingQuality               string    `json:"pricingQuality"`
	UnpricedAssetCount           int       `json:"unpricedAssetCount"`
	PositionCount                int       `json:"positionCount"`
	SnapshotID                   string    `json:"snapshotId"`
	Watermark                    Watermark `json:"watermark"`
}

func ptr(s string) *string { return &s }

// Derive validates coverage and recomputes all nullable amounts. A stale mark is
// retained as provenance but never contributes to complete current valuation.
func validateWatermark(w Watermark) error {
	if w.ChainID != "125" || (w.Integrity != "healthy" && w.Integrity != "quarantined") || !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(w.BlockHash) {
		return ErrInvalid
	}
	if _, e := uint256(w.Height); e != nil {
		return e
	}
	r, e := uint256(w.ProjectionRevision)
	if e != nil || r.Sign() == 0 {
		return ErrInvalid
	}
	if _, e = time.Parse("2006-01-02T15:04:05.000Z", w.ObservedAt); e != nil {
		return ErrInvalid
	}
	return nil
}
func Derive(p Position) (Position, *big.Int, error) {
	if e := validateWatermark(p.SourceWatermark); e != nil {
		return p, nil, e
	}
	k, e := uint256(p.KnownQuantityRaw)
	if e != nil {
		return p, nil, e
	}
	u, e := uint256(p.UnknownQuantityRaw)
	if e != nil {
		return p, nil, e
	}
	b, e := uint256(p.BalanceRaw)
	if e != nil {
		return p, nil, e
	}
	c, e := number(p.KnownRemainingCostUsdlRaw, false, 156)
	if e != nil {
		return p, nil, e
	}
	r, e := number(p.KnownRealizedSubtotalUsdlRaw, true, 156)
	if e != nil {
		return p, nil, e
	}
	if new(big.Int).Add(k, u).Cmp(b) != 0 || (k.Sign() == 0 && c.Sign() != 0) {
		return p, nil, ErrInvalid
	}
	if p.ChainID != "125" || p.PositionID == "" || len(p.PositionID) > 160 {
		return p, nil, ErrInvalid
	}
	p.Wallet, e = Wallet(p.Wallet)
	if e != nil {
		return p, nil, e
	}
	p.TokenAddress, e = Wallet(p.TokenAddress)
	if e != nil {
		return p, nil, e
	}
	p.BasisQuality = "complete"
	if u.Sign() > 0 {
		p.BasisQuality = "partial"
		if k.Sign() == 0 {
			p.BasisQuality = "unknown"
		}
	}
	p.BasisCoverage = Ratio{k.String(), b.String()}
	if b.Sign() == 0 {
		p.BasisCoverage = Ratio{"1", "1"}
	}
	p.RemainingCostUsdlRaw = nil
	if u.Sign() == 0 {
		p.RemainingCostUsdlRaw = ptr(c.String())
	}
	p.RealizedPnlRaw = nil
	switch p.RealizedBasisQuality {
	case "complete":
		p.RealizedPnlRaw = ptr(r.String())
	case "partial", "unknown":
	default:
		return p, nil, ErrInvalid
	}
	p.UnrealizedPnlRaw = nil
	if p.PricingQuality == "unpriced" {
		if p.MarkPrice != nil {
			return p, nil, ErrInvalid
		}
		return p, nil, nil
	}
	if p.PricingQuality != "priced" && p.PricingQuality != "stale" {
		return p, nil, ErrInvalid
	}
	if p.MarkPrice == nil {
		return p, nil, ErrInvalid
	}
	m := p.MarkPrice
	if m.Source.Kind != "post_swap_spot" || m.Source.AmountBasis != "effective_reserves" || m.Source.SourceEventID == nil || *m.Source.SourceEventID == "" || m.Source.Position == nil || m.Source.Position.ChainID != "125" {
		return p, nil, ErrInvalid
	}
	cp := m.Source.Position
	if _, e := uint256(cp.Height); e != nil {
		return p, nil, e
	}
	sourceHeight, _ := uint256(cp.Height)
	snapshotHeight, _ := uint256(p.SourceWatermark.Height)
	if sourceHeight.Cmp(snapshotHeight) > 0 || (sourceHeight.Cmp(snapshotHeight) == 0 && !strings.EqualFold(cp.BlockHash, p.SourceWatermark.BlockHash)) {
		return p, nil, ErrInvalid
	}
	if _, e := uint256(cp.TransactionIndex); e != nil {
		return p, nil, e
	}
	if _, e := uint256(cp.LogIndex); e != nil {
		return p, nil, e
	}
	if !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(cp.BlockHash) || !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(cp.TransactionHash) {
		return p, nil, ErrInvalid
	}
	priceObserved, e := time.Parse("2006-01-02T15:04:05.000Z", m.Source.ObservedAt)
	snapshotObserved, _ := time.Parse("2006-01-02T15:04:05.000Z", p.SourceWatermark.ObservedAt)
	if e != nil || priceObserved.After(snapshotObserved) {
		return p, nil, ErrInvalid
	}
	n, e := number(m.Numerator, false, 512)
	if e != nil {
		return p, nil, e
	}
	d, e := number(m.Denominator, false, 512)
	if e != nil || d.Sign() == 0 {
		return p, nil, ErrInvalid
	}
	if m.BaseAsset.Decimals == nil || *m.BaseAsset.Decimals < 0 || *m.BaseAsset.Decimals > 255 {
		return p, nil, ErrInvalid
	}
	if m.QuoteAsset.Kind != "erc20" || m.QuoteAsset.ChainID == nil || *m.QuoteAsset.ChainID != "125" || m.QuoteAsset.Address == nil || !strings.EqualFold(*m.QuoteAsset.Address, "0x85FcD13735F4309833A503EE804ea32395851479") {
		return p, nil, ErrInvalid
	}
	if new(big.Int).GCD(nil, nil, n, d).Cmp(big.NewInt(1)) != 0 {
		return p, nil, ErrInvalid
	}
	if m.BaseAsset.Kind != "erc20" || m.BaseAsset.ChainID == nil || *m.BaseAsset.ChainID != "125" || m.BaseAsset.Address == nil || !strings.EqualFold(*m.BaseAsset.Address, p.TokenAddress) || m.QuoteAsset.CurrencyCode == nil || *m.QuoteAsset.CurrencyCode != "USDL" || m.QuoteAsset.Decimals == nil || *m.QuoteAsset.Decimals != 6 {
		return p, nil, ErrInvalid
	}
	if p.SourceWatermark.Integrity == "quarantined" {
		p.PricingQuality = "stale"
	}
	if p.PricingQuality == "stale" {
		return p, nil, nil
	}
	// Frozen IF_PRICE is reduced human quote units per WHOLE base token.
	// Supplied decimal metadata must originate in L25 verified asset registry.
	scaledNumerator := new(big.Int).Mul(new(big.Int).Mul(b, n), big.NewInt(1000000))
	scaledDenominator := new(big.Int).Mul(d, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*m.BaseAsset.Decimals)), nil))
	v := new(big.Int).Quo(scaledNumerator, scaledDenominator)
	if len(v.String()) > 156 {
		return p, nil, ErrInvalid
	}
	if u.Sign() == 0 {
		p.UnrealizedPnlRaw = ptr(new(big.Int).Sub(v, c).String())
	}
	return p, v, nil
}

// Aggregate is for projection-time work, never request-time whole-wallet scans.
func Aggregate(wallet, snapshot string, w Watermark, positions []Position) (Summary, error) {
	if e := validateWatermark(w); e != nil {
		return Summary{}, e
	}
	wallet, e := Wallet(wallet)
	if e != nil {
		return Summary{}, e
	}
	s := Summary{Wallet: wallet, SnapshotID: snapshot, Watermark: w, PositionCount: len(positions), BasisQuality: "complete", PricingQuality: "complete"}
	v, r := new(big.Int), new(big.Int)
	allRealized := true
	knownBasis := false
	unknownBasis := false
	seen := map[string]bool{}
	blocks := map[string]string{w.Height: strings.ToLower(w.BlockHash)}
	for _, input := range positions {
		p, value, e := Derive(input)
		if e != nil {
			return s, e
		}
		positionHeight, _ := uint256(p.SourceWatermark.Height)
		snapshotHeight, _ := uint256(w.Height)
		positionRevision, _ := uint256(p.SourceWatermark.ProjectionRevision)
		snapshotRevision, _ := uint256(w.ProjectionRevision)
		if p.Wallet != wallet || p.SourceWatermark.ChainID != w.ChainID || positionRevision.Cmp(snapshotRevision) > 0 || positionHeight.Cmp(snapshotHeight) > 0 || (positionHeight.Cmp(snapshotHeight) == 0 && !strings.EqualFold(p.SourceWatermark.BlockHash, w.BlockHash)) || (w.Integrity == "quarantined" && p.SourceWatermark.Integrity != "quarantined") || seen[p.PositionID] {
			return s, ErrInvalid
		}
		block := strings.ToLower(p.SourceWatermark.BlockHash)
		if previous, exists := blocks[p.SourceWatermark.Height]; exists && previous != block {
			return s, ErrConflict
		}
		blocks[p.SourceWatermark.Height] = block
		if p.MarkPrice != nil {
			proof := p.MarkPrice.Source.Position
			block := strings.ToLower(proof.BlockHash)
			if previous, exists := blocks[proof.Height]; exists && previous != block {
				return s, ErrConflict
			}
			blocks[proof.Height] = block
		}
		seen[p.PositionID] = true
		if value == nil {
			s.UnpricedAssetCount++
		} else {
			v.Add(v, value)
		}
		nr, _ := number(p.KnownRealizedSubtotalUsdlRaw, true, 156)
		r.Add(r, nr)
		allRealized = allRealized && p.RealizedPnlRaw != nil
		knownBasis = knownBasis || p.BasisQuality != "unknown"
		unknownBasis = unknownBasis || p.BasisQuality != "complete" || p.RealizedBasisQuality != "complete"
	}
	if unknownBasis {
		s.BasisQuality = "unknown"
		if knownBasis {
			s.BasisQuality = "partial"
		}
	}
	if len(v.String()) > 156 || len(strings.TrimPrefix(r.String(), "-")) > 156 {
		return s, ErrInvalid
	}
	s.PricedSubtotalUsdlRaw = v.String()
	s.KnownRealizedSubtotalUsdlRaw = r.String()
	if s.UnpricedAssetCount == 0 {
		s.CompleteValueUsdlRaw = ptr(v.String())
	} else {
		s.PricingQuality = "partial"
		if s.UnpricedAssetCount == s.PositionCount {
			s.PricingQuality = "unpriced"
		}
	}
	if allRealized {
		s.RealizedPnlRaw = ptr(r.String())
	}
	return s, nil
}

// EquityChange is an absolute flow-adjusted result, not TWR or IRR. Those methods
// require separately qualified subperiod valuations and dated cash flows.
type EquityResult struct {
	ProfitRaw *string `json:"profitRaw"`
	Return    Ratio   `json:"return"`
	Method    string  `json:"method"`
	Reason    string  `json:"reason,omitempty"`
}

func EquityChange(open, close, deposits, withdrawals, transferIn, transferOut string, complete bool) (EquityResult, error) {
	a := make([]*big.Int, 6)
	for i, s := range []string{open, close, deposits, withdrawals, transferIn, transferOut} {
		n, e := number(s, false, 156)
		if e != nil {
			return EquityResult{}, e
		}
		a[i] = n
	}
	if !complete {
		return EquityResult{Method: "unavailable", Reason: "incomplete_cash_flows"}, nil
	}
	profit := new(big.Int).Sub(a[1], a[0])
	profit.Sub(profit, a[2])
	profit.Add(profit, a[3])
	profit.Sub(profit, a[4])
	profit.Add(profit, a[5])
	return EquityResult{ProfitRaw: ptr(profit.String()), Method: "flow_adjusted_absolute_v1", Reason: "percentage_requires_dated_subperiod_inputs"}, nil
}
func RequireOwner(wallet, verifiedWallet string) error {
	w, e := Wallet(wallet)
	if e != nil {
		return e
	}
	v, e := Wallet(verifiedWallet)
	if e != nil || w != v {
		return ErrUnauthorized
	}
	return nil
}
func Limit(n int) (int, error) {
	if n == 0 {
		return 50, nil
	}
	if n < 1 || n > 100 {
		return 0, fmt.Errorf("%w: page size", ErrInvalid)
	}
	return n, nil
}
