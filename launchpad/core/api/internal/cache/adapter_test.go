package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisclient "github.com/redis/go-redis/v9"
)

func TestRoutePoliciesRejectSensitiveStale(t *testing.T) {
	for _, class := range []RouteClass{PrivateAccount, Authorization, Ownership, ExecutableQuote, AccountWrite, Administrative} {
		p := RoutePolicy{ID: "authority", Version: "v2", Origin: "postgres", Generation: "g1", Class: class, FreshTTL: time.Second, StaleTTL: time.Second, RequestLimit: 10, RequestWindow: time.Second}
		if p.validate() == nil {
			t.Fatalf("accepted stale %s", class)
		}
		p.StaleTTL = 0
		if class == PrivateAccount {
			if e := p.validate(); e != nil {
				t.Fatal(e)
			}
		} else {
			if p.validate() == nil {
				t.Fatalf("accepted cached %s", class)
			}
			p.FreshTTL = 0
			if e := p.validate(); e != nil {
				t.Fatal(e)
			}
		}
	}
	p := RoutePolicy{Parameters: map[string]ParameterRule{"limit": {Required: true, MaxBytes: 4, Pattern: "[1-9][0-9]{0,3}", MaxInteger: 2000}}}
	for _, q := range []url.Values{{"limit": {"1", "2"}}, {"limit": {"01"}}, {"limit": {"2001"}}, {"limit": {"1"}, "wallet": {"secret"}}, nil} {
		if _, e := p.normalize(q); e == nil {
			t.Fatal("invalid variant accepted", q)
		}
	}
	if _, e := p.normalize(url.Values{"limit": {"2000"}}); e != nil {
		t.Fatal(e)
	}
}
func TestEmergencyAllocationSumAndScope(t *testing.T) {
	var total int64
	for i := 0; i < 10; i++ {
		e := EmergencyAllocation{AllocationID: "approved", ReplicaSlot: i, ReplicaCount: 10, AggregateLimit: 37, Window: time.Minute}
		if err := e.validate(); err != nil {
			t.Fatal(err)
		}
		total += e.share()
	}
	if total != 37 {
		t.Fatal(total)
	}
	a := &Adapter{config: AdapterConfig{Emergency: EmergencyAllocation{AllocationID: "approved", ReplicaSlot: 0, ReplicaCount: 10, AggregateLimit: 37, Window: time.Hour}}}
	var count int
	for i := 0; i < 100; i++ {
		if a.emergency().Allowed {
			count++
		}
	}
	if count != 4 {
		t.Fatal(count)
	}
}
func TestLocalPolicyRevisionFloorAndBounds(t *testing.T) {
	a := &Adapter{local: NewWithConfig(Config{Capacity: 8, MaxBytes: 8192, MaxEntryBytes: 2048})}
	now := time.Now()
	s := sharedredis.PolicySnapshot{Generation: "g1", Revision: "9007199254740993", Visibility: "public", Class: "public_snapshot", FetchedAt: now, FreshUntil: now.Add(time.Minute), RetainUntil: now.Add(time.Minute), Body: json.RawMessage(`{"value":"new"}`), SourceWatermark: json.RawMessage(`null`)}
	s.ContentDigest = sharedredis.PolicySnapshotDigest(s)
	if !a.localPut("key", s) {
		t.Fatal("initial put")
	}
	old := s
	old.Revision = "9007199254740992"
	old.Body = json.RawMessage(`null`)
	if a.localPut("key", old) {
		t.Fatal("old invalidation accepted")
	}
	v, ok := a.localGet("key")
	if !ok || string(v.Body) != string(s.Body) {
		t.Fatal("new body lost")
	}
	floor := s
	floor.Revision = "9007199254740994"
	floor.Body = json.RawMessage(`null`)
	if !a.localPut("key", floor) {
		t.Fatal("new floor rejected")
	}
	if a.localPut("key", s) {
		t.Fatal("old body resurrected")
	}
	for i := 0; i < 100; i++ {
		a.localPut("entry"+strconv.Itoa(i), s)
		b, _, _, _ := a.local.MemoryStats()
		if b > 8192 {
			t.Fatal("byte bound", b)
		}
	}
	before, entries, _, _ := a.local.MemoryStats()
	large := s
	large.Body = json.RawMessage(`"` + strings.Repeat("x", 3000) + `"`)
	a.localPut("oversize", large)
	after, n, _, _ := a.local.MemoryStats()
	if before != after || entries != n {
		t.Fatal("oversize evicted working set")
	}
}

func TestRealPostgresOriginBudgetAndRedisLoss(t *testing.T) {
	dsn := os.Getenv("L09_DATABASE_URL")
	redisURL := os.Getenv("L09_REDIS_URL")
	ns := os.Getenv("L09_NAMESPACE")
	if dsn == "" || redisURL == "" {
		t.Skip("requires allocated real PostgreSQL and Redis")
	}
	if ns == "" {
		t.Fatal("namespace required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "cache_l09_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
	ddl := `CREATE TABLE ` + quoted + `.cache_query_budgets_v2(namespace text NOT NULL,origin text NOT NULL,window_bucket bigint NOT NULL,window_ms bigint NOT NULL,budget_limit bigint NOT NULL CHECK(budget_limit>0),used bigint NOT NULL CHECK(used>0 AND used<=budget_limit),PRIMARY KEY(namespace,origin,window_bucket,window_ms));CREATE TABLE ` + quoted + `.account_truth(wallet text PRIMARY KEY,balance numeric(78,0) NOT NULL);INSERT INTO ` + quoted + `.account_truth VALUES ('a',900719925474099312345),('b',7)`
	if _, e = pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	options, e := redisclient.ParseURL(redisURL)
	if e != nil {
		t.Fatal(e)
	}
	options.MaxRetries = -1
	client := redisclient.NewClient(options)
	defer client.Close()
	if e = client.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	budget := PostgresQueryBudget{Pool: pool, Schema: schema, Namespace: "fixture", Limit: 37, QueryCost: 1, Window: time.Hour}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for replica := 0; replica < 10; replica++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				ad, e := budget.admit(ctx, "postgres")
				if e != nil {
					t.Error(e)
					return
				}
				if ad.Allowed {
					admitted.Add(1)
				} else if ad.RetryAfter <= 0 {
					t.Error("missing retry delay")
				}
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 37 {
		t.Fatal("aggregate queries", admitted.Load())
	}
	// A closed actual client exercises unavailable Redis without stopping a shared
	// service. Every replica still shares the same durable origin query allocation.
	client.Close()
	before := ""
	if e = pool.QueryRow(ctx, `SELECT sum(balance)::text FROM `+quoted+`.account_truth`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	var fallback atomic.Int64
	var requestAdmissions atomic.Int64
	for replica := 0; replica < 10; replica++ {
		replica := replica
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := &Adapter{config: AdapterConfig{Redis: client, Namespace: "fixture", RedisTimeout: 20 * time.Millisecond, Emergency: EmergencyAllocation{AllocationID: "allocated", ReplicaSlot: replica, ReplicaCount: 10, AggregateLimit: 31, Window: time.Hour}}}
			p := RoutePolicy{ID: "public", Class: PublicSnapshot, RequestLimit: 100, RequestWindow: time.Hour}
			for i := 0; i < 20; i++ {
				if e := a.requestAdmission(ctx, p, VerifiedScope{NetworkID: "trusted-fixture"}); e != nil {
					continue
				}
				requestAdmissions.Add(1)
				ad, e := budget.admit(ctx, "outage")
				if e != nil {
					t.Error(e)
					return
				}
				if ad.Allowed {
					fallback.Add(1)
				}
			}
			p.Class = AccountWrite
			if e := a.requestAdmission(ctx, p, VerifiedScope{Principal: "a", AuthorizationRevision: "1"}); !errors.Is(e, ErrAdmissionUnavailable) {
				t.Error("sensitive outage admission", e)
			}
		}()
	}
	wg.Wait()
	if requestAdmissions.Load() != 31 || fallback.Load() > 37 {
		t.Fatal("outage exceeded allocations", requestAdmissions.Load(), fallback.Load())
	}
	after := ""
	if e = pool.QueryRow(ctx, `SELECT sum(balance)::text FROM `+quoted+`.account_truth`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if before != after {
		t.Fatal("business truth changed", before, after)
	}
}

func TestGenerationCutoverAndCopies(t *testing.T) {
	a := &Adapter{policies: map[string]RoutePolicy{"candles": {ID: "candles", Version: "v2", Generation: "g1", Class: PublicSnapshot}}, local: New(8)}
	old, e := a.policy("candles")
	if e != nil {
		t.Fatal(e)
	}
	oldKey, e := a.key(old, VerifiedScope{}, url.Values{"pool": {"125"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Cutover("candles", "wrong", "g2"); !errors.Is(e, ErrProjectionChanged) {
		t.Fatal(e)
	}
	if e = a.Cutover("candles", "g1", "g2"); e != nil {
		t.Fatal(e)
	}
	current, e := a.policy("candles")
	if e != nil {
		t.Fatal(e)
	}
	newKey, e := a.key(current, VerifiedScope{}, url.Values{"pool": {"125"}})
	if e != nil || oldKey == newKey {
		t.Fatal("generation key reused", e)
	}
	now := time.Now()
	body := json.RawMessage(`{"value":"retained"}`)
	s := sharedredis.PolicySnapshot{Generation: "g2", Revision: "1", Visibility: "public", Class: "public_snapshot", Body: body, SourceWatermark: json.RawMessage(`null`), FetchedAt: now, FreshUntil: now.Add(time.Minute), RetainUntil: now.Add(time.Minute)}
	s.ContentDigest = sharedredis.PolicySnapshotDigest(s)
	if !a.localPut(newKey, s) {
		t.Fatal("put")
	}
	body[2] = 'X'
	read, ok := a.localGet(newKey)
	if !ok || string(read.Body) != `{"value":"retained"}` {
		t.Fatal("caller buffer retained")
	}
	read.Body[2] = 'Y'
	again, ok := a.localGet(newKey)
	if !ok || string(again.Body) != `{"value":"retained"}` {
		t.Fatal("returned buffer retained")
	}
	source := AccountingProjectionSource{Generation: "g1"}
	if _, e := source.Fetch(context.Background(), ProjectionRequest{Generation: "g2"}); e == nil {
		t.Fatal("unavailable/mismatched source returned data")
	}
	if sameProjectionJSON([]byte(`{"raw":9007199254740993}`), []byte(`{"raw":9007199254740992}`)) {
		t.Fatal("exact metadata lost precision")
	}
}

type adversarialCacheFixture struct {
	ctx               context.Context
	pool              *pgxpool.Pool
	redis             *redisclient.Client
	schema, namespace string
}

func newAdversarialCacheFixture(t *testing.T) adversarialCacheFixture {
	t.Helper()
	dsn, redisURL, namespace := os.Getenv("L09_DATABASE_URL"), os.Getenv("L09_REDIS_URL"), os.Getenv("L09_NAMESPACE")
	if dsn == "" || redisURL == "" || !strings.HasPrefix(namespace, "kd-421d7541932d2fd2136b3973") {
		t.Fatal("allocated real PostgreSQL, Redis and lane namespace required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	client, e := sharedredis.NewBoundedClient(redisURL, 200*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = client.Close() })
	if e = client.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	schema := "cache_adversarial_" + suffix
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if _, e := pool.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	if _, e = pool.Exec(ctx, `CREATE TABLE `+quoted+`.cache_query_budgets_v2(namespace text NOT NULL,origin text NOT NULL,window_bucket bigint NOT NULL,window_ms bigint NOT NULL,budget_limit bigint NOT NULL CHECK(budget_limit>0),used bigint NOT NULL CHECK(used>0 AND used<=budget_limit),PRIMARY KEY(namespace,origin,window_bucket,window_ms))`); e != nil {
		t.Fatal(e)
	}
	f := adversarialCacheFixture{ctx, pool, client, schema, namespace + "_" + suffix}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		var cursor uint64
		for {
			keys, next, e := client.Scan(cleanup, cursor, f.namespace+":*", 100).Result()
			if e != nil {
				t.Error(e)
				return
			}
			if len(keys) > 0 {
				if e = client.Del(cleanup, keys...).Err(); e != nil {
					t.Error(e)
					return
				}
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
	return f
}

func (f adversarialCacheFixture) adapter(t *testing.T, replica int) *Adapter {
	t.Helper()
	private := RoutePolicy{ID: "portfolio_private", Version: "v2", Origin: "postgres", Generation: "g1", Class: PrivateAccount, FreshTTL: 2 * time.Second, RequestLimit: 10000, RequestWindow: time.Hour, Parameters: map[string]ParameterRule{"page": {Required: true, MaxBytes: 1, Pattern: "1"}}}
	public := private
	public.ID, public.Class = "market_public", PublicSnapshot
	public.StaleTTL = 0
	a, e := NewAdapter(AdapterConfig{Redis: f.redis, Namespace: strings.ReplaceAll(f.namespace, ":", "_"), Local: Config{Capacity: 128, MaxBytes: 4 << 20, MaxEntryBytes: 1 << 20, MaxFetches: 4, MaxPerOrigin: 4, FetchTimeout: time.Second}, Policies: []RoutePolicy{private, public}, Budget: PostgresQueryBudget{Pool: f.pool, Schema: f.schema, Namespace: "adversarial", Limit: 37, QueryCost: 1, Window: time.Hour}, Emergency: EmergencyAllocation{AllocationID: "adversarial", ReplicaSlot: replica, ReplicaCount: 10, AggregateLimit: 37, Window: time.Hour}, RedisTimeout: 200 * time.Millisecond, LeaseTTL: 2 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func (f adversarialCacheFixture) privateFetch(ctx context.Context, r ProjectionRequest) (Projection, error) {
	var p Projection
	parts := strings.Split(r.Scope.Principal, ":")
	if len(parts) != 2 || r.Scope.AuthorizationRevision == "" {
		return p, sharedauth.ErrScopeDenied
	}
	wallet, e := sharedauth.NormalizeWallet(parts[1])
	if e != nil || wallet != parts[1] {
		return p, sharedauth.ErrScopeDenied
	}
	p.Generation, p.Visibility = r.Generation, "private"
	e = f.pool.QueryRow(ctx, `SELECT revision::text,jsonb_build_object('snapshotId',id::text,'sourceEvent',source_event,'digest',digest),jsonb_build_object('wallet',wallet,'summary',summary) FROM pnl.portfolio_snapshots WHERE wallet=$1 ORDER BY revision DESC LIMIT 1`, wallet).Scan(&p.Revision, &p.SourceWatermark, &p.Body)
	return p, e
}

func (f adversarialCacheFixture) publicFetch(ctx context.Context, r ProjectionRequest) (Projection, error) {
	p := Projection{Generation: r.Generation, Visibility: "public"}
	if r.Scope.Principal != "" || r.Scope.AuthorizationRevision != "" {
		return p, sharedauth.ErrScopeDenied
	}
	e := f.pool.QueryRow(ctx, `SELECT sequence::text,position,fill FROM stats.accounting_market_commits ORDER BY sequence DESC,publication_id LIMIT 1`).Scan(&p.Revision, &p.SourceWatermark, &p.Body)
	return p, e
}

func phase3IdentityResolver(t *testing.T) ScopeResolver {
	t.Helper()
	endpoint, token := os.Getenv("L09_IDENTITY_ENDPOINT"), os.Getenv("L09_IDENTITY_SERVICE_TOKEN")
	u, e := url.Parse(endpoint)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || len(token) < 32 {
		t.Fatal("real loopback identity authority and service credential required")
	}
	return IdentityScope(sharedauth.HTTPIdentityAuthority{Endpoint: endpoint, ServiceToken: token})
}

func phase3AuthenticatedRequest(t *testing.T, ctx context.Context, token string) *http.Request {
	t.Helper()
	if len(token) < 32 {
		t.Fatal("real admitted wallet session credential required")
	}
	r, e := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1/private/portfolio?page=1", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestRealTwoAuthenticatedWalletCacheIsolation(t *testing.T) {
	f := newAdversarialCacheFixture(t)
	a := f.adapter(t, 0)
	resolve := phase3IdentityResolver(t)
	tokens := []string{os.Getenv("L09_WALLET_A_ACCESS"), os.Getenv("L09_WALLET_B_ACCESS")}
	var scopes [2]VerifiedScope
	var expected [2]Projection
	for i, token := range tokens {
		r := phase3AuthenticatedRequest(t, f.ctx, token)
		var e error
		scopes[i], e = resolve(r)
		if e != nil {
			t.Fatal(e)
		}
		expected[i], e = f.privateFetch(f.ctx, ProjectionRequest{RouteID: "portfolio_private", Generation: "g1", Parameters: url.Values{"page": {"1"}}, Scope: scopes[i]})
		if e != nil {
			t.Fatal(e)
		}
	}
	if scopes[0].Principal == scopes[1].Principal || bytes.Equal(expected[0].Body, expected[1].Body) {
		t.Fatal("fixture must contain distinct authenticated wallets with distinct durable portfolios")
	}
	handler := a.Handler("portfolio_private", resolve, f.privateFetch)
	var etags [2]string
	for round := 0; round < 3; round++ {
		for i, token := range tokens {
			r := phase3AuthenticatedRequest(t, f.ctx, token)
			if round > 0 {
				r.Header.Set("If-None-Match", etags[1-i])
			}
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), expected[i].Body) || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("private response crossed scope or failed: wallet index %d status %d", i, w.Code)
			}
			etags[i] = w.Header().Get("ETag")
		}
	}
	if etags[0] == "" || etags[0] == etags[1] {
		t.Fatal("private representation validator crossed wallet scope")
	}
	invalid := phase3AuthenticatedRequest(t, f.ctx, tokens[0])
	invalid.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
	w := httptest.NewRecorder()
	handler(w, invalid)
	if w.Code != http.StatusForbidden {
		t.Fatal("unauthenticated request received cached private data", w.Code)
	}
}

func TestRealHotExpiryAcrossTenReplicas(t *testing.T) {
	f := newAdversarialCacheFixture(t)
	var calls atomic.Int64
	fetch := func(ctx context.Context, r ProjectionRequest) (Projection, error) {
		calls.Add(1)
		return f.publicFetch(ctx, r)
	}
	var adapters [10]*Adapter
	for i := range adapters {
		adapters[i] = f.adapter(t, i)
	}
	scope := VerifiedScope{NetworkID: "127.0.0.1"}
	q := url.Values{"page": {"1"}}
	for _, a := range adapters {
		if _, _, e := a.Read(f.ctx, "market_public", q, scope, fetch); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("warm replicas repeated origin query", calls.Load())
	}
	p, e := adapters[0].policy("market_public")
	if e != nil {
		t.Fatal(e)
	}
	key, e := adapters[0].key(p, scope, q)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.redis.PExpire(f.ctx, key, time.Millisecond).Err(); e != nil {
		t.Fatal(e)
	}
	time.Sleep(3 * time.Millisecond)
	if n, e := f.redis.Exists(f.ctx, key).Result(); e != nil || n != 0 {
		t.Fatal("hot key did not expire", n, e)
	}
	calls.Store(0)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int64
	for i := 0; i < 200; i++ {
		a := adapters[i%len(adapters)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, stale, e := a.Read(f.ctx, "market_public", q, scope, fetch)
			if e == nil {
				if stale {
					t.Error("expired snapshot presented as fresh")
				}
				successes.Add(1)
			} else if !errors.Is(e, ErrRefreshHeld) {
				t.Error(e)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() == 0 || calls.Load() != 1 {
		t.Fatal("expiry amplified origin queries", successes.Load(), calls.Load())
	}
	var used int64
	if e := f.pool.QueryRow(f.ctx, `SELECT COALESCE(sum(used),0)::bigint FROM `+pgx.Identifier{f.schema, "cache_query_budgets_v2"}.Sanitize()).Scan(&used); e != nil || used != 2 {
		t.Fatal("origin budget does not match actual refreshes", used, e)
	}
}

func (f adversarialCacheFixture) durableTruth(t *testing.T) [3]json.RawMessage {
	t.Helper()
	var result [3]json.RawMessage
	tx, e := f.pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	for i, table := range []string{"pnl.portfolio_snapshots", "indexer.indexer_replay_checkpoints", "users.operation_ledger"} {
		var count int64
		if e = tx.QueryRow(f.ctx, `SELECT count(*),COALESCE(jsonb_agg(to_jsonb(source) ORDER BY to_jsonb(source)::text),'[]'::jsonb) FROM `+table+` source`).Scan(&count, &result[i]); e != nil {
			t.Fatal(e)
		}
		if count == 0 {
			t.Fatal("admitted durable truth fixture must be populated", table)
		}
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	return result
}

func TestRealExclusiveRedisFlushPreservesDurableTruth(t *testing.T) {
	f := newAdversarialCacheFixture(t)
	u, e := url.Parse(os.Getenv("L09_REDIS_URL"))
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || f.redis.Options().DB <= 0 || os.Getenv("L09_EXCLUSIVE_REDIS_DB") != strconv.Itoa(f.redis.Options().DB) {
		t.Fatal("flush requires an explicitly allocated exclusive nonzero loopback Redis database")
	}
	a := f.adapter(t, 0)
	resolve := phase3IdentityResolver(t)
	tokens := []string{os.Getenv("L09_WALLET_A_ACCESS"), os.Getenv("L09_WALLET_B_ACCESS")}
	var scopes [2]VerifiedScope
	for i, token := range tokens {
		scopes[i], e = resolve(phase3AuthenticatedRequest(t, f.ctx, token))
		if e != nil {
			t.Fatal(e)
		}
	}
	if scopes[0].Principal == scopes[1].Principal {
		t.Fatal("distinct live wallet sessions required")
	}
	q := url.Values{"page": {"1"}}
	for _, scope := range scopes {
		if _, _, e = a.Read(f.ctx, "portfolio_private", q, scope, f.privateFetch); e != nil {
			t.Fatal(e)
		}
	}
	publicScope := VerifiedScope{NetworkID: "127.0.0.1"}
	if _, _, e = a.Read(f.ctx, "market_public", q, publicScope, f.publicFetch); e != nil {
		t.Fatal(e)
	}
	before := f.durableTruth(t)
	started := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		close(started)
		for i := 0; i < 60; i++ {
			var readErr error
			if i%3 == 0 {
				_, _, readErr = a.Read(f.ctx, "market_public", q, publicScope, f.publicFetch)
			} else {
				_, _, readErr = a.Read(f.ctx, "portfolio_private", q, scopes[i%2], f.privateFetch)
			}
			if readErr != nil && !errors.Is(readErr, ErrProjectionChanged) && !errors.Is(readErr, ErrRefreshHeld) {
				t.Error(readErr)
				return
			}
		}
	}()
	<-started
	if e = f.redis.FlushDB(f.ctx).Err(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-completed:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	for _, scope := range scopes {
		want, e := f.privateFetch(f.ctx, ProjectionRequest{Generation: "g1", Scope: scope})
		if e != nil {
			t.Fatal(e)
		}
		got, stale, e := a.Read(f.ctx, "portfolio_private", q, scope, f.privateFetch)
		if e != nil || stale || !bytes.Equal(want.Body, got.Body) || want.Revision != got.Revision {
			t.Fatal("private projection failed durable reconciliation", e)
		}
	}
	restarted := f.adapter(t, 1)
	if _, _, e = restarted.Read(f.ctx, "market_public", q, publicScope, f.publicFetch); e != nil {
		t.Fatal(e)
	}
	after := f.durableTruth(t)
	for i := range before {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatal("Redis flush changed persisted business truth", i)
		}
	}
	invalid := phase3AuthenticatedRequest(t, f.ctx, tokens[0])
	invalid.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
	w := httptest.NewRecorder()
	a.Handler("portfolio_private", resolve, f.privateFetch)(w, invalid)
	if w.Code != http.StatusForbidden {
		t.Fatal("flush authorized forbidden private request", w.Code)
	}
	stats := a.Stats()
	if stats.ActiveReconstructions != 0 || stats.CompletedReconstructions < 3 || stats.Bytes > 4<<20 {
		t.Fatal("reconstruction or bounded memory state", stats)
	}
}

func TestRealAdapterMeasurementCapture(t *testing.T) {
	f := newAdversarialCacheFixture(t)
	a := f.adapter(t, 0)
	q := url.Values{"page": {"1"}}
	scope := VerifiedScope{NetworkID: "127.0.0.1"}
	for i := 0; i < 2; i++ {
		if _, _, e := a.Read(f.ctx, "market_public", q, scope, f.publicFetch); e != nil {
			t.Fatal(e)
		}
	}
	capture := a.MeasurementSnapshot("replica0", strings.Repeat("a", 64))
	counts := map[string]uint64{}
	for _, series := range capture.Series {
		if series.Route != "market_public" || series.Class != "anonymous_read" {
			t.Fatal("unscoped measurement", series)
		}
		var bucketTotal uint64
		for _, count := range series.DurationBuckets {
			bucketTotal += count
		}
		if bucketTotal != series.Count {
			t.Fatal("measurement histogram count lost", series.Event)
		}
		counts[series.Event] += series.Count
	}
	if counts["origin"] != 1 || counts["hit"] != 1 || counts["database_pool_wait"] != 1 || capture.Stats.Bytes <= 0 || capture.Stats.Bytes > capture.MaxBytes {
		t.Fatal("actual request measurements absent or inconsistent", counts, capture.Stats)
	}
	w := httptest.NewRecorder()
	a.MeasurementHandler("replica0", strings.Repeat("a", 64), nil)(w, httptest.NewRequest("GET", "http://127.0.0.1/measurements", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("measurement handler bypassed missing authority", w.Code)
	}
}
