package proxy

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// upstreamResponse parses a raw HTTP/1.1 response the way the gateway's
// transport delivers it.
func upstreamResponse(t *testing.T, raw string) *http.Response {
	t.Helper()
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(raw)), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func upstream5xx(status, contentType, body string, headers ...string) string {
	return "HTTP/1.1 " + status + "\r\nContent-Type: " + contentType + "\r\n" + strings.Join(headers, "") + "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

// TestUpstream5xxCarriesEnvelopeCode: every upstream 5xx becomes a 503; a v2
// Error envelope keeps its code, retry hints and request id while the
// upstream message and details never leave the gateway; anything else is the
// gateway's own upstream_unavailable.
func TestUpstream5xxCarriesEnvelopeCode(t *testing.T) {
	const requestID = "8a5f44d2-8ee0-42c5-b365-cffdd2e91723"
	coded := `{"code":"DEPENDENCY_UNAVAILABLE","message":"Authoritative livestream dependency unavailable","requestId":"` + requestID + `","retryable":true,"retryAfterMs":1000,"fieldErrors":[],"details":{"dependency":"livestream_store"}}`
	rec := httptest.NewRecorder()
	upstreamUnavailable(rec, upstreamResponse(t, upstream5xx("503 Service Unavailable", "application/json; charset=utf-8", coded, "Retry-After: 1\r\n")))
	if rec.Code != 503 || v2.ValidateJSON("Error", rec.Body.Bytes()) != nil || rec.Header().Get("Retry-After") != "1" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("coded 5xx: %d %s %v", rec.Code, rec.Body.String(), rec.Header())
	}
	var e v2.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Code != "DEPENDENCY_UNAVAILABLE" || e.RequestId != requestID || !e.Retryable || e.RetryAfterMs == nil || *e.RetryAfterMs != 1000 || e.Message != upstreamUnavailableMessage || e.Details != nil {
		t.Fatalf("relayed envelope %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "livestream_store") || strings.Contains(rec.Body.String(), "Authoritative") {
		t.Fatalf("internal detail relayed: %s", rec.Body.String())
	}

	// A non-retryable coded condition stays non-retryable, without delay.
	final := `{"code":"FEATURE_UNAVAILABLE","message":"x","requestId":"` + requestID + `","retryable":false,"retryAfterMs":null,"fieldErrors":[],"details":null}`
	rec = httptest.NewRecorder()
	upstreamUnavailable(rec, upstreamResponse(t, upstream5xx("501 Not Implemented", "application/json", final, "Retry-After: 30\r\n")))
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || rec.Code != 503 || e.Code != "FEATURE_UNAVAILABLE" || e.Retryable || e.RetryAfterMs != nil || rec.Header().Get("Retry-After") != "" {
		t.Fatalf("non-retryable 5xx: %d %s", rec.Code, rec.Body.String())
	}

	for name, raw := range map[string]string{
		"legacy body":       upstream5xx("500 Internal Server Error", "application/json", `{"error":"Internal server error"}`),
		"html":              upstream5xx("502 Bad Gateway", "text/html", "<html>bad gateway</html>"),
		"json not json":     upstream5xx("503 Service Unavailable", "text/plain", coded),
		"lowercase code":    upstream5xx("503 Service Unavailable", "application/json", strings.Replace(coded, "DEPENDENCY_UNAVAILABLE", "dependency_unavailable", 1)),
		"missing requestId": upstream5xx("503 Service Unavailable", "application/json", strings.Replace(coded, `"requestId":"`+requestID+`",`, "", 1)),
		"oversized":         upstream5xx("503 Service Unavailable", "application/json", `{"code":"DEPENDENCY_UNAVAILABLE","pad":"`+strings.Repeat("x", 17<<10)+`"}`),
	} {
		rec := httptest.NewRecorder()
		upstreamUnavailable(rec, upstreamResponse(t, raw))
		if rec.Code != 503 || strings.TrimSpace(rec.Body.String()) != `{"code":"upstream_unavailable"}` {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}
