package keeper_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receiptstore "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var scenarioAccounts = [...]common.Address{
	common.HexToAddress("0x0000000000000000000000000000000000000011"),
	common.HexToAddress("0x0000000000000000000000000000000000000022"),
	common.HexToAddress("0x0000000000000000000000000000000000000033"),
}

type scenarioEntry struct{ Key, Value []byte }
type scenarioBlock struct {
	Name   string     `json:"name"`
	Events [][]string `json:"events"`
}
type scenarioSnapshot struct {
	Block         scenarioBlock
	Store, Prices []scenarioEntry
	Receipt       []byte
	ContextEvents sdk.Events
	Custody       []byte
}
type scenarioEnv struct {
	t             *testing.T
	app           *app.App
	k             *keeper.Keeper
	ctx           sdk.Context
	key, priceKey sdk.StoreKey
	token         common.Address
	snapshots     []scenarioSnapshot
}

func newScenarioEnv(t *testing.T, collateral uint64, sweep uint64) *scenarioEnv {
	t.Helper()
	application := app.Setup(t, false, false, false)
	ctx := application.GetContextForDeliverTx(nil).WithBlockHeight(10).
		WithBlockTime(time.Unix(3600, 0).UTC()).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, priceKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, priceKey)
	prices := pricefeedkeeper.NewKeeper(priceKey, application.StakingKeeper, application.AccountKeeper, &application.EvmKeeper)
	e := &scenarioEnv{t: t, app: application, ctx: ctx, key: key, priceKey: priceKey,
		k: keeper.NewKeeper(key, &application.EvmKeeper, prices)}
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(invUSDLSupply), invHolder)
	require.NoError(t, err)
	deployer := common.HexToAddress("0x0000000000000000000000000000000000000044")
	nonce := application.EvmKeeper.GetNonce(ctx, deployer)
	_, err = application.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(invUSDLCreation), args...))
	require.NoError(t, err)
	e.token = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, application.EvmKeeper.GetCode(ctx, e.token))
	params := types.DefaultParams()
	params.CollateralToken, params.SweepPositionsPerBlock = e.token, sweep
	require.NoError(t, e.k.UpdateParams(ctx, e.k.Authority(), params))
	require.NoError(t, e.k.SetMarket(ctx, e.k.Authority(), listedMarket(0, "BTC", 5, 1, types.TierClassA, 20_000, 500_000_000)))
	mark := invNum(60_000, 18)
	e.k.SetMarketState(ctx, types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark,
		MarkPrice: mark, LastTradePrice: mark, Fresh: true, PriceTimestampMs: 3_600_000,
		LastIndexHour: 1, LastSampleTime: 3600})
	for i, address := range scenarioAccounts {
		amount := uint64(100_000_000_000)
		if i < 2 {
			amount = 1_000_000_000
		}
		if i == 0 {
			amount = collateral
		}
		e.deposit(address, amount)
	}
	for _, address := range []common.Address{e.k.FeesAccountAddress(), e.k.InsuranceAccountAddress()} {
		_, _, err := e.k.EnsureAccount(ctx, address)
		require.NoError(t, err)
	}
	e.setPrice(60_000)
	e.checkInvariants()
	return e
}

func (e *scenarioEnv) deposit(address common.Address, amount uint64) {
	e.t.Helper()
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(e.k.ModuleEVMAddress().Bytes(), 32)...)
	input = append(input, new(big.Int).SetUint64(amount).FillBytes(make([]byte, 32))...)
	ret, err := e.app.EvmKeeper.CallEVM(e.ctx, invHolder, &e.token, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret)
	_, err = e.k.Deposit(e.ctx, address, *uint256.NewInt(amount))
	require.NoError(e.t, err)
}

func (e *scenarioEnv) pair(size uint256.Int) []types.EventRecord {
	e.t.Helper()
	price := invNum(60_000, 18)
	oi, err := e.k.CheckOpenInterest(e.ctx, keeper.OpenInterestFill{MarketID: 0,
		SizeBase: size, PriceUsd: price, BuyerOpensBase: size, SellerOpensBase: size})
	require.NoError(e.t, err)
	require.NotNil(e.t, oi)
	events := []types.EventRecord{*oi}
	for i, address := range scenarioAccounts[:2] {
		result, err := e.k.ApplyFill(e.ctx, keeper.PositionFill{Account: address, MarketID: 0,
			IsLong: i == 0, SizeBase: size, Price: price, Leverage: invNum(1000, 18), Kind: keeper.FillKindTrade})
		require.NoError(e.t, err)
		require.Equal(e.t, *uint256.NewInt(uint64(i + 1)), result.PositionID)
		events = append(events, result.Events...)
		events = append(events, result.TradeSettledEvents(new(big.Int), new(big.Int))...)
	}
	return events
}

func (e *scenarioEnv) begin(seconds int64, price int64) {
	e.t.Helper()
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(time.Unix(seconds, 0).UTC()).
		WithEventManager(sdk.NewEventManager())
	e.app.EvmKeeper.BeginBlock(e.ctx)
	e.setPrice(price)
}

func (e *scenarioEnv) setPrice(price int64) {
	e.t.Helper()
	aggregate, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: sdk.NewIntWithDecimal(price, 18),
		MarkPrice: sdk.NewIntWithDecimal(price, 18), Basis: sdk.ZeroInt(), TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()),
		Height: e.ctx.BlockHeight(), Sources: 1})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.priceKey).Set(pricefeedtypes.AggregateKey(0), aggregate)
}

func (e *scenarioEnv) prices() []types.EventRecord {
	e.t.Helper()
	events, err := e.k.UpdatePrices(e.ctx)
	require.NoError(e.t, err)
	return events
}

func (e *scenarioEnv) account(address common.Address) types.Account {
	e.t.Helper()
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account
}

func (e *scenarioEnv) position(id uint64) types.Position {
	e.t.Helper()
	var position types.Position
	raw := e.ctx.KVStore(e.key).Get(types.PositionKey(*uint256.NewInt(id)))
	require.NotNil(e.t, raw)
	require.NoError(e.t, position.Unmarshal(raw))
	return position
}

func (e *scenarioEnv) checkInvariants() {
	e.t.Helper()
	registry := &invRegistry{routes: make(map[string]sdk.Invariant)}
	keeper.RegisterInvariants(registry, e.k)
	names := make([]string, 0, len(registry.routes))
	for name := range registry.routes {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Len(e.t, names, 9)
	for _, name := range names {
		message, broken := registry.routes[name](e.ctx)
		assert.False(e.t, broken, "height %d: %s: %s", e.ctx.BlockHeight(), name, message)
	}
}

func scenarioStore(store sdk.KVStore) []scenarioEntry {
	entries := make([]scenarioEntry, 0)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		entries = append(entries, scenarioEntry{Key: bytes.Clone(iterator.Key()), Value: bytes.Clone(iterator.Value())})
	}
	return entries
}

func (e *scenarioEnv) finish(name string, events []types.EventRecord, endBlock bool) {
	e.t.Helper()
	if !endBlock {
		require.NoError(e.t, e.k.WriteEndBlockReceipt(e.ctx, events))
	}
	e.checkInvariants()
	for _, event := range e.ctx.EventManager().Events() {
		assert.NotEqual(e.t, "perps_endblock_error", event.Type, "%+v", event)
	}
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, e.ctx.BlockHeight())
	receipt, err := e.app.EvmKeeper.GetTransientReceipt(e.ctx, hash, math.MaxUint32-2)
	var encoded []byte
	if len(events) == 0 {
		require.ErrorIs(e.t, err, receiptstore.ErrNotFound)
	} else {
		require.NoError(e.t, err)
		require.Equal(e.t, hash.Hex(), receipt.TxHashHex)
		require.Equal(e.t, uint64(e.ctx.BlockHeight()), receipt.BlockNumber)
		require.Len(e.t, receipt.Logs, len(events))
		for i, event := range events {
			topics, data, err := perpsabi.PackEvent(event.Name, event.Args...)
			require.NoError(e.t, err)
			log := receipt.Logs[i]
			require.Equal(e.t, common.HexToAddress("0x1015").Hex(), log.Address)
			require.Equal(e.t, uint32(i), log.Index)
			require.True(e.t, log.Synthetic)
			require.Equal(e.t, data, log.Data)
			require.Len(e.t, log.Topics, len(topics))
			for j, topic := range topics {
				require.Equal(e.t, topic.Hex(), log.Topics[j])
			}
		}
		encoded, err = receipt.Marshal()
		require.NoError(e.t, err)
	}
	input := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(e.k.ModuleEVMAddress().Bytes(), 32)...)
	custody, err := e.app.EvmKeeper.StaticCallEVM(e.ctx, e.k.ModuleAddress(), &e.token, input)
	require.NoError(e.t, err)
	require.Len(e.t, custody, 32)
	e.snapshots = append(e.snapshots, scenarioSnapshot{Block: scenarioBlock{Name: name, Events: recordStrings(events)},
		Store: scenarioStore(e.ctx.KVStore(e.key)), Prices: scenarioStore(e.ctx.KVStore(e.priceKey)),
		Receipt: bytes.Clone(encoded), ContextEvents: e.ctx.EventManager().Events(), Custody: bytes.Clone(custody)})
}

func (e *scenarioEnv) bid(price int64) []types.EventRecord {
	e.t.Helper()
	result, err := e.k.PlaceOrder(e.ctx, scenarioAccounts[2], keeper.OrderParams{MarketID: 0,
		IsLong: true, OrderType: uint8(types.OrderTypeLimit), TimeInForce: uint8(types.TimeInForceGTC),
		SizeUsd: invNum(price, 18), LimitPrice: invNum(price, 18), Leverage: invNum(10, 18)})
	require.NoError(e.t, err)
	require.Empty(e.t, result.Fills)
	order, err := e.k.GetOrder(e.ctx, result.OrderID)
	require.NoError(e.t, err)
	require.Equal(e.t, types.OrderStatusResting, order.Status)
	return result.Events
}

func scenarioFunding(e *scenarioEnv, bounded bool) {
	e.pair(invNum(1, 16))
	e.checkInvariants()
	e.begin(7200, 60_000)
	records := e.k.EndBlock(e.ctx)
	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(e.t, err)
	require.Equal(e.t, *uint256.NewInt(750_000_000_000_000_000), state.FundingIndex)
	require.Equal(e.t, uint64(2), state.LastIndexHour)
	require.Equal(e.t, *uint256.NewInt(999_992_500), e.account(scenarioAccounts[0]).Collateral)
	if bounded {
		require.Equal(e.t, *uint256.NewInt(1_000_000_000), e.account(scenarioAccounts[1]).Collateral)
		require.Equal(e.t, uint256.Int{}, e.position(2).FundingBasis)
		require.Equal(e.t, wordBytes(*uint256.NewInt(2)), e.ctx.KVStore(e.key).Get(types.SweepCursorKey))
	} else {
		require.Equal(e.t, *uint256.NewInt(1_000_007_500), e.account(scenarioAccounts[1]).Collateral)
	}
	e.finish("funding_hour", records, true)
	e.begin(7201, 60_000)
	records = e.k.EndBlock(e.ctx)
	require.Equal(e.t, *uint256.NewInt(1_000_007_500), e.account(scenarioAccounts[1]).Collateral)
	require.Equal(e.t, *uint256.NewInt(7_500_000_000_000_000), e.position(2).FundingBasis)
	e.finish("sweep", records, true)
	e.begin(7202, 60_000)
	records = e.k.EndBlock(e.ctx)
	require.Equal(e.t, *uint256.NewInt(999_992_500), e.account(scenarioAccounts[0]).Collateral)
	require.Equal(e.t, *uint256.NewInt(1_000_007_500), e.account(scenarioAccounts[1]).Collateral)
	e.finish("sweep_wrap", records, true)
}

func TestScenarioDeterminism(t *testing.T) {
	fundingBytes, err := os.ReadFile("testdata/funding_golden.json")
	require.NoError(t, err)
	liquidationBytes, err := os.ReadFile("testdata/liquidation_golden.json")
	require.NoError(t, err)
	goldens := make(map[string][]scenarioBlock)
	require.NoError(t, json.Unmarshal(fundingBytes, &goldens))
	require.NoError(t, json.Unmarshal(liquidationBytes, &goldens))
	scenarios := []struct {
		name              string
		collateral, sweep uint64
		run               func(*scenarioEnv)
	}{
		{"open", 1_000_000_000, 512, func(e *scenarioEnv) {
			e.begin(3601, 60_000)
			records := e.pair(invNum(1, 16))
			require.Equal(e.t, invNum(1, 16), e.position(1).SizeBase)
			require.Equal(e.t, invNum(1, 16), e.position(2).SizeBase)
			require.True(e.t, e.position(1).IsLong)
			require.False(e.t, e.position(2).IsLong)
			e.finish("open", records, false)
		}},
		{"fill", 1_000_000_000, 512, func(e *scenarioEnv) {
			e.begin(3601, 60_000)
			maker, err := e.k.PlaceOrder(e.ctx, scenarioAccounts[1], keeper.OrderParams{MarketID: 0,
				OrderType: uint8(types.OrderTypeLimit), TimeInForce: uint8(types.TimeInForceGTC),
				SizeUsd: invNum(600, 18), LimitPrice: invNum(60_000, 18), Leverage: invNum(10, 18)})
			require.NoError(e.t, err)
			require.Empty(e.t, maker.Fills)
			e.finish("resting_order", maker.Events, false)
			e.begin(3602, 60_000)
			taker, err := e.k.PlaceOrder(e.ctx, scenarioAccounts[0], keeper.OrderParams{MarketID: 0, IsLong: true,
				OrderType: uint8(types.OrderTypeMarket), TimeInForce: uint8(types.TimeInForceIOC),
				SizeUsd: invNum(600, 18), AcceptablePrice: invNum(60_000, 18), Leverage: invNum(10, 18)})
			require.NoError(e.t, err)
			require.Len(e.t, taker.Fills, 1)
			require.Equal(e.t, invNum(1, 16), taker.Fills[0].SizeBase)
			require.Equal(e.t, invNum(60_000, 18), taker.Fills[0].Price)
			require.Equal(e.t, maker.OrderID, taker.Fills[0].MakerOrderID)
			require.Equal(e.t, *uint256.NewInt(999_730_000), e.account(scenarioAccounts[0]).Collateral)
			require.Equal(e.t, *uint256.NewInt(999_910_000), e.account(scenarioAccounts[1]).Collateral)
			require.Equal(e.t, *uint256.NewInt(360_000), e.account(e.k.FeesAccountAddress()).Collateral)
			order, err := e.k.GetOrder(e.ctx, maker.OrderID)
			require.NoError(e.t, err)
			require.Equal(e.t, types.OrderStatusFilled, order.Status)
			e.finish("fill", taker.Events, false)
		}},
		{"funding_hour", 1_000_000_000, 512, func(e *scenarioEnv) { scenarioFunding(e, false) }},
		{"sweep", 1_000_000_000, 1, func(e *scenarioEnv) { scenarioFunding(e, true) }},
		{"liquidation", 1_029_000_000, 512, func(e *scenarioEnv) {
			e.pair(invNum(1, 18))
			e.bid(59_000)
			e.checkInvariants()
			e.begin(3601, 59_000)
			records := e.k.EndBlock(e.ctx)
			account := e.account(scenarioAccounts[0])
			require.Zero(e.t, account.PositionCount)
			require.Equal(e.t, *uint256.NewInt(2_450_000), account.Collateral)
			require.Nil(e.t, e.ctx.KVStore(e.key).Get(types.PositionKey(*uint256.NewInt(1))))
			e.finish("liquidation", records, true)
			e.begin(3602, 59_000)
			e.finish("after_liquidation", e.k.EndBlock(e.ctx), true)
		}},
		{"backstop", 1_019_000_000, 512, func(e *scenarioEnv) {
			e.deposit(e.k.InsuranceAccountAddress(), 100_000_000)
			e.pair(invNum(1, 18))
			e.checkInvariants()
			e.begin(3601, 59_000)
			records := e.prices()
			backstop, err := e.k.Backstop(e.ctx, scenarioAccounts[0])
			require.NoError(e.t, err)
			records = append(records, backstop...)
			require.Equal(e.t, e.k.InsuranceAccountAddress(), e.position(1).Account)
			require.Zero(e.t, e.account(scenarioAccounts[0]).PositionCount)
			require.Equal(e.t, uint256.Int{}, e.account(scenarioAccounts[0]).Collateral)
			require.Equal(e.t, *uint256.NewInt(119_000_000), e.account(e.k.InsuranceAccountAddress()).Collateral)
			e.finish("backstop", records, false)
			e.begin(3602, 59_000)
			records = e.bid(59_000)
			closed, err := e.k.CloseBackstopPositions(e.ctx)
			require.NoError(e.t, err)
			require.Empty(e.t, closed.Failures)
			require.Equal(e.t, uint64(1), closed.Slices)
			require.Zero(e.t, e.account(e.k.InsuranceAccountAddress()).PositionCount)
			require.Equal(e.t, *uint256.NewInt(92_450_000), e.account(e.k.InsuranceAccountAddress()).Collateral)
			e.finish("backstop_close", append(records, closed.Events...), false)
		}},
		{"adl", 1_000_000_000, 512, func(e *scenarioEnv) {
			e.pair(invNum(1, 18))
			e.checkInvariants()
			e.begin(3601, 59_000)
			records := e.prices()
			for _, address := range scenarioAccounts[:2] {
				settled, err := e.k.Settle(e.ctx, address)
				require.NoError(e.t, err)
				records = append(records, settled...)
			}
			e.finish("previous_mark", records, false)
			e.begin(3602, 58_000)
			records = e.prices()
			for _, address := range scenarioAccounts[:2] {
				settled, err := e.k.Settle(e.ctx, address)
				require.NoError(e.t, err)
				records = append(records, settled...)
			}
			require.Equal(e.t, invNum(1000, 18), e.account(scenarioAccounts[0]).DeficitUsd)
			require.Equal(e.t, uint256.Int{}, e.account(e.k.InsuranceAccountAddress()).Collateral)
			adl, err := e.k.ExecuteADL(e.ctx, []uint256.Int{*uint256.NewInt(1)}, map[uint64]uint256.Int{0: invNum(59_000, 18)})
			require.NoError(e.t, err)
			matched, executed := 0, 0
			for _, event := range adl {
				switch event.Name {
				case types.EventADLMatched:
					matched++
					require.Equal(e.t, "2", fmt.Sprint(event.Args[1]))
					require.Equal(e.t, "59000000000000000000000", fmt.Sprint(event.Args[3]))
					require.Equal(e.t, "59000000000000000000000", fmt.Sprint(event.Args[4]))
				case types.EventADLExecuted:
					executed++
					require.Equal(e.t, "1", fmt.Sprint(event.Args[0]))
					require.Equal(e.t, "59000000000000000000000", fmt.Sprint(event.Args[1]))
				}
			}
			require.Equal(e.t, 1, matched)
			require.Equal(e.t, 1, executed)
			for _, address := range scenarioAccounts[:2] {
				require.Zero(e.t, e.account(address).PositionCount)
			}
			e.finish("adl", append(records, adl...), false)
		}},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			var replays [2][]scenarioSnapshot
			for replay := range replays {
				t.Run(fmt.Sprintf("replay_%d", replay+1), func(t *testing.T) {
					env := newScenarioEnv(t, scenario.collateral, scenario.sweep)
					scenario.run(env)
					require.NotEmpty(t, env.snapshots)
					replays[replay] = env.snapshots
					if golden, found := goldens[scenario.name]; found {
						blocks := make([]scenarioBlock, len(env.snapshots))
						for i, snapshot := range env.snapshots {
							blocks[i] = snapshot.Block
						}
						require.Equal(t, golden, blocks)
					}
				})
			}
			require.NotEmpty(t, replays[0])
			require.NotEmpty(t, replays[1])
			require.Equal(t, replays[0], replays[1], "every block must replay identical raw keys, values, ordered logs and receipts")
		})
	}
	require.Len(t, goldens, 3)
	for _, name := range []string{"funding_hour", "sweep", "liquidation"} {
		require.Contains(t, goldens, name)
	}
}
