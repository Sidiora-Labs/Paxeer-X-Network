package types_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	usdl  = common.HexToAddress("0x00000000000000000000000000000000000a0001")
	pax   = common.HexToAddress("0x00000000000000000000000000000000000a0002")
	alice = common.HexToAddress("0x00000000000000000000000000000000000b0001")
	bob   = common.HexToAddress("0x00000000000000000000000000000000000b0002")
)

func e18(n int64) sdk.Int { return sdk.NewInt(n).Mul(sdk.NewInt(1_000_000_000_000_000_000)) }

// populatedGenesis holds one order of each kind active, one final limit
// order, and every index that the active orders imply.
func populatedGenesis() *types.GenesisState {
	gs := types.DefaultGenesis()
	gs.NextOrderID = 5
	gs.LimitOrders = []types.LimitOrder{
		{ID: 1, User: alice, Stablecoin: usdl, Token: pax, Amount: sdk.NewInt(100_000_000), TargetPrice: e18(1).QuoRaw(2),
			OrderType: types.OrderTypeLimitBuy, Status: types.OrderStatusPending, CreatedAt: 1000, ExpiresAt: 2000},
		{ID: 4, User: bob, Stablecoin: usdl, Token: pax, Amount: sdk.NewInt(7_000_000), TargetPrice: e18(1),
			OrderType: types.OrderTypeLimitSell, Status: types.OrderStatusCancelled, CreatedAt: 1100},
	}
	gs.StopLimitOrders = []types.StopLimitOrder{
		{ID: 2, User: alice, Stablecoin: usdl, Token: pax, Amount: sdk.NewInt(30_000_000), StopPrice: e18(1),
			LimitPrice: e18(1).QuoRaw(2), OrderType: types.OrderTypeStopLimitSell, Status: types.OrderStatusActivated,
			CreatedAt: 1000},
	}
	gs.StopLossOrders = []types.StopLossOrder{
		{ID: 3, User: bob, Stablecoin: usdl, Token: pax, Amount: sdk.NewInt(20_000_000), TriggerPrice: e18(1).QuoRaw(4),
			Status: types.OrderStatusPending, CreatedAt: 1050, ExpiresAt: 3000},
	}
	gs.UserOrders = []types.UserOrder{
		{User: alice, OrderID: 1, OrderType: types.OrderTypeLimitBuy},
		{User: alice, OrderID: 2, OrderType: types.OrderTypeStopLimitSell},
		{User: bob, OrderID: 3, OrderType: types.OrderTypeStopLoss},
		{User: bob, OrderID: 4, OrderType: types.OrderTypeLimitSell},
	}
	gs.ActiveLimitOrders = []uint64{1}
	gs.ActiveStopLimitOrders = []uint64{2}
	gs.ActiveStopLossOrders = []uint64{3}
	gs.Expiries = []types.OrderExpiry{{ExpiresAt: 2000, OrderID: 1}, {ExpiresAt: 3000, OrderID: 3}}
	gs.Escrows = []types.TokenEscrow{
		{Token: usdl, Amount: sdk.NewInt(100_000_000)},
		{Token: pax, Amount: sdk.NewInt(50_000_000)},
	}
	gs.ActiveCounts = []types.ActiveOrderCount{{User: alice, Count: 2}, {User: bob, Count: 1}}
	gs.OrderMinOutputs = []types.OrderMinOutput{{OrderID: 1, MinOutput: sdk.ZeroInt()}}
	gs.FillBackoffs = []types.FillBackoff{{OrderID: 2, Failures: 1, NextRetryHeight: 12}}
	gs.Cursors = types.EndBlockCursors{Activation: 2, FillIndex: types.FillIndexLimit, Fill: 1}
	gs.Books = []types.TokenBook{{Token: usdl, Reserves: sdk.NewInt(500_000_000_000), TotalDeposited: sdk.NewInt(500_000_000_000),
		TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + usdl.Hex(), MinimumSharesLocked: true}}
	gs.FeeAccruals = []types.FeeAccrual{{Rail: types.FeeRailOrder, Token: usdl, Amount: sdk.NewInt(10_000)}}
	gs.TwapRings = []types.TwapRing{{Token: pax, Head: 1, Count: 1}}
	gs.TwapSamples = []types.TwapSample{{Token: pax, Slot: 0, Time: 1000, Index: e18(1).QuoRaw(2)}}
	gs.Volumes = []types.TokenVolume{{Token: pax, VolumeUSD: e18(50), TradeCount: 1}}
	return gs
}

func TestGenesisValidate(t *testing.T) {
	require.NoError(t, types.DefaultGenesis().Validate())
	require.Equal(t, uint64(1), types.DefaultGenesis().NextOrderID)
	require.NoError(t, populatedGenesis().Validate())

	cases := []struct {
		name   string
		mutate func(*types.GenesisState)
		err    string
	}{
		{"active limit entry without its record", func(gs *types.GenesisState) {
			gs.ActiveLimitOrders = append(gs.ActiveLimitOrders, 9)
		}, "invalid genesis: active limit index entry 9 has no record"},
		{"active stop-limit entry naming a limit record", func(gs *types.GenesisState) {
			gs.ActiveStopLimitOrders = append(gs.ActiveStopLimitOrders, 1)
		}, "invalid genesis: active stop-limit index entry 1 has no record"},
		{"active stop-loss entry without its record", func(gs *types.GenesisState) {
			gs.StopLossOrders = nil
			gs.UserOrders = append(gs.UserOrders[:2:2], gs.UserOrders[3])
		}, "invalid genesis: active stop-loss index entry 3 has no record"},
		{"final order in an active index", func(gs *types.GenesisState) {
			gs.ActiveLimitOrders = append(gs.ActiveLimitOrders, 4)
		}, "invalid genesis: active limit index entry 4 is duplicate or not active"},
		{"active order outside its index", func(gs *types.GenesisState) {
			gs.ActiveStopLimitOrders = nil
		}, "invalid genesis: active order 2 is missing from its active index"},
		{"escrow above the sum of its active orders", func(gs *types.GenesisState) {
			gs.Escrows[1].Amount = sdk.NewInt(50_000_001)
		}, "invalid genesis: escrow of " + pax.Hex() + " differs from the sum 50000000 of its active orders"},
		{"escrow counting a final order", func(gs *types.GenesisState) {
			gs.Escrows[1].Amount = sdk.NewInt(57_000_000)
		}, "invalid genesis: escrow of " + pax.Hex() + " differs from the sum 50000000 of its active orders"},
		{"escrow missing for active orders", func(gs *types.GenesisState) {
			gs.Escrows = gs.Escrows[:1]
		}, "invalid genesis: escrow of " + pax.Hex() + " differs from the sum 50000000 of its active orders"},
		{"active count above the active orders of its user", func(gs *types.GenesisState) {
			gs.ActiveCounts[1].Count = 2
		}, "invalid genesis: active count of " + bob.Hex() + " differs from its 1 active orders"},
		{"active count missing for a user with active orders", func(gs *types.GenesisState) {
			gs.ActiveCounts = gs.ActiveCounts[:1]
		}, "invalid genesis: active count of " + bob.Hex() + " differs from its 1 active orders"},
		{"next order id not above every record", func(gs *types.GenesisState) {
			gs.NextOrderID = 4
		}, "invalid genesis: order id 4 is duplicate or not below the next id 4"},
		{"user order entry of another kind", func(gs *types.GenesisState) {
			gs.UserOrders[2].OrderType = types.OrderTypeLimitSell
		}, "invalid genesis: user order entry " + bob.Hex() + "/3 does not match a record"},
		{"expiry entry with another time", func(gs *types.GenesisState) {
			gs.Expiries[0].ExpiresAt = 2001
		}, "invalid genesis: expiry entry 2001/1 does not match an active order"},
		{"back-off of a final order", func(gs *types.GenesisState) {
			gs.FillBackoffs[0].OrderID = 4
		}, "invalid genesis: fill back-off 4"},
		{"stop-limit buy with the limit below the stop", func(gs *types.GenesisState) {
			gs.StopLimitOrders[0].OrderType = types.OrderTypeStopLimitBuy
			gs.UserOrders[1].OrderType = types.OrderTypeStopLimitBuy
		}, "invalid genesis: stop-limit order 2 limit on the wrong side of the stop"},
		{"twap ring count without its samples", func(gs *types.GenesisState) {
			gs.TwapRings[0].Count = 2
		}, "invalid genesis: twap ring " + pax.Hex() + " counts 2 samples, genesis has 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := populatedGenesis()
			tc.mutate(gs)
			err := gs.Validate()
			require.ErrorIs(t, err, types.ErrInvalidGenesis)
			require.EqualError(t, err, tc.err)
		})
	}
}
