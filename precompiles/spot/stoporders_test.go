package spot_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	feedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	feedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	feedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type stopOrdersFixture struct {
	t                         *testing.T
	ctx                       sdk.Context
	k                         *keeper.Keeper
	key, feedKey              sdk.StoreKey
	user, other, stable, base common.Address
	params                    types.Params
	executor                  pcommon.PrecompileExecutor
	abi                       abi.ABI
	logs                      []*ethtypes.Log
}

func stopOrdersTestAddress(value string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(value)))
}

func stopOrdersTestPut(t *testing.T, store sdk.KVStore, key []byte, value interface{}) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	store.Set(key, encoded)
}

func stopOrdersTestPrice(value int64) sdk.Int {
	return sdk.NewInt(value).Mul(sdk.NewIntWithDecimal(1, 16))
}

func newStopOrdersFixture(t *testing.T) *stopOrdersFixture {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockHeight(8).WithBlockTime(time.Unix(1000, 0).UTC()).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, feedKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(feedtypes.StoreKey)
	ctx = feedtest.WithPriceFeedStore(ctx, feedKey)
	ctx = feedtest.WithPriceFeedStore(ctx, key)
	feed := feedkeeper.NewKeeper(feedKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	f := &stopOrdersFixture{t: t, ctx: ctx, k: k, key: key, feedKey: feedKey, user: stopOrdersTestAddress("stop order user"), other: stopOrdersTestAddress("stop order other"), base: stopOrdersTestAddress("stop order native token")}

	source, err := parser.ParseFile(token.NewFileSet(), "../../modules/launchpad/keeper/custody_test.go", nil, 0)
	require.NoError(t, err)
	var creation string
	ast.Inspect(source, func(n ast.Node) bool {
		declaration, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range declaration.Names {
			if name.Name != "sidioraERC20Creation" {
				continue
			}
			literal, ok := declaration.Values[i].(*ast.BasicLit)
			require.True(t, ok)
			creation, err = strconv.Unquote(literal.Value)
			require.NoError(t, err)
		}
		return true
	})
	require.NotEmpty(t, creation)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.Pack("Quote Token", "QUOTE", big.NewInt(2_000_000_000_000), f.user)
	require.NoError(t, err)
	nonce := app.EvmKeeper.GetNonce(ctx, f.user)
	_, err = app.EvmKeeper.CallEVM(ctx, f.user, nil, nil, append(common.FromHex(creation), args...))
	require.NoError(t, err)
	f.stable = crypto.CreateAddress(f.user, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, f.stable))
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, "uhpx", f.base))
	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: "uhpx", Display: "pax", Name: "Paxeer X Network", Symbol: "PAX", DenomUnits: []*banktypes.DenomUnit{{Denom: "uhpx", Exponent: 0}, {Denom: "pax", Exponent: 6}}})
	f.params = types.DefaultParams()
	f.params.OrderFeeBps = 10
	f.params.OrderTier1FeeBps = 20
	f.params.OrderTier2FeeBps = 50
	f.params.Tokens = []types.TokenParams{
		{Address: f.stable, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: f.base, Kind: types.TokenKindNative, MarketID: 11, Decimals: 6, MaxUtilizationBps: 9000},
	}
	f.setParams()
	f.setPrice(50)
	custody := f.k.CustodyEVMAddress(ctx)
	f.tokenCall("transfer(address,uint256)", custody, big.NewInt(500_000_000_000))
	f.tokenCall("approve(address,uint256)", common.HexToAddress(types.PECORStopOrdersAddress), big.NewInt(1_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewInt64Coin("uhpx", 2_000_000_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", app.EvmKeeper.GetPaxAddressOrDefault(ctx, f.user), coins))
	require.NoError(t, app.BankKeeper.SendCoins(ctx, app.EvmKeeper.GetPaxAddressOrDefault(ctx, f.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000_000))))
	for _, entry := range []struct {
		token    common.Address
		reserves int64
	}{{f.stable, 500_000_000_000}, {f.base, 1_000_000_000_000}} {
		stopOrdersTestPut(t, ctx.KVStore(key), append(append([]byte(nil), types.BookPrefix...), entry.token.Bytes()...), types.TokenBook{Token: entry.token, Reserves: sdk.NewInt(entry.reserves), TotalDeposited: sdk.NewInt(entry.reserves), TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + common.Bytes2Hex(entry.token.Bytes()), MinimumSharesLocked: true})
	}
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.executor, err = spot.NewStopOrdersExecutor(k, &app.EvmKeeper)
	require.NoError(t, err)
	raw, err := os.ReadFile("abi/stoporders.json")
	require.NoError(t, err)
	f.abi, err = abi.JSON(strings.NewReader(string(raw)))
	require.NoError(t, err)
	return f
}

func (f *stopOrdersFixture) setParams() {
	f.t.Helper()
	require.NoError(f.t, f.params.Validate())
	stopOrdersTestPut(f.t, f.ctx.KVStore(f.key), types.ParamsKey, f.params)
}

func (f *stopOrdersFixture) setPrice(value int64) {
	f.t.Helper()
	price := stopOrdersTestPrice(value)
	stopOrdersTestPut(f.t, f.ctx.KVStore(f.feedKey), feedtypes.AggregateKey(11), feedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(), TimestampMs: uint64(f.ctx.BlockTime().UnixMilli()), Height: f.ctx.BlockHeight(), Sources: 4})
}

func (f *stopOrdersFixture) tokenCall(signature string, address common.Address, amount *big.Int) {
	f.t.Helper()
	data := append(crypto.Keccak256([]byte(signature))[:4], common.LeftPadBytes(address.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	result, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(f.ctx, f.user, &f.stable, nil, data)
	require.NoError(f.t, err)
	require.Equal(f.t, common.LeftPadBytes(big.NewInt(1).Bytes(), 32), result)
}

func (f *stopOrdersFixture) quoteBalance() sdk.Int {
	f.t.Helper()
	data := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(f.user.Bytes(), 32)...)
	result, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(f.ctx, keeper.CustodyAddress(), &f.stable, data)
	require.NoError(f.t, err)
	require.Len(f.t, result, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(result))
}

func (f *stopOrdersFixture) baseBalance() sdk.Int {
	app := testkeeper.EVMTestApp
	return app.BankKeeper.GetBalance(f.ctx, app.EvmKeeper.GetPaxAddressOrDefault(f.ctx, f.user), "uhpx").Amount
}

func (f *stopOrdersFixture) invoke(caller common.Address, name string, args ...interface{}) ([]interface{}, error) {
	f.t.Helper()
	method, ok := f.abi.Methods[name]
	require.True(f.t, ok)
	input, err := f.abi.Pack(name, args...)
	require.NoError(f.t, err)
	decoded, err := method.Inputs.Unpack(input[4:])
	require.NoError(f.t, err)
	var output []byte
	f.logs = nil
	err = testkeeper.EVMTestApp.EvmKeeper.RunWithOneOffEVMInstance(f.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		require.NotNil(f.t, db)
		snapshot := db.Snapshot()
		ctx := keeper.WithRun(db.Ctx(), &keeper.Run{EVM: evm, Gas: 30_000_000})
		output, err = f.executor.Execute(ctx, &method, caller, common.HexToAddress(types.PECORStopOrdersAddress), decoded, big.NewInt(0), method.IsConstant(), evm, nil)
		if err != nil {
			db.RevertToSnapshot(snapshot)
			require.Empty(f.t, db.GetAllLogs())
			return err
		}
		f.logs = db.GetAllLogs()
		return nil
	}, func(string, string) {})
	if err != nil {
		return nil, err
	}
	values, err := method.Outputs.Unpack(output)
	require.NoError(f.t, err)
	for i, value := range values {
		values[i] = stopOrdersTestCanonical(reflect.ValueOf(value)).Interface()
	}
	return values, nil
}

func stopOrdersTestCanonical(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Ptr:
		if n, ok := value.Interface().(*big.Int); ok && n != nil {
			return reflect.ValueOf(new(big.Int).Set(n))
		}
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		canonical := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			canonical.Index(i).Set(stopOrdersTestCanonical(value.Index(i)))
		}
		return canonical
	case reflect.Struct:
		canonical := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			canonical.Field(i).Set(stopOrdersTestCanonical(value.Field(i)))
		}
		return canonical
	}
	return value
}

func (f *stopOrdersFixture) call(name string, args ...interface{}) []interface{} {
	f.t.Helper()
	values, err := f.invoke(f.user, name, args...)
	require.NoError(f.t, err)
	return values
}

func (f *stopOrdersFixture) refusal(caller common.Address, expected *types.AbiError, name string, args ...interface{}) {
	f.t.Helper()
	_, err := f.invoke(caller, name, args...)
	var actual *types.AbiError
	require.True(f.t, errors.As(err, &actual), "%v", err)
	require.Equal(f.t, expected.Data(), actual.Data())
	require.Empty(f.t, f.logs)
}

func (f *stopOrdersFixture) assertLog(name string, indexed []common.Hash, values ...interface{}) {
	f.t.Helper()
	event := f.abi.Events[name]
	data, err := event.Inputs.NonIndexed().Pack(values...)
	require.NoError(f.t, err)
	var found []*ethtypes.Log
	for _, log := range f.logs {
		if log.Address == common.HexToAddress(types.PECORStopOrdersAddress) && log.Topics[0] == event.ID {
			found = append(found, log)
		}
	}
	require.Len(f.t, found, 1)
	require.Equal(f.t, append([]common.Hash{event.ID}, indexed...), found[0].Topics)
	require.Equal(f.t, data, found[0].Data)
}

func (f *stopOrdersFixture) placeBuy() *big.Int {
	f.t.Helper()
	return f.call("placeStopLimitBuy", f.stable, f.base, big.NewInt(100_000_000), stopOrdersTestPrice(55).BigInt(), stopOrdersTestPrice(60).BigInt(), big.NewInt(0))[0].(*big.Int)
}
func (f *stopOrdersFixture) placeSell() *big.Int {
	f.t.Helper()
	return f.call("placeStopLimitSell", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(45).BigInt(), stopOrdersTestPrice(40).BigInt(), big.NewInt(0))[0].(*big.Int)
}
func (f *stopOrdersFixture) placeLoss() *big.Int {
	f.t.Helper()
	return f.call("placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), big.NewInt(0))[0].(*big.Int)
}

func stopOrdersAdminRefusal(t *testing.T, name string, args ...interface{}) {
	t.Helper()
	f := newStopOrdersFixture(t)
	before, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	authority, err := sdk.AccAddressFromBech32(f.k.Authority())
	require.NoError(t, err)
	owner := testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(f.ctx, authority)
	for _, caller := range []common.Address{f.user, owner, common.Address{}} {
		f.refusal(caller, types.OwnableUnauthorizedAccount(caller), name, args...)
	}
	after, err := f.k.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestStopOrdersOwner(t *testing.T) {
	f := newStopOrdersFixture(t)
	authority, err := sdk.AccAddressFromBech32(f.k.Authority())
	require.NoError(t, err)
	require.Equal(t, []interface{}{testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(f.ctx, authority)}, f.call("owner"))
}
func TestStopOrdersRenounceOwnership(t *testing.T) { stopOrdersAdminRefusal(t, "renounceOwnership") }
func TestStopOrdersTransferOwnership(t *testing.T) {
	stopOrdersAdminRefusal(t, "transferOwnership", common.Address{19: 1})
}
func TestStopOrdersPause(t *testing.T)   { stopOrdersAdminRefusal(t, "pause") }
func TestStopOrdersUnpause(t *testing.T) { stopOrdersAdminRefusal(t, "unpause") }
func TestStopOrdersSetKeeper(t *testing.T) {
	stopOrdersAdminRefusal(t, "setKeeper", common.Address{19: 1}, true)
}
func TestStopOrdersSetOrderFee(t *testing.T) {
	stopOrdersAdminRefusal(t, "setOrderFee", big.NewInt(20))
}
func TestStopOrdersSetFillQualityTwapPeriod(t *testing.T) {
	stopOrdersAdminRefusal(t, "setFillQualityTwapPeriod", big.NewInt(60))
}
func TestStopOrdersSetFeeCollector(t *testing.T) {
	stopOrdersAdminRefusal(t, "setFeeCollector", common.Address{19: 1})
}
func TestStopOrdersSetPriceOracle(t *testing.T) {
	stopOrdersAdminRefusal(t, "setPriceOracle", common.Address{19: 1})
}
func TestStopOrdersSetTransactionTracker(t *testing.T) {
	stopOrdersAdminRefusal(t, "setTransactionTracker", common.Address{19: 1})
}
func TestStopOrdersPaused(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{false}, f.call("paused"))
	f.params.OrdersPaused = true
	f.setParams()
	require.Equal(t, []interface{}{true}, f.call("paused"))
	f.refusal(f.user, types.ErrEnforcedPause, "placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), big.NewInt(0))
	for _, name := range []string{"executeStopLimitOrder", "executeStopLossOrder", "checkStopLimitActivation"} {
		f.refusal(f.user, types.ErrEnforcedPause, name, big.NewInt(1))
	}
	for _, name := range []string{"batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders"} {
		f.refusal(f.user, types.ErrEnforcedPause, name, []*big.Int{})
	}
}
func TestStopOrdersKeepers(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{true}, f.call("keepers", f.other))
	f.params.KeeperOpen = false
	f.params.Keepers = []common.Address{f.user}
	f.setParams()
	require.Equal(t, []interface{}{true}, f.call("keepers", f.user))
	require.Equal(t, []interface{}{false}, f.call("keepers", f.other))
	for _, name := range []string{"executeStopLimitOrder", "executeStopLossOrder", "checkStopLimitActivation"} {
		f.refusal(f.other, types.ErrNotAuthorized, name, big.NewInt(1))
	}
	for _, name := range []string{"batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders"} {
		f.refusal(f.other, types.ErrNotAuthorized, name, []*big.Int{})
	}
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("batchExecuteStopLossOrders", []*big.Int{}))
}
func TestStopOrdersOrderFeeBps(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(10)}, f.call("orderFeeBps"))
	f.params.OrderFeeBps = 17
	f.setParams()
	require.Equal(t, []interface{}{big.NewInt(17)}, f.call("orderFeeBps"))
}
func TestStopOrdersMaxOrderFeeBps(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(200)}, f.call("MAX_ORDER_FEE_BPS"))
}
func TestStopOrdersFillQualityTwapPeriod(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("fillQualityTwapPeriod"))
	f.params.FillQualityTwapPeriod = 60
	f.setParams()
	require.Equal(t, []interface{}{big.NewInt(60)}, f.call("fillQualityTwapPeriod"))
}
func TestStopOrdersFeeCollector(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{f.params.FeeCollector}, f.call("feeCollector"))
	f.params.FeeCollector = f.other
	f.setParams()
	require.Equal(t, []interface{}{f.other}, f.call("feeCollector"))
}
func TestStopOrdersPriceOracle(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{common.HexToAddress("0x1020")}, f.call("priceOracle"))
}
func TestStopOrdersVault(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{common.HexToAddress("0x1025")}, f.call("vault"))
}
func TestStopOrdersTransactionTracker(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{common.Address{}}, f.call("transactionTracker"))
}
func TestStopOrdersBpsDenominator(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(10000)}, f.call("BPS_DENOMINATOR"))
}
func TestStopOrdersNextOrderId(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(1)}, f.call("nextOrderId"))
	f.placeLoss()
	require.Equal(t, []interface{}{big.NewInt(2)}, f.call("nextOrderId"))
}

func stopOrdersFeeView(t *testing.T, name string) {
	t.Helper()
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call(name, f.base))
	require.NoError(t, f.k.AccrueFees(f.ctx, common.HexToAddress(types.PECOROrdersAddress), f.base, sdk.NewInt(2_000_000)))
	require.NoError(t, f.k.AccrueFees(f.ctx, common.HexToAddress(types.PECORAddress), f.base, sdk.NewInt(3_000_000)))
	require.Equal(t, []interface{}{big.NewInt(2_000_000)}, f.call(name, f.base))
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call(name, f.stable))
}
func TestStopOrdersAccumulatedFees(t *testing.T)    { stopOrdersFeeView(t, "accumulatedFees") }
func TestStopOrdersGetAccumulatedFees(t *testing.T) { stopOrdersFeeView(t, "getAccumulatedFees") }
func TestStopOrdersCollectFees(t *testing.T) {
	f := newStopOrdersFixture(t)
	f.params.FeeCollector = f.user
	f.setParams()
	f.refusal(f.other, types.ErrNotFeeCollector, "collectFees", f.base)
	f.refusal(f.user, types.ErrNoFeesToCollect, "collectFees", f.base)
	require.NoError(t, f.k.AccrueFees(f.ctx, common.HexToAddress(types.PECOROrdersAddress), f.base, sdk.NewInt(2_000_000)))
	require.NoError(t, f.k.AccrueFees(f.ctx, common.HexToAddress(types.PECORAddress), f.base, sdk.NewInt(3_000_000)))
	before := f.baseBalance()
	f.call("collectFees", f.base)
	require.Equal(t, "2000000", f.baseBalance().Sub(before).String())
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("accumulatedFees", f.base))
	fees, err := f.k.AccumulatedFees(f.ctx, common.HexToAddress(types.PECOROrdersAddress), f.base)
	require.NoError(t, err)
	require.Equal(t, "0", fees.String())
	fees, err = f.k.AccumulatedFees(f.ctx, common.HexToAddress(types.PECORAddress), f.base)
	require.NoError(t, err)
	require.Equal(t, "3000000", fees.String())
}
func TestStopOrdersPlaceStopLimitBuy(t *testing.T) {
	f := newStopOrdersFixture(t)
	before := f.quoteBalance()
	id := f.placeBuy()
	require.Equal(t, big.NewInt(1), id)
	require.Equal(t, "100000000", before.Sub(f.quoteBalance()).String())
	order, err := f.k.GetStopLimitOrder(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.StopLimitOrder{ID: 1, User: f.user, Stablecoin: f.stable, Token: f.base, Amount: sdk.NewInt(100_000_000), StopPrice: stopOrdersTestPrice(55), LimitPrice: stopOrdersTestPrice(60), OrderType: types.OrderTypeStopLimitBuy, Status: types.OrderStatusPending, CreatedAt: 1000, ExpiresAt: 0}, order)
	f.assertLog("StopLimitOrderCreated", []common.Hash{common.BigToHash(id), common.BytesToHash(f.user.Bytes())}, uint8(3), big.NewInt(100_000_000), stopOrdersTestPrice(55).BigInt(), stopOrdersTestPrice(60).BigInt())
	before = f.quoteBalance()
	for _, prices := range [][2]int64{{0, 60}, {55, 0}, {55, 54}} {
		expected := types.ErrInvalidPrices
		if prices[0] != 0 && prices[1] != 0 {
			expected = types.ErrLimitMustBeAboveStopBuy
		}
		f.refusal(f.user, expected, "placeStopLimitBuy", f.stable, f.base, big.NewInt(100_000_000), stopOrdersTestPrice(prices[0]).BigInt(), stopOrdersTestPrice(prices[1]).BigInt(), big.NewInt(0))
	}
	require.Equal(t, before.String(), f.quoteBalance().String())
	require.Equal(t, []interface{}{big.NewInt(2)}, f.call("nextOrderId"))
	method := f.abi.Methods["placeStopLimitBuy"]
	args := []interface{}{f.stable, f.base, big.NewInt(100_000_000), stopOrdersTestPrice(55).BigInt(), stopOrdersTestPrice(60).BigInt(), big.NewInt(0)}
	_, err = f.executor.Execute(f.ctx, &method, f.user, common.Address{}, args, big.NewInt(0), true, nil, nil)
	require.ErrorIs(t, err, vm.ErrWriteProtection)
	_, err = f.executor.Execute(f.ctx, &method, f.user, common.Address{}, args, big.NewInt(1), false, nil, nil)
	require.Error(t, err)
	require.Equal(t, before.String(), f.quoteBalance().String())
}
func TestStopOrdersPlaceStopLimitSell(t *testing.T) {
	f := newStopOrdersFixture(t)
	before := f.baseBalance()
	id := f.placeSell()
	require.Equal(t, big.NewInt(1), id)
	require.Equal(t, "1000000000", before.Sub(f.baseBalance()).String())
	order, err := f.k.GetStopLimitOrder(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.OrderTypeStopLimitSell, order.OrderType)
	require.Equal(t, f.stable, order.Stablecoin)
	require.Equal(t, f.base, order.Token)
	f.assertLog("StopLimitOrderCreated", []common.Hash{common.BigToHash(id), common.BytesToHash(f.user.Bytes())}, uint8(4), big.NewInt(1_000_000_000), stopOrdersTestPrice(45).BigInt(), stopOrdersTestPrice(40).BigInt())
	f.refusal(f.user, types.ErrLimitMustBeBelowStopSell, "placeStopLimitSell", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(45).BigInt(), stopOrdersTestPrice(50).BigInt(), big.NewInt(0))
	f.refusal(f.user, types.ErrInvalidPrices, "placeStopLimitSell", f.base, f.stable, big.NewInt(1_000_000_000), big.NewInt(0), stopOrdersTestPrice(40).BigInt(), big.NewInt(0))
}
func TestStopOrdersPlaceStopLoss(t *testing.T) {
	f := newStopOrdersFixture(t)
	before := f.baseBalance()
	id := f.placeLoss()
	require.Equal(t, big.NewInt(1), id)
	require.Equal(t, "1000000000", before.Sub(f.baseBalance()).String())
	order, err := f.k.GetStopLossOrder(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.StopLossOrder{ID: 1, User: f.user, Stablecoin: f.stable, Token: f.base, Amount: sdk.NewInt(1_000_000_000), TriggerPrice: stopOrdersTestPrice(40), Status: types.OrderStatusPending, CreatedAt: 1000, ExpiresAt: 0}, order)
	f.assertLog("StopLossOrderCreated", []common.Hash{common.BigToHash(id), common.BytesToHash(f.user.Bytes())}, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt())
	f.refusal(f.user, types.ErrInvalidPrice, "placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), big.NewInt(0), big.NewInt(0))
	f.refusal(f.user, types.ErrInvalidExpiry, "placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), new(big.Int).Lsh(big.NewInt(1), 128))
}
func TestStopOrdersCancelStopLimitOrder(t *testing.T) {
	for _, activate := range []bool{false, true} {
		t.Run(strconv.FormatBool(activate), func(t *testing.T) {
			f := newStopOrdersFixture(t)
			before := f.baseBalance()
			id := f.placeSell()
			if activate {
				f.setPrice(45)
				f.call("checkStopLimitActivation", id)
			}
			f.refusal(f.other, types.ErrNotOrderOwner, "cancelStopLimitOrder", id)
			f.params.OrdersPaused = true
			f.setParams()
			require.Empty(t, f.call("cancelStopLimitOrder", id))
			require.Equal(t, before.String(), f.baseBalance().String())
			f.assertLog("StopLimitCancelled", []common.Hash{common.BigToHash(id)})
			order, err := f.k.GetStopLimitOrder(f.ctx, id.Uint64())
			require.NoError(t, err)
			require.Equal(t, types.OrderStatusCancelled, order.Status)
			require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLimitOrderCount"))
			f.refusal(f.user, types.ErrCannotCancel, "cancelStopLimitOrder", id)
		})
	}
}
func TestStopOrdersCancelStopLossOrder(t *testing.T) {
	f := newStopOrdersFixture(t)
	before := f.baseBalance()
	id := f.placeLoss()
	f.refusal(f.other, types.ErrNotOrderOwner, "cancelStopLossOrder", id)
	f.params.OrdersPaused = true
	f.setParams()
	require.Empty(t, f.call("cancelStopLossOrder", id))
	require.Equal(t, before.String(), f.baseBalance().String())
	f.assertLog("StopLossOrderCancelled", []common.Hash{common.BigToHash(id)})
	order, err := f.k.GetStopLossOrder(f.ctx, id.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusCancelled, order.Status)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLossOrderCount"))
	f.refusal(f.user, types.ErrNotPending, "cancelStopLossOrder", id)
}
func TestStopOrdersCheckStopLimitActivation(t *testing.T) {
	f := newStopOrdersFixture(t)
	id := f.placeBuy()
	f.call("checkStopLimitActivation", id)
	require.Empty(t, f.logs)
	order, err := f.k.GetStopLimitOrder(f.ctx, id.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusPending, order.Status)
	f.setPrice(55)
	require.Empty(t, f.call("checkStopLimitActivation", id))
	f.assertLog("StopLimitActivated", []common.Hash{common.BigToHash(id)}, stopOrdersTestPrice(55).BigInt())
	order, err = f.k.GetStopLimitOrder(f.ctx, id.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActivated, order.Status)
	f.refusal(f.user, types.ErrOrderNotPending, "checkStopLimitActivation", id)
	f.refusal(f.user, types.ErrOrderNotPending, "checkStopLimitActivation", big.NewInt(999))
	expiry := big.NewInt(f.ctx.BlockTime().Unix() + 1)
	id = f.call("placeStopLimitBuy", f.stable, f.base, big.NewInt(100_000_000), stopOrdersTestPrice(55).BigInt(), stopOrdersTestPrice(60).BigInt(), expiry)[0].(*big.Int)
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Second))
	f.refusal(f.user, types.ErrOrderExpired, "checkStopLimitActivation", id)
}
func TestStopOrdersExecuteStopLimitOrder(t *testing.T) {
	t.Run("buy", func(t *testing.T) {
		f := newStopOrdersFixture(t)
		id := f.placeBuy()
		f.refusal(f.user, types.ErrNotActivated, "executeStopLimitOrder", id)
		f.setPrice(56)
		f.call("checkStopLimitActivation", id)
		f.setPrice(61)
		f.refusal(f.user, types.ErrPriceAboveLimit, "executeStopLimitOrder", id)
		f.setPrice(56)
		before := f.baseBalance()
		require.Empty(t, f.call("executeStopLimitOrder", id))
		require.Equal(t, "178392857", f.baseBalance().Sub(before).String())
		f.assertLog("StopLimitExecuted", []common.Hash{common.BigToHash(id)}, stopOrdersTestPrice(56).BigInt())
		order, err := f.k.GetStopLimitOrder(f.ctx, id.Uint64())
		require.NoError(t, err)
		require.Equal(t, types.OrderStatusExecuted, order.Status)
		require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLimitOrderCount"))
		f.refusal(f.user, types.ErrNotActivated, "executeStopLimitOrder", id)
	})
	t.Run("sell", func(t *testing.T) {
		f := newStopOrdersFixture(t)
		id := f.placeSell()
		f.setPrice(39)
		f.call("checkStopLimitActivation", id)
		f.refusal(f.user, types.ErrPriceBelowLimit, "executeStopLimitOrder", id)
		order, err := f.k.GetStopLimitOrder(f.ctx, id.Uint64())
		require.NoError(t, err)
		require.Equal(t, types.OrderStatusActivated, order.Status)
		f.setPrice(45)
		before := f.quoteBalance()
		f.call("executeStopLimitOrder", id)
		require.Equal(t, "449550000", f.quoteBalance().Sub(before).String())
		f.assertLog("StopLimitExecuted", []common.Hash{common.BigToHash(id)}, stopOrdersTestPrice(45).BigInt())
	})
}
func TestStopOrdersExecuteStopLossOrder(t *testing.T) {
	f := newStopOrdersFixture(t)
	id := f.placeLoss()
	f.refusal(f.user, types.ErrPriceAboveTrigger, "executeStopLossOrder", id)
	f.setPrice(40)
	before := f.quoteBalance()
	require.Empty(t, f.call("executeStopLossOrder", id))
	require.Equal(t, "399600000", f.quoteBalance().Sub(before).String())
	f.assertLog("StopLossOrderExecuted", []common.Hash{common.BigToHash(id)}, stopOrdersTestPrice(40).BigInt())
	order, err := f.k.GetStopLossOrder(f.ctx, id.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusExecuted, order.Status)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLossOrderCount"))
	f.refusal(f.user, types.ErrNotPending, "executeStopLossOrder", id)
	f.refusal(f.user, types.ErrNotPending, "executeStopLossOrder", new(big.Int).Lsh(big.NewInt(1), 64))
}
func TestStopOrdersBatchCheckAndExecuteStopLimits(t *testing.T) {
	f := newStopOrdersFixture(t)
	buy := f.placeBuy()
	sell := f.placeSell()
	f.setPrice(39)
	require.Equal(t, []interface{}{big.NewInt(1), big.NewInt(0)}, f.call("batchCheckAndExecuteStopLimits", []*big.Int{big.NewInt(999), sell, sell, buy}))
	f.assertLog("StopLimitActivated", []common.Hash{common.BigToHash(sell)}, stopOrdersTestPrice(39).BigInt())
	order, err := f.k.GetStopLimitOrder(f.ctx, sell.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActivated, order.Status)
	f.setPrice(56)
	require.Equal(t, []interface{}{big.NewInt(1), big.NewInt(2)}, f.call("batchCheckAndExecuteStopLimits", []*big.Int{new(big.Int).Lsh(big.NewInt(1), 128), buy, buy, sell}))
	f.assertLog("StopLimitActivated", []common.Hash{common.BigToHash(buy)}, stopOrdersTestPrice(56).BigInt())
	var ids []common.Hash
	for _, log := range f.logs {
		if log.Topics[0] == f.abi.Events["StopLimitExecuted"].ID {
			ids = append(ids, log.Topics[1])
		}
	}
	require.Equal(t, []common.Hash{common.BigToHash(buy), common.BigToHash(sell)}, ids)
	require.Equal(t, []interface{}{big.NewInt(0), big.NewInt(0)}, f.call("batchCheckAndExecuteStopLimits", []*big.Int{}))
}
func TestStopOrdersBatchExecuteStopLossOrders(t *testing.T) {
	f := newStopOrdersFixture(t)
	first := f.placeLoss()
	second := f.placeLoss()
	pending := f.call("placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(30).BigInt(), big.NewInt(0))[0].(*big.Int)
	f.setPrice(40)
	before := f.quoteBalance()
	require.Equal(t, []interface{}{big.NewInt(2)}, f.call("batchExecuteStopLossOrders", []*big.Int{big.NewInt(999), pending, first, first, second}))
	require.Equal(t, "799200000", f.quoteBalance().Sub(before).String())
	var ids []common.Hash
	for _, log := range f.logs {
		if log.Topics[0] == f.abi.Events["StopLossOrderExecuted"].ID {
			ids = append(ids, log.Topics[1])
		}
	}
	require.Equal(t, []common.Hash{common.BigToHash(first), common.BigToHash(second)}, ids)
	order, err := f.k.GetStopLossOrder(f.ctx, pending.Uint64())
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusPending, order.Status)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("batchExecuteStopLossOrders", []*big.Int{}))
}
func TestStopViewsCap(t *testing.T) {
	f := newStopOrdersFixture(t)
	f.params.MaxActiveOrdersPerUser = 200
	f.setParams()
	ids := make([]*big.Int, 120)
	for i := range ids {
		ids[i] = f.placeBuy()
	}
	f.setPrice(55)
	for _, max := range []*big.Int{big.NewInt(200), new(big.Int).Lsh(big.NewInt(1), 128)} {
		require.Equal(t, []interface{}{ids[:100]}, f.call("getActivatableStopLimits", max))
	}
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getActivatableStopLimits", big.NewInt(0)))
	for _, id := range ids {
		f.call("checkStopLimitActivation", id)
	}
	require.Equal(t, []interface{}{ids[:100]}, f.call("getExecutableStopLimits", big.NewInt(200)))
	require.Equal(t, []interface{}{ids[:2]}, f.call("getExecutableStopLimits", big.NewInt(2)))
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getExecutableStopLimits", big.NewInt(0)))
}
func TestStopLossProjection(t *testing.T) {
	f := newStopOrdersFixture(t)
	f.tokenCall("approve(address,uint256)", common.HexToAddress(types.PECOROrdersAddress), big.NewInt(1_000_000_000))
	id, _, err := f.k.PlaceLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopOrdersTestPrice(50), 0, types.OrderTypeLimitBuy)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id)
	loss := f.placeLoss()
	require.Equal(t, big.NewInt(2), loss)
	flat := f.call("stopLossOrders", big.NewInt(1))
	require.Equal(t, []interface{}{big.NewInt(0), common.Address{}, common.Address{}, common.Address{}, big.NewInt(0), big.NewInt(0), uint8(0), big.NewInt(0), big.NewInt(0)}, flat)
	tuple := f.call("getStopLossOrder", big.NewInt(1))
	zero := abi.ConvertType(tuple[0], new(stopLossTuple)).(*stopLossTuple)
	require.Equal(t, &stopLossTuple{Id: big.NewInt(0), Amount: big.NewInt(0), TriggerPrice: big.NewInt(0), CreatedAt: big.NewInt(0), ExpiresAt: big.NewInt(0)}, zero)
	require.Equal(t, []interface{}{loss, f.user, f.stable, f.base, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), uint8(0), big.NewInt(1000), big.NewInt(0)}, f.call("stopLossOrders", loss))
	limits, err := f.k.GetUserLimitOrders(f.ctx, f.user)
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, limits)
}
func TestSharedOrderCounter(t *testing.T) {
	f := newStopOrdersFixture(t)
	f.tokenCall("approve(address,uint256)", common.HexToAddress(types.PECOROrdersAddress), big.NewInt(1_000_000_000))
	first, _, err := f.k.PlaceLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopOrdersTestPrice(50), 0, types.OrderTypeLimitBuy)
	require.NoError(t, err)
	require.Equal(t, uint64(1), first)
	require.Equal(t, big.NewInt(2), f.placeLoss())
	next, err := f.k.NextOrderID(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), next)
	require.Equal(t, []interface{}{big.NewInt(3)}, f.call("nextOrderId"))
}
func stopOrdersActiveView(t *testing.T, loss bool) {
	t.Helper()
	f := newStopOrdersFixture(t)
	place := f.placeSell
	active, cancel, count := "activeStopLimitOrderIds", "cancelStopLimitOrder", "getActiveStopLimitOrderCount"
	if loss {
		place = f.placeLoss
		active, cancel, count = "activeStopLossOrderIds", "cancelStopLossOrder", "getActiveStopLossOrderCount"
	}
	first, second, third := place(), place(), place()
	require.Equal(t, []interface{}{big.NewInt(3)}, f.call(count))
	for i, id := range []*big.Int{first, second, third} {
		require.Equal(t, []interface{}{id}, f.call(active, big.NewInt(int64(i))))
	}
	f.call(cancel, second)
	require.Equal(t, []interface{}{third}, f.call(active, big.NewInt(1)))
	require.Equal(t, []interface{}{big.NewInt(2)}, f.call(count))
	for _, index := range []*big.Int{big.NewInt(2), new(big.Int).Lsh(big.NewInt(1), 64)} {
		_, err := f.invoke(f.user, active, index)
		require.EqualError(t, err, "spot: active index out of bounds")
	}
}
func TestStopOrdersActiveStopLimitOrderIds(t *testing.T) { stopOrdersActiveView(t, false) }
func TestStopOrdersActiveStopLossOrderIds(t *testing.T)  { stopOrdersActiveView(t, true) }
func TestStopOrdersGetActiveStopLimitOrderCount(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLimitOrderCount"))
	id := f.placeSell()
	f.placeLoss()
	require.Equal(t, []interface{}{big.NewInt(1)}, f.call("getActiveStopLimitOrderCount"))
	f.call("cancelStopLimitOrder", id)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLimitOrderCount"))
}
func TestStopOrdersGetActiveStopLossOrderCount(t *testing.T) {
	f := newStopOrdersFixture(t)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLossOrderCount"))
	id := f.placeLoss()
	f.placeSell()
	require.Equal(t, []interface{}{big.NewInt(1)}, f.call("getActiveStopLossOrderCount"))
	f.call("cancelStopLossOrder", id)
	require.Equal(t, []interface{}{big.NewInt(0)}, f.call("getActiveStopLossOrderCount"))
}
func TestStopOrdersGetActivatableStopLimits(t *testing.T) {
	f := newStopOrdersFixture(t)
	buy, sell := f.placeBuy(), f.placeSell()
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getActivatableStopLimits", big.NewInt(100)))
	f.setPrice(55)
	require.Equal(t, []interface{}{[]*big.Int{buy}}, f.call("getActivatableStopLimits", big.NewInt(100)))
	f.call("checkStopLimitActivation", buy)
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getActivatableStopLimits", big.NewInt(100)))
	f.setPrice(45)
	require.Equal(t, []interface{}{[]*big.Int{sell}}, f.call("getActivatableStopLimits", big.NewInt(100)))
	f.params.OrdersPaused = true
	f.setParams()
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getActivatableStopLimits", big.NewInt(100)))
}
func TestStopOrdersGetExecutableStopLimits(t *testing.T) {
	f := newStopOrdersFixture(t)
	id := f.placeSell()
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getExecutableStopLimits", big.NewInt(100)))
	f.setPrice(39)
	f.call("checkStopLimitActivation", id)
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getExecutableStopLimits", big.NewInt(100)))
	f.setPrice(40)
	require.Equal(t, []interface{}{[]*big.Int{id}}, f.call("getExecutableStopLimits", big.NewInt(100)))
	f.call("executeStopLimitOrder", id)
	require.Equal(t, []interface{}{[]*big.Int{}}, f.call("getExecutableStopLimits", big.NewInt(100)))
}
func stopOrdersLimitRecord(t *testing.T, flat bool) {
	t.Helper()
	f := newStopOrdersFixture(t)
	expiry := big.NewInt(2000)
	id := f.call("placeStopLimitSell", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(45).BigInt(), stopOrdersTestPrice(40).BigInt(), expiry)[0].(*big.Int)
	if flat {
		require.Equal(t, []interface{}{id, f.user, f.stable, f.base, big.NewInt(1_000_000_000), stopOrdersTestPrice(45).BigInt(), stopOrdersTestPrice(40).BigInt(), uint8(4), uint8(0), big.NewInt(1000), expiry}, f.call("stopLimitOrders", id))
	} else {
		values := f.call("getStopLimitOrder", id)
		got := abi.ConvertType(values[0], new(stopLimitTuple)).(*stopLimitTuple)
		require.Equal(t, &stopLimitTuple{Id: id, User: f.user, Stablecoin: f.stable, Token: f.base, Amount: big.NewInt(1_000_000_000), StopPrice: stopOrdersTestPrice(45).BigInt(), LimitPrice: stopOrdersTestPrice(40).BigInt(), OrderType: 4, Status: 0, CreatedAt: big.NewInt(1000), ExpiresAt: expiry}, got)
	}
	huge := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), id)
	require.Equal(t, []interface{}{big.NewInt(0), common.Address{}, common.Address{}, common.Address{}, big.NewInt(0), big.NewInt(0), big.NewInt(0), uint8(0), uint8(0), big.NewInt(0), big.NewInt(0)}, f.call("stopLimitOrders", huge))
	require.Empty(t, f.logs)
}
func stopOrdersLossRecord(t *testing.T, flat bool) {
	t.Helper()
	f := newStopOrdersFixture(t)
	expiry := big.NewInt(2000)
	id := f.call("placeStopLoss", f.base, f.stable, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), expiry)[0].(*big.Int)
	if flat {
		require.Equal(t, []interface{}{id, f.user, f.stable, f.base, big.NewInt(1_000_000_000), stopOrdersTestPrice(40).BigInt(), uint8(0), big.NewInt(1000), expiry}, f.call("stopLossOrders", id))
	} else {
		values := f.call("getStopLossOrder", id)
		got := abi.ConvertType(values[0], new(stopLossTuple)).(*stopLossTuple)
		require.Equal(t, &stopLossTuple{Id: id, User: f.user, Stablecoin: f.stable, Token: f.base, Amount: big.NewInt(1_000_000_000), TriggerPrice: stopOrdersTestPrice(40).BigInt(), Status: 0, CreatedAt: big.NewInt(1000), ExpiresAt: expiry}, got)
	}
	huge := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), id)
	require.Equal(t, []interface{}{big.NewInt(0), common.Address{}, common.Address{}, common.Address{}, big.NewInt(0), big.NewInt(0), uint8(0), big.NewInt(0), big.NewInt(0)}, f.call("stopLossOrders", huge))
	require.Empty(t, f.logs)
}
func TestStopOrdersGetStopLimitOrder(t *testing.T) { stopOrdersLimitRecord(t, false) }
func TestStopOrdersStopLimitOrders(t *testing.T)   { stopOrdersLimitRecord(t, true) }
func TestStopOrdersGetStopLossOrder(t *testing.T)  { stopOrdersLossRecord(t, false) }
func TestStopOrdersStopLossOrders(t *testing.T)    { stopOrdersLossRecord(t, true) }
func stopOrdersUserView(t *testing.T, loss, indexed bool) {
	t.Helper()
	f := newStopOrdersFixture(t)
	first := f.placeSell()
	second := f.placeLoss()
	third := f.placeBuy()
	fourth := f.placeLoss()
	f.call("cancelStopLimitOrder", first)
	f.call("cancelStopLossOrder", second)
	name := "getUserStopLimitOrders"
	want := []*big.Int{first, third}
	if loss {
		name = "getUserStopLossOrders"
		want = []*big.Int{second, fourth}
	}
	if indexed {
		name = "userStopLimitOrders"
		if loss {
			name = "userStopLossOrders"
		}
		for i, id := range want {
			require.Equal(t, []interface{}{id}, f.call(name, f.user, big.NewInt(int64(i))))
		}
		for _, index := range []*big.Int{big.NewInt(2), new(big.Int).Lsh(big.NewInt(1), 64)} {
			_, err := f.invoke(f.user, name, f.user, index)
			require.EqualError(t, err, "spot: user order index out of bounds")
		}
	} else {
		require.Equal(t, []interface{}{want}, f.call(name, f.user))
		require.Equal(t, []interface{}{[]*big.Int{}}, f.call(name, f.other))
	}
}
func TestStopOrdersGetUserStopLimitOrders(t *testing.T) { stopOrdersUserView(t, false, false) }
func TestStopOrdersGetUserStopLossOrders(t *testing.T)  { stopOrdersUserView(t, true, false) }
func TestStopOrdersUserStopLimitOrders(t *testing.T)    { stopOrdersUserView(t, false, true) }
func TestStopOrdersUserStopLossOrders(t *testing.T)     { stopOrdersUserView(t, true, true) }

type stopLimitTuple struct {
	Id         *big.Int
	User       common.Address
	Stablecoin common.Address
	Token      common.Address
	Amount     *big.Int
	StopPrice  *big.Int
	LimitPrice *big.Int
	OrderType  uint8
	Status     uint8
	CreatedAt  *big.Int
	ExpiresAt  *big.Int
}

type stopLossTuple struct {
	Id           *big.Int
	User         common.Address
	Stablecoin   common.Address
	Token        common.Address
	Amount       *big.Int
	TriggerPrice *big.Int
	Status       uint8
	CreatedAt    *big.Int
	ExpiresAt    *big.Int
}
