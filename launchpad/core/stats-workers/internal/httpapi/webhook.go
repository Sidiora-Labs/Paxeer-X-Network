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

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
)

// WebhookEvent is one event in a webhook batch.
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

// WebhookDeps holds the consumers the webhook receiver dispatches to.
type WebhookDeps struct {
	Swap     *consumer.SwapConsumer
	Market   *consumer.MarketConsumer
	State    *consumer.StateConsumer
	Holder   *consumer.HolderTracker
	Multihop *consumer.MultihopConsumer
	Logger   *slog.Logger
	Secret   string
}

// RegisterWebhook registers the HMAC-authenticated POST /webhooks/events receiver.
func RegisterWebhook(r chi.Router, deps WebhookDeps) {
	r.With(webhookAuth(deps.Secret)).Post("/webhooks/events", webhookHandler(deps))
}

// webhookHandler dispatches each event to the matching consumer, counting
// processed vs errored events (parity with the TS webhook route, including the
// behaviour that unknown event names count as processed and that a Swap requires
// BOTH the swap consumer and the holder tracker to succeed).
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

// dispatchEvent routes one event to its consumer(s). Unknown event names are a
// no-op success (parity).
func dispatchEvent(ctx context.Context, deps WebhookDeps, ev WebhookEvent) error {
	switch ev.EventName {
	case "Swap":
		if err := deps.Swap.ProcessEvent(ctx, consumer.SwapEvent{
			PoolAddress:    asString(ev.Args["poolAddress"]),
			Sender:         asString(ev.Args["sender"]),
			IsBuy:          asBool(ev.Args["isBuy"]),
			AmountIn:       asString(ev.Args["amountIn"]),
			AmountOut:      asString(ev.Args["amountOut"]),
			Price:          asString(ev.Args["price"]),
			Fee:            asString(ev.Args["fee"]),
			BlockTimestamp: ev.BlockTimestamp,
			TxHash:         ev.TxHash,
			LogIndex:       ev.LogIndex,
		}); err != nil {
			return err
		}
		return deps.Holder.ProcessSwap(ctx, consumer.HolderSwap{
			PoolAddress: asString(ev.Args["poolAddress"]),
			Sender:      asString(ev.Args["sender"]),
			IsBuy:       asBool(ev.Args["isBuy"]),
			AmountIn:    asString(ev.Args["amountIn"]),
			AmountOut:   asString(ev.Args["amountOut"]),
		})
	case "MarketCreated":
		return deps.Market.ProcessEvent(ctx, consumer.MarketEvent{
			Pool:    asString(ev.Args["pool"]),
			Token:   asString(ev.Args["token"]),
			Creator: asStringPtr(ev.Args["creator"]),
		})
	case "PoolStateUpdated":
		return deps.State.ProcessEvent(ctx, consumer.StateEvent{
			PoolAddress: asString(ev.Args["poolAddress"]),
			Price:       asString(ev.Args["price"]),
		})
	case "MultihopSwap":
		// Native Router cross-token swap (Router.swapTokenForToken). The on-chain
		// event emits sender/tokenIn/tokenOut/amountIn/intermediateUsdl/amountOut;
		// poolIn/poolOut/feeIn/feeOut are indexer-enriched (all NOT NULL).
		return deps.Multihop.ProcessEvent(ctx, consumer.MultihopEvent{
			Sender:           asString(ev.Args["sender"]),
			TokenIn:          asString(ev.Args["tokenIn"]),
			TokenOut:         asString(ev.Args["tokenOut"]),
			PoolIn:           asString(ev.Args["poolIn"]),
			PoolOut:          asString(ev.Args["poolOut"]),
			AmountIn:         asString(ev.Args["amountIn"]),
			IntermediateUsdl: asString(ev.Args["intermediateUsdl"]),
			AmountOut:        asString(ev.Args["amountOut"]),
			FeeIn:            asString(ev.Args["feeIn"]),
			FeeOut:           asString(ev.Args["feeOut"]),
			BlockTimestamp:   ev.BlockTimestamp,
			TxHash:           ev.TxHash,
			LogIndex:         ev.LogIndex,
		})
	default:
		return nil
	}
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
