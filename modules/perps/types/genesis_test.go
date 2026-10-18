package types_test

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
)

var (
	holderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	holderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	holderC = common.HexToAddress("0x00000000000000000000000000000000000000c3")
	cloidA  = [16]byte{0xc1}
)

func word(value uint64) uint256.Int { return *uint256.NewInt(value) }

func e18(value uint64) uint256.Int {
	var out uint256.Int
	out.Mul(uint256.NewInt(value), uint256.NewInt(1_000_000_000_000_000_000))
	return out
}

// validGenesis holds a resting limit order with a cloid and an expiry, an
// untriggered reduce-only stop-loss attached to a long position with its indexes and the volume
// buckets of its account.
func validGenesis() types.GenesisState {
	gs := *types.DefaultGenesis()
	gs.Markets = []types.Market{btcMarket(), paxMarket()}
	gs.MarketStates = []types.MarketState{{MarketID: 0, IndexPrice: e18(60_000), MarkPrice: e18(60_000), Fresh: true}}
	gs.Accounts = []types.Account{
		{Address: holderA, Collateral: word(250_000_000), DeficitUsd: e18(3), OpenOrderCount: 2, PositionCount: 1, MarginRatioBucket: 140, Created: true},
		{Address: holderB, Collateral: word(100_000_000), DeficitUsd: e18(2), Created: true},
	}
	gs.Orders = []types.Order{
		{
			ID: word(1), Account: holderA, MarketID: 0, IsLong: true, Type: types.OrderTypeLimit, Status: types.OrderStatusResting,
			LimitPrice: e18(59_000), SizeBase: word(101_690_000_000_000_000), Leverage: e18(10), Deadline: 1_800_000_100,
			Cloid: cloidA, Sequence: 1,
		},
		{
			ID: word(3), Account: holderA, MarketID: 0, IsLong: false, Type: types.OrderTypeStopLoss,
			TimeInForce: types.TimeInForceIOC, Status: types.OrderStatusUntriggered, ReduceOnly: true, TriggerPrice: e18(57_000),
			SizeBase: word(16_500_000_000_000_000), Leverage: e18(10), Sequence: 2,
		},
	}
	gs.BookEntries = []types.BookEntry{{MarketID: 0, Side: types.SideBuy, Price: e18(59_000), Sequence: 1, OrderID: word(1)}}
	gs.TriggerEntries = []types.TriggerEntry{{MarketID: 0, Direction: types.TriggerFalling, TriggerPrice: e18(57_000), Sequence: 2, OrderID: word(3)}}
	gs.CloidEntries = []types.CloidEntry{{Account: holderA, MarketID: 0, Cloid: cloidA, OrderID: word(1)}}
	gs.AccountOrders = []types.AccountOrderEntry{{Account: holderA, OrderID: word(1)}, {Account: holderA, OrderID: word(3)}}
	gs.ExpiryEntries = []types.ExpiryEntry{{Deadline: 1_800_000_100, OrderID: word(1)}}
	gs.Positions = []types.Position{{
		ID: word(7), Account: holderA, MarketID: 0, IsLong: true, SizeBase: word(16_500_000_000_000_000),
		ReferencePrice: e18(60_000), CostBasisUsd: e18(990), EntryPrice: e18(60_000), Leverage: e18(10),
		RankScore: e18(5), OpenedAt: 1_800_000_000,
	}}
	gs.AccountMarketPositions = []types.AccountMarketPositionEntry{{Account: holderA, MarketID: 0, PositionID: word(7)}}
	gs.LiquidationCooldowns = []types.LiquidationCooldown{{PositionID: word(7), LastSliceTime: 1_800_000_050,
		SliceBasis: word(16_500_000_000_000_000)}}
	gs.AttachedTpSls = []types.AttachedTpSl{{PositionID: word(7), StopLossOrderID: word(3)}}
	gs.MarginRatioEntries = []types.MarginRatioEntry{{Bucket: 140, Account: holderA}}
	gs.AdlRankEntries = []types.AdlRankEntry{{MarketID: 0, Side: types.SideBuy, RankScore: e18(5), PositionID: word(7)}}
	gs.VolumeBuckets = []types.VolumeBucket{{Account: holderA, Day: 20_833, TakerVolumeUsd: e18(990)}}
	gs.VenueVolumeBuckets = []types.VenueVolumeBucket{{Day: 20_833, VolumeUsd: e18(990)}}
	gs.Counters = types.Counters{NextOrderID: word(4), NextPositionID: word(8), NextSequence: 3}
	gs.SweepCursor = word(7)
	gs.VenueBooks = types.VenueBooks{ResidueUsd: word(17), DeficitSumUsd: e18(5)}
	return gs
}

func TestGenesisValidateAccepts(t *testing.T) {
	require.NoError(t, types.ValidateGenesis(*types.DefaultGenesis()))
	require.NoError(t, types.ValidateGenesis(validGenesis()))
}

func TestGenesisValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.GenesisState)
		reason string
	}{
		{"invalid params", func(gs *types.GenesisState) { gs.Params.CollateralToken = common.Address{} },
			"LXP_PERPS_INVALID_GENESIS: LXP_PERPS_INVALID_PARAMS: zero collateral token"},
		{"invalid market", func(gs *types.GenesisState) { gs.Markets[1].MaxLeverage = 0 },
			"LXP_PERPS_INVALID_GENESIS: LXP_PERPS_INVALID_PARAMS: market 11 max leverage must be in [1, 1000]"},
		{"duplicate market", func(gs *types.GenesisState) { gs.Markets[1] = gs.Markets[0] },
			"duplicate market 0"},
		{"market state of unknown market", func(gs *types.GenesisState) { gs.MarketStates[0].MarketID = 5 },
			"market state of unknown market 5"},
		{"duplicate market state", func(gs *types.GenesisState) { gs.MarketStates = append(gs.MarketStates, gs.MarketStates[0]) },
			"duplicate market state 0"},
		{"zero account address", func(gs *types.GenesisState) { gs.Accounts[1].Address = common.Address{} },
			"account with the zero address"},
		{"duplicate account", func(gs *types.GenesisState) { gs.Accounts[1].Address = holderA },
			"duplicate account " + holderA.Hex()},
		{"deficit sum mismatch", func(gs *types.GenesisState) { gs.VenueBooks.DeficitSumUsd = e18(4) },
			"venue deficit sum 4000000000000000000 differs from the account deficits 5000000000000000000"},
		{"order id 0", func(gs *types.GenesisState) { gs.Orders[0].ID = word(0) },
			"order with id 0"},
		{"duplicate order", func(gs *types.GenesisState) { gs.Orders[1].ID = word(1) },
			"duplicate order 1"},
		{"duplicate sequence", func(gs *types.GenesisState) { gs.Orders[1].Sequence = 1 },
			"duplicate placement sequence 1"},
		{"order of unknown market", func(gs *types.GenesisState) { gs.Orders[0].MarketID = 9 },
			"order 1 of unknown market 9"},
		{"order without its account", func(gs *types.GenesisState) {
			gs.Orders[0].Account = holderB
			gs.Accounts = gs.Accounts[:1]
			gs.VenueBooks.DeficitSumUsd = e18(3)
		},
			"order 1 without its account " + holderB.Hex()},
		{"unknown order type", func(gs *types.GenesisState) { gs.Orders[0].Type = types.OrderTypeStopMarket + 1 },
			"order 1 has an unknown type, time in force or status"},
		{"unknown time in force", func(gs *types.GenesisState) { gs.Orders[0].TimeInForce = types.TimeInForcePostOnly + 1 },
			"order 1 has an unknown type, time in force or status"},
		{"unknown status", func(gs *types.GenesisState) { gs.Orders[0].Status = types.OrderStatusExpired + 1 },
			"order 1 has an unknown type, time in force or status"},
		{"overfilled order", func(gs *types.GenesisState) { gs.Orders[0].FilledBase = word(101_690_000_000_000_001) },
			"order 1 filled above its size"},
		{"book entry without its order", func(gs *types.GenesisState) { gs.BookEntries[0].OrderID = word(2) },
			"book entry without its order 2"},
		{"duplicate book entry", func(gs *types.GenesisState) { gs.BookEntries = append(gs.BookEntries, gs.BookEntries[0]) },
			"duplicate book entry of order 1"},
		{"book entry of a non-resting order", func(gs *types.GenesisState) { gs.BookEntries[0].OrderID = word(3) },
			"book entry disagrees with order 3"},
		{"book entry on the wrong side", func(gs *types.GenesisState) { gs.BookEntries[0].Side = types.SideSell },
			"book entry disagrees with order 1"},
		{"book entry at another price", func(gs *types.GenesisState) { gs.BookEntries[0].Price = e18(59_001) },
			"book entry disagrees with order 1"},
		{"resting order without a book entry", func(gs *types.GenesisState) { gs.BookEntries = nil },
			"resting order 1 without a book entry"},
		{"trigger entry without its order", func(gs *types.GenesisState) { gs.TriggerEntries[0].OrderID = word(2) },
			"trigger entry without its order 2"},
		{"duplicate trigger entry", func(gs *types.GenesisState) { gs.TriggerEntries = append(gs.TriggerEntries, gs.TriggerEntries[0]) },
			"duplicate trigger entry of order 3"},
		{"trigger entry with an unknown direction", func(gs *types.GenesisState) { gs.TriggerEntries[0].Direction = 3 },
			"trigger entry disagrees with order 3"},
		{"trigger entry at another price", func(gs *types.GenesisState) { gs.TriggerEntries[0].TriggerPrice = e18(60_999) },
			"trigger entry disagrees with order 3"},
		{"untriggered order without a trigger entry", func(gs *types.GenesisState) { gs.TriggerEntries = nil },
			"untriggered order 3 without a trigger entry"},
		{"cloid entry without its order", func(gs *types.GenesisState) { gs.CloidEntries[0].OrderID = word(2) },
			"cloid entry without its order 2"},
		{"duplicate cloid entry", func(gs *types.GenesisState) { gs.CloidEntries = append(gs.CloidEntries, gs.CloidEntries[0]) },
			"duplicate cloid entry of order 1"},
		{"cloid entry of another cloid", func(gs *types.GenesisState) { gs.CloidEntries[0].Cloid = [16]byte{0xc2} },
			"cloid entry disagrees with order 1"},
		{"account order entry without its account", func(gs *types.GenesisState) {
			gs.AccountOrders[0].Account = holderB
			gs.Accounts = gs.Accounts[:1]
			gs.VenueBooks.DeficitSumUsd = e18(3)
		},
			"account order entry without its account " + holderB.Hex()},
		{"account order entry without its order", func(gs *types.GenesisState) { gs.AccountOrders[0].OrderID = word(2) },
			"account order entry without its order 2"},
		{"duplicate account order entry", func(gs *types.GenesisState) { gs.AccountOrders[1].OrderID = word(1) },
			"duplicate account order entry of order 1"},
		{"account order entry of another account", func(gs *types.GenesisState) { gs.AccountOrders[0].Account = holderB },
			"account order entry disagrees with order 1"},
		{"expiry entry without its order", func(gs *types.GenesisState) { gs.ExpiryEntries[0].OrderID = word(2) },
			"expiry entry without its order 2"},
		{"duplicate expiry entry", func(gs *types.GenesisState) { gs.ExpiryEntries = append(gs.ExpiryEntries, gs.ExpiryEntries[0]) },
			"duplicate expiry entry of order 1"},
		{"expiry entry at another deadline", func(gs *types.GenesisState) { gs.ExpiryEntries[0].Deadline = 1_800_000_101 },
			"expiry entry disagrees with order 1"},
		{"position id 0", func(gs *types.GenesisState) { gs.Positions[0].ID = word(0) },
			"position with id 0"},
		{"duplicate position", func(gs *types.GenesisState) { gs.Positions = append(gs.Positions, gs.Positions[0]) },
			"duplicate position 7"},
		{"position of unknown market", func(gs *types.GenesisState) { gs.Positions[0].MarketID = 9 },
			"position 7 of unknown market 9"},
		{"position without its account", func(gs *types.GenesisState) {
			gs.Positions[0].Account = holderC
		}, "position 7 without its account " + holderC.Hex()},
		{"zero size position", func(gs *types.GenesisState) { gs.Positions[0].SizeBase = word(0) },
			"position 7 has zero size"},
		{"second position in a market", func(gs *types.GenesisState) {
			second := gs.Positions[0]
			second.ID = word(8)
			gs.Positions = append(gs.Positions, second)
			gs.Counters.NextPositionID = word(9)
		}, "second position of account " + holderA.Hex() + " in market 0"},
		{"account market entry without its account", func(gs *types.GenesisState) {
			gs.AccountMarketPositions[0].Account = holderC
		}, "account market entry without its account " + holderC.Hex()},
		{"account market entry without its position", func(gs *types.GenesisState) { gs.AccountMarketPositions[0].PositionID = word(6) },
			"account market entry without its position 6"},
		{"duplicate account market entry", func(gs *types.GenesisState) {
			gs.AccountMarketPositions = append(gs.AccountMarketPositions, gs.AccountMarketPositions[0])
		}, "duplicate account market entry of position 7"},
		{"account market entry in another market", func(gs *types.GenesisState) { gs.AccountMarketPositions[0].MarketID = 11 },
			"account market entry disagrees with position 7"},
		{"liquidation cooldown without its position", func(gs *types.GenesisState) { gs.LiquidationCooldowns[0].PositionID = word(6) },
			"liquidation cooldown entry without its position 6"},
		{"duplicate liquidation cooldown", func(gs *types.GenesisState) {
			gs.LiquidationCooldowns = append(gs.LiquidationCooldowns, gs.LiquidationCooldowns[0])
		}, "duplicate liquidation cooldown of position 7"},
		{"attached TP/SL without its position", func(gs *types.GenesisState) { gs.AttachedTpSls[0].PositionID = word(6) },
			"attached TP/SL entry without its position 6"},
		{"duplicate attached TP/SL", func(gs *types.GenesisState) { gs.AttachedTpSls = append(gs.AttachedTpSls, gs.AttachedTpSls[0]) },
			"duplicate attached TP/SL of position 7"},
		{"attached TP/SL without its order", func(gs *types.GenesisState) { gs.AttachedTpSls[0].TakeProfitOrderID = word(2) },
			"attached TP/SL entry without its order 2"},
		{"opening limit order as attached stop-loss", func(gs *types.GenesisState) { gs.AttachedTpSls[0].StopLossOrderID = word(1) },
			"attached TP/SL order 1 is not a reduce-only stop-loss trigger of position 7"},
		{"stop-loss order as attached take-profit", func(gs *types.GenesisState) {
			gs.AttachedTpSls[0].TakeProfitOrderID = word(3)
		}, "attached TP/SL order 3 is not a reduce-only take-profit trigger of position 7"},
		{"attached stop-loss not reduce-only", func(gs *types.GenesisState) { gs.Orders[1].ReduceOnly = false },
			"attached TP/SL order 3 is not a reduce-only stop-loss trigger of position 7"},
		{"attached stop-loss on the position's side", func(gs *types.GenesisState) { gs.Orders[1].IsLong = true },
			"attached TP/SL order 3 is not a reduce-only stop-loss trigger of position 7"},
		{"attached stop-loss of another account", func(gs *types.GenesisState) {
			gs.Orders[1].Account = holderB
			gs.AccountOrders[1].Account = holderB
		}, "attached TP/SL order 3 is not a reduce-only stop-loss trigger of position 7"},
		{"attached stop-loss in another market", func(gs *types.GenesisState) {
			gs.Orders[1].MarketID = 11
			gs.TriggerEntries[0].MarketID = 11
		}, "attached TP/SL order 3 is not a reduce-only stop-loss trigger of position 7"},
		{"attached stop-loss already cancelled", func(gs *types.GenesisState) {
			gs.Orders[1].Status = types.OrderStatusCancelled
			gs.TriggerEntries = nil
		}, "attached TP/SL order 3 is not a reduce-only stop-loss trigger of position 7"},
		{"margin ratio entry without its account", func(gs *types.GenesisState) {
			gs.MarginRatioEntries[0].Account = holderC
		}, "margin ratio entry without its account " + holderC.Hex()},
		{"duplicate margin ratio entry", func(gs *types.GenesisState) {
			gs.MarginRatioEntries = append(gs.MarginRatioEntries, gs.MarginRatioEntries[0])
		}, "duplicate margin ratio entry of account " + holderA.Hex()},
		{"margin ratio entry in another bucket", func(gs *types.GenesisState) { gs.MarginRatioEntries[0].Bucket = 139 },
			"margin ratio entry disagrees with account " + holderA.Hex()},
		{"ADL rank entry without its position", func(gs *types.GenesisState) { gs.AdlRankEntries[0].PositionID = word(6) },
			"ADL rank entry without its position 6"},
		{"duplicate ADL rank entry", func(gs *types.GenesisState) { gs.AdlRankEntries = append(gs.AdlRankEntries, gs.AdlRankEntries[0]) },
			"duplicate ADL rank entry of position 7"},
		{"ADL rank entry with a stale score", func(gs *types.GenesisState) { gs.AdlRankEntries[0].RankScore = e18(4) },
			"ADL rank entry disagrees with position 7"},
		{"ADL rank entry on the wrong side", func(gs *types.GenesisState) { gs.AdlRankEntries[0].Side = types.SideSell },
			"ADL rank entry disagrees with position 7"},
		{"volume bucket without its account", func(gs *types.GenesisState) {
			gs.VolumeBuckets[0].Account = holderC
		}, "volume bucket without its account " + holderC.Hex()},
		{"duplicate volume bucket", func(gs *types.GenesisState) { gs.VolumeBuckets = append(gs.VolumeBuckets, gs.VolumeBuckets[0]) },
			"duplicate volume bucket of account " + holderA.Hex() + " day 20833"},
		{"duplicate venue volume bucket", func(gs *types.GenesisState) {
			gs.VenueVolumeBuckets = append(gs.VenueVolumeBuckets, gs.VenueVolumeBuckets[0])
		}, "duplicate venue volume bucket of day 20833"},
		{"next order id at a stored id", func(gs *types.GenesisState) { gs.Counters.NextOrderID = word(3) },
			"next order id 3 not above stored order id 3"},
		{"next position id below a stored id", func(gs *types.GenesisState) { gs.Counters.NextPositionID = word(5) },
			"next position id 5 not above stored position id 7"},
		{"next sequence at a stored sequence", func(gs *types.GenesisState) { gs.Counters.NextSequence = 2 },
			"next placement sequence 2 not above stored sequence 2"},
		{"zero counters of an empty venue", func(gs *types.GenesisState) {
			*gs = *types.DefaultGenesis()
			gs.Counters.NextOrderID = word(0)
		}, "next order id 0 not above stored order id 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := validGenesis()
			tc.mutate(&gs)
			err := types.ValidateGenesis(gs)
			require.ErrorIs(t, err, types.ErrInvalidGenesis)
			require.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestGenesisValidateRecordsRoundTrip(t *testing.T) {
	gs := validGenesis()
	var negative uint256.Int
	negative.Neg(uint256.NewInt(750_000_000_000_000_000))
	gs.Positions[0].FundingBasis = negative
	gs.MarketStates[0].FundingIndex = negative

	order := gs.Orders[0]
	encoded := order.Marshal()
	require.Len(t, encoded, 32+20+8+5+9*32+8+16+8)
	var decodedOrder types.Order
	require.NoError(t, decodedOrder.Unmarshal(encoded))
	require.Equal(t, order, decodedOrder)
	require.EqualError(t, decodedOrder.Unmarshal(encoded[:len(encoded)-1]), "perps: order record truncated")
	require.EqualError(t, decodedOrder.Unmarshal(append(encoded, 0)), "perps: order record has 1 trailing bytes")
	flagged := append([]byte(nil), encoded...)
	flagged[32+20+8] = 2
	require.EqualError(t, decodedOrder.Unmarshal(flagged), "perps: order record flag 2 is not 0 or 1")

	position := gs.Positions[0]
	var decodedPosition types.Position
	require.NoError(t, decodedPosition.Unmarshal(position.Marshal()))
	require.Equal(t, position, decodedPosition)
	require.Equal(t, -1, decodedPosition.FundingBasis.Sign())

	account := gs.Accounts[0]
	var decodedAccount types.Account
	require.NoError(t, decodedAccount.Unmarshal(account.Marshal()))
	require.Equal(t, account, decodedAccount)

	state := gs.MarketStates[0]
	var decodedState types.MarketState
	require.NoError(t, decodedState.Unmarshal(state.Marshal()))
	require.Equal(t, state, decodedState)

	books := gs.VenueBooks
	var decodedBooks types.VenueBooks
	require.NoError(t, decodedBooks.Unmarshal(books.Marshal()))
	require.Equal(t, books, decodedBooks)

	bz, err := json.Marshal(&gs)
	require.NoError(t, err)
	var imported types.GenesisState
	require.NoError(t, json.Unmarshal(bz, &imported))
	require.Equal(t, gs.Orders, imported.Orders)
	require.Equal(t, gs.Positions, imported.Positions)
	require.Equal(t, gs.Counters, imported.Counters)
	require.Equal(t, gs.VenueBooks, imported.VenueBooks)
	require.Equal(t, gs.SweepCursor, imported.SweepCursor)
	require.NoError(t, types.ValidateGenesis(imported))
}
