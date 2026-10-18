package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
)

// echoUpstream is a fake media/social WS hub: it records the actor header from
// the handshake and echoes every message back, prefixed, so the tunnel's
// bidirectional copy is observable.
func echoUpstream(t *testing.T, gotActor *string) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		*gotActor = r.Header.Get("X-Actor-Wallet")
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(mt, append([]byte("echo:"), msg...)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(httpURL string) string { return "ws" + strings.TrimPrefix(httpURL, "http") }

func TestWS_TunnelsAndInjectsActor(t *testing.T) {
	var gotActor string
	up := echoUpstream(t, &gotActor)

	ws, err := NewWS(WSDeps{TargetBaseURL: wsURL(up.URL)})
	if err != nil {
		t.Fatalf("NewWS: %v", err)
	}

	// The gateway endpoint injects the authenticated actor, as RequireSession
	// would in production.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(), "0xfeed")))
	}))
	t.Cleanup(gw.Close)

	c, _, err := websocket.DefaultDialer.Dial(wsURL(gw.URL)+"/social/ws", nil)
	if err != nil {
		t.Fatalf("dial gateway ws: %v", err)
	}
	defer c.Close()

	if err := c.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "echo:hi" {
		t.Errorf("got %q, want echo:hi", msg)
	}
	if gotActor != "0xfeed" {
		t.Errorf("upstream actor = %q, want 0xfeed", gotActor)
	}
}

func TestWS_RejectsWithoutSession(t *testing.T) {
	var gotActor string
	up := echoUpstream(t, &gotActor)
	ws, err := NewWS(WSDeps{TargetBaseURL: wsURL(up.URL)})
	if err != nil {
		t.Fatalf("NewWS: %v", err)
	}
	// No actor injected -> handler must 401 before any upgrade.
	gw := httptest.NewServer(ws)
	t.Cleanup(gw.Close)

	resp, err := http.Get(gw.URL + "/social/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWS_BadGatewayWhenUpstreamDown(t *testing.T) {
	up := echoUpstream(t, new(string))
	addr := up.URL
	up.Close()

	ws, err := NewWS(WSDeps{TargetBaseURL: wsURL(addr), DialTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewWS: %v", err)
	}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(), "0xfeed")))
	}))
	t.Cleanup(gw.Close)

	resp, err := http.Get(gw.URL + "/social/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}
