package search

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"math/big"
	"strings"
)

// Version1 ranges are exact inclusive-minimum/exclusive-maximum comparisons.
// Unknown or stale metrics never satisfy a numeric range. Liquidity is total
// pool depth denominated in raw USDL (6 decimals); volume uses raw USDL too.
type RawRange struct {
	MinInclusive *string `json:"minInclusive,omitempty"`
	MaxExclusive *string `json:"maxExclusive,omitempty"`
}
type AgeRange struct {
	MinInclusiveSeconds *uint64 `json:"minInclusiveSeconds,omitempty"`
	MaxExclusiveSeconds *uint64 `json:"maxExclusiveSeconds,omitempty"`
}
type VolumeFilter struct {
	Window string   `json:"window"`
	Range  RawRange `json:"range"`
}
type LiquidityFilter struct {
	Asset    string   `json:"asset"`
	Decimals int      `json:"decimals"`
	Depth    string   `json:"depth"`
	Range    RawRange `json:"range"`
}

func rawUInt(s string) (*big.Int, bool) {
	if len(s) < 1 || len(s) > 78 || len(s) > 1 && s[0] == '0' {
		return nil, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	return n, ok && n.BitLen() <= 256
}
func validateRange(r RawRange) (int, error) {
	n := 0
	var lo, hi *big.Int
	var ok bool
	if r.MinInclusive != nil {
		n++
		lo, ok = rawUInt(*r.MinInclusive)
		if !ok {
			return 0, ErrInvalid
		}
	}
	if r.MaxExclusive != nil {
		n++
		hi, ok = rawUInt(*r.MaxExclusive)
		if !ok {
			return 0, ErrInvalid
		}
	}
	if n == 0 || lo != nil && hi != nil && lo.Cmp(hi) >= 0 {
		return 0, ErrInvalid
	}
	return n, nil
}
func (f Filter) Validate() error {
	if f.Version != 0 && f.Version != Version {
		return ErrInvalid
	}
	n := 0
	if f.PoolStatus != "" {
		n++
		if f.PoolStatus != "active" && f.PoolStatus != "paused" {
			return ErrInvalid
		}
	}
	if a := f.CreationAge; a != nil {
		if a.MinInclusiveSeconds == nil && a.MaxExclusiveSeconds == nil {
			return ErrInvalid
		}
		if a.MinInclusiveSeconds != nil {
			n++
		}
		if a.MaxExclusiveSeconds != nil {
			n++
		}
		if a.MinInclusiveSeconds != nil && a.MaxExclusiveSeconds != nil && *a.MinInclusiveSeconds >= *a.MaxExclusiveSeconds {
			return ErrInvalid
		}
	}
	if f.VerifiedQuoteAsset != nil {
		n++
	}
	if v := f.VolumeWindow; v != nil {
		if v.Window != "1h" && v.Window != "24h" && v.Window != "7d" {
			return ErrInvalid
		}
		k, e := validateRange(v.Range)
		if e != nil {
			return e
		}
		n += k
	}
	if l := f.LiquidityRange; l != nil {
		if l.Asset != "USDL" || l.Decimals != 6 || l.Depth != "total" {
			return ErrInvalid
		}
		k, e := validateRange(l.Range)
		if e != nil {
			return e
		}
		n += k
	}
	if f.Creator != "" {
		n++
		q, e := Normalize(f.Creator)
		if e != nil || q.Kind != "exact_address" || common.HexToAddress(f.Creator) == (common.Address{}) {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, flag := range f.RiskDisclosureFlags {
		if flag != "unverified" && flag != "paused" && flag != "concentrated_holders" && flag != "restricted_metadata" {
			return ErrInvalid
		}
		if seen[flag] {
			return ErrInvalid
		}
		seen[flag] = true
		n++
	}
	if n > 10 {
		return fmt.Errorf("%w: maximum ten filter predicates", ErrInvalid)
	}
	return nil
}
func (f Filter) requiresProjection() bool {
	return f.CreationAge != nil || f.VerifiedQuoteAsset != nil || f.VolumeWindow != nil || f.LiquidityRange != nil || f.Creator != "" || len(f.RiskDisclosureFlags) > 0
}
func canonicalTicker(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return -1
		}
		return r
	}, s)
	return strings.ToUpper(strings.TrimSpace(cases.Fold().String(norm.NFKC.String(s))))
}

func (f Filter) Public() (Filter, error) {
	if err := f.Validate(); err != nil {
		return f, err
	}
	f.Version = Version
	if f.requiresProjection() {
		return f, ErrUnsupported
	}
	return f, nil
}
