package jwt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	jwxjwt "github.com/lestrrat-go/jwx/v2/jwt"
)

var (
	ErrMalformed        = errors.New("jwt: malformed token")
	ErrAlgorithm        = errors.New("jwt: algorithm not accepted")
	ErrUnknownKey       = errors.New("jwt: no key matches the token key id")
	ErrKeySetFetch      = errors.New("jwt: key set cannot be fetched")
	ErrSignature        = errors.New("jwt: signature invalid")
	ErrClaims           = errors.New("jwt: claims invalid")
	ErrNotOwner         = errors.New("jwt: subject does not own the key id")
	ErrOwnershipLookup  = errors.New("jwt: ownership lookup failed")
	ErrInvalidConfig    = errors.New("jwt: invalid configuration")
	ErrMissingSubject   = errors.New("jwt: subject missing")
	ErrMissingKeyIDName = errors.New("jwt: key id being signed for is empty")
	ErrTooOld           = errors.New("jwt: token is older than the maximum age")
	ErrReplayed         = errors.New("jwt: token has already authorised this request")
	ErrReplayStore      = errors.New("jwt: replay store failed")
	ErrRequest          = errors.New("jwt: request cannot be canonicalised")
)

const DefaultMaxAge = time.Hour

type TokenReplayStore interface {
	UseTokenRequest(token [32]byte, request [32]byte, expiresAt time.Time) (bool, error)
}

func TokenID(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

func RequestDigest(method, keyID string, body []byte) ([32]byte, error) {
	if method == "" || keyID == "" {
		return [32]byte{}, fmt.Errorf("%w: method and key id are required", ErrRequest)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return [32]byte{}, fmt.Errorf("%w: %v", ErrRequest, err)
	}
	if _, ok := v.(map[string]any); !ok {
		return [32]byte{}, fmt.Errorf("%w: body is not a JSON object", ErrRequest)
	}
	if dec.More() {
		return [32]byte{}, fmt.Errorf("%w: trailing data after the body", ErrRequest)
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: %v", ErrRequest, err)
	}
	h := sha256.New()
	for _, field := range [][]byte{[]byte("attestor bearer request v1"), []byte(method), []byte(keyID), canonical} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(field)))
		h.Write(n[:])
		h.Write(field)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

type Config struct {
	JWKSURL            string
	Issuer             string
	Audience           string
	MinRefreshInterval time.Duration
	ClockSkew          time.Duration
	HTTPClient         *http.Client
	Now                func() time.Time
	MaxAge             time.Duration
	Replay             TokenReplayStore
}

type TokenVerifier struct {
	jwksURL    string
	issuer     string
	audience   string
	minRefresh time.Duration
	skew       time.Duration
	client     *http.Client
	now        func() time.Time
	maxAge     time.Duration
	replay     TokenReplayStore

	mu          sync.Mutex
	keys        jwk.Set
	lastFetch   time.Time
	lastFetchOK bool
}

func NewTokenVerifier(cfg Config) (*TokenVerifier, error) {
	if cfg.JWKSURL == "" || cfg.Issuer == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("%w: jwks url, issuer and audience are required", ErrInvalidConfig)
	}
	if cfg.HTTPClient == nil {
		return nil, fmt.Errorf("%w: http client is required", ErrInvalidConfig)
	}
	if cfg.MinRefreshInterval < 0 || cfg.ClockSkew < 0 || cfg.MaxAge < 0 {
		return nil, fmt.Errorf("%w: negative interval", ErrInvalidConfig)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = DefaultMaxAge
	}
	return &TokenVerifier{
		maxAge:     maxAge,
		replay:     cfg.Replay,
		jwksURL:    cfg.JWKSURL,
		issuer:     cfg.Issuer,
		audience:   cfg.Audience,
		minRefresh: cfg.MinRefreshInterval,
		skew:       cfg.ClockSkew,
		client:     cfg.HTTPClient,
		now:        now,
	}, nil
}

func (v *TokenVerifier) Verify(ctx context.Context, token string, keyID string, request [32]byte, owns func(subject, keyID string) (bool, error)) (string, error) {
	if keyID == "" {
		return "", ErrMissingKeyIDName
	}
	if owns == nil {
		return "", fmt.Errorf("%w: no ownership lookup supplied", ErrOwnershipLookup)
	}
	msg, err := jws.ParseString(token)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	sigs := msg.Signatures()
	if len(sigs) != 1 {
		return "", fmt.Errorf("%w: expected exactly one signature", ErrMalformed)
	}
	hdr := sigs[0].ProtectedHeaders()
	alg := hdr.Algorithm()
	if alg != jwa.RS256 && alg != jwa.ES256 {
		return "", fmt.Errorf("%w: %s", ErrAlgorithm, alg)
	}
	kid := hdr.KeyID()
	if kid == "" {
		return "", fmt.Errorf("%w: token names no key id", ErrUnknownKey)
	}
	key, err := v.lookupKey(ctx, kid)
	if err != nil {
		return "", err
	}
	if err := keyMatchesAlgorithm(key, alg); err != nil {
		return "", err
	}
	if _, err := jws.Verify([]byte(token), jws.WithKey(alg, key)); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSignature, err)
	}
	tok, err := jwxjwt.ParseString(token,
		jwxjwt.WithKey(alg, key),
		jwxjwt.WithValidate(true),
		jwxjwt.WithContext(ctx),
		jwxjwt.WithIssuer(v.issuer),
		jwxjwt.WithAudience(v.audience),
		jwxjwt.WithRequiredClaim(jwxjwt.ExpirationKey),
		jwxjwt.WithRequiredClaim(jwxjwt.IssuedAtKey),
		jwxjwt.WithAcceptableSkew(v.skew),
		jwxjwt.WithClock(jwxjwt.ClockFunc(v.now)),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrClaims, err)
	}
	if age := v.now().Sub(tok.IssuedAt()); age > v.maxAge+v.skew {
		return "", fmt.Errorf("%w: issued %s ago, maximum %s", ErrTooOld, age.Truncate(time.Second), v.maxAge)
	}
	subject := tok.Subject()
	if subject == "" {
		return "", ErrMissingSubject
	}
	ok, err := owns(subject, keyID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOwnershipLookup, err)
	}
	if !ok {
		return "", ErrNotOwner
	}
	if v.replay != nil {
		expiresAt := tok.Expiration()
		if limit := tok.IssuedAt().Add(v.maxAge); limit.Before(expiresAt) {
			expiresAt = limit
		}
		fresh, err := v.replay.UseTokenRequest(TokenID(token), request, expiresAt.Add(v.skew))
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrReplayStore, err)
		}
		if !fresh {
			return "", ErrReplayed
		}
	}
	return subject, nil
}

func keyMatchesAlgorithm(key jwk.Key, alg jwa.SignatureAlgorithm) error {
	if keyAlg := key.Algorithm(); keyAlg != nil && keyAlg.String() != "" && keyAlg.String() != alg.String() {
		return fmt.Errorf("%w: key is for %s, token uses %s", ErrAlgorithm, keyAlg, alg)
	}
	switch alg {
	case jwa.RS256:
		if _, ok := key.(jwk.RSAPublicKey); !ok {
			return fmt.Errorf("%w: RS256 requires an RSA public key", ErrAlgorithm)
		}
	case jwa.ES256:
		pub, ok := key.(jwk.ECDSAPublicKey)
		if !ok || pub.Crv() != jwa.P256 {
			return fmt.Errorf("%w: ES256 requires a P-256 public key", ErrAlgorithm)
		}
	default:
		return fmt.Errorf("%w: %s", ErrAlgorithm, alg)
	}
	return nil
}

func (v *TokenVerifier) lookupKey(ctx context.Context, kid string) (jwk.Key, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.keys != nil {
		if key, ok := v.keys.LookupKeyID(kid); ok {
			return key, nil
		}
	}
	now := v.now()
	if !v.lastFetch.IsZero() && now.Sub(v.lastFetch) < v.minRefresh {
		if !v.lastFetchOK && v.keys == nil {
			return nil, fmt.Errorf("%w: refresh interval not elapsed since failed fetch", ErrKeySetFetch)
		}
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}
	v.lastFetch = now
	set, err := jwk.Fetch(ctx, v.jwksURL, jwk.WithHTTPClient(v.client))
	if err != nil {
		v.lastFetchOK = false
		return nil, fmt.Errorf("%w: %v", ErrKeySetFetch, err)
	}
	v.lastFetchOK = true
	v.keys = set
	if key, ok := set.LookupKeyID(kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownKey, kid)
}
