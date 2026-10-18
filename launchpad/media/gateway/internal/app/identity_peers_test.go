package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/devidentity"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/proxy"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
)

// The gateway's private identity listener, end to end, with every platform
// peer of the production deployment:
//
//   - the identity authority is the real auth.Identity (the development
//     runtime's configuration source over a throwaway database, a chain-125
//     node for wallet proofs), mounted with the production identity routes and
//     the production verifiers built from an identityPeers document listing
//     user, metadata, social, livestream, pnld, apid, stats and ranking;
//   - it is served by the production private handler on a TLS 1.3 listener
//     requiring a client certificate of a throwaway private CA;
//   - media/social's platform runtime runs as its real binary (sociald,
//     NODE_ENV=production RUNTIME_PROFILE=platform) with the mTLS identity
//     client and its private mTLS listener, reached through the gateway's
//     production social proxy (proxy.Connected) and socket proxy
//     (proxy.ConnectedWS) exactly as the connected gateway runtime wires them;
//   - core/stats-workers' and media/livestream's identity clients (the shared
//     sharedauth.NewMTLSIdentityClient with issuers "stats" and "livestream")
//     authenticate a browser session through the same listener.
//
// Container-free: Postgres is the fixture server (throwaway databases created
// and dropped here), Redis the fixture server's database 12.

const e2eOrigin = "https://kindle.test"

type e2ePeer struct {
	issuer string
	leaf   testpki.Leaf
	key    testpki.AssertionKey
}

type identityE2E struct {
	t                   *testing.T
	pki                 *testpki.PKI
	gatewayServer       testpki.Leaf
	gatewayClient       testpki.Leaf
	gatewayKey          testpki.AssertionKey
	peers               map[string]e2ePeer
	public              *httptest.Server
	private             *httptest.Server
	identityURL         string
	serviceToken        string
	adminDSN            string
	databases           []string
	authorityForGateway sharedauth.MTLSIdentityAuthority
}

func e2eFixture(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// throwawayDatabase creates a database on the fixture server and drops it at
// cleanup.
func (e *identityE2E) throwawayDatabase(prefix string) string {
	t := e.t
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, e.adminDSN)
	if err != nil {
		t.Fatalf("fixture postgres: %v", err)
	}
	defer admin.Close(ctx)
	name := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), e.adminDSN)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	pc, err := pgx.ParseConfig(e.adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", pc.User, pc.Password, pc.Host, pc.Port, name)
}

// e2eChain is a chain-125 go-ethereum node with HTTP JSON-RPC, so the
// identity's wallet-proof verifier runs its real RPC calls.
func e2eChain(t *testing.T) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(125)
	backend := simulated.NewBackend(types.GenesisAlloc{}, func(nodeConf *node.Config, ethConf *ethconfig.Config) {
		nodeConf.HTTPHost = "127.0.0.1"
		nodeConf.HTTPPort = port
		nodeConf.HTTPModules = []string{"eth", "net", "web3"}
		ethConf.Genesis.Config = &chainConfig
		ethConf.NetworkId = 125
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	rpc := fmt.Sprintf("http://127.0.0.1:%d", port)
	client, err := ethclient.Dial(rpc)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if id, err := client.ChainID(context.Background()); err != nil || id.Cmp(big.NewInt(125)) != 0 {
		t.Fatalf("chain-125 node: %v %v", id, err)
	}
	return rpc
}

func newIdentityE2E(t *testing.T) *identityE2E {
	t.Helper()
	e := &identityE2E{t: t, adminDSN: e2eFixture("L33_POSTGRES_DSN", "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"), peers: map[string]e2ePeer{}}
	e.pki = testpki.New(t)
	e.gatewayServer = e.pki.Leaf("gateway-server", "gateway.railway.internal")
	e.gatewayClient = e.pki.Leaf("gateway-client", "gateway.railway.internal")
	e.gatewayKey = testpki.Ed25519(t)
	// The identityPeers document of the production deployment.
	var document []gatewayIdentityPeer
	for _, issuer := range []string{"user", "metadata", "social", "livestream", "pnld", "apid", "stats", "ranking"} {
		peer := e2ePeer{issuer: issuer, leaf: e.pki.Leaf(issuer+"-client", issuer+".railway.internal"), key: testpki.Ed25519(t)}
		e.peers[issuer] = peer
		document = append(document, gatewayIdentityPeer{Issuer: issuer, PeerCertSHA256: peer.leaf.SHA256, PublicKey: peer.key.PublicRawURL})
	}
	verifiers, err := identityServiceVerifiers(document, e.gatewayClient.SHA256, e.gatewayKey.Private.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("identityPeers rejected: %v", err)
	}

	secret := strings.Repeat("a1", 32)
	t.Setenv(devidentity.SigningSecretReference, secret)
	e.serviceToken = strings.Repeat("s7", 32)
	identity, err := devidentity.Open(context.Background(), devidentity.Options{DatabaseURL: e.throwawayDatabase("gw_identity_e2e_"), RPCURL: e2eChain(t), PublicURL: e2eOrigin, CORSAllowedOrigins: e2eOrigin, SigningSecret: secret, LimiterKey: strings.Repeat("l", 32), ServiceToken: e.serviceToken})
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	t.Cleanup(identity.Close)
	mux := chi.NewRouter()
	// The production registration: the verifiers are configured, so the
	// IDENTITY_SERVICE_TOKEN bearer is never accepted by the internal routes.
	if err = identity.Identity.RegisterV2Routes(mux, auth.IdentityHTTPConfig{ServiceToken: e.serviceToken, ServiceVerifiers: verifiers}); err != nil {
		t.Fatal(err)
	}
	e.public = httptest.NewServer(publicGatewayHandler(mux, config.Config{NodeEnv: "production", CORSAllowedOrigins: e2eOrigin}))
	t.Cleanup(e.public.Close)
	e.private = httptest.NewUnstartedServer(privateIdentityHandler(mux))
	e.private.TLS = privateListenerTLS(e.pki.Roots(), e.gatewayServer.Pair)
	e.private.StartTLS()
	t.Cleanup(e.private.Close)
	e.identityURL = e.private.URL

	// The gateway's own identity client (issuer "gateway"), as
	// newGatewayConnectedRuntime builds it.
	transport := &http.Transport{TLSClientConfig: e.gatewayClientTLS(), Proxy: nil, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 2 * time.Second}
	e.authorityForGateway, err = sharedauth.NewMTLSIdentityAuthority(e.identityURL, e.gatewayServer.SHA256, &http.Client{Transport: transport, Timeout: 2 * time.Second}, sharedauth.ServiceRequestIssuer{Issuer: "gateway", Audience: "identity", Key: e.gatewayKey.Private, Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *identityE2E) gatewayClientTLS() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{e.gatewayClient.Pair}}
}

// peerClient is the shared identity client a platform peer builds from its
// IDENTITY_MTLS_* / IDENTITY_ASSERTION_SIGNING_KEY environment.
func (e *identityE2E) peerClient(issuer string, leaf testpki.Leaf, key testpki.AssertionKey) (sharedauth.MTLSIdentityAuthority, error) {
	return sharedauth.NewMTLSIdentityClient(sharedauth.MTLSIdentityClientConfig{Endpoint: e.identityURL, PeerCertSHA256: e.gatewayServer.SHA256, CAFile: e.pki.CAFile, ClientCertFile: leaf.CertFile, ClientKeyFile: leaf.KeyFile, Issuer: issuer, SigningKey: key.StdBase64})
}

type e2eSession struct {
	wallet, token, csrf, cookie string
}

func e2eSend(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// signIn runs the browser sign-in (challenge, personal_sign, session) on the
// public identity surface.
func (e *identityE2E) signIn() e2eSession {
	t := e.t
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	resp, raw := e2eSend(t, nil, http.MethodPost, e.public.URL+"/v2/auth/challenges", map[string]string{"wallet": wallet, "origin": e2eOrigin}, map[string]string{"Origin": e2eOrigin, "Idempotency-Key": uuid.NewString()})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("challenge %d: %s", resp.StatusCode, raw)
	}
	var challenge struct {
		Data struct {
			Challenge struct{ ChallengeID, Message string }
		}
	}
	if err = json.Unmarshal(raw, &challenge); err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(challenge.Data.Challenge.Message)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	resp, raw = e2eSend(t, nil, http.MethodPost, e.public.URL+"/v2/auth/sessions", map[string]any{"challengeId": challenge.Data.Challenge.ChallengeID, "proof": map[string]string{"kind": "eoa_eip191", "wallet": wallet, "signature": "0x" + hex.EncodeToString(sig)}, "deviceLabel": "Web browser"}, map[string]string{"Origin": e2eOrigin})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session %d: %s", resp.StatusCode, raw)
	}
	var login struct {
		Data struct{ AccessToken string }
	}
	if err = json.Unmarshal(raw, &login); err != nil || login.Data.AccessToken == "" {
		t.Fatalf("login %s", raw)
	}
	s := e2eSession{wallet: wallet, token: login.Data.AccessToken, csrf: resp.Header.Get("X-CSRF-Token")}
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-kindle_access" {
			s.cookie = c.Value
		}
	}
	if len(s.csrf) != 64 || s.cookie == "" {
		t.Fatalf("session cookie/csrf missing: %v", resp.Header)
	}
	return s
}

// chatTicket issues a single-use chat stream ticket on the public surface.
func (e *identityE2E) chatTicket(s e2eSession, topics []string) string {
	t := e.t
	t.Helper()
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: e2eOrigin, Endpoint: "/v2/stream"}
	resp, raw := e2eSend(t, nil, http.MethodPost, e.public.URL+"/v2/stream/tickets", binding, map[string]string{"Origin": e2eOrigin, "Authorization": "Bearer " + s.token})
	if resp.StatusCode/100 != 2 {
		t.Fatalf("ticket %d: %s", resp.StatusCode, raw)
	}
	var envelope struct {
		Ticket string `json:"ticket"`
		Data   struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Ticket != "" {
		return envelope.Data.Ticket
	}
	if envelope.Ticket == "" {
		t.Fatalf("ticket response %s", raw)
	}
	return envelope.Ticket
}

func bearerRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://stats.railway.internal/v2/search/expanded?q=coin&type=pool", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// lockedBuffer collects a subprocess's output.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type socialProcess struct {
	plainURL, privateURL string
	server, ranking      testpki.Leaf
	pool, creator        string
}

// startSocial builds media/social's sociald and runs its production platform
// runtime against the identity listener.
func (e *identityE2E) startSocial() socialProcess {
	t := e.t
	t.Helper()
	binary := buildService(t, "social", "sociald")
	dsn := e.throwawayDatabase("social_platform_e2e_")
	ctx := context.Background()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	sp := socialProcess{server: e.pki.Leaf("social-server", "social.railway.internal"), ranking: e.peers["ranking"].leaf, pool: "0x" + strings.Repeat("9a", 20), creator: "0x" + strings.Repeat("c0", 20)}
	if _, err = db.Exec(ctx, `CREATE SCHEMA indexer;
CREATE TABLE "indexer"."pools" ("pool_address" varchar(42) PRIMARY KEY NOT NULL,"token_address" varchar(42) NOT NULL,"creator" varchar(42) NOT NULL,"optical" varchar(42) NOT NULL,"pool_id" varchar(66) NOT NULL,"nft_id" bigint,"created_at" bigint NOT NULL,"created_block" bigint NOT NULL,"tx_hash" varchar(66) NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO indexer.pools VALUES($1,$2,$3,$4,$5,1,$6,100,$7)`, sp.pool, "0x"+strings.Repeat("70", 20), sp.creator, "0x"+strings.Repeat("0b", 20), "0x"+strings.Repeat("ab", 32), time.Now().Unix(), "0x"+strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	cursor := make([]byte, 32)
	_, _ = rand.Read(cursor)
	plainPort, privatePort := freePort(t), freePort(t)
	social := e.peers["social"]
	env := []string{
		"NODE_ENV=production", "RUNTIME_PROFILE=platform", "ZONE=zone-a", "METRICS_ENVIRONMENT=production", "LOG_LEVEL=error",
		"SECRETS_PROVIDER=env", "SECRETS_REFRESH_SECONDS=30", "DB_MAX_CONNS=5",
		fmt.Sprintf("PORT=%d", plainPort), "BIND_ADDRESS=0.0.0.0",
		"DATABASE_URL=" + dsn,
		"REDIS_URL=" + e2eFixture("GATEWAY_E2E_REDIS_URL", "redis://:kindle_fixture_password@127.0.0.1:26379/12"),
		"SOCIAL_CURSOR_KEY=" + base64.RawURLEncoding.EncodeToString(cursor),
		"CORS_ALLOWED_ORIGINS=" + e2eOrigin,
		"IDENTITY_SERVICE_URL=" + e.identityURL + "/",
		"IDENTITY_MTLS_CA_FILE=" + e.pki.CAFile,
		"IDENTITY_MTLS_CLIENT_CERT_FILE=" + social.leaf.CertFile,
		"IDENTITY_MTLS_CLIENT_KEY_PEM=" + string(social.leaf.KeyPEM),
		"IDENTITY_MTLS_PEER_CERT_SHA256=" + e.gatewayServer.SHA256,
		"IDENTITY_ASSERTION_SIGNING_KEY=" + social.key.StdBase64,
		fmt.Sprintf("SOCIAL_PRIVATE_LISTEN_ADDRESS=127.0.0.1:%d", privatePort),
		"SOCIAL_TLS_CERT_FILE=" + sp.server.CertFile,
		"SOCIAL_TLS_KEY_FILE=" + sp.server.KeyFile,
		"SOCIAL_TLS_CLIENT_CA_FILE=" + e.pki.CAFile,
		"GATEWAY_PEER_CERT_SHA256=" + e.gatewayClient.SHA256,
		"SOCIAL_AUTHORITY_PEERS=ranking.railway.internal=" + sp.ranking.SHA256,
	}
	sp.plainURL = fmt.Sprintf("http://127.0.0.1:%d", plainPort)
	sp.privateURL = fmt.Sprintf("https://127.0.0.1:%d", privatePort)
	// Readiness includes the identity authority probe: social's client
	// certificate, the pinned gateway server certificate and its request
	// assertion, all verified by the gateway's production verifiers.
	runService(t, "sociald", binary, env, sp.plainURL)
	return sp
}

// buildService builds media/<module>/cmd/<command> into a test directory.
func buildService(t *testing.T, module, command string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	binary := filepath.Join(t.TempDir(), command)
	build := exec.Command("go", "build", "-o", binary, "./cmd/"+command)
	build.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..", module)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", command, err, out)
	}
	return binary
}

// runService starts binary with exactly env, stops it (SIGINT) at cleanup
// and waits until plainURL/health/ready answers 200.
func runService(t *testing.T, name, binary string, env []string, plainURL string) {
	t.Helper()
	output := &lockedBuffer{}
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, binary)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		<-exited
		if t.Failed() {
			t.Logf("%s output:\n%s", name, output.String())
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case err := <-exited:
			t.Fatalf("%s exited: %v\n%s", name, err, output.String())
		default:
		}
		res, err := http.Get(plainURL + "/health/ready")
		if err == nil {
			raw, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s not ready: %d %s\n%s", name, res.StatusCode, raw, output.String())
			}
		} else if time.Now().After(deadline) {
			t.Fatalf("%s unreachable: %v\n%s", name, err, output.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type livestreamProcess struct {
	plainURL, privateURL, dsn string
}

// startLivestream runs media/livestream's livestreamd in the production
// platform runtime with its private mTLS listener.
func (e *identityE2E) startLivestream() livestreamProcess {
	t := e.t
	t.Helper()
	binary := buildService(t, "livestream", "livestreamd")
	dsn := e.throwawayDatabase("livestream_platform_e2e_")
	ctx := context.Background()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	// media/social owns chat.safety_bans; the platform runtime reads it.
	if _, err = db.Exec(ctx, `CREATE SCHEMA chat; CREATE TABLE chat.safety_bans(wallet text NOT NULL,revoked_at timestamptz,expires_at timestamptz,scope text NOT NULL,resource text)`); err != nil {
		t.Fatal(err)
	}
	server := e.pki.Leaf("livestream-server", "livestream.railway.internal")
	plainPort, privatePort := freePort(t), freePort(t)
	lp := livestreamProcess{plainURL: fmt.Sprintf("http://127.0.0.1:%d", plainPort), privateURL: fmt.Sprintf("https://127.0.0.1:%d", privatePort), dsn: dsn}
	env := []string{
		"NODE_ENV=production", "RUNTIME_PROFILE=platform", "ZONE=zone-a", "METRICS_ENVIRONMENT=production", "LOG_LEVEL=error",
		"SECRETS_PROVIDER=env", "SECRETS_REFRESH_SECONDS=30", "DB_MAX_CONNS=5",
		fmt.Sprintf("PORT=%d", plainPort), "BIND_ADDRESS=0.0.0.0",
		"DATABASE_URL=" + dsn,
		"REDIS_URL=" + e2eFixture("GATEWAY_E2E_REDIS_URL", "redis://:kindle_fixture_password@127.0.0.1:26379/12"),
		"LIVEPEER_PROVIDER_MODE=unavailable", "MAX_STREAMS_PER_WALLET=3", "MAX_GLOBAL_STREAMS=100",
		"LIVESTREAM_VIEWER_TOKEN_KEY=" + strings.Repeat("v7", 20),
		"CORS_ALLOWED_ORIGINS=" + e2eOrigin,
		fmt.Sprintf("LIVESTREAM_PRIVATE_LISTEN_ADDRESS=127.0.0.1:%d", privatePort),
		"LIVESTREAM_TLS_CERT_FILE=" + server.CertFile,
		"LIVESTREAM_TLS_KEY_PEM=" + string(server.KeyPEM),
		"LIVESTREAM_TLS_CLIENT_CA_FILE=" + e.pki.CAFile,
		"GATEWAY_PEER_CERT_SHA256=" + e.gatewayClient.SHA256,
	}
	runService(t, "livestreamd", binary, env, lp.plainURL)
	return lp
}

func e2eFrame(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) == nil && frame["type"] == want {
			return frame
		}
	}
}

func TestPlatformPeersThroughGatewayIdentityListener(t *testing.T) {
	e := newIdentityE2E(t)
	alice := e.signIn()
	bob := e.signIn()
	ctx := context.Background()

	t.Run("listener transport", func(t *testing.T) {
		// No client certificate, or one from another CA: no TLS session.
		noCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots()}}}
		if _, err := noCert.Post(e.identityURL+"/internal/v2/identity/session:check", "application/json", strings.NewReader("{}")); err == nil {
			t.Fatal("private listener served a client without a certificate")
		}
		foreign := testpki.Foreign(t, "stats-client", "stats.railway.internal")
		if client, err := e.peerClient("stats", foreign, e.peers["stats"].key); err == nil {
			if err = client.Probe(ctx); err == nil {
				t.Fatal("client certificate of a foreign CA accepted")
			}
		}
		// The service bearer is never an identity credential on the
		// private listener, even with a valid client certificate.
		bearerOnly := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{e.peers["stats"].leaf.Pair}}}}
		if resp, raw := e2eSend(t, bearerOnly, http.MethodPost, e.identityURL+"/internal/v2/identity/request:authenticate", sharedauth.IdentityAuthenticationRequest{Authorization: "Bearer " + alice.token, Method: "GET"}, map[string]string{"Authorization": "Bearer " + e.serviceToken, "X-Request-ID": uuid.NewString()}); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("IDENTITY_SERVICE_TOKEN bearer accepted: %d %s", resp.StatusCode, raw)
		}
		// Only /internal/v2/identity/* is served there.
		if resp, _ := e2eSend(t, bearerOnly, http.MethodGet, e.identityURL+"/health/live", nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("private listener served /health/live: %d", resp.StatusCode)
		}
		// The internal routes are not reachable on the public listener.
		if resp, _ := e2eSend(t, nil, http.MethodPost, e.public.URL+"/internal/v2/identity/session:check", map[string]string{}, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("public listener served the internal identity routes: %d", resp.StatusCode)
		}
	})

	t.Run("stats and livestream identity clients", func(t *testing.T) {
		for _, issuer := range []string{"stats", "livestream", "user", "metadata", "pnld", "apid"} {
			peer := e.peers[issuer]
			client, err := e.peerClient(issuer, peer.leaf, peer.key)
			if err != nil {
				t.Fatalf("%s client: %v", issuer, err)
			}
			if err = client.Probe(ctx); err != nil {
				t.Fatalf("%s probe: %v", issuer, err)
			}
			principal, err := client.AuthenticateRequest(ctx, bearerRequest(alice.token))
			if err != nil || principal.Wallet != alice.wallet {
				t.Fatalf("%s authenticate: %+v %v", issuer, principal, err)
			}
			if err = client.CheckStreamSession(ctx, principal.StreamPrincipal()); err != nil {
				t.Fatalf("%s session check: %v", issuer, err)
			}
			var authority *sharedauth.IdentityAuthorityError
			if _, err = client.AuthenticateRequest(ctx, bearerRequest(alice.token+"x")); !errors.As(err, &authority) || authority.Status != http.StatusUnauthorized {
				t.Fatalf("%s forged bearer: %v", issuer, err)
			}
		}
		// A registered certificate with another service's assertion key, an
		// issuer the gateway does not list, and a certificate it does not pin
		// are all refused before any session is looked at.
		stats := e.peers["stats"]
		unlisted := e.pki.Leaf("unlisted-client", "unlisted.railway.internal")
		for name, attempt := range map[string]func() (sharedauth.MTLSIdentityAuthority, error){
			"wrong key": func() (sharedauth.MTLSIdentityAuthority, error) {
				return e.peerClient("stats", stats.leaf, e.peers["social"].key)
			},
			"wrong cert": func() (sharedauth.MTLSIdentityAuthority, error) {
				return e.peerClient("stats", e.peers["social"].leaf, stats.key)
			},
			"unlisted": func() (sharedauth.MTLSIdentityAuthority, error) {
				return e.peerClient("unlisted", unlisted, testpki.Ed25519(t))
			},
			"wrong pin": func() (sharedauth.MTLSIdentityAuthority, error) {
				return sharedauth.NewMTLSIdentityClient(sharedauth.MTLSIdentityClientConfig{Endpoint: e.identityURL, PeerCertSHA256: e.gatewayClient.SHA256, CAFile: e.pki.CAFile, ClientCertFile: stats.leaf.CertFile, ClientKeyFile: stats.leaf.KeyFile, Issuer: "stats", SigningKey: stats.key.StdBase64})
			},
			"bad issuer": func() (sharedauth.MTLSIdentityAuthority, error) {
				return e.peerClient("ranking", stats.leaf, stats.key)
			},
			"gateway key": func() (sharedauth.MTLSIdentityAuthority, error) {
				return e.peerClient("stats", stats.leaf, e.gatewayKey)
			},
		} {
			client, err := attempt()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err = client.Probe(ctx); err == nil {
				t.Fatalf("%s: probe accepted", name)
			}
			if _, err = client.AuthenticateRequest(ctx, bearerRequest(alice.token)); err == nil {
				t.Fatalf("%s: session verified", name)
			}
		}
	})

	sp := e.startSocial()
	clientTLS := e.gatewayClientTLS()
	routes := append(proxy.SocialConnectedRoutes(),
		proxy.ConnectedRoute{Method: http.MethodPost, Template: "/v2/callouts", Authenticate: true},
		proxy.ConnectedRoute{Method: http.MethodGet, Template: "/v2/pools/{poolAddress}/comments"},
		proxy.ConnectedRoute{Method: http.MethodPost, Template: "/v2/pools/{poolAddress}/comments", Authenticate: true},
	)
	rest, err := proxy.NewConnected(proxy.ConnectedDeps{Name: "social", URL: sp.privateURL, Routes: routes, Identity: e.authorityForGateway, SigningKey: e.gatewayKey.Private, TLS: clientTLS})
	if err != nil {
		t.Fatal(err)
	}
	socket, err := proxy.NewConnectedWS(proxy.ConnectedWSDeps{URL: sp.privateURL, Identity: e.authorityForGateway, SigningKey: e.gatewayKey.Private, TLS: clientTLS, Origins: []string{e2eOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	edgeMux := http.NewServeMux()
	edgeMux.Handle("/v2/stream", socket)
	edgeMux.Handle("/", rest)
	edge := httptest.NewServer(edgeMux)
	t.Cleanup(edge.Close)

	t.Run("social REST through the gateway proxy", func(t *testing.T) {
		// Bearer: the gateway authenticates the session, forwards the
		// credential and Origin over mTLS, and social re-verifies it at the
		// identity listener as issuer "social".
		callout := map[string]any{"poolAddress": sp.pool, "body": "accumulating here", "direction": "bullish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}
		resp, raw := e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/callouts", callout, map[string]string{"Origin": e2eOrigin, "Authorization": "Bearer " + alice.token, "X-Actor-Wallet": bob.wallet})
		if resp.StatusCode != http.StatusCreated || !strings.Contains(string(raw), `"authorWallet":"`+alice.wallet+`"`) {
			t.Fatalf("callout through the gateway: %d %s", resp.StatusCode, raw)
		}
		// Cookie session: __Host-kindle_access, X-CSRF-Token and Origin are
		// all forwarded and verified again by social.
		comment := map[string]any{"body": "gm from a cookie session", "clientCommentId": uuid.NewString()}
		cookie := map[string]string{"Origin": e2eOrigin, "Cookie": "__Host-kindle_access=" + bob.cookie, "X-CSRF-Token": bob.csrf}
		resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/pools/"+sp.pool+"/comments", comment, cookie)
		if resp.StatusCode != http.StatusCreated || !strings.Contains(string(raw), bob.wallet) {
			t.Fatalf("cookie comment through the gateway: %d %s", resp.StatusCode, raw)
		}
		// Without the CSRF token the cookie session is refused.
		resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/pools/"+sp.pool+"/comments", map[string]any{"body": "no csrf", "clientCommentId": uuid.NewString()}, map[string]string{"Origin": e2eOrigin, "Cookie": "__Host-kindle_access=" + bob.cookie})
		if resp.StatusCode/100 != 4 {
			t.Fatalf("cookie write without CSRF: %d %s", resp.StatusCode, raw)
		}
		resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/pools/"+sp.pool+"/comments", nil, nil)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "gm from a cookie session") || strings.Contains(string(raw), "no csrf") {
			t.Fatalf("anonymous comments read: %d %s", resp.StatusCode, raw)
		}
		resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/callouts", callout, map[string]string{"Origin": e2eOrigin, "Authorization": "Bearer forged"})
		if resp.StatusCode/100 != 4 {
			t.Fatalf("forged bearer through the gateway: %d %s", resp.StatusCode, raw)
		}
		// Social's private listener serves the API only to the gateway's
		// pinned client certificate, its plain listener only health.
		ranking := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{sp.ranking.Pair}}}}
		if resp, raw = e2eSend(t, ranking, http.MethodGet, sp.privateURL+"/v2/pools/"+sp.pool+"/comments", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("social API served to a non-gateway peer: %d %s", resp.StatusCode, raw)
		}
		if resp, raw = e2eSend(t, nil, http.MethodGet, sp.plainURL+"/v2/pools/"+sp.pool+"/comments", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("social API served on the plain listener: %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("social chat stream through the gateway socket proxy", func(t *testing.T) {
		topics := []string{"chat.pool." + sp.pool}
		wsURL := "ws" + strings.TrimPrefix(edge.URL, "http") + "/v2/stream?topics=" + topics[0]
		dial := func(s e2eSession, ticket string) (*websocket.Conn, *http.Response, error) {
			d := websocket.Dialer{Subprotocols: []string{"sidiora.v2", "ticket." + ticket}, HandshakeTimeout: 10 * time.Second}
			return d.Dial(wsURL, http.Header{"Origin": {e2eOrigin}, "Authorization": {"Bearer " + s.token}})
		}
		aliceTicket := e.chatTicket(alice, topics)
		ac, _, err := dial(alice, aliceTicket)
		if err != nil {
			t.Fatalf("alice stream: %v", err)
		}
		defer ac.Close()
		if ac.Subprotocol() != "sidiora.v2" {
			t.Fatalf("subprotocol %q", ac.Subprotocol())
		}
		if accepted := e2eFrame(t, ac, "session_accepted"); accepted["protocol_version"] != float64(2) {
			t.Fatalf("session_accepted %v", accepted)
		}
		// The ticket was consumed by social at the identity listener.
		if _, res, err := dial(alice, aliceTicket); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ticket replay: %v %v", err, res)
		}
		// A ticket presented with another session's credential is refused.
		if _, res, err := dial(bob, e.chatTicket(alice, topics)); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ticket with another session: %v %v", err, res)
		}
		bc, _, err := dial(bob, e.chatTicket(bob, topics))
		if err != nil {
			t.Fatalf("bob stream: %v", err)
		}
		defer bc.Close()
		e2eFrame(t, bc, "session_accepted")
		// The cross-site browser socket: no Authorization header, no
		// cookie, only Origin and the sidiora.v2 + ticket.<hex>
		// subprotocols. The gateway forwards the unconsumed ticket and
		// social redeems it at the identity listener as issuer "social".
		ticketOnly := func(ticket, origin string) (*websocket.Conn, *http.Response, error) {
			d := websocket.Dialer{Subprotocols: []string{"sidiora.v2", "ticket." + ticket}, HandshakeTimeout: 10 * time.Second}
			return d.Dial(wsURL, http.Header{"Origin": {origin}})
		}
		bobTicket := e.chatTicket(bob, topics)
		if _, res, err := ticketOnly(bobTicket, "https://evil.test"); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Fatalf("ticket-only upgrade from a foreign origin: %v %v", err, res)
		}
		tc, _, err := ticketOnly(bobTicket, e2eOrigin)
		if err != nil {
			t.Fatalf("ticket-only stream: %v", err)
		}
		defer tc.Close()
		if tc.Subprotocol() != "sidiora.v2" {
			t.Fatalf("ticket-only subprotocol %q", tc.Subprotocol())
		}
		e2eFrame(t, tc, "session_accepted")
		if _, res, err := ticketOnly(bobTicket, e2eOrigin); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ticket-only replay: %v %v", err, res)
		}
		if _, res, err := ticketOnly(strings.Repeat("ab", 32), e2eOrigin); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("forged ticket: %v %v", err, res)
		}
		if err := tc.WriteJSON(map[string]any{"type": "join_pool", "pool_address": sp.pool}); err != nil {
			t.Fatal(err)
		}
		e2eFrame(t, tc, "joined_pool")
		for _, conn := range []*websocket.Conn{ac, bc} {
			if err := conn.WriteJSON(map[string]any{"type": "join_pool", "pool_address": sp.pool}); err != nil {
				t.Fatal(err)
			}
			e2eFrame(t, conn, "joined_pool")
		}
		if err := ac.WriteJSON(map[string]any{"type": "pool_message", "pool_address": sp.pool, "content": "gm through the gateway", "client_message_id": "e2e-1"}); err != nil {
			t.Fatal(err)
		}
		e2eFrame(t, ac, "message_persisted")
		if delivered := e2eFrame(t, bc, "pool_message"); delivered["content"] != "gm through the gateway" || delivered["sender"] != alice.wallet {
			t.Fatalf("fan-out %v", delivered)
		}
		if delivered := e2eFrame(t, tc, "pool_message"); delivered["content"] != "gm through the gateway" {
			t.Fatalf("fan-out to the ticket-only stream %v", delivered)
		}
		// A ticket-only stream can send as its redeemed session's wallet.
		if err := tc.WriteJSON(map[string]any{"type": "pool_message", "pool_address": sp.pool, "content": "gm from a ticket", "client_message_id": "e2e-2"}); err != nil {
			t.Fatal(err)
		}
		e2eFrame(t, tc, "message_persisted")
		for {
			delivered := e2eFrame(t, ac, "pool_message")
			if delivered["content"] == "gm from a ticket" {
				if delivered["sender"] != bob.wallet {
					t.Fatalf("ticket-only send %v", delivered)
				}
				break
			}
		}
	})

	t.Run("livestream through the gateway proxy", func(t *testing.T) {
		lp := e.startLivestream()
		upstream, err := proxy.NewConnected(proxy.ConnectedDeps{Name: "livestream", URL: lp.privateURL, Routes: proxy.LivestreamConnectedRoutes(), Identity: e.authorityForGateway, SigningKey: e.gatewayKey.Private, TLS: clientTLS})
		if err != nil {
			t.Fatal(err)
		}
		edge := httptest.NewServer(upstream)
		defer edge.Close()
		if resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/streams/live", nil, nil); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"streams":[]`) {
			t.Fatalf("directory through the gateway: %d %s", resp.StatusCode, raw)
		}
		if resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/streams/pool/"+sp.pool, nil, nil); resp.StatusCode != http.StatusOK {
			t.Fatalf("pool listing through the gateway: %d %s", resp.StatusCode, raw)
		}
		live := uuid.NewString()
		db, err := pgx.Connect(ctx, lp.dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close(ctx)
		if _, err = db.Exec(ctx, `INSERT INTO livestream.stream_operations(id,wallet,pool,title,idem_key,request_hash,state,provider_id,provider_time) VALUES($1,$2,$3,'Gateway e2e',$4,'fixture','LIVE',$5,clock_timestamp())`, live, alice.wallet, sp.pool, "idem_"+live, "prov_"+strings.ReplaceAll(live, "-", "")); err != nil {
			t.Fatal(err)
		}
		// A live stream's document needs provider playback, which the
		// platform runtime does not have: livestream answers the deployment
		// state 409 BROADCAST_NOT_PROVISIONED and the gateway relays it.
		if resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/streams/"+live, nil, nil); resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), `"code":"BROADCAST_NOT_PROVISIONED"`) {
			t.Fatalf("live stream document through the gateway: %d %s", resp.StatusCode, raw)
		}
		ended := uuid.NewString()
		if _, err = db.Exec(ctx, `INSERT INTO livestream.stream_operations(id,wallet,pool,title,idem_key,request_hash,state,provider_id) VALUES($1,$2,$3,'Gateway e2e',$4,'fixture','ENDED',$5)`, ended, alice.wallet, sp.pool, "idem_"+ended, "prov_"+strings.ReplaceAll(ended, "-", "")); err != nil {
			t.Fatal(err)
		}
		if resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/streams/"+ended, nil, nil); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"status":"ended"`) {
			t.Fatalf("stream document through the gateway: %d %s", resp.StatusCode, raw)
		}
		// Anonymous viewer writes pass the gateway without a wallet session.
		resp, raw := e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/streams/"+live+"/viewer-session", map[string]string{}, map[string]string{"Origin": e2eOrigin})
		var session struct{ SessionID, Token string }
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &session) != nil || session.Token == "" {
			t.Fatalf("viewer session through the gateway: %d %s", resp.StatusCode, raw)
		}
		if resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/streams/"+live+"/heartbeat", map[string]string{"sessionId": session.SessionID, "token": session.Token}, map[string]string{"Origin": e2eOrigin}); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"viewerCount":1`) {
			t.Fatalf("heartbeat through the gateway: %d %s", resp.StatusCode, raw)
		}
		// Broadcaster actions need a session at the gateway; with one they
		// reach livestream, which has no broadcast provider on the platform.
		if resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/streams", map[string]string{"poolAddress": sp.pool, "title": "x"}, map[string]string{"Origin": e2eOrigin}); resp.StatusCode/100 != 4 {
			t.Fatalf("anonymous stream create: %d %s", resp.StatusCode, raw)
		}
		if resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v2/streams", map[string]string{"poolAddress": sp.pool, "title": "x"}, map[string]string{"Origin": e2eOrigin, "Authorization": "Bearer " + alice.token}); resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), `"code":"BROADCAST_NOT_PROVISIONED"`) {
			t.Fatalf("stream create without a provider: %d %s", resp.StatusCode, raw)
		}
		// The API is served only to the gateway, on the private listener.
		other := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{e.peers["social"].leaf.Pair}}}}
		if resp, raw = e2eSend(t, other, http.MethodGet, lp.privateURL+"/v2/streams/live", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("livestream API served to a non-gateway peer: %d %s", resp.StatusCode, raw)
		}
		if resp, raw = e2eSend(t, nil, http.MethodGet, lp.plainURL+"/v2/streams/live", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("livestream API served on the plain listener: %d %s", resp.StatusCode, raw)
		}
		// A real dependency failure (the moderation store livestream reads
		// is gone) stays a 5xx: the gateway answers 503 with livestream's
		// envelope code, not its message or dependency names.
		if _, err = db.Exec(ctx, `ALTER TABLE chat.safety_bans RENAME TO safety_bans_offline`); err != nil {
			t.Fatal(err)
		}
		resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/v2/streams/"+ended, nil, nil)
		if _, err = db.Exec(ctx, `ALTER TABLE chat.safety_bans_offline RENAME TO safety_bans`); err != nil {
			t.Fatal(err)
		}
		var relayed interfaces.Error
		if resp.StatusCode != http.StatusServiceUnavailable || interfaces.ValidateJSON("Error", raw) != nil || json.Unmarshal(raw, &relayed) != nil || relayed.Code != "DEPENDENCY_UNAVAILABLE" || !relayed.Retryable || relayed.Details != nil || strings.Contains(string(raw), "livestream_store") {
			t.Fatalf("dependency failure through the gateway: %d %s", resp.StatusCode, raw)
		}
		// A dead upstream has no envelope: the gateway's own code.
		dead, err := proxy.NewConnected(proxy.ConnectedDeps{Name: "livestream", URL: fmt.Sprintf("https://127.0.0.1:%d", freePort(t)), Routes: proxy.LivestreamConnectedRoutes(), Identity: e.authorityForGateway, SigningKey: e.gatewayKey.Private, TLS: clientTLS})
		if err != nil {
			t.Fatal(err)
		}
		deadEdge := httptest.NewServer(dead)
		defer deadEdge.Close()
		if resp, raw = e2eSend(t, nil, http.MethodGet, deadEdge.URL+"/v2/streams/live", nil, nil); resp.StatusCode != http.StatusServiceUnavailable || strings.TrimSpace(string(raw)) != `{"code":"upstream_unavailable"}` {
			t.Fatalf("dead upstream: %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("ranking visibility authority", func(t *testing.T) {
		ranking := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{sp.ranking.Pair}, ServerName: "social.railway.internal"}}}
		resp, raw := e2eSend(t, ranking, http.MethodGet, sp.privateURL+"/internal/v2/social/ranking-visibility", nil, nil)
		var snapshot struct {
			Revision   string          `json:"revision"`
			ObservedAt time.Time       `json:"observedAt"`
			Complete   bool            `json:"complete"`
			Pools      map[string]bool `json:"pools"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &snapshot) != nil || !snapshot.Complete || len(snapshot.Revision) != 64 || !snapshot.Pools[sp.pool] || time.Since(snapshot.ObservedAt) > time.Minute {
			t.Fatalf("ranking visibility: %d %s", resp.StatusCode, raw)
		}
		gateway := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}
		if resp, raw = e2eSend(t, gateway, http.MethodGet, sp.privateURL+"/internal/v2/social/ranking-visibility", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("ranking authority served to the gateway: %d %s", resp.StatusCode, raw)
		}
	})
}
