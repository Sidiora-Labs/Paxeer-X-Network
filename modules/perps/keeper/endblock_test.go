package keeper_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// endblockHour is the whole hour of block time liquidationGenesisTime opens.
var endblockHour = uint64(liquidationGenesisTime.Unix()) / keeper.SecondsPerHour

// endblockEnv is liquidationEnv over the real pricefeed keeper, each on its
// own in-memory store, at block height 10 and liquidationGenesisTime.
type endblockEnv struct {
	*liquidationEnv
	pfKey sdk.StoreKey
}

func newEndblockEnv(t *testing.T, marketIDs ...uint64) *endblockEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).WithBlockTime(liquidationGenesisTime).
		WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = launchpadtest.WithStore(ctx, key)
	k := keeper.NewKeeper(key, &app.EvmKeeper, pricefeed)
	for _, id := range marketIDs {
		require.NoError(t, k.SetMarket(ctx, k.Authority(), types.Market{
			ID: id, Symbol: fmt.Sprintf("M%d", id), Name: fmt.Sprintf("Market %d", id), OracleSymbol: fmt.Sprintf("M%d", id),
			SzDecimals: 5, PxDecimals: 1, MaxLeverage: types.VenueMaxLeverage, TierClass: types.TierClassA,
			ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, types.USDDecimals),
			OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, types.USDDecimals),
			FundingCapPpmPerHour: 40_000, Enabled: true,
		}))
	}
	word := uint256.NewInt(10_000).Bytes32()
	ctx.KVStore(key).Set(types.SequenceKey(types.SequenceOrderID), word[:])
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePositionID), word[:])
	return &endblockEnv{liquidationEnv: &liquidationEnv{t: t, ctx: ctx, k: k, key: key, nextID: 100}, pfKey: pfKey}
}

// setAggregate stores the pricefeed aggregate GetPrice reads for a market,
// index and pricefeed mark both at usd.
func (e *endblockEnv) setAggregate(marketID uint64, usd int64, timestampMs int64) {
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(usd, types.USDDecimals),
		MarkPrice:   sdk.NewIntWithDecimal(usd, types.USDDecimals),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(timestampMs),
		Height:      e.ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(marketID), bz)
}

func (e *endblockEnv) marketState(marketID uint64) types.MarketState {
	state, err := e.k.GetMarketState(e.ctx, marketID)
	require.NoError(e.t, err)
	return state
}

// errorEvents are the attributes of every perps_endblock_error event of the
// block.
func (e *endblockEnv) errorEvents() []map[string]string {
	var out []map[string]string
	for _, event := range e.ctx.EventManager().Events() {
		if event.Type != keeper.EventTypeEndBlockError {
			continue
		}
		attributes := make(map[string]string, len(event.Attributes))
		for _, attribute := range event.Attributes {
			attributes[string(attribute.Key)] = string(attribute.Value)
		}
		out = append(out, attributes)
	}
	return out
}

func endblockArgs(record types.EventRecord) []string {
	out := []string{record.Name}
	for _, arg := range record.Args {
		out = append(out, fmt.Sprint(arg))
	}
	return out
}

func endblockFirst(t *testing.T, records []types.EventRecord, name string) types.EventRecord {
	t.Helper()
	for _, record := range records {
		if record.Name == name {
			return record
		}
	}
	require.Failf(t, "missing record", "no %s record in the pass", name)
	return types.EventRecord{}
}

func TestEndblockStepOrder(t *testing.T) {
	e := newEndblockEnv(t, 0)
	nowMs := liquidationGenesisTime.UnixMilli()
	e.setMark(usdAmount(60_000))

	// A long of 1 BTC at 60,000 with 1,029 USD of collateral: at mark 59,000 it
	// loses 1,000 USD and pays 0.7375 USD of funding, leaving 28.2625 USD of
	// equity under the 29.5 USD maintenance of 59,000 x 5 bps.
	holder := e.address(1)
	positionID := e.pair(7, holder, 1_029_000_000, baseAmount(100_000), usdAmount(60_000))
	e.maker(2, usdAmount(59_000), baseAmount(200_000))

	// A STOP_LIMIT sell of 0.1 BTC falling through 59,500 that rests at 61,000.
	trader := e.address(3)
	e.account(trader, 100_000_000_000)
	e.nextID++
	e.sequence++
	stop := types.Order{ID: *uint256.NewInt(e.nextID), Account: trader, MarketID: 0, IsLong: false,
		Type: types.OrderTypeStopLimit, TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusUntriggered,
		LimitPrice: usdAmount(61_000), TriggerPrice: usdAmount(59_500), SizeBase: baseAmount(10_000),
		Leverage: usdAmount(10), Sequence: e.sequence}
	store := e.ctx.KVStore(e.key)
	store.Set(types.OrderKey(stop.ID), stop.Marshal())
	store.Set(types.AccountOrderKey(trader, stop.ID), []byte{})
	traderAccount := e.readAccount(trader)
	traderAccount.OpenOrderCount++
	store.Set(types.AccountKey(trader), traderAccount.Marshal())
	require.NoError(t, e.k.TriggerInsert(e.ctx, stop))

	// The previous index step was the hour before the block, the last premium
	// sample is at block time and the pricefeed moved to 59,000 one second ago.
	state := e.marketState(0)
	state.PriceTimestampMs = uint64(nowMs - 2_000)
	state.LastIndexHour = endblockHour - 1
	state.LastSampleTime = uint64(liquidationGenesisTime.Unix())
	e.k.SetMarketState(e.ctx, state)
	e.setAggregate(0, 59_000, nowMs-1_000)

	records := e.k.EndBlock(e.ctx)
	require.Empty(t, e.errorEvents())

	var order []string
	for _, record := range records {
		switch record.Name {
		case types.EventMarkPriceUpdated, types.EventFundingSettled, types.EventOrderTriggered, types.EventLiquidation:
			order = append(order, record.Name)
		}
	}
	require.Equal(t, []string{
		types.EventMarkPriceUpdated, types.EventFundingSettled, types.EventOrderTriggered, types.EventLiquidation,
	}, order)

	mark := usdAmount(59_000)
	require.Equal(t, []string{types.EventMarkPriceUpdated, "0", mark.Dec(), mark.Dec(), mark.Dec()},
		endblockArgs(endblockFirst(t, records, types.EventMarkPriceUpdated)))
	// Hourly rate 0.01 percent / 8 = 1.25e13; index step 59,000 x 1.25e-5 =
	// 0.7375 USD per BTC on 1 BTC of open interest per side.
	require.Equal(t, []string{types.EventFundingSettled, "0", "12500000000000", "-737500000000000000", "737500000000000000"},
		endblockArgs(endblockFirst(t, records, types.EventFundingSettled)))
	require.Equal(t, endblockArgs(keeper.OrderTriggered(stop, mark)),
		endblockArgs(endblockFirst(t, records, types.EventOrderTriggered)))
	require.Equal(t, endblockArgs(liquidationEvent(positionID, holder, mark, common.Address{})),
		endblockArgs(endblockFirst(t, records, types.EventLiquidation)))

	after := e.marketState(0)
	require.Equal(t, mark, after.MarkPrice)
	require.Equal(t, endblockHour, after.LastIndexHour)
	_, open := e.positionSize(positionID)
	require.False(t, open, "the liquidation closes the whole position")
	triggered, err := e.k.GetOrder(e.ctx, stop.ID)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusResting, triggered.Status, "the triggered STOP_LIMIT rests at its limit")
	// The sweep settled positions 7 and 1,000,007 and the cursor moves past
	// the highest one.
	cursor := e.ctx.KVStore(e.key).Get(types.SweepCursorKey)
	want := uint256.NewInt(1_000_008).Bytes32()
	require.Equal(t, want[:], cursor)
}

func TestEndblockMarketIsolation(t *testing.T) {
	ids := make([]uint64, 18)
	for i := range ids {
		ids[i] = uint64(i)
	}
	e := newEndblockEnv(t, ids...)
	nowMs := liquidationGenesisTime.UnixMilli()
	price := usdAmount(100)
	maxIndex := new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 255), uint256.NewInt(1))
	for _, id := range ids {
		e.setAggregate(id, 100, nowMs-1_000)
		state := types.MarketState{MarketID: id, IndexPrice: price, PricefeedMark: price,
			PriceTimestampMs: uint64(nowMs - 1_000), Fresh: true, MarkPrice: price, LastTradePrice: price,
			LastSampleTime: uint64(liquidationGenesisTime.Unix()), LastIndexHour: endblockHour - 1}
		if id == 3 {
			// Any index step pushes market 3 past the int256 range.
			state.FundingIndex = *maxIndex
		}
		e.k.SetMarketState(e.ctx, state)
	}
	before := e.marketState(3)

	records := e.k.EndBlock(e.ctx)

	failures := e.errorEvents()
	require.Len(t, failures, 1)
	require.Equal(t, keeper.StepFundingIndex, failures[0][keeper.AttributeStep])
	require.Equal(t, "3", failures[0][keeper.AttributeMarketID])
	require.Contains(t, failures[0][keeper.AttributeError], types.ErrOverflow.Error())

	require.Equal(t, before, e.marketState(3), "market 3 keeps its state")
	var settled []string
	for _, record := range records {
		if record.Name == types.EventFundingSettled {
			settled = append(settled, fmt.Sprint(record.Args[0]))
		}
	}
	require.Equal(t, []string{"0", "1", "2", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17"}, settled)
	for _, id := range ids {
		if id == 3 {
			continue
		}
		state := e.marketState(id)
		require.Equal(t, endblockHour, state.LastIndexHour, "market %d", id)
		// 100 USD x the hourly rate 1.25e-5 = 1.25e-3 USD per unit of base.
		require.Equal(t, *uint256.NewInt(1_250_000_000_000_000), state.FundingIndex, "market %d", id)
		require.Equal(t, *uint256.NewInt(12_500_000_000_000), state.HourlyRate, "market %d", id)
	}
}

func TestEndblockNoBorrowingFields(t *testing.T) {
	noBorrowing := func(owner, name string) {
		require.NotContains(t, strings.ToLower(name), "borrow", "%s field %s", owner, name)
	}

	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	structs := map[string][]string{}
	var visit func(typ abi.Type)
	visit = func(typ abi.Type) {
		if typ.T == abi.TupleTy {
			for _, name := range []string{"PositionDetails", "OpenPositionQuote", "ClosePositionQuote"} {
				if strings.HasSuffix(typ.TupleRawName, name) {
					structs[name] = typ.TupleRawNames
				}
			}
			for _, elem := range typ.TupleElems {
				visit(*elem)
			}
		}
		if typ.Elem != nil {
			visit(*typ.Elem)
		}
	}
	for _, method := range parsed.Methods {
		for _, argument := range append(append(abi.Arguments{}, method.Inputs...), method.Outputs...) {
			visit(argument.Type)
		}
	}
	for _, name := range []string{"PositionDetails", "OpenPositionQuote", "ClosePositionQuote"} {
		fields, ok := structs[name]
		require.True(t, ok, "the ABI declares %s", name)
		require.NotEmpty(t, fields)
		for _, field := range fields {
			noBorrowing(name, field)
		}
	}
	tradeSettled, ok := parsed.Events[types.EventTradeSettled]
	require.True(t, ok)
	require.Len(t, tradeSettled.Inputs, 10)
	for _, input := range tradeSettled.Inputs {
		noBorrowing(types.EventTradeSettled, input.Name)
	}

	for _, record := range []any{types.MarketState{}, types.Position{}} {
		typ := reflect.TypeOf(record)
		for i := 0; i < typ.NumField(); i++ {
			noBorrowing(typ.Name(), typ.Field(i).Name)
			noBorrowing(typ.Name(), typ.Field(i).Tag.Get("json"))
		}
	}

	var steps []string
	for _, name := range keeper.EndBlockStepNames() {
		noBorrowing("EndBlock", name)
		steps = append(steps, name)
	}
	require.Equal(t, []string{
		keeper.StepPrices, keeper.StepPremiumSamples, keeper.StepFundingIndex, keeper.StepTriggers, keeper.StepExpiries,
		keeper.StepSweep, keeper.StepLiquidation, keeper.StepDeficits, keeper.StepBackstopCloses,
	}, steps)
}
