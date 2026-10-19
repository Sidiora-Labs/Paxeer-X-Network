package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_Get_UnwrapsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":-1,"result":{"sync_info":{"latest_block_height":"42"}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	raw, err := c.Get(context.Background(), "/status")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	got := string(raw)
	if !strings.Contains(got, "latest_block_height") {
		t.Errorf("expected unwrapped result containing latest_block_height, got %s", got)
	}
	if strings.Contains(got, "jsonrpc") {
		t.Error("result should not contain the JSON-RPC envelope")
	}
}

func TestClient_Get_FlatJSON_SeidFormat(t *testing.T) {
	// seid returns flat JSON without the JSON-RPC envelope.
	flat := `{"node_info":{"id":"abc123"},"sync_info":{"latest_block_height":"42","catching_up":false}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(flat))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	raw, err := c.Get(context.Background(), "/status")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	got := string(raw)
	if !strings.Contains(got, "abc123") {
		t.Errorf("expected flat result containing node ID, got %s", got)
	}
	if !strings.Contains(got, "latest_block_height") {
		t.Errorf("expected flat result containing latest_block_height, got %s", got)
	}
}

func TestClient_Get_FlatJSON_WithResultKey(t *testing.T) {
	// A flat response that happens to contain a "result" data key must NOT
	// be mistaken for a JSON-RPC envelope.
	flat := `{"result":{"code":0,"log":"ok"},"hash":"ABC123"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(flat))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	raw, err := c.Get(context.Background(), "/tx")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	got := string(raw)
	// Should return the full body, not just the inner "result" value.
	if !strings.Contains(got, "hash") {
		t.Errorf("expected full flat body with hash field, got %s", got)
	}
	if !strings.Contains(got, "ABC123") {
		t.Errorf("expected full flat body with hash value, got %s", got)
	}
}

func TestClient_Get_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Get(context.Background(), "/status")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("expected HTTP 502 in error, got: %v", err)
	}
}

func TestClient_Get_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Get(context.Background(), "/status")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestClient_GetRaw_ReturnsFullBody(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":-1,"result":{"txs_results":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	raw, err := c.GetRaw(context.Background(), "/block_results?height=1")
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if string(raw) != body {
		t.Errorf("expected full body %q, got %q", body, string(raw))
	}
}

func TestClient_Get_RPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height 999999 is not available"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Get(context.Background(), "/block?height=999999")
	if err == nil {
		t.Fatal("expected error for JSON-RPC error response")
	}
	if !strings.Contains(err.Error(), "Internal error") {
		t.Errorf("expected error to contain CometBFT message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "height 999999 is not available") {
		t.Errorf("expected error to contain data field, got: %v", err)
	}
}

func TestClient_SetTimeout(t *testing.T) {
	c := NewClient("", nil)
	c.SetTimeout(500 * time.Millisecond)
	if c.timeout != 500*time.Millisecond {
		t.Errorf("timeout = %v, want 500ms", c.timeout)
	}
}

func TestClient_DefaultEndpoint(t *testing.T) {
	c := NewClient("", nil)
	if c.Endpoint() != DefaultEndpoint {
		t.Errorf("endpoint = %q, want %q", c.Endpoint(), DefaultEndpoint)
	}
}
