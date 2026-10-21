package auth

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type realEOA struct{}

func (realEOA) VerifyIdentity(_ context.Context, message string, p WalletProof) error {
	if p.Kind == "erc1271" {
		return unavailable()
	}
	if p.Kind != "eoa_eip191" || !sharedauth.VerifyWalletSignature(p.Wallet, message, p.Signature) {
		return unauthorized()
	}
	return nil
}

type identityFixture struct {
	s      *Identity
	pool   *pgxpool.Pool
	key    *ecdsa.PrivateKey
	wallet string
	now    *atomic.Int64
	ctx    context.Context
}

func fixtureIdentity(t *testing.T) *identityFixture {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L33_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	if e = admin.Ping(ctx); e != nil {
		t.Fatalf("real PostgreSQL required: %v", e)
	}
	schema := "kd_81d823a49198f134453a3bb7_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, e = admin.Exec(ctx, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 8
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, e := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		if e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	sql, e := os.ReadFile(filepath.Join("../../../../../launchpad/spec/production-system/handoffs/L33/schema-proposal.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(sql)); e != nil {
		t.Fatal(e)
	}
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	clock := &atomic.Int64{}
	clock.Store(time.Now().UTC().UnixMilli())
	s, e := NewIdentity(IdentityConfig{Pool: pool, Domain: "kindle.test", URI: "https://kindle.test/auth", AllowedOrigins: []string{"https://kindle.test"}, Verifier: realEOA{}, Clock: func() time.Time { return time.UnixMilli(clock.Load()).UTC() }, LimiterKey: []byte(strings.Repeat("k", 32))})
	if e != nil {
		t.Fatal(e)
	}
	return &identityFixture{s, pool, key, strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), clock, ctx}
}
func proofIdentity(t *testing.T, k *ecdsa.PrivateKey, c Challenge) WalletProof {
	t.Helper()
	sig, e := crypto.Sign(accounts.TextHash([]byte(c.Message)), k)
	if e != nil {
		t.Fatal(e)
	}
	sig[64] += 27
	return WalletProof{Kind: "eoa_eip191", Wallet: strings.ToLower(crypto.PubkeyToAddress(k.PublicKey).Hex()), Signature: "0x" + hex.EncodeToString(sig)}
}
func (f *identityFixture) challenge(t *testing.T, w string) Challenge {
	t.Helper()
	c, e := f.s.IssueLogin(f.ctx, ChallengeRequest{w, "https://kindle.test"}, "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func (f *identityFixture) login(t *testing.T) (Credentials, Principal) {
	t.Helper()
	c := f.challenge(t, f.wallet)
	creds, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "test device"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.s.Authenticate(f.ctx, creds.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	return creds, p
}
func requireCode(t *testing.T, e error, code string) {
	t.Helper()
	var p *PublicError
	if !errors.As(e, &p) || p.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
}
func TestIdentityCanonicalChallenge(t *testing.T) {
	f := fixtureIdentity(t)
	c := f.challenge(t, f.wallet)
	if len(c.Nonce) != 64 || c.ChainID != "125" || c.ExpiresAt.Sub(c.IssuedAt) != 300*time.Second {
		t.Fatal("challenge fields")
	}
	for _, change := range []struct{ from, to string }{{"kindle.test wants", "evil.test wants"}, {"URI: https://kindle.test/auth", "URI: https://evil.test/auth"}, {"Chain ID: 125", "Chain ID: 1"}, {"Issued At:", "Issued At: 2030-01-01T00:00:00.000Z #"}} {
		altered := c
		altered.Message = strings.Replace(c.Message, change.from, change.to, 1)
		_, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, altered), "test"})
		requireCode(t, e, "AUTH_EXPIRED")
	}
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&n); e != nil || n != 0 {
		t.Fatal("mutated proof created session", e, n)
	}
	var used *time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT consumed_at FROM identity_challenges WHERE challenge_id=$1`, c.ChallengeID).Scan(&used); e != nil || used != nil {
		t.Fatal("invalid proof consumed challenge", e)
	}
	if _, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "test"}); e != nil {
		t.Fatal(e)
	}
}
func TestIdentityConcurrentNonce(t *testing.T) {
	f := fixtureIdentity(t)
	c := f.challenge(t, f.wallet)
	r := LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "concurrent"}
	var wins atomic.Int64
	var fails atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.s.Login(f.ctx, r)
			if e == nil {
				wins.Add(1)
			} else {
				var p *PublicError
				if errors.As(e, &p) && p.Code == "AUTH_EXPIRED" {
					fails.Add(1)
				} else {
					t.Errorf("unexpected %v", e)
				}
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || fails.Load() != 99 {
		t.Fatalf("wins=%d failures=%d", wins.Load(), fails.Load())
	}
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestIdentityNonceLimitsExpiryAndDependency(t *testing.T) {
	f := fixtureIdentity(t)
	var first Challenge
	for i := 0; i < 10; i++ {
		c := f.challenge(t, f.wallet)
		if i == 0 {
			first = c
		}
	}
	_, e := f.s.IssueLogin(f.ctx, ChallengeRequest{f.wallet, "https://kindle.test"}, "127.0.0.1")
	requireCode(t, e, "RATE_LIMITED")
	if _, e = f.s.Login(f.ctx, LoginRequest{first.ChallengeID, proofIdentity(t, f.key, first), "test"}); e != nil {
		t.Fatal(e)
	}
	f.now.Add(301000)
	_, e = f.s.Login(f.ctx, LoginRequest{first.ChallengeID, proofIdentity(t, f.key, first), "test"})
	requireCode(t, e, "AUTH_EXPIRED")
	_, e = f.s.Login(f.ctx, LoginRequest{uuid.NewString(), proofIdentity(t, f.key, first), "test"})
	requireCode(t, e, "AUTH_EXPIRED")
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	_, e = f.s.IssueLogin(ctx, ChallengeRequest{f.wallet, "https://kindle.test"}, "127.0.0.2")
	requireCode(t, e, "DEPENDENCY_UNAVAILABLE")
}
func TestIdentityRefreshFamily(t *testing.T) {
	f := fixtureIdentity(t)
	creds, _ := f.login(t)
	rotated, e := f.s.Refresh(f.ctx, creds.RefreshSecret)
	if e != nil {
		t.Fatal(e)
	}
	if rotated.RefreshSecret == creds.RefreshSecret || rotated.CSRFSecret == creds.CSRFSecret || !rotated.Session.RefreshExpiresAt.Equal(creds.Session.RefreshExpiresAt) {
		t.Fatal("rotation/absolute expiry")
	}
	_, e = f.s.Authenticate(f.ctx, creds.AccessToken)
	requireCode(t, e, "AUTH_EXPIRED")
	if _, e = f.s.Authenticate(f.ctx, rotated.AccessToken); e != nil {
		t.Fatal(e)
	}
	_, e = f.s.Refresh(f.ctx, creds.RefreshSecret)
	requireCode(t, e, "AUTH_EXPIRED")
	_, e = f.s.Authenticate(f.ctx, rotated.AccessToken)
	requireCode(t, e, "AUTH_EXPIRED")
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_refresh WHERE refresh_hash=$1`, digest(rotated.RefreshSecret)).Scan(&n)
	if n != 1 {
		t.Fatal("hash persistence")
	}
}
func TestIdentityConcurrentRefresh(t *testing.T) {
	f := fixtureIdentity(t)
	c, _ := f.login(t)
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.s.Refresh(f.ctx, c.RefreshSecret)
			if e == nil {
				wins.Add(1)
			} else {
				requireCode(t, e, "AUTH_EXPIRED")
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	var revoked *time.Time
	f.pool.QueryRow(f.ctx, `SELECT revoked_at FROM identity_sessions WHERE session_id=$1`, c.Session.SessionID).Scan(&revoked)
	if revoked == nil {
		t.Fatal("replay must revoke entire family")
	}
}
func TestIdentityTicketAndLogoutAll(t *testing.T) {
	f := fixtureIdentity(t)
	a, p := f.login(t)
	b, p2 := f.login(t)
	ticket, e := f.s.IssueTicket(f.ctx, p, "https://kindle.test", "/private/socket")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.ConsumeTicket(f.ctx, ticket, "https://kindle.test", "/other")
	requireCode(t, e, "AUTH_EXPIRED")
	socket, e := f.s.ConsumeTicket(f.ctx, ticket, "https://kindle.test", "/private/socket")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.ConsumeTicket(f.ctx, ticket, "https://kindle.test", "/private/socket")
	requireCode(t, e, "AUTH_EXPIRED")
	if e = f.s.Logout(f.ctx, p, true); e != nil {
		t.Fatal(e)
	}
	for _, token := range []string{a.AccessToken, b.AccessToken} {
		_, e = f.s.Authenticate(f.ctx, token)
		requireCode(t, e, "AUTH_EXPIRED")
	}
	for _, p := range []Principal{socket, p2} {
		requireCode(t, f.s.CheckSocket(f.ctx, p), "AUTH_EXPIRED")
	}
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_outbox WHERE action='session_revoked'`).Scan(&n)
	if n != 1 {
		t.Fatal("missing durable outbox")
	}
}
func TestIdentityWalletOwnershipAndUnlink(t *testing.T) {
	f := fixtureIdentity(t)
	_, p := f.login(t)
	var linkA string
	f.pool.QueryRow(f.ctx, `SELECT link_id::text FROM identity_wallet_links WHERE wallet=$1`, f.wallet).Scan(&linkA)
	step, e := f.s.IssueStepUp(f.ctx, p, "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.s.VerifyStepUp(f.ctx, p, step.ChallengeID, proofIdentity(t, f.key, step))
	if e != nil {
		t.Fatal(e)
	}
	requireCode(t, f.s.Unlink(f.ctx, p, WalletLinkRemoveRequest{linkA, receipt}), "STATE_CONFLICT")
	keyB, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	walletB := strings.ToLower(crypto.PubkeyToAddress(keyB.PublicKey).Hex())
	challenge, link, e := f.s.IssueLink(f.ctx, p, WalletLinkChallengeRequest{walletB, "125", "private"}, "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	w, e := f.s.VerifyLink(f.ctx, p, WalletLinkVerifyRequest{link, challenge.ChallengeID, proofIdentity(t, keyB, challenge)})
	if e != nil {
		t.Fatal(e)
	}
	if w.AccountID != p.AccountID() {
		t.Fatal("account moved")
	}
	c := f.challenge(t, walletB)
	b, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, keyB, c), "wallet B"})
	if e != nil {
		t.Fatal(e)
	}
	bp, e := f.s.Authenticate(f.ctx, b.AccessToken)
	if e != nil || bp.AccountID() != p.AccountID() {
		t.Fatal("stable ownership", e)
	}
	if e = f.s.Unlink(f.ctx, p, WalletLinkRemoveRequest{linkA, receipt}); e != nil {
		t.Fatal(e)
	}
	requireCode(t, f.s.CheckSocket(f.ctx, p), "AUTH_EXPIRED")
	if e = f.s.CheckSocket(f.ctx, bp); e != nil {
		t.Fatal(e)
	}
}
func TestIdentityStaleSessionAndWalletConflict(t *testing.T) {
	f := fixtureIdentity(t)
	_, p := f.login(t)
	k, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	w := strings.ToLower(crypto.PubkeyToAddress(k.PublicKey).Hex())
	c := f.challenge(t, w)
	b, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, k, c), "other"})
	if e != nil {
		t.Fatal(e)
	}
	bp, e := f.s.Authenticate(f.ctx, b.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	challenge, link, e := f.s.IssueLink(f.ctx, p, WalletLinkChallengeRequest{w, "125", "private"}, "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.VerifyLink(f.ctx, p, WalletLinkVerifyRequest{link, challenge.ChallengeID, proofIdentity(t, k, challenge)})
	requireCode(t, e, "STATE_CONFLICT")
	if bp.AccountID() == p.AccountID() {
		t.Fatal("implicit merge")
	}
	f.now.Add(301000)
	_, _, e = f.s.IssueLink(f.ctx, p, WalletLinkChallengeRequest{w, "125", "private"}, "127.0.0.1")
	requireCode(t, e, "AUTH_EXPIRED")
}
func TestIdentityCSRFAndOrigin(t *testing.T) {
	f := fixtureIdentity(t)
	c, _ := f.login(t)
	r := httptest.NewRequest("POST", "https://kindle.test/profile", nil)
	r.AddCookie(SessionCookie("__Host-kindle_access", c.AccessToken, c.Session.AccessExpiresAt))
	r.Header.Set("Origin", "https://evil.test")
	_, e := f.s.AuthenticateRequest(f.ctx, r)
	requireCode(t, e, "SCOPE_DENIED")
	r.Header.Set("Origin", "https://kindle.test")
	_, e = f.s.AuthenticateRequest(f.ctx, r)
	requireCode(t, e, "SCOPE_DENIED")
	r.Header.Set("X-CSRF-Token", c.CSRFSecret)
	if _, e = f.s.AuthenticateRequest(f.ctx, r); e != nil {
		t.Fatal(e)
	}
	r.URL.RawQuery = "token=" + c.AccessToken
	_, e = f.s.AuthenticateRequest(f.ctx, r)
	requireCode(t, e, "AUTH_EXPIRED")
	StripLegacyCredentialQuery(r)
	if strings.Contains(r.URL.RawQuery, c.AccessToken) {
		t.Fatal("credential not redacted")
	}
	cookie := SessionCookie("__Host-kindle_refresh", c.RefreshSecret, c.Session.RefreshExpiresAt)
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" {
		t.Fatal("cookie flags")
	}
	_, e = NewIdentity(IdentityConfig{Pool: f.pool, Domain: "kindle.test", URI: "https://kindle.test/auth", AllowedOrigins: []string{"*"}, Verifier: realEOA{}, LimiterKey: []byte(strings.Repeat("k", 32))})
	requireCode(t, e, "FIELD_INVALID")
}
func TestIdentityBoundedAdmission(t *testing.T) {
	f := fixtureIdentity(t)
	for i := 0; i < cap(f.s.admission); i++ {
		if e := f.s.enter(); e != nil {
			t.Fatal(e)
		}
	}
	defer func() {
		for len(f.s.admission) > 0 {
			f.s.leave()
		}
	}()
	_, e := f.s.IssueLogin(f.ctx, ChallengeRequest{f.wallet, "https://kindle.test"}, "127.0.0.1")
	requireCode(t, e, "RATE_LIMITED")
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_challenges`).Scan(&n)
	if n != 0 {
		t.Fatal("saturated admission mutated state")
	}
}
func TestIdentityThrottledLinkIsAtomic(t *testing.T) {
	f := fixtureIdentity(t)
	_, p := f.login(t)
	k, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	w := strings.ToLower(crypto.PubkeyToAddress(k.PublicKey).Hex())
	for i := 0; i < 10; i++ {
		f.challenge(t, w)
	}
	_, _, e = f.s.IssueLink(f.ctx, p, WalletLinkChallengeRequest{w, "125", "private"}, "127.0.0.1")
	requireCode(t, e, "RATE_LIMITED")
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_wallet_links WHERE state='pending'`).Scan(&n)
	if n != 0 {
		t.Fatal("orphan pending link", n)
	}
}
func TestIdentityContractDependencyAndNoncePreservation(t *testing.T) {
	f := fixtureIdentity(t)
	c := f.challenge(t, f.wallet)
	proof := proofIdentity(t, f.key, c)
	proof.Kind = "erc1271"
	_, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proof, "test"})
	requireCode(t, e, "DEPENDENCY_UNAVAILABLE")
	if _, e = f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "test"}); e != nil {
		t.Fatal(e)
	}
}
func TestIdentityConsumeRechecksAfterLock(t *testing.T) {
	f := fixtureIdentity(t)
	c := f.challenge(t, f.wallet)
	hold, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(f.ctx)
	_, e = hold.Exec(f.ctx, `UPDATE identity_challenges SET issued_at=$2::timestamptz-interval '300 seconds',expires_at=$2::timestamptz WHERE challenge_id=$1`, c.ChallengeID, time.Now().Add(200*time.Millisecond))
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "blocked"})
		done <- e
	}()
	time.Sleep(350 * time.Millisecond)
	if e = hold.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	requireCode(t, <-done, "AUTH_EXPIRED")
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM identity_sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("expired lock waiter minted session")
	}
}
func TestIdentityExactRequestEnvelopes(t *testing.T) {
	valid := `{"challengeId":"` + uuid.NewString() + `","proof":{"kind":"eoa_eip191","wallet":"0x1111111111111111111111111111111111111111","signature":"0x` + strings.Repeat("00", 65) + `"},"deviceLabel":""}`
	var req LoginRequest
	if e := DecodeIdentityRequest(strings.NewReader(valid), &req); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(valid, `,"deviceLabel":""`, "", 1), strings.Replace(valid, `"kind":"eoa_eip191"`, `"kind":"eoa_eip191","kind":"erc1271"`, 1), strings.Replace(valid, `"deviceLabel":""`, `"deviceLabel":"","extra":1`, 1), strings.Replace(valid, `"wallet":`, `"Wallet":"0x2222222222222222222222222222222222222222","wallet":`, 1), strings.Replace(valid, `"deviceLabel":""`, `"deviceLabel":"","DeviceLabel":"override"`, 1), valid + "?", strings.Repeat(" ", 16385)} {
		if e := DecodeIdentityRequest(strings.NewReader(bad), &req); e == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	var refresh RefreshRequest
	if e := DecodeIdentityRequest(strings.NewReader(`{}`), &refresh); e != nil {
		t.Fatal(e)
	}
	if e := DecodeIdentityRequest(strings.NewReader(`{"token":"x"}`), &refresh); e == nil {
		t.Fatal("refresh body token accepted")
	}
}
func TestIdentityJWTAuthorityAndRevocation(t *testing.T) {
	f := fixtureIdentity(t)
	_, p := f.login(t)
	now := time.Now().UTC().Truncate(time.Second)
	tokens, e := NewIdentityTokens("kindle", "gateway", "key", []SigningKey{{"key", "k", now.Add(-time.Minute), now.Add(time.Hour)}}, &localIdentitySecrets{}, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	token, e := tokens.Mint(f.ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(token) <= 256 {
		t.Fatal("fixture no longer reproduces frozen transport conflict")
	}
	actual, e := f.s.AuthenticateJWT(f.ctx, tokens, token)
	if e != nil || actual.AccountID() != p.AccountID() {
		t.Fatal(e)
	}
	if e = f.s.Logout(f.ctx, p, true); e != nil {
		t.Fatal(e)
	}
	_, e = f.s.AuthenticateJWT(f.ctx, tokens, token)
	requireCode(t, e, "AUTH_EXPIRED")
}
func TestIdentityFrozenWireTimestamps(t *testing.T) {
	f := fixtureIdentity(t)
	c := f.challenge(t, f.wallet)
	creds, p := f.login(t)
	zero := time.Date(2026, 10, 10, 1, 2, 3, 0, time.UTC)
	nano := zero.Add(123456789 * time.Nanosecond)
	c.IssuedAt = zero
	c.ExpiresAt = zero.Add(300 * time.Second)
	creds.Session.AccessExpiresAt = nano
	creds.Session.RevokedAt = &zero
	w := WalletLink{LinkID: uuid.NewString(), AccountID: p.AccountID(), ChainID: "125", Wallet: f.wallet, Visibility: "private", State: "verified", LinkedAt: &nano}
	fixtures := map[string]any{"AuthChallenge": c, "Session": creds.Session, "WalletLink": w, "LoginResponse": Envelope[LoginData]{RequestID: uuid.NewString(), Data: creds.LoginData, Quality: "complete", GeneratedAt: zero}}
	for name, v := range fixtures {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), "123456789") || strings.Contains(string(raw), "03Z") {
			t.Fatal("noncanonical timestamp", string(raw))
		}
		if dir := os.Getenv("L33_WIRE_FIXTURE_DIR"); dir != "" {
			if e = os.MkdirAll(dir, 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
}
func TestIdentityTrustedIPLimitAndRevokedPrincipal(t *testing.T) {
	f := fixtureIdentity(t)
	for i := 0; i < 30; i++ {
		key, e := crypto.GenerateKey()
		if e != nil {
			t.Fatal(e)
		}
		f.challenge(t, strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()))
	}
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	w := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	_, e = f.s.IssueLogin(f.ctx, ChallengeRequest{w, "https://kindle.test"}, "127.0.0.1")
	requireCode(t, e, "RATE_LIMITED")
	_, e = f.s.IssueLogin(f.ctx, ChallengeRequest{w, "https://kindle.test"}, "untrusted-forwarded-header")
	requireCode(t, e, "FIELD_INVALID")
	c, e := f.s.IssueLogin(f.ctx, ChallengeRequest{f.wallet, "https://kindle.test"}, "127.0.0.2")
	if e != nil {
		t.Fatal(e)
	}
	creds, e := f.s.Login(f.ctx, LoginRequest{c.ChallengeID, proofIdentity(t, f.key, c), "fresh"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.s.Authenticate(f.ctx, creds.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Logout(f.ctx, p, false); e != nil {
		t.Fatal(e)
	}
	requireCode(t, f.s.Logout(f.ctx, p, true), "AUTH_EXPIRED")
}
func TestIdentityConfigurationBounds(t *testing.T) {
	f := fixtureIdentity(t)
	for _, cfg := range []IdentityConfig{{Pool: f.pool, Domain: strings.Repeat("a", 254), URI: "https://" + strings.Repeat("a", 254) + "/auth", AllowedOrigins: []string{"https://kindle.test"}, Verifier: realEOA{}, LimiterKey: []byte(strings.Repeat("k", 32))}, {Pool: f.pool, Domain: "kindle.test", URI: "https://kindle.test/" + strings.Repeat("x", 1900), AllowedOrigins: []string{"https://kindle.test"}, Verifier: realEOA{}, LimiterKey: []byte(strings.Repeat("k", 32))}} {
		_, e := NewIdentity(cfg)
		requireCode(t, e, "FIELD_INVALID")
	}
	c := f.challenge(t, f.wallet)
	c.ChainID = "1"
	_, e := BuildSIWE(c, "")
	requireCode(t, e, "FIELD_INVALID")
}

func TestIdentitySIWEChecksumAndExactSigningBytes(t *testing.T) {
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	challenge := Challenge{ChallengeID: "11111111-1111-4111-8111-111111111111", Wallet: strings.ToLower(address), Nonce: strings.Repeat("a", 64), Domain: "kindle.test", URI: "https://kindle.test/auth", ChainID: "125", Purpose: "login", IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	message, err := BuildSIWE(challenge, "")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(message, "\n")
	if lines[1] != address || challenge.Wallet != strings.ToLower(address) {
		t.Fatalf("checksum signing bytes or normalized storage changed: %s", lines[1])
	}
	signature, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}
	signature[64] += 27
	if !sharedauth.VerifyWalletSignature(challenge.Wallet, message, "0x"+hex.EncodeToString(signature)) {
		t.Fatal("exact checksum SIWE signature rejected")
	}
	if sharedauth.VerifyWalletSignature(challenge.Wallet, strings.Replace(message, address, challenge.Wallet, 1), "0x"+hex.EncodeToString(signature)) {
		t.Fatal("changed signing bytes accepted")
	}
}
func TestIdentityStreamScopeBindsWalletAndFilter(t *testing.T) {
	wallet := "0x1111111111111111111111111111111111111111"
	p := sharedauth.StreamPrincipal{AccountID: uuid.NewString(), SessionID: uuid.NewString(), Wallet: wallet, AuthEpoch: 3}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: []string{"private.orders." + wallet, "market.price." + wallet}, Origin: "https://kindle.test", Endpoint: "/v2/stream"}
	binding.FilterDigest = sharedauth.StreamFilterDigest(binding.Topics)
	if err := sharedauth.ValidateStreamBinding(p, binding); err != nil {
		t.Fatal(err)
	}
	foreign := binding
	foreign.Topics = []string{"private.orders.0x2222222222222222222222222222222222222222"}
	foreign.FilterDigest = sharedauth.StreamFilterDigest(foreign.Topics)
	if sharedauth.ValidateStreamBinding(p, foreign) == nil {
		t.Fatal("foreign wallet topic accepted")
	}
	changed := binding
	changed.FilterDigest = strings.Repeat("a", 64)
	if sharedauth.ValidateStreamBinding(p, changed) == nil {
		t.Fatal("changed filter digest accepted")
	}
	room := binding
	room.Topics = []string{"rooms." + uuid.NewString() + ".messages"}
	room.FilterDigest = sharedauth.StreamFilterDigest(room.Topics)
	if sharedauth.ValidateStreamBinding(p, room) == nil {
		t.Fatal("room membership invented")
	}
}

func TestIdentitySerialJWTAndScopedTicket(t *testing.T) {
	f := fixtureIdentity(t)
	if _, err := f.pool.Exec(f.ctx, IdentitySerialDDL); err != nil {
		t.Fatal(err)
	}
	secret, err := randomSecret()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KINDLE_IDENTITY_SERIAL_TEST_KEY", secret)
	now := f.s.now()
	tokens, err := NewIdentityTokens("https://kindle.test", "kindle-gateway-v2", "serial", []SigningKey{{ID: "serial", SecretReference: "KINDLE_IDENTITY_SERIAL_TEST_KEY", ActiveAt: now.Add(-time.Minute), RetireAt: now.Add(time.Hour)}}, EnvironmentIdentitySecrets{}, f.s.now)
	if err != nil {
		t.Fatal(err)
	}
	f.s.tokens = tokens
	challenge := f.challenge(t, f.wallet)
	credentials, err := f.s.Login(f.ctx, LoginRequest{ChallengeID: challenge.ChallengeID, Proof: proofIdentity(t, f.key, challenge), DeviceLabel: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials.AccessToken) > 2048 || len(strings.Split(credentials.AccessToken, ".")) != 3 {
		t.Fatal("JWT transport missing")
	}
	p, err := f.s.AuthenticateJWT(f.ctx, tokens, credentials.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Authenticate(f.ctx, credentials.AccessToken); err == nil {
		t.Fatal("JWT accepted as opaque secret")
	}
	topics := []string{"private.orders." + f.wallet}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://kindle.test", Endpoint: "/v2/stream"}
	ticket, err := f.s.IssueScopedTicket(f.ctx, p, binding)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := f.s.ConsumeStreamTicket(f.ctx, ticket, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ConsumeStreamTicket(f.ctx, ticket, binding); err == nil {
		t.Fatal("ticket replay accepted")
	}
	rotated, err := f.s.Refresh(f.ctx, credentials.RefreshSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, tokens, rotated.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, tokens, credentials.AccessToken); err == nil {
		t.Fatal("old access generation accepted after refresh")
	}
	if err = f.s.Logout(f.ctx, p, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AuthenticateJWT(f.ctx, tokens, rotated.AccessToken); err == nil {
		t.Fatal("revoked epoch accepted")
	}
	if err = f.s.CheckStreamSession(f.ctx, socket); err == nil {
		t.Fatal("revoked socket accepted")
	}
}
func TestIdentityChallengeIdempotency(t *testing.T) {
	f := fixtureIdentity(t)
	if _, err := f.pool.Exec(f.ctx, IdentitySerialDDL); err != nil {
		t.Fatal(err)
	}
	request := ChallengeRequest{Wallet: f.wallet, Origin: "https://kindle.test"}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	first, err := f.s.IssueLoginIdempotent(f.ctx, request, "127.0.0.1", key, raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.IssueLoginIdempotent(f.ctx, request, "127.0.0.1", key, raw)
	if err != nil || second.ChallengeID != first.ChallengeID || second.Message != first.Message {
		t.Fatal("idempotency changed challenge", err)
	}
	_, err = f.s.IssueLoginIdempotent(f.ctx, request, "127.0.0.1", key, append(raw, ' '))
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
}
