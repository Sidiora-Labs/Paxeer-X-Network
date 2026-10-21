package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func RegisterReplayGateway(r chi.Router, gatewayOrigin string) error {
	target, err := url.Parse(gatewayOrigin)
	if err != nil || target.Host == "" || target.User != nil || target.RawPath != "" || target.RawQuery != "" || target.Fragment != "" || (target.Path != "" && target.Path != "/") || (target.Scheme != "https" && target.Scheme != "http") {
		return fmt.Errorf("invalid replay gateway origin")
	}
	if target.Scheme == "http" && target.Hostname() != "127.0.0.1" && target.Hostname() != "localhost" && target.Hostname() != "::1" {
		return fmt.Errorf("replay gateway requires HTTPS outside loopback")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 32, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = target.Host
		for key := range req.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-actor-") || strings.HasPrefix(strings.ToLower(key), "x-forwarded-") || strings.EqualFold(key, "Forwarded") || strings.EqualFold(key, "X-Real-IP") || strings.EqualFold(key, "X-Trusted-Principal") {
				req.Header.Del(key)
			}
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		historyError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "durable candle replay unavailable", true)
	}
	r.Get("/v2/snapshots", proxy.ServeHTTP)
	r.Get("/v2/stream", proxy.ServeHTTP)
	return nil
}
