// Package proxy fronts media/social: a REST reverse proxy and a WebSocket tunnel.
// Both establish the trusted actor identity for the sign-free social service by
// (1) ALWAYS stripping any client-supplied X-Actor-Wallet and X-API-Key headers
// (a client must never be able to forge identity or reach admin routes through
// the public edge) and (2) injecting X-Actor-Wallet from the gateway session
// when the caller is authenticated.
package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
)

// actorHeader is the trusted identity header media/social reads.
const actorHeader = "X-Actor-Wallet"

// adminHeader gates social's /admin routes; it must never cross the public edge.
const adminHeader = "X-API-Key"

// REST is a reverse proxy to media/social's HTTP surface.
type REST struct {
	rp      *httputil.ReverseProxy
	prefix  string
	timeout time.Duration
}

// RESTDeps configures NewREST.
type RESTDeps struct {
	// TargetBaseURL is the internal base URL of media/social (e.g.
	// http://social:3000).
	TargetBaseURL string
	// Prefix is the public mount path stripped before forwarding (e.g.
	// "/social").
	Prefix string
	// Timeout caps the upstream round-trip.
	Timeout time.Duration
	Logger  *slog.Logger
	// ForwardAuthorization keeps the caller's Authorization header for an
	// upstream that re-verifies the browser session itself against the
	// identity authority (media/user's development owner writes, which have
	// a public edge and so never trust X-Actor-Wallet). Cookies and every
	// other identity header are still stripped. Social upstreams leave it
	// false: they trust only the gateway-injected actor header.
	ForwardAuthorization bool
	// Upstream names the target in logs and errors (default "social").
	Upstream string
}

// NewREST builds a REST proxy. It returns an error if the target URL is invalid.
func NewREST(d RESTDeps) (*REST, error) {
	target, err := url.Parse(strings.TrimRight(d.TargetBaseURL, "/"))
	if err != nil {
		return nil, err
	}
	if target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, errors.New("invalid private upstream")
	}
	if d.Timeout <= 0 || d.Timeout > 3*time.Second {
		d.Timeout = 3 * time.Second
	}
	prefix := strings.TrimRight(d.Prefix, "/")
	upstream := d.Upstream
	if upstream == "" {
		upstream = "social"
	}

	rp := &httputil.ReverseProxy{
		Transport: &http.Transport{
			Proxy:                 nil,
			DisableCompression:    true,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   64,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: d.Timeout,
			ExpectContinueTimeout: time.Second,
		},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = singleJoin(target.Path, strings.TrimPrefix(pr.In.URL.Path, prefix))
			pr.Out.Host = target.Host
			// Identity is gateway-controlled: drop anything the client sent.
			pr.Out.Header.Del(actorHeader)
			pr.Out.Header.Del(adminHeader)
			for name := range pr.Out.Header {
				n := strings.ToLower(name)
				if strings.HasPrefix(n, "x-actor-") || strings.HasPrefix(n, "x-role") || strings.HasPrefix(n, "x-internal-") || (n == "authorization" && !d.ForwardAuthorization) || n == "cookie" {
					pr.Out.Header.Del(name)
				}
			}
			if wallet := auth.Actor(pr.In.Context()); wallet != "" {
				pr.Out.Header.Set(actorHeader, wallet)
			}
			pr.SetXForwarded()
		},
		// CORS is decided once, at the gateway edge. media/social runs the same
		// shared CORS middleware and answers with its own Access-Control-*
		// headers; copying them through would give the browser two
		// Access-Control-Allow-Origin values, which every browser rejects as a
		// network error. Drop the upstream's CORS decision before the gateway's
		// middleware adds its own.
		ModifyResponse: func(resp *http.Response) error {
			stripUpstreamCORS(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if d.Logger != nil {
				d.Logger.Error(upstream+" proxy upstream error", slog.String("reason", "upstream_unavailable"))
			}
			sharedhttp.WriteError(w, http.StatusBadGateway, "Bad Gateway", upstream+" upstream unavailable")
		},
	}
	return &REST{rp: rp, prefix: prefix, timeout: d.Timeout}, nil
}

// ServeHTTP proxies the request to media/social.
func (p *REST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, p.prefix+"/") || strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "%") {
		http.Error(w, "unknown route", 404)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), p.timeout)
	defer cancel()
	p.rp.ServeHTTP(w, r.WithContext(ctx))
}

// stripUpstreamCORS removes every Access-Control-* header an upstream set so
// the gateway's CORS middleware is the single origin-policy authority.
func stripUpstreamCORS(h http.Header) {
	for name := range h {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			h.Del(name)
		}
	}
}

// singleJoin joins two URL path segments with exactly one slash.
func singleJoin(a, b string) string {
	switch {
	case a == "":
		if b == "" {
			return "/"
		}
		return b
	case b == "":
		return a
	}
	as := strings.HasSuffix(a, "/")
	bs := strings.HasPrefix(b, "/")
	switch {
	case as && bs:
		return a + b[1:]
	case !as && !bs:
		return a + "/" + b
	default:
		return a + b
	}
}
