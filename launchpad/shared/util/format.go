// Package util holds dependency-free, money-exact helpers shared across
// services. All decimal/price math is done with math/big — never float
// (invariant i1). Ported from shared/src/util.
package util

import (
	"fmt"
	"math/big"
	"strings"
)

// weiPerEther is 10^18, the fixed-point scale of raw on-chain price/volume ints.
var weiPerEther = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// FormatPrice formats a raw 18-decimal integer string into a decimal string
// with 8 truncated fractional digits, byte-identical to the TS shared
// formatPrice (shared/src/util/format.ts). raw must be a non-negative integer
// string (the on-chain price domain).
func FormatPrice(raw string) (string, error) {
	return formatFixed(raw, 8)
}

// FormatVolume formats a raw 18-decimal integer string into a decimal string
// with 2 truncated fractional digits, byte-identical to the TS shared
// formatVolume.
func FormatVolume(raw string) (string, error) {
	return formatFixed(raw, 2)
}

func formatFixed(raw string, fracDigits int) (string, error) {
	val, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok {
		return "", fmt.Errorf("util: %q is not a valid integer string", raw)
	}
	if val.Sign() < 0 {
		return "", fmt.Errorf("util: %q must be non-negative", raw)
	}

	intPart := new(big.Int)
	frac := new(big.Int)
	intPart.QuoRem(val, weiPerEther, frac)

	// padStart(18, '0') then slice(0, fracDigits) — TS truncates, never rounds.
	fracStr := frac.String()
	if len(fracStr) < 18 {
		fracStr = strings.Repeat("0", 18-len(fracStr)) + fracStr
	}
	fracStr = fracStr[:fracDigits]

	return intPart.String() + "." + fracStr, nil
}
