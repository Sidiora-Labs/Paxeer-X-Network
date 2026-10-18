package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PublicError uses the frozen v1 error vocabulary. Internal causes are never serialized.
type PublicError struct {
	Code         string       `json:"code"`
	Message      string       `json:"message"`
	RequestID    string       `json:"requestId"`
	Retryable    bool         `json:"retryable"`
	RetryAfterMS *int         `json:"retryAfterMs"`
	FieldErrors  []FieldError `json:"fieldErrors"`
	Details      any          `json:"details"`
	Status       int          `json:"-"`
}
type FieldError struct {
	Pointer string `json:"pointer"`
	Rule    string `json:"rule"`
}

func (e *PublicError) Error() string { return e.Code }
func identityError(code string, status int) error {
	return &PublicError{Code: code, Message: "Identity request could not be completed", RequestID: uuid.NewString(), Retryable: status == 503 || status == 429, FieldErrors: []FieldError{}, Status: status}
}

func unauthorized() error { return identityError("AUTH_EXPIRED", 401) }

func unavailable() error { return identityError("DEPENDENCY_UNAVAILABLE", 503) }
func invalid() error     { return identityError("FIELD_INVALID", 422) }
func conflict() error    { return identityError("STATE_CONFLICT", 409) }

type WalletProof struct {
	Kind      string `json:"kind"`
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
}
type ChallengeRequest struct {
	Wallet string `json:"wallet"`
	Origin string `json:"origin"`
}
type LoginRequest struct {
	ChallengeID string      `json:"challengeId"`
	Proof       WalletProof `json:"proof"`
	DeviceLabel string      `json:"deviceLabel"`
}
type WalletLinkChallengeRequest struct {
	Wallet     string `json:"wallet"`
	ChainID    string `json:"chainId"`
	Visibility string `json:"visibility"`
}
type WalletLinkVerifyRequest struct {
	LinkID      string      `json:"linkId"`
	ChallengeID string      `json:"challengeId"`
	Proof       WalletProof `json:"proof"`
}
type WalletLinkRemoveRequest struct {
	LinkID      string `json:"linkId"`
	ReauthProof string `json:"reauthProof"`
}
type RefreshRequest struct{}
type Challenge struct {
	ChallengeID string    `json:"challengeId"`
	Wallet      string    `json:"wallet"`
	Nonce       string    `json:"nonce"`
	Domain      string    `json:"domain"`
	URI         string    `json:"uri"`
	ChainID     string    `json:"chainId"`
	Purpose     string    `json:"purpose"`
	IssuedAt    time.Time `json:"issuedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Message     string    `json:"message"`
}
type Session struct {
	SessionID        string     `json:"sessionId"`
	Wallet           string     `json:"wallet"`
	AccessExpiresAt  time.Time  `json:"accessExpiresAt"`
	RefreshExpiresAt time.Time  `json:"refreshExpiresAt"`
	Scopes           []string   `json:"scopes"`
	DeviceLabel      string     `json:"deviceLabel"`
	RevokedAt        *time.Time `json:"revokedAt"`
}
type WalletLink struct {
	LinkID     string     `json:"linkId"`
	AccountID  string     `json:"accountId"`
	ChainID    string     `json:"chainId"`
	Wallet     string     `json:"wallet"`
	Visibility string     `json:"visibility"`
	State      string     `json:"state"`
	LinkedAt   *time.Time `json:"linkedAt"`
}
type Envelope[T any] struct {
	RequestID   string    `json:"requestId"`
	Data        T         `json:"data"`
	Watermark   any       `json:"watermark"`
	Revision    *string   `json:"revision"`
	Quality     string    `json:"quality"`
	NextCursor  *string   `json:"nextCursor"`
	GeneratedAt time.Time `json:"generatedAt"`
}
type LoginData struct {
	Session     Session `json:"session"`
	AccessToken string  `json:"accessToken"`
}

// Credentials are server-side transport output. Refresh must be sent only in a secure cookie.
type Credentials struct {
	LoginData
	RefreshSecret string `json:"-"`
	CSRFSecret    string `json:"-"`
}

// Principal cannot be constructed from a connector address. Obtain it only through Authenticate.
// Private fields prevent accidental wallet-to-account authority conversion across packages.
type Principal struct {
	accountID, sessionID, wallet string
	accessID                     string
	epoch                        int64
	reauthenticatedAt            time.Time
}

func (p Principal) AccountID() string { return p.accountID }
func (p Principal) SessionID() string { return p.sessionID }
func (p Principal) Wallet() string    { return p.wallet }
func (p Principal) AuthEpoch() int64  { return p.epoch }

// ProofVerifier must verify EOA or chain125 deployed-contract identity against these exact bytes.
// It must reject ERC1271 dependency failure rather than attempting EOA fallback.
type ProofVerifier interface {
	VerifyIdentity(context.Context, string, WalletProof) error
}
type IdentityConfig struct {
	Pool             *pgxpool.Pool
	Domain, URI      string
	AllowedOrigins   []string
	Verifier         ProofVerifier
	Clock            func() time.Time
	LimiterKey       []byte
	Observe          func(string)
	Tokens           *IdentityTokens
	SecurityAuditKey []byte
	Events           *IdentityEventDelivery
}
type Identity struct {
	measureEOACount           atomic.Uint64
	measureEOAWallNS          atomic.Uint64
	measureContractCount      atomic.Uint64
	measureAdmissionRejected  atomic.Uint64
	measureAdmissionHighWater atomic.Int64
	admission                 chan struct{}
	pool                      *pgxpool.Pool
	domain, uri               string
	origins                   map[string]bool
	verifier                  ProofVerifier
	now                       func() time.Time
	limiterKey                []byte
	observe                   func(string)
	tokens                    *IdentityTokens
	securityAuditKey          []byte
	events                    *IdentityEventDelivery
}

func NewIdentity(c IdentityConfig) (*Identity, error) {
	if c.SecurityAuditKey != nil && len(c.SecurityAuditKey) < 32 {
		return nil, invalid()
	}
	if c.Events != nil && (c.Events.Store == nil || c.Events.Subject != IdentityRevisionSubject || c.Events.EventType != IdentityRevisionEventType) {
		return nil, invalid()
	}
	u, e := url.Parse(c.URI)
	if e != nil || u.Scheme != "https" || u.Host != c.Domain || !validIdentityDomain(c.Domain) || u.User != nil || u.Fragment != "" || len(c.URI) > 2048 || strings.ContainsAny(c.Domain, "\r\n") || len(c.LimiterKey) < 32 || c.Pool == nil || c.Verifier == nil {
		return nil, invalid()
	}
	if c.Pool.Config().MaxConns > 32 {
		return nil, invalid()
	}
	origins := map[string]bool{}
	for _, v := range c.AllowedOrigins {
		o, e := url.Parse(v)
		if e != nil || o.Scheme != "https" || o.Host == "" || o.User != nil || o.Path != "" || o.RawQuery != "" || o.Fragment != "" || strings.Contains(v, "*") {
			return nil, invalid()
		}
		origins[v] = true
	}
	if len(origins) == 0 {
		return nil, invalid()
	}
	probeTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	probeID := "11111111-1111-4111-8111-111111111111"
	if _, e := BuildSIWE(Challenge{ChallengeID: probeID, Wallet: "0x1111111111111111111111111111111111111111", Nonce: strings.Repeat("a", 64), Domain: c.Domain, URI: c.URI, ChainID: "125", Purpose: "link_wallet", IssuedAt: probeTime, ExpiresAt: probeTime.Add(300 * time.Second)}, probeID); e != nil {
		return nil, e
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.Observe == nil {
		c.Observe = func(string) {}
	}
	return &Identity{admission: make(chan struct{}, 128), pool: c.Pool, domain: c.Domain, uri: c.URI, origins: origins, verifier: c.Verifier, now: c.Clock, limiterKey: append([]byte(nil), c.LimiterKey...), observe: c.Observe, tokens: c.Tokens, events: c.Events, securityAuditKey: append([]byte(nil), c.SecurityAuditKey...)}, nil
}
func validUUID(s string) bool {
	u, e := uuid.Parse(s)
	return e == nil && u != uuid.Nil && u.String() == s
}
func normalizeWallet(s string) (string, error) {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return "", invalid()
	}
	b, e := hex.DecodeString(s[2:])
	if e != nil || len(b) != 20 || s == "0x0000000000000000000000000000000000000000" {
		return "", invalid()
	}
	return strings.ToLower(s), nil
}
func randomSecret() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", unavailable()
	}
	return hex.EncodeToString(b[:]), nil
}
func digest(v string) []byte { d := sha256.Sum256([]byte(v)); return d[:] }

// Statement is intentionally deterministic and must be derived from trusted purpose/account.
func Statement(purpose, account string) (string, error) {
	switch purpose {
	case "login":
		if account != "" {
			return "", invalid()
		}
		return "Sign in to Sidiora.", nil
	case "link_wallet":
		if validUUID(account) {
			return "Link this wallet to Sidiora account " + account + ".", nil
		}
	case "step_up:account.delete":
		if validUUID(account) {
			return "Authorize account deletion for Sidiora account " + account + ".", nil
		}
	case "step_up":
		if validUUID(account) {
			return "Reauthenticate Sidiora account " + account + ".", nil
		}
	}
	return "", invalid()
}
func BuildSIWE(c Challenge, account string) (string, error) {
	wallet, e := normalizeWallet(c.Wallet)
	u, uriErr := url.Parse(c.URI)
	nonce, nonceErr := hex.DecodeString(c.Nonce)
	if e != nil || !validUUID(c.ChallengeID) || !validIdentityDomain(c.Domain) || uriErr != nil || u.Scheme != "https" || u.Host != c.Domain || u.User != nil || u.Fragment != "" || c.ChainID != "125" || nonceErr != nil || len(nonce) != 32 || c.IssuedAt.Year() < 1 || c.IssuedAt.Year() > 9999 || !c.ExpiresAt.Equal(c.IssuedAt.Add(300*time.Second)) {
		return "", invalid()
	}
	statement, e := Statement(c.Purpose, account)
	if e != nil {
		return "", e
	}
	message := fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n%s\n\n%s\n\nURI: %s\nVersion: 1\nChain ID: 125\nNonce: %s\nIssued At: %s\nExpiration Time: %s\nRequest ID: %s", c.Domain, common.HexToAddress(wallet).Hex(), statement, c.URI, c.Nonce, c.IssuedAt.UTC().Format("2006-01-02T15:04:05.000Z"), c.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z"), c.ChallengeID)
	if len(message) > 2048 {
		return "", invalid()
	}
	return message, nil
}
func (s *Identity) observeError(err error) error {
	var p *PublicError
	if errors.As(err, &p) {
		s.observe(p.Code)
	}
	return err
}

func (s *Identity) enter() error {
	select {
	case s.admission <- struct{}{}:
		used := int64(len(s.admission))
		for prior := s.measureAdmissionHighWater.Load(); used > prior; prior = s.measureAdmissionHighWater.Load() {
			if s.measureAdmissionHighWater.CompareAndSwap(prior, used) {
				break
			}
		}
		return nil
	default:
		s.measureAdmissionRejected.Add(1)
		s.observe("saturated")
		return identityError("RATE_LIMITED", 429)
	}
}
func (s *Identity) leave() { <-s.admission }

var identityDomainLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

func validIdentityDomain(v string) bool {
	if len(v) == 0 || len(v) > 253 {
		return false
	}
	for _, label := range strings.Split(v, ".") {
		if len(label) > 63 || !identityDomainLabel.MatchString(label) {
			return false
		}
	}
	return true
}

type Chain125ProofVerifier struct {
	Backend sharedauth.ContractBackend
	Observe sharedauth.ObserveVerification
}

func (v Chain125ProofVerifier) VerifyIdentity(ctx context.Context, message string, proof WalletProof) error {
	_, err := sharedauth.VerifyProof(ctx, v.Backend, sharedauth.WalletProof{Kind: proof.Kind, Wallet: proof.Wallet, Signature: proof.Signature}, message, v.Observe)
	if err != nil {
		var failure *sharedauth.VerificationError
		if errors.As(err, &failure) && failure.Retryable {
			return unavailable()
		}
		return unauthorized()
	}
	return nil
}

const IdentitySerialDDL = `ALTER TABLE identity_tickets ADD COLUMN binding jsonb;
CREATE TABLE identity_challenge_requests(wallet text NOT NULL,request_id uuid NOT NULL,body_hash bytea NOT NULL,response jsonb NOT NULL,retain_until timestamptz NOT NULL,PRIMARY KEY(wallet,request_id));`

const IdentityActionReceiptUpgradeDDL = `ALTER TABLE users.identity_challenges DROP CONSTRAINT identity_challenges_purpose_check;
ALTER TABLE users.identity_challenges ADD CONSTRAINT identity_challenges_purpose_check CHECK(purpose IN ('login','link_wallet','step_up','step_up:account.delete'));
ALTER TABLE users.identity_reauth DROP CONSTRAINT identity_reauth_action_check;
ALTER TABLE users.identity_reauth ADD CONSTRAINT identity_reauth_action_check CHECK(action IN ('wallet_unlink','account.delete'));`
