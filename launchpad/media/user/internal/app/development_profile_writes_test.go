package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/internaltest"
)

const identityServiceToken = "s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7s7"

// identityWire serves the gateway's private v2 identity introspection wire
// (POST /internal/v2/identity/request:authenticate, IDENTITY_SERVICE_TOKEN
// bearer) for a fixed set of browser sessions, the way the development gateway
// answers it over the private network. "Bearer unavailable" models an
// authority outage (503).
func identityWire(t *testing.T, sessions map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v2/identity/request:authenticate" || r.Header.Get("Authorization") != "Bearer "+identityServiceToken {
			http.NotFound(w, r)
			return
		}
		var req sharedauth.IdentityAuthenticationRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 16384))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || sharedauth.ValidateIdentityAuthenticationRequest(req) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Authorization == "Bearer unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		wallet, ok := sessions[req.Authorization]
		if !ok || req.Method != http.MethodPatch {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		now := time.Now().UTC()
		_ = json.NewEncoder(w).Encode(sharedauth.IdentityPrincipal{AccountID: uuid.NewString(), SessionID: uuid.NewString(), Wallet: wallet, AuthEpoch: 1, ProofAt: now.Add(-time.Minute), AccessExpiresAt: now.Add(15 * time.Minute)})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestDevelopmentProfileWrite boots the real development app over real
// Postgres + Redis and drives the web profile editor's write
// (PATCH /v3/profiles/{wallet}, ConnectedExtendedProfileBody) end to end:
// session authentication, ownership, contract validation, optimistic
// concurrency and read-after-write through the development reads.
func TestDevelopmentProfileWrite(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	owner := "0x1111111111111111111111111111111111111111"
	other := "0x2222222222222222222222222222222222222222"
	cfg := config.Config{
		NodeEnv:               "development",
		DatabaseURL:           pool.Config().ConnString(),
		RedisURL:              redisURL,
		S3Endpoint:            "http://127.0.0.1:9",
		S3Bucket:              "user-assets",
		S3AccessKeyID:         "development",
		S3SecretAccessKey:     "development",
		S3Region:              "us-east-1",
		SecretsRefreshSeconds: 30,
		LogLevel:              "info",
		IdentityURL:           identityWire(t, map[string]string{"Bearer owner-session": owner, "Bearer other-session": other}),
		IdentityServiceToken:  identityServiceToken,
	}
	a, err := app.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)

	ownerAuth := map[string]string{"Authorization": "Bearer owner-session", "Origin": "https://kindle.test"}
	do := func(method, path string, body any, header map[string]string) (*http.Response, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, ok := body.([]byte)
			if !ok {
				raw, _ = json.Marshal(body)
			}
			reader = bytes.NewReader(raw)
		}
		req, e := http.NewRequest(method, srv.URL+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatalf("%s %s: %v", method, path, e)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, raw
	}
	failure := func(name string, resp *http.Response, raw []byte, status int, code, pointer string) {
		t.Helper()
		if resp.StatusCode != status || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s = %d %s; want %d", name, resp.StatusCode, raw, status)
		}
		if e := v2.ValidateJSON("Error", raw); e != nil {
			t.Fatalf("%s error envelope: %v: %s", name, e, raw)
		}
		var body struct {
			Code        string `json:"code"`
			FieldErrors []struct {
				Pointer string `json:"pointer"`
			} `json:"fieldErrors"`
		}
		if e := json.Unmarshal(raw, &body); e != nil || body.Code != code {
			t.Fatalf("%s code = %q (%v) %s; want %s", name, body.Code, e, raw, code)
		}
		if pointer != "" && (len(body.FieldErrors) != 1 || body.FieldErrors[0].Pointer != pointer) {
			t.Fatalf("%s fieldErrors = %+v; want %s", name, body.FieldErrors, pointer)
		}
	}
	type presentation struct {
		Wallet      string            `json:"wallet"`
		DisplayName string            `json:"displayName"`
		Bio         string            `json:"bio"`
		Website     *string           `json:"website"`
		Revision    string            `json:"revision"`
		Socials     map[string]string `json:"socials"`
	}
	decode := func(raw []byte) (presentation, string) {
		t.Helper()
		if e := v2.ValidateJSON("ConnectedExtendedProfileResponse", raw); e != nil {
			t.Fatalf("contract: %v: %s", e, raw)
		}
		var body struct {
			Revision string `json:"revision"`
			Data     struct {
				Profile presentation `json:"profile"`
			} `json:"data"`
		}
		if e := json.Unmarshal(raw, &body); e != nil {
			t.Fatal(e)
		}
		return body.Data.Profile, body.Revision
	}
	// patch sends a contract-valid body (checked with the generated
	// validator first) with the web's If-Match and Idempotency-Key headers.
	patch := func(wallet string, body map[string]any, header map[string]string) (*http.Response, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		if e := v2.ValidateJSON("ConnectedExtendedProfileBody", raw); e != nil {
			t.Fatalf("test body is not a ConnectedExtendedProfileBody: %v: %s", e, raw)
		}
		h := map[string]string{"If-Match": `"` + body["expectedRevision"].(string) + `"`, "Idempotency-Key": uuid.NewString()}
		for k, v := range header {
			h[k] = v
		}
		return do(http.MethodPatch, "/v3/profiles/"+wallet, raw, h)
	}
	greater := func(a, b string) bool {
		x, ok1 := new(big.Int).SetString(a, 10)
		y, ok2 := new(big.Int).SetString(b, 10)
		return ok1 && ok2 && x.Cmp(y) > 0
	}

	t.Run("authentication and ownership", func(t *testing.T) {
		body := map[string]any{"expectedRevision": "0", "displayName": "Ada"}
		resp, raw := patch(owner, body, nil)
		failure("no session", resp, raw, http.StatusUnauthorized, "AUTH_EXPIRED", "")
		resp, raw = patch(owner, body, map[string]string{"Authorization": "Bearer forged"})
		failure("unknown session", resp, raw, http.StatusUnauthorized, "AUTH_EXPIRED", "")
		resp, raw = patch(owner, body, map[string]string{"Authorization": "Bearer other-session"})
		failure("other wallet's profile", resp, raw, http.StatusForbidden, "SCOPE_DENIED", "")
		resp, raw = patch(owner, body, map[string]string{"Authorization": "Bearer other-session", "X-Actor-Wallet": owner})
		failure("forwarded actor header is not trusted", resp, raw, http.StatusForbidden, "SCOPE_DENIED", "")
		resp, raw = patch(owner, body, map[string]string{"Authorization": "Bearer unavailable"})
		failure("identity outage", resp, raw, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "")
		// Nothing above wrote: the wallet still has no profile.
		resp, raw = do(http.MethodGet, "/v3/profiles/"+owner, nil, nil)
		failure("profile after rejected writes", resp, raw, http.StatusNotFound, "NOT_FOUND", "")
	})

	var revision string
	t.Run("first save creates the profile at revision 0", func(t *testing.T) {
		resp, raw := patch(owner, map[string]any{
			"expectedRevision": "0", "displayName": "  Ada  ", "bio": "builds markets", "website": "https://ada.example",
			"avatarAssetId": nil, "bannerAssetId": nil,
			"socials": map[string]any{"twitter": "https://x.com/ada", "telegram": "https://t.me/ada", "discord": nil},
		}, ownerAuth)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d %s", resp.StatusCode, raw)
		}
		p, envelopeRevision := decode(raw)
		if p.Wallet != owner || p.DisplayName != "Ada" || p.Bio != "builds markets" || p.Website == nil || *p.Website != "https://ada.example" {
			t.Fatalf("profile = %+v", p)
		}
		if p.Socials["twitter"] != "https://x.com/ada" || p.Socials["telegram"] != "https://t.me/ada" || p.Socials["website"] != "https://ada.example" {
			t.Fatalf("socials = %v", p.Socials)
		}
		if !greater(p.Revision, "0") || envelopeRevision != p.Revision {
			t.Fatalf("revision = %q envelope %q", p.Revision, envelopeRevision)
		}
		if etag := resp.Header.Get("ETag"); etag != `"`+owner+":"+p.Revision+`"` {
			t.Fatalf("ETag = %q", etag)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "private, no-store" {
			t.Fatalf("Cache-Control = %q", cc)
		}
		revision = p.Revision

		resp, raw = do(http.MethodGet, "/v3/profiles/"+owner, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("read after write = %d %s", resp.StatusCode, raw)
		}
		if got, _ := decode(raw); got.DisplayName != "Ada" || got.Revision != revision || got.Socials["twitter"] != "https://x.com/ada" {
			t.Fatalf("read after write = %+v", got)
		}
	})

	t.Run("stale revision and If-Match conflicts", func(t *testing.T) {
		resp, raw := patch(owner, map[string]any{"expectedRevision": "0", "displayName": "Stale"}, ownerAuth)
		failure("stale expectedRevision", resp, raw, http.StatusPreconditionFailed, "REVISION_CONFLICT", "")
		resp, raw = patch(owner, map[string]any{"expectedRevision": revision, "displayName": "Mismatch"}, map[string]string{"Authorization": "Bearer owner-session", "If-Match": `"1"`})
		failure("If-Match disagrees with body", resp, raw, http.StatusPreconditionFailed, "REVISION_CONFLICT", "")
		resp, raw = do(http.MethodGet, "/v3/profiles/"+owner, nil, nil)
		if got, _ := decode(raw); resp.StatusCode != http.StatusOK || got.DisplayName != "Ada" || got.Revision != revision {
			t.Fatalf("conflicts wrote: %+v", got)
		}
	})

	t.Run("contract validation", func(t *testing.T) {
		invalid := func(name string, raw []byte, pointer string) {
			t.Helper()
			if v2.ValidateJSON("ConnectedExtendedProfileBody", raw) == nil && pointer != "/website-shape" {
				t.Fatalf("%s: generated validator accepts %s", name, raw)
			}
			resp, body := do(http.MethodPatch, "/v3/profiles/"+owner, raw, ownerAuth)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("%s = %d %s; want 422", name, resp.StatusCode, body)
			}
			if e := v2.ValidateJSON("Error", body); e != nil {
				t.Fatalf("%s error envelope: %v: %s", name, e, body)
			}
		}
		invalid("displayName over 50", []byte(`{"expectedRevision":"`+revision+`","displayName":"`+strings.Repeat("a", 51)+`"}`), "")
		invalid("bio over 500", []byte(`{"expectedRevision":"`+revision+`","bio":"`+strings.Repeat("b", 501)+`"}`), "")
		invalid("http website", []byte(`{"expectedRevision":"`+revision+`","website":"http://ada.example"}`), "")
		invalid("http social", []byte(`{"expectedRevision":"`+revision+`","socials":{"twitter":"http://x.com/ada"}}`), "")
		invalid("unknown social", []byte(`{"expectedRevision":"`+revision+`","socials":{"github":"https://github.com/ada"}}`), "")
		invalid("unknown field", []byte(`{"expectedRevision":"`+revision+`","avatarUrl":"https://ada.example/a.png"}`), "")
		invalid("missing expectedRevision", []byte(`{"displayName":"Ada"}`), "")
		invalid("non-decimal revision", []byte(`{"expectedRevision":"01","displayName":"Ada"}`), "")
		// A https link the read projection cannot serve back is refused
		// rather than silently dropped.
		invalid("unservable link host", []byte(`{"expectedRevision":"`+revision+`","website":"https://ada_example.com"}`), "/website-shape")

		resp, raw := patch(owner, map[string]any{"expectedRevision": revision, "avatarAssetId": uuid.NewString()}, ownerAuth)
		failure("asset without asset runtime", resp, raw, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "")
	})

	t.Run("edit advances the revision and clears fields", func(t *testing.T) {
		// Prime the legacy cached read so the commit must invalidate it.
		if resp, raw := do(http.MethodGet, "/users/"+owner, nil, nil); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"Ada"`) {
			t.Fatalf("legacy read = %d %s", resp.StatusCode, raw)
		}
		resp, raw := patch(owner, map[string]any{
			"expectedRevision": revision, "displayName": "Ada Lovelace", "bio": "", "website": nil,
			"socials": map[string]any{"twitter": nil, "telegram": "https://t.me/lovelace", "discord": "https://discord.gg/ada"},
		}, map[string]string{"Authorization": "Bearer owner-session", "If-Match": `"` + owner + ":" + revision + `"`})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d %s", resp.StatusCode, raw)
		}
		p, _ := decode(raw)
		if !greater(p.Revision, revision) {
			t.Fatalf("revision did not advance: %q -> %q", revision, p.Revision)
		}
		if p.DisplayName != "Ada Lovelace" || p.Bio != "" || p.Website != nil {
			t.Fatalf("profile = %+v", p)
		}
		if _, ok := p.Socials["twitter"]; ok || p.Socials["telegram"] != "https://t.me/lovelace" || p.Socials["discord"] != "https://discord.gg/ada" {
			t.Fatalf("socials = %v", p.Socials)
		}
		if _, ok := p.Socials["website"]; ok {
			t.Fatalf("cleared website still in socials: %v", p.Socials)
		}
		next := p.Revision

		// Two commits inside one second still advance strictly.
		resp, raw = patch(owner, map[string]any{"expectedRevision": next, "bio": "first programmer"}, ownerAuth)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("immediate re-save = %d %s", resp.StatusCode, raw)
		}
		if again, _ := decode(raw); !greater(again.Revision, next) || again.Bio != "first programmer" || again.DisplayName != "Ada Lovelace" {
			t.Fatalf("immediate re-save = %+v (after %s)", again, next)
		}

		for _, path := range []string{"/v3/profiles/" + owner, "/v2/profiles/" + owner} {
			resp, raw = do(http.MethodGet, path, nil, nil)
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"displayName":"Ada Lovelace"`) || !strings.Contains(string(raw), `"bio":"first programmer"`) {
				t.Fatalf("GET %s after edit = %d %s", path, resp.StatusCode, raw)
			}
		}
		if resp, raw = do(http.MethodGet, "/users/"+owner, nil, nil); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"Ada Lovelace"`) {
			t.Fatalf("legacy read after edit (cache not invalidated?) = %d %s", resp.StatusCode, raw)
		}
	})
}

// Without IDENTITY_SERVICE_URL/TOKEN the development write cannot verify a
// session and answers 503 instead of trusting the caller.
func TestDevelopmentProfileWriteWithoutIdentity(t *testing.T) {
	ctx := context.Background()
	pool := internaltest.NewPostgres(t)
	cfg := config.Config{
		NodeEnv:               "development",
		DatabaseURL:           pool.Config().ConnString(),
		RedisURL:              internaltest.NewRedisURL(t),
		S3Endpoint:            "http://127.0.0.1:9",
		S3Bucket:              "user-assets",
		S3AccessKeyID:         "development",
		S3SecretAccessKey:     "development",
		S3Region:              "us-east-1",
		SecretsRefreshSeconds: 30,
		LogLevel:              "info",
	}
	a, err := app.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v3/profiles/0x1111111111111111111111111111111111111111", strings.NewReader(`{"expectedRevision":"0","displayName":"Ada"}`))
	req.Header.Set("Authorization", "Bearer owner-session")
	req.Header.Set("X-Actor-Wallet", "0x1111111111111111111111111111111111111111")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || v2.ValidateJSON("Error", raw) != nil || !strings.Contains(string(raw), `"DEPENDENCY_UNAVAILABLE"`) {
		t.Fatalf("PATCH without identity = %d %s", resp.StatusCode, raw)
	}
}
