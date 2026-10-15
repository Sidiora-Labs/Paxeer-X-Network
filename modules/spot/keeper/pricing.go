package keeper

import (
	"errors"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

var (
	ErrInvalidPriceOrDecimals = errors.New("InvalidPriceOrDecimals()")
	ErrQuoteOverflow          = errors.New("QuoteOverflow()")
)

func (k *Keeper) IndexPrice(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	price, _, stale, err := k.TokenPrice(ctx, token)
	if err != nil {
		return sdk.ZeroInt(), err
	}
	if stale {
		return sdk.ZeroInt(), types.ErrStalePrice
	}
	return price, nil
}

func (k *Keeper) TokenPrice(ctx sdk.Context, token common.Address) (sdk.Int, uint64, bool, error) {
	for _, registered := range k.GetParams(ctx).Tokens {
		if registered.Address != token {
			continue
		}
		if registered.IsStablecoin {
			now := ctx.BlockTime().Unix()
			if now < 0 {
				return sdk.ZeroInt(), 0, true, types.ErrStalePrice
			}
			return sdk.NewIntWithDecimal(1, 18), uint64(now), false, nil
		}
		if registered.MarketID == 0 {
			return sdk.ZeroInt(), 0, true, types.ErrInvalidOracle
		}
		price, err := k.pricefeedKeeper.GetPrice(ctx, registered.MarketID)
		if err != nil {
			return sdk.ZeroInt(), 0, true, err
		}
		return price.Index, price.TimestampMs / 1000, !price.Fresh, nil
	}
	return sdk.ZeroInt(), 0, true, types.ErrInvalidVault
}

func Convert(amount, priceIn, priceOut sdk.Int, decIn, decOut uint32, up bool) (sdk.Int, error) {
	if !pricingUint256(amount) || !pricingPositiveUint256(priceIn) || !pricingPositiveUint256(priceOut) ||
		decIn > types.MaxTokenDecimals || decOut > types.MaxTokenDecimals {
		return sdk.ZeroInt(), ErrInvalidPriceOrDecimals
	}
	numerator := new(big.Int).Mul(amount.BigInt(), priceIn.BigInt())
	denominator := priceOut.BigInt()
	if decOut >= decIn {
		numerator.Mul(numerator, pricingScale(decOut-decIn))
	} else {
		denominator.Mul(denominator, pricingScale(decIn-decOut))
	}
	return pricingQuotient(numerator, denominator, up)
}

func VolumeUSD(amount, price sdk.Int, dec uint32) (sdk.Int, error) {
	if !pricingUint256(amount) || !pricingPositiveUint256(price) || dec > types.MaxTokenDecimals {
		return sdk.ZeroInt(), ErrInvalidPriceOrDecimals
	}
	return pricingQuotient(new(big.Int).Mul(amount.BigInt(), price.BigInt()), pricingScale(dec), false)
}

func pricingUint256(value sdk.Int) bool {
	return !value.IsNil() && !value.IsNegative() && value.BigInt().BitLen() <= 256
}

func pricingPositiveUint256(value sdk.Int) bool {
	return pricingUint256(value) && value.IsPositive()
}

func pricingScale(dec uint32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(dec)), nil)
}

func pricingQuotient(numerator, denominator *big.Int, up bool) (sdk.Int, error) {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if up && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if quotient.BitLen() > 256 {
		return sdk.ZeroInt(), ErrQuoteOverflow
	}
	return sdk.NewIntFromBigInt(quotient), nil
}
