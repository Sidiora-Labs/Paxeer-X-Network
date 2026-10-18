package ws_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/ws"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newServer(t *testing.T, opts ws.Options) (*httptest.Server, *broker.Broker) {
	t.Helper()
	if opts.Broker == nil {
		opts.Broker = broker.New(broker.Options{Logger: discard()})
	}
	if opts.Logger == nil {
		opts.Logger = discard()
	}
	if opts.Flush == 0 {
		opts.Flush = 20 * time.Millisecond
	}
	hub := ws.NewHub(opts)
	r := chi.NewRouter()
	hub.Register(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, opts.Broker
}

func dial(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	c, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func readFrame(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, b, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", string(b), err)
	}
	return m
}

// readUntil reads frames (skipping server keepalive pings) until one matches
// want, or fails after a bounded number of frames.
func readUntil(t *testing.T, c *websocket.Conn, wantType string) map[string]any {
	t.Helper()
	for i := 0; i < 10; i++ {
		m := readFrame(t, c)
		if m["type"] == wantType {
			return m
		}
		if m["type"] == "error" {
			t.Fatalf("got error frame waiting for %q: %v", wantType, m["message"])
		}
	}
	t.Fatalf("did not receive %q frame", wantType)
	return nil
}

func swapPayload(pool string) []byte {
	b, _ := json.Marshal(map[string]any{"poolAddress": pool, "blockTimestamp": 1})
	return b
}

func TestWS_ConnectedWelcome(t *testing.T) {
	srv, _ := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws")
	if m := readFrame(t, c); m["type"] != "connected" {
		t.Fatalf("first frame type = %v, want connected", m["type"])
	}
}

func TestWS_SubscribeAndReceiveFilteredByPool(t *testing.T) {
	srv, b := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws")
	readUntil(t, c, "connected")

	sub := map[string]any{"type": "subscribe", "channels": []string{constants.ChannelSwap}, "pools": []string{"0xAAA"}}
	if err := c.WriteJSON(sub); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	ack := readUntil(t, c, "subscribed")
	if pools, ok := ack["pools"].([]any); !ok || len(pools) != 1 {
		t.Fatalf("subscribed ack pools = %v", ack["pools"])
	}

	// Give the server a moment to apply the Resubscribe before publishing.
	time.Sleep(50 * time.Millisecond)
	b.Dispatch(constants.ChannelSwap, swapPayload("0xBBB")) // filtered out by pool
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA")) // delivered

	got := readUntil(t, c, "swap")
	if got["pool"] != "0xaaa" {
		t.Fatalf("received swap pool = %v, want 0xaaa (0xBBB should have been filtered)", got["pool"])
	}
}

func TestWS_Unsubscribe(t *testing.T) {
	srv, b := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws")
	readUntil(t, c, "connected")

	_ = c.WriteJSON(map[string]any{"type": "subscribe", "channels": []string{constants.ChannelSwap}})
	readUntil(t, c, "subscribed")
	_ = c.WriteJSON(map[string]any{"type": "unsubscribe", "channels": []string{constants.ChannelSwap}})
	ack := readUntil(t, c, "unsubscribed")
	if chs, ok := ack["channels"].([]any); !ok || len(chs) != 0 {
		t.Fatalf("after unsubscribe channels = %v, want empty", ack["channels"])
	}
	_ = b
}

func TestWS_Ping(t *testing.T) {
	srv, _ := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws")
	readUntil(t, c, "connected")

	_ = c.WriteJSON(map[string]any{"type": "ping"})
	readUntil(t, c, "pong")
}

func TestWS_CandlesEndpointParity(t *testing.T) {
	srv, b := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws/candles")
	readUntil(t, c, "connected")

	_ = c.WriteJSON(map[string]any{"type": "subscribe", "pools": []string{"0xAAA"}, "timeframes": []string{"1m"}})
	readUntil(t, c, "subscribed")

	time.Sleep(50 * time.Millisecond)
	candle, _ := json.Marshal(map[string]any{
		"poolAddress": "0xAAA", "timeframe": "1m", "candleStart": 60,
		"open": "1000000", "high": "2000000", "low": "500000", "close": "1500000",
		"volumeUsdl": "0", "volumeToken": "0", "buyVolumeUsdl": "0", "sellVolumeUsdl": "0",
		"tradeCount": 1, "uniqueTraders": 1, "largeTradeCount": 0,
		"mcapOpen": "0", "mcapHigh": "0", "mcapLow": "0", "mcapClose": "0",
	})
	b.Dispatch(constants.ChannelCandleUpdate, candle)

	got := readUntil(t, c, "candle_update")
	data, ok := got["data"].(map[string]any)
	if !ok || data["poolAddress"] != "0xAAA" {
		t.Fatalf("candle_update data = %v", got["data"])
	}
}

func TestWS_PerIPConnectionCap(t *testing.T) {
	srv, _ := newServer(t, ws.Options{MaxPerIP: 1})

	// First connection from 127.0.0.1 succeeds.
	c1 := dial(t, srv, "/ws")
	readUntil(t, c1, "connected")

	// Second from the same IP is rejected before the upgrade with 503.
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected second same-IP connection to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("rejection status = %v, want 503", resp)
	}
}

func TestWS_InvalidJSONFrame(t *testing.T) {
	srv, _ := newServer(t, ws.Options{})
	c := dial(t, srv, "/ws")
	readUntil(t, c, "connected")

	if err := c.WriteMessage(websocket.TextMessage, []byte("{not json")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readUntil(t, c, "error")
	if got["message"] != "Invalid JSON" {
		t.Fatalf("error message = %v", got["message"])
	}
}
