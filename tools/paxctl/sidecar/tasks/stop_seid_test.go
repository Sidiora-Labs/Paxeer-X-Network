package tasks

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"
)

// newStopSeider builds a StopSeider wired to a test signaler and probe.
func newStopSeider(sig *fakeSignaler, probeUp func(context.Context) bool, grace time.Duration) *StopSeider {
	return &StopSeider{
		stopper: seidStopper{
			signaler:         sig,
			probeUp:          probeUp,
			gracePeriod:      grace,
			exitPollInterval: time.Millisecond,
			log:              stopSeidLog,
			op:               "stop",
		},
	}
}

func TestStopSeider_StopsAndDoesNotWaitForUp(t *testing.T) {
	sig := &fakeSignaler{findPID: 42}
	sig.alive.Store(false) // exits immediately after SIGTERM

	// neverUp would hang restart-seid's waitForUp; stop-seid must ignore it.
	if _, err := newStopSeider(sig, neverUp, time.Second).Handler()(context.Background(), nil); err != nil {
		t.Fatalf("expected success without waiting for up, got %v", err)
	}
	if len(sig.signals) != 1 || sig.signals[0] != syscall.SIGTERM {
		t.Errorf("expected single SIGTERM, got %v", sig.signals)
	}
}

func TestStopSeider_GraceTimeoutFailsWithoutSIGKILL(t *testing.T) {
	sig := &fakeSignaler{findPID: 42}
	sig.alive.Store(true) // never exits

	_, err := newStopSeider(sig, neverUp, 50*time.Millisecond).Handler()(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "still alive") {
		t.Fatalf("expected still-alive failure, got %v", err)
	}
	if len(sig.signals) != 1 || sig.signals[0] != syscall.SIGTERM {
		t.Errorf("expected single SIGTERM and no SIGKILL, got %v", sig.signals)
	}
}

func TestStopSeider_NotFoundRPCDownIsSuccess(t *testing.T) {
	sig := &fakeSignaler{findErr: fmt.Errorf("process \"seid\" not found in /proc")}

	if _, err := newStopSeider(sig, neverUp, time.Second).Handler()(context.Background(), nil); err != nil {
		t.Fatalf("expected success when seid absent and RPC down, got %v", err)
	}
	if len(sig.signals) != 0 {
		t.Errorf("expected no signals when seid not running, got %v", sig.signals)
	}
}

func TestStopSeider_NotFoundRPCUpRefuses(t *testing.T) {
	sig := &fakeSignaler{findErr: fmt.Errorf("process \"seid\" not found in /proc")}

	_, err := newStopSeider(sig, upAfter(0), time.Second).Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected refusal when RPC serves but process not found")
	}
	if !strings.Contains(err.Error(), "stop that did not happen") {
		t.Errorf("expected stop-that-did-not-happen refusal, got %v", err)
	}
	if len(sig.signals) != 0 {
		t.Errorf("expected no signals on refusal, got %v", sig.signals)
	}
}
