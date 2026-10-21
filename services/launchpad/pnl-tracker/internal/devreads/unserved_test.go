package devreads_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/devreads"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The production-only export, card and share operations answer the frozen
// non-retryable 503 envelope in development instead of a plain-text 404.
func TestUnservedPortfolioOperationsAnswerNotConfigured(t *testing.T) {
	r := chi.NewRouter()
	devreads.Register(r, devreads.Deps{CursorSecret: "x", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/v2/me/portfolio/exports"},
		{http.MethodGet, "/v2/me/portfolio/exports/j1"},
		{http.MethodGet, "/v2/me/portfolio/exports/j1/download"},
		{http.MethodDelete, "/v2/me/portfolio/exports/j1"},
		{http.MethodPost, "/v2/me/portfolio/cards"},
		{http.MethodGet, "/v2/me/portfolio/cards/c1"},
		{http.MethodDelete, "/v2/me/portfolio/cards/c1"},
		{http.MethodPost, "/v3/me/portfolio/shares/preview"},
		{http.MethodPost, "/v3/me/portfolio/shares"},
		{http.MethodGet, "/v3/me/portfolio/shares/s1"},
		{http.MethodDelete, "/v3/me/portfolio/shares/s1"},
	} {
		req, _ := http.NewRequest(op.method, srv.URL+op.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer x")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s = %d: %s", op.method, op.path, resp.StatusCode, raw)
		}
		if err = interfaces.ValidateJSON("Error", raw); err != nil {
			t.Fatalf("%s %s: %v\n%s", op.method, op.path, err, raw)
		}
		var body struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Code != "DEPENDENCY_UNAVAILABLE" || body.Retryable {
			t.Fatalf("%s %s envelope %s", op.method, op.path, raw)
		}
	}
}
