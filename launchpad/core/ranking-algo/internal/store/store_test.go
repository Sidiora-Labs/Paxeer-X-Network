package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/store"
)

func now() int64 { return time.Now().Unix() }

type statRow struct {
	addr      string
	vol24h    string
	vol1h     string
	vol5m     string
	mcap      string
	pchg24h   string
	buy       int
	sell      int
	traders   int
	holders   int
	updatedAt int64
}

func insertStat(t *testing.T, pool *pgxpool.Pool, r statRow) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO stats.pool_stats (
			pool_address, volume_24h, volume_1h, volume_5m, market_cap, price_change_24h,
			buy_count_24h, sell_count_24h, unique_traders_24h, holder_count, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, r.addr, r.vol24h, r.vol1h, r.vol5m, r.mcap, r.pchg24h,
		r.buy, r.sell, r.traders, r.holders, r.updatedAt)
	if err != nil {
		t.Fatalf("insert stat %s: %v", r.addr, err)
	}
}

func insertPool(t *testing.T, pool *pgxpool.Pool, addr string, createdAt int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO indexer.pools (pool_address, created_at) VALUES ($1,$2)`, addr, createdAt); err != nil {
		t.Fatalf("insert pool %s: %v", addr, err)
	}
}

func TestTrendingCandidatesFilters(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	insertStat(t, pool, statRow{addr: "0xkeep", vol24h: "100", traders: 5, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xno_traders", vol24h: "100", traders: 0, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xzero_vol", vol24h: "0", traders: 5, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xstale", vol24h: "100", traders: 5, updatedAt: now() - 200000})

	got, err := st.TrendingCandidates(ctx, now()-86400)
	if err != nil {
		t.Fatalf("TrendingCandidates: %v", err)
	}
	if len(got) != 1 || got[0].PoolAddress != "0xkeep" {
		t.Fatalf("expected only 0xkeep, got %+v", got)
	}
}

func TestBreakoutCandidatesFilters(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	insertStat(t, pool, statRow{addr: "0xkeep", vol1h: "10", updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xzero_1h", vol1h: "0", updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xstale", vol1h: "10", updatedAt: now() - 7200})

	got, err := st.BreakoutCandidates(ctx, now()-3600)
	if err != nil {
		t.Fatalf("BreakoutCandidates: %v", err)
	}
	if len(got) != 1 || got[0].PoolAddress != "0xkeep" {
		t.Fatalf("expected only 0xkeep, got %+v", got)
	}
}

func TestMoversCandidatesFilters(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	insertStat(t, pool, statRow{addr: "0xkeep", vol24h: "100", mcap: "1000", holders: 3, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xno_holders", vol24h: "100", mcap: "1000", holders: 0, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xno_mcap", vol24h: "100", mcap: "0", holders: 3, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xno_vol", vol24h: "0", mcap: "1000", holders: 3, updatedAt: now()})

	got, err := st.MoversCandidates(ctx, now()-86400)
	if err != nil {
		t.Fatalf("MoversCandidates: %v", err)
	}
	if len(got) != 1 || got[0].PoolAddress != "0xkeep" {
		t.Fatalf("expected only 0xkeep, got %+v", got)
	}
}

func TestUnusualCandidatesFilters(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	insertStat(t, pool, statRow{addr: "0xkeep", buy: 1, sell: 0, updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xno_trades", buy: 0, sell: 0, updatedAt: now()})

	got, err := st.UnusualCandidates(ctx, now()-86400)
	if err != nil {
		t.Fatalf("UnusualCandidates: %v", err)
	}
	if len(got) != 1 || got[0].PoolAddress != "0xkeep" {
		t.Fatalf("expected only 0xkeep, got %+v", got)
	}
}

func TestTopVolumeOrderingAndLimit(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	// Insert out of numeric order; verify NUMERIC ordering (not lexical).
	insertStat(t, pool, statRow{addr: "0xa", vol24h: "9", updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xb", vol24h: "100", updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xc", vol24h: "20", updatedAt: now()})
	insertStat(t, pool, statRow{addr: "0xzero", vol24h: "0", updatedAt: now()})

	got, err := st.TopVolume(ctx, now()-86400, 2)
	if err != nil {
		t.Fatalf("TopVolume: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
	if got[0].Address != "0xb" || got[1].Address != "0xc" {
		t.Fatalf("expected NUMERIC desc [0xb,0xc], got %+v", got)
	}
}

func TestNewPoolsOrderingAndLimit(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)
	ctx := context.Background()

	insertPool(t, pool, "0xold", 100)
	insertPool(t, pool, "0xnew", 300)
	insertPool(t, pool, "0xmid", 200)

	got, err := st.NewPools(ctx, 2)
	if err != nil {
		t.Fatalf("NewPools: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
	if got[0].Address != "0xnew" || got[1].Address != "0xmid" {
		t.Fatalf("expected newest-first [0xnew,0xmid], got %+v", got)
	}
	if got[0].CreatedAt != 300 {
		t.Errorf("CreatedAt = %d, want 300", got[0].CreatedAt)
	}
}
