// Package projectionstats implements phase-one stats primitives. Inputs are
// decoded, qualified receipts; these types do not manufacture receipt evidence.
package projectionstats

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("invalid stats input")
var ErrIntegrity = errors.New("conflicting canonical event")
var digits = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var hash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

func Uint(s string) (*big.Int, error) {
	if len(s) > 78 || !digits.MatchString(s) {
		return nil, ErrInvalid
	}
	n, _ := new(big.Int).SetString(s, 10)
	if n.BitLen() > 256 {
		return nil, ErrInvalid
	}
	return n, nil
}

type Position struct {
	ChainID          string `json:"chainId"`
	Height           string `json:"height"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex         string `json:"logIndex"`
	TransactionHash  string `json:"transactionHash"`
	BlockHash        string `json:"blockHash"`
}

func (p Position) Validate() error {
	if p.ChainID != "125" || !hash.MatchString(p.TransactionHash) || !hash.MatchString(p.BlockHash) {
		return ErrInvalid
	}
	for _, s := range []string{p.Height, p.TransactionIndex, p.LogIndex} {
		if _, e := Uint(s); e != nil {
			return e
		}
	}
	return nil
}
func (p Position) Compare(q Position) int {
	for i, s := range []string{p.Height, p.TransactionIndex, p.LogIndex} {
		a, _ := new(big.Int).SetString(s, 10)
		b, _ := new(big.Int).SetString([]string{q.Height, q.TransactionIndex, q.LogIndex}[i], 10)
		if a == nil || b == nil {
			panic("Compare requires validated positions")
		}
		if c := a.Cmp(b); c != 0 {
			return c
		}
	}
	return 0
}
func (p Position) Identity() string { return p.ChainID + ":" + p.TransactionHash + ":" + p.LogIndex }

type Trade struct {
	EventID                string   `json:"eventId"`
	PoolAddress            string   `json:"poolAddress"`
	Sender                 string   `json:"sender"`
	Recipient              *string  `json:"recipient"`
	Side                   string   `json:"side"`
	TokenAmountRaw         string   `json:"tokenAmountRaw"`
	USDLAmountRaw          string   `json:"usdlAmountRaw"`
	Position               Position `json:"position"`
	FeeRaw                 string   `json:"feeRaw"`
	FeeAssetAddress        string   `json:"feeAssetAddress"`
	FeeAssetDecimals       int      `json:"feeAssetDecimals"`
	FeeDisposition         string   `json:"feeDisposition"`
	PaidInputRaw           *string  `json:"paidInputRaw"`
	PricedInputRaw         *string  `json:"pricedInputRaw"`
	AMMInputAfterFeeRaw    *string  `json:"ammInputAfterFeeRaw"`
	InputDerivationQuality string   `json:"inputDerivationQuality"`
	RecipientAttribution   string   `json:"recipientAttribution"`
	RecipientEvidenceRefs  []string `json:"recipientEvidenceRefs"`
	EmittingContract       string   `json:"emittingContract"`
	CodeEpoch              string   `json:"codeEpoch"`
}

type PoolAssets struct {
	USDL, Token   string
	TokenDecimals int
}
type Accounting struct {
	TradedUSDLRaw, InputReserveIncrementRaw string
	FeeGroup                                FeeGroup
}
type FeeGroup struct {
	ChainID, AssetAddress string
	AssetDecimals         int
	FeeType, Disposition  string
}

// Account preserves paidInput, uses pricedInput only when proven, and never
// subtracts a token-denominated sell fee from USDL output.
func Account(t Trade, assets PoolAssets, feeBPS int) (Accounting, error) {
	if feeBPS < 0 || feeBPS > 10000 || len(t.RecipientEvidenceRefs) > 8 {
		return Accounting{}, ErrInvalid
	}
	bps := uint16(feeBPS)
	return accountAmounts(t, assets, &bps, false)
}

func accountAmounts(t Trade, assets PoolAssets, feeBPS *uint16, qualified bool) (Accounting, error) {
	bad := func() (Accounting, error) { return Accounting{}, ErrInvalid }
	if t.Position.Validate() != nil || t.EventID != t.Position.Identity() || !address.MatchString(t.PoolAddress) || !address.MatchString(t.Sender) || (!qualified && t.EmittingContract != t.PoolAddress) || len(t.CodeEpoch) == 0 || len(t.CodeEpoch) > 64 || (feeBPS != nil && *feeBPS > 10000) || !address.MatchString(assets.USDL) || !address.MatchString(assets.Token) || assets.USDL == assets.Token || assets.TokenDecimals < 0 || assets.TokenDecimals > 255 {
		return bad()
	}
	if t.Recipient == nil {
		if t.RecipientAttribution != "unavailable" || len(t.RecipientEvidenceRefs) != 0 {
			return bad()
		}
	} else {
		if !address.MatchString(*t.Recipient) || (t.RecipientAttribution != "exact_receipt" && t.RecipientAttribution != "qualified_calldata") || len(t.RecipientEvidenceRefs) == 0 {
			return bad()
		}
	}
	if t.RecipientEvidenceRefs == nil || len(t.RecipientEvidenceRefs) > 64 {
		return bad()
	}
	for _, r := range t.RecipientEvidenceRefs {
		if len(r) == 0 || len(r) > 256 {
			return bad()
		}
	}
	if t.PaidInputRaw == nil {
		return bad()
	}
	paid, e := Uint(*t.PaidInputRaw)
	if e != nil {
		return bad()
	}
	fee, e := Uint(t.FeeRaw)
	if e != nil {
		return bad()
	}
	for _, s := range []string{t.TokenAmountRaw, t.USDLAmountRaw} {
		if _, e := Uint(s); e != nil {
			return bad()
		}
	}
	expectedAsset, expectedDecimals, disposition := assets.Token, assets.TokenDecimals, "retained_token_reserve"
	input := t.TokenAmountRaw
	reserve := new(big.Int).Set(paid)
	if t.Side == "buy" {
		expectedAsset, expectedDecimals, disposition = assets.USDL, 6, "distributed_usdl"
		input = t.USDLAmountRaw
		reserve.Sub(reserve, fee)
	} else if t.Side != "sell" {
		return bad()
	}
	if input != *t.PaidInputRaw || t.FeeAssetAddress != expectedAsset || t.FeeAssetDecimals != expectedDecimals || t.FeeDisposition != disposition || reserve.Sign() < 0 {
		return bad()
	}
	if t.PricedInputRaw == nil {
		if t.AMMInputAfterFeeRaw != nil || (t.InputDerivationQuality != "legacy_unavailable" && t.InputDerivationQuality != "receipt_incomplete" && t.InputDerivationQuality != "abi_unqualified") {
			return bad()
		}
	} else {
		if t.InputDerivationQuality != "exact" || t.AMMInputAfterFeeRaw == nil {
			return bad()
		}
		priced, e := Uint(*t.PricedInputRaw)
		if e != nil {
			return bad()
		}
		var expected *big.Int
		if feeBPS != nil {
			expected = new(big.Int).Mul(priced, big.NewInt(int64(*feeBPS)))
			expected.Quo(expected, big.NewInt(10000))
		}
		net := new(big.Int).Sub(priced, fee)
		if (expected != nil && expected.Cmp(fee) != 0) || net.Sign() < 0 || net.String() != *t.AMMInputAfterFeeRaw {
			return bad()
		}
	}
	return Accounting{t.USDLAmountRaw, reserve.String(), FeeGroup{"125", expectedAsset, expectedDecimals, "trading_fee", disposition}}, nil
}
func Digest(t Trade) string       { b, _ := json.Marshal(t); return digestBytes(b) }
func digestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Minute(timestamp int64) (int64, error) {
	if timestamp < 0 {
		return 0, ErrInvalid
	}
	return timestamp / 60 * 60, nil
}
func InWindow(timestamp, observedAt, seconds int64) bool {
	return seconds > 0 && observedAt >= 0 && timestamp >= 0 && timestamp <= observedAt && timestamp > observedAt-seconds
}
func ValidateTime(timestamp, watermarkTime, tolerance int64) error {
	if timestamp < 0 || watermarkTime < 0 || tolerance < 0 {
		return ErrInvalid
	}
	if timestamp > watermarkTime && timestamp-watermarkTime > tolerance {
		return fmt.Errorf("%w: future chain timestamp", ErrIntegrity)
	}
	return nil
}

// NormalizeAddress is explicit at the receipt adapter boundary. Account requires
// canonical lowercase values so payload digests cannot vary by hex casing.
func NormalizeAddress(s string) (string, error) {
	s = strings.ToLower(s)
	if !address.MatchString(s) {
		return "", ErrInvalid
	}
	return s, nil
}

// EmittedDigest excludes version-dependent enrichment (priced input, recipient
// attribution, ABI/code provenance). Those remain protected by the full inbox
// digest per consumer version. New projections may improve enrichment while
// immutable emitted payment/output/fee facts cannot change at a chain position.
func EmittedDigest(t Trade) string {
	b, _ := json.Marshal(struct {
		Position                                                         Position
		PoolAddress, Sender, Side, TokenAmountRaw, USDLAmountRaw, FeeRaw string
		PaidInputRaw                                                     *string
	}{t.Position, t.PoolAddress, t.Sender, t.Side, t.TokenAmountRaw, t.USDLAmountRaw, t.FeeRaw, t.PaidInputRaw})
	return digestBytes(b)
}
