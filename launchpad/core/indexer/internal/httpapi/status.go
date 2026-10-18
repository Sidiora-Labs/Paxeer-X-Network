// Package httpapi registers the indexer's read/operational HTTP routes (status +
// dead-letter inspection), parity with the TS routes/status.ts. Public data
// reads are served through core/api, not here.
package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
)

// StatusDeps wires the status routes.
type StatusDeps struct {
	Store     *store.Store
	ChainID   int32
	Publisher *publisher.Publisher
	StartTime time.Time
}

// RegisterStatus mounts /status, /status/dead-letter and the drain endpoint.
func RegisterStatus(r chi.Router, d StatusDeps) {
	r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		var lastBlock *int64
		if c, err := d.Store.GetCursor(ctx, d.ChainID); err == nil {
			lastBlock = c
		}
		pools, err := d.Store.PoolCount(ctx)
		if err != nil {
			pools = 0
		}
		swaps, err := d.Store.SwapCount(ctx)
		if err != nil {
			swaps = 0
		}

		body := map[string]any{
			"chainId":            d.ChainID,
			"lastProcessedBlock": lastBlock,
			"totalPoolsIndexed":  pools,
			"totalSwapsIndexed":  swaps,
			"uptime":             time.Since(d.StartTime).Round(time.Second).String(),
			"status":             "ok",
		}
		if d.Publisher != nil {
			m := d.Publisher.Snapshot()
			body["webhooks"] = map[string]any{
				"totalEventsPublished": m.TotalEventsPublished,
				"totalDeliveries":      m.TotalDeliveries,
				"totalFailures":        m.TotalFailures,
				"totalDeadLettered":    m.TotalDeadLettered,
				"totalDeduplicated":    m.TotalDeduplicated,
				"totalRedisPublishes":  m.TotalRedisPublishes,
				"totalRedisFailures":   m.TotalRedisFailures,
				"currentInflight":      m.CurrentInflight,
				"deadLetterQueueSize":  d.Publisher.DeadLetterCount(),
			}
		}
		sharedhttp.WriteJSON(w, http.StatusOK, body)
	})

	if d.Publisher == nil {
		return
	}

	r.Get("/status/dead-letter", func(w http.ResponseWriter, _ *http.Request) {
		entries := d.Publisher.DeadLetterQueue()
		if len(entries) > 100 {
			entries = entries[:100]
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"count":   d.Publisher.DeadLetterCount(),
			"entries": entries,
		})
	})

	r.Post("/status/dead-letter/drain", func(w http.ResponseWriter, _ *http.Request) {
		drained := d.Publisher.DrainDeadLetterQueue()
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{"drained": len(drained)})
	})
}
