package keeper_test

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var insuranceGenesisTime = time.Unix(1_800_000_000, 0).UTC()

// insuranceEnv is the perps keeper over the test application's EVM keeper
// and the real pricefeed keeper, each with its own store, one BTC market of class A (lot 1e-5 BTC, maintenance 5
// bps in tier 0) and a fresh mark.
type insuranceEnv struct {
	t        *testing.T
	ctx      sdk.Context
	k        *keeper.Keeper
	key      sdk.StoreKey
	pfKey    sdk.StoreKey
	usdl     common.Address
	sequence uint64
	nextID   uint64
}

func newInsuranceEnv(t *testing.T, mark uint256.Int) *insuranceEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(insuranceGenesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, &app.EvmKeeper, pricefeed)
	ctx = launchpadtest.WithStore(ctx, key)
	e := &insuranceEnv{t: t, ctx: ctx, k: k, key: key, pfKey: pfKey, nextID: 100}
	require.NoError(t, k.SetMarket(ctx, k.Authority(), types.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: types.VenueMaxLeverage, TierClass: types.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, types.USDDecimals),
		OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, types.USDDecimals),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}))
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	e.usdl = params.CollateralToken
	e.setMark(mark)
	word := uint256.NewInt(10_000).Bytes32()
	ctx.KVStore(key).Set(types.SequenceKey(types.SequenceOrderID), word[:])
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePositionID), word[:])
	return e
}

// insuranceUsd is whole USD in 18-decimal fixed point.
func insuranceUsd(whole uint64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(whole), uint256.NewInt(1_000_000_000_000_000_000))
}

func insuranceUsdBig(whole uint64) *big.Int {
	value := insuranceUsd(whole)
	return value.ToBig()
}

// insuranceBase is lots of 1e-5 BTC in 18-decimal base units.
func insuranceBase(lots uint64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(lots), uint256.NewInt(10_000_000_000_000))
}

// setMark stores the market state at mark and the pricefeed aggregate of
// the market at the block time, so its price reads fresh.
func (e *insuranceEnv) setMark(mark uint256.Int) {
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice: sdk.NewIntFromBigInt(mark.ToBig()), MarkPrice: sdk.NewIntFromBigInt(mark.ToBig()),
		Basis: sdk.ZeroInt(), TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1,
	})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
	state := types.MarketState{MarketID: 0, IndexPrice: mark, PricefeedMark: mark, Fresh: true, MarkPrice: mark,
		LastTradePrice: mark, PriceTimestampMs: uint64(e.ctx.BlockTime().UnixMilli())}
	if bz := e.ctx.KVStore(e.key).Get(types.MarketStateKey(0)); bz != nil {
		var stored types.MarketState
		require.NoError(e.t, stored.Unmarshal(bz))
		state.LongOpenInterest, state.ShortOpenInterest = stored.LongOpenInterest, stored.ShortOpenInterest
		state.FundingIndex = stored.FundingIndex
	}
	e.ctx.KVStore(e.key).Set(types.MarketStateKey(0), state.Marshal())
}

func (e *insuranceEnv) at(d time.Duration) {
	e.ctx = e.ctx.WithBlockTime(insuranceGenesisTime.Add(d))
	var state types.MarketState
	require.NoError(e.t, state.Unmarshal(e.ctx.KVStore(e.key).Get(types.MarketStateKey(0))))
	e.setMark(state.MarkPrice)
}

func (e *insuranceEnv) addOpenInterest(size uint256.Int) {
	var state types.MarketState
	require.NoError(e.t, state.Unmarshal(e.ctx.KVStore(e.key).Get(types.MarketStateKey(0))))
	state.LongOpenInterest.Add(&state.LongOpenInterest, &size)
	state.ShortOpenInterest.Add(&state.ShortOpenInterest, &size)
	e.ctx.KVStore(e.key).Set(types.MarketStateKey(0), state.Marshal())
}

func (e *insuranceEnv) address(n uint64) common.Address {
	return common.BigToAddress(new(big.Int).SetUint64(0x6000_0000 + n))
}

// account stores an account holding collateral USDL units and deficitUsd.
func (e *insuranceEnv) account(address common.Address, collateral uint64, deficitUsd uint256.Int) {
	account := types.Account{Address: address, Created: true}
	if bz := e.ctx.KVStore(e.key).Get(types.AccountKey(address)); bz != nil {
		require.NoError(e.t, account.Unmarshal(bz))
	}
	account.Collateral = *uint256.NewInt(collateral)
	account.DeficitUsd = deficitUsd
	e.ctx.KVStore(e.key).Set(types.AccountKey(address), account.Marshal())
	if !deficitUsd.IsZero() {
		books := e.books()
		books.DeficitSumUsd.Add(&books.DeficitSumUsd, &deficitUsd)
		e.ctx.KVStore(e.key).Set(types.VenueBooksKey, books.Marshal())
	}
}

func (e *insuranceEnv) readAccount(address common.Address) types.Account {
	account, err := e.k.LiquidationAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account
}

func (e *insuranceEnv) books() types.VenueBooks {
	books, err := e.k.InsuranceVenueBooks(e.ctx)
	require.NoError(e.t, err)
	return books
}

// position stores a position of size at reference and entry price price with
// its account index and ADL rank entry, and returns its id.
func (e *insuranceEnv) position(id uint64, address common.Address, isLong bool, size, price uint256.Int) uint256.Int {
	positionID := *uint256.NewInt(id)
	var cost uint256.Int
	cost.Mul(&size, &price)
	cost.Div(&cost, uint256.NewInt(1_000_000_000_000_000_000))
	position := types.Position{ID: positionID, Account: address, MarketID: 0, IsLong: isLong, SizeBase: size,
		ReferencePrice: price, CostBasisUsd: cost, EntryPrice: price,
		Leverage: insuranceUsd(100), OpenedAt: uint64(e.ctx.BlockTime().Unix())}
	store := e.ctx.KVStore(e.key)
	store.Set(types.PositionKey(positionID), position.Marshal())
	store.Set(types.AdlRankIndexKey(0, types.SideOf(isLong), position.RankScore, positionID), []byte{})
	word := positionID.Bytes32()
	store.Set(types.AccountMarketPositionKey(address, 0), word[:])
	account := e.readAccount(address)
	account.PositionCount++
	store.Set(types.AccountKey(address), account.Marshal())
	_, err := e.k.LiquidationRebucket(e.ctx, address)
	require.NoError(e.t, err)
	return positionID
}

// rank rewrites the stored rank score of a position and its ADL rank entry.
func (e *insuranceEnv) rank(positionID, score uint256.Int) {
	position := e.readPosition(positionID)
	store := e.ctx.KVStore(e.key)
	side := types.SideOf(position.IsLong)
	store.Delete(types.AdlRankIndexKey(0, side, position.RankScore, positionID))
	position.RankScore = score
	store.Set(types.PositionKey(positionID), position.Marshal())
	store.Set(types.AdlRankIndexKey(0, side, score, positionID), []byte{})
}

// pair stores a position of holder on side isLong and the opposite position
// of a well collateralised counterparty, so both sides of the open interest
// stay equal.
func (e *insuranceEnv) pair(id uint64, holder common.Address, isLong bool, collateral uint64, size, price uint256.Int) uint256.Int {
	e.account(holder, collateral, uint256.Int{})
	counterparty := e.address(9_000_000 + id)
	e.account(counterparty, 100_000_000_000_000, uint256.Int{})
	e.addOpenInterest(size)
	e.position(id+1_000_000, counterparty, !isLong, size, price)
	return e.position(id, holder, isLong, size, price)
}

// bid rests a GTC limit buy of a well collateralised maker in the book.
func (e *insuranceEnv) bid(n uint64, price, size uint256.Int) {
	maker := e.address(n)
	e.account(maker, 100_000_000_000_000, uint256.Int{})
	e.nextID++
	e.sequence++
	orderID := *uint256.NewInt(e.nextID)
	order := types.Order{ID: orderID, Account: maker, MarketID: 0, IsLong: true, Type: types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC, Status: types.OrderStatusResting, LimitPrice: price, SizeBase: size,
		Leverage: insuranceUsd(10), Sequence: e.sequence}
	store := e.ctx.KVStore(e.key)
	store.Set(types.OrderKey(orderID), order.Marshal())
	word := orderID.Bytes32()
	store.Set(types.BookKey(0, types.SideBuy, types.BookPriceKey(true, price), e.sequence), word[:])
	store.Set(types.AccountOrderKey(maker, orderID), []byte{})
	account := e.readAccount(maker)
	account.OpenOrderCount++
	store.Set(types.AccountKey(maker), account.Marshal())
}

func (e *insuranceEnv) readPosition(positionID uint256.Int) types.Position {
	position, err := e.k.LiquidationPosition(e.ctx, positionID)
	require.NoError(e.t, err)
	return position
}

func (e *insuranceEnv) fundPositionID() (uint256.Int, bool) {
	bz := e.ctx.KVStore(e.key).Get(types.AccountMarketPositionKey(e.k.InsuranceAccountAddress(), 0))
	if bz == nil {
		return uint256.Int{}, false
	}
	var id uint256.Int
	id.SetBytes32(bz)
	return id, true
}

func insuranceUnits(value uint64) *big.Int { return new(big.Int).SetUint64(value) }

func insuranceWord(decimal string) uint256.Int {
	value, err := uint256.FromDecimal(decimal)
	if err != nil {
		panic(err)
	}
	return *value
}

func backstopEvent(account common.Address, sizeUsd, mark, marginUsd uint256.Int) types.EventRecord {
	return types.EventRecord{Name: types.EventBackstopLiquidation, Args: []any{
		account, new(big.Int), sizeUsd.ToBig(), mark.ToBig(), marginUsd.ToBig(),
	}}
}

func contributionEvent(usdl common.Address, amount uint64, source uint8) types.EventRecord {
	return types.EventRecord{Name: types.EventInsuranceContribution, Args: []any{usdl, insuranceUnits(amount), source}}
}

func drawEvent(amount, newBalance uint64) types.EventRecord {
	balance := uint256.NewInt(newBalance)
	return types.EventRecord{Name: types.EventInsuranceDraw, Args: []any{new(big.Int), insuranceUnits(amount),
		balance.ToBig()}}
}

func TestInsuranceBackstopTakeover(t *testing.T) {
	mark := insuranceUsd(60_000)
	e := newInsuranceEnv(t, mark)
	fund := e.k.InsuranceAccountAddress()

	// 1 BTC long at 60,000: maintenance 30 USD, backstop line 20 USD.
	healthy := e.address(2)
	e.pair(8, healthy, true, 21_000_000, insuranceBase(100_000), mark)
	_, err := e.k.Backstop(e.ctx, healthy)
	require.ErrorIs(t, err, types.ErrNotLiquidatable, "equity 21 USD is above two thirds of 30 USD")
	require.Equal(t, *uint256.NewInt(21_000_000), e.readAccount(healthy).Collateral)
	require.Equal(t, uint64(1), e.readAccount(healthy).PositionCount)

	holder := e.address(1)
	positionID := e.pair(7, holder, true, 18_000_000, insuranceBase(100_000), mark)
	events, err := e.k.Backstop(e.ctx, holder)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{
		backstopEvent(holder, insuranceUsd(60_000), mark, insuranceUsd(18)),
		contributionEvent(e.usdl, 18_000_000, keeper.InsuranceSourceBackstopMargin),
	}, events)

	store := e.ctx.KVStore(e.key)
	account := e.readAccount(holder)
	require.True(t, account.Collateral.IsZero())
	require.True(t, account.DeficitUsd.IsZero())
	require.Zero(t, account.PositionCount)
	require.Nil(t, store.Get(types.AccountMarketPositionKey(holder, 0)))
	require.False(t, store.Has(types.MarginRatioIndexKey(account.MarginRatioBucket, holder)))

	insurance := e.readAccount(fund)
	require.Equal(t, *uint256.NewInt(18_000_000), insurance.Collateral)
	require.Equal(t, uint64(1), insurance.PositionCount)
	held, ok := e.fundPositionID()
	require.True(t, ok)
	require.Equal(t, positionID, held)
	position := e.readPosition(positionID)
	require.Equal(t, fund, position.Account)
	require.True(t, position.IsLong)
	require.Equal(t, insuranceBase(100_000), position.SizeBase)
	require.Equal(t, mark, position.EntryPrice)
	require.Equal(t, insuranceUsd(60_000), position.CostBasisUsd)
	// Rank (mark / entry) x notional / account value = 60,000 / 18.
	require.Equal(t, insuranceWord("3333333333333333333333"), position.RankScore)
	require.True(t, store.Has(types.AdlRankIndexKey(0, types.SideBuy, position.RankScore, positionID)))
	require.True(t, store.Has(types.MarginRatioIndexKey(insurance.MarginRatioBucket, fund)))

	// A 0.4 BTC short below its line (maintenance 12 USD, equity 7.2 USD) nets
	// against the fund's long: 0.4 BTC leaves both sides of the open interest.
	short := e.address(3)
	shortID := e.pair(9, short, false, 7_200_000, insuranceBase(40_000), mark)
	events, err = e.k.Backstop(e.ctx, short)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{
		{Name: types.EventOpenInterestChanged, Args: []any{new(big.Int), insuranceUsdBig(120_000),
			insuranceUsdBig(120_000), insuranceUsdBig(24_000), false}},
		backstopEvent(short, insuranceUsd(24_000), mark, insuranceWord("7200000000000000000")),
		contributionEvent(e.usdl, 7_200_000, keeper.InsuranceSourceBackstopMargin),
	}, events)
	require.False(t, store.Has(types.PositionKey(shortID)))
	require.Nil(t, store.Get(types.AccountMarketPositionKey(short, 0)))
	position = e.readPosition(positionID)
	require.True(t, position.IsLong)
	require.Equal(t, insuranceBase(60_000), position.SizeBase)
	require.Equal(t, insuranceUsd(36_000), position.CostBasisUsd)
	require.Equal(t, mark, position.EntryPrice)
	insurance = e.readAccount(fund)
	require.Equal(t, *uint256.NewInt(25_200_000), insurance.Collateral)
	require.Equal(t, uint64(1), insurance.PositionCount)
	require.Zero(t, e.readAccount(short).PositionCount)
}

func TestInsuranceClosesBackstop(t *testing.T) {
	mark := insuranceUsd(60_000)
	e := newInsuranceEnv(t, mark)
	holder := e.address(1)
	positionID := e.pair(7, holder, true, 18_000_000, insuranceBase(100_000), mark)
	_, err := e.k.Backstop(e.ctx, holder)
	require.NoError(t, err)

	// No bid: the backstop position waits for liquidity.
	result, err := e.k.CloseBackstopPositions(e.ctx)
	require.NoError(t, err)
	require.Zero(t, result.Slices)
	require.Empty(t, result.Events)
	require.Empty(t, result.Failures)
	require.True(t, e.ctx.KVStore(e.key).Has(types.PositionKey(positionID)))

	// At 61,000 the 61,000 USD position closes in one order: 1,000 USD profit
	// less the 27.45 USD taker fee is booked to the fund.
	e.setMark(insuranceUsd(61_000))
	e.bid(2, insuranceUsd(61_000), insuranceBase(200_000))
	result, err = e.k.CloseBackstopPositions(e.ctx)
	require.NoError(t, err)
	require.Empty(t, result.Failures)
	require.Equal(t, uint64(1), result.Slices)
	require.Len(t, eventsNamed(result.Events, types.EventFill), 1, "one reduce-only IOC order closes against the book")
	require.Equal(t, []types.EventRecord{
		contributionEvent(e.usdl, 972_550_000, keeper.InsuranceSourceBackstopProfit),
	}, eventsNamed(result.Events, types.EventInsuranceContribution))
	require.Empty(t, eventsNamed(result.Events, types.EventInsuranceDraw))
	require.Equal(t, contributionEvent(e.usdl, 972_550_000, keeper.InsuranceSourceBackstopProfit),
		result.Events[len(result.Events)-1])
	_, held := e.fundPositionID()
	require.False(t, held)
	require.False(t, e.ctx.KVStore(e.key).Has(types.PositionKey(positionID)))
	insurance := e.readAccount(e.k.InsuranceAccountAddress())
	require.Equal(t, *uint256.NewInt(990_550_000), insurance.Collateral)
	require.Zero(t, insurance.PositionCount)

	// 3 BTC at 61,000 is 183,000 USD, above the 100,000 USD slice threshold:
	// one 0.6 BTC slice per 30 seconds.
	g := newInsuranceEnv(t, mark)
	large := g.address(1)
	largeID := g.pair(7, large, true, 50_000_000, insuranceBase(300_000), mark)
	_, err = g.k.Backstop(g.ctx, large)
	require.NoError(t, err)
	g.setMark(insuranceUsd(61_000))
	g.bid(2, insuranceUsd(61_000), insuranceBase(300_000))
	result, err = g.k.CloseBackstopPositions(g.ctx)
	require.NoError(t, err)
	require.Empty(t, result.Failures)
	require.Equal(t, uint64(1), result.Slices)
	// The fill settles the whole position at 61,000 (3,000 USD) and charges
	// 0.6 x 61,000 x 450 ppm = 16.47 USD.
	require.Len(t, eventsNamed(result.Events, types.EventFill), 1, "one reduce-only IOC order closes against the book")
	require.Equal(t, []types.EventRecord{
		contributionEvent(g.usdl, 2_983_530_000, keeper.InsuranceSourceBackstopProfit),
	}, eventsNamed(result.Events, types.EventInsuranceContribution))
	require.Empty(t, eventsNamed(result.Events, types.EventInsuranceDraw))
	require.Equal(t, contributionEvent(g.usdl, 2_983_530_000, keeper.InsuranceSourceBackstopProfit),
		result.Events[len(result.Events)-1])
	require.Equal(t, insuranceBase(240_000), g.readPosition(largeID).SizeBase)
	require.Equal(t, types.LiquidationCooldownValue(uint64(insuranceGenesisTime.Unix()), insuranceBase(300_000)),
		g.ctx.KVStore(g.key).Get(types.LiquidationCooldownKey(largeID)))

	g.at(10 * time.Second)
	result, err = g.k.CloseBackstopPositions(g.ctx)
	require.NoError(t, err)
	require.Zero(t, result.Slices, "a second slice waits for the cooldown")
	require.Equal(t, insuranceBase(240_000), g.readPosition(largeID).SizeBase)

	g.at(30 * time.Second)
	result, err = g.k.CloseBackstopPositions(g.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Slices)
	require.Equal(t, insuranceBase(180_000), g.readPosition(largeID).SizeBase)
}

func insuranceUint64(value uint64) []byte {
	return new(big.Int).SetUint64(value).FillBytes(make([]byte, 8))
}

func TestInsuranceCoversDeficit(t *testing.T) {
	e := newInsuranceEnv(t, insuranceUsd(60_000))
	fund := e.k.InsuranceAccountAddress()
	e.account(fund, 1_000_000_000, uint256.Int{})

	closed := e.address(1)
	e.account(closed, 0, insuranceUsd(12))
	events, err := e.k.CoverDeficit(e.ctx, closed, 0)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{drawEvent(12_000_000, 988_000_000)}, events)
	require.Equal(t, uint256.Int{}, e.readAccount(closed).DeficitUsd)
	require.Equal(t, *uint256.NewInt(988_000_000), e.readAccount(fund).Collateral)
	require.Equal(t, uint256.Int{}, e.readAccount(fund).DeficitUsd)
	books := e.books()
	require.True(t, books.DeficitSumUsd.IsZero())
	require.True(t, books.ResidueUsd.IsZero())

	// A deficit of 12.0000005 USD draws 12.000001 USDL rounded up; the
	// 0.0000005 USD above the deficit is residue.
	odd := e.address(2)
	e.account(odd, 0, insuranceWord("12000000500000000000"))
	events, err = e.k.CoverDeficit(e.ctx, odd, 0)
	require.NoError(t, err)
	require.Equal(t, []types.EventRecord{drawEvent(12_000_001, 975_999_999)}, events)
	require.Equal(t, uint256.Int{}, e.readAccount(odd).DeficitUsd)
	books = e.books()
	require.True(t, books.DeficitSumUsd.IsZero())
	require.Equal(t, *uint256.NewInt(500_000_000_000), books.ResidueUsd)

	// An account without a deficit draws nothing.
	events, err = e.k.CoverDeficit(e.ctx, closed, 0)
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, *uint256.NewInt(975_999_999), e.readAccount(fund).Collateral)
}

func TestInsuranceDeficitStartsAdl(t *testing.T) {
	mark := insuranceUsd(60_000)
	e := newInsuranceEnv(t, mark)
	fund := e.k.InsuranceAccountAddress()
	e.account(fund, 500_000_000, uint256.Int{})

	// The fund holds a backstop long of 1 BTC; counterparty short 9 of C,
	// entered at 66,000, is profitable at 60,000 and ranks first.
	e.addOpenInterest(insuranceBase(100_000))
	backstopID := e.position(5, fund, true, insuranceBase(100_000), mark)
	counterparty := e.address(3)
	e.account(counterparty, 12_000_000_000, uint256.Int{})
	counterID := e.position(9, counterparty, false, insuranceBase(100_000), insuranceUsd(66_000))
	e.rank(counterID, insuranceWord("5500000000000000000"))

	closed := e.address(1)
	e.account(closed, 0, insuranceUsd(2_000))
	events, err := e.k.CoverDeficit(e.ctx, closed, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 3)
	require.Equal(t, drawEvent(500_000_000, 0), events[0])
	require.Equal(t, types.EventRecord{Name: types.EventVaultDeficit, Args: []any{e.usdl, insuranceUnits(1_500_000_000)}},
		events[1])
	var executed bool
	for _, event := range events[2:] {
		if event.Name == types.EventADLExecuted {
			executed = true
			require.Equal(t, backstopID.ToBig(), event.Args[0])
		}
	}
	require.True(t, executed, "ADL starts in the same pass")

	require.Equal(t, uint256.Int{}, e.readAccount(closed).DeficitUsd)
	insurance := e.readAccount(fund)
	require.True(t, insurance.Collateral.IsZero())
	require.True(t, insurance.DeficitUsd.IsZero(), "ADL covers the 1,500 USD the fund could not")
	require.Equal(t, uint256.Int{}, e.books().DeficitSumUsd)
}
