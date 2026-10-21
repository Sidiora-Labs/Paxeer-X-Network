package serve

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
)

const objKey = "logos/logo-0xabc.png"

func setup(t *testing.T) (*Handler, func(ctx context.Context, key string)) {
	t.Helper()
	store := internaltest.NewStore(t, "token-bucket")
	rdb, _ := internaltest.NewRedis(t)
	h := New(Deps{
		Buckets:      map[string]Reader{"token": store},
		Cache:        cache.New(rdb, time.Minute),
		CacheMaxSize: 1 << 20,
	})
	put := func(ctx context.Context, key string) {
		const body = "PNGDATA-1234"
		if err := store.Put(ctx, key, strings.NewReader(body), int64(len(body)), "image/png"); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	return h, put
}

func do(t *testing.T, h *Handler, method, path string, hdr http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(method, path, nil)
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestServe_OK_WithCacheHeaders(t *testing.T) {
	h, put := setup(t)
	put(context.Background(), objKey)

	rec := do(t, h, http.MethodGet, "/media/token/"+objKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "PNGDATA-1234" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("cache-control = %q", cc)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("missing ETag")
	}
}

func TestServe_CacheHit_SurvivesOriginDelete(t *testing.T) {
	store := internaltest.NewStore(t, "token-bucket")
	rdb, _ := internaltest.NewRedis(t)
	h := New(Deps{Buckets: map[string]Reader{"token": store}, Cache: cache.New(rdb, time.Minute), CacheMaxSize: 1 << 20})

	ctx := context.Background()
	if err := store.Put(ctx, objKey, strings.NewReader("CACHED"), 6, "image/png"); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Prime the cache.
	if rec := do(t, h, http.MethodGet, "/media/token/"+objKey, nil); rec.Code != http.StatusOK {
		t.Fatalf("prime status = %d", rec.Code)
	}
	// Remove from origin; a subsequent hit must be served from Redis.
	if err := store.Delete(ctx, objKey); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/media/token/"+objKey, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "CACHED" {
		t.Fatalf("cache hit status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServe_NotModified(t *testing.T) {
	h, put := setup(t)
	put(context.Background(), objKey)

	first := do(t, h, http.MethodGet, "/media/token/"+objKey, nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing etag")
	}
	rec := do(t, h, http.MethodGet, "/media/token/"+objKey, http.Header{"If-None-Match": {etag}})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestServe_Range(t *testing.T) {
	h, put := setup(t)
	put(context.Background(), objKey)

	rec := do(t, h, http.MethodGet, "/media/token/"+objKey, http.Header{"Range": {"bytes=0-3"}})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "PNGD" {
		t.Errorf("partial body = %q, want PNGD", rec.Body.String())
	}
}

func TestServe_StreamLargeUncached(t *testing.T) {
	store := internaltest.NewStore(t, "token-bucket")
	rdb, _ := internaltest.NewRedis(t)
	// Tiny cache cap forces the streaming (non-cached) path for this object.
	h := New(Deps{Buckets: map[string]Reader{"token": store}, Cache: cache.New(rdb, time.Minute), CacheMaxSize: 4})

	ctx := context.Background()
	big := strings.Repeat("X", 5000)
	if err := store.Put(ctx, "big.bin", strings.NewReader(big), int64(len(big)), "application/octet-stream"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/media/token/big.bin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != len(big) {
		t.Errorf("streamed %d bytes, want %d", rec.Body.Len(), len(big))
	}
	if rec.Header().Get("Content-Length") != "5000" {
		t.Errorf("content-length = %q", rec.Header().Get("Content-Length"))
	}
}

func TestServe_NoCacheConfigured(t *testing.T) {
	store := internaltest.NewStore(t, "token-bucket")
	h := New(Deps{Buckets: map[string]Reader{"token": store}}) // Cache nil

	ctx := context.Background()
	if err := store.Put(ctx, objKey, strings.NewReader("RAW"), 3, "image/png"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/media/token/"+objKey, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "RAW" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServe_UnknownBucket(t *testing.T) {
	h, _ := setup(t)
	rec := do(t, h, http.MethodGet, "/media/nope/key.png", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServe_MissingObject(t *testing.T) {
	h, _ := setup(t)
	rec := do(t, h, http.MethodGet, "/media/token/does/not/exist.png", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServe_InvalidKey(t *testing.T) {
	h, _ := setup(t)
	rec := do(t, h, http.MethodGet, "/media/token/../secret.png", nil)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 400/404", rec.Code)
	}
}

// errReader is a Reader whose Get always fails with a non-NotFound error, to
// exercise the origin-error (502) + logErr path.
type errReader struct{}

func (errReader) Get(context.Context, string) (*storage.Object, error) {
	return nil, errors.New("boom")
}

func TestServe_OriginError(t *testing.T) {
	rdb, _ := internaltest.NewRedis(t)
	h := New(Deps{
		Buckets:      map[string]Reader{"token": errReader{}},
		Cache:        cache.New(rdb, time.Minute),
		CacheMaxSize: 1 << 20,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := do(t, h, http.MethodGet, "/media/token/x.png", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestValidKey(t *testing.T) {
	good := []string{"logos/a.png", "a/b/c.webp"}
	bad := []string{"", "../x", "a/../b", "a//b", "."}
	for _, g := range good {
		if !validKey(g) {
			t.Errorf("validKey(%q) = false", g)
		}
	}
	for _, b := range bad {
		if validKey(b) {
			t.Errorf("validKey(%q) = true", b)
		}
	}
}
