package perps_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"math"
	"math/big"
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
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	revertNowMs int64  = 1_700_000_010_000
	revertNow          = uint64(revertNowMs / 1_000)
	revertGas   uint64 = 30_000_000

	// revertUsdlSupply is the USDL minted to account A at deployment, in
	// 6-decimal units: 1,000 USDL.
	revertUsdlSupply int64 = 1_000_000_000

	// revertStaticProxyCode copies its calldata, STATICCALLs 0x1015 with it
	// and returns or reverts with the returned data.
	revertStaticProxyCode = "366000600037" + "600060003660006110155a" + "fa" + "3d600060003e" + "601f57" +
		"3d6000fd" + "5b3d6000f3"
	// revertDelegateProxyCode is revertStaticProxyCode with DELEGATECALL.
	revertDelegateProxyCode = "366000600037" + "600060003660006110155a" + "f4" + "3d600060003e" + "601f57" +
		"3d6000fd" + "5b3d6000f3"
	// revertFalseTokenCode answers transferFrom(address,address,uint256)
	// with false and every other call, balanceOf and allowance included,
	// with 2^64-1.
	revertFalseTokenCode = "600035" + "60e01c" + "6323b872dd" + "14" + "602057" + "67ffffffffffffffff" +
		"600052" + "60206000f3" + "5b" + "60206000f3"

	// revertTwoTo255 is 2^255, a sizeUsd whose fixed-point products exceed
	// 256 bits.
	revertTwoTo255 = "57896044618658097711785492504343953926634992332820282019728792003956564819968"
)

var (
	revertPerpsAddress = common.HexToAddress(perps.PerpsAddress)
	revertA            = revertAddress("revert account A")
	revertB            = revertAddress("revert account B")
	revertCloid        = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
)

// revertKeeperTested names the section 19 errors no EVM call reaches: they
// are refused in the keeper and types tasks.
var revertKeeperTested = map[string]bool{
	"PerpsInvalidParams":  true,
	"PerpsUnauthorized":   true,
	"PerpsInvalidGenesis": true,
}

// revertOrderParams is the OrderParams tuple of placeOrder as the ABI packs
// it.
type revertOrderParams struct {
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

// revertEnv is the perps precompile at 0x1015 on the application's EVM, over
// the perps keeper on the real pricefeed keeper, with USDL deployed, BTC (0,
// tick 0.1, lot 0.00001) and SOL (2, tick 0.001, lot 0.01, cap 2,000,000
// USD) listed, ETH (1) stored disabled and fresh prices of 60,000 and 150 USD.
type revertEnv struct {
	t        *testing.T
	app      *app.App
	k        *perpskeeper.Keeper
	pfKey    sdk.StoreKey
	perpsKey sdk.StoreKey
	abi      abi.ABI
	usdl     common.Address
	stateDB  *state.DBImpl
	evm      *vm.EVM
}

func revertAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func revertE18(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

func revertDec(t *testing.T, decimal string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(decimal, 10)
	require.True(t, ok, decimal)
	return value
}

func revertWord(value *big.Int) uint256.Int {
	return *uint256.MustFromBig(value)
}

func revertMarket(id uint64, symbol string, sz, px uint8, class perpstypes.TierClass, impactUsd, capUsd int64) perpstypes.Market {
	return perpstypes.Market{
		ID:                   id,
		Symbol:               symbol,
		Name:                 symbol,
		OracleSymbol:         symbol,
		SzDecimals:           sz,
		PxDecimals:           px,
		MaxLeverage:          1000,
		TierClass:            class,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(impactUsd, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(capUsd, 18),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}
}

func newRevertEnv(t *testing.T) *revertEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.UnixMilli(revertNowMs).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := perpskeeper.NewKeeper(perpsKey, &testApp.EvmKeeper, pricefeed)
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	e := &revertEnv{t: t, app: testApp, k: k, pfKey: pfKey, perpsKey: perpsKey, abi: parsed}

	e.usdl = installAcctUsdl(t, ctx, testApp, revertA, big.NewInt(revertUsdlSupply))

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), revertMarket(0, "BTC", 5, 1, perpstypes.TierClassA, 20_000, 500_000_000)))
	eth := revertMarket(1, "ETH", 4, 2, perpstypes.TierClassA, 20_000, 300_000_000)
	eth.Enabled = false
	require.NoError(t, k.SetMarket(ctx, k.Authority(), eth))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), revertMarket(2, "SOL", 2, 3, perpstypes.TierClassB, 10_000, 2_000_000)))
	e.setAggregate(ctx, 0, 60_000, revertNowMs-1_000)
	e.setAggregate(ctx, 2, 150, revertNowMs-1_000)
	_, err = k.UpdatePrices(ctx)
	require.NoError(t, err)

	precompile, err := perps.NewPrecompile(wiredKeepers{Keepers: testApp.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.stateDB = state.NewDBImpl(ctx, &testApp.EvmKeeper, false)
	blockCtx, err := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))
	contracts := map[common.Address]vm.PrecompiledContract{}
	maps.Copy(contracts, testApp.EvmKeeper.CustomPrecompiles(ctx))
	contracts[revertPerpsAddress] = precompile
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, contracts)

	btc, err := k.GetMarketState(e.ctx(), 0)
	require.NoError(t, err)
	require.Equal(t, revertWord(revertE18(60_000)), btc.MarkPrice)
	require.Equal(t, big.NewInt(revertUsdlSupply), e.balanceOf(revertA))
	return e
}

// ctx is the context the precompile runs on: every seed below writes there.
func (e *revertEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

func (e *revertEnv) setAggregate(ctx sdk.Context, marketID uint64, price int64, timestampMs int64) {
	e.t.Helper()
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(price, 18),
		MarkPrice:   sdk.NewIntWithDecimal(price, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(timestampMs),
		Height:      ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(e.t, err)
	ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(marketID), bz)
}

func (e *revertEnv) store() sdk.KVStore { return e.ctx().KVStore(e.perpsKey) }

func (e *revertEnv) updateParams(edit func(*perpstypes.Params)) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx())
	require.NoError(e.t, err)
	edit(&params)
	require.NoError(e.t, e.k.UpdateParams(e.ctx(), e.k.Authority(), params))
}

// fund stores a created account of holder with units 6-decimal USDL units of
// collateral.
func (e *revertEnv) fund(holder common.Address, units int64) {
	e.store().Set(perpstypes.AccountKey(holder),
		perpstypes.Account{Address: holder, Collateral: *uint256.NewInt(uint64(units)), Created: true}.Marshal())
}

func (e *revertEnv) account(holder common.Address) (perpstypes.Account, bool) {
	e.t.Helper()
	bz := e.store().Get(perpstypes.AccountKey(holder))
	if bz == nil {
		return perpstypes.Account{}, false
	}
	var account perpstypes.Account
	require.NoError(e.t, account.Unmarshal(bz))
	return account, true
}

// seedPair stores position id of longHolder and position id+1 of shortHolder,
// both of sizeBase in marketID entered at price whole USD and leverage, and
// adds sizeBase to both open interest sides.
func (e *revertEnv) seedPair(id uint64, longHolder, shortHolder common.Address, marketID uint64, sizeBase *big.Int,
	price, leverage int64) {
	e.t.Helper()
	store := e.store()
	size := revertWord(sizeBase)
	entry := revertWord(revertE18(price))
	var cost uint256.Int
	cost.MulDivOverflow(&size, &entry, uint256.NewInt(1_000_000_000_000_000_000))
	for i, holder := range []common.Address{longHolder, shortHolder} {
		isLong := i == 0
		position := perpstypes.Position{
			ID:             *uint256.NewInt(id + uint64(i)),
			Account:        holder,
			MarketID:       marketID,
			IsLong:         isLong,
			SizeBase:       size,
			ReferencePrice: entry,
			CostBasisUsd:   cost,
			EntryPrice:     entry,
			Leverage:       revertWord(revertE18(leverage)),
			OpenedAt:       revertNow,
		}
		store.Set(perpstypes.PositionKey(position.ID), position.Marshal())
		positionID := position.ID.Bytes32()
		store.Set(perpstypes.AccountMarketPositionKey(holder, marketID), positionID[:])
		store.Set(perpstypes.AdlRankIndexKey(marketID, perpstypes.SideOf(isLong), position.RankScore, position.ID), []byte{})
		account, ok := e.account(holder)
		require.True(e.t, ok, holder)
		account.PositionCount++
		store.Set(perpstypes.AccountKey(holder), account.Marshal())
	}
	marketState, err := e.k.GetMarketState(e.ctx(), marketID)
	require.NoError(e.t, err)
	marketState.LongOpenInterest.Add(&marketState.LongOpenInterest, &size)
	marketState.ShortOpenInterest.Add(&marketState.ShortOpenInterest, &size)
	e.k.SetMarketState(e.ctx(), marketState)
}

func (e *revertEnv) input(method string, args ...interface{}) []byte {
	e.t.Helper()
	input, err := e.abi.Pack(method, args...)
	require.NoError(e.t, err, method)
	return input
}

// call runs method from caller against 0x1015 through the EVM.
func (e *revertEnv) call(caller common.Address, method string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	return e.raw(caller, revertPerpsAddress, e.input(method, args...))
}

func (e *revertEnv) raw(caller, to common.Address, input []byte) ([]byte, error) {
	e.t.Helper()
	ret, _, err := e.evm.Call(caller, to, input, revertGas, uint256.NewInt(0))
	return ret, err
}

// ok runs method from caller and requires it to succeed.
func (e *revertEnv) ok(caller common.Address, method string, args ...interface{}) []byte {
	e.t.Helper()
	ret, err := e.call(caller, method, args...)
	require.NoError(e.t, err, "%s reverted with %x", method, ret)
	return ret
}

// place places order for caller and returns the order id.
func (e *revertEnv) place(caller common.Address, order revertOrderParams) *big.Int {
	e.t.Helper()
	out, err := e.abi.Unpack("placeOrder", e.ok(caller, "placeOrder", order))
	require.NoError(e.t, err)
	require.Len(e.t, out, 1)
	id, ok := out[0].(*big.Int)
	require.True(e.t, ok)
	require.Positive(e.t, id.Sign())
	return id
}

// requireRevert requires a reverted call whose whole revert data is the
// selector of the parameterless custom error name of the perps ABI.
func (e *revertEnv) requireRevert(name string, ret []byte, err error) {
	e.t.Helper()
	requireRevertData(e.t, e.abi, name, ret, err)
}

func requireRevertData(t *testing.T, parsed abi.ABI, name string, ret []byte, err error) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	custom, ok := parsed.Errors[name]
	require.True(t, ok, name)
	require.Equal(t, custom.ID[:4], ret, "want %s, got %x", name, ret)
	var decoded *abi.Error
	for _, candidate := range parsed.Errors {
		if string(candidate.ID[:4]) == string(ret[:4]) {
			c := candidate
			decoded = &c
		}
	}
	require.NotNil(t, decoded, "%x decodes to no custom error", ret)
	require.Equal(t, name, decoded.Name)
	args, err := decoded.Unpack(ret)
	require.NoError(t, err)
	require.Empty(t, args)
}

func (e *revertEnv) tokenWord(holder common.Address, signature string, words ...[]byte) *big.Int {
	e.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, word := range words {
		input = append(input, word...)
	}
	ret, err := e.raw(holder, e.usdl, input)
	require.NoError(e.t, err, signature)
	require.Len(e.t, ret, 32)
	return new(big.Int).SetBytes(ret)
}

func revertAddressWord(address common.Address) []byte {
	return common.LeftPadBytes(address.Bytes(), 32)
}

func revertAmountWord(amount int64) []byte {
	return common.LeftPadBytes(big.NewInt(amount).Bytes(), 32)
}

func (e *revertEnv) balanceOf(holder common.Address) *big.Int {
	return e.tokenWord(holder, "balanceOf(address)", revertAddressWord(holder))
}

func (e *revertEnv) allowance(owner common.Address) *big.Int {
	return e.tokenWord(owner, "allowance(address,address)", revertAddressWord(owner), revertAddressWord(revertPerpsAddress))
}

func (e *revertEnv) approve(owner common.Address, amount int64) {
	e.t.Helper()
	require.Equal(e.t, big.NewInt(1), e.tokenWord(owner, "approve(address,uint256)", revertAddressWord(revertPerpsAddress), revertAmountWord(amount)))
}

func (e *revertEnv) transfer(from, to common.Address, amount int64) {
	e.t.Helper()
	require.Equal(e.t, big.NewInt(1), e.tokenWord(from, "transfer(address,uint256)", revertAddressWord(to), revertAmountWord(amount)))
}

func (e *revertEnv) deploy(name, code string) common.Address {
	address := revertAddress(name)
	e.stateDB.SetCode(address, common.FromHex(code))
	return address
}

func revertZero() *big.Int { return new(big.Int) }

// revertLimit is a GTC limit order at 20x of sizeUsd at price, both 18
// decimals.
func revertLimit(marketID int64, isLong bool, sizeUsd, price *big.Int) revertOrderParams {
	return revertOrderParams{
		MarketId:         big.NewInt(marketID),
		IsLong:           isLong,
		OrderType:        uint8(perpstypes.OrderTypeLimit),
		TimeInForce:      uint8(perpstypes.TimeInForceGTC),
		SizeUsd:          sizeUsd,
		Leverage:         revertE18(20),
		LimitPrice:       price,
		TriggerPrice:     revertZero(),
		AcceptablePrice:  revertZero(),
		CollateralAmount: revertZero(),
		TakeProfitPrice:  revertZero(),
		StopLossPrice:    revertZero(),
	}
}

// revertMarketOrder is an IOC MARKET order at 20x of sizeUsd bounded by
// acceptable, both 18 decimals.
func revertMarketOrder(marketID int64, isLong bool, sizeUsd, acceptable *big.Int) revertOrderParams {
	order := revertLimit(marketID, isLong, sizeUsd, revertZero())
	order.OrderType = uint8(perpstypes.OrderTypeMarket)
	order.TimeInForce = uint8(perpstypes.TimeInForceIOC)
	order.AcceptablePrice = acceptable
	return order
}

func (e *revertEnv) order(id *big.Int) perpstypes.Order {
	e.t.Helper()
	order, err := e.k.GetOrder(e.ctx(), revertWord(id))
	require.NoError(e.t, err)
	return order
}

func (e *revertEnv) bestAsk(marketID uint64) uint256.Int {
	e.t.Helper()
	ask, ok, err := e.k.BestAsk(e.ctx(), marketID)
	require.NoError(e.t, err)
	require.True(e.t, ok)
	return ask
}

func TestRevertUnknownMarket(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(18, true, revertE18(600), revertE18(59_000)))
	e.requireRevert("PerpsUnknownMarket", ret, err)
}

func TestRevertMarketDisabled(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(1, true, revertE18(600), revertE18(3_000)))
	e.requireRevert("PerpsMarketDisabled", ret, err)
}

func TestRevertPriceStale(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	// 16 seconds old is beyond max_price_age_ms of 15,000.
	e.setAggregate(e.ctx(), 2, 150, revertNowMs-16_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(2, true, revertE18(100), revertE18(140)))
	e.requireRevert("PerpsPriceStale", ret, err)
}

func TestRevertZeroAmount(t *testing.T) {
	e := newRevertEnv(t)
	e.approve(revertA, 1_000_000)
	ret, err := e.call(revertA, "deposit", e.usdl, revertZero())
	e.requireRevert("PerpsZeroAmount", ret, err)
	_, exists := e.account(revertA)
	require.False(t, exists)
}

func TestRevertUnsupportedCollateral(t *testing.T) {
	e := newRevertEnv(t)
	usdc := common.HexToAddress("0x4b29871681c95DFB2c7824BC4b0326B80217bCe8")
	ret, err := e.call(revertA, "deposit", usdc, big.NewInt(1))
	e.requireRevert("PerpsUnsupportedCollateral", ret, err)
	ret, err = e.call(revertA, "deposit", common.Address{}, big.NewInt(1))
	e.requireRevert("PerpsUnsupportedCollateral", ret, err)
}

func TestRevertInsufficientCollateral(t *testing.T) {
	e := newRevertEnv(t)
	shortBalance := revertAddress("revert holder of 10 units")
	e.transfer(revertA, shortBalance, 10)
	e.approve(shortBalance, 1_000_000)
	ret, err := e.call(shortBalance, "deposit", e.usdl, big.NewInt(11))
	e.requireRevert("PerpsInsufficientCollateral", ret, err)
	require.Equal(t, big.NewInt(10), e.balanceOf(shortBalance))
	require.Equal(t, big.NewInt(1_000_000), e.allowance(shortBalance))
	_, exists := e.account(shortBalance)
	require.False(t, exists)

	shortAllowance := revertAddress("revert holder approving 10 units")
	e.transfer(revertA, shortAllowance, 1_000_000)
	e.approve(shortAllowance, 10)
	ret, err = e.call(shortAllowance, "deposit", e.usdl, big.NewInt(11))
	e.requireRevert("PerpsInsufficientCollateral", ret, err)
	require.Equal(t, big.NewInt(1_000_000), e.balanceOf(shortAllowance))
	require.Equal(t, big.NewInt(10), e.allowance(shortAllowance))
	_, exists = e.account(shortAllowance)
	require.False(t, exists)
}

func TestRevertInsufficientMargin(t *testing.T) {
	e := newRevertEnv(t)
	// 100 USD of equity against 2,400 USD at 20x: 120 USD of initial margin.
	e.fund(revertA, 100_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertE18(2_400), revertE18(59_000)))
	e.requireRevert("PerpsInsufficientMargin", ret, err)
}

func TestRevertLeverageTooHigh(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	order := revertLimit(0, true, revertE18(600), revertE18(59_000))
	order.Leverage = revertE18(1_001)
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsLeverageTooHigh", ret, err)
}

func TestRevertTickSize(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertE18(600), revertDec(t, "59000050000000000000000")))
	e.requireRevert("PerpsTickSize", ret, err)
}

func TestRevertLotSize(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertDec(t, "500000000000000000"), revertE18(60_000)))
	e.requireRevert("PerpsLotSize", ret, err)
}

func TestRevertMinOrderValue(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertE18(9), revertE18(59_000)))
	e.requireRevert("PerpsMinOrderValue", ret, err)
}

func TestRevertMaxOrderValue(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 16_000_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertMarketOrder(0, true, revertE18(30_000_001), revertE18(60_600)))
	e.requireRevert("PerpsMaxOrderValue", ret, err)
}

func TestRevertOpenInterestCap(t *testing.T) {
	e := newRevertEnv(t)
	// 13,320 SOL a side at 150 USD is 1,998,000 USD of the 2,000,000 cap.
	sol, err := e.k.GetMarketState(e.ctx(), 2)
	require.NoError(t, err)
	sol.LongOpenInterest = revertWord(revertE18(13_320))
	sol.ShortOpenInterest = revertWord(revertE18(13_320))
	e.k.SetMarketState(e.ctx(), sol)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.place(revertB, revertLimit(2, false, revertE18(3_000), revertE18(150)))
	ret, err := e.call(revertA, "placeOrder", revertMarketOrder(2, true, revertE18(3_000), revertE18(151)))
	e.requireRevert("PerpsOpenInterestCap", ret, err)
	after, err := e.k.GetMarketState(e.ctx(), 2)
	require.NoError(t, err)
	require.Equal(t, revertWord(revertE18(13_320)), after.LongOpenInterest)
}

func TestRevertInvalidTimeInForce(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	order := revertLimit(0, true, revertE18(600), revertE18(59_000))
	order.TimeInForce = 4
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsInvalidTimeInForce", ret, err)
	market := revertMarketOrder(0, true, revertE18(600), revertE18(60_600))
	market.TimeInForce = uint8(perpstypes.TimeInForceGTC)
	ret, err = e.call(revertA, "placeOrder", market)
	e.requireRevert("PerpsInvalidTimeInForce", ret, err)
}

func TestRevertInvalidOrderType(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	order := revertLimit(0, true, revertE18(600), revertE18(59_000))
	order.OrderType = 6
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsInvalidOrderType", ret, err)
}

func TestRevertPostOnlyWouldCross(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.place(revertB, revertLimit(0, false, revertE18(600), revertE18(60_000)))
	order := revertLimit(0, true, revertE18(600), revertE18(60_000))
	order.TimeInForce = uint8(perpstypes.TimeInForcePostOnly)
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsPostOnlyWouldCross", ret, err)
	require.Equal(t, revertWord(revertE18(60_000)), e.bestAsk(0))
}

func TestRevertFokNotFilled(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	// 0.01 BTC on the ask at 60,000 against an FOK buy of 0.02 BTC.
	ask := e.place(revertB, revertLimit(0, false, revertE18(600), revertE18(60_000)))
	before := e.order(ask)
	order := revertLimit(0, true, revertE18(1_200), revertE18(60_000))
	order.TimeInForce = uint8(perpstypes.TimeInForceFOK)
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsFokNotFilled", ret, err)
	require.Equal(t, before, e.order(ask))
	require.Equal(t, perpstypes.OrderStatusResting, e.order(ask).Status)
	require.Equal(t, revertWord(revertE18(60_000)), e.bestAsk(0))
}

func TestRevertNoLiquidity(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertMarketOrder(0, true, revertE18(1_000), revertE18(60_600)))
	e.requireRevert("PerpsNoLiquidity", ret, err)
}

func TestRevertSlippageExceeded(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	ask := e.place(revertB, revertLimit(0, false, revertE18(1_210), revertE18(60_500)))
	before := e.order(ask)
	ret, err := e.call(revertA, "placeOrder", revertMarketOrder(0, true, revertE18(1_200), revertE18(60_100)))
	e.requireRevert("PerpsSlippageExceeded", ret, err)
	require.Equal(t, before, e.order(ask))
}

func TestRevertDeadlineExpired(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	order := revertLimit(0, true, revertE18(600), revertE18(59_000))
	order.Deadline = revertNow - 1
	ret, err := e.call(revertA, "placeOrder", order)
	e.requireRevert("PerpsDeadlineExpired", ret, err)
}

func TestRevertReduceOnlyViolation(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	// A long 0.01 BTC.
	e.seedPair(7, revertA, revertB, 0, revertDec(t, "10000000000000000"), 60_000, 20)
	sell := revertLimit(0, false, revertE18(1_200), revertE18(60_000))
	sell.ReduceOnly = true
	ret, err := e.call(revertA, "placeOrder", sell)
	e.requireRevert("PerpsReduceOnlyViolation", ret, err)
	buy := revertLimit(0, true, revertE18(600), revertE18(59_000))
	buy.ReduceOnly = true
	ret, err = e.call(revertA, "placeOrder", buy)
	e.requireRevert("PerpsReduceOnlyViolation", ret, err)
}

func TestRevertDuplicateCloid(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	first := revertLimit(0, true, revertE18(600), revertE18(59_000))
	first.Cloid = revertCloid
	e.place(revertA, first)
	second := revertLimit(0, true, revertE18(600), revertE18(58_000))
	second.Cloid = revertCloid
	ret, err := e.call(revertA, "placeOrder", second)
	e.requireRevert("PerpsDuplicateCloid", ret, err)
}

func TestRevertUnknownOrder(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "cancelOrder", big.NewInt(77))
	e.requireRevert("PerpsUnknownOrder", ret, err)
}

func TestRevertNotOrderHolder(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	id := e.place(revertA, revertLimit(0, true, revertE18(600), revertE18(59_000)))
	ret, err := e.call(revertB, "cancelOrder", id)
	e.requireRevert("PerpsNotOrderHolder", ret, err)
	require.Equal(t, perpstypes.OrderStatusResting, e.order(id).Status)
}

func TestRevertUnknownPosition(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "closePosition", big.NewInt(99))
	e.requireRevert("PerpsUnknownPosition", ret, err)
}

func TestRevertNotPositionHolder(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertDec(t, "10000000000000000"), 60_000, 20)
	ret, err := e.call(revertB, "closePosition", big.NewInt(7))
	e.requireRevert("PerpsNotPositionHolder", ret, err)
}

func TestRevertInvalidTriggerPrice(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertDec(t, "10000000000000000"), 60_000, 20)
	// A long take-profit sits above the mark of 60,000.
	ret, err := e.call(revertA, "placeTakeProfitOrder", big.NewInt(7), revertE18(59_000))
	e.requireRevert("PerpsInvalidTriggerPrice", ret, err)
}

func TestRevertTriggerNotReached(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	order := revertMarketOrder(0, true, revertE18(1_200), revertE18(61_600))
	order.OrderType = uint8(perpstypes.OrderTypeStopMarket)
	order.TriggerPrice = revertE18(61_000)
	id := e.place(revertA, order)
	require.Equal(t, perpstypes.OrderStatusUntriggered, e.order(id).Status)
	ret, err := e.call(revertA, "executeOrder", id)
	e.requireRevert("PerpsTriggerNotReached", ret, err)
	require.Equal(t, perpstypes.OrderStatusUntriggered, e.order(id).Status)
}

func TestRevertTooManyOpenOrders(t *testing.T) {
	e := newRevertEnv(t)
	e.updateParams(func(params *perpstypes.Params) { params.MaxOpenOrdersPerAccount = 1 })
	e.fund(revertA, 1_000_000_000)
	e.place(revertA, revertLimit(0, true, revertE18(600), revertE18(59_000)))
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertE18(600), revertE18(58_000)))
	e.requireRevert("PerpsTooManyOpenOrders", ret, err)
}

func TestRevertCrossMarginOnly(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.ok(revertA, "setMarginMode", uint8(0))
	ret, err := e.call(revertA, "setMarginMode", uint8(1))
	e.requireRevert("PerpsCrossMarginOnly", ret, err)
}

func TestRevertNotLiquidatable(t *testing.T) {
	e := newRevertEnv(t)
	// 1 BTC long at mark 60,000: maintenance 30 USD against 31 USD of equity.
	e.fund(revertA, 31_000_000)
	e.fund(revertB, 1_000_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertE18(1), 60_000, 1_000)
	ret, err := e.call(revertB, "liquidate", big.NewInt(7))
	e.requireRevert("PerpsNotLiquidatable", ret, err)
	account, ok := e.account(revertA)
	require.True(t, ok)
	require.Equal(t, *uint256.NewInt(31_000_000), account.Collateral)
}

func TestRevertLiquidationCooldown(t *testing.T) {
	e := newRevertEnv(t)
	// 15 BTC long at mark 60,000 is 900,000 USD, above the 100,000 USD slice
	// threshold: maintenance 450 USD against 400 USD of equity.
	e.fund(revertA, 400_000_000)
	e.fund(revertB, 1_000_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertE18(15), 60_000, 1_000)
	e.store().Set(perpstypes.LiquidationCooldownKey(*uint256.NewInt(7)),
		perpstypes.LiquidationCooldownValue(revertNow-10, revertWord(revertE18(15))))
	ret, err := e.call(revertB, "liquidate", big.NewInt(7))
	e.requireRevert("PerpsLiquidationCooldown", ret, err)
	account, ok := e.account(revertA)
	require.True(t, ok)
	require.Equal(t, *uint256.NewInt(400_000_000), account.Collateral)
}

func TestRevertStaticCall(t *testing.T) {
	e := newRevertEnv(t)
	proxy := e.deploy("revert static proxy", revertStaticProxyCode)
	ret, err := e.raw(revertA, proxy, e.input("deposit", e.usdl, big.NewInt(1_000_000)))
	e.requireRevert("PerpsStaticCall", ret, err)
}

func TestRevertDelegatecall(t *testing.T) {
	e := newRevertEnv(t)
	proxy := e.deploy("revert delegate proxy", revertDelegateProxyCode)
	ret, err := e.raw(revertA, proxy, e.input("getMarket", big.NewInt(0)))
	e.requireRevert("PerpsDelegatecall", ret, err)
}

func TestRevertOverflow(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "placeOrder", revertLimit(0, true, revertDec(t, revertTwoTo255), revertE18(59_000)))
	e.requireRevert("PerpsOverflow", ret, err)
}

func TestRevertCloseSizeExceedsPosition(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertDec(t, "10000000000000000"), 60_000, 20)
	ret, err := e.call(revertA, "partialClose", big.NewInt(7), revertE18(2_000))
	e.requireRevert("PerpsCloseSizeExceedsPosition", ret, err)
}

func TestRevertSelfTransfer(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "transferCollateral", revertA, big.NewInt(1))
	e.requireRevert("PerpsSelfTransfer", ret, err)
}

func TestRevertZeroAddress(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "transferCollateral", common.Address{}, big.NewInt(1))
	e.requireRevert("PerpsZeroAddress", ret, err)
}

func TestRevertNoAccount(t *testing.T) {
	e := newRevertEnv(t)
	ret, err := e.call(revertB, "withdraw", e.usdl, big.NewInt(1))
	e.requireRevert("PerpsNoAccount", ret, err)
	_, exists := e.account(revertB)
	require.False(t, exists)
}

func TestRevertTokenTransferFailed(t *testing.T) {
	e := newRevertEnv(t)
	falseToken := e.deploy("revert false token", revertFalseTokenCode)
	e.updateParams(func(params *perpstypes.Params) { params.CollateralToken = falseToken })
	e.fund(revertA, 5_000_000)
	before := e.store().Get(perpstypes.AccountKey(revertA))
	ret, err := e.call(revertA, "deposit", falseToken, big.NewInt(1_000_000))
	e.requireRevert("PerpsTokenTransferFailed", ret, err)
	require.Equal(t, before, e.store().Get(perpstypes.AccountKey(revertA)))
	require.Empty(t, e.stateDB.GetAllLogs())
}

func TestRevertPaused(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	e.fund(revertB, 1_000_000_000)
	e.seedPair(7, revertA, revertB, 0, revertDec(t, "10000000000000000"), 60_000, 20)
	// B's bid takes A's close while the venue is paused.
	e.place(revertB, revertLimit(0, true, revertE18(600), revertE18(60_000)))
	e.updateParams(func(params *perpstypes.Params) { params.Paused = true })

	ret, err := e.call(revertA, "openPosition", big.NewInt(0), e.usdl, big.NewInt(100_000_000), revertE18(10), true)
	e.requireRevert("PerpsPaused", ret, err)

	e.ok(revertA, "closePosition", big.NewInt(7))
	require.Nil(t, e.store().Get(perpstypes.PositionKey(*uint256.NewInt(7))))

	before, ok := e.account(revertA)
	require.True(t, ok)
	e.approve(revertA, 1_000_000)
	e.ok(revertA, "deposit", e.usdl, big.NewInt(1_000_000))
	after, ok := e.account(revertA)
	require.True(t, ok)
	var want uint256.Int
	want.Add(&before.Collateral, uint256.NewInt(1_000_000))
	require.Equal(t, want, after.Collateral)
	require.Equal(t, big.NewInt(revertUsdlSupply-1_000_000), e.balanceOf(revertA))
}

// TestRevertCustomErrorData shows the revert data of a wrapped keeper error is
// the bare custom error selector, not an Error(string) carrying its text, and
// that a call Prepare refuses reverts with no data.
func TestRevertCustomErrorData(t *testing.T) {
	e := newRevertEnv(t)
	e.fund(revertA, 1_000_000_000)
	ret, err := e.call(revertA, "cancelOrder", big.NewInt(77))
	e.requireRevert("PerpsUnknownOrder", ret, err)
	require.Len(t, ret, 4)
	_, unpackErr := abi.UnpackRevert(ret)
	require.Error(t, unpackErr)
	require.NotContains(t, string(ret), perpstypes.ErrUnknownOrder.Error())

	// GetMarket wraps ErrUnknownMarket with the market id.
	_, wrapErr := e.k.GetMarket(e.ctx(), 99)
	require.ErrorIs(t, wrapErr, perpstypes.ErrUnknownMarket)
	require.NotEqual(t, perpstypes.ErrUnknownMarket.Error(), wrapErr.Error())
	ret, err = e.call(revertA, "getMarket", big.NewInt(99))
	e.requireRevert("PerpsUnknownMarket", ret, err)
	require.Equal(t, common.FromHex("0x65644af8"), ret)

	// An internal error without an ABI error reverts as Error(string).
	e.stateDB.AddBalance(revertA, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	ret, _, err = e.evm.Call(revertA, revertPerpsAddress, e.input("getMarket", big.NewInt(0)), revertGas,
		uint256.NewInt(1_000_000_000_000))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, pcommon.RevertReason(pcommon.ValidateNonPayable(big.NewInt(1))), ret)
	reason, unpackErr := abi.UnpackRevert(ret)
	require.NoError(t, unpackErr)
	require.Equal(t, "sending funds to a non-payable function", reason)

	// An unknown selector fails in Prepare.
	ret, err = e.raw(revertA, revertPerpsAddress, []byte{0xde, 0xad, 0xbe, 0xef})
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Empty(t, ret)

	// Arguments that do not unpack fail in Prepare.
	cancel := e.abi.Methods["cancelOrder"]
	ret, err = e.raw(revertA, revertPerpsAddress, append(append([]byte(nil), cancel.ID...), 0x01, 0x02, 0x03))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Empty(t, ret)
}

// TestRevertCoverage pairs every keeper error of modules/perps/types/errors.go
// with the TestRevert of its custom error in this file, or with the keeper and
// types tasks for the three errors no EVM call reaches.
func TestRevertCoverage(t *testing.T) {
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	fset := token.NewFileSet()
	errorsFile, err := parser.ParseFile(fset, "../../modules/perps/types/errors.go", nil, 0)
	require.NoError(t, err)
	testFile, err := parser.ParseFile(fset, "errors_test.go", nil, 0)
	require.NoError(t, err)

	tests := map[string]bool{}
	for _, decl := range testFile.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestRevert") {
			tests[fn.Name.Name] = true
		}
	}

	covered := map[string]string{}
	wantTests := map[string]bool{"TestRevertCustomErrorData": true, "TestRevertCoverage": true}
	for _, decl := range errorsFile.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			require.True(t, ok)
			for _, ident := range value.Names {
				require.True(t, strings.HasPrefix(ident.Name, "Err"), ident.Name)
				name := "Perps" + strings.TrimPrefix(ident.Name, "Err")
				_, inABI := parsed.Errors[name]
				require.True(t, inABI, "%s has no custom error %s", ident.Name, name)
				if revertKeeperTested[name] {
					covered[name] = "keeper"
					continue
				}
				test := "TestRevert" + strings.TrimPrefix(ident.Name, "Err")
				require.True(t, tests[test], "%s has no %s", ident.Name, test)
				covered[name] = test
				wantTests[test] = true
			}
		}
	}
	require.Len(t, covered, 44)
	require.Len(t, parsed.Errors, 44)
	require.Equal(t, wantTests, tests)
	require.Len(t, tests, 43)
	require.Equal(t, "TestRevertTokenTransferFailed", covered["PerpsTokenTransferFailed"])
	require.Equal(t, "TestRevertPaused", covered["PerpsPaused"])
}

// TestErrorsSelectorsFrozen pins the code, LXP text and selector of every
// custom error of contracts/perps.md section 19.
func TestErrorsSelectorsFrozen(t *testing.T) {
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	cases := []struct {
		err      error
		name     string
		code     uint32
		text     string
		selector string
	}{
		{perpstypes.ErrInvalidParams, "PerpsInvalidParams", 2, "INVALID_PARAMS", "c7088a37"},
		{perpstypes.ErrUnknownMarket, "PerpsUnknownMarket", 3, "UNKNOWN_MARKET", "65644af8"},
		{perpstypes.ErrMarketDisabled, "PerpsMarketDisabled", 4, "MARKET_DISABLED", "386d4503"},
		{perpstypes.ErrPriceStale, "PerpsPriceStale", 5, "PRICE_STALE", "737dedba"},
		{perpstypes.ErrZeroAmount, "PerpsZeroAmount", 6, "ZERO_AMOUNT", "811bd7a1"},
		{perpstypes.ErrUnsupportedCollateral, "PerpsUnsupportedCollateral", 7, "UNSUPPORTED_COLLATERAL", "0a8b8f76"},
		{perpstypes.ErrInsufficientCollateral, "PerpsInsufficientCollateral", 8, "INSUFFICIENT_COLLATERAL", "61997bc4"},
		{perpstypes.ErrInsufficientMargin, "PerpsInsufficientMargin", 9, "INSUFFICIENT_MARGIN", "12b52d40"},
		{perpstypes.ErrLeverageTooHigh, "PerpsLeverageTooHigh", 10, "LEVERAGE_TOO_HIGH", "794b54cf"},
		{perpstypes.ErrTickSize, "PerpsTickSize", 11, "TICK_SIZE", "73f775c1"},
		{perpstypes.ErrLotSize, "PerpsLotSize", 12, "LOT_SIZE", "8e28a39a"},
		{perpstypes.ErrMinOrderValue, "PerpsMinOrderValue", 13, "MIN_ORDER_VALUE", "d1d78b4f"},
		{perpstypes.ErrMaxOrderValue, "PerpsMaxOrderValue", 14, "MAX_ORDER_VALUE", "e9933ae1"},
		{perpstypes.ErrOpenInterestCap, "PerpsOpenInterestCap", 15, "OPEN_INTEREST_CAP", "021901d6"},
		{perpstypes.ErrInvalidTimeInForce, "PerpsInvalidTimeInForce", 16, "INVALID_TIME_IN_FORCE", "47306489"},
		{perpstypes.ErrInvalidOrderType, "PerpsInvalidOrderType", 17, "INVALID_ORDER_TYPE", "7be0d47b"},
		{perpstypes.ErrPostOnlyWouldCross, "PerpsPostOnlyWouldCross", 18, "POST_ONLY_WOULD_CROSS", "096e07cd"},
		{perpstypes.ErrFokNotFilled, "PerpsFokNotFilled", 19, "FOK_NOT_FILLED", "c3dbe522"},
		{perpstypes.ErrNoLiquidity, "PerpsNoLiquidity", 20, "NO_LIQUIDITY", "ef4eb50b"},
		{perpstypes.ErrSlippageExceeded, "PerpsSlippageExceeded", 21, "SLIPPAGE_EXCEEDED", "b6c82773"},
		{perpstypes.ErrDeadlineExpired, "PerpsDeadlineExpired", 22, "DEADLINE_EXPIRED", "d28988d8"},
		{perpstypes.ErrReduceOnlyViolation, "PerpsReduceOnlyViolation", 23, "REDUCE_ONLY_VIOLATION", "8adbf151"},
		{perpstypes.ErrDuplicateCloid, "PerpsDuplicateCloid", 24, "DUPLICATE_CLOID", "6298d438"},
		{perpstypes.ErrUnknownOrder, "PerpsUnknownOrder", 25, "UNKNOWN_ORDER", "1930eff6"},
		{perpstypes.ErrNotOrderHolder, "PerpsNotOrderHolder", 26, "NOT_ORDER_HOLDER", "edef32ca"},
		{perpstypes.ErrUnknownPosition, "PerpsUnknownPosition", 27, "UNKNOWN_POSITION", "6dcd3d95"},
		{perpstypes.ErrNotPositionHolder, "PerpsNotPositionHolder", 28, "NOT_POSITION_HOLDER", "e59be79c"},
		{perpstypes.ErrInvalidTriggerPrice, "PerpsInvalidTriggerPrice", 29, "INVALID_TRIGGER_PRICE", "d7580015"},
		{perpstypes.ErrTriggerNotReached, "PerpsTriggerNotReached", 30, "TRIGGER_NOT_REACHED", "c6298dab"},
		{perpstypes.ErrTooManyOpenOrders, "PerpsTooManyOpenOrders", 31, "TOO_MANY_OPEN_ORDERS", "fc1f0263"},
		{perpstypes.ErrCrossMarginOnly, "PerpsCrossMarginOnly", 32, "CROSS_MARGIN_ONLY", "6ff920cc"},
		{perpstypes.ErrNotLiquidatable, "PerpsNotLiquidatable", 33, "NOT_LIQUIDATABLE", "ce578943"},
		{perpstypes.ErrLiquidationCooldown, "PerpsLiquidationCooldown", 34, "LIQUIDATION_COOLDOWN", "6098ef55"},
		{perpstypes.ErrUnauthorized, "PerpsUnauthorized", 35, "UNAUTHORIZED", "f73dc939"},
		{perpstypes.ErrStaticCall, "PerpsStaticCall", 36, "STATIC_CALL", "32a35088"},
		{perpstypes.ErrDelegatecall, "PerpsDelegatecall", 37, "DELEGATECALL", "856fe7e1"},
		{perpstypes.ErrOverflow, "PerpsOverflow", 38, "OVERFLOW", "927150ed"},
		{perpstypes.ErrInvalidGenesis, "PerpsInvalidGenesis", 39, "INVALID_GENESIS", "d06690f2"},
		{perpstypes.ErrCloseSizeExceedsPosition, "PerpsCloseSizeExceedsPosition", 40, "CLOSE_SIZE_EXCEEDS_POSITION", "1be787db"},
		{perpstypes.ErrSelfTransfer, "PerpsSelfTransfer", 41, "SELF_TRANSFER", "98bc8e6a"},
		{perpstypes.ErrZeroAddress, "PerpsZeroAddress", 42, "ZERO_ADDRESS", "d6d30277"},
		{perpstypes.ErrNoAccount, "PerpsNoAccount", 43, "NO_ACCOUNT", "d7b16785"},
		{perpstypes.ErrTokenTransferFailed, "PerpsTokenTransferFailed", 44, "TOKEN_TRANSFER_FAILED", "6fdbf101"},
		{perpstypes.ErrPaused, "PerpsPaused", 45, "PAUSED", "e0753633"},
	}
	require.Len(t, cases, 44)
	require.Len(t, parsed.Errors, 44)
	for i, tc := range cases {
		custom, ok := parsed.Errors[tc.name]
		require.True(t, ok, tc.name)
		require.Empty(t, custom.Inputs, tc.name)
		require.Equal(t, tc.selector, common.Bytes2Hex(custom.ID[:4]), tc.name)
		require.Equal(t, tc.selector, common.Bytes2Hex(crypto.Keccak256([]byte(tc.name + "()"))[:4]), tc.name)
		coded, ok := tc.err.(interface{ ABCICode() uint32 })
		require.True(t, ok, tc.name)
		require.Equal(t, uint32(i+2), coded.ABCICode(), tc.name)
		require.Equal(t, tc.code, coded.ABCICode(), tc.name)
		require.Equal(t, "LXP_PERPS_"+tc.text, tc.err.Error(), tc.name)
	}
}
