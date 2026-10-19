package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"syscall"
	"time"

	"github.com/sei-protocol/seictl/sidecar/actions"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var stopSeidLog = seilog.NewLogger("seictl", "task", "stop-seid")

// seidStopper SIGTERMs the running `seid start` process and waits for it to
// exit within a grace window, never escalating to SIGKILL — a stuck-but-alive
// validator is safer than a SIGKILL mid-commit. It is the shared stop core of
// restart-seid (stop then wait-for-up) and stop-seid (stop and leave held at
// the gate). op names the calling operation for the honesty-check error only.
type seidStopper struct {
	signaler         actions.ProcessSignaler
	probeUp          func(ctx context.Context) bool
	gracePeriod      time.Duration
	exitPollInterval time.Duration
	log              *slog.Logger
	op               string
}

// stop finds seid and SIGTERMs it. When /proc shows no seid it disambiguates on
// the local RPC: serving means the process exists but is invisible to us (a
// non-shared-PID-namespace profile) and we refuse to report a stop that did not
// happen; down means seid is already stopped or mid-restart (including the
// entrypoint's bash wait-loop window before it execs seid), a no-op success.
func (s seidStopper) stop(ctx context.Context) error {
	pid, err := s.signaler.FindPID(restartSeidProcess)
	if err != nil {
		if s.probeUp(ctx) {
			return fmt.Errorf("seid RPC is serving but its process was not found in /proc: %w — refusing to report a %s that did not happen", err, s.op)
		}
		s.log.Info("seid process not running and RPC down; nothing to stop", "reason", err.Error())
		return nil
	}
	s.log.Info("stopping seid", "pid", pid, "grace", s.gracePeriod)
	return s.gracefulStop(ctx, pid)
}

// gracefulStop SIGTERMs pid and polls until it exits or the grace window
// elapses. It never escalates to SIGKILL: if seid is still alive at the
// deadline the task fails and the process is left running for an operator.
func (s seidStopper) gracefulStop(ctx context.Context, pid int) error {
	if err := s.signaler.Signal(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM to seid pid %d: %w", pid, err)
	}

	ticker := time.NewTicker(s.exitPollInterval)
	defer ticker.Stop()
	deadline := time.After(s.gracePeriod)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("seid pid %d still alive %s after SIGTERM; leaving it running (not force-killing a validator mid-commit)", pid, s.gracePeriod)
		case <-ticker.C:
			if !s.signaler.Alive(pid) {
				s.log.Info("seid exited after SIGTERM", "pid", pid)
				return nil
			}
		}
	}
}

// StopSeider SIGTERMs the co-located seid process and confirms it exited, then
// returns — unlike restart-seid it never waits for seid to come back up. The
// kubelet restarts the container and the start gate parks it (healthz 503) once
// the readiness flag is false. This is the hold's stop step: pair it with a
// prior mark-not-ready so the restarted container blocks at the gate instead of
// booting onto the data directory reset-data is about to clear.
type StopSeider struct {
	stopper seidStopper
}

// NewStopSeider builds a StopSeider with the real /proc + syscall + local-RPC
// implementations, sharing restart-seid's graceful-stop core and grace window.
func NewStopSeider() *StopSeider {
	statusClient := rpc.NewStatusClient("", nil)
	return &StopSeider{
		stopper: seidStopper{
			signaler:         seidStartFinder{},
			probeUp:          func(ctx context.Context) bool { return seidRPCUp(ctx, statusClient) },
			gracePeriod:      restartSeidGracePeriod,
			exitPollInterval: restartSeidExitPollInterval,
			log:              stopSeidLog,
			op:               "stop",
		},
	}
}

// Handler returns an engine.TaskHandler for the stop-seid task type. Params are
// empty: stop-seid is a fire-and-confirm operation.
func (s *StopSeider) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, _ struct{}) error {
		return s.stopper.stop(ctx)
	})
}
