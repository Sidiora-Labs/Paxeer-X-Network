package reconcile_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/reconcile"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const oneToken = "1000000000000000000"

// seed builds a store with the indexer schema + a pool, returning the store.
func seed(t *testing.T) *store.Store {
	t.Helper()
	st := store.New(internaltest.NewPostgres(t))
	internaltest.EnsureIndexerSchema(t, st.Pool())
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xc',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	return st
}

func seedSwap(t *testing.T, st *store.Store, id, sender string, isBuy bool, in, out string, block, logIndex int64) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO indexer.swaps (id, pool_id, pool_address, sender, is_buy, amount_in, amount_out, fee, price, block_number, block_timestamp, tx_hash, log_index)
		VALUES ($1::text,'pid','0xpool',$2,$3,$4,$5,'0','0',$6,$6,$1::varchar(66),$7)`,
		id, sender, isBuy, in, out, block, logIndex); err != nil {
		t.Fatalf("seed swap: %v", err)
	}
}

func TestReconcilerBackfillsAndAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	st := seed(t)
	rdb := internaltest.NewRedis(t)
	r := reconcile.New(st, rdb, discardLogger(), 500)

	seedSwap(t, st, "0xa-0", "0xu", true, "1000000", oneToken, 100, 0)
	seedSwap(t, st, "0xb-0", "0xu", true, "3000000", oneToken, 101, 0)

	n, err := r.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if n != 2 {
		t.Fatalf("processed = %d, want 2", n)
	}

	pos, _ := st.GetPosition(ctx, "0xu", "0xpool")
	if pos == nil || pos.TotalUsdlSpent != "4000000" || pos.TokenAddress != "0xtoken" {
		t.Fatalf("folded position = %+v", pos)
	}

	cur, _ := st.GetCursor(ctx)
	if cur.LastBlock != 101 || cur.LastLogIndex != 0 {
		t.Fatalf("cursor = %+v, want (101,0)", cur)
	}

	// A second run with no new swaps is a no-op.
	n, err = r.RunOnce(ctx)
	if err != nil || n != 0 {
		t.Fatalf("idle run = %d err = %v", n, err)
	}
}

func TestReconcilerIdempotentWithConsumerOverlap(t *testing.T) {
	ctx := context.Background()
	st := seed(t)
	rdb := internaltest.NewRedis(t)
	r := reconcile.New(st, rdb, discardLogger(), 500)

	// Simulate the realtime consumer already having folded this swap.
	seedSwap(t, st, "0xdup-0", "0xu", true, "1000000", oneToken, 100, 0)
	if _, err := st.FoldTrade(ctx, store.TradeInput{
		ID: "0xdup-0", UserAddress: "0xu", PoolAddress: "0xpool", TokenAddress: "0xtoken",
		IsBuy: true, UsdlAmount: "1000000", TokenAmount: oneToken, Price: "0", Fee: "0",
		BlockNumber: 100, BlockTimestamp: 100, TxHash: "0xdup",
	}); err != nil {
		t.Fatalf("pre-fold: %v", err)
	}

	if _, err := r.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}
	pos, _ := st.GetPosition(ctx, "0xu", "0xpool")
	if pos.TradeCount != 1 || pos.TotalUsdlSpent != "1000000" {
		t.Fatalf("reconciler double-counted an already-folded swap: %+v", pos)
	}
}

func TestReconcilerBatchKeysetDrainsLargeBlock(t *testing.T) {
	ctx := context.Background()
	st := seed(t)
	rdb := internaltest.NewRedis(t)
	// Batch of 1 forces multiple runs to drain a single 3-swap block.
	r := reconcile.New(st, rdb, discardLogger(), 1)

	seedSwap(t, st, "0xa-0", "0xu", true, "1000000", oneToken, 100, 0)
	seedSwap(t, st, "0xa-1", "0xu", true, "1000000", oneToken, 100, 1)
	seedSwap(t, st, "0xa-2", "0xu", true, "1000000", oneToken, 100, 2)

	total := 0
	for i := 0; i < 5; i++ {
		n, err := r.RunOnce(ctx)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		total += n
		if n == 0 {
			break
		}
	}
	if total != 3 {
		t.Fatalf("drained %d swaps, want 3", total)
	}
	pos, _ := st.GetPosition(ctx, "0xu", "0xpool")
	if pos.TradeCount != 3 || pos.TotalUsdlSpent != "3000000" {
		t.Fatalf("position after drain = %+v", pos)
	}
}
