// Package cache provides the read-path response cache for core/api's snapshot
// REST surface: a bounded in-process LRU with per-entry TTL, strong ETags for
// conditional 304 responses, and singleflight stampede protection so a cold key
// hit by N concurrent requests triggers exactly ONE upstream fetch (SECTION 17
// caching + stampede protection). It sits in front of the Redis/Postgres reads
// in internal/store, shielding them from read-heavy bursts at 500K scale.
package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// Cache is a concurrency-safe LRU of JSON payloads with TTL + singleflight.
type Cache struct {
	origins                   map[string]int
	maxPerOrigin              int
	mu                        sync.Mutex
	ll                        *list.List
	items                     map[string]*list.Element
	capacity                  int
	maxBytes, maxEntry, bytes int64
	evictions                 uint64
	slots                     chan struct{}
	fetchTimeout              time.Duration
	inflight                  map[string]*flightCall

	hits   uint64
	misses uint64
}

type entry struct {
	key     string
	body    []byte
	etag    string
	expires time.Time
}

type flightCall struct {
	done chan struct{}
	body []byte
	etag string
	err  error
}

// New builds a Cache holding at most capacity entries (LRU eviction).
// Config bounds retained payloads, keys, metadata and concurrent origin work.
type Config struct {
	Capacity                int
	MaxBytes, MaxEntryBytes int64
	MaxFetches              int
	MaxPerOrigin            int
	FetchTimeout            time.Duration
}

func New(capacity int) *Cache { return NewWithConfig(Config{Capacity: capacity}) }
func NewWithConfig(cfg Config) *Cache {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1024
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 32 << 20
	}
	if cfg.MaxEntryBytes <= 0 {
		cfg.MaxEntryBytes = 1 << 20
	}
	if cfg.MaxFetches <= 0 {
		cfg.MaxFetches = 32
	}
	if cfg.MaxPerOrigin <= 0 {
		cfg.MaxPerOrigin = 8
	}
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = 10 * time.Second
	}
	return &Cache{origins: make(map[string]int), maxPerOrigin: cfg.MaxPerOrigin, ll: list.New(), items: make(map[string]*list.Element), capacity: cfg.Capacity, inflight: make(map[string]*flightCall), maxBytes: cfg.MaxBytes, maxEntry: cfg.MaxEntryBytes, slots: make(chan struct{}, cfg.MaxFetches), fetchTimeout: cfg.FetchTimeout}
}

var ErrOriginBusy = errors.New("cache: origin concurrency exhausted")

const entryOverhead int64 = 256

// MemoryStats includes conservative entry metadata accounting, not the Go heap.
func (c *Cache) MemoryStats() (bytes int64, entries int, evictions uint64, metadata int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes, c.ll.Len(), c.evictions, int64(c.ll.Len()) * entryOverhead
}

// FetchFunc produces a fresh payload for a cache miss.
type FetchFunc func(ctx context.Context) ([]byte, error)

// GetOrFetch returns the cached payload for key, or fetches+stores it. Concurrent
// callers for the same cold key share a single fetch (singleflight). A zero or
// negative ttl disables caching (the fetch result is returned but not stored).
func (c *Cache) GetOrFetch(ctx context.Context, key string, ttl time.Duration, fetch FetchFunc) ([]byte, string, error) {
	return c.GetOrFetchOrigin(ctx, "default", key, ttl, fetch)
}

// GetOrFetchOrigin separates dependency concurrency budgets. Origins are configured names.
func (c *Cache) GetOrFetchOrigin(ctx context.Context, origin, key string, ttl time.Duration, fetch FetchFunc) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(origin) == 0 || len(origin) > 128 || strings.IndexFunc(origin, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return nil, "", errors.New("cache: invalid origin")
	}
	key = origin + "\x00" + key
	if len(key) > 4096 {
		return nil, "", errors.New("cache: key too large")
	}
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		if time.Now().Before(e.expires) {
			c.ll.MoveToFront(el)
			c.hits++
			b, et := append([]byte(nil), e.body...), e.etag
			c.mu.Unlock()
			return b, et, nil
		}
		c.removeElement(el)
	}
	fc, ok := c.inflight[key]
	if !ok {
		if c.origins == nil {
			c.origins = make(map[string]int)
			c.maxPerOrigin = 8
		}
		if c.origins[origin] >= c.maxPerOrigin {
			c.mu.Unlock()
			return nil, "", ErrOriginBusy
		}
		select {
		case c.slots <- struct{}{}:
		default:
			c.mu.Unlock()
			return nil, "", ErrOriginBusy
		}
		if fetch == nil {
			<-c.slots
			c.mu.Unlock()
			return nil, "", errors.New("cache: missing origin fetcher")
		}
		c.origins[origin]++
		fc = &flightCall{done: make(chan struct{})}
		c.inflight[key] = fc
		c.misses++
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.fetchTimeout)
		go func() {
			defer cancel()
			defer func() { <-c.slots }()
			b, err := fetch(workCtx)
			fc.body = append([]byte(nil), b...)
			fc.err = err
			if err == nil {
				fc.etag = ETag(b)
			}
			c.mu.Lock()
			if err == nil && ttl > 0 {
				c.storeLocked(key, b, fc.etag, time.Now().Add(ttl))
			}
			delete(c.inflight, key)
			c.origins[origin]--
			if c.origins[origin] == 0 {
				delete(c.origins, origin)
			}
			close(fc.done)
			c.mu.Unlock()
		}()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-fc.done:
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		return append([]byte(nil), fc.body...), fc.etag, fc.err
	}
}

// storeLocked inserts/refreshes an entry and evicts the LRU tail if over cap.
func (c *Cache) storeLocked(key string, body []byte, etag string, expires time.Time) {
	cost := int64(len(key)+len(body)+len(etag)) + entryOverhead
	if int64(len(body)) > c.maxEntry || cost > c.maxBytes {
		return
	}
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
	el := c.ll.PushFront(&entry{key: key, body: append([]byte(nil), body...), etag: etag, expires: expires})
	c.items[key] = el
	c.bytes += cost
	for c.ll.Len() > c.capacity || c.bytes > c.maxBytes {
		c.removeElement(c.ll.Back())
		c.evictions++
	}
}

func (c *Cache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	if e, ok := el.Value.(*entry); ok {
		delete(c.items, e.key)
		c.bytes -= int64(len(e.key)+len(e.body)+len(e.etag)) + entryOverhead
	}
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Stats returns cumulative hit/miss counters (for metrics).
func (c *Cache) Stats() (hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// Handler wraps a JSON read as a cached, ETag-aware HTTP handler. keyFn derives
// the cache key from the request (typically path + relevant query); fetch loads
// the payload on a miss. On a matching If-None-Match it returns 304.
func (c *Cache) Handler(keyFn func(*http.Request) string, ttl time.Duration, fetch func(*http.Request) ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := keyFn(r)
		body, _, err := c.GetOrFetch(r.Context(), key, ttl, func(ctx context.Context) ([]byte, error) {
			return fetch(r.WithContext(ctx))
		})
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "read failed")
			return
		}
		ServeJSON(w, r, body, ttl)
	}
}

// ServeJSON writes a JSON body with a strong ETag and a public max-age derived
// from ttl (ttl<=0 => no-store), honouring a matching If-None-Match with 304.
// It is shared by Handler and by the manually-cached REST handlers so the
// conditional-request semantics are identical across the snapshot surface.
func ServeJSON(w http.ResponseWriter, r *http.Request, body []byte, ttl time.Duration) {
	etag := ETag(body)
	w.Header().Set("ETag", etag)
	secs := int(ttl / time.Second)
	if secs > 0 {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(secs))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		return
	}
}

// ETag computes a strong ETag from the payload (quoted hex of a truncated
// SHA-256). Identical bodies yield identical ETags.
func ETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// matchesETag reports whether the If-None-Match header matches etag. It honours
// the "*" wildcard and comma-separated lists, ignoring weak-validator prefixes.
func matchesETag(ifNoneMatch, etag string) bool {
	ifNoneMatch = strings.TrimSpace(ifNoneMatch)
	if ifNoneMatch == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// ServePrivateJSON uses a revision-bearing representation validator and forbids
// shared HTTP storage. Scope is an opaque versioned key built after authorization.
func ServePrivateJSON(w http.ResponseWriter, r *http.Request, body []byte, scope, revision string) {
	if scope == "" || revision == "" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		return
	}
	sum := sha256.New()
	sum.Write([]byte(scope))
	sum.Write([]byte{0})
	sum.Write([]byte(revision))
	sum.Write([]byte{0})
	sum.Write(body)
	etag := `"` + hex.EncodeToString(sum.Sum(nil)) + `"`
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", etag)
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// StartOrigin starts at most one bounded detached refresh without retaining a
// waiter goroutine. Cold readers join the same flight through GetOrFetchOrigin.
func (c *Cache) StartOrigin(ctx context.Context, origin, key string, fetch FetchFunc) error {
	if fetch == nil || origin == "" || len(origin) > 128 || strings.IndexFunc(origin, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return errors.New("cache: invalid refresh origin")
	}
	key = origin + "\x00" + key
	if len(key) > 4096 {
		return errors.New("cache: key too large")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.inflight[key]; ok {
		return nil
	}
	if c.origins[origin] >= c.maxPerOrigin {
		return ErrOriginBusy
	}
	select {
	case c.slots <- struct{}{}:
	default:
		return ErrOriginBusy
	}
	c.origins[origin]++
	fc := &flightCall{done: make(chan struct{})}
	c.inflight[key] = fc
	c.misses++
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.fetchTimeout)
	go func() {
		defer cancel()
		defer func() { <-c.slots }()
		b, e := fetch(work)
		fc.body = append([]byte(nil), b...)
		fc.err = e
		if e == nil {
			fc.etag = ETag(b)
		}
		c.mu.Lock()
		delete(c.inflight, key)
		c.origins[origin]--
		if c.origins[origin] == 0 {
			delete(c.origins, origin)
		}
		close(fc.done)
		c.mu.Unlock()
	}()
	return nil
}
