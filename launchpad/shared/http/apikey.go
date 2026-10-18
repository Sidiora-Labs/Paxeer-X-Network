package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// APIKeyOptions configures the API-key middleware (parity with TS
// api-key-auth.ts). Only the three exact health paths are exempt.
type APIKeyOptions struct {
	APIKey      string
	HeaderName  string
	RoutePrefix string
}

// APIKeyAuth returns a middleware that requires a matching API key (compared in
// constant time, SECTION 17). Missing key -> 401, mismatch -> 403.
func APIKeyAuth(opts APIKeyOptions) (func(http.Handler) http.Handler, error) {
	if opts.APIKey == "" {
		return nil, errors.New("http: APIKeyAuth requires APIKey (set ADMIN_API_KEY)")
	}
	header := opts.HeaderName
	if header == "" {
		header = "X-API-Key"
	}
	expected := sha256.Sum256([]byte(opts.APIKey))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/health" || r.URL.Path == "/health/live" || r.URL.Path == "/health/ready") {
				next.ServeHTTP(w, r)
				return
			}
			if opts.RoutePrefix != "" && !(r.URL.Path == strings.TrimSuffix(opts.RoutePrefix, "/") || strings.HasPrefix(r.URL.Path, strings.TrimSuffix(opts.RoutePrefix, "/")+"/")) {
				next.ServeHTTP(w, r)
				return
			}
			if len(r.Header.Values(header)) != 1 {
				WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or ambiguous API key")
				return
			}
			provided := r.Header.Get(header)
			if provided == "" || len(provided) > 4096 {
				WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing API key")
				return
			}
			if subtle.ConstantTimeCompare(func() []byte { h := sha256.Sum256([]byte(provided)); return h[:] }(), expected[:]) != 1 {
				WriteError(w, http.StatusForbidden, "Forbidden", "invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
