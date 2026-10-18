package consumer_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
)

// TestMultihopConsumer verifies the native-Router MultihopSwap consumer records a
// cross-token swap (all indexer-enriched NOT NULL columns) and is idempotent on
// redelivery (ON CONFLICT (id) DO NOTHING). This is the native DEX multihop, not
// the out-of-scope MetaAG aggregator event.
func TestMultihopConsumer(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	mc := consumer.NewMultihopConsumer(st, discardLogger())

	ev := consumer.MultihopEvent{
		Sender:           "0xrouteruser",
		TokenIn:          "0xtokenin",
		TokenOut:         "0xtokenout",
		PoolIn:           "0xpoolin",
		PoolOut:          "0xpoolout",
		AmountIn:         "1000000",
		IntermediateUsdl: "500000",
		AmountOut:        "2000000",
		FeeIn:            "100",
		FeeOut:           "200",
		BlockTimestamp:   1700,
		TxHash:           "0xmh",
		LogIndex:         3,
	}

	t.Run("records the cross-token swap", func(t *testing.T) {
		if err := mc.ProcessEvent(ctx, ev); err != nil {
			t.Fatalf("process: %v", err)
		}
		swaps, err := st.ListCrossTokenSwapsByWallet(ctx, "0xrouteruser", 50, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(swaps) != 1 {
			t.Fatalf("swaps = %d, want 1", len(swaps))
		}
		got := swaps[0]
		if got.ID != "0xmh-3" {
			t.Errorf("id = %s, want 0xmh-3 (txHash-logIndex)", got.ID)
		}
		if got.TokenIn != "0xtokenin" || got.TokenOut != "0xtokenout" ||
			got.PoolIn != "0xpoolin" || got.PoolOut != "0xpoolout" {
			t.Errorf("addresses mismatched: %+v", got)
		}
		if got.AmountIn != "1000000" || got.IntermediateUsdl != "500000" || got.AmountOut != "2000000" ||
			got.FeeIn != "100" || got.FeeOut != "200" {
			t.Errorf("amounts/fees mismatched: %+v", got)
		}
	})

	t.Run("redelivery is idempotent", func(t *testing.T) {
		if err := mc.ProcessEvent(ctx, ev); err != nil {
			t.Fatalf("reprocess: %v", err)
		}
		var n int
		if err := st.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM stats.cross_token_swaps WHERE id='0xmh-3'`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("rows = %d, want 1 after redelivery", n)
		}
	})
}
