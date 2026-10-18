package projectionstats

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type PublicHandlers struct {
	Service *ReadService
	Clock   func() time.Time
}

func (h PublicHandlers) Register(r chi.Router) {
	r.Get("/v2/stats/platform", h.platform)
	r.Get("/v2/stats/{poolAddress}/transactions", h.trades)
	r.Get("/v2/stats/{poolAddress}", h.pool)
}
func (h PublicHandlers) now() int64 {
	if h.Clock != nil {
		return h.Clock().Unix()
	}
	return time.Now().Unix()
}
func statsResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func statsFailure(w http.ResponseWriter, err error) {
	status, code := 503, "unavailable"
	if errors.Is(err, ErrInvalid) {
		status, code = 400, "invalid_request"
	}
	if errors.Is(err, ErrNotFound) {
		status, code = 404, "pool_not_found"
	}
	statsResponse(w, status, map[string]any{"error": map[string]string{"code": code}})
}
func (h PublicHandlers) trades(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		statsFailure(w, ErrUnavailable)
		return
	}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 || (key != "limit" && key != "side" && key != "cursor" && key != "direction") {
			statsFailure(w, ErrInvalid)
			return
		}
	}
	if q.Has("direction") && q.Get("direction") != "desc" {
		statsFailure(w, ErrInvalid)
		return
	}
	limit := 50
	if q.Has("limit") {
		value := q.Get("limit")
		n, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(n) != value {
			statsFailure(w, ErrInvalid)
			return
		}
		limit = n
	}
	result, err := h.Service.Trades(r.Context(), chi.URLParam(r, "poolAddress"), q.Get("side"), limit, q.Get("cursor"), h.now())
	if err != nil {
		statsFailure(w, err)
		return
	}
	statsResponse(w, 200, result)
}
func (h PublicHandlers) pool(w http.ResponseWriter, r *http.Request) {
	h.window(w, r, chi.URLParam(r, "poolAddress"))
}
func (h PublicHandlers) platform(w http.ResponseWriter, r *http.Request) { h.window(w, r, "") }
func (h PublicHandlers) window(w http.ResponseWriter, r *http.Request, pool string) {
	if h.Service == nil {
		statsFailure(w, ErrUnavailable)
		return
	}
	q := r.URL.Query()
	seconds := int64(3600)
	for key, values := range q {
		if key != "windowSeconds" || len(values) != 1 {
			statsFailure(w, ErrInvalid)
			return
		}
	}
	if q.Has("windowSeconds") {
		raw := q.Get("windowSeconds")
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != raw {
			statsFailure(w, ErrInvalid)
			return
		}
		seconds = n
	}
	if pool != "" && !address.MatchString(pool) || seconds < 1 || seconds > 86400 {
		statsFailure(w, ErrInvalid)
		return
	}
	result, err := h.Service.Window(r.Context(), pool, seconds, h.now())
	if err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrIntegrity) {
		result, err = h.Service.LastWindow(r.Context(), pool, seconds)
	}
	if err != nil {
		statsFailure(w, err)
		return
	}
	statsResponse(w, 200, result)
}
