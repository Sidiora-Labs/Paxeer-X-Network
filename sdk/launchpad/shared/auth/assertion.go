package auth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	ActorAssertionHeader     = "X-Kindle-Service-Assertion"
	ActorSignatureHeader     = "X-Kindle-Service-Signature"
	IdentityAssertionHeader  = "X-Kindle-Identity-Assertion"
	IdentitySignatureHeader  = "X-Kindle-Identity-Signature"
	OperationEnvelopeHeader  = "X-Kindle-Operation"
	OperationSignatureHeader = "X-Kindle-Operation-Signature"
)

type ServiceAssertionIssuer struct {
	Issuer, Audience string
	Key              ed25519.PrivateKey
	Transport        AssertionTransport
}

func (s ServiceAssertionIssuer) SignRequest(r *http.Request, identity IdentityPrincipal, roles []string, body []byte) error {
	if r == nil || r.URL == nil || len(s.Key) != ed25519.PrivateKeySize || !tokenPattern.MatchString(s.Issuer) || s.Audience == "" || !validIdentityID(identity.AccountID) || !validIdentityID(identity.SessionID) || !validIdentityID(r.Header.Get("X-Request-ID")) || !identity.AccessExpiresAt.After(time.Now()) || identity.AuthEpoch < 0 || identity.ProofAt.IsZero() || len(roles) > 16 {
		return ErrScopeDenied
	}
	if identity.Validate(time.Now()) != nil {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(identity.Wallet)
	if err != nil || wallet != identity.Wallet {
		return ErrScopeDenied
	}
	for _, role := range roles {
		if !tokenPattern.MatchString(role) {
			return ErrScopeDenied
		}
	}
	now := time.Now().Unix()
	expires := now + 30
	if expiry := identity.AccessExpiresAt.Unix(); expiry < expires {
		expires = expiry
	}
	if expires <= now {
		return ErrScopeDenied
	}
	a := ServiceAssertion{Version: 1, Issuer: s.Issuer, Audience: s.Audience, Principal: Principal{Wallet: wallet, SessionID: identity.SessionID, Roles: append([]string(nil), roles...), TraceID: r.Header.Get("X-Request-ID")}, RequestDigest: RequestDigest(r.Method, r.URL.RequestURI(), body), IssuedAt: now, ExpiresAt: expires}
	return writeSignedAssertion(r, s.Transport, s.Key, a)
}

type ServiceRequestAssertion struct {
	Version       int    `json:"version"`
	Issuer        string `json:"issuer"`
	Audience      string `json:"audience"`
	RequestDigest string `json:"requestDigest"`
	TraceID       string `json:"traceId"`
	IssuedAt      int64  `json:"issuedAt"`
	ExpiresAt     int64  `json:"expiresAt"`
}

type ServiceRequestIssuer struct {
	Issuer, Audience string
	Key              ed25519.PrivateKey
	Transport        AssertionTransport
}

func writeSignedAssertion(r *http.Request, t AssertionTransport, key ed25519.PrivateKey, value any) error {
	if r == nil || r.Header == nil || len(key) != ed25519.PrivateKeySize || t.AssertionHeader == "" || t.SignatureHeader == "" || http.CanonicalHeaderKey(t.AssertionHeader) == http.CanonicalHeaderKey(t.SignatureHeader) {
		return ErrScopeDenied
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 8192 {
		return ErrScopeDenied
	}
	r.Header.Set(t.AssertionHeader, base64.RawURLEncoding.EncodeToString(raw))
	r.Header.Set(t.SignatureHeader, base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw)))
	return nil
}

func (s ServiceRequestIssuer) SignRequest(r *http.Request, body []byte) error {
	if r == nil || r.URL == nil || !tokenPattern.MatchString(s.Issuer) || !tokenPattern.MatchString(s.Audience) || !validIdentityID(r.Header.Get("X-Request-ID")) || len(body) > 1<<20 {
		return ErrScopeDenied
	}
	now := time.Now().Unix()
	return writeSignedAssertion(r, s.Transport, s.Key, ServiceRequestAssertion{Version: 2, Issuer: s.Issuer, Audience: s.Audience, RequestDigest: RequestDigest(r.Method, r.URL.RequestURI(), body), TraceID: r.Header.Get("X-Request-ID"), IssuedAt: now, ExpiresAt: now + 30})
}

type ServiceRequestVerifier struct {
	Transport                        AssertionTransport
	Issuer, Audience, PeerCertSHA256 string
	Key                              ed25519.PublicKey
}

func (v ServiceRequestVerifier) Authenticate(r *http.Request, body []byte) error {
	if r == nil || r.URL == nil || r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || len(body) > 1<<20 || len(v.Key) != ed25519.PublicKeySize || strings.EqualFold(v.Transport.AssertionHeader, v.Transport.SignatureHeader) || len(r.Header.Values("X-Request-ID")) != 1 {
		return ErrScopeDenied
	}
	digest := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	if len(v.PeerCertSHA256) != 64 || hex.EncodeToString(digest[:]) != v.PeerCertSHA256 {
		return ErrScopeDenied
	}
	encoded, err := authorizationHeader(r, v.Transport.AssertionHeader)
	if err != nil {
		return err
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return ErrScopeDenied
	}
	var a ServiceRequestAssertion
	if decodeAuthorizationJSON(raw, &a) != nil {
		return ErrScopeDenied
	}
	encoded, err = authorizationHeader(r, v.Transport.SignatureHeader)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrScopeDenied
	}
	now := time.Now().Unix()
	if a.Version != 2 || a.Issuer != v.Issuer || !tokenPattern.MatchString(a.Issuer) || a.Audience != v.Audience || !tokenPattern.MatchString(a.Audience) || a.RequestDigest != RequestDigest(r.Method, r.URL.RequestURI(), body) || !validIdentityID(a.TraceID) || a.TraceID != r.Header.Get("X-Request-ID") || a.IssuedAt < 0 || a.IssuedAt > now || a.ExpiresAt <= now || a.ExpiresAt <= a.IssuedAt || a.ExpiresAt-a.IssuedAt > 30 {
		return ErrScopeDenied
	}
	canonical, _ := json.Marshal(a)
	if !ed25519.Verify(v.Key, canonical, signature) {
		return ErrScopeDenied
	}
	return nil
}

type ServiceAssertion struct {
	Version       int       `json:"version"`
	Issuer        string    `json:"issuer"`
	Audience      string    `json:"audience"`
	Principal     Principal `json:"principal"`
	RequestDigest string    `json:"requestDigest"`
	IssuedAt      int64     `json:"issuedAt"`
	ExpiresAt     int64     `json:"expiresAt"`
}

func RequestDigest(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyServiceAssertion requires TLS verified chains and an explicitly pinned
// peer certificate digest for the same assertion issuer. Headers are irrelevant.
func VerifyServiceAssertion(a ServiceAssertion, signature []byte, key ed25519.PublicKey, connection *tls.ConnectionState, peerCertSHA256, expectedIssuer, audience, digest string, now time.Time) (Principal, error) {
	if connection == nil || len(connection.VerifiedChains) == 0 || len(connection.PeerCertificates) == 0 {
		return Principal{}, fmt.Errorf("authenticated peer required")
	}
	cert := sha256.Sum256(connection.PeerCertificates[0].Raw)
	if hex.EncodeToString(cert[:]) != peerCertSHA256 || len(peerCertSHA256) != 64 {
		return Principal{}, fmt.Errorf("peer mismatch")
	}
	if a.Version != 1 || a.Issuer != expectedIssuer || !tokenPattern.MatchString(a.Issuer) || a.Audience != audience || a.RequestDigest != digest || a.IssuedAt < 0 || a.ExpiresAt < 0 || a.ExpiresAt-a.IssuedAt > 30 || a.ExpiresAt <= a.IssuedAt || a.IssuedAt > now.Unix() || a.ExpiresAt <= now.Unix() {
		return Principal{}, fmt.Errorf("assertion binding")
	}
	if _, err := NormalizeWallet(a.Principal.Wallet); err != nil || a.Principal.SessionID == "" || a.Principal.TraceID == "" || len(a.Principal.Roles) > 16 {
		return Principal{}, fmt.Errorf("principal invalid")
	}
	encoded, _ := json.Marshal(a)
	if len(encoded) > 8192 || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, encoded, signature) {
		return Principal{}, fmt.Errorf("assertion signature")
	}
	return a.Principal, nil
}
