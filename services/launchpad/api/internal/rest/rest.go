// Package rest is the thin REST snapshot surface of core/api.
//
// By design the gateway is PUSH-FIRST: live updates ride the WSS/SSE multiplexer
// (internal/ws, internal/sse), never client polling. REST is reserved for the
// frozen v2 bootstrap SNAPSHOTS a client needs once before it starts consuming
// deltas (RegisterProductionReads, RegisterAccountingV2WithAdmission). The
// unversioned legacy routes (/udf/*, /stats/*, /rankings/*, /platform/metrics,
// /bff/token/*) were removed: they had no frozen contract and core/api's
// production roles cannot read the legacy stats tables they served.
package rest

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

func RegisterV2SearchProxy(r chi.Router, origin string) error {
	target, err := url.Parse(origin)
	if err != nil || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Path != "" && target.Path != "/" || (target.Scheme != "https" && target.Scheme != "http") {
		return fmt.Errorf("stats_service_origin_invalid")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: 2 * time.Second, MaxIdleConnsPerHost: 8}
	original := proxy.Director
	proxy.Director = func(req *http.Request) {
		original(req)
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("X-Actor-Wallet")
		req.Header.Del("X-Forwarded-Host")
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		sharedhttp.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "DEPENDENCY_UNAVAILABLE", "message": "Search is unavailable", "retryable": true})
	}
	r.Method(http.MethodGet, "/v2/search", http.TimeoutHandler(proxy, 3*time.Second, "search unavailable"))
	return nil
}
