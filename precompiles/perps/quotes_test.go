package perps_test

import (
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const quotesNowMs int64 = 1_800_000_000_000

var (
	quotesHolder = common.HexToAddress("0x00000000000000000000000000000000000c0001")
	quotesMaker  = common.HexToAddress("0x00000000000000000000000000000000000c0002")
)

type quotesKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k quotesKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

type quotesEnv struct {
	t        *testing.T
	k        *perpskeeper.Keeper
	ctx      sdk.Context
	perpsKey sdk.StoreKey
	executor perps.PrecompileExecutor
	abi      abi.ABI
}

type openPositionQuote struct {
	EntryPrice           *big.Int
	SizeUsd              *big.Int
	CollateralUsd        *big.Int
	Leverage             *big.Int
	TradingFee           *big.Int
	TradingFeeUsd        *big.Int
	PriceImpact          *big.Int
	LiquidationPrice     *big.Int
	EstimatedFunding24h  *big.Int
	MaintenanceMarginBps *big.Int
}

type closePositionQuote struct {
	ExitPrice       *big.Int
	UnrealizedPnl   *big.Int
	TradingFee      *big.Int
	TradingFeeUsd   *big.Int
	FundingOwed     *big.Int
	NetPnl          *big.Int
	EstimatedPayout *big.Int
}

// newQuotesEnv lists BTC (0) with a lot of 0.00001 and a tick of 0.1 at the
// given mark over the real pricefeed keeper, with a fresh pricefeed price at
// the block time, and builds the perps precompile on the real perps keeper.
func newQuotesEnv(t *testing.T, mark int64) *quotesEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).WithBlockTime(time.UnixMilli(quotesNowMs).UTC())
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := perpskeeper.NewKeeper(perpsKey, &app.EvmKeeper, pricefeed)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), perpstypes.Market{
		ID:                   0,
		Symbol:               "BTC",
		Name:                 "BTC",
		OracleSymbol:         "BTC",
		SzDecimals:           5,
		PxDecimals:           1,
		MaxLeverage:          1000,
		TierClass:            perpstypes.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, perpstypes.USDDecimals),
		OiCapUsd:             sdk.NewIntWithDecimal(500_000_000, perpstypes.USDDecimals),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}))
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(mark, 18),
		MarkPrice:   sdk.NewIntWithDecimal(mark, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(quotesNowMs),
		Height:      ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(t, err)
	ctx.KVStore(pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
	price := usd(mark)
	k.SetMarketState(ctx, perpstypes.MarketState{MarketID: 0, IndexPrice: price, PricefeedMark: price,
		PriceTimestampMs: uint64(quotesNowMs), Fresh: true, MarkPrice: price})

	precompile, err := perps.NewPrecompile(quotesKeepers{Keepers: &utils.EmptyKeepers{}, perps: k})
	require.NoError(t, err)
	executor, ok := precompile.GetExecutor().(perps.PrecompileExecutor)
	require.True(t, ok)
	env := &quotesEnv{t: t, k: k, ctx: ctx, perpsKey: perpsKey, executor: executor, abi: precompile.GetABI()}
	env.fund(quotesMaker, 1_000_000_000_000)
	return env
}

// usd is a whole number of USD (or a price) in 18-decimal fixed point.
func usd(whole int64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(uint64(whole)), uint256.NewInt(1_000_000_000_000_000_000))
}

func fixed(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value, ok := new(big.Rat).SetString(decimal)
	require.True(t, ok, decimal)
	value.Mul(value, new(big.Rat).SetInt(big.NewInt(1_000_000_000_000_000_000)))
	require.True(t, value.IsInt(), decimal)
	var word uint256.Int
	require.False(t, word.SetFromBig(value.Num()))
	return word
}

// fund stores the created account record the deposit leg leaves behind.
func (e *quotesEnv) fund(account common.Address, usdlUnits uint64) {
	e.ctx.KVStore(e.perpsKey).Set(perpstypes.AccountKey(account), perpstypes.Account{
		Address: account, Collateral: *uint256.NewInt(usdlUnits), Created: true,
	}.Marshal())
}

func (e *quotesEnv) account(ctx sdk.Context, address common.Address) perpstypes.Account {
	e.t.Helper()
	bz := ctx.KVStore(e.perpsKey).Get(perpstypes.AccountKey(address))
	require.NotNil(e.t, bz)
	var account perpstypes.Account
	require.NoError(e.t, account.Unmarshal(bz))
	return account
}

// rest places a GTC limit order of the maker that rests in the book.
func (e *quotesEnv) rest(isLong bool, price uint256.Int, sizeBase string) {
	e.t.Helper()
	base := fixed(e.t, sizeBase)
	sizeUsd, overflow := new(uint256.Int).MulDivOverflow(&base, &price, uint256.NewInt(1_000_000_000_000_000_000))
	require.False(e.t, overflow)
	result, err := e.k.PlaceOrder(e.ctx, quotesMaker, perpskeeper.OrderParams{
		MarketID: 0, IsLong: isLong, OrderType: uint8(perpstypes.OrderTypeLimit),
		TimeInForce: uint8(perpstypes.TimeInForceGTC), SizeUsd: *sizeUsd, Leverage: usd(10), LimitPrice: price,
	})
	require.NoError(e.t, err)
	require.Empty(e.t, result.Fills)
}

// holdLong stores the holder's long as execution leaves it: the position,
// its account and market index entries, the account's position count and
// the market's long open interest.
func (e *quotesEnv) holdLong(id uint64, sizeBase string, entry uint256.Int, collateral uint64) perpstypes.Position {
	e.t.Helper()
	base := fixed(e.t, sizeBase)
	cost, overflow := new(uint256.Int).MulDivOverflow(&base, &entry, uint256.NewInt(1_000_000_000_000_000_000))
	require.False(e.t, overflow)
	position := perpstypes.Position{
		ID: *uint256.NewInt(id), Account: quotesHolder, MarketID: 0, IsLong: true, SizeBase: base,
		ReferencePrice: entry, CostBasisUsd: *cost, EntryPrice: entry, Leverage: usd(10),
		OpenedAt: uint64(quotesNowMs / 1000),
	}
	store := e.ctx.KVStore(e.perpsKey)
	store.Set(perpstypes.PositionKey(position.ID), position.Marshal())
	idBytes := position.ID.Bytes32()
	store.Set(perpstypes.AccountMarketPositionKey(quotesHolder, 0), idBytes[:])
	store.Set(perpstypes.AdlRankIndexKey(0, perpstypes.SideOf(true), position.RankScore, position.ID), []byte{})
	store.Set(perpstypes.AccountKey(quotesHolder), perpstypes.Account{
		Address: quotesHolder, Collateral: *uint256.NewInt(collateral), PositionCount: 1, Created: true,
	}.Marshal())
	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(e.t, err)
	state.LongOpenInterest = base
	e.k.SetMarketState(e.ctx, state)
	return position
}

func (e *quotesEnv) setFundingIndex(index uint256.Int) {
	e.t.Helper()
	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(e.t, err)
	state.FundingIndex = index
	e.k.SetMarketState(e.ctx, state)
}

// storeHash digests every key and value of the perps store.
func (e *quotesEnv) storeHash() [32]byte {
	iterator := e.ctx.KVStore(e.perpsKey).Iterator(nil, nil)
	defer iterator.Close()
	digest := sha256.New()
	for ; iterator.Valid(); iterator.Next() {
		digest.Write(iterator.Key())
		digest.Write(iterator.Value())
	}
	var sum [32]byte
	copy(sum[:], digest.Sum(nil))
	return sum
}

// call runs one quote as an eth_call of caller through the precompile
// executor, after the arguments are encoded and decoded by the ABI.
func (e *quotesEnv) call(caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	method, ok := e.abi.Methods[name]
	require.True(e.t, ok, name)
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	decoded, err := method.Inputs.Unpack(packed)
	require.NoError(e.t, err)
	ret, err := e.executor.Execute(e.ctx, &method, caller, caller, decoded, big.NewInt(0), true, nil, nil)
	require.NoError(e.t, err)
	out, err := method.Outputs.Unpack(ret)
	require.NoError(e.t, err)
	return out
}

// execute places the reduce-only MARKET IOC close order the keeper builds for
// the holder's position on a branch of the state and returns the branch.
func (e *quotesEnv) execute(order perpstypes.Order) (sdk.Context, perpskeeper.PlaceResult) {
	e.t.Helper()
	branch, _ := e.ctx.CacheContext()
	result, err := e.k.PlaceOrder(branch, order.Account, perpskeeper.OrderParams{
		MarketID: order.MarketID, IsLong: order.IsLong, OrderType: uint8(order.Type),
		TimeInForce: uint8(order.TimeInForce), SizeUsd: order.SizeUsd, Leverage: order.Leverage, ReduceOnly: true,
	})
	require.NoError(e.t, err)
	require.NotEmpty(e.t, result.Fills)
	return branch, result
}

func bigOf(t *testing.T, decimal string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(decimal, 10)
	require.True(t, ok, decimal)
	return value
}

func TestQuotesQuoteOpenPositionWalksBook(t *testing.T) {
	env := newQuotesEnv(t, 60_000)
	env.rest(false, usd(60_000), "0.01")
	env.rest(false, usd(60_100), "0.01")
	params, err := env.k.GetParams(env.ctx)
	require.NoError(t, err)

	before := env.storeHash()
	out := env.call(quotesHolder, "quoteOpenPosition", big.NewInt(0), params.CollateralToken, big.NewInt(100_000_000),
		usd(10).ToBig(), true)
	require.Equal(t, before, env.storeHash())
	require.Len(t, out, 1)
	quote := *abi.ConvertType(out[0], new(openPositionQuote)).(*openPositionQuote)

	// N = 100 x 10 / (1 + 10 x 0.00045) = 995.52 USD sizes 0.01650 BTC at the
	// 60,300 bound; the walk fills 0.01 at 60,000 and 0.0065 at 60,100.
	entry := bigOf(t, "60039393939393939393939")
	require.Equal(t, entry, quote.EntryPrice)
	require.Equal(t, fixed(t, "990.65").ToBig(), quote.SizeUsd)
	require.Equal(t, usd(100).ToBig(), quote.CollateralUsd)
	require.Equal(t, usd(10).ToBig(), quote.Leverage)
	require.Equal(t, big.NewInt(445_793), quote.TradingFee)
	require.Equal(t, fixed(t, "0.4457925").ToBig(), quote.TradingFeeUsd)
	mark := usd(60_000).ToBig()
	impact := new(big.Int).Sub(entry, mark)
	require.Equal(t, impact.Mul(impact, big.NewInt(10_000)).Quo(impact, mark), quote.PriceImpact)
	require.Equal(t, big.NewInt(5), quote.MaintenanceMarginBps)
	require.Zero(t, quote.EstimatedFunding24h.Sign())

	// Section 14 at mark: collateral 100 USDL less the 445,793-unit fee, PnL
	// 0.0165 x 60,000 - 990.65 = -0.65 USD and maintenance 990 x 0.0005 leave
	// 98.409207 USD above maintenance; the price is mark - that / size / (1 -
	// 0.0005), rounded down.
	available := new(big.Rat).Sub(new(big.Rat).SetFrac64(99_554_207, 1_000_000), new(big.Rat).SetFrac64(65, 100))
	available.Sub(available, new(big.Rat).SetFrac64(495, 1000))
	drop := new(big.Rat).Quo(available, new(big.Rat).Mul(new(big.Rat).SetFrac64(165, 10_000),
		new(big.Rat).SetFrac64(9995, 10_000)))
	liquidation := new(big.Rat).Sub(new(big.Rat).SetInt64(60_000), drop)
	liquidation.Mul(liquidation, new(big.Rat).SetInt(big.NewInt(1_000_000_000_000_000_000)))
	require.Equal(t, new(big.Int).Quo(liquidation.Num(), liquidation.Denom()), quote.LiquidationPrice)

	// openPosition with the same arguments: the deposit leg credits 100 USDL
	// and the MARKET IOC order of 0.0165 BTC at mark fills what the quote said.
	branch, _ := env.ctx.CacheContext()
	branch.KVStore(env.perpsKey).Set(perpstypes.AccountKey(quotesHolder), perpstypes.Account{
		Address: quotesHolder, Collateral: *uint256.NewInt(100_000_000), Created: true,
	}.Marshal())
	result, err := env.k.PlaceOrder(branch, quotesHolder, perpskeeper.OrderParams{
		MarketID: 0, IsLong: true, OrderType: uint8(perpstypes.OrderTypeMarket),
		TimeInForce: uint8(perpstypes.TimeInForceIOC), SizeUsd: usd(990), Leverage: usd(10),
	})
	require.NoError(t, err)
	require.Len(t, result.Fills, 2)
	filledUsd := new(big.Int)
	filledFee := new(big.Int)
	for _, fill := range result.Fills {
		filledUsd.Add(filledUsd, fill.SizeUsd.ToBig())
		filledFee.Add(filledFee, fill.TakerFeeUsd.ToBig())
	}
	require.Equal(t, quote.SizeUsd, filledUsd)
	require.Equal(t, quote.TradingFeeUsd, filledFee)
	positionID := result.Fills[1].TakerPositionID
	bz := branch.KVStore(env.perpsKey).Get(perpstypes.PositionKey(positionID))
	require.NotNil(t, bz)
	var position perpstypes.Position
	require.NoError(t, position.Unmarshal(bz))
	require.Equal(t, fixed(t, "0.0165"), position.SizeBase)
	require.Equal(t, quote.EntryPrice, position.EntryPrice.ToBig())
	require.Equal(t, uint256.NewInt(100_000_000-445_793), &env.account(branch, quotesHolder).Collateral)
}

func TestQuotesQuoteClosePosition(t *testing.T) {
	env := newQuotesEnv(t, 61_000)
	env.rest(true, usd(61_000), "1")
	env.holdLong(7, "0.01", usd(60_000), 100_000_000)
	// The index rose 100 USD per BTC since the position's funding basis of
	// 0: the long owes 0.01 x 100 = 1 USD.
	env.setFundingIndex(usd(100))

	before := env.storeHash()
	out := env.call(quotesMaker, "quoteClosePosition", big.NewInt(7))
	require.Equal(t, before, env.storeHash())
	require.Len(t, out, 1)
	quote := *abi.ConvertType(out[0], new(closePositionQuote)).(*closePositionQuote)

	require.Equal(t, usd(61_000).ToBig(), quote.ExitPrice)
	require.Equal(t, usd(10).ToBig(), quote.UnrealizedPnl)
	require.Equal(t, fixed(t, "0.2745").ToBig(), quote.TradingFeeUsd)
	require.Equal(t, big.NewInt(274_500), quote.TradingFee)
	require.Equal(t, usd(1).ToBig(), new(big.Int).Neg(quote.FundingOwed))
	require.Equal(t, fixed(t, "8.7255").ToBig(), quote.NetPnl)
	require.Equal(t, big.NewInt(108_725_500), quote.EstimatedPayout)

	order, err := env.k.ClosePosition(env.ctx, quotesHolder, *uint256.NewInt(7))
	require.NoError(t, err)
	branch, result := env.execute(order)
	require.Len(t, result.Fills, 1)
	require.Equal(t, quote.ExitPrice, result.Fills[0].Price.ToBig())
	require.Equal(t, quote.TradingFeeUsd, result.Fills[0].TakerFeeUsd.ToBig())
	require.Equal(t, quote.EstimatedPayout, env.account(branch, quotesHolder).Collateral.ToBig())
	require.Nil(t, branch.KVStore(env.perpsKey).Get(perpstypes.PositionKey(*uint256.NewInt(7))))
}

func TestQuotesQuotePartialCloseMatchesExecution(t *testing.T) {
	env := newQuotesEnv(t, 61_000)
	env.rest(true, usd(61_000), "1")
	env.holdLong(7, "0.02", usd(60_000), 100_000_000)

	before := env.storeHash()
	out := env.call(quotesHolder, "quotePartialClose", big.NewInt(7), usd(500).ToBig())
	require.Equal(t, before, env.storeHash())
	require.Len(t, out, 4)
	exitPrice, closedPnl, fee, payout := out[0].(*big.Int), out[1].(*big.Int), out[2].(*big.Int), out[3].(*big.Int)

	// 500 USD at mark 61,000 is 0.00819 BTC after the lot rounding; it fills
	// at 61,000 for 499.59 USD, closing 0.00819 x (61,000 - 60,000) of PnL at
	// the 450 ppm taker rate.
	require.Equal(t, usd(61_000).ToBig(), exitPrice)
	require.Equal(t, fixed(t, "8.19").ToBig(), closedPnl)
	require.Equal(t, fixed(t, "0.2248155").ToBig(), fee)

	order, err := env.k.PartialClose(env.ctx, quotesHolder, *uint256.NewInt(7), usd(500))
	require.NoError(t, err)
	require.Equal(t, fixed(t, "0.00819"), order.SizeBase)
	branch, result := env.execute(order)
	require.Len(t, result.Fills, 1)
	require.Equal(t, exitPrice, result.Fills[0].Price.ToBig())
	require.Equal(t, fee, result.Fills[0].TakerFeeUsd.ToBig())
	credited := new(big.Int).Sub(env.account(branch, quotesHolder).Collateral.ToBig(), big.NewInt(100_000_000))
	require.Equal(t, credited, payout)
	bz := branch.KVStore(env.perpsKey).Get(perpstypes.PositionKey(*uint256.NewInt(7)))
	require.NotNil(t, bz)
	var position perpstypes.Position
	require.NoError(t, position.Unmarshal(bz))
	require.Equal(t, fixed(t, "0.01181"), position.SizeBase)
}
