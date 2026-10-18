package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/position"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// newWebhookRouter wires the HMAC webhook onto the swap consumer, exactly as
// internal/app does, with indexer.pools seeded so the fold resolves the token.
func newWebhookRouter(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	internaltest.EnsureIndexerSchema(t, st.Pool())
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xc',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	r := chi.NewRouter()
	httpapi.RegisterWebhook(r, httpapi.WebhookDeps{
		Swap:   position.NewConsumer(st, rdb, quietLogger()),
		Logger: quietLogger(),
		Secret: testSecret,
	})
	return r, st
}

func TestWebhookAuth(t *testing.T) {
	r, _ := newWebhookRouter(t)
	body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{}})

	t.Run("missing signature is 401", func(t *testing.T) {
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid signature is 401", func(t *testing.T) {
		headers := map[string]string{
			"Content-Type":        "application/json",
			"X-Sidiora-Timestamp": "9999999999",
			"X-Sidiora-Signature": "sha256=deadbeef",
		}
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid signature on empty batch is 200", func(t *testing.T) {
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestWebhookDispatchFoldsSwaps(t *testing.T) {
	ctx := context.Background()
	r, st := newWebhookRouter(t)

	ts := time.Now().Unix()
	body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{
		{EventName: "Swap", BlockNumber: 50, BlockTimestamp: ts, TxHash: "0xs", LogIndex: 0,
			Args: map[string]any{"poolAddress": "0xpool", "sender": "0xbuyer", "isBuy": true,
				"amountIn": "1000000", "amountOut": oneToken, "price": "1000000", "fee": "10"}},
		{EventName: "PoolStateUpdated", BlockNumber: 51, BlockTimestamp: ts, TxHash: "0xst", LogIndex: 0,
			Args: map[string]any{"poolAddress": "0xpool", "price": "2"}},
		{EventName: "SomethingUnknown", BlockTimestamp: ts, TxHash: "0xu", LogIndex: 0, Args: nil},
	}})

	rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK        bool `json:"ok"`
		Processed int  `json:"processed"`
		Errors    int  `json:"errors"`
	}
	decode(t, rec, &resp)
	// Swap folds; PoolStateUpdated + unknown are no-op successes -> 3 processed.
	if !resp.OK || resp.Processed != 3 || resp.Errors != 0 {
		t.Fatalf("resp = %+v, want processed=3 errors=0", resp)
	}

	pos, err := st.GetPosition(ctx, "0xbuyer", "0xpool")
	if err != nil || pos == nil {
		t.Fatalf("position: %v %v", pos, err)
	}
	if pos.TokenAddress != "0xtoken" || pos.TotalUsdlSpent != "1000000" {
		t.Fatalf("folded position = %+v", pos)
	}
}

func TestWebhookFailingEventCounted(t *testing.T) {
	r, _ := newWebhookRouter(t)
	ts := time.Now().Unix()
	// A negative amount makes the fold math reject the event.
	body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{
		{EventName: "Swap", BlockNumber: 1, BlockTimestamp: ts, TxHash: "0xbad", LogIndex: 0,
			Args: map[string]any{"poolAddress": "0xpool", "sender": "0xx", "isBuy": true,
				"amountIn": "-5", "amountOut": "1", "price": "0", "fee": "0"}},
	}})
	rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Processed int `json:"processed"`
		Errors    int `json:"errors"`
	}
	decode(t, rec, &resp)
	if resp.Processed != 0 || resp.Errors != 1 {
		t.Fatalf("resp = %+v, want processed=0 errors=1", resp)
	}
}

func TestWebhookNonArrayBody(t *testing.T) {
	r, _ := newWebhookRouter(t)
	body := []byte(`{"events":"nope"}`)
	rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
