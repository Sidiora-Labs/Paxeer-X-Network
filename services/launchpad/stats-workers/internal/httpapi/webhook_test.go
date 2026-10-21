package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const testSecret = "0123456789abcdef0123456789abcdef" // >=32 chars

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// newWebhookRouter wires the HMAC webhook receiver onto every consumer, exactly
// as internal/app does, returning the router plus the store for side-effect
// assertions. The holder tracker is closed via t.Cleanup so its timers never
// outlive the test.
func newWebhookRouter(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	logger := quietLogger()

	market, err := consumer.NewMarketConsumer(st, logger)
	if err != nil {
		t.Fatalf("market consumer: %v", err)
	}
	// Long debounce: the synchronous balance delta is what the dispatch tests
	// assert; the (deferred) holder-stats refresh is cancelled by Close before it
	// can fire, so no query is in flight when the container is torn down.
	holder := consumer.NewHolderTracker(st, rdb, logger, time.Hour)
	t.Cleanup(holder.Close)

	r := chi.NewRouter()
	httpapi.RegisterWebhook(r, httpapi.WebhookDeps{
		Swap:     consumer.NewSwapConsumer(st, rdb, logger),
		Market:   market,
		State:    consumer.NewStateConsumer(st, logger),
		Holder:   holder,
		Multihop: consumer.NewMultihopConsumer(st, logger),
		Logger:   logger,
		Secret:   testSecret,
	})
	return r, st
}

func TestWebhookAuth(t *testing.T) {
	r, _ := newWebhookRouter(t)
	body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{}})

	t.Run("missing signature is rejected 401", func(t *testing.T) {
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid signature is rejected 401", func(t *testing.T) {
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

	t.Run("valid signature on empty batch is accepted", func(t *testing.T) {
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestWebhookDispatch(t *testing.T) {
	ctx := context.Background()
	r, st := newWebhookRouter(t)

	t.Run("dispatches every event type and counts processed; unknown counts as processed", func(t *testing.T) {
		const pool = "0xwh_pool"
		ts := time.Now().Unix()
		body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{
			{EventName: "MarketCreated", BlockTimestamp: ts, TxHash: "0xm", LogIndex: 0,
				Args: map[string]any{"pool": pool, "token": "0xtok", "creator": "0xcreator"}},
			{EventName: "Swap", BlockTimestamp: ts, TxHash: "0xs", LogIndex: 0,
				Args: map[string]any{"poolAddress": pool, "sender": "0xbuyer", "isBuy": true,
					"amountIn": "1000000", "amountOut": "5000", "price": "20000000000000", "fee": "10"}},
			{EventName: "PoolStateUpdated", BlockTimestamp: ts, TxHash: "0xst", LogIndex: 0,
				Args: map[string]any{"poolAddress": pool, "price": "25000000000000"}},
			{EventName: "MultihopSwap", BlockTimestamp: ts, TxHash: "0xmh", LogIndex: 1,
				Args: map[string]any{"sender": "0xrouter", "tokenIn": "0xa", "tokenOut": "0xb",
					"poolIn": "0xpa", "poolOut": "0xpb", "amountIn": "100", "intermediateUsdl": "50",
					"amountOut": "200", "feeIn": "1", "feeOut": "2"}},
			{EventName: "SomethingUnknown", BlockTimestamp: ts, TxHash: "0xu", LogIndex: 0, Args: nil},
		}})

		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		var resp struct {
			OK        bool `json:"ok"`
			Processed int  `json:"processed"`
			Errors    int  `json:"errors"`
		}
		decode(t, rec, &resp)
		if !resp.OK || resp.Processed != 5 || resp.Errors != 0 {
			t.Fatalf("resp = %+v, want ok=true processed=5 errors=0", resp)
		}

		// MarketCreated bootstrapped the row; PoolStateUpdated set the latest price.
		row, err := st.GetPoolStats(ctx, pool)
		if err != nil || row == nil {
			t.Fatalf("pool stats: row=%v err=%v", row, err)
		}
		if row.Price != "25000000000000" {
			t.Errorf("price = %s, want 25000000000000 (state update applied last)", row.Price)
		}
		// The Swap event ran BOTH the swap consumer (transaction recorded)...
		var txCount int
		if err := st.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM stats.pool_transactions WHERE id='0xs-0'`).Scan(&txCount); err != nil {
			t.Fatalf("count tx: %v", err)
		}
		if txCount != 1 {
			t.Errorf("transaction rows = %d, want 1 (swap consumer ran)", txCount)
		}
		// ...AND the holder tracker (sender balance = amountOut for a buy).
		if bal, ok, _ := st.GetHolderBalance(ctx, pool, "0xbuyer"); !ok || bal != "5000" {
			t.Errorf("holder balance = %s ok=%v, want 5000 (holder tracker ran)", bal, ok)
		}
		// MultihopSwap recorded the cross-token swap.
		swaps, _ := st.ListCrossTokenSwapsByWallet(ctx, "0xrouter", 10, 0)
		if len(swaps) != 1 {
			t.Errorf("cross-token swaps = %d, want 1", len(swaps))
		}
	})

	t.Run("a failing event is counted as an error, not processed", func(t *testing.T) {
		ts := time.Now().Unix()
		// price="bad" makes the swap consumer's price-change computation fail.
		body := mustJSON(t, httpapi.WebhookBody{Events: []httpapi.WebhookEvent{
			{EventName: "Swap", BlockTimestamp: ts, TxHash: "0xbad", LogIndex: 0,
				Args: map[string]any{"poolAddress": "0xwh_bad", "sender": "0xx", "isBuy": true,
					"amountIn": "1", "amountOut": "1", "price": "not-a-number", "fee": "0"}},
		}})
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp struct {
			Processed int `json:"processed"`
			Errors    int `json:"errors"`
		}
		decode(t, rec, &resp)
		if resp.Processed != 0 || resp.Errors != 1 {
			t.Fatalf("resp = %+v, want processed=0 errors=1", resp)
		}
	})

	t.Run("non-array events body is a 400", func(t *testing.T) {
		body := []byte(`{"events": "nope"}`)
		rec := serve(t, r, http.MethodPost, "/webhooks/events", body, hmacHeaders(testSecret, body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}
