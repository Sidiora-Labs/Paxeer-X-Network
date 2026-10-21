// Package ratelimit hardens the core/api public ingress (invariant i12). It
// complements the shared per-key token bucket (shared/http.RateLimit) with:
//
//   - ClientKey: a rate-limit key that prefers an API key over the client IP,
//     so authenticated callers get their own quota instead of sharing an IP
//     bucket behind a NAT/CDN.
//   - Limiter: a global in-flight concurrency cap that LOAD-SHEDS with 503 over
//     capacity rather than accepting unbounded work and risking OOM (no-OOM
//     path, SECTION 17 backpressure). /health* always bypasses the cap so probes
//     stay green under load.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// ClientKey returns the rate-limit identity for a request: the API key when
// present (X-API-Key), otherwise the client IP. The prefix keeps the two key
// spaces disjoint so an IP and an API key can never collide.
func ClientKey(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return "key:" + k
	}
	return "ip:" + clientIP(r)
}

// Limiter bounds the number of concurrently in-flight requests.
type Limiter struct {
	sem      chan struct{}
	inflight atomic.Int64
	max      int
}

// NewLimiter builds a Limiter allowing at most max concurrent requests.
func NewLimiter(maxConcurrent int) *Limiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 10000
	}
	return &Limiter{sem: make(chan struct{}, maxConcurrent), max: maxConcurrent}
}

// Middleware enforces the in-flight cap, load-shedding with 503 + Retry-After
// when full. Health endpoints bypass the cap.
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
			sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable",
				"server at capacity, retry shortly")
		}
	})
}

// InFlight returns the current number of in-flight requests (for metrics).
func (l *Limiter) InFlight() int64 { return l.inflight.Load() }

// Max returns the configured concurrency ceiling.
func (l *Limiter) Max() int { return l.max }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
