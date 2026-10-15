package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/holiman/uint256"
)

var (
	two255 = new(big.Int).Lsh(big.NewInt(1), 255)
	two256 = new(big.Int).Lsh(big.NewInt(1), 256)
)

// GetMarketState returns the stored state of a market, the zero state of the
// market before its first price update.
func (k *Keeper) GetMarketState(ctx sdk.Context, marketID uint64) (types.MarketState, error) {
	bz := k.store(ctx).Get(types.MarketStateKey(marketID))
	if bz == nil {
		return types.MarketState{MarketID: marketID}, nil
	}
	var state types.MarketState
	if err := state.Unmarshal(bz); err != nil {
		return types.MarketState{}, fmt.Errorf("%w: corrupt market state %d: %v", types.ErrInvalidParams, marketID, err)
	}
	if state.MarketID != marketID {
		return types.MarketState{}, fmt.Errorf("%w: market state %d holds market %d", types.ErrInvalidParams, marketID, state.MarketID)
	}
	return state, nil
}

// SetMarketState stores the state of its market.
func (k *Keeper) SetMarketState(ctx sdk.Context, state types.MarketState) {
	k.store(ctx).Set(types.MarketStateKey(state.MarketID), state.Marshal())
}

// UpdatePrices is the first EndBlock step: it reads GetPrice of the pricefeed
// once for every enabled market, stores the index price, the pricefeed mark,
// their timestamp and the freshness flag in market state and, while the price
// is fresh, advances the EMA basis and recomputes the mark. It returns one
// MarkPriceUpdated record for each market whose mark changed. A stale price
// leaves the mark and the EMA basis as they were.
func (k *Keeper) UpdatePrices(ctx sdk.Context) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	var enabled []uint64
	if err := k.IterateMarkets(ctx, func(market types.Market) bool {
		if market.Enabled {
			enabled = append(enabled, market.ID)
		}
		return false
	}); err != nil {
		return nil, err
	}
	var events []types.EventRecord
	for _, marketID := range enabled {
		event, changed, err := k.updateMarketPrice(ctx, marketID, params.MarkEmaSeconds)
		if err != nil {
			return nil, err
		}
		if changed {
			events = append(events, event)
		}
	}
	return events, nil
}

func (k *Keeper) updateMarketPrice(ctx sdk.Context, marketID uint64, emaSeconds uint64) (types.EventRecord, bool, error) {
	price, err := k.pricefeed.GetPrice(ctx, marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	index, err := priceWord(price.Index, "index", marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	pricefeedMark, err := priceWord(price.Mark, "mark", marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	state, err := k.GetMarketState(ctx, marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	previousTimestampMs := state.PriceTimestampMs
	state.IndexPrice = index
	state.PricefeedMark = pricefeedMark
	state.PriceTimestampMs = price.TimestampMs
	state.Fresh = price.Fresh
	if !price.Fresh {
		k.SetMarketState(ctx, state)
		return types.EventRecord{}, false, nil
	}

	bid, hasBid, err := k.BestBid(ctx, marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	ask, hasAsk, err := k.BestAsk(ctx, marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	mid, hasMid, err := k.MidPrice(ctx, marketID)
	if err != nil {
		return types.EventRecord{}, false, err
	}
	if !hasBid {
		bid = uint256.Int{}
	}
	if !hasAsk {
		ask = uint256.Int{}
	}
	if !hasMid {
		mid = index
	}

	basis := state.PremiumEma
	if state.MarkPrice.IsZero() {
		basis, err = signedWord(new(big.Int).Sub(mid.ToBig(), index.ToBig()), marketID)
	} else if price.TimestampMs > previousTimestampMs {
		basis, err = emaBasis(state.PremiumEma, index, mid, price.TimestampMs-previousTimestampMs, emaSeconds, marketID)
	}
	if err != nil {
		return types.EventRecord{}, false, err
	}
	state.PremiumEma = basis

	mark := ComputeMark(index, basis, bid, ask, state.LastTradePrice, pricefeedMark)
	changed := !mark.Eq(&state.MarkPrice)
	state.MarkPrice = mark
	k.SetMarketState(ctx, state)
	if !changed {
		return types.EventRecord{}, false, nil
	}
	return types.EventRecord{
		Name: types.EventMarkPriceUpdated,
		Args: []any{new(big.Int).SetUint64(marketID), mark.ToBig(), index.ToBig(), mid.ToBig()},
	}, true, nil
}

// ComputeMark is median(index + basis, median(bid, ask, last), pricefeedMark)
// with basis the signed EMA of mid minus index. A zero bid, ask, last trade
// or pricefeed mark is undefined and takes the index price. The result lies
// between the two unsigned components, so index + basis needs no bound: below
// zero it is the lower one and above 2^256-1 the higher one.
func ComputeMark(index, basis, bid, ask, last, pricefeedMark uint256.Int) uint256.Int {
	book := median3(orIndex(bid, index), orIndex(ask, index), orIndex(last, index))
	external := orIndex(pricefeedMark, index)
	low, high := book, external
	if low.Gt(&high) {
		low, high = high, low
	}
	var smoothed uint256.Int
	_, carry := smoothed.AddOverflow(&index, &basis)
	negative := basis.Sign() < 0
	switch {
	case negative && !carry:
		return low
	case !negative && carry:
		return high
	case smoothed.Lt(&low):
		return low
	case smoothed.Gt(&high):
		return high
	default:
		return smoothed
	}
}

// RequireFresh refuses an opening order, a market close or a liquidation of
// a market whose pricefeed price is not fresh with ErrPriceStale.
func (k *Keeper) RequireFresh(ctx sdk.Context, marketID uint64) error {
	price, err := k.pricefeed.GetPrice(ctx, marketID)
	if err != nil {
		return err
	}
	if !price.Fresh {
		return fmt.Errorf("%w: market %d", types.ErrPriceStale, marketID)
	}
	return nil
}

// emaBasis advances the EMA of mid minus index by dtMs milliseconds with the
// time constant of emaSeconds: B + (D - B) x dt / (dt + tau), truncating
// toward zero. The result lies between B and D.
func emaBasis(previous, index, mid uint256.Int, dtMs, emaSeconds, marketID uint64) (uint256.Int, error) {
	prev := signedBig(previous)
	observed := new(big.Int).Sub(mid.ToBig(), index.ToBig())
	dt := new(big.Int).SetUint64(dtMs)
	tau := new(big.Int).Mul(new(big.Int).SetUint64(emaSeconds), big.NewInt(1000))
	step := new(big.Int).Sub(observed, prev)
	step.Mul(step, dt).Quo(step, tau.Add(tau, dt))
	return signedWord(prev.Add(prev, step), marketID)
}

func orIndex(value, index uint256.Int) uint256.Int {
	if value.IsZero() {
		return index
	}
	return value
}

func median3(a, b, c uint256.Int) uint256.Int {
	if a.Gt(&b) {
		a, b = b, a
	}
	if b.Gt(&c) {
		b = c
	}
	if a.Gt(&b) {
		return a
	}
	return b
}

// signedBig reads a two's complement int256 word.
func signedBig(word uint256.Int) *big.Int {
	value := word.ToBig()
	if word.Sign() < 0 {
		value.Sub(value, two256)
	}
	return value
}

// signedWord writes value as a two's complement int256 word, refusing a value
// outside [-2^255, 2^255).
func signedWord(value *big.Int, marketID uint64) (uint256.Int, error) {
	if value.Cmp(two255) >= 0 || value.Cmp(new(big.Int).Neg(two255)) < 0 {
		return uint256.Int{}, fmt.Errorf("%w: market %d EMA basis %s outside int256", types.ErrOverflow, marketID, value)
	}
	unsigned := new(big.Int).Set(value)
	if unsigned.Sign() < 0 {
		unsigned.Add(unsigned, two256)
	}
	var word uint256.Int
	word.SetFromBig(unsigned)
	return word, nil
}

// priceWord converts a pricefeed price to a word; a nil price is zero.
func priceWord(price sdk.Int, name string, marketID uint64) (uint256.Int, error) {
	if price.IsNil() {
		return uint256.Int{}, nil
	}
	value := price.BigInt()
	if value.Sign() < 0 || value.BitLen() > 256 {
		return uint256.Int{}, fmt.Errorf("%w: market %d pricefeed %s price %s outside uint256", types.ErrOverflow, marketID, name, value)
	}
	var word uint256.Int
	word.SetFromBig(value)
	return word, nil
}
