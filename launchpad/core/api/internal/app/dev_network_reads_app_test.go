package app_test

import (
	"io"
	"net/http"
	"testing"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The development app mounts the deployment manifest, cups and the frozen
// not-served operations on the REST group, so none of them falls through to
// the router's plain-text 404 any more (Railway audit D5).
func TestApp_DevelopmentNetworkReadsWired(t *testing.T) {
	srv, _, _ := newApp(t)
	for _, c := range []struct {
		method, path, document string
		status                 int
	}{
		{http.MethodGet, "/v2/network/deployment", "DeploymentResponse", 200},
		{http.MethodGet, "/v2/cups/current", "CupCurrentResponse", 200},
		{http.MethodGet, "/v2/cups/c1", "Error", 404},
		{http.MethodGet, "/v2/me/orders?status=active&limit=100", "Error", 503},
		{http.MethodPost, "/v2/me/orders/0f6b6c52-3f0e-4c38-9a59-2b2b3f6f8c11:cancel", "Error", 503},
		{http.MethodPost, "/v2/me/alerts", "Error", 503},
		{http.MethodGet, "/v2/me/notifications", "Error", 503},
		{http.MethodGet, "/v2/discovery/notifications", "Error", 503},
		{http.MethodGet, "/v2/funding/rails", "Error", 503},
		{http.MethodGet, "/v2/narratives", "Error", 503},
		{http.MethodGet, "/v2/events/stream", "Error", 503},
		// newApp has no NETWORK_RPC_URL: not configured, never a bare 404.
		{http.MethodGet, "/v2/transactions/0xabababababababababababababababababababababababababababababababab", "Error", 503},
	} {
		req, err := http.NewRequest(c.method, srv.URL+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != c.status || resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s = %d (%s), want %d: %s", c.method, c.path, resp.StatusCode, resp.Header.Get("Content-Type"), c.status, body)
		}
		if err = interfaces.ValidateJSON(c.document, body); err != nil {
			t.Fatalf("%s %s is not a valid %s: %v\n%s", c.method, c.path, c.document, err, body)
		}
	}
}
