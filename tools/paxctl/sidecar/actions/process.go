package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("seictl", "actions")

const (
	DefaultGracePeriod = 30 * time.Second
	exitPollInterval   = 100 * time.Millisecond
)

// ProcessSignaler abstracts process discovery and signaling for testability.
type ProcessSignaler interface {
	FindPID(processName string) (int, error)
	Signal(pid int, sig syscall.Signal) error
	Alive(pid int) bool
}

// FindPID scans /proc for a process whose argv[0] matches processName.
// Requires shareProcessNamespace: true in the Kubernetes pod spec.
func FindPID(processName string) (int, error) {
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
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		exe := firstArg(cmdline)
		if exe == processName || strings.HasSuffix(exe, "/"+processName) {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("process %q not found in /proc", processName)
}

// SignalPID sends a signal to the given process.
func SignalPID(pid int, sig syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// PIDAlive returns true if the process is still running.
func PIDAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// isProcessGone returns true if the error indicates the process no longer exists.
func isProcessGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// GracefulStop sends SIGTERM to the named process, waits up to
// gracePeriod for it to exit, then escalates to SIGKILL.
// When signaler is nil the package-level functions are used directly.
func GracefulStop(ctx context.Context, signaler ProcessSignaler, processName string, gracePeriod time.Duration) error {
	findPID := FindPID
	signal := SignalPID
	alive := PIDAlive
	if signaler != nil {
		findPID = signaler.FindPID
		signal = signaler.Signal
		alive = signaler.Alive
	}

	pid, err := findPID(processName)
	if err != nil {
		if isProcessGone(err) {
			logger.Info("process already exited", "process", processName)
			return nil
		}
		return fmt.Errorf("finding %s process: %w", processName, err)
	}
	logger.Info("sending SIGTERM", "process", processName, "pid", pid)

	if err := signal(pid, syscall.SIGTERM); err != nil {
		if isProcessGone(err) {
			logger.Info("process exited before SIGTERM delivered", "process", processName, "pid", pid)
			return nil
		}
		return fmt.Errorf("sending SIGTERM to pid %d: %w", pid, err)
	}

	ticker := time.NewTicker(exitPollInterval)
	defer ticker.Stop()
	deadline := time.After(gracePeriod)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			logger.Warn("process did not exit after SIGTERM, sending SIGKILL", "process", processName, "pid", pid)
			if err := signal(pid, syscall.SIGKILL); err != nil {
				if isProcessGone(err) {
					logger.Info("process exited before SIGKILL delivered", "process", processName, "pid", pid)
					return nil
				}
				return fmt.Errorf("sending SIGKILL to pid %d: %w", pid, err)
			}
			logger.Info("SIGKILL sent", "process", processName, "pid", pid)
			return nil
		case <-ticker.C:
			if !alive(pid) {
				logger.Info("process exited after SIGTERM", "process", processName, "pid", pid)
				return nil
			}
		}
	}
}

func firstArg(cmdline []byte) string {
	for i, c := range cmdline {
		if c == 0 {
			return string(cmdline[:i])
		}
	}
	return string(cmdline)
}
