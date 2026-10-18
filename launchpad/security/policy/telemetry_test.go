package policy

import (
	"bytes"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActualSecurityLogSecretRedaction(t *testing.T) {
	var output bytes.Buffer
	logger := &SecurityLog{Writer: &output, Key: []byte(strings.Repeat("k", 32))}
	headers := http.Header{}
	for _, name := range []string{"Authorization", "Cookie", "X-API-Key", "X-Signature", "X-Stream-Key"} {
		headers.Set(name, "unique-secret-"+name)
	}
	if err := logger.Write("request-opaque", "session-secret", "operator", "deny", "v2", headers); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "unique-secret") || strings.Contains(output.String(), "session-secret") {
		t.Fatal("secret serialized")
	}
	if err := logger.Write("request", "session", "wallet-address", "allow", "v2", headers); err == nil {
		t.Fatal("unbounded route label accepted")
	}
}

func TestProtectedHeaderCanaryCannotLeakThroughRequestID(t *testing.T) {
	var output bytes.Buffer
	logger := &SecurityLog{Writer: &output, Key: []byte(strings.Repeat("k", 32))}
	headers := http.Header{}
	canary := "unique-secret-Authorization"
	headers.Set("Authorization", canary)
	if err := logger.Write(canary, canary, "operator", "deny", "v3", headers); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), canary) {
		t.Fatal("request ID reflected protected secret")
	}
}

func TestNativeLimiterCaptureUsesActualBackendOutcome(t *testing.T) {
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: filepath.Join(t.TempDir(), "absent-redis.sock"), MaxRetries: -1, DialTimeout: time.Millisecond})
	defer client.Close()
	file, err := os.CreateTemp(t.TempDir(), "capture-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	capture := &MiddlewareCapture{Writer: file, Key: []byte(strings.Repeat("k", 32)), RunID: "actual-run", ReplicaID: "replica-a", CandidateSHA256: strings.Repeat("a", 64), PolicyVersion: "v4"}
	handler, err := MeasuredAdmission(AdmissionOptions{Redis: client, Namespace: "l45-phase4", Class: "public-read", Cost: 1}, http.NotFoundHandler(), capture)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/market", nil)
	request.Header.Set("X-Request-ID", "secret-canary-request-id")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 503 {
		t.Fatal("actual unavailable backend admission", recorder.Code)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "secret-canary") {
		t.Fatal("raw request secret captured")
	}
	var record MiddlewareRecord
	if err = json.Unmarshal(bytes.TrimSpace(wire), &record); err != nil {
		t.Fatal(err)
	}
	if record.Allowed || !record.BackendUnavailable || record.Stage != "limiter" || record.DurationNS <= 0 || record.ObservedUTCNS <= 0 || !digestPattern.MatchString(record.RequestReference) {
		t.Fatal("native timing/outcome absent", record)
	}
}

func TestMeasuredAdmissionCaptureFailureDeniesRequest(t *testing.T) {
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: filepath.Join(t.TempDir(), "absent-redis.sock"), MaxRetries: -1, DialTimeout: time.Millisecond})
	defer client.Close()
	file, err := os.CreateTemp(t.TempDir(), "closed-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	capture := &MiddlewareCapture{Writer: file, Key: []byte(strings.Repeat("k", 32)), RunID: "run", ReplicaID: "replica", CandidateSHA256: strings.Repeat("a", 64), PolicyVersion: "v4"}
	handler, err := MeasuredAdmission(AdmissionOptions{Redis: client, Namespace: "l45-phase4", Class: "public-read", Cost: 1}, http.NotFoundHandler(), capture)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/market", nil)
	request.Header.Set("X-Request-ID", "request-id")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "capture_unavailable") {
		t.Fatal("failed native capture accepted", recorder.Code)
	}
}
