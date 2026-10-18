package http

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// requireEnvelope validates a recorded body against the shared v2 Error JSON
// schema, checks it carries exactly the contract's seven properties, and
// decodes it.
func requireEnvelope(t *testing.T, name string, rec *httptest.ResponseRecorder, status int, code string) v2.Error {
	t.Helper()
	raw := rec.Body.Bytes()
	if rec.Code != status {
		t.Fatalf("%s: status = %d, want %d (%s)", name, rec.Code, status, raw)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s: Content-Type = %q", name, ct)
	}
	if err := v2.ValidateJSON("Error", raw); err != nil {
		t.Fatalf("%s: body violates the v2 Error schema: %v (%s)", name, err, raw)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "code,details,fieldErrors,message,requestId,retryAfterMs,retryable" {
		t.Fatalf("%s: envelope keys = %v (%s)", name, got, raw)
	}
	if _, legacy := keys["statusCode"]; legacy {
		t.Fatalf("%s: legacy statusCode body: %s", name, raw)
	}
	var e v2.Error
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	if e.Code != code {
		t.Fatalf("%s: code = %q, want %q (%s)", name, e.Code, code, raw)
	}
	return e
}

func requireFieldRule(t *testing.T, name string, e v2.Error, rule string) {
	t.Helper()
	if len(e.FieldErrors) != 1 || e.FieldErrors[0].Pointer != "" || e.FieldErrors[0].Rule != rule {
		t.Fatalf("%s: fieldErrors = %+v, want one root-pointer %q rule", name, e.FieldErrors, rule)
	}
}

func TestRateLimitAnswersRateLimitedEnvelope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		window     time.Duration
		header     string
		retryAfter int64
	}{
		// Retry-After stays window+1 s; retryAfterMs is clamped to the schema maximum.
		{time.Minute, "61", 60000},
		{5 * time.Second, "6", 6000},
	} {
		mw := RateLimit(RateLimitOptions{Max: 1, Window: tc.window})
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		var rec *httptest.ResponseRecorder
		for i := 0; i < 2; i++ {
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "192.0.2.10:1234"
			h.ServeHTTP(rec, req)
		}
		e := requireEnvelope(t, "rate limited", rec, http.StatusTooManyRequests, "RATE_LIMITED")
		if !e.Retryable || e.RetryAfterMs == nil || int64(*e.RetryAfterMs) != tc.retryAfter || len(e.FieldErrors) != 0 || e.Details != nil {
			t.Fatalf("window %s: envelope = %+v", tc.window, e)
		}
		if rec.Header().Get("Retry-After") != tc.header || rec.Header().Get("X-RateLimit-Limit") != "1" || rec.Header().Get("X-RateLimit-Remaining") != "0" {
			t.Fatalf("window %s: headers = %v", tc.window, rec.Header())
		}
	}
}

func TestRateLimitInvalidPrincipalAndBudgetOutageEnvelopes(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	RateLimit(RateLimitOptions{KeyFunc: func(*http.Request) string { return "" }})(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	e := requireEnvelope(t, "invalid principal", rec, http.StatusBadRequest, "FIELD_INVALID")
	requireFieldRule(t, "invalid principal", e, "principal")
	if e.Retryable || e.RetryAfterMs != nil {
		t.Fatalf("invalid principal retryable: %+v", e)
	}

	// A configured Redis that refuses connections fails closed with 503.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	client := goredis.NewClient(&goredis.Options{Addr: addr, MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })
	rec = httptest.NewRecorder()
	RateLimit(RateLimitOptions{Redis: client})(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	e = requireEnvelope(t, "budget outage", rec, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
	if !e.Retryable || e.RetryAfterMs != nil || e.Details == nil || !e.Details.Dependency.Present || e.Details.Dependency.Value != "request_budget" {
		t.Fatalf("budget outage envelope = %+v", e)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatalf("budget outage set Retry-After: %v", rec.Header())
	}
}

func TestNotFoundAndMethodNotAllowedEnvelopes(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	NotFoundHandler()(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	e := requireEnvelope(t, "not found", rec, http.StatusNotFound, "NOT_FOUND")
	if e.Retryable || e.RetryAfterMs != nil || len(e.FieldErrors) != 0 {
		t.Fatalf("not found envelope = %+v", e)
	}
	rec = httptest.NewRecorder()
	MethodNotAllowedHandler()(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
	e = requireEnvelope(t, "method not allowed", rec, http.StatusMethodNotAllowed, "NOT_FOUND")
	requireFieldRule(t, "method not allowed", e, "method")
}

func TestWriteRequestBodyErrorEnvelopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		rule   string
	}{
		{http.StatusBadRequest, RuleJSON},
		{http.StatusBadRequest, RuleMultipart},
		{http.StatusRequestEntityTooLarge, RuleMaxBytes},
		{http.StatusUnsupportedMediaType, RuleMediaType},
	} {
		rec := httptest.NewRecorder()
		WriteRequestBodyError(rec, tc.status, tc.rule, "request body rejected")
		e := requireEnvelope(t, tc.rule, rec, tc.status, "FIELD_INVALID")
		requireFieldRule(t, tc.rule, e, tc.rule)
		if e.Retryable || e.RetryAfterMs != nil || e.Message != "request body rejected" {
			t.Fatalf("%s envelope = %+v", tc.rule, e)
		}
	}
}

func TestWriteErrorEnvelopeRequestIDAndBounds(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-ID", "0b7f3c1e-5d2a-4c7b-9e8f-1a2b3c4d5e6f")
	WriteErrorEnvelope(rec, http.StatusTooManyRequests, CodeRateLimited, strings.Repeat("é", 300), true, 10*time.Minute, nil, nil)
	e := requireEnvelope(t, "bounds", rec, http.StatusTooManyRequests, "RATE_LIMITED")
	if e.RequestId != "0b7f3c1e-5d2a-4c7b-9e8f-1a2b3c4d5e6f" || *e.RetryAfterMs != 60000 || len([]rune(e.Message)) != 256 {
		t.Fatalf("bounds envelope = %+v", e)
	}
	rec = httptest.NewRecorder()
	rec.Header().Set("X-Request-ID", "host/abc-000001")
	WriteErrorEnvelope(rec, http.StatusNotFound, CodeNotFound, "x", false, 0, nil, nil)
	if e := requireEnvelope(t, "non-uuid request id", rec, http.StatusNotFound, "NOT_FOUND"); e.RequestId == "host/abc-000001" {
		t.Fatalf("non-UUID request id reused: %+v", e)
	}
}

// The schema check above is meaningful: the legacy body and an envelope with
// an out-of-range retryAfterMs both fail it.
func TestErrorSchemaRejectsLegacyAndOutOfRangeBodies(t *testing.T) {
	t.Parallel()
	legacy, _ := json.Marshal(ErrorResponse{StatusCode: 429, Error: "Too Many Requests", Message: "rate limit exceeded"})
	if err := v2.ValidateJSON("Error", legacy); err == nil {
		t.Fatalf("legacy body passed the v2 Error schema: %s", legacy)
	}
	over := `{"code":"RATE_LIMITED","details":null,"fieldErrors":[],"message":"x","requestId":"0b7f3c1e-5d2a-4c7b-9e8f-1a2b3c4d5e6f","retryAfterMs":61000,"retryable":true}`
	if err := v2.ValidateJSON("Error", []byte(over)); err == nil {
		t.Fatal("retryAfterMs 61000 passed the v2 Error schema")
	}
}
