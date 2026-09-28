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
	v, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: p.srv.URL, Issuer: p.issuer, Audience: "authenticated", HTTPClient: p.srv.Client(), ClockSkew: 5 * time.Second, MaxAge: maxAge, Replay: replay})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTokenReplayRefusedAcrossRestart(t *testing.T) {
	p := newProvider(t)
	dir := filepath.Join(t.TempDir(), "shares")
	st := openStore(t, dir)
	token := p.mint(t, time.Now().Add(-time.Minute))
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", owns); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", owns); !errors.Is(err, jwt.ErrReplayed) {
		t.Fatalf("second use err = %v, want %v", err, jwt.ErrReplayed)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = openStore(t, dir)
	defer st.Close()
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), token, "key-1", owns); !errors.Is(err, jwt.ErrReplayed) {
		t.Fatalf("use after restart err = %v, want %v", err, jwt.ErrReplayed)
	}
	if _, err := verifier(t, p, st, time.Hour).Verify(context.Background(), p.mint(t, time.Now()), "key-1", owns); err != nil {
		t.Fatalf("a different token: %v", err)
	}
}

func TestTokenRefusedWhenNotOwnedDoesNotConsumeIt(t *testing.T) {
	p := newProvider(t)
	st := openStore(t, filepath.Join(t.TempDir(), "shares"))
	defer st.Close()
	v := verifier(t, p, st, time.Hour)
	token := p.mint(t, time.Now())
	if _, err := v.Verify(context.Background(), token, "key-2", owns); !errors.Is(err, jwt.ErrNotOwner) {
		t.Fatalf("foreign key err = %v, want %v", err, jwt.ErrNotOwner)
	}
	if _, err := v.Verify(context.Background(), token, "key-1", owns); err != nil {
		t.Fatalf("owned key after a refusal: %v", err)
	}
}

func TestTokenOlderThanMaximumAgeRefused(t *testing.T) {
	p := newProvider(t)
	st := openStore(t, filepath.Join(t.TempDir(), "shares"))
	defer st.Close()
	v := verifier(t, p, st, 10*time.Minute)
	if _, err := v.Verify(context.Background(), p.mint(t, time.Now().Add(-11*time.Minute)), "key-1", owns); !errors.Is(err, jwt.ErrTooOld) {
		t.Fatalf("old token err = %v, want %v", err, jwt.ErrTooOld)
	}
	if _, err := v.Verify(context.Background(), p.mint(t, time.Now().Add(-9*time.Minute)), "key-1", owns); err != nil {
		t.Fatalf("token within the maximum age: %v", err)
	}
	if _, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: p.srv.URL, Issuer: p.issuer, Audience: "authenticated", HTTPClient: p.srv.Client(), MaxAge: -time.Second}); !errors.Is(err, jwt.ErrInvalidConfig) {
		t.Fatalf("negative maximum age err = %v, want %v", err, jwt.ErrInvalidConfig)
	}
}

func TestDefaultMaximumAgeApplies(t *testing.T) {
	p := newProvider(t)
	v := verifier(t, p, nil, 0)
	if _, err := v.Verify(context.Background(), p.mint(t, time.Now().Add(-jwt.DefaultMaxAge-time.Minute)), "key-1", owns); !errors.Is(err, jwt.ErrTooOld) {
		t.Fatalf("token past the default maximum age err = %v, want %v", err, jwt.ErrTooOld)
	}
}
