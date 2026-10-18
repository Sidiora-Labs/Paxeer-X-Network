package store_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
)

func newStore(t *testing.T) (*store.Store, *pgxpool.Pool, *goredis.Client) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	rdb := internaltest.NewRedis(t)
	return store.New(pool, rdb), pool, rdb
}

func insertPoolStats(t *testing.T, pool *pgxpool.Pool, addr, token, price, mcap string, holders int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, holder_count, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,100,200)`, addr, token, price, mcap, holders)
	if err != nil {
		t.Fatalf("insert pool_stats: %v", err)
	}
}

func TestStatsBatch_CacheAndDBFallback(t *testing.T) {
	st, pool, rdb := newStore(t)
	ctx := context.Background()
	// 0xA cached, 0xB only in DB, 0xC absent everywhere.
	_ = rdb.Set(ctx, "stats:0xA", `{"poolAddress":"0xA","price":"1"}`, 0).Err()
	insertPoolStats(t, pool, "0xB", "0xTOKENB", "2", "0", 0)

	res, err := st.StatsBatch(ctx, []string{"0xA", "0xB", "0xC"})
	if err != nil {
		t.Fatalf("StatsBatch: %v", err)
	}
	if _, ok := res["0xA"]; !ok {
		t.Error("0xA (cached) missing")
	}
	if _, ok := res["0xB"]; !ok {
		t.Error("0xB (db) missing")
	}
	if _, ok := res["0xC"]; ok {
		t.Error("0xC should be absent")
	}
}

func TestCachedJSON_HitAndMiss(t *testing.T) {
	st, _, rdb := newStore(t)
	ctx := context.Background()
	_ = rdb.Set(ctx, "platform:metrics", `{"totalVolume":"123"}`, 0).Err()

	raw, found, err := st.CachedJSON(ctx, "platform:metrics")
	if err != nil || !found {
		t.Fatalf("hit: found=%v err=%v", found, err)
	}
	if string(raw) != `{"totalVolume":"123"}` {
		t.Errorf("value = %s", raw)
	}

	_, found, err = st.CachedJSON(ctx, "platform:nope")
	if err != nil {
		t.Fatalf("miss err = %v", err)
	}
	if found {
		t.Error("missing key should report found=false")
	}
}

func TestPing(t *testing.T) {
	st, _, _ := newStore(t)
	if err := st.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestProductionLegacyStoreCancellationReleasesReadTransactions(t *testing.T) {
	st, pool, rdb := newStore(t)
	ctx := context.Background()
	if err := rdb.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, "LOCK TABLE stats.pool_stats IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	reads := []func(context.Context) error{
		func(ctx context.Context) error { _, err := st.StatsBatch(ctx, []string{"0xPOOL"}); return err },
	}
	for _, read := range reads {
		before := pool.Stat().AcquiredConns()
		request, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		start := time.Now()
		err := read(request)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 150*time.Millisecond {
			t.Fatal("cancelled read", err, time.Since(start))
		}
		// A cancelled read closes its connection, and pgxpool destroys closed
		// connections asynchronously, so give the destroy a bounded window.
		for settle := time.Now().Add(time.Second); pool.Stat().AcquiredConns() != before && time.Now().Before(settle); {
			time.Sleep(5 * time.Millisecond)
		}
		if pool.Stat().AcquiredConns() != before {
			t.Fatal("read leaked a connection", pool.Stat())
		}
	}
	if err = lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var value int
	if err = pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil || value != 1 {
		t.Fatal("pool failed to recover", err, value)
	}
}

func TestProductionLegacyStoreUsesReadOnlyTransactionAndStatementBudget(t *testing.T) {
	_, pool, rdb := newStore(t)
	st := store.New(pool, rdb)
	ctx := context.Background()
	lock, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, "LOCK TABLE stats.pool_stats IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if rows, err := st.StatsBatch(ctx, []string{"0xPOOL"}); err == nil || rows != nil || time.Since(start) > 300*time.Millisecond {
		t.Fatal(rows, err, time.Since(start))
	}
}
