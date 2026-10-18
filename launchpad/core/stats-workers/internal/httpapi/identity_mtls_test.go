package httpapi_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
)

// statsIdentityListener reproduces the gateway's private identity listener
// for statsd: a TLS 1.3 listener with the gateway server certificate that
// requires a client certificate of the private CA, serving only
// /internal/v2/identity/* to a verified chain and authenticating the calling
// service with the gateway's verifier (auth.ServiceRequestVerifier) for
// issuer "stats" before answering request:authenticate / session:check for a
// fixed set of sessions. media/gateway's
// TestPlatformPeersThroughGatewayIdentityListener runs the same "stats" client
// against the real gateway identity.
type statsIdentityListener struct {
	mu       sync.Mutex
	verifier auth.ServiceRequestVerifier
	sessions map[string]auth.IdentityPrincipal
	served   map[string]int
	outage   bool
	server   *httptest.Server
}

func (l *statsIdentityListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/internal/v2/identity/") || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil || r.Method != http.MethodPost || l.verifier.Authenticate(r, raw) != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	l.served[r.URL.Path]++
	if l.outage {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	switch r.URL.Path {
	case "/internal/v2/identity/request:authenticate":
		var request auth.IdentityAuthenticationRequest
		if json.Unmarshal(raw, &request) != nil || auth.ValidateIdentityAuthenticationRequest(request) != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		principal, ok := l.sessions[request.Authorization]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(principal)
	case "/internal/v2/identity/session:check":
		var p auth.StreamPrincipal
		if json.Unmarshal(raw, &p) != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		for _, s := range l.sessions {
			if s.StreamPrincipal() == p {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestReactionIdentityOverGatewayMTLSListener(t *testing.T) {
	ctx := context.Background()
	pki := testpki.New(t)
	gatewayServer := pki.Leaf("gateway-server", "gateway.railway.internal")
	statsClient := pki.Leaf("stats-client", "stats.railway.internal")
	statsKey := testpki.Ed25519(t)
	alice := "0x" + strings.Repeat("a1", 20)
	bob := "0x" + strings.Repeat("b0", 20)
	principal := func(wallet string) auth.IdentityPrincipal {
		return auth.IdentityPrincipal{AccountID: uuid.NewString(), SessionID: uuid.NewString(), Wallet: wallet, AuthEpoch: 1, ProofAt: time.Now().Add(-time.Minute).UTC(), AccessExpiresAt: time.Now().Add(time.Hour).UTC()}
	}
	listener := &statsIdentityListener{
		verifier: auth.ServiceRequestVerifier{Transport: auth.AssertionTransport{AssertionHeader: auth.IdentityAssertionHeader, SignatureHeader: auth.IdentitySignatureHeader}, Issuer: "stats", Audience: "identity", PeerCertSHA256: statsClient.SHA256, Key: statsKey.Private.Public().(ed25519.PublicKey)},
		sessions: map[string]auth.IdentityPrincipal{"Bearer alice-token": principal(alice), "Bearer bob-token": principal(bob)},
		served:   map[string]int{},
	}
	listener.server = httptest.NewUnstartedServer(listener)
	listener.server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.Roots(), Certificates: []tls.Certificate{gatewayServer.Pair}}
	listener.server.StartTLS()
	t.Cleanup(listener.server.Close)

	// statsd's production identity settings, as ResolveProductionSecrets
	// validates them and app.New turns them into the client.
	cfg := config.Config{IdentityURL: listener.server.URL + "/", IdentityMTLSCAFile: pki.CAFile, IdentityMTLSClientCertFile: statsClient.CertFile, IdentityMTLSClientKeyPEM: string(statsClient.KeyPEM), IdentityMTLSPeerCertSHA256: gatewayServer.SHA256, IdentitySigningKey: statsKey.StdBase64}
	if err := cfg.ValidateIdentityClient(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*config.Config){
		"plain private network": func(c *config.Config) { c.IdentityURL = "http://gateway.railway.internal:8080" },
		"loopback http":         func(c *config.Config) { c.IdentityURL = "http://127.0.0.1:8080" },
		"path":                  func(c *config.Config) { c.IdentityURL = "https://gateway.railway.internal:8443/internal" },
		"no ca":                 func(c *config.Config) { c.IdentityMTLSCAFile = "" },
		"no key":                func(c *config.Config) { c.IdentityMTLSClientKeyPEM = "" },
		"two keys":              func(c *config.Config) { c.IdentityMTLSClientKeyFile = statsClient.KeyFile },
		"upper pin":             func(c *config.Config) { c.IdentityMTLSPeerCertSHA256 = strings.ToUpper(gatewayServer.SHA256) },
		"signing key":           func(c *config.Config) { c.IdentitySigningKey = strings.Repeat("A", 88) },
	} {
		bad := cfg
		mutate(&bad)
		if err := bad.ValidateIdentityClient(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	client := cfg.IdentityClient()
	if client.Issuer != "stats" || strings.HasSuffix(client.Endpoint, "/") {
		t.Fatalf("identity client %+v", client)
	}
	identity, err := auth.NewMTLSIdentityClient(client)
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.Probe(ctx); err != nil {
		t.Fatalf("probe: %v", err)
	}

	redisURL := os.Getenv("STATS_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://:kindle_fixture_password@127.0.0.1:26379/12"
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(options)
	t.Cleanup(func() { _ = rdb.Close() })
	if err = rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("fixture redis: %v", err)
	}
	namespace := "identity_mtls_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	t.Cleanup(func() {
		keys, _ := rdb.Keys(context.Background(), namespace+":*").Result()
		if len(keys) > 0 {
			_ = rdb.Del(context.Background(), keys...).Err()
		}
	})
	router := chi.NewRouter()
	policies := auth.PolicyRegistry{Version: "identity-mtls", Actions: map[string]auth.Policy{"reaction.replace": {Scope: "wallet", OwnerRequired: true, RouteTemplate: "/v2/stats/{poolAddress}/reactions"}}}
	if err = httpapi.RegisterDurableReactions(router, httpapi.DurableReactionDeps{Identity: identity, Policies: policies, Admission: &httpapi.ReactionAdmission{Redis: rdb, Namespace: namespace, WalletPerMinute: 12, IPPerMinute: 100}}); err != nil {
		t.Fatal(err)
	}
	pool := "0x" + strings.Repeat("1", 40)
	react := func(wallet, bearer string) *httptest.ResponseRecorder {
		body := `{"chainId":"125","pool":"` + pool + `","wallet":"` + wallet + `","reaction":"hot","nonce":"n` + strings.ReplaceAll(uuid.NewString(), "-", "")[:12] + `","expiry":1,"expectedRevision":0}`
		request := httptest.NewRequest(http.MethodPost, "/v2/stats/"+pool+"/reactions", strings.NewReader(body))
		request.RemoteAddr = "192.0.2.30:41000"
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	admitted := func(wallet string) int {
		n, err := rdb.Get(ctx, namespace+":{reaction-admission}:wallet:"+wallet).Int()
		if err != nil && !errors.Is(err, redis.Nil) {
			t.Fatal(err)
		}
		return n
	}

	// A verified session reaches admission (keyed by the wallet the identity
	// authority returned) and then needs its signed operation envelope.
	if response := react(alice, "alice-token"); response.Code != http.StatusForbidden || admitted(alice) != 1 {
		t.Fatalf("verified reaction: %d %s admitted=%d", response.Code, response.Body.String(), admitted(alice))
	}
	// Forged credentials and another wallet's session never reach admission.
	if response := react(alice, "forged"); response.Code != http.StatusForbidden || admitted(alice) != 1 {
		t.Fatalf("forged bearer: %d admitted=%d", response.Code, admitted(alice))
	}
	if response := react(alice, "bob-token"); response.Code != http.StatusForbidden || admitted(alice) != 1 || admitted(bob) != 0 {
		t.Fatalf("other session: %d", response.Code)
	}
	if response := react(alice, ""); response.Code != http.StatusForbidden || admitted(alice) != 1 {
		t.Fatalf("anonymous reaction: %d", response.Code)
	}
	listener.mu.Lock()
	authenticated := listener.served["/internal/v2/identity/request:authenticate"]
	listener.outage = true
	listener.mu.Unlock()
	if authenticated != 3 {
		t.Fatalf("identity authority saw %d authenticated statsd requests, want 3", authenticated)
	}
	// An identity authority outage is a dependency failure, not a denial.
	if response := react(alice, "alice-token"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authority outage: %d", response.Code)
	}
	listener.mu.Lock()
	listener.outage = false
	listener.mu.Unlock()

	// The session re-check the operation verifier runs goes over the same
	// channel.
	verified, err := identity.AuthenticateRequest(ctx, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v2/search/expanded", nil)
		r.Header.Set("Authorization", "Bearer bob-token")
		return r
	}())
	if err != nil || verified.Wallet != bob {
		t.Fatalf("authenticate: %+v %v", verified, err)
	}
	if err = identity.CheckStreamSession(ctx, verified.StreamPrincipal()); err != nil {
		t.Fatal(err)
	}

	// A statsd whose assertion key or client certificate the gateway does
	// not hold is refused by the listener.
	for name, c := range map[string]auth.MTLSIdentityClientConfig{
		"unregistered key": func() auth.MTLSIdentityClientConfig {
			c := client
			c.SigningKey = testpki.Ed25519(t).StdBase64
			return c
		}(),
		"unregistered cert": func() auth.MTLSIdentityClientConfig {
			other := pki.Leaf("other-client", "stats.railway.internal")
			c := client
			c.ClientCertFile, c.ClientKeyPEM = other.CertFile, string(other.KeyPEM)
			return c
		}(),
		"foreign ca cert": func() auth.MTLSIdentityClientConfig {
			other := testpki.Foreign(t, "stats-client", "stats.railway.internal")
			c := client
			c.ClientCertFile, c.ClientKeyPEM = other.CertFile, string(other.KeyPEM)
			return c
		}(),
		"wrong gateway pin": func() auth.MTLSIdentityClientConfig { c := client; c.PeerCertSHA256 = statsClient.SHA256; return c }(),
	} {
		rejected, err := auth.NewMTLSIdentityClient(c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err = rejected.Probe(ctx); err == nil {
			t.Fatalf("%s: probe accepted", name)
		}
		if _, err = rejected.AuthenticateRequest(ctx, func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/v2/search/expanded", nil)
			r.Header.Set("Authorization", "Bearer alice-token")
			return r
		}()); err == nil {
			t.Fatalf("%s: session verified", name)
		}
	}
}
