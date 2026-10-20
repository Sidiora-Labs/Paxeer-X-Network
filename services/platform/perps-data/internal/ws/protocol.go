// Package ws serves the shared WebSocket topic protocol of the perps data
// services (contracts/data-services.md section 8): the client frame codec,
// the control and data frames, the error codes and the per-socket limits.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Event is the event of a client frame.
type Event string

const (
	EventSubscribe   Event = "subscribe"
	EventUnsubscribe Event = "unsubscribe"
	EventPing        Event = "ping"
)

// ErrorCode is the message of an error frame. Every code leaves the socket
// open.
type ErrorCode string

const (
	ErrInvalidJSON          ErrorCode = "invalid_json"
	ErrMissingTopic         ErrorCode = "missing_topic"
	ErrUnknownEvent         ErrorCode = "unknown_event"
	ErrUnknownTopic         ErrorCode = "unknown_topic"
	ErrSubscribeRateLimit   ErrorCode = "subscribe_rate_limit"
	ErrTooManySubscriptions ErrorCode = "too_many_subscriptions"
)

func (c ErrorCode) Error() string { return string(c) }

// ClientFrame is a decoded client frame. Limit is the optional snapshot size
// of a subscribe.
type ClientFrame struct {
	Event Event   `json:"event"`
	Topic string  `json:"topic,omitempty"`
	Limit *uint32 `json:"limit,omitempty"`
}

// ParseClientFrame decodes raw and checks it against the served topic set.
// A non-nil error is always an ErrorCode; the returned frame then still
// carries the topic that was read so the error frame can name it.
func ParseClientFrame(raw []byte, served func(topic string) bool) (ClientFrame, error) {
	var f ClientFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return ClientFrame{}, ErrInvalidJSON
	}
	switch f.Event {
	case EventPing:
		return ClientFrame{Event: EventPing}, nil
	case EventSubscribe:
	case EventUnsubscribe:
		f.Limit = nil
	default:
		return ClientFrame{}, ErrUnknownEvent
	}
	if f.Topic == "" {
		return ClientFrame{Event: f.Event}, ErrMissingTopic
	}
	if !served(f.Topic) {
		return f, ErrUnknownTopic
	}
	return f, nil
}

const (
	controlWelcome      = "welcome"
	controlSubscribed   = "subscribed"
	controlUnsubscribed = "unsubscribed"
	controlPong         = "pong"
	controlError        = "error"
)

// Control is a server control frame; ts is milliseconds since the epoch.
type Control struct {
	Event      string    `json:"event"`
	Topic      string    `json:"topic,omitempty"`
	Message    ErrorCode `json:"message,omitempty"`
	TS         int64     `json:"ts"`
	SnapshotID string    `json:"snapshotId,omitempty"`
}

// Welcome is the first frame of every socket.
func Welcome(ts int64, snapshotID string) Control {
	return Control{Event: controlWelcome, TS: ts, SnapshotID: snapshotID}
}

// Subscribed acknowledges a subscribe to topic.
func Subscribed(topic string, ts int64, snapshotID string) Control {
	return Control{Event: controlSubscribed, Topic: topic, TS: ts, SnapshotID: snapshotID}
}

// Unsubscribed acknowledges an unsubscribe from topic.
func Unsubscribed(topic string, ts int64) Control {
	return Control{Event: controlUnsubscribed, Topic: topic, TS: ts}
}

// Pong answers a client ping.
func Pong(ts int64) Control {
	return Control{Event: controlPong, TS: ts}
}

// Error reports code, naming topic when it is not empty.
func Error(code ErrorCode, topic string, ts int64) Control {
	return Control{Event: controlError, Message: code, Topic: topic, TS: ts}
}

// DataFrame is the envelope of every topic frame. Snapshot is set only on
// the snapshot frame that follows a subscribe and is omitted on live frames.
type DataFrame struct {
	Topic      string          `json:"topic"`
	TS         int64           `json:"ts"`
	SnapshotID string          `json:"snapshotId"`
	Seq        uint64          `json:"seq"`
	Snapshot   bool            `json:"snapshot,omitempty"`
	Data       json.RawMessage `json:"data"`
}

// Limits bounds one socket.
type Limits struct {
	MaxSubscriptions    int
	SubscribesPerSecond int
	PingInterval        time.Duration
}

// DefaultLimits returns 200 subscriptions, 50 subscribe operations per second
// and a 30 second server ping.
func DefaultLimits() Limits {
	return Limits{MaxSubscriptions: 200, SubscribesPerSecond: 50, PingInterval: 30 * time.Second}
}

const (
	maxClientFrameBytes = 4096
	writeWait           = 10 * time.Second
	// sendQueue bounds the frames queued for one socket; a socket that
	// falls this far behind is closed.
	sendQueue     = 1024
	retryAfterMin = 5
	retryAfterMax = 30
)

// Server upgrades requests to protocol sockets. The hooks run on the reading
// goroutine of the socket: OnOpen after the welcome frame is written, and
// every topic it returns is subscribed as a client subscribe would be;
// OnSubscribe after the subscribed frame is written, OnUnsubscribe after the
// unsubscribed frame, OnClose once the socket ends.
type Server struct {
	OnOpen        func(*http.Request) []string
	OnSubscribe   func(*Socket, ClientFrame)
	OnUnsubscribe func(*Socket, string)
	OnClose       func(*Socket)
	// Shedding reports load shedding; a shedding server closes every new
	// socket with 1013 and a retry-after reason.
	Shedding func() bool
	Now      func() time.Time
	Ticker   func(time.Duration) (<-chan time.Time, func())

	snapshotID string
	served     func(string) bool
	limits     Limits
	upgrader   websocket.Upgrader
}

// NewServer builds a Server for the process snapshotID serving the topics
// accepted by served.
func NewServer(snapshotID string, served func(topic string) bool, limits Limits) (*Server, error) {
	switch {
	case snapshotID == "":
		return nil, errors.New("ws: empty snapshotId")
	case served == nil:
		return nil, errors.New("ws: nil served topic set")
	case limits.MaxSubscriptions <= 0, limits.SubscribesPerSecond <= 0, limits.PingInterval <= 0:
		return nil, fmt.Errorf("ws: non-positive limits %+v", limits)
	}
	return &Server{
		Now:        time.Now,
		Ticker:     realTicker,
		snapshotID: snapshotID,
		served:     served,
		limits:     limits,
		// The protocol is public, read-only and unauthenticated, so any
		// origin may connect.
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}, nil
}

func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// ServeHTTP runs one socket until the client leaves, fails a write or misses
// two ping intervals.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	sock := &Socket{conn: conn, server: s, subs: make(map[string]struct{}), out: make(chan outFrame, sendQueue), stop: make(chan struct{})}
	if s.Shedding != nil && s.Shedding() {
		_ = sock.CloseRetryAfter()
		return
	}
	var writer sync.WaitGroup
	writer.Go(sock.writeLoop)
	defer func() {
		close(sock.stop)
		writer.Wait()
	}()
	conn.SetReadLimit(maxClientFrameBytes)
	sock.lastPong.Store(s.Now().UnixNano())
	conn.SetPongHandler(func(string) error {
		sock.lastPong.Store(s.Now().UnixNano())
		return nil
	})
	if err := sock.write(Welcome(s.Now().UnixMilli(), s.snapshotID)); err != nil {
		_ = conn.Close()
		return
	}
	if s.OnOpen != nil {
		for _, topic := range s.OnOpen(r) {
			now := s.Now()
			if err := sock.subscribe(ClientFrame{Event: EventSubscribe, Topic: topic}, now, now.UnixMilli()); err != nil {
				_ = conn.Close()
				if s.OnClose != nil {
					s.OnClose(sock)
				}
				return
			}
		}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sock.keepalive(done)
	}()
	sock.readLoop()
	close(done)
	_ = conn.Close()
	wg.Wait()
	if s.OnClose != nil {
		s.OnClose(sock)
	}
}

// Socket is one client connection.
type Socket struct {
	conn     *websocket.Conn
	server   *Server
	writeMu  sync.Mutex
	lastPong atomic.Int64
	// out queues every frame of a served socket in write order; writeLoop
	// writes them until stop is closed.
	out  chan outFrame
	stop chan struct{}

	subsMu sync.Mutex
	subs   map[string]struct{}

	recent []time.Time
	next   int
}

// SendData writes one data frame.
func (s *Socket) SendData(f DataFrame) error { return s.write(f) }

// PrepareData encodes f once, in the bytes SendData writes, for many sockets.
func PrepareData(f DataFrame) (*websocket.PreparedMessage, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return websocket.NewPreparedMessage(websocket.TextMessage, append(b, '\n'))
}

// outFrame is one queued frame: a frame encoded by PrepareData or a value
// written as JSON. done, when set, receives the result of the write.
type outFrame struct {
	prepared *websocket.PreparedMessage
	value    any
	done     chan error
}

// errSocketClosed reports a write to a socket whose writer has stopped.
var errSocketClosed = errors.New("ws: socket closed")

// queue appends f to the send queue of s without waiting; it reports false
// when the socket is closed or its queue is full.
func (s *Socket) queue(f outFrame) bool {
	select {
	case <-s.stop:
		return false
	default:
	}
	select {
	case s.out <- f:
		return true
	default:
		return false
	}
}

// writeLoop writes the queued frames in order; a failed write closes the
// connection, which ends the read loop and the socket.
func (s *Socket) writeLoop() {
	for {
		select {
		case <-s.stop:
			return
		case f := <-s.out:
			var err error
			if f.prepared != nil {
				err = s.direct(func() error { return s.conn.WritePreparedMessage(f.prepared) })
			} else {
				err = s.direct(func() error { return s.conn.WriteJSON(f.value) })
			}
			if f.done != nil {
				f.done <- err
			}
			if err != nil {
				_ = s.conn.Close()
			}
		}
	}
}

// Subscriptions returns the number of topics the socket holds.
func (s *Socket) Subscriptions() int {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	return len(s.subs)
}

// CloseRetryAfter closes the socket with 1013 and the reason
// retry-after=<seconds>, jittered between 5 and 30 seconds.
func (s *Socket) CloseRetryAfter() error {
	secs := retryAfterMin + rand.IntN(retryAfterMax-retryAfterMin+1)
	msg := websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "retry-after="+strconv.Itoa(secs))
	werr := s.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(writeWait))
	return errors.Join(werr, s.conn.Close())
}

// closeLater closes s as CloseRetryAfter does on a goroutine of its own, so a
// socket that stalls the close frame never blocks the caller.
func (s *Socket) closeLater() {
	go func() { _ = s.CloseRetryAfter() }()
}

// write writes v and returns the result; on a served socket it goes through
// the send queue, behind the frames queued before it.
func (s *Socket) write(v any) error {
	if s.out == nil {
		return s.direct(func() error { return s.conn.WriteJSON(v) })
	}
	f := outFrame{value: v, done: make(chan error, 1)}
	select {
	case s.out <- f:
	case <-s.stop:
		return errSocketClosed
	}
	select {
	case err := <-f.done:
		return err
	case <-s.stop:
		return errSocketClosed
	}
}

func (s *Socket) direct(write func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return write()
}

func (s *Socket) keepalive(done <-chan struct{}) {
	interval := s.server.limits.PingInterval
	ticks, stop := s.server.Ticker(interval)
	defer stop()
	for {
		select {
		case <-done:
			return
		case <-ticks:
			idle := s.server.Now().Sub(time.Unix(0, s.lastPong.Load()))
			if idle >= 2*interval {
				_ = s.conn.Close()
				return
			}
			if err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				_ = s.conn.Close()
				return
			}
		}
	}
}

func (s *Socket) readLoop() {
	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if err := s.handle(raw); err != nil {
			return
		}
	}
}

func (s *Socket) handle(raw []byte) error {
	now := s.server.Now()
	ts := now.UnixMilli()
	f, err := ParseClientFrame(raw, s.server.served)
	if err != nil {
		var code ErrorCode
		if !errors.As(err, &code) {
			return err
		}
		return s.write(Error(code, f.Topic, ts))
	}
	switch f.Event {
	case EventPing:
		return s.write(Pong(ts))
	case EventUnsubscribe:
		s.subsMu.Lock()
		delete(s.subs, f.Topic)
		s.subsMu.Unlock()
		if err := s.write(Unsubscribed(f.Topic, ts)); err != nil {
			return err
		}
		if s.server.OnUnsubscribe != nil {
			s.server.OnUnsubscribe(s, f.Topic)
		}
		return nil
	default:
		return s.subscribe(f, now, ts)
	}
}

// subscribe admits the subscribe f under the rate and subscription limits and
// acknowledges it.
func (s *Socket) subscribe(f ClientFrame, now time.Time, ts int64) error {
	if !s.allowSubscribe(now) {
		return s.write(Error(ErrSubscribeRateLimit, f.Topic, ts))
	}
	s.subsMu.Lock()
	_, held := s.subs[f.Topic]
	full := !held && len(s.subs) >= s.server.limits.MaxSubscriptions
	if !full {
		s.subs[f.Topic] = struct{}{}
	}
	s.subsMu.Unlock()
	if full {
		return s.write(Error(ErrTooManySubscriptions, f.Topic, ts))
	}
	if err := s.write(Subscribed(f.Topic, ts, s.server.snapshotID)); err != nil {
		return err
	}
	if s.server.OnSubscribe != nil {
		s.server.OnSubscribe(s, f)
	}
	return nil
}

// allowSubscribe admits a subscribe operation when fewer than
// SubscribesPerSecond were admitted within the last second; recent is a ring
// of the admitted times and next indexes its oldest entry once full.
func (s *Socket) allowSubscribe(now time.Time) bool {
	n := s.server.limits.SubscribesPerSecond
	if len(s.recent) < n {
		s.recent = append(s.recent, now)
		return true
	}
	if now.Sub(s.recent[s.next]) < time.Second {
		return false
	}
	s.recent[s.next] = now
	s.next = (s.next + 1) % n
	return true
}
