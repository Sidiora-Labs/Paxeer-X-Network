package keeper

import (
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/dbadapter"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func feeFixture(t *testing.T, rail common.Address) (types.Params, feePricing) {
	t.Helper()
	params := types.DefaultParams()
	params.SwapFeeBps = 30
	params.Tier1FeeBps = 20
	params.Tier2FeeBps = 50
	params.OrderFeeBps = 10
	params.OrderTier1FeeBps = 20
	params.OrderTier2FeeBps = 50
	params.Tokens = types.OpeningTokens(common.HexToAddress("0x01"), common.HexToAddress("0x02"))
	require.NoError(t, params.Validate())
	schedule, err := pricingSchedule(params, rail)
	require.NoError(t, err)
	return params, feePricing{
		schedule: schedule, input: params.Tokens[0], output: params.Tokens[1],
		priceIn: sdk.NewIntWithDecimal(1, 18), priceOut: sdk.NewIntWithDecimal(5, 17),
		reserveUSD: sdk.NewIntWithDecimal(500_000, 18),
	}
}

func TestTieredFeeStack(t *testing.T) {
	cases := []struct {
		name   string
		volume sdk.Int
		want   uint64
	}{
		{"zero", sdk.ZeroInt(), 30},
		{"below_first", Tier1Threshold().SubRaw(1), 30},
		{"at_first", Tier1Threshold(), 50},
		{"above_first", Tier1Threshold().AddRaw(1), 50},
		{"below_second", Tier2Threshold().SubRaw(1), 50},
		{"at_second", Tier2Threshold(), 100},
		{"above_second", Tier2Threshold().AddRaw(1), 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bps, err := EffectiveFeeBps(30, 20, 50, tc.volume)
			require.NoError(t, err)
			require.Equal(t, tc.want, bps)
		})
	}
	_, pricing := feeFixture(t, common.HexToAddress(types.PECORAddress))
	result, err := pricing.price(sdk.NewIntWithDecimal(20_000, 6))
	require.NoError(t, err)
	require.Equal(t, "40000000000", result.Gross.String())
	require.Equal(t, uint64(50), result.FeeBps)
	require.Equal(t, "200000000", result.FeeAmount.String())
	require.Equal(t, sdk.NewIntWithDecimal(20_000, 18), result.VolumeUSD)
	result, err = pricing.price(sdk.NewIntWithDecimal(1_000, 6))
	require.NoError(t, err)
	require.Equal(t, uint64(30), result.FeeBps)
	require.Equal(t, "6000000", result.FeeAmount.String())
	result, err = pricing.price(sdk.OneInt())
	require.NoError(t, err)
	require.Equal(t, "2", result.Gross.String())
	require.Equal(t, "0", result.FeeAmount.String())
	require.Equal(t, "2", result.Net.String())

	for _, volume := range []sdk.Int{sdk.Int{}, sdk.NewInt(-1)} {
		_, err := EffectiveFeeBps(30, 20, 50, volume)
		require.ErrorIs(t, err, types.ErrZeroAmount)
	}
	_, err = EffectiveFeeBps(^uint64(0), 20, 50, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrFeeTooHigh)
	_, err = EffectiveFeeBps(131, 20, 50, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrFeeTooHigh)
	_, err = EffectiveFeeBps(30, 50, 20, sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrTier2BelowTier1)
	bps, err := EffectiveFeeBps(130, 20, 50, Tier2Threshold())
	require.NoError(t, err)
	require.Equal(t, uint64(200), bps)

	params := types.DefaultParams()
	schedule, err := pricingSchedule(params, common.HexToAddress(types.PECORAddress))
	require.NoError(t, err)
	require.Equal(t, uint64(7), schedule.base)
	schedule, err = pricingSchedule(params, common.HexToAddress(types.PECOROrdersAddress))
	require.NoError(t, err)
	require.Equal(t, uint64(4), schedule.base)
	_, err = pricingSchedule(params, common.HexToAddress(types.PECORQuoterAddress))
	require.ErrorIs(t, err, types.ErrNotAuthorized)
}

func TestPriceImpactDeduction(t *testing.T) {
	rail := common.HexToAddress(types.PECORAddress)
	_, pricing := feeFixture(t, rail)
	result, err := pricing.price(sdk.NewIntWithDecimal(20_000, 6))
	require.NoError(t, err)
	require.Equal(t, uint64(4), result.ImpactBps)
	require.Equal(t, "15920000", result.Deduction.String())
	require.Equal(t, "39784080000", result.Net.String())
	resultLarge, err := pricing.price(sdk.NewIntWithDecimal(150_000, 6))
	require.NoError(t, err)
	require.Equal(t, uint64(30), resultLarge.ImpactBps)
	require.Equal(t, "891000000", resultLarge.Deduction.String())
	require.Equal(t, "296109000000", resultLarge.Net.String())

	max := sdk.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	cases := []struct {
		name                   string
		after, volume, reserve sdk.Int
		enabled                bool
		scalar                 uint64
		net                    string
		impact                 uint64
		deduction              string
	}{
		{"cap", sdk.NewInt(10_001), max, sdk.OneInt(), true, 500, "9501", 500, "500"},
		{"empty_reserve", sdk.NewInt(10_001), max, sdk.ZeroInt(), true, 100, "10001", 0, "0"},
		{"disabled", sdk.NewInt(10_001), max, sdk.OneInt(), false, 100, "10001", 0, "0"},
		{"zero_scalar", sdk.NewInt(10_001), max, sdk.OneInt(), true, 0, "10001", 0, "0"},
		{"round_down", sdk.NewInt(9999), sdk.NewInt(199), sdk.NewInt(100), true, 1, "9999", 1, "0"},
		{"at_cap", sdk.NewInt(10_000), sdk.NewInt(500), sdk.NewInt(100), true, 100, "9500", 500, "500"},
		{"below_cap", sdk.NewInt(10_000), sdk.NewInt(499), sdk.NewInt(100), true, 100, "9501", 499, "499"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			net, impact, deduction, err := ApplyImpact(tc.after, tc.volume, tc.reserve, tc.enabled, tc.scalar)
			require.NoError(t, err)
			require.Equal(t, tc.net, net.String())
			require.Equal(t, tc.impact, impact)
			require.Equal(t, tc.deduction, deduction.String())
		})
	}
	_, _, _, err = ApplyImpact(sdk.OneInt(), sdk.OneInt(), sdk.OneInt(), true, 501)
	require.ErrorIs(t, err, types.ErrScalarTooHigh)
	_, _, _, err = ApplyImpact(sdk.Int{}, sdk.OneInt(), sdk.OneInt(), true, 100)
	require.ErrorIs(t, err, types.ErrZeroAmount)
	_, _, _, err = ApplyImpact(sdk.OneInt(), sdk.OneInt(), sdk.NewInt(-1), true, 100)
	require.ErrorIs(t, err, types.ErrZeroAmount)
	net, impact, deduction, err := ApplyImpact(max, max, sdk.OneInt(), true, 500)
	require.NoError(t, err)
	require.Equal(t, uint64(500), impact)
	expected := new(big.Int).Quo(max.BigInt(), big.NewInt(20))
	require.Equal(t, expected.String(), deduction.String())
	require.Equal(t, new(big.Int).Sub(max.BigInt(), expected).String(), net.String())

	user := common.HexToAddress("0x03")
	logs, err := result.FeeEvents(rail, user, pricing.output.Address, true)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	rawABI, err := os.ReadFile("../../../precompiles/spot/abi/pecor.json")
	require.NoError(t, err)
	contract, err := abi.JSON(strings.NewReader(string(rawABI)))
	require.NoError(t, err)
	for i, name := range []string{"TieredFeeApplied", "PriceImpactApplied"} {
		require.Equal(t, rail, logs[i].Address)
		require.Equal(t, []common.Hash{contract.Events[name].ID, common.BytesToHash(user.Bytes()), common.BytesToHash(pricing.output.Address.Bytes())}, logs[i].Topics)
	}
	feeData, err := contract.Events["TieredFeeApplied"].Inputs.NonIndexed().Pack(result.VolumeUSD.BigInt(), big.NewInt(50), big.NewInt(200_000_000))
	require.NoError(t, err)
	require.Equal(t, feeData, logs[0].Data)
	impactData, err := contract.Events["PriceImpactApplied"].Inputs.NonIndexed().Pack(big.NewInt(4), big.NewInt(15_920_000))
	require.NoError(t, err)
	require.Equal(t, impactData, logs[1].Data)
	for _, suppressedRail := range []common.Address{rail, common.HexToAddress(types.PECOROrdersAddress), common.HexToAddress(types.PECORStopOrdersAddress)} {
		logs, err = result.FeeEvents(suppressedRail, user, pricing.output.Address, false)
		require.NoError(t, err)
		require.Empty(t, logs)
		if suppressedRail != rail {
			logs, err = result.FeeEvents(suppressedRail, user, pricing.output.Address, true)
			require.NoError(t, err)
			require.Empty(t, logs)
		}
	}
	dust, err := pricing.price(sdk.OneInt())
	require.NoError(t, err)
	logs, err = dust.FeeEvents(rail, user, pricing.output.Address, true)
	require.NoError(t, err)
	require.Empty(t, logs)
	dust.FeeAmount = sdk.OneInt()
	dust.ImpactBps = 1
	logs, err = dust.FeeEvents(rail, user, pricing.output.Address, true)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, contract.Events["TieredFeeApplied"].ID, logs[0].Topics[0])
}

func TestFeesAccrueReserved(t *testing.T) {
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store := dbadapter.Store{DB: db}
	_, pricing := feeFixture(t, common.HexToAddress(types.PECORAddress))
	token := pricing.output.Address
	reserves := sdk.NewIntWithDecimal(1_000_000, 6)
	quote, err := pricing.price(sdk.NewIntWithDecimal(1_000, 6))
	require.NoError(t, err)
	require.NoError(t, accrueFees(store, types.FeeRailSwap, token, quote.Gross.Sub(quote.Net), reserves))
	accrual, err := readFeeAccrual(store, types.FeeRailSwap, token)
	require.NoError(t, err)
	require.Equal(t, "6000000", accrual.String())
	require.Equal(t, []byte{4, 0}, feeAccrualKey(types.FeeRailSwap, token)[:2])
	var record types.FeeAccrual
	require.NoError(t, json.Unmarshal(store.Get(feeAccrualKey(types.FeeRailSwap, token)), &record))
	require.Equal(t, types.FeeRailSwap, record.Rail)
	require.Equal(t, token, record.Token)
	require.Equal(t, accrual, record.Amount)

	quote, err = pricing.price(sdk.NewIntWithDecimal(20_000, 6))
	require.NoError(t, err)
	require.NoError(t, accrueFees(store, types.FeeRailSwap, token, quote.FeeAmount.Add(quote.Deduction), reserves.Sub(accrual)))
	accrual, err = readFeeAccrual(store, types.FeeRailSwap, token)
	require.NoError(t, err)
	require.Equal(t, "221920000", accrual.String())
	orderRail, err := feeRail(common.HexToAddress(types.PECOROrdersAddress))
	require.NoError(t, err)
	stopRail, err := feeRail(common.HexToAddress(types.PECORStopOrdersAddress))
	require.NoError(t, err)
	require.Equal(t, orderRail, stopRail)
	require.NoError(t, accrueFees(store, orderRail, token, sdk.NewInt(200_000), reserves.Sub(accrual)))
	require.NoError(t, accrueFees(store, stopRail, token, sdk.NewInt(400_000), reserves.Sub(accrual).SubRaw(200_000)))
	orders, err := readFeeAccrual(store, types.FeeRailOrder, token)
	require.NoError(t, err)
	require.Equal(t, "600000", orders.String())
	swap, err := readFeeAccrual(store, types.FeeRailSwap, token)
	require.NoError(t, err)
	require.Equal(t, accrual, swap)
	spendable := reserves.Sub(swap).Sub(orders)
	require.Equal(t, "999777480000", spendable.String())
	before := append([]byte(nil), store.Get(feeAccrualKey(types.FeeRailSwap, token))...)
	err = accrueFees(store, types.FeeRailSwap, token, spendable.AddRaw(1), spendable)
	require.ErrorIs(t, err, types.ErrInsufficientLiquidity)
	require.Equal(t, before, store.Get(feeAccrualKey(types.FeeRailSwap, token)))

	cache := store.CacheWrap(sdk.NewKVStoreKey(types.StoreKey)).(sdk.CacheKVStore)
	require.NoError(t, accrueFees(cache, types.FeeRailSwap, token, sdk.NewInt(7), spendable))
	uncommitted, err := readFeeAccrual(cache, types.FeeRailSwap, token)
	require.NoError(t, err)
	require.Equal(t, "221920007", uncommitted.String())
	require.Equal(t, before, store.Get(feeAccrualKey(types.FeeRailSwap, token)))
	cache.Write()
	committed, err := readFeeAccrual(store, types.FeeRailSwap, token)
	require.NoError(t, err)
	require.Equal(t, uncommitted, committed)

	emptyToken := common.HexToAddress("0x04")
	require.NoError(t, accrueFees(store, types.FeeRailSwap, emptyToken, sdk.ZeroInt(), sdk.ZeroInt()))
	require.False(t, store.Has(feeAccrualKey(types.FeeRailSwap, emptyToken)))
	require.ErrorIs(t, accrueFees(store, types.FeeRailSwap, token, sdk.NewInt(-1), spendable), types.ErrZeroAmount)
	require.ErrorIs(t, accrueFees(store, types.FeeRail(2), token, sdk.OneInt(), spendable), types.ErrNotAuthorized)
	store.Set(feeAccrualKey(types.FeeRailSwap, emptyToken), []byte("invalid"))
	_, err = readFeeAccrual(store, types.FeeRailSwap, emptyToken)
	require.ErrorContains(t, err, "decode fee accrual")
	require.ErrorContains(t, accrueFees(store, types.FeeRailSwap, emptyToken, sdk.OneInt(), spendable), "decode fee accrual")
}

func TestMakerFeeOnRestingFills(t *testing.T) {
	for _, rail := range []common.Address{common.HexToAddress(types.PECOROrdersAddress), common.HexToAddress(types.PECORStopOrdersAddress)} {
		t.Run(rail.Hex(), func(t *testing.T) {
			params, pricing := feeFixture(t, rail)
			order := types.LimitOrder{ID: 1, User: common.HexToAddress("0x03"), Stablecoin: pricing.input.Address, Token: pricing.output.Address,
				Amount: sdk.NewIntWithDecimal(100, 6), TargetPrice: pricing.priceOut, OrderType: types.OrderTypeLimitBuy, Status: types.OrderStatusPending}
			require.NoError(t, order.Validate())
			require.Equal(t, pricing.input.Address, order.EscrowToken())
			quote, err := pricing.price(order.Amount)
			require.NoError(t, err)
			require.Equal(t, "200000000", quote.Gross.String())
			require.Equal(t, uint64(10), quote.FeeBps)
			require.Equal(t, "200000", quote.FeeAmount.String())
			require.Equal(t, "199800000", quote.Net.String())
			require.Equal(t, uint64(0), quote.ImpactBps)
			require.Equal(t, "0", quote.Deduction.String())
			quote, err = pricing.price(sdk.NewIntWithDecimal(20_000, 6))
			require.NoError(t, err)
			require.Equal(t, uint64(30), quote.FeeBps)
			require.Equal(t, uint64(4), quote.ImpactBps)
			require.Equal(t, "39864048000", quote.Net.String())
			params.SwapFeeBps = 130
			params.PriceImpactEnabled = false
			params.PriceImpactScalarBps = 500
			schedule, err := pricingSchedule(params, rail)
			require.NoError(t, err)
			require.Equal(t, pricing.schedule, schedule)
			params.OrderTier1FeeBps = 1
			params.OrderTier2FeeBps = 2
			params.OrderPriceImpactEnabled = false
			params.OrderPriceImpactScalarBps = 0
			pricing.schedule, err = pricingSchedule(params, rail)
			require.NoError(t, err)
			quote, err = pricing.price(sdk.NewIntWithDecimal(100_000, 6))
			require.NoError(t, err)
			require.Equal(t, uint64(13), quote.FeeBps)
			require.Equal(t, uint64(0), quote.ImpactBps)
			require.Equal(t, "199740000000", quote.Net.String())
			params.OrderFeeBps = 200
			_, err = pricingSchedule(params, rail)
			require.ErrorIs(t, err, types.ErrFeeExceedsMax)
		})
	}
}

func TestExactOutTierInverse(t *testing.T) {
	_, pricing := feeFixture(t, common.HexToAddress(types.PECORAddress))
	target := sdk.NewIntWithDecimal(1994, 6)
	amount, err := pricing.quoteExactOut(target)
	require.NoError(t, err)
	require.Equal(t, "1000000000", amount.String())
	quote, err := pricing.price(amount)
	require.NoError(t, err)
	require.Equal(t, target, quote.Net)
	before, err := pricing.price(amount.SubRaw(1))
	require.NoError(t, err)
	require.True(t, before.Net.LT(target))

	for _, target := range []sdk.Int{sdk.NewInt(39_784_080_000), sdk.NewInt(296_109_000_000), sdk.OneInt()} {
		amount, err := pricing.quoteExactOut(target)
		require.NoError(t, err)
		quote, err := pricing.price(amount)
		require.NoError(t, err)
		require.True(t, quote.Net.GTE(target), "target %s", target)
		if amount.GT(sdk.OneInt()) {
			before, err := pricing.price(amount.SubRaw(1))
			require.NoError(t, err)
			require.True(t, before.Net.LT(target), "input %s", amount)
		}
	}

	pricing.input.Decimals = 0
	pricing.output.Decimals = 0
	pricing.priceOut = pricing.priceIn
	for _, target := range []int64{1, 9969, 9970, 9971, 9950, 99_000, 99_100, 99_200, 99_500, 119_000} {
		t.Run(sdk.NewInt(target).String(), func(t *testing.T) {
			var want int64
			for input := int64(1); input <= 125_000; input++ {
				fee := int64(30)
				if input >= 10_000 {
					fee += 20
				}
				if input >= 100_000 {
					fee += 50
				}
				after := input - input*fee/10_000
				impact := input * 100 / 500_000
				if impact > 500 {
					impact = 500
				}
				if after-after*impact/10_000 >= target {
					want = input
					break
				}
			}
			require.NotZero(t, want)
			amount, err := pricing.quoteExactOut(sdk.NewInt(target))
			require.NoError(t, err)
			require.Equal(t, sdk.NewInt(want), amount)
		})
	}

	pricing.schedule.impactEnabled = false
	pricing.input.Decimals = 6
	pricing.output.Decimals = 18
	amount, err = pricing.quoteExactOut(sdk.OneInt())
	require.NoError(t, err)
	require.Equal(t, "1", amount.String())
	quote, err = pricing.price(amount)
	require.NoError(t, err)
	require.Equal(t, "997000000000", quote.Net.String())
	require.Equal(t, "999999999999", quote.Gross.Sub(sdk.OneInt()).String())
	require.True(t, quote.Gross.Sub(sdk.OneInt()).GT(quote.FeeAmount.Add(quote.Deduction)))

	pricing.input.Decimals = 0
	pricing.output.Decimals = 0
	pricing.priceIn = sdk.NewIntWithDecimal(1_000_000, 18)
	pricing.priceOut = sdk.NewIntWithDecimal(1, 18)
	amount, err = pricing.quoteExactOut(sdk.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, "1", amount.String())
	quote, err = pricing.price(amount)
	require.NoError(t, err)
	require.Equal(t, uint64(100), quote.FeeBps)
	require.Equal(t, "990000", quote.Net.String())

	pricing.priceIn = sdk.OneInt()
	pricing.priceOut = sdk.OneInt()
	pricing.input.Decimals = 36
	pricing.output.Decimals = 36
	pricing.schedule = feeSchedule{}
	max := sdk.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	amount, err = pricing.quoteExactOut(max)
	require.NoError(t, err)
	require.Equal(t, max, amount)
	pricing.schedule.base = 200
	_, err = pricing.quoteExactOut(max)
	require.ErrorIs(t, err, types.ErrInsufficientLiquidity)
	_, err = pricing.quoteExactOut(sdk.ZeroInt())
	require.ErrorIs(t, err, types.ErrZeroAmount)
	pricing.priceIn = sdk.ZeroInt()
	_, err = pricing.quoteExactOut(sdk.OneInt())
	require.ErrorIs(t, err, types.ErrInvalidPrice)
}
