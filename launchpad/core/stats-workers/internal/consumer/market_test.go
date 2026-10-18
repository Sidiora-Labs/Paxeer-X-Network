package consumer_test

import (
	"context"
	"testing"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
)

// TestMarketConsumer verifies MarketConsumer bootstraps a pool_stats row with the
// launch price + precomputed initial market cap, and that redelivery of the same
// MarketCreated event is an idempotent no-op (ON CONFLICT DO NOTHING).
func TestMarketConsumer(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	mc, err := consumer.NewMarketConsumer(st, discardLogger())
	if err != nil {
		t.Fatalf("new market consumer: %v", err)
	}

	t.Run("bootstraps the initial pool_stats row", func(t *testing.T) {
		const pool = "0xmkt_new"
		ev := consumer.MarketEvent{Pool: pool, Token: "0xtok", Creator: strptr("0xCreator")}
		if err := mc.ProcessEvent(ctx, ev); err != nil {
			t.Fatalf("process: %v", err)
		}

		row, err := st.GetPoolStats(ctx, pool)
		if err != nil || row == nil {
			t.Fatalf("get: row=%v err=%v", row, err)
		}
		// initialPriceWad = 1e13; market cap = computeMarketCap(1e13).
		const initialPrice = "10000000000000"
		wantMcap, _ := shareddb.ComputeMarketCap(initialPrice)
		if row.Price != initialPrice || row.MarketCap != wantMcap {
			t.Fatalf("price=%s mcap=%s, want %s/%s", row.Price, row.MarketCap, initialPrice, wantMcap)
		}
		if row.High24h != initialPrice || row.Low24h != initialPrice {
			t.Fatalf("high/low = %s/%s, want %s", row.High24h, row.Low24h, initialPrice)
		}
		if row.TokenAddress != "0xtok" {
			t.Fatalf("token = %s, want 0xtok", row.TokenAddress)
		}
		if row.CreatorAddress == nil || *row.CreatorAddress != "0xCreator" {
			t.Fatalf("creator = %v", row.CreatorAddress)
		}
	})

	t.Run("redelivery is an idempotent no-op", func(t *testing.T) {
		const pool = "0xmkt_idem"
		first := consumer.MarketEvent{Pool: pool, Token: "0xtok1", Creator: strptr("0xc1")}
		if err := mc.ProcessEvent(ctx, first); err != nil {
			t.Fatalf("first: %v", err)
		}
		// Second event for the SAME pool with different token/creator must not mutate.
		second := consumer.MarketEvent{Pool: pool, Token: "0xtok2", Creator: strptr("0xc2")}
		if err := mc.ProcessEvent(ctx, second); err != nil {
			t.Fatalf("second: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, pool)
		if row.TokenAddress != "0xtok1" || row.CreatorAddress == nil || *row.CreatorAddress != "0xc1" {
			t.Fatalf("row mutated on redelivery: token=%s creator=%v", row.TokenAddress, row.CreatorAddress)
		}
	})

	t.Run("nil creator is persisted as NULL", func(t *testing.T) {
		const pool = "0xmkt_nocreator"
		if err := mc.ProcessEvent(ctx, consumer.MarketEvent{Pool: pool, Token: "0xtok", Creator: nil}); err != nil {
			t.Fatalf("process: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, pool)
		if row.CreatorAddress != nil {
			t.Fatalf("creator = %v, want nil", row.CreatorAddress)
		}
	})
}
