// Package db holds the shared Postgres client (pgxpool) plus money-exact bigint
// helpers and cross-schema table references. All money math uses math/big on
// decimal strings — never float (invariant i1) — and is byte-identical to the TS
// shared db/utils (shared/src/db/utils.ts).
package db

import (
	"fmt"
	"math/big"
	"time"
)

// TotalSupplyRaw is 1 billion tokens at 6 decimals = 1e15 raw.
const TotalSupplyRaw = "1000000000000000"

// WAD is 1e18 fixed-point precision.
const WAD = "1000000000000000000"

func parse(name, s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("db: %s %q is not a valid integer string", name, s)
	}
	return v, nil
}

// BigintMax returns the larger of two uint256 decimal strings.
func BigintMax(a, b string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	if av.Cmp(bv) >= 0 {
		return a, nil
	}
	return b, nil
}

// BigintMin returns the smaller of two uint256 decimal strings.
func BigintMin(a, b string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	if av.Cmp(bv) <= 0 {
		return a, nil
	}
	return b, nil
}

// BigintAdd returns a + b as a decimal string.
func BigintAdd(a, b string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	return new(big.Int).Add(av, bv).String(), nil
}

// BigintSub returns a - b as a decimal string, clamped at 0 (parity with TS).
func BigintSub(a, b string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	r := new(big.Int).Sub(av, bv)
	if r.Sign() < 0 {
		return "0", nil
	}
	return r.String(), nil
}

// BigintMulDiv returns (a * b) / divisor using integer (truncating) division.
func BigintMulDiv(a, b, divisor string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	dv, err := parse("divisor", divisor)
	if err != nil {
		return "", err
	}
	if dv.Sign() == 0 {
		return "", fmt.Errorf("db: BigintMulDiv divide by zero")
	}
	prod := new(big.Int).Mul(av, bv)
	return new(big.Int).Quo(prod, dv).String(), nil
}

// ComputeMarketCap computes marketCap = priceWad * TotalSupplyRaw / WAD,
// returning USDL raw (6 decimals). Empty or "0" price yields "0" (parity).
func ComputeMarketCap(priceWad string) (string, error) {
	if priceWad == "" || priceWad == "0" {
		return "0", nil
	}
	return BigintMulDiv(priceWad, TotalSupplyRaw, WAD)
}

// NowSeconds returns the current unix time in seconds.
func NowSeconds() int64 {
	return time.Now().Unix()
}

// Uint256Max is the inclusive raw token storage ceiling, independent of decimals.
const Uint256Max = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

// ValidateRawUint256 accepts canonical decimal strings only. Leading zeros,
// signs, whitespace, fractional and exponent forms are rejected before SQL.
func ValidateRawUint256(raw string) error {
	if len(raw) == 0 || len(raw) > len(Uint256Max) || (len(raw) > 1 && raw[0] == '0') {
		return fmt.Errorf("db: invalid raw uint256")
	}
	for i := range raw {
		if raw[i] < '0' || raw[i] > '9' {
			return fmt.Errorf("db: invalid raw uint256")
		}
	}
	if len(raw) == len(Uint256Max) && raw > Uint256Max {
		return fmt.Errorf("db: uint256 overflow")
	}
	return nil
}
