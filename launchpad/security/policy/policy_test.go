package policy

import (
	"context"
	"encoding/json"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type verifier struct {
	id  Identity
	err error
}

func (v verifier) Verify(context.Context, *http.Request) (Identity, error) { return v.id, v.err }

type journal struct {
	n   int
	err error
}

func (j *journal) Record(context.Context, Decision) error { j.n++; return j.err }
func TestOperatorAuthority(t *testing.T) {
	for _, kind := range []string{"user", "workload", "operator"} {
		t.Run(kind, func(t *testing.T) {
			id := Identity{Actor: "fixture", Kind: kind, Audience: "indexer", Issued: time.Now(), Expires: time.Now().Add(time.Minute), MFA: true, Scopes: map[string]bool{"replay": true}}
			j := &journal{}
			writes := 0
			h := Authorize(verifier{id, nil}, j, "indexer", "replay", "v1", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { writes++ }))
			r := httptest.NewRequest("POST", "/status/dead-letter/drain", nil)
			r.Header.Set("X-Actor-Role", "operator")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if (kind == "operator") != (writes == 1) || j.n != 1 {
				t.Fatal(w.Code, writes)
			}
		})
	}
	h := Authorize(verifier{err: ErrDenied}, &journal{}, "indexer", "replay", "v1", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("mutation") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/status/dead-letter/drain", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestRequestParsing(t *testing.T) {
	for _, body := range []string{strings.Repeat(" ", 65537), `{"name":"a","name":"b"}`, `{"unknown":1}`, `{"name":"a"} {}`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)} {
		var dst struct{ Name string }
		if DecodeMutation(httptest.NewRequest("POST", "/", strings.NewReader(body)), &dst, 65536) == nil {
			t.Fatal("accepted invalid")
		}
	}
	var dst struct{ Name string }
	if e := DecodeMutation(httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"}`)), &dst, 65536); e != nil {
		t.Fatal(e)
	}
}
func TestSSRF(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "169.254.169.254", "10.1.2.3", "::1", "fc00::1", "100.64.1.2", "198.18.1.1"} {
		if publicIP(net.ParseIP(a)) {
			t.Fatal(a)
		}
	}
	u, _ := url.Parse("https://169.254.169.254/latest")
	if PublicURL(u, map[string]bool{"metadata.example": true}) == nil {
		t.Fatal("redirect allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, e := DialPublic(ctx, "tcp", "127.0.0.1:443"); e == nil {
		c.Close()
		t.Fatal("connected private")
	}
}
func TestSecretSelection(t *testing.T) {
	t.Setenv("SECRETS_PROVIDER", "vault")
	t.Setenv("CANARY_L45", "must-not-fallback")
	if _, e := secrets.NewChecked(secrets.Options{}); e == nil {
		t.Fatal("startup accepted")
	}
	if _, _, e := secrets.New(secrets.Options{}).Get(context.Background(), "CANARY_L45"); e == nil {
		t.Fatal("fallback")
	}
}
func TestRotation(t *testing.T) {
	n := time.Now()
	old := Key{"hook", "old", "vendor", n.Add(-time.Hour), n.Add(time.Minute), []byte(strings.Repeat("a", 32)), false}
	next := Key{"hook", "new", "vendor", n, n.Add(time.Hour), []byte(strings.Repeat("b", 32)), false}
	r := &KeyRing{}
	if e := r.Replace([]Key{old}); e != nil {
		t.Fatal(e)
	}
	_, sig, _ := r.Sign("vendor", []byte("body"), n)
	if e := r.Replace([]Key{old, next}); e != nil {
		t.Fatal(e)
	}
	v, _, _ := r.Sign("vendor", nil, n)
	if v != "new" || !r.Verify("vendor", "old", sig, []byte("body"), n) || r.Verify("vendor", "old", sig, []byte("body"), n.Add(time.Minute)) || r.Verify("vendor", "new", "", nil, n) {
		t.Fatal("rotation")
	}
	next.Revoked = true
	r.Replace([]Key{next})
	if _, _, e := r.Sign("vendor", nil, n); e == nil {
		t.Fatal("revoked")
	}
}
func TestRouteAndRedaction(t *testing.T) {
	if ValidateRoutes([]Route{{Method: "POST", Path: "/new"}}) == nil {
		t.Fatal("unclassified")
	}
	h := http.Header{}
	for _, k := range []string{"Authorization", "Cookie", "X-API-Key", "X-Signature", "X-Stream-Key"} {
		h.Set(k, "canary-secret-"+k)
	}
	b, _ := json.Marshal(SafeHeaders(h))
	if strings.Contains(string(b), "canary-secret") {
		t.Fatal(string(b))
	}
}
func TestAPIKeyHealthPrefix(t *testing.T) {
	mw, e := sharedhttp.APIKeyAuth(sharedhttp.APIKeyOptions{APIKey: "fixture-key"})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/health-admin", "/health/drain", "/status/dead-letter/drain"} {
		w := httptest.NewRecorder()
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("bypass", path) })).ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}
func TestRedisDistributedBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer r.Close()
	if e := r.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	ns := "kd_12d3ff56525034b7bcfef87a:" + time.Now().Format("150405.000000000")
	r2 := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer r2.Close()
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 61; i++ {
		wg.Add(1)
		client := r
		if i%2 == 1 {
			client = r2
		}
		go func() {
			defer wg.Done()
			ok, _, e := Take(ctx, client, ns, "account-write", "same-principal", 1)
			if e != nil {
				t.Error(e)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatal(allowed.Load())
	}
	keys, e := r.Keys(ctx, ns+":*").Result()
	if e != nil || len(keys) != 1 {
		t.Fatal(keys, e)
	}
	ttl, _ := r.PTTL(ctx, keys[0]).Result()
	if ttl <= 0 || ttl > 120*time.Second {
		t.Fatal(ttl)
	}
	if e := r.Del(ctx, keys...).Err(); e != nil {
		t.Fatal(e)
	}
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:24503", DialTimeout: time.Millisecond, MaxRetries: -1})
	defer dead.Close()
	if ok, _, e := Take(ctx, dead, ns, "account-write", "x", 1); ok || e == nil {
		t.Fatal("outage admitted")
	}
}
func TestAuditRollbackRealPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, e := pgxpool.New(ctx, "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s := "kd_12d3ff56525034b7bcfef87a_" + time.Now().Format("150405000000000")
	schema := pgx.Identifier{s}.Sanitize()
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		if _, e := p.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	if _, e = p.Exec(ctx, "CREATE TABLE "+schema+".caps (n integer); INSERT INTO "+schema+".caps VALUES (1); CREATE TABLE "+schema+".audit(record jsonb CHECK(false))"); e != nil {
		t.Fatal(e)
	}
	e = MutateAudited(ctx, p, pgx.Identifier{s, "audit"}, Audit{Actor: "operator", Action: "cap-write", PolicyVersion: "v1"}, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, "UPDATE "+schema+".caps SET n=2"); return e })
	if e == nil {
		t.Fatal("audit failure accepted")
	}
	var n int
	if e = p.QueryRow(ctx, "SELECT n FROM "+schema+".caps").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func TestUnicodeAndInitialURL(t *testing.T) {
	for _, b := range []string{string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), `{"Name":"\ud800"}`, `{"Name":"\udc00"}`} {
		var d struct{ Name string }
		if DecodeMutation(httptest.NewRequest("POST", "/", strings.NewReader(b)), &d, 65536) == nil {
			t.Fatal("bad Unicode accepted")
		}
	}
	c := PublicClient(map[string]bool{"public.example": true})
	for _, u := range []string{"http://public.example", "https://user:pass@public.example", "https://public.example:444"} {
		if _, e := c.Get(u); e == nil {
			t.Fatal("URL accepted", u)
		}
	}
}
func TestEmptyAuthorityConfig(t *testing.T) {
	id := Identity{Actor: "op", Kind: "operator", MFA: true, Issued: time.Now(), Expires: time.Now().Add(time.Minute), Scopes: map[string]bool{"": true}}
	w := httptest.NewRecorder()
	Authorize(verifier{id, nil}, &journal{}, "", "", "", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("empty policy authorized") })).ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestAuthorityNegatives(t *testing.T) {
	n := time.Now()
	base := Identity{Actor: "op", Kind: "operator", Audience: "indexer", MFA: true, Issued: n, Expires: n.Add(time.Minute), Scopes: map[string]bool{"replay": true}}
	for _, which := range []string{"audience", "scope", "mfa", "expired", "long", "journal"} {
		t.Run(which, func(t *testing.T) {
			id := base
			j := &journal{}
			switch which {
			case "audience":
				id.Audience = "other"
			case "scope":
				id.Scopes = map[string]bool{}
			case "mfa":
				id.MFA = false
			case "expired":
				id.Expires = n.Add(-time.Second)
			case "long":
				id.Expires = n.Add(16 * time.Minute)
			case "journal":
				j.err = ErrDenied
			}
			w := httptest.NewRecorder()
			Authorize(verifier{id, nil}, j, "indexer", "replay", "v1", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthorized mutation") })).ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
			want := 403
			if which == "journal" {
				want = 503
			}
			if w.Code != want {
				t.Fatal(w.Code)
			}
		})
	}
}
func TestAPIKeyAmbiguityAndHealth(t *testing.T) {
	mw, _ := sharedhttp.APIKeyAuth(sharedhttp.APIKeyOptions{APIKey: "fixture-key"})
	r := httptest.NewRequest("POST", "/private", nil)
	r.Header.Add("X-API-Key", "fixture-key")
	r.Header.Add("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("ambiguous allowed") })).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, p := range []string{"/health", "/health/live", "/health/ready"} {
		called := false
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
		if !called {
			t.Fatal(p)
		}
	}
}
func TestRotationInvalidReplace(t *testing.T) {
	n := time.Now()
	v := Key{"id", "v1", "vendor", n.Add(-time.Minute), n.Add(time.Hour), []byte(strings.Repeat("a", 32)), false}
	r := &KeyRing{}
	if e := r.Replace([]Key{v}); e != nil {
		t.Fatal(e)
	}
	next := v
	next.Version = "v2"
	next.Activate = n
	if r.Replace([]Key{v, next}) == nil {
		t.Fatal("long overlap")
	}
	version, sig, e := r.Sign("vendor", nil, n)
	if e != nil || version != "v1" || !r.Verify("vendor", "v1", sig, nil, n) || r.Verify("wrong", "v1", sig, nil, n) {
		t.Fatal("failed replace changed ring")
	}
}

func TestHealthMutationCannotBypassAPIKey(t *testing.T) {
	middleware, err := sharedhttp.APIKeyAuth(sharedhttp.APIKeyOptions{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/health", "/health/live", "/health/ready"} {
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("health mutation bypassed authority", method, path) })).ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
			if recorder.Code != 401 {
				t.Fatal(recorder.Code)
			}
		}
	}
}

func TestUnclassifiedPublicMutationAndBudgetAreDenied(t *testing.T) {
	for _, route := range []Route{{Method: "POST", Path: "/new", Class: "public-read", Public: true}, {Method: "GET", Path: "/new", Class: "guessed", Public: true}, {Method: "get", Path: "/new", Class: "public-read", Public: true}} {
		if ValidateRoutes([]Route{route}) == nil {
			t.Fatal("unclassified public route admitted", route)
		}
	}
	if ValidateRoutes(nil) == nil {
		t.Fatal("empty release registry admitted")
	}
}
