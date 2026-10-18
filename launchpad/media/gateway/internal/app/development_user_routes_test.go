package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
)

// userObservation is what the media/user upstream saw for one forwarded
// request, including the wallet it resolved by re-authenticating the
// forwarded bearer against the gateway's real identity introspection.
type userObservation struct {
	method, path, authorization, actor, cookie, body, sessionWallet string
	authErr                                                         error
}

// The development gateway forwards the web profile editor's
// PATCH /v3/profiles/{wallet} to media/user (USER_HTTP_URL) behind a session,
// passing the caller's bearer through so media/user can verify it with the
// identity authority itself, while client-forged identity headers and cookies
// never cross.
func TestDevelopmentUserProfileWriteProxy(t *testing.T) {
	var (
		mu       sync.Mutex
		seen     []userObservation
		gateway  *devGateway
		ready    = make(chan struct{})
		response = `{"requestId":"r","data":{"profile":{}}}`
	)
	user := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-ready
		raw, _ := io.ReadAll(r.Body)
		o := userObservation{method: r.Method, path: r.URL.Path, authorization: r.Header.Get("Authorization"), actor: r.Header.Get("X-Actor-Wallet"), cookie: r.Header.Get("Cookie"), body: string(raw)}
		// media/user's development write authenticates with exactly this
		// shared/auth client over the private network.
		authority := sharedauth.HTTPIdentityAuthority{Endpoint: "http://gateway.railway.internal:8080", ServiceToken: gateway.serviceToken, AllowPlainHTTP: true, Client: gateway.privateNetwork()}
		principal, err := authority.AuthenticateRequest(context.Background(), r)
		o.sessionWallet, o.authErr = principal.Wallet, err
		mu.Lock()
		seen = append(seen, o)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"etag-from-user"`)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(user.Close)
	gateway = bootDevIdentityWith(t, 1000, func(cfg *config.Config) { cfg.UserHTTPURL = user.URL })
	close(ready)
	wallet, token, _ := signIn(t, gateway)
	last := func() (userObservation, int) {
		mu.Lock()
		defer mu.Unlock()
		if len(seen) == 0 {
			return userObservation{}, 0
		}
		return seen[len(seen)-1], len(seen)
	}

	body := map[string]any{"expectedRevision": "0", "displayName": "Ada"}
	resp, raw := send(t, http.MethodPatch, gateway.base+"/v3/profiles/"+wallet, body, map[string]string{"Origin": devOrigin})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous PATCH = %d %s", resp.StatusCode, raw)
	}
	if _, n := last(); n != 0 {
		t.Fatalf("anonymous PATCH reached media/user")
	}
	resp, _ = send(t, http.MethodPatch, gateway.base+"/v3/profiles/"+wallet, body, map[string]string{"Origin": devOrigin, "Authorization": "Bearer not-a-session"})
	if _, n := last(); resp.StatusCode != http.StatusUnauthorized || n != 0 {
		t.Fatalf("forged bearer = %d (upstream calls %d)", resp.StatusCode, n)
	}

	resp, raw = send(t, http.MethodPatch, gateway.base+"/v3/profiles/"+wallet, body, map[string]string{
		"Origin": devOrigin, "Authorization": "Bearer " + token, "If-Match": `"0"`, "Idempotency-Key": "4b0d5c1e-2f8a-4e57-9a3c-6d2e8f1a7b90",
		"X-Actor-Wallet": "0x000000000000000000000000000000000000dead", "Cookie": "__Host-kindle_access=stolen",
	})
	if resp.StatusCode != http.StatusOK || string(raw) != response {
		t.Fatalf("authenticated PATCH = %d %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("ETag") != `"etag-from-user"` {
		t.Fatalf("ETag not passed through: %v", resp.Header)
	}
	if got := resp.Header.Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != devOrigin {
		t.Fatalf("CORS allow-origin %v", got)
	}
	o, n := last()
	if n != 1 || o.method != http.MethodPatch || o.path != "/v3/profiles/"+wallet {
		t.Fatalf("upstream saw %+v (%d calls)", o, n)
	}
	if o.authorization != "Bearer "+token || o.cookie != "" || o.actor != wallet {
		t.Fatalf("forwarded identity headers: authorization=%t cookie=%q actor=%q", o.authorization == "Bearer "+token, o.cookie, o.actor)
	}
	var forwarded map[string]any
	if json.Unmarshal([]byte(o.body), &forwarded) != nil || forwarded["displayName"] != "Ada" || forwarded["expectedRevision"] != "0" {
		t.Fatalf("forwarded body %s", o.body)
	}
	if o.authErr != nil || o.sessionWallet != wallet {
		t.Fatalf("media/user could not verify the forwarded session: wallet=%q err=%v", o.sessionWallet, o.authErr)
	}

	// Only PATCH is routed to media/user; reads stay on media/user's public
	// host and are not served by the development gateway.
	resp, _ = send(t, http.MethodGet, gateway.base+"/v3/profiles/"+wallet, nil, map[string]string{"Authorization": "Bearer " + token})
	if _, n := last(); n != 1 || resp.StatusCode == http.StatusOK {
		t.Fatalf("GET /v3/profiles reached media/user through the gateway: %d", resp.StatusCode)
	}
}
