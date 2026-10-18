package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The development app (NodeEnv "test") mounts the history, search and
// discovery reads on the rate-limited REST group over the real store.
func TestApp_DevelopmentHistorySearchDiscovery(t *testing.T) {
	srv, _, pool := newApp(t)
	// metadata.token_metadata and users.user_profiles are written by
	// media/metadata and media/user into the shared development database.
	if _, err := pool.Exec(context.Background(), `
		CREATE SCHEMA metadata;
		CREATE TABLE metadata.token_metadata (token_address varchar(42) PRIMARY KEY NOT NULL, pool_address varchar(42) NOT NULL, name text, symbol text, description text, website text, twitter text, telegram text, discord text, custom_tags text DEFAULT '[]', created_by varchar(42) NOT NULL, created_at bigint NOT NULL, updated_at bigint NOT NULL, decimals integer NOT NULL DEFAULT 6);
		CREATE SCHEMA users;
		CREATE TABLE users.user_profiles (wallet_address varchar(42) PRIMARY KEY NOT NULL, display_name text, bio text, twitter text, telegram text, discord text, website text, created_at bigint NOT NULL, updated_at bigint NOT NULL);`); err != nil {
		t.Fatalf("seed writer schemas: %v", err)
	}
	for _, c := range []struct {
		path, document string
		status         int
	}{
		{"/v2/search?q=doge&type=pool", "SearchResponse", 200},
		{"/v2/search?q=doge&type=profile", "SearchResponse", 200},
		{"/v2/search/expanded?q=doge&type=token", "ConnectedExpandedSearchResponse", 200},
		{"/v2/discovery/feed", "ConnectedDiscoveryFeedPage", 200},
		{"/v2/discovery/leaderboards?start=2026-10-12T00:00:00Z&end=2026-10-13T00:00:00Z", "Error", 503},
		{"/v2/pools/0x1111111111111111111111111111111111111111/trades", "Error", 404},
		{"/v2/pools/0x1111111111111111111111111111111111111111/callouts", "Error", 404},
		{"/v2/discovery/callouts?author=0x1111111111111111111111111111111111111111", "Error", 503},
	} {
		resp, err := http.Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != c.status {
			t.Fatalf("GET %s = %d, want %d: %s", c.path, resp.StatusCode, c.status, body)
		}
		if err = interfaces.ValidateJSON(c.document, body); err != nil {
			t.Fatalf("GET %s is not a valid %s: %v\n%s", c.path, c.document, err, body)
		}
		if strings.HasPrefix(c.path, "/v2/discovery/callouts") {
			// Per-author callout history has no development source: the
			// not-configured envelope, which clients must not poll.
			var e struct {
				Code         string `json:"code"`
				Retryable    bool   `json:"retryable"`
				RetryAfterMS *int   `json:"retryAfterMs"`
			}
			if json.Unmarshal(body, &e) != nil || e.Code != "DEPENDENCY_UNAVAILABLE" || e.Retryable || e.RetryAfterMS != nil || resp.Header.Get("Retry-After") != "" || resp.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("GET %s = %s (Retry-After %q)", c.path, body, resp.Header.Get("Retry-After"))
			}
		}
		if c.status == 503 {
			// Discovery leaderboards need the PnL interval authority, which
			// this deployment does not configure: never worth retrying.
			var failure struct {
				Retryable    bool `json:"retryable"`
				RetryAfterMS *int `json:"retryAfterMs"`
			}
			if json.Unmarshal(body, &failure) != nil || failure.Retryable || failure.RetryAfterMS != nil || resp.Header.Get("Retry-After") != "" {
				t.Fatalf("GET %s must be non-retryable: %s Retry-After=%q", c.path, body, resp.Header.Get("Retry-After"))
			}
		}
	}
}
