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
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/auth"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/feeds"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/graphql"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ratelimit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/rest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ws"
)

const (
	// ReadyLag is how far the newest applied stream message may trail the
	// stream head while the gateway is ready.
	ReadyLag = 10 * time.Second
	// spotQueue bounds the stream messages waiting for the spot user socket.
	spotQueue = 1024

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
	// Node reads the node for the spot holdings and positions routes.
	Node    rest.SpotNode
	Metrics *telemetry.Metrics
	Logger  *slog.Logger
}

// Server is one gateway process.
type Server struct {
	db        *sql.DB
	nats      *nats.Conn
	chain     uint64
	js        jetstream.JetStream
	providers *feeds.Providers
	hub       *ws.Hub
	feed      *graphql.Feed
	schema    *gqlgo.Schema
	transport *graphql.TransportWS
	spotUser  *ws.SpotUser
	spotUsers *rest.SpotUsers
	logger    *slog.Logger
	// data serves the shared topic protocol through the hub.
	data *ws.Server
	// closer closes every socket it upgrades with 1013 and a retry-after
	// reason; it serves new sockets while the gateway is not ready.
	closer        *ws.Server
	metrics       *telemetry.Metrics
	sockets       prometheus.Gauge
	subscriptions prometheus.Gauge
	gqlTime       *prometheus.HistogramVec
	limiter       *windowLimiter
	api           http.Handler
	mux           *http.ServeMux
	ready         atomic.Bool
	// running is the time, in Unix nanoseconds, Run attached the hub, zero
	// while the hub is not running; applied is the time the hub last applied
	// a stream message. Together they bound the consumer lag.
	running atomic.Int64
	applied atomic.Int64
	// socketRoutes lists the patterns served as long-lived sockets: they bypass
	// the request budget, the in-flight cap and the request counter.
	socketRoutes map[string]bool

	opsMu sync.Mutex
	ops   map[string]bool

	// held is the topic count of every open data socket, the source of the
	// subscriptions gauge.
	heldMu sync.Mutex
	held   map[*ws.Socket]int
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
	case opts.Node == nil:
		return nil, errors.New("gateway: nil node")
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
	gqlTime := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "perps_gateway_graphql_duration_seconds", Help: "GraphQL request duration, by operation.", Buckets: prometheus.DefBuckets}, []string{"operation"})
	frames := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "perps_gateway_frames_total", Help: "Data frames built for subscribed topics, by topic family."}, []string{"family"})
	resyncs := prometheus.NewCounter(prometheus.CounterOpts{Name: "perps_gateway_resyncs_total", Help: "Fresh snapshots of subscribed topics outside a subscribe."})
	for _, c := range []prometheus.Collector{sockets, subscriptions, gqlTime, frames, resyncs} {
		if err := opts.Metrics.Register(c); err != nil {
			return nil, fmt.Errorf("gateway metrics: %w", err)
		}
	}

	providers, err := feeds.NewProviders(ctx, opts.DB, logger)
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(opts.NATS)
	if err != nil {
		return nil, fmt.Errorf("gateway: jetstream: %w", err)
	}
	provider := &hubProvider{Providers: providers, frames: frames, resyncs: resyncs, watched: map[string]int{}, expected: map[string]int{}}
	hub, err := ws.NewHub(provider, opts.Chain, snapshotID, logger)
	if err != nil {
		return nil, err
	}
	limits := ws.DefaultLimits()
	data, err := ws.NewServer(snapshotID, providers.Served, limits)
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
	spotUsers, err := rest.NewSpotUsers(opts.DB, opts.Node)
	if err != nil {
		return nil, err
	}
	account, err := rest.NewAccountV2(opts.DB, signIn, time.Now)
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
		db: opts.DB, nats: opts.NATS, chain: opts.Chain, js: js, providers: providers, hub: hub, feed: feed,
		schema: schema, transport: transport, spotUser: &ws.SpotUser{}, spotUsers: spotUsers, logger: logger, data: data, closer: closer,
		metrics: opts.Metrics, sockets: sockets, subscriptions: subscriptions, gqlTime: gqlTime,
		limiter:      &windowLimiter{max: clientRequests, window: clientWindow, entries: map[string]windowEntry{}},
		mux:          http.NewServeMux(),
		socketRoutes: map[string]bool{"GET /graphql": true, "GET /graphql-ws": true, "GET /v1/ws": true},
		ops:          map[string]bool{},
		held:         map[*ws.Socket]int{},
	}
	// A socket path subscribes to its path topics on open; dataSocket has
	// already refused the paths SocketTopics does not serve.
	data.OnOpen = func(r *http.Request) []string {
		topics, _ := providers.SocketTopics(r.URL.Path)
		return topics
	}
	data.OnSubscribe = func(sock *ws.Socket, f ws.ClientFrame) {
		provider.expect(f.Topic)
		hub.Subscribe(sock, f)
		s.hold(sock, sock.Subscriptions())
	}
	data.OnUnsubscribe = func(sock *ws.Socket, topic string) {
		hub.Unsubscribe(sock, topic)
		s.hold(sock, sock.Subscriptions())
	}
	data.OnClose = func(sock *ws.Socket) {
		hub.Leave(sock)
		s.hold(sock, 0)
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
	s.mux.Handle("GET /v1/ws", s.gated(s.spotUser))

	httpx.RegisterHealth(s.mux, httpx.HealthDeps{Postgres: opts.DB.PingContext, Custom: map[string]httpx.Check{"consumer": s.consumerReady}})
	s.mux.Handle("GET /metrics", opts.Metrics)
	return s, nil
}

// Handler returns the handler of every gateway route.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

// Run consumes the stream through the topic hub, which also feeds the
// GraphQL subscriptions, and keeps the readiness of the socket gate current
// until ctx ends or the hub stops.
func (s *Server) Run(ctx context.Context) error {
	defer s.ready.Store(false)
	stopFeed := s.hub.Listen(s.feed.Deliver)
	defer stopFeed()
	stopApplied := s.hub.Listen(func(string, delivery.Envelope) { s.applied.Store(time.Now().UnixNano()) })
	defer stopApplied()
	defer s.feedSpotUser(ctx)()
	s.running.Store(time.Now().UnixNano())
	defer s.running.Store(0)
	hubErr := make(chan error, 1)
	go func() { hubErr <- s.hub.Run(ctx, s.nats) }()
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

// feedSpotUser feeds the spot user socket from the spot and user subjects the
// hub applies until ctx ends; stop detaches it from the hub. The listener
// runs under the hub lock, so it only queues the message, and a worker
// translates it into the REST items (rest.SpotUsers.StreamItems).
// ponytail: a full queue drops the message with an error log; the socket
// client then misses that update until its next REST read.
func (s *Server) feedSpotUser(ctx context.Context) (stop func()) {
	type message struct {
		subject string
		env     delivery.Envelope
	}
	ctx, cancel := context.WithCancel(ctx)
	queue := make(chan message, spotQueue)
	prefix := "perps." + strconv.FormatUint(s.chain, 10) + "."
	stopListen := s.hub.Listen(func(subject string, env delivery.Envelope) {
		if !stream.Matches(prefix+"spot.>", subject) && !stream.Matches(prefix+"user.>", subject) {
			return
		}
		select {
		case queue <- message{subject, env}:
		default:
			s.logger.Error("spot user socket queue full", slog.String("subject", subject), slog.String("event", env.EventID))
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-queue:
				items, err := s.spotUsers.StreamItems(ctx, m.env)
				if err != nil {
					if ctx.Err() == nil {
						s.logger.Error("spot user socket item", slog.String("subject", m.subject), slog.Any("err", err))
					}
					continue
				}
				for _, item := range items {
					if err := s.spotUser.Deliver(item.Stream, item.Address, item.Data); err != nil {
						s.logger.Error("spot user socket deliver", slog.String("stream", item.Stream), slog.Any("err", err))
					}
				}
			}
		}
	}()
	return func() {
		stopListen()
		cancel()
		<-done
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

// consumerReady is nil while the hub runs and has applied every stream
// message, or the oldest message it may not have applied is at most ReadyLag
// old: the stream head is newer than both the attach and the last applied
// message only while a message is pending, and that message is no older than
// the later of the two.
func (s *Server) consumerReady(ctx context.Context) error {
	running := s.running.Load()
	if running == 0 {
		return errors.New("gateway: consumer not attached")
	}
	str, err := s.js.Stream(ctx, stream.Name)
	if err != nil {
		return fmt.Errorf("gateway: stream: %w", err)
	}
	info, err := str.Info(ctx)
	if err != nil {
		return fmt.Errorf("gateway: stream: %w", err)
	}
	mark := time.Unix(0, max(running, s.applied.Load()))
	if !info.State.LastTime.After(mark) {
		return nil
	}
	if lag := time.Since(mark); lag > ReadyLag {
		return fmt.Errorf("gateway: consumer %s behind the stream head", lag)
	}
	return nil
}

// hold sets the topic count of sock to n, zero once it closes, and moves the
// subscriptions gauge by the difference.
func (s *Server) hold(sock *ws.Socket, n int) {
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	s.subscriptions.Add(float64(n - s.held[sock]))
	if n == 0 {
		delete(s.held, sock)
		return
	}
	s.held[sock] = n
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

// dataSocket serves the shared topic protocol on the socket paths whose
// topics the providers serve; while the gateway is not ready it closes the
// new socket with 1013 and a retry-after reason.
func (s *Server) dataSocket(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.providers.SocketTopics(r.URL.Path); !ok {
		writeError(w, http.StatusNotFound, "Unknown symbol: "+strings.ToUpper(r.PathValue("symbol")))
		return
	}
	if !s.ready.Load() {
		s.closer.ServeHTTP(w, r)
		return
	}
	s.data.ServeHTTP(w, r)
}

// hubProvider serves the hub from the topic providers, whose frames have the
// fields of ws.Frame, and counts the frames of subscribed topics by family
// and the snapshots the hub rebuilds outside a subscribe.
// A live frame the hub replaces by a fresh snapshot because its
// data is stale counts both as a frame and, with that snapshot, as a resync.
type hubProvider struct {
	*feeds.Providers
	frames  *prometheus.CounterVec
	resyncs prometheus.Counter

	mu sync.Mutex
	// watched is the subscriber count of every subscribed topic as the hub
	// reports it.
	watched map[string]int
	// expected counts the subscribe snapshots OnSubscribe announced that are
	// not built yet; every other snapshot is a resync.
	expected map[string]int
}

// expect announces the subscribe snapshot of topic the hub builds next.
func (p *hubProvider) expect(topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expected[topic]++
}

func (p *hubProvider) Subscribed(topic string) {
	p.Providers.Subscribed(topic)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watched[topic]++
}

func (p *hubProvider) Unsubscribed(topic string) {
	p.Providers.Unsubscribed(topic)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watched[topic] <= 1 {
		delete(p.watched, topic)
		return
	}
	p.watched[topic]--
}

func (p *hubProvider) Snapshot(ctx context.Context, topic string) (json.RawMessage, error) {
	data, err := p.Providers.Snapshot(ctx, topic)
	p.mu.Lock()
	if n := p.expected[topic]; n > 1 {
		p.expected[topic] = n - 1
	} else if n == 1 {
		delete(p.expected, topic)
	} else {
		p.resyncs.Inc()
	}
	p.mu.Unlock()
	if err == nil {
		p.frames.WithLabelValues(topicFamily(topic)).Inc()
	}
	return data, err
}

func (p *hubProvider) Message(ctx context.Context, subject string, env delivery.Envelope) ([]ws.Frame, []string, error) {
	frames, resnap, err := p.Providers.Message(ctx, subject, env)
	out := make([]ws.Frame, len(frames))
	for i, f := range frames {
		out[i] = ws.Frame(f)
		p.count(f.Topic)
	}
	return out, resnap, err
}

func (p *hubProvider) Run(ctx context.Context, emit func(ws.Frame)) error {
	return p.Providers.Run(ctx, func(f feeds.Frame) {
		p.count(f.Topic)
		emit(ws.Frame(f))
	})
}

// count counts one live frame of topic when the topic has a subscriber; the
// hub drops the frames of other topics.
func (p *hubProvider) count(topic string) {
	p.mu.Lock()
	watched := p.watched[topic] > 0
	p.mu.Unlock()
	if watched {
		p.frames.WithLabelValues(topicFamily(topic)).Inc()
	}
}

// topicFamily is the family of a served topic: book, trade, stats, market,
// totals or chain.
func topicFamily(topic string) string {
	switch topic {
	case feeds.TopicMarketsAll:
		return "market"
	case feeds.TopicMarketsTotals:
		return "totals"
	case feeds.TopicChainBlocks, feeds.TopicChainTxs:
		return "chain"
	}
	_, kind, _ := strings.Cut(topic, "@")
	switch strings.TrimPrefix(kind, "perp_") {
	case "orderbook":
		return "book"
	case "trade":
		return "trade"
	case "stats":
		return "stats"
	}
	return "market"
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
