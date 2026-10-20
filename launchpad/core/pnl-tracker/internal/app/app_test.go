package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// TestAppEndToEnd boots the fully-wired service against real Postgres + Redis and
// drives a signed Swap webhook through to a portfolio read — proving the wiring
// (migrations, consumer, read API, HMAC auth, health) holds together.
func TestAppEndToEnd(t *testing.T) {
	ctx := context.Background()
	dsn, pool := internaltest.NewPostgresWithDSN(t)
	internaltest.EnsureIndexerSchema(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xc',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	redisURL := internaltest.NewRedisURL(t)

	cfg := config.Config{
		BaseEnv: sharedconfig.BaseEnv{
			DatabaseURL: dsn,
			RedisURL:    redisURL,
			LogLevel:    "error",
			Port:        0,
		},
		ReconcileIntervalMS: 60000,
		ReconcileBatchSize:  100,
		PublicBaseURL:       "https://sidiora.fun",
		RewardPerConversion: 1,
		WebhookHMACSecret:   testSecret,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(a.Close)

	// A signed Swap webhook folds a position.
	body := mustJSON(t, map[string]any{"events": []map[string]any{{
		"eventName": "Swap", "blockNumber": 10, "blockTimestamp": 100, "txHash": "0xtx", "logIndex": 0,
		"args": map[string]any{"poolAddress": "0xpool", "sender": "0xbuyer", "isBuy": true,
			"amountIn": "1000000", "amountOut": "1000000000000000000", "price": "1000000", "fee": "0"},
	}}})
	rec := do(t, a.Router, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d (body=%s)", rec.Code, rec.Body.String())
	}

	// The development v3 portfolio surface is registered outside production;
	// with no RPC_URL configured it answers a retryable 503, never 404.
	if v3 := do(t, a.Router, http.MethodGet, "/v3/wallets/0x00000000000000000000000000000000000000a1/portfolio", nil, nil); v3.Code != http.StatusServiceUnavailable {
		t.Fatalf("dev v3 portfolio status = %d (body=%s)", v3.Code, v3.Body.String())
	}

	// The position is now readable through the public API.
	pr := do(t, a.Router, http.MethodGet, "/users/0xbuyer/positions", nil, nil)
	if pr.Code != http.StatusOK {
		t.Fatalf("positions status = %d", pr.Code)
	}
	var resp struct {
		Positions []struct {
			PoolAddress     string `json:"poolAddress"`
			TokenAddress    string `json:"tokenAddress"`
			CurrentHoldings string `json:"currentHoldings"`
		} `json:"positions"`
	}
	if err := json.Unmarshal(pr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Positions) != 1 || resp.Positions[0].TokenAddress != "0xtoken" {
		t.Fatalf("positions = %+v", resp.Positions)
	}

	// Readiness probe is healthy (DB + Redis reachable).
	hr := do(t, a.Router, http.MethodGet, "/health/ready", nil, nil)
	if hr.Code != http.StatusOK {
		t.Fatalf("readiness status = %d (body=%s)", hr.Code, hr.Body.String())
	}
}

func do(t *testing.T, h http.Handler, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func hmacHeaders(secret string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return map[string]string{
		"Content-Type":        "application/json",
		"X-Sidiora-Timestamp": ts,
		"X-Sidiora-Signature": auth.SignWebhook(secret, ts, string(body)),
	}
}

func TestJourneysEnabledBySource(t *testing.T) {
	if !app.JourneysEnabled(config.Config{JourneyConfigDocument: "eyJ2IjoxfQ==", JourneyConfigSHA256: "0000000000000000000000000000000000000000000000000000000000000000"}) {
		t.Fatal("inline journey document must enable journeys")
	}
	if app.JourneysEnabled(config.Config{}) {
		t.Fatal("journeys enabled with neither file nor document")
	}
}
