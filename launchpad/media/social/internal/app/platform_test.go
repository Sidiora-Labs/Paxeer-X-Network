package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/config"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production/coordinatortest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
)

// The production platform runtime (NODE_ENV=production, RUNTIME_PROFILE=
// platform) is booted against a fresh database on the fixture Postgres and the
// fixture Redis, with identity served over the gateway's private v2 identity
// wire (request:authenticate, session:check, chat-ticket:consume) for a fixed
// set of sessions, behind the gateway private listener's transport contract: a
// TLS 1.3 listener with the gateway server certificate that requires a client
// certificate of the private CA, and the gateway's ed25519 request-assertion
// verifier (sharedauth.ServiceRequestVerifier) for issuer "social". The
// platform runtime is reached on its private mTLS listener with the gateway's
// client certificate. A throwaway PKI is issued per test (shared/testpki).
// media/gateway's TestPlatformPeersThroughGatewayIdentityListener drives the
// same runtime (the sociald binary) against the real gateway identity.

const platformAdminKey = "a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9"
const platformOrigin = "https://web.example"

type issuedTicket struct {
	principal sharedauth.StreamPrincipal
	binding   sharedauth.StreamTicketBinding
}

// identityWire is the gateway's private identity surface for fixed sessions.
type identityWire struct {
	mu       sync.Mutex
	sessions map[string]sharedauth.IdentityPrincipal // bearer -> principal
	revoked  map[string]bool                         // session id
	tickets  map[string]issuedTicket
	outage   bool
	checks   int
	verifier sharedauth.ServiceRequestVerifier
	server   *httptest.Server
}

// platformPKI is the throwaway private PKI of one platform test.
type platformPKI struct {
	ca                                                *testpki.PKI
	gatewayServer, gatewayClient, socialServer        testpki.Leaf
	socialClient, rankingClient, foreignGatewayClient testpki.Leaf
	socialKey                                         testpki.AssertionKey
}

func newPlatformPKI(t *testing.T) platformPKI {
	ca := testpki.New(t)
	return platformPKI{ca: ca, gatewayServer: ca.Leaf("gateway-server", "gateway.railway.internal"), gatewayClient: ca.Leaf("gateway-client", "gateway.railway.internal"), socialServer: ca.Leaf("social-server", "social.railway.internal"), socialClient: ca.Leaf("social-client", "social.railway.internal"), rankingClient: ca.Leaf("ranking-client", "ranking.railway.internal"), foreignGatewayClient: testpki.Foreign(t, "gateway-client", "gateway.railway.internal"), socialKey: testpki.Ed25519(t)}
}

// mtlsClient is an HTTP client presenting leaf to the private listeners of
// this PKI.
func (p platformPKI) mtlsClient(leaf testpki.Leaf) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: p.ca.Roots(), Certificates: []tls.Certificate{leaf.Pair}}}}
}

func newIdentityWire(t *testing.T, pki platformPKI) *identityWire {
	t.Helper()
	w := &identityWire{sessions: map[string]sharedauth.IdentityPrincipal{}, revoked: map[string]bool{}, tickets: map[string]issuedTicket{}}
	w.verifier = sharedauth.ServiceRequestVerifier{Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}, Issuer: "social", Audience: "identity", PeerCertSHA256: pki.socialClient.SHA256, Key: pki.socialKey.Private.Public().(ed25519.PublicKey)}
	w.server = httptest.NewUnstartedServer(http.HandlerFunc(w.serve))
	w.server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.ca.Roots(), Certificates: []tls.Certificate{pki.gatewayServer.Pair}}
	w.server.StartTLS()
	t.Cleanup(w.server.Close)
	return w
}

func (w *identityWire) session(bearer, wallet string) sharedauth.IdentityPrincipal {
	p := sharedauth.IdentityPrincipal{AccountID: uuid.NewString(), SessionID: uuid.NewString(), Wallet: wallet, AuthEpoch: 1, ProofAt: time.Now().Add(-time.Minute).UTC(), AccessExpiresAt: time.Now().Add(time.Hour).UTC()}
	w.mu.Lock()
	w.sessions[bearer] = p
	w.mu.Unlock()
	return p
}

func (w *identityWire) ticket(p sharedauth.IdentityPrincipal, topics []string) string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	ticket := hex.EncodeToString(raw)
	w.mu.Lock()
	w.tickets[ticket] = issuedTicket{principal: p.StreamPrincipal(), binding: sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: platformOrigin, Endpoint: "/v2/stream"}}
	w.mu.Unlock()
	return ticket
}

func (w *identityWire) set(f func(*identityWire)) {
	w.mu.Lock()
	f(w)
	w.mu.Unlock()
}

func (w *identityWire) serve(rw http.ResponseWriter, r *http.Request) {
	// The gateway private router serves only /internal/v2/identity/* to a
	// verified client chain; the identity routes authenticate the calling
	// service before reading the request.
	if !strings.HasPrefix(r.URL.Path, "/internal/v2/identity/") || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil || r.Method != http.MethodPost || w.verifier.Authenticate(r, raw) != nil {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		// The gateway's strict decoder answers a non-object 422 FIELD_INVALID.
		rw.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if w.outage {
		rw.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	reply := func(v any) {
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(v)
	}
	switch r.URL.Path {
	case "/internal/v2/identity/request:authenticate":
		var req sharedauth.IdentityAuthenticationRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || sharedauth.ValidateIdentityAuthenticationRequest(req) != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		p, ok := w.sessions[req.Authorization]
		if !ok || w.revoked[p.SessionID] {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		reply(p)
	case "/internal/v2/identity/session:check":
		var p sharedauth.StreamPrincipal
		if json.NewDecoder(r.Body).Decode(&p) != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		w.checks++
		for _, s := range w.sessions {
			if s.StreamPrincipal() == p && !w.revoked[p.SessionID] {
				rw.WriteHeader(http.StatusOK)
				return
			}
		}
		rw.WriteHeader(http.StatusUnauthorized)
	case "/internal/v2/identity/chat-ticket:consume":
		var req struct {
			Ticket  string                         `json:"ticket"`
			Binding sharedauth.StreamTicketBinding `json:"binding"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		issued, ok := w.tickets[req.Ticket]
		delete(w.tickets, req.Ticket) // single use, consumed even on mismatch
		if !ok || w.revoked[issued.principal.SessionID] || req.Binding.Origin != issued.binding.Origin || req.Binding.Endpoint != issued.binding.Endpoint || req.Binding.FilterDigest != issued.binding.FilterDigest || strings.Join(req.Binding.Topics, ",") != strings.Join(issued.binding.Topics, ",") {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		reply(issued.principal)
	default:
		rw.WriteHeader(http.StatusNotFound)
	}
}

type platformHarness struct {
	devHarness
	wire  *identityWire
	pki   platformPKI
	plain *httptest.Server
	db    coordinatortest.Database
}

// platformDatabase is a private database bootstrapped the way the production
// migration coordinator (kindle-migrate) bootstraps kindle_prod, scoped to the
// registered migrations chat_runtime reads (chat, indexer, recovery and their
// predecessors in users and metadata), with chat_runtime as a LOGIN role
// holding exactly its reviewed grants.json privileges. The pool seeded in the
// production indexer's canonical creations stands in for core/indexer.
func platformDatabase(t *testing.T) (coordinatortest.Database, *pgx.Conn, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db := coordinatortest.Bootstrap(t, fixtureEnv("SOCIAL_FIXTURE_POSTGRES_DSN", "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"), "chat_runtime")
	conn, err := pgx.Connect(ctx, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	pool, token, creator := addr(0x9001), addr(0x7001), addr(0xc0ffee)
	if _, err = conn.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,1,100,$6,$7,0,0,$8,'{}','{}')`, token, pool, creator, addr(0xfac), addr(0x0b7), "0x"+strings.Repeat("ab", 32), "0x"+strings.Repeat("cd", 32), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return db, conn, pool, token, creator
}

// platformConfig is the platform runtime configuration of a test PKI and
// identity wire on db (secrets resolve from the environment).
func platformConfig(h *platformHarness, db coordinatortest.Database) config.Config {
	cfg := config.Config{
		NodeEnv: "production", RuntimeProfile: config.PlatformProfile, RuntimeZone: "zone-a", RuntimeMetricsEnvironment: "production",
		SecretsProvider: "env", SecretsRefreshSeconds: 30, Port: 8080,
		DatabaseURL: "postgres://unresolved", RedisURL: "redis://unresolved", ChatAdminAPIKey: "unresolved", IdentitySigningKey: "unresolved",
		IdentityURL: h.wire.server.URL, CORSAllowedOrigins: platformOrigin, DBMaxConns: 10,
		IdentityMTLSCAFile: h.pki.ca.CAFile, IdentityMTLSClientCertFile: h.pki.socialClient.CertFile, IdentityMTLSClientKeyFile: h.pki.socialClient.KeyFile, IdentityMTLSPeerCertSHA256: h.pki.gatewayServer.SHA256,
		PrivateListenAddress: "127.0.0.1:0", PrivateTLSCertFile: h.pki.socialServer.CertFile, PrivateTLSKeyFile: h.pki.socialServer.KeyFile, PrivateTLSClientCAFile: h.pki.ca.CAFile,
		GatewayPeerCertSHA256: h.pki.gatewayClient.SHA256, AuthorityPeers: "ranking.railway.internal=" + h.pki.rankingClient.SHA256,
		MaxMessageLength: 500, RateWindowSeconds: 10, MaxPoolMsgsPerWin: 20, MaxDmMsgsPerWindow: 20,
		WSSendBuffer: 64, WSWriteTimeoutSecs: 10, WSReadLimitBytes: 4096,
	}
	cfg.ExpectedSchemaFile, cfg.ExpectedSchemaSHA256 = db.ExpectedSchemaFile(h.t, "chat_runtime")
	return cfg
}

// platformSecrets sets the platform runtime's secrets (SECRETS_PROVIDER=env,
// as on the platform) for databaseURL.
func platformSecrets(h *platformHarness, databaseURL string) {
	cursor := make([]byte, 32)
	_, _ = rand.Read(cursor)
	h.t.Setenv("DATABASE_URL", databaseURL)
	h.t.Setenv("REDIS_URL", fixtureEnv("SOCIAL_FIXTURE_REDIS_URL", "redis://:kindle_fixture_password@127.0.0.1:26379/11"))
	h.t.Setenv("IDENTITY_ASSERTION_SIGNING_KEY", h.pki.socialKey.StdBase64)
	h.t.Setenv("SOCIAL_CURSOR_KEY", base64.RawURLEncoding.EncodeToString(cursor))
	h.t.Setenv("CHAT_ADMIN_API_KEY", platformAdminKey)
}

func newPlatformHarness(t *testing.T) *platformHarness {
	t.Helper()
	ctx := context.Background()
	db, conn, pool, token, creator := platformDatabase(t)
	h := &platformHarness{devHarness: devHarness{t: t, dbConn: conn, pool: pool, token: token, creator: creator}, db: db}
	h.pki = newPlatformPKI(t)
	h.wire = newIdentityWire(t, h.pki)
	// The platform runtime logs in as chat_runtime.
	platformSecrets(h, db.Runtimes["chat_runtime"].DSN)
	cfg := platformConfig(h, db)
	app, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("boot platform app: %v", err)
	}
	if app.platform == nil || app.connected != nil || app.dev == nil || app.private == nil || app.PrivateRouter == nil {
		t.Fatalf("platform runtime not selected: platform=%v connected=%v private=%v", app.platform != nil, app.connected != nil, app.private != nil)
	}
	h.app = app
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = app.Hub.Run(runCtx); done <- struct{}{} }()
	go func() { _ = app.dev.Run(runCtx); done <- struct{}{} }()
	// The private mTLS listener (the gateway's path to the social API) and
	// the plain listener (health and metrics only).
	h.server = httptest.NewUnstartedServer(app.PrivateRouter)
	h.server.TLS = app.private.tls
	h.server.StartTLS()
	h.client = h.pki.mtlsClient(h.pki.gatewayClient)
	h.plain = httptest.NewServer(app.Router)
	t.Cleanup(func() {
		h.plain.Close()
		h.server.Close()
		cancel()
		<-done
		<-done
		app.Close()
	})
	return h
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func with(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestPlatformRuntimeVerifiesIdentityAtGateway(t *testing.T) {
	h := newPlatformHarness(t)
	alice, bob := addr(0xa11ce), addr(0xb0b)
	h.wire.session("Bearer alice-token", alice)
	h.wire.session("Bearer bob-token", bob)

	if r := h.do("GET", "/health/ready", "", nil, nil); r.status != 200 {
		t.Fatalf("readiness %d %s", r.status, r.body)
	}
	h.privateIngress()

	// Anonymous reads are served; anonymous writes are refused, and a
	// client-supplied X-Actor-Wallet is never identity.
	h.expect(h.do("GET", "/v2/pools/"+h.pool+"/comments", "", nil, nil), 200, "CommentsListResponse")
	callout := map[string]any{"poolAddress": h.pool, "body": "accumulating here", "direction": "bullish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}
	if r := h.expect(h.do("POST", "/v2/callouts", bob, callout, nil), 401, "Error"); r["code"] != "AUTH_EXPIRED" {
		t.Fatalf("spoofed actor without credential: %v", r)
	}
	if r := h.do("PUT", "/follow/"+alice, bob, nil, nil); r.status != 401 {
		t.Fatalf("legacy write with spoofed actor: %d %s", r.status, r.body)
	}

	// A verified bearer is the actor, whatever X-Actor-Wallet claims.
	created := data(h.expect(h.do("POST", "/v2/callouts", bob, callout, bearer("alice-token")), 201, "CalloutCreateResponse"))["callout"].(map[string]any)
	if created["authorWallet"] != alice {
		t.Fatalf("callout author = %v, want verified %s", created["authorWallet"], alice)
	}
	comment := map[string]any{"body": "first", "clientCommentId": uuid.NewString()}
	bobComment := data(h.expect(h.do("POST", "/v2/pools/"+h.pool+"/comments", "", comment, bearer("bob-token")), 201, "CommentCreateResponse"))["comment"].(map[string]any)["commentId"].(string)
	if got := items(h.expect(h.do("GET", "/v2/pools/"+h.pool+"/comments", "", nil, nil), 200, "CommentsListResponse")); len(got) != 1 {
		t.Fatalf("comments = %v", got)
	}

	// Legacy and /v2/social routes share the verified actor and one graph.
	if r := h.do("PUT", "/follow/"+bob, "", nil, bearer("alice-token")); r.status/100 != 2 {
		t.Fatalf("legacy follow %d %s", r.status, r.body)
	}
	var stats struct {
		Data struct {
			Followers string `json:"followers"`
		} `json:"data"`
	}
	if r := h.do("GET", "/v2/social/users/"+bob+"/follow-stats", "", nil, nil); r.status != 200 || json.Unmarshal(r.body, &stats) != nil || stats.Data.Followers != "1" {
		t.Fatalf("follow stats %d %s", r.status, r.body)
	}

	// Rejected and unverifiable credentials.
	if r := h.expect(h.do("POST", "/v2/callouts", "", callout, bearer("forged")), 401, "Error"); r["code"] != "AUTH_EXPIRED" {
		t.Fatalf("forged bearer: %v", r)
	}
	if r := h.do("POST", "/v2/social/pool/"+h.pool+"/comments", "", comment, with(bearer("forged"), map[string]string{"Idempotency-Key": uuid.NewString()})); r.status != 401 || !strings.Contains(string(r.body), "AUTH_EXPIRED") {
		t.Fatalf("forged bearer on /v2/social: %d %s", r.status, r.body)
	}
	h.wire.set(func(w *identityWire) { w.outage = true })
	if r := h.expect(h.do("GET", "/v2/bounties", "", nil, bearer("alice-token")), 503, "Error"); r["code"] != "DEPENDENCY_UNAVAILABLE" || r["retryable"] != true {
		t.Fatalf("authority outage: %v", r)
	}
	h.wire.set(func(w *identityWire) { w.outage = false })
	// A gateway whose identityPeers do not hold social's assertion key
	// rejects the service: readiness fails and credentials are not verified.
	registered := h.wire.verifier.Key
	h.wire.set(func(w *identityWire) { w.verifier.Key = testpki.Ed25519(t).Private.Public().(ed25519.PublicKey) })
	if r := h.do("GET", "/health/ready", "", nil, nil); r.status == 200 || !strings.Contains(string(r.body), "identity_authority") {
		t.Fatalf("readiness with an unregistered service key %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/v2/bounties", "", nil, bearer("alice-token")); r.status != 401 {
		t.Fatalf("credential verified by a gateway that rejects the service: %d %s", r.status, r.body)
	}
	h.wire.set(func(w *identityWire) { w.verifier.Key = registered })
	if r := h.do("GET", "/health/ready", "", nil, nil); r.status != 200 {
		t.Fatalf("readiness after re-registration %d %s", r.status, r.body)
	}

	// Moderation runs through the safety store. A user report opens a case;
	// only a session holding a moderator grant may list cases or ban, and the
	// legacy admin routes additionally keep CHAT_ADMIN_API_KEY as an outer gate.
	report := map[string]any{"targetType": "comment", "targetId": bobComment, "category": "spam", "description": "spam link"}
	if r := h.do("POST", "/v2/safety/reports", "", report, nil); r.status != 403 {
		t.Fatalf("anonymous report %d %s", r.status, r.body)
	}
	if r := h.do("POST", "/v2/safety/reports", "", report, bearer("alice-token")); r.status != 200 || !strings.Contains(string(r.body), `"PoolID":"`+h.pool+`"`) {
		t.Fatalf("report %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/v2/safety/cases", "", nil, bearer("alice-token")); r.status != 403 {
		t.Fatalf("participant listing cases %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/admin/bans", "", nil, map[string]string{"X-API-Key": platformAdminKey}); r.status != 403 {
		t.Fatalf("admin key without a moderator session %d %s", r.status, r.body)
	}
	moderator := h.wire.session("Bearer mod-token", addr(0x30d))
	if _, err := h.dbConn.Exec(context.Background(), `INSERT INTO chat.safety_moderator_sessions(session_id,account_id,wallet,role,expires_at,step_up_at) VALUES($1,$2,$3,'global_administrator',now()+interval '1 hour',now())`, moderator.SessionID, moderator.AccountID, moderator.Wallet); err != nil {
		t.Fatal(err)
	}
	if r := h.do("GET", "/v2/safety/cases", "", nil, bearer("mod-token")); r.status != 200 || !strings.Contains(string(r.body), bobComment) {
		t.Fatalf("moderator cases %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/admin/bans", "", nil, with(bearer("mod-token"), map[string]string{"X-API-Key": "wrong"})); r.status != 403 {
		t.Fatalf("admin with wrong key %d", r.status)
	}
	ban := map[string]any{"wallet": bob, "pool_address": h.pool, "reason": "spam"}
	if r := h.do("POST", "/admin/bans", "", ban, with(bearer("mod-token"), map[string]string{"X-API-Key": platformAdminKey})); r.status != 200 || !strings.Contains(string(r.body), `"banned_by":"`+moderator.Wallet+`"`) {
		t.Fatalf("admin ban %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/admin/bans", "", nil, with(bearer("mod-token"), map[string]string{"X-API-Key": platformAdminKey})); r.status != 200 || !strings.Contains(string(r.body), bob) {
		t.Fatalf("admin bans %d %s", r.status, r.body)
	}
	// The pool ban is enforced on the banned wallet's writes in that pool.
	if r := h.expect(h.do("POST", "/v2/pools/"+h.pool+"/comments", "", map[string]any{"body": "again", "clientCommentId": uuid.NewString()}, bearer("bob-token")), 403, "Error"); r["code"] != "SCOPE_DENIED" {
		t.Fatalf("banned comment: %v", r)
	}

	// A revoked session is refused.
	h.wire.set(func(w *identityWire) {
		w.revoked[w.sessions["Bearer alice-token"].SessionID] = true
	})
	h.expect(h.do("POST", "/v2/callouts", "", map[string]any{"poolAddress": h.pool, "body": "x", "direction": "bearish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}, bearer("alice-token")), 401, "Error")
}

func TestPlatformChatStreamUsesSingleUseTickets(t *testing.T) {
	h := newPlatformHarness(t)
	alice, bob := addr(0xa11ce), addr(0xb0b)
	ap := h.wire.session("Bearer alice-token", alice)
	bp := h.wire.session("Bearer bob-token", bob)
	topics := []string{"chat.pool." + h.pool}
	wsURL := "wss" + strings.TrimPrefix(h.server.URL, "https") + "/v2/stream?topics=" + topics[0]
	gatewayTLS := h.client.Transport.(*http.Transport).TLSClientConfig
	dial := func(ticket, token string) (*websocket.Conn, *http.Response, error) {
		d := websocket.Dialer{Subprotocols: []string{"sidiora.v2", "ticket." + ticket}, HandshakeTimeout: 5 * time.Second, TLSClientConfig: gatewayTLS}
		header := http.Header{"Origin": {platformOrigin}}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		return d.Dial(wsURL, header)
	}

	// No ticket, or a ticket for another session's credential, is refused.
	if _, res, err := (&websocket.Dialer{TLSClientConfig: gatewayTLS, HandshakeTimeout: 5 * time.Second}).Dial(wsURL, http.Header{"Origin": {platformOrigin}, "X-Actor-Wallet": {alice}}); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("upgrade without ticket: %v %v", err, res)
	}
	if _, res, err := dial(h.wire.ticket(ap, topics), "bob-token"); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("ticket presented with another session: %v %v", err, res)
	}

	aliceTicket := h.wire.ticket(ap, topics)
	ac, _, err := dial(aliceTicket, "alice-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	if ac.Subprotocol() != "sidiora.v2" {
		t.Fatalf("subprotocol %q", ac.Subprotocol())
	}
	if accepted := readFrame(t, ac, "session_accepted"); accepted["protocol_version"] != float64(2) {
		t.Fatalf("session_accepted = %v", accepted)
	}
	// The ticket is single-use.
	if _, res, err := dial(aliceTicket, "alice-token"); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("ticket replay: %v %v", err, res)
	}
	bc, _, err := dial(h.wire.ticket(bp, topics), "bob-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bc.Close() })
	readFrame(t, bc, "session_accepted")
	// A cross-site browser socket carries no session credential: the
	// single-use ticket alone admits it, once.
	ticketOnly := h.wire.ticket(bp, topics)
	tc, _, err := dial(ticketOnly, "")
	if err != nil {
		t.Fatalf("ticket-only upgrade: %v", err)
	}
	t.Cleanup(func() { _ = tc.Close() })
	readFrame(t, tc, "session_accepted")
	if _, res, err := dial(ticketOnly, ""); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("ticket-only replay: %v %v", err, res)
	}
	if _, res, err := dial(strings.Repeat("ab", 32), ""); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("unknown ticket admitted: %v %v", err, res)
	}
	if err := tc.WriteJSON(map[string]any{"type": "join_pool", "pool_address": h.pool}); err != nil {
		t.Fatal(err)
	}
	readFrame(t, tc, "joined_pool")
	for _, conn := range []*websocket.Conn{ac, bc} {
		if err := conn.WriteJSON(map[string]any{"type": "join_pool", "pool_address": h.pool}); err != nil {
			t.Fatal(err)
		}
		readFrame(t, conn, "joined_pool")
	}
	if err := ac.WriteJSON(map[string]any{"type": "pool_message", "pool_address": h.pool, "content": "gm platform", "client_message_id": "p-1"}); err != nil {
		t.Fatal(err)
	}
	if persisted := readFrame(t, ac, "message_persisted"); persisted["sequence"] != "1" {
		t.Fatalf("persisted = %v", persisted)
	}
	if delivered := readFrame(t, bc, "pool_message"); delivered["content"] != "gm platform" || delivered["sender"] != alice {
		t.Fatalf("fan-out = %v", delivered)
	}
	// History through the verified bearer.
	if r := h.do("GET", "/v2/chat/history?conversation_id=pool:"+h.pool, "", nil, bearer("bob-token")); r.status != 200 || !strings.Contains(string(r.body), "gm platform") {
		t.Fatalf("history %d %s", r.status, r.body)
	}

	// Revoking alice's session closes her stream once the bounded success
	// cache lapses: her next frame is refused with 4001.
	h.wire.set(func(w *identityWire) { w.revoked[ap.SessionID] = true })
	time.Sleep(platformValidationTTL + 500*time.Millisecond)
	_ = ac.WriteJSON(map[string]any{"type": "pool_message", "pool_address": h.pool, "content": "after revoke", "client_message_id": "p-2"})
	_ = ac.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err = ac.ReadMessage(); err != nil {
			break
		}
	}
	if !websocket.IsCloseError(err, 4001) {
		t.Fatalf("revoked stream: %v", err)
	}
}

// privateIngress checks the transport boundaries of the platform runtime: the
// plain listener serves only health and metrics, the private listener admits
// the social API only to the pinned gateway client certificate and the
// ranking-visibility authority only to the listed service peers, and a client
// of another CA cannot complete the handshake.
func (h *platformHarness) privateIngress() {
	t := h.t
	t.Helper()
	get := func(client *http.Client, url string) (int, string) {
		res, err := client.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	if status, _ := get(http.DefaultClient, h.plain.URL+"/health/live"); status != 200 {
		t.Fatalf("plain health %d", status)
	}
	if status, body := get(http.DefaultClient, h.plain.URL+"/v2/pools/"+h.pool+"/comments"); status != 403 || !strings.Contains(body, "TRUSTED_INGRESS_REQUIRED") {
		t.Fatalf("plain listener served the API: %d %s", status, body)
	}
	// Every gateway-proxied request arrives from the gateway's address; the
	// gateway limits per client at its edge, so the per-peer bucket must not
	// throttle all users together on the private listener.
	for i := 0; i < rateLimitMax+5; i++ {
		if status, body := get(h.client, h.server.URL+"/v2/profiles/"+h.creator+"/follow-stats"); status != 200 {
			t.Fatalf("gateway ingress request %d: %d %s", i, status, body)
		}
	}
	ranking := h.pki.mtlsClient(h.pki.rankingClient)
	if status, body := get(ranking, h.server.URL+"/v2/pools/"+h.pool+"/comments"); status != 403 || !strings.Contains(body, "TRUSTED_INGRESS_REQUIRED") {
		t.Fatalf("non-gateway peer reached the API: %d %s", status, body)
	}
	if status, body := get(h.client, h.server.URL+"/internal/v2/social/ranking-visibility"); status != 403 || !strings.Contains(body, "SERVICE_AUTHORITY_REQUIRED") {
		t.Fatalf("gateway reached the ranking authority: %d %s", status, body)
	}
	if _, err := h.pki.mtlsClient(h.pki.foreignGatewayClient).Get(h.server.URL + "/health/live"); err == nil {
		t.Fatal("client certificate of a foreign CA accepted")
	}
	if _, err := (&http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: h.pki.ca.Roots()}}}).Get(h.server.URL + "/health/live"); err == nil {
		t.Fatal("private listener accepted a client without a certificate")
	}
	var snapshot struct {
		Revision   string          `json:"revision"`
		ObservedAt time.Time       `json:"observedAt"`
		Complete   bool            `json:"complete"`
		Pools      map[string]bool `json:"pools"`
	}
	status, body := get(ranking, h.server.URL+"/internal/v2/social/ranking-visibility")
	if status != 200 || json.Unmarshal([]byte(body), &snapshot) != nil || !snapshot.Complete || len(snapshot.Revision) != 64 || snapshot.ObservedAt.IsZero() || len(snapshot.Pools) != 1 || !snapshot.Pools[h.pool] {
		t.Fatalf("ranking visibility %d %s", status, body)
	}
	// Erasing the creator's account hides the pool and moves the revision.
	if _, err := h.dbConn.Exec(context.Background(), `INSERT INTO chat.interaction_erased_wallets(wallet,deletion_case_id,policy_version) VALUES($1,$2,'test')`, h.creator, uuid.New()); err != nil {
		t.Fatal(err)
	}
	before := snapshot.Revision
	status, body = get(ranking, h.server.URL+"/internal/v2/social/ranking-visibility")
	if status != 200 || json.Unmarshal([]byte(body), &snapshot) != nil || snapshot.Pools[h.pool] || snapshot.Revision == before {
		t.Fatalf("ranking visibility after erasure %d %s", status, body)
	}
	if _, err := h.dbConn.Exec(context.Background(), `DELETE FROM chat.interaction_erased_wallets WHERE wallet=$1`, h.creator); err != nil {
		t.Fatal(err)
	}
}

// TestPlatformWritesAsLeastPrivilegeRuntime drives the platform write paths
// over the coordinator-registered tables as chat_runtime: the v2 bounty
// lifecycle and reports (migration 370002, row locks and column updates),
// the legacy comment feed and likes (baselined media/social 00001) with a
// creator-authorized delete resolved from the production indexer's
// canonical creations, and the legacy read routes.
func TestPlatformWritesAsLeastPrivilegeRuntime(t *testing.T) {
	h := newPlatformHarness(t)
	alice, bob := addr(0xa11ce), addr(0xb0b)
	h.wire.session("Bearer alice-token", alice)
	h.wire.session("Bearer bob-token", bob)
	h.wire.session("Bearer creator-token", h.creator)

	bounty := map[string]any{"title": "Best meme", "description": "Make the best meme for the coin.", "assetAddress": strings.ToLower(deployment.SettlementAddress), "budgetRaw": "5000000", "poolAddress": h.pool}
	made := data(h.expect(h.do("POST", "/v2/bounties", "", bounty, with(bearer("alice-token"), map[string]string{"Idempotency-Key": uuid.NewString()})), 201, "BountyCreateResponse"))["bounty"].(map[string]any)
	id := made["bountyId"].(string)
	// Funding is an on-chain escrow step; the fixture records the funded
	// state as the escrow observer would.
	if _, err := h.dbConn.Exec(context.Background(), `UPDATE chat.v2_bounties SET state='open' WHERE bounty_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	sub := data(h.expect(h.do("POST", "/v2/bounties/"+id+"/submissions", "", map[string]any{"body": "here is mine", "clientSubmissionId": uuid.NewString(), "evidenceUrls": []string{"https://example.org/meme.png"}}, bearer("bob-token")), 201, "BountySubmitResponse"))["submission"].(map[string]any)
	accepted := data(h.expect(h.do("POST", "/v2/bounties/"+id+"/submissions/"+sub["submissionId"].(string)+":accept", "", map[string]any{"awardRaw": "2000000", "expectedBountyRevision": "1"}, with(bearer("alice-token"), map[string]string{"Idempotency-Key": uuid.NewString()})), 201, "BountyAcceptResponse"))
	if accepted["submission"].(map[string]any)["state"] != "accepted" {
		t.Fatalf("accept = %v", accepted)
	}
	if got := data(h.expect(h.do("GET", "/v2/bounties/"+id, "", nil, nil), 200, "BountyGetResponse"))["bounty"].(map[string]any); got["state"] != "reviewing" {
		t.Fatalf("after accept = %v", got)
	}
	h.expect(h.do("GET", "/v2/bounties/stats", "", nil, nil), 200, "BountiesStatsResponse")
	h.expect(h.do("POST", "/v2/reports", "", map[string]any{"targetType": "token", "targetId": h.token, "reason": "spam", "evidence": ""}, with(bearer("bob-token"), map[string]string{"Idempotency-Key": uuid.NewString()})), 201, "")

	// Legacy comment feed: create, edit, like, unlike, creator delete.
	var created struct {
		ID string `json:"id"`
	}
	r := h.do("POST", "/pool/"+h.pool+"/comments", "", map[string]any{"content": "legacy first"}, bearer("bob-token"))
	if r.status != 200 || json.Unmarshal(r.body, &created) != nil || created.ID == "" {
		t.Fatalf("legacy comment %d %s", r.status, r.body)
	}
	for _, step := range []struct {
		method, path, token string
		body                any
	}{
		{"PATCH", "/comments/" + created.ID, "bob-token", map[string]any{"content": "legacy edited"}},
		{"PUT", "/comments/" + created.ID + "/like", "alice-token", nil},
		{"DELETE", "/comments/" + created.ID + "/like", "alice-token", nil},
		{"PUT", "/comments/" + created.ID + "/like", "alice-token", nil},
	} {
		if r := h.do(step.method, step.path, "", step.body, bearer(step.token)); r.status != 200 {
			t.Fatalf("%s %s: %d %s", step.method, step.path, r.status, r.body)
		}
	}
	if r := h.do("GET", "/pool/"+h.pool+"/comments", "", nil, nil); r.status != 200 || !strings.Contains(string(r.body), "legacy edited") || !strings.Contains(string(r.body), `"like_count":1`) {
		t.Fatalf("legacy comments %d %s", r.status, r.body)
	}
	if r := h.do("DELETE", "/comments/"+created.ID, "", nil, bearer("alice-token")); r.status != 403 {
		t.Fatalf("delete by a non-creator %d %s", r.status, r.body)
	}
	if r := h.do("DELETE", "/comments/"+created.ID, "", nil, bearer("creator-token")); r.status != 200 {
		t.Fatalf("delete by the pool creator %d %s", r.status, r.body)
	}
	for _, path := range []string{"/pool/" + h.pool + "/messages", "/users/" + bob + "/followers"} {
		if r := h.do("GET", path, "", nil, nil); r.status != 200 {
			t.Fatalf("GET %s: %d %s", path, r.status, r.body)
		}
	}
	if r := h.do("GET", "/dm/conversations", "", nil, bearer("alice-token")); r.status != 200 {
		t.Fatalf("dm conversations %d %s", r.status, r.body)
	}
	if r := h.do("PUT", "/follow/"+bob, "", nil, bearer("alice-token")); r.status/100 != 2 {
		t.Fatalf("follow %d %s", r.status, r.body)
	}
	if r := h.do("DELETE", "/follow/"+bob, "", nil, bearer("alice-token")); r.status/100 != 2 {
		t.Fatalf("unfollow %d %s", r.status, r.body)
	}
}

// TestPlatformBootVerifiesCoordinatorSchemaWithoutMigrating: the platform
// runtime never migrates. It boots as chat_runtime on the coordinator-owned
// schema only when the coordinator's ExpectedSchema document holds exactly
// (applied migrations, catalog fingerprints, recovery epoch and the reviewed
// privileges, no more and no less), and refuses an unmigrated database even
// when its credential could have migrated it.
func TestPlatformBootVerifiesCoordinatorSchemaWithoutMigrating(t *testing.T) {
	h := newPlatformHarness(t)
	ctx := context.Background()
	if r := h.do("GET", "/health/ready", "", nil, nil); r.status != 200 {
		t.Fatalf("readiness %d %s", r.status, r.body)
	}
	var goose int
	if err := h.dbConn.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relname='goose_db_version'`).Scan(&goose); err != nil || goose != 0 {
		t.Fatalf("platform boot ran goose: %d tables (%v)", goose, err)
	}
	boot := func(databaseURL string, mutate func(*config.Config)) error {
		t.Helper()
		platformSecrets(h, databaseURL)
		cfg := platformConfig(h, h.db)
		mutate(&cfg)
		a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil {
			a.Close()
		}
		return err
	}
	runtimeDSN := h.db.Runtimes["chat_runtime"].DSN
	var doc map[string]any
	if err := json.Unmarshal(h.db.Runtimes["chat_runtime"].ExpectedSchema, &doc); err != nil {
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
		"no document":     {func(c *config.Config) { c.ExpectedSchemaFile = "" }, "EXPECTED_SCHEMA_FILE"},
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
	adminDSN := fixtureEnv("SOCIAL_FIXTURE_POSTGRES_DSN", "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	empty := "social_unmigrated_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{empty}.Sanitize())
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), adminDSN)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{empty}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	emptyURL, err := url.Parse(adminDSN)
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
