package upload

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// TestUpload_ScannerUnavailableFailsClosed503 exercises the production scanner
// (shared/http ScanBuffer against clamd) with no clamd configured and with a
// clamd address that refuses connections: the upload must fail closed with 503,
// log the scanner reason, and never reach metadata.
func TestUpload_ScannerUnavailableFailsClosed503(t *testing.T) {
	// A real TCP endpoint that refuses connections: bind a port, then close it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	refused := func(ctx context.Context, data []byte) sharedhttp.ScanResult {
		return sharedhttp.ScanBuffer(ctx, data, sharedhttp.ScanOptions{Host: "127.0.0.1", Port: port, Timeout: time.Second})
	}
	for name, tc := range map[string]struct {
		scan   Scanner
		reason string
	}{
		"not configured": {nil, "clamav-not-configured"}, // nil selects the production default (clamd via $CLAMAV_HOST)
		"refused":        {refused, "clamav-unreachable"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLAMAV_HOST", "")
			t.Setenv("CLAMAV_PORT", "")
			up := metaUpstream{}
			srv := newMetaUpstream(t, &up)
			var logs bytes.Buffer
			h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: tc.scan, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
			body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "banner", "b.png", "image/png", []byte("\x89PNG\r\n\x1a\nbanner"))
			rec := serve(t, h, testAddr, body, ct)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
			}
			// shared/http WriteError masks every 5xx message on the wire (as in
			// media/metadata and media/user); the scanner reason goes to the log.
			if !strings.Contains(rec.Body.String(), `"statusCode":503`) || strings.Contains(rec.Body.String(), tc.reason) {
				t.Fatalf("body = %s", rec.Body.String())
			}
			if up.called {
				t.Fatal("an unscanned upload was forwarded to media/metadata")
			}
			if !strings.Contains(logs.String(), `"msg":"upload scan unavailable"`) || !strings.Contains(logs.String(), `"reason":"`+tc.reason+`"`) {
				t.Fatalf("scan failure not logged: %s", logs.String())
			}
		})
	}
}
