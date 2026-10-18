package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientKey_PrefersAPIKeyThenIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	if got := ClientKey(r); got != "ip:203.0.113.7" {
		t.Errorf("ClientKey (no key) = %q, want ip:203.0.113.7", got)
	}
	r.Header.Set("X-API-Key", "abc123")
	if got := ClientKey(r); got != "key:abc123" {
		t.Errorf("ClientKey (with key) = %q, want key:abc123", got)
	}
}

func TestLimiter_LoadShedsOverCapacity(t *testing.T) {
	lim := NewLimiter(1)

	started := make(chan struct{})
	release := make(chan struct{})
	blocking := lim.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// Occupy the single slot.
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/slow", nil)
		blocking.ServeHTTP(httptest.NewRecorder(), req)
	}()
	<-started

	if got := lim.InFlight(); got != 1 {
		t.Fatalf("InFlight = %d, want 1", got)
	}

	// Second concurrent request must be shed with 503.
	rec := httptest.NewRecorder()
	blocking.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-capacity status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("503 should set Retry-After")
	}

	// Free the slot; a subsequent request succeeds.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for lim.InFlight() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	ok := lim.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec2 := httptest.NewRecorder()
	ok.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("post-release status = %d, want 200", rec2.Code)
	}
}

func TestLimiter_HealthBypassesCap(t *testing.T) {
	lim := NewLimiter(1)
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		h := lim.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}()
	<-started
	defer close(release)

	// Even with the only slot occupied, /health passes through.
	h := lim.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health status under load = %d, want 200", rec.Code)
	}
}

func TestNewLimiter_DefaultsNonPositive(t *testing.T) {
	if lim := NewLimiter(0); lim.Max() != 10000 {
		t.Errorf("default Max = %d, want 10000", lim.Max())
	}
	if lim := NewLimiter(-5); lim.Max() != 10000 {
		t.Errorf("negative Max = %d, want 10000", lim.Max())
	}
}
