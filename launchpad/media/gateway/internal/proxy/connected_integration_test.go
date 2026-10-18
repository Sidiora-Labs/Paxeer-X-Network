package proxy

import (
	"testing"
)

func TestConnectedPublicInventoryAndSecretBoundary(t *testing.T) {
	if !allowedConnectedRoute("metadata", ConnectedRoute{Method: "PUT", Template: "/v2/tokens/{tokenAddress}/assets/{assetId}", Authenticate: true, Upload: true}) {
		t.Fatal("actual connected metadata asset route denied")
	}
	for _, route := range []ConnectedRoute{{Method: "PATCH", Template: "/v2/profiles/{wallet}"}, {Method: "GET", Template: "/admin/users", Authenticate: true}, {Method: "GET", Template: "/v2/profiles/*"}} {
		if allowedConnectedRoute("user", route) {
			t.Fatal("unsafe route admitted", route)
		}
	}
	if !connectedMatch("/v2/streams/{streamId}:start", "/v2/streams/8a5f44d2-8ee0-42c5-b365-cffdd2e91723:start") {
		t.Fatal("action suffix lost")
	}
	for _, raw := range []string{`{"data":{"secretAccessKey":"restricted"}}`, `{"data":{"url":"https://bucket.invalid/x?X-Amz-Signature=restricted"}}`, `{"items":[{"providerToken":"restricted"}]}`} {
		if !containsSecret([]byte(raw)) {
			t.Fatal("private output accepted")
		}
	}
	if containsSecret([]byte(`{"data":{"state":"quarantined","publicUrl":null},"revision":"2"}`)) {
		t.Fatal("truthful pending outcome rejected")
	}
}
