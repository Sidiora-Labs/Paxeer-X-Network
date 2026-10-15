package keeper

import (
	"errors"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

const twapBase = uint64(1_700_000_000)

var twapToken = common.HexToAddress("0x00000000000000000000000000000000000b0011")

func twapKeeper(t *testing.T, params types.Params) (*Keeper, sdk.Context) {
	t.Helper()
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(storeKey, sdk.NewTransientStoreKey("spot_transient_test"))
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := &Keeper{storeKey: storeKey, authority: authority}
	require.NoError(t, k.UpdateParams(ctx, authority, params))
	return k, ctx
}

func atSecond(ctx sdk.Context, second uint64) sdk.Context {
	return ctx.WithBlockTime(time.Unix(int64(second), 0))
}

func e18(milli int64) sdk.Int { return sdk.NewIntWithDecimal(milli, 15) }

func requireAbiSelector(t *testing.T, err error, want *types.AbiError, selector string) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var abiErr *types.AbiError
	require.True(t, errors.As(err, &abiErr))
	require.Equal(t, selector, hexutil.Encode(abiErr.Data()))
}

func TestSampleRing(t *testing.T) {
	k, ctx := twapKeeper(t, types.DefaultParams())
	for block := uint64(1); block <= types.TwapHistoryDepth+1; block++ {
		require.NoError(t, k.appendSample(atSecond(ctx, twapBase+block), twapToken, twapBase+block, sdk.NewIntFromUint64(block)))
	}
	ring, err := k.twapRing(ctx, twapToken)
	require.NoError(t, err)
	require.Equal(t, types.TwapRing{Token: twapToken, Head: 1, Count: types.TwapHistoryDepth}, ring)

	for i := uint32(0); i < types.TwapHistoryDepth; i++ {
		sample, err := k.twapSample(ctx, ring, i)
		require.NoError(t, err)
		want := uint64(i) + 2
		require.Equal(t, twapBase+want, sample.Time)
		require.Equal(t, sdk.NewIntFromUint64(want), sample.Index)
		require.Equal(t, (uint32(1)+i)%types.TwapHistoryDepth, sample.Slot)
	}

	// The first sample was overwritten: the ring covers the latest 1000 only.
	now := atSecond(ctx, twapBase+types.TwapHistoryDepth+1)
	_, err = k.TWAP(now, twapToken, types.TwapHistoryDepth)
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	twap, err := k.TWAP(now, twapToken, types.TwapHistoryDepth-1)
	require.NoError(t, err)
	// Samples 2..1000 each hold one second over the window of 999 seconds.
	require.Equal(t, sdk.NewIntFromUint64(501), twap)

	other, err := k.twapRing(ctx, common.HexToAddress("0x00000000000000000000000000000000000b0012"))
	require.NoError(t, err)
	require.Equal(t, uint32(0), other.Count)
}

func TestTWAPWindow(t *testing.T) {
	k, ctx := twapKeeper(t, types.DefaultParams())
	now := twapBase + 300
	nowCtx := atSecond(ctx, now)

	_, err := k.TWAP(nowCtx, twapToken, 300)
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")

	require.NoError(t, k.appendSample(atSecond(ctx, twapBase), twapToken, twapBase, e18(500)))
	require.NoError(t, k.appendSample(atSecond(ctx, twapBase+200), twapToken, twapBase+200, e18(600)))

	cases := []struct {
		period uint64
		want   string
	}{
		{300, "533333333333333333"},
		{250, "540000000000000000"},
		{100, "600000000000000000"},
		{1, "600000000000000000"},
	}
	for _, tc := range cases {
		twap, err := k.TWAP(nowCtx, twapToken, tc.period)
		require.NoError(t, err)
		require.Equal(t, tc.want, twap.String(), "period %d", tc.period)
	}

	for _, period := range []uint64{0, 301, now + 1} {
		_, err := k.TWAP(nowCtx, twapToken, period)
		requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	}
}

func TestTwapSanity(t *testing.T) {
	params := types.DefaultParams()
	params.TwapSanityEnabled = true
	params.TwapSanityPeriod = 300
	params.MaxTwapDeviationBps = 200
	params.FillQualityTwapPeriod = 300
	k, ctx := twapKeeper(t, params)
	require.NoError(t, k.appendSample(atSecond(ctx, twapBase), twapToken, twapBase, e18(1000)))
	nowCtx := atSecond(ctx, twapBase+300)

	cases := []struct {
		index   sdk.Int
		refused bool
	}{
		{e18(1030), true},
		{e18(970), true},
		{e18(1020), false},
		{e18(980), false},
		{e18(1000), false},
	}
	for _, tc := range cases {
		err := k.CheckTwapSanity(nowCtx, twapToken, tc.index)
		ok, fillErr := k.FillQualityOK(nowCtx, twapToken, tc.index)
		require.NoError(t, fillErr)
		require.Equal(t, !tc.refused, ok, "index %s", tc.index)
		if tc.refused {
			requireAbiSelector(t, err, types.ErrStalePrice, "0x19abf40e")
		} else {
			require.NoError(t, err, "index %s", tc.index)
		}
	}

	// A window the ring does not cover refuses the trade and holds the fill.
	early := atSecond(ctx, twapBase+299)
	requireAbiSelector(t, k.CheckTwapSanity(early, twapToken, e18(1000)), types.ErrTwapWindowInvalid, "0xb307c2d2")
	ok, err := k.FillQualityOK(early, twapToken, e18(1000))
	requireAbiSelector(t, err, types.ErrTwapWindowInvalid, "0xb307c2d2")
	require.False(t, ok)

	params.TwapSanityEnabled = false
	params.FillQualityTwapPeriod = 0
	require.NoError(t, k.UpdateParams(ctx, k.authority, params))
	require.NoError(t, k.CheckTwapSanity(nowCtx, twapToken, e18(1030)))
	ok, err = k.FillQualityOK(nowCtx, twapToken, e18(1030))
	require.NoError(t, err)
	require.True(t, ok)
}
