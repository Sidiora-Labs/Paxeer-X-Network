package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func intOf(t *testing.T, s string) sdk.Int {
	t.Helper()
	v, ok := sdk.NewIntFromString(s)
	require.True(t, ok, s)
	return v
}

func TestUsdToUsdlRounding(t *testing.T) {
	cases := []struct {
		name                         string
		usd                          string
		down, downResidue, up, upRes string
	}{
		{"zero", "0", "0", "0", "0", "0"},
		{"one wei of USD", "1", "0", "1", "1", "999999999999"},
		{"one below a USDL unit", "999999999999", "0", "999999999999", "1", "1"},
		{"exactly one USDL unit", "1000000000000", "1", "0", "1", "0"},
		{"one above a USDL unit", "1000000000001", "1", "1", "2", "999999999999"},
		{"250 USD", "250000000000000000000", "250000000", "0", "250000000", "0"},
		{"taker fee 0.4457925 USD", "445792500000000000", "445792", "500000000000", "445793", "500000000000"},
		{
			"maximum 2^256-1",
			"115792089237316195423570985008687907853269984665640564039457584007913129639935",
			"115792089237316195423570985008687907853269984665640564039457584007", "913129639935",
			"115792089237316195423570985008687907853269984665640564039457584008", "86870360065",
		},
	}
	for _, tc := range cases {
		usd := intOf(t, tc.usd)
		down, downResidue, err := types.UsdToUsdlDown(usd)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.down, down.String(), tc.name)
		require.Equal(t, tc.downResidue, downResidue.String(), tc.name)
		require.True(t, down.Mul(sdk.NewInt(1_000_000_000_000)).Add(downResidue).Equal(usd), tc.name)

		up, upResidue, err := types.UsdToUsdlUp(usd)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.up, up.String(), tc.name)
		require.Equal(t, tc.upRes, upResidue.String(), tc.name)
	}
}

func TestUsdToUsdlRefusals(t *testing.T) {
	for _, convert := range []func(sdk.Int) (sdk.Int, sdk.Int, error){types.UsdToUsdlDown, types.UsdToUsdlUp} {
		_, _, err := convert(sdk.NewInt(-1))
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, _, err = convert(sdk.Int{})
		require.ErrorIs(t, err, types.ErrInvalidParams)
	}
}

func TestUsdToUsdlMarketStep(t *testing.T) {
	cases := []struct {
		sz, px uint8
		valid  bool
	}{
		{5, 1, true},
		{0, 6, true},
		{0, 0, true},
		{6, 1, false},
		{0, 7, false},
		{10, 9, false},
		{255, 255, false},
	}
	for _, tc := range cases {
		m := btcMarket()
		m.SzDecimals, m.PxDecimals = tc.sz, tc.px
		err := m.Validate()
		if tc.valid {
			require.NoError(t, err, "sz %d px %d", tc.sz, tc.px)
		} else {
			require.ErrorIs(t, err, types.ErrInvalidParams, "sz %d px %d", tc.sz, tc.px)
		}
	}
}

func pow2(n uint) sdk.Int {
	return sdk.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), n))
}

func TestMathOverflow(t *testing.T) {
	_, err := types.MulDivUp(pow2(255), sdk.NewInt(4), sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrOverflow)
	_, err = types.MulDivDown(pow2(255), sdk.NewInt(4), sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrOverflow)

	maxUint := pow2(256).Sub(sdk.OneInt())
	out, err := types.MulDivDown(maxUint, maxUint, maxUint)
	require.NoError(t, err)
	require.Equal(t, maxUint.String(), out.String())
	out, err = types.MulDivUp(maxUint, sdk.NewInt(2), sdk.NewInt(2))
	require.NoError(t, err)
	require.Equal(t, maxUint.String(), out.String())

	maxInt := pow2(255).Sub(sdk.OneInt())
	minInt := pow2(255).Neg()
	_, err = types.MulDivDownSigned(minInt, sdk.NewInt(-1), sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrOverflow)
	_, err = types.MulDivUpSigned(maxInt, sdk.NewInt(2), sdk.NewInt(1))
	require.ErrorIs(t, err, types.ErrOverflow)
	_, err = types.MulDivDownSigned(pow2(255), sdk.OneInt(), sdk.OneInt())
	require.ErrorIs(t, err, types.ErrOverflow)
	out, err = types.MulDivDownSigned(minInt, maxInt, maxInt)
	require.NoError(t, err)
	require.Equal(t, minInt.String(), out.String())
}

func TestMathMulDivRefusals(t *testing.T) {
	for _, mulDiv := range []func(a, b, d sdk.Int) (sdk.Int, error){types.MulDivDown, types.MulDivUp} {
		_, err := mulDiv(sdk.OneInt(), sdk.OneInt(), sdk.ZeroInt())
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = mulDiv(sdk.NewInt(-1), sdk.OneInt(), sdk.OneInt())
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = mulDiv(sdk.OneInt(), sdk.Int{}, sdk.OneInt())
		require.ErrorIs(t, err, types.ErrInvalidParams)
	}
	for _, mulDiv := range []func(a, b, d sdk.Int) (sdk.Int, error){types.MulDivDownSigned, types.MulDivUpSigned} {
		_, err := mulDiv(sdk.NewInt(-1), sdk.OneInt(), sdk.ZeroInt())
		require.ErrorIs(t, err, types.ErrInvalidParams)
		_, err = mulDiv(sdk.Int{}, sdk.OneInt(), sdk.OneInt())
		require.ErrorIs(t, err, types.ErrInvalidParams)
	}
}

func TestMathMulDivRounding(t *testing.T) {
	cases := []struct {
		name     string
		a, b, d  int64
		down, up string
	}{
		{"zero product", 0, 7, 3, "0", "0"},
		{"exact", 6, 4, 3, "8", "8"},
		{"ten thirds", 10, 1, 3, "3", "4"},
		{"10 x 3 / 4", 10, 3, 4, "7", "8"},
		{"one half", 1, 1, 2, "0", "1"},
		{"just below one", 999_999, 1, 1_000_000, "0", "1"},
		{"taker fee 400 ppm of 1114.48125", 1_114_481_250, 400, 1_000_000, "445792", "445793"},
	}
	for _, tc := range cases {
		down, err := types.MulDivDown(sdk.NewInt(tc.a), sdk.NewInt(tc.b), sdk.NewInt(tc.d))
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.down, down.String(), tc.name)
		up, err := types.MulDivUp(sdk.NewInt(tc.a), sdk.NewInt(tc.b), sdk.NewInt(tc.d))
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.up, up.String(), tc.name)
	}
}

func TestMathMulDivSignedRounding(t *testing.T) {
	cases := []struct {
		name     string
		a, b, d  int64
		down, up string
	}{
		{"positive exact", 6, 4, 3, "8", "8"},
		{"positive ten thirds", 10, 1, 3, "3", "4"},
		{"negative numerator ten thirds", -10, 1, 3, "-4", "-3"},
		{"negative factor ten thirds", 10, -1, 3, "-4", "-3"},
		{"negative denominator ten thirds", 10, 1, -3, "-4", "-3"},
		{"two negatives ten thirds", -10, 1, -3, "3", "4"},
		{"three negatives ten thirds", -10, -1, -3, "-4", "-3"},
		{"negative exact", -6, 4, 3, "-8", "-8"},
		{"negative exact over negative", -6, 4, -3, "8", "8"},
		{"minus one half", -1, 1, 2, "-1", "0"},
		{"zero product", 0, -7, -3, "0", "0"},
	}
	for _, tc := range cases {
		down, err := types.MulDivDownSigned(sdk.NewInt(tc.a), sdk.NewInt(tc.b), sdk.NewInt(tc.d))
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.down, down.String(), tc.name)
		up, err := types.MulDivUpSigned(sdk.NewInt(tc.a), sdk.NewInt(tc.b), sdk.NewInt(tc.d))
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.up, up.String(), tc.name)
	}
}
