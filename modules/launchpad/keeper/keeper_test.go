package keeper_test

import (
	"bytes"
	"errors"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	moduleimpl "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"encoding/json"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	genesisTime = time.Unix(1_800_000_000, 0).UTC()
	farDeadline = uint64(genesisTime.Add(10_000 * time.Hour).Unix())
	mature      = 1000 * time.Hour
	// reserve holds the USDL supply newEnv mints; account hands it out.
	reserve = address("quote reserve")
)

// env is the launchpad keeper on the real bank, tokenfactory and EVM keepers
// of the test application, on a branch of its state, with a USDL ERC20
// deployed in its EVM and named by Params.QuoteToken.
type env struct {
	t    *testing.T
	ctx  sdk.Context
	k    *keeper.Keeper
	key  sdk.StoreKey
	usdl common.Address
}

func newEnv(t *testing.T) *env {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime).WithBlockHeight(1)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	ctx, _ = ctx.CacheContext()
	k := app.LaunchpadKeeper
	k.InitGenesis(ctx, *types.DefaultGenesis())

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(usdlSupply), reserve)
	require.NoError(t, err)
	deployer := address("quote deployer")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(t, err)
	usdl := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, usdl))
	params := k.GetParams(ctx)
	params.QuoteToken = usdl.Hex()
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	return &env{t: t, ctx: ctx, k: k, key: app.GetKey(types.StoreKey), usdl: usdl}
}

func calldata(signature string, words ...[]byte) []byte {
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	return input
}

func addressWord(a common.Address) []byte { return common.LeftPadBytes(a.Bytes(), 32) }

// callToken calls a state-changing ERC20 method from from and requires it to
// return true.
func (e *env) callToken(token, from common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, from, &token, nil, calldata(signature, words...))
	require.NoError(e.t, err)
	require.Equal(e.t, word(big.NewInt(1)), ret)
}

func (e *env) view(token common.Address, signature string, words ...[]byte) []byte {
	e.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(e.ctx, e.k.ModuleAddress(), &token, calldata(signature, words...))
	require.NoError(e.t, err)
	return ret
}

func (e *env) viewInt(token common.Address, signature string, words ...[]byte) sdk.Int {
	e.t.Helper()
	ret := e.view(token, signature, words...)
	require.Len(e.t, ret, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret))
}

func (e *env) sendToken(token, from, to common.Address, amount *big.Int) {
	e.t.Helper()
	e.callToken(token, from, "transfer(address,uint256)", addressWord(to), word(amount))
}

func (e *env) tokenBalance(token, holder common.Address) sdk.Int {
	return e.viewInt(token, "balanceOf(address)", addressWord(holder))
}

func (e *env) tokenSupply(token common.Address) sdk.Int { return e.viewInt(token, "totalSupply()") }

func (e *env) allowanceOf(token, owner, spender common.Address) sdk.Int {
	return e.viewInt(token, "allowance(address,address)", addressWord(owner), addressWord(spender))
}

func (e *env) evmAddress(acc sdk.AccAddress) common.Address {
	return testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(e.ctx, acc)
}

// approveQuote sets the USDL allowance of acc to the launchpad precompile,
// the spender PullQuote's transferFrom spends.
func (e *env) approveQuote(acc sdk.AccAddress, amount *big.Int) {
	e.t.Helper()
	e.callToken(e.usdl, e.evmAddress(acc), "approve(address,uint256)",
		addressWord(common.HexToAddress(types.LaunchpadAddress)), word(amount))
}

// account is a fresh account holding quoteAmount USDL from the reserve and an
// unlimited USDL allowance to the launchpad.
func (e *env) account(quoteAmount int64) sdk.AccAddress {
	e.t.Helper()
	acc, _ := testkeeper.MockAddressPair()
	if quoteAmount > 0 {
		e.sendToken(e.usdl, reserve, e.evmAddress(acc), big.NewInt(quoteAmount))
	}
	e.approveQuote(acc, types.MaxUint256())
	return acc
}

func (e *env) usdlOf(acc sdk.AccAddress) sdk.Int { return e.tokenBalance(e.usdl, e.evmAddress(acc)) }

// custodyUsdl is the USDL held at the launchpad module EVM address.
func (e *env) custodyUsdl() sdk.Int { return e.tokenBalance(e.usdl, e.k.ModuleEVMAddress(e.ctx)) }

func (e *env) quoteLedger(ledger keeper.QuoteLedger) sdk.Int {
	e.t.Helper()
	amount, err := e.k.GetQuoteLedger(e.ctx, ledger)
	require.NoError(e.t, err)
	return amount
}

func (e *env) treasury() sdk.Int { return e.quoteLedger(keeper.TreasuryHeldLedger()) }

func (e *env) feeHeld() sdk.Int {
	return e.quoteLedger(keeper.FeeHeldLedger(e.k.Generation2().FeeAccumulator))
}

func (e *env) pool(denom string) common.Address {
	return common.HexToAddress(e.market(denom).PoolAddress)
}

func (e *env) token(denom string) common.Address {
	return common.HexToAddress(e.market(denom).TokenAddress)
}

func (e *env) held(denom string) sdk.Int { return e.quoteLedger(keeper.HeldQuoteLedger(e.pool(denom))) }

// balance is the market token of denom that acc holds in the EVM.
func (e *env) balance(acc sdk.AccAddress, denom string) sdk.Int {
	return e.tokenBalance(e.token(denom), e.evmAddress(acc))
}

func (e *env) at(d time.Duration) { e.ctx = e.ctx.WithBlockTime(genesisTime.Add(d)) }

func (e *env) create(creator sdk.AccAddress, strategy types.FeeStrategy) types.Market {
	e.t.Helper()
	ctx, market, err := e.k.CreateMarket(e.ctx, e.k.ModuleCaller(), nil, e.evmAddress(creator), "Kindle Token", "KNDL",
		strategy, common.Address{})
	require.NoError(e.t, err)
	e.ctx = ctx
	return market
}

func (e *env) market(denom string) types.Market {
	e.t.Helper()
	market, found := e.k.GetMarket(e.ctx, denom)
	require.True(e.t, found)
	return market
}

// trySwap is a Router swap on a branch that is written only when the swap
// succeeds: a buy first pulls amountIn USDL from the trader into
// HeldQuote(pool) by transferFrom, a sell first transfers amountIn tokens to
// the pool, and the pool swap then counts that prepaid input.
func (e *env) trySwap(trader sdk.AccAddress, denom string, isBuy bool, amountIn, minAmountOut *big.Int,
	recipient sdk.AccAddress, deadline uint64) (keeper.SwapResult, error) {
	e.t.Helper()
	ctx, write := e.ctx.CacheContext()
	run := &env{t: e.t, ctx: ctx, k: e.k, usdl: e.usdl}
	from := run.evmAddress(trader)
	if market, found := e.k.GetMarket(ctx, denom); found && amountIn.Sign() > 0 {
		pool := common.HexToAddress(market.PoolAddress)
		if isBuy {
			var err error
			ctx, _, err = e.k.PullQuote(ctx, e.k.ModuleCaller(), from, keeper.HeldQuoteLedger(pool), sdk.NewIntFromBigInt(amountIn))
			if err != nil {
				return keeper.SwapResult{}, err
			}
		} else {
			run.sendToken(common.HexToAddress(market.TokenAddress), from, pool, amountIn)
		}
	}
	to := common.Address{}
	if !recipient.Empty() {
		to = run.evmAddress(recipient)
	}
	_, result, err := e.k.Swap(ctx, e.k.ModuleCaller(), state.NewDBImpl(ctx, &testkeeper.EVMTestApp.EvmKeeper, false),
		denom, isBuy, amountIn, minAmountOut, from, to, deadline)
	if err != nil {
		return keeper.SwapResult{}, err
	}
	write()
	return result, nil
}

func (e *env) swap(trader sdk.AccAddress, denom string, isBuy bool, amountIn int64) keeper.SwapResult {
	e.t.Helper()
	result, err := e.trySwap(trader, denom, isBuy, big.NewInt(amountIn), big.NewInt(0), trader, farDeadline)
	require.NoError(e.t, err)
	return result
}

type tokenTransfer struct {
	token, from, to common.Address
	amount          string
}

// transfers is the ERC20 Transfer logs of the current transaction so far, the
// newest first.
func (e *env) transfers() []tokenTransfer {
	e.t.Helper()
	r, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(e.ctx, e.ctx.TxSum(), uint64(e.ctx.TxIndex()))
	if errors.Is(err, receipt.ErrNotFound) {
		return nil
	}
	require.NoError(e.t, err)
	out := []tokenTransfer{}
	for _, l := range r.Logs {
		if len(l.Topics) != 3 || common.HexToHash(l.Topics[0]) != transferTopic {
			continue
		}
		out = append(out, tokenTransfer{token: common.HexToAddress(l.Address), from: common.HexToAddress(l.Topics[1]),
			to: common.HexToAddress(l.Topics[2]), amount: new(big.Int).SetBytes(l.Data).String()})
	}
	return out
}

func (e *env) solvent() {
	e.t.Helper()
	message, broken := keeper.SolvencyInvariant(e.k)(e.ctx)
	require.False(e.t, broken, message)
}

// sameJSON compares values by their stored encoding, where a zero amount
// has one form whatever produced it.
func sameJSON(t *testing.T, expected, actual interface{}) {
	t.Helper()
	want, err := json.Marshal(expected)
	require.NoError(t, err)
	got, err := json.Marshal(actual)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(got))
}

func i(value int64) sdk.Int { return sdk.NewInt(value) }

func b(value int64) *big.Int { return big.NewInt(value) }

func TestCreateMarketChargesFeeAndEscrowsSupply(t *testing.T) {
	e := newEnv(t)
	// Funds two creations at DefaultParams' CreationFee 1_000_000 (the
	// ProtocolConfig value) and leaves 500_000, short of a third.
	creator := e.account(2_500_000)
	market := e.create(creator, types.FeeStrategyClaim)

	// One creation fee: 1_000_000 into custody for the treasury, 2_500_000-1_000_000 left.
	require.Equal(t, i(1_000_000), e.treasury())
	require.Equal(t, i(1_000_000), e.custodyUsdl())
	require.Equal(t, i(1_500_000), e.usdlOf(creator))
	from := e.evmAddress(creator)
	token := tokenAddress(t, e.k.Generation2().SidioraFactory, tokenSalt(from, "Kindle Token", "KNDL", 0), "Kindle Token",
		"KNDL")
	pool, err := e.k.PredictPoolAddress(e.ctx, token, from, common.Address{})
	require.NoError(t, err)
	require.Equal(t, token.Hex(), market.TokenAddress)
	require.NotEmpty(t, testkeeper.EVMTestApp.EvmKeeper.GetCode(e.ctx, token))
	require.Equal(t, pool.Hex(), market.PoolAddress)
	require.Equal(t, types.ModuleName+"/"+pool.Hex(), market.Denom)
	require.Equal(t, i(1_000_000_000_000_000), e.tokenBalance(token, pool))
	require.Equal(t, i(1_000_000_000_000_000), e.tokenSupply(token))

	surface, found, err := e.k.GetSurface(e.ctx, pool)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.RolePool, surface.Role)
	require.Equal(t, market.Denom, surface.Market)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	symbol, err := abi.Arguments{{Type: stringType}}.Unpack(e.view(token, "symbol()"))
	require.NoError(t, err)
	require.Equal(t, []interface{}{"KNDL"}, symbol)
	require.Equal(t, i(types.TokenDecimals), e.viewInt(token, "decimals()"))

	require.Equal(t, creator.String(), market.Creator)
	require.Equal(t, creator.String(), market.Guardian)
	require.Equal(t, creator.String(), market.FeeRightsHolder)
	require.Equal(t, i(10_000_000_000), market.VirtualQuoteReserve)
	require.True(t, market.RealQuoteBalance.IsZero())
	require.Equal(t, market.TotalSupply, market.TokenReserve)
	require.Equal(t, genesisTime.Unix(), market.CreationTime)
	price, err := market.Price()
	require.NoError(t, err)
	require.Equal(t, b(10_000_000_000_000), price)

	second := e.create(creator, types.FeeStrategyBurn)
	require.Equal(t, uint64(2), e.k.GetMarketCount(e.ctx))
	sameJSON(t, []types.Market{market, second}, e.k.GetMarketsByCreator(e.ctx, creator, 0, 10))
	sameJSON(t, []types.Market{second}, e.k.GetMarkets(e.ctx, 1, 10))
	sameJSON(t, []types.Market{market}, e.k.GetMarkets(e.ctx, 0, 1))
	e.solvent()

	_, _, err = e.k.CreateMarket(e.ctx, e.k.ModuleCaller(), nil, e.evmAddress(creator), "Kindle Token", "KNDL", types.FeeStrategyClaim,
		common.Address{})
	require.Error(t, err, "creator can no longer pay the creation fee")
	rich := e.account(1_000_000_000)
	_, _, err = e.k.CreateMarket(e.ctx, e.k.ModuleCaller(), nil, e.evmAddress(rich), "Kindle Token", "KNDL", types.FeeStrategy(4),
		common.Address{})
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, _, err = e.k.CreateMarket(e.ctx, e.k.ModuleCaller(), nil, e.evmAddress(rich), " ", "KNDL", types.FeeStrategyClaim,
		common.Address{})
	require.ErrorIs(t, err, types.ErrInvalidMarket)
}

// TestSwapVectors replays SidioraPool.swap on the protocol defaults and
// checks every output against the Solidity arithmetic. Each sell follows its
// buy by a minute, so the 60 s snapshot gate records every swap.
func TestSwapVectors(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(2_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom

	quoted, err := e.k.QuoteBuy(e.ctx, denom, b(100_000_000))
	require.NoError(t, err)
	buy := e.swap(alice, denom, true, 100_000_000)
	require.Equal(t, quoted, buy)
	require.Equal(t, b(300), buy.FeeBps)
	require.Equal(t, b(3_000_000), buy.FeeAmount)
	require.Equal(t, b(9_606_813_905_120), buy.AmountOut)
	require.Equal(t, b(300_000), buy.ProtocolCut)
	require.Equal(t, b(2_700_000), buy.PoolCut)
	m := e.market(denom)
	require.Equal(t, i(97_000_000), m.RealQuoteBalance)
	require.Equal(t, m.RealQuoteBalance, e.held(denom))
	require.Equal(t, i(990_393_186_094_880), m.TokenReserve)
	require.Equal(t, i(100_000_000), m.CumulativeVolume)
	require.Equal(t, i(100_000_000), m.CumulativeBuyQuoteVolume)
	require.Equal(t, i(3_000_000), m.AccumulatedQuoteFees)
	require.Equal(t, i(2_700_000), m.AccumulatedFees)
	require.Equal(t, i(2_700_000), e.feeHeld())
	require.Equal(t, i(10_000_000_000), m.VirtualQuoteReserve)
	require.Equal(t, i(10_194_940_899_999), m.PriceSnapshots[0])
	require.Equal(t, uint64(1), m.SnapshotIndex)
	require.Equal(t, uint64(1), m.SnapshotCount)
	require.Equal(t, i(9_606_813_905_120), e.balance(alice, denom))
	// CreationFee 1_000_000 plus the protocol cut 3_000_000*1_000/10_000 = 300_000.
	require.Equal(t, i(1_300_000), e.treasury())
	require.Equal(t, i(97_000_000+2_700_000+1_300_000), e.custodyUsdl())

	e.at(time.Minute)
	tokenReserveBefore := m.TokenReserve
	sell := e.swap(alice, denom, false, 4_803_406_952_560)
	require.Equal(t, b(300), sell.FeeBps)
	require.Equal(t, b(144_102_208_576), sell.FeeAmount)
	require.Equal(t, b(47_278_912), sell.AmountOut)
	require.Equal(t, 0, sell.ProtocolCut.Sign())
	m = e.market(denom)
	require.Equal(t, tokenReserveBefore.Add(i(4_803_406_952_560)), m.TokenReserve)
	require.Equal(t, i(49_721_088), m.RealQuoteBalance)
	require.Equal(t, m.RealQuoteBalance, e.held(denom))
	require.Equal(t, i(2_700_000), m.AccumulatedFees, "a sell does not touch the quote fees")
	require.Equal(t, i(2_700_000), e.feeHeld())
	require.Equal(t, i(144_102_208_576), m.AccumulatedTokenFees)
	require.Equal(t, i(10_098_226_981_692), m.PriceSnapshots[1])

	e.at(mature)
	fee, err := e.k.FeeBps(e.ctx, m)
	require.NoError(t, err)
	require.Equal(t, b(30), fee, "two snapshots have one change and no deviation")
	second := e.swap(alice, denom, true, 1_000_000_000)
	require.Equal(t, b(30), second.FeeBps)
	require.Equal(t, b(3_000_000), second.FeeAmount)
	require.Equal(t, b(89_819_503_485_620), second.AmountOut)
	m = e.market(denom)
	require.Equal(t, i(1_046_721_088), m.RealQuoteBalance)
	require.Equal(t, m.RealQuoteBalance, e.held(denom))
	require.Equal(t, i(12_201_237_711_179), m.PriceSnapshots[2])

	e.at(mature + time.Minute)
	last := e.swap(alice, denom, false, 89_819_503_485_620)
	// FeeLib.calculateVolatility over the three snapshots: movements
	// 96_713_918_307*1e6/10_194_940_899_999 = 9_486 and
	// 2_103_010_729_487*1e6/10_098_226_981_692 = 208_255; sqrt((9_486^2 +
	// 208_255^2)/2) = sqrt(21_730_064_610) = 147_411. FeeLib.calculateDynamicFee:
	// base 30, age 500/(1+1000) = 0, threshold mulDiv(270, 1e6, 147_411) =
	// 1_831 >= weight 100, so calculateVolatilityFactor adds 100*147_411/1e6 = 14.
	require.Equal(t, b(44), last.FeeBps, "volatility of three snapshots adds 14 bps")
	// SidioraPool.swapFor sell: fee 89_819_503_485_620*44/10000 =
	// 395_205_815_336; ReserveLib.getAmountOut(905_377_089_561_820,
	// 11_046_721_088, 89_424_297_670_284) = 993_007_536.
	require.Equal(t, b(993_007_536), last.AmountOut)
	m = e.market(denom)
	// 1_046_721_088 - 993_007_536.
	require.Equal(t, i(53_713_552), m.RealQuoteBalance)
	require.Equal(t, m.RealQuoteBalance, e.held(denom))
	require.Equal(t, i(995_196_593_047_440), m.TokenReserve)
	// ReserveLib.getPrice: (10_000_000_000+53_713_552)*1e18/995_196_593_047_440.
	require.Equal(t, i(10_102_238_715_683), m.PriceSnapshots[3])
	require.Equal(t, uint64(4), m.SnapshotCount)
	// USDL volume: paid input on a buy, USDL out on a sell.
	require.Equal(t, i(100_000_000+47_278_912+1_000_000_000+993_007_536), m.CumulativeVolume)
	require.Equal(t, i(100_000_000+1_000_000_000), m.CumulativeBuyQuoteVolume)
	require.Equal(t, i(47_278_912+993_007_536), m.CumulativeSellQuoteVolume)
	// The second buy's protocol cut 3_000_000*1_000/10_000 joins the treasury.
	require.Equal(t, i(1_600_000), e.treasury())
	require.Equal(t, i(5_400_000), e.feeHeld())
	require.Equal(t, i(53_713_552+5_400_000+1_600_000), e.custodyUsdl())
	e.solvent()
}

// TestBuyMovesUsdlIntoCustody checks that a buy moves the caller's USDL to
// the launchpad module EVM address by transferFrom and books it on the
// custody ledgers: the input less the fee to HeldQuote(pool), the pool cut to
// FeeHeld and the protocol cut to the treasury.
func TestBuyMovesUsdlIntoCustody(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	trader := e.account(100_000_000)
	market := e.create(creator, types.FeeStrategyClaim)
	denom := market.Denom
	pool, token := common.HexToAddress(market.PoolAddress), common.HexToAddress(market.TokenAddress)
	from, custody := e.evmAddress(trader), e.k.ModuleEVMAddress(e.ctx)
	launchpad := common.HexToAddress(types.LaunchpadAddress)
	e.approveQuote(trader, b(100_000_000))
	require.Equal(t, i(100_000_000), e.allowanceOf(e.usdl, from, launchpad))

	held, feeHeld, treasury, inCustody := e.held(denom), e.feeHeld(), e.treasury(), e.custodyUsdl()
	supply := e.tokenSupply(e.usdl)
	bank := testkeeper.EVMTestApp.BankKeeper
	banked := func() []sdk.Coins {
		return []sdk.Coins{bank.GetAllBalances(e.ctx, trader), bank.GetAllBalances(e.ctx, e.k.ModuleAddress()),
			bank.GetAllBalances(e.ctx, testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(e.ctx, pool))}
	}
	bankBefore := banked()
	logsBefore := len(e.transfers())

	result := e.swap(trader, denom, true, 100_000_000)
	require.Equal(t, b(300), result.FeeBps)
	require.Equal(t, b(3_000_000), result.FeeAmount)
	require.Equal(t, b(300_000), result.ProtocolCut)
	require.Equal(t, b(2_700_000), result.PoolCut)
	require.Equal(t, b(9_606_813_905_120), result.AmountOut)

	// 100_000_000 - fee 3_000_000 to the pool, 3_000_000 - 300_000 to the
	// FeeAccumulator, 3_000_000*1_000/10_000 to the treasury.
	require.Equal(t, held.AddRaw(97_000_000), e.held(denom))
	require.Equal(t, feeHeld.AddRaw(2_700_000), e.feeHeld())
	require.Equal(t, treasury.AddRaw(300_000), e.treasury())
	require.Equal(t, inCustody.AddRaw(100_000_000), e.custodyUsdl())
	require.True(t, e.usdlOf(trader).IsZero())
	require.True(t, e.allowanceOf(e.usdl, from, launchpad).IsZero(), "transferFrom spent the allowance")
	require.True(t, e.tokenBalance(e.usdl, pool).IsZero(), "no USDL rests at the pool address")
	require.Equal(t, supply, e.tokenSupply(e.usdl))
	require.Equal(t, i(9_606_813_905_120), e.balance(trader, denom))
	require.Equal(t, i(990_393_186_094_880), e.tokenBalance(token, pool))

	logs := e.transfers()
	require.ElementsMatch(t, []tokenTransfer{
		{token: e.usdl, from: from, to: custody, amount: "100000000"},
		{token: token, from: pool, to: from, amount: "9606813905120"},
	}, logs[logsBefore:])
	sameJSON(t, bankBefore, banked())

	m := e.market(denom)
	require.Equal(t, i(97_000_000), m.RealQuoteBalance)
	require.Equal(t, i(3_000_000), m.AccumulatedQuoteFees)
	require.Equal(t, i(2_700_000), m.AccumulatedFees)
	require.Equal(t, i(100_000_000), m.CumulativeBuyQuoteVolume)
	require.True(t, m.CumulativeSellQuoteVolume.IsNil() || m.CumulativeSellQuoteVolume.IsZero())
	e.solvent()
}

func TestSnapshotRingWraps(t *testing.T) {
	e := newSurfacedEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(1_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	require.Equal(t, make([]uint64, types.SnapshotSlots), e.market(denom).SnapshotHeights)
	legacy := e.k.ExportGenesis(e.ctx)
	legacy.Markets[0].SnapshotHeights = nil
	require.NoError(t, legacy.Validate())
	e.k.InitGenesis(e.ctx, *legacy)
	for _, length := range []int{1, types.SnapshotSlots - 1, types.SnapshotSlots + 1} {
		invalid := legacy.Markets[0]
		invalid.SnapshotHeights = make([]uint64, length)
		require.ErrorIs(t, invalid.Validate(), types.ErrInvalidMarket)
	}
	for n := 0; n < types.SnapshotSlots+2; n++ {
		e.ctx = e.ctx.WithBlockHeight(int64(n + 1))
		e.at(time.Duration(n) * time.Minute)
		before := e.market(denom)
		_, err := e.k.QuoteBuy(e.ctx, denom, b(2_000_000))
		require.NoError(t, err)
		sameJSON(t, before, e.market(denom))
		e.swap(alice, denom, true, 2_000_000)
	}
	m := e.market(denom)
	require.Equal(t, uint64(types.SnapshotSlots), m.SnapshotCount)
	require.Equal(t, uint64(2), m.SnapshotIndex)
	price, err := m.Price()
	require.NoError(t, err)
	require.Equal(t, sdk.NewIntFromBigInt(price), m.PriceSnapshots[1])
	require.Equal(t, []uint64{9, 10, 3, 4, 5, 6, 7, 8}, m.SnapshotHeights)
	require.NoError(t, m.Validate())
}

// TestQuoteConservation checks, after every swap of a buy/sell sequence by
// two traders across fee regimes, that USDL paid in equals USDL paid out
// plus fees plus the real balance left in the curve, and that the custody
// ledgers and ERC20 balances match the market's accounting.
func TestQuoteConservation(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(5_000_000_000)
	bob := e.account(5_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	treasuryStart := e.treasury()

	quoteIn, quoteOut, quoteFees, protocol := sdk.ZeroInt(), sdk.ZeroInt(), sdk.ZeroInt(), sdk.ZeroInt()
	steps := []struct {
		at     time.Duration
		trader sdk.AccAddress
		isBuy  bool
		amount int64
	}{
		{0, alice, true, 250_000_000},
		{time.Minute, bob, true, 1_750_000_000},
		{time.Hour, alice, false, 0},
		{2 * time.Hour, bob, false, 0},
		{30 * time.Hour, alice, true, 999_999_999},
		{mature, bob, true, 12_345_678},
		{mature + time.Hour, bob, false, 0},
		{mature + 2*time.Hour, alice, false, 0},
	}
	for n, step := range steps {
		e.at(step.at)
		amount := step.amount
		if !step.isBuy {
			amount = e.balance(step.trader, denom).QuoRaw(3).Int64()
		}
		quoteBefore := e.usdlOf(step.trader)
		result := e.swap(step.trader, denom, step.isBuy, amount)
		if step.isBuy {
			quoteIn = quoteIn.AddRaw(amount)
			quoteFees = quoteFees.Add(sdk.NewIntFromBigInt(result.FeeAmount))
			protocol = protocol.Add(sdk.NewIntFromBigInt(result.ProtocolCut))
			require.Equal(t, quoteBefore.SubRaw(amount), e.usdlOf(step.trader), "step %d", n)
		} else {
			quoteOut = quoteOut.Add(sdk.NewIntFromBigInt(result.AmountOut))
			require.Equal(t, quoteBefore.Add(sdk.NewIntFromBigInt(result.AmountOut)), e.usdlOf(step.trader), "step %d", n)
		}
		m := e.market(denom)
		require.Equal(t, quoteIn, quoteOut.Add(quoteFees).Add(m.RealQuoteBalance), "step %d", n)
		require.Equal(t, quoteFees, m.AccumulatedQuoteFees, "step %d", n)
		require.Equal(t, quoteFees, m.AccumulatedFees.Add(protocol), "step %d", n)
		require.Equal(t, m.RealQuoteBalance, e.held(denom), "step %d", n)
		require.Equal(t, m.AccumulatedFees, e.feeHeld(), "step %d", n)
		require.Equal(t, treasuryStart.Add(protocol), e.treasury(), "step %d", n)
		require.Equal(t, m.RealQuoteBalance.Add(m.AccumulatedFees).Add(e.treasury()), e.custodyUsdl(), "step %d", n)
		require.Equal(t, m.TokenReserve, e.tokenBalance(e.token(denom), e.pool(denom)), "step %d", n)
		require.Equal(t, m.TotalSupply, m.TokenReserve.Add(e.balance(alice, denom)).Add(e.balance(bob, denom)), "step %d", n)
		require.Equal(t, i(10_000_000_000), m.VirtualQuoteReserve)
		e.solvent()
	}
}

func TestSwapRejections(t *testing.T) {
	e := newSurfacedEnv(t)
	creator := e.account(100_000_000)
	trader := e.account(1_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	e.at(time.Hour)
	swap := func(isBuy bool, amount, minOut int64, recipient sdk.AccAddress, deadline uint64) error {
		_, err := e.trySwap(trader, denom, isBuy, b(amount), b(minOut), recipient, deadline)
		return err
	}
	now := uint64(genesisTime.Add(time.Hour).Unix())
	require.ErrorIs(t, swap(true, 1_000_000, 0, trader, now-1), types.ErrDeadlineExpired)
	require.NoError(t, swap(true, 1_000_000, 0, trader, now), "the deadline block itself is allowed")
	require.ErrorIs(t, swap(true, 0, 0, trader, now), types.ErrInsufficientInput)
	require.ErrorIs(t, swap(true, 1_000_000, 0, nil, now), types.ErrZeroAddress)
	before := e.k.ExportGenesis(e.ctx)
	quoteBefore := e.usdlOf(trader)
	tokenBefore := e.balance(trader, denom)
	require.ErrorIs(t, swap(true, 1_000_000, 1_000_000_000_000_000, trader, now), types.ErrSlippageExceeded)
	require.ErrorIs(t, swap(false, 1, 1_000_000_000, trader, now), types.ErrInsufficientInput)
	require.ErrorIs(t, swap(false, tokenBefore.QuoRaw(2).Int64(), 1_000_000_000, trader, now), types.ErrSlippageExceeded)
	sameJSON(t, before, e.k.ExportGenesis(e.ctx))
	require.Equal(t, quoteBefore, e.usdlOf(trader))
	require.Equal(t, tokenBefore, e.balance(trader, denom))
	require.NoError(t, e.k.Pause(e.ctx, creator, denom))
	paused := e.k.ExportGenesis(e.ctx)
	require.ErrorIs(t, swap(true, 1_000_000, 0, trader, now), types.ErrPaused)
	require.ErrorIs(t, swap(false, 1, 0, trader, now), types.ErrPaused)
	sameJSON(t, paused, e.k.ExportGenesis(e.ctx))
	require.NoError(t, e.k.Unpause(e.ctx, creator, denom))
	require.Error(t, swap(true, 2_000_000_000, 0, trader, now), "more quote than trader holds")
	_, err := e.trySwap(trader, "factory/nobody/lp9", true, b(1), b(0), trader, now)
	require.ErrorIs(t, err, types.ErrUnknownMarket)

	// The virtual floor: a sell may never pay out virtual quote. Genesis
	// state with no real quote left behind the tokens trader holds.
	gs := e.k.ExportGenesis(e.ctx)
	gs.Markets[0].RealQuoteBalance = sdk.ZeroInt()
	e.k.InitGenesis(e.ctx, *gs)
	require.ErrorIs(t, swap(false, e.balance(trader, denom).Int64(), 0, trader, now), types.ErrVirtualFloorBreached)
}

func TestFeeSplitMatchesFeeAccumulator(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(1_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	treasury, feeHeld := e.treasury(), e.feeHeld()
	result := e.swap(alice, denom, true, 333_333_334)
	require.Equal(t, b(10_000_000), result.FeeAmount)
	require.Equal(t, b(1_000_000), result.ProtocolCut)
	require.Equal(t, b(9_000_000), result.PoolCut)
	require.Equal(t, treasury.AddRaw(1_000_000), e.treasury())
	require.Equal(t, feeHeld.AddRaw(9_000_000), e.feeHeld())
	require.Equal(t, i(9_000_000), e.market(denom).AccumulatedFees)
	require.Equal(t, e.held(denom).Add(e.feeHeld()).Add(e.treasury()), e.custodyUsdl())
	t.Run("fee_split_and_solvency", func(t *testing.T) {
		e := newEnv(t)
		params := e.k.GetParams(e.ctx)
		params.ProtocolFeeBps = 2000
		require.NoError(t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
		creator := e.account(200_000_000)
		trader := e.account(1_000_000_000)
		markets := []string{e.create(creator, types.FeeStrategyClaim).Denom,
			e.create(creator, types.FeeStrategyClaim).Denom}
		treasury := e.treasury()
		result := e.swap(trader, markets[0], true, 3334)
		require.Equal(t, b(100), result.FeeAmount)
		require.Equal(t, b(20), result.ProtocolCut)
		require.Equal(t, b(80), result.PoolCut)
		require.Equal(t, treasury.AddRaw(20), e.treasury())
		require.Equal(t, i(80), e.feeHeld())
		require.Equal(t, i(80), e.market(markets[0]).AccumulatedFees)
		e.solvent()
		protocol := []sdk.Int{i(20), sdk.ZeroInt()}
		pool := []sdk.Int{i(80), sdk.ZeroInt()}
		random := rand.New(rand.NewSource(1))
		for n := 0; n < 50; n++ {
			index := random.Intn(len(markets))
			denom := markets[index]
			isBuy := random.Intn(2) == 0 || e.balance(trader, denom).IsZero()
			amount := int64(random.Intn(1_000_000) + 1)
			if !isBuy {
				amount = e.balance(trader, denom).QuoRaw(int64(random.Intn(4) + 2)).Int64()
			}
			result := e.swap(trader, denom, isBuy, amount)
			protocol[index] = protocol[index].Add(sdk.NewIntFromBigInt(result.ProtocolCut))
			pool[index] = pool[index].Add(sdk.NewIntFromBigInt(result.PoolCut))
			require.Equal(t, pool[index], e.market(denom).AccumulatedFees)
			require.Equal(t, pool[0].Add(pool[1]), e.feeHeld())
			require.Equal(t, treasury.Add(protocol[0]).Add(protocol[1]), e.treasury())
			require.Equal(t, e.held(markets[0]).Add(e.held(markets[1])).Add(e.feeHeld()).Add(e.treasury()), e.custodyUsdl())
			e.solvent()
		}
	})
}

func TestFeeStrategies(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(2_000_000_000)
	stranger := e.account(0)
	denom := e.create(creator, types.FeeStrategyClaim).Denom

	_, err := e.k.ClaimFees(e.ctx, creator, denom, creator)
	require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	e.swap(alice, denom, true, 333_333_334)

	_, err = e.k.ClaimFees(e.ctx, stranger, denom, stranger)
	require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)
	_, err = e.k.ExecuteBurn(e.ctx, creator, denom)
	require.ErrorIs(t, err, types.ErrWrongStrategy)
	_, err = e.k.ClaimFees(e.ctx, creator, denom, nil)
	require.ErrorIs(t, err, types.ErrZeroAddress)
	claimed, err := e.k.ClaimFees(e.ctx, creator, denom, stranger)
	require.NoError(t, err)
	require.Equal(t, i(9_000_000), claimed)
	require.Equal(t, i(9_000_000), e.usdlOf(stranger))
	require.True(t, e.market(denom).AccumulatedFees.IsZero())
	_, err = e.k.ClaimFees(e.ctx, creator, denom, creator)
	require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	e.solvent()

	require.ErrorIs(t, e.k.SetFeeStrategy(e.ctx, stranger, denom, types.FeeStrategyBurn), types.ErrNotFeeRightsHolder)
	require.ErrorIs(t, e.k.SetFeeStrategy(e.ctx, creator, denom, types.FeeStrategy(4)), types.ErrInvalidStrategy)
	require.NoError(t, e.k.SetFeeStrategy(e.ctx, creator, denom, types.FeeStrategyBurn))
	e.swap(alice, denom, true, 333_333_334)
	pool := e.market(denom).AccumulatedFees
	dead := e.k.DeadAccount(e.ctx)
	require.Equal(t, testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(e.ctx, common.HexToAddress(types.DeadAddress)), dead)
	deadBefore := e.tokenBalance(e.usdl, common.HexToAddress(types.DeadAddress))
	burned, err := e.k.ExecuteBurn(e.ctx, creator, denom)
	require.NoError(t, err)
	require.Equal(t, pool, burned)
	require.Equal(t, deadBefore.Add(pool), e.tokenBalance(e.usdl, common.HexToAddress(types.DeadAddress)))
	e.solvent()

	require.NoError(t, e.k.SetFeeStrategy(e.ctx, creator, denom, types.FeeStrategyLpRewards))
	e.swap(alice, denom, true, 333_333_334)
	before := e.market(denom)
	escrowBefore := e.custodyUsdl()
	rewards, err := e.k.ExecuteLpRewards(e.ctx, creator, denom)
	require.NoError(t, err)
	require.Equal(t, before.AccumulatedFees, rewards)
	after := e.market(denom)
	require.Equal(t, before.RealQuoteBalance.Add(rewards), after.RealQuoteBalance)
	require.True(t, after.AccumulatedFees.IsZero())
	require.Equal(t, escrowBefore, e.custodyUsdl(), "LP rewards stay in custody")
	_, err = e.k.ExecuteLpRewards(e.ctx, creator, denom)
	require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	e.solvent()
}

// TestAirdropSharesByHolding reproduces FeeAccumulator's airdrop vector on the
// generation 2 path: a 10 USDL fee leaves 9 USDL for the epoch that
// triggerAirdrop opens, and a holder of 600 of the 768.696993 eligible pool
// tokens at the snapshot takes 9*600/768.696993 USDL.
func TestAirdropSharesByHolding(t *testing.T) {
	h := newHistoryEnv(t)
	roles := h.k.Generation2()
	pool := h.pool(h.denom)
	alice := h.account(4_000_000_000)
	bob := h.account(0)
	carol := h.account(0)

	_, err := h.claim(h.evmAddress(alice), 1)
	require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
	result := h.swap(alice, h.denom, true, 3_333_333_334)
	require.Equal(t, b(30), result.FeeBps)
	require.Equal(t, b(10_000_000), result.FeeAmount)
	require.Equal(t, b(768_696_993), result.AmountOut)
	h.transfer(alice, bob, 168_696_993)
	require.Equal(t, i(600_000_000), h.balance(alice, h.denom))

	_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), h.evmAddress(bob), pool)
	require.ErrorIs(t, err, types.ErrMissingRole)
	record := h.trigger()
	require.Equal(t, i(9_000_000), record.Amount)
	require.Equal(t, i(768_696_993), record.EligibleSupply)
	m := h.market(h.denom)
	require.Equal(t, uint64(1), m.AirdropEpoch)
	require.Equal(t, i(9_000_000), m.AirdropBalance)
	_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), roles.FeesRouter, pool)
	require.ErrorIs(t, err, types.ErrNoFeesAccumulated)

	aliceBefore := h.usdlOf(alice)
	aliceShare, err := h.claim(h.evmAddress(alice), 1)
	require.NoError(t, err)
	require.Equal(t, i(7_024_874), aliceShare)
	require.Equal(t, aliceBefore.AddRaw(7_024_874), h.usdlOf(alice))
	require.True(t, h.k.HasClaimedPoolAirdrop(h.ctx, pool, h.evmAddress(alice), 1))
	_, err = h.claim(h.evmAddress(alice), 1)
	require.ErrorIs(t, err, types.ErrAlreadyClaimed)
	// Bob is the final eligible claimant and takes the rounding residue.
	bobShare, err := h.claim(h.evmAddress(bob), 1)
	require.NoError(t, err)
	require.Equal(t, i(1_975_126), bobShare)
	_, err = h.claim(h.evmAddress(carol), 1)
	require.ErrorIs(t, err, types.ErrZeroAmount)
	require.True(t, h.market(h.denom).AirdropBalance.IsZero())
	sameJSON(t, types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: 1, Amount: i(9_000_000),
		SnapshotBlock: record.SnapshotBlock, EligibleSupply: i(768_696_993), Remaining: i(0),
		ClaimedSupply: i(768_696_993), LegacyRemaining: i(0)}, h.epochRecord(1))
	h.solvent()

	gs := h.k.ExportGenesis(h.ctx)
	require.NoError(t, gs.Validate())
	sameJSON(t, []types.PoolAirdropEpoch{h.epochRecord(1)}, gs.FeeAccounting.Epochs)
	require.ElementsMatch(t, []types.PoolAirdropClaim{
		{Pool: pool.Hex(), Holder: h.evmAddress(alice).Hex(), Epoch: 1},
		{Pool: pool.Hex(), Holder: h.evmAddress(bob).Hex(), Epoch: 1},
	}, gs.FeeAccounting.Claims)
}

func TestGuardianPause(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(1_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	require.ErrorIs(t, e.k.Pause(e.ctx, alice, denom), types.ErrNotGuardian)
	require.ErrorIs(t, e.k.Unpause(e.ctx, creator, denom), types.ErrNotPaused)
	require.NoError(t, e.k.Pause(e.ctx, creator, denom))
	require.ErrorIs(t, e.k.Pause(e.ctx, creator, denom), types.ErrPaused)
	_, err := e.trySwap(alice, denom, true, b(1_000_000), b(0), alice, farDeadline)
	require.ErrorIs(t, err, types.ErrPaused)
	require.ErrorIs(t, e.k.Unpause(e.ctx, alice, denom), types.ErrNotGuardian)
	require.NoError(t, e.k.Unpause(e.ctx, creator, denom))
	e.swap(alice, denom, true, 1_000_000)
}

func TestParamsChangeOnlyThroughGovernance(t *testing.T) {
	e := newEnv(t)
	gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	require.Equal(t, gov, e.k.Authority())
	current := e.k.GetParams(e.ctx)
	params := current
	params.ProtocolFeeBps = 2_000
	params.CreationFee = i(5)
	outsider, _ := testkeeper.MockAddressPair()
	require.ErrorIs(t, e.k.UpdateParams(e.ctx, outsider.String(), params), types.ErrUnauthorized)
	sameJSON(t, current, e.k.GetParams(e.ctx))
	invalid := params
	invalid.MaxFeeBps = 10_000
	require.ErrorIs(t, e.k.UpdateParams(e.ctx, gov, invalid), types.ErrInvalidParams)
	require.NoError(t, e.k.UpdateParams(e.ctx, gov, params))
	sameJSON(t, params, e.k.GetParams(e.ctx))

	creator := e.account(5)
	alice := e.account(1_000_000_000)
	denom := e.create(creator, types.FeeStrategyClaim).Denom
	require.Equal(t, i(5), e.treasury())
	result := e.swap(alice, denom, true, 333_333_334)
	require.Equal(t, b(2_000_000), result.ProtocolCut)
	require.Equal(t, b(8_000_000), result.PoolCut)
}

func TestGenesisRoundTrip(t *testing.T) {
	e := newSurfacedEnv(t)
	params := e.k.GetParams(e.ctx)
	params.ProtocolFeeBps = 2000
	require.NoError(t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
	creator := e.account(200_000_000)
	trader := e.account(1_000_000_000)
	first := e.create(creator, types.FeeStrategyClaim).Denom
	second := e.create(creator, types.FeeStrategyLpRewards).Denom
	pools := []common.Address{e.pool(first), e.pool(second)}
	// Fee 3334*300/10_000 = 100: 20 to the treasury and 80 to FeeHeld.
	e.swap(trader, first, true, 3334)
	// Fee 5834*300/10_000 = 175: 35 to the treasury and 140 to FeeHeld.
	e.swap(trader, second, true, 5834)
	e.at(time.Minute)
	e.swap(trader, first, false, e.balance(trader, first).QuoRaw(2).Int64())
	require.NoError(t, e.k.Pause(e.ctx, creator, first))
	exported := e.k.ExportGenesis(e.ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Markets, 2)

	held := map[string]string{}
	for _, entry := range exported.HeldQuote {
		held[entry.Pool] = entry.Amount.String()
	}
	require.Equal(t, map[string]string{pools[0].Hex(): e.market(first).RealQuoteBalance.String(),
		pools[1].Hex(): i(5834 - 175).String()}, held)
	require.Equal(t, i(80+140).String(), exported.FeeHeld.String())
	// Two creation fees and the protocol cuts 20 and 35.
	sameJSON(t, []types.TreasuryBalance{{Token: e.usdl.Hex(), Ledger: i(2_000_055), Held: i(2_000_055)}}, exported.Treasury)
	require.Equal(t, i(80+140).String(), exported.FeeAccounting.TotalLiabilities.String())
	accumulated := map[string]string{}
	for _, account := range exported.FeeAccounting.Pools {
		accumulated[account.Pool] = account.AccumulatedFees.String()
	}
	require.Equal(t, map[string]string{pools[0].Hex(): "80", pools[1].Hex(): "140"}, accumulated)
	surfaces := map[string]string{}
	for _, surface := range exported.Surfaces {
		surfaces[surface.Address] = surface.Role
	}
	for _, pool := range pools {
		require.Equal(t, types.RolePool, surfaces[pool.Hex()])
	}
	require.Equal(t, e.held(first).Add(e.held(second)).AddRaw(220).AddRaw(2_000_055), e.custodyUsdl())

	raw, err := json.Marshal(exported)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "protocol_fees_pending")
	var decoded types.GenesisState
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NoError(t, decoded.Validate())

	fresh, ctx := launchpadtest.NewKeeper(testkeeper.EVMTestApp, e.ctx)
	fresh.InitGenesis(ctx, decoded)
	sameJSON(t, exported, fresh.ExportGenesis(ctx))
	sameJSON(t, e.k.GetMarkets(e.ctx, 0, 10), fresh.GetMarkets(ctx, 0, 10))
	surface, found, err := fresh.GetSurface(ctx, pools[0])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.RolePool, surface.Role)
	require.Equal(t, first, surface.Market)
	market, found := fresh.GetMarket(ctx, first)
	require.True(t, found)
	require.True(t, market.Paused)
	for index, denom := range []string{first, second} {
		amount, err := fresh.GetQuoteLedger(ctx, keeper.HeldQuoteLedger(pools[index]))
		require.NoError(t, err)
		require.Equal(t, e.held(denom), amount)
	}
	fees, err := fresh.GetQuoteLedger(ctx, keeper.FeeHeldLedger(fresh.Generation2().FeeAccumulator))
	require.NoError(t, err)
	require.Equal(t, i(220), fees)
	treasury, err := fresh.GetQuoteLedger(ctx, keeper.TreasuryHeldLedger())
	require.NoError(t, err)
	require.Equal(t, i(2_000_055), treasury)
	require.False(t, ctx.KVStore(sdk.NewKVStoreKey(types.StoreKey)).Has(types.ProtocolFeesPrefix))
	for _, entries := range [][]types.ProtocolFeeEntry{
		{{Token: first, Amount: i(20)}, {Token: first, Amount: i(35)}},
		{{Token: first, Amount: i(-1)}}, {{Token: first, Amount: sdk.Int{}}}, {{Token: "", Amount: i(1)}},
		{{Token: "!", Amount: i(1)}},
	} {
		invalid := *exported
		invalid.ProtocolFees = entries
		require.ErrorIs(t, invalid.Validate(), types.ErrInvalidGenesis)
	}

	broken := *exported
	broken.Markets = []types.Market{exported.Markets[1]}
	require.ErrorIs(t, broken.Validate(), types.ErrInvalidGenesis)
}

func parameterRouter(t *testing.T, k *keeper.Keeper) (*baseapp.MsgServiceRouter, *codec.ProtoCodec) {
	t.Helper()
	registry := cdctypes.NewInterfaceRegistry()
	sdk.RegisterInterfaces(registry)
	moduleimpl.AppModuleBasic{}.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(registry)
	moduleimpl.NewAppModule(k).RegisterServices(module.NewConfigurator(cdc, router, baseapp.NewGRPCQueryRouter()))
	return router, cdc
}
func parameterRoute(t *testing.T, router *baseapp.MsgServiceRouter, cdc *codec.ProtoCodec, ctx sdk.Context, msg *types.MsgUpdateParams) error {
	t.Helper()
	raw, err := cdc.MarshalInterface(msg)
	require.NoError(t, err)
	var decoded sdk.Msg
	require.NoError(t, cdc.UnmarshalInterface(raw, &decoded))
	require.Equal(t, "/paxprotocol.paxchain.launchpad.MsgUpdateParams", sdk.MsgTypeURL(decoded))
	raw2, err := cdc.MarshalInterface(decoded)
	require.NoError(t, err)
	require.Equal(t, raw, raw2)
	handler := router.Handler(decoded)
	require.NotNil(t, handler)
	_, err = handler(ctx, decoded)
	return err
}
func TestMsgUpdateParamsGovernanceRoute(t *testing.T) {
	e := newEnv(t)
	k, ctx := e.k, e.ctx
	authority := k.Authority()
	router, cdc := parameterRouter(t, k)
	params := k.GetParams(ctx)
	params.ProtocolFeeBps = 2000
	params.CreationFee = sdk.NewInt(5)
	require.NoError(t, parameterRoute(t, router, cdc, ctx, &types.MsgUpdateParams{Authority: authority, Params: params}))
	require.Equal(t, params, k.GetParams(ctx))
	eventResult, eventErr := router.Handler(&types.MsgUpdateParams{})(ctx, &types.MsgUpdateParams{Authority: authority, Params: params})
	require.NoError(t, eventErr)
	events := eventResult.Events
	require.NotEmpty(t, events)
	require.Equal(t, types.EventTypeParamsUpdated, events[len(events)-1].Type)
	response, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: authority, Params: params})
	require.NoError(t, err)
	require.NotNil(t, response)
}
func TestMsgUpdateParamsCodecAndSigners(t *testing.T) {
	e := newEnv(t)
	k, ctx := e.k, e.ctx
	authority := k.Authority()
	_, cdc := parameterRouter(t, k)
	assignment := types.RoleAssignment{Contract: keeper.Generation2Surfaces[types.RoleFeeAccumulator].Hex(),
		Role: crypto.Keccak256Hash([]byte("POOL_ROLE")).Hex(), Account: address("account A").Hex()}
	msg := &types.MsgUpdateParams{Authority: authority, Params: k.GetParams(ctx),
		RoleGrants: []types.RoleAssignment{assignment}, RoleRevokes: []types.RoleAssignment{assignment}}
	require.NoError(t, msg.ValidateBasic())
	signer, err := sdk.AccAddressFromBech32(authority)
	require.NoError(t, err)
	require.Equal(t, []sdk.AccAddress{signer}, msg.GetSigners())
	require.Equal(t, types.RouterKey, msg.Route())
	require.Equal(t, types.TypeMsgUpdateParams, msg.Type())
	require.Equal(t, sdk.MustSortJSON(types.ModuleCdc.MustMarshalJSON(msg)), msg.GetSignBytes())
	raw, err := proto.Marshal(msg)
	require.NoError(t, err)
	var decoded types.MsgUpdateParams
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	require.Equal(t, *msg, decoded)
	raw, err = cdc.MarshalAsJSON(msg)
	require.NoError(t, err)
	require.NoError(t, cdc.UnmarshalAsJSON(raw, &decoded))
	require.Equal(t, *msg, decoded)
	amino := codec.NewLegacyAmino()
	sdk.RegisterLegacyAminoCodec(amino)
	moduleimpl.AppModuleBasic{}.RegisterLegacyAminoCodec(amino)
	raw, err = amino.MarshalAsJSON(msg)
	require.NoError(t, err)
	require.NoError(t, amino.UnmarshalAsJSON(raw, &decoded))
	require.Equal(t, *msg, decoded)
	preserved := msg.Params
	require.Error(t, preserved.UnmarshalJSONPB(&jsonpb.Unmarshaler{}, []byte(`{"unknown_field":true}`)))
	require.Equal(t, msg.Params, preserved)
	raw, err = json.Marshal(msg.Params)
	require.NoError(t, err)
	require.Error(t, preserved.UnmarshalJSONPB(nil, append(raw, []byte(` {}`)...)))
	require.Equal(t, msg.Params, preserved)

}
func TestMsgUpdateParamsRefusals(t *testing.T) {
	e := newEnv(t)
	k, ctx := e.k, e.ctx
	authority := k.Authority()
	router, cdc := parameterRouter(t, k)
	before := k.GetParams(ctx)
	outsider := sdk.AccAddress(bytes.Repeat([]byte{0xfe}, 20)).String()
	for _, caller := range []string{outsider, "invalid-bech32"} {
		msg := &types.MsgUpdateParams{Authority: caller, Params: before}
		require.Error(t, parameterRoute(t, router, cdc, ctx, msg))
		_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), msg)
		require.Error(t, err)
		require.Equal(t, before, k.GetParams(ctx))
		if caller == "invalid-bech32" {
			require.Empty(t, msg.GetSigners())
			require.Error(t, msg.ValidateBasic())
		}
	}
	cases := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"quote-nil", func(p *types.Params) { p.VirtualQuoteDefault = sdk.Int{} }},
		{"quote-zero", func(p *types.Params) { p.VirtualQuoteDefault = sdk.ZeroInt() }},
		{"quote-negative", func(p *types.Params) { p.VirtualQuoteDefault = sdk.NewInt(-1) }},
		{"token-zero", func(p *types.Params) { p.VirtualTokenDefault = sdk.ZeroInt() }},
		{"creation-negative", func(p *types.Params) { p.CreationFee = sdk.NewInt(-1) }},
		{"fee-order", func(p *types.Params) { p.MinFeeBps = p.MaxFeeBps + 1 }},
		{"fee-max", func(p *types.Params) { p.MaxFeeBps = 10000 }},
		{"base-fee", func(p *types.Params) { p.BaseFeeBps = p.MaxFeeBps + 1 }},
		{"protocol-fee", func(p *types.Params) { p.ProtocolFeeBps = 5001 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			params := before
			test.mutate(&params)
			msg := &types.MsgUpdateParams{Authority: authority, Params: params}
			require.Error(t, msg.ValidateBasic())
			require.Error(t, parameterRoute(t, router, cdc, ctx, msg))
			_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), msg)
			require.Error(t, err)
			require.Equal(t, before, k.GetParams(ctx))
		})
	}
	_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), nil)
	require.Error(t, err)
	require.Equal(t, before, k.GetParams(ctx))
}

func TestAirdropEntitlements(t *testing.T) {
	app := testkeeper.EVMTestApp

	t.Run("basis", func(t *testing.T) {
		h := newHistoryEnv(t)
		a := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 700_000)
		funded := h.swap(h.trader, h.denom, true, 100_000_000)
		require.Positive(t, funded.PoolCut.Sign())
		eligible := h.balance(h.trader, h.denom).Add(h.balance(a, h.denom))
		record := h.trigger()
		require.Equal(t, eligible, record.EligibleSupply)
		require.Equal(t, uint64(h.ctx.BlockHeight()-1), record.SnapshotBlock)
		require.True(t, record.ClaimedSupply.IsZero())
		require.Equal(t, record.Amount, record.Remaining)
		require.Equal(t, i(700_000), h.pastBalance(h.evmAddress(a), record))
		got, err := h.claim(h.evmAddress(a), record.Epoch)
		require.NoError(t, err)
		require.Equal(t, record.Amount.Mul(i(700_000)).Quo(eligible), got)
	})

	t.Run("transfer after claim", func(t *testing.T) {
		h := newHistoryEnv(t)
		a, b2, c := h.account(0), h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 600_000)
		h.transfer(h.trader, b2, 400_000)
		record := h.nextEpoch()
		epoch := record.Epoch
		traderBalance := h.balance(h.trader, h.denom)
		aShare, err := h.claim(h.evmAddress(a), epoch)
		require.NoError(t, err)
		require.Equal(t, share(record, i(600_000)), aShare)
		h.transfer(a, c, 600_000)
		_, err = h.claim(h.evmAddress(c), epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		require.False(t, h.k.HasClaimedPoolAirdrop(h.ctx, h.pool(h.denom), h.evmAddress(c), epoch))
		// Many transfers among the holders never raise anyone's entitlement.
		for round := 0; round < 8; round++ {
			h.transfer(c, b2, 75_000)
			h.transfer(b2, a, 50_000)
			h.transfer(a, c, 50_000)
		}
		_, err = h.claim(h.evmAddress(a), epoch)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		_, err = h.claim(h.evmAddress(c), epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		bShare, err := h.claim(h.evmAddress(b2), epoch)
		require.NoError(t, err)
		require.Equal(t, share(record, i(400_000)), bShare)
		// The trader is the final eligible claimant and takes the residue.
		traderShare, err := h.claim(h.evmAddress(h.trader), epoch)
		require.NoError(t, err)
		require.True(t, traderShare.GTE(share(record, traderBalance)))
		require.Equal(t, record.Amount, aShare.Add(bShare).Add(traderShare))
		after := h.epochRecord(epoch)
		require.True(t, after.Remaining.IsZero())
		require.Equal(t, record.EligibleSupply, after.ClaimedSupply)
		require.True(t, h.market(h.denom).AirdropBalance.IsZero())
		h.solvent()
	})

	t.Run("swaps after boundary", func(t *testing.T) {
		h := newHistoryEnv(t)
		a := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 900_000)
		record := h.nextEpoch()
		before := h.pastBalance(h.evmAddress(a), record)
		require.Equal(t, i(900_000), before)
		supply := h.tokenSupply(h.token(h.denom))
		// Sells and buys move tokens between the pool and the holders after
		// the boundary; the snapshot does not follow them.
		h.swap(a, h.denom, false, 123_456)
		h.swap(h.trader, h.denom, true, 5_000_000)
		h.transfer(h.trader, a, 1_000)
		require.Equal(t, supply, h.tokenSupply(h.token(h.denom)))
		require.NotEqual(t, before, h.balance(a, h.denom))
		require.Equal(t, before, h.pastBalance(h.evmAddress(a), record))
		sameJSON(t, record, h.epochRecord(record.Epoch))
		got, err := h.claim(h.evmAddress(a), record.Epoch)
		require.NoError(t, err)
		require.Equal(t, share(record, before), got)
	})

	t.Run("refusals are atomic", func(t *testing.T) {
		h := newHistoryEnv(t)
		a := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 800_000)
		_, err := h.claim(h.evmAddress(a), 1)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		record := h.nextEpoch()
		epoch := record.Epoch
		_, err = h.claim(h.evmAddress(a), 0)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		_, err = h.claim(h.evmAddress(a), epoch+1)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		_, _, err = h.k.ClaimAirdropEpoch(h.ctx, h.k.ModuleCaller(), address("unregistered pool"), h.evmAddress(a), epoch)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		want := share(record, i(800_000))

		// The custody cannot pay: the claim fails and consumes nothing.
		custody := h.k.ModuleEVMAddress(h.ctx)
		held := h.tokenBalance(h.usdl, custody)
		sink := address("airdrop sink")
		h.sendToken(h.usdl, custody, sink, held.BigInt())
		marketBefore := h.market(h.denom)
		quoteBefore := h.usdlOf(a)
		_, err = h.claim(h.evmAddress(a), epoch)
		require.Error(t, err)
		require.False(t, h.k.HasClaimedPoolAirdrop(h.ctx, h.pool(h.denom), h.evmAddress(a), epoch))
		sameJSON(t, record, h.epochRecord(epoch))
		sameJSON(t, marketBefore, h.market(h.denom))
		require.Equal(t, quoteBefore, h.usdlOf(a))
		h.sendToken(h.usdl, sink, custody, held.BigInt())

		got, err := h.claim(h.evmAddress(a), epoch)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, quoteBefore.Add(want), h.usdlOf(a))
		_, err = h.claim(h.evmAddress(a), epoch)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		require.Equal(t, record.Remaining.Sub(want), h.epochRecord(epoch).Remaining)
	})

	t.Run("older epoch", func(t *testing.T) {
		h := newHistoryEnv(t)
		a, b2 := h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 500_000)
		first := h.nextEpoch()
		want := share(first, i(500_000))
		h.transfer(a, b2, 500_000)
		second := h.nextEpoch()
		_, err := h.claim(h.evmAddress(a), second.Epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		_, err = h.claim(h.evmAddress(b2), first.Epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		got, err := h.claim(h.evmAddress(a), first.Epoch)
		require.NoError(t, err)
		require.Equal(t, want, got)
		bShare, err := h.claim(h.evmAddress(b2), second.Epoch)
		require.NoError(t, err)
		require.True(t, bShare.IsPositive())
		require.Equal(t, share(second, i(500_000)), bShare)
		require.True(t, h.k.HasClaimedPoolAirdrop(h.ctx, h.pool(h.denom), h.evmAddress(b2), second.Epoch))
		h.solvent()
	})

	t.Run("genesis reload", func(t *testing.T) {
		h := newHistoryEnv(t)
		a, b2 := h.account(0), h.account(0)
		pool := h.pool(h.denom)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 300_000)
		record := h.nextEpoch()
		epoch := record.Epoch
		aShare, err := h.claim(h.evmAddress(a), epoch)
		require.NoError(t, err)
		h.transfer(a, b2, 300_000)
		gs := h.k.ExportGenesis(h.ctx)
		require.NoError(t, gs.Validate())
		require.Len(t, gs.FeeAccounting.Epochs, 1)
		require.Equal(t, record.Remaining.Sub(aShare), gs.FeeAccounting.Epochs[0].Remaining)
		require.Equal(t, i(300_000), gs.FeeAccounting.Epochs[0].ClaimedSupply)
		require.Equal(t, []types.PoolAirdropClaim{{Pool: pool.Hex(), Holder: h.evmAddress(a).Hex(), Epoch: epoch}},
			gs.FeeAccounting.Claims)
		bz, err := json.Marshal(gs)
		require.NoError(t, err)
		var reloaded types.GenesisState
		require.NoError(t, json.Unmarshal(bz, &reloaded))
		require.NoError(t, reloaded.Validate())

		// Import into a fresh branch of the same application state.
		cacheCtx, _ := h.ctx.CacheContext()
		fresh, freshCtx := launchpadtest.NewKeeper(app, cacheCtx)
		fresh.InitGenesis(freshCtx, reloaded)
		sameJSON(t, gs, fresh.ExportGenesis(freshCtx))
		require.True(t, fresh.HasClaimedPoolAirdrop(freshCtx, pool, h.evmAddress(a), epoch))
		_, _, err = fresh.ClaimAirdropEpoch(freshCtx, fresh.ModuleCaller(), pool, h.evmAddress(a), epoch)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		_, _, err = fresh.ClaimAirdropEpoch(freshCtx, fresh.ModuleCaller(), pool, h.evmAddress(b2), epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		_, amount, err := fresh.ClaimAirdropEpoch(freshCtx, fresh.ModuleCaller(), pool, h.evmAddress(h.trader), epoch)
		require.NoError(t, err)
		require.Equal(t, record.Remaining.Sub(aShare), amount)

		bad := *gs
		bad.FeeAccounting.Epochs = []types.PoolAirdropEpoch{gs.FeeAccounting.Epochs[0]}
		bad.FeeAccounting.Epochs[0].Remaining = gs.FeeAccounting.Epochs[0].Amount.AddRaw(1)
		require.ErrorIs(t, bad.Validate(), types.ErrInvalidGenesis)
	})
}
