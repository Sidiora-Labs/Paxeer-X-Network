package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
)

// developmentAccountOperations are the account owner operations the web calls
// on the gateway whose serving runtime (media/user's connected account
// handler behind the gateway's mTLS proxy) exists only in production. Outside
// production nothing serves them, so they answer the frozen 503
// DEPENDENCY_UNAVAILABLE envelope with retryable=false instead of the
// router's plain-text 404. No session check runs first: the answer discloses
// nothing, and a transient identity lookup failure must not turn into a 401
// that signs the browser out.
var developmentAccountOperations = []struct{ method, path string }{
	{http.MethodGet, "/v1/me/watchlist"},
	{http.MethodPut, "/v1/me/watchlist/{poolAddress}"},
	{http.MethodDelete, "/v1/me/watchlist/{poolAddress}"},
	{http.MethodGet, "/v3/me/preferences"},
	{http.MethodPatch, "/v3/me/preferences"},
}

func registerDevelopmentAccountRoutes(r chi.Router) {
	notServed := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { auth.WriteNotServed(w) })
	for _, op := range developmentAccountOperations {
		r.Method(op.method, op.path, notServed)
	}
}
