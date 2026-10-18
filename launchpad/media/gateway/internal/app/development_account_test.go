package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Outside production the gateway serves GET /v2/me/wallet-links from the
// development identity database, and answers the owner operations that have
// no development service with the frozen non-retryable 503 envelope instead
// of a plain-text 404 (Railway audit D5).
func TestDevelopmentOwnerRoutes(t *testing.T) {
	g := bootDevIdentity(t, 1000)
	wallet, token, _ := signIn(t, g)
	bearer := map[string]string{"Authorization": "Bearer " + token, "Origin": devOrigin}

	resp, raw := send(t, http.MethodGet, g.base+"/v2/me/wallet-links", nil, bearer)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wallet-links %d: %s", resp.StatusCode, raw)
	}
	var links struct {
		Data struct {
			Items []struct {
				LinkID, AccountID, Wallet, ChainID, Visibility, State string
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatal(err)
	}
	if len(links.Data.Items) != 1 || links.Data.Items[0].Wallet != wallet || links.Data.Items[0].State != "verified" || links.Data.Items[0].ChainID != "125" {
		t.Fatalf("wallet-links body %s", raw)
	}
	if resp, raw = send(t, http.MethodGet, g.base+"/v2/me/wallet-links", nil, map[string]string{"Origin": devOrigin}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous wallet-links %d: %s", resp.StatusCode, raw)
	}

	notServed := func(method, path string, headers map[string]string) {
		t.Helper()
		resp, raw := send(t, method, g.base+path, map[string]any{}, headers)
		if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Retry-After") != "" {
			t.Fatalf("%s %s = %d %v: %s", method, path, resp.StatusCode, resp.Header, raw)
		}
		if err := interfaces.ValidateJSON("Error", raw); err != nil {
			t.Fatalf("%s %s is not a frozen Error: %v\n%s", method, path, err, raw)
		}
		var envelope struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Code != "DEPENDENCY_UNAVAILABLE" || envelope.Retryable {
			t.Fatalf("%s %s envelope %s", method, path, raw)
		}
	}
	notServed(http.MethodPost, "/v2/me/wallet-links/challenges", bearer)
	notServed(http.MethodPost, "/v2/me/wallet-links/0f6b6c52-3f0e-4c38-9a59-2b2b3f6f8c11:verify", bearer)
	notServed(http.MethodDelete, "/v2/me/wallet-links/0f6b6c52-3f0e-4c38-9a59-2b2b3f6f8c11", bearer)
	notServed(http.MethodGet, "/v1/me/watchlist?limit=50", bearer)
	notServed(http.MethodPut, "/v1/me/watchlist/0x1111111111111111111111111111111111111111", bearer)
	notServed(http.MethodDelete, "/v1/me/watchlist/0x1111111111111111111111111111111111111111", bearer)
	notServed(http.MethodGet, "/v3/me/preferences", bearer)
	notServed(http.MethodPatch, "/v3/me/preferences", bearer)
	// Without USER_HTTP_URL the development profile write has no upstream.
	notServed(http.MethodPatch, "/v3/profiles/"+wallet, bearer)
	// The account answer never depends on the session, so a lapsed one is not
	// turned into a sign-out.
	notServed(http.MethodGet, "/v1/me/watchlist?limit=50", map[string]string{"Origin": devOrigin})
}
