// Package auth implements the wallet sign-in of the perps data gateway: a
// single-use challenge, its EIP-191 signature check and the HS256 bearer token
// that the account routes act for. Signatures, tokens and the secret are never
// logged and never echoed in a response.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v4"
)

const (
	// ChallengeTTL is how long a challenge nonce stays valid.
	ChallengeTTL = 5 * time.Minute
	// TokenTTL is the lifetime of one issued token.
	TokenTTL = 3600 * time.Second
	// MaxChain is the longest a chain of refreshes may run from its sign-in.
	MaxChain = 24 * time.Hour
	// MinSecretLen is the shortest accepted PERPS_AUTH_SECRET.
	MinSecretLen = 32
	// ClockSkew is how far in the future a token's iat may be: replicas behind
	// one edge never share a clock exactly, and a token issued by one must
	// verify on the next.
	ClockSkew = 30 * time.Second

	nonceLen     = 32
	maxBodyBytes = 4096
	bearerPrefix = "Bearer "

	insertChallengeSQL = `INSERT INTO perps.auth_challenges (nonce, address, message, issued_at, expires_at) VALUES ($1, $2, $3, $4, $5)`
	consumeSQL         = `UPDATE perps.auth_challenges SET used_at = $3 WHERE address = $1 AND message = $2 AND used_at IS NULL AND expires_at > $3 RETURNING nonce`
)

var (
	errInvalidToken = errors.New("invalid token")
	errChainExpired = errors.New("sign-in older than 24 hours")
)

// Service issues challenges and tokens against perps.auth_challenges.
type Service struct {
	db     *sql.DB
	secret []byte
	now    func() time.Time
}

// NewService returns a Service over db that signs tokens with secret and reads
// time from now; it refuses a secret shorter than MinSecretLen bytes.
func NewService(db *sql.DB, secret string, now func() time.Time) (*Service, error) {
	if db == nil {
		return nil, errors.New("auth: database is required")
	}
	if len(secret) < MinSecretLen {
		return nil, fmt.Errorf("auth: secret must be at least %d bytes", MinSecretLen)
	}
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, secret: []byte(secret), now: now}, nil
}

type challengeRequest struct {
	Address string `json:"address"`
}

type challengeResponse struct {
	Nonce   string `json:"nonce"`
	Message string `json:"message"`
}

type verifyRequest struct {
	Address   string `json:"address"`
	Signature string `json:"signature"`
	Message   string `json:"message"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type claims struct {
	OriginIAT int64 `json:"origin_iat"`
	jwt.RegisteredClaims
}

// Challenge serves POST /v2/auth/challenge: {address} answers {nonce, message}
// with a fresh 32-byte nonce stored for ChallengeTTL.
func (s *Service) Challenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req challengeRequest
	if !decode(w, r, &req) {
		return
	}
	addr, ok := parseAddress(req.Address)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid address")
		return
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	issued := s.now().UTC()
	nonceHex := hexutil.Encode(nonce)
	message := challengeMessage(addr, nonceHex, issued)
	if _, err := s.db.ExecContext(r.Context(), insertChallengeSQL, nonce, addr.Bytes(), message, issued, issued.Add(ChallengeTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{Nonce: nonceHex, Message: message})
}

// Verify serves POST /v2/auth/verify: {address, signature, message} answers
// {token, expires_in} when the EIP-191 signer of message is address and message
// is an unused, unexpired challenge of address; the challenge is consumed.
func (s *Service) Verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req verifyRequest
	if !decode(w, r, &req) {
		return
	}
	addr, ok := parseAddress(req.Address)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid address")
		return
	}
	sig, err := hexutil.Decode(req.Signature)
	if err != nil || len(sig) != crypto.SignatureLength {
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "invalid message")
		return
	}
	signer, ok := recoverSigner(req.Message, sig)
	if !ok || signer != addr {
		writeError(w, http.StatusUnauthorized, "signature does not match address")
		return
	}
	now := s.now().UTC()
	var nonce []byte
	switch err := s.db.QueryRowContext(r.Context(), consumeSQL, addr.Bytes(), req.Message, now).Scan(&nonce); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusUnauthorized, "unknown, used or expired challenge")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.issue(w, lower(addr), now, now)
}

// Refresh serves POST /v2/auth/refresh: a valid bearer token whose sign-in is
// at most MaxChain old answers a new {token, expires_in} of the same chain.
func (s *Service) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := s.now().UTC()
	c, err := s.bearer(r, now)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	origin := time.Unix(c.OriginIAT, 0)
	if now.Sub(origin) > MaxChain {
		writeError(w, http.StatusUnauthorized, errChainExpired.Error())
		return
	}
	if origin.After(now) {
		// A sign-in stamped by a replica whose clock runs ahead: the chain
		// starts no later than this clock, so the new token's origin never
		// follows its own iat.
		origin = now
	}
	s.issue(w, c.Subject, origin, now)
}

func (s *Service) issue(w http.ResponseWriter, sub string, origin, now time.Time) {
	token, err := s.sign(sub, origin, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{Token: token, ExpiresIn: int64(TokenTTL / time.Second)})
}

func (s *Service) sign(sub string, origin, now time.Time) (string, error) {
	c := claims{
		OriginIAT: origin.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

// parse validates an HS256 token against the secret and the Service clock,
// accepting an iat up to ClockSkew ahead of it, and returns its claims.
func (s *Service) parse(token string, now time.Time) (*claims, error) {
	var c claims
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithoutClaimsValidation())
	if _, err := parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.secret, nil }); err != nil {
		return nil, errInvalidToken
	}
	if c.IssuedAt == nil || c.ExpiresAt == nil || c.OriginIAT <= 0 {
		return nil, errInvalidToken
	}
	iat, exp := c.IssuedAt.Unix(), c.ExpiresAt.Unix()
	if c.OriginIAT > iat || iat > now.Add(ClockSkew).Unix() || now.Unix() >= exp {
		return nil, errInvalidToken
	}
	if addr, ok := parseAddress(c.Subject); !ok || lower(addr) != c.Subject {
		return nil, errInvalidToken
	}
	return &c, nil
}

func (s *Service) bearer(r *http.Request, now time.Time) (*claims, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return nil, errors.New("missing bearer token")
	}
	return s.parse(strings.TrimSpace(strings.TrimPrefix(h, bearerPrefix)), now)
}

type addressKey struct{}

// Middleware puts the lower-case address of a valid bearer token on the
// request context, read back with Address, and answers 401 {error} otherwise.
func Middleware(s *Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := s.bearer(r, s.now().UTC())
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), addressKey{}, c.Subject)))
	})
}

// Address returns the lower-case address Middleware put on ctx.
func Address(ctx context.Context) (string, bool) {
	a, ok := ctx.Value(addressKey{}).(string)
	return a, ok
}

func challengeMessage(addr common.Address, nonce string, issued time.Time) string {
	return "Sign in to Paxeer X Network perps\n\nAddress: " + lower(addr) +
		"\nNonce: " + nonce +
		"\nIssued at: " + issued.Format(time.RFC3339) +
		"\nExpires at: " + issued.Add(ChallengeTTL).Format(time.RFC3339)
}

func recoverSigner(message string, sig []byte) (common.Address, bool) {
	rs := make([]byte, len(sig))
	copy(rs, sig)
	if rs[crypto.RecoveryIDOffset] >= 27 {
		rs[crypto.RecoveryIDOffset] -= 27
	}
	if rs[crypto.RecoveryIDOffset] > 1 {
		return common.Address{}, false
	}
	pub, err := crypto.SigToPub(accounts.TextHash([]byte(message)), rs)
	if err != nil {
		return common.Address{}, false
	}
	return crypto.PubkeyToAddress(*pub), true
}

func parseAddress(s string) (common.Address, bool) {
	if len(s) != 2+2*common.AddressLength || !common.IsHexAddress(s) {
		return common.Address{}, false
	}
	return common.HexToAddress(s), true
}

func lower(a common.Address) string { return strings.ToLower(a.Hex()) }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
