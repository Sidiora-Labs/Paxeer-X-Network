package tasks

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sei-protocol/seictl/sidecar/actions"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var restartSeidLog = seilog.NewLogger("seictl", "task", "restart-seid")

const (
	// restartSeidProcess is the comm/argv[0] of the validator process.
	restartSeidProcess = "seid"

	// restartSeidGracePeriod bounds the SIGTERM→exit window. A loaded validator's
	// graceful shutdown (consensus WAL flush + PebbleDB/IAVL close, possibly
	// mid-compaction) can run well past the idle-only ~3s figure, so the window
	// is sized for the loaded-shutdown tail. If seid is still alive at the
	// deadline the task fails loud and leaves the process running for an
	// operator — it is never force-killed (a stuck-but-alive validator is safer
	// than a SIGKILL mid-commit).
	restartSeidGracePeriod = 90 * time.Second

	// restartSeidUpTimeout bounds the wait for seid's local RPC to serve
	// /status again after the restart. Cold-start replay can be slow on a
	// loaded node; the engine has no retry, so this is the full budget. With
	// graceful-only shutdown there is no SIGKILL-induced replay blowup, so 5m
	// holds; revisit for very large archive nodes if replay outgrows it.
	restartSeidUpTimeout = 5 * time.Minute

	restartSeidUpPollInterval = 1 * time.Second

	// restartSeidExitPollInterval is how often gracefulStop checks whether seid
	// has exited after SIGTERM.
	restartSeidExitPollInterval = 100 * time.Millisecond
)

// seidStartFinder scans /proc for the running `seid start` process. It
// corroborates argv[0]==seid with the "start" subcommand so it never matches
// seid-init or the bash wait-loop wrapper that share the PID namespace.
//
// It implements actions.ProcessSignaler so stopSeid can SIGTERM + poll it;
// FindPID ignores the name argument (the corroboration is baked in) and Signal
// / Alive delegate to the real syscall-backed package functions.
type seidStartFinder struct{}

func (seidStartFinder) FindPID(string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("reading /proc: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil || strings.TrimSpace(string(comm)) != restartSeidProcess {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		if isSeidStart(cmdline) {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("process %q not found in /proc", restartSeidProcess)
}

func (seidStartFinder) Signal(pid int, sig syscall.Signal) error { return actions.SignalPID(pid, sig) }

func (seidStartFinder) Alive(pid int) bool { return actions.PIDAlive(pid) }

// isSeidStart reports whether a null-delimited /proc cmdline is `seid start ...`,
// matching both a bare "seid" and an absolute path ending in "/seid".
func isSeidStart(cmdline []byte) bool {
	args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(args) < 2 {
		return false
	}
	exe := args[0]
	if exe != restartSeidProcess && !strings.HasSuffix(exe, "/"+restartSeidProcess) {
		return false
	}
	return args[1] == "start"
}

// RestartSeider restarts the co-located seid process in place: it SIGTERMs seid
// and waits for it to exit gracefully (the kubelet restarts the container), then
// waits for seid's local RPC to serve again. seid re-reads config.toml on this
// restart without bouncing the sidecar. The handler never starts seid and never
// flips the engine ready flag — it is not a readiness operation.
//
// Shutdown is graceful-only and fail-loud: if seid does not exit within the
// grace window it is left running and the task fails (never SIGKILLed). A
// force-kill opt-in is intentionally omitted until a non-validator forced
// restart needs it.
//
// Completion means "seid's RPC is serving /status again," NOT "caught up /
// voting." Callers that need in-service-and-voting must gate height / caught-up
// separately (downstream AwaitNodesAtHeight).
//
// The three OS interactions are injectable for testing:
//   - signaler: process discovery + SIGTERM (defaults to a /proc + syscall
//     implementation that corroborates `seid start`).
//   - probeUp: returns true once seid's local RPC answers /status (defaults to
//     a local CometBFT /status probe).
type RestartSeider struct {
	signaler    actions.ProcessSignaler
	probeUp     func(ctx context.Context) bool
	gracePeriod time.Duration
	upTimeout   time.Duration
	upInterval  time.Duration
}

// NewRestartSeider builds a RestartSeider with the real /proc + syscall +
// local-RPC implementations.
func NewRestartSeider() *RestartSeider {
	statusClient := rpc.NewStatusClient("", nil)
	return &RestartSeider{
		signaler:    seidStartFinder{},
		probeUp:     func(ctx context.Context) bool { return seidRPCUp(ctx, statusClient) },
		gracePeriod: restartSeidGracePeriod,
		upTimeout:   restartSeidUpTimeout,
		upInterval:  restartSeidUpPollInterval,
	}
}

// seidRPCUp reports whether seid's local RPC answers /status (latest_block_height
// parses); any successful parse counts as RPC up. A transport or parse error
// (RPC not yet listening) returns false.
func seidRPCUp(ctx context.Context, c *rpc.StatusClient) bool {
	if _, err := c.Status(ctx); err != nil {
		return false
	}
	return true
}

// Handler returns an engine.TaskHandler for the restart-seid task type.
// Params are empty: restart-seid is a fire-and-confirm operation.
func (r *RestartSeider) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, _ struct{}) error {
		if err := r.stopSeid(ctx); err != nil {
			return err
		}
		return r.waitForUp(ctx)
	})
}

// stopSeid SIGTERMs seid and waits for it to exit gracefully via the shared
// seidStopper (graceful-only, never SIGKILL; honesty check when /proc shows
// nothing but the RPC serves). restart-seid proceeds to waitForUp afterwards.
func (r *RestartSeider) stopSeid(ctx context.Context) error {
	return seidStopper{
		signaler:         r.signaler,
		probeUp:          r.probeUp,
		gracePeriod:      r.gracePeriod,
		exitPollInterval: restartSeidExitPollInterval,
		log:              restartSeidLog,
		op:               "restart",
	}.stop(ctx)
}

// waitForUp polls seid's local RPC until it serves /status or the timeout
// elapses. Success here is the completion signal for the in-place restart.
func (r *RestartSeider) waitForUp(ctx context.Context) error {
	deadline := time.Now().Add(r.upTimeout)
	ticker := time.NewTicker(r.upInterval)
	defer ticker.Stop()

	restartSeidLog.Info("waiting for seid RPC to come back up", "timeout", r.upTimeout)
	for {
		if r.probeUp(ctx) {
			restartSeidLog.Info("seid RPC is up; restart complete")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("seid RPC did not come up within %s after restart", r.upTimeout)
			}
		}
	}
}
