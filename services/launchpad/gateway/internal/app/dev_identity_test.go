package app_test

import (
	"bytes"
	"context"
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
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/devidentity"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
)

const devOrigin = "https://kindle.test"

// recordingUpstream stands in for the social service's network boundary: it
// records the path and the trusted actor header the gateway forwarded.
type recordingUpstream struct {
	mu    sync.Mutex
	paths []string
	actor []string
}

func (u *recordingUpstream) last() (string, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.paths) == 0 {
		return "", ""
	}
	return u.paths[len(u.paths)-1], u.actor[len(u.actor)-1]
}

type devGateway struct {
	base, serviceToken string
	listener           net.Addr
	upstream           *recordingUpstream
}

// realPostgres returns the shared fixture DSN (the same one the identity
// package tests use) and resets the development identity schema.
func realPostgres(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("L33_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Ping(ctx); err != nil {
		t.Fatalf("real PostgreSQL required: %v", err)
	}
	drop := func() {
		if _, e := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+devidentity.Schema+` CASCADE`); e != nil {
			t.Error(e)
		}
	}
	drop()
	t.Cleanup(func() { drop(); admin.Close() })
	return dsn
}

// realChain starts an in-process go-ethereum node (the simulated beacon
// backend) with chain id 125 and its JSON-RPC served over HTTP, so the
// gateway's Chain125ProofVerifier runs its real eth_chainId /
// eth_getBlockByNumber / EIP-1898 eth_getCode calls against a real node.
func realChain(t *testing.T) string {
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
	id, err := client.ChainID(context.Background())
	if err != nil || id.Cmp(big.NewInt(125)) != 0 {
		t.Fatalf("chain-125 node at %s: id=%v err=%v", rpc, id, err)
	}
	return rpc
}

func bootDevIdentity(t *testing.T, rateMax int) *devGateway {
	t.Helper()
	return bootDevIdentityWith(t, rateMax, nil)
}

// bootDevIdentityWith boots the development gateway like bootDevIdentity and
// lets the caller adjust the configuration (for example USER_HTTP_URL) first.
func bootDevIdentityWith(t *testing.T, rateMax int, adjust func(*config.Config)) *devGateway {
	t.Helper()
	_, redisURL := internaltest.NewRedis(t)
	mc := internaltest.NewMinIO(t, "kl-token")
	dsn := realPostgres(t)
	rpc := realChain(t)

	upstream := &recordingUpstream{}
	social := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.mu.Lock()
		upstream.paths = append(upstream.paths, r.Method+" "+r.URL.Path)
		upstream.actor = append(upstream.actor, r.Header.Get("X-Actor-Wallet"))
		upstream.mu.Unlock()
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(social.Close)

	secret := strings.Repeat("a1", 32)
	t.Setenv(devidentity.SigningSecretReference, secret)
	cfg := config.Config{
		NodeEnv:                "development",
		RedisURL:               redisURL,
		LogLevel:               "error",
		JWTSecret:              "integration-test-signing-secret",
		JWTTTLSeconds:          3600,
		NonceTTLSeconds:        300,
		AppDomain:              "kindlelaunch",
		SocialHTTPURL:          social.URL,
		SocialWSURL:            "ws" + strings.TrimPrefix(social.URL, "http"),
		MetadataUploadURL:      social.URL,
		UpstreamTimeoutSeconds: 30,
		S3Endpoint:             mc.Endpoint,
		S3AccessKeyID:          mc.AccessKeyID,
		S3SecretAccessKey:      mc.SecretAccessKey,
		S3Region:               "us-east-1",
		TokenBucket:            "kl-token",
		ObjectCacheMaxBytes:    16,
		ObjectCacheTTLSeconds:  300,
		MaxUploadBytes:         6 << 20,
		RateLimitMax:           rateMax,
		RateLimitWindowSeconds: 60,
		CORSAllowedOrigins:     devOrigin,
		PublicURL:              devOrigin,
		DatabaseURL:            dsn,
		RPCURL:                 rpc,
		IdentitySigningSecret:  secret,
		IdentityLimiterKey:     strings.Repeat("l", 32),
		IdentityServiceToken:   strings.Repeat("s7", 32),
	}
	if adjust != nil {
		adjust(&cfg)
	}
	a, err := app.New(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)
	ts := httptest.NewServer(a.Router)
	t.Cleanup(ts.Close)
	return &devGateway{base: ts.URL, serviceToken: cfg.IdentityServiceToken, listener: ts.Listener.Addr(), upstream: upstream}
}

// privateNetwork dials gateway.railway.internal:8080 at the test listener, so
// the shared/auth client exercises its plain-http private-network mode.
func (g *devGateway) privateNetwork() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "gateway.railway.internal:8080" {
			return nil, errors.New("unexpected dial " + address)
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", g.listener.String())
	}}}
}

func send(t *testing.T, method, url string, body any, headers map[string]string) (*http.Response, []byte) {
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// signIn runs the browser's real sign-in: challenge, personal_sign with a
// secp256k1 key, session.
func signIn(t *testing.T, g *devGateway) (wallet, accessToken, sessionID string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet = strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	resp, raw := send(t, http.MethodPost, g.base+"/v2/auth/challenges", map[string]string{"wallet": wallet, "origin": devOrigin}, map[string]string{"Origin": devOrigin, "Idempotency-Key": "8e0f1b8c-62f4-4f43-9f36-3d2c8b7f6a10"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("challenge status %d: %s", resp.StatusCode, raw)
	}
	var challenge struct {
		Data struct {
			Challenge struct {
				ChallengeID, Domain, URI, Message string
			}
		}
	}
	if err = json.Unmarshal(raw, &challenge); err != nil {
		t.Fatal(err)
	}
	c := challenge.Data.Challenge
	if c.Domain != "kindle.test" || c.URI != devOrigin || !strings.HasPrefix(c.Message, "kindle.test wants you to sign in with your Ethereum account:\n") {
		t.Fatalf("challenge not bound to PUBLIC_URL: %+v", c)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(c.Message)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	resp, raw = send(t, http.MethodPost, g.base+"/v2/auth/sessions", map[string]any{"challengeId": c.ChallengeID, "proof": map[string]string{"kind": "eoa_eip191", "wallet": wallet, "signature": "0x" + hex.EncodeToString(sig)}, "deviceLabel": "Web browser"}, map[string]string{"Origin": devOrigin})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status %d: %s", resp.StatusCode, raw)
	}
	var login struct {
		Data struct {
			Session struct {
				SessionID, Wallet string
			}
			AccessToken string
		}
	}
	if err = json.Unmarshal(raw, &login); err != nil {
		t.Fatal(err)
	}
	if login.Data.Session.Wallet != wallet || login.Data.AccessToken == "" {
		t.Fatalf("login data %s", raw)
	}
	header, _, _ := strings.Cut(login.Data.AccessToken, ".")
	var h map[string]any
	rawHeader, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil || json.Unmarshal(rawHeader, &h) != nil || h["alg"] != "HS256" || h["typ"] != "JWT" {
		t.Fatalf("access token header %s", rawHeader)
	}
	return wallet, login.Data.AccessToken, login.Data.Session.SessionID
}

func TestDevelopmentIdentitySignInIntrospectionAndProxy(t *testing.T) {
	g := bootDevIdentity(t, 1000)
	wallet, token, sessionID := signIn(t, g)

	// Introspection through the shared/auth client in plain-http private mode.
	authority := sharedauth.HTTPIdentityAuthority{Endpoint: "http://gateway.railway.internal:8080", ServiceToken: g.serviceToken, AllowPlainHTTP: true, Client: g.privateNetwork()}
	browser, _ := http.NewRequest(http.MethodGet, "https://api.example/v3/me/portfolio", nil)
	browser.Header.Set("Authorization", "Bearer "+token)
	browser.Header.Set("Origin", devOrigin)
	principal, err := authority.AuthenticateRequest(context.Background(), browser)
	if err != nil {
		t.Fatalf("introspection: %v", err)
	}
	if principal.Wallet != wallet || principal.SessionID != sessionID || principal.AccountID == "" {
		t.Fatalf("principal %+v", principal)
	}
	wrongToken := sharedauth.HTTPIdentityAuthority{Endpoint: "http://gateway.railway.internal:8080", ServiceToken: strings.Repeat("x", 64), AllowPlainHTTP: true, Client: g.privateNetwork()}
	var authorityErr *sharedauth.IdentityAuthorityError
	if _, err = wrongToken.AuthenticateRequest(context.Background(), browser); !errors.As(err, &authorityErr) || authorityErr.Status != http.StatusUnauthorized {
		t.Fatalf("wrong service token: %v", err)
	}

	// The internal surface is invisible through Railway's edge.
	body := map[string]string{"authorization": "Bearer " + token, "accessCookie": "", "csrfToken": "", "origin": "", "method": "GET"}
	resp, _ := send(t, http.MethodPost, g.base+"/internal/v2/identity/request:authenticate", body, map[string]string{"Authorization": "Bearer " + g.serviceToken, "X-Real-IP": "203.0.113.9", "X-Railway-Edge": "railway/us-east4"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("edge request to internal identity: %d", resp.StatusCode)
	}
	resp, _ = send(t, http.MethodPost, g.base+"/internal/v2/identity/request:authenticate", body, map[string]string{"Authorization": "Bearer " + g.serviceToken})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("private request to internal identity: %d", resp.StatusCode)
	}

	// A v2 session drives X-Actor-Wallet on the proxied social surface.
	resp, raw := send(t, http.MethodGet, g.base+"/v2/bounties", nil, map[string]string{"Authorization": "Bearer " + token, "Origin": devOrigin, "X-Actor-Wallet": "0x000000000000000000000000000000000000dead"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bounties status %d: %s", resp.StatusCode, raw)
	}
	if path, actor := g.upstream.last(); path != "GET /v2/bounties" || actor != wallet {
		t.Fatalf("upstream saw %q actor %q, want actor %q", path, actor, wallet)
	}
	if got := resp.Header.Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != devOrigin {
		t.Fatalf("CORS allow-origin %v", got)
	}
	resp, _ = send(t, http.MethodGet, g.base+"/v2/pools/0x1234567890abcdef1234567890abcdef12345678/callouts", nil, nil)
	if path, actor := g.upstream.last(); resp.StatusCode != http.StatusOK || path != "GET /v2/pools/0x1234567890abcdef1234567890abcdef12345678/callouts" || actor != "" {
		t.Fatalf("anonymous read: %d %q actor %q", resp.StatusCode, path, actor)
	}
	resp, _ = send(t, http.MethodPost, g.base+"/v2/callouts", map[string]string{"x": "y"}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous write: %d", resp.StatusCode)
	}
	resp, _ = send(t, http.MethodPost, g.base+"/v2/bounties/b1/submissions/s1:accept", map[string]string{}, map[string]string{"Authorization": "Bearer " + token})
	if path, actor := g.upstream.last(); resp.StatusCode != http.StatusOK || path != "POST /v2/bounties/b1/submissions/s1:accept" || actor != wallet {
		t.Fatalf("authenticated write: %d %q actor %q", resp.StatusCode, path, actor)
	}
	resp, _ = send(t, http.MethodPut, g.base+"/v2/social/follow/0x1234567890abcdef1234567890abcdef12345678", nil, map[string]string{"Authorization": "Bearer " + token})
	if path, actor := g.upstream.last(); resp.StatusCode != http.StatusOK || path != "PUT /v2/social/follow/0x1234567890abcdef1234567890abcdef12345678" || actor != wallet {
		t.Fatalf("social follow: %d %q actor %q", resp.StatusCode, path, actor)
	}
	resp, _ = send(t, http.MethodGet, g.base+"/social/pools/0xabc/messages", nil, map[string]string{"Authorization": "Bearer " + token})
	if path, actor := g.upstream.last(); resp.StatusCode != http.StatusOK || path != "GET /pools/0xabc/messages" || actor != wallet {
		t.Fatalf("legacy tunnel with v2 session: %d %q actor %q", resp.StatusCode, path, actor)
	}

	// The legacy /auth/login bearer still works on the same surface.
	legacyToken, legacyWallet := login(t, g.base)
	resp, _ = send(t, http.MethodGet, g.base+"/v2/chat/history", nil, map[string]string{"Authorization": "Bearer " + legacyToken})
	if path, actor := g.upstream.last(); resp.StatusCode != http.StatusOK || path != "GET /v2/chat/history" || actor != legacyWallet {
		t.Fatalf("legacy bearer: %d %q actor %q", resp.StatusCode, path, actor)
	}

	// Logout revokes: introspection and the proxy stop recognising the token.
	resp, raw = send(t, http.MethodDelete, g.base+"/v2/auth/sessions/"+sessionID, nil, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout %d: %s", resp.StatusCode, raw)
	}
	if _, err = authority.AuthenticateRequest(context.Background(), browser); !errors.As(err, &authorityErr) || authorityErr.Status != http.StatusUnauthorized {
		t.Fatalf("introspection after logout: %v", err)
	}
	resp, _ = send(t, http.MethodPost, g.base+"/v2/callouts", map[string]string{"x": "y"}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("proxy after logout: %d", resp.StatusCode)
	}
}

// Rate limits key on the client address Railway's edge reports (X-Real-IP),
// not on the edge's own connection address.
func TestDevelopmentRateLimitKeysOnEdgeClientIP(t *testing.T) {
	g := bootDevIdentity(t, 2)
	read := func(ip string) int {
		resp, _ := send(t, http.MethodGet, g.base+"/v2/bounties", nil, map[string]string{"X-Real-IP": ip, "X-Forwarded-For": ip + ", 10.0.0.1"})
		return resp.StatusCode
	}
	for i := 0; i < 2; i++ {
		if status := read("198.51.100.1"); status != http.StatusOK {
			t.Fatalf("client A request %d: %d", i, status)
		}
	}
	if status := read("198.51.100.1"); status != http.StatusTooManyRequests {
		t.Fatalf("client A over budget: %d", status)
	}
	if status := read("198.51.100.2"); status != http.StatusOK {
		t.Fatalf("client B shares client A's budget: %d", status)
	}
}
