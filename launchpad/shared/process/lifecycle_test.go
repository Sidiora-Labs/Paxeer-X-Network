package process

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestRunLogsAllBranches(t *testing.T) {
	t.Parallel()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{Logger: lg, OnShutdown: func(context.Context) error { return nil }, Timeout: time.Second}); err != nil {
		t.Fatalf("success path: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if err := Run(ctx2, Options{Logger: lg, OnShutdown: func(context.Context) error { return errors.New("x") }, Timeout: time.Second}); err == nil {
		t.Fatal("error path: want error")
	}

	ctx3, cancel3 := context.WithCancel(context.Background())
	cancel3()
	if err := Run(ctx3, Options{Logger: lg, OnShutdown: func(c context.Context) error { <-c.Done(); return c.Err() }, Timeout: 20 * time.Millisecond}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout path: %v", err)
	}
}

func TestRunRunsShutdownOnContextCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{})
	go func() {
		_ = Run(ctx, Options{
			OnShutdown: func(context.Context) error { close(ran); return nil },
			Timeout:    time.Second,
		})
	}()
	cancel()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("OnShutdown was not invoked after context cancel")
	}
}

func TestRunReturnsShutdownError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantErr := errors.New("boom")
	err := Run(ctx, Options{
		OnShutdown: func(context.Context) error { return wantErr },
		Timeout:    time.Second,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run err = %v, want %v", err, wantErr)
	}
}

func TestRunTimesOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, Options{
		OnShutdown: func(c context.Context) error { <-c.Done(); return c.Err() },
		Timeout:    20 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want DeadlineExceeded", err)
	}
}

func TestRunRespondsToSignal(t *testing.T) {
	t.Parallel()
	ran := make(chan struct{})
	go func() {
		_ = Run(context.Background(), Options{
			Signals:    []os.Signal{syscall.SIGUSR1},
			OnShutdown: func(context.Context) error { close(ran); return nil },
			Timeout:    time.Second,
		})
	}()
	// Give Run time to install the signal handler before raising it.
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("OnShutdown not invoked after signal")
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	o := Options{}
	if o.timeout() != DefaultTimeout {
		t.Errorf("timeout default = %v, want %v", o.timeout(), DefaultTimeout)
	}
	if len(o.signals()) != 2 {
		t.Errorf("signals default = %v, want 2", o.signals())
	}
}
