package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

type probeBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

func TestHealth(t *testing.T) {
	t.Run("live_ok", func(t *testing.T) {
		mux := http.NewServeMux()
		RegisterHealth(mux, HealthDeps{})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		code, body := get(t, srv.URL+"/healthz/live")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		var got probeBody
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		if got.Status != "ok" {
			t.Fatalf("status field = %q, want ok", got.Status)
		}
	})

	t.Run("ready_reports_failed_check", func(t *testing.T) {
		mux := http.NewServeMux()
		RegisterHealth(mux, HealthDeps{
			Postgres: func(context.Context) error { return errors.New("connection refused") },
			Custom:   map[string]Check{"consumer": func(context.Context) error { return nil }},
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		code, body := get(t, srv.URL+"/healthz/ready")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", code)
		}
		var got probeBody
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		if got.Status != "degraded" {
			t.Fatalf("status field = %q, want degraded", got.Status)
		}
		if got.Checks["postgres"] != "failed" {
			t.Fatalf("checks.postgres = %q, want failed; body %s", got.Checks["postgres"], body)
		}
		if got.Checks["consumer"] != "ok" {
			t.Fatalf("checks.consumer = %q, want ok; body %s", got.Checks["consumer"], body)
		}
	})

	t.Run("metrics_exposition", func(t *testing.T) {
		metrics, err := telemetry.New(config.Gateway)
		if err != nil {
			t.Fatalf("telemetry.New: %v", err)
		}
		mux := http.NewServeMux()
		RegisterHealth(mux, HealthDeps{})
		mux.Handle("GET /metrics", metrics)
		srv := httptest.NewServer(mux)
		defer srv.Close()
		code, _ := get(t, srv.URL+"/healthz/live")
		if err := metrics.RecordRead("/healthz/live", code); err != nil {
			t.Fatalf("RecordRead: %v", err)
		}
		code, body := get(t, srv.URL+"/metrics")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		want := `perps_http_requests_total{route="/healthz/live",status="200"} 1`
		if !strings.Contains(body, want) {
			t.Fatalf("exposition lacks %q:\n%s", want, body)
		}
		if !strings.Contains(body, "# TYPE perps_http_requests_total counter") {
			t.Fatalf("exposition lacks the counter type line:\n%s", body)
		}
	})
}
