package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// jwtHeader is the fixed HS256 header; encoded once at init.
var encodedHeader = b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

var b64 = base64.RawURLEncoding

// ErrInvalidToken is returned when a session token fails structural, signature,
// or expiry validation. Callers treat every failure identically (401) so the
// reason is never leaked to the client.
var ErrInvalidToken = errors.New("auth: invalid token")

// claims is the session JWT payload. Subject carries the (lowercased) wallet.
type claims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// signer mints and verifies HS256 session tokens with a single shared secret.
// It is a hand-rolled, dependency-free JWT (header is fixed to HS256) to avoid
// pulling a JWT library for one token shape.
type signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func newSigner(secret string, ttl time.Duration, now func() time.Time) *signer {
	if now == nil {
		now = time.Now
	}
	return &signer{secret: []byte(secret), ttl: ttl, now: now}
}

// mint returns a signed token for wallet and the absolute expiry time.
func (s *signer) mint(wallet string) (string, time.Time) {
	iat := s.now()
	exp := iat.Add(s.ttl)
	payload, _ := json.Marshal(claims{Sub: wallet, Iat: iat.Unix(), Exp: exp.Unix()})
	signingInput := encodedHeader + "." + b64.EncodeToString(payload)
	sig := s.sign(signingInput)
	return signingInput + "." + sig, exp
}

// verify validates a token and returns its subject (wallet) on success.
func (s *signer) verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ErrInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(encodedHeader)) != 1 {
		return "", ErrInvalidToken
	}
	expected := s.sign(parts[0] + "." + parts[1])
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expected)) != 1 {
		return "", ErrInvalidToken
	}
	raw, err := b64.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidToken
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", ErrInvalidToken
	}
	if c.Sub == "" || c.Exp == 0 || s.now().Unix() >= c.Exp {
		return "", ErrInvalidToken
	}
	return c.Sub, nil
}

func (s *signer) sign(input string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(input))
	return b64.EncodeToString(mac.Sum(nil))
}
