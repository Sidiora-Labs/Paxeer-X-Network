package types

import (
	"fmt"
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// maxUint256Bits is the bit length bound of every fixed-point amount.
const maxUint256Bits = 256

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
