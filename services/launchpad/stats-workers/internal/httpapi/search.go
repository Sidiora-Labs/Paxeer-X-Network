package httpapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	canonicalsearch "github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/search"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const (
	searchCacheTTL = 10 * time.Second
	maxSearchHits  = 20
)

// RegisterSearch registers GET /search.
func RegisterSearch(r chi.Router, st *store.Store, rdb *goredis.Client) {
	r.Get("/search", search(st, rdb))
	RegisterCanonicalSearch(r, canonicalsearch.Store{Pool: st.Pool()})
}

// escapeLike escapes LIKE wildcards (% _ \) with a backslash, parity with the TS
// q.replace(/[%_\\]/g, ch => `\\${ch}`).
func escapeLike(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if ch == '%' || ch == '_' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func search(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rawQ := r.URL.Query().Get("q")
		if len(strings.TrimSpace(rawQ)) < 2 {
			sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
				"results": []store.SearchResult{},
				"query":   rawQ,
			})
			return
		}

		query := strings.ToLower(strings.TrimSpace(rawQ))
		escaped := escapeLike(query)
		resultLimit := parseIntDefault(r.URL.Query().Get("limit"), maxSearchHits)
		if resultLimit > maxSearchHits || resultLimit <= 0 {
			resultLimit = maxSearchHits
		}
		cacheKey := "search:" + query + ":" + itoa(resultLimit)

		if cached, err := rdb.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(cached))
			return
		}

		isAddress := strings.HasPrefix(query, "0x") && len(query) >= 6
		likePattern := "%" + escaped + "%"

		rows, err := st.Search(ctx, isAddress, likePattern, resultLimit)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "search failed")
			return
		}

		result := map[string]any{
			"results": rows,
			"query":   strings.TrimSpace(rawQ),
		}
		if payload, err := json.Marshal(result); err == nil {
			rdb.Set(ctx, cacheKey, payload, searchCacheTTL)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, result)
	}
}

// itoa is a tiny non-negative int-to-string helper for cache key building.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func RegisterCanonicalSearch(r chi.Router, st canonicalsearch.Store) {
	r.Get("/v2/search", func(w http.ResponseWriter, r *http.Request) {
		values, err := urlQuery(r)
		if err != nil {
			writeSearchError(w, r, 400, "QUERY_INVALID")
			return
		}
		request, err := canonicalsearch.ParsePublicRequest(values)
		if err != nil {
			var invalid canonicalsearch.PublicRequestError
			if errors.As(err, &invalid) {
				writeSearchError(w, r, invalid.Status, invalid.Code)
			} else {
				writeSearchError(w, r, 500, "INTERNAL_ERROR")
			}
			return
		}
		page, err := st.Search(r.Context(), request)
		if err != nil {
			switch {
			case errors.Is(err, canonicalsearch.ErrInvalid):
				writeSearchError(w, r, 400, "CURSOR_INVALID")
			case errors.Is(err, canonicalsearch.ErrUnsupported):
				writeSearchError(w, r, 501, "FEATURE_UNAVAILABLE")
			default:
				writeSearchError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			}
			return
		}
		var next *string
		if page.Next != nil {
			body, err := json.Marshal(page.Next)
			if err != nil {
				writeSearchError(w, r, 500, "INTERNAL_ERROR")
				return
			}
			text := string(body)
			next = &text
		}
		id := searchRequestID(w, r)
		w.Header().Set("X-Schema-Version", "2")
		w.Header().Set("Cache-Control", "no-store")
		sharedhttp.WriteJSON(w, 200, map[string]any{"requestId": id, "data": map[string]any{"items": page.Items, "matchKind": page.MatchKind, "status": page.Status, "generation": page.Generation}, "watermark": nil, "revision": strconv.FormatInt(page.Watermark, 10), "quality": "partial", "nextCursor": next, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	})
}
func searchRequestID(w http.ResponseWriter, r *http.Request) string {
	id := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(id); err != nil {
		id = uuid.NewString()
	}
	w.Header().Set("X-Request-ID", id)
	return id
}
func writeSearchError(w http.ResponseWriter, r *http.Request, status int, code string) {
	messages := map[string]string{"QUERY_INVALID": "Invalid query syntax", "FIELD_INVALID": "Search field violates its declared bounds", "PAYLOAD_TOO_LARGE": "Search request exceeds its byte limit", "PRECONDITION_FAILED": "Profile search is unavailable", "FEATURE_UNAVAILABLE": "Search filter capability is unavailable", "CURSOR_INVALID": "Invalid or stale cursor; restart pagination", "DEPENDENCY_UNAVAILABLE": "Search dependency temporarily unavailable", "INTERNAL_ERROR": "An unexpected error occurred"}
	message, ok := messages[code]
	if !ok {
		status, code, message = 500, "INTERNAL_ERROR", messages["INTERNAL_ERROR"]
	}
	id := searchRequestID(w, r)
	retry := status == 503
	var retryAfter *int
	if retry {
		ms := 1000
		retryAfter = &ms
		w.Header().Set("Retry-After", "1")
	}
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Cache-Control", "no-store")
	sharedhttp.WriteJSON(w, status, map[string]any{"code": code, "message": message, "requestId": id, "retryable": retry, "retryAfterMs": retryAfter, "fieldErrors": []any{}, "details": nil})
}

func urlQuery(r *http.Request) (url.Values, error) {
	return url.ParseQuery(r.URL.RawQuery)
}

func RegisterIntegratedSearch(r chi.Router, service *canonicalsearch.Service, identity IdentityAuthority) {
	r.Get("/v2/search/expanded", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeSearchError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		host, _, hostErr := net.SplitHostPort(r.RemoteAddr)
		if hostErr != nil || net.ParseIP(host) == nil {
			writeSearchError(w, r, 400, "QUERY_INVALID")
			return
		}
		if err := service.Admit(host); err != nil {
			writeExpandedSearchError(w, r, 429, "RATE_LIMITED")
			return
		}
		values, err := urlQuery(r)
		if err != nil {
			writeSearchError(w, r, 400, "QUERY_INVALID")
			return
		}
		request, err := canonicalsearch.ParseIntegratedRequest(values)
		if err != nil {
			if errors.Is(err, canonicalsearch.ErrUnsupported) {
				writeSearchError(w, r, 501, "FEATURE_UNAVAILABLE")
			} else {
				writeSearchError(w, r, 400, "QUERY_INVALID")
			}
			return
		}
		page, err := service.Public(r.Context(), request)
		if err != nil {
			switch {
			case errors.Is(err, canonicalsearch.ErrBusy):
				writeExpandedSearchError(w, r, 429, "RATE_LIMITED")
			case errors.Is(err, canonicalsearch.ErrInvalid):
				writeSearchError(w, r, 400, "CURSOR_INVALID")
			default:
				writeSearchError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			}
			return
		}
		_, accessCookieErr := r.Cookie("__Host-kindle_access")
		if r.Header.Get("Authorization") != "" || accessCookieErr == nil {
			if identity == nil {
				writeExpandedSearchError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			principal, err := identity.AuthenticateRequest(r.Context(), r)
			if err != nil {
				writeExpandedSearchError(w, r, 401, "AUTH_EXPIRED")
				return
			}
			page, err = service.Personalize(r.Context(), page, principal)
			if err != nil {
				writeExpandedSearchError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
		}
		w.Header().Set("X-Schema-Version", "2")
		w.Header().Set("Cache-Control", "private, no-store")
		query, _ := request.Validate()
		w.Header().Set("ETag", `"`+canonicalsearch.PublicCacheKey(query, request, page.Generation, page.Revision, page.PrivacyRevision)+`"`)
		sharedhttp.WriteJSON(w, 200, map[string]any{"requestId": searchRequestID(w, r), "data": page, "watermark": nil, "revision": strconv.FormatInt(page.Revision, 10), "quality": "partial", "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	})
}
func writeExpandedSearchError(w http.ResponseWriter, r *http.Request, status int, code string) {
	if code != "RATE_LIMITED" && code != "AUTH_EXPIRED" {
		writeSearchError(w, r, status, code)
		return
	}
	message := "Search request budget exhausted; retry shortly"
	var delay *int
	if status == 429 {
		ms := 60000
		delay = &ms
		w.Header().Set("Retry-After", "60")
	} else {
		message = "Authentication expired; sign in again"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Schema-Version", "2")
	sharedhttp.WriteJSON(w, status, map[string]any{"code": code, "message": message, "requestId": searchRequestID(w, r), "retryable": status == 429, "retryAfterMs": delay, "fieldErrors": []any{}, "details": nil})
}
