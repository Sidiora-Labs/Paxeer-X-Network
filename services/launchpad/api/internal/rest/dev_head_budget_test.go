package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
)

// headRPC is a network hop in front of the simulated chain's JSON-RPC that
// adds the latency (or outage) of the public chain 125 RPC to
// eth_getBlockByNumber, the call that resolves the indexer head hash.
type headRPC struct {
	delay atomic.Int64 // nanoseconds
	down  atomic.Bool
	srv   *httptest.Server
}

func newHeadRPC(t *testing.T, upstream string) *headRPC {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	h := &headRPC{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte("eth_getBlockByNumber")) {
			if h.down.Load() {
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
				return
			}
			time.Sleep(time.Duration(h.delay.Load()))
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func newHeadBudgetFixture(t *testing.T) (mktMarketFixture, *headRPC) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	rdb := internaltest.NewRedis(t)
	mktApplyIndexerMigration(t, pool, "cursors", "swaps")
	chain := newMktChain(t)
	rpc := newHeadRPC(t, chain.url)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterDevelopmentMarketReads(context.Background(), router, rest.DevelopmentMarketOptions{Store: store.New(pool, rdb), Admission: admission, ChainRPCURL: rpc.srv.URL}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return mktMarketFixture{pool: pool, rdb: rdb, chain: chain, srv: srv}, rpc
}

func leaderboardWatermarkHeight(t *testing.T, envelope map[string]any) string {
	t.Helper()
	snapshot := mktData(envelope)["snapshot"].(map[string]any)
	if entries, ok := snapshot["entries"].([]any); !ok || len(entries) != 0 {
		t.Fatalf("entries: %v", snapshot["entries"])
	}
	return snapshot["watermark"].(map[string]any)["height"].(string)
}

func assertRetryable(t *testing.T, got v2Response, retryable bool) {
	t.Helper()
	var body struct {
		Retryable    bool `json:"retryable"`
		RetryAfterMS *int `json:"retryAfterMs"`
	}
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatal(err)
	}
	if retryable {
		if !body.Retryable || body.RetryAfterMS == nil || *body.RetryAfterMS != 1000 || got.header.Get("Retry-After") != "1" {
			t.Fatalf("want retryable error, got %s Retry-After=%q", got.body, got.header.Get("Retry-After"))
		}
		return
	}
	if body.Retryable || body.RetryAfterMS != nil || got.header.Get("Retry-After") != "" {
		t.Fatalf("want non-retryable error, got %s Retry-After=%q", got.body, got.header.Get("Retry-After"))
	}
}

// Regression (live 2026-10-13): every indexer head advance made the next
// leaderboard read resolve the new head hash over the chain RPC inside the
// 400ms read budget, and the leaderboard query that followed ran on an
// expired context: 503 DEPENDENCY_UNAVAILABLE, then 200 once the hash was
// cached. With the store reachable the read must answer 200 on every
// request, whatever the RPC latency.
func TestDevelopmentLeaderboardSurvivesSlowHeadResolution(t *testing.T) {
	f, rpc := newHeadBudgetFixture(t)
	path := f.srv.URL + "/v2/leaderboards/trader?window=24h&limit=20"
	first := f.chain.head(t)
	f.setCursor(t, first.Number.Uint64())
	mktAssertWatermark(t, mktData(assertV2Success(t, getV2(t, path), "LeaderboardsResponse"))["snapshot"].(map[string]any)["watermark"], first)

	// The RPC now answers slower than the whole read budget, and the
	// indexer head advances before every request.
	rpc.delay.Store(int64(600 * time.Millisecond))
	var latest uint64
	for i := 0; i < 20; i++ {
		f.chain.backend.Commit()
		latest = f.chain.head(t).Number.Uint64()
		f.setCursor(t, latest)
		start := time.Now()
		got := getV2(t, path)
		if got.status != 200 {
			t.Fatalf("request %d after head advance to %d: %d %s", i, latest, got.status, got.body)
		}
		height, err := strconv.ParseUint(leaderboardWatermarkHeight(t, assertV2Success(t, got, "LeaderboardsResponse")), 10, 64)
		if err != nil || height < first.Number.Uint64() || height > latest {
			t.Fatalf("request %d watermark height %d outside [%s, %d]", i, height, first.Number, latest)
		}
		if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
			t.Fatalf("request %d took %v", i, elapsed)
		}
	}

	// Once the RPC answers within budget, the watermark catches up with the
	// indexer head.
	rpc.delay.Store(0)
	want := f.chain.head(t)
	deadline := time.Now().Add(3 * time.Second)
	for {
		envelope := assertV2Success(t, getV2(t, path), "LeaderboardsResponse")
		if leaderboardWatermarkHeight(t, envelope) == want.Number.String() {
			mktAssertWatermark(t, mktData(envelope)["snapshot"].(map[string]any)["watermark"], want)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watermark never reached head %s", want.Number)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A chain RPC that is genuinely down is a transient dependency failure:
	// 503, retryable, Retry-After.
	rpc.down.Store(true)
	f.chain.backend.Commit()
	f.setCursor(t, f.chain.head(t).Number.Uint64())
	down := getV2(t, path)
	assertV2Error(t, down, 503, "DEPENDENCY_UNAVAILABLE")
	assertRetryable(t, down, true)
	rpc.down.Store(false)
	deadline = time.Now().Add(3 * time.Second)
	for getV2(t, path).status != 200 {
		if time.Now().After(deadline) {
			t.Fatal("leaderboard did not recover with the RPC")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Rankings share the head watermark. They read it last, so a slow head hash
// did not fail them, but it held each response past the 400ms read budget.
func TestDevelopmentRankingsSurviveSlowHeadResolution(t *testing.T) {
	f, rpc := newHeadBudgetFixture(t)
	f.setCursor(t, f.chain.head(t).Number.Uint64())
	assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/trending"), "RankingsResponse")
	rpc.delay.Store(int64(600 * time.Millisecond))
	for i := 0; i < 5; i++ {
		f.chain.backend.Commit()
		f.setCursor(t, f.chain.head(t).Number.Uint64())
		start := time.Now()
		assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/trending"), "RankingsResponse")
		if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
			t.Fatalf("rankings request %d took %v", i, elapsed)
		}
	}
}

// With no watermark served yet, a head resolution that misses the budget is
// a transient 503; the detached resolution fills the cache for the retry.
func TestDevelopmentLeaderboardColdSlowHeadIsTransient(t *testing.T) {
	f, rpc := newHeadBudgetFixture(t)
	head := f.chain.head(t)
	f.setCursor(t, head.Number.Uint64())
	rpc.delay.Store(int64(600 * time.Millisecond))
	path := f.srv.URL + "/v2/leaderboards/creator?window=7d"
	cold := getV2(t, path)
	assertV2Error(t, cold, 503, "DEPENDENCY_UNAVAILABLE")
	assertRetryable(t, cold, true)
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := getV2(t, path)
		if got.status == 200 {
			mktAssertWatermark(t, mktData(assertV2Success(t, got, "LeaderboardsResponse"))["snapshot"].(map[string]any)["watermark"], head)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cold leaderboard never recovered: %d %s", got.status, got.body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Routes whose dependency this deployment does not configure answer the v2
// dependency error as non-retryable, without Retry-After.
func TestV2NotConfiguredIsNotRetryable(t *testing.T) {
	router := chi.NewRouter()
	if err := rest.RegisterDevelopmentMarketReads(context.Background(), router, rest.DevelopmentMarketOptions{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	for _, path := range []string{"/v2/leaderboards/trader?window=24h", "/v2/platform/metrics", "/v2/pools?sort=new", "/v2/holder-rewards/stats"} {
		got := getV2(t, srv.URL+path)
		assertV2Error(t, got, 503, "DEPENDENCY_UNAVAILABLE")
		assertRetryable(t, got, false)
	}

	recorder := httptest.NewRecorder()
	rest.WriteV2APIError(recorder, httptest.NewRequest("GET", "/v2/pools", nil), 503, "DEPENDENCY_UNAVAILABLE")
	assertRetryable(t, v2Response{status: recorder.Code, header: recorder.Header(), body: recorder.Body.Bytes()}, true)
	recorder = httptest.NewRecorder()
	rest.WriteV2APIError(recorder, httptest.NewRequest("GET", "/v2/pools", nil), 429, "RATE_LIMITED")
	assertRetryable(t, v2Response{status: recorder.Code, header: recorder.Header(), body: recorder.Body.Bytes()}, true)
	recorder = httptest.NewRecorder()
	recorder.Header().Set("Retry-After", "5")
	rest.WriteV2NotConfiguredError(recorder, httptest.NewRequest("GET", "/v2/pools", nil))
	got := v2Response{status: recorder.Code, header: recorder.Header(), body: recorder.Body.Bytes()}
	assertV2Error(t, got, 503, "DEPENDENCY_UNAVAILABLE")
	assertRetryable(t, got, false)
	if !strings.Contains(string(got.body), `"retryAfterMs":null`) {
		t.Fatalf("retryAfterMs must be null: %s", got.body)
	}
}
