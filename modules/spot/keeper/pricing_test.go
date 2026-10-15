package keeper

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

type pricingEnv struct {
	keeper  *Keeper
	ctx     sdk.Context
	spotKey sdk.StoreKey
	feedKey sdk.StoreKey
	tokens  []types.TokenParams
}

func newPricingEnv(t *testing.T, mountFeed bool) pricingEnv {
	t.Helper()
	spotKey := sdk.NewKVStoreKey(types.StoreKey)
	feedKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(spotKey, sdk.StoreTypeIAVL, db)
	if mountFeed {
		ms.MountStoreWithDB(feedKey, sdk.StoreTypeIAVL, db)
	}
	require.NoError(t, ms.LoadLatestVersion())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 10, Time: time.Unix(100, 987_000_000).UTC()}, false)
	feed := pricefeedkeeper.NewKeeper(feedKey, nil, nil, nil)
	k := &Keeper{storeKey: spotKey, pricefeedKeeper: feed}
	params := types.DefaultParams()
	params.Tokens = types.OpeningTokens(common.HexToAddress("0xa1"), common.HexToAddress("0xb2"))
	require.NoError(t, params.Validate())
	writePricingValue(t, ctx, spotKey, types.ParamsKey, params)
	if mountFeed {
		require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	}
	return pricingEnv{keeper: k, ctx: ctx, spotKey: spotKey, feedKey: feedKey, tokens: params.Tokens}
}

func writePricingValue(t *testing.T, ctx sdk.Context, key sdk.StoreKey, storageKey []byte, value interface{}) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	ctx.KVStore(key).Set(storageKey, encoded)
}

func putPricingAggregate(t *testing.T, env pricingEnv, marketID uint64, index, mark sdk.Int, timestamp uint64) {
	t.Helper()
	writePricingValue(t, env.ctx, env.feedKey, pricefeedtypes.AggregateKey(marketID), pricefeedtypes.Aggregate{
		IndexPrice: index, MarkPrice: mark, Basis: mark.Sub(index), TimestampMs: timestamp,
		Height: env.ctx.BlockHeight(), Sources: 1,
	})
}

func TestIndexPriceFromPricefeed(t *testing.T) {
	env := newPricingEnv(t, true)
	for _, tc := range []struct {
		token  int
		market uint64
		index  sdk.Int
		mark   sdk.Int
	}{
		{1, 11, sdk.NewIntWithDecimal(5, 17), sdk.NewIntWithDecimal(6, 17)},
		{2, 12, sdk.NewIntWithDecimal(2, 18), sdk.NewIntWithDecimal(3, 18)},
	} {
		putPricingAggregate(t, env, tc.market, tc.index, tc.mark, uint64(env.ctx.BlockTime().UnixMilli()))
		price, err := env.keeper.IndexPrice(env.ctx, env.tokens[tc.token].Address)
		require.NoError(t, err)
		require.Equal(t, tc.index.String(), price.String())
		quoted, timestamp, stale, err := env.keeper.TokenPrice(env.ctx, env.tokens[tc.token].Address)
		require.NoError(t, err)
		require.Equal(t, tc.index.String(), quoted.String())
		require.Equal(t, uint64(100), timestamp)
		require.False(t, stale)
	}
	params := env.keeper.GetParams(env.ctx)
	params.Tokens[1].MarketID = 12
	writePricingValue(t, env.ctx, env.spotKey, types.ParamsKey, params)
	price, err := env.keeper.IndexPrice(env.ctx, env.tokens[1].Address)
	require.NoError(t, err)
	require.Equal(t, "2000000000000000000", price.String())
}

func TestStablecoinPricesAtOne(t *testing.T) {
	env := newPricingEnv(t, false)
	price, err := env.keeper.IndexPrice(env.ctx, env.tokens[0].Address)
	require.NoError(t, err)
	require.Equal(t, "1000000000000000000", price.String())
	price, timestamp, stale, err := env.keeper.TokenPrice(env.ctx, env.tokens[0].Address)
	require.NoError(t, err)
	require.Equal(t, "1000000000000000000", price.String())
	require.Equal(t, uint64(100), timestamp)
	require.False(t, stale)
}

func TestStalePriceRefusesTrades(t *testing.T) {
	for _, tc := range []struct {
		name     string
		offset   int64
		disabled bool
		missing  bool
		stale    bool
	}{
		{name: "age boundary", offset: -15_000},
		{name: "expired", offset: -15_001, stale: true},
		{name: "clock skew boundary", offset: 2_000},
		{name: "future", offset: 2_001, stale: true},
		{name: "disabled", disabled: true, stale: true},
		{name: "missing", missing: true, stale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newPricingEnv(t, true)
			index := sdk.NewIntWithDecimal(5, 17)
			timestamp := uint64(env.ctx.BlockTime().UnixMilli() + tc.offset)
			if !tc.missing {
				putPricingAggregate(t, env, 11, index, sdk.NewIntWithDecimal(6, 17), timestamp)
			}
			if tc.disabled {
				params := pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())
				params.Markets[11].Enabled = false
				writePricingValue(t, env.ctx, env.feedKey, pricefeedtypes.ParamsKey, params)
			}
			before := append([]byte(nil), env.ctx.KVStore(env.feedKey).Get(pricefeedtypes.AggregateKey(11))...)
			price, err := env.keeper.IndexPrice(env.ctx, env.tokens[1].Address)
			if tc.stale {
				require.ErrorIs(t, err, types.ErrStalePrice)
				var abiErr *types.AbiError
				require.ErrorAs(t, err, &abiErr)
				require.Equal(t, []byte{0x19, 0xab, 0xf4, 0x0e}, abiErr.Data())
				require.Equal(t, "0", price.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, index.String(), price.String())
			}
			quoted, ts, stale, err := env.keeper.TokenPrice(env.ctx, env.tokens[1].Address)
			require.NoError(t, err)
			require.Equal(t, tc.stale, stale)
			if tc.missing {
				require.Equal(t, "0", quoted.String())
				require.Zero(t, ts)
			} else {
				require.Equal(t, index.String(), quoted.String())
				require.Equal(t, timestamp/1000, ts)
			}
			require.Equal(t, before, env.ctx.KVStore(env.feedKey).Get(pricefeedtypes.AggregateKey(11)))
			require.Empty(t, env.ctx.EventManager().Events())
		})
	}
}

func TestTokenWithoutMarketIsRefused(t *testing.T) {
	env := newPricingEnv(t, false)
	params := env.keeper.GetParams(env.ctx)
	params.Tokens[1].MarketID = 0
	writePricingValue(t, env.ctx, env.spotKey, types.ParamsKey, params)
	price, err := env.keeper.IndexPrice(env.ctx, env.tokens[1].Address)
	require.ErrorIs(t, err, types.ErrInvalidOracle)
	var abiErr *types.AbiError
	require.ErrorAs(t, err, &abiErr)
	require.Equal(t, []byte{0x95, 0x89, 0xa2, 0x7d}, abiErr.Data())
	require.Equal(t, "0", price.String())
	_, err = env.keeper.IndexPrice(env.ctx, common.HexToAddress("0xc3"))
	require.ErrorIs(t, err, types.ErrInvalidVault)
	require.Empty(t, env.ctx.EventManager().Events())

	listed := newPricingEnv(t, true)
	params = listed.keeper.GetParams(listed.ctx)
	params.Tokens[1].MarketID = uint64(len(pricefeedtypes.Markets))
	writePricingValue(t, listed.ctx, listed.spotKey, types.ParamsKey, params)
	_, err = listed.keeper.IndexPrice(listed.ctx, listed.tokens[1].Address)
	require.ErrorIs(t, err, pricefeedtypes.ErrUnknownMarket)
}

func TestGrossOutputConversion(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maxText := max.String()
	half := new(big.Int).Quo(new(big.Int).Set(max), big.NewInt(2)).String()
	for _, tc := range []struct {
		name, amount, priceIn, priceOut string
		decIn, decOut                   uint32
		down, up                        string
	}{
		{"fixture", "1000000000", "1000000000000000000", "500000000000000000", 6, 6, "2000000000", "2000000000"},
		{"fraction", "1", "1", "3", 6, 6, "0", "1"},
		{"scale before division", "1", "1", "3", 0, 2, "33", "34"},
		{"downscale", "100", "1", "3", 2, 0, "0", "1"},
		{"wide product", maxText, maxText, maxText, 18, 18, maxText, maxText},
		{"wide denominator", maxText, maxText, maxText, 36, 0, "115792089237316195423570985008687907853269", "115792089237316195423570985008687907853270"},
		{"wide numerator scaled", half, "2", maxText, 0, 36, "999999999999999999999999999999999999", "1000000000000000000000000000000000000"},
		{"maximum decimals", "1", "1", "1", 0, 36, "1000000000000000000000000000000000000", "1000000000000000000000000000000000000"},
		{"zero amount", "0", "1", "1", 36, 0, "0", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount, in, out := pricingInt(t, tc.amount), pricingInt(t, tc.priceIn), pricingInt(t, tc.priceOut)
			for _, up := range []bool{false, true} {
				got, err := Convert(amount, in, out, tc.decIn, tc.decOut, up)
				require.NoError(t, err)
				expected := tc.down
				if up {
					expected = tc.up
				}
				require.Equal(t, expected, got.String())
				require.Equal(t, tc.amount, amount.String())
				require.Equal(t, tc.priceIn, in.String())
				require.Equal(t, tc.priceOut, out.String())
			}
		})
	}
	one := sdk.OneInt()
	maxInt := sdk.NewIntFromBigInt(max)
	t.Run("rounding overflow", func(t *testing.T) {
		amount := sdk.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Set(max), big.NewInt(1)))
		denominator := sdk.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Set(max), big.NewInt(2)))
		got, err := Convert(amount, amount, denominator, 0, 0, false)
		require.NoError(t, err)
		require.Equal(t, maxText, got.String())
		got, err = Convert(amount, amount, denominator, 0, 0, true)
		require.ErrorIs(t, err, ErrQuoteOverflow)
		require.Equal(t, "QuoteOverflow()", err.Error())
		require.Equal(t, "0", got.String())
	})
	for _, tc := range []struct {
		name            string
		amount, in, out sdk.Int
		decIn, decOut   uint32
		err             error
	}{
		{"nil amount", sdk.Int{}, one, one, 0, 0, ErrInvalidPriceOrDecimals},
		{"negative amount", sdk.NewInt(-1), one, one, 0, 0, ErrInvalidPriceOrDecimals},
		{"nil input price", one, sdk.Int{}, one, 0, 0, ErrInvalidPriceOrDecimals},
		{"zero input price", one, sdk.ZeroInt(), one, 0, 0, ErrInvalidPriceOrDecimals},
		{"negative input price", one, sdk.NewInt(-1), one, 0, 0, ErrInvalidPriceOrDecimals},
		{"nil output price", one, one, sdk.Int{}, 0, 0, ErrInvalidPriceOrDecimals},
		{"zero output price", one, one, sdk.ZeroInt(), 0, 0, ErrInvalidPriceOrDecimals},
		{"negative output price", one, one, sdk.NewInt(-1), 0, 0, ErrInvalidPriceOrDecimals},
		{"input decimals", one, one, one, 37, 0, ErrInvalidPriceOrDecimals},
		{"output decimals", one, one, one, 0, 37, ErrInvalidPriceOrDecimals},
		{"product overflow", maxInt, sdk.NewInt(2), one, 0, 0, ErrQuoteOverflow},
		{"scale overflow", maxInt, one, one, 0, 1, ErrQuoteOverflow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, up := range []bool{false, true} {
				got, err := Convert(tc.amount, tc.in, tc.out, tc.decIn, tc.decOut, up)
				require.ErrorIs(t, err, tc.err)
				require.Equal(t, tc.err.Error(), err.Error())
				require.Equal(t, "0", got.String())
			}
		})
	}
	for _, tc := range []struct {
		name          string
		amount, price sdk.Int
		dec           uint32
		want          string
		err           error
	}{
		{"fixture", sdk.NewInt(1_000_000_000), sdk.NewIntWithDecimal(1, 18), 6, "1000000000000000000000", nil},
		{"floor", sdk.NewInt(7), sdk.NewInt(3), 1, "2", nil},
		{"wide product", maxInt, sdk.NewIntWithDecimal(1, 36), 36, maxText, nil},
		{"zero amount", sdk.ZeroInt(), one, 0, "0", nil},
		{"overflow", maxInt, sdk.NewInt(2), 0, "0", ErrQuoteOverflow},
		{"nil amount", sdk.Int{}, one, 0, "0", ErrInvalidPriceOrDecimals},
		{"negative amount", sdk.NewInt(-1), one, 0, "0", ErrInvalidPriceOrDecimals},
		{"nil price", one, sdk.Int{}, 0, "0", ErrInvalidPriceOrDecimals},
		{"zero price", one, sdk.ZeroInt(), 0, "0", ErrInvalidPriceOrDecimals},
		{"negative price", one, sdk.NewInt(-1), 0, "0", ErrInvalidPriceOrDecimals},
		{"decimals", one, one, 37, "0", ErrInvalidPriceOrDecimals},
	} {
		t.Run("volume "+tc.name, func(t *testing.T) {
			got, err := VolumeUSD(tc.amount, tc.price, tc.dec)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Equal(t, tc.err.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got.String())
		})
	}
}

func pricingInt(t *testing.T, value string) sdk.Int {
	t.Helper()
	result, ok := sdk.NewIntFromString(value)
	require.True(t, ok)
	return result
}
