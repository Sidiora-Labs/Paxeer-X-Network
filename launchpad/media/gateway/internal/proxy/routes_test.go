package proxy

import (
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExplicitInventoryRejectsUnknownAdminAndTraversal(t *testing.T) {
	h, e := NewInventoried(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "/social", []Route{{"GET", "/pools/{pool}/messages"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		path string
		want int
	}{{"/social/pools/abc/messages", 204}, {"/social/admin", 404}, {"/social/unknown", 404}, {"/social/pools/%2e%2e/messages", 404}, {"/social/pools/a/messages/extra", 404}} {
		r := httptest.NewRequest("GET", v.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != v.want {
			t.Fatal(v.path, w.Code)
		}
	}
}

func TestAllTrustedHeadersStripped(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"X-Actor-Role", "X-Roles", "X-Internal-Principal", "Authorization", "Cookie", "X-API-Key"} {
			if r.Header.Get(h) != "" {
				t.Errorf("forwarded %s", h)
			}
		}
		if r.Header.Get("X-Actor-Wallet") != "verified" {
			t.Error("verified actor absent")
		}
		w.WriteHeader(204)
	}))
	defer up.Close()
	p := newProxy(t, up.URL)
	r := httptest.NewRequest("GET", "/social/pools/abc/messages", nil)
	for _, h := range []string{"X-Actor-Wallet", "X-Actor-Role", "X-Roles", "X-Internal-Principal", "Authorization", "Cookie", "X-API-Key"} {
		r.Header.Set(h, "forged")
	}
	r = r.WithContext(auth.WithActor(r.Context(), "verified"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestDynamicRootCannotExposeAdmin(t *testing.T) {
	h, e := NewInventoried(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "/social", []Route{{"GET", "/{id}"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/social/admin", "/social/ADMIN", "/social/%61dmin"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
}
