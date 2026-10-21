// Package ratelimit hardens the public ingress of the perps data services.
// Origin: Launch-x core/api/internal/ratelimit.
//
//   - ClientKey: the rate-limit key of a request, the client IP as resolved
//     through the trusted proxies of PERPS_TRUSTED_PROXY_CIDRS.
//   - UntrustedProxyHeaders: requests whose peer is outside that set yet carry
//     a forwarding header, so the real edge range is visible on first deploy.
//   - Limiter: a global in-flight concurrency cap that load-sheds with 503 over
//     capacity rather than accepting unbounded work. /health* always bypasses
//     the cap so probes stay green under load.
package ratelimit

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// EnvTrustedProxies lists, comma separated, the CIDRs of the proxies in front
// of the service (the edge) whose X-Real-IP and X-Forwarded-For are honoured. Unset
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

// untrustedWarnEvery bounds the untrusted-forwarding-header warning to one log
// line per interval.
const untrustedWarnEvery = time.Minute

// UntrustedProxyHeaders counts requests whose TCP peer is outside
// EnvTrustedProxies but which carry X-Real-IP or X-Forwarded-For: a non-zero
// rate on a service behind an edge means the configured range misses it.
var UntrustedProxyHeaders = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "perps_untrusted_proxy_headers_total",
	Help: "Requests from a peer outside PERPS_TRUSTED_PROXY_CIDRS carrying X-Real-IP or X-Forwarded-For.",
})

var (
	logger       atomic.Pointer[slog.Logger]
	lastWarnNano atomic.Int64
)

// Report wires the package into a service: it registers UntrustedProxyHeaders
// with register, sends the untrusted-header warnings to log and logs the
// trusted proxy set once.
func Report(log *slog.Logger, register func(prometheus.Collector) error) error {
	if err := register(UntrustedProxyHeaders); err != nil {
		return err
	}
	logger.Store(log)
	cidrs := make([]string, len(trustedProxies))
	for i, p := range trustedProxies {
		cidrs[i] = p.String()
	}
	log.Info("client ip resolution", slog.String("header", "X-Real-IP"), slog.Any("trusted_proxies", cidrs))
	return nil
}

// noteUntrusted counts a forwarding header from an untrusted peer and logs it
// at most once per untrustedWarnEvery.
func noteUntrusted(peer string) {
	UntrustedProxyHeaders.Inc()
	now := time.Now().UnixNano()
	last := lastWarnNano.Load()
	if now-last < int64(untrustedWarnEvery) || !lastWarnNano.CompareAndSwap(last, now) {
		return
	}
	log := logger.Load()
	if log == nil {
		log = slog.Default()
	}
	log.Warn("forwarding header from untrusted peer", slog.String("peer", peer), slog.String("env", EnvTrustedProxies))
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
// address the edge names in X-Real-IP. Without a valid X-Real-IP it is the
// right-most X-Forwarded-For hop that is not itself a trusted proxy: the
// address the outermost trusted proxy saw. Hops left of it are caller-written
// and never read. A malformed hop stops the walk at the last trusted address.
// An untrusted peer's forwarding headers are ignored and counted.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(peer.Unmap(), trusted) {
		if r.Header.Get("X-Real-IP") != "" || r.Header.Get("X-Forwarded-For") != "" {
			noteUntrusted(host)
		}
		return host
	}
	if real, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return real.Unmap().String()
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
