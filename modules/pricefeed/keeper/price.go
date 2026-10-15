package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// Price is the read view of a market aggregate at 18 decimals. Fresh tells
// the venues whether they may match, open, close or liquidate at it.
type Price struct {
	Index       sdk.Int
	Mark        sdk.Int
	TimestampMs uint64
	Height      int64
	Sources     uint32
	Fresh       bool
}

// GetPrice returns the aggregate of a listed market with its freshness
// against the block time. A listed market without an aggregate returns zero
// prices with Fresh false; an id outside the params returns ErrUnknownMarket.
func (k Keeper) GetPrice(ctx sdk.Context, marketId uint64) (Price, error) {
	params := k.GetParams(ctx)
	for _, market := range params.Markets {
		if market.ID == marketId {
			return k.priceOf(ctx, market, params), nil
		}
	}
	return Price{}, types.ErrUnknownMarket.Wrapf("market id %d", marketId)
}

// GetPriceBySymbol returns the Price of the market with that case-sensitive
// symbol, or ErrUnknownMarket when no listed market carries it.
func (k Keeper) GetPriceBySymbol(ctx sdk.Context, symbol string) (Price, error) {
	params := k.GetParams(ctx)
	for _, market := range params.Markets {
		if market.Symbol == symbol {
			return k.priceOf(ctx, market, params), nil
		}
	}
	return Price{}, types.ErrUnknownMarket.Wrapf("market symbol %q", symbol)
}

func (k Keeper) priceOf(ctx sdk.Context, market types.Market, params types.Params) Price {
	var aggregate types.Aggregate
	if !k.get(ctx, types.AggregateKey(market.ID), &aggregate) {
		return Price{Index: sdk.ZeroInt(), Mark: sdk.ZeroInt()}
	}
	return Price{
		Index:       aggregate.IndexPrice,
		Mark:        aggregate.MarkPrice,
		TimestampMs: aggregate.TimestampMs,
		Height:      aggregate.Height,
		Sources:     aggregate.Sources,
		Fresh: market.Enabled &&
			fresh(ctx.BlockTime().UnixMilli(), aggregate.TimestampMs, params.MaxClockSkewMs, params.MaxPriceAgeMs),
	}
}

// fresh applies the regression and staleness checks to a read: the price is
// at most maxClockSkewMs ahead of the block and at most maxPriceAgeMs behind
// it. A block time before the epoch is never fresh.
func fresh(blockMs int64, timestampMs, maxClockSkewMs, maxPriceAgeMs uint64) bool {
	if blockMs < 0 {
		return false
	}
	now := uint64(blockMs)
	if timestampMs > now {
		return timestampMs-now <= maxClockSkewMs
	}
	return now-timestampMs <= maxPriceAgeMs
}
