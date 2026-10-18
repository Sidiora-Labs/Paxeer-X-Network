// Package sse is the Server-Sent Events transport of core/api: a one-directional
// alternative to the WebSocket stream for clients (or proxies) that prefer plain
// HTTP. It shares the internal/broker fan-out engine, so the same bounded-buffer
// + slow-client-eviction + coalescing guarantees apply (invariant i11).
//
// Subscription is expressed via query params on connect (SSE has no client->
// server channel): ?channels=indexer:swap,candles:update&pools=0xA,0xB
// (channels=* subscribes to everything). Each event is emitted as a default
// "data:" frame carrying the same JSON envelope the WS stream uses, so a client
// reads event.type identically across transports. Per-write deadlines (via
// http.ResponseController) bound slow clients without a global server
// WriteTimeout that would otherwise kill long-lived streams.
package sse

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
)

const (
	writeWait         = 5 * time.Second
	heartbeatInterval = 15 * time.Second
)

var validChannels = func() map[string]struct{} {
	m := make(map[string]struct{}, len(constants.Channels))
	for _, c := range constants.Channels {
		m[c] = struct{}{}
	}
	return m
}()

// Hub fronts the broker for SSE clients and enforces connection caps.
type Hub struct {
	privateStream  *broker.PostgresStream
	allowedOrigins map[string]bool
	trustedProxies []string
	fanout         *broker.TopicFanout
	lifecycle      *broker.Lifecycle
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
	PrivateStream     *broker.PostgresStream
	AllowedOrigins    []string
	TrustedProxyCIDRs []string
	Fanout            *broker.TopicFanout
	Lifecycle         *broker.Lifecycle
	Broker            *broker.Broker
	Logger            *slog.Logger
	SendBuffer        int
	Flush             time.Duration
	MaxConns          int
	MaxPerIP          int
}

// NewHub builds an SSE hub.
func NewHub(opts Options) *Hub {
	h := &Hub{
		privateStream:  opts.PrivateStream,
		allowedOrigins: make(map[string]bool),
		trustedProxies: append([]string(nil), opts.TrustedProxyCIDRs...),
		fanout:         opts.Fanout,
		lifecycle:      opts.Lifecycle,
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

// Register mounts /stream (multiplexed) and /stream/candles (charts parity).
func (h *Hub) Register(r chi.Router) {
	r.Get("/v2/stream/sse", h.authenticatedHandle)
	r.Get("/stream", h.handle(""))
	r.Get("/stream/candles", h.handle(constants.ChannelCandleUpdate))
}

func (h *Hub) acquire(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.lifecycle.Ready() || h.total >= h.maxConns || h.ipCounts[ip] >= h.maxPerIP {
		return false
	}
	h.total++
	h.ipCounts[ip]++
	return true
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

// Connections returns the current live SSE connection count.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

func (h *Hub) handle(fixedChannel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !h.acquire(ip) {
			http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
			return
		}
		defer h.release(ip)

		rc := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
		w.WriteHeader(http.StatusOK)

		filter := buildFilter(fixedChannel, r)
		sub := h.broker.Subscribe(filter, h.sendBuffer)
		defer sub.Close()

		// Initial comment opens the stream immediately for the client.
		if !writeRaw(w, rc, []byte(":ok\n\n")) {
			return
		}

		flush := time.NewTicker(h.flush)
		defer flush.Stop()
		heartbeat := time.NewTicker(heartbeatInterval)
		defer heartbeat.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.Signal():
				if !drain(w, rc, sub) {
					return
				}
			case <-flush.C:
				if !drain(w, rc, sub) {
					return
				}
			case <-heartbeat.C:
				if !writeRaw(w, rc, []byte(":hb\n\n")) {
					return
				}
			}
		}
	}
}

// drain writes the buffered broker frames as SSE data events. Returns false to
// end the stream on eviction or a write error.
func drain(w http.ResponseWriter, rc *http.ResponseController, sub *broker.Subscription) bool {
	if sub.Evicted() {
		_ = writeRaw(w, rc, []byte("event: error\ndata: {\"type\":\"error\",\"message\":\"slow consumer evicted\"}\n\n"))
		return false
	}
	for _, b := range sub.Drain() {
		frame := make([]byte, 0, len(b)+8)
		frame = append(frame, "data: "...)
		frame = append(frame, b...)
		frame = append(frame, '\n', '\n')
		if !writeRaw(w, rc, frame) {
			return false
		}
	}
	return true
}

// writeRaw writes b under a bounded write deadline and flushes. Returns false on
// any error (deadline exceeded => slow client => teardown).
func writeRaw(w http.ResponseWriter, rc *http.ResponseController, b []byte) bool {
	if err := rc.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return false
	}
	// G705 false positive: this is a text/event-stream (not HTML); b is a
	// server-generated broker frame, never reflected client input.
	if _, err := w.Write(b); err != nil { //nolint:gosec // SSE stream, not HTML
		return false
	}
	return rc.Flush() == nil
}

// buildFilter derives a broker filter from the request query (or the fixed
// channel for /stream/candles).
func buildFilter(fixedChannel string, r *http.Request) broker.Filter {
	pools := splitSet(r.URL.Query().Get("pools"))
	if fixedChannel != "" {
		return broker.Filter{Channels: map[string]struct{}{fixedChannel: {}}, Pools: pools}
	}
	raw := strings.TrimSpace(r.URL.Query().Get("channels"))
	if raw == "*" {
		return broker.Filter{All: true, Pools: pools}
	}
	channels := make(map[string]struct{})
	for _, c := range strings.Split(raw, ",") {
		c = strings.TrimSpace(c)
		if _, ok := validChannels[c]; ok {
			channels[c] = struct{}{}
		}
	}
	return broker.Filter{Channels: channels, Pools: pools}
}

// splitSet parses the comma-separated `pools` query into a set, canonicalising
// each pool to lowercase so the broker's exact-match filter matches the
// lowercased pool the transform routes on regardless of the casing the client
// sent (Bug 3 / 6a).
func splitSet(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, p := range strings.Split(raw, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
