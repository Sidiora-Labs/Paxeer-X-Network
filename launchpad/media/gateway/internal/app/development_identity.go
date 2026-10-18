package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/devidentity"
)

// openDevelopmentIdentity builds the real v2 identity outside production when
// DATABASE_URL is configured. Without DATABASE_URL the gateway keeps its
// previous development behaviour (legacy /auth/* sessions only).
func openDevelopmentIdentity(ctx context.Context, cfg config.Config, observe func(string), logger *slog.Logger) (*devidentity.Runtime, error) {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		if logger != nil {
			logger.Warn("development identity disabled: DATABASE_URL unset; /v2/auth is not served")
		}
		return nil, nil
	}
	runtime, err := devidentity.Open(ctx, devidentity.Options{
		DatabaseURL:        cfg.DatabaseURL,
		RPCURL:             cfg.RPCURL,
		PublicURL:          cfg.PublicURL,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		SigningSecret:      cfg.IdentitySigningSecret,
		LimiterKey:         cfg.IdentityLimiterKey,
		ServiceToken:       cfg.IdentityServiceToken,
		Observe:            observe,
	})
	if err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Info("development identity enabled", slog.String("schema", devidentity.Schema), slog.String("origin", cfg.PublicURL))
	}
	return runtime, nil
}

// developmentTrustedProxies trusts every peer as a forwarding proxy outside
// production. The container is reachable only through Railway's edge, which
// sets X-Real-IP to the client address, or over the private network, whose
// callers set no forwarding headers. With every hop trusted, realIP skips the
// X-Forwarded-For chain and resolves the client from X-Real-IP, falling back to
// the TCP peer; rate limits and identity IP throttles then key on the browser
// instead of the shared edge address.
func developmentTrustedProxies() []netip.Prefix {
	return []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
}

// edgeHeaders mark a request that crossed Railway's public edge or an HTTP
// proxy (for example the web's same-origin rewrite).
var edgeHeaders = []string{"X-Railway-Edge", "X-Real-IP", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"}

// developmentInternalIdentity admits /internal/v2/identity/* on the main
// listener outside production, only for direct private-network callers (no
// edge or proxy headers). The routes themselves still require the
// IDENTITY_SERVICE_TOKEN bearer. Production never reaches this: its internal
// routes are served only on the mTLS listener.
func developmentInternalIdentity(cfg config.Config, r *http.Request) bool {
	if cfg.NodeEnv == "production" || !strings.HasPrefix(r.URL.Path, "/internal/v2/identity/") {
		return false
	}
	for _, name := range edgeHeaders {
		if len(r.Header.Values(name)) > 0 {
			return false
		}
	}
	return true
}
