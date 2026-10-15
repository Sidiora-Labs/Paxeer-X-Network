package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func twapRingKey(token common.Address) []byte {
	return append(append([]byte{}, types.TwapRingPrefix...), token.Bytes()...)
}

func twapSampleKey(token common.Address, slot uint32) []byte {
	key := append(append([]byte{}, types.TwapSamplePrefix...), token.Bytes()...)
	return binary.BigEndian.AppendUint32(key, slot)
}

// twapRing returns the ring of token; Head is the slot the next sample is
// written to and Count the number of filled slots.
func (k *Keeper) twapRing(ctx sdk.Context, token common.Address) (types.TwapRing, error) {
	bz := ctx.KVStore(k.storeKey).Get(twapRingKey(token))
	if bz == nil {
		return types.TwapRing{Token: token}, nil
	}
	var ring types.TwapRing
	if err := json.Unmarshal(bz, &ring); err != nil {
		return types.TwapRing{}, fmt.Errorf("spot: corrupt twap ring %s: %w", token.Hex(), err)
	}
	if ring.Head >= types.TwapHistoryDepth || ring.Count > types.TwapHistoryDepth {
		return types.TwapRing{}, fmt.Errorf("spot: twap ring %s out of bounds", token.Hex())
	}
	return ring, nil
}

// twapSample returns the sample at logical position i of ring, 0 being the
// oldest retained sample.
func (k *Keeper) twapSample(ctx sdk.Context, ring types.TwapRing, i uint32) (types.TwapSample, error) {
	slot := (ring.Head + types.TwapHistoryDepth - ring.Count + i) % types.TwapHistoryDepth
	bz := ctx.KVStore(k.storeKey).Get(twapSampleKey(ring.Token, slot))
	if bz == nil {
		return types.TwapSample{}, fmt.Errorf("spot: twap sample %s/%d missing", ring.Token.Hex(), slot)
	}
	var sample types.TwapSample
	if err := json.Unmarshal(bz, &sample); err != nil {
		return types.TwapSample{}, fmt.Errorf("spot: corrupt twap sample %s/%d: %w", ring.Token.Hex(), slot, err)
	}
	return sample, nil
}

// RecordSample writes (block time, index price) of token into its ring of
// TwapHistoryDepth samples, overwriting the oldest once the ring is full
// (PriceOracle MAX_HISTORY_DEPTH).
func (k *Keeper) RecordSample(ctx sdk.Context, token common.Address) error {
	index, err := k.IndexPrice(ctx, token)
	if err != nil {
		return err
	}
	return k.appendSample(ctx, token, uint64(ctx.BlockTime().Unix()), index)
}

func (k *Keeper) appendSample(ctx sdk.Context, token common.Address, time uint64, index sdk.Int) error {
	ring, err := k.twapRing(ctx, token)
	if err != nil {
		return err
	}
	sample, err := json.Marshal(types.TwapSample{Token: token, Slot: ring.Head, Time: time, Index: index})
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	store.Set(twapSampleKey(token, ring.Head), sample)
	ring.Head = (ring.Head + 1) % types.TwapHistoryDepth
	if ring.Count < types.TwapHistoryDepth {
		ring.Count++
	}
	bz, err := json.Marshal(ring)
	if err != nil {
		return err
	}
	store.Set(twapRingKey(token), bz)
	return nil
}

// TWAP is the time-weighted index price of token over the period seconds
// ending at the block time, each sample holding until the next one and the
// latest until the block time (PriceOracle getTWAP). A zero period, one
// longer than the block time or one reaching before the oldest retained
// sample returns TwapWindowInvalid().
func (k *Keeper) TWAP(ctx sdk.Context, token common.Address, period uint64) (sdk.Int, error) {
	now := uint64(ctx.BlockTime().Unix())
	if period == 0 || period > now {
		return sdk.Int{}, types.ErrTwapWindowInvalid
	}
	ring, err := k.twapRing(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	if ring.Count == 0 {
		return sdk.Int{}, types.ErrTwapWindowInvalid
	}
	target := now - period
	oldest, err := k.twapSample(ctx, ring, 0)
	if err != nil {
		return sdk.Int{}, err
	}
	if oldest.Time > target {
		return sdk.Int{}, types.ErrTwapWindowInvalid
	}
	// Upper-bound search for the last sample at or before target.
	low, high := uint32(0), ring.Count
	for low+1 < high {
		mid := (low + high) / 2
		sample, err := k.twapSample(ctx, ring, mid)
		if err != nil {
			return sdk.Int{}, err
		}
		if sample.Time <= target {
			low = mid
		} else {
			high = mid
		}
	}
	sum := new(big.Int)
	current, err := k.twapSample(ctx, ring, low)
	if err != nil {
		return sdk.Int{}, err
	}
	for i := low; i < ring.Count; i++ {
		end := now
		var next types.TwapSample
		if i+1 < ring.Count {
			if next, err = k.twapSample(ctx, ring, i+1); err != nil {
				return sdk.Int{}, err
			}
			end = min(next.Time, now)
		}
		if start := max(current.Time, target); end > start {
			sum.Add(sum, new(big.Int).Mul(current.Index.BigInt(), new(big.Int).SetUint64(end-start)))
		}
		current = next
	}
	return sdk.NewIntFromBigInt(sum.Quo(sum, new(big.Int).SetUint64(period))), nil
}

// deviatesFromTWAP reports whether index differs from twap by more than
// maxBps of twap.
func deviatesFromTWAP(index, twap sdk.Int, maxBps uint64) bool {
	diff := new(big.Int).Sub(index.BigInt(), twap.BigInt())
	diff.Abs(diff).Mul(diff, big.NewInt(types.BpsDenominator))
	return diff.Cmp(new(big.Int).Mul(twap.BigInt(), new(big.Int).SetUint64(maxBps))) > 0
}

// CheckTwapSanity refuses a trade of token at index with StalePrice() while
// twapSanityEnabled and index deviates from the TWAP over twapSanityPeriod by
// more than maxTwapDeviationBps; a TWAP that cannot be read refuses the trade
// with its error.
func (k *Keeper) CheckTwapSanity(ctx sdk.Context, token common.Address, index sdk.Int) error {
	params := k.GetParams(ctx)
	if !params.TwapSanityEnabled {
		return nil
	}
	twap, err := k.TWAP(ctx, token, params.TwapSanityPeriod)
	if err != nil {
		return err
	}
	if deviatesFromTWAP(index, twap, params.MaxTwapDeviationBps) {
		return types.ErrStalePrice
	}
	return nil
}

// FillQualityOK reports whether an EndBlock fill of token at index may run:
// always while fillQualityTwapPeriod is zero, otherwise only when index stays
// within maxTwapDeviationBps of the TWAP over that period. A TWAP that cannot
// be read holds the fill and returns its error.
func (k *Keeper) FillQualityOK(ctx sdk.Context, token common.Address, index sdk.Int) (bool, error) {
	params := k.GetParams(ctx)
	if params.FillQualityTwapPeriod == 0 {
		return true, nil
	}
	twap, err := k.TWAP(ctx, token, params.FillQualityTwapPeriod)
	if err != nil {
		return false, err
	}
	return !deviatesFromTWAP(index, twap, params.MaxTwapDeviationBps), nil
}
