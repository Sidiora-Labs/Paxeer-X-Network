package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

const digestDomain = "PXW:AGENT-REQUEST:v1"

var (
	ErrUnregistered   = errors.New("agent: principal not registered")
	ErrFrozen         = errors.New("agent: principal frozen")
	ErrExpired        = errors.New("agent: request expired")
	ErrKeyNotOwned    = errors.New("agent: key id not owned by principal")
	ErrBadSignature   = errors.New("agent: signature invalid")
	ErrReplayedNonce  = errors.New("agent: nonce replayed")
	ErrMalformed      = errors.New("agent: malformed request")
	ErrInvalidConfig  = errors.New("agent: invalid configuration")
	ErrKeyMismatch    = errors.New("agent: registered key differs from request key")
	ErrFieldTooLong   = errors.New("agent: field exceeds length limit")
	ErrExpiryOverflow = errors.New("agent: expiry out of range")
	ErrExpiryTooFar   = errors.New("agent: expiry beyond the accepted maximum")
	ErrNonceStore     = errors.New("agent: nonce store failed")
)

const DefaultMaxExpiry = 5 * time.Minute

type AgentNonceStore interface {
	UseNonce(pub [ed25519.PublicKeySize]byte, nonce [16]byte, expiresAt time.Time) (bool, error)
}

type Principal struct {
	PublicKey [ed25519.PublicKeySize]byte
	Frozen    bool
	KeyIDs    []string
}

type PrincipalSet interface {
	Lookup(pub [ed25519.PublicKeySize]byte) (Principal, bool)
}

type Request struct {
	Method    string
	KeyID     string
	Body      []byte
	PublicKey [ed25519.PublicKeySize]byte
	Nonce     [16]byte
	Expiry    uint64
	Signature [ed25519.SignatureSize]byte
}

func RequestDigest(method, keyID string, nonce [16]byte, expiry uint64, body []byte) ([32]byte, error) {
	if uint64(len(method)) > uint64(^uint32(0)) || uint64(len(keyID)) > uint64(^uint32(0)) {
		return [32]byte{}, ErrFieldTooLong
	}
	bodyHash := sha256.Sum256(body)
	h := sha256.New()
	var u32 [4]byte
	var u64 [8]byte
	h.Write([]byte(digestDomain))
	binary.BigEndian.PutUint32(u32[:], uint32(len(method)))
	h.Write(u32[:])
	h.Write([]byte(method))
	binary.BigEndian.PutUint32(u32[:], uint32(len(keyID)))
	h.Write(u32[:])
	h.Write([]byte(keyID))
	h.Write(nonce[:])
	binary.BigEndian.PutUint64(u64[:], expiry)
	h.Write(u64[:])
	h.Write(bodyHash[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

type nonceKey struct {
	pub   [ed25519.PublicKeySize]byte
	nonce [16]byte
}

type NonceCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[nonceKey]time.Time
}

func NewNonceCache(now func() time.Time) *NonceCache {
	if now == nil {
		now = time.Now
	}
	return &NonceCache{now: now, entries: make(map[nonceKey]time.Time)}
}

func (c *NonceCache) Use(pub [ed25519.PublicKeySize]byte, nonce [16]byte, expiresAt time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, exp := range c.entries {
		if !now.Before(exp) {
			delete(c.entries, k)
		}
	}
	k := nonceKey{pub: pub, nonce: nonce}
	if _, seen := c.entries[k]; seen {
		return false
	}
	c.entries[k] = expiresAt
	return true
}

func (c *NonceCache) UseNonce(pub [ed25519.PublicKeySize]byte, nonce [16]byte, expiresAt time.Time) (bool, error) {
	return c.Use(pub, nonce, expiresAt), nil
}

func (c *NonceCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, exp := range c.entries {
		if !now.Before(exp) {
			delete(c.entries, k)
		}
	}
	return len(c.entries)
}

type Config struct {
	Principals PrincipalSet
	Nonces     AgentNonceStore
	ClockSkew  time.Duration
	MaxExpiry  time.Duration
	Now        func() time.Time
}

type AgentVerifier struct {
	principals PrincipalSet
	nonces     AgentNonceStore
	skew       time.Duration
	maxExpiry  time.Duration
	now        func() time.Time
}

func NewAgentVerifier(cfg Config) (*AgentVerifier, error) {
	if cfg.Principals == nil || cfg.Nonces == nil {
		return nil, fmt.Errorf("%w: principal set and nonce cache are required", ErrInvalidConfig)
	}
	if cfg.ClockSkew < 0 {
		return nil, fmt.Errorf("%w: negative clock skew", ErrInvalidConfig)
	}
	if cfg.MaxExpiry < 0 {
		return nil, fmt.Errorf("%w: negative maximum expiry", ErrInvalidConfig)
	}
	maxExpiry := cfg.MaxExpiry
	if maxExpiry == 0 {
		maxExpiry = DefaultMaxExpiry
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AgentVerifier{principals: cfg.Principals, nonces: cfg.Nonces, skew: cfg.ClockSkew, maxExpiry: maxExpiry, now: now}, nil
}

func (v *AgentVerifier) Verify(ctx context.Context, req Request) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	if req.Method == "" || req.KeyID == "" {
		return Principal{}, fmt.Errorf("%w: method and key id are required", ErrMalformed)
	}
	principal, ok := v.principals.Lookup(req.PublicKey)
	if !ok {
		return Principal{}, ErrUnregistered
	}
	if principal.PublicKey != req.PublicKey {
		return Principal{}, ErrKeyMismatch
	}
	if principal.Frozen {
		return Principal{}, ErrFrozen
	}
	if req.Expiry > uint64(1<<62) {
		return Principal{}, ErrExpiryOverflow
	}
	expiresAt := time.Unix(int64(req.Expiry), 0)
	now := v.now()
	if !now.Before(expiresAt.Add(v.skew)) {
		return Principal{}, ErrExpired
	}
	owned := false
	for _, id := range principal.KeyIDs {
		if id == req.KeyID {
			owned = true
			break
		}
	}
	if !owned {
		return Principal{}, ErrKeyNotOwned
	}
	digest, err := RequestDigest(req.Method, req.KeyID, req.Nonce, req.Expiry, req.Body)
	if err != nil {
		return Principal{}, err
	}
	if !ed25519.Verify(ed25519.PublicKey(principal.PublicKey[:]), digest[:], req.Signature[:]) {
		return Principal{}, ErrBadSignature
	}
	if expiresAt.After(now.Add(v.maxExpiry + v.skew)) {
		return Principal{}, ErrExpiryTooFar
	}
	fresh, err := v.nonces.UseNonce(req.PublicKey, req.Nonce, expiresAt.Add(v.skew))
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrNonceStore, err)
	}
	if !fresh {
		return Principal{}, ErrReplayedNonce
	}
	return principal, nil
}
