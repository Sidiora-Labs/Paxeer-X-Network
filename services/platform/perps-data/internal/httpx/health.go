// Package httpx serves the liveness and readiness probes of a perps data
// service. Origin: Launch-x shared/http, on the standard library ServeMux
// instead of chi.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Check is a readiness probe returning nil when the dependency is healthy.
type Check func(ctx context.Context) error

// HealthDeps wires the readiness checks. Postgres is probed when set; Custom
// names the service-specific checks; State reports the process lifecycle.
type HealthDeps struct {
	Postgres Check
	Custom   map[string]Check
	State    Check
}

// RegisterHealth registers /healthz/live (liveness) and /healthz/ready (deep
// readiness). Ready returns 503 when any dependency check fails.
func RegisterHealth(mux *http.ServeMux, deps HealthDeps) {
	mux.HandleFunc("GET /healthz/live", liveHandler)
	mux.Handle("GET /healthz/ready", readyHandler(deps))
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// ReadinessTimeout bounds one readiness evaluation.
const ReadinessTimeout = 2 * time.Second

var checkName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func readyHandler(deps HealthDeps) http.HandlerFunc {
	checks := map[string]Check{}
	if deps.Postgres != nil {
		checks["postgres"] = deps.Postgres
	}
	for name, check := range deps.Custom {
		if name == "postgres" || name == "lifecycle" {
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
						err = errors.New("readiness_check_panicked")
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
			case res := <-results:
				pending--
				if res.err != nil {
					statuses[res.name] = "failed"
					healthy = false
				} else {
					statuses[res.name] = "ok"
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
		statuses["memory_alloc_mb"] = strconv.FormatUint(mem.Alloc/(1024*1024), 10) + "MB"
		status, code := "ok", http.StatusOK
		if !healthy {
			status, code = "degraded", http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"status": status, "timestamp": time.Now().UTC().Format(time.RFC3339), "checks": statuses})
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
