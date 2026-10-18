package app_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// readClose reads until the server's close frame and returns it. A TCP drop
// without a close frame surfaces as 1006 (CloseAbnormalClosure).
func readClose(t *testing.T, conn *websocket.Conn) *websocket.CloseError {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err == nil {
			t.Logf("frame before close: %s", raw)
			continue
		}
		var closed *websocket.CloseError
		if !errors.As(err, &closed) {
			t.Fatalf("connection ended without a close frame: %v", err)
		}
		return closed
	}
}

// /v2/stream ends a subscription it cannot resume (no afterCursor) with
// subscribed, resnapshot_required and then a 1000 close frame naming the
// reason; an authentication failure ends with error and a 1008 close frame.
func TestDevelopmentStreamTerminalFramesCarryCloseCode(t *testing.T) {
	f := newDstFixture(t)
	// A registered pool without projected frames: the subscription ends with
	// resnapshot_required (an unregistered pool ends with NOT_FOUND instead).
	f.createPool(t)
	subscribe := func(conn *websocket.Conn) {
		raw, _ := json.Marshal(map[string]any{"op": "subscribe", "subscriptionId": "token-page", "topics": f.topics, "afterCursor": nil})
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
	}

	conn, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	subscribe(conn)
	if c := readControl(t, conn); c["op"] != "subscribed" {
		t.Fatalf("first control %v", c)
	}
	if c := readControl(t, conn); c["op"] != "resnapshot_required" || c["code"] != "SNAPSHOT_REQUIRED" {
		t.Fatalf("second control %v", c)
	}
	if closed := readClose(t, conn); closed.Code != websocket.CloseNormalClosure || closed.Text != "resnapshot_required" {
		t.Fatalf("close = %d %q, want 1000 resnapshot_required", closed.Code, closed.Text)
	}

	unknown, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2", "ticket." + strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Close()
	subscribe(unknown)
	if c := readControl(t, unknown); c["op"] != "error" || c["code"] != "AUTH_REQUIRED" {
		t.Fatalf("unknown ticket control %v", c)
	}
	if closed := readClose(t, unknown); closed.Code != websocket.ClosePolicyViolation || closed.Text != "AUTH_REQUIRED" {
		t.Fatalf("close = %d %q, want 1008 AUTH_REQUIRED", closed.Code, closed.Text)
	}

	malformed, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2"})
	if err != nil {
		t.Fatal(err)
	}
	defer malformed.Close()
	if err := malformed.WriteMessage(websocket.TextMessage, []byte(`{"op":"subscribe"}`)); err != nil {
		t.Fatal(err)
	}
	if closed := readClose(t, malformed); closed.Code != websocket.ClosePolicyViolation || closed.Text != "FIELD_INVALID" {
		t.Fatalf("close = %d %q, want 1008 FIELD_INVALID", closed.Code, closed.Text)
	}
}
