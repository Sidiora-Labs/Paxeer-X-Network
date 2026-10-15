package types_test

import (
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
