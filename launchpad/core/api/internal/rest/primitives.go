package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// APIError matches the frozen IF_ERROR contract's camel-case wire names.
type APIError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RequestID    string `json:"requestId"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS *int   `json:"retryAfterMs"`
	FieldErrors  []any  `json:"fieldErrors"`
	Details      any    `json:"details"`
}

func WriteAPIError(w http.ResponseWriter, r *http.Request, status int, code string) {
	if strings.HasPrefix(r.URL.Path, "/v2/") {
		switch code {
		case "ADDRESS_INVALID", "CHAIN_UNSUPPORTED":
			code, status = "FIELD_INVALID", 422
		case "POOL_NOT_FOUND":
			code, status = "NOT_FOUND", 404
		case "DATA_UNAVAILABLE", "CACHE_FILL_SATURATED":
			code, status = "DEPENDENCY_UNAVAILABLE", 503
		case "DATA_INTEGRITY":
			code, status = "INTEGRITY_QUARANTINED", 503
		case "CURSOR_EXPIRED", "CURSOR_SCOPE_MISMATCH":
			code, status = "CURSOR_INVALID", 400
		}
		WriteV2APIError(w, r, status, code)
		return
	}
	messages := map[string]string{"ADDRESS_INVALID": "Invalid pool address", "CHAIN_UNSUPPORTED": "Only chain 125 is supported", "QUERY_INVALID": "Invalid query parameters", "POOL_NOT_FOUND": "Pool not found", "DATA_UNAVAILABLE": "Market data temporarily unavailable", "DATA_INTEGRITY": "Authoritative data failed validation", "CURSOR_INVALID": "Invalid cursor; restart pagination", "CURSOR_EXPIRED": "Cursor expired; restart pagination", "CURSOR_SCOPE_MISMATCH": "Cursor does not match this request", "CACHE_FILL_SATURATED": "Public cache fills are saturated"}
	message := messages[code]
	if message == "" {
		code = "DATA_UNAVAILABLE"
		message = messages[code]
		status = 503
	}
	id := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(id); err != nil {
		id = uuid.NewString()
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Schema-Version", "1")
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(w.Header().Get("Cache-Control"), "private") || r.Header.Get("Authorization") != "" {
		w.Header().Set("Cache-Control", "private, no-store")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Del("Retry-After")
	retry := status == 429 || status == 503
	var ms *int
	if retry {
		v := 1000
		ms = &v
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{Code: code, Message: message, RequestID: id, Retryable: retry, RetryAfterMS: ms, FieldErrors: []any{}})
}
func QueryParameters(q url.Values) error {
	if len(q) > 32 {
		return errors.New("QUERY_INVALID")
	}
	for key, values := range q {
		if len(key) > 64 {
			return errors.New("QUERY_INVALID")
		}
		for _, v := range values {
			if len(v) > 2048 {
				return errors.New("QUERY_INVALID")
			}
			if len(values) > 1 && v != values[0] {
				return errors.New("QUERY_INVALID")
			}
		}
	}
	if chain := q.Get("chainId"); chain != "" && chain != "125" {
		return errors.New("CHAIN_UNSUPPORTED")
	}
	return nil
}
func poolParameter(w http.ResponseWriter, r *http.Request) (string, bool) {
	if err := QueryParameters(r.URL.Query()); err != nil {
		WriteAPIError(w, r, 400, err.Error())
		return "", false
	}
	pool, err := store.Address(chi.URLParam(r, "poolAddress"))
	if err != nil {
		WriteAPIError(w, r, 400, "ADDRESS_INVALID")
		return "", false
	}
	return pool, true
}

// PoolAddresses bounds batch cost before any database/cache lookup.
func PoolAddresses(raw string) ([]string, error) {
	if len(raw) > 100*43 {
		return nil, errors.New("QUERY_INVALID")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range strings.Split(raw, ",") {
		if v == "" {
			continue
		}
		a, e := store.Address(v)
		if e != nil {
			return nil, e
		}
		if !seen[a] {
			out = append(out, a)
			seen[a] = true
		}
		if len(out) > 100 {
			return nil, errors.New("QUERY_INVALID")
		}
	}
	return out, nil
}

// WriteReadError preserves stable domain causes through every market handler.
func WriteReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrCacheFillSaturated):
		WriteAPIError(w, r, 503, "CACHE_FILL_SATURATED")
	case errors.Is(err, store.ErrDataIntegrity), errors.Is(err, ErrCacheBodyTooLarge):
		WriteAPIError(w, r, 500, "DATA_INTEGRITY")
	case errors.Is(err, store.ErrPoolNotFound):
		WriteAPIError(w, r, 404, "POOL_NOT_FOUND")
	default:
		WriteAPIError(w, r, 503, "DATA_UNAVAILABLE")
	}
}

type v2ErrorRule struct {
	Status  int
	Message string
}

var v2ErrorRules = map[string]v2ErrorRule{
	"QUERY_INVALID":          {400, "Invalid request syntax"},
	"FIELD_INVALID":          {422, "Field violates its declared type or range"},
	"FIELD_UNKNOWN":          {422, "Request contains an undeclared field"},
	"PAYLOAD_TOO_LARGE":      {413, "Request exceeds its size limit"},
	"REVISION_CONFLICT":      {409, "Expected revision differs from current resource"},
	"IDEMPOTENCY_CONFLICT":   {409, "Idempotency key already binds another request"},
	"STATE_CONFLICT":         {409, "Operation conflicts with current state"},
	"RESNAPSHOT_REQUIRED":    {409, "Snapshot history is unavailable; install a fresh snapshot"},
	"PRECONDITION_FAILED":    {412, "Request precondition is not satisfied"},
	"FEATURE_UNAVAILABLE":    {501, "Required capability is unavailable"},
	"DEPENDENCY_UNAVAILABLE": {503, "Authoritative dependency temporarily unavailable"},
	"INTEGRITY_QUARANTINED":  {503, "Authoritative data is quarantined"},
	"CURSOR_INVALID":         {400, "Invalid cursor; restart pagination"},
	"NOT_FOUND":              {404, "Resource not found"},
	"AUTH_EXPIRED":           {401, "Credential expired or revoked"},
	"SCOPE_DENIED":           {403, "Action is not permitted"},
	"RATE_LIMITED":           {429, "Request budget exhausted"},
	"INTERNAL_ERROR":         {500, "An unexpected error occurred"},
}

// ErrDependencyNotConfigured marks a read whose provider is not configured in
// this deployment (no store, no loader, no chain RPC, no discovery runtime).
// Retrying cannot succeed until the deployment changes, so the error is
// answered as non-retryable.
var ErrDependencyNotConfigured = errors.New("dependency not configured")

// WriteV2APIError writes the frozen v2 error envelope. 429 and 503 are
// retryable (transient budget or dependency failures) and carry Retry-After.
func WriteV2APIError(w http.ResponseWriter, r *http.Request, status int, code string) {
	writeV2Error(w, r, status, code, status == 429 || status == 503)
}

// WriteV2NotConfiguredError answers a route whose dependency is not
// configured in this deployment: 503 DEPENDENCY_UNAVAILABLE with
// retryable=false, retryAfterMs=null and no Retry-After header, so clients do
// not poll a route that cannot recover by itself.
func WriteV2NotConfiguredError(w http.ResponseWriter, r *http.Request) {
	writeV2Error(w, r, 503, "DEPENDENCY_UNAVAILABLE", false)
}

func writeV2Error(w http.ResponseWriter, r *http.Request, status int, code string, retryable bool) {
	rule, ok := v2ErrorRules[code]
	if !ok || status != rule.Status {
		code = "INTERNAL_ERROR"
		rule = v2ErrorRules[code]
	}
	id := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(id); err != nil {
		id = uuid.NewString()
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(w.Header().Get("Cache-Control"), "private") || r.Header.Get("Authorization") != "" {
		w.Header().Set("Cache-Control", "private, no-store")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Del("Retry-After")
	retry := retryable && (rule.Status == 429 || rule.Status == 503)
	var ms *int
	if retry {
		value := 1000
		ms = &value
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(rule.Status)
	_ = json.NewEncoder(w).Encode(APIError{Code: code, Message: rule.Message, RequestID: id, Retryable: retry, RetryAfterMS: ms, FieldErrors: []any{}})
}
func boundedInteger(raw string, fallback, min, max int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || strconv.Itoa(value) != raw || value < min || value > max {
		return 0, errors.New("FIELD_INVALID")
	}
	return value, nil
}
