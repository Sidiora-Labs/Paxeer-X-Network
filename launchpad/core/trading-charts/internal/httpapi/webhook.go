package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
)

// WebhookEvent is a single event in a webhook batch.
type WebhookEvent struct {
	EventName      string                 `json:"eventName"`
	BlockNumber    int64                  `json:"blockNumber"`
	BlockTimestamp int64                  `json:"blockTimestamp"`
	TxHash         string                 `json:"txHash"`
	LogIndex       int                    `json:"logIndex"`
	Args           map[string]interface{} `json:"args"`
}

// WebhookBody is the expected POST body for /webhooks/events.
type WebhookBody struct {
	Events []WebhookEvent `json:"events"`
}

// WebhookDeps holds the dependencies for the webhook route.
type WebhookDeps struct {
	Builder *engine.Builder
	Logger  *slog.Logger
	Secret  string
}

// RegisterWebhook registers the HMAC-authenticated webhook receiver.
func RegisterWebhook(r chi.Router, deps WebhookDeps) {
	//nolint:bodyclose // body is read by json.Decode
	r.With(webhookAuth(deps.Secret)).Post("/webhooks/events", webhookHandler(deps))
}

func webhookHandler(deps WebhookDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sharedhttp.WriteError(w, 400, "Bad Request", "failed to read body")
			return
		}

		var wb WebhookBody
		if err := json.Unmarshal(body, &wb); err != nil {
			sharedhttp.WriteError(w, 400, "Bad Request", "invalid JSON body")
			return
		}

		if wb.Events == nil {
			sharedhttp.WriteError(w, 400, "Bad Request", "events must be an array")
			return
		}

		processed := 0
		errCount := 0

		for _, event := range wb.Events {
			if event.EventName != "Swap" {
				continue
			}

			if event.BlockTimestamp == 0 || event.BlockTimestamp < 1704067200 {
				deps.Logger.Warn("skipping swap with invalid blockTimestamp",
					slog.String("txHash", event.TxHash), slog.Int64("ts", event.BlockTimestamp))
				errCount++
				continue
			}

			swap := engine.SwapEvent{
				PoolID:         asString(event.Args["poolId"]),
				PoolAddress:    asString(event.Args["poolAddress"]),
				Sender:         asString(event.Args["sender"]),
				IsBuy:          asBool(event.Args["isBuy"]),
				AmountIn:       asString(event.Args["amountIn"]),
				AmountOut:      asString(event.Args["amountOut"]),
				Fee:            asString(event.Args["fee"]),
				Price:          asString(event.Args["price"]),
				BlockTimestamp: event.BlockTimestamp,
				TxHash:         event.TxHash,
				LogIndex:       event.LogIndex,
			}

			if err := deps.Builder.ProcessSwap(r.Context(), swap); err != nil {
				errCount++
				deps.Logger.Error("failed to process swap for candles",
					slog.String("err", err.Error()), slog.String("txHash", event.TxHash))
			} else {
				processed++
			}
		}

		sharedhttp.WriteJSON(w, 200, map[string]interface{}{
			"ok":        true,
			"processed": processed,
			"errors":    errCount,
		})
	}
}

// webhookAuth is the HMAC verification middleware for /webhooks/* routes.
func webhookAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-Sidiora-Signature")
			ts := r.Header.Get("X-Sidiora-Timestamp")
			if sig == "" {
				sig = r.Header.Get("X-Hub-Signature-256")
			}

			// Read body for verification.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				sharedhttp.WriteError(w, 400, "Bad Request", "failed to read body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			if err := auth.VerifyWebhook(secret, ts, string(body), sig, time.Now(), 0); err != nil {
				sharedhttp.WriteError(w, 401, "Unauthorized", "invalid webhook signature")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
