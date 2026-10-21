package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/position"
)

// WebhookEvent is one event in a webhook batch (matched-index fanout contract,
// invariant i3). BlockNumber comes from the envelope so the fold records the
// trade's block for the consumer-lag status.
type WebhookEvent struct {
	EventName      string         `json:"eventName"`
	BlockNumber    int64          `json:"blockNumber"`
	BlockTimestamp int64          `json:"blockTimestamp"`
	TxHash         string         `json:"txHash"`
	LogIndex       int            `json:"logIndex"`
	Args           map[string]any `json:"args"`
}

// WebhookBody is the POST body for /webhooks/events.
type WebhookBody struct {
	Events []WebhookEvent `json:"events"`
}

// WebhookDeps holds the consumer the webhook dispatches to.
type WebhookDeps struct {
	Swap   *position.Consumer
	Logger *slog.Logger
	Secret string
}

// RegisterWebhook registers the HMAC-authenticated POST /webhooks/events receiver.
func RegisterWebhook(r chi.Router, deps WebhookDeps) {
	r.With(webhookAuth(deps.Secret)).Post("/webhooks/events", webhookHandler(deps))
}

// webhookHandler folds each Swap event into positions, counting processed vs
// errored events. Unknown event names count as processed (parity), so an indexer
// can fan the full event stream at pnl without bespoke filtering.
func webhookHandler(deps WebhookDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "failed to read body")
			return
		}
		var wb WebhookBody
		if err := json.Unmarshal(body, &wb); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}
		if wb.Events == nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "events must be an array")
			return
		}

		ctx := r.Context()
		processed, errCount := 0, 0
		for _, ev := range wb.Events {
			if err := dispatchEvent(ctx, deps, ev); err != nil {
				errCount++
				deps.Logger.Error("failed to process webhook event",
					slog.String("event", ev.EventName), slog.String("txHash", ev.TxHash), slog.Any("err", err))
				continue
			}
			processed++
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"processed": processed,
			"errors":    errCount,
		})
	}
}

// dispatchEvent routes one event. Only Swap folds a position; everything else is
// a no-op success.
func dispatchEvent(ctx context.Context, deps WebhookDeps, ev WebhookEvent) error {
	if ev.EventName != "Swap" {
		return nil
	}
	return deps.Swap.ProcessEvent(ctx, position.SwapEvent{
		PoolAddress:    asString(ev.Args["poolAddress"]),
		Sender:         asString(ev.Args["sender"]),
		IsBuy:          asBool(ev.Args["isBuy"]),
		AmountIn:       asString(ev.Args["amountIn"]),
		AmountOut:      asString(ev.Args["amountOut"]),
		Price:          asString(ev.Args["price"]),
		Fee:            asString(ev.Args["fee"]),
		BlockNumber:    ev.BlockNumber,
		BlockTimestamp: ev.BlockTimestamp,
		TxHash:         ev.TxHash,
		LogIndex:       ev.LogIndex,
	})
}

// webhookAuth verifies the HMAC signature on /webhooks/* requests, mirroring the
// shared registerWebhookAuth (header names + replay window). The body is read and
// restored so the handler can re-read it.
func webhookAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-Sidiora-Signature")
			ts := r.Header.Get("X-Sidiora-Timestamp")
			if sig == "" {
				sig = r.Header.Get("X-Hub-Signature-256")
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "failed to read body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			if err := auth.VerifyWebhook(secret, ts, string(body), sig, time.Now(), 0); err != nil {
				sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "invalid webhook signature")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
