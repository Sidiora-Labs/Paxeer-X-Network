package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
)

// Edge ingress: the session channel authenticates every write and asset
// read with the identity service's request introspection (the real gateway
// binary here), and passes public reads through.
func TestProductionSessionChannelWithRealIdentityService(t *testing.T) {
	_, dsn := internaltest.NewPostgresURL(t)
	redisURL := internaltest.NewRedisURL(t)
	objects := internaltest.NewMinIO(t, "metadata-identity")
	origin := "https://web.example"
	gateway := internaltest.NewGateway(t, dsn, redisURL, objects, origin)
	_, access := gateway.SignIn(t)

	reached := 0
	serve := func(identity RequestIdentityAuthority) http.Handler {
		router := chi.NewRouter()
		router.Use(SessionChannel(identity))
		router.HandleFunc("/*", func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusNoContent) })
		return router
	}
	call := func(h http.Handler, method, path, bearer string) int {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader("body"))
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent && (recorder.Header().Get("Cache-Control") != "no-store" || !strings.Contains(recorder.Body.String(), "media_channel_unavailable")) {
			t.Fatalf("%s %s: denial shape %d %q", method, path, recorder.Code, recorder.Body)
		}
		return recorder.Code
	}
	token := "/v2/tokens/0x" + strings.Repeat("a", 40)
	asset := token + "/assets/11111111-1111-4111-8111-111111111111"
	live := serve(sharedauth.HTTPIdentityAuthority{Endpoint: gateway.URL, ServiceToken: gateway.ServiceToken, AllowPlainHTTP: true})
	for _, c := range []struct {
		method, path, bearer string
		want                 int
	}{
		{http.MethodGet, token + "/metadata", "", http.StatusNoContent},
		{http.MethodGet, "/metadata/batch", "", http.StatusNoContent},
		{http.MethodGet, "/logo/0x" + strings.Repeat("a", 40) + ".png", "", http.StatusNoContent},
		{http.MethodPut, asset, "", http.StatusForbidden},
		{http.MethodGet, asset, "", http.StatusForbidden},
		{http.MethodPost, "/v3" + token + "/metadata-publications", "", http.StatusForbidden},
		{http.MethodPut, asset, "not-a-session-token", http.StatusForbidden},
		{http.MethodPut, asset, access, http.StatusNoContent},
		{http.MethodGet, asset, access, http.StatusNoContent},
		{http.MethodPost, "/v3" + token + "/metadata-publications", access, http.StatusNoContent},
	} {
		if got := call(live, c.method, c.path, c.bearer); got != c.want {
			t.Fatalf("%s %s (session=%v): %d, want %d", c.method, c.path, c.bearer != "", got, c.want)
		}
	}
	if reached != 6 {
		t.Fatalf("handler reached %d times, want 6", reached)
	}
	// An identity service that does not accept this service's credential
	// never lets a write through.
	wrong := serve(sharedauth.HTTPIdentityAuthority{Endpoint: gateway.URL, ServiceToken: strings.Repeat("x", 64), AllowPlainHTTP: true})
	if got := call(wrong, http.MethodPut, asset, access); got == http.StatusNoContent {
		t.Fatal("write admitted through a rejected service credential")
	}
	// Unreachable or absent authority: 503, never a pass.
	down := serve(sharedauth.HTTPIdentityAuthority{Endpoint: "http://127.0.0.1:9", ServiceToken: gateway.ServiceToken, AllowPlainHTTP: true})
	if got := call(down, http.MethodPut, asset, access); got != http.StatusServiceUnavailable {
		t.Fatalf("unreachable authority: %d", got)
	}
	if got := call(serve(nil), http.MethodPut, asset, access); got != http.StatusServiceUnavailable {
		t.Fatalf("absent authority: %d", got)
	}

	// Publication identity confirmation (the publication worker's ConfirmTx)
	// through the same identity service's session check.
	authority := sharedauth.HTTPIdentityAuthority{Endpoint: gateway.URL, ServiceToken: gateway.ServiceToken, AllowPlainHTTP: true}
	request := httptest.NewRequest(http.MethodPut, asset, nil)
	request.Header.Set("Authorization", "Bearer "+access)
	principal, e := authority.AuthenticateRequest(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	identities := SessionCheckIdentities{Authority: sharedauth.HTTPStreamAuthority(authority)}
	if !identities.ReadyFor(nil) {
		t.Fatal("configured session check reported unready")
	}
	confirmed, e := identities.ConfirmTx(context.Background(), nil, principal)
	if e != nil || confirmed != principal {
		t.Fatal("live session not confirmed:", e)
	}
	for name, mutate := range map[string]func(*sharedauth.IdentityPrincipal){
		"rotated epoch":   func(p *sharedauth.IdentityPrincipal) { p.AuthEpoch++ },
		"other wallet":    func(p *sharedauth.IdentityPrincipal) { p.Wallet = "0x" + strings.Repeat("b", 40) },
		"unknown session": func(p *sharedauth.IdentityPrincipal) { p.SessionID = "11111111-1111-4111-8111-111111111111" },
	} {
		altered := principal
		mutate(&altered)
		if _, e := identities.ConfirmTx(context.Background(), nil, altered); !errors.Is(e, sharedauth.ErrScopeDenied) {
			t.Fatalf("%s: %v", name, e)
		}
	}
	unreachable := SessionCheckIdentities{Authority: sharedauth.HTTPStreamAuthority{Endpoint: "http://127.0.0.1:9", ServiceToken: gateway.ServiceToken, AllowPlainHTTP: true}}
	var upstream *sharedauth.VerificationError
	if _, e := unreachable.ConfirmTx(context.Background(), nil, principal); !errors.As(e, &upstream) || !upstream.Retryable {
		t.Fatal("unreachable identity service must be a retryable upstream failure:", e)
	}
	if (SessionCheckIdentities{}).ReadyFor(nil) {
		t.Fatal("unconfigured session check reported ready")
	}
}
