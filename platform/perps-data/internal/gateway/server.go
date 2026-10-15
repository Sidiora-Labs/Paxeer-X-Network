// Package gateway serves the perps gateway process: the REST routes, the
// shared WebSocket, the spot user socket and GraphQL with its
// graphql-transport-ws subscriptions on one net/http ServeMux, behind the
// per-client request budget, with the readiness gate of
// contracts/data-services.md section 19.
package gateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	gqlgo "github.com/graph-gophers/graphql-go"
	gqlerrors "github.com/graph-gophers/graphql-go/errors"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/auth"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/feeds"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/graphql"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/ratelimit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/rest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/ws"
)

const (
	// ReadyLag is how far the newest applied stream message may trail the
	// stream head while the gateway is ready.
	ReadyLag = 10 * time.Second
	// readyInterval is how often the readiness of the socket gate is
	// evaluated.
	readyInterval = time.Second

	// clientRequests is the number of REST and GraphQL requests one client
	// key may make per clientWindow.
	clientRequests  = 600
	clientWindow    = time.Minute
	maxClientKeyLen = 512
	sweepInterval   = time.Minute
	// maxInFlight is the global in-flight cap of REST and GraphQL.
	maxInFlight = 10000

	maxGraphQLBody = 1 << 20
	// maxOperations bounds the operation label set of the GraphQL duration
	// histogram; later operation names count as otherOperation.
	maxOperations      = 256
	anonymousOperation = "anonymous"
	otherOperation     = "other"

	snapshotIDBytes = 8
	unmatchedRoute  = "unmatched"
	privateCache    = "no-store"
)

var operationName = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]{0,63}$`)

// dataSockets are the paths of the shared topic protocol: /ws and / as the
// data protocol names them (Hyperpax:data_api.md:27) and the socket paths the
// application opens (contracts/data-services.md section 8).
var dataSockets = []string{"GET /{$}", "GET /ws", "GET /ws/markets", "GET /ws/explorer", "GET /ws/{symbol}"}

// Options wires a Server.
type Options struct {
	DB         *sql.DB
	NATS       *nats.Conn
	Chain      uint64
	AuthSecret string
	// RPC reads the node for the spot holdings route.
	RPC     *source.RPC
	Metrics *telemetry.Metrics
	Logger  *slog.Logger
}

// Server is one gateway process.
type Server struct {
	db        *sql.DB
	nats      *nats.Conn
	providers *feeds.Providers
	hub       *ws.Hub
	feed      *graphql.Feed
	schema    *gqlgo.Schema
	transport *graphql.TransportWS
	spotUser  *ws.SpotUser
	// closer closes every socket it upgrades with 1013 and a retry-after
	// reason; it serves new sockets while the gateway is not ready.
	closer  *ws.Server
	metrics *telemetry.Metrics
	sockets prometheus.Gauge
	gqlTime *prometheus.HistogramVec
	limiter *windowLimiter
	api     http.Handler
	mux     *http.ServeMux
	ready   atomic.Bool
	// socketRoutes lists the patterns served as long-lived sockets: they bypass
	// the request budget, the in-flight cap and the request counter.
	socketRoutes map[string]bool

	opsMu sync.Mutex
	ops   map[string]bool
}

// NewServer builds the gateway over opts. It reads the market set of
// perps.markets, so the schema must be migrated and seeded.
func NewServer(ctx context.Context, opts Options) (*Server, error) {
	switch {
	case opts.DB == nil:
		return nil, errors.New("gateway: nil database")
	case opts.NATS == nil:
		return nil, errors.New("gateway: nil nats connection")
	case opts.Chain == 0:
		return nil, errors.New("gateway: zero chain id")
	case opts.RPC == nil:
		return nil, errors.New("gateway: nil rpc source")
	case opts.Metrics == nil:
		return nil, errors.New("gateway: nil metrics")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	snapshotID, err := newSnapshotID()
	if err != nil {
		return nil, err
	}

	sockets := prometheus.NewGauge(prometheus.GaugeOpts{Name: "perps_gateway_sockets", Help: "Open sockets."})
	subscriptions := prometheus.NewGauge(prometheus.GaugeOpts{Name: "perps_gateway_subscriptions", Help: "Topic subscriptions held by open sockets."})
	frames := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "perps_gateway_frames_total", Help: "Topic frames sent, by topic family."}, []string{"family"})
	resyncs := prometheus.NewCounter(prometheus.CounterOpts{Name: "perps_gateway_resyncs_total", Help: "Fresh snapshots sent after a rewind, a gap or a stale frame."})
	gqlTime := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "perps_gateway_graphql_duration_seconds", Help: "GraphQL request duration, by operation.", Buckets: prometheus.DefBuckets}, []string{"operation"})
	for _, c := range []prometheus.Collector{sockets, subscriptions, frames, resyncs, gqlTime} {
		if err := opts.Metrics.Register(c); err != nil {
			return nil, fmt.Errorf("gateway metrics: %w", err)
		}
	}

	providers, err := feeds.NewProviders(ctx, opts.DB, logger)
	if err != nil {
		return nil, err
	}
	limits := ws.DefaultLimits()
	hub, err := ws.NewHub(ws.HubConfig{
		NATS:          opts.NATS,
		Chain:         opts.Chain,
		SnapshotID:    snapshotID,
		Provider:      providers,
		Limits:        limits,
		Frames:        frames,
		Resyncs:       resyncs,
		Subscriptions: subscriptions,
		Logger:        logger,
	})
	if err != nil {
		return nil, err
	}
	closer, err := ws.NewServer(snapshotID, providers.Served, limits)
	if err != nil {
		return nil, err
	}
	closer.Shedding = func() bool { return true }

	signIn, err := auth.NewService(opts.DB, opts.AuthSecret, time.Now)
	if err != nil {
		return nil, err
	}
	symbol, err := rest.NewSymbol(opts.DB)
	if err != nil {
		return nil, err
	}
	markets, err := rest.NewMarkets(opts.DB, time.Now)
	if err != nil {
		return nil, err
	}
	venue, err := rest.NewVenue(opts.DB)
	if err != nil {
		return nil, err
	}
	spotUsers, err := rest.NewSpotUsers(opts.DB, opts.RPC)
	if err != nil {
		return nil, err
	}
	account, err := rest.NewAccountV2(opts.DB, signIn, time.Now)
	if err != nil {
		return nil, err
	}
	spotUser, err := ws.NewSpotUser(opts.DB, hub)
	if err != nil {
		return nil, err
	}
	schema, err := graphql.NewSchema(opts.DB)
	if err != nil {
		return nil, fmt.Errorf("graphql schema: %w", err)
	}
	feed, err := graphql.NewFeed(opts.Chain, logger)
	if err != nil {
		return nil, err
	}
	transport := &graphql.TransportWS{Schema: schema, Feed: feed}

	s := &Server{
		db: opts.DB, nats: opts.NATS, providers: providers, hub: hub, feed: feed,
		schema: schema, transport: transport, spotUser: spotUser, closer: closer,
		metrics: opts.Metrics, sockets: sockets, gqlTime: gqlTime,
		limiter:      &windowLimiter{max: clientRequests, window: clientWindow, entries: map[string]windowEntry{}},
		mux:          http.NewServeMux(),
		socketRoutes: map[string]bool{"GET /graphql": true, "GET /graphql-ws": true, "GET /v1/ws": true},
		ops:          map[string]bool{},
	}
	s.api = ratelimit.NewLimiter(maxInFlight).Middleware(http.HandlerFunc(s.limited))

	// The fixed routes are strictly more specific than /api/{symbol}/... and
	// /api/markets/{symbol}, so the ServeMux serves them first.
	symbol.Register(s.mux)
	markets.Register(s.mux)
	venue.Register(s.mux)
	spotUsers.Register(s.mux)
	account.Register(s.mux)
	s.mux.HandleFunc("POST /v2/auth/challenge", signIn.Challenge)
	s.mux.HandleFunc("POST /v2/auth/verify", signIn.Verify)
	s.mux.HandleFunc("POST /v2/auth/refresh", signIn.Refresh)

	s.mux.HandleFunc("POST /graphql", s.graphql)
	s.mux.HandleFunc("POST /api/subgraph", s.graphql)
	s.mux.HandleFunc("GET /graphql", s.graphqlUpgrade)
	s.mux.Handle("GET /graphql-ws", transport)

	for _, pattern := range dataSockets {
		s.mux.HandleFunc(pattern, s.dataSocket)
		s.socketRoutes[pattern] = true
	}
	s.mux.Handle("GET /v1/ws", s.gated(spotUser))

	httpx.RegisterHealth(s.mux, httpx.HealthDeps{Postgres: opts.DB.PingContext, Custom: map[string]httpx.Check{"consumer": s.consumerReady}})
	s.mux.Handle("GET /metrics", opts.Metrics)
	return s, nil
}

// Handler returns the handler of every gateway route.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

// Run consumes the stream into the topic hub and the GraphQL subscription
// feed and keeps the readiness of the socket gate current until ctx ends or
// the hub stops.
func (s *Server) Run(ctx context.Context) error {
	defer s.ready.Store(false)
	if err := s.feed.Start(ctx, s.nats); err != nil {
		return fmt.Errorf("graphql feed: %w", err)
	}
	hubErr := make(chan error, 1)
	go func() { hubErr <- s.hub.Run(ctx) }()
	tick := time.NewTicker(readyInterval)
	defer tick.Stop()
	for {
		s.ready.Store(s.evaluate(ctx))
		select {
		case err := <-hubErr:
			return err
		case <-tick.C:
		}
	}
}

// Ready reports the readiness the socket gate last evaluated.
func (s *Server) Ready() bool { return s.ready.Load() }

// evaluate is true when Postgres answers and the consumer is attached and at
// most ReadyLag behind the stream head.
func (s *Server) evaluate(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, httpx.ReadinessTimeout)
	defer cancel()
	return s.db.PingContext(ctx) == nil && s.consumerReady(ctx) == nil
}

func (s *Server) consumerReady(ctx context.Context) error {
	lag, err := s.hub.Lag(ctx)
	if err != nil {
		return err
	}
	if lag > ReadyLag {
		return fmt.Errorf("gateway: consumer %s behind the stream head", lag)
	}
	return nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	_, pattern := s.mux.Handler(r)
	if s.socketRoutes[pattern] {
		s.sockets.Inc()
		defer s.sockets.Dec()
		s.mux.ServeHTTP(w, r)
		return
	}
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	if strings.HasPrefix(pattern, "GET /healthz/") || pattern == "GET /metrics" {
		s.mux.ServeHTTP(sw, r)
	} else {
		s.api.ServeHTTP(sw, r)
	}
	route := pattern
	if route == "" {
		route = unmatchedRoute
	}
	_ = s.metrics.RecordRead(route, sw.status)
}

// limited spends one request of the client key's budget, marks the account
// routes uncacheable and serves the route.
func (s *Server) limited(w http.ResponseWriter, r *http.Request) {
	key := ratelimit.ClientKey(r)
	if len(key) > maxClientKeyLen {
		writeError(w, http.StatusBadRequest, "invalid request principal")
		return
	}
	if ok, retry := s.limiter.allow(key, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(retry.Seconds())), 10))
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	if _, pattern := s.mux.Handler(r); !publicSnapshot(pattern) {
		w.Header().Set("Cache-Control", privateCache)
	}
	s.mux.ServeHTTP(w, r)
}

// publicSnapshot reports the route cache class of RoutePolicy
// (Launch-x:core/api/internal/cache/policy.go:29): the market, venue and
// GraphQL reads are public snapshots whose handlers set their own freshness;
// the spot user, sign-in and account routes are private account reads,
// authorization decisions or account writes, which are never stored.
func publicSnapshot(pattern string) bool {
	_, path, _ := strings.Cut(pattern, " ")
	switch {
	case path == "/v2/leaderboard":
		return true
	case strings.HasPrefix(path, "/v1/users/"), strings.HasPrefix(path, "/v2/"):
		return false
	}
	return true
}

// dataSocket serves the shared topic protocol, subscribing the socket on open
// to the topics of its path; while the gateway is not ready it closes the new
// socket with 1013 and a retry-after reason.
func (s *Server) dataSocket(w http.ResponseWriter, r *http.Request) {
	topics, ok := s.providers.SocketTopics(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown symbol: "+strings.ToUpper(r.PathValue("symbol")))
		return
	}
	if !s.ready.Load() {
		s.closer.ServeHTTP(w, r)
		return
	}
	s.hub.ServeSocket(w, r, topics)
}

// gated closes new sockets of next with 1013 while the gateway is not ready.
func (s *Server) gated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			s.closer.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// graphqlUpgrade answers a graphql-transport-ws upgrade on /graphql, the URL
// the PerfDB client builds (Hyperpax:src/core/perfdb/client.ts:120).
func (s *Server) graphqlUpgrade(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.transport.ServeHTTP(w, r)
}

type graphqlRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

// graphql executes one {query, operationName, variables} request and answers
// {data} or {errors: [{message}]} with HTTP 200 (contracts/data-services.md
// section 13).
func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	var req graphqlRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGraphQLBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, &gqlgo.Response{Errors: []*gqlerrors.QueryError{{Message: "invalid request body"}}})
		return
	}
	start := time.Now()
	resp := s.schema.Exec(r.Context(), req.Query, req.OperationName, req.Variables)
	s.gqlTime.WithLabelValues(s.operation(req.OperationName)).Observe(time.Since(start).Seconds())
	writeJSON(w, http.StatusOK, resp)
}

// operation names the histogram label of an operation: its name when it is
// a GraphQL name among the first maxOperations seen, anonymous for an
// unnamed or malformed one and other beyond the bound.
func (s *Server) operation(name string) string {
	if !operationName.MatchString(name) {
		return anonymousOperation
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.ops[name] {
		if len(s.ops) >= maxOperations {
			return otherOperation
		}
		s.ops[name] = true
	}
	return name
}

func newSnapshotID() (string, error) {
	b := make([]byte, snapshotIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gateway: snapshot id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

// windowLimiter is a per-client-key fixed-window request budget
// (Launch-x:shared/http/ratelimit.go in-memory store), one per process.
type windowLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]windowEntry
	swept   time.Time
}

type windowEntry struct {
	count   int
	resetAt time.Time
}

// allow spends one request of key's budget and reports whether it was
// available and, when not, how long until the window resets.
func (l *windowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > sweepInterval {
		for k, e := range l.entries {
			if !e.resetAt.After(now) {
				delete(l.entries, k)
			}
		}
		l.swept = now
	}
	e, ok := l.entries[key]
	if !ok || !e.resetAt.After(now) {
		e = windowEntry{resetAt: now.Add(l.window)}
	}
	if e.count >= l.max {
		return false, e.resetAt.Sub(now)
	}
	e.count++
	l.entries[key] = e
	return true, 0
}
