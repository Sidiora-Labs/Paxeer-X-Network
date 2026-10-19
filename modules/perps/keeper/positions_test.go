package keeper_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	positionHolder = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	positionOther  = common.HexToAddress("0x00000000000000000000000000000000000000b2")
)

type positionsEnv struct {
	k     *keeper.Keeper
	ctx   sdk.Context
	store sdk.KVStore
}

// newPositionsEnv lists BTC (0) with 5 size decimals, so the lot is 1e13
// base units, at the given mark and cumulative funding index.
func newPositionsEnv(t *testing.T, mark, fundingIndex uint256.Int) positionsEnv {
	t.Helper()
	k, ctx, key := newKeeper(t)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	k.SetMarketState(ctx, types.MarketState{MarketID: 0, MarkPrice: mark, Fresh: true, FundingIndex: fundingIndex})
	return positionsEnv{k: k, ctx: ctx, store: ctx.KVStore(key)}
}

func (env positionsEnv) setAccount(address common.Address, collateral uint64, positions, openOrders uint64) {
	env.store.Set(types.AccountKey(address), types.Account{
		Address: address, Collateral: *uint256.NewInt(collateral), PositionCount: positions,
		OpenOrderCount: openOrders, Created: true,
	}.Marshal())
}

func (env positionsEnv) account(t *testing.T, address common.Address) types.Account {
	t.Helper()
	bz := env.store.Get(types.AccountKey(address))
	require.NotNil(t, bz)
	var account types.Account
	require.NoError(t, account.Unmarshal(bz))
	return account
}

func (env positionsEnv) setPosition(position types.Position) {
	env.store.Set(types.PositionKey(position.ID), position.Marshal())
	id := position.ID.Bytes32()
	env.store.Set(types.AccountMarketPositionKey(position.Account, position.MarketID), id[:])
	env.store.Set(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID),
		[]byte{})
}

func (env positionsEnv) position(t *testing.T, id uint64) types.Position {
	t.Helper()
	bz := env.store.Get(types.PositionKey(*uint256.NewInt(id)))
	require.NotNil(t, bz)
	var position types.Position
	require.NoError(t, position.Unmarshal(bz))
	return position
}

func (env positionsEnv) books(t *testing.T) types.VenueBooks {
	t.Helper()
	var books types.VenueBooks
	if bz := env.store.Get(types.VenueBooksKey); bz != nil {
		require.NoError(t, books.Unmarshal(bz))
	}
	return books
}

func (env positionsEnv) order(t *testing.T, id uint64) types.Order {
	t.Helper()
	order, err := env.k.GetOrder(env.ctx, *uint256.NewInt(id))
	require.NoError(t, err)
	return order
}

// base is n x 1e13 base units, n lots of BTC.
func base(lots uint64) uint256.Int {
	var value uint256.Int
	value.Mul(uint256.NewInt(lots), uint256.NewInt(10_000_000_000_000))
	return value
}

func longBTC(id uint64, size uint256.Int, price int64, rank uint256.Int) types.Position {
	var cost uint256.Int
	cost.MulDivOverflow(&size, ptr(e18(price)), ptr(e18(1)))
	return types.Position{
		ID: *uint256.NewInt(id), Account: positionHolder, MarketID: 0, IsLong: true, SizeBase: size,
		ReferencePrice: e18(price), CostBasisUsd: cost, EntryPrice: e18(price), Leverage: e18(10),
		RankScore: rank, OpenedAt: uint64(listingTime - 100),
	}
}

func ptr(value uint256.Int) *uint256.Int { return &value }

func recordStrings(events []types.EventRecord) [][]string {
	out := make([][]string, 0, len(events))
	for _, event := range events {
		row := []string{event.Name}
		for _, arg := range event.Args {
			row = append(row, fmt.Sprint(arg))
		}
		out = append(out, row)
	}
	return out
}

func record(name string, args ...any) types.EventRecord {
	return types.EventRecord{Name: name, Args: args}
}

func dec(t *testing.T, value string) *big.Int {
	t.Helper()
	out, ok := new(big.Int).SetString(value, 10)
	require.True(t, ok)
	return out
}

func num(value int64) *big.Int { return big.NewInt(value) }

func usd(value int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(value), big.NewInt(1_000_000_000_000_000_000))
}

func TestPositionsRealisedPnl(t *testing.T) {
	t.Run("close at 61000 realises 10 USD net of the taker fee", func(t *testing.T) {
		env := newPositionsEnv(t, e18(61_000), uint256.Int{})
		env.setAccount(positionHolder, 100_000_000, 1, 0)
		env.setPosition(longBTC(7, base(1_000), 60_000, e18(5)))

		_, err := env.k.ClosePosition(env.ctx, positionOther, *uint256.NewInt(7))
		require.ErrorIs(t, err, types.ErrNotPositionHolder)
		_, err = env.k.ClosePosition(env.ctx, positionHolder, *uint256.NewInt(99))
		require.ErrorIs(t, err, types.ErrUnknownPosition)
		order, err := env.k.ClosePosition(env.ctx, positionHolder, *uint256.NewInt(7))
		require.NoError(t, err)
		require.Equal(t, types.Order{
			Account: positionHolder, MarketID: 0, IsLong: false, Type: types.OrderTypeMarket,
			TimeInForce: types.TimeInForceIOC, ReduceOnly: true, SizeUsd: e18(610), SizeBase: base(1_000),
			Leverage: e18(10),
		}, order)

		result, err := env.k.ApplyFill(env.ctx, keeper.PositionFill{
			Account: positionHolder, MarketID: 0, IsLong: order.IsLong, SizeBase: order.SizeBase, Price: e18(61_000),
			Leverage: order.Leverage,
		})
		require.NoError(t, err)
		require.Equal(t, *uint256.NewInt(7), result.PositionID)
		require.Equal(t, recordStrings([]types.EventRecord{
			record(types.EventPositionClosed, num(7), positionHolder, num(0), usd(610), usd(61_000),
				usd(10), true),
		}), recordStrings(result.Events))
		require.Equal(t, recordStrings([]types.EventRecord{
			record(types.EventTradeSettled, num(7), positionHolder, num(0), keeper.TradeTypeClose, usd(610),
				usd(61_000), usd(10), dec(t, "274500000000000000"), num(0), num(9_725_500)),
		}), recordStrings(result.TradeSettledEvents(dec(t, "274500000000000000"), num(274_500))))

		account := env.account(t, positionHolder)
		require.Equal(t, *uint256.NewInt(110_000_000), account.Collateral)
		require.Equal(t, uint64(0), account.PositionCount)
		require.True(t, account.DeficitUsd.IsZero())
		require.False(t, env.store.Has(types.PositionKey(*uint256.NewInt(7))))
		require.False(t, env.store.Has(types.AccountMarketPositionKey(positionHolder, 0)))
		require.False(t, env.store.Has(types.AdlRankIndexKey(0, types.SideBuy, e18(5), *uint256.NewInt(7))))
		require.Equal(t, uint256.Int{}, env.books(t).ResidueUsd)
	})

	t.Run("funding since the funding basis is settled before the PnL", func(t *testing.T) {
		env := newPositionsEnv(t, e18(61_000), wordOf(t, "10750000000000000000"))
		env.setAccount(positionHolder, 100_000_000, 1, 0)
		position := longBTC(7, base(1_000), 60_000, e18(5))
		position.FundingBasis = wordOf(t, "100000000000000000")
		env.setPosition(position)

		result, err := env.k.ApplyFill(env.ctx, keeper.PositionFill{
			Account: positionHolder, MarketID: 0, IsLong: false, SizeBase: base(1_000), Price: e18(61_000),
			Leverage: e18(10),
		})
		require.NoError(t, err)
		require.Equal(t, recordStrings([]types.EventRecord{
			record(types.EventPositionFundingApplied, num(7), positionHolder, num(0), dec(t, "-7500000000000000"),
				dec(t, "99992500000000000000"), num(99_992_500)),
			record(types.EventPositionClosed, num(7), positionHolder, num(0), usd(610), usd(61_000),
				usd(10), true),
		}), recordStrings(result.Events))
		require.Equal(t, recordStrings([]types.EventRecord{
			record(types.EventTradeSettled, num(7), positionHolder, num(0), keeper.TradeTypeClose, usd(610),
				usd(61_000), usd(10), dec(t, "274500000000000000"), dec(t, "-7500000000000000"),
				num(9_718_000)),
		}), recordStrings(result.TradeSettledEvents(dec(t, "274500000000000000"), num(274_500))))
		require.Equal(t, *uint256.NewInt(109_992_500), env.account(t, positionHolder).Collateral)
		require.Equal(t, uint256.Int{}, env.books(t).ResidueUsd)
	})

	t.Run("a loss beyond collateral records the deficit", func(t *testing.T) {
		env := newPositionsEnv(t, e18(50_000), uint256.Int{})
		env.setAccount(positionHolder, 40_000_000, 1, 0)
		env.setPosition(longBTC(7, base(1_000), 60_000, e18(5)))

		result, err := env.k.ApplyFill(env.ctx, keeper.PositionFill{
			Account: positionHolder, MarketID: 0, IsLong: false, SizeBase: base(1_000), Price: e18(55_000),
			Leverage: e18(10), Kind: keeper.FillKindLiquidation,
		})
		require.NoError(t, err)
		require.Equal(t, recordStrings([]types.EventRecord{
			record(types.EventTradeSettled, num(7), positionHolder, num(0), keeper.TradeTypeLiquidation, usd(550),
				usd(55_000), dec(t, "-50000000000000000000"), num(0), num(0), num(0)),
		}), recordStrings(result.TradeSettledEvents(num(0), num(0))))
		account := env.account(t, positionHolder)
		require.True(t, account.Collateral.IsZero())
		require.Equal(t, e18(10), account.DeficitUsd)
		require.Equal(t, e18(10), env.books(t).DeficitSumUsd)
	})
}

func TestPositionsWeightedEntry(t *testing.T) {
	env := newPositionsEnv(t, e18(62_000), uint256.Int{})
	env.setAccount(positionHolder, 100_000_000, 1, 0)
	env.setPosition(longBTC(7, base(1_650), 60_000, e18(5)))

	result, err := env.k.ApplyFill(env.ctx, keeper.PositionFill{
		Account: positionHolder, MarketID: 0, IsLong: true, SizeBase: base(798), Price: e18(62_000), Leverage: e18(10),
	})
	require.NoError(t, err)
	entry := wordOf(t, "60651960784313725490196")
	cost := wordOf(t, "1517760000000000000000")
	require.Equal(t, *uint256.NewInt(7), result.PositionID)
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventPositionEntryPriceChanged, num(7), usd(60_000), entry.ToBig()),
		record(types.EventPositionModified, num(7), cost.ToBig(), usd(133), num(133_000_000)),
	}), recordStrings(result.Events))
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventTradeSettled, num(7), positionHolder, num(0), keeper.TradeTypeIncrease,
			dec(t, "494760000000000000000"), usd(62_000), usd(33), dec(t, "222642000000000000"), num(0),
			num(32_777_358)),
	}), recordStrings(result.TradeSettledEvents(dec(t, "222642000000000000"), num(222_642))))

	rank := wordOf(t, "11665364299782025985")
	position := env.position(t, 7)
	require.Equal(t, types.Position{
		ID: *uint256.NewInt(7), Account: positionHolder, MarketID: 0, IsLong: true, SizeBase: base(2_448),
		ReferencePrice: e18(62_000), CostBasisUsd: cost, EntryPrice: entry, Leverage: e18(10), RankScore: rank,
		OpenedAt: uint64(listingTime - 100),
	}, position)
	require.Equal(t, *uint256.NewInt(133_000_000), env.account(t, positionHolder).Collateral)
	require.False(t, env.store.Has(types.AdlRankIndexKey(0, types.SideBuy, e18(5), *uint256.NewInt(7))))
	require.True(t, env.store.Has(types.AdlRankIndexKey(0, types.SideBuy, rank, *uint256.NewInt(7))))
}

// TestPositionsModifiedAfterFee books the taker fee of the increase of
// TestPositionsWeightedEntry, 494.76 USD at 450 ppm, inside ApplyFill:
// PositionModified carries the collateral net of the 0.222642 USDL fee.
func TestPositionsModifiedAfterFee(t *testing.T) {
	env := newPositionsEnv(t, e18(62_000), uint256.Int{})
	env.setAccount(positionHolder, 100_000_000, 1, 0)
	env.setPosition(longBTC(7, base(1_650), 60_000, e18(5)))

	result, err := env.k.ApplyFill(env.ctx, keeper.PositionFill{
		Account: positionHolder, MarketID: 0, IsLong: true, SizeBase: base(798), Price: e18(62_000), Leverage: e18(10),
		Fee: keeper.FeeRoleTaker,
	})
	require.NoError(t, err)
	require.Equal(t, keeper.FeeTypeTrading, result.Fee.FeeType)
	require.Equal(t, wordOf(t, "222642000000000000"), result.Fee.FeeUsd)
	require.Equal(t, *uint256.NewInt(222_642), result.Fee.Tokens)
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventPositionEntryPriceChanged, num(7), usd(60_000), dec(t, "60651960784313725490196")),
		record(types.EventPositionModified, num(7), dec(t, "1517760000000000000000"),
			dec(t, "132777358000000000000"), num(132_777_358)),
	}), recordStrings(result.Events))
	require.Equal(t, *uint256.NewInt(132_777_358), env.account(t, positionHolder).Collateral)
}

func TestPositionsNettingFlip(t *testing.T) {
	env := newPositionsEnv(t, e18(60_000), uint256.Int{})
	env.setAccount(positionHolder, 100_000_000, 1, 0)
	env.setPosition(longBTC(7, base(2_000), 60_000, e18(5)))
	next := uint256.NewInt(8).Bytes32()
	env.store.Set(types.SequenceKey(types.SequencePositionID), next[:])

	fill := keeper.PositionFill{
		Account: positionHolder, MarketID: 0, IsLong: false, SizeBase: base(3_000), Price: e18(60_000), Leverage: e18(5),
	}
	refused := fill
	refused.SizeBase = *uint256.NewInt(15_000_000_000_000)
	_, err := env.k.ApplyFill(env.ctx, refused)
	require.ErrorIs(t, err, types.ErrLotSize)
	refused = fill
	refused.Account = positionOther
	_, err = env.k.ApplyFill(env.ctx, refused)
	require.ErrorIs(t, err, types.ErrNoAccount)
	refused = fill
	refused.SizeBase = uint256.Int{}
	_, err = env.k.ApplyFill(env.ctx, refused)
	require.ErrorIs(t, err, types.ErrZeroAmount)

	result, err := env.k.ApplyFill(env.ctx, fill)
	require.NoError(t, err)
	params, err := env.k.GetParams(env.ctx)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(8), result.PositionID)
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventPositionClosed, num(7), positionHolder, num(0), usd(1_200), usd(60_000),
			num(0), true),
		record(types.EventPositionOpened, num(8), positionHolder, num(0), false, usd(600), usd(5),
			usd(60_000), params.CollateralToken, num(100_000_000)),
	}), recordStrings(result.Events))
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventTradeSettled, num(7), positionHolder, num(0), keeper.TradeTypeClose, usd(1_200),
			usd(60_000), num(0), num(0), num(0), num(0)),
		record(types.EventTradeSettled, num(8), positionHolder, num(0), keeper.TradeTypeOpen, usd(600),
			usd(60_000), num(0), dec(t, "810000000000000000"), num(0), num(0)),
	}), recordStrings(result.TradeSettledEvents(dec(t, "810000000000000000"), num(810_000))))

	require.False(t, env.store.Has(types.PositionKey(*uint256.NewInt(7))))
	require.False(t, env.store.Has(types.AdlRankIndexKey(0, types.SideBuy, e18(5), *uint256.NewInt(7))))
	id := uint256.NewInt(8).Bytes32()
	require.Equal(t, id[:], env.store.Get(types.AccountMarketPositionKey(positionHolder, 0)))
	require.Equal(t, types.Position{
		ID: *uint256.NewInt(8), Account: positionHolder, MarketID: 0, IsLong: false, SizeBase: base(1_000),
		ReferencePrice: e18(60_000), CostBasisUsd: e18(600), EntryPrice: e18(60_000), Leverage: e18(5),
		RankScore: e18(6), OpenedAt: uint64(listingTime),
	}, env.position(t, 8))
	require.True(t, env.store.Has(types.AdlRankIndexKey(0, types.SideSell, e18(6), *uint256.NewInt(8))))
	account := env.account(t, positionHolder)
	require.Equal(t, uint64(1), account.PositionCount)
	require.Equal(t, *uint256.NewInt(100_000_000), account.Collateral)
	require.Equal(t, uint256.Int{}, env.books(t).ResidueUsd)
}

func TestPositionsReduceOnlyShrink(t *testing.T) {
	env := newPositionsEnv(t, e18(61_000), uint256.Int{})
	env.setAccount(positionHolder, 100_000_000, 1, 5)
	env.setPosition(longBTC(7, base(1_000), 60_000, e18(5)))

	resting := func(id uint64, market uint64, isLong, reduceOnly bool, price int64, size, filled uint256.Int,
		sizeUsd uint256.Int) types.Order {
		order := types.Order{
			ID: *uint256.NewInt(id), Account: positionHolder, MarketID: market, IsLong: isLong, Type: types.OrderTypeLimit,
			TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, ReduceOnly: reduceOnly,
			LimitPrice: e18(price), TriggerPrice: e18(price), SizeUsd: sizeUsd, SizeBase: size, FilledBase: filled,
			Leverage: e18(10), Sequence: id,
		}
		require.NoError(t, env.k.BookInsert(env.ctx, order))
		env.store.Set(types.AccountOrderKey(positionHolder, order.ID), []byte{})
		return order
	}
	resting(1, 0, false, true, 61_000, base(2_000), uint256.Int{}, e18(1_220))
	untouched := resting(2, 0, false, false, 61_000, base(2_000), uint256.Int{}, e18(1_220))
	otherMarket := resting(3, 1, false, true, 61_000, base(2_000), uint256.Int{}, e18(1_220))
	resting(4, 0, true, true, 60_000, base(500), uint256.Int{}, e18(300))
	resting(5, 0, false, true, 62_000, base(2_000), base(500), e18(1_240))

	events, err := env.k.ReconcileReduceOnly(env.ctx, positionHolder, 0)
	require.NoError(t, err)
	require.Equal(t, recordStrings([]types.EventRecord{
		record(types.EventOrderCancelled, num(4), positionHolder),
	}), recordStrings(events))

	shrunk := env.order(t, 1)
	require.Equal(t, base(1_000), shrunk.SizeBase)
	require.Equal(t, e18(610), shrunk.SizeUsd)
	require.Equal(t, types.OrderStatusResting, shrunk.Status)
	partly := env.order(t, 5)
	require.Equal(t, base(1_500), partly.SizeBase)
	require.Equal(t, e18(930), partly.SizeUsd)
	require.Equal(t, base(500), partly.FilledBase)
	require.Equal(t, untouched, env.order(t, 2))
	require.Equal(t, otherMarket, env.order(t, 3))

	cancelled := env.order(t, 4)
	require.Equal(t, types.OrderStatusCancelled, cancelled.Status)
	require.False(t, env.store.Has(types.AccountOrderKey(positionHolder, *uint256.NewInt(4))))
	var bids []uint64
	require.NoError(t, env.k.IterateBook(env.ctx, 0, types.SideBuy, func(order types.Order) bool {
		bids = append(bids, order.ID.Uint64())
		return false
	}))
	require.Empty(t, bids)
	require.Equal(t, uint64(4), env.account(t, positionHolder).OpenOrderCount)

	partial, err := env.k.PartialClose(env.ctx, positionHolder, *uint256.NewInt(7), e18(305))
	require.NoError(t, err)
	require.Equal(t, base(500), partial.SizeBase)
	require.Equal(t, e18(305), partial.SizeUsd)
	require.True(t, partial.ReduceOnly)
	require.False(t, partial.IsLong)
	require.Equal(t, types.OrderTypeMarket, partial.Type)
	require.Equal(t, types.TimeInForceIOC, partial.TimeInForce)
	rounded, err := env.k.PartialClose(env.ctx, positionHolder, *uint256.NewInt(7), e18(300))
	require.NoError(t, err)
	require.Equal(t, base(491), rounded.SizeBase)
	require.Equal(t, wordOf(t, "299510000000000000000"), rounded.SizeUsd)
	_, err = env.k.PartialClose(env.ctx, positionHolder, *uint256.NewInt(7), e18(611))
	require.ErrorIs(t, err, types.ErrCloseSizeExceedsPosition)
	_, err = env.k.PartialClose(env.ctx, positionHolder, *uint256.NewInt(7), *uint256.NewInt(100_000_000_000_000))
	require.ErrorIs(t, err, types.ErrLotSize)
	_, err = env.k.PartialClose(env.ctx, positionHolder, *uint256.NewInt(7), uint256.Int{})
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, err = env.k.PartialClose(env.ctx, positionOther, *uint256.NewInt(7), e18(305))
	require.ErrorIs(t, err, types.ErrNotPositionHolder)
}
