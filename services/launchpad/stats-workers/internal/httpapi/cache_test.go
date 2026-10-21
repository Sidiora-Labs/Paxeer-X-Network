package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

func TestPoolStatsRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterPoolStats(r, st, rdb)

	t.Run("cache miss reads DB and populates the cache", func(t *testing.T) {
		const pool = "0xps_miss"
		seedInitial(t, ctx, st, pool, nil)
		rec := serve(t, r, http.MethodGet, "/stats/"+pool, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got store.PoolStatsRow
		decode(t, rec, &got)
		if got.PoolAddress != pool || got.Price != "1000000" {
			t.Fatalf("row = %+v", got)
		}
		if n, _ := rdb.Exists(ctx, "stats:"+pool).Result(); n != 1 {
			t.Fatal("cache key not populated after miss")
		}
	})

	t.Run("cache hit is served without touching the DB", func(t *testing.T) {
		const pool = "0xps_hit"
		seedInitial(t, ctx, st, pool, nil) // DB price = 1000000
		// Seed a divergent cached row; a hit must return the cached price.
		sentinel := store.PoolStatsRow{PoolAddress: pool, TokenAddress: "0xt", Price: "777"}
		if err := rdb.Set(ctx, "stats:"+pool, string(mustJSON(t, sentinel)), time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/stats/"+pool, nil, nil)
		var got store.PoolStatsRow
		decode(t, rec, &got)
		if got.Price != "777" {
			t.Fatalf("price = %s, want 777 (served from cache)", got.Price)
		}
	})

	t.Run("absent pool is 404", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/stats/0xps_absent", nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("batch mixes cache hits and DB fallback", func(t *testing.T) {
		const cached = "0xbatch_cached"
		const fromDB = "0xbatch_db"
		seedInitial(t, ctx, st, fromDB, nil)
		sentinel := store.PoolStatsRow{PoolAddress: cached, TokenAddress: "0xt", Price: "555"}
		if err := rdb.Set(ctx, "stats:"+cached, string(mustJSON(t, sentinel)), time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/stats/batch?pools="+cached+","+fromDB+",0xbatch_missing", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got map[string]store.PoolStatsRow
		decode(t, rec, &got)
		if got[cached].Price != "555" {
			t.Errorf("cached entry price = %s, want 555", got[cached].Price)
		}
		if got[fromDB].Price != "1000000" {
			t.Errorf("db entry price = %s, want 1000000", got[fromDB].Price)
		}
		if _, ok := got["0xbatch_missing"]; ok {
			t.Errorf("missing pool should be absent from the batch result")
		}
		// The DB-fallback row is now cached too.
		if n, _ := rdb.Exists(ctx, "stats:"+fromDB).Result(); n != 1 {
			t.Errorf("db-fallback row not cached after batch")
		}
	})
}

func TestPressureRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterPressure(r, st, rdb)

	type pressureResp struct {
		PoolAddress string `json:"poolAddress"`
		H24         struct {
			BuyCount  int     `json:"buyCount"`
			SellCount int     `json:"sellCount"`
			BuyPct    float64 `json:"buyPct"`
			SellPct   float64 `json:"sellPct"`
			Direction string  `json:"direction"`
		} `json:"24h"`
		H1 struct {
			BuyCount  int `json:"buyCount"`
			SellCount int `json:"sellCount"`
		} `json:"1h"`
	}

	t.Run("computes percentages + direction and caches", func(t *testing.T) {
		const pool = "0xpressure"
		if _, err := st.Pool().Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, buy_count_24h, sell_count_24h, volume_24h, volume_1h, created_at, updated_at)
			VALUES ($1,'0xt',60,40,'1000','100',100,100)`, pool); err != nil {
			t.Fatalf("seed pool_stats: %v", err)
		}
		now := time.Now().Unix()
		insertTx(t, ctx, st, "p-buy", pool, "0xa", true, "100", "1", now)
		insertTx(t, ctx, st, "p-sell", pool, "0xb", false, "50", "1", now)

		rec := serve(t, r, http.MethodGet, "/stats/"+pool+"/pressure", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got pressureResp
		decode(t, rec, &got)
		if got.H24.BuyCount != 60 || got.H24.SellCount != 40 {
			t.Fatalf("24h counts = %+v", got.H24)
		}
		if got.H24.BuyPct != 60 || got.H24.SellPct != 40 || got.H24.Direction != "bullish" {
			t.Fatalf("24h pressure = %+v, want 60/40 bullish", got.H24)
		}
		if got.H1.BuyCount != 1 || got.H1.SellCount != 1 {
			t.Fatalf("1h counts = %+v, want 1/1", got.H1)
		}
		if n, _ := rdb.Exists(ctx, "pressure:"+pool).Result(); n != 1 {
			t.Fatal("pressure cache not populated")
		}
	})

	t.Run("cache hit served verbatim", func(t *testing.T) {
		const pool = "0xpressure_hit"
		if err := rdb.Set(ctx, "pressure:"+pool, `{"poolAddress":"`+pool+`","24h":{"buyCount":7,"sellCount":0,"buyPct":100,"sellPct":0,"direction":"bullish"},"1h":{"buyCount":0,"sellCount":0}}`, time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/stats/"+pool+"/pressure", nil, nil)
		var got pressureResp
		decode(t, rec, &got)
		if got.H24.BuyCount != 7 {
			t.Fatalf("expected cached buyCount 7, got %+v", got.H24)
		}
	})

	t.Run("absent pool is 404", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/stats/0xpressure_absent/pressure", nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestPlatformRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterPlatform(r, st, rdb)

	t.Run("cache hit served verbatim", func(t *testing.T) {
		if err := rdb.Set(ctx, "platform:metrics", `{"totalTokensLaunched":999,"updatedAt":1}`, time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/stats/platform", nil, nil)
		var got store.PlatformMetrics
		decode(t, rec, &got)
		if got.TotalTokensLaunched != 999 {
			t.Fatalf("totalTokensLaunched = %d, want 999 (cache hit)", got.TotalTokensLaunched)
		}
	})

	t.Run("cache miss computes from the DB", func(t *testing.T) {
		// Clear the seeded cache so this path recomputes.
		if err := rdb.Del(ctx, "platform:metrics").Err(); err != nil {
			t.Fatalf("clear cache: %v", err)
		}
		seedInitial(t, ctx, st, "0xplat1", nil)
		seedInitial(t, ctx, st, "0xplat2", nil)
		rec := serve(t, r, http.MethodGet, "/stats/platform", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got store.PlatformMetrics
		decode(t, rec, &got)
		if got.TotalTokensLaunched != 2 {
			t.Fatalf("totalTokensLaunched = %d, want 2", got.TotalTokensLaunched)
		}
		if got.UpdatedAt == 0 {
			t.Fatal("updatedAt should be set")
		}
		if n, _ := rdb.Exists(ctx, "platform:metrics").Result(); n != 1 {
			t.Fatal("platform cache not populated on miss")
		}
	})
}

func TestSearchRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	internaltest.EnsureMetadataSchema(t, st.Pool())
	r := chi.NewRouter()
	httpapi.RegisterSearch(r, st, rdb)

	seedToken := func(token, pool, name, symbol string) {
		t.Helper()
		if _, err := st.Pool().Exec(ctx, `
			INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, description, created_by, created_at)
			VALUES ($1,$2,$3,$4,'desc','0xcreator',1)`, token, pool, name, symbol); err != nil {
			t.Fatalf("seed metadata: %v", err)
		}
		if _, err := st.Pool().Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, volume_24h, created_at, updated_at)
			VALUES ($1,$2,'1000',1,1)`, pool, token); err != nil {
			t.Fatalf("seed pool_stats: %v", err)
		}
	}
	seedToken("0xtokdoge", "0xpooldoge", "Dogecoin", "DOGE")

	type searchResp struct {
		Results []store.SearchResult `json:"results"`
		Query   string               `json:"query"`
	}

	t.Run("query shorter than 2 chars returns empty", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/search?q=a", nil, nil)
		var got searchResp
		decode(t, rec, &got)
		if len(got.Results) != 0 {
			t.Fatalf("short query returned %d results", len(got.Results))
		}
	})

	t.Run("matches by name substring", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/search?q=doge", nil, nil)
		var got searchResp
		decode(t, rec, &got)
		if len(got.Results) != 1 || got.Results[0].PoolAddress != "0xpooldoge" {
			t.Fatalf("name search = %+v", got.Results)
		}
		// The query is now cached under search:doge:20.
		if n, _ := rdb.Exists(ctx, "search:doge:20").Result(); n != 1 {
			t.Fatal("search result not cached")
		}
	})

	t.Run("matches by address prefix", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/search?q=0xpooldoge", nil, nil)
		var got searchResp
		decode(t, rec, &got)
		if len(got.Results) != 1 || got.Results[0].TokenAddress != "0xtokdoge" {
			t.Fatalf("address search = %+v", got.Results)
		}
	})

	t.Run("cache hit served verbatim", func(t *testing.T) {
		if err := rdb.Set(ctx, "search:cached:20", `{"results":[],"query":"sentinel"}`, time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/search?q=cached", nil, nil)
		var got searchResp
		decode(t, rec, &got)
		if got.Query != "sentinel" {
			t.Fatalf("query = %s, want sentinel (cache hit)", got.Query)
		}
	})
}
