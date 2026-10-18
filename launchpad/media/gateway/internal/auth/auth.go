// Package auth implements the gateway's sign-in-once identity: an EIP-191
// (personal_sign) challenge/response that mints a short-lived HS256 JWT session.
// The session is how the gateway establishes the trusted actor wallet it injects
// (as X-Actor-Wallet) when fronting media/social, whose writes are sign-free
// (2026-06-26 decision) and therefore trust the gateway as the sole ingress.
//
// Flow: POST /auth/nonce {wallet} -> a single-use nonce + a human-readable
// message to sign; POST /auth/login {wallet,message,signature} -> verifies the
// signature and consumes the nonce, returning a bearer token; GET /auth/me
// echoes the authenticated wallet. Nonces live in Redis (single-use, TTL'd).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/common"
)

const noncePrefix = "gw:nonce:"

var nonceRe = regexp.MustCompile(`(?m)^Nonce:\s*([0-9a-fA-F]+)\s*$`)

type ctxKey struct{}

// actorKey is the request-context key under which the authenticated wallet is
// stored by the session middleware.
var actorKey = ctxKey{}

// Deps are the auth handler dependencies.
type Deps struct {
	Redis     *goredis.Client
	JWTSecret string
	JWTTTL    time.Duration
	NonceTTL  time.Duration
	AppDomain string
	Logger    *slog.Logger
	// Clock defaults to time.Now (overridable in tests).
	Clock func() time.Time
}

// Auth owns the session signer + nonce store and serves the auth routes.
type Auth struct {
	redis    *goredis.Client
	signer   *signer
	nonceTTL time.Duration
	domain   string
	logger   *slog.Logger
	clock    func() time.Time
}

// New constructs an Auth, applying defaults for optional dependencies.
func New(d Deps) *Auth {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	domain := d.AppDomain
	if domain == "" {
		domain = "kindlelaunch"
	}
	return &Auth{
		redis:    d.Redis,
		signer:   newSigner(d.JWTSecret, d.JWTTTL, clock),
		nonceTTL: d.NonceTTL,
		domain:   domain,
		logger:   d.Logger,
		clock:    clock,
	}
}

// RegisterRoutes mounts the auth endpoints onto r.
func (a *Auth) RegisterRoutes(r chi.Router) {
	r.Post("/auth/nonce", a.nonce)
	r.Post("/auth/login", a.login)
	r.With(a.RequireSession).Get("/auth/me", a.me)
}

type nonceRequest struct {
	Wallet string `json:"wallet"`
}

type nonceResponse struct {
	Nonce   string `json:"nonce"`
	Message string `json:"message"`
}

func (a *Auth) nonce(w http.ResponseWriter, r *http.Request) {
	var req nonceRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		sharedhttp.WriteRequestBodyError(w, http.StatusBadRequest, sharedhttp.RuleJSON, "invalid JSON body")
		return
	}
	wallet := common.NormalizeAddr(req.Wallet)
	if !common.IsAddr(wallet) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		a.fail(w, "rand nonce", err)
		return
	}
	nonce := hex.EncodeToString(raw[:])
	if err := a.redis.Set(r.Context(), noncePrefix+wallet+":"+nonce, "1", a.nonceTTL).Err(); err != nil {
		a.fail(w, "store nonce", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, nonceResponse{
		Nonce:   nonce,
		Message: a.buildMessage(wallet, nonce),
	})
}

// buildMessage renders the human-readable sign-in challenge embedding the nonce.
func (a *Auth) buildMessage(wallet, nonce string) string {
	return fmt.Sprintf(
		"%s wants you to sign in with your wallet:\n%s\n\n"+
			"Sign in to Sidiora. This request will not trigger a blockchain transaction or cost any gas.\n\n"+
			"Nonce: %s\nIssued At: %s",
		a.domain, wallet, nonce, a.clock().UTC().Format(time.RFC3339),
	)
}

type loginRequest struct {
	Wallet    string `json:"wallet"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

type loginResponse struct {
	Token     string `json:"token"`
	Wallet    string `json:"wallet"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		sharedhttp.WriteRequestBodyError(w, http.StatusBadRequest, sharedhttp.RuleJSON, "invalid JSON body")
		return
	}
	wallet := common.NormalizeAddr(req.Wallet)
	if !common.IsAddr(wallet) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	if req.Message == "" || req.Signature == "" {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "missing message or signature")
		return
	}
	m := nonceRe.FindStringSubmatch(req.Message)
	if m == nil {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "message missing nonce")
		return
	}
	nonce := strings.ToLower(m[1])

	// Consume the nonce atomically (single-use): GETDEL returns redis.Nil when
	// it was never issued, already used, or expired.
	if err := a.redis.GetDel(r.Context(), noncePrefix+wallet+":"+nonce).Err(); err != nil {
		if errors.Is(err, goredis.Nil) {
			sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "nonce expired or already used")
			return
		}
		a.fail(w, "consume nonce", err)
		return
	}
	if !sharedauth.VerifyWalletSignature(wallet, req.Message, req.Signature) {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "invalid signature")
		return
	}
	token, exp := a.signer.mint(wallet)
	sharedhttp.WriteJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		Wallet:    wallet,
		ExpiresAt: exp.Unix(),
	})
}

func (a *Auth) me(w http.ResponseWriter, r *http.Request) {
	sharedhttp.WriteJSON(w, http.StatusOK, map[string]string{"wallet": Actor(r.Context())})
}

// RequireSession is middleware that rejects requests without a valid session.
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wallet, ok := a.resolve(r)
		if !ok {
			sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid session")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), wallet)))
	})
}

// OptionalSession is middleware that attaches the actor when a valid session is
// present but never rejects (used on mixed public/authed proxy surfaces).
func (a *Auth) OptionalSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wallet, ok := a.resolve(r); ok {
			r = r.WithContext(WithActor(r.Context(), wallet))
		}
		next.ServeHTTP(w, r)
	})
}

// resolve extracts and validates the session token from the Authorization
// bearer header or, as a fallback for browser WebSocket clients (which cannot
// set headers), the ?token= query parameter.
func (a *Auth) resolve(r *http.Request) (string, bool) {
	token := bearer(r.Header.Get("Authorization"))
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		return "", false
	}
	wallet, err := a.signer.verify(token)
	if err != nil || !common.IsAddr(wallet) {
		return "", false
	}
	return wallet, true
}

func bearer(h string) string {
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

func (a *Auth) fail(w http.ResponseWriter, op string, err error) {
	if a.logger != nil {
		a.logger.Error("auth error", slog.String("op", op), slog.String("err", err.Error()))
	}
	sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "")
}

// WithActor stores an authenticated wallet in the context. It is the write
// counterpart of Actor, used by the session middleware (and by other gateway
// packages, e.g. tests exercising the proxy with a known actor).
func WithActor(ctx context.Context, wallet string) context.Context {
	return context.WithValue(ctx, actorKey, wallet)
}

// Actor returns the authenticated wallet from the context, or "" when absent.
func Actor(ctx context.Context) string {
	if v, ok := ctx.Value(actorKey).(string); ok {
		return v
	}
	return ""
}
