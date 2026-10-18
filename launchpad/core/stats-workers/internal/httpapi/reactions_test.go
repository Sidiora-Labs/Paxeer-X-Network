package httpapi_test

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	redis "github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
)

type reactionsResp struct {
	PoolAddress string `json:"poolAddress"`
	Reactions   struct {
		Bullish int `json:"bullish"`
		Hot     int `json:"hot"`
		Diamond int `json:"diamond"`
		Bearish int `json:"bearish"`
		Trash   int `json:"trash"`
		Warning int `json:"warning"`
	} `json:"reactions"`
	Total    int     `json:"total"`
	UserVote *string `json:"userVote"`
}

func TestReactionsRoutes(t *testing.T) {
	ctx := context.Background()
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterReactions(r, rdb)

	// postReaction signs message with key, claims wallet, and POSTs the vote.
	postReaction := func(t *testing.T, pool, reaction, wallet, sig, msg string) *httptest.ResponseRecorder {
		t.Helper()
		body := mustJSON(t, map[string]string{
			"reaction": reaction, "walletAddress": wallet, "signature": sig, "message": msg,
		})
		return serve(t, r, http.MethodPost, "/stats/"+pool+"/reactions", body, map[string]string{"Content-Type": "application/json"})
	}
	clearCooldown := func(wallet string) {
		_ = rdb.Del(ctx, "reactions:cooldown:"+strings.ToLower(wallet)).Err()
	}

	t.Run("GET cache miss computes and caches; cache hit serves verbatim", func(t *testing.T) {
		const pool = "0xreact_cache"
		rec := serve(t, r, http.MethodGet, "/stats/"+pool+"/reactions", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var got reactionsResp
		decode(t, rec, &got)
		if got.Total != 0 || got.Reactions.Bullish != 0 {
			t.Fatalf("fresh pool should have zero reactions: %+v", got)
		}
		// The aggregate is now cached under reactions:<pool>.
		if n, _ := rdb.Exists(ctx, "reactions:"+pool).Result(); n != 1 {
			t.Fatalf("reactions cache key not set after GET")
		}
		// Overwrite the cache with a sentinel; a cache HIT must return it verbatim.
		if err := rdb.Set(ctx, "reactions:"+pool, `{"poolAddress":"`+pool+`","reactions":{"bullish":7,"hot":0,"diamond":0,"bearish":0,"trash":0,"warning":0},"total":7}`, 0).Err(); err != nil {
			t.Fatalf("seed sentinel: %v", err)
		}
		rec2 := serve(t, r, http.MethodGet, "/stats/"+pool+"/reactions", nil, nil)
		var hit reactionsResp
		decode(t, rec2, &hit)
		if hit.Total != 7 || hit.Reactions.Bullish != 7 {
			t.Fatalf("cache hit not served verbatim: %+v", hit)
		}
	})

	t.Run("valid signature adds a vote; switch and toggle update the set", func(t *testing.T) {
		const pool = "0xreact_vote"
		key, wallet := newWallet(t)
		msg := "vote on " + pool
		sig := signWallet(t, key, msg)

		// First vote: bullish.
		rec := postReaction(t, pool, "bullish", wallet, sig, msg)
		var got reactionsResp
		decode(t, rec, &got)
		if rec.Code != http.StatusOK || got.Reactions.Bullish != 1 || got.Total != 1 {
			t.Fatalf("first vote: code=%d resp=%+v", rec.Code, got)
		}
		if got.UserVote == nil || *got.UserVote != "bullish" {
			t.Fatalf("userVote = %v, want bullish", got.UserVote)
		}

		// Switch to hot (clear cooldown first): bullish removed, hot added.
		clearCooldown(wallet)
		rec = postReaction(t, pool, "hot", wallet, sig, msg)
		decode(t, rec, &got)
		if got.Reactions.Bullish != 0 || got.Reactions.Hot != 1 || got.Total != 1 {
			t.Fatalf("switch to hot: resp=%+v", got)
		}
		if got.UserVote == nil || *got.UserVote != "hot" {
			t.Fatalf("userVote = %v, want hot", got.UserVote)
		}

		// Toggle hot off: hot removed, no vote.
		clearCooldown(wallet)
		rec = postReaction(t, pool, "hot", wallet, sig, msg)
		decode(t, rec, &got)
		if got.Reactions.Hot != 0 || got.Total != 0 {
			t.Fatalf("toggle off: resp=%+v", got)
		}
		if got.UserVote != nil {
			t.Fatalf("userVote = %v, want nil after toggle off", got.UserVote)
		}
	})

	t.Run("a second vote within the cooldown window is rate-limited 429", func(t *testing.T) {
		const pool = "0xreact_cooldown"
		key, wallet := newWallet(t)
		msg := "cooldown " + pool
		sig := signWallet(t, key, msg)

		if rec := postReaction(t, pool, "bullish", wallet, sig, msg); rec.Code != http.StatusOK {
			t.Fatalf("first vote code = %d, want 200", rec.Code)
		}
		// No cooldown clear: the immediate second vote is rejected.
		if rec := postReaction(t, pool, "bearish", wallet, sig, msg); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second vote code = %d, want 429", rec.Code)
		}
	})

	t.Run("invalid signature is forbidden 403", func(t *testing.T) {
		const pool = "0xreact_badsig"
		key, _ := newWallet(t)
		_, otherWallet := newWallet(t)
		msg := "spoof"
		sig := signWallet(t, key, msg) // signed by key, but claims otherWallet
		if rec := postReaction(t, pool, "bullish", otherWallet, sig, msg); rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d, want 403", rec.Code)
		}
	})

	t.Run("invalid reaction name is 400", func(t *testing.T) {
		const pool = "0xreact_badname"
		key, wallet := newWallet(t)
		msg := "bad reaction"
		sig := signWallet(t, key, msg)
		if rec := postReaction(t, pool, "moon", wallet, sig, msg); rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", rec.Code)
		}
	})

	t.Run("missing required fields is 400", func(t *testing.T) {
		const pool = "0xreact_missing"
		body := mustJSON(t, map[string]string{"reaction": "bullish"}) // no wallet/sig/msg
		rec := serve(t, r, http.MethodPost, "/stats/"+pool+"/reactions", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", rec.Code)
		}
	})
}

func TestPhase3DurableRoutesRejectHostileBodiesAndMissingAuthority(t *testing.T) {
	router := chi.NewRouter()
	policies := auth.PolicyRegistry{Version: "phase3", Actions: map[string]auth.Policy{"reaction.replace": {Scope: "wallet", OwnerRequired: true, RouteTemplate: "/v2/stats/{poolAddress}/reactions"}}}
	if err := httpapi.RegisterDurableReactions(router, httpapi.DurableReactionDeps{Policies: policies}); err != nil {
		t.Fatal(err)
	}
	httpapi.RegisterDurableAnalytics(router, analytics.ReactionAuthority{})
	pool := "0x" + strings.Repeat("1", 40)
	valid := `{"chainId":"125","pool":"` + pool + `","wallet":"0x` + strings.Repeat("2", 40) + `","reaction":"hot","nonce":"attack","expiry":1,"expectedRevision":0}`
	for _, c := range []struct {
		body   string
		status int
	}{
		{strings.Repeat("x", 65537), 413},
		{`{"reaction":"hot","reaction":"diamond"}`, 400},
		{valid + ` {}`, 400},
		{strings.Replace(valid, `"nonce":"attack"`, `"nonce":"attack","unknown":true`, 1), 400},
		{strings.Replace(valid, `"expectedRevision":0`, `"expectedRevision":-1`, 1), 422},
		{strings.Replace(valid, `"reaction":"hot"`, `"reaction":"unknown"`, 1), 422},
		{strings.Replace(valid, `"chainId":"125"`, `"chainId":"1"`, 1), 422},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/v2/stats/"+pool+"/reactions", strings.NewReader(c.body)))
		if response.Code != c.status {
			t.Fatal(c.status, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"pressure", "risk", "reactions", "analytics/revisions"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/stats/"+pool+"/"+path, nil))
		if response.Code != 503 {
			t.Fatal("missing durable dependency fabricated success", path, response.Code, response.Body.String())
		}
	}
}
func TestPhase3RealRedisConcurrentAdmissionAndDependencyFailure(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Fatal("real redis-server required", err)
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "redis.sock")
	child := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--dir", dir, "--save", "", "--appendonly", "no")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			child.Process.Kill()
			child.Wait()
		}
	}()
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket, MaxRetries: -1, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err = client.Ping(ctx).Err(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	admission := &httpapi.ReactionAdmission{Redis: client, Namespace: "phase3", WalletPerMinute: 12, IPPerMinute: 12}
	results := make(chan error, 32)
	wallet := "0x" + strings.Repeat("3", 40)
	for n := 0; n < 32; n++ {
		go func() { results <- admission.Admit(ctx, wallet, "127.0.0.1") }()
	}
	accepted, rejected := 0, 0
	for n := 0; n < 32; n++ {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, analytics.ErrCooldown) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 12 || rejected != 20 {
		t.Fatal("admission was not atomic", accepted, rejected)
	}
	for _, key := range []string{"phase3:{reaction-admission}:wallet:" + wallet, "phase3:{reaction-admission}:ip:127.0.0.1"} {
		count, err := client.Get(ctx, key).Int()
		if err != nil || count != 12 {
			t.Fatal(key, count, err)
		}
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > time.Minute {
			t.Fatal(key, ttl, err)
		}
	}
	if err = admission.Admit(ctx, "bad-wallet", "127.0.0.1"); !errors.Is(err, analytics.ErrInvalidInput) {
		t.Fatal(err)
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	stopped = true
	if err = admission.Admit(ctx, wallet, "127.0.0.1"); !errors.Is(err, analytics.ErrDependencyUnavailable) {
		t.Fatal("Redis outage did not fail closed", err)
	}
	var absent *httpapi.ReactionAdmission
	if err = absent.Admit(ctx, wallet, "127.0.0.1"); !errors.Is(err, analytics.ErrDependencyUnavailable) {
		t.Fatal(err)
	}
}
