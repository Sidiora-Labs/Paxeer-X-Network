package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
)

// registerDevelopmentHistorySearchDiscovery mounts the non-production trade
// history, candles, search and anonymous discovery reads over the development
// store (indexer, candles, metadata, users and narrative_discovery tables in
// the shared database). Production mounts the same paths from the accounting
// read model, the stats search proxy and registerDiscovery instead.
func registerDevelopmentHistorySearchDiscovery(r chi.Router, deps developmentReadDeps) {
	rest.RegisterDevelopmentHistorySearchDiscovery(r, deps.store, deps.admission)
	// The web profile reads per-author callout history from
	// GET /v2/discovery/callouts?author=<wallet>. The development router only
	// mounts POST on that path, so chi answered 405 with an empty body. No
	// by-author read exists (the v2 contract declares only create and edit on
	// this path and the development store indexes callouts by pool), so the
	// read answers the v2 envelope production's registerDiscovery gives it:
	// 503 DEPENDENCY_UNAVAILABLE, not retryable.
	var calloutHistory http.Handler = http.HandlerFunc(rest.WriteV2NotConfiguredError)
	if deps.admission != nil {
		calloutHistory = deps.admission.Middleware(rest.AdmissionAggregate)(calloutHistory)
	}
	r.Method(http.MethodGet, "/v2/discovery/callouts", rest.ResponseMetadata(calloutHistory))
}
