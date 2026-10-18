package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestETag_StableAndDistinct(t *testing.T) {
	a := ETag([]byte(`{"x":1}`))
	b := ETag([]byte(`{"x":1}`))
	c := ETag([]byte(`{"x":2}`))
	if a != b {
		t.Errorf("ETag not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Error("ETag should differ for different bodies")
	}
	if len(a) < 4 || a[0] != '"' {
		t.Errorf("ETag not quoted: %q", a)
	}
}

func TestGetOrFetch_CachesWithinTTL(t *testing.T) {
	c := New(8)
	var calls atomic.Int64
	fetch := func(_ context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte(`{"v":1}`), nil
	}

	for i := 0; i < 3; i++ {
		body, etag, err := c.GetOrFetch(context.Background(), "k", time.Minute, fetch)
		if err != nil || string(body) != `{"v":1}` || etag == "" {
			t.Fatalf("call %d: body=%q etag=%q err=%v", i, body, etag, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("fetch called %d times, want 1 (cached)", calls.Load())
	}
}

func TestGetOrFetch_RefetchesAfterTTL(t *testing.T) {
	c := New(8)
	var calls atomic.Int64
	fetch := func(_ context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte(`{}`), nil
	}
	_, _, _ = c.GetOrFetch(context.Background(), "k", 20*time.Millisecond, fetch)
	time.Sleep(40 * time.Millisecond)
	_, _, _ = c.GetOrFetch(context.Background(), "k", 20*time.Millisecond, fetch)
	if calls.Load() != 2 {
		t.Errorf("fetch called %d times, want 2 (TTL expiry)", calls.Load())
	}
}

func TestGetOrFetch_SingleflightDedupesConcurrent(t *testing.T) {
	c := New(8)
	var calls atomic.Int64
	gate := make(chan struct{})
	fetch := func(_ context.Context) ([]byte, error) {
		calls.Add(1)
		<-gate // hold all callers inside the single fetch
		return []byte(`{"shared":true}`), nil
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = c.GetOrFetch(context.Background(), "hot", time.Minute, fetch)
		}()
	}
	// Let goroutines pile up on the inflight call, then release.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	if calls.Load() != 1 {
		t.Errorf("fetch called %d times under stampede, want 1", calls.Load())
	}
}

func TestGetOrFetch_LRUEviction(t *testing.T) {
	c := New(2)
	mk := func(v string) FetchFunc {
		return func(_ context.Context) ([]byte, error) { return []byte(v), nil }
	}
	_, _, _ = c.GetOrFetch(context.Background(), "a", time.Minute, mk("a"))
	_, _, _ = c.GetOrFetch(context.Background(), "b", time.Minute, mk("b"))
	_, _, _ = c.GetOrFetch(context.Background(), "c", time.Minute, mk("c")) // evicts "a"

	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
	var aCalls atomic.Int64
	_, _, _ = c.GetOrFetch(context.Background(), "a", time.Minute, func(_ context.Context) ([]byte, error) {
		aCalls.Add(1)
		return []byte("a"), nil
	})
	if aCalls.Load() != 1 {
		t.Error("evicted key 'a' should have triggered a re-fetch")
	}
}

func TestGetOrFetch_ErrorNotCached(t *testing.T) {
	c := New(8)
	var calls atomic.Int64
	fetch := func(_ context.Context) ([]byte, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	}
	if _, _, err := c.GetOrFetch(context.Background(), "k", time.Minute, fetch); err == nil {
		t.Fatal("expected error")
	}
	_, _, _ = c.GetOrFetch(context.Background(), "k", time.Minute, fetch)
	if calls.Load() != 2 {
		t.Errorf("errors must not be cached: calls = %d, want 2", calls.Load())
	}
}

func TestHandler_ServesAndConditional304(t *testing.T) {
	c := New(8)
	keyFn := func(r *http.Request) string { return r.URL.Path }
	h := c.Handler(keyFn, 30*time.Second, func(_ *http.Request) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats/0xABC", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=30" {
		t.Errorf("Cache-Control = %q", cc)
	}

	// Conditional request with the ETag -> 304.
	req := httptest.NewRequest(http.MethodGet, "/stats/0xABC", nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}
}

func TestHandler_FetchErrorReturns500(t *testing.T) {
	c := New(8)
	h := c.Handler(func(r *http.Request) string { return r.URL.Path }, time.Minute,
		func(_ *http.Request) ([]byte, error) { return nil, context.Canceled })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
