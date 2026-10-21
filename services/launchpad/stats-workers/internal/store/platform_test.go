package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

func insertPoolStatsFull(t *testing.T, ctx context.Context, pool *pgxpool.Pool, addr, vol24, vol1h, mcap string, buy24, sell24 int, createdAt int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats.pool_stats
			(pool_address, token_address, volume_24h, volume_1h, market_cap, buy_count_24h, sell_count_24h, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
		addr, addr+"-tok", vol24, vol1h, mcap, buy24, sell24, createdAt); err != nil {
		t.Fatalf("seed pool_stats: %v", err)
	}
}

func insertTxFull(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, addr string, isBuy bool, amountIn, fee string, ts int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats.pool_transactions
			(id, pool_address, sender, is_buy, amount_in, amount_out, price, fee, block_timestamp, tx_hash)
		VALUES ($1::text,$2,'0xs',$3,$4,'0','1000',$5,$6,$1::varchar(66))`,
		id, addr, isBuy, amountIn, fee, ts); err != nil {
		t.Fatalf("seed tx: %v", err)
	}
}

func TestPlatformMetrics(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const now int64 = 1_000_000 // cutoff24h=913600, cutoff1h=996400

	insertPoolStatsFull(t, ctx, pool, "0xp1", "100", "40", "1000", 3, 2, now-100)    // new within 24h
	insertPoolStatsFull(t, ctx, pool, "0xp2", "200", "60", "2000", 1, 4, now-200000) // old

	// Transactions: tx1 within 1h+24h, tx2 within 24h only, tx3 excluded.
	insertTxFull(t, ctx, pool, "f1", "0xp1", true, "1", "5", now-100)
	insertTxFull(t, ctx, pool, "f2", "0xp1", false, "1", "3", now-5000)
	insertTxFull(t, ctx, pool, "f3", "0xp1", true, "1", "99", now-90000)

	// bucket_traders: 2 distinct within 24h, 1 old excluded.
	insertTrader(t, ctx, pool, "0xp1", now-100, "0xta")
	insertTrader(t, ctx, pool, "0xp1", now-100, "0xtb")
	insertTrader(t, ctx, pool, "0xp1", now-90000, "0xtc")

	// cross_token_swaps: 1 within 24h, 1 old.
	_ = st.InsertCrossTokenSwap(ctx, crossSwap("c1", "0xw", "0xa", "0xb", now-100))
	_ = st.InsertCrossTokenSwap(ctx, crossSwap("c2", "0xw", "0xa", "0xb", now-90000))

	m, err := st.PlatformMetrics(ctx, now)
	if err != nil {
		t.Fatalf("platform metrics: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"totalVolume24h", m.TotalVolume24h, "300"},
		{"totalVolume1h", m.TotalVolume1h, "100"},
		{"totalMarketCap", m.TotalMarketCap, "3000"},
		{"totalFees24h", m.TotalFees24h, "8"},
		{"totalTransactions24h", m.TotalTransactions24h, 2},
		{"totalTransactions1h", m.TotalTransactions1h, 1},
		{"totalBuys24h", m.TotalBuys24h, 4},
		{"totalSells24h", m.TotalSells24h, 6},
		{"uniqueTraders24h", m.UniqueTraders24h, 2},
		{"totalTokensLaunched", m.TotalTokensLaunched, 2},
		{"newTokens24h", m.NewTokens24h, 1},
		{"crossTokenSwaps24h", m.CrossTokenSwaps24h, 1},
		{"updatedAt", m.UpdatedAt, now},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestPruneBucketTraders(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	insertTrader(t, ctx, pool, "0xp", 100, "0xold1")
	insertTrader(t, ctx, pool, "0xp", 200, "0xold2")
	insertTrader(t, ctx, pool, "0xp", 1000, "0xnew1")
	insertTrader(t, ctx, pool, "0xp", 2000, "0xnew2")

	removed, err := st.PruneBucketTraders(ctx, 500)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	var remaining int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM stats.bucket_traders`).Scan(&remaining)
	if remaining != 2 {
		t.Fatalf("remaining = %d, want 2", remaining)
	}
}

func TestPressureStats(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	const now int64 = 1_000_000
	const addr = "0xpressure"
	insertPoolStatsFull(t, ctx, pool, addr, "500", "120", "1000", 6, 4, now-100)

	// 1h window (>= now-3600): 2 buys (30,20), 1 sell (10). Older excluded.
	insertTxFull(t, ctx, pool, "p1", addr, true, "30", "1", now-100)
	insertTxFull(t, ctx, pool, "p2", addr, true, "20", "1", now-200)
	insertTxFull(t, ctx, pool, "p3", addr, false, "10", "1", now-300)
	insertTxFull(t, ctx, pool, "p4", addr, true, "999", "1", now-90000)

	t.Run("found with 24h + 1h breakdown", func(t *testing.T) {
		ps, found, err := st.PressureStats(ctx, addr, now-3600)
		if err != nil || !found {
			t.Fatalf("pressure: found=%v err=%v", found, err)
		}
		if ps.BuyCount24h != 6 || ps.SellCount24h != 4 {
			t.Errorf("24h counts: buy=%d sell=%d, want 6/4", ps.BuyCount24h, ps.SellCount24h)
		}
		if ps.Volume24h != "500" || ps.Volume1h != "120" {
			t.Errorf("volumes: 24h=%s 1h=%s, want 500/120", ps.Volume24h, ps.Volume1h)
		}
		if ps.BuyVolume1h != "50" || ps.SellVolume1h != "10" {
			t.Errorf("1h vols: buy=%s sell=%s, want 50/10", ps.BuyVolume1h, ps.SellVolume1h)
		}
		if ps.BuyCount1h != 2 || ps.SellCount1h != 1 {
			t.Errorf("1h counts: buy=%d sell=%d, want 2/1", ps.BuyCount1h, ps.SellCount1h)
		}
	})

	t.Run("unknown pool -> found=false", func(t *testing.T) {
		_, found, err := st.PressureStats(ctx, "0xunknown", now-3600)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if found {
			t.Fatal("expected found=false for unknown pool")
		}
	})
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	internaltest.EnsureMetadataSchema(t, pool)
	st := store.New(pool)

	seedToken := func(token, p, name, symbol, vol string) {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, description, created_by, created_at)
			VALUES ($1,$2,$3,$4,'desc','0xc',100)`, token, p, name, symbol); err != nil {
			t.Fatalf("seed token: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, volume_24h, created_at, updated_at)
			VALUES ($1,$2,$3,100,100)`, p, token, vol); err != nil {
			t.Fatalf("seed pool_stats: %v", err)
		}
	}
	seedToken("0xtoken1", "0xpool1", "DogeCoin", "DOGE", "500")
	seedToken("0xtoken2", "0xpool2", "CatCoin", "CAT", "900")

	t.Run("by name", func(t *testing.T) {
		res, err := st.Search(ctx, false, "%doge%", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res) != 1 || res[0].TokenAddress != "0xtoken1" {
			t.Fatalf("by name = %+v", res)
		}
		if res[0].Name == nil || *res[0].Name != "DogeCoin" || res[0].Volume24h != "500" {
			t.Fatalf("fields = %+v", res[0])
		}
	})

	t.Run("ordered by volume_24h desc", func(t *testing.T) {
		res, err := st.Search(ctx, false, "%coin%", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res) != 2 || res[0].TokenAddress != "0xtoken2" || res[1].TokenAddress != "0xtoken1" {
			t.Fatalf("order = %+v (want CatCoin/900 first)", res)
		}
	})

	t.Run("by address", func(t *testing.T) {
		res, err := st.Search(ctx, true, "%0xtoken1%", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res) != 1 || res[0].TokenAddress != "0xtoken1" {
			t.Fatalf("by address = %+v", res)
		}
	})

	t.Run("limit respected", func(t *testing.T) {
		res, err := st.Search(ctx, false, "%coin%", 1)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("limit = %d, want 1", len(res))
		}
	})

	t.Run("no match -> empty non-nil", func(t *testing.T) {
		res, err := st.Search(ctx, false, "%zzz%", 20)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Fatalf("want empty non-nil, got %+v", res)
		}
	})
}
