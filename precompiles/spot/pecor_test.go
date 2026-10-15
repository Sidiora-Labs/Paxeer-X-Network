package spot_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type pecorFixture struct {
	t         *testing.T
	ctx       sdk.Context
	key       sdk.StoreKey
	feedKey   sdk.StoreKey
	keeper    *spotkeeper.Keeper
	executor  pcommon.PrecompileExecutor
	abi       abi.ABI
	user      common.Address
	stable    common.Address
	pax       common.Address
	sid       common.Address
	collector common.Address
}

func pecorJSON(t *testing.T, ctx sdk.Context, key sdk.StoreKey, name []byte, value interface{}) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	ctx.KVStore(key).Set(name, encoded)
}

func newPECORFixture(t *testing.T) *pecorFixture {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx, _ := app.GetContextForDeliverTx(nil).CacheContext()
	ctx = ctx.WithBlockHeight(8).WithBlockTime(time.Unix(100, 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, feedKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, feedKey)
	feed := pricefeedkeeper.NewKeeper(feedKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	keeper := spotkeeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	contractJSON, err := os.ReadFile("abi/pecor.json")
	require.NoError(t, err)
	contract, err := abi.JSON(bytes.NewReader(contractJSON))
	require.NoError(t, err)
	executor, err := spot.NewPECORExecutor(contract, keeper, &app.EvmKeeper)
	require.NoError(t, err)
	f := &pecorFixture{t: t, ctx: ctx, key: key, feedKey: feedKey, keeper: keeper, executor: executor, abi: contract,
		user: common.HexToAddress("0x3100"), pax: common.HexToAddress("0x4100"), sid: common.HexToAddress("0x4200"), collector: common.HexToAddress("0x3200")}
	for _, address := range []common.Address{f.user, f.collector} {
		app.EvmKeeper.SetAddressMapping(ctx, sdk.AccAddress(address.Bytes()), address)
	}
	app.AccountKeeper.SetModuleAccount(ctx, authtypes.NewEmptyModuleAccount(types.ModuleName))
	for _, entry := range []struct {
		denom   string
		pointer common.Address
	}{{"uhpx", f.pax}, {"uspot", f.sid}} {
		app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: entry.denom, Display: entry.denom[1:], DenomUnits: []*banktypes.DenomUnit{{Denom: entry.denom, Exponent: 0}, {Denom: entry.denom[1:], Exponent: 6}}})
		require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, entry.denom, entry.pointer))
	}
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	constructor, err := (abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}).Pack("USD Ledger", "USDL", big.NewInt(1_500_000_000000), f.user)
	require.NoError(t, err)
	deployer := common.HexToAddress("0x3300")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(pecorTokenCreation), constructor...))
	require.NoError(t, err)
	f.stable = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, f.stable))
	f.tokenCall(f.user, "transfer(address,uint256)", common.LeftPadBytes(keeper.CustodyEVMAddress(ctx).Bytes(), 32), common.LeftPadBytes(big.NewInt(500_000_000000).Bytes(), 32))
	f.tokenCall(f.user, "approve(address,uint256)", common.LeftPadBytes(common.HexToAddress(types.PECORAddress).Bytes(), 32), common.LeftPadBytes(big.NewInt(1_000_000_000000).Bytes(), 32))
	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.FeeCollector = f.collector
	params.Tokens = []types.TokenParams{
		{Address: f.stable, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 10000},
		{Address: f.pax, Kind: types.TokenKindNative, MarketID: types.PAXMarketID, Decimals: 6, MaxUtilizationBps: 10000},
		{Address: f.sid, Kind: types.TokenKindNative, MarketID: types.SIDMarketID, Decimals: 6, MaxUtilizationBps: 10000},
	}
	require.NoError(t, keeper.UpdateParams(ctx, keeper.Authority(), params))
	pecorJSON(t, ctx, feedKey, pricefeedtypes.ParamsKey, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority()))
	for _, entry := range []struct {
		id    uint64
		price sdk.Int
	}{{types.PAXMarketID, sdk.NewIntWithDecimal(5, 17)}, {types.SIDMarketID, sdk.NewIntWithDecimal(2, 18)}} {
		pecorJSON(t, ctx, feedKey, pricefeedtypes.AggregateKey(entry.id), pricefeedtypes.Aggregate{IndexPrice: entry.price, MarkPrice: entry.price, Basis: sdk.ZeroInt(), TimestampMs: 100000, Height: 8, Sources: 1})
	}
	for _, entry := range []struct {
		address common.Address
		amount  int64
	}{{f.stable, 500_000_000000}, {f.pax, 1_000_000_000000}, {f.sid, 250_000_000000}} {
		pecorJSON(t, ctx, key, append(append([]byte(nil), types.BookPrefix...), entry.address.Bytes()...), types.TokenBook{Token: entry.address, Reserves: sdk.NewInt(entry.amount), Decimals: 6, TotalDeposited: sdk.NewInt(entry.amount), TotalWithdrawn: sdk.ZeroInt()})
	}
	for _, entry := range []struct {
		account sdk.AccAddress
		denom   string
		amount  int64
	}{
		{spotkeeper.CustodyAddress(), "uhpx", 1_000_000_000000}, {spotkeeper.CustodyAddress(), "uspot", 250_000_000000},
		{sdk.AccAddress(f.user.Bytes()), "uhpx", 1_000_000_000000}, {sdk.AccAddress(f.user.Bytes()), "uspot", 1_000_000_000000},
	} {
		coins := sdk.NewCoins(sdk.NewInt64Coin(entry.denom, entry.amount))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", entry.account, coins))
	}
	return f
}

func (f *pecorFixture) tokenCall(caller common.Address, signature string, words ...[]byte) {
	f.t.Helper()
	data := append([]byte(nil), crypto.Keccak256([]byte(signature))[:4]...)
	for _, word := range words {
		data = append(data, word...)
	}
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(f.ctx, caller, &f.stable, nil, data)
	require.NoError(f.t, err)
	require.Equal(f.t, common.LeftPadBytes([]byte{1}, 32), ret)
}

func (f *pecorFixture) balance(token, holder common.Address) *big.Int {
	f.t.Helper()
	if token == f.stable {
		data := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(holder.Bytes(), 32)...)
		ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(f.ctx, spotkeeper.CustodyAddress(), &token, data)
		require.NoError(f.t, err)
		return new(big.Int).SetBytes(ret)
	}
	denom := "uhpx"
	if token == f.sid {
		denom = "uspot"
	}
	app := testkeeper.EVMTestApp
	return app.BankKeeper.GetBalance(f.ctx, app.EvmKeeper.GetPaxAddressOrDefault(f.ctx, holder), denom).Amount.BigInt()
}

func (f *pecorFixture) pack(name string, args ...interface{}) []byte {
	f.t.Helper()
	data, err := f.abi.Pack(name, args...)
	require.NoError(f.t, err)
	return data
}

func (f *pecorFixture) invoke(caller common.Address, input []byte, value *big.Int, readOnly bool) (ret []byte, logs []*ethtypes.Log, callErr error) {
	f.t.Helper()
	var method *abi.Method
	var args []interface{}
	var err error
	if len(input) == 0 {
		method = &f.abi.Receive
	} else {
		method, err = f.abi.MethodById(input[:4])
		require.NoError(f.t, err)
		args, err = method.Inputs.Unpack(input[4:])
		require.NoError(f.t, err)
	}
	app := testkeeper.EVMTestApp
	require.NoError(f.t, app.EvmKeeper.RunWithOneOffEVMInstance(f.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		require.NotNil(f.t, db)
		revision := db.Snapshot()
		ctx := db.Ctx()
		if value != nil && value.Sign() > 0 {
			amount := new(big.Int).Quo(value, big.NewInt(1_000_000_000_000))
			if amount.Sign() > 0 {
				require.NoError(f.t, app.BankKeeper.SendCoins(ctx, app.EvmKeeper.GetPaxAddressOrDefault(ctx, caller), app.EvmKeeper.GetPaxAddressOrDefault(ctx, common.HexToAddress(types.PECORAddress)), sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewIntFromBigInt(amount)))))
			}
		}
		ret, callErr = f.executor.Execute(ctx, method, caller, common.HexToAddress(types.PECORAddress), args, value, readOnly, evm, nil)
		if callErr != nil {
			db.RevertToSnapshot(revision)
		}
		logs = append(logs, db.GetAllLogs()...)
		return nil
	}, func(string, string) {}))
	return
}

func (f *pecorFixture) call(name string, args ...interface{}) ([]interface{}, []*ethtypes.Log) {
	f.t.Helper()
	method := f.abi.Methods[name]
	ret, logs, err := f.invoke(f.user, f.pack(name, args...), new(big.Int), method.StateMutability == "view")
	require.NoError(f.t, err)
	values, err := method.Outputs.Unpack(ret)
	require.NoError(f.t, err)
	return values, logs
}

func pecorSnapshot(t *testing.T, ctx sdk.Context, key sdk.StoreKey) map[string]string {
	t.Helper()
	out := map[string]string{}
	it := ctx.KVStore(key).Iterator(nil, nil)
	defer func() { require.NoError(t, it.Close()) }()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	return out
}

func (f *pecorFixture) refusal(want *types.AbiError, name string, args ...interface{}) {
	f.t.Helper()
	before := pecorSnapshot(f.t, f.ctx, f.key)
	stable, pax := f.balance(f.stable, f.user), f.balance(f.pax, f.user)
	ret, logs, err := f.invoke(f.user, f.pack(name, args...), new(big.Int), false)
	require.ErrorIs(f.t, err, want)
	require.Equal(f.t, want.Data(), ret)
	require.Empty(f.t, logs)
	require.Equal(f.t, before, pecorSnapshot(f.t, f.ctx, f.key))
	require.Equal(f.t, stable, f.balance(f.stable, f.user))
	require.Equal(f.t, pax, f.balance(f.pax, f.user))
}

func (f *pecorFixture) event(logs []*ethtypes.Log, name string, indexed []common.Address, values ...interface{}) {
	f.t.Helper()
	event := f.abi.Events[name]
	expected, err := event.Inputs.NonIndexed().Pack(values...)
	require.NoError(f.t, err)
	topics := []common.Hash{event.ID}
	for _, address := range indexed {
		topics = append(topics, common.BytesToHash(address.Bytes()))
	}
	count := 0
	for _, log := range logs {
		if log.Address == common.HexToAddress(types.PECORAddress) && len(log.Topics) > 0 && log.Topics[0] == event.ID {
			count++
			require.Equal(f.t, topics, log.Topics)
			require.Equal(f.t, expected, log.Data)
		}
	}
	require.Equal(f.t, 1, count, name)
}

func TestPECORMarketBuy(t *testing.T) {
	f := newPECORFixture(t)
	before := f.balance(f.pax, f.user)
	values, logs := f.call("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1990_000000), big.NewInt(100))
	require.Equal(t, big.NewInt(1994_000000), values[0])
	require.Equal(t, new(big.Int).Add(before, big.NewInt(1994_000000)), f.balance(f.pax, f.user))
	f.event(logs, "MarketOrderExecuted", []common.Address{f.user, f.stable, f.pax}, big.NewInt(1000_000000), big.NewInt(1994_000000), true)
	f.event(logs, "TieredFeeApplied", []common.Address{f.user, f.pax}, sdk.NewIntWithDecimal(1000, 18).BigInt(), big.NewInt(30), big.NewInt(6_000000))
	f.refusal(types.ErrExpired, "marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), big.NewInt(99))
	f.refusal(types.ErrNotAStablecoin, "marketBuy", f.pax, f.sid, big.NewInt(1), new(big.Int), new(big.Int))
	f.refusal(types.ErrTokenIsStablecoin, "marketBuy", f.stable, f.stable, big.NewInt(1), new(big.Int), new(big.Int))
}

func TestPECORMarketSell(t *testing.T) {
	f := newPECORFixture(t)
	before := f.balance(f.stable, f.user)
	values, logs := f.call("marketSell", f.pax, f.stable, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	require.Equal(t, big.NewInt(498_500000), values[0])
	require.Equal(t, new(big.Int).Add(before, big.NewInt(498_500000)), f.balance(f.stable, f.user))
	f.event(logs, "MarketOrderExecuted", []common.Address{f.user, f.stable, f.pax}, big.NewInt(498_500000), big.NewInt(1000_000000), false)
}

func TestPECORMarketSlippage(t *testing.T) {
	f := newPECORFixture(t)
	f.refusal(types.ErrInsufficientOutput, "marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1995_000000), new(big.Int))
}

func TestPECORMarketLiquidity(t *testing.T) {
	f := newPECORFixture(t)
	f.refusal(types.ErrInsufficientLiquidity, "marketBuy", f.stable, f.pax, big.NewInt(600_000_000000), new(big.Int), new(big.Int))
}

func TestPECORSwapExactIn(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("swapExactIn", f.sid, f.pax, big.NewInt(100_000000), new(big.Int), new(big.Int))
	require.Equal(t, big.NewInt(398_800000), values[0])
	f.event(logs, "SimpleSwap", []common.Address{f.user, f.sid, f.pax}, big.NewInt(100_000000), big.NewInt(398_800000), big.NewInt(500_000_000_000_000_000))
	f.refusal(types.ErrSameToken, "swapExactIn", f.pax, f.pax, big.NewInt(1), new(big.Int), new(big.Int))
	f.refusal(types.ErrZeroAmount, "swapExactIn", f.sid, f.pax, new(big.Int), new(big.Int), new(big.Int))
}

func TestPECORSwapExactOut(t *testing.T) {
	f := newPECORFixture(t)
	f.refusal(types.ErrExcessiveInput, "swapExactOut", f.stable, f.pax, big.NewInt(1994_000000), big.NewInt(999_999999), new(big.Int))
	values, logs := f.call("swapExactOut", f.stable, f.pax, big.NewInt(1994_000000), big.NewInt(1000_000000), new(big.Int))
	require.Equal(t, big.NewInt(1000_000000), values[0])
	f.event(logs, "SimpleSwap", []common.Address{f.user, f.stable, f.pax}, big.NewInt(1000_000000), big.NewInt(1994_000000), big.NewInt(500_000_000_000_000_000))
	for _, log := range logs {
		require.NotEqual(t, f.abi.Events["TieredFeeApplied"].ID, log.Topics[0])
		require.NotEqual(t, f.abi.Events["PriceImpactApplied"].ID, log.Topics[0])
	}
}

func TestPECORSwapExactInNative(t *testing.T) {
	f := newPECORFixture(t)
	value := new(big.Int).Mul(big.NewInt(1000_000000), big.NewInt(1_000_000_000_000))
	before := f.balance(f.pax, f.user)
	ret, logs, err := f.invoke(f.user, f.pack("swapExactInNative", f.stable, new(big.Int), new(big.Int)), value, false)
	require.NoError(t, err)
	values, err := f.abi.Methods["swapExactInNative"].Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(498_500000), values[0])
	require.Equal(t, new(big.Int).Sub(before, big.NewInt(1000_000000)), f.balance(f.pax, f.user))
	f.event(logs, "NativeSwap", []common.Address{f.user, f.stable}, big.NewInt(1000_000000), big.NewInt(498_500000), true)
	ret, _, err = f.invoke(f.user, f.pack("swapExactInNative", f.pax, new(big.Int), new(big.Int)), value, false)
	require.ErrorIs(t, err, types.ErrUseWethDeposit)
	require.Equal(t, types.ErrUseWethDeposit.Data(), ret)
	ret, _, err = f.invoke(f.user, f.pack("swapExactInNative", f.stable, new(big.Int), new(big.Int)), big.NewInt(1), false)
	require.ErrorIs(t, err, types.ErrUnsupportedInputTransfer)
	require.Equal(t, types.ErrUnsupportedInputTransfer.Data(), ret)
}

func TestPECORSwapExactInToNative(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("swapExactInToNative", f.stable, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	require.Equal(t, big.NewInt(1994_000000), values[0])
	f.event(logs, "NativeSwap", []common.Address{f.user, f.stable}, big.NewInt(1994_000000), big.NewInt(1000_000000), false)
	f.refusal(types.ErrUseWethWithdraw, "swapExactInToNative", f.pax, big.NewInt(1), new(big.Int), new(big.Int))
}

func TestPECORMulticall(t *testing.T) {
	t.Run("ordered results and caller", func(t *testing.T) {
		f := newPECORFixture(t)
		values, logs := f.call("multicall", [][]byte{f.pack("swapFeeBps"), f.pack("tier1FeeBps"), f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1990_000000), new(big.Int))})
		results := values[0].([][]byte)
		require.Len(t, results, 3)
		require.Equal(t, big.NewInt(30), new(big.Int).SetBytes(results[0]))
		require.Equal(t, big.NewInt(20), new(big.Int).SetBytes(results[1]))
		require.Equal(t, big.NewInt(1994_000000), new(big.Int).SetBytes(results[2]))
		f.event(logs, "MarketOrderExecuted", []common.Address{f.user, f.stable, f.pax}, big.NewInt(1000_000000), big.NewInt(1994_000000), true)
	})
	t.Run("nonpayable", func(t *testing.T) {
		f := newPECORFixture(t)
		before := pecorSnapshot(t, f.ctx, f.key)
		_, logs, err := f.invoke(f.user, f.pack("multicall", [][]byte{f.pack("swapFeeBps")}), big.NewInt(1), false)
		require.EqualError(t, err, pcommon.ValidateNonPayable(big.NewInt(1)).Error())
		require.Empty(t, logs)
		require.Equal(t, before, pecorSnapshot(t, f.ctx, f.key))
	})
	t.Run("bubble custom revert and undo earlier trade", func(t *testing.T) {
		f := newPECORFixture(t)
		f.refusal(types.OwnableUnauthorizedAccount(f.user), "multicall", [][]byte{
			f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int)),
			f.pack("setSwapFee", big.NewInt(50)),
		})
	})
	t.Run("empty revert index and rollback", func(t *testing.T) {
		f := newPECORFixture(t)
		f.refusal(types.MulticallFailed(uint256.NewInt(1)), "multicall", [][]byte{
			f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int)),
			{0xff, 0xff, 0xff, 0xff},
		})
	})
	t.Run("malformed element", func(t *testing.T) {
		f := newPECORFixture(t)
		f.refusal(types.MulticallFailed(uint256.NewInt(0)), "multicall", [][]byte{{0x01}})
		f.refusal(types.MulticallFailed(uint256.NewInt(0)), "multicall", [][]byte{f.abi.Methods["marketBuy"].ID})
	})
	t.Run("nested caller", func(t *testing.T) {
		f := newPECORFixture(t)
		f.refusal(types.OwnableUnauthorizedAccount(f.user), "multicall", [][]byte{f.pack("multicall", [][]byte{f.pack("pause")})})
	})
}

func TestPECORReceiveRefuses(t *testing.T) {
	f := newPECORFixture(t)
	before := f.balance(f.pax, f.user)
	ret, logs, err := f.invoke(f.user, nil, big.NewInt(1e18), false)
	require.ErrorIs(t, err, types.ErrNativeTransferFailed)
	require.Equal(t, types.ErrNativeTransferFailed.Data(), ret)
	require.Empty(t, logs)
	require.Equal(t, before, f.balance(f.pax, f.user))
}

func TestPECORReceive(t *testing.T) {
	f := newPECORFixture(t)
	ret, logs, err := f.invoke(f.user, nil, new(big.Int), false)
	require.ErrorIs(t, err, types.ErrNativeTransferFailed)
	require.Equal(t, types.ErrNativeTransferFailed.Data(), ret)
	require.Empty(t, logs)
}

func TestPECORQuoteMatchesSwap(t *testing.T) {
	f := newPECORFixture(t)
	quote, _ := f.call("getQuoteExactIn", f.stable, f.pax, big.NewInt(1000_000000))
	result, _ := f.call("swapExactIn", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	require.Equal(t, big.NewInt(1994_000000), quote[0])
	require.Equal(t, quote, result)
}

func pecorDetailedQuote(t *testing.T, amount int64, expected ...*big.Int) {
	t.Helper()
	f := newPECORFixture(t)
	values, logs := f.call("getDetailedQuote", f.stable, f.pax, big.NewInt(amount))
	require.Empty(t, logs)
	require.Len(t, values, len(expected))
	for i, want := range expected {
		require.Equal(t, want, values[i])
	}
}

func TestPECORDetailedQuote(t *testing.T) {
	pecorDetailedQuote(t, 20_000_000000, big.NewInt(40_000_000000), big.NewInt(39_784_080000), big.NewInt(4), big.NewInt(50), big.NewInt(200_000000))
}

func TestPECORGetDetailedQuote(t *testing.T) {
	pecorDetailedQuote(t, 150_000_000000, big.NewInt(300_000_000000), big.NewInt(296_109_000000), big.NewInt(30), big.NewInt(100), big.NewInt(3000_000000))
}

func TestPECORQuoteIsView(t *testing.T) {
	f := newPECORFixture(t)
	before := pecorSnapshot(t, f.ctx, f.key)
	stable, pax := f.balance(f.stable, f.user), f.balance(f.pax, f.user)
	for _, name := range []string{"getQuoteExactIn", "getQuoteExactOut", "getDetailedQuote"} {
		_, logs := f.call(name, f.stable, f.pax, big.NewInt(1000_000000))
		require.Empty(t, logs)
		require.Equal(t, before, pecorSnapshot(t, f.ctx, f.key))
		require.Equal(t, stable, f.balance(f.stable, f.user))
		require.Equal(t, pax, f.balance(f.pax, f.user))
	}
	_, logs, err := f.invoke(f.user, f.pack("marketBuy", f.stable, f.pax, big.NewInt(1), new(big.Int), new(big.Int)), new(big.Int), true)
	require.ErrorIs(t, err, vm.ErrWriteProtection)
	require.Empty(t, logs)
}

func TestPECORGetQuoteExactIn(t *testing.T) {
	f := newPECORFixture(t)
	values, _ := f.call("getQuoteExactIn", f.stable, f.pax, big.NewInt(1000_000000))
	require.Equal(t, big.NewInt(1994_000000), values[0])
	f.refusal(types.ErrSameToken, "getQuoteExactIn", f.pax, f.pax, big.NewInt(1))
	aggregate := pricefeedtypes.Aggregate{IndexPrice: sdk.NewIntWithDecimal(5, 17), MarkPrice: sdk.NewIntWithDecimal(5, 17), Basis: sdk.ZeroInt(), TimestampMs: 1, Height: 1, Sources: 1}
	pecorJSON(t, f.ctx, f.feedKey, pricefeedtypes.AggregateKey(types.PAXMarketID), aggregate)
	f.refusal(types.ErrStalePrice, "getQuoteExactIn", f.stable, f.pax, big.NewInt(1000_000000))
	f.refusal(types.ErrStalePrice, "marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
}

func TestPECORGetQuoteExactOut(t *testing.T) {
	f := newPECORFixture(t)
	values, _ := f.call("getQuoteExactOut", f.stable, f.pax, big.NewInt(1994_000000))
	require.Equal(t, big.NewInt(1000_000000), values[0])
	f.refusal(types.ErrSameToken, "getQuoteExactOut", f.pax, f.pax, big.NewInt(1))
	f.refusal(types.ErrZeroAmount, "getQuoteExactOut", f.stable, f.pax, new(big.Int))
}

func pecorAdminRefusal(t *testing.T, name string, args ...interface{}) {
	t.Helper()
	f := newPECORFixture(t)
	f.refusal(types.OwnableUnauthorizedAccount(f.user), name, args...)
	owner := testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(f.ctx, authtypes.NewModuleAddress(govtypes.ModuleName))
	ret, logs, err := f.invoke(owner, f.pack(name, args...), new(big.Int), false)
	require.ErrorIs(t, err, types.ErrOwnableUnauthorizedAccount)
	require.Equal(t, types.OwnableUnauthorizedAccount(owner).Data(), ret)
	require.Empty(t, logs)
}

func TestPECORAdminRefused(t *testing.T) {
	pecorAdminRefusal(t, "setSwapFee", big.NewInt(50))
}

func TestPECORCollectFees(t *testing.T) {
	f := newPECORFixture(t)
	f.call("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	f.refusal(types.OwnableUnauthorizedAccount(f.user), "collectFees", f.pax)
	before := f.balance(f.pax, f.collector)
	ret, _, err := f.invoke(f.collector, f.pack("collectFees", f.pax), new(big.Int), false)
	require.NoError(t, err)
	require.Empty(t, ret)
	require.Equal(t, new(big.Int).Add(before, big.NewInt(6_000000)), f.balance(f.pax, f.collector))
	values, _ := f.call("accumulatedFees", f.pax)
	require.Equal(t, new(big.Int), values[0])
	ret, _, err = f.invoke(f.collector, f.pack("collectFees", f.pax), new(big.Int), false)
	require.ErrorIs(t, err, types.ErrNoFeesToCollect)
	require.Equal(t, types.ErrNoFeesToCollect.Data(), ret)
}

func TestPECORAccumulatedFees(t *testing.T) {
	f := newPECORFixture(t)
	f.call("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	values, _ := f.call("accumulatedFees", f.pax)
	require.Equal(t, big.NewInt(6_000000), values[0])
	orderFees, err := f.keeper.AccumulatedFees(f.ctx, common.HexToAddress(types.PECOROrdersAddress), f.pax)
	require.NoError(t, err)
	require.True(t, orderFees.IsZero())
}

func TestPECORGetAccumulatedFees(t *testing.T) {
	f := newPECORFixture(t)
	f.call("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
	values, _ := f.call("getAccumulatedFees", f.pax)
	require.Equal(t, big.NewInt(6_000000), values[0])
}

func TestPECORPaused(t *testing.T) {
	f := newPECORFixture(t)
	values, _ := f.call("paused")
	require.Equal(t, false, values[0])
	params, err := f.keeper.GetParams(f.ctx)
	require.NoError(t, err)
	params.SwapPaused = true
	require.NoError(t, f.keeper.UpdateParams(f.ctx, f.keeper.Authority(), params))
	values, _ = f.call("paused")
	require.Equal(t, true, values[0])
	f.refusal(types.ErrEnforcedPause, "marketBuy", f.stable, f.pax, big.NewInt(1), new(big.Int), new(big.Int))
}

func TestPECORPrecision(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("PRECISION")
	require.Equal(t, big.NewInt(1e18), values[0])
	require.Empty(t, logs)
}

func TestPECORPriceOracle(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("priceOracle")
	require.Equal(t, common.HexToAddress("0x1020"), values[0])
	require.Empty(t, logs)
}

func TestPECORWeth(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("WETH")
	require.Equal(t, f.pax, values[0])
	require.Empty(t, logs)
}

func TestPECORMaxFeeBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("MAX_FEE_BPS")
	require.Equal(t, big.NewInt(200), values[0])
	require.Empty(t, logs)
}

func TestPECORMaxImpactBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("MAX_IMPACT_BPS")
	require.Equal(t, big.NewInt(500), values[0])
	require.Empty(t, logs)
}

func TestPECORTier1Threshold(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("TIER1_THRESHOLD")
	require.Equal(t, sdk.NewIntWithDecimal(10000, 18).BigInt(), values[0])
	require.Empty(t, logs)
}

func TestPECORTier2Threshold(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("TIER2_THRESHOLD")
	require.Equal(t, sdk.NewIntWithDecimal(100000, 18).BigInt(), values[0])
	require.Empty(t, logs)
}

func TestPECORSwapFeeBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("swapFeeBps")
	require.Equal(t, big.NewInt(30), values[0])
	require.Empty(t, logs)
}

func TestPECORTier1FeeBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("tier1FeeBps")
	require.Equal(t, big.NewInt(20), values[0])
	require.Empty(t, logs)
}

func TestPECORTier2FeeBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("tier2FeeBps")
	require.Equal(t, big.NewInt(50), values[0])
	require.Empty(t, logs)
}

func TestPECORImpactScalarBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("impactScalarBps")
	require.Equal(t, big.NewInt(100), values[0])
	require.Empty(t, logs)
}

func TestPECORPriceImpactEnabled(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("priceImpactEnabled")
	require.Equal(t, true, values[0])
	require.Empty(t, logs)
}

func TestPECORBpsDenominator(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("BPS_DENOMINATOR")
	require.Equal(t, big.NewInt(10000), values[0])
	require.Empty(t, logs)
}

func TestPECOROwner(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("owner")
	require.Equal(t, testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(f.ctx, authtypes.NewModuleAddress(govtypes.ModuleName)), values[0])
	require.Empty(t, logs)
}

func TestPECORTwapSanityEnabled(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("twapSanityEnabled")
	require.Equal(t, false, values[0])
	require.Empty(t, logs)
}

func TestPECORTwapSanityPeriod(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("twapSanityPeriod")
	require.Equal(t, big.NewInt(300), values[0])
	require.Empty(t, logs)
}

func TestPECORMaxTwapDeviationBps(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("maxTwapDeviationBps")
	require.Equal(t, big.NewInt(200), values[0])
	require.Empty(t, logs)
}

func TestPECORTransactionTracker(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("transactionTracker")
	require.Equal(t, common.Address{}, values[0])
	require.Empty(t, logs)
}

func TestPECORVault(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("vault")
	require.Equal(t, common.HexToAddress("0x1025"), values[0])
	require.Empty(t, logs)
}

func TestPECORFeeCollector(t *testing.T) {
	f := newPECORFixture(t)
	values, logs := f.call("feeCollector")
	require.Equal(t, f.collector, values[0])
	require.Empty(t, logs)
}

func TestPECORRenounceOwnership(t *testing.T) {
	pecorAdminRefusal(t, "renounceOwnership")
}

func TestPECORTransferOwnership(t *testing.T) {
	pecorAdminRefusal(t, "transferOwnership", common.Address{})
}

func TestPECORPause(t *testing.T) {
	pecorAdminRefusal(t, "pause")
}

func TestPECORUnpause(t *testing.T) {
	pecorAdminRefusal(t, "unpause")
}

func TestPECORSetSwapFee(t *testing.T) {
	pecorAdminRefusal(t, "setSwapFee", big.NewInt(50))
}

func TestPECORSetTieredFees(t *testing.T) {
	pecorAdminRefusal(t, "setTieredFees", big.NewInt(25), big.NewInt(50))
}

func TestPECORSetPriceImpact(t *testing.T) {
	pecorAdminRefusal(t, "setPriceImpact", true, big.NewInt(101))
}

func TestPECORSetTwapSanity(t *testing.T) {
	pecorAdminRefusal(t, "setTwapSanity", true, big.NewInt(301), big.NewInt(201))
}

func TestPECORSetPriceOracle(t *testing.T) {
	pecorAdminRefusal(t, "setPriceOracle", common.Address{})
}

func TestPECORSetTransactionTracker(t *testing.T) {
	pecorAdminRefusal(t, "setTransactionTracker", common.Address{})
}

func TestPECORSetFeeCollector(t *testing.T) {
	pecorAdminRefusal(t, "setFeeCollector", common.Address{})
}

const pecorTokenCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
