package search

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestPublicCacheBindingAndAuthentication(t *testing.T) {
	pool := integratedDatabase(t)
	service, err := NewService(Store{Pool: pool}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	r := IntegratedRequest{Request: Request{Query: "coin", Kind: "pool", Limit: 25}, Sort: "address"}
	q, _ := r.Validate()
	key := PublicCacheKey(q, r, "g1", 1, 1)
	if strings.Contains(key, "coin") || key == PublicCacheKey(q, r, "g2", 1, 1) || key == PublicCacheKey(q, r, "g1", 2, 1) {
		t.Fatal("cache binding or privacy")
	}
	wire, err := service.encodeCached(key, IntegratedPage{Items: []IntegratedHit{}, Generation: "g1", Revision: 1, PrivacyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.decodeCached(key, wire); !ok {
		t.Fatal("authentic cache rejected")
	}
	wire[len(wire)-2] ^= 1
	if _, ok := service.decodeCached(key, wire); ok {
		t.Fatal("modified Redis payload became authority")
	}
	for i := 0; i < 120; i++ {
		if err = service.Admit("127.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Admit("127.0.0.1"); err != ErrBusy {
		t.Fatal("IP admission unbounded", err)
	}
}
func TestSearchPrivateOverlayUsesVerifiedAccountBinding(t *testing.T) {
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	a := PreferenceSnapshot{AccountID: uuid.NewString(), Wallet: fixtureAddress(701), Revision: 1, MutedCreators: []string{d.Creator}, FollowedCreators: []string{}, SavedPools: []string{}}
	b := PreferenceSnapshot{AccountID: uuid.NewString(), Wallet: fixtureAddress(702), Revision: 1, MutedCreators: []string{}, FollowedCreators: []string{d.Creator}, SavedPools: []string{d.PoolAddress}}
	applyFixture(t, pool, Change{Version: 2, Kind: "preferences", Revision: 1, Preferences: &a})
	applyFixture(t, pool, Change{Version: 2, Kind: "preferences", Revision: 1, Preferences: &b})
	r := IntegratedRequest{Request: Request{Query: "coin", Kind: "pool", Limit: 25}, Sort: "address"}
	page, err := service.Public(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	principal := func(p PreferenceSnapshot) auth.IdentityPrincipal {
		return auth.IdentityPrincipal{AccountID: p.AccountID, Wallet: p.Wallet, SessionID: uuid.NewString(), AuthEpoch: 1, ProofAt: time.Now().UTC(), AccessExpiresAt: time.Now().UTC().Add(time.Minute)}
	}
	privateA, err := service.Personalize(context.Background(), page, principal(a))
	if err != nil || len(privateA.Items) != 0 {
		t.Fatal(privateA, err)
	}
	privateB, err := service.Personalize(context.Background(), page, principal(b))
	if err != nil || len(privateB.Items) != 1 || !privateB.Items[0].Followed || !privateB.Items[0].Saved {
		t.Fatal(privateB, err)
	}
	if len(page.Items) != 1 || page.Items[0].Followed || page.Items[0].Saved {
		t.Fatal("private state altered public page")
	}
	wrong := principal(a)
	wrong.AccountID = b.AccountID
	if _, err = service.Personalize(context.Background(), page, wrong); err == nil {
		t.Fatal("cross-account overlay read")
	}
}
func TestRealRedisGenerationAndTombstoneCache(t *testing.T) {
	rawURL := os.Getenv("L20_REDIS_URL")
	if rawURL == "" {
		t.Fatal("L20_REDIS_URL required for real isolated Redis qualification")
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialTimeout = 40 * time.Millisecond
	options.ReadTimeout = 40 * time.Millisecond
	options.WriteTimeout = 40 * time.Millisecond
	options.PoolTimeout = 40 * time.Millisecond
	rdb := redis.NewClient(options)
	defer rdb.Close()
	if err = rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, rdb, 2)
	if err != nil {
		t.Fatal(err)
	}
	r := IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"}
	page, err := service.Public(context.Background(), r)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	q, _ := r.Validate()
	key := service.cacheKey(q, r, page.Generation, page.Revision, page.PrivacyRevision)
	defer rdb.Del(context.Background(), key)
	wire, err := rdb.Get(context.Background(), key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var envelope authenticatedPage
	if json.Unmarshal(wire, &envelope) != nil {
		t.Fatal("invalid signed cache")
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 3, PoolAddress: d.PoolAddress, Public: false, Tombstone: true})
	page, err = service.Public(context.Background(), r)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("cached tombstone resurrected", page, err)
	}
	newKey := service.cacheKey(q, r, page.Generation, page.Revision, page.PrivacyRevision)
	defer rdb.Del(context.Background(), newKey)
	ttl, err := rdb.TTL(context.Background(), newKey).Result()
	if err != nil || ttl > 2*time.Second {
		t.Fatal(ttl, err)
	}
	if err = rdb.Del(context.Background(), key, newKey).Err(); err != nil {
		t.Fatal(err)
	}
	page, err = service.Public(context.Background(), r)
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestRedisRemovalDatabaseTimeoutAndRecovery(t *testing.T) {
	rawURL := os.Getenv("L20_REDIS_URL")
	if rawURL == "" {
		t.Fatal("L20_REDIS_URL required for real Redis interruption qualification")
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialTimeout = 40 * time.Millisecond
	options.ReadTimeout = 40 * time.Millisecond
	options.WriteTimeout = 40 * time.Millisecond
	options.PoolTimeout = 40 * time.Millisecond
	rdb := redis.NewClient(options)
	ctx := context.Background()
	if err = rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, rdb, 2)
	if err != nil {
		t.Fatal(err)
	}
	request := IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"}
	page, err := service.Public(ctx, request)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	query, _ := request.Validate()
	key := service.cacheKey(query, request, page.Generation, page.Revision, page.PrivacyRevision)
	if err = rdb.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	if err = rdb.Close(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	page, err = service.Public(ctx, request)
	if err != nil || len(page.Items) != 1 || *page.Items[0].PoolAddress != d.PoolAddress || time.Since(started) > time.Second {
		t.Fatal("cache removal lost database authority", page, err, time.Since(started))
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSearch(blocker)
	if _, err = blocker.Exec(ctx, "LOCK TABLE stats_search.documents_v2 IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	page, err = service.Public(ctx, request)
	if !errors.Is(err, ErrUnavailable) || page.Status != "" || time.Since(started) > time.Second {
		t.Fatal("database timeout became empty success", page, err, time.Since(started))
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = service.Public(ctx, request)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("database did not recover", page, err)
	}
	options.MaxRetries = -1
	recovered := redis.NewClient(options)
	defer recovered.Close()
	restored, err := NewService(Store{Pool: pool}, recovered, 2)
	if err != nil {
		t.Fatal(err)
	}
	page, err = restored.Public(ctx, request)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("cache reconnection did not recover", page, err)
	}
	key = restored.cacheKey(query, request, page.Generation, page.Revision, page.PrivacyRevision)
	defer recovered.Del(ctx, key)
	if err = recovered.Get(ctx, key).Err(); err != nil {
		t.Fatal("recovered cache did not store result", err)
	}
}

func TestSearchRejectsRedisWithoutBoundedDeadlinePolicy(t *testing.T) {
	pool := integratedDatabase(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	if _, err := NewService(Store{Pool: pool}, client, 2); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded cache provider admitted", err)
	}
}

func TestSearchAdmissionCanonicalizesIPv6Identity(t *testing.T) {
	pool := integratedDatabase(t)
	service, err := NewService(Store{Pool: pool}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if err = service.Admit("2001:db8::1"); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Admit("2001:0db8:0000:0000:0000:0000:0000:0001"); err != ErrBusy {
		t.Fatal("alternate IP spelling bypassed admission", err)
	}
	if err = service.Admit("a forged IP"); err != ErrInvalid {
		t.Fatal("untrusted IP admitted", err)
	}
}

func TestPersonalizationRejectsStalePublicRevision(t *testing.T) {
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	preferences := PreferenceSnapshot{AccountID: uuid.NewString(), Wallet: fixtureAddress(701), Revision: 1, MutedCreators: []string{}, FollowedCreators: []string{}, SavedPools: []string{}}
	applyFixture(t, pool, Change{Version: 2, Kind: "preferences", Revision: 1, Preferences: &preferences})
	page, err := service.Public(context.Background(), IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
	if err != nil {
		t.Fatal(err)
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 2, PoolAddress: d.PoolAddress, Tombstone: true})
	principal := auth.IdentityPrincipal{AccountID: preferences.AccountID, Wallet: preferences.Wallet, SessionID: uuid.NewString(), AuthEpoch: 1, ProofAt: time.Now().UTC(), AccessExpiresAt: time.Now().UTC().Add(time.Minute)}
	if _, err = service.Personalize(context.Background(), page, principal); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stale page survived personalization", err)
	}
}

func TestDatabasePressureHasBoundedCoalescedWaiters(t *testing.T) {
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSearch(blocker)
	if _, err = blocker.Exec(ctx, "LOCK TABLE stats_search.documents_v2 IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	request := IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"}
	results := make(chan error, 64)
	go func() { _, err := service.Public(ctx, request); results <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		service.mu.Lock()
		started := len(service.fills) == 1
		service.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("query did not enter bounded fill")
		}
		time.Sleep(time.Millisecond)
	}
	for i := 1; i < 64; i++ {
		go func() { _, err := service.Public(ctx, request); results <- err }()
	}
	busy := 0
	for i := 0; i < 64; i++ {
		select {
		case err := <-results:
			if errors.Is(err, ErrBusy) {
				busy++
			} else if !errors.Is(err, ErrUnavailable) {
				t.Fatal("pressure returned successful empty page", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("coalesced search exceeded bounded deadline")
		}
	}
	if busy == 0 {
		t.Fatal("excess coalesced demand was not rejected")
	}
	service.mu.Lock()
	fills := len(service.fills)
	service.mu.Unlock()
	if fills != 0 {
		t.Fatal("failed fill remained admitted", fills)
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := service.Public(ctx, request)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("coalesced admission did not recover", page, err)
	}
}

func TestActualSearchCacheMeasurementCounters(t *testing.T) {
	rawURL := os.Getenv("L20_REDIS_URL")
	if rawURL == "" {
		t.Fatal("actual Redis provider required for cache measurements")
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialTimeout = 40 * time.Millisecond
	options.ReadTimeout = 40 * time.Millisecond
	options.WriteTimeout = 40 * time.Millisecond
	options.PoolTimeout = 40 * time.Millisecond
	client := redis.NewClient(options)
	defer client.Close()
	pool := integratedDatabase(t)
	document := seedIndexed(t, pool, 1)
	service, err := NewService(Store{Pool: pool}, client, 2)
	if err != nil {
		t.Fatal(err)
	}
	request := IntegratedRequest{Request: Request{Query: document.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"}
	for i := 0; i < 2; i++ {
		page, err := service.Public(context.Background(), request)
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
	}
	measured, err := service.CacheMeasurement()
	if err != nil || measured.Requests != 2 || measured.Hits != 1 || measured.Misses != 1 || measured.DatabaseReads != 1 || measured.ActiveFills != 0 || measured.Waiters != 0 || measured.Goroutines < 1 || measured.HeapAllocBytes == 0 {
		t.Fatal("actual cache counters mismatch", measured, err)
	}
	query, _ := request.Validate()
	generation, revision, privacy, err := service.state(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Del(context.Background(), service.cacheKey(query, request, generation, revision, privacy)).Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := service.Public(context.Background(), request)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	measured, err = service.CacheMeasurement()
	if err != nil || measured.Requests != 3 || measured.Hits != 1 || measured.Misses != 2 || measured.DatabaseReads != 2 || measured.RedisErrors < 1 {
		t.Fatal("actual dependency loss measurements absent", measured, err)
	}
}
