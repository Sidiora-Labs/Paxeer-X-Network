package http

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Check is a readiness probe returning nil when the dependency is healthy.
type Check func(ctx context.Context) error

// HealthDeps wires the readiness checks (parity with TS HealthDependencies).
// Decoupled from concrete pgx/redis types so http stays dependency-light.
type HealthDeps struct {
	DB     Check
	Redis  Check
	Custom map[string]Check
	State  Check
}

// RegisterHealth registers /health, /health/live (liveness) and /health/ready
// (deep readiness). Ready returns 503 when any dependency check fails (i6).
func RegisterHealth(r chi.Router, deps HealthDeps) {
	r.Get("/health", liveHandler)
	r.Get("/health/live", liveHandler)
	r.Get("/health/ready", readyHandler(deps))
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

const ReadinessTimeout = 2 * time.Second

var checkName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func readyHandler(deps HealthDeps) http.HandlerFunc {
	checks := map[string]Check{"database": deps.DB, "redis": deps.Redis}
	for name, check := range deps.Custom {
		if _, reserved := checks[name]; reserved || name == "lifecycle" {
			checks["invalid_registration"] = nil
			continue
		}
		checks[name] = check
	}
	if deps.State != nil {
		checks["lifecycle"] = deps.State
	}
	var mu sync.Mutex
	inflight := map[string]bool{}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), ReadinessTimeout)
		defer cancel()
		type result struct {
			name string
			err  error
		}
		results := make(chan result, len(checks))
		statuses := map[string]string{}
		pending := 0
		healthy := true
		for name, check := range checks {
			if !checkName.MatchString(name) {
				statuses["invalid_check"] = "failed"
				healthy = false
				continue
			}
			if check == nil {
				statuses[name] = "missing"
				healthy = false
				continue
			}
			mu.Lock()
			busy := inflight[name]
			if !busy {
				inflight[name] = true
			}
			mu.Unlock()
			if busy {
				statuses[name] = "busy"
				healthy = false
				continue
			}
			pending++
			go func(name string, check Check) {
				var err error
				defer func() {
					if recover() != nil {
						err = fmt.Errorf("readiness_check_panicked")
					}
					mu.Lock()
					delete(inflight, name)
					mu.Unlock()
					results <- result{name, err}
				}()
				err = check(ctx)
			}(name, check)
		}
		for pending > 0 {
			select {
			case result := <-results:
				pending--
				if result.err != nil {
					statuses[result.name] = "failed"
					healthy = false
				} else {
					statuses[result.name] = "ok"
				}
			case <-ctx.Done():
				healthy = false
				pending = 0
				for name := range checks {
					if _, ok := statuses[name]; !ok {
						statuses[name] = "timeout"
					}
				}
			}
		}
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		statuses["memory_alloc_mb"] = formatMB(mem.Alloc)
		status, code := "ok", http.StatusOK
		if !healthy {
			status, code = "degraded", http.StatusServiceUnavailable
		}
		WriteJSON(w, code, map[string]any{"status": status, "timestamp": time.Now().UTC().Format(time.RFC3339), "checks": statuses})
	}
}

func formatMB(b uint64) string {
	const mb = 1024 * 1024
	v := b / mb
	return itoa(v) + "MB"
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
