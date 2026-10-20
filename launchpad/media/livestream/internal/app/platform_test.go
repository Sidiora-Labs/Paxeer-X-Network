package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/streams"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production/coordinatortest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const platformViewerKey = "v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7v7"

// platformPKI is the throwaway private PKI of one platform test: the gateway
// client certificate the private listener pins, the livestream server
// certificate, and another service's certificate of the same CA.
type platformPKI struct {
	ca                                  *testpki.PKI
	gatewayClient, server, otherService testpki.Leaf
}

func newPlatformPKI(t *testing.T) platformPKI {
	ca := testpki.New(t)
	return platformPKI{ca: ca, gatewayClient: ca.Leaf("gateway-client", "gateway.railway.internal"), server: ca.Leaf("livestream-server", "livestream.railway.internal"), otherService: ca.Leaf("social-client", "social.railway.internal")}
}

func (p platformPKI) client(leaf testpki.Leaf) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: p.ca.Roots(), Certificates: []tls.Certificate{leaf.Pair}}}}
}

type platformServers struct {
	private, plain *httptest.Server
	pki            platformPKI
}

// platformDatabase is a private database bootstrapped the way the production
// migration coordinator (kindle-migrate) bootstraps kindle_prod, scoped to the
// registered migrations livestream_runtime reads (livestream, chat incl.
// chat.safety_bans_v1, stats incl. stats.pool_market_caps_v1, indexer,
// recovery), with livestream_runtime as a LOGIN role holding exactly its
// reviewed grants.json privileges. L40_TEST_APP_POSTGRES_DSN names the
// server (its role may CREATE DATABASE and CREATE ROLE); the database and the
// roles are dropped when the test ends. The returned pool is the
// administrator's, standing in for the producers of the seeded rows.
func platformDatabase(t *testing.T) (coordinatortest.Database, *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv("L40_TEST_APP_POSTGRES_DSN")
	if admin == "" {
		t.Fatal("L40_TEST_APP_POSTGRES_DSN (a server whose role may CREATE DATABASE and CREATE ROLE) is required; no silent skip")
	}
	db := coordinatortest.Bootstrap(t, admin, "livestream_runtime")
	pool, err := pgxpool.New(context.Background(), db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return db, pool
}

// platformAppServers boots the production platform runtime
// (NODE_ENV=production, RUNTIME_PROFILE=platform,
// LIVEPEER_PROVIDER_MODE=unavailable) on a coordinator-bootstrapped database
// as livestream_runtime, verified against the coordinator's ExpectedSchema
// document, with its secrets resolved through SECRETS_PROVIDER=env as on the
// platform. The API is reached on the private mTLS listener with the
// gateway's client certificate (a throwaway PKI per call); the plain listener
// serves health.
func platformAppServers(t *testing.T, db coordinatortest.Database) platformServers {
	t.Helper()
	redisURL := os.Getenv("L40_TEST_REDIS_URL")
	if redisURL == "" {
		t.Fatal("L40_TEST_REDIS_URL is required; no silent skip")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pki := newPlatformPKI(t)
	t.Setenv("DATABASE_URL", db.Runtimes["livestream_runtime"].DSN)
	t.Setenv("REDIS_URL", redisURL)
	t.Setenv("LIVESTREAM_VIEWER_TOKEN_KEY", platformViewerKey)
	t.Setenv("LIVESTREAM_TLS_KEY_PEM", string(pki.server.KeyPEM))
	cfg := platformConfig()
	cfg.ExpectedSchemaFile, cfg.ExpectedSchemaSHA256 = db.ExpectedSchemaFile(t, "livestream_runtime")
	cfg.PrivateListenAddress = "127.0.0.1:0"
	cfg.PrivateTLSCertFile, cfg.PrivateTLSKeyFile, cfg.PrivateTLSKeyPEM, cfg.PrivateTLSClientCAFile = pki.server.CertFile, "", "unresolved", pki.ca.CAFile
	cfg.GatewayPeerCertSHA256 = pki.gatewayClient.SHA256
	if err := cfg.Validate(); err != nil {
		cancel()
		t.Fatal(err)
	}
	a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if a.private == nil || a.PrivateRouter == nil {
		t.Fatal("platform private listener not configured")
	}
	private := httptest.NewUnstartedServer(a.PrivateRouter)
	private.TLS = a.private.tls
	private.StartTLS()
	testClients.Store(private.URL, pki.client(pki.gatewayClient))
	plain := httptest.NewServer(a.Router)
	t.Cleanup(func() {
		testClients.Delete(private.URL)
		private.Close()
		plain.Close()
		a.Close()
		cancel()
	})
	return platformServers{private: private, plain: plain, pki: pki}
}

func platformConfig() config.Config {
	return config.Config{ExpectedSchemaFile: "/etc/sidiora/livestream/expected-schema.json", ExpectedSchemaSHA256: strings.Repeat("cd", 32), NodeEnv: "production", RuntimeProfile: config.PlatformProfile, RuntimeZone: "zone-a", RuntimeMetricsEnvironment: "production", SecretsProvider: "env", SecretsRefreshSeconds: 30, DatabaseURL: "postgres://unresolved/unresolved", RedisURL: "redis://unresolved", LivepeerAPIURL: "https://livepeer.studio/api", ProviderMode: "unavailable", LogLevel: "info", Port: 8080, MaxStreamsPerWallet: 3, MaxGlobalStreams: 100, DBMaxConns: 10, CORSAllowedOrigins: "https://web.example", TrustedProxyCIDRs: "127.0.0.0/8,::1/128",
		PrivateListenAddress: "[::]:8443", PrivateTLSCertFile: "/etc/sidiora/pki/livestream/server.pem", PrivateTLSKeyFile: "/etc/sidiora/pki/livestream/server.key", PrivateTLSClientCAFile: "/etc/sidiora/pki/ca/ca.pem", GatewayPeerCertSHA256: strings.Repeat("ab", 32)}
}

func TestPlatformRuntimeServesPublicReadSurface(t *testing.T) {
	db, pool := platformDatabase(t)
	server := platformAppServers(t, db).private
	status, raw, _ := do(t, http.MethodGet, server.URL+"/health/ready", "", nil)
	if status != 200 || !strings.Contains(string(raw), `"stream_read_schema"`) || strings.Contains(string(raw), "stream_provider_authority") {
		t.Fatalf("readiness %d %s", status, raw)
	}
	for _, path := range []string{"/v2/streams/live", "/v2/streams/live?sort=viewers", "/v2/streams/pool/" + address(t)} {
		status, raw, _ = do(t, http.MethodGet, server.URL+path, "", nil)
		if status != 200 {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
		decodeDirectory(t, raw)
	}
	wallet, poolAddress, ended := address(t), address(t), uuid4(t)
	insertOperation(t, pool, ended, wallet, poolAddress, "ENDED")
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/"+ended, "", nil)
	if status != 200 || !strings.Contains(string(raw), `"status":"ended"`) {
		t.Fatalf("stream document %d %s", status, raw)
	}
	// The market-cap reader (the directory's mcap sort) reads the indexer's
	// canonical creations and the stats market caps through the versioned
	// views livestream_runtime is granted (indexer.indexer_canonical_creations_v1,
	// stats.pool_market_caps_v1); exercised as livestream_runtime. The platform
	// runtime itself answers live rows BROADCAST_NOT_PROVISIONED (no playback
	// provider), so its directory never reaches the join.
	small, large := uuid4(t), uuid4(t)
	caps := map[string]string{}
	for _, live := range []struct{ id, wallet, pool, cap string }{{small, address(t), address(t), "250"}, {large, address(t), address(t), "5000000"}} {
		insertOperation(t, pool, live.id, live.wallet, live.pool, "LIVE")
		if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$4,1,1,$5,$6,0,0,1,'{}','{}')`, address(t), live.pool, live.wallet, address(t), "0x"+strings.Repeat("ab", 32), "0x"+strings.ReplaceAll(uuid4(t), "-", "")+strings.Repeat("0", 32)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `INSERT INTO stats.pool_stats(pool_address,token_address,market_cap,created_at,updated_at) VALUES($1,$2,$3,1,1)`, live.pool, address(t), live.cap); err != nil {
			t.Fatal(err)
		}
		caps[live.pool] = live.cap
	}
	runtimePool, err := pgxpool.New(context.Background(), db.Runtimes["livestream_runtime"].DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	pools := []string{}
	for p := range caps {
		pools = append(pools, p)
	}
	got, err := (&streams.MarketCapReader{Pool: runtimePool}).MarketCaps(context.Background(), pools)
	if err != nil || len(got) != 2 {
		t.Fatalf("market caps as livestream_runtime: %v %v", got, err)
	}
	for p, want := range caps {
		if got[p] == nil || got[p].RatString() != want {
			t.Fatalf("market cap %s = %v, want %s", p, got[p], want)
		}
	}
	// A pool-scoped ban issued through media/social's safety store suspends
	// the stream (read through chat.safety_bans_v1).
	if _, err := pool.Exec(context.Background(), `INSERT INTO chat.safety_bans(id,wallet,scope,resource,actor) SELECT gen_random_uuid(),wallet,'pool',pool,'moderator' FROM livestream.stream_operations WHERE id=$1`, large); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/"+large, "", nil)
	if status != 200 || !strings.Contains(string(raw), `"status":"suspended"`) {
		t.Fatalf("banned stream document %d %s", status, raw)
	}
	var banned struct{ wallet, pool string }
	if err = pool.QueryRow(context.Background(), `SELECT wallet,pool FROM livestream.stream_operations WHERE id=$1`, large).Scan(&banned.wallet, &banned.pool); err != nil {
		t.Fatal(err)
	}
	if allowed, err := (&streams.RestrictionReader{Pool: runtimePool}).Allowed(context.Background(), banned.wallet, banned.pool); err != nil || allowed {
		t.Fatalf("restriction as livestream_runtime: allowed=%v %v", allowed, err)
	}
	// Provider-bound routes answer the stable unprovisioned contract error and
	// persist nothing.
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/v2/streams", `{"poolAddress":"` + address(t) + `","title":"x"}`},
		{http.MethodPost, "/v2/streams/" + ended + "/go-live", `{"revision":"1"}`},
		{http.MethodPost, "/webhooks/livepeer", `{}`},
	} {
		status, raw, header := do(t, route.method, server.URL+route.path, route.body, nil)
		e := requireError(t, status, raw, 409, "BROADCAST_NOT_PROVISIONED")
		if e.Retryable || header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: %s", route.method, route.path, raw)
		}
	}
}

func TestPlatformViewerSessionsVerifyAcrossInstances(t *testing.T) {
	db, pool := platformDatabase(t)
	first, second := platformAppServers(t, db).private, platformAppServers(t, db).private
	live := uuid4(t)
	insertOperation(t, pool, live, address(t), address(t), "LIVE")
	status, raw, _ := do(t, http.MethodPost, first.URL+"/v2/streams/"+live+"/viewer-session", `{}`, nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	var session struct {
		SessionID string `json:"sessionId"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(raw, &session); err != nil || session.Token == "" {
		t.Fatalf("%s %v", raw, err)
	}
	// The session issued by one instance is honoured by another (the signing
	// key is the shared LIVESTREAM_VIEWER_TOKEN_KEY, not a per-process key).
	body, _ := json.Marshal(map[string]string{"sessionId": session.SessionID, "token": session.Token})
	status, raw, _ = do(t, http.MethodPost, second.URL+"/v2/streams/"+live+"/heartbeat", string(body), nil)
	if status != 200 || !strings.Contains(string(raw), `"viewerCount":1`) {
		t.Fatalf("cross-instance heartbeat %d %s", status, raw)
	}
	requireRequestErrors(t, first.URL, live, string(body))
	status, raw, _ = do(t, http.MethodPost, second.URL+"/v2/streams/"+live+"/heartbeat", `{"sessionId":"`+session.SessionID+`","token":"forged.token"}`, nil)
	requireError(t, status, raw, 401, "AUTH_EXPIRED")
}

// TestPlatformPrivateIngressAndGatewayLimits: the API is served only on the
// private mTLS listener and only to the pinned gateway client certificate;
// the plain listener serves health. Every gateway-proxied request arrives
// from the gateway's address and the gateway limits per client at its edge,
// so the per-peer bucket does not throttle all viewers together there.
func TestPlatformPrivateIngressAndGatewayLimits(t *testing.T) {
	db, _ := platformDatabase(t)
	s := platformAppServers(t, db)
	if status, raw, _ := do(t, http.MethodGet, s.plain.URL+"/health/live", "", nil); status != 200 {
		t.Fatalf("plain health %d %s", status, raw)
	}
	if status, raw, _ := do(t, http.MethodGet, s.plain.URL+"/v2/streams/live", "", nil); status != 403 || !strings.Contains(string(raw), "TRUSTED_INGRESS_REQUIRED") {
		t.Fatalf("plain listener served the API: %d %s", status, raw)
	}
	res, err := s.pki.client(s.pki.otherService).Get(s.private.URL + "/v2/streams/live")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("non-gateway peer reached the API: %d", res.StatusCode)
	}
	if _, err = s.pki.client(testpki.Foreign(t, "gateway-client", "gateway.railway.internal")).Get(s.private.URL + "/v2/streams/live"); err == nil {
		t.Fatal("client certificate of a foreign CA accepted")
	}
	if _, err = (&http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: s.pki.ca.Roots()}}}).Get(s.private.URL + "/v2/streams/live"); err == nil {
		t.Fatal("client without a certificate accepted")
	}
	for i := 0; i < rateLimitMax+5; i++ {
		if status, raw, _ := do(t, http.MethodGet, s.private.URL+"/v2/streams/live", "", map[string]string{"X-Real-IP": "203.0.113.7"}); status != 200 {
			t.Fatalf("gateway ingress request %d: %d %s", i, status, raw)
		}
	}
}

func TestPlatformRuntimeRefusesMisconfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://fixture:fixture@127.0.0.1:1/fixture")
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1")
	t.Setenv("LIVEPEER_API_KEY", "fixture")
	t.Setenv("LIVEPEER_WEBHOOK_SECRET", strings.Repeat("w", 32))
	for name, c := range map[string]struct {
		key, want string
		mutate    func(*config.Config)
	}{
		"short viewer key":   {"short", "LIVESTREAM_VIEWER_TOKEN_KEY", func(*config.Config) {}},
		"provider available": {platformViewerKey, "LIVEPEER_PROVIDER_MODE=unavailable", func(c *config.Config) { c.ProviderMode = "available" }},
		"connected enabled":  {platformViewerKey, "LIVESTREAM_CONNECTED_ENABLED", func(c *config.Config) { c.ConnectedEnabled = true }},
		"no cors":            {platformViewerKey, "CORS_ALLOWED_ORIGINS", func(c *config.Config) { c.CORSAllowedOrigins = "" }},
		"no private listen":  {platformViewerKey, "LIVESTREAM_PRIVATE_LISTEN_ADDRESS", func(c *config.Config) { c.PrivateListenAddress = "" }},
		"listen on port":     {platformViewerKey, "LIVESTREAM_PRIVATE_LISTEN_ADDRESS", func(c *config.Config) { c.PrivateListenAddress = "0.0.0.0:8080" }},
		"no server cert":     {platformViewerKey, "LIVESTREAM_TLS_CERT_FILE", func(c *config.Config) { c.PrivateTLSCertFile = "" }},
		"two server keys":    {platformViewerKey, "LIVESTREAM_TLS_KEY_PEM", func(c *config.Config) { c.PrivateTLSKeyPEM = "pem" }},
		"no gateway pin":     {platformViewerKey, "GATEWAY_PEER_CERT_SHA256", func(c *config.Config) { c.GatewayPeerCertSHA256 = strings.ToUpper(strings.Repeat("ab", 32)) }},
		"no expected schema": {platformViewerKey, "EXPECTED_SCHEMA_FILE", func(c *config.Config) { c.ExpectedSchemaFile = "" }},
		"bad schema digest":  {platformViewerKey, "EXPECTED_SCHEMA_SHA256", func(c *config.Config) { c.ExpectedSchemaSHA256 = strings.ToUpper(strings.Repeat("cd", 32)) }},
	} {
		t.Setenv("LIVESTREAM_VIEWER_TOKEN_KEY", c.key)
		cfg := platformConfig()
		c.mutate(&cfg)
		if _, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// TestPlatformBootVerifiesCoordinatorSchemaWithoutMigrating: the platform
// runtime never migrates. It boots as livestream_runtime on the
// coordinator-owned schema only when the coordinator's ExpectedSchema
// document holds exactly (applied migrations, catalog fingerprints, recovery
// epoch and the reviewed privileges, no more and no less), and refuses an
// unmigrated database even when its credential could have migrated it.
func TestPlatformBootVerifiesCoordinatorSchemaWithoutMigrating(t *testing.T) {
	db, admin := platformDatabase(t)
	s := platformAppServers(t, db)
	if status, raw, _ := do(t, http.MethodGet, s.plain.URL+"/health/ready", "", nil); status != 200 {
		t.Fatalf("readiness %d %s", status, raw)
	}
	ctx := context.Background()
	var goose int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relname='goose_db_version'`).Scan(&goose); err != nil || goose != 0 {
		t.Fatalf("platform boot ran goose: %d tables (%v)", goose, err)
	}

	boot := func(dsn string, mutate func(*config.Config)) error {
		t.Helper()
		pki := newPlatformPKI(t)
		t.Setenv("DATABASE_URL", dsn)
		t.Setenv("REDIS_URL", os.Getenv("L40_TEST_REDIS_URL"))
		t.Setenv("LIVESTREAM_VIEWER_TOKEN_KEY", platformViewerKey)
		t.Setenv("LIVESTREAM_TLS_KEY_PEM", string(pki.server.KeyPEM))
		cfg := platformConfig()
		cfg.ExpectedSchemaFile, cfg.ExpectedSchemaSHA256 = db.ExpectedSchemaFile(t, "livestream_runtime")
		cfg.PrivateListenAddress = "127.0.0.1:0"
		cfg.PrivateTLSCertFile, cfg.PrivateTLSKeyFile, cfg.PrivateTLSKeyPEM, cfg.PrivateTLSClientCAFile = pki.server.CertFile, "", "unresolved", pki.ca.CAFile
		mutate(&cfg)
		a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil {
			a.Close()
		}
		return err
	}
	runtimeDSN := db.Runtimes["livestream_runtime"].DSN
	// A document that omits one reviewed privilege the role holds.
	var doc map[string]any
	if err := json.Unmarshal(db.Runtimes["livestream_runtime"].ExpectedSchema, &doc); err != nil {
		t.Fatal(err)
	}
	privileges := doc["privileges"].([]any)
	doc["privileges"] = privileges[:len(privileges)-1]
	narrowed, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	narrowedFile := t.TempDir() + "/narrowed.json"
	if err = os.WriteFile(narrowedFile, narrowed, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mutate func(*config.Config)
		want   string
	}{
		"digest mismatch": {func(c *config.Config) { c.ExpectedSchemaSHA256 = strings.Repeat("0", 64) }, "binding"},
		"undeclared privilege": {func(c *config.Config) {
			c.ExpectedSchemaFile, c.ExpectedSchemaSHA256 = narrowedFile, fmt.Sprintf("%x", sha256.Sum256(narrowed))
		}, "unexpected"},
	} {
		if err := boot(runtimeDSN, c.mutate); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	// An unmigrated database reached with the cluster administrator's
	// credential: refused, and still unmigrated.
	conn, err := pgx.Connect(ctx, os.Getenv("L40_TEST_APP_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	empty := "livestream_unmigrated_" + strings.ReplaceAll(uuid4(t), "-", "")[:12]
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{empty}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), os.Getenv("L40_TEST_APP_POSTGRES_DSN"))
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{empty}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	emptyURL, err := url.Parse(os.Getenv("L40_TEST_APP_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	emptyURL.Path = "/" + empty
	if err = boot(emptyURL.String(), func(*config.Config) {}); err == nil {
		t.Fatal("platform runtime booted on an unmigrated database")
	}
	check, err := pgx.Connect(ctx, emptyURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close(ctx)
	var relations int
	if err = check.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')`).Scan(&relations); err != nil || relations != 0 {
		t.Fatalf("platform boot created %d relations in an unmigrated database (%v)", relations, err)
	}
}

func TestPlatformPrivateFromInlinePEM(t *testing.T) {
	pki := testpki.New(t)
	server := pki.Leaf("livestream-server", "livestream.railway.internal")
	caPEM, err := os.ReadFile(pki.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{PrivateListenAddress: "[::]:8443", PrivateTLSCertPEM: string(server.CertPEM), PrivateTLSKeyPEM: string(server.KeyPEM), PrivateTLSClientCAPEM: string(caPEM), GatewayPeerCertSHA256: strings.Repeat("cd", 32)}
	private, err := newPlatformPrivate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(private.tls.Certificates) != 1 || private.tls.ClientCAs == nil || private.tls.ClientAuth != tls.RequireAndVerifyClientCert || private.tls.MinVersion != tls.VersionTLS13 {
		t.Fatalf("inline private TLS: %+v", private.tls)
	}
	if !private.tls.ClientCAs.Equal(pki.Roots()) {
		t.Fatal("inline client CA not the private CA")
	}
	cfg.PrivateTLSClientCAPEM = "not pem"
	if _, err = newPlatformPrivate(cfg); err == nil {
		t.Fatal("invalid inline client CA accepted")
	}
}
