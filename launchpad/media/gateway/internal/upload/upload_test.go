package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

const testAddr = "0x1234567890abcdef1234567890abcdef12345678"

// metaUpstream is a fake media/metadata that records the forwarded request.
type metaUpstream struct {
	path   string
	fields map[string]string
	files  map[string]int // field -> byte length received
	called bool
}

func newMetaUpstream(t *testing.T, rec *metaUpstream) *httptest.Server {
	t.Helper()
	rec.fields = map[string]string{}
	rec.files = map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.called = true
		rec.path = r.URL.Path
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		for k, v := range r.MultipartForm.Value {
			rec.fields[k] = v[0]
		}
		for k, fhs := range r.MultipartForm.File {
			f, _ := fhs[0].Open()
			b, _ := io.ReadAll(f)
			_ = f.Close()
			rec.files[k] = len(b)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildUpload builds a multipart body with the given text fields and image file.
func buildUpload(t *testing.T, fields map[string]string, fileField, filename, mime string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if fileField != "" {
		hdr := map[string][]string{
			"Content-Disposition": {`form-data; name="` + fileField + `"; filename="` + filename + `"`},
			"Content-Type":        {mime},
		}
		part, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		_, _ = part.Write(data)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func serve(t *testing.T, h *Handler, addr string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/upload/token/"+addr, body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func cleanScan(context.Context, []byte) sharedhttp.ScanResult {
	return sharedhttp.ScanResult{Clean: true}
}

func TestUpload_ForwardsValidUpload(t *testing.T) {
	var up metaUpstream
	srv := newMetaUpstream(t, &up)
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: cleanScan})

	png := []byte("\x89PNG\r\n\x1a\nfake-png-bytes")
	body, ct := buildUpload(t, map[string]string{
		"wallet":    testAddr,
		"signature": "0xsig",
		"message":   "sign me",
		"metadata":  `{"name":"Tok"}`,
	}, "logo", "logo.png", "image/png", png)

	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !up.called || up.path != "/metadata/"+testAddr {
		t.Fatalf("upstream path = %q called=%v", up.path, up.called)
	}
	if up.fields["wallet"] != testAddr || up.fields["metadata"] != `{"name":"Tok"}` {
		t.Errorf("forwarded fields = %v", up.fields)
	}
	if up.files["logo"] != len(png) {
		t.Errorf("forwarded logo bytes = %d, want %d", up.files["logo"], len(png))
	}
}

func TestUpload_RejectsBadMime(t *testing.T) {
	var up metaUpstream
	srv := newMetaUpstream(t, &up)
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: cleanScan})

	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "logo", "a.gif", "image/gif", []byte("GIF89a"))
	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if up.called {
		t.Error("upstream should not be called on rejected upload")
	}
}

func TestUpload_RejectsUnsafeSVG(t *testing.T) {
	srv := newMetaUpstream(t, &metaUpstream{})
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: cleanScan})

	svg := []byte(`<svg onload="evil()"></svg>`)
	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "logo", "a.svg", "image/svg+xml", svg)
	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpload_RejectsInfectedFile(t *testing.T) {
	srv := newMetaUpstream(t, &metaUpstream{})
	infected := func(context.Context, []byte) sharedhttp.ScanResult {
		return sharedhttp.ScanResult{Clean: false, Verdict: sharedhttp.ScanInfected, Reason: "Eicar-Test-Signature"}
	}
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: infected})

	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "logo", "a.png", "image/png", []byte("x"))
	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "virus scanner") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestUpload_RejectsInvalidAddr(t *testing.T) {
	srv := newMetaUpstream(t, &metaUpstream{})
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 6 << 20, Scan: cleanScan})
	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "", "", "", nil)
	rec := serve(t, h, "not-an-address", body, ct)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpload_BadGatewayWhenMetadataDown(t *testing.T) {
	srv := newMetaUpstream(t, &metaUpstream{})
	addr := srv.URL
	srv.Close() // upstream unreachable
	h := New(Deps{MetadataBaseURL: addr, MaxBytes: 6 << 20, Scan: cleanScan})

	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "logo", "a.png", "image/png", []byte("\x89PNG"))
	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestUpload_RejectsOversize(t *testing.T) {
	srv := newMetaUpstream(t, &metaUpstream{})
	h := New(Deps{MetadataBaseURL: srv.URL, MaxBytes: 64, Scan: cleanScan}) // 64-byte cap

	big := bytes.Repeat([]byte("A"), 4096)
	body, ct := buildUpload(t, map[string]string{"wallet": testAddr}, "logo", "a.png", "image/png", big)
	rec := serve(t, h, testAddr, body, ct)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if err := v2.ValidateJSON("Error", rec.Body.Bytes()); err != nil {
		t.Fatalf("413 body violates the v2 Error schema: %v (%s)", err, rec.Body.String())
	}
	var e v2.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "FIELD_INVALID" || e.Retryable || len(e.FieldErrors) != 1 || e.FieldErrors[0].Rule != sharedhttp.RuleMaxBytes {
		t.Fatalf("413 envelope = %+v", e)
	}
}
