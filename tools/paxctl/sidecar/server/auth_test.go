package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthnMode(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		want    string
		wantErr string
	}{
		{"unset", "", AuthnModeUnauthenticated, ""},
		{"explicit unauthenticated", "unauthenticated", AuthnModeUnauthenticated, ""},
		{"trusted-header", "trusted-header", AuthnModeTrustedHeader, ""},
		{"case-insensitive", "TRUSTED-HEADER", AuthnModeTrustedHeader, ""},
		{"whitespace tolerated", "  trusted-header  ", AuthnModeTrustedHeader, ""},
		{"typo with underscore", "trusted_header", "", "not recognized"},
		{"missing hyphen", "trustedheader", "", "not recognized"},
		{"future mode not yet supported", "mtls", "", "not recognized"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SEI_SIDECAR_AUTHN_MODE", c.env)
			got, err := AuthnMode()
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("want err containing %q, got nil (mode=%q)", c.wantErr, got)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %q, want substring %q", err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("AuthnMode() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBindAddress(t *testing.T) {
	if got := BindAddress("7777", AuthnModeUnauthenticated); got != ":7777" {
		t.Errorf("unauthenticated: %q, want :7777", got)
	}
	if got := BindAddress("7777", AuthnModeTrustedHeader); got != "127.0.0.1:7777" {
		t.Errorf("trusted-header: %q, want 127.0.0.1:7777", got)
	}
}

func TestTrustedHeaderMiddleware(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := trustedHeaderMiddleware(inner)

	cases := []struct {
		name    string
		path    string
		headers map[string][]string
		want    int
		called  bool
	}{
		{"healthz bypasses (readiness probe)", "/v0/healthz", nil, http.StatusOK, true},
		{"startupz bypasses (startup probe)", "/v0/startupz", nil, http.StatusOK, true},
		{"livez bypasses (liveness probe)", "/v0/livez", nil, http.StatusOK, true},
		{"metrics bypasses (Prometheus scrape)", "/v0/metrics", nil, http.StatusOK, true},
		{"node-id requires auth", "/v0/node-id", nil, http.StatusUnauthorized, false},
		{"missing header on tasks", "/v0/tasks", nil, http.StatusUnauthorized, false},
		{"empty header on tasks", "/v0/tasks", map[string][]string{"X-Remote-User": {""}}, http.StatusUnauthorized, false},
		// Defense against an APPEND-misconfigured proxy: an
		// attacker-supplied value would arrive first.
		{"duplicate header rejected", "/v0/tasks", map[string][]string{"X-Remote-User": {"attacker", "system:serviceaccount:platform:bot"}}, http.StatusUnauthorized, false},
		{"single header passes", "/v0/tasks", map[string][]string{"X-Remote-User": {"system:serviceaccount:platform:bot"}}, http.StatusOK, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			req.Header = http.Header(c.headers)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			if rr.Code != c.want {
				body, _ := io.ReadAll(rr.Body)
				t.Fatalf("status = %d (body: %s), want %d", rr.Code, body, c.want)
			}
			if called != c.called {
				t.Fatalf("inner called = %v, want %v", called, c.called)
			}
		})
	}
}

func TestNewServerAppliesMiddlewareInTrustedHeaderMode(t *testing.T) {
	s := NewServer(":0", nil, t.TempDir(), AuthnModeTrustedHeader)

	t.Run("healthz bypasses", func(t *testing.T) {
		defer func() { _ = recover() }() // handleHealthz panics on nil engine; the auth gate is what we're testing.
		req := httptest.NewRequest(http.MethodGet, "/v0/healthz", nil)
		rr := httptest.NewRecorder()
		s.handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusUnauthorized {
			t.Fatalf("healthz hit 401 unexpectedly: %s", rr.Body.String())
		}
	})

	t.Run("status without header 401s", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/status", nil)
		rr := httptest.NewRecorder()
		s.handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})
}

func TestNewServerSkipsMiddlewareInUnauthenticatedMode(t *testing.T) {
	s := NewServer(":0", nil, t.TempDir(), AuthnModeUnauthenticated)
	if s.handler != s.mux {
		t.Fatal("unauthenticated mode wrapped the mux in middleware")
	}
}

func TestBypassPaths(t *testing.T) {
	got := BypassPaths()
	want := []string{"/v0/healthz", "/v0/livez", "/v0/metrics", "/v0/startupz"}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(got), len(want), got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("path %d: %q, want %q", i, got[i], p)
		}
	}
}
