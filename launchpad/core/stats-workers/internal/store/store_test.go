package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

func strptr(s string) *string { return &s }

// seedInitial inserts a baseline pool_stats row for addr via the production
// InsertInitialPoolStats path.
func seedInitial(t *testing.T, ctx context.Context, st *store.Store, addr string, creator *string) {
	t.Helper()
	if _, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{
		PoolAddress:    addr,
		TokenAddress:   addr + "-tok",
		CreatorAddress: creator,
		Price:          "1000",
		MarketCap:      "2000",
		High24h:        "1000",
		Low24h:         "1000",
		CreatedAt:      100,
		UpdatedAt:      100,
	}); err != nil {
		t.Fatalf("seed initial: %v", err)
	}
}

func TestStorePoolStats(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	t.Run("GetPoolStats absent returns nil,nil", func(t *testing.T) {
		row, err := st.GetPoolStats(ctx, "0xabsent")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if row != nil {
			t.Fatalf("expected nil row, got %+v", row)
		}
	})

	t.Run("InsertInitialPoolStats is idempotent", func(t *testing.T) {
		addr := "0xpool_initial"
		inserted, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{
			PoolAddress: addr, TokenAddress: "0xtok", CreatorAddress: strptr("0xcreator"),
			Price: "500", MarketCap: "999", High24h: "500", Low24h: "500",
			CreatedAt: 10, UpdatedAt: 10,
		})
		if err != nil || !inserted {
			t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
		}
		// Second insert with different price must be a no-op (ON CONFLICT DO NOTHING).
		inserted2, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{
			PoolAddress: addr, TokenAddress: "0xtok", CreatorAddress: strptr("0xcreator"),
			Price: "777", MarketCap: "777", High24h: "777", Low24h: "777",
			CreatedAt: 20, UpdatedAt: 20,
		})
		if err != nil {
			t.Fatalf("second insert err: %v", err)
		}
		if inserted2 {
			t.Fatal("second insert should report inserted=false")
		}
		row, err := st.GetPoolStats(ctx, addr)
		if err != nil || row == nil {
			t.Fatalf("get: row=%v err=%v", row, err)
		}
		if row.Price != "500" || row.MarketCap != "999" {
			t.Fatalf("row mutated on conflict: price=%s mcap=%s", row.Price, row.MarketCap)
		}
		if row.CreatorAddress == nil || *row.CreatorAddress != "0xcreator" {
			t.Fatalf("creator = %v", row.CreatorAddress)
		}
		// Defaults applied for unspecified columns.
		if row.RiskRating != 50 {
			t.Fatalf("default risk_rating = %d, want 50", row.RiskRating)
		}
		if row.RiskFactors == nil || *row.RiskFactors != "[]" {
			t.Fatalf("default risk_factors = %v, want '[]'", row.RiskFactors)
		}
	})

	t.Run("GetPoolStatsBatch returns present, skips absent", func(t *testing.T) {
		seedInitial(t, ctx, st, "0xbatch1", nil)
		seedInitial(t, ctx, st, "0xbatch2", nil)
		rows, err := st.GetPoolStatsBatch(ctx, []string{"0xbatch1", "0xbatch2", "0xbatch_missing"})
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("batch len = %d, want 2", len(rows))
		}
		got := map[string]bool{}
		for _, r := range rows {
			got[r.PoolAddress] = true
		}
		if !got["0xbatch1"] || !got["0xbatch2"] {
			t.Fatalf("batch missing expected pools: %v", got)
		}
	})

	t.Run("GetPoolStatsBatch empty input returns nil", func(t *testing.T) {
		rows, err := st.GetPoolStatsBatch(ctx, nil)
		if err != nil {
			t.Fatalf("batch nil: %v", err)
		}
		if rows != nil {
			t.Fatalf("expected nil, got %v", rows)
		}
	})

	t.Run("UpdatePoolStatsPrice sets price/mcap/updated_at", func(t *testing.T) {
		addr := "0xprice"
		seedInitial(t, ctx, st, addr, nil)
		if err := st.UpdatePoolStatsPrice(ctx, addr, "1500", "3000", 200); err != nil {
			t.Fatalf("update price: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.Price != "1500" || row.MarketCap != "3000" || row.UpdatedAt != 200 {
			t.Fatalf("price=%s mcap=%s updated=%d", row.Price, row.MarketCap, row.UpdatedAt)
		}
	})

	t.Run("UpdatePoolStatsPrice on unknown pool is a silent no-op", func(t *testing.T) {
		if err := st.UpdatePoolStatsPrice(ctx, "0xnope", "1", "1", 1); err != nil {
			t.Fatalf("expected no error for unknown pool, got %v", err)
		}
	})
}

// TestStoreAdvisoryLockSerialization proves the per-bucket advisory lock
// serialises concurrent read-modify-write volume-bucket updates: 50 concurrent
// buys on the same bucket must yield buy_count == 50 with no lost updates.
func TestStoreAdvisoryLockSerialization(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xconc"
	const bucketStart int64 = 0
	const n = 50

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Distinct trader per call so unique-trader count is also n.
			trader := "0xtrader" + string(rune('a'+i%26)) + string(rune('a'+i/26))
			if err := st.ApplyVolumeBucket(ctx, addr, bucketStart, "100", "1000", trader, true); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ApplyVolumeBucket: %v", err)
	}

	var buyCount, sellCount int
	var volume string
	if err := pool.QueryRow(ctx, `
		SELECT buy_count, sell_count, volume_usdl FROM stats.volume_buckets
		WHERE pool_address = $1 AND bucket_start = $2`, addr, bucketStart).
		Scan(&buyCount, &sellCount, &volume); err != nil {
		t.Fatalf("read bucket: %v", err)
	}
	if buyCount != n {
		t.Fatalf("buy_count = %d, want %d (lost updates -> lock not serialising)", buyCount, n)
	}
	if sellCount != 0 {
		t.Fatalf("sell_count = %d, want 0", sellCount)
	}
	if volume != "5000" {
		t.Fatalf("volume_usdl = %s, want 5000 (50 * 100)", volume)
	}

	var traders int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT trader) FROM stats.bucket_traders
		WHERE pool_address = $1 AND bucket_start = $2`, addr, bucketStart).Scan(&traders); err != nil {
		t.Fatalf("count traders: %v", err)
	}
	if traders != n {
		t.Fatalf("distinct traders = %d, want %d", traders, n)
	}
}
