package httpapi

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

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

const (
	whPool   = "0xpool00000000000000000000000000000000ffff"
	whSecret = "test-webhook-secret-at-least-32-chars!!"
)

func webhookRouter(t *testing.T) (*chi.Mux, *store.Store) {
	t.Helper()
	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := goredis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	builder := engine.New(pool, rdb, st, logger)

	r := chi.NewRouter()
	RegisterWebhook(r, WebhookDeps{Builder: builder, Logger: logger, Secret: whSecret})
	return r, st
}

func signedRequest(body []byte) *http.Request {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := auth.SignWebhook(whSecret, ts, string(body))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/events", bytes.NewReader(body))
	req.Header.Set("X-Sidiora-Signature", sig)
	req.Header.Set("X-Sidiora-Timestamp", ts)
	return req
}

func swapBody(blockTS int64) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"events": []map[string]interface{}{{
			"eventName":      "Swap",
			"blockNumber":    100,
			"blockTimestamp": blockTS,
			"txHash":         "0xfeed",
			"logIndex":       0,
			"args": map[string]interface{}{
				"poolAddress": whPool,
				"sender":      "0xtrader",
				"isBuy":       true,
				"amountIn":    "100",
				"amountOut":   "200",
				"fee":         "1",
				"price":       "1000000000000000000",
			},
		}},
	})
	return b
}

func TestWebhookValidSwap(t *testing.T) {
	r, st := webhookRouter(t)
	body := swapBody(1704067260)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, signedRequest(body))

	if rec.Code != 200 {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["processed"].(float64) != 1 {
		t.Errorf("processed = %v, want 1", resp["processed"])
	}

	got, err := st.GetCandle(context.Background(), whPool, "1m", 1704067260)
	if err != nil || got == nil || got.VolumeUsdl != "100" {
		t.Fatalf("candle not folded: %+v err %v", got, err)
	}
}

func TestWebhookMissingSignature(t *testing.T) {
	r, _ := webhookRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/events", bytes.NewReader(swapBody(1704067260)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookBadSignature(t *testing.T) {
	r, _ := webhookRouter(t)
	body := swapBody(1704067260)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/events", bytes.NewReader(body))
	req.Header.Set("X-Sidiora-Signature", "sha256=deadbeef")
	req.Header.Set("X-Sidiora-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookInvalidJSON(t *testing.T) {
	r, _ := webhookRouter(t)
	body := []byte("{not valid json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, signedRequest(body))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestWebhookEventsNotArray(t *testing.T) {
	r, _ := webhookRouter(t)
	body := []byte(`{"other":true}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, signedRequest(body))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400 (events must be array)", rec.Code)
	}
}

func TestWebhookSkipsNonSwapAndBadTimestamp(t *testing.T) {
	r, _ := webhookRouter(t)
	body, _ := json.Marshal(map[string]interface{}{
		"events": []map[string]interface{}{
			{"eventName": "MarketCreated", "blockTimestamp": 1704067260, "txHash": "0x1", "args": map[string]interface{}{}},
			{"eventName": "Swap", "blockTimestamp": 100, "txHash": "0x2", "args": map[string]interface{}{
				"poolAddress": whPool, "sender": "0xt", "isBuy": true,
				"amountIn": "1", "amountOut": "1", "fee": "0", "price": "1000000000000000000",
			}},
		},
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, signedRequest(body))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["processed"].(float64) != 0 {
		t.Errorf("processed = %v, want 0 (non-swap skipped, bad-ts swap skipped)", resp["processed"])
	}
	if resp["errors"].(float64) != 1 {
		t.Errorf("errors = %v, want 1 (bad timestamp)", resp["errors"])
	}
}
