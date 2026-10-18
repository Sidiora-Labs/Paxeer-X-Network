package consumer_test

import (
	"context"
	"testing"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
)

// TestStateConsumer verifies StateConsumer refreshes price + market cap for an
// existing pool on a PoolStateUpdated event, and is a silent no-op for an unknown
// pool (parity: the UPDATE matches zero rows).
func TestStateConsumer(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	sc := consumer.NewStateConsumer(st, discardLogger())

	t.Run("refreshes price and market cap", func(t *testing.T) {
		const addr = "0xstate"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		before, _ := st.GetPoolStats(ctx, addr)

		newPrice := "30000000000000" // 3e13
		if err := sc.ProcessEvent(ctx, consumer.StateEvent{PoolAddress: addr, Price: newPrice}); err != nil {
			t.Fatalf("process: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.Price != newPrice {
			t.Errorf("price = %s, want %s", row.Price, newPrice)
		}
		wantMcap, _ := shareddb.ComputeMarketCap(newPrice)
		if row.MarketCap != wantMcap {
			t.Errorf("market_cap = %s, want %s", row.MarketCap, wantMcap)
		}
		if row.UpdatedAt < before.UpdatedAt {
			t.Errorf("updated_at went backwards: %d < %d", row.UpdatedAt, before.UpdatedAt)
		}
	})

	t.Run("unknown pool is a silent no-op", func(t *testing.T) {
		if err := sc.ProcessEvent(ctx, consumer.StateEvent{PoolAddress: "0xunknown", Price: "1"}); err != nil {
			t.Fatalf("expected no error for unknown pool, got %v", err)
		}
		if row, _ := st.GetPoolStats(ctx, "0xunknown"); row != nil {
			t.Fatalf("unknown pool must not be created: %+v", row)
		}
	})

	t.Run("empty price yields zero market cap", func(t *testing.T) {
		const addr = "0xstate_zero"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		if err := sc.ProcessEvent(ctx, consumer.StateEvent{PoolAddress: addr, Price: ""}); err != nil {
			t.Fatalf("process: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.Price != "" || row.MarketCap != "0" {
			t.Fatalf("price=%q mcap=%s, want empty price + 0 mcap", row.Price, row.MarketCap)
		}
	})
}
