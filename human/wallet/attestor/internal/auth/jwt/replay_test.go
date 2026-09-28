package jwt_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
)

type provider struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	issuer string
}

func enc(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newProvider(t *testing.T) *provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &provider{key: key}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "replay-key", "alg": "RS256", "use": "sig",
			"n": enc(key.PublicKey.N.Bytes()), "e": enc(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	}))
	t.Cleanup(p.srv.Close)
	p.issuer = p.srv.URL + "/auth/v1"
	return p
}

func (p *provider) mint(t *testing.T, issuedAt time.Time) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "replay-key", "typ": "JWT"})
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"sub": "user-0001", "iss": p.issuer, "aud": "authenticated", "iat": issuedAt.Unix(), "exp": issuedAt.Add(3 * time.Hour).Unix(), "jti": hex.EncodeToString(nonce)})
	signing := enc(header) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + enc(sig)
}

func owns(subject, keyID string) (bool, error) {
	return subject == "user-0001" && keyID == "key-1", nil
}

func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	key := sha256.Sum256([]byte("replay store key"))
	st, err := store.Open(dir, key[:])
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func verifier(t *testing.T, p *provider, replay jwt.TokenReplayStore, maxAge time.Duration) *jwt.TokenVerifier {
	t.Helper()
	return verifierAt(t, p, replay, maxAge, time.Now)
}

func verifierAt(t *testing.T, p *provider, replay jwt.TokenReplayStore, maxAge time.Duration, now func() time.Time) *jwt.TokenVerifier {
	t.Helper()
	v, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: p.srv.URL, Issuer: p.issuer, Audience: "authenticated", HTTPClient: p.srv.Client(), ClockSkew: 5 * time.Second, MaxAge: maxAge, Replay: replay, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func digest(t *testing.T, keyID, body string) [32]byte {
	t.Helper()
	d, err := jwt.RequestDigest("/v1/sign", keyID, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const (
	bodyA = `{"session_id":"session-a","key_id":"key-1","kind":"lx_bind","signers":["node-1","node-2","node-3"],"message":"0a0b"}`
	bodyB = `{"session_id":"session-b","key_id":"key-1","kind":"lx_bind","signers":["node-1","node-2","node-3"],"message":"0a0b"}`
	bodyC = `{"session_id":"session-c","key_id":"key-1","kind":"evm_transaction","signers":["node-1","node-2","node-3"],"transaction":"02f0"}`
)

func TestTokenAuthorisesDistinctRequestsAndRefusesARepeatAcrossRestart(t *testing.T) {
	p := newProvider(t)
	dir := filepath.Join(t.TempDir(), "shares")
	st := openStore(t, dir)
	token := p.mint(t, time.Now().Add(-time.Minute))
	reqA, reqB, reqC := digest(t, "key-1", bodyA), digest(t, "key-1", bodyB), digest(t, "key-1", bodyC)
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", reqA, owns); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", reqB, owns); err != nil {
		t.Fatalf("second distinct request under the same token: %v", err)
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", reqA, owns); !errors.Is(err, jwt.ErrReplayed) {
		t.Fatalf("repeated request err = %v, want %v", err, jwt.ErrReplayed)
	}
	reordered := digest(t, "key-1", `{ "message":"0a0b", "kind":"lx_bind", "key_id":"key-1", "signers":["node-1","node-2","node-3"], "session_id":"session-a" }`)
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", reordered, owns); !errors.Is(err, jwt.ErrReplayed) {
		t.Fatalf("repeated request with reordered fields err = %v, want %v", err, jwt.ErrReplayed)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = openStore(t, dir)
	defer st.Close()
	for name, req := range map[string][32]byte{"first": reqA, "second": reqB} {
		if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", req, owns); !errors.Is(err, jwt.ErrReplayed) {
			t.Fatalf("%s request after restart err = %v, want %v", name, err, jwt.ErrReplayed)
		}
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", reqC, owns); err != nil {
		t.Fatalf("a new request under the same token after restart: %v", err)
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), p.mint(t, time.Now()), "key-1", reqA, owns); err != nil {
		t.Fatalf("the first request under a different token: %v", err)
	}
}

func TestRequestDigestCanonicalisesTheBody(t *testing.T) {
	if digest(t, "key-1", bodyA) == digest(t, "key-1", bodyB) {
		t.Fatal("requests with different session ids share a digest")
	}
	if digest(t, "key-1", bodyA) == digest(t, "key-2", bodyA) {
		t.Fatal("requests for different key ids share a digest")
	}
	if digest(t, "key-1", bodyA) != digest(t, "key-1", "\n"+bodyA+"\n") {
		t.Fatal("surrounding whitespace changed the digest")
	}
	for name, body := range map[string]string{"array": `[1]`, "trailing": bodyA + `{}`, "malformed": `{"session_id":`} {
		if _, err := jwt.RequestDigest("/v1/sign", "key-1", []byte(body)); !errors.Is(err, jwt.ErrRequest) {
			t.Fatalf("%s body err = %v, want %v", name, err, jwt.ErrRequest)
		}
	}
	if _, err := jwt.RequestDigest("", "key-1", []byte(bodyA)); !errors.Is(err, jwt.ErrRequest) {
		t.Fatalf("empty method err = %v, want %v", err, jwt.ErrRequest)
	}
	if _, err := jwt.RequestDigest("/v1/sign", "", []byte(bodyA)); !errors.Is(err, jwt.ErrRequest) {
		t.Fatalf("empty key id err = %v, want %v", err, jwt.ErrRequest)
	}
}

func TestTokenRefusedWhenNotOwnedDoesNotConsumeIt(t *testing.T) {
	p := newProvider(t)
	st := openStore(t, filepath.Join(t.TempDir(), "shares"))
	defer st.Close()
	v := verifier(t, p, st, time.Hour)
	token := p.mint(t, time.Now())
	req := digest(t, "key-1", bodyA)
	if _, err := v.Verify(context.Background(), token, "key-2", req, owns); !errors.Is(err, jwt.ErrNotOwner) {
		t.Fatalf("foreign key err = %v, want %v", err, jwt.ErrNotOwner)
	}
	if _, err := v.Verify(context.Background(), token, "key-1", req, owns); err != nil {
		t.Fatalf("owned key after a refusal: %v", err)
	}
}

func TestTokenOlderThanMaximumAgeRefused(t *testing.T) {
	p := newProvider(t)
	st := openStore(t, filepath.Join(t.TempDir(), "shares"))
	defer st.Close()
	v := verifier(t, p, st, 10*time.Minute)
	req := digest(t, "key-1", bodyA)
	if _, err := v.Verify(context.Background(), p.mint(t, time.Now().Add(-11*time.Minute)), "key-1", req, owns); !errors.Is(err, jwt.ErrTooOld) {
		t.Fatalf("old token err = %v, want %v", err, jwt.ErrTooOld)
	}
	token := p.mint(t, time.Now().Add(-9*time.Minute))
	if _, err := v.Verify(context.Background(), token, "key-1", req, owns); err != nil {
		t.Fatalf("token within the maximum age: %v", err)
	}
	later := verifierAt(t, p, st, 10*time.Minute, func() time.Time { return time.Now().Add(2 * time.Minute) })
	if _, err := later.Verify(context.Background(), token, "key-1", digest(t, "key-1", bodyB), owns); !errors.Is(err, jwt.ErrTooOld) {
		t.Fatalf("a new request once the token passed the maximum age err = %v, want %v", err, jwt.ErrTooOld)
	}
	if _, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: p.srv.URL, Issuer: p.issuer, Audience: "authenticated", HTTPClient: p.srv.Client(), MaxAge: -time.Second}); !errors.Is(err, jwt.ErrInvalidConfig) {
		t.Fatalf("negative maximum age err = %v, want %v", err, jwt.ErrInvalidConfig)
	}
}

func TestDefaultMaximumAgeApplies(t *testing.T) {
	p := newProvider(t)
	v := verifier(t, p, nil, 0)
	if _, err := v.Verify(context.Background(), p.mint(t, time.Now().Add(-jwt.DefaultMaxAge-time.Minute)), "key-1", digest(t, "key-1", bodyA), owns); !errors.Is(err, jwt.ErrTooOld) {
		t.Fatalf("token past the default maximum age err = %v, want %v", err, jwt.ErrTooOld)
	}
}
