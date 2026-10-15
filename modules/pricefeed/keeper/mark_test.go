package keeper

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func e18(n int64) sdk.Int { return sdk.NewIntWithDecimal(n, 18) }

func markParams(maxMarkBasisBps uint32) types.Params {
	params := types.DefaultParams(types.DefaultAuthority())
	params.MaxMarkBasisBps = maxMarkBasisBps
	return params
}

func checkMark(t *testing.T, name string, prev *types.Aggregate, index, markMedian sdk.Int, ts uint64, params types.Params, wantMark, wantBasis sdk.Int) {
	t.Helper()
	mark, basis := markPrice(prev, index, markMedian, ts, params)
	if !mark.Equal(wantMark) {
		t.Fatalf("%s: mark = %s, want %s", name, mark, wantMark)
	}
	if !basis.Equal(wantBasis) {
		t.Fatalf("%s: basis = %s, want %s", name, basis, wantBasis)
	}
}

func TestMarkFirstAggregate(t *testing.T) {
	params := markParams(types.DefaultMaxMarkBasisBps)
	checkMark(t, "first aggregate", nil, e18(1000), e18(1004), 5000, params, e18(1004), e18(4))
	checkMark(t, "first aggregate negative basis", nil, e18(1000), e18(990), 5000, params, e18(990), e18(-10))
}

func TestMarkBasisAverage(t *testing.T) {
	params := markParams(types.DefaultMaxMarkBasisBps)
	if params.MarkEmaTauMs != 150000 {
		t.Fatalf("mark_ema_tau_ms = %d, want 150000", params.MarkEmaTauMs)
	}
	prev := &types.Aggregate{IndexPrice: e18(100000), MarkPrice: e18(100000), Basis: e18(4000), TimestampMs: 1000000}
	checkMark(t, "dt 50000", prev, e18(100000), e18(98000), 1050000, params, e18(100000), e18(2500))
	checkMark(t, "dt 0", prev, e18(100000), e18(98000), 1000000, params, e18(100000), e18(4000))
	checkMark(t, "earlier timestamp", prev, e18(100000), e18(98000), 999999, params, e18(100000), e18(4000))

	zero := &types.Aggregate{IndexPrice: sdk.NewInt(1000), MarkPrice: sdk.NewInt(1000), Basis: sdk.ZeroInt(), TimestampMs: 1000}
	checkMark(t, "negative step truncates toward zero", zero, sdk.NewInt(1000), sdk.NewInt(999), 1001, params, sdk.NewInt(1000), sdk.ZeroInt())
	checkMark(t, "positive step truncates toward zero", zero, sdk.NewInt(1000), sdk.NewInt(1001), 1001, params, sdk.NewInt(1000), sdk.ZeroInt())
}

func TestMarkMedianOfThree(t *testing.T) {
	params := markParams(types.DefaultMaxMarkBasisBps)
	prev := &types.Aggregate{IndexPrice: e18(1000), MarkPrice: e18(1000), Basis: e18(3), TimestampMs: 7000}
	checkMark(t, "index in the middle", prev, e18(1000), e18(990), 7000, params, e18(1000), e18(3))
	checkMark(t, "external mark in the middle", prev, e18(1000), e18(1002), 7000, params, e18(1002), e18(3))
	checkMark(t, "index plus basis in the middle", prev, e18(1000), e18(1010), 7000, params, e18(1003), e18(3))
}

func TestMarkClamp(t *testing.T) {
	params := markParams(500)
	above := &types.Aggregate{IndexPrice: e18(1000), MarkPrice: e18(1000), Basis: e18(120), TimestampMs: 7000}
	checkMark(t, "upper clamp", above, e18(1000), e18(1200), 7000, params, e18(1050), e18(120))
	below := &types.Aggregate{IndexPrice: e18(1000), MarkPrice: e18(1000), Basis: e18(-120), TimestampMs: 7000}
	checkMark(t, "lower clamp", below, e18(1000), e18(800), 7000, params, e18(950), e18(-120))

	tiny := &types.Aggregate{IndexPrice: sdk.OneInt(), MarkPrice: sdk.OneInt(), Basis: sdk.NewInt(-5), TimestampMs: 7000}
	checkMark(t, "index 1 negative basis", tiny, sdk.OneInt(), sdk.OneInt(), 7000, params, sdk.OneInt(), sdk.NewInt(-5))
	checkMark(t, "index 1 full band", tiny, sdk.OneInt(), sdk.OneInt(), 7000, markParams(types.BpsDenominator), sdk.OneInt(), sdk.NewInt(-5))
}

func TestMarkBasisPersists(t *testing.T) {
	params := markParams(types.DefaultMaxMarkBasisBps)
	stored := &types.Aggregate{IndexPrice: e18(1000), MarkPrice: e18(1010), Basis: e18(10), TimestampMs: 1000, Height: 10, Sources: 3}
	checkMark(t, "next aggregate after a skipped block", stored, e18(1000), e18(1162), 3000, params, e18(1012), e18(12))
	if !stored.Basis.Equal(e18(10)) || stored.TimestampMs != 1000 {
		t.Fatalf("stored aggregate changed: basis %s timestamp %d", stored.Basis, stored.TimestampMs)
	}
}
