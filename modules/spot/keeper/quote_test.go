package keeper

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/stretchr/testify/require"
)

func TestQuoteUtilizationFields(t *testing.T) {
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	cases := []struct {
		name       string
		gross      *big.Int
		spendable  *big.Int
		capBps     uint64
		tradeBps   *big.Int
		capped     bool
		sufficient bool
	}{
		{"market buy of 300,000e6 under the PAX cap", big.NewInt(600_000_000_000), big.NewInt(1_000_000_000_000),
			types.OpeningTokenMaxUtilizationBps, big.NewInt(6_000), false, true},
		{"market buy of 500,000e6 over the PAX cap", big.NewInt(1_000_000_000_000), big.NewInt(1_000_000_000_000),
			types.OpeningTokenMaxUtilizationBps, big.NewInt(10_000), true, false},
		{"at the opening cap", big.NewInt(900_000_000), big.NewInt(1_000_000_000), types.OpeningTokenMaxUtilizationBps,
			big.NewInt(9_000), false, true},
		{"one unit above the opening cap rounds up", big.NewInt(900_000_001), big.NewInt(1_000_000_000),
			types.OpeningTokenMaxUtilizationBps, big.NewInt(9_001), true, false},
		{"below the opening cap", big.NewInt(1), big.NewInt(3), types.OpeningTokenMaxUtilizationBps,
			big.NewInt(3_334), false, true},
		{"whole book at the default cap", big.NewInt(1_000_000_000), big.NewInt(1_000_000_000),
			types.DefaultMaxUtilizationBps, big.NewInt(10_000), false, true},
		{"above the book at the default cap", big.NewInt(1_000_000_001), big.NewInt(1_000_000_000),
			types.DefaultMaxUtilizationBps, big.NewInt(10_001), true, false},
		{"empty book", big.NewInt(1), new(big.Int), types.DefaultMaxUtilizationBps, maxUint256, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tradeBps, capped, sufficient := utilization(tc.gross, tc.spendable, tc.capBps)
			require.Equal(t, 0, tc.tradeBps.Cmp(tradeBps), "tradeUtilizationBps %s", tradeBps)
			require.Equal(t, tc.capped, capped)
			require.Equal(t, tc.sufficient, sufficient)
		})
	}

	require.Equal(t, uint64(types.OpeningTokenMaxUtilizationBps),
		utilizationCap(types.TokenParams{MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps}))
	require.Equal(t, uint64(types.DefaultMaxUtilizationBps), utilizationCap(types.TokenParams{}))
}
