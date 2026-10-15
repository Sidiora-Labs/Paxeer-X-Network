package keeper

import (
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// checkObservation applies the per-observation checks in order and returns
// the reason of the first one that fails, or "" and the record to store.
// The checks are unknown_market, market_disabled, sequence (the kernel
// sequence rule), future and stale (the kernel timestamp regression and
// staleness checks), bounds on the index and the mark, and deviation of the
// index from the aggregate index. Deviation is skipped while the aggregate is
// not fresh, so a market whose aggregate aged out recovers from new
// observations as the kernel clears its halt on the next accepted one.
func (k Keeper) checkObservation(ctx sdk.Context, params types.Params, operator sdk.ValAddress,
	observation types.Observation) (string, types.ObservationRecord, error) {
	if observation.MarketId >= uint64(len(params.Markets)) {
		return types.RefusalUnknownMarket, types.ObservationRecord{}, nil
	}
	market := params.Markets[observation.MarketId]
	if !market.Enabled {
		return types.RefusalMarketDisabled, types.ObservationRecord{}, nil
	}

	var stored types.ObservationRecord
	if k.get(ctx, types.ObservationKey(market.ID, operator), &stored) && observation.Sequence <= stored.Sequence {
		return types.RefusalSequence, types.ObservationRecord{}, nil
	}

	var nowMs uint64
	if blockMs := ctx.BlockTime().UnixMilli(); blockMs > 0 {
		nowMs = uint64(blockMs)
	}
	source := observation.SourceTimestampMs
	if source > nowMs && source-nowMs > params.MaxClockSkewMs {
		return types.RefusalFuture, types.ObservationRecord{}, nil
	}
	if nowMs > source && nowMs-source > params.StalenessWindowMs {
		return types.RefusalStale, types.ObservationRecord{}, nil
	}

	index, ok := sdk.NewIntFromString(observation.IndexPrice)
	if !ok {
		return "", types.ObservationRecord{}, types.ErrInvalidObservation.Wrapf("index price %q", observation.IndexPrice)
	}
	mark, ok := sdk.NewIntFromString(observation.MarkPrice)
	if !ok {
		return "", types.ObservationRecord{}, types.ErrInvalidObservation.Wrapf("mark price %q", observation.MarkPrice)
	}
	if !withinBounds(index, market) || !withinBounds(mark, market) {
		return types.RefusalBounds, types.ObservationRecord{}, nil
	}

	price, err := k.GetPrice(ctx, market.ID)
	if err != nil {
		return "", types.ObservationRecord{}, err
	}
	if price.Fresh {
		bps := sdk.NewIntFromUint64(uint64(types.BpsDenominator))
		allowed := price.Index.Mul(sdk.NewIntFromUint64(uint64(params.MaxDeviationBps)))
		if index.Sub(price.Index).Abs().Mul(bps).GT(allowed) {
			return types.RefusalDeviation, types.ObservationRecord{}, nil
		}
	}

	return "", types.ObservationRecord{
		Sequence:          observation.Sequence,
		IndexPrice:        index,
		MarkPrice:         mark,
		SourceTimestampMs: source,
		Height:            ctx.BlockHeight(),
	}, nil
}

// withinBounds reports min_price <= price <= max_price of the market.
func withinBounds(price sdk.Int, market types.Market) bool {
	return price.GTE(market.MinPrice) && price.LTE(market.MaxPrice)
}

func emitAccepted(ctx sdk.Context, operator sdk.ValAddress, marketID uint64, record types.ObservationRecord) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeObservation,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeMarketID, strconv.FormatUint(marketID, 10)),
		sdk.NewAttribute(types.AttributeSequence, strconv.FormatUint(record.Sequence, 10)),
		sdk.NewAttribute(types.AttributeIndexPrice, record.IndexPrice.String()),
		sdk.NewAttribute(types.AttributeMarkPrice, record.MarkPrice.String()),
		sdk.NewAttribute(types.AttributeSourceTimestampMs, strconv.FormatUint(record.SourceTimestampMs, 10)),
	))
}

func emitRefused(ctx sdk.Context, operator sdk.ValAddress, observation types.Observation, reason string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeObservationRefused,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeMarketID, strconv.FormatUint(observation.MarketId, 10)),
		sdk.NewAttribute(types.AttributeSequence, strconv.FormatUint(observation.Sequence, 10)),
		sdk.NewAttribute(types.AttributeReason, reason),
	))
}
