package keeper_test

import (
	"fmt"
	"math/big"
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

var (
	traderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	traderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	traderC = common.HexToAddress("0x00000000000000000000000000000000000000c3")
)

type triggerEnv struct {
	markEnv
	perpsKey sdk.StoreKey
}

// newTriggerEnv is newMarkEnv keeping the perps store key, with funded
// accounts A, B and C and the venue fee and insurance accounts.
func newTriggerEnv(t *testing.T) triggerEnv {
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
	env := triggerEnv{markEnv: markEnv{k: k, ctx: ctx, pfKey: pfKey}, perpsKey: perpsKey}
	for _, address := range []common.Address{traderA, traderB, traderC, k.FeesAccountAddress(), k.InsuranceAccountAddress()} {
		env.setAccount(types.Account{Address: address, Collateral: e18(1_000_000), Created: true})
	}
	return env
}

func (env triggerEnv) store() sdk.KVStore { return env.ctx.KVStore(env.perpsKey) }

func (env triggerEnv) at(height int64) triggerEnv {
	env.markEnv = env.atBlock(height, markNowMs+(height-10)*1_000)
	return env
}

func (env triggerEnv) setAccount(account types.Account) {
	env.store().Set(types.AccountKey(account.Address), account.Marshal())
}

func (env triggerEnv) account(t *testing.T, address common.Address) types.Account {
	t.Helper()
	var account types.Account
	require.NoError(t, account.Unmarshal(env.store().Get(types.AccountKey(address))))
	return account
}

func (env triggerEnv) order(t *testing.T, id uint256.Int) types.Order {
	t.Helper()
	bz := env.store().Get(types.OrderKey(id))
	require.NotNil(t, bz)
	var order types.Order
	require.NoError(t, order.Unmarshal(bz))
	return order
}

// price stores the pricefeed aggregate of BTC with index and pricefeed mark
// both at usd, one second old, refreshes the market state and returns the
// mark it computed.
func (env triggerEnv) price(t *testing.T, usd int64) uint256.Int {
	t.Helper()
	env.setAggregate(t, 0, usd, usd, env.ctx.BlockTime().UnixMilli()-1_000)
	_, err := env.k.UpdatePrices(env.ctx)
	require.NoError(t, err)
	state, err := env.k.GetMarketState(env.ctx, 0)
	require.NoError(t, err)
	require.True(t, state.Fresh)
	require.Equal(t, e18(usd), state.MarkPrice)
	return state.MarkPrice
}

// seedOrder stores an order the way placement leaves it: the record, the
// account order index, one more open order on the account and the trigger
// index for an UNTRIGGERED order or the book for a RESTING one.
func (env triggerEnv) seedOrder(t *testing.T, order types.Order) types.Order {
	t.Helper()
	id, err := env.k.NextOrderID(env.ctx)
	require.NoError(t, err)
	order.ID = id
	order.Sequence = id.Uint64()
	if order.Leverage.IsZero() {
		order.Leverage = e18(10)
	}
	env.store().Set(types.OrderKey(order.ID), order.Marshal())
	env.store().Set(types.AccountOrderKey(order.Account, order.ID), []byte{})
	account := env.account(t, order.Account)
	account.OpenOrderCount++
	env.setAccount(account)
	if order.Status == types.OrderStatusUntriggered {
		require.NoError(t, env.k.TriggerInsert(env.ctx, order))
	} else {
		require.NoError(t, env.k.BookInsert(env.ctx, order))
	}
	return order
}

func (env triggerEnv) seedBid(t *testing.T, account common.Address, price, sizeBase uint256.Int) types.Order {
	return env.seedOrder(t, types.Order{Account: account, MarketID: 0, IsLong: true, Type: types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, LimitPrice: price, SizeBase: sizeBase})
}

func (env triggerEnv) seedAsk(t *testing.T, account common.Address, price, sizeBase uint256.Int) types.Order {
	return env.seedOrder(t, types.Order{Account: account, MarketID: 0, IsLong: false, Type: types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, LimitPrice: price, SizeBase: sizeBase})
}

// seedPosition stores an open position with its account index, the account's
// position count and its side of open interest.
func (env triggerEnv) seedPosition(t *testing.T, account common.Address, isLong bool, sizeBase, entry uint256.Int) types.Position {
	t.Helper()
	id, err := env.k.NextPositionID(env.ctx)
	require.NoError(t, err)
	var cost uint256.Int
	cost.MulDivOverflow(&sizeBase, &entry, uint256.NewInt(1_000_000_000_000_000_000))
	position := types.Position{ID: id, Account: account, MarketID: 0, IsLong: isLong, SizeBase: sizeBase,
		ReferencePrice: entry, CostBasisUsd: cost, EntryPrice: entry, Leverage: e18(10),
		OpenedAt: uint64(env.ctx.BlockHeight())}
	env.store().Set(types.PositionKey(id), position.Marshal())
	word := id.Bytes32()
	env.store().Set(types.AccountMarketPositionKey(account, 0), word[:])
	holder := env.account(t, account)
	holder.PositionCount++
	env.setAccount(holder)
	state, err := env.k.GetMarketState(env.ctx, 0)
	require.NoError(t, err)
	if isLong {
		state.LongOpenInterest.Add(&state.LongOpenInterest, &sizeBase)
	} else {
		state.ShortOpenInterest.Add(&state.ShortOpenInterest, &sizeBase)
	}
	env.k.SetMarketState(env.ctx, state)
	return position
}

func (env triggerEnv) triggerEntries(t *testing.T) int {
	t.Helper()
	iterator := sdk.KVStorePrefixIterator(env.store(), types.TriggerPrefix)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	return count
}

// lots is a base size of n lots of 0.00001 BTC in 18-decimal units.
func lots(n uint64) uint256.Int {
	var size uint256.Int
	size.Mul(uint256.NewInt(n), uint256.NewInt(10_000_000_000_000))
	return size
}

// tenths is a price of n tenths of a USD in 18-decimal units.
func tenths(n uint64) uint256.Int {
	var price uint256.Int
	price.Mul(uint256.NewInt(n), uint256.NewInt(100_000_000_000_000_000))
	return price
}

func argString(arg any) string {
	switch value := arg.(type) {
	case *big.Int:
		return value.String()
	case common.Address:
		return value.Hex()
	case [16]byte:
		return fmt.Sprintf("%x", value)
	default:
		return fmt.Sprint(value)
	}
}

func eventArgs(event types.EventRecord) []string {
	out := []string{event.Name}
	for _, arg := range event.Args {
		out = append(out, argString(arg))
	}
	return out
}

func eventsNamed(events []types.EventRecord, name string) []types.EventRecord {
	var out []types.EventRecord
	for _, event := range events {
		if event.Name == name {
			out = append(out, event)
		}
	}
	return out
}

func TestTriggersStopAtMark(t *testing.T) {
	env := newTriggerEnv(t)
	market, err := env.k.GetMarket(env.ctx, 0)
	require.NoError(t, err)
	maker := env.seedBid(t, traderB, e18(58_980), lots(1_000))
	stop := env.seedOrder(t, types.Order{Account: traderA, MarketID: 0, IsLong: false, Type: types.OrderTypeStopMarket,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusUntriggered, TriggerPrice: e18(59_000),
		SizeBase: lots(1_000)})
	require.Equal(t, 1, env.triggerEntries(t))

	// Mark 59,100 is above the falling trigger: nothing fires, and
	// executeOrder refuses.
	mark := env.price(t, 59_100)
	evaluated, events, err := env.k.EvaluateTriggers(env.ctx, market, mark, 256)
	require.NoError(t, err)
	require.Zero(t, evaluated)
	require.Empty(t, events)
	_, _, err = env.k.ExecuteOrder(env.ctx, stop.ID)
	require.ErrorIs(t, err, types.ErrTriggerNotReached)
	require.Equal(t, types.OrderStatusUntriggered, env.order(t, stop.ID).Status)

	// Mark 58,990 crosses 59,000: the stop runs as an IOC sell into the bid.
	env = env.at(11)
	mark = env.price(t, 58_990)
	evaluated, events, err = env.k.EvaluateTriggers(env.ctx, market, mark, 256)
	require.NoError(t, err)
	require.Equal(t, uint64(1), evaluated)
	require.Equal(t, []string{types.EventOrderTriggered, stop.ID.Dec(), "0", e18(58_990).Dec()}, eventArgs(events[0]))
	fills := eventsNamed(events, types.EventFill)
	require.Len(t, fills, 1)
	require.Equal(t, []string{types.EventFill, "0", maker.ID.Dec(), stop.ID.Dec(), traderB.Hex(), traderA.Hex(), "false",
		e18(58_980).Dec(), "589800000000000000000"}, eventArgs(fills[0])[:9])

	filled := env.order(t, stop.ID)
	require.Equal(t, types.OrderStatusFilled, filled.Status)
	require.Equal(t, types.TimeInForceIOC, filled.TimeInForce)
	require.Equal(t, lots(1_000), filled.FilledBase)
	require.Zero(t, env.triggerEntries(t))
	_, _, err = env.k.ExecuteOrder(env.ctx, stop.ID)
	require.ErrorIs(t, err, types.ErrUnknownOrder)
}

func TestTriggersPerBlockBound(t *testing.T) {
	env := newTriggerEnv(t)
	market, err := env.k.GetMarket(env.ctx, 0)
	require.NoError(t, err)
	params, err := env.k.GetParams(env.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(256), params.TriggersPerBlock)

	env.seedBid(t, traderB, e18(58_980), lots(100_000))
	stops := make([]types.Order, 300)
	for i := range stops {
		stops[i] = env.seedOrder(t, types.Order{Account: traderA, MarketID: 0, IsLong: false,
			Type: types.OrderTypeStopMarket, TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusUntriggered,
			TriggerPrice: tenths(590_000 + uint64(i)), SizeBase: lots(100)})
	}
	require.Equal(t, 300, env.triggerEntries(t))

	// Falling triggers run from the highest trigger price down.
	expect := func(from, to int) []string {
		var ids []string
		for i := from; i >= to; i-- {
			ids = append(ids, stops[i].ID.Dec())
		}
		return ids
	}
	triggeredIDs := func(events []types.EventRecord) []string {
		var ids []string
		for _, event := range eventsNamed(events, types.EventOrderTriggered) {
			ids = append(ids, argString(event.Args[0]))
		}
		return ids
	}

	env = env.at(11)
	mark := env.price(t, 58_990)
	evaluated, events, err := env.k.EvaluateTriggers(env.ctx, market, mark, params.TriggersPerBlock)
	require.NoError(t, err)
	require.Equal(t, uint64(256), evaluated)
	require.Equal(t, expect(299, 44), triggeredIDs(events))
	require.Len(t, eventsNamed(events, types.EventFill), 256)
	require.Equal(t, 44, env.triggerEntries(t))
	require.Equal(t, types.OrderStatusUntriggered, env.order(t, stops[43].ID).Status)
	require.Equal(t, types.OrderStatusFilled, env.order(t, stops[44].ID).Status)

	env = env.at(12)
	mark = env.price(t, 58_990)
	evaluated, events, err = env.k.EvaluateTriggers(env.ctx, market, mark, params.TriggersPerBlock)
	require.NoError(t, err)
	require.Equal(t, uint64(44), evaluated)
	require.Equal(t, expect(43, 0), triggeredIDs(events))
	require.Zero(t, env.triggerEntries(t))
	require.Equal(t, types.OrderStatusFilled, env.order(t, stops[0].ID).Status)
}

func TestTriggersOneCancelsOther(t *testing.T) {
	env := newTriggerEnv(t)
	market, err := env.k.GetMarket(env.ctx, 0)
	require.NoError(t, err)
	env.price(t, 60_000)
	position := env.seedPosition(t, traderA, true, lots(10_000), e18(60_000))
	env.seedPosition(t, traderB, false, lots(10_000), e18(60_000))

	// A take-profit at or below mark for a long already holds, so it is
	// refused, and only the holder may attach a pair.
	_, err = env.k.SetPositionTpSl(env.ctx, traderA, position.ID, e18(59_000), e18(57_000))
	require.ErrorIs(t, err, types.ErrInvalidTriggerPrice)
	_, err = env.k.SetPositionTpSl(env.ctx, traderA, position.ID, e18(66_000), tenths(570_005))
	require.NoError(t, err)
	_, err = env.k.SetPositionTpSl(env.ctx, traderB, position.ID, e18(66_000), e18(57_000))
	require.ErrorIs(t, err, types.ErrNotPositionHolder)

	// Replacing the pair cancels the previous orders.
	before, err := env.k.GetAttachedTpSl(env.ctx, position.ID)
	require.NoError(t, err)
	events, err := env.k.SetPositionTpSl(env.ctx, traderA, position.ID, e18(66_000), e18(57_000))
	require.NoError(t, err)
	require.Equal(t, []string{types.EventPositionTpSlSet, position.ID.Dec(), e18(66_000).Dec(), e18(57_000).Dec()},
		eventArgs(events[len(events)-1]))
	require.Len(t, eventsNamed(events, types.EventOrderCancelled), 2)
	require.Equal(t, types.OrderStatusCancelled, env.order(t, before.TakeProfitOrderID).Status)
	require.Equal(t, types.OrderStatusCancelled, env.order(t, before.StopLossOrderID).Status)
	pair, err := env.k.GetAttachedTpSl(env.ctx, position.ID)
	require.NoError(t, err)
	tp, sl := env.order(t, pair.TakeProfitOrderID), env.order(t, pair.StopLossOrderID)
	require.Equal(t, []string{types.EventOrderPlaced, tp.ID.Dec(), traderA.Hex(), "0",
		fmt.Sprint(uint8(types.OrderTypeTakeProfit)), "false", e18(66_000).Dec(), e18(6_000).Dec()},
		eventArgs(eventsNamed(events, types.EventOrderPlaced)[0]))
	for _, leg := range []types.Order{tp, sl} {
		require.Equal(t, types.OrderStatusUntriggered, leg.Status)
		require.True(t, leg.ReduceOnly)
		require.False(t, leg.IsLong)
		require.Equal(t, position.SizeBase, leg.SizeBase)
	}
	require.Equal(t, types.OrderTypeTakeProfit, tp.Type)
	require.Equal(t, types.OrderTypeStopLoss, sl.Type)
	require.Equal(t, 2, env.triggerEntries(t))
	require.Equal(t, uint64(2), env.account(t, traderA).OpenOrderCount)

	// Mark 66,000 fires the take-profit into C's bid; the stop-loss is
	// cancelled with it.
	env.seedBid(t, traderC, e18(65_990), lots(10_000))
	env = env.at(11)
	mark := env.price(t, 66_000)
	evaluated, events, err := env.k.EvaluateTriggers(env.ctx, market, mark, 256)
	require.NoError(t, err)
	require.Equal(t, uint64(1), evaluated)
	require.Equal(t, []string{types.EventOrderTriggered, tp.ID.Dec(), "0", e18(66_000).Dec()}, eventArgs(events[0]))
	require.Len(t, eventsNamed(events, types.EventFill), 1)
	cancelled := eventsNamed(events, types.EventOrderCancelled)
	require.Len(t, cancelled, 1)
	require.Equal(t, []string{types.EventOrderCancelled, sl.ID.Dec(), traderA.Hex()}, eventArgs(cancelled[0]))

	require.Equal(t, types.OrderStatusFilled, env.order(t, tp.ID).Status)
	require.Equal(t, types.OrderStatusCancelled, env.order(t, sl.ID).Status)
	pair, err = env.k.GetAttachedTpSl(env.ctx, position.ID)
	require.NoError(t, err)
	require.Equal(t, keeper.AttachedTpSl{}, pair)
	require.Nil(t, env.store().Get(types.AttachedTpSlKey(position.ID)))
	require.Zero(t, env.triggerEntries(t))
	require.Zero(t, env.account(t, traderA).OpenOrderCount)
}

func TestTriggersStopLimitRests(t *testing.T) {
	env := newTriggerEnv(t)
	market, err := env.k.GetMarket(env.ctx, 0)
	require.NoError(t, err)
	env.seedAsk(t, traderB, e18(61_200), lots(1_000))
	stop := env.seedOrder(t, types.Order{Account: traderA, MarketID: 0, IsLong: true, Type: types.OrderTypeStopLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusUntriggered, TriggerPrice: e18(61_000),
		LimitPrice: e18(61_100), SizeBase: lots(1_000)})

	mark := env.price(t, 60_000)
	evaluated, events, err := env.k.EvaluateTriggers(env.ctx, market, mark, 256)
	require.NoError(t, err)
	require.Zero(t, evaluated)
	require.Empty(t, events)

	// Mark 61,000 fires the rising trigger; the limit 61,100 does not reach
	// the 61,200 ask, so the order rests as a limit bid.
	env = env.at(11)
	mark = env.price(t, 61_000)
	evaluated, events, err = env.k.EvaluateTriggers(env.ctx, market, mark, 256)
	require.NoError(t, err)
	require.Equal(t, uint64(1), evaluated)
	require.Len(t, events, 1)
	require.Equal(t, []string{types.EventOrderTriggered, stop.ID.Dec(), "0", e18(61_000).Dec()}, eventArgs(events[0]))

	rested := env.order(t, stop.ID)
	require.Equal(t, types.OrderStatusResting, rested.Status)
	require.Equal(t, types.TimeInForceGTC, rested.TimeInForce)
	require.Equal(t, e18(61_100), rested.LimitPrice)
	require.True(t, rested.FilledBase.IsZero())
	require.Greater(t, rested.Sequence, stop.Sequence)
	bid, ok, err := env.k.BestBid(env.ctx, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, e18(61_100), bid)
	require.Zero(t, env.triggerEntries(t))
	require.Equal(t, uint64(1), env.account(t, traderA).OpenOrderCount)
}
