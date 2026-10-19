package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeSignaler implements actions.ProcessSignaler for restart-seid tests.
type fakeSignaler struct {
	findPID  int
	findErr  error
	alive    atomic.Bool
	signals  []syscall.Signal
	signalFn func(pid int, sig syscall.Signal) error
}

func (f *fakeSignaler) FindPID(string) (int, error) { return f.findPID, f.findErr }

func (f *fakeSignaler) Signal(pid int, sig syscall.Signal) error {
	f.signals = append(f.signals, sig)
	if f.signalFn != nil {
		return f.signalFn(pid, sig)
	}
	return nil
}

func (f *fakeSignaler) Alive(int) bool { return f.alive.Load() }

// upAfter returns a probe that reports down for the first n calls, then up.
func upAfter(n int) func(context.Context) bool {
	var calls int32
	return func(context.Context) bool {
		return atomic.AddInt32(&calls, 1) > int32(n)
	}
}

func neverUp(context.Context) bool { return false }

func TestRestartSeider_HappyPath(t *testing.T) {
	sig := &fakeSignaler{findPID: 42}
	sig.alive.Store(false) // exits immediately after SIGTERM

	r := &RestartSeider{
		signaler:    sig,
		probeUp:     upAfter(0), // up on first probe
		gracePeriod: time.Second,
		upTimeout:   time.Second,
		upInterval:  time.Millisecond,
	}

	if _, err := r.Handler()(context.Background(), nil); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(sig.signals) != 1 || sig.signals[0] != syscall.SIGTERM {
		t.Errorf("expected single SIGTERM, got %v", sig.signals)
	}
}

func TestRestartSeider_GraceTimeoutFailsWithoutSIGKILL(t *testing.T) {
	sig := &fakeSignaler{findPID: 42}
	sig.alive.Store(true) // never exits on SIGTERM

	r := &RestartSeider{
		signaler:    sig,
		probeUp:     upAfter(0),
		gracePeriod: 50 * time.Millisecond,
		upTimeout:   time.Second,
		upInterval:  time.Millisecond,
	}

	_, err := r.Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected grace-timeout failure, got nil")
	}
	if !strings.Contains(err.Error(), "still alive") {
		t.Errorf("expected still-alive error, got %v", err)
	}
	if len(sig.signals) != 1 || sig.signals[0] != syscall.SIGTERM {
		t.Errorf("expected single SIGTERM and no SIGKILL, got %v", sig.signals)
	}
}

func TestRestartSeider_NotFoundRPCDownWaitsForUp(t *testing.T) {
	sig := &fakeSignaler{findErr: fmt.Errorf("process \"seid\" not found in /proc")}

	r := &RestartSeider{
		signaler:    sig,
		probeUp:     upAfter(2), // RPC down (not-found guard + first poll), then up
		gracePeriod: time.Second,
		upTimeout:   time.Second,
		upInterval:  time.Millisecond,
	}

	if _, err := r.Handler()(context.Background(), nil); err != nil {
		t.Fatalf("expected success when seid not found and RPC down, got %v", err)
	}
	if len(sig.signals) != 0 {
		t.Errorf("expected no signals when seid not running, got %v", sig.signals)
	}
}

func TestRestartSeider_NotFoundRPCUpFails(t *testing.T) {
	sig := &fakeSignaler{findErr: fmt.Errorf("process \"seid\" not found in /proc")}

	r := &RestartSeider{
		signaler:    sig,
		probeUp:     upAfter(0), // RPC serving despite no /proc match
		gracePeriod: time.Second,
		upTimeout:   time.Second,
		upInterval:  time.Millisecond,
	}

	_, err := r.Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when RPC up but process not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found in /proc") {
		t.Errorf("expected not-found-in-/proc error, got %v", err)
	}
	if len(sig.signals) != 0 {
		t.Errorf("expected no signals, got %v", sig.signals)
	}
}

func TestRestartSeider_WaitForUpContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &RestartSeider{
		signaler:    &fakeSignaler{findErr: fmt.Errorf("process \"seid\" not found in /proc")},
		probeUp:     neverUp,
		gracePeriod: time.Second,
		upTimeout:   time.Minute,
		upInterval:  10 * time.Millisecond,
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := r.waitForUp(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRestartSeider_RPCNeverUpTimesOut(t *testing.T) {
	sig := &fakeSignaler{findPID: 42}
	sig.alive.Store(false)

	r := &RestartSeider{
		signaler:    sig,
		probeUp:     neverUp,
		gracePeriod: time.Second,
		upTimeout:   50 * time.Millisecond,
		upInterval:  time.Millisecond,
	}

	_, err := r.Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestIsSeidStart(t *testing.T) {
	tests := []struct {
		name    string
		cmdline []byte
		want    bool
	}{
		{"bare seid start", []byte("seid\x00start\x00--home\x00/.sei"), true},
		{"absolute path seid start", []byte("/usr/bin/seid\x00start"), true},
		{"trailing null", []byte("seid\x00start\x00"), true},
		{"seid non-start subcommand", []byte("seid\x00version"), false},
		{"seid-init", []byte("seid-init\x00start"), false},
		{"bash wrapper", []byte("bash\x00-c\x00seid start"), false},
		{"seid no args", []byte("seid"), false},
		{"empty", []byte{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSeidStart(tt.cmdline); got != tt.want {
				t.Errorf("isSeidStart(%q) = %v, want %v", tt.cmdline, got, tt.want)
			}
		})
	}
}
