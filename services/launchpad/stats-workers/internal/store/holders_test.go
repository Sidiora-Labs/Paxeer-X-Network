package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// 1e14 tokens = 10% of the 1e15 total supply => 1000 bps.
const tenPctBalance = "100000000000000"

func insertHolder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, addr, holder, balance, pct string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats.pool_holders (pool_address, holder_address, balance, pct_of_supply, last_updated)
		VALUES ($1,$2,$3,$4,1)`, addr, holder, balance, pct); err != nil {
		t.Fatalf("insert holder: %v", err)
	}
}

func TestApplyHolderDelta(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xhpool"
	const holder = "0xholder"

	// New holder buying: tokenDelta = amountOut.
	applied, err := st.ApplyHolderDelta(ctx, addr, holder, true, "0", tenPctBalance, 100)
	if err != nil || !applied {
		t.Fatalf("new buy: applied=%v err=%v", applied, err)
	}
	bal, ok, _ := st.GetHolderBalance(ctx, addr, holder)
	if !ok || bal != tenPctBalance {
		t.Fatalf("balance = %s ok=%v, want %s", bal, ok, tenPctBalance)
	}
	var pct string
	if err := pool.QueryRow(ctx, `SELECT pct_of_supply FROM stats.pool_holders WHERE pool_address=$1 AND holder_address=$2`, addr, holder).Scan(&pct); err != nil {
		t.Fatalf("read pct: %v", err)
	}
	if pct != "1000" {
		t.Fatalf("pct = %s, want 1000 (10%%)", pct)
	}

	// Existing holder buying again: balance adds -> 2e14 (20%).
	if applied, err = st.ApplyHolderDelta(ctx, addr, holder, true, "0", tenPctBalance, 101); err != nil || !applied {
		t.Fatalf("buy add: applied=%v err=%v", applied, err)
	}
	bal, _, _ = st.GetHolderBalance(ctx, addr, holder)
	if bal != "200000000000000" {
		t.Fatalf("balance = %s, want 200000000000000", bal)
	}

	// Selling part: tokenDelta = amountIn -> back to 1e14.
	if applied, err = st.ApplyHolderDelta(ctx, addr, holder, false, tenPctBalance, "0", 102); err != nil || !applied {
		t.Fatalf("sell sub: applied=%v err=%v", applied, err)
	}
	bal, _, _ = st.GetHolderBalance(ctx, addr, holder)
	if bal != tenPctBalance {
		t.Fatalf("balance = %s, want %s", bal, tenPctBalance)
	}

	// Selling the rest: balance hits zero -> row deleted, but applied=true.
	if applied, err = st.ApplyHolderDelta(ctx, addr, holder, false, tenPctBalance, "0", 103); err != nil || !applied {
		t.Fatalf("sell to zero: applied=%v err=%v", applied, err)
	}
	if _, ok, _ := st.GetHolderBalance(ctx, addr, holder); ok {
		t.Fatal("holder row should be deleted at zero balance")
	}

	// New holder selling: no-op, applied=false, no row created.
	applied, err = st.ApplyHolderDelta(ctx, addr, "0xnewseller", false, tenPctBalance, "0", 104)
	if err != nil {
		t.Fatalf("new sell err: %v", err)
	}
	if applied {
		t.Fatal("new-holder sell must report applied=false")
	}
	if _, ok, _ := st.GetHolderBalance(ctx, addr, "0xnewseller"); ok {
		t.Fatal("new-holder sell must not create a row")
	}

	// Over-sell clamps to zero and deletes (BigintSub clamp).
	if _, err := st.ApplyHolderDelta(ctx, addr, "0xwhale", true, "0", tenPctBalance, 105); err != nil {
		t.Fatalf("seed whale: %v", err)
	}
	if applied, err = st.ApplyHolderDelta(ctx, addr, "0xwhale", false, "999999999999999999", "0", 106); err != nil || !applied {
		t.Fatalf("over-sell: applied=%v err=%v", applied, err)
	}
	if _, ok, _ := st.GetHolderBalance(ctx, addr, "0xwhale"); ok {
		t.Fatal("over-sell must delete the row (clamped to zero)")
	}
}

func TestRefreshPoolHolderStats(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	t.Run("computes counts, concentration, creator holdings and risk", func(t *testing.T) {
		const addr = "0xrefresh"
		const now int64 = 1_000_000
		createdAt := now - 200*3600 // > 168h old: no age factor

		// pool_stats with creator + 10 unique traders (no low_activity factor).
		if _, err := pool.Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, creator_address, unique_traders_24h, created_at, updated_at)
			VALUES ($1,'0xtok','0xcreator',10,$2,$2)`, addr, createdAt); err != nil {
			t.Fatalf("seed pool_stats: %v", err)
		}
		// Holders: creator 50%, H2 30%, H3 10% => top10 = 90% = 9000 bps.
		insertHolder(t, ctx, pool, addr, "0xcreator", "500000000000000", "5000")
		insertHolder(t, ctx, pool, addr, "0xh2", "300000000000000", "3000")
		insertHolder(t, ctx, pool, addr, "0xh3", "100000000000000", "1000")

		if err := st.RefreshPoolHolderStats(ctx, addr, now); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.HolderCount != 3 {
			t.Errorf("holder_count = %d, want 3", row.HolderCount)
		}
		if row.Top10Concentration != "9000" {
			t.Errorf("top10_concentration = %s, want 9000", row.Top10Concentration)
		}
		if row.CreatorHoldingsPct != "5000" {
			t.Errorf("creator_holdings_pct = %s, want 5000", row.CreatorHoldingsPct)
		}
		// extreme_concentration(25, >8000) + creator_significant(10, >2000 not >5000) + few_holders(10, <5) = 45.
		if row.RiskRating != 45 {
			t.Errorf("risk_rating = %d, want 45", row.RiskRating)
		}
		want := `["extreme_concentration","creator_significant","few_holders"]`
		if row.RiskFactors == nil || *row.RiskFactors != want {
			t.Errorf("risk_factors = %v, want %s", row.RiskFactors, want)
		}
	})

	t.Run("absent pool_stats row is a no-op (no error)", func(t *testing.T) {
		// Holders exist but no pool_stats row -> UPDATE affects 0 rows.
		const addr = "0xorphan"
		insertHolder(t, ctx, pool, addr, "0xh", "100000000000000", "1000")
		if err := st.RefreshPoolHolderStats(ctx, addr, 1_000_000); err != nil {
			t.Fatalf("refresh orphan: %v", err)
		}
	})
}

func TestHolderReads(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xreads"
	insertHolder(t, ctx, pool, addr, "0xbig", "500000000000000", "5000")
	insertHolder(t, ctx, pool, addr, "0xmid", "300000000000000", "3000")
	insertHolder(t, ctx, pool, addr, "0xsmall", "100000000000000", "1000")

	t.Run("CountHolders", func(t *testing.T) {
		n, err := st.CountHolders(ctx, addr)
		if err != nil || n != 3 {
			t.Fatalf("count = %d err=%v, want 3", n, err)
		}
	})

	t.Run("ListHolders ordered by balance desc with paging", func(t *testing.T) {
		hs, err := st.ListHolders(ctx, addr, 2, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(hs) != 2 || hs[0].HolderAddress != "0xbig" || hs[1].HolderAddress != "0xmid" {
			t.Fatalf("page1 = %+v", hs)
		}
		hs2, _ := st.ListHolders(ctx, addr, 2, 2)
		if len(hs2) != 1 || hs2[0].HolderAddress != "0xsmall" {
			t.Fatalf("page2 = %+v", hs2)
		}
	})

	t.Run("ListHoldersByBalance returns all desc", func(t *testing.T) {
		hs, err := st.ListHoldersByBalance(ctx, addr)
		if err != nil {
			t.Fatalf("all: %v", err)
		}
		if len(hs) != 3 || hs[0].HolderAddress != "0xbig" || hs[2].HolderAddress != "0xsmall" {
			t.Fatalf("all = %+v", hs)
		}
	})

	t.Run("GetHolderBalance present and absent", func(t *testing.T) {
		bal, ok, err := st.GetHolderBalance(ctx, addr, "0xbig")
		if err != nil || !ok || bal != "500000000000000" {
			t.Fatalf("present: bal=%s ok=%v err=%v", bal, ok, err)
		}
		_, ok, err = st.GetHolderBalance(ctx, addr, "0xmissing")
		if err != nil || ok {
			t.Fatalf("absent: ok=%v err=%v", ok, err)
		}
	})
}
