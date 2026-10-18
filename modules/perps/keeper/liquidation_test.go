package keeper_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var liquidationGenesisTime = time.Unix(1_800_000_000, 0).UTC()

// liquidationEnv is the perps keeper over the test application's EVM keeper
// and the real pricefeed keeper, each with its own store, one BTC market of
// class A and a fresh mark.
type liquidationEnv struct {
	t        *testing.T
	ctx      sdk.Context
	k        *keeper.Keeper
	key      sdk.StoreKey
	pfKey    sdk.StoreKey
	mark     uint256.Int
	sequence uint64
	nextID   uint64
}

func newLiquidationEnv(t *testing.T, mark uint256.Int) *liquidationEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(liquidationGenesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, &app.EvmKeeper, pricefeed)
	ctx = launchpadtest.WithStore(ctx, key)
	e := &liquidationEnv{t: t, ctx: ctx, k: k, key: key, pfKey: pfKey, nextID: 100}
	require.NoError(t, k.SetMarket(ctx, k.Authority(), types.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: types.VenueMaxLeverage, TierClass: types.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, types.USDDecimals),
		OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, types.USDDecimals),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}))
	e.setMark(mark)
	word := uint256.NewInt(10_000).Bytes32()
	ctx.KVStore(key).Set(types.SequenceKey(types.SequenceOrderID), word[:])
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePositionID), word[:])
	return e
}

func usdAmount(whole int64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(uint64(whole)), uint256.NewInt(1_000_000_000_000_000_000))
}

// baseAmount is units x 10^-5 BTC, the lot of the market, in 18-decimal base.
func baseAmount(lots uint64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(lots), uint256.NewInt(10_000_000_000_000))
}

// setMark stores the market state at mark and the pricefeed aggregate of
// the market at the block time, so its price reads fresh.
func (e *liquidationEnv) setMark(mark uint256.Int) {
	e.mark = mark
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice: sdk.NewIntFromBigInt(mark.ToBig()), MarkPrice: sdk.NewIntFromBigInt(mark.ToBig()),
		Basis: sdk.ZeroInt(), TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1,
	})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
	state := types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark, Fresh: true, MarkPrice: mark,
		LastTradePrice: mark, PriceTimestampMs: uint64(e.ctx.BlockTime().UnixMilli())}
	if bz := e.ctx.KVStore(e.key).Get(types.MarketStateKey(0)); bz != nil {
		var stored types.MarketState
		require.NoError(e.t, stored.Unmarshal(bz))
		state.LongOpenInterest, state.ShortOpenInterest = stored.LongOpenInterest, stored.ShortOpenInterest
		state.FundingIndex = stored.FundingIndex
	}
	e.ctx.KVStore(e.key).Set(types.MarketStateKey(0), state.Marshal())
}

func (e *liquidationEnv) addOpenInterest(size uint256.Int) {
	var state types.MarketState
	require.NoError(e.t, state.Unmarshal(e.ctx.KVStore(e.key).Get(types.MarketStateKey(0))))
	state.LongOpenInterest.Add(&state.LongOpenInterest, &size)
	state.ShortOpenInterest.Add(&state.ShortOpenInterest, &size)
	e.ctx.KVStore(e.key).Set(types.MarketStateKey(0), state.Marshal())
}

func (e *liquidationEnv) at(d time.Duration) {
	e.ctx = e.ctx.WithBlockTime(liquidationGenesisTime.Add(d))
	e.setMark(e.mark)
}

func (e *liquidationEnv) address(n uint64) common.Address {
	return common.BigToAddress(new(big.Int).SetUint64(0x5000_0000 + n))
}

// account stores an account holding collateral USDL units.
func (e *liquidationEnv) account(address common.Address, collateral uint64) {
	account := types.Account{Address: address, Collateral: *uint256.NewInt(collateral), Created: true}
	if bz := e.ctx.KVStore(e.key).Get(types.AccountKey(address)); bz != nil {
		require.NoError(e.t, account.Unmarshal(bz))
		account.Collateral = *uint256.NewInt(collateral)
	}
	e.ctx.KVStore(e.key).Set(types.AccountKey(address), account.Marshal())
}

func (e *liquidationEnv) readAccount(address common.Address) types.Account {
	account, err := e.k.LiquidationAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account
}

// position stores a position of size at reference price price, with its
// account and market indexes, and returns its id.
func (e *liquidationEnv) position(id uint64, address common.Address, isLong bool, size, price uint256.Int) uint256.Int {
	positionID := *uint256.NewInt(id)
	var cost uint256.Int
	cost.Mul(&size, &price)
	cost.Div(&cost, uint256.NewInt(1_000_000_000_000_000_000))
	position := types.Position{ID: positionID, Account: address, MarketID: 0, IsLong: isLong, SizeBase: size,
		ReferencePrice: price, CostBasisUsd: cost, EntryPrice: price,
		Leverage: usdAmount(100), OpenedAt: uint64(e.ctx.BlockTime().Unix())}
	store := e.ctx.KVStore(e.key)
	store.Set(types.PositionKey(positionID), position.Marshal())
	word := positionID.Bytes32()
	store.Set(types.AccountMarketPositionKey(address, 0), word[:])
	account := e.readAccount(address)
	account.PositionCount++
	store.Set(types.AccountKey(address), account.Marshal())
	_, err := e.k.LiquidationRebucket(e.ctx, address)
	require.NoError(e.t, err)
	return positionID
}

// pair stores a long of holder and the equal short of a well collateralised
// counterparty, so the open interest of both sides stays equal.
func (e *liquidationEnv) pair(id uint64, holder common.Address, collateral uint64, size, price uint256.Int) uint256.Int {
	e.account(holder, collateral)
	counterparty := e.address(9_000_000 + id)
	e.account(counterparty, 100_000_000_000_000)
	e.addOpenInterest(size)
	e.position(id+1_000_000, counterparty, false, size, price)
	return e.position(id, holder, true, size, price)
}

// order rests a GTC limit order of the account in the book.
func (e *liquidationEnv) order(address common.Address, isLong bool, price, size uint256.Int) uint256.Int {
	e.nextID++
	e.sequence++
	orderID := *uint256.NewInt(e.nextID)
	order := types.Order{ID: orderID, Account: address, MarketID: 0, IsLong: isLong, Type: types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, LimitPrice: price, SizeBase: size,
		Leverage: usdAmount(10), Sequence: e.sequence}
	store := e.ctx.KVStore(e.key)
	store.Set(types.OrderKey(orderID), order.Marshal())
	word := orderID.Bytes32()
	store.Set(types.BookKey(0, types.SideOf(isLong), types.BookPriceKey(isLong, price), e.sequence), word[:])
	store.Set(types.AccountOrderKey(address, orderID), []byte{})
	account := e.readAccount(address)
	account.OpenOrderCount++
	store.Set(types.AccountKey(address), account.Marshal())
	return orderID
}

// maker is an account with ample collateral bidding size at price.
func (e *liquidationEnv) maker(n uint64, price, size uint256.Int) common.Address {
	maker := e.address(n)
	e.account(maker, 100_000_000_000_000)
	e.order(maker, true, price, size)
	return maker
}

func (e *liquidationEnv) positionSize(id uint256.Int) (uint256.Int, bool) {
	bz := e.ctx.KVStore(e.key).Get(types.PositionKey(id))
	if bz == nil {
		return uint256.Int{}, false
	}
	var position types.Position
	require.NoError(e.t, position.Unmarshal(bz))
	return position.SizeBase, true
}

func liquidationEvent(positionID uint256.Int, account common.Address, mark uint256.Int, keeper common.Address) types.EventRecord {
	return types.EventRecord{Name: types.EventLiquidation, Args: []any{positionID.ToBig(), account, new(big.Int).SetUint64(0),
		mark.ToBig(), new(big.Int), keeper}}
}

func TestLiquidationAccountLevel(t *testing.T) {
	mark := usdAmount(60_000)
	e := newLiquidationEnv(t, mark)
	holder := e.address(1)
	// 1 BTC long at mark 60,000: maintenance 60,000 x 5 bps = 30 USD, equity 29 USD.
	positionID := e.pair(7, holder, 29_000_000, baseAmount(100_000), mark)
	restingAsk := e.order(holder, false, usdAmount(70_000), baseAmount(50_000))
	e.maker(2, mark, baseAmount(200_000))

	equity, err := e.k.Equity(e.ctx, holder)
	require.NoError(t, err)
	require.Equal(t, sdk.NewIntWithDecimal(29, types.USDDecimals), equity)
	summary, err := e.k.AccountMargin(e.ctx, holder)
	require.NoError(t, err)
	require.Equal(t, sdk.NewIntWithDecimal(30, types.USDDecimals), summary.MaintenanceMargin)

	caller := e.address(3)
	events, err := e.k.Liquidate(e.ctx, caller, positionID)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{liquidationEvent(positionID, holder, mark, caller)},
		eventsNamed(events, types.EventLiquidation))
	require.Equal(t, liquidationEvent(positionID, holder, mark, caller), events[len(events)-1])
	require.Equal(t, types.EventRecord{Name: types.EventOrderCancelled, Args: []any{restingAsk.ToBig(), holder}}, events[0],
		"resting orders are cancelled before the closing order")
	require.Len(t, eventsNamed(events, types.EventFill), 1, "one reduce-only IOC order closes the position")

	store := e.ctx.KVStore(e.key)
	_, open := e.positionSize(positionID)
	require.False(t, open, "the one reduce-only IOC order closes the whole position")
	require.Nil(t, store.Get(types.AccountMarketPositionKey(holder, 0)))
	require.Nil(t, store.Get(types.LiquidationCooldownKey(positionID)))
	require.False(t, store.Has(types.AccountOrderKey(holder, restingAsk)), "resting orders are cancelled first")
	require.False(t, store.Has(types.BookKey(0, types.SideSell, types.BookPriceKey(false, usdAmount(70_000)), 1)))
	account := e.readAccount(holder)
	require.Zero(t, account.OpenOrderCount)
	require.Zero(t, account.PositionCount)
	// The sell fills at 60,000 with no PnL; the taker fee 60,000 x 450 ppm = 27 USD
	// is the only charge: no liquidation fee, no penalty.
	require.Equal(t, *uint256.NewInt(2_000_000), account.Collateral)
	require.False(t, store.Has(types.MarginRatioIndexKey(account.MarginRatioBucket, holder)))
	callerAccount := store.Get(types.AccountKey(caller))
	require.Nil(t, callerAccount, "the caller is never paid")
}

func TestLiquidationSlices(t *testing.T) {
	mark := usdAmount(50_000)
	e := newLiquidationEnv(t, mark)
	holder := e.address(1)
	// 20 BTC at 50,000 = 1,000,000 USD, maintenance 500 USD, equity 400 USD.
	positionID := e.pair(7, holder, 400_000_000, baseAmount(2_000_000), mark)
	e.maker(2, mark, baseAmount(2_000_000))
	caller := e.address(3)

	events, err := e.k.Liquidate(e.ctx, caller, positionID)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{liquidationEvent(positionID, holder, mark, caller)},
		eventsNamed(events, types.EventLiquidation))
	require.Len(t, eventsNamed(events, types.EventFill), 1)
	size, open := e.positionSize(positionID)
	require.True(t, open)
	require.Equal(t, baseAmount(1_600_000), size, "the first slice closes 200,000 USD")
	last := e.ctx.KVStore(e.key).Get(types.LiquidationCooldownKey(positionID))
	require.Equal(t, append(uint64ToBytes(uint64(liquidationGenesisTime.Unix())), wordBytes(baseAmount(2_000_000))...), last,
		"the cooldown record holds the slice time and the original size as slice basis")

	e.at(10 * time.Second)
	_, err = e.k.Liquidate(e.ctx, caller, positionID)
	require.ErrorIs(t, err, types.ErrLiquidationCooldown)
	size, _ = e.positionSize(positionID)
	require.Equal(t, baseAmount(1_600_000), size)

	e.at(30 * time.Second)
	events, err = e.k.Liquidate(e.ctx, caller, positionID)
	require.NoError(t, err)
	require.Len(t, eventsNamed(events, types.EventLiquidation), 1)
	require.Len(t, eventsNamed(events, types.EventFill), 1)
	size, _ = e.positionSize(positionID)
	require.Equal(t, baseAmount(1_200_000), size, "at t + 30 another 200,000 USD closes")

	// With 455 USD of collateral the account stays below maintenance after
	// each 90 USD taker fee, so every 30 seconds one 200,000 USD slice of the
	// original 1,000,000 USD closes; the fifth is capped at the remaining
	// 200,000 USD, closes the position and clears the cooldown record.
	f := newLiquidationEnv(t, mark)
	sliced := f.pair(7, holder, 455_000_000, baseAmount(2_000_000), mark)
	f.maker(2, mark, baseAmount(2_000_000))
	cooldownKey := types.LiquidationCooldownKey(sliced)
	for i := uint64(1); i <= 4; i++ {
		f.at(time.Duration(i-1) * 30 * time.Second)
		events, err = f.k.Liquidate(f.ctx, caller, sliced)
		require.NoError(t, err, "slice %d", i)
		require.Equal(t, []types.EventRecord{liquidationEvent(sliced, holder, mark, caller)},
			eventsNamed(events, types.EventLiquidation))
		require.Len(t, eventsNamed(events, types.EventFill), 1)
		size, open = f.positionSize(sliced)
		require.True(t, open)
		require.Equal(t, baseAmount(2_000_000-400_000*i), size, "slice %d closes 20 percent of the original size", i)
		require.Equal(t, append(uint64ToBytes(uint64(f.ctx.BlockTime().Unix())), wordBytes(baseAmount(2_000_000))...),
			f.ctx.KVStore(f.key).Get(cooldownKey), "slice %d keeps the original slice basis", i)
	}
	f.at(100 * time.Second)
	_, err = f.k.Liquidate(f.ctx, caller, sliced)
	require.ErrorIs(t, err, types.ErrLiquidationCooldown)
	f.at(120 * time.Second)
	events, err = f.k.Liquidate(f.ctx, caller, sliced)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{liquidationEvent(sliced, holder, mark, caller)},
		eventsNamed(events, types.EventLiquidation))
	require.Len(t, eventsNamed(events, types.EventFill), 1)
	_, open = f.positionSize(sliced)
	require.False(t, open, "the last slice is capped at the remaining size and closes the position")
	require.Nil(t, f.ctx.KVStore(f.key).Get(cooldownKey), "a full close clears the cooldown record")
	require.Zero(t, f.readAccount(holder).PositionCount)
	require.Equal(t, *uint256.NewInt(5_000_000), f.readAccount(holder).Collateral)
}

func uint64ToBytes(value uint64) []byte {
	return new(big.Int).SetUint64(value).FillBytes(make([]byte, 8))
}

func TestLiquidationPerBlockBound(t *testing.T) {
	mark := usdAmount(60_000)
	e := newLiquidationEnv(t, mark)
	// 70 accounts long 0.01 BTC (600 USD, maintenance 0.3 USD) with equity
	// 0.27 to 0.2976 USD, buckets 90 to 99.
	const count = 70
	holders := make(map[common.Address]uint256.Int, count)
	for i := uint64(0); i < count; i++ {
		holder := e.address(100 + i)
		holders[holder] = e.pair(1_000+i, holder, 270_000+400*i, baseAmount(1_000), mark)
	}
	e.maker(2, mark, baseAmount(100_000))

	type indexed struct {
		bucket  uint16
		account common.Address
	}
	order := make([]indexed, 0, count)
	for holder := range holders {
		bucket := e.readAccount(holder).MarginRatioBucket
		require.Less(t, bucket, uint16(100))
		order = append(order, indexed{bucket, holder})
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].bucket != order[j].bucket {
			return order[i].bucket < order[j].bucket
		}
		return bytes.Compare(order[i].account.Bytes(), order[j].account.Bytes()) < 0
	})

	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	result, err := e.k.LiquidationScan(e.ctx)
	require.NoError(t, err)
	require.Empty(t, result.Failures)
	require.Equal(t, uint64(64), result.Liquidated)
	require.Equal(t, uint64(64), result.Slices)
	require.Equal(t, uint64(64), result.Visited)
	require.LessOrEqual(t, result.Visited, params.LiquidationScanPerBlock)
	liquidations := eventsNamed(result.Events, types.EventLiquidation)
	require.Len(t, liquidations, 64)
	require.Len(t, eventsNamed(result.Events, types.EventFill), 64)
	for i, entry := range order {
		_, open := e.positionSize(holders[entry.account])
		require.Equal(t, i >= 64, open, "accounts liquidate in ascending bucket order, entry %d", i)
	}
	for i, event := range liquidations {
		require.Equal(t, liquidationEvent(holders[order[i].account], order[i].account, mark, common.Address{}), event)
	}

	result, err = e.k.LiquidationScan(e.ctx)
	require.NoError(t, err)
	require.Empty(t, result.Failures)
	require.Equal(t, uint64(6), result.Liquidated, "the other 6 liquidate in the next block")
	for _, entry := range order {
		_, open := e.positionSize(holders[entry.account])
		require.False(t, open)
	}

	// The visit bound holds on its own: 10 visits per block liquidate 10.
	f := newLiquidationEnv(t, mark)
	params.LiquidationScanPerBlock = 10
	require.NoError(t, f.k.UpdateParams(f.ctx, f.k.Authority(), params))
	for i := uint64(0); i < 20; i++ {
		f.pair(1_000+i, f.address(100+i), 270_000+400*i, baseAmount(1_000), mark)
	}
	f.maker(2, mark, baseAmount(100_000))
	result, err = f.k.LiquidationScan(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(10), result.Visited)
	require.Equal(t, uint64(10), result.Liquidated)
}

func TestLiquidationPriceFormula(t *testing.T) {
	mark := usdAmount(60_000)

	// 10,000 USD BTC long at 60,000, 100x, collateral 100 USD: maintenance 5 USD.
	e := newLiquidationEnv(t, mark)
	sixth := *new(uint256.Int).Div(uint256.NewInt(1_000_000_000_000_000_000), uint256.NewInt(6))
	positionID := e.pair(7, e.address(1), 100_000_000, sixth, mark)
	got, err := e.k.LiquidationPrice(e.ctx, positionID)
	require.NoError(t, err)
	// 60,000 - (100 - 5) / (1/6) / (1 - 0.0005)
	want := new(big.Rat).Sub(big.NewRat(60_000, 1),
		new(big.Rat).Quo(new(big.Rat).Quo(big.NewRat(95, 1), big.NewRat(1, 6)), big.NewRat(9_995, 10_000)))
	want.Mul(want, new(big.Rat).SetInt(keeper.USDUnit))
	diff := new(big.Rat).Sub(new(big.Rat).SetInt(got.ToBig()), want)
	require.True(t, diff.Abs(diff).Cmp(new(big.Rat).SetInt64(1_000_000_000_000)) <= 0,
		"liquidation price %s within 1e-6 USD of %s", got.Dec(), want.FloatString(18))

	// 1 BTC long at 60,000 with 600 USD above maintenance 30 USD.
	g := newLiquidationEnv(t, mark)
	longID := g.pair(7, g.address(1), 630_000_000, baseAmount(100_000), mark)
	got, err = g.k.LiquidationPrice(g.ctx, longID)
	require.NoError(t, err)
	expected, ok := new(big.Int).SetString("59399699849924962481240", 10)
	require.True(t, ok)
	require.Equal(t, expected, got.ToBig())

	// The short side: 60,000 + 600 / 1 / (1 + 0.0005), rounded down.
	shortID := *uint256.NewInt(1_000_007)
	got, err = g.k.LiquidationPrice(g.ctx, shortID)
	require.NoError(t, err)
	equity, err := g.k.Equity(g.ctx, g.address(9_000_007))
	require.NoError(t, err)
	summary, err := g.k.AccountMargin(g.ctx, g.address(9_000_007))
	require.NoError(t, err)
	available := new(big.Rat).SetInt(new(big.Int).Sub(equity.BigInt(), summary.MaintenanceMargin.BigInt()))
	short := new(big.Rat).Add(new(big.Rat).SetInt(mark.ToBig()),
		new(big.Rat).Quo(available, big.NewRat(10_005, 10_000)))
	require.Equal(t, new(big.Int).Quo(short.Num(), short.Denom()), got.ToBig())
}

func TestLiquidationHealthyRefused(t *testing.T) {
	mark := usdAmount(60_000)
	e := newLiquidationEnv(t, mark)
	holder := e.address(1)
	// Equity 31 USD above maintenance 30 USD.
	positionID := e.pair(7, holder, 31_000_000, baseAmount(100_000), mark)
	restingAsk := e.order(holder, false, usdAmount(70_000), baseAmount(50_000))
	e.maker(2, mark, baseAmount(200_000))

	events, err := e.k.Liquidate(e.ctx, e.address(3), positionID)
	require.ErrorIs(t, err, types.ErrNotLiquidatable)
	require.Nil(t, events)
	size, open := e.positionSize(positionID)
	require.True(t, open)
	require.Equal(t, baseAmount(100_000), size)
	require.True(t, e.ctx.KVStore(e.key).Has(types.AccountOrderKey(holder, restingAsk)))
	require.Equal(t, *uint256.NewInt(31_000_000), e.readAccount(holder).Collateral)
	require.Nil(t, e.ctx.KVStore(e.key).Get(types.AccountKey(e.address(3))))

	result, err := e.k.LiquidationScan(e.ctx)
	require.NoError(t, err)
	require.Zero(t, result.Liquidated)
	require.Empty(t, result.Events)

	_, err = e.k.Liquidate(e.ctx, e.address(3), *uint256.NewInt(999))
	require.ErrorIs(t, err, types.ErrUnknownPosition)
}
