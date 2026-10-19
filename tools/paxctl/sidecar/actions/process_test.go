package actions

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type mockSignaler struct {
	findPID   int
	findErr   error
	signalFn  func(pid int, sig syscall.Signal) error
	signalErr error
	alive     atomic.Bool
	signals   []syscall.Signal
}

func (m *mockSignaler) FindPID(string) (int, error) { return m.findPID, m.findErr }

func (m *mockSignaler) Signal(pid int, sig syscall.Signal) error {
	m.signals = append(m.signals, sig)
	if m.signalFn != nil {
		return m.signalFn(pid, sig)
	}
	return m.signalErr
}

func (m *mockSignaler) Alive(int) bool { return m.alive.Load() }

func TestGracefulStop_ImmediateExit(t *testing.T) {
	sig := &mockSignaler{findPID: 42}
	sig.alive.Store(false)

	if err := GracefulStop(context.Background(), sig, "seid", time.Second); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(sig.signals) != 1 || sig.signals[0] != syscall.SIGTERM {
		t.Errorf("expected single SIGTERM, got %v", sig.signals)
	}
}

func TestGracefulStop_EscalatesToSIGKILL(t *testing.T) {
	sig := &mockSignaler{findPID: 42}
	sig.alive.Store(true)

	if err := GracefulStop(context.Background(), sig, "seid", 200*time.Millisecond); err != nil {
		t.Fatalf("expected success after SIGKILL, got %v", err)
	}
	if len(sig.signals) < 2 {
		t.Fatalf("expected >= 2 signals, got %d", len(sig.signals))
	}
	if sig.signals[0] != syscall.SIGTERM {
		t.Errorf("first signal: expected SIGTERM, got %v", sig.signals[0])
	}
	if sig.signals[len(sig.signals)-1] != syscall.SIGKILL {
		t.Errorf("last signal: expected SIGKILL, got %v", sig.signals[len(sig.signals)-1])
	}
}

func TestGracefulStop_FindPIDError(t *testing.T) {
	sig := &mockSignaler{findErr: fmt.Errorf("not found")}
	if err := GracefulStop(context.Background(), sig, "seid", time.Second); err == nil {
		t.Fatal("expected error")
	}
}

func TestGracefulStop_FindPIDProcessGone(t *testing.T) {
	sig := &mockSignaler{findErr: syscall.ESRCH}
	err := GracefulStop(context.Background(), sig, "seid", time.Second)
	if err != nil {
		t.Fatalf("expected success when process already gone, got %v", err)
	}
	if len(sig.signals) != 0 {
		t.Errorf("expected no signals sent, got %v", sig.signals)
	}
}

func TestGracefulStop_SIGTERMProcessGone(t *testing.T) {
	sig := &mockSignaler{
		findPID: 42,
		signalFn: func(_ int, s syscall.Signal) error {
			if s == syscall.SIGTERM {
				return os.ErrProcessDone
			}
			return nil
		},
	}
	err := GracefulStop(context.Background(), sig, "seid", time.Second)
	if err != nil {
		t.Fatalf("expected success when process exited before SIGTERM, got %v", err)
	}
}

func TestGracefulStop_SIGKILLProcessGone(t *testing.T) {
	sig := &mockSignaler{
		findPID: 42,
		signalFn: func(_ int, s syscall.Signal) error {
			if s == syscall.SIGKILL {
				return syscall.ESRCH
			}
			return nil
		},
	}
	sig.alive.Store(true)

	err := GracefulStop(context.Background(), sig, "seid", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success when process exited before SIGKILL, got %v", err)
	}
}

func TestGracefulStop_SignalError(t *testing.T) {
	sig := &mockSignaler{findPID: 42, signalErr: fmt.Errorf("permission denied")}
	if err := GracefulStop(context.Background(), sig, "seid", time.Second); err == nil {
		t.Fatal("expected error")
	}
}

func TestGracefulStop_ContextCancellation(t *testing.T) {
	sig := &mockSignaler{findPID: 42}
	sig.alive.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := GracefulStop(ctx, sig, "seid", 10*time.Second)
	if err == nil {
		t.Fatal("expected context error")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestFirstArg(t *testing.T) {
	tests := []struct {
		name     string
		cmdline  []byte
		expected string
	}{
		{"null delimited", []byte("seid\x00start\x00--home\x00/sei"), "seid"},
		{"no null byte", []byte("seid"), "seid"},
		{"full path", []byte("/usr/bin/seid\x00start"), "/usr/bin/seid"},
		{"null at start", []byte("\x00rest"), ""},
		{"empty", []byte{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstArg(tt.cmdline)
			if got != tt.expected {
				t.Errorf("firstArg(%q) = %q, want %q", tt.cmdline, got, tt.expected)
			}
		})
	}
}

func TestIsProcessGone(t *testing.T) {
	if !isProcessGone(os.ErrProcessDone) {
		t.Error("expected ErrProcessDone to be recognized")
	}
	if !isProcessGone(syscall.ESRCH) {
		t.Error("expected ESRCH to be recognized")
	}
	if isProcessGone(fmt.Errorf("something else")) {
		t.Error("expected generic error to not be recognized")
	}
	if isProcessGone(nil) {
		t.Error("expected nil to not be recognized")
	}
}
