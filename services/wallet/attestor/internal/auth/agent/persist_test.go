package agent_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/auth/agent"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	key := sha256.Sum256([]byte("agent nonce store key"))
	st, err := store.Open(dir, key[:])
	if err != nil {
		t.Fatal(err)
	}
	return st
}

type agentKey struct {
	pub  [ed25519.PublicKeySize]byte
	priv ed25519.PrivateKey
}

func newKey(t *testing.T) agentKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var k agentKey
	copy(k.pub[:], pub)
	k.priv = priv
	return k
}

func writePrincipals(t *testing.T, dir string, keys ...agentKey) string {
	t.Helper()
	body := "["
	for i, k := range keys {
		if i > 0 {
			body += ","
		}
		body += `{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","key_ids":["key-1"]}`
	}
	body += "]"
	path := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func signed(t *testing.T, k agentKey, expiry time.Time) agent.Request {
	t.Helper()
	req := agent.Request{Method: "/v1/sign", KeyID: "key-1", Body: []byte(`{"kind":"lx_activity"}`), PublicKey: k.pub, Expiry: uint64(expiry.Unix())}
	if _, err := rand.Read(req.Nonce[:]); err != nil {
		t.Fatal(err)
	}
	digest, err := agent.RequestDigest(req.Method, req.KeyID, req.Nonce, req.Expiry, req.Body)
	if err != nil {
		t.Fatal(err)
	}
	copy(req.Signature[:], ed25519.Sign(k.priv, digest[:]))
	return req
}

func newVerifier(t *testing.T, principals agent.PrincipalSet, nonces agent.AgentNonceStore, maxExpiry time.Duration) *agent.AgentVerifier {
	t.Helper()
	v, err := agent.NewAgentVerifier(agent.Config{Principals: principals, Nonces: nonces, ClockSkew: 5 * time.Second, MaxExpiry: maxExpiry})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAgentNonceRefusedAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	k := newKey(t)
	principals, err := agent.LoadPrincipals(writePrincipals(t, dir, k))
	if err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, "shares")
	st := openStore(t, storeDir)
	req := signed(t, k, time.Now().Add(time.Minute))
	if _, err := newVerifier(t, principals, st, time.Minute*5).Verify(context.Background(), req); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = openStore(t, storeDir)
	defer st.Close()
	v := newVerifier(t, principals, st, time.Minute*5)
	if _, err := v.Verify(context.Background(), req); !errors.Is(err, agent.ErrReplayedNonce) {
		t.Fatalf("replay after restart err = %v, want %v", err, agent.ErrReplayedNonce)
	}
	if _, err := v.Verify(context.Background(), signed(t, k, time.Now().Add(time.Minute))); err != nil {
		t.Fatalf("fresh nonce after restart: %v", err)
	}
}

func TestAgentExpiryBeyondMaximumRefused(t *testing.T) {
	dir := t.TempDir()
	k := newKey(t)
	principals, err := agent.LoadPrincipals(writePrincipals(t, dir, k))
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t, filepath.Join(dir, "shares"))
	defer st.Close()
	v := newVerifier(t, principals, st, 2*time.Minute)
	if _, err := v.Verify(context.Background(), signed(t, k, time.Now().Add(time.Hour))); !errors.Is(err, agent.ErrExpiryTooFar) {
		t.Fatalf("far expiry err = %v, want %v", err, agent.ErrExpiryTooFar)
	}
	if _, err := v.Verify(context.Background(), signed(t, k, time.Now().Add(90*time.Second))); err != nil {
		t.Fatalf("expiry within the maximum: %v", err)
	}
	if _, err := newVerifier(t, principals, st, 0).Verify(context.Background(), signed(t, k, time.Now().Add(agent.DefaultMaxExpiry+time.Minute))); !errors.Is(err, agent.ErrExpiryTooFar) {
		t.Fatalf("expiry past the default maximum err = %v, want %v", err, agent.ErrExpiryTooFar)
	}
	if _, err := agent.NewAgentVerifier(agent.Config{Principals: principals, Nonces: st, MaxExpiry: -time.Second}); !errors.Is(err, agent.ErrInvalidConfig) {
		t.Fatalf("negative maximum expiry err = %v, want %v", err, agent.ErrInvalidConfig)
	}
}

func TestLoadPrincipalsRefusesMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	k := newKey(t)
	cases := map[string]string{
		"short key":     `[{"public_key":"abcd","key_ids":["key-1"]}]`,
		"no key ids":    `[{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","key_ids":[]}]`,
		"unknown field": `[{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","key_ids":["key-1"],"admin":true}]`,
		"duplicate":     `[{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","key_ids":["key-1"]},{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","key_ids":["key-2"]}]`,
	}
	for label, body := range cases {
		path := filepath.Join(dir, "agents.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := agent.LoadPrincipals(path); !errors.Is(err, agent.ErrPrincipalFile) {
			t.Fatalf("%s: err = %v, want %v", label, err, agent.ErrPrincipalFile)
		}
	}
	if _, err := agent.LoadPrincipals(filepath.Join(dir, "absent.json")); !errors.Is(err, agent.ErrPrincipalFile) {
		t.Fatalf("missing file err = %v", err)
	}
	frozen := `[{"public_key":"` + hex.EncodeToString(k.pub[:]) + `","frozen":true,"key_ids":["key-1"]}]`
	path := filepath.Join(dir, "frozen.json")
	if err := os.WriteFile(path, []byte(frozen), 0o600); err != nil {
		t.Fatal(err)
	}
	principals, err := agent.LoadPrincipals(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newVerifier(t, principals, agent.NewNonceCache(nil), 0).Verify(context.Background(), signed(t, k, time.Now().Add(time.Minute))); !errors.Is(err, agent.ErrFrozen) {
		t.Fatalf("frozen principal err = %v, want %v", err, agent.ErrFrozen)
	}
}
