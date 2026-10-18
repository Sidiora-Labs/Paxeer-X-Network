package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

func tx(id, addr, sender string, isBuy bool, ts int64) store.TransactionRow {
	return store.TransactionRow{
		ID: id, PoolAddress: addr, Sender: sender, IsBuy: isBuy,
		AmountIn: "10", AmountOut: "20", Price: "1000", Fee: "1",
		BlockTimestamp: ts, TxHash: id,
	}
}

func TestInsertAndListTransactions(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xtxpool"

	t.Run("InsertTransaction is idempotent on id", func(t *testing.T) {
		row := tx("h1-0", addr, "0xa", true, 100)
		if err := st.InsertTransaction(ctx, row); err != nil {
			t.Fatalf("insert: %v", err)
		}
		// Re-insert with same id but different fields -> ignored.
		row.AmountIn = "999"
		if err := st.InsertTransaction(ctx, row); err != nil {
			t.Fatalf("reinsert: %v", err)
		}
		var n int
		var amountIn string
		if err := pool.QueryRow(ctx, `SELECT COUNT(*), MAX(amount_in) FROM stats.pool_transactions WHERE id='h1-0'`).Scan(&n, &amountIn); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 || amountIn != "10" {
			t.Fatalf("idempotency broken: n=%d amountIn=%s", n, amountIn)
		}
	})

	t.Run("ListTransactions ordering, side filter and paging", func(t *testing.T) {
		a := "0xlist"
		_ = st.InsertTransaction(ctx, tx("b-300", a, "0xa", true, 300))
		_ = st.InsertTransaction(ctx, tx("s-200", a, "0xb", false, 200))
		_ = st.InsertTransaction(ctx, tx("b-100", a, "0xc", true, 100))

		all, err := st.ListTransactions(ctx, a, 50, 0, "all")
		if err != nil {
			t.Fatalf("list all: %v", err)
		}
		if len(all) != 3 || all[0].ID != "b-300" || all[2].ID != "b-100" {
			t.Fatalf("all order = %+v", all)
		}

		buys, _ := st.ListTransactions(ctx, a, 50, 0, "buy")
		if len(buys) != 2 || buys[0].ID != "b-300" || buys[1].ID != "b-100" {
			t.Fatalf("buys = %+v", buys)
		}
		sells, _ := st.ListTransactions(ctx, a, 50, 0, "sell")
		if len(sells) != 1 || sells[0].ID != "s-200" {
			t.Fatalf("sells = %+v", sells)
		}

		page, _ := st.ListTransactions(ctx, a, 1, 1, "all")
		if len(page) != 1 || page[0].ID != "s-200" {
			t.Fatalf("paged = %+v", page)
		}
	})

	t.Run("ListTransactions empty is non-nil slice", func(t *testing.T) {
		got, err := st.ListTransactions(ctx, "0xnopool", 50, 0, "all")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if got == nil {
			t.Fatal("want non-nil empty slice for JSON '[]' parity")
		}
	})

	t.Run("CreatorTransactions filters by sender", func(t *testing.T) {
		a := "0xcreatortx"
		_ = st.InsertTransaction(ctx, tx("c-300", a, "0xcreator", true, 300))
		_ = st.InsertTransaction(ctx, tx("c-100", a, "0xcreator", false, 100))
		_ = st.InsertTransaction(ctx, tx("o-200", a, "0xother", true, 200))

		got, err := st.CreatorTransactions(ctx, a, "0xcreator")
		if err != nil {
			t.Fatalf("creator txs: %v", err)
		}
		if len(got) != 2 || got[0].ID != "c-300" || got[1].ID != "c-100" {
			t.Fatalf("creator txs = %+v", got)
		}
	})
}

func crossSwap(id, sender, tokenIn, tokenOut string, ts int64) store.CrossTokenSwapRow {
	return store.CrossTokenSwapRow{
		ID: id, Sender: sender, TokenIn: tokenIn, TokenOut: tokenOut,
		PoolIn: "0xpin", PoolOut: "0xpout", AmountIn: "10", IntermediateUsdl: "5",
		AmountOut: "20", FeeIn: "1", FeeOut: "1", BlockTimestamp: ts, TxHash: id,
	}
}

func TestCrossTokenSwaps(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	t.Run("InsertCrossTokenSwap idempotent", func(t *testing.T) {
		s := crossSwap("x1-0", "0xw", "0xtin", "0xtout", 100)
		if err := st.InsertCrossTokenSwap(ctx, s); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if err := st.InsertCrossTokenSwap(ctx, s); err != nil {
			t.Fatalf("reinsert: %v", err)
		}
		var n int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM stats.cross_token_swaps WHERE id='x1-0'`).Scan(&n)
		if n != 1 {
			t.Fatalf("rows = %d, want 1", n)
		}
	})

	t.Run("ListCrossTokenSwapsByWallet desc + paging", func(t *testing.T) {
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("w-300", "0xwallet", "0xA", "0xB", 300))
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("w-100", "0xwallet", "0xB", "0xC", 100))
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("w-other", "0xnope", "0xA", "0xB", 200))

		got, err := st.ListCrossTokenSwapsByWallet(ctx, "0xwallet", 50, 0)
		if err != nil {
			t.Fatalf("by wallet: %v", err)
		}
		if len(got) != 2 || got[0].ID != "w-300" || got[1].ID != "w-100" {
			t.Fatalf("by wallet = %+v", got)
		}
	})

	t.Run("ListCrossTokenSwapsByToken matches token_in OR token_out", func(t *testing.T) {
		// Stored lower-cased (the route lower-cases before calling).
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("t-in", "0xw1", "0xtarget", "0xother", 300))
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("t-out", "0xw2", "0xother", "0xtarget", 200))
		_ = st.InsertCrossTokenSwap(ctx, crossSwap("t-none", "0xw3", "0xx", "0xy", 100))

		got, err := st.ListCrossTokenSwapsByToken(ctx, "0xtarget", 50, 0)
		if err != nil {
			t.Fatalf("by token: %v", err)
		}
		if len(got) != 2 || got[0].ID != "t-in" || got[1].ID != "t-out" {
			t.Fatalf("by token = %+v (want t-in then t-out, desc by ts)", got)
		}
	})
}
