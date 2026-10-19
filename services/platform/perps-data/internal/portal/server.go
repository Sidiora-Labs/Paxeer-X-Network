package portal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

// Bounds of the page cache and the gateway readiness probe.
const (
	CacheTTL           = 5 * time.Second
	maxCacheEntries    = 10000
	maxProbeBody       = 64 << 10
	readinessPath      = "/healthz/ready"
	unmatchedRoute     = "unmatched"
	htmlContentType    = "text/html; charset=utf-8"
	unknownSymbol      = "Unknown symbol"
	malformedAddress   = "Malformed address"
	unknownAccount     = "Unknown account"
	pageNotFound       = "Page not found"
	gatewayUnavailable = "gateway unavailable"
)

// ContentSecurityPolicy is set on every response: the portal renders on the
// server, so no script source other than self is allowed.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Page labels of perps_portal_render_seconds.
const (
	pageMarkets     = "markets"
	pageMarket      = "market"
	pageAccount     = "account"
	pageLeaderboard = "leaderboard"
	pageStats       = "stats"
	pageUnknown     = "not_found"
)

var (
	symbolPattern  = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

type cached struct {
	status  int
	body    []byte
	expires time.Time
}

// Server serves the five portal pages, the health probes and the metrics of
// the perps-portal process on one ServeMux.
type Server struct {
	pages   *Pages
	ready   string
	client  *http.Client
	metrics *telemetry.Metrics
	render  *prometheus.HistogramVec
	mux     *http.ServeMux

	mu    sync.Mutex
	cache map[string]cached
}

// NewServer mounts the pages rendered by pages, /healthz/live, /healthz/ready
// (ready while the gateway at gatewayURL answers 200 on its /healthz/ready)
// and /metrics, and registers perps_portal_render_seconds on metrics.
func NewServer(pages *Pages, gatewayURL string, metrics *telemetry.Metrics, client *http.Client) (*Server, error) {
	if pages == nil {
		return nil, errors.New("portal_pages_required")
	}
	if metrics == nil {
		return nil, errors.New("portal_metrics_required")
	}
	if client == nil {
		return nil, errors.New("portal_http_client_required")
	}
	base, err := url.Parse(gatewayURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("portal_gateway_url_needs_http_or_https")
	}
	render := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "perps_portal_render_seconds",
		Help:    "Portal page render time, by page.",
		Buckets: prometheus.DefBuckets,
	}, []string{"page"})
	if err := metrics.Register(render); err != nil {
		return nil, fmt.Errorf("register render histogram: %w", err)
	}
	s := &Server{
		pages:   pages,
		ready:   strings.TrimRight(base.String(), "/") + readinessPath,
		client:  client,
		metrics: metrics,
		render:  render,
		mux:     http.NewServeMux(),
		cache:   map[string]cached{},
	}
	httpx.RegisterHealth(s.mux, httpx.HealthDeps{Custom: map[string]httpx.Check{"gateway": s.gatewayReady}})
	s.mux.Handle("GET /metrics", metrics)
	s.mux.HandleFunc("GET /{$}", s.page(pageMarkets, func(ctx context.Context, w io.Writer, _ *http.Request) error {
		return s.pages.Markets(ctx, w)
	}))
	s.mux.HandleFunc("GET /markets/{symbol}", s.page(pageMarket, func(ctx context.Context, w io.Writer, r *http.Request) error {
		symbol := r.PathValue("symbol")
		if !symbolPattern.MatchString(symbol) {
			return notFound(unknownSymbol)
		}
		return s.pages.Market(ctx, w, symbol)
	}))
	s.mux.HandleFunc("GET /accounts/{address}", s.page(pageAccount, func(ctx context.Context, w io.Writer, r *http.Request) error {
		address := r.PathValue("address")
		if !addressPattern.MatchString(address) {
			return notFound(malformedAddress)
		}
		return s.pages.Account(ctx, w, strings.ToLower(address))
	}))
	s.mux.HandleFunc("GET /leaderboard", s.page(pageLeaderboard, func(ctx context.Context, w io.Writer, _ *http.Request) error {
		return s.pages.Leaderboard(ctx, w)
	}))
	s.mux.HandleFunc("GET /stats", s.page(pageStats, func(ctx context.Context, w io.Writer, _ *http.Request) error {
		return s.pages.Stats(ctx, w)
	}))
	s.mux.HandleFunc("GET /", s.page(pageUnknown, func(context.Context, io.Writer, *http.Request) error {
		return notFound(pageNotFound)
	}))
	return s, nil
}

// ServeHTTP sets the Content-Security-Policy on every response, serves the
// request and counts it under its route pattern and status.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rec, r)
	route := r.Pattern
	if route == "" {
		route = unmatchedRoute
	}
	_ = s.metrics.RecordRead(route, rec.status)
}

// unknownKey is a page key absent from the gateway or refused before the
// gateway is asked; it renders as 404 inside the layout.
type unknownKey struct{ message string }

func (e unknownKey) Error() string { return e.message }

func notFound(message string) error { return unknownKey{message: message} }

type renderFunc func(ctx context.Context, w io.Writer, r *http.Request) error

// page serves one page from the cache, or renders it, caching the 200 and 404
// answers for CacheTTL under the path and query. A gateway failure answers 502
// and is not cached.
func (s *Server) page(label string, render renderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()
		if entry, ok := s.lookup(key); ok {
			writePage(w, entry.status, entry.body)
			return
		}
		var body bytes.Buffer
		started := time.Now()
		status := http.StatusOK
		err := render(r.Context(), &body, r)
		var unknown unknownKey
		switch {
		case errors.As(err, &unknown):
			body.Reset()
			status, err = http.StatusNotFound, s.pages.NotFound(&body, unknown.message)
		case errors.Is(err, ErrNotFound):
			body.Reset()
			status, err = http.StatusNotFound, s.pages.NotFound(&body, notFoundMessage(label))
		}
		s.render.WithLabelValues(label).Observe(time.Since(started).Seconds())
		if err != nil {
			http.Error(w, gatewayUnavailable, http.StatusBadGateway)
			return
		}
		s.store(key, status, body.Bytes())
		writePage(w, status, body.Bytes())
	}
}

// notFoundMessage names the missing key of a page the gateway answered 404 for.
func notFoundMessage(label string) string {
	switch label {
	case pageMarket:
		return unknownSymbol
	case pageAccount:
		return unknownAccount
	default:
		return pageNotFound
	}
}

func (s *Server) lookup(key string) (cached, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return cached{}, false
	}
	if !time.Now().Before(entry.expires) {
		delete(s.cache, key)
		return cached{}, false
	}
	return entry, true
}

// store keeps one rendered page. When the cache is full the expired entries
// are dropped first; if it is still full the page is served uncached.
func (s *Server) store(key string, status int, body []byte) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= maxCacheEntries {
		for k, entry := range s.cache {
			if !now.Before(entry.expires) {
				delete(s.cache, k)
			}
		}
		if len(s.cache) >= maxCacheEntries {
			return
		}
	}
	s.cache[key] = cached{status: status, body: bytes.Clone(body), expires: now.Add(CacheTTL)}
}

func writePage(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", htmlContentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// gatewayReady is nil while the gateway answers 200 on /healthz/ready.
func (s *Server) gatewayReady(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.ready, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBody))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway readiness answered %d", resp.StatusCode)
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}
