package http

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// RateLimitOptions configures the rate-limit middleware (parity with TS
// rate-limit.ts). Redis is used when set; otherwise a per-process in-memory
// store is local development compatibility only, not a distributed quota.
// A configured Redis outage fails closed; weighted production budgets use policy.Take.
type RateLimitOptions struct {
	Max         int
	Window      time.Duration
	Redis       *redis.Client
	KeyFunc     func(*http.Request) string
	RoutePrefix string
}

// RateLimit returns a chi-compatible middleware enforcing the limit, setting
// X-RateLimit-* headers and returning 429 when exceeded (i12 load-shedding).
func RateLimit(opts RateLimitOptions) func(http.Handler) http.Handler {
	maxN := opts.Max
	if maxN <= 0 {
		maxN = 100
	}
	window := opts.Window
	if window <= 0 {
		window = 60 * time.Second
	}
	keyFunc := opts.KeyFunc
	if keyFunc == nil {
		keyFunc = clientIP
	}
	store := newMemStore()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.RoutePrefix != "" && !(r.URL.Path == strings.TrimSuffix(opts.RoutePrefix, "/") || strings.HasPrefix(r.URL.Path, strings.TrimSuffix(opts.RoutePrefix, "/")+"/")) {
				next.ServeHTTP(w, r)
				return
			}
			principal := keyFunc(r)
			if principal == "" || len(principal) > 512 {
				WriteErrorEnvelope(w, http.StatusBadRequest, CodeFieldInvalid, "invalid request principal", false, 0, []v2.FieldError{{Pointer: "", Rule: RulePrincipal}}, nil)
				return
			}
			key := opts.RoutePrefix + ":" + principal

			var allowed bool
			var remaining int
			if opts.Redis != nil {
				a, rem, err := checkRedis(r.Context(), opts.Redis, key, maxN, window)
				if err != nil {
					WriteErrorEnvelope(w, http.StatusServiceUnavailable, CodeDependencyUnavailable, "request budget unavailable", true, 0, nil, &v2.ErrorDetails{Dependency: v2.Optional[string]{Present: true, Value: "request_budget"}})
					return
				}
				allowed, remaining = a, rem
			} else {
				allowed, remaining = store.check(key, maxN, window)
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(maxN))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			if !allowed {
				WriteRateLimited(w, int(window.Seconds())+1,
					"rate limit exceeded, try again in "+strconv.Itoa(int(window.Seconds()))+"s")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func checkRedis(ctx context.Context, rdb *redis.Client, key string, maxN int, window time.Duration) (allowed bool, remaining int, err error) {
	full := fmt.Sprintf("ratelimit:%x", sha256.Sum256([]byte(key)))
	if window < time.Millisecond {
		window = time.Millisecond
	}
	current, err := rdb.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 or redis.call('PTTL',KEYS[1])<0 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return n`, []string{full}, window.Milliseconds()).Int64()
	if err != nil {
		return false, 0, err
	}

	rem := maxN - int(current)
	if rem < 0 {
		rem = 0
	}
	return current <= int64(maxN), rem, nil
}

type memEntry struct {
	count   int
	resetAt time.Time
}

type memStore struct {
	mu        sync.Mutex
	entries   map[string]memEntry
	lastSweep time.Time
}

func newMemStore() *memStore {
	return &memStore{entries: make(map[string]memEntry), lastSweep: time.Now()}
}

func (s *memStore) check(key string, maxN int, window time.Duration) (allowed bool, remaining int) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Opportunistic sweep (no background goroutine -> no leak).
	if now.Sub(s.lastSweep) > time.Minute {
		for k, e := range s.entries {
			if !e.resetAt.After(now) {
				delete(s.entries, k)
			}
		}
		s.lastSweep = now
	}

	if len(key) > 512 {
		return false, 0
	}
	e, ok := s.entries[key]
	if !ok || !e.resetAt.After(now) {
		if !ok && len(s.entries) >= 10000 {
			return false, 0
		}
		s.entries[key] = memEntry{count: 1, resetAt: now.Add(window)}
		return true, maxN - 1
	}
	e.count++
	s.entries[key] = e
	rem := maxN - e.count
	if rem < 0 {
		rem = 0
	}
	return e.count <= maxN, rem
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
