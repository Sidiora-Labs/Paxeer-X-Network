package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIdentityOwnerHTTPLogoutAllAndCookieCSRF(t *testing.T) {
	f := ownerHTTPFixture(t)
	first, p := f.loginHTTP(t)
	second, _ := f.loginHTTP(t)
	ticket, err := f.s.IssueTicket(f.ctx, p, "https://kindle.test", "/private/socket")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:logout-all", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://attacker.test")
	req.AddCookie(&http.Cookie{Name: "__Host-kindle_access", Value: first.AccessToken})
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("cross-origin %d", response.StatusCode)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, f.s.tokens, first.AccessToken); err != nil {
		t.Fatal("denied CSRF mutated session", err)
	}
	status, body, _ := f.request(t, "POST", "/v2/auth/sessions:logout-all", second.AccessToken, "", map[string]any{})
	if status != 204 || len(body) != 0 {
		t.Fatalf("logout all %d %s", status, body)
	}
	start := time.Now()
	for _, token := range []string{first.AccessToken, second.AccessToken} {
		if _, err = f.s.AuthenticateJWT(f.ctx, f.s.tokens, token); err == nil {
			t.Fatal("revoked token accepted")
		}
	}
	if _, err = f.s.ConsumeTicket(f.ctx, ticket, "https://kindle.test", "/private/socket"); err == nil {
		t.Fatal("revoked ticket accepted")
	}
	if err = f.s.CheckSocket(f.ctx, p); err == nil {
		t.Fatal("revoked socket authority accepted")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("revocation enforcement exceeded five seconds")
	}
}
func TestIdentityOwnerHTTPLastWalletAndReauthentication(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, p := f.loginHTTP(t)
	var link string
	if err := f.pool.QueryRow(f.ctx, `SELECT link_id::text FROM identity_wallet_links WHERE account_id=$1 AND wallet=$2 AND state='verified'`, p.accountID, p.wallet).Scan(&link); err != nil {
		t.Fatal(err)
	}
	status, raw, _ := f.request(t, "POST", "/v2/auth/reauth/challenges", credentials.AccessToken, "", map[string]any{"action": "wallet_unlink"})
	if status != 201 {
		t.Fatalf("step up %d %s", status, raw)
	}
	var challenge Envelope[struct {
		Challenge Challenge `json:"challenge"`
	}]
	if err := json.Unmarshal(raw, &challenge); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = f.request(t, "POST", "/v2/auth/reauth:verify", credentials.AccessToken, "", map[string]any{"action": "wallet_unlink", "challengeId": challenge.Data.Challenge.ChallengeID, "proof": proofIdentity(t, f.key, challenge.Data.Challenge)})
	if status != 200 {
		t.Fatalf("reauth %d %s", status, raw)
	}
	var receipt Envelope[struct {
		ReauthProof string `json:"reauthProof"`
		ExpiresAt   string `json:"expiresAt"`
	}]
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = f.request(t, "DELETE", "/v2/me/wallet-links/"+link, credentials.AccessToken, uuid.NewString(), map[string]any{"reauthProof": receipt.Data.ReauthProof})
	if status != 409 {
		t.Fatalf("last wallet %d %s", status, raw)
	}
	var consumed *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT consumed_at FROM identity_reauth WHERE proof_hash=$1`, digest(receipt.Data.ReauthProof)).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("failed unlink consumed receipt %v %v", consumed, err)
	}
	f.now.Add(int64((5*time.Minute + time.Second) / time.Millisecond))
	status, raw, _ = f.request(t, "POST", "/v2/me/wallet-links/challenges", credentials.AccessToken, uuid.NewString(), WalletLinkChallengeRequest{"0x2222222222222222222222222222222222222222", "125", "private"})
	if status != 401 {
		t.Fatalf("stale reauth %d %s", status, raw)
	}
}
func TestIdentityOwnerHTTPRejectsReusableQueryAndBodyPathOverride(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, _ := f.loginHTTP(t)
	status, body, _ := f.request(t, "GET", "/v2/auth/sessions?token="+credentials.AccessToken, credentials.AccessToken, "", nil)
	if status != 401 {
		t.Fatalf("query %d %s", status, body)
	}
	status, body, _ = f.request(t, "POST", "/v2/me/wallet-links/"+uuid.NewString()+":verify", credentials.AccessToken, uuid.NewString(), map[string]any{"linkId": uuid.NewString(), "challengeId": uuid.NewString(), "proof": WalletProof{}})
	if status != 422 {
		t.Fatalf("path override %d %s", status, body)
	}
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	if _, err := f.s.AuthenticateJWT(ctx, f.s.tokens, credentials.AccessToken); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityOwnerHTTPRejectsChangedChallengeBytes(t *testing.T) {
	f := ownerHTTPFixture(t)
	challenge := f.challenge(t, f.wallet)
	for _, change := range []struct{ From, To string }{{"kindle.test wants", "evil.test wants"}, {"URI: https://kindle.test/auth", "URI: https://evil.test/auth"}, {"Chain ID: 125", "Chain ID: 1"}, {"Issued At:", "Issued At: 2030-01-01T00:00:00.000Z #"}} {
		altered := challenge
		altered.Message = strings.Replace(challenge.Message, change.From, change.To, 1)
		status, body, _ := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, altered), "changed"})
		if status != 401 {
			t.Fatalf("changed challenge %d %s", status, body)
		}
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("changed challenge session count%d %v", count, err)
	}
}

func TestIdentityOwnerHTTPUnlinkAfterVerifiedReplacement(t *testing.T) {
	f := ownerHTTPFixture(t)
	first, p := f.loginHTTP(t)
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	walletB := strings.ToLower(crypto.PubkeyToAddress(keyB.PublicKey).Hex())
	status, raw, _ := f.request(t, "POST", "/v2/me/wallet-links/challenges", first.AccessToken, uuid.NewString(), WalletLinkChallengeRequest{walletB, "125", "private"})
	if status != 201 {
		t.Fatalf("challenge %d %s", status, raw)
	}
	var link Envelope[LinkChallengeData]
	if err = json.Unmarshal(raw, &link); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = f.request(t, "POST", "/v2/me/wallet-links/"+link.Data.LinkID+":verify", first.AccessToken, uuid.NewString(), map[string]any{"challengeId": link.Data.Challenge.ChallengeID, "proof": proofIdentity(t, keyB, link.Data.Challenge)})
	if status != 200 {
		t.Fatalf("link %d %s", status, raw)
	}
	challenge := f.challenge(t, walletB)
	second, err := f.s.Login(f.ctx, LoginRequest{challenge.ChallengeID, proofIdentity(t, keyB, challenge), "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	var oldLink string
	var originalAt time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT link_id::text,linked_at FROM identity_wallet_links WHERE account_id=$1 AND wallet=$2 AND state='verified'`, p.accountID, p.wallet).Scan(&oldLink, &originalAt); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = f.request(t, "POST", "/v2/auth/reauth/challenges", second.AccessToken, "", map[string]any{"action": "wallet_unlink"})
	if status != 201 {
		t.Fatalf("step challenge %d %s", status, raw)
	}
	var step Envelope[struct {
		Challenge Challenge `json:"challenge"`
	}]
	if err = json.Unmarshal(raw, &step); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = f.request(t, "POST", "/v2/auth/reauth:verify", second.AccessToken, "", map[string]any{"action": "wallet_unlink", "challengeId": step.Data.Challenge.ChallengeID, "proof": proofIdentity(t, keyB, step.Data.Challenge)})
	if status != 200 {
		t.Fatalf("step verify %d %s", status, raw)
	}
	var reauth Envelope[struct {
		ReauthProof string `json:"reauthProof"`
	}]
	if err = json.Unmarshal(raw, &reauth); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	body := map[string]any{"reauthProof": reauth.Data.ReauthProof}
	path := "/v2/me/wallet-links/" + oldLink
	status, raw, _ = f.request(t, "DELETE", path, second.AccessToken, key, body)
	if status != 204 || len(raw) != 0 {
		t.Fatalf("unlink %d %s", status, raw)
	}
	status, raw, _ = f.request(t, "DELETE", path, second.AccessToken, key, body)
	if status != 204 || len(raw) != 0 {
		t.Fatalf("unlink retry %d %s", status, raw)
	}
	var state string
	var retainedAt time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT state,linked_at FROM identity_wallet_links WHERE link_id=$1`, oldLink).Scan(&state, &retainedAt); err != nil || state != "revoked" || !retainedAt.Equal(originalAt) {
		t.Fatalf("historical verified proof state=%s time=%v original=%v err=%v", state, retainedAt, originalAt, err)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, f.s.tokens, first.AccessToken); err == nil {
		t.Fatal("unlinked wallet retained session")
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, f.s.tokens, second.AccessToken); err != nil {
		t.Fatal("replacement session lost authority", err)
	}
}

func TestIdentityOwnerRealPrivateSocketsRevokeWithinFiveSeconds(t *testing.T) {
	f := ownerHTTPFixture(t)
	first, p1 := f.loginHTTP(t)
	second, p2 := f.loginHTTP(t)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://kindle.test" }}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	socketServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := f.s.ConsumeTicket(r.Context(), r.URL.Query().Get("ticket"), r.Header.Get("Origin"), "/owner/private")
		if err != nil {
			f.s.writeIdentityError(w, err)
			return
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		f.s.EnforceWebSocket(ctx, p, connection)
	}))
	t.Cleanup(socketServer.Close)
	url := "ws" + strings.TrimPrefix(socketServer.URL, "http") + "/owner/private?ticket="
	connections := []*websocket.Conn{}
	for _, p := range []Principal{p1, p2} {
		ticket, err := f.s.IssueTicket(f.ctx, p, "https://kindle.test", "/owner/private")
		if err != nil {
			t.Fatal(err)
		}
		conn, _, err := websocket.DefaultDialer.Dial(url+ticket, http.Header{"Origin": []string{"https://kindle.test"}})
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		t.Cleanup(func() { conn.Close() })
	}
	status, raw, _ := f.request(t, "POST", "/v2/auth/sessions:logout-all", first.AccessToken, "", map[string]any{})
	if status != 204 {
		t.Fatalf("logout %d %s", status, raw)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, conn := range connections {
		conn.SetReadDeadline(deadline)
		_, _, err := conn.ReadMessage()
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatalf("private socket remained authorized: %v", err)
		}
	}
	if _, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, second.AccessToken); err == nil {
		t.Fatal("second device remained authorized")
	}
}

func ownerCredentials(t *testing.T, f *identityHTTPFixture) (Credentials, Principal) {
	t.Helper()
	c := f.challenge(t, f.wallet)
	credentials, err := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "adversarial owner"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, credentials.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return credentials, p
}

func TestIdentityOwnerInterruptedRefreshRestoresOriginalFamily(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, _ := ownerCredentials(t, f)
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE identity_sessions RENAME TO identity_sessions_interrupted`); err != nil {
		t.Fatal(err)
	}
	_, failure := f.s.Refresh(f.ctx, credentials.RefreshSecret)
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE identity_sessions_interrupted RENAME TO identity_sessions`); err != nil {
		t.Fatal(err)
	}
	requireCode(t, failure, "DEPENDENCY_UNAVAILABLE")
	var consumed *time.Time
	var accessExpires time.Time
	var family int
	if err := f.pool.QueryRow(f.ctx, `SELECT consumed_at FROM identity_refresh WHERE refresh_hash=$1`, digest(credentials.RefreshSecret)).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("interrupted refresh consumed original: %v %v", consumed, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT access_expires_at FROM identity_sessions WHERE session_id=$1`, credentials.Session.SessionID).Scan(&accessExpires); err != nil || !accessExpires.Equal(credentials.Session.AccessExpiresAt) {
		t.Fatalf("interrupted refresh extended access: %v %v", accessExpires, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_refresh WHERE session_id=$1`, credentials.Session.SessionID).Scan(&family); err != nil || family != 1 {
		t.Fatalf("interrupted refresh changed family: %d %v", family, err)
	}
	rotated, err := f.s.Refresh(f.ctx, credentials.RefreshSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, f.s.tokens, rotated.AccessToken); err != nil {
		t.Fatal("recovery lost session", err)
	}
}

func TestIdentityOwnerOutboxInterruptionRollsBackLoginAndRevocation(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, p := ownerCredentials(t, f)
	challenge := f.challenge(t, f.wallet)
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE delivery_outbox RENAME TO delivery_outbox_interrupted`); err != nil {
		t.Fatal(err)
	}
	status, raw, headers := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "interrupted"})
	logoutStatus, logoutRaw, _ := f.request(t, "POST", "/v2/auth/sessions:logout-all", credentials.AccessToken, "", map[string]any{})
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE delivery_outbox_interrupted RENAME TO delivery_outbox`); err != nil {
		t.Fatal(err)
	}
	if status != 503 || len(headers.Values("Set-Cookie")) != 0 || logoutStatus != 503 {
		t.Fatalf("interrupted mutation acknowledged: login=%d logout=%d %s %s", status, logoutStatus, raw, logoutRaw)
	}
	var consumed *time.Time
	var epoch int64
	var sessions, events int
	if err := f.pool.QueryRow(f.ctx, `SELECT consumed_at FROM identity_challenges WHERE challenge_id=$1`, challenge.ChallengeID).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("login consumed on failed commit: %v %v", consumed, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1`, p.AccountID()).Scan(&epoch); err != nil || epoch != p.AuthEpoch() {
		t.Fatalf("revocation epoch partially committed: %d %v", epoch, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("partial login session: %d %v", sessions, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM delivery_outbox`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("partial outbox: %d %v", events, err)
	}
	if _, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, credentials.AccessToken); err != nil {
		t.Fatal("acknowledged original session lost", err)
	}
	status, raw, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "recovered"})
	if status != 200 {
		t.Fatalf("recovery %d %s", status, raw)
	}
	status, raw, _ = f.request(t, "POST", "/v2/auth/sessions:logout-all", credentials.AccessToken, "", map[string]any{})
	if status != 204 {
		t.Fatalf("revocation recovery %d %s", status, raw)
	}
}

func TestIdentityOwnerRefreshRejectsAmbiguousCredentialsAndRotatesCSRF(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, _ := ownerCredentials(t, f)
	for _, variant := range []string{"refresh_cookie", "origin", "csrf", "authorization"} {
		req, err := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:refresh", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://kindle.test")
		req.Header.Set("X-CSRF-Token", credentials.CSRFSecret)
		req.AddCookie(SessionCookie("__Host-kindle_refresh", credentials.RefreshSecret, credentials.Session.RefreshExpiresAt))
		switch variant {
		case "refresh_cookie":
			req.AddCookie(SessionCookie("__Host-kindle_refresh", credentials.RefreshSecret, credentials.Session.RefreshExpiresAt))
		case "origin":
			req.Header.Add("Origin", "https://kindle.test")
		case "csrf":
			req.Header.Add("X-CSRF-Token", credentials.CSRFSecret)
		case "authorization":
			req.Header.Add("Authorization", "Bearer "+credentials.AccessToken)
			req.Header.Add("Authorization", "Bearer "+credentials.AccessToken)
		}
		response, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 401 || len(response.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("ambiguous %s status=%d", variant, response.StatusCode)
		}
	}
	rotated, err := f.s.Refresh(f.ctx, credentials.RefreshSecret)
	if err != nil {
		t.Fatal("rejected transport consumed refresh", err)
	}
	req, err := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:logout-all", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://kindle.test")
	req.Header.Set("X-CSRF-Token", credentials.CSRFSecret)
	req.AddCookie(SessionCookie("__Host-kindle_access", rotated.AccessToken, rotated.Session.AccessExpiresAt))
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("old CSRF accepted after rotation: %d", response.StatusCode)
	}
	req, err = http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:logout-all", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://kindle.test")
	req.Header.Set("X-CSRF-Token", rotated.CSRFSecret)
	req.AddCookie(SessionCookie("__Host-kindle_access", rotated.AccessToken, rotated.Session.AccessExpiresAt))
	response, err = f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("current same-origin CSRF rejected: %d", response.StatusCode)
	}
}

func TestIdentityOwnerServiceBoundaryRevocationAndOwnership(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, p := ownerCredentials(t, f)
	authority := sharedauth.HTTPIdentityAuthority{Endpoint: f.server.URL, ServiceToken: strings.Repeat("t", 32), Client: f.server.Client()}
	request := httptest.NewRequest("POST", "https://kindle.test/profile", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("Origin", "https://kindle.test")
	identity, err := authority.AuthenticateRequest(f.ctx, request)
	if err != nil || identity.AccountID != p.AccountID() || identity.SessionID != p.SessionID() || identity.Wallet != p.Wallet() {
		t.Fatalf("real identity service authority: %v %v", identity, err)
	}
	forged := p
	forged.wallet = "0x2222222222222222222222222222222222222222"
	requireCode(t, f.s.CheckSocket(f.ctx, forged), "AUTH_EXPIRED")
	stream := sharedauth.HTTPStreamAuthority(authority)
	other := identity.StreamPrincipal()
	other.AccountID = uuid.NewString()
	if err = stream.CheckStreamSession(f.ctx, other); err == nil {
		t.Fatal("service boundary accepted mismatched account")
	}
	status, raw, _ := f.request(t, "POST", "/v2/auth/sessions:logout-all", credentials.AccessToken, "", map[string]any{})
	if status != 204 {
		t.Fatalf("logout %d %s", status, raw)
	}
	started := time.Now()
	_, err = authority.AuthenticateRequest(f.ctx, request)
	var rejected *sharedauth.IdentityAuthorityError
	if !errors.As(err, &rejected) || rejected.Status != 401 {
		t.Fatalf("subsequent HTTP write retained authority: %v", err)
	}
	if err = stream.CheckStreamSession(f.ctx, identity.StreamPrincipal()); !errors.As(err, &rejected) || rejected.Status != 401 {
		t.Fatalf("private service retained authority: %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("service revocation propagation exceeded five seconds")
	}
}

func TestIdentityOwnerStaleAccessGenerationCannotLink(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, oldPrincipal := ownerCredentials(t, f)
	rotated, err := f.s.Refresh(f.ctx, credentials.RefreshSecret)
	if err != nil {
		t.Fatal(err)
	}
	wallet := "0x2222222222222222222222222222222222222222"
	_, _, err = f.s.IssueLink(f.ctx, oldPrincipal, WalletLinkChallengeRequest{wallet, "125", "private"}, "127.0.0.1")
	requireCode(t, err, "AUTH_EXPIRED")
	current, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, rotated.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.IssueLink(f.ctx, current, WalletLinkChallengeRequest{wallet, "125", "private"}, "127.0.0.1"); err != nil {
		t.Fatal("current generation rejected", err)
	}
}

func TestIdentityOwnerErrorAndTelemetryPrivacy(t *testing.T) {
	f := ownerHTTPFixture(t)
	var mu sync.Mutex
	categories := []string{}
	observe := func(category string) {
		mu.Lock()
		defer mu.Unlock()
		categories = append(categories, category)
	}
	f.s.observe = observe
	f.s.tokens.SetObserver(observe)
	challenge := f.challenge(t, f.wallet)
	changed := challenge
	changed.Message += " private signed text"
	proof := proofIdentity(t, f.key, changed)
	status, raw, _ := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proof, "private device"})
	if status != 401 {
		t.Fatalf("tampered proof %d %s", status, raw)
	}
	credentials, _ := ownerCredentials(t, f)
	status, listed, _ := f.request(t, "GET", "/v2/auth/sessions", credentials.AccessToken, "", nil)
	if status != 200 {
		t.Fatalf("sessions %d %s", status, listed)
	}
	mu.Lock()
	telemetry, err := json.Marshal(categories)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{challenge.Message, challenge.Nonce, proof.Signature, credentials.AccessToken, credentials.RefreshSecret, credentials.CSRFSecret, "private signed text"} {
		for _, projection := range [][]byte{raw, listed, telemetry} {
			if bytes.Contains(projection, []byte(secret)) {
				t.Fatal("sensitive material crossed public or telemetry boundary")
			}
		}
	}
	if bytes.Contains(raw, []byte(f.wallet)) || bytes.Contains(telemetry, []byte(f.wallet)) {
		t.Fatal("wallet leaked in rejection or metric category")
	}
	for _, category := range categories {
		if strings.ContainsAny(category, ":/. \n") || len(category) > 64 {
			t.Fatal("unbounded identity telemetry category")
		}
	}
	rows, err := f.pool.Query(f.ctx, `SELECT convert_from(envelope,'UTF8')::jsonb->'payload' FROM delivery_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload []byte
		if err = rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{f.wallet, challenge.Nonce, proof.Signature, credentials.AccessToken, credentials.RefreshSecret, credentials.CSRFSecret} {
			if bytes.Contains(payload, []byte(secret)) {
				t.Fatal("identity event leaked credential material")
			}
		}
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
}

func TestIdentityOwnerKeyDependencyLossRollsBackChallenge(t *testing.T) {
	f := ownerHTTPFixture(t)
	challenge := f.challenge(t, f.wallet)
	t.Setenv("KINDLE_OWNER_HTTP_TEST_KEY", "")
	status, raw, headers := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "key interruption"})
	if status != 503 || len(headers.Values("Set-Cookie")) != 0 {
		t.Fatalf("key interruption acknowledged credentials: %d %s", status, raw)
	}
	var sessions int
	var consumed *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("key interruption created session: %d %v", sessions, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT consumed_at FROM identity_challenges WHERE challenge_id=$1`, challenge.ChallengeID).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("key interruption consumed challenge: %v %v", consumed, err)
	}
	t.Setenv("KINDLE_OWNER_HTTP_TEST_KEY", strings.Repeat("s", 48))
	status, raw, _ = f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "key recovery"})
	if status != 200 {
		t.Fatalf("key recovery lost valid proof: %d %s", status, raw)
	}
}

func TestIdentityOwnerConcurrentLinkIdempotencyAndAccountIsolation(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, p := ownerCredentials(t, f)
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	walletB := strings.ToLower(crypto.PubkeyToAddress(keyB.PublicKey).Hex())
	request := WalletLinkChallengeRequest{walletB, "125", "private"}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("POST", f.server.URL+"/v2/me/wallet-links/challenges", bytes.NewReader(body))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Origin", "https://kindle.test")
			req.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
			req.Header.Set("Idempotency-Key", key)
			response, err := f.server.Client().Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			raw, err := io.ReadAll(response.Body)
			response.Body.Close()
			results <- result{response.StatusCode, raw, err}
		}()
	}
	wg.Wait()
	close(results)
	var first LinkChallengeData
	for response := range results {
		if response.err != nil || response.status != 201 {
			t.Fatalf("duplicate request status=%d err=%v", response.status, response.err)
		}
		var envelope Envelope[LinkChallengeData]
		if err = json.Unmarshal(response.raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if first.LinkID == "" {
			first = envelope.Data
		}
		if first.LinkID != envelope.Data.LinkID || first.Challenge.ChallengeID != envelope.Data.Challenge.ChallengeID {
			t.Fatal("duplicate owner request created multiple challenges")
		}
	}
	keyC, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	walletC := strings.ToLower(crypto.PubkeyToAddress(keyC.PublicKey).Hex())
	challengeC := f.challenge(t, walletC)
	foreign, err := f.s.Login(f.ctx, LoginRequest{challengeC.ChallengeID, proofIdentity(t, keyC, challengeC), "foreign owner"})
	if err != nil {
		t.Fatal(err)
	}
	status, raw, _ := f.request(t, "POST", "/v2/me/wallet-links/"+first.LinkID+":verify", foreign.AccessToken, uuid.NewString(), map[string]any{"challengeId": first.Challenge.ChallengeID, "proof": proofIdentity(t, keyB, first.Challenge)})
	if status != 401 || bytes.Contains(raw, []byte(p.AccountID())) {
		t.Fatalf("foreign account verification %d %s", status, raw)
	}
	status, raw, _ = f.request(t, "POST", "/v2/me/wallet-links/"+first.LinkID+":verify", credentials.AccessToken, uuid.NewString(), map[string]any{"challengeId": first.Challenge.ChallengeID, "proof": proofIdentity(t, keyB, first.Challenge)})
	if status != 200 {
		t.Fatalf("foreign rejection consumed owner's challenge: %d %s", status, raw)
	}
	status, raw, _ = f.request(t, "POST", "/v2/me/wallet-links/challenges", foreign.AccessToken, uuid.NewString(), WalletLinkChallengeRequest{walletB, "125", "private"})
	if status != 409 || bytes.Contains(raw, []byte(p.AccountID())) {
		t.Fatalf("verified owner conflict leaked account or merged: %d %s", status, raw)
	}
	var count int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_wallet_links WHERE wallet=$1 AND state='verified'`, walletB).Scan(&count); err != nil || count != 1 {
		t.Fatalf("wallet owner count=%d err=%v", count, err)
	}
}

func TestIdentityOwnerHTTPConcurrentRefreshRevokesFamilyOnReplay(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, _ := ownerCredentials(t, f)
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions:refresh", strings.NewReader(`{}`))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Origin", "https://kindle.test")
			req.Header.Set("X-CSRF-Token", credentials.CSRFSecret)
			req.AddCookie(&http.Cookie{Name: "__Host-kindle_refresh", Value: credentials.RefreshSecret})
			response, err := f.server.Client().Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			raw, err := io.ReadAll(response.Body)
			response.Body.Close()
			results <- result{response.StatusCode, raw, err}
		}()
	}
	wg.Wait()
	close(results)
	wins, denied := 0, 0
	var winner Envelope[LoginData]
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		switch result.status {
		case 200:
			wins++
			if err := json.Unmarshal(result.raw, &winner); err != nil {
				t.Fatal(err)
			}
		case 401:
			denied++
		default:
			t.Fatalf("unexpected refresh response=%d %s", result.status, result.raw)
		}
	}
	if wins != 1 || denied != 19 {
		t.Fatalf("refresh winners=%d rejected=%d", wins, denied)
	}
	if _, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, winner.Data.AccessToken); err == nil {
		t.Fatal("replay left winning generation authorized")
	}
	var family int
	var revoked *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_refresh WHERE session_id=$1`, credentials.Session.SessionID).Scan(&family); err != nil || family != 2 {
		t.Fatalf("refresh family rows=%d err=%v", family, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT revoked_at FROM identity_sessions WHERE session_id=$1`, credentials.Session.SessionID).Scan(&revoked); err != nil || revoked == nil {
		t.Fatalf("family not revoked %v %v", revoked, err)
	}
}

func TestIdentityOwnerIndistinguishableChallengeFailures(t *testing.T) {
	f := ownerHTTPFixture(t)
	used := f.challenge(t, f.wallet)
	usedProof := proofIdentity(t, f.key, used)
	status, raw, _ := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{used.ChallengeID, usedProof, "consume"})
	if status != 200 {
		t.Fatalf("consume %d %s", status, raw)
	}
	expired := f.challenge(t, f.wallet)
	expiredProof := proofIdentity(t, f.key, expired)
	f.now.Add(301000)
	var first PublicError
	for index, request := range []LoginRequest{{uuid.NewString(), usedProof, "unknown"}, {expired.ChallengeID, expiredProof, "expired"}, {used.ChallengeID, usedProof, "used"}} {
		status, raw, _ := f.request(t, "POST", "/v2/auth/sessions", "", "", request)
		if status != 401 {
			t.Fatalf("challenge failure %d %s", status, raw)
		}
		var public PublicError
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = public
		}
		if public.Code != first.Code || public.Message != first.Message || public.Retryable != first.Retryable || len(public.FieldErrors) != 0 || public.Details != nil {
			t.Fatal("challenge states distinguishable in public response")
		}
	}
}

func TestIdentityOwnerServiceTicketBindingAndReplay(t *testing.T) {
	f := ownerHTTPFixture(t)
	_, p := ownerCredentials(t, f)
	f.s.origins["https://staging.kindle.test"] = true
	topics := []string{"private.orders." + p.Wallet()}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://kindle.test", Endpoint: "/v2/stream"}
	ticket, err := f.s.IssueScopedTicket(f.ctx, p, binding)
	if err != nil {
		t.Fatal(err)
	}
	authority := sharedauth.HTTPStreamAuthority{Endpoint: f.server.URL, ServiceToken: strings.Repeat("t", 32), Client: f.server.Client()}
	for _, mutate := range []func(*sharedauth.StreamTicketBinding){
		func(b *sharedauth.StreamTicketBinding) { b.Origin = "https://staging.kindle.test" },
		func(b *sharedauth.StreamTicketBinding) { b.Endpoint = "/v2/other" },
		func(b *sharedauth.StreamTicketBinding) {
			b.Topics = []string{"private.orders.0x2222222222222222222222222222222222222222"}
			b.FilterDigest = sharedauth.StreamFilterDigest(b.Topics)
		},
	} {
		changed := binding
		mutate(&changed)
		_, err = authority.ConsumeStreamTicket(f.ctx, ticket, changed)
		var rejected *sharedauth.IdentityAuthorityError
		if !errors.As(err, &rejected) || rejected.Status != 401 {
			t.Fatalf("moved ticket accepted by owning HTTP service: %v", err)
		}
	}
	admitted, err := authority.ConsumeStreamTicket(f.ctx, ticket, binding)
	if err != nil || admitted.AccountID != p.AccountID() || admitted.SessionID != p.SessionID() {
		t.Fatalf("binding rejection consumed valid ticket: %v %v", admitted, err)
	}
	_, err = authority.ConsumeStreamTicket(f.ctx, ticket, binding)
	var rejected *sharedauth.IdentityAuthorityError
	if !errors.As(err, &rejected) || rejected.Status != 401 {
		t.Fatalf("replayed ticket accepted by owning HTTP service: %v", err)
	}
}
