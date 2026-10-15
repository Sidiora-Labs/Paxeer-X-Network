package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// ordersNow is the block time of markNowMs in seconds.
const ordersNow uint64 = uint64(markNowMs / 1_000)

var (
	traderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	traderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	cloidC  = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
)

type ordersEnv struct {
	markEnv
	perpsKey sdk.StoreKey
}

// newOrdersEnv builds the perps keeper over the real pricefeed keeper with
// BTC (0, tick 0.1, lot 0.00001) and SOL (2) listed, ETH (1) stored
// disabled, fresh prices of 60,000 and 150 USD and the BTC mark at 60,000.
func newOrdersEnv(t *testing.T) ordersEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(markNowMs).UTC())
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := keeper.NewKeeper(perpsKey, &testApp.EvmKeeper, pricefeed)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	eth := listedMarket(1, "ETH", 4, 2, types.TierClassA, 20_000, 300_000_000)
	eth.Enabled = false
	require.NoError(t, k.SetMarket(ctx, k.Authority(), eth))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), listedMarket(2, "SOL", 2, 3, types.TierClassB, 10_000, 100_000_000)))
	env := ordersEnv{markEnv: markEnv{k: k, ctx: ctx, pfKey: pfKey}, perpsKey: perpsKey}
	env.setAggregate(t, 0, 60_000, 60_000, markNowMs-1_000)
	env.setAggregate(t, 2, 150, 150, markNowMs-1_000)
	_, err := k.UpdatePrices(ctx)
	require.NoError(t, err)
	state, err := k.GetMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, e18(60_000), state.MarkPrice)
	return env
}

// fund stores a created account holding usdl whole USDL.
func (env ordersEnv) fund(t *testing.T, account common.Address, usdl uint64) {
	t.Helper()
	var collateral uint256.Int
	collateral.Mul(uint256.NewInt(usdl), uint256.NewInt(1_000_000))
	env.ctx.KVStore(env.perpsKey).Set(types.AccountKey(account),
		types.Account{Address: account, Collateral: collateral, Created: true}.Marshal())
}

func (env ordersEnv) account(t *testing.T, address common.Address) types.Account {
	t.Helper()
	bz := env.ctx.KVStore(env.perpsKey).Get(types.AccountKey(address))
	require.NotNil(t, bz)
	var account types.Account
	require.NoError(t, account.Unmarshal(bz))
	return account
}

func (env ordersEnv) order(t *testing.T, id uint64) types.Order {
	t.Helper()
	bz := env.ctx.KVStore(env.perpsKey).Get(types.OrderKey(*uint256.NewInt(id)))
	require.NotNil(t, bz)
	var order types.Order
	require.NoError(t, order.Unmarshal(bz))
	return order
}

func (env ordersEnv) updateParams(t *testing.T, edit func(*types.Params)) {
	t.Helper()
	params, err := env.k.GetParams(env.ctx)
	require.NoError(t, err)
	edit(&params)
	require.NoError(t, env.k.UpdateParams(env.ctx, env.k.Authority(), params))
}

// limitBuy is a GTC BTC limit buy at 20x of sizeUsd whole USD at price whole
// USD.
func limitBuy(sizeUsd, price int64) keeper.OrderParams {
	return keeper.OrderParams{
		MarketID:    0,
		IsLong:      true,
		OrderType:   uint8(types.OrderTypeLimit),
		TimeInForce: uint8(types.TimeInForceGTC),
		SizeUsd:     e18(sizeUsd),
		Leverage:    e18(20),
		LimitPrice:  e18(price),
	}
}

func argStrings(event types.EventRecord) []string {
	out := []string{event.Name}
	for _, arg := range event.Args {
		out = append(out, fmt.Sprint(arg))
	}
	return out
}

func requireRefused(t *testing.T, err error, want error, code uint32) {
	t.Helper()
	require.ErrorIs(t, err, want)
	require.Equal(t, code, sdkErrorCode(want))
}

func sdkErrorCode(err error) uint32 {
	return err.(interface{ ABCICode() uint32 }).ABCICode()
}

func TestOrdersValidationOrder(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 100)
	first := limitBuy(100, 59_000)
	first.Cloid = cloidC
	placed, err := env.k.PlaceOrder(env.ctx, traderA, first)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(1), placed.OrderID)

	// Every case breaks its own rule and every later one; fixing the rule of
	// one case exposes the refusal of the next.
	order := limitBuy(2_000_000, 59_000)
	order.MarketID = 9
	order.OrderType = 6
	order.TimeInForce = 4
	order.Deadline = ordersNow - 1
	order.LimitPrice = wordOf(t, "59000050000000000000000")
	order.Leverage = e18(1001)
	order.Cloid = cloidC
	order.ReduceOnly = false
	account := traderB

	env.updateParams(t, func(p *types.Params) { p.Paused = true })
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrPaused, 45)
	env.updateParams(t, func(p *types.Params) { p.Paused = false })

	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrNoAccount, 43)
	account = traderA

	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrUnknownMarket, 3)
	order.MarketID = 1
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrMarketDisabled, 4)
	order.MarketID = 2
	env.setAggregate(t, 2, 150, 150, markNowMs-16_000)
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrPriceStale, 5)
	order.MarketID = 0

	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrInvalidOrderType, 17)
	order.OrderType = uint8(types.OrderTypeLimit)
	// A LIMIT order with timeInForce 4 is refused before any margin check.
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrInvalidTimeInForce, 16)
	market := order
	market.OrderType = uint8(types.OrderTypeMarket)
	market.TimeInForce = uint8(types.TimeInForceGTC)
	_, err = env.k.PlaceOrder(env.ctx, account, market)
	requireRefused(t, err, types.ErrInvalidTimeInForce, 16)
	order.TimeInForce = uint8(types.TimeInForceGTC)

	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrDeadlineExpired, 22)
	order.Deadline = 0
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrTickSize, 11)
	order.LimitPrice = e18(59_000)

	lot := order
	lot.SizeUsd = wordOf(t, "500000000000000000")
	_, err = env.k.PlaceOrder(env.ctx, account, lot)
	requireRefused(t, err, types.ErrLotSize, 12)
	small := order
	small.SizeUsd = e18(9)
	_, err = env.k.PlaceOrder(env.ctx, account, small)
	requireRefused(t, err, types.ErrMinOrderValue, 13)

	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrLeverageTooHigh, 10)
	order.Leverage = e18(20)

	env.updateParams(t, func(p *types.Params) { p.MaxOpenOrdersPerAccount = 1 })
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrTooManyOpenOrders, 31)
	env.updateParams(t, func(p *types.Params) { p.MaxOpenOrdersPerAccount = 1_000 })

	reduce := order
	reduce.ReduceOnly = true
	_, err = env.k.PlaceOrder(env.ctx, account, reduce)
	requireRefused(t, err, types.ErrDuplicateCloid, 24)
	reduce.Cloid = [16]byte{0xd0}
	order.Cloid = [16]byte{0xd0}
	_, err = env.k.PlaceOrder(env.ctx, account, reduce)
	requireRefused(t, err, types.ErrReduceOnlyViolation, 23)

	// 1,999,999.7 USD at 20x needs about 100,000 USD of the 100 held.
	_, err = env.k.PlaceOrder(env.ctx, account, order)
	requireRefused(t, err, types.ErrInsufficientMargin, 9)

	// No refusal wrote anything: the next order is id 2 and A holds two.
	placed, err = env.k.PlaceOrder(env.ctx, account, limitBuy(100, 59_000))
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(2), placed.OrderID)
	require.Equal(t, uint64(2), env.account(t, traderA).OpenOrderCount)
}

func TestOrdersTickAndLot(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 1_000)

	offTick := limitBuy(6_000, 0)
	offTick.LimitPrice = wordOf(t, "59000050000000000000000")
	_, err := env.k.PlaceOrder(env.ctx, traderA, offTick)
	requireRefused(t, err, types.ErrTickSize, 11)

	stop := keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeStopMarket),
		TimeInForce: uint8(types.TimeInForceIOC), SizeUsd: e18(6_000), Leverage: e18(20),
		TriggerPrice: wordOf(t, "61000050000000000000000")}
	_, err = env.k.PlaceOrder(env.ctx, traderA, stop)
	requireRefused(t, err, types.ErrTickSize, 11)
	stop.TriggerPrice = e18(59_000)
	_, err = env.k.PlaceOrder(env.ctx, traderA, stop)
	requireRefused(t, err, types.ErrInvalidTriggerPrice, 29)

	// 0.5 USD at 60,000 is 0.0000083 BTC, zero lots of 0.00001.
	dust := limitBuy(0, 60_000)
	dust.SizeUsd = wordOf(t, "500000000000000000")
	_, err = env.k.PlaceOrder(env.ctx, traderA, dust)
	requireRefused(t, err, types.ErrLotSize, 12)
	dustMarket := keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeMarket),
		TimeInForce: uint8(types.TimeInForceIOC), SizeUsd: wordOf(t, "500000000000000"), Leverage: e18(20)}
	_, err = env.k.PlaceOrder(env.ctx, traderA, dustMarket)
	requireRefused(t, err, types.ErrLotSize, 12)

	// The normative example: 6,000 USD at 59,000 rests as 0.10169 BTC.
	example := limitBuy(6_000, 59_000)
	example.Cloid = cloidC
	placed, err := env.k.PlaceOrder(env.ctx, traderA, example)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(1), placed.OrderID)
	require.Empty(t, placed.Fills)
	require.Len(t, placed.Events, 2)
	require.Equal(t, []string{types.EventOrderPlaced, "1", traderA.String(), "0", "0", "true",
		"59000000000000000000000", "5999710000000000000000"}, argStrings(placed.Events[0]))
	require.Equal(t, []string{types.EventOrderOptions, "1", "0", "false", "59000000000000000000000", "0", "0",
		fmt.Sprint(cloidC)}, argStrings(placed.Events[1]))
	rested := env.order(t, 1)
	require.Equal(t, types.OrderStatusResting, rested.Status)
	require.Equal(t, wordOf(t, "101690000000000000"), rested.SizeBase)
	require.Equal(t, wordOf(t, "5999710000000000000000"), rested.SizeUsd)
	require.Equal(t, e18(59_000), rested.LimitPrice)
	require.Equal(t, e18(59_000), rested.TriggerPrice)
	require.Equal(t, uint64(1), rested.Sequence)
	bid, ok, err := env.k.BestBid(env.ctx, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, e18(59_000), bid)

	// A STOP_LIMIT sizes at its limit price and waits untriggered.
	stopLimit := keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeStopLimit),
		TimeInForce: uint8(types.TimeInForceGTC), SizeUsd: e18(6_000), Leverage: e18(20),
		TriggerPrice: e18(61_000), LimitPrice: e18(61_100)}
	placed, err = env.k.PlaceOrder(env.ctx, traderA, stopLimit)
	require.NoError(t, err)
	require.Equal(t, []string{types.EventOrderPlaced, "2", traderA.String(), "0", "1", "true",
		"61000000000000000000000", "5999409000000000000000"}, argStrings(placed.Events[0]))
	waiting := env.order(t, 2)
	require.Equal(t, types.OrderStatusUntriggered, waiting.Status)
	require.Equal(t, wordOf(t, "98190000000000000"), waiting.SizeBase)
	trigger := e18(61_000).Bytes32()
	require.True(t, env.ctx.KVStore(env.perpsKey).Has(types.TriggerKey(0, types.TriggerRising, trigger[:], 2)))
	require.Equal(t, uint64(2), env.account(t, traderA).OpenOrderCount)
}

func TestOrdersValueBounds(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 16_000_000)

	// BTC lists at 1000x: market orders up to 30,000,000 USD. 30,000,001 USD
	// at mark 60,000 sizes to 500.00001 BTC, 30,000,000.6 USD.
	market := keeper.OrderParams{MarketID: 0, IsLong: true, OrderType: uint8(types.OrderTypeMarket),
		TimeInForce: uint8(types.TimeInForceIOC), SizeUsd: e18(30_000_001), Leverage: e18(20)}
	_, err := env.k.PlaceOrder(env.ctx, traderA, market)
	requireRefused(t, err, types.ErrMaxOrderValue, 14)
	// 30,000,000 USD passes the value check and meets the empty book.
	market.SizeUsd = e18(30_000_000)
	_, err = env.k.PlaceOrder(env.ctx, traderA, market)
	requireRefused(t, err, types.ErrNoLiquidity, 20)

	// Limit orders take ten times the market maximum.
	_, err = env.k.PlaceOrder(env.ctx, traderA, limitBuy(300_000_060, 60_000))
	requireRefused(t, err, types.ErrMaxOrderValue, 14)
	placed, err := env.k.PlaceOrder(env.ctx, traderA, limitBuy(300_000_000, 60_000))
	require.NoError(t, err)
	require.Equal(t, e18(300_000_000), env.order(t, placed.OrderID.Uint64()).SizeUsd)
	require.Equal(t, e18(5_000), env.order(t, placed.OrderID.Uint64()).SizeBase)

	// Ten USD is the minimum: 9 USD and 10 USD at 60,000 (9.6 USD of lots)
	// are refused, 10 USD at 50,000 is exactly 0.0002 BTC.
	_, err = env.k.PlaceOrder(env.ctx, traderA, limitBuy(9, 59_000))
	requireRefused(t, err, types.ErrMinOrderValue, 13)
	_, err = env.k.PlaceOrder(env.ctx, traderA, limitBuy(10, 60_000))
	requireRefused(t, err, types.ErrMinOrderValue, 13)
	placed, err = env.k.PlaceOrder(env.ctx, traderA, limitBuy(10, 50_000))
	require.NoError(t, err)
	require.Equal(t, e18(10), env.order(t, placed.OrderID.Uint64()).SizeUsd)

	// The 300,000,000 USD limit sits in the 150,000,000 USD tier of 20x.
	over := limitBuy(300_000_000, 60_000)
	over.Leverage = e18(21)
	_, err = env.k.PlaceOrder(env.ctx, traderA, over)
	requireRefused(t, err, types.ErrLeverageTooHigh, 10)

	overflow := limitBuy(0, 60_000)
	overflow.SizeUsd = wordOf(t, "57896044618658097711785492504343953926634992332820282019728792003956564819968")
	_, err = env.k.PlaceOrder(env.ctx, traderA, overflow)
	requireRefused(t, err, types.ErrOverflow, 38)
}

func TestOrdersCloidUnique(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 10_000)
	env.fund(t, traderB, 10_000)
	withCloid := limitBuy(1_000, 59_000)
	withCloid.Cloid = cloidC

	placed, err := env.k.PlaceOrder(env.ctx, traderA, withCloid)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(1), placed.OrderID)
	_, err = env.k.PlaceOrder(env.ctx, traderA, withCloid)
	requireRefused(t, err, types.ErrDuplicateCloid, 24)

	// The cloid is unique per account and market only.
	placed, err = env.k.PlaceOrder(env.ctx, traderB, withCloid)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(2), placed.OrderID)
	sol := keeper.OrderParams{MarketID: 2, IsLong: true, OrderType: uint8(types.OrderTypeLimit),
		TimeInForce: uint8(types.TimeInForceGTC), SizeUsd: e18(1_000), Leverage: e18(20), LimitPrice: e18(140),
		Cloid: cloidC}
	placed, err = env.k.PlaceOrder(env.ctx, traderA, sol)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(3), placed.OrderID)

	// Only the placer cancels; a closed order is unknown.
	_, err = env.k.CancelOrder(env.ctx, traderB, *uint256.NewInt(1))
	requireRefused(t, err, types.ErrNotOrderHolder, 26)
	_, err = env.k.CancelOrder(env.ctx, traderA, *uint256.NewInt(77))
	requireRefused(t, err, types.ErrUnknownOrder, 25)
	// Cancelling stays open while the venue is paused.
	env.updateParams(t, func(p *types.Params) { p.Paused = true })
	event, err := env.k.CancelOrder(env.ctx, traderA, *uint256.NewInt(1))
	require.NoError(t, err)
	require.Equal(t, []string{types.EventOrderCancelled, "1", traderA.String()}, argStrings(event))
	env.updateParams(t, func(p *types.Params) { p.Paused = false })
	_, err = env.k.CancelOrder(env.ctx, traderA, *uint256.NewInt(1))
	requireRefused(t, err, types.ErrUnknownOrder, 25)
	cancelled := env.order(t, 1)
	require.Equal(t, types.OrderStatusCancelled, cancelled.Status)
	_, ok, err := env.k.BestBid(env.ctx, 0)
	require.NoError(t, err)
	require.True(t, ok, "order 2 of B still rests")
	require.Equal(t, uint64(1), env.account(t, traderA).OpenOrderCount)
	require.False(t, env.ctx.KVStore(env.perpsKey).Has(types.AccountOrderKey(traderA, *uint256.NewInt(1))))

	// The freed cloid takes a new order, which cancelByCloid cancels.
	placed, err = env.k.PlaceOrder(env.ctx, traderA, withCloid)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(4), placed.OrderID)
	event, err = env.k.CancelByCloid(env.ctx, traderA, 0, cloidC)
	require.NoError(t, err)
	require.Equal(t, []string{types.EventOrderCancelled, "4", traderA.String()}, argStrings(event))
	_, err = env.k.CancelByCloid(env.ctx, traderA, 0, cloidC)
	requireRefused(t, err, types.ErrUnknownOrder, 25)
	_, err = env.k.CancelByCloid(env.ctx, traderA, 0, [16]byte{})
	requireRefused(t, err, types.ErrUnknownOrder, 25)
	// B's order with the same cloid is untouched.
	require.Equal(t, types.OrderStatusResting, env.order(t, 2).Status)
	require.Equal(t, types.OrderStatusResting, env.order(t, 3).Status)
}

func TestOrdersOpenOrderLimit(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 1_000_000)
	order := limitBuy(100, 50_000)
	order.Leverage = e18(1)
	for i := 0; i < 1_000; i++ {
		_, err := env.k.PlaceOrder(env.ctx, traderA, order)
		require.NoError(t, err, "order %d", i+1)
	}
	require.Equal(t, uint64(1_000), env.account(t, traderA).OpenOrderCount)

	_, err := env.k.PlaceOrder(env.ctx, traderA, order)
	requireRefused(t, err, types.ErrTooManyOpenOrders, 31)
	require.Equal(t, uint64(1_000), env.account(t, traderA).OpenOrderCount)

	_, err = env.k.CancelOrder(env.ctx, traderA, *uint256.NewInt(500))
	require.NoError(t, err)
	placed, err := env.k.PlaceOrder(env.ctx, traderA, order)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(1_001), placed.OrderID)
	require.Equal(t, uint64(1_000), env.account(t, traderA).OpenOrderCount)
}

func TestOrdersDeadlineExpiry(t *testing.T) {
	env := newOrdersEnv(t)
	env.fund(t, traderA, 100_000)

	expired := limitBuy(100, 59_000)
	expired.Deadline = ordersNow - 1
	_, err := env.k.PlaceOrder(env.ctx, traderA, expired)
	requireRefused(t, err, types.ErrDeadlineExpired, 22)

	for i := 0; i < 4; i++ {
		_, err := env.k.PlaceOrder(env.ctx, traderA, limitBuy(100, 59_000))
		require.NoError(t, err)
	}
	five := limitBuy(100, 59_000)
	five.Deadline = 1_700_000_100
	five.Cloid = cloidC
	placed, err := env.k.PlaceOrder(env.ctx, traderA, five)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(5), placed.OrderID)
	// 300 orders due earlier than order 5 overflow one block's 256 expiries.
	early := limitBuy(100, 58_000)
	early.Deadline = 1_700_000_090
	for i := 0; i < 300; i++ {
		_, err := env.k.PlaceOrder(env.ctx, traderA, early)
		require.NoError(t, err)
	}
	require.Equal(t, uint64(305), env.account(t, traderA).OpenOrderCount)

	// One block expires at most 256 orders, earliest deadline first; a
	// deadline equal to the block time is not yet below it.
	atDeadline := env.atBlock(11, 1_700_000_100_000)
	events, err := env.k.ExpireOrders(atDeadline.ctx)
	require.NoError(t, err)
	require.Len(t, events, 256)
	require.Equal(t, []string{types.EventOrderExpired, "6", traderA.String(), "0"}, argStrings(events[0]))
	require.Equal(t, []string{types.EventOrderExpired, "261", traderA.String(), "0"}, argStrings(events[255]))
	require.Equal(t, types.OrderStatusResting, env.order(t, 5).Status)
	require.Equal(t, uint64(49), env.account(t, traderA).OpenOrderCount)

	following := env.atBlock(12, 1_700_000_101_000)
	events, err = following.k.ExpireOrders(following.ctx)
	require.NoError(t, err)
	require.Len(t, events, 45)
	require.Equal(t, []string{types.EventOrderExpired, "262", traderA.String(), "0"}, argStrings(events[0]))
	require.Equal(t, []string{types.EventOrderExpired, "5", traderA.String(), "0"}, argStrings(events[44]))

	require.Equal(t, types.OrderStatusExpired, env.order(t, 5).Status)
	require.Equal(t, types.OrderStatusExpired, env.order(t, 305).Status)
	require.Equal(t, types.OrderStatusResting, env.order(t, 4).Status)
	require.Equal(t, uint64(4), env.account(t, traderA).OpenOrderCount)
	store := env.ctx.KVStore(env.perpsKey)
	require.False(t, store.Has(types.ExpiryKey(1_700_000_100, *uint256.NewInt(5))))
	require.False(t, store.Has(types.CloidKey(traderA, 0, cloidC)))
	require.False(t, store.Has(types.AccountOrderKey(traderA, *uint256.NewInt(5))))
	_, err = env.k.CancelOrder(env.ctx, traderA, *uint256.NewInt(5))
	requireRefused(t, err, types.ErrUnknownOrder, 25)

	events, err = following.k.ExpireOrders(following.ctx)
	require.NoError(t, err)
	require.Empty(t, events)
}
