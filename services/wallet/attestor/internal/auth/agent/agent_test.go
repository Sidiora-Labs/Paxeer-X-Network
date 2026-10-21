package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"
)

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

type registry struct {
	mu         sync.RWMutex
	principals map[[ed25519.PublicKeySize]byte]Principal
}

func (r *registry) Lookup(pub [ed25519.PublicKeySize]byte) (Principal, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.principals[pub]
	return p, ok
}

func (r *registry) put(p Principal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.principals[p.PublicKey] = p
}

type agentKey struct {
	pub  [ed25519.PublicKeySize]byte
	priv ed25519.PrivateKey
}

func newAgentKey(t *testing.T) agentKey {
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

func randomNonce(t *testing.T) [16]byte {
	t.Helper()
	var n [16]byte
	if _, err := rand.Read(n[:]); err != nil {
		t.Fatal(err)
	}
	return n
}

func signRequest(t *testing.T, signer ed25519.PrivateKey, req Request) Request {
	t.Helper()
	digest, err := RequestDigest(req.Method, req.KeyID, req.Nonce, req.Expiry, req.Body)
	if err != nil {
		t.Fatal(err)
	}
	copy(req.Signature[:], ed25519.Sign(signer, digest[:]))
	return req
}

type fixture struct {
	clock    *testClock
	registry *registry
	nonces   *NonceCache
	verifier *AgentVerifier
	agent    agentKey
}

const testSkew = 5 * time.Second

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &testClock{t: time.Unix(1_900_000_000, 0)}
	reg := &registry{principals: make(map[[ed25519.PublicKeySize]byte]Principal)}
	agent := newAgentKey(t)
	reg.put(Principal{PublicKey: agent.pub, KeyIDs: []string{"wallet-key-1", "wallet-key-2"}})
	nonces := NewNonceCache(clock.Now)
	v, err := NewAgentVerifier(Config{Principals: reg, Nonces: nonces, ClockSkew: testSkew, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{clock: clock, registry: reg, nonces: nonces, verifier: v, agent: agent}
}

func (f *fixture) request(t *testing.T) Request {
	t.Helper()
	return Request{
		Method:    "sign",
		KeyID:     "wallet-key-1",
		Body:      []byte(`{"kind":"evm_tx","bytes":"0x02f86c"}`),
		PublicKey: f.agent.pub,
		Nonce:     randomNonce(t),
		Expiry:    uint64(f.clock.Now().Add(2 * time.Minute).Unix()),
	}
}

func TestRequestDigestLayout(t *testing.T) {
	var nonce [16]byte
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	body := []byte("payload")
	got, err := RequestDigest("sign", "key-9", nonce, 1_900_000_120, body)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	buf = append(buf, "PXW:AGENT-REQUEST:v1"...)
	buf = binary.BigEndian.AppendUint32(buf, 4)
	buf = append(buf, "sign"...)
	buf = binary.BigEndian.AppendUint32(buf, 5)
	buf = append(buf, "key-9"...)
	buf = append(buf, nonce[:]...)
	buf = binary.BigEndian.AppendUint64(buf, 1_900_000_120)
	bodyHash := sha256.Sum256(body)
	buf = append(buf, bodyHash[:]...)
	want := sha256.Sum256(buf)
	if got != want {
		t.Fatalf("digest = %x, want %x", got, want)
	}
	other, err := RequestDigest("sig", "nkey-9", nonce, 1_900_000_120, body)
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Fatal("length prefixes do not separate method and key id")
	}
}

func TestVerifyAcceptsValidRequest(t *testing.T) {
	f := newFixture(t)
	req := signRequest(t, f.agent.priv, f.request(t))
	p, err := f.verifier.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if p.PublicKey != f.agent.pub {
		t.Fatal("verified principal differs from the signing agent")
	}
}

func TestVerifyRefusesReplayedNonce(t *testing.T) {
	f := newFixture(t)
	req := signRequest(t, f.agent.priv, f.request(t))
	if _, err := f.verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("first use: %v", err)
	}
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrReplayedNonce) {
		t.Fatalf("err = %v, want %v", err, ErrReplayedNonce)
	}
}

func TestVerifyRefusesExpired(t *testing.T) {
	f := newFixture(t)
	req := f.request(t)
	req.Expiry = uint64(f.clock.Now().Add(-testSkew - time.Second).Unix())
	req = signRequest(t, f.agent.priv, req)
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want %v", err, ErrExpired)
	}
}

func TestVerifyAllowsExpiryWithinSkew(t *testing.T) {
	f := newFixture(t)
	req := f.request(t)
	req.Expiry = uint64(f.clock.Now().Add(-2 * time.Second).Unix())
	req = signRequest(t, f.agent.priv, req)
	if _, err := f.verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify within skew: %v", err)
	}
}

func TestVerifyRefusesWrongKey(t *testing.T) {
	f := newFixture(t)
	intruder := newAgentKey(t)
	req := signRequest(t, intruder.priv, f.request(t))
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want %v", err, ErrBadSignature)
	}
}

func TestVerifyRefusesUnregisteredPrincipal(t *testing.T) {
	f := newFixture(t)
	intruder := newAgentKey(t)
	req := f.request(t)
	req.PublicKey = intruder.pub
	req = signRequest(t, intruder.priv, req)
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrUnregistered) {
		t.Fatalf("err = %v, want %v", err, ErrUnregistered)
	}
}

func TestVerifyRefusesFrozenPrincipal(t *testing.T) {
	f := newFixture(t)
	f.registry.put(Principal{PublicKey: f.agent.pub, Frozen: true, KeyIDs: []string{"wallet-key-1"}})
	req := signRequest(t, f.agent.priv, f.request(t))
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrFrozen) {
		t.Fatalf("err = %v, want %v", err, ErrFrozen)
	}
}

func TestVerifyRefusesKeyIDNotOwned(t *testing.T) {
	f := newFixture(t)
	req := f.request(t)
	req.KeyID = "wallet-key-3"
	req = signRequest(t, f.agent.priv, req)
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrKeyNotOwned) {
		t.Fatalf("err = %v, want %v", err, ErrKeyNotOwned)
	}
}

func TestVerifyRefusesTamperedBody(t *testing.T) {
	f := newFixture(t)
	req := signRequest(t, f.agent.priv, f.request(t))
	req.Body = []byte(`{"kind":"evm_tx","bytes":"0x02f86d"}`)
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want %v", err, ErrBadSignature)
	}
}

func TestVerifyRefusesTamperedExpiry(t *testing.T) {
	f := newFixture(t)
	req := signRequest(t, f.agent.priv, f.request(t))
	req.Expiry += 3600
	_, err := f.verifier.Verify(context.Background(), req)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want %v", err, ErrBadSignature)
	}
}

func TestVerifyRejectedRequestDoesNotConsumeNonce(t *testing.T) {
	f := newFixture(t)
	req := signRequest(t, f.agent.priv, f.request(t))
	tampered := req
	tampered.Body = []byte("other")
	if _, err := f.verifier.Verify(context.Background(), tampered); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered err = %v, want %v", err, ErrBadSignature)
	}
	if _, err := f.verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("genuine request after tampered copy: %v", err)
	}
}

func TestNonceCacheEvictsAfterExpiry(t *testing.T) {
	clock := &testClock{t: time.Unix(1_900_000_000, 0)}
	cache := NewNonceCache(clock.Now)
	agent := newAgentKey(t)
	other := newAgentKey(t)
	nonce := randomNonce(t)
	exp := clock.Now().Add(time.Minute)
	if !cache.Use(agent.pub, nonce, exp) {
		t.Fatal("first use refused")
	}
	if cache.Use(agent.pub, nonce, exp) {
		t.Fatal("replay accepted")
	}
	if !cache.Use(other.pub, nonce, exp) {
		t.Fatal("same nonce under another key refused")
	}
	if got := cache.Len(); got != 2 {
		t.Fatalf("len = %d, want 2", got)
	}
	clock.Advance(time.Minute)
	if got := cache.Len(); got != 0 {
		t.Fatalf("len after expiry = %d, want 0", got)
	}
	if !cache.Use(agent.pub, nonce, clock.Now().Add(time.Minute)) {
		t.Fatal("use after eviction refused")
	}
}

func TestNewAgentVerifierRequiresConfiguration(t *testing.T) {
	reg := &registry{principals: make(map[[ed25519.PublicKeySize]byte]Principal)}
	if _, err := NewAgentVerifier(Config{Nonces: NewNonceCache(nil)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing principals err = %v", err)
	}
	if _, err := NewAgentVerifier(Config{Principals: reg}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing nonces err = %v", err)
	}
	if _, err := NewAgentVerifier(Config{Principals: reg, Nonces: NewNonceCache(nil), ClockSkew: -time.Second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative skew err = %v", err)
	}
}
