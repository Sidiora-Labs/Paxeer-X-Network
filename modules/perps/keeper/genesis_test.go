package keeper_test

import (
	"encoding/json"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	genesisA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	genesisB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	genesisC = common.HexToAddress("0x00000000000000000000000000000000000000c3")
	genesisD = common.HexToAddress("0x00000000000000000000000000000000000000d4")
)

func genesisWord(value uint64) uint256.Int { return *uint256.NewInt(value) }

func genesisE18(value uint64) uint256.Int {
	var out uint256.Int
	out.Mul(uint256.NewInt(value), uint256.NewInt(1_000_000_000_000_000_000))
	return out
}

func wordValue(value uint64) []byte {
	word := genesisWord(value).Bytes32()
	return word[:]
}

// scenarioGenesis holds 3 markets, 10 orders and 4 positions: four resting
// limits (two with a cloid, two with a deadline), a stop market, a stop
// limit, the attached take-profit and stop-loss of position 1, a cancelled
// and a filled order, and every non-derived prefix besides.
func scenarioGenesis() types.GenesisState {
	gs := *types.DefaultGenesis()
	gs.Markets = []types.Market{classEMarket(0, "BTC"), classEMarket(3, "ETH"), classEMarket(7, "SOL")}
	gs.MarketStates = []types.MarketState{
		{MarketID: 0, IndexPrice: genesisE18(60_000), PricefeedMark: genesisE18(60_010), PriceTimestampMs: 1_800_000_000_000,
			Fresh: true, MarkPrice: genesisE18(60_005), LastTradePrice: genesisE18(60_000), FundingIndex: genesisE18(2),
			LongOpenInterest: genesisE18(3), ShortOpenInterest: genesisE18(3), LastIndexHour: 500_000},
		{MarketID: 3, IndexPrice: genesisE18(3_000), MarkPrice: genesisE18(3_000), Fresh: true,
			LongOpenInterest: genesisE18(10), ShortOpenInterest: genesisE18(10)},
	}
	gs.Accounts = []types.Account{
		{Address: genesisA, Collateral: genesisWord(900_000_000), DeficitUsd: genesisE18(3), OpenOrderCount: 4, PositionCount: 2,
			MarginRatioBucket: 140, Created: true},
		{Address: genesisB, Collateral: genesisWord(500_000_000), OpenOrderCount: 2, PositionCount: 1, MarginRatioBucket: 65_535,
			Created: true},
		{Address: genesisC, Collateral: genesisWord(300_000_000), DeficitUsd: genesisE18(2), OpenOrderCount: 2, PositionCount: 1,
			MarginRatioBucket: 0, Created: true},
		{Address: genesisD, Collateral: genesisWord(100_000_000), Created: true},
	}
	gs.Orders = []types.Order{
		{ID: genesisWord(1), Account: genesisA, MarketID: 0, IsLong: true, Type: types.OrderTypeLimit,
			Status: types.OrderStatusResting, LimitPrice: genesisE18(59_000), SizeBase: genesisE18(1), Leverage: genesisE18(10),
			Deadline: 1_800_000_100, Cloid: [16]byte{0xc1}, Sequence: 1},
		{ID: genesisWord(2), Account: genesisB, MarketID: 0, Type: types.OrderTypeLimit, Status: types.OrderStatusResting,
			LimitPrice: genesisE18(61_000), SizeBase: genesisE18(2), FilledBase: genesisE18(1), Leverage: genesisE18(5), Sequence: 2},
		{ID: genesisWord(3), Account: genesisC, MarketID: 3, IsLong: true, Type: types.OrderTypeLimit,
			TimeInForce: types.TimeInForcePostOnly, Status: types.OrderStatusResting, LimitPrice: genesisE18(2_990),
			SizeBase: genesisE18(4), Leverage: genesisE18(20), Cloid: [16]byte{0xc2}, Sequence: 3},
		{ID: genesisWord(4), Account: genesisA, MarketID: 0, IsLong: true, Type: types.OrderTypeStopMarket,
			TimeInForce: types.TimeInForceIOC, Status: types.OrderStatusUntriggered, TriggerPrice: genesisE18(62_000),
			SizeBase: genesisE18(1), Leverage: genesisE18(10), Sequence: 4},
		{ID: genesisWord(5), Account: genesisB, MarketID: 0, Type: types.OrderTypeStopLimit, Status: types.OrderStatusUntriggered,
			LimitPrice: genesisE18(57_900), TriggerPrice: genesisE18(58_000), SizeBase: genesisE18(1), Leverage: genesisE18(5),
			Deadline: 1_800_000_300, Sequence: 5},
		{ID: genesisWord(6), Account: genesisA, MarketID: 0, Type: types.OrderTypeTakeProfit, TimeInForce: types.TimeInForceIOC,
			Status: types.OrderStatusUntriggered, ReduceOnly: true, TriggerPrice: genesisE18(66_000), SizeBase: genesisE18(3),
			Leverage: genesisE18(10), Sequence: 6},
		{ID: genesisWord(7), Account: genesisA, MarketID: 0, Type: types.OrderTypeStopLoss, TimeInForce: types.TimeInForceIOC,
			Status: types.OrderStatusUntriggered, ReduceOnly: true, TriggerPrice: genesisE18(57_000), SizeBase: genesisE18(3),
			Leverage: genesisE18(10), Sequence: 7},
		{ID: genesisWord(8), Account: genesisC, MarketID: 7, Type: types.OrderTypeLimit, Status: types.OrderStatusResting,
			LimitPrice: genesisE18(150), SizeBase: genesisE18(40), Leverage: genesisE18(50), Deadline: 1_800_000_200, Sequence: 8},
		{ID: genesisWord(9), Account: genesisD, MarketID: 7, IsLong: true, Type: types.OrderTypeLimit,
			Status: types.OrderStatusCancelled, LimitPrice: genesisE18(140), SizeBase: genesisE18(10), Leverage: genesisE18(5),
			Deadline: 1_800_000_050, Cloid: [16]byte{0xc3}, Sequence: 9},
		{ID: genesisWord(10), Account: genesisB, MarketID: 3, IsLong: true, Type: types.OrderTypeMarket,
			TimeInForce: types.TimeInForceIOC, Status: types.OrderStatusFilled, SizeBase: genesisE18(10), FilledBase: genesisE18(10),
			Leverage: genesisE18(5), Sequence: 10},
	}
	gs.AccountOrders = []types.AccountOrderEntry{
		{Account: genesisA, OrderID: genesisWord(1)}, {Account: genesisB, OrderID: genesisWord(2)},
		{Account: genesisC, OrderID: genesisWord(3)}, {Account: genesisA, OrderID: genesisWord(4)},
		{Account: genesisB, OrderID: genesisWord(5)}, {Account: genesisA, OrderID: genesisWord(6)},
		{Account: genesisA, OrderID: genesisWord(7)}, {Account: genesisC, OrderID: genesisWord(8)},
	}
	gs.Positions = []types.Position{
		{ID: genesisWord(1), Account: genesisA, MarketID: 0, IsLong: true, SizeBase: genesisE18(3),
			ReferencePrice: genesisE18(60_000), CostBasisUsd: genesisE18(180_000), FundingBasis: genesisE18(6),
			EntryPrice: genesisE18(60_000), Leverage: genesisE18(10), RankScore: genesisE18(7), OpenedAt: 1_800_000_000},
		{ID: genesisWord(2), Account: genesisB, MarketID: 0, SizeBase: genesisE18(3), ReferencePrice: genesisE18(60_000),
			CostBasisUsd: genesisE18(180_000), EntryPrice: genesisE18(60_000), Leverage: genesisE18(5), RankScore: genesisE18(2),
			OpenedAt: 1_800_000_000},
		{ID: genesisWord(3), Account: genesisA, MarketID: 3, IsLong: true, SizeBase: genesisE18(10),
			ReferencePrice: genesisE18(3_000), CostBasisUsd: genesisE18(30_000), EntryPrice: genesisE18(3_000),
			Leverage: genesisE18(20), RankScore: genesisE18(4), OpenedAt: 1_800_000_010},
		{ID: genesisWord(4), Account: genesisC, MarketID: 3, SizeBase: genesisE18(10), ReferencePrice: genesisE18(3_000),
			CostBasisUsd: genesisE18(30_000), EntryPrice: genesisE18(3_000), Leverage: genesisE18(20), RankScore: genesisE18(4),
			OpenedAt: 1_800_000_010},
	}
	gs.AccountMarketPositions = []types.AccountMarketPositionEntry{
		{Account: genesisA, MarketID: 0, PositionID: genesisWord(1)}, {Account: genesisB, MarketID: 0, PositionID: genesisWord(2)},
		{Account: genesisA, MarketID: 3, PositionID: genesisWord(3)}, {Account: genesisC, MarketID: 3, PositionID: genesisWord(4)},
	}
	gs.VolumeBuckets = []types.VolumeBucket{
		{Account: genesisA, Day: 20_833, TakerVolumeUsd: genesisE18(180_000), MakerVolumeUsd: genesisE18(30_000)},
		{Account: genesisB, Day: 20_833, MakerVolumeUsd: genesisE18(180_000)},
	}
	gs.VenueVolumeBuckets = []types.VenueVolumeBucket{{Day: 20_833, VolumeUsd: genesisE18(210_000)}}
	gs.LiquidationCooldowns = []types.LiquidationCooldown{{PositionID: genesisWord(2), LastSliceTime: 1_800_000_050}}
	gs.AttachedTpSls = []types.AttachedTpSl{
		{PositionID: genesisWord(1), TakeProfitOrderID: genesisWord(6), StopLossOrderID: genesisWord(7)},
	}
	gs.Counters = types.Counters{NextOrderID: genesisWord(11), NextPositionID: genesisWord(5), NextSequence: 11}
	gs.SweepCursor = genesisWord(2)
	gs.VenueBooks = types.VenueBooks{ResidueUsd: genesisWord(17), DeficitSumUsd: genesisE18(5)}
	return gs
}

type storeEntry struct{ key, value []byte }

func storeContents(t *testing.T, store sdk.KVStore) []storeEntry {
	t.Helper()
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	var out []storeEntry
	for ; iterator.Valid(); iterator.Next() {
		out = append(out, storeEntry{key: append([]byte(nil), iterator.Key()...), value: append([]byte{}, iterator.Value()...)})
	}
	return out
}

// importGenesis runs InitGenesis on a fresh store and returns the keeper,
// its context and its store.
func importGenesis(t *testing.T, gs types.GenesisState) (*keeper.Keeper, sdk.Context, sdk.KVStore) {
	t.Helper()
	k, ctx, key := newKeeper(t)
	k.InitGenesis(ctx, gs)
	return k, ctx, ctx.KVStore(key)
}

func genesisPanic(t *testing.T, run func()) (err error) {
	t.Helper()
	defer func() {
		recovered := recover()
		require.NotNil(t, recovered, "InitGenesis did not panic")
		var ok bool
		err, ok = recovered.(error)
		require.True(t, ok, "InitGenesis panicked with %v", recovered)
	}()
	run()
	return nil
}

func TestGenesisRoundTrip(t *testing.T) {
	kA, ctxA, storeA := importGenesis(t, scenarioGenesis())
	kA.SetMarketState(ctxA, types.MarketState{MarketID: 7, IndexPrice: genesisE18(150), MarkPrice: genesisE18(151), Fresh: true})
	orderID, err := kA.NextOrderID(ctxA)
	require.NoError(t, err)
	require.Equal(t, genesisWord(11), orderID)

	require.Equal(t, wordValue(1), storeA.Get(types.BookKey(0, types.SideBuy, types.BookPriceKey(true, genesisE18(59_000)), 1)))
	require.Equal(t, wordValue(2), storeA.Get(types.BookKey(0, types.SideSell, types.BookPriceKey(false, genesisE18(61_000)), 2)))
	require.Equal(t, wordValue(8), storeA.Get(types.BookKey(7, types.SideSell, types.BookPriceKey(false, genesisE18(150)), 8)))
	require.Equal(t, wordValue(4),
		storeA.Get(types.TriggerKey(0, types.TriggerRising, types.BookPriceKey(false, genesisE18(62_000)), 4)))
	require.Equal(t, wordValue(5),
		storeA.Get(types.TriggerKey(0, types.TriggerFalling, types.BookPriceKey(true, genesisE18(58_000)), 5)))
	require.Equal(t, wordValue(6),
		storeA.Get(types.TriggerKey(0, types.TriggerRising, types.BookPriceKey(false, genesisE18(66_000)), 6)))
	require.Equal(t, wordValue(7),
		storeA.Get(types.TriggerKey(0, types.TriggerFalling, types.BookPriceKey(true, genesisE18(57_000)), 7)))
	require.Equal(t, wordValue(1), storeA.Get(types.CloidKey(genesisA, 0, [16]byte{0xc1})))
	require.False(t, storeA.Has(types.CloidKey(genesisD, 7, [16]byte{0xc3})))
	require.True(t, storeA.Has(types.ExpiryKey(1_800_000_100, genesisWord(1))))
	require.True(t, storeA.Has(types.ExpiryKey(1_800_000_300, genesisWord(5))))
	require.False(t, storeA.Has(types.ExpiryKey(1_800_000_050, genesisWord(9))))
	require.True(t, storeA.Has(types.MarginRatioIndexKey(65_535, genesisB)))
	require.False(t, storeA.Has(types.MarginRatioIndexKey(0, genesisD)))
	require.True(t, storeA.Has(types.AdlRankIndexKey(3, types.SideSell, genesisE18(4), genesisWord(4))))

	exported := kA.ExportGenesis(ctxA)
	require.Empty(t, exported.BookEntries)
	require.Empty(t, exported.CloidEntries)
	require.Empty(t, exported.TriggerEntries)
	require.Empty(t, exported.ExpiryEntries)
	require.Empty(t, exported.MarginRatioEntries)
	require.Empty(t, exported.AdlRankEntries)
	require.Len(t, exported.Markets, 3)
	require.Len(t, exported.MarketStates, 3)
	require.Len(t, exported.Orders, 10)
	require.Len(t, exported.Positions, 4)
	require.Equal(t, types.Counters{NextOrderID: genesisWord(12), NextPositionID: genesisWord(5), NextSequence: 11},
		exported.Counters)

	bz, err := json.Marshal(exported)
	require.NoError(t, err)
	var imported types.GenesisState
	require.NoError(t, json.Unmarshal(bz, &imported))
	kB, ctxB, storeB := importGenesis(t, imported)

	contentsA := storeContents(t, storeA)
	require.Len(t, contentsA, 67)
	require.Equal(t, contentsA, storeContents(t, storeB))
	require.Equal(t, exported, kB.ExportGenesis(ctxB))

	t.Run("supplied index entries equal to the rebuilt ones", func(t *testing.T) {
		supplied := scenarioGenesis()
		supplied.BookEntries = []types.BookEntry{
			{MarketID: 7, Side: types.SideSell, Price: genesisE18(150), Sequence: 8, OrderID: genesisWord(8)},
			{MarketID: 0, Side: types.SideBuy, Price: genesisE18(59_000), Sequence: 1, OrderID: genesisWord(1)},
			{MarketID: 3, Side: types.SideBuy, Price: genesisE18(2_990), Sequence: 3, OrderID: genesisWord(3)},
			{MarketID: 0, Side: types.SideSell, Price: genesisE18(61_000), Sequence: 2, OrderID: genesisWord(2)},
		}
		_, _, rebuilt := importGenesis(t, scenarioGenesis())
		_, _, fromSupplied := importGenesis(t, supplied)
		require.Equal(t, storeContents(t, rebuilt), storeContents(t, fromSupplied))

		supplied.BookEntries[1].Price = genesisE18(59_001)
		k, ctx, key := newKeeper(t)
		err := genesisPanic(t, func() { k.InitGenesis(ctx, supplied) })
		require.ErrorIs(t, err, types.ErrInvalidGenesis)
		require.Contains(t, err.Error(), "LXP_PERPS_INVALID_GENESIS: book entry ")
		require.Contains(t, err.Error(), " disagrees with the records")
		require.Empty(t, storeContents(t, ctx.KVStore(key)))

		supplied.BookEntries = supplied.BookEntries[:3]
		err = genesisPanic(t, func() { k.InitGenesis(ctx, supplied) })
		require.ErrorIs(t, err, types.ErrInvalidGenesis)
		require.EqualError(t, err, "LXP_PERPS_INVALID_GENESIS: 3 book entries where the records give 4")
	})
}

func TestGenesisRejectsDuplicateMarket(t *testing.T) {
	gs := *types.DefaultGenesis()
	gs.Markets = []types.Market{classEMarket(3, "ETH"), classEMarket(3, "ETH2")}
	k, ctx, key := newKeeper(t)
	err := genesisPanic(t, func() { k.InitGenesis(ctx, gs) })
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.EqualError(t, err, "LXP_PERPS_INVALID_GENESIS: duplicate market 3")
	require.Empty(t, storeContents(t, ctx.KVStore(key)))
}
