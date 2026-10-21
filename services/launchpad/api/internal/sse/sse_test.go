package sse_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/sse"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newServer(t *testing.T, opts sse.Options) (*httptest.Server, *broker.Broker) {
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
	hub := sse.NewHub(opts)
	r := chi.NewRouter()
	hub.Register(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, opts.Broker
}

// openStream issues a streaming GET and returns a line reader over the body.
func openStream(t *testing.T, srv *httptest.Server, path string) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		cancel()
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	t.Cleanup(func() {
		cancel()
		_ = resp.Body.Close()
	})
	return bufio.NewReader(resp.Body)
}

// readNextData reads lines until a "data:" SSE frame arrives, returning the
// parsed JSON envelope. Comment/heartbeat lines are skipped.
func readNextData(t *testing.T, br *bufio.Reader) map[string]any {
	t.Helper()
	type result struct {
		m   map[string]any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				ch <- result{nil, err}
				return
			}
			line = strings.TrimRight(line, "\n")
			if strings.HasPrefix(line, "data: ") {
				var m map[string]any
				e := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m)
				ch <- result{m, e}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read stream: %v", r.err)
		}
		return r.m
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE data frame")
		return nil
	}
}

func swapPayload(pool string) []byte {
	b, _ := json.Marshal(map[string]any{"poolAddress": pool, "blockTimestamp": 1})
	return b
}

func TestSSE_StreamsSubscribedChannelFilteredByPool(t *testing.T) {
	srv, b := newServer(t, sse.Options{})
	br := openStream(t, srv, "/stream?channels="+constants.ChannelSwap+"&pools=0xAAA")

	// Let the subscription register before publishing.
	time.Sleep(50 * time.Millisecond)
	b.Dispatch(constants.ChannelSwap, swapPayload("0xBBB")) // filtered out
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA")) // delivered

	got := readNextData(t, br)
	if got["type"] != "swap" || got["pool"] != "0xaaa" {
		t.Fatalf("frame = %v, want swap/0xaaa", got)
	}
}

func TestSSE_WildcardChannels(t *testing.T) {
	srv, b := newServer(t, sse.Options{})
	br := openStream(t, srv, "/stream?channels=*")

	time.Sleep(50 * time.Millisecond)
	b.Dispatch(constants.ChannelMarketCreated, swapPayload("0xCCC"))

	got := readNextData(t, br)
	if got["type"] != "market_created" {
		t.Fatalf("frame type = %v, want market_created", got["type"])
	}
}

func TestSSE_CandlesEndpoint(t *testing.T) {
	srv, b := newServer(t, sse.Options{})
	br := openStream(t, srv, "/stream/candles?pools=0xAAA")

	time.Sleep(50 * time.Millisecond)
	candle, _ := json.Marshal(map[string]any{
		"poolAddress": "0xAAA", "timeframe": "1m", "candleStart": 60,
		"open": "1000000", "high": "2000000", "low": "500000", "close": "1500000",
		"volumeUsdl": "0", "volumeToken": "0", "buyVolumeUsdl": "0", "sellVolumeUsdl": "0",
		"tradeCount": 1, "uniqueTraders": 1, "largeTradeCount": 0,
		"mcapOpen": "0", "mcapHigh": "0", "mcapLow": "0", "mcapClose": "0",
	})
	b.Dispatch(constants.ChannelCandleUpdate, candle)

	got := readNextData(t, br)
	if got["type"] != "candle_update" {
		t.Fatalf("frame type = %v, want candle_update", got["type"])
	}
}

func TestSSE_PerIPConnectionCap(t *testing.T) {
	srv, _ := newServer(t, sse.Options{MaxPerIP: 1})

	// Hold one stream open.
	_ = openStream(t, srv, "/stream?channels=*")

	// Second from same IP must be rejected with 503.
	resp, err := http.Get(srv.URL + "/stream?channels=*")
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second stream status = %d, want 503", resp.StatusCode)
	}
}

func TestSSE_OpensWithComment(t *testing.T) {
	srv, _ := newServer(t, sse.Options{})
	br := openStream(t, srv, "/stream?channels="+constants.ChannelSwap)

	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read opener: %v", err)
	}
	if !strings.HasPrefix(line, ":") {
		t.Fatalf("opener = %q, want a leading SSE comment", line)
	}
}
