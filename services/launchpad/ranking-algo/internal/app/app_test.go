package app_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
)

// VisibilityPeerEnv names a JSON document describing a reachable social
// ranking-visibility mTLS peer (see visibilityPeer). Ranking freezes every
// universe against that peer's moderation revision and fails closed without
// it, so publication and readiness are asserted only when it is provided.
const VisibilityPeerEnv = "RANKING_TEST_VISIBILITY_PEER_FILE"

const (
	gatewayDNS    = "gateway.railway.internal"
	rankingDNS    = "ranking.railway.internal"
	visibilityDNS = "social.railway.internal"
	sourceStream  = "chain125-canonical"
	projection    = "accounting-market-v1"
)

// visibilityPeer is the operator-supplied social peer: its endpoint, the DNS
// name and DER sha256 of its server certificate, the CA that issued it, and a
// client identity it admits for ranking.
type visibilityPeer struct {
	Endpoint       string `json:"endpoint"`
	DNS            string `json:"dns"`
	CertSHA256     string `json:"certSha256"`
	CAFile         string `json:"caFile"`
	ClientCertFile string `json:"clientCertFile"`
	ClientKeyFile  string `json:"clientKeyFile"`
}

// testPKI is an ephemeral CA and the leaf identities of the ranking mTLS
// perimeter, generated with crypto/x509 for this test only.
type testPKI struct {
	caKey  *ecdsa.PrivateKey
	caCert *x509.Certificate
	caPEM  []byte
}

type leaf struct {
	certPEM, keyPEM []byte
	der             []byte
}

func (l leaf) pin() string { return fmt.Sprintf("%x", sha256.Sum256(l.der)) }

func (l leaf) pair(t *testing.T) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair(l.certPEM, l.keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func newPKI(t *testing.T) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t), Subject: pkix.Name{CommonName: "ranking test CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testPKI{caKey: key, caCert: cert, caPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (p *testPKI) issue(t *testing.T, dns string, usage x509.ExtKeyUsage) leaf {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t), Subject: pkix.Name{CommonName: dns}, DNSNames: []string{dns},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return leaf{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), der}
}

// writeBound writes a bound runtime document and returns its sha256.
func writeBound(t *testing.T, path string, raw []byte) string {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return production.Checksum(raw)
}

// connectionBudget is the fleet connection budget document in the shape the
// production renderer emits (one direct client per runtime role, a dedicated
// migration allocation), targeting the private test database. Only the
// rankingd client is opened; its user is the bootstrapped runtime role.
func connectionBudget(t *testing.T, db internaltest.RuntimeDatabase, maxConnections int64) []byte {
	t.Helper()
	clients := []production.ClientBudget{}
	for _, c := range []struct{ role, user, pool, class string }{
		{"mediagatewayd", "identity_runtime", "gateway_identity", "interactive"},
		{"userd", "users_runtime", "userd_primary", "interactive"},
		{"metadatad", "metadata_runtime", "metadata_primary", "interactive"},
		{"sociald", "chat_runtime", "social_primary", "interactive"},
		{"livestreamd", "livestream_runtime", "livestream_primary", "interactive"},
		{"pnld", "pnl_runtime", "pnl_batch", "batch"},
		{"apid:rest", "api_rest_runtime", "api_rest", "interactive"},
		{"apid:sockets", "api_sockets_runtime", "api_sockets", "interactive"},
		{"statsd", "stats_runtime", "stats_batch", "batch"},
		{"chartsd", "candles_runtime", "charts_batch", "batch"},
		{"rankingd", db.RuntimeRole, "ranking_batch", "batch"},
		{"indexerd", "indexer_runtime", "indexer_primary", "batch"},
	} {
		clients = append(clients, production.ClientBudget{Role: c.role, PoolID: c.pool, Target: "fixture_pg", Transport: shareddb.TransportDirect, Alias: db.Database, User: c.user, QueryClass: shareddb.QueryClass(c.class), Processes: production.LiveProcesses{Replicas: 1, Surge: 1, Maximum: 2}, MaxConnections: 3})
	}
	budget := production.ConnectionBudget{
		Identity: "ranking_app_test",
		Targets:  []production.PostgresTarget{{ID: "fixture_pg", Member: "fixture_pg_primary", Host: db.Host, Port: db.Port, MaxConnections: maxConnections}},
		Poolers:  []production.PoolerBudget{}, Clients: clients,
		Operational: []production.OperationalBudget{{ID: "migration", Target: "fixture_pg", Category: "migration", Connections: 2}},
	}
	raw, err := json.Marshal(budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = production.ParseConnectionBudget(raw); err != nil {
		t.Fatalf("connection budget: %v", err)
	}
	return raw
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func contractEnv(t *testing.T) {
	t.Helper()
	// Contract addresses must equal the reviewed chain-125 deployment manifest
	// (shared/config.BaseEnv.Validate rejects any other address).
	roles := deployment.Current().Roles
	for k, v := range map[string]string{
		"EVENT_EMITTER_ADDRESS":   roles["EventEmitter"].Address,
		"POOL_REGISTRY_ADDRESS":   roles["PoolRegistry"].Address,
		"ROUTER_ADDRESS":          roles["Router"].Address,
		"FACTORY_ADDRESS":         roles["SidioraFactory"].Address,
		"QUOTER_ADDRESS":          roles["Quoter"].Address,
		"PROTOCOL_CONFIG_ADDRESS": roles["ProtocolConfig"].Address,
		"FEE_ACCUMULATOR_ADDRESS": roles["FeeAccumulator"].Address,
		"SIDIORA_NFT_ADDRESS":     roles["SidioraNFT"].Address,
		"FEES_ROUTER_ADDRESS":     roles["FeesRouter"].Address,
		"POOL_BEACON_ADDRESS":     roles["PoolBeacon"].Address,
	} {
		t.Setenv(k, v)
	}
}

// TestNewServesAndRanks boots rankingd on its production native path
// (NODE_ENV=production, owned_local profile): the bound connection budget
// admits the pool, the bound ExpectedSchema is checked against the actual
// catalog as the least-privilege runtime role, the bound runtime document
// supplies the mTLS perimeter, and the HTTPS listener enforces the pinned
// gateway identity. Publication and readiness additionally need the social
// visibility peer named by VisibilityPeerEnv.
func TestNewServesAndRanks(t *testing.T) {
	db := internaltest.NewRankingRuntimeDatabase(t)
	redisURL := internaltest.NewRedisURL(t)
	ctx := context.Background()
	dir := t.TempDir()

	pki := newPKI(t)
	server := pki.issue(t, rankingDNS, x509.ExtKeyUsageServerAuth)
	client := pki.issue(t, rankingDNS, x509.ExtKeyUsageClientAuth)
	gateway := pki.issue(t, gatewayDNS, x509.ExtKeyUsageClientAuth)
	observer := pki.issue(t, "observer.railway.internal", x509.ExtKeyUsageClientAuth)
	social := pki.issue(t, visibilityDNS, x509.ExtKeyUsageServerAuth)

	caBundle := pki.caPEM
	clientCert, clientKey := client.certPEM, client.keyPEM
	// No peer: an https endpoint on a closed loopback port, pinned to an
	// ephemeral social certificate nothing serves.
	endpoint := fmt.Sprintf("https://127.0.0.1:%d/internal/v2/social/ranking-visibility", freePort(t))
	peerDNS, peerPin := visibilityDNS, social.pin()
	peerFile := os.Getenv(VisibilityPeerEnv)
	if peerFile != "" {
		raw, err := os.ReadFile(peerFile)
		if err != nil {
			t.Fatal(err)
		}
		var peer visibilityPeer
		if err = json.Unmarshal(raw, &peer); err != nil {
			t.Fatalf("%s: %v", VisibilityPeerEnv, err)
		}
		peerCA, err := os.ReadFile(peer.CAFile)
		if err != nil {
			t.Fatal(err)
		}
		if clientCert, err = os.ReadFile(peer.ClientCertFile); err != nil {
			t.Fatal(err)
		}
		if clientKey, err = os.ReadFile(peer.ClientKeyFile); err != nil {
			t.Fatal(err)
		}
		// One CA file serves both directions: the peer's CA verifies the
		// visibility server, the ephemeral CA verifies the gateway client.
		caBundle = append(append([]byte{}, pki.caPEM...), peerCA...)
		endpoint, peerDNS, peerPin = peer.Endpoint, peer.DNS, peer.CertSHA256
	}

	files := map[string][]byte{"ca.pem": caBundle, "client.pem": clientCert, "server.pem": server.certPEM}
	pins := map[string]string{}
	for name, raw := range files {
		pins[name] = writeBound(t, filepath.Join(dir, name), raw)
	}
	cursor := make([]byte, 32)
	if _, err := rand.Read(cursor); err != nil {
		t.Fatal(err)
	}
	binding := config.RankingRuntimeBinding{
		Version: 2, RankingSchema: "ranking", StatsSchema: "stats", SourceSchema: "indexer",
		SourceStream: sourceStream, ProjectionVersion: projection,
		CacheNamespace: "snapshots", CursorKeyReference: "RANKING_CURSOR_KEY",
		RecoverySchema: "recovery", RecoveryEpoch: db.Epoch, RecoveryRegion: db.Region,
		VisibilityEndpoint: endpoint, VisibilityDNS: peerDNS, VisibilityCertSHA256: peerPin,
		GatewayDNS: gatewayDNS, GatewayCertSHA256: gateway.pin(),
		CAFile: filepath.Join(dir, "ca.pem"), CASHA256: pins["ca.pem"],
		ClientCertFile: filepath.Join(dir, "client.pem"), ClientCertSHA256: pins["client.pem"], ClientKeyReference: "RANKING_CLIENT_KEY_PEM",
		ServerCertFile: filepath.Join(dir, "server.pem"), ServerCertSHA256: pins["server.pem"], ServerKeyReference: "RANKING_SERVER_KEY_PEM",
		MaxAttempts: 10, MaximumReads: 16,
	}
	runtimeDoc, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := pgx.Connect(ctx, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	var maxConnections int64
	if err = admin.QueryRow(ctx, "SELECT setting::bigint FROM pg_settings WHERE name='max_connections'").Scan(&maxConnections); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	contractEnv(t)
	for k, v := range map[string]string{
		"NODE_ENV":                     "production",
		"RUNTIME_PROFILE":              "owned_local",
		"BIND_ADDRESS":                 "127.0.0.1",
		"PORT":                         strconv.Itoa(port),
		"ZONE":                         "zone-a",
		"METRICS_ENVIRONMENT":          "production",
		"LOG_LEVEL":                    "error",
		"SECRETS_PROVIDER":             "env",
		"SECRETS_REFRESH_SECONDS":      "30",
		"DATABASE_URL":                 db.RuntimeDSN,
		"REDIS_URL":                    redisURL,
		"REDIS_BULL_URL":               redisURL,
		"RPC_URL":                      "https://rpc.example.com/rpc",
		"DB_BUDGET_FILE":               filepath.Join(dir, "db-budget.json"),
		"DB_BUDGET_SHA256":             writeBound(t, filepath.Join(dir, "db-budget.json"), connectionBudget(t, db, maxConnections)),
		"DB_POOL_ID":                   "ranking_batch",
		"INSTANCE_ID":                  "ranking-test-1",
		"EXPECTED_SCHEMA_FILE":         filepath.Join(dir, "expected-schema.json"),
		"EXPECTED_SCHEMA_SHA256":       writeBound(t, filepath.Join(dir, "expected-schema.json"), db.ExpectedSchema),
		"RANKING_RUNTIME_FILE":         filepath.Join(dir, "ranking-runtime.json"),
		"RANKING_RUNTIME_SHA256":       writeBound(t, filepath.Join(dir, "ranking-runtime.json"), runtimeDoc),
		"RANKING_TICK_INTERVAL_MS":     "30000",
		"RANKING_NEW_TICK_INTERVAL_MS": "10000",
		"RANKING_CLIENT_KEY_PEM":       string(clientKey),
		"RANKING_SERVER_KEY_PEM":       string(server.keyPEM),
		"RANKING_CURSOR_KEY":           base64.RawURLEncoding.EncodeToString(cursor),
	} {
		t.Setenv(k, v)
	}

	// The bound documents are refused when their pins differ.
	t.Run("bound documents", func(t *testing.T) {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		cfg.ExpectedSchemaSHA256 = strings.Repeat("0", 64)
		if _, err = app.New(ctx, cfg, quietLogger()); err == nil {
			t.Fatal("expected-schema pin mismatch admitted")
		}
		cfg, _ = config.Load()
		cfg.RankingRuntimeSHA256 = strings.Repeat("0", 64)
		if _, err = app.New(ctx, cfg, quietLogger()); err == nil {
			t.Fatal("runtime document pin mismatch admitted")
		}
	})

	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()
	stopped := false
	shutdown := func() error {
		if stopped {
			return nil
		}
		stopped = true
		stop()
		select {
		case err := <-done:
			return err
		case <-time.After(45 * time.Second):
			return errors.New("rankingd did not shut down")
		}
	}
	defer func() { _ = shutdown() }()

	roots := x509.NewCertPool()
	roots.AddCert(pki.caCert)
	base := fmt.Sprintf("https://127.0.0.1:%d", port)
	httpsClient := func(identity *leaf) *http.Client {
		cfg := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: rankingDNS}
		if identity != nil {
			cfg.Certificates = []tls.Certificate{identity.pair(t)}
		}
		return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}
	}
	gatewayClient, observerClient := httpsClient(&gateway), httpsClient(&observer)
	get := func(c *http.Client, path string) (int, map[string]any) {
		t.Helper()
		resp, err := c.Get(base + path) //nolint:noctx // bounded client timeout
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		return resp.StatusCode, body
	}

	// The native listener comes up (New succeeded: budget-admitted pool,
	// runtime-role schema readiness, secrets, mTLS material, epoch guard).
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case err := <-done:
			stopped = true
			t.Fatalf("rankingd exited during startup: %v", err)
		default:
		}
		resp, err := observerClient.Get(base + "/health/live") //nolint:noctx // bounded client timeout
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rankingd listener not live: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// mTLS is mandatory: no client certificate, no connection.
	if resp, err := httpsClient(nil).Get(base + "/health/live"); err == nil { //nolint:noctx // bounded client timeout
		resp.Body.Close()
		t.Fatal("TLS handshake without a client certificate succeeded")
	}
	// A CA-issued identity that is not the pinned gateway is refused.
	if code, body := get(observerClient, "/rankings/trending"); code != http.StatusForbidden || body["code"] != "TRUSTED_INGRESS_REQUIRED" {
		t.Fatalf("non-gateway ingress = %d %v", code, body)
	}
	// Through the pinned gateway the snapshot API is reached.
	if code, body := get(gatewayClient, "/rankings/not-a-category"); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body), "INVALID_CATEGORY") {
		t.Fatalf("invalid category = %d %v", code, body)
	}
	if code, _ := get(observerClient, "/metrics"); code != http.StatusOK {
		t.Fatalf("metrics status = %d", code)
	}

	if peerFile == "" {
		// Fail closed: without a moderation revision no snapshot is published
		// or served, and readiness reports unavailable.
		if code, body := get(gatewayClient, "/rankings/trending"); code != http.StatusServiceUnavailable || !strings.Contains(fmt.Sprint(body), "RANKING_UNAVAILABLE") {
			t.Fatalf("trending without visibility = %d %v", code, body)
		}
		if code, _ := get(observerClient, "/health/ready"); code != http.StatusServiceUnavailable {
			t.Fatalf("health/ready without visibility = %d, want 503", code)
		}
	}

	t.Run("publishes and becomes ready", func(t *testing.T) {
		if peerFile == "" {
			t.Skipf("%s not set: snapshot publication and readiness need the social ranking-visibility mTLS peer", VisibilityPeerEnv)
		}
		// Seed a complete canonical source (lease, watermark, completed block,
		// one canonical creation) and wait for every category to publish and
		// the instance to report ready.
		now := time.Now().UTC()
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,$1,'indexer-1',1,clock_timestamp()+interval '10 minutes')`, []any{sourceStream}},
			{`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,200,200)`, []any{sourceStream}},
			{`INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete,completed_at) VALUES(125,$1,200,$2,0,true,true,$3)`, []any{sourceStream, "0x" + strings.Repeat("c8", 32), now}},
			{`INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$3,$3,1,150,$4,$5,0,0,$6,'{}','{}')`, []any{"0x" + strings.Repeat("7", 40), "0x" + strings.Repeat("a", 40), "0x" + strings.Repeat("1", 40), "0x" + strings.Repeat("96", 32), "0x" + hex.EncodeToString(make([]byte, 32)), now.Unix() - 600}},
		} {
			if _, err := admin.Exec(ctx, step.sql, step.args...); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		deadline := time.Now().Add(90 * time.Second)
		for {
			code, _ := get(observerClient, "/health/ready")
			if code == http.StatusOK {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("health/ready = %d after publication window", code)
			}
			time.Sleep(time.Second)
		}
		for _, category := range []string{"trending", "breakout", "volume", "movers", "unusual", "new"} {
			if code, body := get(gatewayClient, "/rankings/"+category); code != http.StatusOK {
				t.Fatalf("%s = %d %v", category, code, body)
			}
		}
	})

	if err := shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestNewFailsOnBadRedisURL(t *testing.T) {
	dsn, _ := internaltest.NewPostgresWithDSN(t)
	contractEnv(t)
	for k, v := range map[string]string{
		"DATABASE_URL":   dsn,
		"REDIS_URL":      "not-a-valid-redis-url",
		"REDIS_BULL_URL": "redis://localhost:6379",
		"RPC_URL":        "https://rpc.example.com/rpc",
		"ZONE":           "zone-a",
	} {
		t.Setenv(k, v)
	}
	// The shared base environment rejects a malformed REDIS_URL at startup
	// validation, before app.New could ever dial it.
	cfg, err := config.Load()
	if err != nil {
		if !strings.Contains(err.Error(), "REDIS_URL: url_invalid") {
			t.Fatalf("config.Load error = %v, want REDIS_URL: url_invalid", err)
		}
		return
	}
	if _, err := app.New(context.Background(), cfg, quietLogger()); err == nil {
		t.Fatal("app.New: expected error for invalid REDIS_URL")
	}
}
