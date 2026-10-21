package serve

import (
	"context"
	mediastore "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
	"time"
)

// RestrictionAuthority must be consulted before BOTH cache and origin reads.
// This intentionally denies on dependency outage; cached bytes do not override
// a newer restriction or a deletion tombstone.
type RestrictionAuthority interface {
	Allowed(context.Context, string) error
}
type Approved struct {
	connected bool
	next      *Handler
	authority RestrictionAuthority
	slots     chan struct{}
}

func NewApproved(d Deps, authority RestrictionAuthority) *Approved {
	return &Approved{next: New(d), authority: authority, slots: make(chan struct{}, 256)}
}
func (a *Approved) RegisterRoutes(r chi.Router) {
	r.Get("/media/{bucket}/*", a.ServeHTTP)
	r.Head("/media/{bucket}/*", a.ServeHTTP)
}
func (a *Approved) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("CDN-Cache-Control", "no-store")
	key := chi.URLParam(r, "*")
	valid := mediastore.ValidApprovedKey(key)
	if a.connected {
		valid = mediastore.ValidDeliveryKey(key)
	}
	if !valid || a.authority == nil || r.URL.RawPath != "" || strings.ContainsAny(r.URL.Path, "%\\") {
		http.Error(w, "media unavailable", 404)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "media busy", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.authority.Allowed(ctx, key); err != nil {
		http.Error(w, "media unavailable", 404)
		return
	}
	// Prevent expensive multipart ranges and unsupported byte syntax before any
	// origin request. PNG/JPEG/WebP variants support one bounded byte range.
	if v := r.Header.Get("Range"); v != "" {
		if !strings.HasPrefix(v, "bytes=") || strings.Contains(v, ",") || len(v) > 64 {
			http.Error(w, "invalid range", 416)
			return
		}
	}
	a.next.serve(w, r.WithContext(ctx))
}

func NewConnectedApproved(d Deps, authority *mediastore.Authority) (*Approved, error) {
	if authority == nil || len(d.Buckets) == 0 {
		return nil, mediastore.ErrUnavailable
	}
	for _, reader := range d.Buckets {
		if reader == nil {
			return nil, mediastore.ErrUnavailable
		}
	}
	handler := NewApproved(d, authority)
	handler.connected = true
	return handler, nil
}

func (a *Approved) RegisterConnectedRoutes(r chi.Router, bucket string) error {
	if !a.connected || a.next.buckets[bucket] == nil {
		return mediastore.ErrUnavailable
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		route := chi.NewRouteContext()
		route.URLParams.Add("bucket", bucket)
		route.URLParams.Add("*", strings.TrimPrefix(req.URL.Path, "/"))
		a.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route)))
	})
	r.Method(http.MethodGet, "/approved/{hash}/{size}/{file}", handler)
	r.Method(http.MethodHead, "/approved/{hash}/{size}/{file}", handler)
	return nil
}
