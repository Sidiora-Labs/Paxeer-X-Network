package perps_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	ordersNowMs   int64  = 1_700_000_010_000
	ordersCallGas uint64 = 30_000_000
	ordersERC20   string = "../../example/contracts/erc20/ERC20"
	// ordersBalanceSlot is the storage slot of the balanceOf mapping of the
	// example ERC20 (name, symbol and decimals take slots 0 to 2).
	ordersBalanceSlot int64 = 3
)

var (
	ordersPerps    = common.HexToAddress(perps.PerpsAddress)
	ordersTraderA  = common.HexToAddress("0x00000000000000000000000000000000000A11CE")
	ordersTraderB  = common.HexToAddress("0x0000000000000000000000000000000000000B0B")
	ordersKeeperC  = common.HexToAddress("0x0000000000000000000000000000000000000C0C")
	ordersDeployer = common.HexToAddress("0x00000000000000000000000000000000000000D0")
	ordersCloid    = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
)

// ordersKeepers are the application's precompile keepers plus the perps
// keeper, as the application exposes them once it wires the module.
type ordersKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k ordersKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// ordersEnv is the perps precompile at 0x1015 of a real EVM over the real
// perps and pricefeed keepers, with BTC (market 0, tick 0.1, lot 0.00001)
// listed at a fresh price and mark of 60,000 USD and the example ERC20
// runtime at the USDL address of Params.CollateralToken.
type ordersEnv struct {
	t       *testing.T
	k       *perpskeeper.Keeper
	pfKey   sdk.StoreKey
	key     sdk.StoreKey
	stateDB *state.DBImpl
	evm     *vm.EVM
	perps   abi.ABI
	erc20   abi.ABI
	usdl    common.Address
}

func newOrdersEnv(t *testing.T) *ordersEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(ordersNowMs).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := perpskeeper.NewKeeper(perpsKey, &testApp.EvmKeeper, pricefeed)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), perpstypes.Market{
		ID: 0, Symbol: "BTC", Name: "BTC", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1, MaxLeverage: 1000,
		TierClass: perpstypes.TierClassA, ImpactNotionalUsd: sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd: sdk.NewIntWithDecimal(500_000_000, 18), FundingCapPpmPerHour: 40_000, Enabled: true,
	}))

	e := &ordersEnv{t: t, k: k, pfKey: pfKey, key: perpsKey}
	e.setAggregate(ctx, 60_000)
	_, err := k.UpdatePrices(ctx)
	require.NoError(t, err)
	marketState, err := k.GetMarketState(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, ordersE18(60_000), marketState.MarkPrice)

	e.perps, err = perpsabi.ABI()
	require.NoError(t, err)
	abiJSON, err := os.ReadFile(ordersERC20 + ".abi")
	require.NoError(t, err)
	e.erc20, err = abi.JSON(bytes.NewReader(abiJSON))
	require.NoError(t, err)
	bin, err := os.ReadFile(ordersERC20 + ".bin")
	require.NoError(t, err)
	nonce := testApp.EvmKeeper.GetNonce(ctx, ordersDeployer)
	_, err = testApp.EvmKeeper.CallEVM(ctx, ordersDeployer, nil, nil, common.FromHex(strings.TrimSpace(string(bin))))
	require.NoError(t, err)
	runtime := testApp.EvmKeeper.GetCode(ctx, crypto.CreateAddress(ordersDeployer, nonce))
	require.NotEmpty(t, runtime)
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	e.usdl = params.CollateralToken
	testApp.EvmKeeper.SetCode(ctx, e.usdl, runtime)

	precompile, err := perps.NewPrecompile(ordersKeepers{Keepers: testApp.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.stateDB = state.NewDBImpl(ctx, &testApp.EvmKeeper, false)
	blockCtx, err := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))
	contracts := maps.Clone(testApp.EvmKeeper.CustomPrecompiles(ctx))
	contracts[ordersPerps] = precompile
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, contracts)
	return e
}

func (e *ordersEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

// setAggregate stores the fresh BTC pricefeed aggregate at index and mark
// price whole USD.
func (e *ordersEnv) setAggregate(ctx sdk.Context, price int64) {
	e.t.Helper()
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice: sdk.NewIntWithDecimal(price, 18), MarkPrice: sdk.NewIntWithDecimal(price, 18),
		Basis: sdk.ZeroInt(), TimestampMs: uint64(ordersNowMs - 1_000), Height: ctx.BlockHeight(), Sources: 1,
	})
	require.NoError(e.t, err)
	ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
}

// setMark sets the stored BTC mark of this block to price whole USD.
func (e *ordersEnv) setMark(price int64) {
	e.t.Helper()
	marketState, err := e.k.GetMarketState(e.ctx(), 0)
	require.NoError(e.t, err)
	marketState.MarkPrice = ordersE18(price)
	e.k.SetMarketState(e.ctx(), marketState)
}

// fund stores a created perps account holding usdl whole USDL.
func (e *ordersEnv) fund(account common.Address, usdl uint64) {
	var collateral uint256.Int
	collateral.Mul(uint256.NewInt(usdl), uint256.NewInt(1_000_000))
	e.ctx().KVStore(e.key).Set(perpstypes.AccountKey(account),
		perpstypes.Account{Address: account, Collateral: collateral, Created: true}.Marshal())
}

func (e *ordersEnv) account(address common.Address) perpstypes.Account {
	e.t.Helper()
	account, err := e.k.GetAccount(e.ctx(), address)
	require.NoError(e.t, err)
	return account
}

func (e *ordersEnv) order(id uint64) perpstypes.Order {
	e.t.Helper()
	order, err := e.k.GetOrder(e.ctx(), *uint256.NewInt(id))
	require.NoError(e.t, err)
	return order
}

// positionOf is the id of the account's BTC position.
func (e *ordersEnv) positionOf(account common.Address) *big.Int {
	e.t.Helper()
	bz := e.ctx().KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(account, 0))
	require.Len(e.t, bz, 32)
	return new(big.Int).SetBytes(bz)
}

// holdUsdl sets holder's USDL balance and approves allowance to 0x1015.
func (e *ordersEnv) holdUsdl(holder common.Address, balance, allowance int64) {
	e.t.Helper()
	slot := crypto.Keccak256Hash(common.LeftPadBytes(holder.Bytes(), 32),
		common.LeftPadBytes(big.NewInt(ordersBalanceSlot).Bytes(), 32))
	e.stateDB.SetState(e.usdl, slot, common.BigToHash(big.NewInt(balance)))
	input, err := e.erc20.Pack("approve", ordersPerps, big.NewInt(allowance))
	require.NoError(e.t, err)
	ret, _, err := e.evm.Call(holder, e.usdl, input, ordersCallGas, uint256.NewInt(0))
	require.NoError(e.t, err)
	require.Equal(e.t, common.LeftPadBytes([]byte{1}, 32), ret)
}

func (e *ordersEnv) usdlBalance(holder common.Address) *big.Int {
	e.t.Helper()
	input, err := e.erc20.Pack("balanceOf", holder)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(holder, e.usdl, input, ordersCallGas)
	require.NoError(e.t, err)
	return new(big.Int).SetBytes(ret)
}

// call sends a perps method from caller through the EVM and returns the
// decoded outputs, or the revert data when the call reverts.
func (e *ordersEnv) call(caller common.Address, name string, args ...interface{}) ([]interface{}, []byte) {
	e.t.Helper()
	method, ok := e.perps.Methods[name]
	require.True(e.t, ok, name)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err, name)
	ret, _, err := e.evm.Call(caller, ordersPerps, append(common.CopyBytes(method.ID), packed...), ordersCallGas,
		uint256.NewInt(0))
	if err != nil {
		require.ErrorIs(e.t, err, vm.ErrExecutionReverted, name)
		return nil, ret
	}
	out, err := method.Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	return out, nil
}

func (e *ordersEnv) mustCall(caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	out, reverted := e.call(caller, name, args...)
	require.Nil(e.t, reverted, "%s reverted with %x", name, reverted)
	return out
}

// requireRevert asserts the call reverts with exactly the selector of the
// parameterless custom error.
func (e *ordersEnv) requireRevert(customError string, caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	out, reverted := e.call(caller, name, args...)
	require.Nil(e.t, out, name)
	require.Equal(e.t, crypto.Keccak256([]byte(customError + "()"))[:4], reverted, "%s: want %s", name, customError)
}

func (e *ordersEnv) logCount() int { return len(e.stateDB.GetAllLogs()) }

// perpsLogsSince are the logs of 0x1015 after the first from logs.
func (e *ordersEnv) perpsLogsSince(from int) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range e.stateDB.GetAllLogs()[from:] {
		if log.Address == ordersPerps {
			out = append(out, log)
		}
	}
	return out
}

// requireLog asserts log is the named event of 0x1015 with args.
func (e *ordersEnv) requireLog(log *ethtypes.Log, name string, args ...interface{}) {
	e.t.Helper()
	topics, data, err := perpsabi.PackEvent(name, args...)
	require.NoError(e.t, err, name)
	require.Equal(e.t, ordersPerps, log.Address, name)
	require.Equal(e.t, topics, log.Topics, name)
	require.Equal(e.t, data, log.Data, name)
}

// requireHasLog asserts one log of 0x1015 after from is the named event.
func (e *ordersEnv) requireHasLog(from int, name string, args ...interface{}) {
	e.t.Helper()
	topics, data, err := perpsabi.PackEvent(name, args...)
	require.NoError(e.t, err, name)
	for _, log := range e.perpsLogsSince(from) {
		if slicesEqualHashes(log.Topics, topics) && bytes.Equal(log.Data, data) {
			return
		}
	}
	require.Failf(e.t, "missing log", "%s %v", name, args)
}

func slicesEqualHashes(a, b []common.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// orderParams is the OrderParams tuple of the ABI.
type orderParams struct {
	MarketId         *big.Int
	IsLong           bool
	OrderType        uint8
	TimeInForce      uint8
	SizeUsd          *big.Int
	Leverage         *big.Int
	LimitPrice       *big.Int
	TriggerPrice     *big.Int
	AcceptablePrice  *big.Int
	CollateralAmount *big.Int
	ReduceOnly       bool
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	Deadline         uint64
	Cloid            [16]byte
}

func newOrderParams(isLong bool, orderType perpstypes.OrderType, timeInForce perpstypes.TimeInForce,
	sizeUsd, leverage int64) orderParams {
	return orderParams{
		MarketId: big.NewInt(0), IsLong: isLong, OrderType: uint8(orderType), TimeInForce: uint8(timeInForce),
		SizeUsd: ordersBig(sizeUsd), Leverage: ordersBig(leverage), LimitPrice: new(big.Int),
		TriggerPrice: new(big.Int), AcceptablePrice: new(big.Int), CollateralAmount: new(big.Int),
		TakeProfitPrice: new(big.Int), StopLossPrice: new(big.Int),
	}
}

// limitOrder is a GTC BTC limit order of sizeUsd whole USD at leverage x at
// price whole USD.

// openLong gives A a BTC long of 0.02 BTC at 60,000 USD: B rests a sell of
// 6,000 USD at 60,000 and A buys 1,200 USD at market.
func (e *ordersEnv) openLong() *big.Int {
	e.t.Helper()
	e.fund(ordersTraderA, 1_000)
	e.fund(ordersTraderB, 1_000)
	e.mustCall(ordersTraderB, "placeOrder", limitOrder(false, 6_000, 20, 60_000))
	e.mustCall(ordersTraderA, "placeOrder",
		newOrderParams(true, perpstypes.OrderTypeMarket, perpstypes.TimeInForceIOC, 1_200, 10))
	return e.positionOf(ordersTraderA)
}

func ordersE18(n int64) uint256.Int {
	var value uint256.Int
	value.Mul(uint256.NewInt(uint64(n)), uint256.NewInt(1_000_000_000_000_000_000))
	return value
}

func ordersBig(n int64) *big.Int {
	value := ordersE18(n)
	return value.ToBig()
}

// ordersFixed is the 18-decimal value of a decimal string such as "5999.71".
func ordersFixed(t *testing.T, decimal string) *big.Int {
	t.Helper()
	whole, frac, _ := strings.Cut(decimal, ".")
	require.LessOrEqual(t, len(frac), 18)
	value, ok := new(big.Int).SetString(whole+frac+strings.Repeat("0", 18-len(frac)), 10)
	require.True(t, ok, decimal)
	return value
}

func ordersWord(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	var word uint256.Int
	require.False(t, word.SetFromBig(ordersFixed(t, decimal)))
	return word
}

func TestOrdersPlaceOrderRestsALimit(t *testing.T) {
	e := newOrdersEnv(t)
	e.fund(ordersTraderA, 1_000)
	p := limitOrder(true, 6_000, 20, 59_000)
	p.Cloid = ordersCloid
	from := e.logCount()
	out := e.mustCall(ordersTraderA, "placeOrder", p)
	require.Equal(t, []interface{}{big.NewInt(1)}, out)

	order := e.order(1)
	require.Equal(t, ordersTraderA, order.Account)
	require.Equal(t, perpstypes.OrderStatusResting, order.Status)
	require.Equal(t, perpstypes.OrderTypeLimit, order.Type)
	require.Equal(t, perpstypes.TimeInForceGTC, order.TimeInForce)
	require.Equal(t, ordersE18(59_000), order.LimitPrice)
	require.Equal(t, ordersWord(t, "0.10169"), order.SizeBase)
	require.Equal(t, ordersWord(t, "5999.71"), order.SizeUsd)
	require.Equal(t, ordersE18(20), order.Leverage)
	require.Equal(t, ordersCloid, order.Cloid)
	require.Equal(t, uint64(1), e.account(ordersTraderA).OpenOrderCount)

	logs := e.perpsLogsSince(from)
	require.Len(t, logs, 2)
	e.requireLog(logs[0], "OrderPlaced", big.NewInt(1), ordersTraderA, big.NewInt(0), uint8(0), true,
		ordersBig(59_000), ordersFixed(t, "5999.71"))
	e.requireLog(logs[1], "OrderOptions", big.NewInt(1), uint8(perpstypes.TimeInForceGTC), false, ordersBig(59_000),
		new(big.Int), uint64(0), ordersCloid)
}

func TestOrdersPlaceOrderRefusals(t *testing.T) {
	e := newOrdersEnv(t)
	e.requireRevert("PerpsNoAccount", ordersTraderA, "placeOrder", limitOrder(true, 6_000, 20, 59_000))
	e.fund(ordersTraderA, 1_000)

	unknown := limitOrder(true, 6_000, 20, 59_000)
	unknown.MarketId = new(big.Int).Lsh(big.NewInt(1), 64)
	e.requireRevert("PerpsUnknownMarket", ordersTraderA, "placeOrder", unknown)
	offTick := limitOrder(true, 6_000, 20, 59_000)
	offTick.LimitPrice = ordersFixed(t, "59000.05")
	e.requireRevert("PerpsTickSize", ordersTraderA, "placeOrder", offTick)
	badType := limitOrder(true, 6_000, 20, 59_000)
	badType.OrderType = 6
	e.requireRevert("PerpsInvalidOrderType", ordersTraderA, "placeOrder", badType)
	e.requireRevert("PerpsMinOrderValue", ordersTraderA, "placeOrder", limitOrder(true, 9, 20, 59_000))

	p := limitOrder(true, 6_000, 20, 59_000)
	p.Cloid = ordersCloid
	e.mustCall(ordersTraderA, "placeOrder", p)
	e.requireRevert("PerpsDuplicateCloid", ordersTraderA, "placeOrder", p)
}

func TestOrdersPlaceLimitOrderCompat(t *testing.T) {
	e := newOrdersEnv(t)
	e.holdUsdl(ordersTraderA, 1_000_000_000, 300_000_000)
	module := e.k.ModuleEVMAddress()

	e.requireRevert("PerpsUnsupportedCollateral", ordersTraderA, "placeLimitOrder", big.NewInt(0), true,
		ordersBig(59_000), ordersBig(6_000), ordersBig(20), ordersDeployer, big.NewInt(300_000_000))
	require.Equal(t, big.NewInt(1_000_000_000), e.usdlBalance(ordersTraderA))

	from := e.logCount()
	out := e.mustCall(ordersTraderA, "placeLimitOrder", big.NewInt(0), true, ordersBig(59_000), ordersBig(6_000),
		ordersBig(20), e.usdl, big.NewInt(300_000_000))
	require.Equal(t, []interface{}{big.NewInt(1)}, out)
	require.Equal(t, big.NewInt(700_000_000), e.usdlBalance(ordersTraderA))
	require.Equal(t, big.NewInt(300_000_000), e.usdlBalance(module))
	require.Equal(t, *uint256.NewInt(300_000_000), e.account(ordersTraderA).Collateral)

	order := e.order(1)
	require.Equal(t, perpstypes.OrderTypeLimit, order.Type)
	require.Equal(t, perpstypes.TimeInForceGTC, order.TimeInForce)
	require.Equal(t, perpstypes.OrderStatusResting, order.Status)
	require.Equal(t, ordersE18(59_000), order.LimitPrice)
	require.Equal(t, ordersE18(59_000), order.TriggerPrice)
	require.Equal(t, ordersWord(t, "0.10078"), order.SizeBase)
	require.Equal(t, ordersWord(t, "5946.02"), order.SizeUsd)
	require.Equal(t, ordersE18(20), order.Leverage)

	transfer := e.erc20.Events["Transfer"].ID
	var transferAt, placedAt int
	for i, log := range e.stateDB.GetAllLogs()[from:] {
		switch {
		case log.Address == e.usdl && log.Topics[0] == transfer:
			require.Equal(t, common.BytesToHash(ordersTraderA.Bytes()), log.Topics[1])
			require.Equal(t, common.BytesToHash(module.Bytes()), log.Topics[2])
			require.Equal(t, common.BigToHash(big.NewInt(300_000_000)).Bytes(), log.Data)
			transferAt = i + 1
		case log.Address == ordersPerps && log.Topics[0] == e.perps.Events["OrderPlaced"].ID:
			placedAt = i + 1
		}
	}
	require.NotZero(t, transferAt)
	require.Greater(t, placedAt, transferAt)
	logs := e.perpsLogsSince(from)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], "OrderPlaced", big.NewInt(1), ordersTraderA, big.NewInt(0), uint8(0), true,
		ordersBig(59_000), ordersFixed(t, "5946.02"))
	e.requireLog(logs[len(logs)-1], "OrderOptions", big.NewInt(1), uint8(perpstypes.TimeInForceGTC), false,
		ordersBig(59_000), new(big.Int), uint64(0), [16]byte{})
}

func TestOrdersPlaceStopLimitOrderCompat(t *testing.T) {
	e := newOrdersEnv(t)
	e.holdUsdl(ordersTraderA, 1_000_000_000, 300_000_000)

	from := e.logCount()
	out := e.mustCall(ordersTraderA, "placeStopLimitOrder", big.NewInt(0), true, ordersBig(61_000),
		ordersBig(61_100), ordersBig(6_000), ordersBig(20), e.usdl, big.NewInt(300_000_000))
	require.Equal(t, []interface{}{big.NewInt(1)}, out)
	require.Equal(t, big.NewInt(300_000_000), e.usdlBalance(e.k.ModuleEVMAddress()))

	order := e.order(1)
	require.Equal(t, perpstypes.OrderTypeStopLimit, order.Type)
	require.Equal(t, perpstypes.TimeInForceGTC, order.TimeInForce)
	require.Equal(t, perpstypes.OrderStatusUntriggered, order.Status)
	require.Equal(t, ordersE18(61_000), order.TriggerPrice)
	require.Equal(t, ordersE18(61_100), order.LimitPrice)
	require.Equal(t, ordersWord(t, "0.09732"), order.SizeBase)
	require.Equal(t, ordersWord(t, "5946.252"), order.SizeUsd)

	logs := e.perpsLogsSince(from)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], "OrderPlaced", big.NewInt(1), ordersTraderA, big.NewInt(0), uint8(1), true,
		ordersBig(61_000), ordersFixed(t, "5946.252"))
	e.requireLog(logs[len(logs)-1], "OrderOptions", big.NewInt(1), uint8(perpstypes.TimeInForceGTC), false,
		ordersBig(61_100), new(big.Int), uint64(0), [16]byte{})
}

func TestOrdersCancelOrderHolderOnly(t *testing.T) {
	e := newOrdersEnv(t)
	e.fund(ordersTraderA, 1_000)
	e.fund(ordersTraderB, 1_000)
	e.mustCall(ordersTraderA, "placeOrder", limitOrder(true, 6_000, 20, 59_000))

	e.requireRevert("PerpsNotOrderHolder", ordersTraderB, "cancelOrder", big.NewInt(1))
	require.Equal(t, perpstypes.OrderStatusResting, e.order(1).Status)

	from := e.logCount()
	require.Empty(t, e.mustCall(ordersTraderA, "cancelOrder", big.NewInt(1)))
	logs := e.perpsLogsSince(from)
	require.Len(t, logs, 1)
	e.requireLog(logs[0], "OrderCancelled", big.NewInt(1), ordersTraderA)
	require.Equal(t, perpstypes.OrderStatusCancelled, e.order(1).Status)
	require.Zero(t, e.account(ordersTraderA).OpenOrderCount)
	e.requireRevert("PerpsUnknownOrder", ordersTraderA, "cancelOrder", big.NewInt(1))

	e.mustCall(ordersTraderA, "placeOrder", limitOrder(true, 6_000, 20, 59_000))
	params, err := e.k.GetParams(e.ctx())
	require.NoError(t, err)
	params.Paused = true
	require.NoError(t, e.k.UpdateParams(e.ctx(), e.k.Authority(), params))
	e.requireRevert("PerpsPaused", ordersTraderA, "placeOrder", limitOrder(true, 6_000, 20, 59_000))
	e.mustCall(ordersTraderA, "cancelOrder", big.NewInt(2))
	require.Equal(t, perpstypes.OrderStatusCancelled, e.order(2).Status)
}

func TestOrdersCancelByCloid(t *testing.T) {
	e := newOrdersEnv(t)
	e.fund(ordersTraderA, 1_000)
	cloid := [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	p := limitOrder(true, 6_000, 20, 59_000)
	p.Cloid = cloid
	e.mustCall(ordersTraderA, "placeOrder", p)

	e.requireRevert("PerpsUnknownOrder", ordersTraderB, "cancelByCloid", big.NewInt(0), cloid)
	from := e.logCount()
	require.Empty(t, e.mustCall(ordersTraderA, "cancelByCloid", big.NewInt(0), cloid))
	logs := e.perpsLogsSince(from)
	require.Len(t, logs, 1)
	e.requireLog(logs[0], "OrderCancelled", big.NewInt(1), ordersTraderA)
	require.Equal(t, perpstypes.OrderStatusCancelled, e.order(1).Status)
	e.requireRevert("PerpsUnknownOrder", ordersTraderA, "cancelByCloid", big.NewInt(0), cloid)

	// The freed cloid takes a new order.
	e.mustCall(ordersTraderA, "placeOrder", p)
	require.Equal(t, cloid, e.order(2).Cloid)
}

func TestOrdersPlaceTakeProfitOrderOnPosition(t *testing.T) {
	e := newOrdersEnv(t)
	positionID := e.openLong()

	e.requireRevert("PerpsInvalidTriggerPrice", ordersTraderA, "placeTakeProfitOrder", positionID, ordersBig(59_000))
	e.requireRevert("PerpsInvalidTriggerPrice", ordersTraderA, "placeTakeProfitOrder", positionID, new(big.Int))
	e.requireRevert("PerpsNotPositionHolder", ordersTraderB, "placeTakeProfitOrder", positionID, ordersBig(66_000))

	from := e.logCount()
	out := e.mustCall(ordersTraderA, "placeTakeProfitOrder", positionID, ordersBig(66_000))
	require.Len(t, out, 1)
	orderID := out[0].(*big.Int)
	pair, err := e.k.GetAttachedTpSl(e.ctx(), *uint256.MustFromBig(positionID))
	require.NoError(t, err)
	require.Equal(t, orderID, pair.TakeProfitOrderID.ToBig())
	require.True(t, pair.StopLossOrderID.IsZero())

	order := e.order(orderID.Uint64())
	require.Equal(t, perpstypes.OrderTypeTakeProfit, order.Type)
	require.Equal(t, perpstypes.OrderStatusUntriggered, order.Status)
	require.True(t, order.ReduceOnly)
	require.False(t, order.IsLong)
	require.Equal(t, ordersE18(66_000), order.TriggerPrice)
	require.Equal(t, ordersWord(t, "0.02"), order.SizeBase)
	e.requireHasLog(from, "OrderPlaced", orderID, ordersTraderA, big.NewInt(0), uint8(perpstypes.OrderTypeTakeProfit),
		false, ordersBig(66_000), ordersBig(1_200))
}

func TestOrdersPlaceStopLossOrderOnPosition(t *testing.T) {
	e := newOrdersEnv(t)
	positionID := e.openLong()
	e.requireRevert("PerpsInvalidTriggerPrice", ordersTraderA, "placeStopLossOrder", positionID, ordersBig(61_000))

	e.mustCall(ordersTraderA, "placeTakeProfitOrder", positionID, ordersBig(66_000))
	from := e.logCount()
	out := e.mustCall(ordersTraderA, "placeStopLossOrder", positionID, ordersBig(57_000))
	require.Len(t, out, 1)
	orderID := out[0].(*big.Int)

	order := e.order(orderID.Uint64())
	require.Equal(t, perpstypes.OrderTypeStopLoss, order.Type)
	require.Equal(t, perpstypes.OrderStatusUntriggered, order.Status)
	require.True(t, order.ReduceOnly)
	require.Equal(t, ordersE18(57_000), order.TriggerPrice)
	require.Equal(t, ordersWord(t, "0.02"), order.SizeBase)
	e.requireHasLog(from, "OrderPlaced", orderID, ordersTraderA, big.NewInt(0), uint8(perpstypes.OrderTypeStopLoss),
		false, ordersBig(57_000), ordersBig(1_200))

	// The take-profit leg placed first stays attached at its trigger.
	pair, err := e.k.GetAttachedTpSl(e.ctx(), *uint256.MustFromBig(positionID))
	require.NoError(t, err)
	require.Equal(t, orderID, pair.StopLossOrderID.ToBig())
	takeProfit, err := e.k.GetOrder(e.ctx(), pair.TakeProfitOrderID)
	require.NoError(t, err)
	require.Equal(t, perpstypes.OrderStatusUntriggered, takeProfit.Status)
	require.Equal(t, ordersE18(66_000), takeProfit.TriggerPrice)
}

func TestOrdersSetPositionTpSlReplaces(t *testing.T) {
	e := newOrdersEnv(t)
	positionID := e.openLong()
	key := *uint256.MustFromBig(positionID)
	e.requireRevert("PerpsNotPositionHolder", ordersTraderB, "setPositionTpSl", positionID, ordersBig(66_000),
		ordersBig(57_000))

	from := e.logCount()
	require.Empty(t, e.mustCall(ordersTraderA, "setPositionTpSl", positionID, ordersBig(66_000), ordersBig(57_000)))
	logs := e.perpsLogsSince(from)
	require.NotEmpty(t, logs)
	e.requireLog(logs[len(logs)-1], "PositionTpSlSet", positionID, ordersBig(66_000), ordersBig(57_000))
	first, err := e.k.GetAttachedTpSl(e.ctx(), key)
	require.NoError(t, err)
	require.False(t, first.TakeProfitOrderID.IsZero())
	require.False(t, first.StopLossOrderID.IsZero())

	e.mustCall(ordersTraderA, "setPositionTpSl", positionID, ordersBig(67_000), ordersBig(56_000))
	second, err := e.k.GetAttachedTpSl(e.ctx(), key)
	require.NoError(t, err)
	for _, id := range []uint256.Int{first.TakeProfitOrderID, first.StopLossOrderID} {
		require.Equal(t, perpstypes.OrderStatusCancelled, e.order(id.Uint64()).Status)
	}
	require.Equal(t, ordersE18(67_000), e.order(second.TakeProfitOrderID.Uint64()).TriggerPrice)
	require.Equal(t, ordersE18(56_000), e.order(second.StopLossOrderID.Uint64()).TriggerPrice)

	from = e.logCount()
	e.mustCall(ordersTraderA, "setPositionTpSl", positionID, new(big.Int), new(big.Int))
	logs = e.perpsLogsSince(from)
	require.NotEmpty(t, logs)
	e.requireLog(logs[len(logs)-1], "PositionTpSlSet", positionID, new(big.Int), new(big.Int))
	cleared, err := e.k.GetAttachedTpSl(e.ctx(), key)
	require.NoError(t, err)
	require.True(t, cleared.TakeProfitOrderID.IsZero())
	require.True(t, cleared.StopLossOrderID.IsZero())
	for _, id := range []uint256.Int{second.TakeProfitOrderID, second.StopLossOrderID} {
		require.Equal(t, perpstypes.OrderStatusCancelled, e.order(id.Uint64()).Status)
	}
}

func TestOrdersExecuteOrderRequiresTrigger(t *testing.T) {
	e := newOrdersEnv(t)
	e.fund(ordersTraderA, 1_000)
	e.fund(ordersTraderB, 1_000)
	e.mustCall(ordersTraderB, "placeOrder", limitOrder(false, 6_000, 20, 61_100))
	stop := newOrderParams(true, perpstypes.OrderTypeStopMarket, perpstypes.TimeInForceIOC, 1_200, 10)
	stop.TriggerPrice = ordersBig(61_000)
	out := e.mustCall(ordersTraderA, "placeOrder", stop)
	require.Equal(t, []interface{}{big.NewInt(2)}, out)
	require.Equal(t, perpstypes.OrderStatusUntriggered, e.order(2).Status)

	e.setMark(60_900)
	e.requireRevert("PerpsTriggerNotReached", ordersKeeperC, "executeOrder", big.NewInt(2))
	require.Equal(t, perpstypes.OrderStatusUntriggered, e.order(2).Status)

	e.setMark(61_050)
	from := e.logCount()
	out = e.mustCall(ordersKeeperC, "executeOrder", big.NewInt(2))
	require.Len(t, out, 1)
	positionID := out[0].(*big.Int)
	require.Equal(t, e.positionOf(ordersTraderA), positionID)
	require.NotZero(t, positionID.Sign())
	require.Equal(t, perpstypes.OrderStatusFilled, e.order(2).Status)
	e.requireHasLog(from, "OrderTriggered", big.NewInt(2), big.NewInt(0), ordersBig(61_050))
	e.requireRevert("PerpsUnknownOrder", ordersKeeperC, "executeOrder", big.NewInt(2))
}
