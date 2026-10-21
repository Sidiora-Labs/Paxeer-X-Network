package fanout

import (
	"context"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/gorilla/websocket"
	"net/http"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
)

// Principal comes exclusively from the D11 verified gateway/ticket adapter.
// A wallet header alone is never an authenticated principal. Validate must
// recheck revocation, audience and session expiry on every private operation.
type Principal struct {
	Topics    []string
	Wallet    string
	SessionID string
	ExpiresAt time.Time
	Validate  func(context.Context) error
}
type AuthenticateUpgrade func(*http.Request) (Principal, error)

func (p Principal) valid(ctx context.Context, now time.Time) bool {
	return common.IsAddr(p.Wallet) && p.Wallet == common.NormalizeAddr(p.Wallet) && p.SessionID != "" && p.ExpiresAt.After(now) && p.Validate != nil && p.Validate(ctx) == nil
}

// VerifiedRequestAuthenticator uses the shared identity authority's current
// session and revocation checks; caller ingress must authenticate the gateway.
func VerifiedRequestAuthenticator(authority sharedauth.HTTPIdentityAuthority) AuthenticateUpgrade {
	return func(r *http.Request) (Principal, error) {
		value, err := authority.AuthenticateRequest(r.Context(), r)
		if err != nil {
			return Principal{}, err
		}
		return Principal{Wallet: common.NormalizeAddr(value.Wallet), SessionID: value.SessionID, ExpiresAt: value.AccessExpiresAt, Validate: func(ctx context.Context) error {
			return sharedauth.HTTPStreamAuthority(authority).CheckStreamSession(ctx, value.StreamPrincipal())
		}}, nil
	}
}

// TicketStreamLifetime bounds a stream admitted by its chat ticket alone: one
// identity access-token lifetime, after which the client opens a new stream
// with a fresh ticket.
const TicketStreamLifetime = 15 * time.Minute

func hasSessionCredential(r *http.Request) bool {
	if len(r.Header.Values("Authorization")) > 0 {
		return true
	}
	_, err := r.Cookie("__Host-kindle_access")
	return err == nil
}

type ChatTicketAuthority interface {
	ConsumeChatStreamTicket(context.Context, string, sharedauth.StreamTicketBinding) (sharedauth.StreamPrincipal, error)
	CheckStreamSession(context.Context, sharedauth.StreamPrincipal) error
}

func TicketAuthenticator(authority ChatTicketAuthority, request sharedauth.RequestIdentityAuthority) AuthenticateUpgrade {
	return func(r *http.Request) (Principal, error) {
		var ticket string
		for _, protocol := range websocket.Subprotocols(r) {
			if strings.HasPrefix(protocol, "ticket.") {
				if ticket != "" {
					return Principal{}, ErrInvalid
				}
				ticket = strings.TrimPrefix(protocol, "ticket.")
			}
		}
		topics := strings.Split(r.URL.Query().Get("topics"), ",")
		binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: r.Header.Get("Origin"), Endpoint: "/v2/stream"}
		principal, err := authority.ConsumeChatStreamTicket(r.Context(), ticket, binding)
		if err != nil {
			return Principal{}, err
		}
		if !hasSessionCredential(r) {
			// Ticket-only upgrade (a browser socket opened cross-site to the
			// gateway carries no Authorization header and no host-only
			// access cookie): the redeemed single-use ticket, bound to the
			// session, topics, filter digest, origin and endpoint, is the
			// credential. The stream lives at most one access-token lifetime
			// and every delivery and send re-checks the session.
			return Principal{Topics: append([]string(nil), topics...), Wallet: common.NormalizeAddr(principal.Wallet), SessionID: principal.SessionID, ExpiresAt: time.Now().Add(TicketStreamLifetime), Validate: func(ctx context.Context) error { return authority.CheckStreamSession(ctx, principal) }}, nil
		}
		session, err := request.AuthenticateRequest(r.Context(), r)
		if err != nil {
			return Principal{}, err
		}
		if session.SessionID != principal.SessionID || session.AccountID != principal.AccountID || session.Wallet != principal.Wallet || session.AuthEpoch != principal.AuthEpoch {
			return Principal{}, ErrNotFound
		}
		return Principal{Topics: append([]string(nil), topics...), Wallet: common.NormalizeAddr(principal.Wallet), SessionID: principal.SessionID, ExpiresAt: session.AccessExpiresAt, Validate: func(ctx context.Context) error { return authority.CheckStreamSession(ctx, principal) }}, nil
	}
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok || !principal.valid(ctx, time.Now()) {
		return Principal{}, false
	}
	return principal, true
}

func PrincipalValue(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
