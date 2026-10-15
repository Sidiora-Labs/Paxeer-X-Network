package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func bookOrder(id uint64, isLong bool, price uint256.Int, sequence uint64) types.Order {
	return types.Order{
		ID:          *uint256.NewInt(id),
		Account:     common.BigToAddress(uint256.NewInt(0xb000 + id).ToBig()),
		MarketID:    0,
		IsLong:      isLong,
		Type:        types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC,
		Status:      types.OrderStatusResting,
		LimitPrice:  price,
		SizeBase:    e18(1),
		Leverage:    e18(10),
		Sequence:    sequence,
	}
}

func bookSequences(t *testing.T, k *keeper.Keeper, ctx sdk.Context, marketID uint64, side byte) []uint64 {
	t.Helper()
	var sequences []uint64
	require.NoError(t, k.IterateBook(ctx, marketID, side, func(order types.Order) bool {
		sequences = append(sequences, order.Sequence)
		return false
	}))
	return sequences
}

func TestBookKeyOrdering(t *testing.T) {
	k, ctx, key := newKeeper(t)
	for _, order := range []types.Order{
		bookOrder(1, true, e18(59_990), 4),
		bookOrder(2, true, e18(60_000), 7),
		bookOrder(3, true, e18(60_000), 2),
		bookOrder(4, false, e18(60_020), 3),
		bookOrder(5, false, e18(60_010), 9),
		bookOrder(6, false, e18(60_010), 5),
	} {
		require.NoError(t, k.BookInsert(ctx, order))
	}

	require.Equal(t, []uint64{2, 7, 4}, bookSequences(t, k, ctx, 0, types.SideBuy))
	require.Equal(t, []uint64{5, 9, 3}, bookSequences(t, k, ctx, 0, types.SideSell))
	require.Empty(t, bookSequences(t, k, ctx, 2, types.SideBuy))

	bidKey := types.BookKey(0, types.SideBuy, types.BookPriceKey(true, e18(60_000)), 2)
	var inverted uint256.Int
	price := e18(60_000)
	inverted.Not(&price)
	word := inverted.Bytes32()
	require.Equal(t, append(append([]byte{0x06, 0, 0, 0, 0, 0, 0, 0, 0, types.SideBuy}, word[:]...), 0, 0, 0, 0, 0, 0, 0, 2), bidKey)
	id := uint256.NewInt(3).Bytes32()
	require.Equal(t, id[:], ctx.KVStore(key).Get(bidKey))

	stored, err := k.GetOrder(ctx, *uint256.NewInt(3))
	require.NoError(t, err)
	require.Equal(t, bookOrder(3, true, e18(60_000), 2), stored)

	var visited []uint64
	require.NoError(t, k.IterateBook(ctx, 0, types.SideBuy, func(order types.Order) bool {
		visited = append(visited, order.ID.Uint64())
		return len(visited) == 2
	}))
	require.Equal(t, []uint64{3, 2}, visited)
}

func TestBookBestPricesAndMid(t *testing.T) {
	k, ctx, _ := newKeeper(t)
	_, ok, err := k.BestBid(ctx, 0)
	require.NoError(t, err)
	require.False(t, ok)
	_, ok, err = k.MidPrice(ctx, 0)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, k.BookInsert(ctx, bookOrder(1, true, e18(59_990), 1)))
	require.NoError(t, k.BookInsert(ctx, bookOrder(2, true, e18(60_000), 2)))
	bid, ok, err := k.BestBid(ctx, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, e18(60_000), bid)
	_, ok, err = k.MidPrice(ctx, 0)
	require.NoError(t, err)
	require.False(t, ok, "mid is undefined while the ask side is empty")

	odd := e18(60_001)
	odd.AddUint64(&odd, 1)
	require.NoError(t, k.BookInsert(ctx, bookOrder(3, false, odd, 3)))
	require.NoError(t, k.BookInsert(ctx, bookOrder(4, false, e18(60_050), 4)))
	ask, ok, err := k.BestAsk(ctx, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, odd, ask)
	mid, ok, err := k.MidPrice(ctx, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "60000500000000000000000", mid.Dec(), "60,000 + floor((60,001e18 + 1 - 60,000e18) / 2)")
}

func TestBookInsertAndRemoveKeepIndexAndRecord(t *testing.T) {
	k, ctx, key := newKeeper(t)
	order := bookOrder(7, false, e18(61_000), 11)
	require.NoError(t, k.BookInsert(ctx, order))
	entry := types.BookKey(0, types.SideSell, types.BookPriceKey(false, e18(61_000)), 11)
	require.True(t, ctx.KVStore(key).Has(entry))

	err := k.BookInsert(ctx, order)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "already rests")

	taken := bookOrder(8, false, e18(61_000), 11)
	err = k.BookInsert(ctx, taken)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	_, err = k.GetOrder(ctx, *uint256.NewInt(8))
	require.ErrorIs(t, err, types.ErrUnknownOrder, "a refused insert writes no record")

	untriggered := bookOrder(9, true, e18(59_000), 12)
	untriggered.Status = types.OrderStatusUntriggered
	require.ErrorIs(t, k.BookInsert(ctx, untriggered), types.ErrInvalidParams)
	require.ErrorIs(t, k.BookInsert(ctx, bookOrder(0, true, e18(59_000), 13)), types.ErrInvalidParams)
	require.ErrorIs(t, k.BookInsert(ctx, bookOrder(10, true, uint256.Int{}, 14)), types.ErrInvalidParams)

	stillResting := order
	require.ErrorIs(t, k.BookRemove(ctx, stillResting), types.ErrInvalidParams)
	moved := order
	moved.Status = types.OrderStatusCancelled
	moved.LimitPrice = e18(61_001)
	require.ErrorIs(t, k.BookRemove(ctx, moved), types.ErrInvalidParams)
	require.True(t, ctx.KVStore(key).Has(entry), "a refused removal keeps the entry")

	filled := order
	filled.Status = types.OrderStatusFilled
	filled.FilledBase = filled.SizeBase
	require.NoError(t, k.BookRemove(ctx, filled))
	require.False(t, ctx.KVStore(key).Has(entry))
	stored, err := k.GetOrder(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, filled, stored)
	require.Empty(t, bookSequences(t, k, ctx, 0, types.SideSell))

	require.ErrorIs(t, k.BookRemove(ctx, filled), types.ErrUnknownOrder)
	unknown := bookOrder(99, true, e18(1), 1)
	unknown.Status = types.OrderStatusCancelled
	require.ErrorIs(t, k.BookRemove(ctx, unknown), types.ErrUnknownOrder)

	relisted := order
	relisted.Sequence = 20
	require.NoError(t, k.BookInsert(ctx, relisted))
	require.Equal(t, []uint64{20}, bookSequences(t, k, ctx, 0, types.SideSell))
}

func TestBookIterateRefusesCorruptEntries(t *testing.T) {
	k, ctx, key := newKeeper(t)
	require.ErrorIs(t, k.IterateBook(ctx, 0, 0, func(types.Order) bool { return false }), types.ErrInvalidParams)

	order := bookOrder(1, true, e18(60_000), 1)
	require.NoError(t, k.BookInsert(ctx, order))
	orphan := types.BookKey(0, types.SideBuy, types.BookPriceKey(true, e18(59_000)), 2)
	id := uint256.NewInt(2).Bytes32()
	ctx.KVStore(key).Set(orphan, id[:])
	err := k.IterateBook(ctx, 0, types.SideBuy, func(types.Order) bool { return false })
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "LXP_PERPS_UNKNOWN_ORDER")

	ctx.KVStore(key).Delete(orphan)
	misplaced := types.BookKey(0, types.SideBuy, types.BookPriceKey(true, e18(59_000)), 1)
	one := order.ID.Bytes32()
	ctx.KVStore(key).Set(misplaced, one[:])
	_, _, err = k.BestBid(ctx, 0)
	require.NoError(t, err, "the best entry is consistent and iteration stops there")
	_, _, err = k.MidPrice(ctx, 0)
	require.NoError(t, err)
	err = k.IterateBook(ctx, 0, types.SideBuy, func(types.Order) bool { return false })
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "disagrees with order 1")
}
