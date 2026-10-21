// Package serve is the gateway's R2 CDN edge: GET /media/{bucket}/{key...} reads
// an object straight from the appropriate Cloudflare R2 bucket and serves it
// with immutable cache headers, ETag/304 revalidation, and HTTP range support.
// Small objects are cached in Redis (internal/cache) for an origin-free hit;
// large objects stream straight through with bounded memory (no full buffering).
//
// The edge is intentionally schema-free: it serves by raw object key, so it owns
// no database and never resolves token/user addresses — media/metadata and
// media/user return fully-qualified gateway URLs that point here.
package serve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	gatewaystorage "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
)

// immutableCacheControl is sent for served objects. Media keys are
// content-addressed/versioned by the writing service, so a long immutable TTL is
// safe and offloads the vast majority of reads to the CDN/browser.
const immutableCacheControl = "public, max-age=31536000, immutable"

// Reader is the read surface of shared/storage.Client used by the handler (the
// real client / a real MinIO client in tests satisfies it — no fakes).
type Reader interface {
	Get(ctx context.Context, key string) (*storage.Object, error)
}

// Handler serves media bytes from one or more R2 buckets.
type Handler struct {
	observer     *gatewaystorage.MediaObserver
	buckets      map[string]Reader
	cache        *cache.Cache
	cacheMaxSize int64
	logger       *slog.Logger
}

// Deps configures New.
type Deps struct {
	Observer *gatewaystorage.MediaObserver
	// Buckets maps a logical name (e.g. "token", "user", "og") to its R2 reader.
	Buckets map[string]Reader
	// Cache is the Redis hot-cache; nil disables caching (always origin reads).
	Cache *cache.Cache
	// CacheMaxSize is the largest object body eligible for the Redis cache.
	CacheMaxSize int64
	Logger       *slog.Logger
}

// New constructs a Handler.
func New(d Deps) *Handler {
	if d.CacheMaxSize < 0 || d.CacheMaxSize > 1<<20 {
		d.CacheMaxSize = 1 << 20
	}
	return &Handler{
		observer:     d.Observer,
		buckets:      d.Buckets,
		cache:        d.Cache,
		cacheMaxSize: d.CacheMaxSize,
		logger:       d.Logger,
	}
}

// RegisterRoutes mounts the media-serving endpoint onto r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/media/{bucket}/*", h.serve)
	r.Head("/media/{bucket}/*", h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	reader, ok := h.buckets[bucket]
	if !ok {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "unknown media bucket")
		return
	}
	key := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if !validKey(key) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid object key")
		return
	}

	if h.cache != nil {
		if obj, found, err := h.cache.Get(r.Context(), bucket, key); err == nil && found {
			if h.observer != nil {
				h.observer.RecordRead(false)
			}
			h.write(w, r, obj.ContentType, obj.ETag, bytes.NewReader(obj.Body), int64(len(obj.Body)))
			return
		} else if err != nil {
			h.logErr("cache get", err)
		}
	}

	if h.cache != nil {
		if missing, err := h.cache.Missing(r.Context(), bucket, key); err == nil && missing {
			w.Header().Set("Cache-Control", "public, max-age=30")
			sharedhttp.WriteError(w, 404, "Not Found", "object not found")
			return
		}
	}
	if r.Method == http.MethodHead || r.Header.Get("If-None-Match") != "" {
		hr, ok := reader.(interface {
			Head(context.Context, string) (*storage.ObjectInfo, error)
		})
		if !ok {
			sharedhttp.WriteError(w, 503, "Service Unavailable", "origin metadata unavailable")
			return
		}
		if h.observer != nil {
			h.observer.RecordRead(true)
		}
		info, err := hr.Head(r.Context(), key)
		if errors.Is(err, storage.ErrNotFound) {
			h.missing(w, r, bucket, key)
			return
		}
		if err != nil || info == nil || info.Size < 0 || info.Size > 32<<20 || !safeHeaders(info.ContentType, info.ETag) {
			sharedhttp.WriteError(w, 502, "Bad Gateway", "invalid origin metadata")
			return
		}
		if matchesETag(r.Header.Get("If-None-Match"), info.ETag) {
			h.headers(w, info.ContentType, info.ETag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead {
			h.headers(w, info.ContentType, info.ETag)
			w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	if h.observer != nil {
		h.observer.RecordRead(true)
	}
	obj, err := reader.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		h.missing(w, r, bucket, key)
		return
	}
	if err != nil {
		h.logErr("origin get", err)
		sharedhttp.WriteError(w, http.StatusBadGateway, "Bad Gateway", "origin unavailable")
		return
	}
	if obj == nil || obj.Body == nil {
		sharedhttp.WriteError(w, 502, "Bad Gateway", "invalid origin object")
		return
	}
	defer obj.Body.Close()
	if obj.Size < 0 || obj.Size > 32<<20 || !safeHeaders(obj.ContentType, obj.ETag) {
		sharedhttp.WriteError(w, 502, "Bad Gateway", "invalid origin size")
		return
	}

	// Small objects: buffer, cache, and serve with full range/304 support.
	if h.cache != nil && obj.Size >= 0 && obj.Size <= h.cacheMaxSize {
		body, err := io.ReadAll(io.LimitReader(obj.Body, h.cacheMaxSize+1))
		if err != nil {
			h.logErr("origin read", err)
			sharedhttp.WriteError(w, http.StatusBadGateway, "Bad Gateway", "origin read failed")
			return
		}
		if int64(len(body)) != obj.Size {
			sharedhttp.WriteError(w, 502, "Bad Gateway", "origin size mismatch")
			return
		}
		if int64(len(body)) <= h.cacheMaxSize {
			if err := h.cache.Set(r.Context(), bucket, key, cache.Object{ContentType: obj.ContentType, ETag: obj.ETag, Body: body}); err != nil {
				h.logErr("cache set", err)
			}
			h.write(w, r, obj.ContentType, obj.ETag, bytes.NewReader(body), int64(len(body)))
			return
		}
		sharedhttp.WriteError(w, 502, "Bad Gateway", "origin size mismatch")
		return
	}

	h.stream(w, r, obj.ContentType, obj.ETag, obj.Body, obj.Size)
}

// write serves a fully-buffered body via http.ServeContent, which handles
// conditional requests (If-None-Match -> 304) and Range requests for free.
func (h *Handler) write(w http.ResponseWriter, r *http.Request, contentType, etag string, body *bytes.Reader, _ int64) {
	if value := r.Header.Get("Range"); value != "" && (r.Header.Get("If-Range") == "" || r.Header.Get("If-Range") == quoteETag(etag)) {
		_, _, ok := boundedRange(value, int64(body.Len()))
		if !ok || (contentType != "image/png" && contentType != "image/jpeg" && contentType != "image/webp") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Del("CDN-Cache-Control")
			http.Error(w, "invalid range", 416)
			return
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if etag != "" {
		w.Header().Set("ETag", quoteETag(etag))
	}
	w.Header().Set("Cache-Control", immutableCacheControl)
	w.Header().Set("CDN-Cache-Control", "public, max-age=31536000")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Zero modtime disables Last-Modified handling; ServeContent still does
	// ETag/If-None-Match and Range based on the headers set above.
	http.ServeContent(w, r, "", time.Time{}, body)
}

// stream serves a body without buffering (large objects). Range is not offered
// on this path; the immutable cache headers make repeat full reads rare.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, contentType, etag string, body io.Reader, size int64) {
	h.headers(w, contentType, etag)
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	start, end, status := int64(0), size-1, http.StatusOK
	if value := r.Header.Get("Range"); value != "" && (r.Header.Get("If-Range") == "" || r.Header.Get("If-Range") == quoteETag(etag)) {
		if contentType != "image/png" && contentType != "image/jpeg" && contentType != "image/webp" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Del("CDN-Cache-Control")
			http.Error(w, "range unavailable", 416)
			return
		}
		var ok bool
		start, end, ok = boundedRange(value, size)
		if !ok {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Del("CDN-Cache-Control")
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
			http.Error(w, "invalid range", 416)
			return
		}
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(size, 10))
	}
	if start > 0 {
		if _, err := io.CopyN(io.Discard, body, start); err != nil {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Del("CDN-Cache-Control")
			sharedhttp.WriteError(w, 502, "Bad Gateway", "origin size mismatch")
			return
		}
	}
	length := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		if _, err := io.CopyN(w, body, length); err != nil {
			h.logErr("stream", err)
		}
	}
}

func (h *Handler) headers(w http.ResponseWriter, contentType, etag string) {
	w.Header().Set("Content-Type", contentType)
	if etag != "" {
		w.Header().Set("ETag", quoteETag(etag))
	}
	w.Header().Set("Cache-Control", immutableCacheControl)
	w.Header().Set("CDN-Cache-Control", "public, max-age=31536000")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (h *Handler) missing(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if h.cache != nil {
		if err := h.cache.SetMissing(r.Context(), bucket, key); err != nil {
			h.logErr("cache missing", err)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	sharedhttp.WriteError(w, 404, "Not Found", "object not found")
}

func matchesETag(value, etag string) bool {
	if value == "" {
		return false
	}
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || (etag != "" && strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(quoteETag(etag), "W/")) {
			return true
		}
	}
	return false
}

func boundedRange(value string, size int64) (int64, int64, bool) {
	if size <= 0 || len(value) > 64 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, 0, false
			}
		}
	}
	if parts[0] == "" {
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 || n > 1<<20 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	if end-start+1 > 1<<20 {
		return 0, 0, false
	}
	return start, end, true
}

// validKey rejects empty keys and any path-traversal attempt.
func validKey(key string) bool {
	if key == "" || len(key) > 1024 {
		return false
	}
	if strings.Contains(key, "..") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// quoteETag wraps a bare ETag value in the quotes the HTTP spec requires.
func quoteETag(etag string) string {
	if strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, `W/`) {
		return etag
	}
	return `"` + etag + `"`
}

func (h *Handler) logErr(op string, err error) {
	if h.logger != nil {
		h.logger.Error("media serve error", slog.String("op", op), slog.String("reason", "origin_or_cache_failure"))
	}
}

func safeHeaders(ct, etag string) bool {
	return len(ct) <= 128 && len(etag) <= 128 && !strings.ContainsAny(ct+etag, "\r\n") && (ct == "image/png" || ct == "image/jpeg" || ct == "image/webp" || ct == "application/octet-stream")
}
