package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testSnapshotID = "lqz3a7-9f2b1c0d4e5f"

var epoch = time.UnixMilli(1747526400000)

// testClock drives Server.Now and the server ping ticker by hand.
type testClock struct {
	mu    sync.Mutex
	now   time.Time
	ticks chan time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Ticker(time.Duration) (<-chan time.Time, func()) { return c.ticks, func() {} }

func (c *testClock) set(at time.Duration) {
	c.mu.Lock()
	c.now = epoch.Add(at)
	c.mu.Unlock()
}

func (c *testClock) ms() int64 { return c.Now().UnixMilli() }

func (c *testClock) tick(t *testing.T, at time.Duration) {
	t.Helper()
	c.set(at)
	select {
	case c.ticks <- c.Now():
	case <-time.After(5 * time.Second):
		t.Fatalf("server ping loop did not take the tick at %v", at)
	}
}

type subscribeCall struct {
	sock  *Socket
	frame ClientFrame
}

type harness struct {
	clock    *testClock
	server   *Server
	url      string
	subs     chan subscribeCall
	unsubs   chan string
	closed   chan *Socket
	shedding atomic.Bool
}

func served(topic string) bool {
	switch topic {
	case "BTC@trade", "BTC@orderbook", "BTC@kline_1m":
		return true
	}
	return strings.HasPrefix(topic, "CAP") && strings.HasSuffix(topic, "@trade")
}

func newHarness(t *testing.T, shedding bool) *harness {
	t.Helper()
	srv, err := NewServer(testSnapshotID, served, DefaultLimits())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	h := &harness{
		clock:  &testClock{now: epoch, ticks: make(chan time.Time)},
		server: srv,
		subs:   make(chan subscribeCall, 1024),
		unsubs: make(chan string, 16),
		closed: make(chan *Socket, 4),
	}
	h.shedding.Store(shedding)
	srv.Now = h.clock.Now
	srv.Ticker = h.clock.Ticker
	srv.Shedding = h.shedding.Load
	srv.OnSubscribe = func(s *Socket, f ClientFrame) { h.subs <- subscribeCall{s, f} }
	srv.OnUnsubscribe = func(_ *Socket, topic string) { h.unsubs <- topic }
	srv.OnClose = func(s *Socket) { h.closed <- s }
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	h.url = "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	return h
}

// client reads on a background goroutine so control frames are handled as
// they arrive.
type client struct {
	conn   *websocket.Conn
	msgs   chan []byte
	err    chan error
	pinged chan struct{}
}

func (h *harness) dial(t *testing.T, answerPings bool) *client {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(h.url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", h.url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &client{conn: conn, msgs: make(chan []byte, 1024), err: make(chan error, 1), pinged: make(chan struct{}, 16)}
	conn.SetPingHandler(func(data string) error {
		var err error
		if answerPings {
			err = conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
		}
		c.pinged <- struct{}{}
		return err
	})
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				c.err <- err
				return
			}
			c.msgs <- msg
		}
	}()
	return c
}

func (c *client) send(t *testing.T, raw string) {
	t.Helper()
	if err := c.conn.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
		t.Fatalf("write %s: %v", raw, err)
	}
}

func (c *client) next(t *testing.T) []byte {
	t.Helper()
	select {
	case msg := <-c.msgs:
		return msg
	case err := <-c.err:
		t.Fatalf("socket ended while a frame was expected: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
	}
	return nil
}

func (c *client) control(t *testing.T) Control {
	t.Helper()
	raw := c.next(t)
	var got Control
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode control %s: %v", raw, err)
	}
	return got
}

func (c *client) closeError(t *testing.T) *websocket.CloseError {
	t.Helper()
	select {
	case msg := <-c.msgs:
		t.Fatalf("frame %s where a close was expected", msg)
	case err := <-c.err:
		var ce *websocket.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("read error %v is not a close error", err)
		}
		return ce
	case <-time.After(5 * time.Second):
		t.Fatal("socket not closed within 5s")
	}
	return nil
}

func keys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func wantKeys(t *testing.T, raw []byte, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := keys(t, raw); !slices.Equal(got, want) {
		t.Fatalf("frame %s has keys %v, want %v", raw, got, want)
	}
}

func wantControl(t *testing.T, got, want Control) {
	t.Helper()
	if got != want {
		t.Fatalf("control frame %+v, want %+v", got, want)
	}
}

// open dials and consumes the welcome frame.
func (h *harness) open(t *testing.T) *client {
	t.Helper()
	c := h.dial(t, true)
	wantControl(t, c.control(t), Welcome(h.clock.ms(), testSnapshotID))
	return c
}

func (h *harness) subscribed(t *testing.T) subscribeCall {
	t.Helper()
	select {
	case call := <-h.subs:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("OnSubscribe not called")
	}
	return subscribeCall{}
}

// stillOpen proves the socket answers a ping after an error frame.
func (h *harness) stillOpen(t *testing.T, c *client) {
	t.Helper()
	c.send(t, `{"event":"ping"}`)
	wantControl(t, c.control(t), Pong(h.clock.ms()))
}

func (h *harness) wantError(t *testing.T, c *client, raw string, code ErrorCode, topic string) {
	t.Helper()
	c.send(t, raw)
	frame := c.next(t)
	var got Control
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("decode %s: %v", frame, err)
	}
	wantControl(t, got, Control{Event: "error", Message: code, Topic: topic, TS: h.clock.ms()})
	if topic == "" {
		wantKeys(t, frame, "event", "message", "ts")
	} else {
		wantKeys(t, frame, "event", "message", "ts", "topic")
	}
	h.stillOpen(t, c)
}

// fill subscribes n distinct topics, 50 per test-clock second.
func (h *harness) fill(t *testing.T, c *client, n int, at time.Duration) time.Duration {
	t.Helper()
	h.clock.set(at)
	for i := range n {
		if i > 0 && i%50 == 0 {
			at += time.Second
			h.clock.set(at)
		}
		topic := "CAP" + strconv.Itoa(i) + "@trade"
		c.send(t, `{"event":"subscribe","topic":"`+topic+`"}`)
		wantControl(t, c.control(t), Subscribed(topic, h.clock.ms(), testSnapshotID))
		h.subscribed(t)
	}
	return at
}

func TestWSProtocol(t *testing.T) {
	t.Run("event_subscribe", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		c.send(t, `{"event":"subscribe","topic":"BTC@trade"}`)
		frame := c.next(t)
		wantKeys(t, frame, "event", "topic", "ts", "snapshotId")
		var got Control
		if err := json.Unmarshal(frame, &got); err != nil {
			t.Fatal(err)
		}
		wantControl(t, got, Subscribed("BTC@trade", h.clock.ms(), testSnapshotID))
		call := h.subscribed(t)
		if call.frame.Event != EventSubscribe || call.frame.Topic != "BTC@trade" || call.frame.Limit != nil {
			t.Fatalf("OnSubscribe frame %+v", call.frame)
		}
		if n := call.sock.Subscriptions(); n != 1 {
			t.Fatalf("Subscriptions() = %d, want 1", n)
		}

		c.send(t, `{"event":"subscribe","topic":"BTC@kline_1m","limit":500}`)
		wantControl(t, c.control(t), Subscribed("BTC@kline_1m", h.clock.ms(), testSnapshotID))
		call = h.subscribed(t)
		if call.frame.Limit == nil || *call.frame.Limit != 500 {
			t.Fatalf("OnSubscribe limit %v, want 500", call.frame.Limit)
		}
		h.wantError(t, c, `{"event":"subscribe","topic":"BTC@kline_1m","limit":-1}`, ErrInvalidJSON, "")
	})

	t.Run("event_unsubscribe", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		c.send(t, `{"event":"subscribe","topic":"BTC@trade"}`)
		c.control(t)
		call := h.subscribed(t)
		c.send(t, `{"event":"unsubscribe","topic":"BTC@trade"}`)
		frame := c.next(t)
		wantKeys(t, frame, "event", "topic", "ts")
		var got Control
		if err := json.Unmarshal(frame, &got); err != nil {
			t.Fatal(err)
		}
		wantControl(t, got, Unsubscribed("BTC@trade", h.clock.ms()))
		select {
		case topic := <-h.unsubs:
			if topic != "BTC@trade" {
				t.Fatalf("OnUnsubscribe topic %q", topic)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("OnUnsubscribe not called")
		}
		if n := call.sock.Subscriptions(); n != 0 {
			t.Fatalf("Subscriptions() = %d after unsubscribe, want 0", n)
		}
	})

	t.Run("event_ping", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.clock.set(1500 * time.Millisecond)
		c.send(t, `{"event":"ping"}`)
		frame := c.next(t)
		wantKeys(t, frame, "event", "ts")
		var got Control
		if err := json.Unmarshal(frame, &got); err != nil {
			t.Fatal(err)
		}
		wantControl(t, got, Control{Event: "pong", TS: epoch.UnixMilli() + 1500})
	})

	t.Run("error_invalid_json", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.wantError(t, c, `{not json`, ErrInvalidJSON, "")
		h.wantError(t, c, `{"event":7}`, ErrInvalidJSON, "")
	})

	t.Run("error_missing_topic", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.wantError(t, c, `{"event":"subscribe"}`, ErrMissingTopic, "")
		h.wantError(t, c, `{"event":"unsubscribe","topic":""}`, ErrMissingTopic, "")
	})

	t.Run("error_unknown_event", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.wantError(t, c, `{"event":"hello"}`, ErrUnknownEvent, "")
		h.wantError(t, c, `{}`, ErrUnknownEvent, "")
	})

	t.Run("error_unknown_topic", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.wantError(t, c, `{"event":"subscribe","topic":"nothing@else"}`, ErrUnknownTopic, "nothing@else")
		h.wantError(t, c, `{"event":"unsubscribe","topic":"nothing@else"}`, ErrUnknownTopic, "nothing@else")
	})

	t.Run("error_subscribe_rate_limit", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.fill(t, c, 50, 0)
		h.wantError(t, c, `{"event":"subscribe","topic":"CAP50@trade"}`, ErrSubscribeRateLimit, "CAP50@trade")
	})

	t.Run("error_too_many_subscriptions", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		at := h.fill(t, c, 200, 0)
		h.clock.set(at + time.Second)
		h.wantError(t, c, `{"event":"subscribe","topic":"CAP200@trade"}`, ErrTooManySubscriptions, "CAP200@trade")
	})

	t.Run("control_frames", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.dial(t, true)
		frame := c.next(t)
		wantKeys(t, frame, "event", "ts", "snapshotId")
		var welcome Control
		if err := json.Unmarshal(frame, &welcome); err != nil {
			t.Fatal(err)
		}
		wantControl(t, welcome, Control{Event: "welcome", TS: 1747526400000, SnapshotID: testSnapshotID})

		c.send(t, `{"event":"subscribe","topic":"BTC@orderbook"}`)
		wantControl(t, c.control(t), Control{Event: "subscribed", Topic: "BTC@orderbook", TS: 1747526400000, SnapshotID: testSnapshotID})
		call := h.subscribed(t)
		if err := call.sock.SendData(DataFrame{Topic: "BTC@orderbook", TS: h.clock.ms(), SnapshotID: testSnapshotID, Seq: 3, Snapshot: true, Data: json.RawMessage(`{"bids":[],"asks":[]}`)}); err != nil {
			t.Fatal(err)
		}
		var data DataFrame
		if err := json.Unmarshal(c.next(t), &data); err != nil {
			t.Fatal(err)
		}
		if data.SnapshotID != welcome.SnapshotID {
			t.Fatalf("data snapshotId %q differs from welcome %q", data.SnapshotID, welcome.SnapshotID)
		}
		c.send(t, `{"event":"unsubscribe","topic":"BTC@orderbook"}`)
		wantControl(t, c.control(t), Control{Event: "unsubscribed", Topic: "BTC@orderbook", TS: 1747526400000})
		c.send(t, `{"event":"ping"}`)
		wantControl(t, c.control(t), Control{Event: "pong", TS: 1747526400000})
	})

	t.Run("data_envelope", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		c.send(t, `{"event":"subscribe","topic":"BTC@trade"}`)
		c.control(t)
		sock := h.subscribed(t).sock

		if err := sock.SendData(DataFrame{Topic: "BTC@trade", TS: h.clock.ms(), SnapshotID: testSnapshotID, Seq: 41, Snapshot: true, Data: json.RawMessage(`[]`)}); err != nil {
			t.Fatal(err)
		}
		snap := c.next(t)
		wantKeys(t, snap, "topic", "ts", "snapshotId", "seq", "snapshot", "data")
		if want := `{"topic":"BTC@trade","ts":1747526400000,"snapshotId":"lqz3a7-9f2b1c0d4e5f","seq":41,"snapshot":true,"data":[]}`; string(snap) != want+"\n" {
			t.Fatalf("snapshot frame %s, want %s", snap, want)
		}

		live := json.RawMessage(`{"px":"64250.5","sz":"0.01","side":"buy"}`)
		if err := sock.SendData(DataFrame{Topic: "BTC@trade", TS: h.clock.ms(), SnapshotID: testSnapshotID, Seq: 42, Data: live}); err != nil {
			t.Fatal(err)
		}
		frame := c.next(t)
		wantKeys(t, frame, "topic", "ts", "snapshotId", "seq", "data")
		if want := `{"topic":"BTC@trade","ts":1747526400000,"snapshotId":"lqz3a7-9f2b1c0d4e5f","seq":42,"data":{"px":"64250.5","sz":"0.01","side":"buy"}}`; string(frame) != want+"\n" {
			t.Fatalf("live frame %s, want %s", frame, want)
		}
	})

	t.Run("subscription_cap", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		c.send(t, `{"event":"subscribe","topic":"CAP0@trade"}`)
		c.control(t)
		sock := h.subscribed(t).sock
		c.send(t, `{"event":"unsubscribe","topic":"CAP0@trade"}`)
		c.control(t)
		<-h.unsubs

		at := h.fill(t, c, 200, time.Second)
		if n := sock.Subscriptions(); n != 200 {
			t.Fatalf("Subscriptions() = %d, want 200", n)
		}
		at += time.Second
		h.clock.set(at)
		h.wantError(t, c, `{"event":"subscribe","topic":"CAP200@trade"}`, ErrTooManySubscriptions, "CAP200@trade")
		if n := sock.Subscriptions(); n != 200 {
			t.Fatalf("Subscriptions() = %d after the refused subscribe, want 200", n)
		}

		c.send(t, `{"event":"subscribe","topic":"CAP7@trade"}`)
		wantControl(t, c.control(t), Subscribed("CAP7@trade", h.clock.ms(), testSnapshotID))
		h.subscribed(t)

		c.send(t, `{"event":"unsubscribe","topic":"CAP0@trade"}`)
		wantControl(t, c.control(t), Unsubscribed("CAP0@trade", h.clock.ms()))
		<-h.unsubs
		c.send(t, `{"event":"subscribe","topic":"CAP200@trade"}`)
		wantControl(t, c.control(t), Subscribed("CAP200@trade", h.clock.ms(), testSnapshotID))
		h.subscribed(t)
		if n := sock.Subscriptions(); n != 200 {
			t.Fatalf("Subscriptions() = %d after swapping a topic, want 200", n)
		}
	})

	t.Run("subscribe_rate", func(t *testing.T) {
		h := newHarness(t, false)
		c := h.open(t)
		h.fill(t, c, 50, 0)
		h.clock.set(999 * time.Millisecond)
		h.wantError(t, c, `{"event":"subscribe","topic":"CAP50@trade"}`, ErrSubscribeRateLimit, "CAP50@trade")
		c.send(t, `{"event":"unsubscribe","topic":"CAP0@trade"}`)
		wantControl(t, c.control(t), Unsubscribed("CAP0@trade", h.clock.ms()))
		h.wantError(t, c, `{"event":"subscribe","topic":"CAP0@trade"}`, ErrSubscribeRateLimit, "CAP0@trade")
		h.clock.set(time.Second)
		c.send(t, `{"event":"subscribe","topic":"CAP50@trade"}`)
		wantControl(t, c.control(t), Subscribed("CAP50@trade", h.clock.ms(), testSnapshotID))
		c.send(t, `{"event":"subscribe","topic":"CAP0@trade"}`)
		wantControl(t, c.control(t), Subscribed("CAP0@trade", h.clock.ms(), testSnapshotID))
	})

	t.Run("server_ping", func(t *testing.T) {
		silent := newHarness(t, false)
		c := silent.dial(t, false)
		c.control(t)
		silent.clock.tick(t, 30*time.Second)
		<-c.pinged
		silent.stillOpen(t, c)
		silent.clock.tick(t, 60*time.Second)
		silent.clock.set(61 * time.Second)
		if ce := c.closeError(t); ce.Code != websocket.CloseAbnormalClosure {
			t.Fatalf("close code %d, want %d", ce.Code, websocket.CloseAbnormalClosure)
		}
		select {
		case <-silent.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("OnClose not called for the silent socket")
		}

		live := newHarness(t, false)
		a := live.open(t)
		live.clock.tick(t, 30*time.Second)
		<-a.pinged
		live.stillOpen(t, a)
		live.clock.tick(t, 60*time.Second)
		<-a.pinged
		live.clock.set(61 * time.Second)
		live.stillOpen(t, a)
	})

	t.Run("close_1013", func(t *testing.T) {
		reason := regexp.MustCompile(`^retry-after=([0-9]+)$`)
		appHint := regexp.MustCompile(`(?i)retry[-_ ]?after[=: ]+(\d+)`)
		h := newHarness(t, true)
		for i := range 40 {
			c := h.dial(t, true)
			ce := c.closeError(t)
			if ce.Code != websocket.CloseTryAgainLater {
				t.Fatalf("socket %d close code %d, want 1013", i, ce.Code)
			}
			m := reason.FindStringSubmatch(ce.Text)
			if m == nil || appHint.FindStringSubmatch(ce.Text)[1] != m[1] {
				t.Fatalf("socket %d close reason %q", i, ce.Text)
			}
			if n, _ := strconv.Atoi(m[1]); n < 5 || n > 30 {
				t.Fatalf("socket %d retry-after %d outside 5..30", i, n)
			}
		}

		h.shedding.Store(false)
		c := h.open(t)
		c.send(t, `{"event":"subscribe","topic":"BTC@trade"}`)
		c.control(t)
		if err := h.subscribed(t).sock.CloseRetryAfter(); err != nil {
			t.Fatalf("CloseRetryAfter: %v", err)
		}
		ce := c.closeError(t)
		if ce.Code != websocket.CloseTryAgainLater || !reason.MatchString(ce.Text) {
			t.Fatalf("open socket closed with %d %q", ce.Code, ce.Text)
		}
	})

	t.Run("new_server_refuses_bad_config", func(t *testing.T) {
		for _, tc := range []struct {
			id     string
			served func(string) bool
			limits Limits
		}{
			{"", served, DefaultLimits()},
			{testSnapshotID, nil, DefaultLimits()},
			{testSnapshotID, served, Limits{MaxSubscriptions: 0, SubscribesPerSecond: 50, PingInterval: time.Second}},
			{testSnapshotID, served, Limits{MaxSubscriptions: 200, SubscribesPerSecond: 0, PingInterval: time.Second}},
			{testSnapshotID, served, Limits{MaxSubscriptions: 200, SubscribesPerSecond: 50}},
		} {
			if srv, err := NewServer(tc.id, tc.served, tc.limits); err == nil || srv != nil {
				t.Fatalf("NewServer(%q, %v) = %v, %v; want refusal", tc.id, fmt.Sprint(tc.limits), srv, err)
			}
		}
		if got := DefaultLimits(); got != (Limits{MaxSubscriptions: 200, SubscribesPerSecond: 50, PingInterval: 30 * time.Second}) {
			t.Fatalf("DefaultLimits() = %+v", got)
		}
	})
}
