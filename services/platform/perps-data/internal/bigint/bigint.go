// Package bigint holds the money-exact integer helpers of the perps data
// services. All arithmetic uses math/big on canonical decimal strings, never
// float. Origin: Launch-x shared/db (bigint.go), with ValidateRawInt256 added
// for the signed event columns.
package bigint

import (
	"errors"
	"fmt"
	"math/big"
)

// Uint256Max is the inclusive raw unsigned storage ceiling.
const Uint256Max = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

// Int256Max and Int256Min bound the raw signed storage range.
const (
	Int256Max = "57896044618658097711785492504343953926634992332820282019728792003956564819967"
	Int256Min = "-57896044618658097711785492504343953926634992332820282019728792003956564819968"
)

var (
	// ErrInvalidRaw reports a value that is not a canonical decimal integer.
	ErrInvalidRaw = errors.New("bigint: invalid raw integer")
	// ErrOverflow reports a value or result outside its 256-bit range.
	ErrOverflow = errors.New("bigint: 256-bit overflow")
	// ErrDivideByZero reports a zero MulDiv divisor.
	ErrDivideByZero = errors.New("bigint: divide by zero")
)

var uint256Max, _ = new(big.Int).SetString(Uint256Max, 10)

func parse(name, s string) (*big.Int, error) {
	if err := ValidateRaw(s); err != nil {
		return nil, fmt.Errorf("%s %q: %w", name, s, err)
	}
	v, _ := new(big.Int).SetString(s, 10)
	return v, nil
}

func result(v *big.Int) (string, error) {
	if v.Cmp(uint256Max) > 0 {
		return "", ErrOverflow
	}
	return v.String(), nil
}

// Add returns a + b as a decimal string, refusing a sum above Uint256Max.
func Add(a, b string) (string, error) {
	av, err := parse("a", a)
	if err != nil {
		return "", err
	}
	bv, err := parse("b", b)
	if err != nil {
		return "", err
	}
	return result(new(big.Int).Add(av, bv))
}

// Sub returns a - b as a decimal string, clamped at 0 (parity with the origin).
func Sub(a, b string) (string, error) {
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

// MulDiv returns (a * b) / divisor with flooring integer division, refusing a
// quotient above Uint256Max.
func MulDiv(a, b, divisor string) (string, error) {
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
		return "", ErrDivideByZero
	}
	return result(new(big.Int).Quo(new(big.Int).Mul(av, bv), dv))
}

// ValidateRaw accepts canonical unsigned decimal strings up to Uint256Max.
// Leading zeros, signs, whitespace, fractional and exponent forms are refused.
func ValidateRaw(raw string) error {
	return validateDigits(raw, Uint256Max)
}

// ValidateRawInt256 accepts canonical signed decimal strings in
// [Int256Min, Int256Max]: an optional leading minus followed by the digits
// ValidateRaw accepts. Negative zero is refused.
func ValidateRawInt256(raw string) error {
	if len(raw) > 0 && raw[0] == '-' {
		if raw == "-0" {
			return ErrInvalidRaw
		}
		return validateDigits(raw[1:], Int256Min[1:])
	}
	return validateDigits(raw, Int256Max)
}

func validateDigits(raw, limit string) error {
	if len(raw) == 0 || (len(raw) > 1 && raw[0] == '0') {
		return ErrInvalidRaw
	}
	for i := range raw {
		if raw[i] < '0' || raw[i] > '9' {
			return ErrInvalidRaw
		}
	}
	if len(raw) > len(limit) || (len(raw) == len(limit) && raw > limit) {
		return ErrOverflow
	}
	return nil
}
