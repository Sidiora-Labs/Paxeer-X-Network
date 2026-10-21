package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestHTTPIdentityAuthorityCheckStreamSession drives the session check over a
// real loopback HTTP identity endpoint: a valid principal is posted verbatim to
// /internal/v2/identity/session:check with the bearer service token, the
// identity service's refusal maps to IdentityAuthorityError, and malformed
// principals are refused before any request is sent.
func TestHTTPIdentityAuthorityCheckStreamSession(t *testing.T) {
	token := strings.Repeat("t", 40)
	principal := StreamPrincipal{AccountID: "6f9b1a52-7c0e-4b8e-9d4a-1f2e3d4c5b6a", SessionID: "0d7c2e41-5a9b-4c3d-8e1f-2a3b4c5d6e7f", Wallet: "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154", AuthEpoch: 3}
	var calls atomic.Int32
	status := atomic.Int32{}
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v2/identity/session:check" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		var got StreamPrincipal
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != principal {
			http.Error(w, "unexpected principal", http.StatusTeapot)
			return
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	authority := HTTPIdentityAuthority{Endpoint: server.URL, ServiceToken: token}

	var _ interface {
		RequestIdentityAuthority
		CheckStreamSession(context.Context, StreamPrincipal) error
	} = authority

	if err := authority.CheckStreamSession(context.Background(), principal); err != nil {
		t.Fatalf("live session: %v", err)
	}
	status.Store(http.StatusUnauthorized)
	var refused *IdentityAuthorityError
	if err := authority.CheckStreamSession(context.Background(), principal); !errors.As(err, &refused) || refused.Status != http.StatusUnauthorized {
		t.Fatalf("revoked session: want IdentityAuthorityError 401, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("identity calls = %d, want 2", calls.Load())
	}

	for name, bad := range map[string]StreamPrincipal{
		"checksummed wallet": {AccountID: principal.AccountID, SessionID: principal.SessionID, Wallet: "0x1DAe78b53C9F39930a2Ff2B6B7AaC12EC75FB154", AuthEpoch: 3},
		"account id":         {AccountID: "acct", SessionID: principal.SessionID, Wallet: principal.Wallet, AuthEpoch: 3},
		"session id":         {AccountID: principal.AccountID, SessionID: "", Wallet: principal.Wallet, AuthEpoch: 3},
		"auth epoch":         {AccountID: principal.AccountID, SessionID: principal.SessionID, Wallet: principal.Wallet, AuthEpoch: -1},
	} {
		if err := authority.CheckStreamSession(context.Background(), bad); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("%s: want ErrScopeDenied, got %v", name, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("malformed principals reached the identity service: %d calls", calls.Load())
	}
}
