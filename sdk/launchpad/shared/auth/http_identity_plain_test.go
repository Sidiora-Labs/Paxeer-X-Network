package auth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestHTTPIdentityAuthorityPlainHTTPGate pins the transport rule: a plain
// http:// endpoint on a non-loopback host is refused before any network I/O
// unless AllowPlainHTTP is set, and with it the request is dialled at exactly
// that private host:port.
func TestHTTPIdentityAuthorityPlainHTTPGate(t *testing.T) {
	const endpoint = "http://gateway.railway.internal:8080"
	token := strings.Repeat("s", 32)
	var mu sync.Mutex
	var dialled []string
	dialErr := errors.New("dial recorded")
	transport := &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialled = append(dialled, address)
		mu.Unlock()
		return nil, dialErr
	}}
	request, err := http.NewRequest(http.MethodGet, "https://api.example/v3/me/portfolio", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer a.b.c")

	strict := HTTPIdentityAuthority{Endpoint: endpoint, ServiceToken: token, Client: &http.Client{Transport: transport}}
	if _, err := strict.AuthenticateRequest(context.Background(), request); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("plain http to a private host without AllowPlainHTTP: want ErrScopeDenied, got %v", err)
	}
	if len(dialled) != 0 {
		t.Fatalf("strict authority dialled %v", dialled)
	}

	private := HTTPIdentityAuthority{Endpoint: endpoint, ServiceToken: token, Client: &http.Client{Transport: transport}, AllowPlainHTTP: true}
	if _, err := private.AuthenticateRequest(context.Background(), request); err == nil || !errors.Is(err, dialErr) {
		t.Fatalf("want the recorded dial error, got %v", err)
	}
	if len(dialled) != 1 || dialled[0] != "gateway.railway.internal:8080" {
		t.Fatalf("dialled %v", dialled)
	}

	short := HTTPIdentityAuthority{Endpoint: endpoint, ServiceToken: "short", Client: &http.Client{Transport: transport}, AllowPlainHTTP: true}
	if _, err := short.AuthenticateRequest(context.Background(), request); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("short service token: want ErrScopeDenied, got %v", err)
	}
	withPath := HTTPIdentityAuthority{Endpoint: endpoint + "/", ServiceToken: token, Client: &http.Client{Transport: transport}, AllowPlainHTTP: true}
	if _, err := withPath.AuthenticateRequest(context.Background(), request); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("endpoint with path: want ErrScopeDenied, got %v", err)
	}
	if len(dialled) != 1 {
		t.Fatalf("rejected configurations dialled %v", dialled)
	}
}
