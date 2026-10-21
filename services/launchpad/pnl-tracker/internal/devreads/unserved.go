package devreads

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// unservedOperations are the frozen portfolio export, card and share
// operations the web calls on this service. Their runtime (l17 export, card
// and share workers with private object storage) is mounted only in
// production, so outside production they answer the frozen 503
// DEPENDENCY_UNAVAILABLE envelope with retryable=false instead of the
// router's plain-text 404. GET /v2/portfolio/cards/{cardId}/image.png and the
// public share reads keep their 404: no card or share exists here.
var unservedOperations = []struct{ method, path string }{
	{http.MethodPost, "/v2/me/portfolio/exports"},
	{http.MethodGet, "/v2/me/portfolio/exports/{jobId}"},
	{http.MethodGet, "/v2/me/portfolio/exports/{jobId}/download"},
	{http.MethodDelete, "/v2/me/portfolio/exports/{jobId}"},
	{http.MethodPost, "/v2/me/portfolio/cards"},
	{http.MethodGet, "/v2/me/portfolio/cards/{cardId}"},
	{http.MethodDelete, "/v2/me/portfolio/cards/{cardId}"},
	{http.MethodPost, "/v3/me/portfolio/shares/preview"},
	{http.MethodPost, "/v3/me/portfolio/shares"},
	{http.MethodGet, "/v3/me/portfolio/shares/{shareId}"},
	{http.MethodDelete, "/v3/me/portfolio/shares/{shareId}"},
}

func registerUnserved(r chi.Router) {
	for _, op := range unservedOperations {
		r.Method(op.method, op.path, http.HandlerFunc(writeNotServed))
	}
}

func writeNotServed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(interfaces.Error{Code: "DEPENDENCY_UNAVAILABLE", Message: "This operation is not served in this deployment", RequestId: requestID(), Retryable: false, FieldErrors: []interfaces.FieldError{}, Details: nil, RetryAfterMs: nil})
}
