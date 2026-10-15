package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// markPrice returns the mark price and the basis average of a new aggregate
// with index I = index, external mark E = markMedian and timestamp ts. prev is
// the stored aggregate of the market, nil on its first aggregate. Division
// truncates toward zero. A timestamp at or before the stored one keeps the
// stored basis average.
func markPrice(prev *types.Aggregate, index, markMedian sdk.Int, ts uint64, params types.Params) (mark, basis sdk.Int) {
	observed := markMedian.Sub(index)
	basis = observed
	if prev != nil {
		basis = prev.Basis
		if ts > prev.TimestampMs {
			dt := sdk.NewIntFromUint64(ts - prev.TimestampMs)
			tau := sdk.NewIntFromUint64(params.MarkEmaTauMs)
			basis = prev.Basis.Add(observed.Sub(prev.Basis).Mul(dt).Quo(dt.Add(tau)))
		}
	}

	mark = middle(index.Add(basis), markMedian, index)
	band := index.Mul(sdk.NewIntFromUint64(uint64(params.MaxMarkBasisBps))).Quo(sdk.NewIntFromUint64(uint64(types.BpsDenominator)))
	mark = sdk.MaxInt(mark, index.Sub(band))
	mark = sdk.MinInt(mark, index.Add(band))
	return sdk.MaxInt(mark, sdk.OneInt()), basis
}

// middle returns the median of three values.
func middle(a, b, c sdk.Int) sdk.Int {
	return sdk.MaxInt(sdk.MinInt(a, b), sdk.MinInt(sdk.MaxInt(a, b), c))
}
