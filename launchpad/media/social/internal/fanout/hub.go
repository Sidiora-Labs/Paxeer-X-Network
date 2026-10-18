// Package fanout implements the media/social realtime hub: an authenticated
// WebSocket endpoint plus Redis pub/sub fan-out for pool chat rooms and direct
// messages. Its bounded queues support later capacity qualification:
//
//   - every connection has a BOUNDED outbound queue; a client that can't keep up
//     is evicted (slow-client eviction) rather than growing memory without bound;
//   - Redis channels are subscribed lazily (refcounted) and unsubscribed when the
//     last local subscriber leaves, so a single subscriber connection multiplexes
//     all rooms;
//   - sends commit message and publication intent atomically; the injected durable
//     publisher drains outbox work. Redis is the live delivery adapter, while
//     membership-checked database history provides bounded recovery.
//
// Identity is supplied by a verified gateway upgrade adapter, never a raw header.
package fanout

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
)

const (
	poolChannelPrefix = "pool:"
	dmChannelPrefix   = "dm:wallet:"
)

// Deps are the hub dependencies.
type Deps struct {
	ProtocolVersion int

	ChannelNamespace string
	Durable          *DurableStore
	Authenticate     AuthenticateUpgrade
	Queries          sqlcdb.Querier
	// Pub publishes fan-out messages and backs the rate limiter.
	Pub *goredis.Client
	// Sub is a DEDICATED client for the pub/sub subscriber (a subscribed
	// connection cannot issue other commands).
	Sub *goredis.Client

	MaxMessageLength int
	RateWindow       time.Duration
	MaxPoolMsgs      int
	MaxDmMsgs        int

	SendBuffer int
	WriteWait  time.Duration
	ReadLimit  int64

	Logger *slog.Logger
	Clock  func() time.Time
	// CheckOrigin overrides the upgrader origin check (gateway terminates origin
	// policy; defaults to allow-all behind the gateway).
	CheckOrigin func(*http.Request) bool
}

type metrics struct {
	connected       atomic.Int64
	evicted         atomic.Int64
	queueFramesPeak atomic.Int64
	queueBytesPeak  atomic.Int64
	enqueuedFrames  atomic.Int64
	enqueuedBytes   atomic.Int64
	writtenFrames   atomic.Int64
	writtenBytes    atomic.Int64
}

// Hub is the realtime fan-out hub.
type Hub struct {
	protocolVersion int

	namespace    string
	durable      *DurableStore
	authenticate AuthenticateUpgrade
	q            sqlcdb.Querier
	pub          *goredis.Client
	sub          *goredis.Client
	ps           *goredis.PubSub
	upg          websocket.Upgrader
	log          *slog.Logger
	clock        func() time.Time

	maxMsgLen   int
	rateWindow  time.Duration
	maxPoolMsgs int
	maxDmMsgs   int

	sendBuffer int
	writeWait  time.Duration
	readLimit  int64

	mu           sync.RWMutex
	poolRooms    map[string]map[*conn]struct{}
	walletConns  map[string]map[*conn]struct{}
	poolChanRefs map[string]int
	dmChanRefs   map[string]int

	metrics metrics
}

// New constructs a Hub. The dedicated subscriber PubSub is created eagerly (with
// no channels) so rooms can be subscribed lazily as clients join.
func New(d Deps) *Hub {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	checkOrigin := d.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = func(*http.Request) bool { return true }
	}
	h := &Hub{
		protocolVersion: orDefault(d.ProtocolVersion, 1),
		q:               d.Queries,
		namespace:       d.ChannelNamespace,
		durable:         d.Durable,
		authenticate:    d.Authenticate,
		pub:             d.Pub,
		sub:             d.Sub,
		log:             d.Logger,
		clock:           clock,
		maxMsgLen:       orDefault(d.MaxMessageLength, 500),
		rateWindow:      orDefaultDur(d.RateWindow, 10*time.Second),
		maxPoolMsgs:     orDefault(d.MaxPoolMsgs, 5),
		maxDmMsgs:       orDefault(d.MaxDmMsgs, 5),
		sendBuffer:      min(orDefault(d.SendBuffer, 256), 256),
		writeWait:       orDefaultDur(d.WriteWait, 10*time.Second),
		readLimit:       int64(min(orDefault(int(d.ReadLimit), 16384), 16384)),
		poolRooms:       make(map[string]map[*conn]struct{}),
		walletConns:     make(map[string]map[*conn]struct{}),
		poolChanRefs:    make(map[string]int),
		dmChanRefs:      make(map[string]int),
		upg: websocket.Upgrader{
			Subprotocols:    []string{"sidiora.v2"},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     checkOrigin,
		},
	}
	h.ps = d.Sub.Subscribe(context.Background())
	return h
}

func NewConnected(d Deps) (*Hub, error) {
	if !d.Durable.ready() || d.Durable.ReadPolicy == nil || d.Durable.Projection == nil || d.Durable.Policy == nil || d.Durable.NotificationQueue == nil || d.Durable.PendingOutboxLimit < 1 || d.Durable.MaxOutboxAge <= 0 || d.Authenticate == nil || d.Pub == nil || d.Sub == nil {
		return nil, ErrUnavailable
	}
	d.ProtocolVersion = 2
	return New(d), nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func orDefaultDur(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// Run drives the Redis subscriber loop until ctx is cancelled, then closes the
// subscriber connection.
func (h *Hub) Run(ctx context.Context) error {
	defer func() {
		h.mu.RLock()
		conns := []*conn{}
		for _, set := range h.walletConns {
			for c := range set {
				conns = append(conns, c)
			}
		}
		h.mu.RUnlock()
		for _, c := range conns {
			c.close()
		}
	}()
	ch := h.ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return h.ps.Close()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			h.dispatch(msg.Channel, []byte(msg.Payload))
		}
	}
}

// ServeWS is the GET /ws handler: it requires a verified upgrade principal,
// upgrades the connection, and starts its read/write pumps.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if h.authenticate == nil {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "verified upgrade identity required")
		return
	}
	principal, err := h.authenticate(r)
	if err != nil || !principal.valid(r.Context(), h.clock()) {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "invalid or expired session")
		return
	}
	if h.protocolVersion == 2 {
		accepted := false
		for _, protocol := range websocket.Subprotocols(r) {
			if protocol == "sidiora.v2" {
				accepted = true
			}
		}
		if !accepted || len(principal.Topics) == 0 {
			sharedhttp.WriteError(w, 401, "Unauthorized", "scoped upgrade ticket required")
			return
		}
	}
	ws, err := h.upg.Upgrade(w, r, nil)
	if err != nil {
		return // upgrader already wrote the error
	}
	c := newConn(h, ws, principal.Wallet)
	c.principal = principal
	h.addConn(c)
	h.metrics.connected.Add(1)
	c.enqueue(h.marshal(map[string]any{"type": "session_accepted", "protocol_version": h.protocolVersion}))
	go c.writePump()
	go c.readPump()
}

// ── connection registry ───────────────────────────────────────────────────────

func (h *Hub) addConn(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.walletConns[c.actor]
	if set == nil {
		set = make(map[*conn]struct{})
		h.walletConns[c.actor] = set
	}
	set[c] = struct{}{}
}

// removeConn detaches a connection from every room and the wallet registry,
// unsubscribing Redis channels that have no remaining local subscribers.
func (h *Hub) removeConn(c *conn) {
	pools := c.joinedPools()
	dmSub := c.isDMSubscribed()

	var toUnsubPool []string
	var toUnsubDM string

	h.mu.Lock()
	for _, p := range pools {
		if room := h.poolRooms[p]; room != nil {
			if _, ok := room[c]; ok {
				delete(room, c)
				h.poolChanRefs[p]--
				if h.poolChanRefs[p] <= 0 {
					delete(h.poolChanRefs, p)
					delete(h.poolRooms, p)
					toUnsubPool = append(toUnsubPool, h.namespace+poolChannelPrefix+p)
				}
			}
		}
	}
	if set := h.walletConns[c.actor]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.walletConns, c.actor)
		}
	}
	if dmSub {
		h.dmChanRefs[c.actor]--
		if h.dmChanRefs[c.actor] <= 0 {
			delete(h.dmChanRefs, c.actor)
			toUnsubDM = h.namespace + dmChannelPrefix + c.actor
		}
	}
	h.mu.Unlock()

	h.metrics.connected.Add(-1)
	for _, ch := range toUnsubPool {
		h.redisUnsubscribe(ch)
	}
	if toUnsubDM != "" {
		h.redisUnsubscribe(toUnsubDM)
	}
}

// joinRoom adds c to a pool room, subscribing the Redis channel on first join.
// Returns false if c was already a member.
func (h *Hub) joinRoom(c *conn, pool string) bool {
	var subscribe bool
	h.mu.Lock()
	room := h.poolRooms[pool]
	if room == nil {
		room = make(map[*conn]struct{})
		h.poolRooms[pool] = room
	}
	if _, ok := room[c]; ok {
		h.mu.Unlock()
		return false
	}
	room[c] = struct{}{}
	h.poolChanRefs[pool]++
	if h.poolChanRefs[pool] == 1 {
		subscribe = true
	}
	h.mu.Unlock()

	c.addPool(pool)
	if subscribe {
		h.redisSubscribe(h.namespace + poolChannelPrefix + pool)
	}
	return true
}

// leaveRoom removes c from a pool room, unsubscribing the Redis channel when the
// last local member leaves.
func (h *Hub) leaveRoom(c *conn, pool string) {
	var unsubscribe bool
	h.mu.Lock()
	if room := h.poolRooms[pool]; room != nil {
		if _, ok := room[c]; ok {
			delete(room, c)
			h.poolChanRefs[pool]--
			if h.poolChanRefs[pool] <= 0 {
				delete(h.poolChanRefs, pool)
				delete(h.poolRooms, pool)
				unsubscribe = true
			}
		}
	}
	h.mu.Unlock()

	c.removePool(pool)
	if unsubscribe {
		h.redisUnsubscribe(h.namespace + poolChannelPrefix + pool)
	}
}

// subscribeDMs marks c as a DM receiver and subscribes the wallet's Redis DM
// channel on first subscription for that wallet.
func (h *Hub) subscribeDMs(c *conn) {
	if c.isDMSubscribed() {
		return
	}
	c.setDMSubscribed()
	var subscribe bool
	h.mu.Lock()
	h.dmChanRefs[c.actor]++
	if h.dmChanRefs[c.actor] == 1 {
		subscribe = true
	}
	h.mu.Unlock()
	if subscribe {
		h.redisSubscribe(h.namespace + dmChannelPrefix + c.actor)
	}
}

func (h *Hub) redisSubscribe(channel string) {
	if err := h.ps.Subscribe(context.Background(), channel); err != nil {
		h.logErr("redis subscribe", err)
	}
}

func (h *Hub) redisUnsubscribe(channel string) {
	if err := h.ps.Unsubscribe(context.Background(), channel); err != nil {
		h.logErr("redis unsubscribe", err)
	}
}

// dispatch routes a Redis message to the correct local subscribers.
func (h *Hub) dispatch(channel string, payload []byte) {
	if h.durable != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		current, e := h.durable.CurrentEvent(ctx, payload)
		cancel()
		if e != nil {
			h.logErr("authoritative fanout projection", e)
			return
		}
		payload = current
	}
	if !strings.HasPrefix(channel, h.namespace) {
		return
	}
	channel = strings.TrimPrefix(channel, h.namespace)
	switch {
	case len(channel) > len(poolChannelPrefix) && channel[:len(poolChannelPrefix)] == poolChannelPrefix:
		pool := channel[len(poolChannelPrefix):]
		h.mu.RLock()
		room := h.poolRooms[pool]
		conns := make([]*conn, 0, len(room))
		for c := range room {
			conns = append(conns, c)
		}
		h.mu.RUnlock()
		for _, c := range conns {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			valid := c.principal.valid(ctx, h.clock())
			cancel()
			if valid {
				c.enqueue(payload)
			}
		}
	case len(channel) > len(dmChannelPrefix) && channel[:len(dmChannelPrefix)] == dmChannelPrefix:
		wallet := channel[len(dmChannelPrefix):]
		h.mu.RLock()
		set := h.walletConns[wallet]
		conns := make([]*conn, 0, len(set))
		for c := range set {
			conns = append(conns, c)
		}
		h.mu.RUnlock()
		for _, c := range conns {
			var kind struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(payload, &kind) != nil {
				continue
			}
			if c.isDMSubscribed() || kind.Type == "read_marker" {
				var event DurableMessage
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				allowed := json.Unmarshal(payload, &event) == nil && c.principal.valid(ctx, h.clock()) && h.durable.Authorized(WithPrincipal(ctx, c.principal), c.actor, event.ConversationID) == nil
				cancel()
				if !allowed {
					continue
				}
				c.enqueue(payload)
			}
		}
	}
}

func (h *Hub) logErr(op string, err error) {
	if h.log != nil {
		h.log.Error("fanout hub error", slog.String("op", op), slog.String("err", err.Error()))
	}
}

// ── rate limiting (per-actor sliding window via Redis) ─────────────────────────

func (h *Hub) allow(ctx context.Context, actor, kind string, max int) bool {
	key := h.namespace + "ratelimit:" + kind + ":" + actor + ":ws"
	n, err := h.pub.Incr(ctx, key).Result()
	if err != nil {
		// Reject writes when the global limit cannot be enforced.
		return false
	}
	if n == 1 {
		_ = h.pub.Expire(ctx, key, h.rateWindow).Err()
	}
	return n <= int64(max)
}

// marshal returns the JSON encoding of v, or nil on error (logged).
func (h *Hub) marshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		h.logErr("marshal", err)
		return nil
	}
	return b
}

// optInt8 builds a non-null pgtype.Int8.
func optInt8(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }

// textOrNull builds a pgtype.Text from an optional string (nil/"" => NULL).
func textOrNull(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	t := *s
	if t == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: t, Valid: true}
}

type Measurement struct {
	Connected          int64     `json:"connected"`
	Evicted            int64     `json:"evicted"`
	QueueFramesPeak    int64     `json:"queue_frames_peak"`
	QueueBytesPeak     int64     `json:"queue_bytes_peak"`
	QueueFramesCurrent int64     `json:"queue_frames_current"`
	QueueBytesCurrent  int64     `json:"queue_bytes_current"`
	EnqueuedFrames     int64     `json:"enqueued_frames"`
	EnqueuedBytes      int64     `json:"enqueued_bytes"`
	WrittenFrames      int64     `json:"written_frames"`
	WrittenBytes       int64     `json:"written_bytes"`
	ObservedAt         time.Time `json:"observed_at"`
}

func observeMaximum(value *atomic.Int64, incoming int64) {
	for {
		previous := value.Load()
		if previous >= incoming || value.CompareAndSwap(previous, incoming) {
			return
		}
	}
}
func (h *Hub) Measurement() Measurement {
	m := Measurement{Connected: h.metrics.connected.Load(), Evicted: h.metrics.evicted.Load(), QueueFramesPeak: h.metrics.queueFramesPeak.Load(), QueueBytesPeak: h.metrics.queueBytesPeak.Load(), EnqueuedFrames: h.metrics.enqueuedFrames.Load(), EnqueuedBytes: h.metrics.enqueuedBytes.Load(), WrittenFrames: h.metrics.writtenFrames.Load(), WrittenBytes: h.metrics.writtenBytes.Load(), ObservedAt: time.Now().UTC()}
	h.mu.RLock()
	conns := []*conn{}
	for _, set := range h.walletConns {
		for c := range set {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.queueMu.Lock()
		m.QueueFramesCurrent += int64(c.queuedFrames)
		m.QueueBytesCurrent += int64(c.queuedBytes)
		c.queueMu.Unlock()
	}
	return m
}
