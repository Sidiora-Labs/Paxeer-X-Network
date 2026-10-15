package types

import (
	"fmt"
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// maxUint256Bits is the bit length bound of every fixed-point amount.
const maxUint256Bits = 256

// minInt256 and maxInt256 bound every signed fixed-point amount.
var (
	maxInt256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), maxUint256Bits-1), big.NewInt(1))
	minInt256 = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), maxUint256Bits-1))
)

// usdPerUsdlUnit is the number of 18-decimal USD units in one USDL unit.
var usdPerUsdlUnit = new(big.Int).Exp(big.NewInt(10), big.NewInt(USDDecimals-CollateralDecimals), nil)

// UsdToUsdlDown converts an 18-decimal USD amount to USDL units rounding down,
// the conversion of credits. It returns the USDL amount and the residue in USD
// that the rounding keeps out of the credit, for residueUsd.
func UsdToUsdlDown(usd sdk.Int) (usdl sdk.Int, residueUsd sdk.Int, err error) {
	amount, err := usdOperand(usd)
	if err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	quotient, remainder := new(big.Int).QuoRem(amount, usdPerUsdlUnit, new(big.Int))
	return sdk.NewIntFromBigInt(quotient), sdk.NewIntFromBigInt(remainder), nil
}

// UsdToUsdlUp converts an 18-decimal USD amount to USDL units rounding up,
// the conversion of debits. It returns the USDL amount and the residue in USD
// that the rounding collects above the debit, for residueUsd.
func UsdToUsdlUp(usd sdk.Int) (usdl sdk.Int, residueUsd sdk.Int, err error) {
	amount, err := usdOperand(usd)
	if err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	quotient, remainder := new(big.Int).QuoRem(amount, usdPerUsdlUnit, new(big.Int))
	if remainder.Sign() == 0 {
		return sdk.NewIntFromBigInt(quotient), sdk.ZeroInt(), nil
	}
	quotient.Add(quotient, big.NewInt(1))
	return sdk.NewIntFromBigInt(quotient), sdk.NewIntFromBigInt(remainder.Sub(usdPerUsdlUnit, remainder)), nil
}

func usdOperand(usd sdk.Int) (*big.Int, error) {
	if usd.IsNil() {
		return nil, invalid("USD amount is nil")
	}
	amount := usd.BigInt()
	if amount.Sign() < 0 {
		return nil, invalid("USD amount %s is negative", amount)
	}
	if amount.BitLen() > maxUint256Bits {
		return nil, fmt.Errorf("%w: USD amount %s above 2^256-1", ErrOverflow, amount)
	}
	return amount, nil
}

// MulDivDown returns floor(a*b/denominator) over unsigned 256-bit operands with
// a full-width product, failing with ErrOverflow when the result is above
// 2^256-1.
func MulDivDown(a, b, denominator sdk.Int) (sdk.Int, error) {
	return mulDivUnsigned(a, b, denominator, false)
}

// MulDivUp returns ceil(a*b/denominator) over unsigned 256-bit operands with a
// full-width product, failing with ErrOverflow when the result is above
// 2^256-1.
func MulDivUp(a, b, denominator sdk.Int) (sdk.Int, error) {
	return mulDivUnsigned(a, b, denominator, true)
}

// MulDivDownSigned returns floor(a*b/denominator), rounding toward negative
// infinity, over signed 256-bit operands with a full-width product, failing
// with ErrOverflow when the result is outside [-2^255, 2^255-1].
func MulDivDownSigned(a, b, denominator sdk.Int) (sdk.Int, error) {
	return mulDivSigned(a, b, denominator, false)
}

// MulDivUpSigned returns ceil(a*b/denominator), rounding toward positive
// infinity, over signed 256-bit operands with a full-width product, failing
// with ErrOverflow when the result is outside [-2^255, 2^255-1].
func MulDivUpSigned(a, b, denominator sdk.Int) (sdk.Int, error) {
	return mulDivSigned(a, b, denominator, true)
}

func mulDivUnsigned(a, b, denominator sdk.Int, up bool) (sdk.Int, error) {
	operands := make([]*big.Int, 0, 3)
	for _, value := range []sdk.Int{a, b, denominator} {
		operand, err := unsignedOperand(value)
		if err != nil {
			return sdk.Int{}, err
		}
		operands = append(operands, operand)
	}
	out, err := mulDiv(operands[0], operands[1], operands[2], up)
	if err != nil {
		return sdk.Int{}, err
	}
	if out.BitLen() > maxUint256Bits {
		return sdk.Int{}, fmt.Errorf("%w: result %s above 2^256-1", ErrOverflow, out)
	}
	return sdk.NewIntFromBigInt(out), nil
}

func mulDivSigned(a, b, denominator sdk.Int, up bool) (sdk.Int, error) {
	operands := make([]*big.Int, 0, 3)
	for _, value := range []sdk.Int{a, b, denominator} {
		operand, err := signedOperand(value)
		if err != nil {
			return sdk.Int{}, err
		}
		operands = append(operands, operand)
	}
	out, err := mulDiv(operands[0], operands[1], operands[2], up)
	if err != nil {
		return sdk.Int{}, err
	}
	if out.Cmp(minInt256) < 0 || out.Cmp(maxInt256) > 0 {
		return sdk.Int{}, fmt.Errorf("%w: result %s outside the int256 range", ErrOverflow, out)
	}
	return sdk.NewIntFromBigInt(out), nil
}

// mulDiv divides the exact product a*b by denominator with floor or ceiling
// rounding, whatever the signs.
func mulDiv(a, b, denominator *big.Int, up bool) (*big.Int, error) {
	if denominator.Sign() == 0 {
		return nil, invalid("division by zero")
	}
	product := new(big.Int).Mul(a, b)
	quotient, modulus := new(big.Int).DivMod(product, denominator, new(big.Int))
	if denominator.Sign() < 0 && modulus.Sign() != 0 {
		// Euclidean division rounds toward positive infinity for a negative
		// denominator; step down to the floor.
		quotient.Sub(quotient, big.NewInt(1))
	}
	if up && modulus.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient, nil
}

func unsignedOperand(value sdk.Int) (*big.Int, error) {
	if value.IsNil() {
		return nil, invalid("operand is nil")
	}
	operand := value.BigInt()
	if operand.Sign() < 0 {
		return nil, invalid("operand %s is negative", operand)
	}
	if operand.BitLen() > maxUint256Bits {
		return nil, fmt.Errorf("%w: operand %s above 2^256-1", ErrOverflow, operand)
	}
	return operand, nil
}

func signedOperand(value sdk.Int) (*big.Int, error) {
	if value.IsNil() {
		return nil, invalid("operand is nil")
	}
	operand := value.BigInt()
	if operand.Cmp(minInt256) < 0 || operand.Cmp(maxInt256) > 0 {
		return nil, fmt.Errorf("%w: operand %s outside the int256 range", ErrOverflow, operand)
	}
	return operand, nil
}
