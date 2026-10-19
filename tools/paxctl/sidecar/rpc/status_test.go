package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// wrapResult wraps a JSON payload in the CometBFT JSON-RPC envelope.
func wrapResult(inner string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":-1,"result":%s}`, inner)
}

func TestStatusClient_LatestHeight(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(wrapResult(`{"sync_info":{"latest_block_height":"12345","catching_up":false}}`)))
	}))
	defer srv.Close()

	c := NewStatusClient(srv.URL, nil)
	h, err := c.LatestHeight(context.Background())
	if err != nil {
		t.Fatalf("LatestHeight: %v", err)
	}
	if h != 12345 {
		t.Errorf("height = %d, want 12345", h)
	}
}

func TestStatusClient_CatchingUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(wrapResult(`{"sync_info":{"latest_block_height":"100","catching_up":true}}`)))
	}))
	defer srv.Close()

	c := NewStatusClient(srv.URL, nil)
	s, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !s.CatchingUp {
		t.Error("expected CatchingUp=true")
	}
}

func TestStatusClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewStatusClient(srv.URL, nil)
	_, err := c.LatestHeight(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestStatusClient_ConnectionRefused(t *testing.T) {
	c := NewStatusClient("http://127.0.0.1:1", nil)
	_, err := c.LatestHeight(context.Background())
	if err == nil {
		t.Fatal("expected error for refused connection")
	}
}

func TestStatusClient_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewStatusClient(srv.URL, nil)
	_, err := c.LatestHeight(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestStatusClient_InvalidBlockHeight(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"empty height", wrapResult(`{"sync_info":{"latest_block_height":"","catching_up":false}}`)},
		{"non-numeric height", wrapResult(`{"sync_info":{"latest_block_height":"abc","catching_up":false}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.payload))
			}))
			defer srv.Close()

			c := NewStatusClient(srv.URL, nil)
			_, err := c.LatestHeight(context.Background())
			if err == nil {
				t.Fatal("expected error for invalid block height")
			}
		})
	}
}

func TestStatusClient_DefaultEndpoint(t *testing.T) {
	c := NewStatusClient("", nil)
	if c.Endpoint() != DefaultEndpoint {
		t.Errorf("endpoint = %q, want %q", c.Endpoint(), DefaultEndpoint)
	}
}

func TestStatusClient_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":-1}`))
	}))
	defer srv.Close()

	c := NewStatusClient(srv.URL, nil)
	_, err := c.LatestHeight(context.Background())
	if err == nil {
		t.Fatal("expected error for empty result")
	}
}
