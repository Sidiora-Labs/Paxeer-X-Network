package keeper

import (
	"errors"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// Volume thresholds of the fee tiers in USD at 18 decimals (PECOR
// TIER1_THRESHOLD and TIER2_THRESHOLD).
var (
	quoteTier1Volume = new(big.Int).Mul(big.NewInt(10_000), big.NewInt(1e18))
	quoteTier2Volume = new(big.Int).Mul(big.NewInt(100_000), big.NewInt(1e18))
)

// QuoteResult is the PECORQuoter tuple in ABI order: the 14 fields of
// IMetaAGQuoter.QuoteResult followed by the nine extras of
// contracts/spot.md#pecorquoter-interface.
type QuoteResult struct {
	AmountIn             *big.Int
	AmountOut            *big.Int
	GrossAmountOut       *big.Int
	ExecutionPrice       *big.Int
	SpotPriceIn          *big.Int
	SpotPriceOut         *big.Int
	FeeAmount            *big.Int
	FeeBps               *big.Int
	SufficientLiquidity  bool
	AvailableLiquidity   *big.Int
	PriceTimestampIn     *big.Int
	PriceTimestampOut    *big.Int
	PriceStaleIn         bool
	PriceStaleOut        bool
	UtilizationCapBps    *big.Int
	TradeUtilizationBps  *big.Int
	CappedByUtilization  bool
	TwapOut              *big.Int
	TwapDeviationBps     *big.Int
	EffectiveFeeBps      *big.Int
	Tier                 *big.Int
	PriceImpactBps       *big.Int
	PriceImpactDeduction *big.Int
}

func newQuoteResult() QuoteResult {
	return QuoteResult{
		AmountIn:             new(big.Int),
		AmountOut:            new(big.Int),
		GrossAmountOut:       new(big.Int),
		ExecutionPrice:       new(big.Int),
		SpotPriceIn:          new(big.Int),
		SpotPriceOut:         new(big.Int),
		FeeAmount:            new(big.Int),
		FeeBps:               new(big.Int),
		AvailableLiquidity:   new(big.Int),
		PriceTimestampIn:     new(big.Int),
		PriceTimestampOut:    new(big.Int),
		UtilizationCapBps:    new(big.Int),
		TradeUtilizationBps:  new(big.Int),
		TwapOut:              new(big.Int),
		TwapDeviationBps:     new(big.Int),
		EffectiveFeeBps:      new(big.Int),
		Tier:                 new(big.Int),
		PriceImpactBps:       new(big.Int),
		PriceImpactDeduction: new(big.Int),
	}
}

// Quote prices an exact-input or exact-output trade on 0x1021 without writing
// state (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:271-315).
// It never fails: a pair that cannot be priced returns zero amounts with the
// stale flags set, and any other refusal returns both stale flags with every
// amount zero, as the source catch does.
func (k *Keeper) Quote(ctx sdk.Context, tokenIn, tokenOut common.Address, amount sdk.Int, exactIn bool) QuoteResult {
	cacheCtx, _ := ctx.CacheContext()
	result, err := k.quoteChecked(cacheCtx, tokenIn, tokenOut, amount, exactIn)
	if err != nil {
		failed := newQuoteResult()
		failed.PriceStaleIn = true
		failed.PriceStaleOut = true
		return failed
	}
	return result
}

func (k *Keeper) quoteChecked(ctx sdk.Context, tokenIn, tokenOut common.Address, amount sdk.Int,
	exactIn bool) (QuoteResult, error) {
	result := newQuoteResult()
	params, err := k.GetParams(ctx)
	if err != nil {
		return QuoteResult{}, err
	}
	paramsIn, registeredIn := registeredToken(params, tokenIn)
	paramsOut, registeredOut := registeredToken(params, tokenOut)

	priceIn, timestampIn, staleIn := k.tokenPrice(ctx, paramsIn, registeredIn)
	priceOut, timestampOut, staleOut := k.tokenPrice(ctx, paramsOut, registeredOut)
	result.SpotPriceIn, result.PriceTimestampIn, result.PriceStaleIn = priceIn, timestampIn, staleIn
	result.SpotPriceOut, result.PriceTimestampOut, result.PriceStaleOut = priceOut, timestampOut, staleOut
	if staleIn || staleOut || priceIn.Sign() == 0 || priceOut.Sign() == 0 {
		return result, nil
	}

	rail := common.HexToAddress(types.PECORAddress)
	executedIn := amount
	if !exactIn {
		required, err := k.QuoteExactOut(ctx, rail, tokenIn, tokenOut, amount)
		if err != nil {
			return QuoteResult{}, err
		}
		executedIn = required
	}
	pricing, err := k.Price(ctx, rail, tokenIn, tokenOut, executedIn)
	if err != nil {
		return QuoteResult{}, err
	}

	result.AmountIn = executedIn.BigInt()
	result.GrossAmountOut = pricing.Gross.BigInt()
	if exactIn {
		result.AmountOut = pricing.Net.BigInt()
		result.FeeAmount = pricing.FeeAmount.BigInt()
		result.FeeBps = new(big.Int).SetUint64(pricing.FeeBps)
	} else {
		// The fee of an exact-output quote is the input above the fee-free
		// input, which rounds up (MetaAGQuoter.sol:348-362).
		baseInput, err := Convert(amount, sdk.NewIntFromBigInt(priceOut), sdk.NewIntFromBigInt(priceIn),
			paramsOut.Decimals, paramsIn.Decimals, true)
		if err != nil {
			return QuoteResult{}, err
		}
		result.AmountOut = amount.BigInt()
		if executedIn.GT(baseInput) {
			result.FeeAmount = executedIn.Sub(baseInput).BigInt()
		}
		if baseInput.IsPositive() {
			result.FeeBps = new(big.Int).Quo(new(big.Int).Mul(result.FeeAmount, big.NewInt(types.BpsDenominator)),
				baseInput.BigInt())
		}
	}
	result.EffectiveFeeBps = new(big.Int).Set(result.FeeBps)
	result.PriceImpactBps = new(big.Int).SetUint64(pricing.ImpactBps)
	result.PriceImpactDeduction = pricing.Deduction.BigInt()

	volume, err := VolumeUSD(executedIn, sdk.NewIntFromBigInt(priceIn), paramsIn.Decimals)
	if err != nil {
		return QuoteResult{}, err
	}
	result.Tier = big.NewInt(feeTier(volume.BigInt()))

	if result.AmountIn.Sign() != 0 {
		unitIn := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(paramsIn.Decimals)), nil)
		result.ExecutionPrice = new(big.Int).Quo(new(big.Int).Mul(result.AmountOut, unitIn), result.AmountIn)
	}

	spendable, err := k.Spendable(ctx, tokenOut)
	if err != nil {
		return QuoteResult{}, err
	}
	capBps := utilizationCap(paramsOut)
	result.AvailableLiquidity = spendable.BigInt()
	result.UtilizationCapBps = new(big.Int).SetUint64(capBps)
	result.TradeUtilizationBps, result.CappedByUtilization, result.SufficientLiquidity =
		utilization(result.GrossAmountOut, result.AvailableLiquidity, capBps)

	if !paramsOut.IsStablecoin {
		twap, err := k.TWAP(ctx, tokenOut, params.TwapSanityPeriod)
		switch {
		case err == nil:
			if twap.IsPositive() {
				result.TwapOut = twap.BigInt()
				deviation := new(big.Int).Sub(priceOut, result.TwapOut)
				deviation.Abs(deviation).Mul(deviation, big.NewInt(types.BpsDenominator))
				result.TwapDeviationBps = deviation.Quo(deviation, result.TwapOut)
			}
		case errors.Is(err, types.ErrTwapWindowInvalid), errors.Is(err, types.ErrStalePrice):
			// A ring that does not cover the period leaves both TWAP fields 0.
		default:
			return QuoteResult{}, err
		}
	}
	return result, nil
}

// LiquidityInfo returns the spendable reserves, index price and stale flag of
// token (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:172-188);
// an unregistered token has no reserves and a stale price.
func (k *Keeper) LiquidityInfo(ctx sdk.Context, token common.Address) (available, price *big.Int, stale bool,
	err error) {
	all, err := k.GetParams(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	params, registered := registeredToken(all, token)
	price, _, stale = k.tokenPrice(ctx, params, registered)
	if !registered {
		return new(big.Int), price, stale, nil
	}
	spendable, err := k.Spendable(ctx, token)
	if err != nil {
		return nil, nil, false, err
	}
	return spendable.BigInt(), price, stale, nil
}

// TokenPrice returns the index price, its timestamp in seconds and the stale
// flag of token (Launch-x:contracts/contracts/meta-ag/quoter/MetaAGQuoter.sol:221-233):
// a stable token prices at 1e18 at the block time, and a token that cannot be
// priced returns (0, 0, true).
func (k *Keeper) TokenPrice(ctx sdk.Context, token common.Address) (price, timestamp *big.Int, stale bool) {
	all, err := k.GetParams(ctx)
	if err != nil {
		return new(big.Int), new(big.Int), true
	}
	params, registered := registeredToken(all, token)
	return k.tokenPrice(ctx, params, registered)
}

func (k *Keeper) tokenPrice(ctx sdk.Context, params types.TokenParams, registered bool) (price,
	timestamp *big.Int, stale bool) {
	if !registered {
		return new(big.Int), new(big.Int), true
	}
	if params.IsStablecoin {
		return big.NewInt(1e18), big.NewInt(ctx.BlockTime().Unix()), false
	}
	if params.MarketID == 0 {
		return new(big.Int), new(big.Int), true
	}
	feed, err := k.pricefeedKeeper.GetPrice(ctx, params.MarketID)
	if err != nil || !feed.Index.IsPositive() {
		return new(big.Int), new(big.Int), true
	}
	return feed.Index.BigInt(), new(big.Int).SetUint64(feed.TimestampMs / 1000), !feed.Fresh
}

func registeredToken(params types.Params, token common.Address) (types.TokenParams, bool) {
	for _, registered := range params.Tokens {
		if registered.Address == token {
			return registered, true
		}
	}
	return types.TokenParams{}, false
}

// feeTier is 2 from the tier-2 volume, 1 from the tier-1 volume and 0 below.
func feeTier(volume *big.Int) int64 {
	switch {
	case volume.Cmp(quoteTier2Volume) >= 0:
		return 2
	case volume.Cmp(quoteTier1Volume) >= 0:
		return 1
	default:
		return 0
	}
}

// utilizationCap is the maxUtilizationBps of token, 10000 when unset.
func utilizationCap(params types.TokenParams) uint64 {
	if params.MaxUtilizationBps == 0 {
		return types.DefaultMaxUtilizationBps
	}
	return params.MaxUtilizationBps
}

// utilization returns tradeUtilizationBps, gross x 10000 / spendable rounded
// up and the maximum uint256 when spendable is 0, cappedByUtilization, above
// capBps, and sufficientLiquidity, not capped with gross at most spendable.
func utilization(gross, spendable *big.Int, capBps uint64) (tradeBps *big.Int, capped, sufficient bool) {
	if spendable.Sign() == 0 {
		tradeBps = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	} else {
		var remainder big.Int
		tradeBps, _ = new(big.Int).QuoRem(new(big.Int).Mul(gross, big.NewInt(types.BpsDenominator)), spendable,
			&remainder)
		if remainder.Sign() != 0 {
			tradeBps.Add(tradeBps, big.NewInt(1))
		}
	}
	capped = tradeBps.Cmp(new(big.Int).SetUint64(capBps)) > 0
	sufficient = !capped && gross.Cmp(spendable) <= 0
	return tradeBps, capped, sufficient
}
