package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// StatusDeps wires the service status probe.
type StatusDeps struct {
	Store     *store.Store
	StartedAt time.Time
}

// RegisterStatus wires GET /status (the pipeline health surfaced by the client).
func RegisterStatus(r chi.Router, deps StatusDeps) {
	r.Get("/status", statusHandler(deps))
}

// statusHandler reports indexer head vs the realtime consumer's folded block and
// the reconciler cursor, plus uptime — the pnl.ts PnlStatus shape.
func statusHandler(deps StatusDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		indexerHead, err := deps.Store.IndexerHead(ctx)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "status failed")
			return
		}
		consumerBlock, err := deps.Store.ConsumerBlock(ctx)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "status failed")
			return
		}
		cursor, err := deps.Store.GetCursor(ctx)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "status failed")
			return
		}

		lag := indexerHead - consumerBlock
		if lag < 0 {
			lag = 0
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"indexerHead":     indexerHead,
			"consumerBlock":   consumerBlock,
			"consumerLag":     lag,
			"reconcilerBlock": cursor.LastBlock,
			"uptime":          time.Since(deps.StartedAt).Truncate(time.Second).String(),
			"status":          "ok",
		})
	}
}
