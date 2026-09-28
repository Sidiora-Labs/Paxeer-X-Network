package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	jwxjwt "github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	testAudience = "authenticated"
	testSubject  = "5b1f0c3e-8a44-4f7e-9b2a-0d6c1e7f9a21"
	testKeyID    = "wallet-key-7"
)

var testRequest = [32]byte{1}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type jwksServer struct {
	mu      sync.Mutex
	keys    []jwk.Key
	fail    bool
	fetches atomic.Int64
	srv     *httptest.Server
}

func newJWKSServer(t *testing.T, keys ...jwk.Key) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		s.fetches.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		set := jwk.NewSet()
		for _, k := range s.keys {
			if err := set.AddKey(k); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(set); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *jwksServer) issuer() string { return s.srv.URL + "/auth/v1" }

func (s *jwksServer) jwksURL() string { return s.srv.URL + "/auth/v1/.well-known/jwks.json" }

func (s *jwksServer) addKey(k jwk.Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, k)
}

func (s *jwksServer) setFail(f bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = f
}

type signingKey struct {
	alg     jwa.SignatureAlgorithm
	private jwk.Key
	public  jwk.Key
}

func newRSAKey(t *testing.T, kid string) signingKey {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return wrapKey(t, jwa.RS256, raw, &raw.PublicKey, kid)
}

func newP256Key(t *testing.T, kid string) signingKey {
	t.Helper()
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return wrapKey(t, jwa.ES256, raw, &raw.PublicKey, kid)
}

func wrapKey(t *testing.T, alg jwa.SignatureAlgorithm, priv, pub any, kid string) signingKey {
	t.Helper()
	privJWK, err := jwk.FromRaw(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubJWK, err := jwk.FromRaw(pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []jwk.Key{privJWK, pubJWK} {
		if err := k.Set(jwk.KeyIDKey, kid); err != nil {
			t.Fatal(err)
		}
		if err := k.Set(jwk.AlgorithmKey, alg); err != nil {
			t.Fatal(err)
		}
	}
	if err := pubJWK.Set(jwk.KeyUsageKey, "sig"); err != nil {
		t.Fatal(err)
	}
	return signingKey{alg: alg, private: privJWK, public: pubJWK}
}

type claims struct {
	issuer    string
	audience  string
	subject   string
	issuedAt  time.Time
	notBefore time.Time
	expiry    time.Time
	noExpiry  bool
}

func mint(t *testing.T, key signingKey, c claims) string {
	t.Helper()
	b := jwxjwt.NewBuilder().Issuer(c.issuer).Audience([]string{c.audience}).IssuedAt(c.issuedAt)
	if c.subject != "" {
		b = b.Subject(c.subject)
	}
	if !c.noExpiry {
		b = b.Expiration(c.expiry)
	}
	if !c.notBefore.IsZero() {
		b = b.NotBefore(c.notBefore)
	}
	tok, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Set("role", "authenticated"); err != nil {
		t.Fatal(err)
	}
	signed, err := jwxjwt.Sign(tok, jwxjwt.WithKey(key.alg, key.private))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}

type fixture struct {
	clock    *testClock
	server   *jwksServer
	verifier *TokenVerifier
	rsaKey   signingKey
	ecKey    signingKey
}

const minRefresh = 30 * time.Second

func newFixture(t *testing.T) *fixture {
	t.Helper()
	rsaKey := newRSAKey(t, "rsa-1")
	ecKey := newP256Key(t, "ec-1")
	server := newJWKSServer(t, rsaKey.public, ecKey.public)
	clock := &testClock{t: time.Unix(1_900_000_000, 0)}
	v, err := NewTokenVerifier(Config{
		JWKSURL:            server.jwksURL(),
		Issuer:             server.issuer(),
		Audience:           testAudience,
		MinRefreshInterval: minRefresh,
		ClockSkew:          5 * time.Second,
		HTTPClient:         server.srv.Client(),
		Now:                clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{clock: clock, server: server, verifier: v, rsaKey: rsaKey, ecKey: ecKey}
}

func (f *fixture) validClaims() claims {
	now := f.clock.Now()
	return claims{
		issuer:   f.server.issuer(),
		audience: testAudience,
		subject:  testSubject,
		issuedAt: now.Add(-time.Minute),
		expiry:   now.Add(time.Hour),
	}
}

func ownsTestKey(subject, keyID string) (bool, error) {
	return subject == testSubject && keyID == testKeyID, nil
}

func TestVerifyAcceptsRS256(t *testing.T) {
	f := newFixture(t)
	sub, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != testSubject {
		t.Fatalf("subject = %q, want %q", sub, testSubject)
	}
}

func TestVerifyAcceptsES256(t *testing.T) {
	f := newFixture(t)
	sub, err := f.verifier.Verify(context.Background(), mint(t, f.ecKey, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != testSubject {
		t.Fatalf("subject = %q, want %q", sub, testSubject)
	}
}

func TestVerifyRefusesExpired(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.issuedAt = f.clock.Now().Add(-2 * time.Hour)
	c.expiry = f.clock.Now().Add(-time.Minute)
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("err = %v, want %v", err, ErrClaims)
	}
}

func TestVerifyAllowsExpiryWithinSkew(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.expiry = f.clock.Now().Add(-2 * time.Second)
	if _, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, c), testKeyID, testRequest, ownsTestKey); err != nil {
		t.Fatalf("verify within skew: %v", err)
	}
}

func TestVerifyRefusesNotYetValid(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.notBefore = f.clock.Now().Add(10 * time.Minute)
	_, err := f.verifier.Verify(context.Background(), mint(t, f.ecKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("err = %v, want %v", err, ErrClaims)
	}
}

func TestVerifyRefusesMissingExpiry(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.noExpiry = true
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("err = %v, want %v", err, ErrClaims)
	}
}

func TestVerifyRefusesWrongIssuer(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.issuer = f.server.srv.URL + "/other/v1"
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("err = %v, want %v", err, ErrClaims)
	}
}

func TestVerifyRefusesWrongAudience(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.audience = "anon"
	_, err := f.verifier.Verify(context.Background(), mint(t, f.ecKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("err = %v, want %v", err, ErrClaims)
	}
}

func TestVerifyRefusesMissingSubject(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c.subject = ""
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, c), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrMissingSubject) {
		t.Fatalf("err = %v, want %v", err, ErrMissingSubject)
	}
}

func TestVerifyRefusesWhenSubjectDoesNotOwnKey(t *testing.T) {
	f := newFixture(t)
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), "wallet-key-other", testRequest, ownsTestKey)
	if !errors.Is(err, ErrNotOwner) {
		t.Fatalf("err = %v, want %v", err, ErrNotOwner)
	}
}

func TestVerifyRefusesWhenOwnershipLookupFails(t *testing.T) {
	f := newFixture(t)
	lookupErr := errors.New("store unavailable")
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), testKeyID, testRequest, func(string, string) (bool, error) {
		return true, lookupErr
	})
	if !errors.Is(err, ErrOwnershipLookup) {
		t.Fatalf("err = %v, want %v", err, ErrOwnershipLookup)
	}
}

func TestVerifyRefusesTamperedSignature(t *testing.T) {
	f := newFixture(t)
	other := newRSAKey(t, "rsa-1")
	_, err := f.verifier.Verify(context.Background(), mint(t, other, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want %v", err, ErrSignature)
	}
}

func TestVerifyRefreshesForUnknownKeyID(t *testing.T) {
	f := newFixture(t)
	if _, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), testKeyID, testRequest, ownsTestKey); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if got := f.server.fetches.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
	rotated := newRSAKey(t, "rsa-2")
	f.server.addKey(rotated.public)

	_, err := f.verifier.Verify(context.Background(), mint(t, rotated, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("within min interval err = %v, want %v", err, ErrUnknownKey)
	}
	if got := f.server.fetches.Load(); got != 1 {
		t.Fatalf("fetches within min interval = %d, want 1", got)
	}

	f.clock.Advance(minRefresh)
	sub, err := f.verifier.Verify(context.Background(), mint(t, rotated, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if err != nil {
		t.Fatalf("after refresh: %v", err)
	}
	if sub != testSubject {
		t.Fatalf("subject = %q, want %q", sub, testSubject)
	}
	if got := f.server.fetches.Load(); got != 2 {
		t.Fatalf("fetches after refresh = %d, want 2", got)
	}
}

func TestVerifyRefusesUnknownKeyIDWhenFetchFails(t *testing.T) {
	f := newFixture(t)
	if _, err := f.verifier.Verify(context.Background(), mint(t, f.ecKey, f.validClaims()), testKeyID, testRequest, ownsTestKey); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	f.server.setFail(true)
	f.clock.Advance(minRefresh)
	unknown := newP256Key(t, "ec-2")
	_, err := f.verifier.Verify(context.Background(), mint(t, unknown, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrKeySetFetch) {
		t.Fatalf("err = %v, want %v", err, ErrKeySetFetch)
	}
	if _, err := f.verifier.Verify(context.Background(), mint(t, f.ecKey, f.validClaims()), testKeyID, testRequest, ownsTestKey); err != nil {
		t.Fatalf("cached key during outage: %v", err)
	}
}

func TestVerifyRefusesWhenKeySetNeverFetched(t *testing.T) {
	f := newFixture(t)
	f.server.setFail(true)
	_, err := f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrKeySetFetch) {
		t.Fatalf("err = %v, want %v", err, ErrKeySetFetch)
	}
	_, err = f.verifier.Verify(context.Background(), mint(t, f.rsaKey, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrKeySetFetch) {
		t.Fatalf("second attempt err = %v, want %v", err, ErrKeySetFetch)
	}
}

func TestVerifyRefusesHS256(t *testing.T) {
	f := newFixture(t)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	sym, err := jwk.FromRaw(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := sym.Set(jwk.KeyIDKey, "rsa-1"); err != nil {
		t.Fatal(err)
	}
	token := mint(t, signingKey{alg: jwa.HS256, private: sym}, f.validClaims())
	_, err = f.verifier.Verify(context.Background(), token, testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("err = %v, want %v", err, ErrAlgorithm)
	}
	if got := f.server.fetches.Load(); got != 0 {
		t.Fatalf("fetches = %d, want 0", got)
	}
}

func TestVerifyRefusesNone(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"none","kid":"rsa-1","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{
		"iss": c.issuer,
		"aud": c.audience,
		"sub": c.subject,
		"iat": c.issuedAt.Unix(),
		"exp": c.expiry.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	token := header + "." + enc.EncodeToString(payload) + "."
	_, err = f.verifier.Verify(context.Background(), token, testKeyID, testRequest, ownsTestKey)
	if err == nil {
		t.Fatal("unsigned token accepted")
	}
	if !errors.Is(err, ErrAlgorithm) && !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want %v or %v", err, ErrAlgorithm, ErrMalformed)
	}
}

func TestVerifyRefusesAlgorithmKeyMismatch(t *testing.T) {
	f := newFixture(t)
	impostor := newP256Key(t, "rsa-1")
	_, err := f.verifier.Verify(context.Background(), mint(t, impostor, f.validClaims()), testKeyID, testRequest, ownsTestKey)
	if !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("err = %v, want %v", err, ErrAlgorithm)
	}
}

func TestNewTokenVerifierRequiresConfiguration(t *testing.T) {
	cases := []Config{
		{Issuer: "i", Audience: "a", HTTPClient: http.DefaultClient},
		{JWKSURL: "u", Audience: "a", HTTPClient: http.DefaultClient},
		{JWKSURL: "u", Issuer: "i", HTTPClient: http.DefaultClient},
		{JWKSURL: "u", Issuer: "i", Audience: "a"},
		{JWKSURL: "u", Issuer: "i", Audience: "a", HTTPClient: http.DefaultClient, ClockSkew: -time.Second},
	}
	for i, cfg := range cases {
		if _, err := NewTokenVerifier(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("case %d: err = %v, want %v", i, err, ErrInvalidConfig)
		}
	}
}
