package paxlog

import (
	"context"
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"
)

var _ slog.Handler = (*discardHandler)(nil)

// discardHandler is a minimal slog.Handler that discards all output.
// Used to isolate handler overhead from I/O cost.
type discardHandler struct {
	level slog.Leveler
	attrs []slog.Attr
	group string
}

func newDiscardHandler(l slog.Leveler) *discardHandler                 { return &discardHandler{level: l} }
func (h *discardHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }
func (h *discardHandler) Handle(context.Context, slog.Record) error    { return nil }
func (h *discardHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(merged, h.attrs)
	copy(merged[len(h.attrs):], attrs)
	return &discardHandler{level: h.level, attrs: merged, group: h.group}
}

func (h *discardHandler) WithGroup(name string) slog.Handler {
	return &discardHandler{level: h.level, attrs: h.attrs, group: name}
}

// setupPaxlog configures paxlog to write JSON to io.Discard and returns
// a cleanup function. It resets global state so benchmarks are isolated.
func setupPaxlog(b *testing.B) func() {
	b.Helper()
	mu.Lock()
	// Save and reset global state.
	oldRegistry := registry
	oldHandler := handler.Load()
	oldDefault := defaultLevel.Level()

	registry = make(map[string]*slog.LevelVar)
	defaultLevel.Set(slog.LevelInfo)

	h := slog.Handler(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.Level(math.MinInt),
	}))
	handler.Store(&h)
	mu.Unlock()

	return func() {
		mu.Lock()
		registry = oldRegistry
		handler.Store(oldHandler)
		defaultLevel.Set(oldDefault)
		mu.Unlock()
	}
}

// newStdlibLogger creates a standard slog.Logger writing JSON to io.Discard.
func newStdlibLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// Benchmark: Info with simple string attrs (the most common case)
// ---------------------------------------------------------------------------

func BenchmarkInfo_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "info")

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("request handled", "method", "GET", "path", "/api/v1/users", "status", 200)
	}
}

func BenchmarkInfo_Stdlib(b *testing.B) {
	log := newStdlibLogger()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("request handled", "method", "GET", "path", "/api/v1/users", "status", 200)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Debug when level is Info (disabled path — should be near-zero)
// ---------------------------------------------------------------------------

func BenchmarkDisabledLevel_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "disabled")
	// Default level is Info, so Debug calls should be filtered.

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Debug("this should be skipped", "key", "value")
	}
}

func BenchmarkDisabledLevel_Stdlib(b *testing.B) {
	log := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Debug("this should be skipped", "key", "value")
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Structured attrs with slog.String/slog.Int (typed API)
// ---------------------------------------------------------------------------

func BenchmarkTypedAttrs_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "typed")

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.LogAttrs(nil, slog.LevelInfo, "request",
			slog.String("method", "POST"),
			slog.String("path", "/api/v1/orders"),
			slog.Int("status", 201),
			slog.Int("bytes", 4096),
		)
	}
}

func BenchmarkTypedAttrs_Stdlib(b *testing.B) {
	log := newStdlibLogger()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.LogAttrs(nil, slog.LevelInfo, "request",
			slog.String("method", "POST"),
			slog.String("path", "/api/v1/orders"),
			slog.Int("status", 201),
			slog.Int("bytes", 4096),
		)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: With() pre-bound attrs (logger created once, used many times)
// ---------------------------------------------------------------------------

func BenchmarkWithAttrs_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "with").With("request-id", "abc-123", "user-id", 42)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("query executed", "duration-ms", 15)
	}
}

func BenchmarkWithAttrs_Stdlib(b *testing.B) {
	log := newStdlibLogger().With("request-id", "abc-123", "user-id", 42)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("query executed", "duration-ms", 15)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Parallel logging from multiple goroutines
// ---------------------------------------------------------------------------

func BenchmarkParallel_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "parallel")

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.Info("concurrent write", "goroutine", "worker", "item", 7)
		}
	})
}

func BenchmarkParallel_Stdlib(b *testing.B) {
	log := newStdlibLogger()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.Info("concurrent write", "goroutine", "worker", "item", 7)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark: Mixed read/write contention — logging while SetLevel runs
// ---------------------------------------------------------------------------

func BenchmarkContention_Paxlog(b *testing.B) {
	cleanup := setupPaxlog(b)
	defer cleanup()
	log := NewLogger("bench", "contention")

	// Background goroutine toggling levels during the benchmark.
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		toggle := true
		for {
			select {
			case <-done:
				return
			default:
				if toggle {
					SetLevel("bench/contention", slog.LevelDebug)
				} else {
					SetLevel("bench/contention", slog.LevelInfo)
				}
				toggle = !toggle
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.Info("under contention", "key", "value")
		}
	})
	b.StopTimer()

	close(done)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Benchmark: NewLogger creation (cold vs warm — registry hit)
// ---------------------------------------------------------------------------

func BenchmarkNewLogger_Cold_Paxlog(b *testing.B) {
	// Each iteration creates a unique logger name (registry miss).
	cleanup := setupPaxlog(b)
	defer cleanup()

	names := make([]string, b.N)
	for i := range names {
		// Pre-generate valid names to keep alloc noise out of the hot loop.
		names[i] = "bench"
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// All hit the same name after first iteration — measures the
		// double-checked lock fast path after warmup.
		_ = NewLogger(names[i])
	}
}

func BenchmarkNewLogger_Warm_Paxlog(b *testing.B) {
	// Logger already registered — pure RLock path.
	cleanup := setupPaxlog(b)
	defer cleanup()
	_ = NewLogger("bench", "warm") // seed

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewLogger("bench", "warm")
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Text handler comparison
// ---------------------------------------------------------------------------

func BenchmarkInfoText_Paxlog(b *testing.B) {
	mu.Lock()
	oldRegistry := registry
	oldHandler := handler.Load()
	oldDefault := defaultLevel.Level()

	registry = make(map[string]*slog.LevelVar)
	defaultLevel.Set(slog.LevelInfo)
	h := slog.Handler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.Level(math.MinInt),
	}))
	handler.Store(&h)
	mu.Unlock()

	defer func() {
		mu.Lock()
		registry = oldRegistry
		handler.Store(oldHandler)
		defaultLevel.Set(oldDefault)
		mu.Unlock()
	}()

	log := NewLogger("bench", "text")

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("request handled", "method", "GET", "path", "/api/v1/users", "status", 200)
	}
}

func BenchmarkInfoText_Stdlib(b *testing.B) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		log.Info("request handled", "method", "GET", "path", "/api/v1/users", "status", 200)
	}
}
