package tasks

import (
	"context"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

// MarkReadyHandler returns a no-op TaskHandler. When it succeeds, the engine
// marks itself as ready.
func MarkReadyHandler() engine.TaskHandler {
	return engine.TypedHandler(func(_ context.Context, _ struct{}) error {
		return nil
	})
}
