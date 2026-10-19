package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/seictl/sidecar/actions"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var awaitLog = seilog.NewLogger("seictl", "task", "await-condition")

const (
	conditionHeight     = "height"
	conditionCatchingUp = "catchingUp"
	actionSIGTERM       = "SIGTERM_SEID"

	heightPollInterval = 100 * time.Millisecond
)

// AwaitConditionRequest holds the typed parameters for the await-condition task.
type AwaitConditionRequest struct {
	Condition    string `json:"condition"`
	Action       string `json:"action"`
	TargetHeight int64  `json:"targetHeight"`
}

// ConditionWaiter polls a local node until a condition is met, then
// optionally executes a post-condition action.
type ConditionWaiter struct {
	rpc *rpc.StatusClient
}

// NewConditionWaiter creates a ConditionWaiter. Pass nil for the default RPC client.
func NewConditionWaiter(rpcClient *rpc.StatusClient) *ConditionWaiter {
	if rpcClient == nil {
		rpcClient = rpc.NewStatusClient("", nil)
	}
	return &ConditionWaiter{rpc: rpcClient}
}

// Handler returns an engine.TaskHandler for the await-condition task type.
func (w *ConditionWaiter) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, params AwaitConditionRequest) error {
		if params.Condition == "" {
			return fmt.Errorf("condition is required")
		}

		switch params.Condition {
		case conditionHeight:
			if params.TargetHeight <= 0 {
				return fmt.Errorf("targetHeight must be > 0, got %d", params.TargetHeight)
			}
			if err := w.awaitHeight(ctx, params.TargetHeight); err != nil {
				return err
			}
		case conditionCatchingUp:
			if err := w.awaitCaughtUp(ctx); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown condition %q", params.Condition)
		}

		if params.Action == "" {
			return nil
		}
		return w.executeAction(ctx, params.Action)
	})
}

func (w *ConditionWaiter) awaitHeight(ctx context.Context, targetHeight int64) error {
	awaitLog.Info("awaiting height", "target", targetHeight, "rpc", w.rpc.Endpoint())

	var rpcHealthy bool
	var loggedInitialWait bool
	ticker := time.NewTicker(heightPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		height, err := w.rpc.LatestHeight(ctx)
		if err != nil {
			if rpcHealthy {
				awaitLog.Warn("rpc became unavailable", "err", err)
				rpcHealthy = false
			} else if !loggedInitialWait {
				awaitLog.Info("waiting for rpc to become available", "err", err)
				loggedInitialWait = true
			}
			continue
		}
		if !rpcHealthy {
			awaitLog.Info("rpc available", "height", height)
			rpcHealthy = true
		}

		if height >= targetHeight {
			awaitLog.Info("target height reached", "current", height, "target", targetHeight)
			return nil
		}
	}
}

// awaitCaughtUp polls the local node until it reports catching_up=false at a
// height past genesis (>1). A freshly state-synced node reports catching_up
// while it applies the snapshot and backfills; the height>1 floor rejects the
// degenerate window where a just-started node reads catching_up=false before it
// has synced. This matches the sdk/sei readiness semantics the controller uses.
func (w *ConditionWaiter) awaitCaughtUp(ctx context.Context) error {
	awaitLog.Info("awaiting caught up", "rpc", w.rpc.Endpoint())

	var rpcHealthy bool
	var loggedInitialWait bool
	ticker := time.NewTicker(heightPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		status, err := w.rpc.Status(ctx)
		if err != nil {
			if rpcHealthy {
				awaitLog.Warn("rpc became unavailable", "err", err)
				rpcHealthy = false
			} else if !loggedInitialWait {
				awaitLog.Info("waiting for rpc to become available", "err", err)
				loggedInitialWait = true
			}
			continue
		}
		if !rpcHealthy {
			awaitLog.Info("rpc available", "height", status.LatestBlockHeight, "catchingUp", status.CatchingUp)
			rpcHealthy = true
		}

		if !status.CatchingUp && status.LatestBlockHeight > 1 {
			awaitLog.Info("node caught up", "height", status.LatestBlockHeight)
			return nil
		}
	}
}

func (w *ConditionWaiter) executeAction(ctx context.Context, action string) error {
	switch action {
	case actionSIGTERM:
		return actions.GracefulStop(ctx, nil, "seid", actions.DefaultGracePeriod)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}
