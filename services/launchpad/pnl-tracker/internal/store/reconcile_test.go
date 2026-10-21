package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// seedSwap inserts an indexer.swaps row.
func seedSwap(t *testing.T, st *store.Store, id, pool, sender string, isBuy bool, in, out string, block, logIndex int64) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO indexer.swaps (id, pool_id, pool_address, sender, is_buy, amount_in, amount_out, fee, price, block_number, block_timestamp, tx_hash, log_index)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'0','0',$8,$9,$10,$11)`,
		id, "pid", pool, sender, isBuy, in, out, block, block, id, logIndex); err != nil {
		t.Fatalf("seed swap: %v", err)
	}
}

func TestCursorAndUnreconciledSwaps(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	internaltest.EnsureIndexerSchema(t, st.Pool())

	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xc',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	// Two swaps in block 100 (log 0,1) then one in block 101.
	seedSwap(t, st, "0xs1-0", "0xpool", "0xa", true, "1000000", oneToken, 100, 0)
	seedSwap(t, st, "0xs1-1", "0xpool", "0xb", false, oneToken, "500000", 100, 1)
	seedSwap(t, st, "0xs2-0", "0xpool", "0xa", true, "2000000", oneToken, 101, 0)

	t.Run("fresh cursor sees all swaps in keyset order with token joined", func(t *testing.T) {
		cur, err := st.GetCursor(ctx)
		if err != nil {
			t.Fatalf("cursor: %v", err)
		}
		if cur.LastBlock != 0 || cur.LastLogIndex != -1 {
			t.Fatalf("fresh cursor = %+v, want (0,-1)", cur)
		}
		swaps, err := st.ListUnreconciledSwaps(ctx, cur, 10)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(swaps) != 3 {
			t.Fatalf("swaps = %d, want 3", len(swaps))
		}
		if swaps[0].ID != "0xs1-0" || swaps[1].ID != "0xs1-1" || swaps[2].ID != "0xs2-0" {
			t.Fatalf("order = %s,%s,%s", swaps[0].ID, swaps[1].ID, swaps[2].ID)
		}
		if swaps[0].TokenAddress != "0xtoken" {
			t.Errorf("token join = %q, want 0xtoken", swaps[0].TokenAddress)
		}
	})

	t.Run("keyset paginates within a block and advances forward", func(t *testing.T) {
		cur, _ := st.GetCursor(ctx)
		// Batch of 1 drains block 100 log 0 first.
		first, _ := st.ListUnreconciledSwaps(ctx, cur, 1)
		if len(first) != 1 || first[0].ID != "0xs1-0" {
			t.Fatalf("first batch = %+v", first)
		}
		if err := st.AdvanceCursor(ctx, first[0].BlockNumber, first[0].LogIndex, first[0].ID, 5); err != nil {
			t.Fatalf("advance: %v", err)
		}
		cur2, _ := st.GetCursor(ctx)
		next, _ := st.ListUnreconciledSwaps(ctx, cur2, 10)
		if len(next) != 2 || next[0].ID != "0xs1-1" {
			t.Fatalf("next batch = %+v (cursor=%+v)", next, cur2)
		}
	})

	t.Run("IndexerHead is the max swap block", func(t *testing.T) {
		head, err := st.IndexerHead(ctx)
		if err != nil || head != 101 {
			t.Fatalf("indexer head = %d err = %v", head, err)
		}
	})
}

func TestConsumerBlockTracksFoldedTrades(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if b, err := st.ConsumerBlock(ctx); err != nil || b != 0 {
		t.Fatalf("empty consumer block = %d err = %v", b, err)
	}
	mustFold(t, st, trade("0xa-0", "0xu", "0xp", "0xt", true, usdl1, oneToken, 42, 1))
	b, err := st.ConsumerBlock(ctx)
	if err != nil || b != 42 {
		t.Fatalf("consumer block = %d err = %v, want 42", b, err)
	}
}
