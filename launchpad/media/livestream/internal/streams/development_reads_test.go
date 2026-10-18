package streams

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// The production wiring without a read store passes no BroadcastUnprovisioned
// flag; its dependency failures are the same v2 envelope as every runtime.
func TestProductionWiringAnswersDependencyUnavailableEnvelope(t *testing.T) {
	router := chi.NewRouter()
	New(Deps{}).RegisterRoutes(router)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v2/streams/live", ""},
		{http.MethodGet, "/v2/streams/11111111-1111-4111-8111-111111111111", ""},
		{http.MethodPost, "/v2/streams/11111111-1111-4111-8111-111111111111/go-live", `{"revision":"1"}`},
		{http.MethodPost, "/streams", `{}`},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		body := requireEnvelope(t, c.method+" "+c.path, rec, 503, "DEPENDENCY_UNAVAILABLE")
		if !body.Retryable || body.Details == nil || body.Details.Dependency.Value != "livestream_store" {
			t.Fatalf("%s %s: %s", c.method, c.path, rec.Body.String())
		}
	}
}

// requireEnvelope asserts status, a schema-valid v2 Error body and its code.
func requireEnvelope(t *testing.T, name string, rec *httptest.ResponseRecorder, status int, code string) v2.Error {
	t.Helper()
	if e := v2.ValidateJSON("Error", rec.Body.Bytes()); e != nil {
		t.Fatalf("%s: %s %v", name, rec.Body.String(), e)
	}
	var body v2.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != status || body.Code != code || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%s: %d %s want %d %s", name, rec.Code, rec.Body.String(), status, code)
	}
	return body
}

func TestContractErrorsValidateAgainstSharedSchema(t *testing.T) {
	// One writer: the unprovisioned (development/platform) runtime and the
	// provisioned (connected) runtime answer the same status, code and
	// retryability for every lifecycle failure. State and client-correctable
	// conditions are 4xx; only dependency failures are 5xx (the gateway
	// collapses every 5xx into a 503).
	for runtime, h := range map[string]*Handlers{"unprovisioned": New(Deps{BroadcastUnprovisioned: true}), "connected": New(Deps{})} {
		for _, c := range []struct {
			err        error
			status     int
			code       string
			retryable  bool
			retryAfter string
		}{
			{ErrDenied, 403, "SCOPE_DENIED", false, ""},
			{ErrInvalid, 422, "FIELD_INVALID", false, ""},
			{ErrConflict, 409, "REVISION_CONFLICT", false, ""},
			{ErrQuota, 429, "RATE_LIMITED", true, "30"},
			{ErrNotProvisioned, 409, BroadcastNotProvisionedCode, false, ""},
			{ErrState, 409, "STATE_CONFLICT", false, ""},
			{ErrViewerToken, 401, "AUTH_EXPIRED", false, ""},
			{pgx.ErrNoRows, 404, "NOT_FOUND", false, ""},
			{ErrUnavailable, 503, "DEPENDENCY_UNAVAILABLE", true, ""},
			{fmt.Errorf("viewer estimate unavailable: %w", ErrUnavailable), 503, "DEPENDENCY_UNAVAILABLE", true, ""},
			{ErrMarketCapUnavailable, 503, "DEPENDENCY_UNAVAILABLE", true, ""},
		} {
			rec := httptest.NewRecorder()
			h.lifecycleError(rec, c.err)
			body := requireEnvelope(t, runtime+" "+c.err.Error(), rec, c.status, c.code)
			if body.Retryable != c.retryable || rec.Header().Get("Retry-After") != c.retryAfter || (c.retryAfter != "") != (body.RetryAfterMs != nil) {
				t.Fatalf("%s %v: %s retry-after %q", runtime, c.err, rec.Body.String(), rec.Header().Get("Retry-After"))
			}
		}
	}
	rec := httptest.NewRecorder()
	New(Deps{BroadcastUnprovisioned: true}).provisioned(func(http.ResponseWriter, *http.Request) { t.Fatal("provider route reached") })(rec, httptest.NewRequest(http.MethodPost, "/v2/streams", nil))
	if rec.Code != 409 || v2.ValidateJSON("Error", rec.Body.Bytes()) != nil || !strings.Contains(rec.Body.String(), BroadcastNotProvisionedCode) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

// Request-body, heartbeat-throttle and provider-webhook failures answer the
// same v2 envelope as lifecycle failures on every runtime.
func TestRequestAndThrottleErrorsUseContractEnvelope(t *testing.T) {
	request := func(contentType, body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v2/streams/x/heartbeat", strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		return r
	}
	var dst struct {
		SessionID string `json:"sessionId"`
	}
	bounded := func(r *http.Request) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { _, _ = boundedBody(w, r, 1024, &dst) }
	}
	decoded := func(r *http.Request) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { decodeJSON(w, r, &dst) }
	}
	encoded := request("application/json", `{}`)
	encoded.Header.Set("Content-Encoding", "gzip")
	secret := strings.Repeat("k", 32)
	webhook := func(key, signature, body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			r := httptest.NewRequest(http.MethodPost, "/webhooks/livepeer", strings.NewReader(body))
			r.Header.Set("Livepeer-Signature", signature)
			_, _ = VerifyProviderWebhook(w, r, key, time.Now(), false)
		}
	}
	for _, c := range []struct {
		name   string
		write  func(http.ResponseWriter)
		status int
		code   string
		rule   string
	}{
		{"bounded text/plain", bounded(request("text/plain", `{}`)), 415, "FIELD_INVALID", ruleMediaType},
		{"bounded missing content type", bounded(request("", `{}`)), 415, "FIELD_INVALID", ruleMediaType},
		{"bounded content encoding", bounded(encoded), 415, "FIELD_INVALID", ruleMediaType},
		{"bounded oversize", bounded(request("application/json", `{"sessionId":"`+strings.Repeat("a", 1024)+`"}`)), 413, "FIELD_INVALID", ruleBodyTooLarge},
		{"bounded invalid utf8", bounded(request("application/json", "{\"sessionId\":\"\xff\"}")), 400, "FIELD_INVALID", ruleUTF8},
		{"bounded syntax", bounded(request("application/json", `{"sessionId":`)), 400, "FIELD_INVALID", ruleJSON},
		{"bounded unknown field", bounded(request("application/json", `{"other":1}`)), 400, "FIELD_INVALID", ruleJSON},
		{"bounded trailing value", bounded(request("application/json", `{} {}`)), 400, "FIELD_INVALID", ruleJSON},
		{"decode oversize", decoded(request("application/json", strings.Repeat(" ", (1<<20)+1))), 413, "FIELD_INVALID", ruleBodyTooLarge},
		{"decode syntax", decoded(request("application/json", `{`)), 400, "FIELD_INVALID", ruleJSON},
		{"webhook signing unavailable", webhook("", "", `{}`), 503, "DEPENDENCY_UNAVAILABLE", ""},
		{"webhook unsigned", webhook(secret, "", `{}`), 401, "AUTH_EXPIRED", ""},
		{"webhook oversize", webhook(secret, "t=1,v1=00", strings.Repeat("x", (1<<20)+1)), 413, "FIELD_INVALID", ruleBodyTooLarge},
	} {
		rec := httptest.NewRecorder()
		c.write(rec)
		body := requireEnvelope(t, c.name, rec, c.status, c.code)
		if c.rule != "" && (len(body.FieldErrors) != 1 || body.FieldErrors[0].Pointer != "" || body.FieldErrors[0].Rule != c.rule || body.Retryable) {
			t.Fatalf("%s: %s", c.name, rec.Body.String())
		}
		if c.rule == "" && len(body.FieldErrors) != 0 {
			t.Fatalf("%s: %s", c.name, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	writeRateLimited(rec, 15, "Viewer heartbeat rate limited")
	body := requireEnvelope(t, "heartbeat throttle", rec, 429, "RATE_LIMITED")
	if !body.Retryable || body.RetryAfterMs == nil || *body.RetryAfterMs != 15000 || rec.Header().Get("Retry-After") != "15" {
		t.Fatalf("heartbeat throttle: %s %q", rec.Body.String(), rec.Header().Get("Retry-After"))
	}
}
