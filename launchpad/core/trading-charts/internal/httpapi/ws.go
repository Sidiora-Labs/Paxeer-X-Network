package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
)

const (
	// sendBufferSize bounds each connection's outbound queue (invariant i11:
	// bounded buffers). When a client's queue is full it is evicted as a slow
	// consumer rather than letting the broadcast goroutine block or grow
	// unbounded.
	sendBufferSize = 256
	// writeWait bounds a single socket write so a stalled client cannot pin a
	// writer goroutine forever.
	writeWait = 10 * time.Second
	// pingInterval is the server-initiated keepalive cadence (C-5).
	pingInterval = 30 * time.Second
)

// WSDeps holds dependencies for the WebSocket candle stream.
type WSDeps struct {
	ReplayGatewayOrigin string
	RedisURL            string
	Logger              *slog.Logger
	MaxConnections      int
	MaxPerIP            int
	AllowedOrigins      []string
	// CandleChannel must be a coordinator-provisioned partitioned delivery subject.
	CandleChannel string
	// CandleRetentionCapacity is bounded to4096 identities; exhaustion requests resync.
	CandleRetentionCapacity int
}

type clientSub struct {
	conn     *websocket.Conn
	send     chan []byte
	quit     chan struct{}
	quitOnce sync.Once
	queued   atomic.Int64
	cursor   string
	topics   map[string]struct{}
}

// stop signals the client's writer goroutine to exit. It is idempotent and safe
// to call from multiple goroutines (broadcast eviction and read-loop teardown).
func (c *clientSub) stop() {
	c.quitOnce.Do(func() { close(c.quit) })
}

type wsHub struct {
	mu                sync.RWMutex
	clients           map[*clientSub]struct{}
	subscriptionIndex map[string]map[*clientSub]struct{}
	wildcardSubs      map[*clientSub]struct{}
	ipCounts          map[string]int
	logger            *slog.Logger
	maxConns          int
	maxPerIP          int
	origins           []string
	channel           string
	retentionCapacity int
}

// RegisterWS registers the /ws WebSocket endpoint for real-time candle updates.
func RegisterWS(ctx context.Context, r chi.Router, deps WSDeps) {
	if deps.ReplayGatewayOrigin != "" {
		if err := RegisterReplayGateway(r, deps.ReplayGatewayOrigin); err != nil && deps.Logger != nil {
			deps.Logger.Error("durable replay gateway registration failed", "error", err)
		}
	}
	hub := &wsHub{
		clients:           make(map[*clientSub]struct{}),
		subscriptionIndex: make(map[string]map[*clientSub]struct{}),
		wildcardSubs:      make(map[*clientSub]struct{}),
		ipCounts:          make(map[string]int),
		logger:            deps.Logger,
		maxConns:          deps.MaxConnections,
		maxPerIP:          deps.MaxPerIP,
		origins:           deps.AllowedOrigins, channel: deps.CandleChannel,
		retentionCapacity: deps.CandleRetentionCapacity,
	}
	if hub.maxConns <= 0 {
		hub.maxConns = 10000
	}
	if hub.maxPerIP <= 0 {
		hub.maxPerIP = 20
	}

	// Redis subscriber for candle updates (dedicated connection).
	go hub.runRedisSubscriber(ctx, deps.RedisURL)
	go func() {
		<-ctx.Done()
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		for c := range hub.clients {
			c.stop()
		}
	}()

	r.Get("/ws", hub.handleWS)
}

func (h *wsHub) subKey(pool, tf string) string {
	return pool + ":" + tf
}

func (h *wsHub) indexClient(c *clientSub) {
	h.removeFromIndex(c)
	for key := range c.topics {
		set := h.subscriptionIndex[key]
		if set == nil {
			set = map[*clientSub]struct{}{}
			h.subscriptionIndex[key] = set
		}
		set[c] = struct{}{}
	}

}

func (h *wsHub) removeFromIndex(c *clientSub) {
	delete(h.wildcardSubs, c)
	for key, set := range h.subscriptionIndex {
		delete(set, c)
		if len(set) == 0 {
			delete(h.subscriptionIndex, key)
		}
	}
}

func (h *wsHub) runRedisSubscriber(ctx context.Context, redisURL string) {
	rdb := goredis.NewClient(parseRedisOpts(redisURL))
	defer rdb.Close()

	if h.channel == "" {
		return
	} // Fail closed until the common delivery gateway supplies a partition.
	ps := rdb.Subscribe(ctx, h.channel)
	defer ps.Close()

	ch := ps.Channel()
	capacity := h.retentionCapacity
	if capacity <= 0 || capacity > 4096 {
		capacity = 4096
	}
	coalescer := NewCandleCoalescer(capacity)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var candle CandleSnapshot
			if json.Unmarshal([]byte(msg.Payload), &candle) == nil {
				if err := coalescer.Add(candle); err != nil {
					if h.logger != nil {
						h.logger.Warn("candle rejected", slog.String("error", err.Error()))
					}
					h.resyncSeries(candle)
				}
			}
		case now := <-ticker.C:
			for _, candle := range coalescer.Drain(now) {
				raw, _ := json.Marshal(candle)
				h.broadcastCandleUpdate(string(raw))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *wsHub) broadcastCandleUpdate(payload string) {
	var event CandleSnapshot
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}
	event.PoolAddress = strings.ToLower(event.PoolAddress)
	msg, err := EncodeCandle(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.subscriptionIndex[h.subKey(event.PoolAddress, event.Timeframe)] {
		h.enqueue(client, msg)
	}
}

// enqueue performs a non-blocking send to the client's bounded queue. If the
// queue is full the client is a slow consumer and is evicted (invariant i11
// slow-client eviction) instead of blocking the broadcast goroutine.
func (h *wsHub) enqueue(c *clientSub, msg []byte) {
	select {
	case <-c.quit:
		return
	default:
	}
	if c.queued.Add(int64(len(msg))) > 256*1024 {
		c.queued.Add(-int64(len(msg)))
		c.stop()
		return
	}
	select {
	case c.send <- msg:
	case <-c.quit:
		c.queued.Add(-int64(len(msg)))
	default:
		c.queued.Add(-int64(len(msg)))
		c.stop()
	}
}

// writePump is the SOLE writer for a connection (gorilla/websocket permits only
// one concurrent writer). It drains the bounded send queue and emits keepalive
// pings, each bounded by writeWait. It exits on write error or stop(), closing
// the connection so the read loop unblocks and teardown runs.
func (h *wsHub) writePump(c *clientSub) {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg := <-c.send:
			c.queued.Add(-int64(len(msg)))
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
			var delivered struct {
				Data struct {
					Cursor string `json:"cursor"`
				} `json:"data"`
			}
			if json.Unmarshal(msg, &delivered) == nil && delivered.Data.Cursor != "" {
				c.cursor = delivered.Data.Cursor
			}
		case <-ticker.C:
			ping, err := json.Marshal(map[string]interface{}{
				"type": "ping",
				"ts":   time.Now().UnixMilli(),
			})
			if err != nil {
				continue
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, ping); err != nil {
				return
			}
		case <-c.quit:
			_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1013, "resync_required;cursor="+c.cursor), time.Now().Add(writeWait))
			return
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		u, e := url.Parse(o)
		return e == nil && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
	},
}

func (h *wsHub) handleWS(w http.ResponseWriter, r *http.Request) {
	up := upgrader
	if len(h.origins) > 0 {
		up.CheckOrigin = func(r *http.Request) bool {
			for _, o := range h.origins {
				if r.Header.Get("Origin") == o {
					return true
				}
			}
			return false
		}
	}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn.SetReadLimit(16 * 1024)
	clientIP := clientIPFromRequest(r)

	h.mu.Lock()
	if len(h.clients) >= h.maxConns {
		h.mu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"Server at capacity"}`)) //nolint:errcheck // best-effort capacity notice; the connection is closed immediately after
		_ = conn.Close()
		return
	}
	if h.ipCounts[clientIP] >= h.maxPerIP {
		h.mu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"Too many connections from this IP"}`)) //nolint:errcheck // best-effort limit notice; the connection is closed immediately after
		_ = conn.Close()
		return
	}
	h.ipCounts[clientIP]++

	sub := &clientSub{
		conn: conn,
		send: make(chan []byte, sendBufferSize),
		quit: make(chan struct{}),
	}
	h.clients[sub] = struct{}{}
	h.mu.Unlock()

	// writePump is the single writer for this connection; the loop below is the
	// single reader. All outbound frames go through sub.send (invariant i11).
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); h.writePump(sub) }()

	defer func() {
		sub.stop()
		select {
		case <-writerDone:
		case <-time.After(writeWait):
			_ = conn.Close()
		}
		h.mu.Lock()
		h.removeFromIndex(sub)
		delete(h.clients, sub)
		h.ipCounts[clientIP]--
		if h.ipCounts[clientIP] <= 0 {
			delete(h.ipCounts, clientIP)
		}
		h.mu.Unlock()
		_ = conn.Close()
	}()

	// Send welcome message.
	if welcome, err := json.Marshal(map[string]interface{}{
		"type":    "connected",
		"message": `Send { type: "subscribe", pools: ["0x..."], timeframes: ["1m"] } to start receiving candle updates`,
	}); err == nil {
		h.enqueue(sub, welcome)
	}

	budget := subscriptionCommandBudget{tokens: 40, updated: time.Now()}
	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if messageType != websocket.TextMessage || !budget.take(time.Now()) {
			sub.stop()
			return
		}
		var msg struct {
			Type       string               `json:"type"`
			RequestID  string               `json:"requestId"`
			Pools      []string             `json:"pools"`
			Timeframes []string             `json:"timeframes"`
			Series     []seriesSubscription `json:"series"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			if errMsg, mErr := json.Marshal(map[string]string{"type": "error", "message": "Invalid JSON"}); mErr == nil {
				h.enqueue(sub, errMsg)
			}
			continue
		}

		switch msg.Type {
		case "subscribe", "unsubscribe":
			topics, err := validateTopics(msg.Series, msg.Pools, msg.Timeframes)
			if err == nil {
				h.mu.Lock()
				next := copySet(sub.topics)
				for key := range topics {
					if msg.Type == "subscribe" {
						next[key] = struct{}{}
					} else {
						delete(next, key)
					}
				}

				if len(next) > 20 {
					err = fmt.Errorf("subscription quota exceeded")
				}
				if err == nil {
					sub.topics = next
					h.indexClient(sub)
				}
				h.mu.Unlock()
			}
			response := map[string]interface{}{"requestId": msg.RequestID}
			if err != nil {
				response["type"] = "error"
				response["message"] = err.Error()
			} else {
				response["type"] = msg.Type + "d"
			}
			raw, _ := json.Marshal(response)
			h.enqueue(sub, raw)

		case "ping":
			if pong, err := json.Marshal(map[string]string{"type": "pong"}); err == nil {
				h.enqueue(sub, pong)
			}
		}
	}
}

func clientIPFromRequest(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func parseRedisOpts(redisURL string) *goredis.Options {
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return &goredis.Options{}
	}
	return opts
}

func copySet(m map[string]struct{}) map[string]struct{} {
	n := make(map[string]struct{}, len(m))
	for k := range m {
		n[k] = struct{}{}
	}
	return n
}
func validateSubscriptions(pools, tfs []string) (map[string]struct{}, map[string]struct{}, error) {
	p, t := map[string]struct{}{}, map[string]struct{}{}
	if len(pools) == 0 || len(tfs) == 0 || len(pools) > 20 || len(tfs) > 20 {
		return p, t, fmt.Errorf("explicit series required; maximum 20")
	}
	for _, v := range pools {
		v = strings.ToLower(v)
		if !poolPattern.MatchString(v) {
			return p, t, fmt.Errorf("invalid pool")
		}
		p[v] = struct{}{}
	}
	for _, v := range tfs {
		if _, ok := constants.Timeframes[v]; !ok {
			return p, t, fmt.Errorf("unknown timeframe")
		}
		t[v] = struct{}{}
	}
	if len(p)*len(t) > 20 {
		return p, t, fmt.Errorf("subscription quota exceeded")
	}
	return p, t, nil
}

type seriesSubscription struct {
	PoolAddress string `json:"poolAddress"`
	Timeframe   string `json:"timeframe"`
}

func validateTopics(series []seriesSubscription, pools, tfs []string) (map[string]struct{}, error) {
	topics := map[string]struct{}{}
	if len(series) > 0 {
		if len(series) > 20 || len(pools) > 0 || len(tfs) > 0 {
			return nil, fmt.Errorf("maximum20 explicit series; mixed subscription formats prohibited")
		}
		for _, v := range series {
			p, t, e := validateSubscriptions([]string{v.PoolAddress}, []string{v.Timeframe})
			if e != nil {
				return nil, e
			}
			for pool := range p {
				for tf := range t {
					topics[pool+":"+tf] = struct{}{}
				}
			}
		}
	} else {
		p, t, e := validateSubscriptions(pools, tfs)
		if e != nil {
			return nil, e
		}
		for pool := range p {
			for tf := range t {
				topics[pool+":"+tf] = struct{}{}
			}
		}
	}
	return topics, nil
}

func (h *wsHub) resyncSeries(candle CandleSnapshot) {
	raw, _ := json.Marshal(map[string]string{"type": "resync_required", "reason": "candle_revision_integrity_or_capacity"})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.subscriptionIndex[h.subKey(strings.ToLower(candle.PoolAddress), candle.Timeframe)] {
		h.enqueue(client, raw)
	}
}

type subscriptionCommandBudget struct {
	tokens  float64
	updated time.Time
}

func (b *subscriptionCommandBudget) take(now time.Time) bool {
	elapsed := now.Sub(b.updated).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * 10
		if b.tokens > 40 {
			b.tokens = 40
		}
		b.updated = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
