package ranker_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/store"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func seedStat(t *testing.T, pool *pgxpool.Pool, addr, vol24h, vol1h, vol5m, mcap, pchg string, buy, sell, traders, holders int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO stats.pool_stats (
			pool_address, volume_24h, volume_1h, volume_5m, market_cap, price_change_24h,
			buy_count_24h, sell_count_24h, unique_traders_24h, holder_count, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, addr, vol24h, vol1h, vol5m, mcap, pchg, buy, sell, traders, holders, time.Now().Unix())
	if err != nil {
		t.Fatalf("seed stat %s: %v", addr, err)
	}
}

func newService(t *testing.T) (*ranker.Service, *goredis.Client, *pgxpool.Pool) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	rdb := internaltest.NewRedis(t)
	svc := ranker.NewService(store.New(pool), rdb, 200, quietLogger())
	return svc, rdb, pool
}

func TestRunAllProducesRankings(t *testing.T) {
	svc, rdb, pool := newService(t)
	ctx := context.Background()

	seedStat(t, pool, "0xhot", "2400", "200", "30", "100000", "80", 200, 150, 40, 60)
	seedStat(t, pool, "0xmid", "1200", "60", "5", "50000", "20", 50, 40, 20, 30)
	seedStat(t, pool, "0xcold", "100", "1", "0", "10000", "2", 3, 2, 2, 5)

	if err := svc.RunAll(ctx); err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	for _, key := range []string{"ranking:trending", "ranking:breakout", "ranking:top_volume", "ranking:unusual"} {
		n, err := rdb.ZCard(ctx, key).Result()
		if err != nil {
			t.Fatalf("zcard %s: %v", key, err)
		}
		if n == 0 {
			t.Errorf("%s: expected entries, got 0", key)
		}
	}

	// top_volume must be ordered by 24h volume descending.
	tv, err := rdb.ZRangeArgs(ctx, goredis.ZRangeArgs{Key: "ranking:top_volume", Start: 0, Stop: -1, Rev: true}).Result()
	if err != nil {
		t.Fatalf("zrevrange top_volume: %v", err)
	}
	if len(tv) != 3 || tv[0] != "0xhot" || tv[2] != "0xcold" {
		t.Fatalf("top_volume order = %v, want [0xhot 0xmid 0xcold]", tv)
	}
}

func TestMoversExcludesTrendingTop(t *testing.T) {
	svc, rdb, pool := newService(t)
	ctx := context.Background()

	seedStat(t, pool, "0xhot", "2400", "200", "30", "100000", "80", 200, 150, 40, 60)
	seedStat(t, pool, "0xmover", "1200", "300", "5", "50000", "20", 50, 40, 20, 30)

	// Run trending first so ranking:trending is populated, then movers reads it.
	if err := svc.Trending(ctx); err != nil {
		t.Fatalf("Trending: %v", err)
	}
	// Force 0xhot into the trending set even if scoring would not (deterministic).
	if err := rdb.ZAdd(ctx, "ranking:trending", goredis.Z{Score: 99, Member: "0xhot"}).Err(); err != nil {
		t.Fatalf("seed trending: %v", err)
	}
	if err := svc.Movers(ctx); err != nil {
		t.Fatalf("Movers: %v", err)
	}

	movers, err := rdb.ZRange(ctx, "ranking:movers", 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange movers: %v", err)
	}
	for _, m := range movers {
		if m == "0xhot" {
			t.Fatalf("0xhot is in the trending top-50 and must be excluded from movers, got %v", movers)
		}
	}
}

func TestRunNewOrdersByCreatedAt(t *testing.T) {
	svc, rdb, pool := newService(t)
	ctx := context.Background()

	for addr, created := range map[string]int64{"0xold": 100, "0xnew": 300, "0xmid": 200} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO indexer.pools (pool_address, created_at) VALUES ($1,$2)`, addr, created); err != nil {
			t.Fatalf("insert pool %s: %v", addr, err)
		}
	}

	if err := svc.RunNew(ctx); err != nil {
		t.Fatalf("RunNew: %v", err)
	}

	got, err := rdb.ZRangeArgs(ctx, goredis.ZRangeArgs{Key: "ranking:new", Start: 0, Stop: -1, Rev: true}).Result()
	if err != nil {
		t.Fatalf("zrevrange new: %v", err)
	}
	want := []string{"0xnew", "0xmid", "0xold"}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("new order = %v, want %v", got, want)
		}
	}

	// Score must equal the creation timestamp.
	score, err := rdb.ZScore(ctx, "ranking:new", "0xnew").Result()
	if err != nil {
		t.Fatalf("zscore: %v", err)
	}
	if score != 300 {
		t.Errorf("0xnew score = %v, want 300", score)
	}
}

func TestRunAllEmptyDBProducesNoEntries(t *testing.T) {
	svc, rdb, _ := newService(t)
	ctx := context.Background()

	if err := svc.RunAll(ctx); err != nil {
		t.Fatalf("RunAll on empty DB: %v", err)
	}
	for _, key := range []string{"ranking:trending", "ranking:breakout", "ranking:top_volume", "ranking:movers", "ranking:unusual"} {
		n, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			t.Fatalf("exists %s: %v", key, err)
		}
		if n != 0 {
			t.Errorf("%s should not exist for empty DB", key)
		}
	}
}
