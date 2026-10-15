package keeper

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/holiman/uint256"
)

// bookPriceKeyLength and bookSequenceLength are the widths of the price key
// and the placement sequence that follow a book side prefix.
const (
	bookPriceKeyLength = 32
	bookSequenceLength = 8
)

// GetOrder returns the stored order record, ErrUnknownOrder when none is
// stored.
func (k *Keeper) GetOrder(ctx sdk.Context, orderID uint256.Int) (types.Order, error) {
	bz := k.store(ctx).Get(types.OrderKey(orderID))
	if bz == nil {
		return types.Order{}, fmt.Errorf("%w: %s", types.ErrUnknownOrder, orderID.Dec())
	}
	var order types.Order
	if err := order.Unmarshal(bz); err != nil {
		return types.Order{}, fmt.Errorf("%w: corrupt order %s: %v", types.ErrInvalidParams, orderID.Dec(), err)
	}
	if !order.ID.Eq(&orderID) {
		return types.Order{}, fmt.Errorf("%w: order key %s holds order %s", types.ErrInvalidParams, orderID.Dec(), order.ID.Dec())
	}
	return order, nil
}

func (k *Keeper) setOrder(ctx sdk.Context, order types.Order) {
	k.store(ctx).Set(types.OrderKey(order.ID), order.Marshal())
}

// bookKey is the book entry key of a resting order: its market, its side, its
// limit price keyed best first and its placement sequence.
func bookKey(order types.Order) []byte {
	return types.BookKey(order.MarketID, types.SideOf(order.IsLong), types.BookPriceKey(order.IsLong, order.LimitPrice), order.Sequence)
}

// BookInsert stores a RESTING order record and its one book entry, keyed so
// that ascending iteration of the side yields the best price, then the
// earliest placement sequence. It refuses an order that is not RESTING, has a
// zero id or a zero limit price, or whose stored record is already RESTING, so
// a book entry never exists without its record nor a record with two entries
// (invariant 3 of contracts/perps.md section 24).
func (k *Keeper) BookInsert(ctx sdk.Context, order types.Order) error {
	if order.Status != types.OrderStatusResting {
		return fmt.Errorf("%w: order %s with status %d cannot rest", types.ErrInvalidParams, order.ID.Dec(), order.Status)
	}
	if order.ID.IsZero() {
		return fmt.Errorf("%w: order id 0 cannot rest", types.ErrInvalidParams)
	}
	if order.LimitPrice.IsZero() {
		return fmt.Errorf("%w: order %s rests at price 0", types.ErrInvalidParams, order.ID.Dec())
	}
	stored, err := k.GetOrder(ctx, order.ID)
	switch {
	case err == nil:
		if stored.Status == types.OrderStatusResting {
			return fmt.Errorf("%w: order %s already rests", types.ErrInvalidParams, order.ID.Dec())
		}
	case !errors.Is(err, types.ErrUnknownOrder):
		return err
	}
	key := bookKey(order)
	if k.store(ctx).Has(key) {
		return fmt.Errorf("%w: book key %x of order %s is taken", types.ErrInvalidParams, key, order.ID.Dec())
	}
	id := order.ID.Bytes32()
	k.store(ctx).Set(key, id[:])
	k.setOrder(ctx, order)
	return nil
}

// BookRemove deletes the book entry of a RESTING order and stores the order
// record with its final status, FILLED, CANCELLED or EXPIRED, in the same
// write set. The stored record must be RESTING and agree with order on
// account, market, side, limit price and sequence, and its book entry must
// point to it; otherwise nothing is written.
func (k *Keeper) BookRemove(ctx sdk.Context, order types.Order) error {
	switch order.Status {
	case types.OrderStatusFilled, types.OrderStatusCancelled, types.OrderStatusExpired:
	default:
		return fmt.Errorf("%w: order %s leaves the book with status %d", types.ErrInvalidParams, order.ID.Dec(), order.Status)
	}
	stored, err := k.GetOrder(ctx, order.ID)
	if err != nil {
		return err
	}
	if stored.Status != types.OrderStatusResting {
		return fmt.Errorf("%w: order %s does not rest", types.ErrUnknownOrder, order.ID.Dec())
	}
	if stored.Account != order.Account || stored.MarketID != order.MarketID || stored.IsLong != order.IsLong ||
		!stored.LimitPrice.Eq(&order.LimitPrice) || stored.Sequence != order.Sequence {
		return fmt.Errorf("%w: order %s disagrees with its stored record", types.ErrInvalidParams, order.ID.Dec())
	}
	key := bookKey(stored)
	id := stored.ID.Bytes32()
	if !bytes.Equal(k.store(ctx).Get(key), id[:]) {
		return fmt.Errorf("%w: resting order %s has no book entry", types.ErrInvalidParams, order.ID.Dec())
	}
	k.store(ctx).Delete(key)
	k.setOrder(ctx, order)
	return nil
}

// IterateBook visits the resting orders of one side of a market's book, best
// price first and, at one price, in ascending placement sequence, until visit
// returns true. Every entry is checked against its order record: a malformed
// key or value, a missing record, or a record that is not RESTING at that
// market, side, price and sequence is reported as corrupt state.
func (k *Keeper) IterateBook(ctx sdk.Context, marketID uint64, side byte, visit func(types.Order) (stop bool)) error {
	if side != types.SideBuy && side != types.SideSell {
		return fmt.Errorf("%w: book side %d", types.ErrInvalidParams, side)
	}
	prefix := types.BookSidePrefix(marketID, side)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key, value := iterator.Key(), iterator.Value()
		if len(key) != len(prefix)+bookPriceKeyLength+bookSequenceLength || len(value) != 32 {
			return fmt.Errorf("%w: book entry %x", types.ErrInvalidParams, key)
		}
		var orderID uint256.Int
		orderID.SetBytes32(value)
		order, err := k.GetOrder(ctx, orderID)
		if err != nil {
			return fmt.Errorf("%w: book entry %x: %v", types.ErrInvalidParams, key, err)
		}
		if order.Status != types.OrderStatusResting || order.MarketID != marketID || types.SideOf(order.IsLong) != side ||
			!bytes.Equal(bookKey(order), key) {
			return fmt.Errorf("%w: book entry %x disagrees with order %s", types.ErrInvalidParams, key, orderID.Dec())
		}
		if visit(order) {
			return nil
		}
	}
	return nil
}

// BestBid is the highest resting buy price of the market; false when no bid
// rests.
func (k *Keeper) BestBid(ctx sdk.Context, marketID uint64) (uint256.Int, bool, error) {
	return k.bestPrice(ctx, marketID, types.SideBuy)
}

// BestAsk is the lowest resting sell price of the market; false when no ask
// rests.
func (k *Keeper) BestAsk(ctx sdk.Context, marketID uint64) (uint256.Int, bool, error) {
	return k.bestPrice(ctx, marketID, types.SideSell)
}

func (k *Keeper) bestPrice(ctx sdk.Context, marketID uint64, side byte) (uint256.Int, bool, error) {
	var best uint256.Int
	found := false
	err := k.IterateBook(ctx, marketID, side, func(order types.Order) bool {
		best, found = order.LimitPrice, true
		return true
	})
	if err != nil {
		return uint256.Int{}, false, err
	}
	return best, found, nil
}

// MidPrice is the lower of the best bid and ask plus half their distance,
// rounded down, as book_mid_price computes it; false when either side is
// empty, the mid of contracts/perps-parameters.md section 5 being undefined.
func (k *Keeper) MidPrice(ctx sdk.Context, marketID uint64) (uint256.Int, bool, error) {
	bid, hasBid, err := k.BestBid(ctx, marketID)
	if err != nil || !hasBid {
		return uint256.Int{}, false, err
	}
	ask, hasAsk, err := k.BestAsk(ctx, marketID)
	if err != nil || !hasAsk {
		return uint256.Int{}, false, err
	}
	low, high := bid, ask
	if high.Lt(&low) {
		low, high = high, low
	}
	var mid uint256.Int
	mid.Sub(&high, &low)
	mid.Rsh(&mid, 1)
	mid.Add(&mid, &low)
	return mid, true, nil
}
