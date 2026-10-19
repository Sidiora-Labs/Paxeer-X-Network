package tasks

import (
	"context"
	"fmt"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var markNotReadyLog = seilog.NewLogger("seictl", "task", "mark-not-ready")

// markReadyPurger deletes recorded mark-ready results. The concrete
// implementation is the engine's ResultStore; a narrow interface keeps the
// handler testable without a full store.
type markReadyPurger interface {
	DeleteByType(taskType string) (int, error)
}

// MarkNotReadier re-arms the seid start gate for a node hold. Its handler
// purges recorded mark-ready results from the task store; the engine then
// flips the readiness flag false on success (the sole false-writer, see
// engine.execute). The purge is what closes the rehydration release path: a
// mark-ready left running by an ungraceful shutdown would otherwise re-run on
// restart and mark the engine ready again, releasing seid onto a wiped data
// directory.
type MarkNotReadier struct {
	purger markReadyPurger
}

// NewMarkNotReadier builds a MarkNotReadier over the given store.
func NewMarkNotReadier(purger markReadyPurger) *MarkNotReadier {
	return &MarkNotReadier{purger: purger}
}

// Handler returns an engine.TaskHandler for the mark-not-ready task type.
// Params are empty. The handler purges mark-ready records and returns; the
// engine's completion hook performs the readiness flip. On purge failure it
// returns an error so the engine skips the flip (fail-safe: readiness is left
// untouched rather than flipped over a store that still holds a releasable
// mark-ready).
func (m *MarkNotReadier) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(_ context.Context, _ struct{}) error {
		n, err := m.purger.DeleteByType(string(engine.TaskMarkReady))
		if err != nil {
			return fmt.Errorf("mark-not-ready: purging mark-ready records: %w", err)
		}
		markNotReadyLog.Info("purged mark-ready records before hold", "count", n)
		return nil
	})
}
