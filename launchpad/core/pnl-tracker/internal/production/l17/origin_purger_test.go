package l17

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestOriginPurgerRequiresOriginGone(t *testing.T) {
	const card = "3f1e9c7a-2b4d-4e6f-8a1b-9c0d1e2f3a4b"
	const share = "7a6b5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d"
	var mu sync.Mutex
	gone := map[string]bool{}
	seen := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		if gone[r.URL.Path] {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	purger := OriginPurger{PublicOrigin: server.URL, Client: server.Client()}
	ctx := context.Background()

	if e := purger.PurgeCard(ctx, card); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("card still served: want ErrUnavailable, got %v", e)
	}
	mu.Lock()
	gone["/v2/portfolio/cards/"+card+"/image.png"] = true
	mu.Unlock()
	if e := purger.PurgeCard(ctx, card); e != nil {
		t.Fatalf("card gone at origin: %v", e)
	}

	mu.Lock()
	gone["/v3/portfolio/shares/"+share] = true
	mu.Unlock()
	if e := purger.PurgeShare(ctx, share); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("share image still served: want ErrUnavailable, got %v", e)
	}
	mu.Lock()
	gone["/v3/portfolio/shares/"+share+"/image.png"] = true
	mu.Unlock()
	if e := purger.PurgeShare(ctx, share); e != nil {
		t.Fatalf("share gone at origin: %v", e)
	}

	mu.Lock()
	want := []string{
		"GET /v2/portfolio/cards/" + card + "/image.png",
		"GET /v2/portfolio/cards/" + card + "/image.png",
		"GET /v3/portfolio/shares/" + share,
		"GET /v3/portfolio/shares/" + share + "/image.png",
		"GET /v3/portfolio/shares/" + share,
		"GET /v3/portfolio/shares/" + share + "/image.png",
	}
	if len(seen) != len(want) {
		mu.Unlock()
		t.Fatalf("requests %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			mu.Unlock()
			t.Fatalf("request %d = %s, want %s", i, seen[i], want[i])
		}
	}
	mu.Unlock()

	if e := purger.PurgeCard(ctx, "not-a-uuid"); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("invalid card id: %v", e)
	}
	for _, origin := range []string{"http://pnl.example", "https://pnl.example/base", "https://user@pnl.example", "https://pnl.example?x=1", ""} {
		if e := (OriginPurger{PublicOrigin: origin}).PurgeShare(ctx, share); !errors.Is(e, ErrInvalid) {
			t.Fatalf("origin %q: want ErrInvalid, got %v", origin, e)
		}
	}
}
