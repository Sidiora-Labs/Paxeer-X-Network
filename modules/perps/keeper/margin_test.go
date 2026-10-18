package keeper_test

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type marginEnv struct {
	k   *keeper.Keeper
	ctx sdk.Context
	key sdk.StoreKey
}

func newMarginEnv(t *testing.T) marginEnv {
	t.Helper()
	k, ctx, key := newKeeper(t)
	return marginEnv{k: k, ctx: ctx, key: key}
}

func (env marginEnv) list(t *testing.T, id uint64, symbol string, sz, px uint8, class types.TierClass) {
	t.Helper()
	capUsd := int64(100_000_000)
	if class == types.TierClassE {
		capUsd = 0
	}
	require.NoError(t, env.k.SetMarket(env.ctx, env.k.Authority(), listedMarket(id, symbol, sz, px, class, 6_000, capUsd)))
}

func (env marginEnv) setPrices(marketID uint64, mark, fundingIndex uint256.Int) {
	env.k.SetMarketState(env.ctx, types.MarketState{
		MarketID: marketID, IndexPrice: mark, PricefeedMark: mark, Fresh: true, MarkPrice: mark, FundingIndex: fundingIndex,
	})
}

func (env marginEnv) putAccount(t *testing.T, account common.Address, collateralUnits uint64) {
	t.Helper()
	env.ctx.KVStore(env.key).Set(types.AccountKey(account), types.Account{
		Address: account, Collateral: *uint256.NewInt(collateralUnits), Created: true,
	}.Marshal())
}

func (env marginEnv) storedAccount(t *testing.T, account common.Address) types.Account {
	t.Helper()
	var stored types.Account
	require.NoError(t, stored.Unmarshal(env.ctx.KVStore(env.key).Get(types.AccountKey(account))))
	return stored
}

// putPosition stores a position opened at price with cost basis size x price
// / 1e18 and funding basis size x fundingIndex / 1e18, with its account index.
func (env marginEnv) putPosition(t *testing.T, id uint64, account common.Address, marketID uint64, isLong bool, size, price, fundingIndex, leverage uint256.Int) types.Position {
	t.Helper()
	cost := new(big.Int).Mul(size.ToBig(), price.ToBig())
	cost.Quo(cost, big.NewInt(1e18))
	basis := new(big.Int).Mul(size.ToBig(), fundingIndex.ToBig())
	basis.Quo(basis, big.NewInt(1e18))
	position := types.Position{
		ID: *uint256.NewInt(id), Account: account, MarketID: marketID, IsLong: isLong, SizeBase: size,
		ReferencePrice: price, CostBasisUsd: *uint256.MustFromBig(cost), FundingBasis: *uint256.MustFromBig(basis),
		EntryPrice: price, Leverage: leverage, OpenedAt: uint64(listingTime),
	}
	store := env.ctx.KVStore(env.key)
	store.Set(types.PositionKey(position.ID), position.Marshal())
	word := position.ID.Bytes32()
	store.Set(types.AccountMarketPositionKey(account, marketID), word[:])
	return position
}

func (env marginEnv) putOrder(t *testing.T, id uint64, account common.Address, status types.OrderStatus, size, limit, leverage uint256.Int) {
	t.Helper()
	order := types.Order{
		ID: *uint256.NewInt(id), Account: account, IsLong: true, Type: types.OrderTypeLimit, Status: status,
		LimitPrice: limit, SizeBase: size, Leverage: leverage,
	}
	store := env.ctx.KVStore(env.key)
	store.Set(types.OrderKey(order.ID), order.Marshal())
	store.Set(types.AccountOrderKey(account, order.ID), []byte{})
}

func (env marginEnv) open(account common.Address, marketID uint64, isLong bool, notional sdk.Int, leverage uint256.Int) error {
	return env.k.CheckOpen(env.ctx, keeper.OpenRequest{
		Account: account, MarketID: marketID, IsLong: isLong, NotionalUsd: notional, Leverage: leverage, TakerFeePpm: 450,
	})
}

func usdWhole(n int64) sdk.Int { return sdk.NewIntWithDecimal(n, types.USDDecimals) }

func usdWei(t *testing.T, wei string) sdk.Int {
	t.Helper()
	value, ok := sdk.NewIntFromString(wei)
	require.True(t, ok, wei)
	return value
}

func requireUsd(t *testing.T, want, got sdk.Int, label string) {
	t.Helper()
	require.True(t, want.Equal(got), "%s: want %s got %s", label, want, got)
}

func marginAccount(n uint64) common.Address {
	return common.BigToAddress(new(big.Int).SetUint64(0xa000 + n))
}

func TestMarginTieredMaintenance(t *testing.T) {
	env := newMarginEnv(t)
	env.list(t, 0, "BTC", 5, 1, types.TierClassA)
	env.list(t, 1, "ETH", 4, 2, types.TierClassA)
	env.setPrices(0, e18(50_000), uint256.Int{})
	tenX := e18(10)

	a := marginAccount(1)
	env.putAccount(t, a, 300_000_000_000)
	long := env.putPosition(t, 1, a, 0, true, e18(40), e18(50_000), uint256.Int{}, tenX)

	cases := []struct {
		name      string
		isLong    bool
		size      uint256.Int
		wantWhole int64
	}{
		{"tier 0 at 5 bps", true, e18(1), 25},
		{"tier boundary is continuous", true, e18(20), 500},
		{"2,000,000 USD at tier 1", true, e18(40), 1_500},
		{"tier 4 short", false, e18(1_200), 317_000},
	}
	for _, tc := range cases {
		position := types.Position{MarketID: 0, IsLong: tc.isLong, SizeBase: tc.size, Leverage: tenX}
		mm, err := env.k.MaintenanceMargin(env.ctx, position)
		require.NoError(t, err, tc.name)
		requireUsd(t, usdWhole(tc.wantWhole), mm, tc.name)
	}

	mm, err := env.k.MaintenanceMargin(env.ctx, long)
	require.NoError(t, err)
	requireUsd(t, usdWhole(1_500), mm, "stored long")
	require.False(t, mm.Equal(usdWhole(1_000)), "the single 5 bps rate is replaced by the tier")

	summary, err := env.k.AccountMargin(env.ctx, a)
	require.NoError(t, err)
	require.Equal(t, uint64(1), summary.Positions)
	requireUsd(t, usdWhole(300_000), summary.Equity, "equity at entry")
	requireUsd(t, usdWhole(200_000), summary.InitialMargin, "initial margin 2,000,000 / 10")
	requireUsd(t, usdWhole(1_500), summary.MaintenanceMargin, "maintenance")

	env.setPrices(0, e18(49_000), uint256.Int{})
	equity, err := env.k.Equity(env.ctx, a)
	require.NoError(t, err)
	requireUsd(t, usdWhole(260_000), equity, "equity after a 1,000 USD drop on 40 BTC")
	mm, err = env.k.MaintenanceMargin(env.ctx, long)
	require.NoError(t, err)
	requireUsd(t, usdWhole(1_460), mm, "maintenance at the new mark")

	_, err = env.k.MaintenanceMargin(env.ctx, types.Position{MarketID: 1, IsLong: true, SizeBase: e18(1), Leverage: tenX})
	require.ErrorIs(t, err, types.ErrPriceStale)
	_, err = env.k.MaintenanceMargin(env.ctx, types.Position{MarketID: 9, IsLong: true, SizeBase: e18(1), Leverage: tenX})
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}

func TestMarginTierLeverageCap(t *testing.T) {
	env := newMarginEnv(t)
	env.list(t, 0, "BTC", 5, 1, types.TierClassA)
	env.list(t, 4, "LINK", 1, 3, types.TierClassF)
	env.setPrices(0, e18(50_000), uint256.Int{})
	env.setPrices(4, e18(20), uint256.Int{})

	a := marginAccount(1)
	env.putAccount(t, a, 5_000_000_000)
	err := env.open(a, 0, true, usdWhole(2_000_000), e18(1000))
	require.ErrorIs(t, err, types.ErrLeverageTooHigh)
	require.Contains(t, err.Error(), "LXP_PERPS_LEVERAGE_TOO_HIGH")
	require.NoError(t, env.open(a, 0, true, usdWhole(2_000_000), e18(500)))
	over := e18(500)
	over.AddUint64(&over, 1)
	require.ErrorIs(t, env.open(a, 0, true, usdWhole(2_000_000), over), types.ErrLeverageTooHigh)
	require.ErrorIs(t, env.open(a, 4, true, usdWhole(1_000), e18(1001)), types.ErrLeverageTooHigh)

	c := marginAccount(3)
	env.putAccount(t, c, 100_000_000_000)
	below := usdWhole(1_000_000)
	below = below.Sub(sdk.OneInt())
	require.NoError(t, env.open(c, 0, true, below, e18(1000)))
	require.ErrorIs(t, env.open(c, 0, true, usdWhole(1_000_000), e18(1000)), types.ErrLeverageTooHigh)

	b := marginAccount(2)
	env.putAccount(t, b, 100_000_000_000)
	env.putPosition(t, 1, b, 0, true, e18(18), e18(50_000), uint256.Int{}, e18(1000))
	require.ErrorIs(t, env.open(b, 0, true, usdWhole(200_000), e18(1000)), types.ErrLeverageTooHigh)
	require.NoError(t, env.open(b, 0, false, usdWhole(200_000), e18(1000)))
	require.ErrorIs(t, env.open(b, 0, false, usdWhole(2_000_000), e18(1000)), types.ErrLeverageTooHigh)

	half := e18(1)
	half.Rsh(&half, 1)
	require.ErrorIs(t, env.open(a, 0, true, usdWhole(1_000), half), types.ErrInvalidParams)
	require.ErrorIs(t, env.open(a, 0, true, sdk.ZeroInt(), e18(10)), types.ErrZeroAmount)
	require.ErrorIs(t, env.open(a, 7, true, usdWhole(1_000), e18(10)), types.ErrUnknownMarket)
	err = env.open(marginAccount(9), 0, true, usdWhole(1_000), e18(10))
	require.ErrorIs(t, err, types.ErrNoAccount)
	require.Contains(t, err.Error(), "LXP_PERPS_NO_ACCOUNT")
}

func TestMarginOpeningFeeCovered(t *testing.T) {
	env := newMarginEnv(t)
	env.list(t, 0, "BTC", 5, 1, types.TierClassA)
	env.setPrices(0, e18(60_000), uint256.Int{})

	short := marginAccount(1)
	env.putAccount(t, short, 1_440_000)
	err := env.open(short, 0, true, usdWhole(1_000), e18(1000))
	require.ErrorIs(t, err, types.ErrInsufficientMargin)
	require.Contains(t, err.Error(), "LXP_PERPS_INSUFFICIENT_MARGIN")

	enough := marginAccount(2)
	env.putAccount(t, enough, 1_450_000)
	require.NoError(t, env.open(enough, 0, true, usdWhole(1_000), e18(1000)))
	im, err := keeper.InitialMargin(usdWhole(1_000), e18(1000))
	require.NoError(t, err)
	requireUsd(t, usdWhole(1), im, "initial margin of 1,000 USD at 1000x")

	env.putOrder(t, 1, enough, types.OrderStatusUntriggered, e18(1), e18(10), e18(1000))
	require.NoError(t, env.open(enough, 0, true, usdWhole(1_000), e18(1000)), "an untriggered order holds no margin")
	env.putOrder(t, 2, enough, types.OrderStatusResting, e18(1), e18(10), e18(1000))
	summary, err := env.k.AccountMargin(env.ctx, enough)
	require.NoError(t, err)
	requireUsd(t, usdWei(t, "10000000000000000"), summary.InitialMargin, "resting order margin 10 / 1000")
	require.ErrorIs(t, env.open(enough, 0, true, usdWhole(1_000), e18(1000)), types.ErrInsufficientMargin)

	n, err := keeper.CompatibilityNotional(usdWei(t, "1450000000000000000"), e18(1000), 450)
	require.NoError(t, err)
	requireUsd(t, usdWhole(1_000), n, "1.45 USD at 1000x and 4.5 bps sizes 1,000 USD")
	n, err = keeper.CompatibilityNotional(usdWhole(100), e18(10), 450)
	require.NoError(t, err)
	requireUsd(t, usdWei(t, "995520159283225485316"), n, "100 USD at 10x and 4.5 bps")

	sized := marginAccount(3)
	env.putAccount(t, sized, 100_000_000)
	require.NoError(t, env.open(sized, 0, true, n, e18(10)))
	require.ErrorIs(t, env.open(sized, 0, true, n.Add(usdWei(t, "1000000000000")), e18(10)), types.ErrInsufficientMargin)
}

func TestMarginThousandXConsequences(t *testing.T) {
	markets := []struct {
		id     uint64
		symbol string
		sz, px uint8
		class  types.TierClass
	}{
		{0, "BTC", 5, 1, types.TierClassA}, {1, "ETH", 4, 2, types.TierClassA},
		{2, "SOL", 2, 3, types.TierClassB}, {3, "AVAX", 2, 3, types.TierClassB},
		{4, "LINK", 1, 3, types.TierClassF}, {5, "TSLA", 3, 2, types.TierClassD},
		{6, "NVDA", 3, 2, types.TierClassD}, {7, "NAS100", 4, 1, types.TierClassC},
		{8, "XAU", 4, 1, types.TierClassC}, {9, "SPX500", 4, 1, types.TierClassC},
		{10, "GOOGL", 3, 2, types.TierClassD}, {11, "PAX", 0, 5, types.TierClassE},
		{12, "SID", 0, 5, types.TierClassE}, {13, "HYPE", 2, 3, types.TierClassB},
		{14, "XRP", 0, 4, types.TierClassB}, {15, "ASTER", 0, 5, types.TierClassE},
		{16, "TRUMP", 1, 3, types.TierClassE}, {17, "BNB", 3, 2, types.TierClassB},
	}
	env := newMarginEnv(t)
	thousandX := e18(1000)
	notional := usdWhole(10_000)
	takerFee := notional.MulRaw(450).QuoRaw(types.PpmDenominator)
	requireUsd(t, usdWei(t, "4500000000000000000"), takerFee, "tier 0 taker fee on 10,000 USD")
	requireUsd(t, usdWhole(9), takerFee.MulRaw(2), "9 bps taker round trip")
	params := types.DefaultParams()
	require.Equal(t, uint64(450), params.FeeTiers[0].TakerFeePpm)
	require.Equal(t, uint64(40_000), params.FundingCapPpmPerHour)

	for _, m := range markets {
		env.list(t, m.id, m.symbol, m.sz, m.px, m.class)
		market, err := env.k.GetMarket(env.ctx, m.id)
		require.NoError(t, err)
		tiers, err := types.TiersFor(market)
		require.NoError(t, err)
		require.Equal(t, uint64(1000), tiers[0].MaxLeverage, m.symbol)
		require.Equal(t, uint64(5), tiers[0].MaintenanceBps, "%s MMR 5 bps", m.symbol)
		require.Equal(t, uint64(40_000), market.FundingCapPpmPerHour, m.symbol)
		require.Equal(t, uint64(40*types.PpmDenominator/1000), market.FundingCapPpmPerHour, "%s cap is 40 x IMR", m.symbol)

		price := e18(100)
		env.setPrices(m.id, price, uint256.Int{})
		im, err := keeper.InitialMargin(notional, thousandX)
		require.NoError(t, err)
		requireUsd(t, usdWhole(10), im, m.symbol+" IMR 10 bps")
		mm, err := env.k.MaintenanceMargin(env.ctx, types.Position{MarketID: m.id, IsLong: true, SizeBase: e18(100), Leverage: thousandX})
		require.NoError(t, err)
		requireUsd(t, usdWhole(5), mm, m.symbol+" MMR 5 bps")
		requireUsd(t, usdWhole(5), im.Sub(mm), m.symbol+" 5 bps buffer")
		require.True(t, takerFee.MulRaw(2).GT(im.Sub(mm)), "%s round trip exceeds the buffer", m.symbol)

		opener := marginAccount(100 + m.id)
		openNotional := usdWhole(5_000)
		env.putAccount(t, opener, 7_249_999)
		require.ErrorIs(t, env.open(opener, m.id, true, openNotional, thousandX), types.ErrInsufficientMargin, m.symbol)
		env.putAccount(t, opener, 7_250_000)
		require.NoError(t, env.open(opener, m.id, true, openNotional, thousandX), "%s opens with 14.5 bps", m.symbol)

		holder := marginAccount(200 + m.id)
		env.putAccount(t, holder, 10_000_000)
		env.putPosition(t, 1+m.id, holder, m.id, true, e18(100), price, uint256.Int{}, thousandX)
		liquidatable, err := env.k.IsLiquidatable(env.ctx, holder)
		require.NoError(t, err)
		require.False(t, liquidatable, m.symbol)

		env.setPrices(m.id, wordOf(t, "99950000000000000000"), uint256.Int{})
		summary, err := env.k.AccountMargin(env.ctx, holder)
		require.NoError(t, err)
		requireUsd(t, usdWhole(5), summary.Equity, m.symbol+" equity after a 5 bps move")
		requireUsd(t, usdWei(t, "4997500000000000000"), summary.MaintenanceMargin, m.symbol+" maintenance after a 5 bps move")
		liquidatable, err = env.k.IsLiquidatable(env.ctx, holder)
		require.NoError(t, err)
		require.False(t, liquidatable, "%s at maintenance after 5 bps", m.symbol)
		env.setPrices(m.id, wordOf(t, "99949000000000000000"), uint256.Int{})
		liquidatable, err = env.k.IsLiquidatable(env.ctx, holder)
		require.NoError(t, err)
		require.True(t, liquidatable, "%s below maintenance past 5 bps", m.symbol)

		cappedStep := new(big.Int).Mul(price.ToBig(), big.NewInt(int64(market.FundingCapPpmPerHour)))
		cappedStep.Quo(cappedStep, big.NewInt(types.PpmDenominator))
		env.setPrices(m.id, price, *uint256.MustFromBig(cappedStep))
		summary, err = env.k.AccountMargin(env.ctx, holder)
		require.NoError(t, err)
		requireUsd(t, usdWhole(-390), summary.Equity, m.symbol+" equity owes one capped funding step")
		liquidatable, err = env.k.IsLiquidatable(env.ctx, holder)
		require.NoError(t, err)
		require.True(t, liquidatable, "%s liquidatable in the block the index advances", m.symbol)
		require.Equal(t, uint16(0), env.storedAccount(t, holder).MarginRatioBucket)
	}

	a := marginAccount(1)
	env.putAccount(t, a, 10_000_000)
	size := wordOf(t, "166660000000000000")
	env.setPrices(0, e18(60_000), uint256.Int{})
	env.putPosition(t, 100, a, 0, true, size, e18(60_000), uint256.Int{}, thousandX)
	_, err := env.k.RefreshMarginRatio(env.ctx, a)
	require.NoError(t, err)
	require.Equal(t, uint16(200), env.storedAccount(t, a).MarginRatioBucket)
	store := env.ctx.KVStore(env.key)
	require.True(t, store.Has(types.MarginRatioIndexKey(200, a)))

	env.setPrices(0, e18(59_960), uint256.Int{})
	summary, err := env.k.AccountMargin(env.ctx, a)
	require.NoError(t, err)
	requireUsd(t, usdWei(t, "3333600000000000000"), summary.Equity, "equity after the 40 USD drop")
	requireUsd(t, usdWei(t, "4996466800000000000"), summary.MaintenanceMargin, "maintenance at 59,960")
	liquidatable, err := env.k.IsLiquidatable(env.ctx, a)
	require.NoError(t, err)
	require.True(t, liquidatable)
	require.Equal(t, uint16(66), env.storedAccount(t, a).MarginRatioBucket)
	require.True(t, store.Has(types.MarginRatioIndexKey(66, a)))
	require.False(t, store.Has(types.MarginRatioIndexKey(200, a)))

	store.Delete(types.AccountMarketPositionKey(a, 0))
	summary, err = env.k.RefreshMarginRatio(env.ctx, a)
	require.NoError(t, err)
	require.Equal(t, uint64(0), summary.Positions)
	require.False(t, store.Has(types.MarginRatioIndexKey(66, a)), "an account without a position leaves the index")
	require.False(t, store.Has(types.MarginRatioIndexKey(0, a)))
}

func TestMarginCrossOnly(t *testing.T) {
	env := newMarginEnv(t)
	require.Equal(t, uint8(0), keeper.MarginModeCross)
	a := marginAccount(1)
	env.putAccount(t, a, 1_000_000)
	before := env.ctx.KVStore(env.key).Get(types.AccountKey(a))

	err := env.k.SetMarginMode(env.ctx, a, 1)
	require.ErrorIs(t, err, types.ErrCrossMarginOnly)
	require.Contains(t, err.Error(), "LXP_PERPS_CROSS_MARGIN_ONLY")
	require.Equal(t, before, env.ctx.KVStore(env.key).Get(types.AccountKey(a)))

	require.ErrorIs(t, env.k.SetMarginMode(env.ctx, a, 2), types.ErrCrossMarginOnly)
	require.NoError(t, env.k.SetMarginMode(env.ctx, a, keeper.MarginModeCross))
	require.Equal(t, before, env.ctx.KVStore(env.key).Get(types.AccountKey(a)))

	require.ErrorIs(t, env.k.SetMarginMode(env.ctx, marginAccount(2), keeper.MarginModeCross), types.ErrNoAccount)
}
