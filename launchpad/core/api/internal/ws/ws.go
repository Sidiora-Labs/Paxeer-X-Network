// Package ws is the WebSocket transport of core/api. It exposes a multiplexed
// real-time stream backed by internal/broker: a client opens one socket, sends
// subscribe/unsubscribe frames (channels + pools), and receives the fanned-out
// events. A dedicated /ws/candles endpoint preserves the trading-charts client
// contract (subscribe by pools/timeframes -> candle_update frames).
//
// 500K-concurrency posture (invariant i11): a SINGLE writer goroutine per
// connection (gorilla permits only one concurrent writer); a bounded broker
// Subscription buffer with slow-client eviction; total + per-IP connection
// caps; server-initiated keepalive pings. The reader goroutine never writes —
// acknowledgements are routed to the writer via a small control channel.
package ws

import (
	"context"
	"errors"
	"net/url"

	"encoding/json"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
)

const (
	writeWait    = 5 * time.Second
	pingInterval = 30 * time.Second
	ctrlBuffer   = 8
)

// validChannels is the allowlist a client may subscribe to on the general /ws
// endpoint; anything else is ignored (defensive input handling, SECTION 17).
var validChannels = func() map[string]struct{} {
	m := make(map[string]struct{}, len(constants.Channels))
	for _, c := range constants.Channels {
		m[c] = struct{}{}
	}
	return m
}()

// TopicRegistry reports which requested v2 topics name a resource that does
// not exist (for example a pool the indexer never registered). Such topics can
// never be projected, so /v2/snapshots answers 404 NOT_FOUND (retryable=false)
// and /v2/stream ends the subscription with an error control and a 1008 close
// instead of a retryable dependency failure. A non-nil error is a read failure
// and keeps the retryable outcome.
type TopicRegistry interface {
	UnknownTopics(ctx context.Context, topics []string) ([]string, error)
}

// Hub fronts the broker for WebSocket clients and enforces connection caps.
type Hub struct {
	lifecycle      *broker.Lifecycle
	privateStream  *broker.PostgresStream
	topicRegistry  TopicRegistry
	allowedOrigins map[string]bool
	trustedProxies []string
	fanout         *broker.TopicFanout
	broker         *broker.Broker
	logger         *slog.Logger
	sendBuffer     int
	flush          time.Duration

	maxConns int
	maxPerIP int

	mu       sync.Mutex
	total    int
	ipCounts map[string]int
}

// Options configures NewHub.
type Options struct {
	Lifecycle     *broker.Lifecycle
	PrivateStream *broker.PostgresStream
	// TopicRegistry, when set, classifies topics naming unknown resources
	// (see TopicRegistry). Nil keeps every projection miss a 503.
	TopicRegistry     TopicRegistry
	AllowedOrigins    []string
	TrustedProxyCIDRs []string
	Fanout            *broker.TopicFanout
	Broker            *broker.Broker
	Logger            *slog.Logger
	SendBuffer        int
	Flush             time.Duration
	MaxConns          int
	MaxPerIP          int
}

// NewHub builds a WebSocket hub.
func NewHub(opts Options) *Hub {
	h := &Hub{
		lifecycle:      opts.Lifecycle,
		privateStream:  opts.PrivateStream,
		topicRegistry:  opts.TopicRegistry,
		allowedOrigins: make(map[string]bool),
		trustedProxies: append([]string(nil), opts.TrustedProxyCIDRs...),
		fanout:         opts.Fanout,
		broker:         opts.Broker,
		logger:         opts.Logger,
		sendBuffer:     opts.SendBuffer,
		flush:          opts.Flush,
		maxConns:       opts.MaxConns,
		maxPerIP:       opts.MaxPerIP,
		ipCounts:       make(map[string]int),
	}
	if h.lifecycle == nil {
		h.lifecycle = broker.NewLifecycle()
	}
	for _, origin := range opts.AllowedOrigins {
		h.allowedOrigins[origin] = true
	}
	if h.sendBuffer <= 0 {
		h.sendBuffer = 256
	}
	if h.flush <= 0 {
		h.flush = 100 * time.Millisecond
	}
	if h.maxConns <= 0 {
		h.maxConns = 50000
	}
	if h.maxPerIP <= 0 {
		h.maxPerIP = 20
	}
	return h
}

// Register mounts /ws (multiplexed) and /ws/candles (charts parity).
func (h *Hub) Register(r chi.Router) {
	r.Get("/v2/stream", h.privateHandle)
	r.Get("/v2/snapshots", h.snapshotHandle)
	r.Get("/ws", h.handle(""))
	r.Get("/ws/candles", h.handle(constants.ChannelCandleUpdate))
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// acquire reserves a connection slot for ip, or returns false (with a reason)
// when a capacity limit is reached.
func (h *Hub) acquire(ip string) (ok bool, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.lifecycle.Ready() || h.total >= h.maxConns {
		return false, "Server at capacity"
	}
	if h.ipCounts[ip] >= h.maxPerIP {
		return false, "Too many connections from this IP"
	}
	h.total++
	h.ipCounts[ip]++
	return true, ""
}

func (h *Hub) release(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total--
	if h.ipCounts[ip] <= 1 {
		delete(h.ipCounts, ip)
	} else {
		h.ipCounts[ip]--
	}
}

// Connections returns the current live WebSocket connection count.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// handle returns the HTTP handler for a mount. fixedChannel != "" locks the
// connection to a single channel (the /ws/candles parity endpoint).
func (h *Hub) handle(fixedChannel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if ok, reason := h.acquire(ip); !ok {
			// Reject before the upgrade so we never hold a socket we can't serve.
			http.Error(w, reason, http.StatusServiceUnavailable)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			h.release(ip)
			return
		}

		c := &wsConn{
			hub:          h,
			conn:         conn,
			ip:           ip,
			fixedChannel: fixedChannel,
			ctrl:         make(chan []byte, ctrlBuffer),
			channels:     make(map[string]struct{}),
			pools:        make(map[string]struct{}),
		}
		// A wildcard/candles connection has an implicit channel; a general one
		// starts subscribed to nothing until it sends a subscribe frame.
		c.sub = h.broker.Subscribe(c.filter(), h.sendBuffer)
		c.run(r.Context())
	}
}

// wsConn is one client connection. The reader goroutine (run) owns channels/
// pools; the writer goroutine (writePump) owns all socket writes.
type wsConn struct {
	hub          *Hub
	conn         *websocket.Conn
	ip           string
	fixedChannel string
	sub          *broker.Subscription
	ctrl         chan []byte

	channels map[string]struct{}
	pools    map[string]struct{}
}

// filter builds the broker filter from the connection's desired channels/pools.
func (c *wsConn) filter() broker.Filter {
	pools := cloneSet(c.pools)
	if c.fixedChannel != "" {
		return broker.Filter{Channels: map[string]struct{}{c.fixedChannel: {}}, Pools: pools}
	}
	return broker.Filter{Channels: cloneSet(c.channels), Pools: pools}
}

func (c *wsConn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.sub.Close()
		_ = c.conn.Close()
		c.hub.release(c.ip)
	}()

	go c.writePump(ctx)

	c.enqueueCtrl(mustJSON(map[string]any{
		"type":    "connected",
		"message": `Send {"type":"subscribe","channels":["indexer:swap"],"pools":["0x..."]} to stream events`,
	}))

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var frame struct {
			Type       string   `json:"type"`
			Channels   []string `json:"channels"`
			Pools      []string `json:"pools"`
			Timeframes []string `json:"timeframes"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			c.enqueueCtrl(mustJSON(map[string]string{"type": "error", "message": "Invalid JSON"}))
			continue
		}
		switch frame.Type {
		case "subscribe":
			c.applySubscribe(frame.Channels, frame.Pools, true)
			c.ackSubscription("subscribed", frame.Timeframes)
		case "unsubscribe":
			c.applySubscribe(frame.Channels, frame.Pools, false)
			c.ackSubscription("unsubscribed", frame.Timeframes)
		case "ping":
			c.enqueueCtrl(mustJSON(map[string]string{"type": "pong"}))
		}
	}
}

// applySubscribe mutates the desired channel/pool sets and re-indexes the broker
// subscription. add=false removes the given keys.
func (c *wsConn) applySubscribe(channels, pools []string, add bool) {
	if c.fixedChannel == "" {
		for _, ch := range channels {
			if _, ok := validChannels[ch]; !ok {
				continue
			}
			if add {
				c.channels[ch] = struct{}{}
			} else {
				delete(c.channels, ch)
			}
		}
	}
	for _, p := range pools {
		// Canonicalise to lowercase so the broker's exact-match pool filter
		// matches the lowercased pool the transform routes on, regardless of the
		// casing the client subscribed with (Bug 3 / 6a). The ack echo
		// (setKeys(c.pools)) is then lowercase too.
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if add {
			c.pools[p] = struct{}{}
		} else {
			delete(c.pools, p)
		}
	}
	c.hub.broker.Resubscribe(c.sub, c.filter())
}

func (c *wsConn) ackSubscription(typ string, timeframes []string) {
	ack := map[string]any{
		"type":  typ,
		"pools": setKeys(c.pools),
	}
	if c.fixedChannel != "" {
		ack["timeframes"] = timeframes
	} else {
		ack["channels"] = setKeys(c.channels)
	}
	c.enqueueCtrl(mustJSON(ack))
}

// writePump is the SOLE writer. It drains broker frames on the flush tick and on
// the must-deliver signal, interleaves control frames, and emits keepalive pings.
func (c *wsConn) writePump(ctx context.Context) {
	flush := time.NewTicker(c.hub.flush)
	ping := time.NewTicker(pingInterval)
	defer func() {
		flush.Stop()
		ping.Stop()
		_ = c.conn.Close() // unblock the reader on teardown
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.ctrl:
			if !c.write(msg) {
				return
			}
		case <-c.sub.Signal():
			if !c.drain() {
				return
			}
		case <-flush.C:
			if !c.drain() {
				return
			}
		case <-ping.C:
			if !c.write(mustJSON(map[string]any{"type": "ping", "ts": time.Now().UnixMilli()})) {
				return
			}
		}
	}
}

// drain flushes the buffered broker frames. It returns false (ending the pump)
// if the subscription was evicted or a write failed.
func (c *wsConn) drain() bool {
	if c.sub.Evicted() {
		_ = c.write(mustJSON(map[string]string{"type": "error", "message": "slow consumer evicted"}))
		return false
	}
	for _, b := range c.sub.Drain() {
		if !c.write(b) {
			return false
		}
	}
	return true
}

func (c *wsConn) write(b []byte) bool {
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return false
	}
	return c.conn.WriteMessage(websocket.TextMessage, b) == nil
}

// enqueueCtrl performs a non-blocking control-frame enqueue. A full control
// buffer means the client is not draining; the connection will be torn down by
// the read side, so dropping the ack is safe.
func (c *wsConn) enqueueCtrl(b []byte) {
	select {
	case c.ctrl <- b:
	default:
		_ = c.conn.Close()
	}
}

func cloneSet(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func setKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func streamHTTPError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Schema-Version", "2")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": code, "requestId": uuid.NewString(), "retryable": status == 503, "retryAfterMs": nil, "fieldErrors": []any{}, "details": nil})
}
func (h *Hub) streamBinding(r *http.Request, topics []string) sharedauth.StreamTicketBinding {
	return sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: append([]string(nil), topics...), FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: r.Header.Get("Origin"), Endpoint: "/v2/stream"}
}
func (h *Hub) snapshotHandle(w http.ResponseWriter, r *http.Request) {
	if h.privateStream == nil || !h.lifecycle.Ready() {
		streamHTTPError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	origin, originErr := broker.TrustedHTTPOrigin(r, h.allowedOrigins, h.trustedProxies)
	if originErr != nil {
		streamHTTPError(w, 403, "FORBIDDEN")
		return
	}
	if len(r.URL.RawQuery) > 8192 {
		streamHTTPError(w, 422, "FIELD_INVALID")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 1 || len(q["topics"]) != 1 {
		streamHTTPError(w, 422, "FIELD_INVALID")
		return
	}
	topics := strings.Split(q.Get("topics"), ",")
	shape, err := json.Marshal(map[string]any{"topics": topics})
	if err != nil || interfaces.ValidateJSON("TopicSnapshotReadRequest", shape) != nil {
		streamHTTPError(w, 422, "FIELD_INVALID")
		return
	}
	var principal sharedauth.StreamPrincipal
	auth := r.Header.Get("Authorization")
	if auth == "" && broker.PublicTopics(topics) {
	} else {
		authRequest := r.Clone(r.Context())
		authRequest.Header = r.Header.Clone()
		authRequest.Header.Set("Origin", origin)
		principal, err = h.privateStream.AuthenticateRequest(r.Context(), authRequest)
		if err != nil {
			streamHTTPError(w, 401, "AUTH_REQUIRED")
			return
		}
	}
	binding := h.streamBinding(r, topics)
	binding.Origin = origin
	snapshot, err := h.privateStream.Snapshot(r.Context(), principal, binding)
	if err != nil {
		// A projection miss for a topic naming an unregistered resource is
		// permanent: 404 NOT_FOUND (declared for IF_TOPIC_SNAPSHOT_READ),
		// retryable=false, no Retry-After. Any other miss, and any registry
		// read failure, stays 503 DEPENDENCY_UNAVAILABLE retryable.
		if errors.Is(err, broker.ErrStreamUnavailable) && h.topicRegistry != nil {
			if unknown, registryErr := h.topicRegistry.UnknownTopics(r.Context(), topics); registryErr == nil && len(unknown) > 0 {
				streamHTTPError(w, 404, "NOT_FOUND")
				return
			}
		}
		streamHTTPError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	body, err := json.Marshal(map[string]any{"requestId": uuid.NewString(), "data": map[string]any{"snapshot": snapshot}, "watermark": nil, "revision": nil, "quality": "complete", "nextCursor": nil, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	if err != nil || interfaces.ValidateJSON("TopicSnapshotReadResponse", body) != nil {
		streamHTTPError(w, 503, "INTEGRITY_QUARANTINED")
		return
	}
	if principal.AccountID != "" {
		if err = h.privateStream.Authority.CheckStreamSession(r.Context(), principal); err != nil {
			streamHTTPError(w, 401, "AUTH_REQUIRED")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Schema-Version", "2")
	_, _ = w.Write(body)
}
