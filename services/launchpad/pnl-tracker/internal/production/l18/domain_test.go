package l18

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExactSyntheticArithmetic(t *testing.T) {
	for _, raw := range []string{"-1", "01", "1e6", "1.0", strings.Repeat("9", 79), "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		if _, e := amount(raw); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted %q", raw)
		}
	}
	n, e := Cashback("100000000", 10, "1000000")
	if e != nil || n != "100000" {
		t.Fatalf("cashback %s %v", n, e)
	}
	for _, tc := range []struct{ remaining, claimed, balance, want string }{{"10", "0", "1", "3"}, {"7", "1", "1", "3"}, {"4", "2", "1", "4"}} {
		n, e := EpochShare("10", tc.remaining, "3", tc.claimed, tc.balance, false)
		if e != nil || n != tc.want {
			t.Fatalf("share %+v got %s %v", tc, n, e)
		}
	}
	if n, e := EpochShare("10", "0", "3", "3", "1", true); e != nil || n != "0" {
		t.Fatalf("already-claimed final holder %s %v", n, e)
	}
	if _, e := EpochShare("10", "11", "3", "0", "1", false); e == nil {
		t.Fatal("overfunded epoch accepted")
	}
	if c := StrategyCapability(2, false); c.Available || c.Reason == "" {
		t.Fatal(c)
	}
	if c := StrategyCapability(99, true); c.Available || c.Strategy != "UNSUPPORTED" {
		t.Fatal(c)
	}
	if RewardSettlementCapability().Available || SharingCapability().Available {
		t.Fatal("invented authority")
	}
}

func TestMissingCreatorDependenciesReturnUnavailable(t *testing.T) {
	ctx := context.Background()
	for _, creator := range []*Creator{nil, NewCreator(nil, time.Now)} {
		if _, e := creator.Preview(ctx, "1", actorA); !errors.Is(e, ErrUnavailable) {
			t.Fatalf("preview without provider: %v", e)
		}
		if _, e := creator.PrepareClaim(ctx, "1", actorA, actorA); !errors.Is(e, ErrUnavailable) {
			t.Fatalf("claim without provider: %v", e)
		}
		if _, e := creator.ClaimReceipt(ctx, "0x"+strings.Repeat("a", 64), actorB, actorA); !errors.Is(e, ErrUnavailable) {
			t.Fatalf("receipt without provider: %v", e)
		}
	}
	creator := NewCreator(nil, nil)
	if _, e := creator.PrepareClaim(ctx, "1", actorA, actorA); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("claim without clock: %v", e)
	}
	var ledger *Ledger
	if _, e := ledger.PrepareEpochClaim(ctx, creator, "1", "1", actorA); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("epoch claim without clock: %v", e)
	}
}
