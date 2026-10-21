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

// TestHTTPStreamAuthorityConsumeChatStreamTicket pins the bearer-transport
// chat ticket redemption: ticket shape is checked before any I/O, the request
// carries the service bearer and binding to chat-ticket:consume, and the
// returned principal must satisfy the chat binding.
func TestHTTPStreamAuthorityConsumeChatStreamTicket(t *testing.T) {
	token := strings.Repeat("s", 32)
	wallet := "0x" + strings.Repeat("a", 40)
	other := "0x" + strings.Repeat("b", 40)
	principal := StreamPrincipal{AccountID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222", Wallet: wallet, AuthEpoch: 3}
	var calls atomic.Int32
	var answer atomic.Value
	answer.Store(principal)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Ticket  string              `json:"ticket"`
			Binding StreamTicketBinding `json:"binding"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v2/identity/chat-ticket:consume" || r.Header.Get("Authorization") != "Bearer "+token || json.NewDecoder(r.Body).Decode(&body) != nil || body.Ticket != strings.Repeat("ab", 32) || body.Binding.Endpoint != "/v2/stream" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(answer.Load().(StreamPrincipal))
	}))
	defer server.Close()
	authority := HTTPStreamAuthority{Endpoint: server.URL, ServiceToken: token}
	topics := []string{"chat.pool." + other, "private.chat." + wallet}
	binding := StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: StreamFilterDigest(topics), Origin: "https://web.example", Endpoint: "/v2/stream"}

	got, err := authority.ConsumeChatStreamTicket(context.Background(), strings.Repeat("ab", 32), binding)
	if err != nil || got != principal {
		t.Fatalf("consume: %+v %v", got, err)
	}
	for _, bad := range []string{"", strings.Repeat("AB", 32), strings.Repeat("ab", 31), strings.Repeat("zz", 32)} {
		if _, err = authority.ConsumeChatStreamTicket(context.Background(), bad, binding); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("ticket %q: %v", bad, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("malformed tickets reached the authority: %d calls", calls.Load())
	}
	// A principal whose wallet does not own the private topic fails the binding.
	answer.Store(StreamPrincipal{AccountID: principal.AccountID, SessionID: principal.SessionID, Wallet: other, AuthEpoch: 3})
	if _, err = authority.ConsumeChatStreamTicket(context.Background(), strings.Repeat("ab", 32), binding); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("foreign private topic: %v", err)
	}
	var rejected *IdentityAuthorityError
	if _, err = authority.ConsumeChatStreamTicket(context.Background(), strings.Repeat("cd", 32), binding); !errors.As(err, &rejected) || rejected.Status != http.StatusUnauthorized {
		t.Fatalf("unknown ticket: %v", err)
	}
}
