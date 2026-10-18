// Package process provides graceful-shutdown lifecycle handling, porting the TS
// shared registerProcessHandlers (shared/src/process/lifecycle.ts): on SIGTERM/
// SIGINT (or parent context cancellation) it runs the shutdown hook bounded by a
// timeout, so in-flight work drains before exit (invariant i7).
package process

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// DefaultTimeout matches the TS default (SH-2: raised from 10s to 30s).
const DefaultTimeout = 30 * time.Second
const PropagationDelay = 5 * time.Second
const TerminationGrace = 45 * time.Second

// ShutdownFunc drains in-flight work and closes resources. The context carries
// the shutdown deadline.
type ShutdownFunc func(context.Context) error

// Options configures Run.
type Options struct {
	// Logger is required; nil disables logging.
	Logger *slog.Logger
	// OnShutdown is invoked once a shutdown trigger fires. Required.
	OnShutdown ShutdownFunc
	// Timeout bounds OnShutdown. Zero uses DefaultTimeout.
	Timeout time.Duration
	// Signals overrides the default trigger set (SIGTERM, SIGINT).
	Signals []os.Signal
	Tracker *Tracker
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return DefaultTimeout
	}
	return o.Timeout
}

func (o Options) signals() []os.Signal {
	if len(o.Signals) == 0 {
		return []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	}
	return o.Signals
}

// Run blocks until a shutdown signal is received or ctx is cancelled, then runs
// OnShutdown under a timeout-bounded context. It returns the shutdown hook's
// error, or context.DeadlineExceeded if the hook overruns the timeout. Run never
// calls os.Exit, leaving the exit code to the caller (testable).
func Run(ctx context.Context, opts Options) error { //nolint:contextcheck // shutdown ctx is intentionally rooted at Background() (see below) so the drain window outlives the already-cancelled parent
	if opts.OnShutdown == nil {
		return errors.New("shutdown_hook_missing")
	}
	if opts.Timeout < 0 || opts.Timeout > DefaultTimeout {
		return errors.New("shutdown_timeout_out_of_range")
	}
	sigCtx, stop := signal.NotifyContext(ctx, opts.signals()...)
	defer stop()

	<-sigCtx.Done()
	if opts.Tracker != nil {
		opts.Tracker.BeginDrain()
		timer := time.NewTimer(PropagationDelay)
		<-timer.C
	}
	if opts.Logger != nil {
		opts.Logger.Info("received shutdown trigger, draining")
	}

	// The shutdown context is intentionally rooted at context.Background(), not
	// the incoming ctx: by the time we get here ctx is already cancelled (that is
	// what triggered shutdown), so inheriting it would cancel the drain window
	// immediately. The timeout is the sole bound on OnShutdown.
	shutCtx, cancel := context.WithTimeout(context.Background(), opts.timeout())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		if opts.Tracker == nil {
			done <- opts.OnShutdown(shutCtx)
			return
		}
		tracked := make(chan error, 1)
		go func() { tracked <- opts.Tracker.Drain(shutCtx) }()
		err := opts.OnShutdown(shutCtx)
		done <- errors.Join(err, <-tracked)
	}()

	select {
	case err := <-done:
		if opts.Logger != nil {
			if err != nil {
				opts.Logger.Error("error during shutdown", slog.Any("err", err))
			} else {
				opts.Logger.Info("graceful shutdown complete")
			}
		}
		return err
	case <-shutCtx.Done():
		if opts.Logger != nil {
			opts.Logger.Error("graceful shutdown timed out")
		}
		return shutCtx.Err()
	}
}

type Tracker struct {
	mu              sync.Mutex
	ready, draining bool
	active          int
	changed         chan struct{}
	connections     map[net.Conn]struct{}
}

func NewTracker() *Tracker {
	return &Tracker{changed: make(chan struct{}), connections: make(map[net.Conn]struct{})}
}
func (t *Tracker) signal() {
	if t.changed != nil {
		close(t.changed)
	}
	t.changed = make(chan struct{})
}
func (t *Tracker) SetReady() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.draining {
		return errors.New("service_draining")
	}
	t.ready = true
	t.signal()
	return nil
}
func (t *Tracker) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ready || t.draining {
		return errors.New("service_not_ready")
	}
	return nil
}
func (t *Tracker) BeginDrain() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ready = false
	t.draining = true
	t.signal()
}
func (t *Tracker) Acquire() (func(), error) {
	t.mu.Lock()
	if !t.ready || t.draining {
		t.mu.Unlock()
		return nil, errors.New("service_not_accepting_work")
	}
	t.active++
	t.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { t.mu.Lock(); t.active--; t.signal(); t.mu.Unlock() }) }, nil
}
func (t *Tracker) TrackConnection(conn net.Conn) (func(), error) {
	if conn == nil {
		return nil, errors.New("connection_missing")
	}
	t.mu.Lock()
	if !t.ready || t.draining {
		t.mu.Unlock()
		return nil, errors.New("service_not_accepting_work")
	}
	if t.connections == nil {
		t.connections = make(map[net.Conn]struct{})
	}
	if _, exists := t.connections[conn]; exists {
		t.mu.Unlock()
		return nil, errors.New("connection_already_tracked")
	}
	t.connections[conn] = struct{}{}
	t.active++
	t.mu.Unlock()
	return func() { t.releaseConnection(conn) }, nil
}
func (t *Tracker) releaseConnection(conn net.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.connections[conn]; exists {
		delete(t.connections, conn)
		t.active--
		t.signal()
	}
}
func (t *Tracker) ConnState(conn net.Conn, state http.ConnState) {
	switch state {
	case http.StateHijacked:
		_, err := t.TrackConnection(conn)
		if err != nil {
			t.mu.Lock()
			_, alreadyTracked := t.connections[conn]
			t.mu.Unlock()
			if !alreadyTracked {
				_ = conn.Close()
			}
		}
	case http.StateClosed:
		t.releaseConnection(conn)
	}
}
func (t *Tracker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, err := t.Acquire()
		if err != nil {
			http.Error(w, "service draining", http.StatusServiceUnavailable)
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}
func (t *Tracker) Drain(ctx context.Context) error {
	t.BeginDrain()
	t.mu.Lock()
	conns := make([]net.Conn, 0, len(t.connections))
	for conn := range t.connections {
		conns = append(conns, conn)
	}
	t.mu.Unlock()
	var closeErrors []error
	for _, conn := range conns {
		_ = conn.SetDeadline(time.Now())
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErrors = append(closeErrors, err)
		}
		t.releaseConnection(conn)
	}
	for {
		t.mu.Lock()
		active, changed := t.active, t.changed
		t.mu.Unlock()
		if active == 0 {
			return errors.Join(closeErrors...)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
