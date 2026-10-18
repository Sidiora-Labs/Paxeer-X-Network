package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
)

func newServer(t *testing.T) (*httptest.Server, *goredis.Client) {
	t.Helper()
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterRankings(r, rdb)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, rdb
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx,bodyclose // test client; body closed below
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode, body
}

func TestRankingInvalidCategory(t *testing.T) {
	srv, _ := newServer(t)
	code, body := getJSON(t, srv.URL+"/rankings/bogus")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	if body["error"] != "Invalid category" {
		t.Errorf("error = %v, want Invalid category", body["error"])
	}
}

func TestRankingByCategoryWithEnrichment(t *testing.T) {
	srv, rdb := newServer(t)
	ctx := context.Background()

	if err := rdb.ZAdd(ctx, "ranking:trending",
		goredis.Z{Score: 9, Member: "0xa"},
		goredis.Z{Score: 5, Member: "0xb"},
	).Err(); err != nil {
		t.Fatalf("seed zset: %v", err)
	}
	// Cached stats for 0xa only; 0xb has none.
	if err := rdb.Set(ctx, "stats:0xa",
		`{"price":"1.5","volume24h":"100","holderCount":42,"extra":"drop"}`, 0).Err(); err != nil {
		t.Fatalf("seed stats: %v", err)
	}

	code, body := getJSON(t, srv.URL+"/rankings/trending")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["category"] != "trending" {
		t.Errorf("category = %v", body["category"])
	}
	if body["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", body["total"])
	}

	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %v", body["items"])
	}
	first := items[0].(map[string]any)
	if first["poolAddress"] != "0xa" || first["rank"].(float64) != 1 || first["score"].(float64) != 9 {
		t.Errorf("first item = %v", first)
	}
	stats, ok := first["stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected enriched stats on 0xa, got %v", first)
	}
	if stats["price"] != "1.5" || stats["holderCount"].(float64) != 42 {
		t.Errorf("stats subset = %v", stats)
	}
	if _, dropped := stats["extra"]; dropped {
		t.Errorf("non-subset key 'extra' must be dropped, got %v", stats)
	}

	second := items[1].(map[string]any)
	if _, has := second["stats"]; has {
		t.Errorf("0xb has no cached stats; must not be enriched, got %v", second)
	}
}

func TestRankingPagination(t *testing.T) {
	srv, rdb := newServer(t)
	ctx := context.Background()
	members := []goredis.Z{
		{Score: 5, Member: "0xa"},
		{Score: 4, Member: "0xb"},
		{Score: 3, Member: "0xc"},
	}
	if err := rdb.ZAdd(ctx, "ranking:movers", members...).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	code, body := getJSON(t, srv.URL+"/rankings/movers?limit=1&offset=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(map[string]any)
	if it["poolAddress"] != "0xb" || it["rank"].(float64) != 2 {
		t.Errorf("paginated item = %v, want 0xb rank 2", it)
	}
	if body["limit"].(float64) != 1 || body["offset"].(float64) != 1 {
		t.Errorf("limit/offset echo = %v/%v", body["limit"], body["offset"])
	}
}

func TestPoolAppearances(t *testing.T) {
	srv, rdb := newServer(t)
	ctx := context.Background()
	// 0xp is rank 1 in trending, absent elsewhere.
	if err := rdb.ZAdd(ctx, "ranking:trending", goredis.Z{Score: 7, Member: "0xp"}).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	code, body := getJSON(t, srv.URL+"/rankings/pool/0xp")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body["poolAddress"] != "0xp" {
		t.Errorf("poolAddress = %v", body["poolAddress"])
	}
	rankings := body["rankings"].(map[string]any)
	tr, ok := rankings["trending"].(map[string]any)
	if !ok {
		t.Fatalf("expected trending appearance, got %v", rankings["trending"])
	}
	if tr["rank"].(float64) != 1 || tr["score"].(float64) != 7 {
		t.Errorf("trending appearance = %v", tr)
	}
	if rankings["movers"] != nil {
		t.Errorf("movers should be null, got %v", rankings["movers"])
	}
}
