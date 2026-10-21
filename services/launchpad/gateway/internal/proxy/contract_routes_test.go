package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFollowContractRoutesBelongToSocialUpstream(t *testing.T) {
	for _, route := range SocialContractRoutes() {
		if !allowedConnectedRoute("social", route) {
			t.Fatal("social follow route denied", route)
		}
		if allowedConnectedRoute("user", route) {
			t.Fatal("follow route admitted on user upstream", route)
		}
		flipped := route
		flipped.Authenticate, flipped.Optional = !route.Authenticate, !route.Optional
		if allowedConnectedRoute("social", flipped) {
			t.Fatal("follow route admitted with wrong identity policy", flipped)
		}
	}
	for _, route := range []ConnectedRoute{{Method: "GET", Template: "/v2/profiles/{wallet}/followers", Authenticate: true}, {Method: "GET", Template: "/v2/profiles/{wallet}/follow-stats"}, {Method: "PUT", Template: "/v2/me/following/{wallet}"}, {Method: "GET", Template: "/v2/profiles/{wallet}/follow-stats", Optional: true, Authenticate: true}, {Method: "POST", Template: "/v2/me/following/{wallet}", Optional: true}} {
		if allowedConnectedRoute("social", route) {
			t.Fatal("unsafe follow route admitted", route)
		}
	}
	inventory := map[string]ConnectedRoute{}
	for _, route := range SocialConnectedRoutes() {
		inventory[route.Method+" "+route.Template] = route
	}
	for _, route := range SocialContractRoutes() {
		if inventory[route.Method+" "+route.Template] != route {
			t.Fatal("contract route missing from the social inventory", route)
		}
	}
}

func TestProfileReadsStayOnUserUpstream(t *testing.T) {
	for _, route := range []ConnectedRoute{{Method: "GET", Template: "/v2/profiles/{wallet}"}, {Method: "GET", Template: "/v2/profiles:batch"}} {
		if !allowedConnectedRoute("user", route) {
			t.Fatal("public profile read denied", route)
		}
		route.Authenticate = true
		if allowedConnectedRoute("user", route) {
			t.Fatal("public profile read admitted as session-only", route)
		}
	}
	found := map[string]bool{}
	for _, route := range UserConnectedRoutes() {
		if route.Method == http.MethodGet && (route.Template == "/v2/profiles/{wallet}" || route.Template == "/v2/profiles:batch") {
			if route.Authenticate || route.Optional {
				t.Fatal("public profile read requires identity", route)
			}
			found[route.Template] = true
		}
		if route.Template == "/v2/me/following/{wallet}" || route.Template == "/v2/profiles/{wallet}/followers" {
			t.Fatal("follow route still mounted on user upstream", route)
		}
	}
	if len(found) != 2 {
		t.Fatal("profile reads absent from the user inventory", found)
	}
	wallet := "0x1234567890abcdef1234567890abcdef12345678"
	if !connectedMatch("/v2/profiles:batch", "/v2/profiles:batch") || connectedMatch("/v2/profiles/{wallet}", "/v2/profiles:batch") || connectedMatch("/v2/profiles:batch", "/v2/profiles/"+wallet) {
		t.Fatal("batch literal confused with the wallet template")
	}
	if !connectedMatch("/v2/profiles/{wallet}/follow-stats", "/v2/profiles/"+wallet+"/follow-stats") || !connectedMatch("/v2/me/following/{wallet}", "/v2/me/following/"+wallet) {
		t.Fatal("follow templates do not match contract paths")
	}
}

func TestSessionCredentialsDetection(t *testing.T) {
	anonymous := httptest.NewRequest(http.MethodGet, "/v2/profiles/0x1234567890abcdef1234567890abcdef12345678/follow-stats", nil)
	if sessionCredentials(anonymous) {
		t.Fatal("anonymous request treated as a session")
	}
	bearer := anonymous.Clone(anonymous.Context())
	bearer.Header.Set("Authorization", "Bearer token")
	cookie := anonymous.Clone(anonymous.Context())
	cookie.AddCookie(&http.Cookie{Name: "__Host-kindle_access", Value: "token"})
	other := anonymous.Clone(anonymous.Context())
	other.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})
	if !sessionCredentials(bearer) || !sessionCredentials(cookie) || sessionCredentials(other) {
		t.Fatal("session credential detection mismatch")
	}
}

// The web's watchlist calls /v1/me/watchlist on the gateway and media/user's
// account handler serves exactly that surface (GET, PUT, DELETE), so the
// production user inventory must carry it as owner-only account routes.
func TestWatchlistOwnerRoutesStayOnUserUpstream(t *testing.T) {
	want := map[string]bool{"GET /v1/me/watchlist": true, "PUT /v1/me/watchlist/{poolAddress}": true, "DELETE /v1/me/watchlist/{poolAddress}": true}
	for _, route := range UserConnectedRoutes() {
		key := route.Method + " " + route.Template
		if !want[key] {
			continue
		}
		if !route.Authenticate || route.Optional || route.Upload {
			t.Fatal("watchlist route must require the owner session", route)
		}
		if !allowedConnectedRoute("user", route) || allowedConnectedRoute("social", route) {
			t.Fatal("watchlist route admitted on the wrong upstream", route)
		}
		anonymous := route
		anonymous.Authenticate = false
		if allowedConnectedRoute("user", anonymous) {
			t.Fatal("anonymous watchlist route admitted", anonymous)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatal("watchlist routes absent from the user inventory", want)
	}
	if !connectedMatch("/v1/me/watchlist/{poolAddress}", "/v1/me/watchlist/0xce20da50f92d21d8bee497910c782e9aa25b6263") {
		t.Fatal("watchlist template does not match a pool path")
	}
}

// The frozen v2 bounty reads are public social reads; writes stay session-only.
func TestBountyReadsBelongToSocialUpstream(t *testing.T) {
	for _, route := range []ConnectedRoute{{Method: "GET", Template: "/v2/bounties/stats"}, {Method: "GET", Template: "/v2/bounties/{bountyId}"}, {Method: "GET", Template: "/v2/bounties/{bountyId}/submissions"}, {Method: "POST", Template: "/v2/bounties/{bountyId}/submissions", Authenticate: true}} {
		if !allowedConnectedRoute("social", route) {
			t.Fatal("contract bounty route denied", route)
		}
		if allowedConnectedRoute("user", route) || allowedConnectedRoute("metadata", route) {
			t.Fatal("bounty route admitted on the wrong upstream", route)
		}
	}
	if allowedConnectedRoute("social", ConnectedRoute{Method: "POST", Template: "/v2/bounties/{bountyId}/submissions"}) {
		t.Fatal("anonymous bounty submission admitted")
	}
}
