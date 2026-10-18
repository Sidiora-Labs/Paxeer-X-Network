package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

func insertBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, addr string, bucketStart int64, vol string, buy, sell int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats.volume_buckets (pool_address, bucket_start, volume_usdl, buy_count, sell_count, high_price, low_price)
		VALUES ($1,$2,$3,$4,$5,'0','0')`, addr, bucketStart, vol, buy, sell); err != nil {
		t.Fatalf("insert bucket: %v", err)
	}
}

func insertTrader(t *testing.T, ctx context.Context, pool *pgxpool.Pool, addr string, bucketStart int64, trader string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats.bucket_traders (pool_address, bucket_start, bucket_size, trader)
		VALUES ($1,$2,60,$3)`, addr, bucketStart, trader); err != nil {
		t.Fatalf("insert trader: %v", err)
	}
}

func TestApplyVolumeBucket(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xbucket"
	const bs int64 = 0

	// New bucket via a buy.
	if err := st.ApplyVolumeBucket(ctx, addr, bs, "100", "1000", "0xtA", true); err != nil {
		t.Fatalf("buy insert: %v", err)
	}
	var vol, high, low string
	var buy, sell int
	if err := pool.QueryRow(ctx, `
		SELECT volume_usdl, buy_count, sell_count, high_price, low_price
		FROM stats.volume_buckets WHERE pool_address=$1 AND bucket_start=$2`, addr, bs).
		Scan(&vol, &buy, &sell, &high, &low); err != nil {
		t.Fatalf("read: %v", err)
	}
	if vol != "100" || buy != 1 || sell != 0 || high != "1000" || low != "1000" {
		t.Fatalf("after buy insert: vol=%s buy=%d sell=%d high=%s low=%s", vol, buy, sell, high, low)
	}

	// Same bucket via a sell: volume accumulates, sell increments, buy unchanged.
	if err := st.ApplyVolumeBucket(ctx, addr, bs, "50", "1100", "0xtB", false); err != nil {
		t.Fatalf("sell update: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT volume_usdl, buy_count, sell_count FROM stats.volume_buckets
		WHERE pool_address=$1 AND bucket_start=$2`, addr, bs).Scan(&vol, &buy, &sell); err != nil {
		t.Fatalf("read2: %v", err)
	}
	if vol != "150" || buy != 1 || sell != 1 {
		t.Fatalf("after sell: vol=%s buy=%d sell=%d", vol, buy, sell)
	}

	// Trader dedup: repeating the same trader must not create a second row.
	if err := st.ApplyVolumeBucket(ctx, addr, bs, "10", "1000", "0xtA", true); err != nil {
		t.Fatalf("repeat trader: %v", err)
	}
	var tACount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM stats.bucket_traders
		WHERE pool_address=$1 AND bucket_start=$2 AND trader=$3`, addr, bs, "0xtA").Scan(&tACount); err != nil {
		t.Fatalf("count tA: %v", err)
	}
	if tACount != 1 {
		t.Fatalf("trader 0xtA rows = %d, want 1 (ON CONFLICT DO NOTHING)", tACount)
	}
}

func TestRecalculateRollingStats(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xrolling"
	const now int64 = 100000
	seedInitial(t, ctx, st, addr, nil)

	// A: within 5m/1h/24h. B: within 1h/24h. C: within 24h only. D: older than 24h.
	insertBucket(t, ctx, pool, addr, now-100, "10", 2, 1)   // A (>= now-300)
	insertBucket(t, ctx, pool, addr, now-1000, "20", 1, 0)  // B (>= now-3600, < now-300)
	insertBucket(t, ctx, pool, addr, now-5000, "30", 0, 3)  // C (>= now-86400, < now-3600)
	insertBucket(t, ctx, pool, addr, now-90000, "40", 5, 5) // D (< now-86400) excluded

	// Distinct traders within 24h: t1,t2 (A), t2 (B), t3 (C) => 3. t4 (D) excluded.
	insertTrader(t, ctx, pool, addr, now-100, "0xt1")
	insertTrader(t, ctx, pool, addr, now-100, "0xt2")
	insertTrader(t, ctx, pool, addr, now-1000, "0xt2")
	insertTrader(t, ctx, pool, addr, now-5000, "0xt3")
	insertTrader(t, ctx, pool, addr, now-90000, "0xt4")

	if err := st.RecalculateRollingStats(ctx, addr, now); err != nil {
		t.Fatalf("recalc: %v", err)
	}

	row, err := st.GetPoolStats(ctx, addr)
	if err != nil || row == nil {
		t.Fatalf("get: %v", err)
	}
	if row.Volume5m != "10" {
		t.Errorf("volume_5m = %s, want 10", row.Volume5m)
	}
	if row.Volume1h != "30" {
		t.Errorf("volume_1h = %s, want 30 (A+B)", row.Volume1h)
	}
	if row.Volume24h != "60" {
		t.Errorf("volume_24h = %s, want 60 (A+B+C)", row.Volume24h)
	}
	if row.BuyCount24h != 3 {
		t.Errorf("buy_count_24h = %d, want 3", row.BuyCount24h)
	}
	if row.SellCount24h != 4 {
		t.Errorf("sell_count_24h = %d, want 4", row.SellCount24h)
	}
	if row.UniqueTraders24h != 3 {
		t.Errorf("unique_traders_24h = %d, want 3", row.UniqueTraders24h)
	}
	if row.UpdatedAt != now {
		t.Errorf("updated_at = %d, want %d", row.UpdatedAt, now)
	}
}

func TestUpsertPriceSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const addr = "0xsnap"
	if err := st.UpsertPriceSnapshot(ctx, addr, 60, "1000"); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	// Upsert same minute -> price replaced.
	if err := st.UpsertPriceSnapshot(ctx, addr, 60, "2000"); err != nil {
		t.Fatalf("upsert snapshot: %v", err)
	}
	var price string
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM stats.price_snapshots WHERE pool_address=$1 AND minute_ts=60`, addr).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("snapshot rows = %d, want 1", count)
	}
	if err := pool.QueryRow(ctx, `SELECT price FROM stats.price_snapshots WHERE pool_address=$1 AND minute_ts=60`, addr).Scan(&price); err != nil {
		t.Fatalf("read price: %v", err)
	}
	if price != "2000" {
		t.Fatalf("price = %s, want 2000", price)
	}
}

func TestComputePriceChanges(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const now int64 = 100000

	t.Run("no snapshots -> empty map", func(t *testing.T) {
		out, err := st.ComputePriceChanges(ctx, "0xnone", "1100000", now)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
	})

	t.Run("positive change across all windows", func(t *testing.T) {
		addr := "0xpos"
		// Single snapshot at minute_ts=99840 (>= every window's threshold).
		if err := st.UpsertPriceSnapshot(ctx, addr, 99840, "1000000"); err != nil {
			t.Fatalf("snap: %v", err)
		}
		out, err := st.ComputePriceChanges(ctx, addr, "1100000", now)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		// delta=100000; pct=100000*10000/1000000=1000; dollar=100000*1e15/1e18=100.
		cols := []string{"price_change_1m", "price_change_5m", "price_change_15m", "price_change_1h", "price_change_24h"}
		for _, c := range cols {
			if out[c] != "1000" {
				t.Errorf("%s = %q, want 1000", c, out[c])
			}
		}
		dcols := []string{"price_change_dollar_1m", "price_change_dollar_5m", "price_change_dollar_15m", "price_change_dollar_1h", "price_change_dollar_24h"}
		for _, c := range dcols {
			if out[c] != "100" {
				t.Errorf("%s = %q, want 100", c, out[c])
			}
		}
		if len(out) != 10 {
			t.Errorf("len(out) = %d, want 10", len(out))
		}
	})

	t.Run("negative change truncates toward zero", func(t *testing.T) {
		addr := "0xneg"
		if err := st.UpsertPriceSnapshot(ctx, addr, 99840, "1000000"); err != nil {
			t.Fatalf("snap: %v", err)
		}
		out, err := st.ComputePriceChanges(ctx, addr, "900000", now)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		// delta=-100000; pct=-1000; dollar=-100.
		if out["price_change_1m"] != "-1000" {
			t.Errorf("pct = %q, want -1000", out["price_change_1m"])
		}
		if out["price_change_dollar_1m"] != "-100" {
			t.Errorf("dollar = %q, want -100", out["price_change_dollar_1m"])
		}
	})

	t.Run("zero old price is skipped", func(t *testing.T) {
		addr := "0xzero"
		if err := st.UpsertPriceSnapshot(ctx, addr, 99840, "0"); err != nil {
			t.Fatalf("snap: %v", err)
		}
		out, err := st.ComputePriceChanges(ctx, addr, "1100000", now)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty (old price 0 skipped), got %v", out)
		}
	})

	t.Run("invalid current price errors", func(t *testing.T) {
		if _, err := st.ComputePriceChanges(ctx, "0xbad", "not-a-number", now); err == nil {
			t.Fatal("expected error for invalid current price")
		}
	})
}

func TestUpdatePoolStatsPriceAndChanges(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	t.Run("updates price/mcap/high-low and dynamic change columns", func(t *testing.T) {
		addr := "0xupd"
		seedInitial(t, ctx, st, addr, nil) // price/high/low=1000
		changes := map[string]string{
			"price_change_1m":        "1000",
			"price_change_dollar_1m": "100",
		}
		if err := st.UpdatePoolStatsPriceAndChanges(ctx, addr, "1500", "3000", 200, changes); err != nil {
			t.Fatalf("update: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.Price != "1500" || row.MarketCap != "3000" || row.UpdatedAt != 200 {
			t.Fatalf("price=%s mcap=%s updated=%d", row.Price, row.MarketCap, row.UpdatedAt)
		}
		if row.High24h != "1500" {
			t.Errorf("high_24h = %s, want 1500 (max(1000,1500))", row.High24h)
		}
		if row.Low24h != "1000" {
			t.Errorf("low_24h = %s, want 1000 (min(1000,1500))", row.Low24h)
		}
		if row.PriceChange1m != "1000" || row.PriceChangeDollar1m != "100" {
			t.Errorf("change cols not applied: pct=%s dollar=%s", row.PriceChange1m, row.PriceChangeDollar1m)
		}
		// Untouched change columns keep their default.
		if row.PriceChange24h != "0" {
			t.Errorf("price_change_24h = %s, want default 0", row.PriceChange24h)
		}
	})

	t.Run("high/low seeded at 0 adopt the first price", func(t *testing.T) {
		addr := "0xzerohl"
		if _, err := pool.Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, price, high_24h, low_24h, created_at, updated_at)
			VALUES ($1,'0xtok','0','0','0',10,10)`, addr); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := st.UpdatePoolStatsPriceAndChanges(ctx, addr, "500", "600", 20, nil); err != nil {
			t.Fatalf("update: %v", err)
		}
		row, _ := st.GetPoolStats(ctx, addr)
		if row.High24h != "500" || row.Low24h != "500" {
			t.Fatalf("high/low from 0 seed: high=%s low=%s, want 500/500", row.High24h, row.Low24h)
		}
	})

	t.Run("absent pool is a silent no-op", func(t *testing.T) {
		if err := st.UpdatePoolStatsPriceAndChanges(ctx, "0xmissing", "1", "1", 1, nil); err != nil {
			t.Fatalf("expected no-op, got %v", err)
		}
	})
}
