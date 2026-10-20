// Package ratelimit hardens the public ingress of the perps data services.
// Origin: Launch-x core/api/internal/ratelimit.
//
//   - ClientKey: the rate-limit key of a request, the client IP as resolved
//     through the trusted proxies of PERPS_TRUSTED_PROXY_CIDRS.
//   - Limiter: a global in-flight concurrency cap that load-sheds with 503 over
//     capacity rather than accepting unbounded work. /health* always bypasses
//     the cap so probes stay green under load.
package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
)

// EnvTrustedProxies lists, comma separated, the CIDRs of the proxies in front
// of the service (the edge) whose X-Forwarded-For hops are honoured. Unset
// trusts no proxy, so every request is keyed by its TCP peer.
const EnvTrustedProxies = "PERPS_TRUSTED_PROXY_CIDRS"

// maxTrustedProxies bounds EnvTrustedProxies.
const maxTrustedProxies = 32

// trustedProxies is EnvTrustedProxies, parsed once at start; a malformed value
// stops the process rather than trusting a proxy set nobody configured.
var trustedProxies = mustTrustedProxies(os.Getenv(EnvTrustedProxies))

func mustTrustedProxies(raw string) []netip.Prefix {
	out, err := ParseTrustedProxies(raw)
	if err != nil {
		panic(err)
	}
	return out
}

// ParseTrustedProxies parses a comma-separated CIDR list; an empty list trusts
// no proxy.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("ratelimit: %s entry %q is not a CIDR", EnvTrustedProxies, part)
		}
		out = append(out, prefix.Masked())
	}
	if len(out) > maxTrustedProxies {
		return nil, fmt.Errorf("ratelimit: %s allows at most %d entries", EnvTrustedProxies, maxTrustedProxies)
	}
	return out, nil
}

// capacityBody is the masked 5xx JSON error envelope of the origin.
const capacityBody = `{"statusCode":503,"error":"Internal Server Error","message":"An unexpected error occurred"}` + "\n"

// ClientKey returns the rate-limit identity for a request: its client IP. An
// X-API-Key header is not an identity: the services issue no API keys, so any
// value is caller-chosen and keying on it would hand every random key a fresh
// budget and a fresh bucket.
func ClientKey(r *http.Request) string {
	return "ip:" + clientIP(r, trustedProxies)
}

// Limiter bounds the number of concurrently in-flight requests.
type Limiter struct {
	sem      chan struct{}
	inflight atomic.Int64
	max      int
}

// NewLimiter builds a Limiter allowing at most maxConcurrent concurrent
// requests; a non-positive value selects 10000.
func NewLimiter(maxConcurrent int) *Limiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 10000
	}
	return &Limiter{sem: make(chan struct{}, maxConcurrent), max: maxConcurrent}
}

// Middleware enforces the in-flight cap, load-shedding with 503 and
// Retry-After when full. Health endpoints bypass the cap.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/health") {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case l.sem <- struct{}{}:
			l.inflight.Add(1)
			defer func() {
				l.inflight.Add(-1)
				<-l.sem
			}()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(capacityBody))
		}
	})
}

// InFlight returns the current number of in-flight requests.
func (l *Limiter) InFlight() int64 { return l.inflight.Load() }

// Max returns the configured concurrency ceiling.
func (l *Limiter) Max() int { return l.max }

// clientIP is the TCP peer of r, or, when the peer is a trusted proxy, the
// right-most X-Forwarded-For hop that is not itself a trusted proxy: the
// address the outermost trusted proxy saw. Hops left of it are caller-written
// and never read. A malformed hop stops the walk at the last trusted address.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(peer.Unmap(), trusted) {
		return host
	}
	client := peer.Unmap()
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = hop.Unmap()
		if !isTrusted(client, trusted) {
			break
		}
	}
	return client.String()
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
