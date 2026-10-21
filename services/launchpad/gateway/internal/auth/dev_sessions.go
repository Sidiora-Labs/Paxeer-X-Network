package auth

import (
	"net/http"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/common"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// DevSessions resolves the actor for the development-mode proxies. It accepts
// either credential the browser may hold outside production:
//   - the legacy HS256 session from /auth/login (GATEWAY_JWT_SECRET), or
//   - a v2 identity session from /v2/auth/sessions, as a Bearer access token
//     or the __Host-kindle_access cookie (cookie mutations still require the
//     allowed Origin and X-CSRF-Token, enforced by Identity.AuthenticateRequest).
//
// Identity is nil when development identity is disabled (no DATABASE_URL); the
// legacy session then remains the only credential, exactly as before.
type DevSessions struct {
	Legacy   *Auth
	Identity *Identity
}

// Optional attaches the actor when a valid session is present and never rejects.
func (d DevSessions) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wallet, ok := d.resolve(r); ok {
			r = r.WithContext(WithActor(r.Context(), wallet))
		}
		next.ServeHTTP(w, r)
	})
}

// Require rejects requests without a valid legacy or v2 session.
func (d DevSessions) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wallet, ok := d.resolve(r)
		if !ok {
			sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid session")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), wallet)))
	})
}

func (d DevSessions) resolve(r *http.Request) (string, bool) {
	if d.Legacy != nil {
		if wallet, ok := d.Legacy.resolve(r); ok {
			return wallet, true
		}
	}
	if d.Identity == nil {
		return "", false
	}
	probe := r
	if token := r.URL.Query().Get("token"); token != "" && r.Header.Get("Authorization") == "" {
		// Browser WebSockets cannot set headers, so the legacy tunnel carries
		// the token in ?token=. Identity.AuthenticateRequest refuses token
		// query strings by design; present it as the bearer it is instead.
		probe = r.Clone(r.Context())
		query := probe.URL.Query()
		query.Del("token")
		probe.URL.RawQuery = query.Encode()
		probe.Header.Set("Authorization", "Bearer "+token)
	}
	if probe.Header.Get("Authorization") == "" {
		if _, err := probe.Cookie("__Host-kindle_access"); err != nil {
			return "", false
		}
	}
	principal, err := d.Identity.AuthenticateRequest(r.Context(), probe)
	if err != nil {
		return "", false
	}
	wallet := strings.ToLower(principal.Wallet())
	if !common.IsAddr(wallet) {
		return "", false
	}
	return wallet, true
}
