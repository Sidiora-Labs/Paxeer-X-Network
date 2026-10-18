package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/devsocial"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/social"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// platformValidationTTL bounds how long a successful session re-check
// (/internal/v2/identity/session:check) is reused for one session. The hub
// re-validates a connection's principal on every fan-out delivery, so without
// it every chat message would cost one identity round trip per recipient; a
// revoked session is refused within this window.
const platformValidationTTL = 5 * time.Second

// platformValidationEntries caps the success cache.
const platformValidationEntries = 100000

// platformIdentity is the production platform runtime's identity adapter: it
// verifies the forwarded browser credential (bearer or __Host-kindle_access
// cookie) at the gateway identity authority and redeems chat stream tickets
// there, over the gateway's private mTLS identity listener as issuer "social"
// (client certificate, pinned gateway server certificate, ed25519 request
// assertion).
type platformIdentity struct {
	authority sharedauth.MTLSIdentityAuthority
	ticket    fanout.AuthenticateUpgrade
	cache     *validationCache
}

// platformIdentityKey carries the verified identity principal of a request.
type platformIdentityKey struct{}

func verifiedIdentity(ctx context.Context) (sharedauth.IdentityPrincipal, bool) {
	p, ok := ctx.Value(platformIdentityKey{}).(sharedauth.IdentityPrincipal)
	return p, ok
}

func newPlatformIdentity(cfg config.Config) (*platformIdentity, error) {
	client, err := cfg.PlatformIdentityClient()
	if err != nil {
		return nil, err
	}
	authority, err := sharedauth.NewMTLSIdentityClient(client)
	if err != nil {
		return nil, err
	}
	p := &platformIdentity{authority: authority, cache: &validationCache{entries: map[string]time.Time{}}}
	p.ticket = p.cached(fanout.TicketAuthenticator(authority, authority))
	return p, nil
}

// authenticate verifies the request's browser credential at the identity
// authority and returns the identity principal and the fanout principal
// whose Validate re-checks the session (through the success cache).
func (p *platformIdentity) authenticate(r *http.Request) (sharedauth.IdentityPrincipal, fanout.Principal, error) {
	identity, err := p.authority.AuthenticateRequest(r.Context(), r)
	if err != nil {
		return sharedauth.IdentityPrincipal{}, fanout.Principal{}, err
	}
	authority := p.authority
	stream := identity.StreamPrincipal()
	principal, err := p.cached(func(*http.Request) (fanout.Principal, error) {
		return fanout.Principal{Wallet: common.NormalizeAddr(identity.Wallet), SessionID: identity.SessionID, ExpiresAt: identity.AccessExpiresAt, Validate: func(ctx context.Context) error {
			return authority.CheckStreamSession(ctx, stream)
		}}, nil
	})(r)
	return identity, principal, err
}

// cached wraps an authenticator so the principal's session re-check reuses a
// recent success for platformValidationTTL.
func (p *platformIdentity) cached(next fanout.AuthenticateUpgrade) fanout.AuthenticateUpgrade {
	return func(r *http.Request) (fanout.Principal, error) {
		principal, err := next(r)
		if err != nil {
			return fanout.Principal{}, err
		}
		validate, session := principal.Validate, principal.SessionID+"|"+principal.Wallet
		principal.Validate = func(ctx context.Context) error {
			if p.cache.fresh(session) {
				return nil
			}
			if err := validate(ctx); err != nil {
				p.cache.forget(session)
				return err
			}
			p.cache.store(session)
			return nil
		}
		return principal, nil
	}
}

// contextPrincipal is the devsocial authenticator of the platform runtime: the
// principal the platform ingress verified for this request, re-validated.
func contextPrincipal(r *http.Request) (fanout.Principal, error) {
	principal, ok := fanout.PrincipalFromContext(r.Context())
	if !ok {
		return fanout.Principal{}, fanout.ErrNotFound
	}
	return principal, nil
}

// platformIdentityHeaders are the identity headers only the platform ingress
// may set; any client-supplied value is dropped.
var platformIdentityHeaders = []string{"X-Actor-Wallet", "X-Actor-Role", "X-Roles", "X-Admin", "X-Admin-Key", "X-Admin-Role"}

func isStreamPath(path string) bool { return path == "/ws" || path == "/v2/stream" }

// ingress strips client identity headers, verifies a presented credential at
// the identity authority and attaches the verified principal (context, and
// X-Actor-Wallet for the legacy handlers). Requests without a credential are
// anonymous; every write path refuses them. WebSocket upgrades are
// authenticated by the hub from their single-use chat ticket.
func (p *platformIdentity) ingress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range platformIdentityHeaders {
			r.Header.Del(name)
		}
		for name := range r.Header {
			if lower := strings.ToLower(name); strings.HasPrefix(lower, "x-actor-") || strings.HasPrefix(lower, "x-role") {
				r.Header.Del(name)
			}
		}
		path := r.URL.Path
		if path == "/health" || strings.HasPrefix(path, "/health/") || path == "/metrics" || isStreamPath(path) || !hasCredential(r) {
			next.ServeHTTP(w, r)
			return
		}
		identity, principal, err := p.authenticate(r)
		if err != nil {
			writePlatformIdentityError(w, r, err)
			return
		}
		r.Header.Set("X-Actor-Wallet", principal.Wallet)
		ctx := context.WithValue(r.Context(), platformIdentityKey{}, identity)
		ctx = fanout.WithPrincipal(ctx, principal)
		ctx = interactions.WithIdentity(ctx, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func hasCredential(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	_, err := r.Cookie("__Host-kindle_access")
	return err == nil
}

// ready reports whether the identity authority accepts this service on its
// private mTLS listener (pinned server, client certificate and request
// assertion all verified).
func (p *platformIdentity) ready(ctx context.Context) error {
	if err := p.authority.Probe(ctx); err != nil {
		var authorityErr *sharedauth.IdentityAuthorityError
		if errors.As(err, &authorityErr) && authorityErr.Status != http.StatusServiceUnavailable {
			return errors.New("identity_authority_rejected_service")
		}
		return errors.New("identity_authority_unreachable")
	}
	return nil
}

// writePlatformIdentityError answers a rejected credential: the /v2/social
// interaction envelope there, the v2 Error envelope everywhere else.
func writePlatformIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusUnauthorized, "AUTH_EXPIRED"
	var authorityErr *sharedauth.IdentityAuthorityError
	if errors.As(err, &authorityErr) {
		switch authorityErr.Status {
		case http.StatusForbidden:
			status, code = http.StatusForbidden, "SCOPE_DENIED"
		case http.StatusTooManyRequests:
			status, code = http.StatusTooManyRequests, "RATE_LIMITED"
		case http.StatusServiceUnavailable:
			status, code = http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"
		}
	} else if !errors.Is(err, sharedauth.ErrScopeDenied) {
		// Transport failure reaching the authority.
		status, code = http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"
	}
	retryable := status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	if strings.HasPrefix(r.URL.Path, "/v2/social/") {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "retryable": retryable})
		return
	}
	requestID := uuid.NewString()
	if id, e := uuid.Parse(r.Header.Get("X-Request-ID")); e == nil && id != uuid.Nil {
		requestID = id.String()
	}
	_ = json.NewEncoder(w).Encode(v2.Error{Code: code, Message: "Session verification failed", RequestId: requestID, Retryable: retryable, FieldErrors: []v2.FieldError{}})
}

// validationCache remembers recent successful session re-checks.
type validationCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func (c *validationCache) fresh(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.entries[key]
	if ok && time.Now().Before(until) {
		return true
	}
	if ok {
		delete(c.entries, key)
	}
	return false
}

func (c *validationCache) store(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) >= platformValidationEntries {
		for k, until := range c.entries {
			if !now.Before(until) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= platformValidationEntries {
			c.entries = map[string]time.Time{}
		}
	}
	c.entries[key] = now.Add(platformValidationTTL)
}

func (c *validationCache) forget(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// moderation attaches the safety store (and, for a verified request, the
// moderation principal resolved from the session's moderator grant) the way
// the bound runtime's safety middleware does. With require set, a request
// without a verified identity is refused.
func (p *platformIdentity) moderation(store *safety.Store, require bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := social.WithModeration(r.Context(), store)
			identity, ok := verifiedIdentity(ctx)
			if !ok {
				if require {
					sharedhttp.WriteError(w, safety.HTTPStatus(safety.ErrForbidden), "Moderation rejected", safety.ErrForbidden.Error())
					return
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			principal, err := store.ResolvePrincipal(ctx, identity.Wallet, identity.AccountID, identity.SessionID, identity.AccessExpiresAt)
			if err != nil {
				sharedhttp.WriteError(w, safety.HTTPStatus(err), "Moderation rejected", err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(safety.WithPrincipal(ctx, principal)))
		})
	}
}

// platformPools answers voice.Pools from the production indexer's canonical
// creations (indexer.indexer_canonical_creations_v1).
type platformPools struct{ db *pgxpool.Pool }

func (p platformPools) Exists(ctx context.Context, pool string) (bool, error) {
	_, _, found, err := devsocial.LookupPool(ctx, p.db, devsocial.CanonicalPools, pool)
	return found, err
}

// platformMessageProjection hides chat messages a moderator removed
// (safety_visibility) and messages of erased accounts, as the bound runtime's
// projection does.
func platformMessageProjection(db *pgxpool.Pool) func(context.Context, fanout.DurableMessage) (fanout.DurableMessage, error) {
	return func(ctx context.Context, m fanout.DurableMessage) (fanout.DurableMessage, error) {
		var denied bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat.interaction_erased_wallets WHERE wallet=$1) OR EXISTS(SELECT 1 FROM chat.safety_visibility WHERE target_type='message' AND target_id=$2 AND status='removed')`, m.Sender, m.ID).Scan(&denied); err != nil {
			return m, err
		}
		if denied && !m.Deleted {
			m.Deleted = true
			m.Content = ""
			m.ReplyTo = ""
			m.Revision++
		}
		return m, nil
	}
}
