package keeper

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	matchA = common.HexToAddress("0x00000000000000000000000000000000000a0001")
	matchB = common.HexToAddress("0x00000000000000000000000000000000000a0002")
	matchC = common.HexToAddress("0x00000000000000000000000000000000000a0003")
	matchD = common.HexToAddress("0x00000000000000000000000000000000000a0004")
)

type matchEnv struct {
	k      *Keeper
	ctx    sdk.Context
	market types.Market
}

// newMatchEnv lists BTC (lot 0.00001, tick 0.1) at the given mark with the
// default params minus the rebate tiers, so every fill pays the tier 0 maker
// fee of 150 ppm and taker fee of 450 ppm, and funds four accounts.
func newMatchEnv(t *testing.T, mark uint256.Int, accounts ...common.Address) *matchEnv {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_perps_matching")).
		WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	k := &Keeper{storeKey: key, authority: authtypes.NewModuleAddress(govtypes.ModuleName).String()}
	params := types.DefaultParams()
	params.RebateTiers = nil
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), types.Market{
		ID:                   0,
		Symbol:               "BTC",
		Name:                 "Bitcoin",
		OracleSymbol:         "BTC",
		SzDecimals:           5,
		PxDecimals:           1,
		MaxLeverage:          1000,
		TierClass:            types.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, types.USDDecimals),
		OiCapUsd:             sdk.NewIntWithDecimal(500_000_000, types.USDDecimals),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}))
	market, err := k.GetMarket(ctx, 0)
	require.NoError(t, err)
	k.SetMarketState(ctx, types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark, Fresh: true, MarkPrice: mark})
	for _, account := range append([]common.Address{matchA, matchB, matchC, matchD}, accounts...) {
		record := types.Account{Address: account, Collateral: *uint256.NewInt(1_000_000_000_000), Created: true}
		ctx.KVStore(key).Set(types.AccountKey(account), record.Marshal())
	}
	return &matchEnv{k: k, ctx: ctx, market: market}
}

// matchFixed reads a decimal string as 18-decimal fixed point.
func matchFixedDec(t *testing.T, decimal string) string {
	value := matchFixed(t, decimal)
	return value.Dec()
}

func matchFixed(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	whole, fraction, _ := strings.Cut(decimal, ".")
	require.LessOrEqual(t, len(fraction), types.USDDecimals)
	value, err := uint256.FromDecimal(whole + fraction + strings.Repeat("0", types.USDDecimals-len(fraction)))
	require.NoError(t, err)
	return *value
}

type matchOrder struct {
	account         common.Address
	isLong          bool
	orderType       types.OrderType
	timeInForce     types.TimeInForce
	limitPrice      uint256.Int
	acceptablePrice uint256.Int
	sizeBase        uint256.Int
	sequence        uint64
}

func limitOrder(t *testing.T, account common.Address, isLong bool, tif types.TimeInForce, price, size string) matchOrder {
	t.Helper()
	return matchOrder{account: account, isLong: isLong, orderType: types.OrderTypeLimit, timeInForce: tif,
		limitPrice: matchFixed(t, price), sizeBase: matchFixed(t, size)}
}

func marketOrder(t *testing.T, account common.Address, isLong bool, acceptable, size string) matchOrder {
	t.Helper()
	return matchOrder{account: account, isLong: isLong, orderType: types.OrderTypeMarket,
		timeInForce: types.TimeInForceIOC, acceptablePrice: matchFixed(t, acceptable), sizeBase: matchFixed(t, size)}
}

// register stores an incoming order the way placement leaves it before
// matching: a fresh order id, the placement sequence (the order id unless
// given), the account's open order count raised by one and its open order
// index entry.
func (e *matchEnv) register(t *testing.T, ctx sdk.Context, o matchOrder) types.Order {
	t.Helper()
	id, err := e.k.NextOrderID(ctx)
	require.NoError(t, err)
	sequence := o.sequence
	if sequence == 0 {
		sequence = id.Uint64()
	}
	sizingPrice := o.limitPrice
	if o.orderType != types.OrderTypeLimit {
		state, err := e.k.GetMarketState(ctx, e.market.ID)
		require.NoError(t, err)
		sizingPrice = state.MarkPrice
	}
	sizeUsd, overflow := new(uint256.Int).MulDivOverflow(&o.sizeBase, &sizingPrice, matchUnit)
	require.False(t, overflow)
	order := types.Order{
		ID: id, Account: o.account, MarketID: e.market.ID, IsLong: o.isLong, Type: o.orderType,
		TimeInForce: o.timeInForce, LimitPrice: o.limitPrice, AcceptablePrice: o.acceptablePrice,
		SizeUsd: *sizeUsd, SizeBase: o.sizeBase, Leverage: matchFixed(t, "10"), Sequence: sequence,
	}
	store := ctx.KVStore(e.k.storeKey)
	account := e.account(t, ctx, o.account)
	account.OpenOrderCount++
	store.Set(types.AccountKey(o.account), account.Marshal())
	store.Set(types.AccountOrderKey(o.account, id), []byte{})
	return order
}

// place registers and matches one order as one transaction: a failed match
// leaves no trace, the registration included.
func (e *matchEnv) place(t *testing.T, o matchOrder) (MatchResult, error) {
	t.Helper()
	cacheCtx, write := e.ctx.CacheContext()
	order := e.register(t, cacheCtx, o)
	result, err := e.k.Match(cacheCtx, e.market, order)
	if err == nil {
		write()
	}
	return result, err
}

func (e *matchEnv) mustPlace(t *testing.T, o matchOrder) MatchResult {
	t.Helper()
	result, err := e.place(t, o)
	require.NoError(t, err)
	return result
}

func (e *matchEnv) account(t *testing.T, ctx sdk.Context, address common.Address) types.Account {
	t.Helper()
	bz := ctx.KVStore(e.k.storeKey).Get(types.AccountKey(address))
	require.NotNil(t, bz)
	var account types.Account
	require.NoError(t, account.Unmarshal(bz))
	return account
}

func (e *matchEnv) order(t *testing.T, id uint256.Int) types.Order {
	t.Helper()
	bz := e.ctx.KVStore(e.k.storeKey).Get(types.OrderKey(id))
	require.NotNil(t, bz)
	var order types.Order
	require.NoError(t, order.Unmarshal(bz))
	return order
}

func (e *matchEnv) book(t *testing.T, side byte) []types.Order {
	t.Helper()
	var orders []types.Order
	require.NoError(t, e.k.IterateBook(e.ctx, e.market.ID, side, func(order types.Order) bool {
		orders = append(orders, order)
		return false
	}))
	return orders
}

func (e *matchEnv) bookIDs(t *testing.T, side byte) []string {
	t.Helper()
	ids := []string{}
	for _, order := range e.book(t, side) {
		ids = append(ids, order.ID.Dec())
	}
	return ids
}

func (e *matchEnv) dump() [][2][]byte {
	iterator := e.ctx.KVStore(e.k.storeKey).Iterator(nil, nil)
	defer iterator.Close()
	var out [][2][]byte
	for ; iterator.Valid(); iterator.Next() {
		out = append(out, [2][]byte{append([]byte(nil), iterator.Key()...), append([]byte(nil), iterator.Value()...)})
	}
	return out
}

func matchArgStrings(t *testing.T, event types.EventRecord) []string {
	t.Helper()
	out := []string{event.Name}
	for _, arg := range event.Args {
		switch value := arg.(type) {
		case *big.Int:
			out = append(out, value.String())
		case common.Address:
			out = append(out, value.Hex())
		case bool:
			out = append(out, strconv.FormatBool(value))
		default:
			t.Fatalf("event %s argument of type %T", event.Name, arg)
		}
	}
	return out
}

func matchEvents(t *testing.T, events []types.EventRecord, names ...string) [][]string {
	t.Helper()
	out := [][]string{}
	for _, event := range events {
		for _, name := range names {
			if event.Name == name {
				out = append(out, matchArgStrings(t, event))
			}
		}
	}
	return out
}

func fillArgs(makerID, takerID string, maker, taker common.Address, takerIsLong bool, price, sizeUsd, makerFee, takerFee string) []string {
	return []string{types.EventFill, "0", makerID, takerID, maker.Hex(), taker.Hex(), strconv.FormatBool(takerIsLong),
		price, sizeUsd, makerFee, takerFee}
}

func requireReleased(t *testing.T, e *matchEnv, id uint256.Int, status types.OrderStatus, filled string) {
	t.Helper()
	order := e.order(t, id)
	require.Equal(t, status, order.Status)
	require.Equal(t, matchFixedDec(t, filled), order.FilledBase.Dec())
	require.Nil(t, e.ctx.KVStore(e.k.storeKey).Get(types.AccountOrderKey(order.Account, id)))
}

func TestMatchingPriceTimePriority(t *testing.T) {
	e := newMatchEnv(t, matchFixed(t, "60000"))
	late := limitOrder(t, matchB, false, types.TimeInForceGTC, "60000", "0.01")
	late.sequence = 3
	early := limitOrder(t, matchC, false, types.TimeInForceGTC, "60000", "0.01")
	early.sequence = 1
	better := limitOrder(t, matchD, false, types.TimeInForceGTC, "59990", "0.01")
	better.sequence = 2
	lateID := e.mustPlace(t, late).Taker.ID
	earlyID := e.mustPlace(t, early).Taker.ID
	betterID := e.mustPlace(t, better).Taker.ID
	require.Equal(t, []string{betterID.Dec(), earlyID.Dec(), lateID.Dec()}, e.bookIDs(t, types.SideSell))

	buy := marketOrder(t, matchA, true, "0", "0.02")
	buy.sequence = 4
	result := e.mustPlace(t, buy)
	takerID := result.Taker.ID.Dec()
	require.Equal(t, types.OrderStatusFilled, result.Taker.Status)
	require.Equal(t, matchFixedDec(t, "0.02"), result.Taker.FilledBase.Dec())
	require.False(t, result.Rested)
	require.Empty(t, result.Cancelled)
	require.Len(t, result.Fills, 2)
	require.Equal(t, betterID, result.Fills[0].MakerOrderID)
	require.Equal(t, earlyID, result.Fills[1].MakerOrderID)
	require.Equal(t, [][]string{
		fillArgs(betterID.Dec(), takerID, matchD, matchA, true, matchFixedDec(t, "59990"),
			matchFixedDec(t, "599.9"), matchFixedDec(t, "0.089985"), matchFixedDec(t, "0.269955")),
		fillArgs(earlyID.Dec(), takerID, matchC, matchA, true, matchFixedDec(t, "60000"),
			matchFixedDec(t, "600"), matchFixedDec(t, "0.09"), matchFixedDec(t, "0.27")),
	}, matchEvents(t, result.Events, types.EventFill))

	executed := matchEvents(t, result.Events, types.EventOrderExecuted)
	require.Len(t, executed, 3)
	require.Equal(t, betterID.Dec(), executed[0][1])
	require.Equal(t, matchFixedDec(t, "59990"), executed[0][3])
	require.Equal(t, earlyID.Dec(), executed[1][1])
	require.Equal(t, matchFixedDec(t, "60000"), executed[1][3])
	require.Equal(t, takerID, executed[2][1])
	require.Equal(t, matchFixedDec(t, "59995"), executed[2][3])

	requireReleased(t, e, betterID, types.OrderStatusFilled, "0.01")
	requireReleased(t, e, earlyID, types.OrderStatusFilled, "0.01")
	requireReleased(t, e, result.Taker.ID, types.OrderStatusFilled, "0.02")
	for _, account := range []common.Address{matchA, matchC, matchD} {
		require.Zero(t, e.account(t, e.ctx, account).OpenOrderCount)
	}
	require.Equal(t, uint64(1), e.account(t, e.ctx, matchB).OpenOrderCount)
	require.Equal(t, []string{lateID.Dec()}, e.bookIDs(t, types.SideSell))
	require.Equal(t, types.OrderStatusResting, e.order(t, lateID).Status)

	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, matchFixedDec(t, "60000"), state.LastTradePrice.Dec())
}

func TestMatchingPostOnlyAndFok(t *testing.T) {
	e := newMatchEnv(t, matchFixed(t, "60000"))
	askID := e.mustPlace(t, limitOrder(t, matchB, false, types.TimeInForceGTC, "60000", "0.01")).Taker.ID

	fok := e.register(t, e.ctx, limitOrder(t, matchA, true, types.TimeInForceFOK, "60000", "0.02"))
	before := e.dump()
	_, err := e.k.Match(e.ctx, e.market, fok)
	require.ErrorIs(t, err, types.ErrFokNotFilled)
	require.Contains(t, err.Error(), "LXP_PERPS_FOK_NOT_FILLED")
	require.Equal(t, before, e.dump())

	postOnly := e.register(t, e.ctx, limitOrder(t, matchA, true, types.TimeInForcePostOnly, "60000", "0.01"))
	before = e.dump()
	_, err = e.k.Match(e.ctx, e.market, postOnly)
	require.ErrorIs(t, err, types.ErrPostOnlyWouldCross)
	require.Contains(t, err.Error(), "LXP_PERPS_POST_ONLY_WOULD_CROSS")
	require.Equal(t, before, e.dump())

	postOnlyMarket := marketOrder(t, matchA, true, "0", "0.01")
	postOnlyMarket.timeInForce = types.TimeInForcePostOnly
	_, err = e.place(t, postOnlyMarket)
	require.ErrorIs(t, err, types.ErrInvalidTimeInForce)

	maker := e.mustPlace(t, limitOrder(t, matchA, true, types.TimeInForcePostOnly, "59999.9", "0.01"))
	require.True(t, maker.Rested)
	require.Empty(t, maker.Fills)
	require.Empty(t, maker.Events)
	require.Equal(t, types.OrderStatusResting, maker.Taker.Status)
	require.Equal(t, []string{maker.Taker.ID.Dec()}, e.bookIDs(t, types.SideBuy))
	require.Equal(t, types.OrderStatusResting, e.order(t, maker.Taker.ID).Status)

	filled := e.mustPlace(t, limitOrder(t, matchC, true, types.TimeInForceFOK, "60000", "0.01"))
	require.Equal(t, types.OrderStatusFilled, filled.Taker.Status)
	require.False(t, filled.Rested)
	require.Len(t, filled.Fills, 1)
	require.Equal(t, askID, filled.Fills[0].MakerOrderID)
	require.Equal(t, matchFixedDec(t, "600"), filled.Fills[0].SizeUsd.Dec())
	require.Empty(t, e.bookIDs(t, types.SideSell))
	requireReleased(t, e, askID, types.OrderStatusFilled, "0.01")
	requireReleased(t, e, filled.Taker.ID, types.OrderStatusFilled, "0.01")
}

func TestMatchingSelfTradePrevention(t *testing.T) {
	e := newMatchEnv(t, matchFixed(t, "60000"))
	ownID := e.mustPlace(t, limitOrder(t, matchA, false, types.TimeInForceGTC, "60000", "0.01")).Taker.ID
	otherID := e.mustPlace(t, limitOrder(t, matchB, false, types.TimeInForceGTC, "60010", "0.01")).Taker.ID

	result := e.mustPlace(t, marketOrder(t, matchA, true, "0", "0.01"))
	takerID := result.Taker.ID.Dec()
	require.Equal(t, []uint256.Int{ownID}, result.Cancelled)
	require.Equal(t, types.OrderStatusFilled, result.Taker.Status)
	require.Len(t, result.Fills, 1)
	require.Equal(t, otherID, result.Fills[0].MakerOrderID)
	require.Equal(t, [][]string{
		{types.EventOrderCancelled, ownID.Dec(), matchA.Hex()},
		fillArgs(otherID.Dec(), takerID, matchB, matchA, true, matchFixedDec(t, "60010"),
			matchFixedDec(t, "600.1"), matchFixedDec(t, "0.090015"), matchFixedDec(t, "0.270045")),
	}, matchEvents(t, result.Events, types.EventOrderCancelled, types.EventFill))
	requireReleased(t, e, ownID, types.OrderStatusCancelled, "0")
	requireReleased(t, e, otherID, types.OrderStatusFilled, "0.01")
	require.Zero(t, e.account(t, e.ctx, matchA).OpenOrderCount)
	require.Zero(t, e.account(t, e.ctx, matchB).OpenOrderCount)
	require.Empty(t, e.bookIDs(t, types.SideSell))

	// With only the account's own order on the opposite side nothing fills,
	// so the order reverts and the self-trade cancellation with it.
	lonelyID := e.mustPlace(t, limitOrder(t, matchA, false, types.TimeInForceGTC, "60000", "0.01")).Taker.ID
	before := e.dump()
	_, err := e.place(t, marketOrder(t, matchA, true, "0", "0.01"))
	require.ErrorIs(t, err, types.ErrSlippageExceeded)
	require.Equal(t, before, e.dump())
	require.Equal(t, []string{lonelyID.Dec()}, e.bookIDs(t, types.SideSell))

	// A resting order of the same account at a crossing price is cancelled
	// before a GTC remainder rests, so the book never crosses.
	rest := e.mustPlace(t, limitOrder(t, matchA, true, types.TimeInForceGTC, "60000", "0.01"))
	require.Equal(t, []uint256.Int{lonelyID}, rest.Cancelled)
	require.True(t, rest.Rested)
	require.Empty(t, rest.Fills)
	require.Empty(t, e.bookIDs(t, types.SideSell))
	require.Equal(t, []string{rest.Taker.ID.Dec()}, e.bookIDs(t, types.SideBuy))
	require.Equal(t, uint64(1), e.account(t, e.ctx, matchA).OpenOrderCount)
}

func TestMatchingSlippageBound(t *testing.T) {
	e := newMatchEnv(t, matchFixed(t, "60000"))
	_, err := e.place(t, marketOrder(t, matchA, true, "0", "0.01"))
	require.ErrorIs(t, err, types.ErrNoLiquidity)
	require.Contains(t, err.Error(), "LXP_PERPS_NO_LIQUIDITY")

	farID := e.mustPlace(t, limitOrder(t, matchB, false, types.TimeInForceGTC, "60400", "0.01")).Taker.ID
	buy := e.register(t, e.ctx, marketOrder(t, matchA, true, "0", "0.01"))
	before := e.dump()
	_, err = e.k.Match(e.ctx, e.market, buy)
	require.ErrorIs(t, err, types.ErrSlippageExceeded)
	require.Contains(t, err.Error(), "LXP_PERPS_SLIPPAGE_EXCEEDED")
	require.Equal(t, before, e.dump())

	nearID := e.mustPlace(t, limitOrder(t, matchC, false, types.TimeInForceGTC, "60200", "0.01")).Taker.ID
	result := e.mustPlace(t, marketOrder(t, matchA, true, "0", "0.02"))
	require.Len(t, result.Fills, 1)
	require.Equal(t, nearID, result.Fills[0].MakerOrderID)
	require.Equal(t, matchFixedDec(t, "60200"), result.Fills[0].Price.Dec())
	require.Equal(t, matchFixedDec(t, "0.01"), result.Fills[0].SizeBase.Dec())
	require.Equal(t, []uint256.Int{result.Taker.ID}, result.Cancelled)
	require.Equal(t, [][]string{{types.EventOrderCancelled, result.Taker.ID.Dec(), matchA.Hex()}},
		matchEvents(t, result.Events, types.EventOrderCancelled))
	requireReleased(t, e, result.Taker.ID, types.OrderStatusCancelled, "0.01")
	require.Equal(t, []string{farID.Dec()}, e.bookIDs(t, types.SideSell))

	// An explicit acceptablePrice replaces the default bound.
	a := newMatchEnv(t, matchFixed(t, "60000"))
	a.mustPlace(t, limitOrder(t, matchB, false, types.TimeInForceGTC, "60500", "0.02"))
	_, err = a.place(t, marketOrder(t, matchA, true, "60100", "0.02"))
	require.ErrorIs(t, err, types.ErrSlippageExceeded)
	cheapID := a.mustPlace(t, limitOrder(t, matchC, false, types.TimeInForceGTC, "60000", "0.01")).Taker.ID
	accepted := a.mustPlace(t, marketOrder(t, matchA, true, "60100", "0.02"))
	require.Len(t, accepted.Fills, 1)
	require.Equal(t, cheapID, accepted.Fills[0].MakerOrderID)
	require.Equal(t, matchFixedDec(t, "0.01"), accepted.Taker.FilledBase.Dec())
	require.Equal(t, types.OrderStatusCancelled, accepted.Taker.Status)

	// The default bound is mark x (1 +- 5,000 ppm) rounded to the 0.1 tick
	// toward the mark; a limit order is bounded by its limit price.
	odd := newMatchEnv(t, matchFixed(t, "60000.05"))
	for _, tc := range []struct {
		order matchOrder
		bound string
	}{
		{marketOrder(t, matchA, true, "0", "0.01"), "60300"},
		{marketOrder(t, matchA, false, "0", "0.01"), "59700.1"},
		{marketOrder(t, matchA, true, "60100", "0.01"), "60100"},
		{limitOrder(t, matchA, false, types.TimeInForceIOC, "59950.5", "0.01"), "59950.5"},
	} {
		order := odd.register(t, odd.ctx, tc.order)
		bound, err := odd.k.slippageBound(odd.ctx, odd.market, order, types.DefaultParams().DefaultSlippagePpm)
		require.NoError(t, err)
		require.Equal(t, matchFixedDec(t, tc.bound), bound.Dec())
	}

	stale := newMatchEnv(t, uint256.Int{})
	stale.mustPlace(t, limitOrder(t, matchB, false, types.TimeInForceGTC, "60000", "0.01"))
	_, err = stale.place(t, marketOrder(t, matchA, true, "0", "0.01"))
	require.ErrorIs(t, err, types.ErrPriceStale)
}

type matchGolden struct {
	Market struct {
		ID         uint64 `json:"id"`
		Symbol     string `json:"symbol"`
		SzDecimals uint8  `json:"sz_decimals"`
		PxDecimals uint8  `json:"px_decimals"`
	} `json:"market"`
	MarkPrice   string                 `json:"mark_price"`
	TakerFeePpm uint64                 `json:"taker_fee_ppm"`
	MakerFeePpm int64                  `json:"maker_fee_ppm"`
	Accounts    map[string]string      `json:"accounts"`
	Operations  []matchGoldenOperation `json:"operations"`
	FinalBook   struct {
		Bids []matchGoldenBookEntry `json:"bids"`
		Asks []matchGoldenBookEntry `json:"asks"`
	} `json:"final_book"`
}

type matchGoldenOperation struct {
	Op              string            `json:"op"`
	Account         string            `json:"account"`
	IsLong          bool              `json:"is_long"`
	Type            types.OrderType   `json:"type"`
	TimeInForce     types.TimeInForce `json:"time_in_force"`
	LimitPrice      string            `json:"limit_price"`
	AcceptablePrice string            `json:"acceptable_price"`
	SizeBase        string            `json:"size_base"`
	OrderID         string            `json:"order_id"`
	Expect          struct {
		Error      string            `json:"error"`
		OrderID    string            `json:"order_id"`
		Status     types.OrderStatus `json:"status"`
		FilledBase string            `json:"filled_base"`
		Fills      []matchGoldenFill `json:"fills"`
		Cancelled  []string          `json:"cancelled"`
	} `json:"expect"`
}

type matchGoldenFill struct {
	MakerOrderID string `json:"maker_order_id"`
	Maker        string `json:"maker"`
	Price        string `json:"price"`
	SizeBase     string `json:"size_base"`
	SizeUsd      string `json:"size_usd"`
	MakerFeeUsd  string `json:"maker_fee_usd"`
	TakerFeeUsd  string `json:"taker_fee_usd"`
}

type matchGoldenBookEntry struct {
	OrderID    string `json:"order_id"`
	Account    string `json:"account"`
	Price      string `json:"price"`
	SizeBase   string `json:"size_base"`
	FilledBase string `json:"filled_base"`
	Sequence   uint64 `json:"sequence"`
}

var matchGoldenErrors = map[string]error{
	"LXP_PERPS_NO_LIQUIDITY":          types.ErrNoLiquidity,
	"LXP_PERPS_SLIPPAGE_EXCEEDED":     types.ErrSlippageExceeded,
	"LXP_PERPS_FOK_NOT_FILLED":        types.ErrFokNotFilled,
	"LXP_PERPS_POST_ONLY_WOULD_CROSS": types.ErrPostOnlyWouldCross,
}

func matchWord(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value, err := uint256.FromDecimal(decimal)
	require.NoError(t, err)
	return *value
}

func matchGoldenBook(t *testing.T, e *matchEnv, side byte, names map[common.Address]string) []matchGoldenBookEntry {
	t.Helper()
	entries := []matchGoldenBookEntry{}
	for _, order := range e.book(t, side) {
		entries = append(entries, matchGoldenBookEntry{
			OrderID: order.ID.Dec(), Account: names[order.Account], Price: order.LimitPrice.Dec(),
			SizeBase: order.SizeBase.Dec(), FilledBase: order.FilledBase.Dec(), Sequence: order.Sequence,
		})
	}
	return entries
}

// TestMatchingGolden replays the 40 operations of testdata/matching_golden.json
// on market 0 and compares every fill, cancellation, Fill and OrderCancelled
// event and the final book with the expectation derived by hand from
// contracts/perps.md section 6: price then placement sequence, self-trade
// prevention, the 32-fill cap and the slippage bound.
func TestMatchingGolden(t *testing.T) {
	bz, err := os.ReadFile(filepath.Join("testdata", "matching_golden.json"))
	require.NoError(t, err)
	var golden matchGolden
	require.NoError(t, json.Unmarshal(bz, &golden))
	require.Len(t, golden.Operations, 40)

	accounts := map[string]common.Address{}
	names := map[common.Address]string{}
	for name, hex := range golden.Accounts {
		accounts[name] = common.HexToAddress(hex)
		names[common.HexToAddress(hex)] = name
	}
	extra := make([]common.Address, 0, len(accounts))
	for _, name := range []string{"A", "B", "C", "D"} {
		address, ok := accounts[name]
		require.True(t, ok, name)
		extra = append(extra, address)
	}
	e := newMatchEnv(t, matchWord(t, golden.MarkPrice), extra...)
	require.Equal(t, golden.Market.ID, e.market.ID)
	require.Equal(t, golden.Market.Symbol, e.market.Symbol)
	require.Equal(t, golden.Market.SzDecimals, e.market.SzDecimals)
	require.Equal(t, golden.Market.PxDecimals, e.market.PxDecimals)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	require.Equal(t, golden.TakerFeePpm, params.FeeTiers[0].TakerFeePpm)
	require.Equal(t, golden.MakerFeePpm, params.FeeTiers[0].MakerFeePpm)
	require.Equal(t, uint64(32), params.MaxFillsPerOrder)

	for i, op := range golden.Operations {
		switch op.Op {
		case "place":
			account, ok := accounts[op.Account]
			require.True(t, ok, "operation %d account %s", i+1, op.Account)
			result, err := e.place(t, matchOrder{
				account: account, isLong: op.IsLong, orderType: op.Type, timeInForce: op.TimeInForce,
				limitPrice: matchWord(t, op.LimitPrice), acceptablePrice: matchWord(t, op.AcceptablePrice),
				sizeBase: matchWord(t, op.SizeBase),
			})
			if op.Expect.Error != "" {
				expected, known := matchGoldenErrors[op.Expect.Error]
				require.True(t, known, "operation %d error %s", i+1, op.Expect.Error)
				require.ErrorIs(t, err, expected, "operation %d", i+1)
				require.Contains(t, err.Error(), op.Expect.Error, "operation %d", i+1)
				continue
			}
			require.NoError(t, err, "operation %d", i+1)
			require.Equal(t, op.Expect.OrderID, result.Taker.ID.Dec(), "operation %d", i+1)
			require.Equal(t, op.Expect.Status, result.Taker.Status, "operation %d", i+1)
			require.Equal(t, op.Expect.FilledBase, result.Taker.FilledBase.Dec(), "operation %d", i+1)

			fills := make([]matchGoldenFill, 0, len(result.Fills))
			for _, fill := range result.Fills {
				fills = append(fills, matchGoldenFill{
					MakerOrderID: fill.MakerOrderID.Dec(), Maker: names[fill.Maker], Price: fill.Price.Dec(),
					SizeBase: fill.SizeBase.Dec(), SizeUsd: fill.SizeUsd.Dec(), MakerFeeUsd: fill.MakerFeeUsd.String(),
					TakerFeeUsd: fill.TakerFeeUsd.Dec(),
				})
			}
			require.Equal(t, op.Expect.Fills, fills, "operation %d", i+1)

			cancelled := make([]string, 0, len(result.Cancelled))
			for _, id := range result.Cancelled {
				cancelled = append(cancelled, id.Dec())
			}
			require.Equal(t, op.Expect.Cancelled, cancelled, "operation %d", i+1)

			wantEvents := [][]string{}
			for _, id := range op.Expect.Cancelled {
				if id == op.Expect.OrderID {
					continue
				}
				wantEvents = append(wantEvents, []string{types.EventOrderCancelled, id,
					e.order(t, matchWord(t, id)).Account.Hex()})
			}
			for _, fill := range op.Expect.Fills {
				wantEvents = append(wantEvents, fillArgs(fill.MakerOrderID, op.Expect.OrderID, accounts[fill.Maker],
					account, op.IsLong, fill.Price, fill.SizeUsd, fill.MakerFeeUsd, fill.TakerFeeUsd))
			}
			for _, id := range op.Expect.Cancelled {
				if id == op.Expect.OrderID {
					wantEvents = append(wantEvents, []string{types.EventOrderCancelled, id, account.Hex()})
				}
			}
			require.Equal(t, wantEvents, matchEvents(t, result.Events, types.EventOrderCancelled, types.EventFill),
				"operation %d", i+1)
			for _, id := range op.Expect.Cancelled {
				require.Equal(t, types.OrderStatusCancelled, e.order(t, matchWord(t, id)).Status, "operation %d", i+1)
			}
		case "cancel":
			order := e.order(t, matchWord(t, op.OrderID))
			require.Equal(t, types.OrderStatusResting, order.Status, "operation %d", i+1)
			cacheCtx, write := e.ctx.CacheContext()
			event, err := e.k.cancelResting(cacheCtx, order)
			require.NoError(t, err, "operation %d", i+1)
			write()
			require.Equal(t, op.Expect.Cancelled, []string{order.ID.Dec()}, "operation %d", i+1)
			require.Equal(t, []string{types.EventOrderCancelled, op.OrderID, order.Account.Hex()},
				matchArgStrings(t, event), "operation %d", i+1)
			requireReleased(t, e, order.ID, types.OrderStatusCancelled, "0")
		default:
			t.Fatalf("operation %d has unknown op %q", i+1, op.Op)
		}
	}

	require.Equal(t, golden.FinalBook.Bids, matchGoldenBook(t, e, types.SideBuy, names))
	require.Equal(t, golden.FinalBook.Asks, matchGoldenBook(t, e, types.SideSell, names))
}
