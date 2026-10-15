package keeper

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// snapshotInterval is SidioraPool.SNAPSHOT_INTERVAL in seconds.
	snapshotInterval = 60
	// snapshotMinVolumeBps is SidioraPool.SNAPSHOT_MIN_VOLUME_BPS.
	snapshotMinVolumeBps = 1
)

// SwapResult is what a buy or sell moves.
type SwapResult struct {
	AmountIn    *big.Int
	AmountOut   *big.Int
	FeeBps      *big.Int
	FeeAmount   *big.Int
	ProtocolCut *big.Int
	PoolCut     *big.Int
}

func addUint256(a, b *big.Int) (*big.Int, error) {
	out := new(big.Int).Add(a, b)
	if out.Cmp(types.MaxUint256()) > 0 {
		return nil, types.ErrOverflow
	}
	return out, nil
}

func intOrZero(value sdk.Int) *big.Int {
	if value.IsNil() {
		return new(big.Int)
	}
	return value.BigInt()
}

// quote runs the arithmetic of SidioraPool._swap on market without moving
// funds and returns the market as the swap leaves it: the input fee, the buy
// or sell branch, the zero-output refusal, the volume per side and the price
// snapshot.
func quote(params types.Params, market types.Market, now, height int64, isBuy bool,
	amountIn *big.Int) (SwapResult, types.Market, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return SwapResult{}, market, types.ErrInsufficientInput
	}
	fee, err := feeBps(params, market, now)
	if err != nil {
		return SwapResult{}, market, err
	}
	bps := big.NewInt(types.BpsDenominator)
	feeAmount, err := types.MulDiv(amountIn, fee, bps)
	if err != nil {
		return SwapResult{}, market, err
	}
	afterFee := new(big.Int).Sub(amountIn, feeAmount)
	effective := market.EffectiveQuote()
	tokenReserve := market.TokenReserve.BigInt()
	real := market.RealQuoteBalance.BigInt()
	result := SwapResult{AmountIn: new(big.Int).Set(amountIn), FeeBps: fee, FeeAmount: feeAmount,
		ProtocolCut: new(big.Int), PoolCut: new(big.Int)}

	var volume *big.Int
	if isBuy {
		out, err := types.GetAmountOut(effective, tokenReserve, afterFee)
		if err != nil {
			return SwapResult{}, market, err
		}
		if out.Cmp(tokenReserve) > 0 {
			return SwapResult{}, market, types.ErrInsufficientLiquidity
		}
		balance, err := addUint256(real, afterFee)
		if err != nil {
			return SwapResult{}, market, err
		}
		quoteFees, err := addUint256(intOrZero(market.AccumulatedQuoteFees), feeAmount)
		if err != nil {
			return SwapResult{}, market, err
		}
		// FeeAccumulator.recordFee's split: protocolFeeBps of the fee to the
		// Treasury, the rest to the pool.
		protocolCut, err := types.MulDiv(feeAmount, new(big.Int).SetUint64(params.ProtocolFeeBps), bps)
		if err != nil {
			return SwapResult{}, market, err
		}
		market.RealQuoteBalance = sdk.NewIntFromBigInt(balance)
		market.TokenReserve = sdk.NewIntFromBigInt(new(big.Int).Sub(tokenReserve, out))
		market.AccumulatedQuoteFees = sdk.NewIntFromBigInt(quoteFees)
		result.AmountOut = out
		result.ProtocolCut = protocolCut
		result.PoolCut = new(big.Int).Sub(feeAmount, protocolCut)
		volume = amountIn
	} else {
		out, err := types.GetAmountOut(tokenReserve, effective, afterFee)
		if err != nil {
			return SwapResult{}, market, err
		}
		// The virtual floor: virtual quote prices the curve but is never paid.
		if out.Cmp(real) > 0 {
			return SwapResult{}, market, types.ErrVirtualFloorBreached
		}
		reserve, err := addUint256(tokenReserve, amountIn)
		if err != nil {
			return SwapResult{}, market, err
		}
		tokenFees, err := addUint256(intOrZero(market.AccumulatedTokenFees), feeAmount)
		if err != nil {
			return SwapResult{}, market, err
		}
		market.TokenReserve = sdk.NewIntFromBigInt(reserve)
		market.RealQuoteBalance = sdk.NewIntFromBigInt(new(big.Int).Sub(real, out))
		market.AccumulatedTokenFees = sdk.NewIntFromBigInt(tokenFees)
		result.AmountOut = out
		volume = out
	}
	if result.AmountOut.Sign() == 0 {
		return SwapResult{}, market, types.ErrInsufficientInput
	}

	cumulative, err := addUint256(intOrZero(market.CumulativeVolume), volume)
	if err != nil {
		return SwapResult{}, market, err
	}
	market.CumulativeVolume = sdk.NewIntFromBigInt(cumulative)
	side := &market.CumulativeSellQuoteVolume
	if isBuy {
		side = &market.CumulativeBuyQuoteVolume
	}
	sideVolume, err := addUint256(intOrZero(*side), volume)
	if err != nil {
		return SwapResult{}, market, err
	}
	*side = sdk.NewIntFromBigInt(sideVolume)
	price, err := market.Price()
	if err != nil {
		return SwapResult{}, market, err
	}
	if err := updateSnapshot(&market, price, volume, now, height); err != nil {
		return SwapResult{}, market, err
	}
	return result, market, nil
}

// updateSnapshot is SidioraPool._updatePriceSnapshot: price enters the ring
// only when volume is at least SNAPSHOT_MIN_VOLUME_BPS of the effective quote
// and, once a snapshot exists, SNAPSHOT_INTERVAL seconds passed since the
// last one. The block height of each snapshot is kept beside its slot.
func updateSnapshot(market *types.Market, price, volume *big.Int, now, height int64) error {
	threshold, err := types.MulDiv(market.EffectiveQuote(), big.NewInt(snapshotMinVolumeBps),
		big.NewInt(types.BpsDenominator))
	if err != nil {
		return err
	}
	if volume.Cmp(threshold) < 0 {
		return nil
	}
	if market.SnapshotCount != 0 && now < market.LastSnapshotTimestamp+snapshotInterval {
		return nil
	}
	if len(market.PriceSnapshots) != types.SnapshotSlots || market.SnapshotIndex >= types.SnapshotSlots ||
		(len(market.SnapshotHeights) != 0 && len(market.SnapshotHeights) != types.SnapshotSlots) || height < 0 {
		return fmt.Errorf("%w: snapshot ring", types.ErrInvalidMarket)
	}
	snapshots := append([]sdk.Int(nil), market.PriceSnapshots...)
	snapshots[market.SnapshotIndex] = sdk.NewIntFromBigInt(price)
	heights := make([]uint64, types.SnapshotSlots)
	copy(heights, market.SnapshotHeights)
	heights[market.SnapshotIndex] = uint64(height)
	market.LastSnapshotTimestamp = now
	market.PriceSnapshots = snapshots
	market.SnapshotHeights = heights
	market.SnapshotIndex = (market.SnapshotIndex + 1) % types.SnapshotSlots
	if market.SnapshotCount < types.SnapshotSlots {
		market.SnapshotCount++
	}
	return nil
}

// QuoteBuy is the outcome of buying with quoteIn now.
func (k *Keeper) QuoteBuy(ctx sdk.Context, denom string, quoteIn *big.Int) (SwapResult, error) {
	return k.quoteView(ctx, denom, true, quoteIn)
}

// QuoteSell is the outcome of selling amountIn tokens now.
func (k *Keeper) QuoteSell(ctx sdk.Context, denom string, amountIn *big.Int) (SwapResult, error) {
	return k.quoteView(ctx, denom, false, amountIn)
}

func (k *Keeper) quoteView(ctx sdk.Context, denom string, isBuy bool, amountIn *big.Int) (SwapResult, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return SwapResult{}, err
	}
	result, _, err := quote(k.GetParams(ctx), market, ctx.BlockTime().Unix(), ctx.BlockHeight(), isBuy, amountIn)
	return result, err
}

// Swap is SidioraPool.swap and swapFor over custody, in the order of
// SidioraPool._swap under the pool lock held in locks: the pool address is
// swept into HeldQuote(pool), then the deadline, the input, the recipient and
// the prepaid input are checked, the input already at the pool counting as
// paid: HeldQuote(pool) above realUsdlBalance for a buy, token.balanceOf(pool)
// above tokenReserve for a sell. A buy pays tokens from the pool address with
// token.transfer(recipient) and moves the fee from HeldQuote(pool) to FeeHeld
// of the market's FeeAccumulator, where RecordFee splits it; a sell pays USDL
// from custody with PayQuote and its fee tokens stay in the pool. The market
// is written before the first nested call. Swap returns the context the run
// continues on.
func (k *Keeper) Swap(ctx sdk.Context, caller NestedCaller, locks TransientStore, denom string, isBuy bool,
	amountIn, minAmountOut *big.Int, trader, recipient common.Address, deadline uint64) (sdk.Context, SwapResult, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return ctx, SwapResult{}, err
	}
	if market.Generation != types.Generation1 && market.Generation != types.Generation2 {
		return ctx, SwapResult{}, fmt.Errorf("%w: market %s has no pool", types.ErrInvalidMarket, denom)
	}
	pool := common.HexToAddress(market.PoolAddress)
	release, err := k.EnterLock(locks, PoolLock(pool))
	if err != nil {
		return ctx, SwapResult{}, err
	}
	defer release()
	if market.Paused {
		return ctx, SwapResult{}, types.ErrPaused
	}
	ctx, _, err = k.SweepPool(ctx, caller, pool)
	if err != nil {
		return ctx, SwapResult{}, err
	}

	now := ctx.BlockTime().Unix()
	if now < 0 || uint64(now) > deadline {
		return ctx, SwapResult{}, types.ErrDeadlineExpired
	}
	if amountIn == nil || amountIn.Sign() <= 0 {
		return ctx, SwapResult{}, types.ErrInsufficientInput
	}
	if recipient == (common.Address{}) {
		return ctx, SwapResult{}, types.ErrZeroAddress
	}
	token := common.HexToAddress(market.TokenAddress)
	var balance, accounted sdk.Int
	if isBuy {
		balance, err = k.GetQuoteLedger(ctx, HeldQuoteLedger(pool))
		accounted = market.RealQuoteBalance
	} else {
		balance, err = k.quoteBalance(ctx, caller, token, pool)
		accounted = market.TokenReserve
	}
	if err != nil {
		return ctx, SwapResult{}, err
	}
	if balance.LT(accounted) || balance.Sub(accounted).BigInt().Cmp(amountIn) < 0 {
		return ctx, SwapResult{}, types.ErrInsufficientInput
	}

	result, updated, err := quote(k.GetParams(ctx), market, now, ctx.BlockHeight(), isBuy, amountIn)
	if err != nil {
		return ctx, SwapResult{}, err
	}
	if minAmountOut != nil && result.AmountOut.Cmp(minAmountOut) < 0 {
		return ctx, SwapResult{}, types.ErrSlippageExceeded
	}
	k.setMarket(ctx, updated)

	if isBuy {
		ctx, err = callQuote(ctx, caller, pool, token,
			erc20Call(erc20Transfer, addressWord(recipient), amountWord(result.AmountOut)), types.ErrTransferFailed)
		if err != nil {
			return ctx, SwapResult{}, err
		}
		if result.FeeAmount.Sign() > 0 {
			fee := sdk.NewIntFromBigInt(result.FeeAmount)
			accumulator := common.HexToAddress(updated.FeeAccumulator)
			if err := k.MoveQuote(ctx, HeldQuoteLedger(pool), FeeHeldLedger(accumulator), fee); err != nil {
				return ctx, SwapResult{}, err
			}
			var protocolCut, poolCut sdk.Int
			if ctx, protocolCut, poolCut, err = k.RecordFee(ctx, caller, denom, fee); err != nil {
				return ctx, SwapResult{}, err
			}
			result.ProtocolCut, result.PoolCut = protocolCut.BigInt(), poolCut.BigInt()
		}
	} else {
		ctx, err = k.PayQuote(ctx, caller, HeldQuoteLedger(pool), recipient, sdk.NewIntFromBigInt(result.AmountOut))
		if err != nil {
			return ctx, SwapResult{}, err
		}
	}

	price, err := updated.Price()
	if err != nil {
		return ctx, SwapResult{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeSwap,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyTrader, trader.Hex()),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipient.Hex()),
		sdk.NewAttribute(types.AttributeKeyIsBuy, strconv.FormatBool(isBuy)),
		sdk.NewAttribute(types.AttributeKeyAmountIn, amountIn.String()),
		sdk.NewAttribute(types.AttributeKeyAmountOut, result.AmountOut.String()),
		sdk.NewAttribute(types.AttributeKeyFee, result.FeeAmount.String()),
		sdk.NewAttribute(types.AttributeKeyFeeBps, result.FeeBps.String()),
		sdk.NewAttribute(types.AttributeKeyPrice, price.String())))
	return ctx, result, nil
}
