package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
)

// Development network reads. IF_DEPLOYMENT and IF_TX_STATUS are the
// environment-neutral handlers of network_reads.go, here over the embedded
// reviewed manifest (shared/config/deployment current.json, the source the
// development network authority attests) and NETWORK_RPC_URL; production
// mounts the same handlers over its bound network authority document. The
// remaining routes are development-only and used to fall through to the
// router's plain-text 404 (Railway audit D5):
//
//   - GET /v2/cups/current answers the documented empty read (cup: null; no
//     cup program exists) and the per-cup reads answer 404 NOT_FOUND;
//   - operations with no backing store anywhere in the development stack
//     (conditional orders, price alerts, the push inbox, funding rails,
//     narratives, the SSE event stream) answer the frozen v2 503
//     DEPENDENCY_UNAVAILABLE envelope with retryable=false, so clients render
//     their unavailable state instead of retrying or treating a plain-text
//     404 as a transport fault.
//
// Every success body is validated against shared/interfaces/v2 before it is
// written.

// developmentUnservedOperations are the frozen v2 operations (method, OpenAPI
// path) that no development store can serve.
var developmentUnservedOperations = []struct{ method, path string }{
	{http.MethodGet, "/v2/me/orders"},
	{http.MethodPost, "/v2/me/orders"},
	{http.MethodPost, "/v2/me/orders/{orderId}:cancel"},
	{http.MethodPost, "/v2/me/alerts"},
	{http.MethodDelete, "/v2/me/alerts/{alertId}"},
	{http.MethodGet, "/v2/me/notifications"},
	{http.MethodPut, "/v2/me/notifications/{notificationId}/read"},
	{http.MethodGet, "/v2/discovery/notifications"},
	{http.MethodPatch, "/v2/discovery/notifications/{id}"},
	{http.MethodDelete, "/v2/discovery/notifications/{id}"},
	{http.MethodGet, "/v2/funding/rails"},
	{http.MethodGet, "/v2/narratives"},
	{http.MethodGet, "/v2/narratives/{narrativeId}/feed"},
	{http.MethodGet, "/v2/events/stream"},
}

// developmentNetworkReads serves the network reads from the embedded
// manifest plus the development-only routes below.
type developmentNetworkReads struct {
	*networkReads
}

func newDevelopmentNetworkReads(ctx context.Context, rpcURL string, logger *slog.Logger) (*developmentNetworkReads, error) {
	if logger == nil {
		logger = slog.Default()
	}
	d := &developmentNetworkReads{&networkReads{manifest: deployment.Current(), timeout: 3 * time.Second, logger: logger}}
	if strings.TrimSpace(rpcURL) == "" {
		return d, nil
	}
	var err error
	if d.rpc, err = dialNetworkRPC(ctx, rpcURL, true); err != nil {
		return nil, err
	}
	return d, nil
}

// registerDevelopmentNetworkReads mounts the adapters above. A malformed
// NETWORK_RPC_URL leaves IF_TX_STATUS answering the not-configured envelope;
// the other routes do not depend on the chain.
func registerDevelopmentNetworkReads(r chi.Router, deps developmentReadDeps) {
	d, err := newDevelopmentNetworkReads(context.Background(), deps.cfg.NetworkRPCURL, slog.Default())
	if err != nil {
		slog.Default().Error("development transaction status disabled", "reason", err.Error())
		d = &developmentNetworkReads{&networkReads{manifest: deployment.Current(), timeout: 3 * time.Second, logger: slog.Default()}}
	}
	d.register(r, deps.admission)
}

func (d *developmentNetworkReads) register(r chi.Router, admission *rest.Admission) {
	mount := func(method, path string, class rest.AdmissionClass, h http.HandlerFunc) {
		var handler http.Handler = h
		if admission != nil {
			handler = admission.Middleware(class)(handler)
		}
		r.Method(method, path, rest.ResponseMetadata(handler))
	}
	d.networkReads.mount(r, admission)
	mount(http.MethodGet, "/v2/cups/current", rest.AdmissionCached, currentCupEmpty)
	for _, path := range []string{"/v2/cups/{cupId}", "/v2/cups/{cupId}/standings", "/v2/cups/{cupId}/results"} {
		mount(http.MethodGet, path, rest.AdmissionCached, cupNotFound)
	}
	for _, op := range developmentUnservedOperations {
		mount(op.method, op.path, rest.AdmissionCached, rest.WriteV2NotConfiguredError)
	}
}

// currentCupEmpty is IF_CUP_CURRENT's documented empty read: no cup program
// is produced by any development service, so no cup is live or scheduled.
func currentCupEmpty(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return
	}
	writeNetworkEnvelope(w, r, "CupCurrentResponse", map[string]any{"cup": nil}, "complete", "public, max-age=30")
}

// cupNotFound answers the per-cup reads: no cup is published, and unknown
// cups return 404 without existence disclosure.
func cupNotFound(w http.ResponseWriter, r *http.Request) {
	rest.WriteV2APIError(w, r, 404, "NOT_FOUND")
}
