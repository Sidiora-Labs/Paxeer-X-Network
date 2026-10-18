package spot_test

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type quoterEnv struct {
	t              *testing.T
	k              *spotkeeper.Keeper
	precompile     *pcommon.Precompile
	stateDB        *state.DBImpl
	evm            *vm.EVM
	app            *app.App
	spotKey        sdk.StoreKey
	feedKey        sdk.StoreKey
	usdl, pax, sid common.Address
	caller         common.Address
}

func newQuoterEnv(t *testing.T) *quoterEnv {
	t.Helper()
	a := app.Setup(t, false, false, false)
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).WithBlockTime(time.Unix(1000, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	a.EvmKeeper.BeginBlock(ctx)
	feedKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	spotKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, feedKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, spotKey)
	feed := pricefeedkeeper.NewKeeper(feedKey, a.StakingKeeper, a.AccountKeeper, &a.EvmKeeper)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	k := spotkeeper.NewKeeper(spotKey, a.BankKeeper, &a.EvmKeeper, feed)
	e := &quoterEnv{t: t, k: k, app: a, spotKey: spotKey, feedKey: feedKey,
		pax: common.HexToAddress("0xa1"), sid: common.HexToAddress("0xb2"), caller: common.HexToAddress("0xc3")}
	require.NoError(t, a.EvmKeeper.SetERC20NativePointer(ctx, a.EvmKeeper.GetBaseDenom(ctx), e.pax))
	require.NoError(t, a.EvmKeeper.SetERC20NativePointer(ctx, layerxbridgetypes.SidioraDenom(), e.sid))
	tokenBytes, err := os.ReadFile("../launchpad/abi/SidioraERC20.json")
	require.NoError(t, err)
	tokenABI, err := abi.JSON(bytes.NewReader(tokenBytes))
	require.NoError(t, err)
	constructor, err := tokenABI.Constructor.Inputs.Pack("USD Ledger", "USDL", big.NewInt(500_000e6), k.CustodyEVMAddress(ctx))
	require.NoError(t, err)
	nonce := a.EvmKeeper.GetNonce(ctx, e.caller)
	_, err = a.EvmKeeper.CallEVM(ctx, e.caller, nil, nil, append(common.FromHex(quoterTokenCreation), constructor...))
	require.NoError(t, err)
	e.usdl = crypto.CreateAddress(e.caller, nonce)
	callerFunds := sdk.NewCoins(sdk.NewInt64Coin(a.EvmKeeper.GetBaseDenom(ctx), 1))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, callerFunds))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, authtypes.NewModuleAddress(evmtypes.ModuleName), a.EvmKeeper.GetPaxAddressOrDefault(ctx, e.caller), callerFunds))
	require.NotEmpty(t, a.EvmKeeper.GetCode(ctx, e.usdl))
	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.Tokens = types.OpeningTokens(e.pax, e.sid)
	params.Tokens[0].Address = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	coins := sdk.NewCoins(sdk.NewInt64Coin(a.EvmKeeper.GetBaseDenom(ctx), 1_000_000e6), sdk.NewInt64Coin(layerxbridgetypes.SidioraDenom(), 250_000e6))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, coins))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, authtypes.NewModuleAddress(evmtypes.ModuleName), spotkeeper.CustodyAddress(), coins))
	for i, token := range []common.Address{e.usdl, e.pax, e.sid} {
		amount := sdk.NewInt([]int64{500_000e6, 1_000_000e6, 250_000e6}[i])
		require.NoError(t, k.SetBook(ctx, types.TokenBook{Token: token, Reserves: amount, TotalDeposited: amount, TotalWithdrawn: sdk.ZeroInt(), Decimals: 6}))
	}
	e.writePrice(ctx, 11, sdk.NewIntWithDecimal(5, 17), 1000_000)
	e.writePrice(ctx, 12, sdk.NewIntWithDecimal(2, 18), 1000_000)
	e.precompile, err = spot.NewQuoterPrecompile(k, &a.EvmKeeper)
	require.NoError(t, err)
	e.stateDB = state.NewDBImpl(ctx, &a.EvmKeeper, false)
	block, err := a.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(a.EvmKeeper.ChainID(ctx))
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, precompile := range a.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = precompile
	}
	precompiles[common.HexToAddress(types.PECORQuoterAddress)] = e.precompile
	e.evm = vm.NewEVM(*block, e.stateDB, cfg, vm.Config{}, precompiles)
	return e
}

func (e *quoterEnv) writePrice(ctx sdk.Context, market uint64, price sdk.Int, timestamp uint64) {
	e.t.Helper()
	record := pricefeedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(), TimestampMs: timestamp, Height: ctx.BlockHeight(), Sources: 1}
	data, err := json.Marshal(record)
	require.NoError(e.t, err)
	ctx.KVStore(e.feedKey).Set(pricefeedtypes.AggregateKey(market), data)
}

func quoterStore(ctx sdk.Context, key sdk.StoreKey) map[string][]byte {
	values := map[string][]byte{}
	iter := ctx.KVStore(key).Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		values[string(iter.Key())] = common.CopyBytes(iter.Value())
	}
	return values
}

func (e *quoterEnv) send(method string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	input, err := e.precompile.Pack(method, args...)
	require.NoError(e.t, err)
	beforeSpot := quoterStore(e.stateDB.Ctx(), e.spotKey)
	beforeFeed := quoterStore(e.stateDB.Ctx(), e.feedKey)
	ret, _, err := e.evm.StaticCall(e.caller, common.HexToAddress(types.PECORQuoterAddress), input, 30_000_000)
	require.Equal(e.t, beforeSpot, quoterStore(e.stateDB.Ctx(), e.spotKey))
	require.Equal(e.t, beforeFeed, quoterStore(e.stateDB.Ctx(), e.feedKey))
	return ret, err
}

func (e *quoterEnv) call(method string, args ...interface{}) []interface{} {
	e.t.Helper()
	data, err := e.send(method, args...)
	require.NoError(e.t, err, method)
	result, err := e.precompile.Unpack(method, data)
	require.NoError(e.t, err, method)
	return result
}

func (e *quoterEnv) quote(method string, args ...interface{}) spotkeeper.QuoteResult {
	e.t.Helper()
	values := e.call(method, args...)
	require.Len(e.t, values, 1)
	return *abi.ConvertType(values[0], new(spotkeeper.QuoteResult)).(*spotkeeper.QuoteResult)
}

func quoterInt(t *testing.T, want int64, got *big.Int) {
	t.Helper()
	require.NotNil(t, got)
	require.Equal(t, big.NewInt(want).String(), got.String())
}

func requireQuoterBuy(t *testing.T, q spotkeeper.QuoteResult) {
	t.Helper()
	quoterInt(t, 1_000e6, q.AmountIn)
	quoterInt(t, 1_994e6, q.AmountOut)
	quoterInt(t, 2_000e6, q.GrossAmountOut)
	quoterInt(t, 1_994_000, q.ExecutionPrice)
	quoterInt(t, 1e18, q.SpotPriceIn)
	quoterInt(t, 5e17, q.SpotPriceOut)
	quoterInt(t, 6e6, q.FeeAmount)
	quoterInt(t, 30, q.FeeBps)
	require.True(t, q.SufficientLiquidity)
	quoterInt(t, 1_000_000e6, q.AvailableLiquidity)
	quoterInt(t, 1000, q.PriceTimestampIn)
	quoterInt(t, 1000, q.PriceTimestampOut)
	require.False(t, q.PriceStaleIn)
	require.False(t, q.PriceStaleOut)
	quoterInt(t, 9000, q.UtilizationCapBps)
	quoterInt(t, 20, q.TradeUtilizationBps)
	require.False(t, q.CappedByUtilization)
	quoterInt(t, 0, q.TwapOut)
	quoterInt(t, 0, q.TwapDeviationBps)
	quoterInt(t, 30, q.EffectiveFeeBps)
	quoterInt(t, 0, q.Tier)
	quoterInt(t, 0, q.PriceImpactBps)
	quoterInt(t, 0, q.PriceImpactDeduction)
}

func TestQuoterMarketBuy(t *testing.T) {
	e := newQuoterEnv(t)
	requireQuoterBuy(t, e.quote("quoteMarketBuy", e.usdl, e.pax, big.NewInt(1_000e6)))
}

func TestQuoterFlagsInsteadOfRevert(t *testing.T) {
	e := newQuoterEnv(t)
	e.writePrice(e.stateDB.Ctx(), 11, sdk.NewIntWithDecimal(5, 17), 1)
	q := e.quote("quoteMarketBuy", e.usdl, e.pax, big.NewInt(1_000e6))
	require.False(t, q.PriceStaleIn)
	require.True(t, q.PriceStaleOut)
	require.False(t, q.SufficientLiquidity)
	quoterInt(t, 0, q.AmountOut)
	unknown := common.HexToAddress("0xd4")
	q = e.quote("quoteExactIn", unknown, e.usdl, big.NewInt(1))
	require.True(t, q.PriceStaleIn)
	require.False(t, q.SufficientLiquidity)
	quoterInt(t, 0, q.AmountOut)
}

func TestQuoterTier(t *testing.T) {
	e := newQuoterEnv(t)
	for _, tc := range []struct{ amount, tier, fee, net, impact int64 }{{1000e6, 0, 30, 1994e6, 0}, {20_000e6, 1, 50, 39_784_080_000, 4}, {150_000e6, 2, 100, 296_109e6, 30}} {
		q := e.quote("quoteMarketBuy", e.usdl, e.pax, big.NewInt(tc.amount))
		quoterInt(t, tc.tier, q.Tier)
		quoterInt(t, tc.fee, q.EffectiveFeeBps)
		quoterInt(t, tc.fee, q.FeeBps)
		quoterInt(t, tc.net, q.AmountOut)
		quoterInt(t, tc.impact, q.PriceImpactBps)
	}
}

func TestQuoterWeth(t *testing.T) {
	e := newQuoterEnv(t)
	require.Equal(t, []interface{}{e.pax}, e.call("WETH"))
	input, err := e.precompile.Pack("WETH")
	require.NoError(t, err)
	_, _, err = e.evm.Call(e.caller, common.HexToAddress(types.PECORQuoterAddress), input, 30_000_000, uint256.NewInt(1))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
}

type quoterBatchRequest struct {
	TokenIn   common.Address
	TokenOut  common.Address
	Amount    *big.Int
	IsExactIn bool
}

func TestQuoterBatchQuote(t *testing.T) {
	e := newQuoterEnv(t)
	requests := []quoterBatchRequest{
		{e.usdl, common.Address{}, big.NewInt(1000e6), true},
		{common.Address{}, e.usdl, big.NewInt(1000e6), true},
		{e.usdl, e.pax, big.NewInt(1994e6), false},
		{common.HexToAddress("0xd4"), e.pax, big.NewInt(1), true},
	}
	results := *abi.ConvertType(e.call("batchQuote", requests)[0], new([]spotkeeper.QuoteResult)).(*[]spotkeeper.QuoteResult)
	require.Len(t, results, 4)
	requireQuoterBuy(t, results[0])
	quoterInt(t, 498_500_000, results[1].AmountOut)
	quoterInt(t, 1_000e6, results[2].AmountIn)
	require.True(t, results[3].PriceStaleIn)
	require.False(t, results[3].SufficientLiquidity)
	quoterInt(t, 0, results[3].AmountOut)
	empty := *abi.ConvertType(e.call("batchQuote", []quoterBatchRequest{})[0], new([]spotkeeper.QuoteResult)).(*[]spotkeeper.QuoteResult)
	require.Empty(t, empty)
}

func TestQuoterGetAllLiquidityInfo(t *testing.T) {
	e := newQuoterEnv(t)
	got := e.call("getAllLiquidityInfo")
	require.Equal(t, []common.Address{e.usdl, e.pax, e.sid}, got[0])
	reserves := got[1].([]*big.Int)
	prices := got[2].([]*big.Int)
	require.Len(t, reserves, 3)
	require.Len(t, prices, 3)
	for i, n := range []int64{500_000e6, 1_000_000e6, 250_000e6} {
		quoterInt(t, n, reserves[i])
	}
	for i, n := range []int64{1e18, 5e17, 2e18} {
		quoterInt(t, n, prices[i])
	}
	require.Equal(t, []bool{false, false, false}, got[3])
}

func TestQuoterGetLiquidityInfo(t *testing.T) {
	e := newQuoterEnv(t)
	got := e.call("getLiquidityInfo", e.pax)
	quoterInt(t, 1_000_000e6, got[0].(*big.Int))
	quoterInt(t, 5e17, got[1].(*big.Int))
	require.Equal(t, false, got[2])
	got = e.call("getLiquidityInfo", common.HexToAddress("0xd4"))
	quoterInt(t, 0, got[0].(*big.Int))
	quoterInt(t, 0, got[1].(*big.Int))
	require.Equal(t, true, got[2])
}

func TestQuoterGetTWAP(t *testing.T) {
	e := newQuoterEnv(t)
	ctx := e.stateDB.Ctx()
	for _, sample := range []struct {
		second int64
		price  sdk.Int
	}{{700, sdk.NewIntWithDecimal(5, 17)}, {900, sdk.NewIntWithDecimal(6, 17)}} {
		at := ctx.WithBlockTime(time.Unix(sample.second, 0).UTC())
		e.writePrice(at, 11, sample.price, uint64(sample.second)*1000)
		require.NoError(t, e.k.RecordSample(at, e.pax))
	}
	e.writePrice(ctx, 11, sdk.NewIntWithDecimal(6, 17), 1_000_000)
	got := e.call("getTWAP", e.pax, big.NewInt(300))
	quoterInt(t, 533_333_333_333_333_333, got[0].(*big.Int))
	for _, period := range []*big.Int{big.NewInt(0), big.NewInt(301), new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1001)} {
		data, err := e.send("getTWAP", e.pax, period)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, common.FromHex("0xb307c2d2"), data)
	}
	e.writePrice(ctx, 11, sdk.NewIntWithDecimal(6, 17), 1)
	data, err := e.send("getTWAP", e.pax, big.NewInt(300))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, common.FromHex("0x19abf40e"), data)
}

func TestQuoterGetTokenPrice(t *testing.T) {
	e := newQuoterEnv(t)
	for _, tc := range []struct {
		token common.Address
		price int64
	}{{e.usdl, 1e18}, {e.pax, 5e17}, {e.sid, 2e18}} {
		got := e.call("getTokenPrice", tc.token)
		quoterInt(t, tc.price, got[0].(*big.Int))
		quoterInt(t, 1000, got[1].(*big.Int))
		require.Equal(t, false, got[2])
	}
}

func TestQuoterGetTokenPrices(t *testing.T) {
	e := newQuoterEnv(t)
	tokens := []common.Address{e.sid, e.usdl, e.pax, common.HexToAddress("0xd4")}
	got := e.call("getTokenPrices", tokens)
	require.Len(t, got[0], 4)
	require.Len(t, got[1], 4)
	require.Len(t, got[2], 4)
	for i, token := range tokens {
		single := e.call("getTokenPrice", token)
		require.Equal(t, single[0].(*big.Int).String(), got[0].([]*big.Int)[i].String())
		require.Equal(t, single[1].(*big.Int).String(), got[1].([]*big.Int)[i].String())
		require.Equal(t, single[2], got[2].([]bool)[i])
	}
	require.Equal(t, []bool{false, false, false, true}, got[2])
	empty := e.call("getTokenPrices", []common.Address{})
	for _, values := range empty {
		require.Empty(t, values)
	}
}

func TestQuoterPecor(t *testing.T) {
	e := newQuoterEnv(t)
	require.Equal(t, []interface{}{common.HexToAddress(types.PECORAddress)}, e.call("pecor"))
}

func TestQuoterPriceOracle(t *testing.T) {
	e := newQuoterEnv(t)
	require.Equal(t, []interface{}{common.HexToAddress("0x1020")}, e.call("priceOracle"))
}

func TestQuoterQuoteExactIn(t *testing.T) {
	e := newQuoterEnv(t)
	requireQuoterBuy(t, e.quote("quoteExactIn", e.usdl, e.pax, big.NewInt(1000e6)))
	q := e.quote("quoteExactIn", e.sid, e.pax, big.NewInt(100e6))
	quoterInt(t, 398_800_000, q.AmountOut)
}

func TestQuoterQuoteExactInNative(t *testing.T) {
	e := newQuoterEnv(t)
	q := e.quote("quoteExactInNative", e.usdl, big.NewInt(1000e6))
	quoterInt(t, 498_500_000, q.AmountOut)
	require.Equal(t, e.quote("quoteExactIn", e.pax, e.usdl, big.NewInt(1000e6)), q)
}

func TestQuoterQuoteExactInToNative(t *testing.T) {
	e := newQuoterEnv(t)
	requireQuoterBuy(t, e.quote("quoteExactInToNative", e.usdl, big.NewInt(1000e6)))
}

func TestQuoterQuoteExactOut(t *testing.T) {
	e := newQuoterEnv(t)
	q := e.quote("quoteExactOut", e.usdl, e.pax, big.NewInt(1994e6))
	quoterInt(t, 1_000e6, q.AmountIn)
	quoterInt(t, 1_994e6, q.AmountOut)
	q = e.quote("quoteExactOut", e.usdl, e.pax, big.NewInt(1))
	quoterInt(t, 1, q.AmountIn)
	quoterInt(t, 1, q.AmountOut)
}

func TestQuoterQuoteMarketBuy(t *testing.T) {
	e := newQuoterEnv(t)
	requireQuoterBuy(t, e.quote("quoteMarketBuy", e.usdl, e.pax, big.NewInt(1000e6)))
}

func TestQuoterQuoteMarketSell(t *testing.T) {
	e := newQuoterEnv(t)
	q := e.quote("quoteMarketSell", e.pax, e.usdl, big.NewInt(1000e6))
	quoterInt(t, 500e6, q.GrossAmountOut)
	quoterInt(t, 498_500_000, q.AmountOut)
	quoterInt(t, 1_500_000, q.FeeAmount)
	quoterInt(t, 30, q.FeeBps)
	require.True(t, q.SufficientLiquidity)
}

func TestQuoterSetPecor(t *testing.T) {
	e := newQuoterEnv(t)
	input, err := e.precompile.Pack("setPecor", common.HexToAddress("0xd4"))
	require.NoError(t, err)
	for _, caller := range []common.Address{e.caller, types.GovernanceEVMAddress(), common.Address{}} {
		ret, _, err := e.evm.Call(caller, common.HexToAddress(types.PECORQuoterAddress), input, 30_000_000, uint256.NewInt(0))
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, append(common.FromHex("0x118cdaa7"), common.LeftPadBytes(caller.Bytes(), 32)...), ret)
	}
	require.Equal(t, []interface{}{common.HexToAddress(types.PECORAddress)}, e.call("pecor"))
}

func TestQuoterVault(t *testing.T) {
	e := newQuoterEnv(t)
	require.Equal(t, []interface{}{common.HexToAddress(types.PECORVaultAddress)}, e.call("vault"))
}

const quoterTokenCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
