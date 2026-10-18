package rest

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"time"
)

var ErrCacheFillSaturated = errors.New("CACHE_FILL_SATURATED")
var ErrCacheBodyTooLarge = errors.New("CACHE_BODY_TOO_LARGE")

type publicEntry struct {
	key     string
	body    []byte
	expires time.Time
}
type publicFill struct {
	done chan struct{}
	body []byte
	err  error
}

// PublicCache must never receive account-scoped responses. Bodies are copied
// at both boundaries, and bytes include keys to avoid unbounded identifier use.
type PublicCache struct {
	mu                                           sync.Mutex
	entries                                      map[string]*list.Element
	lru                                          *list.List
	fills                                        map[string]*publicFill
	bytes, maxBytes, maxEntries, maxInflight     int
	timeout                                      time.Duration
	hits, misses, rejected, cancelled, evictions uint64
}

func NewPublicCache(maxBytes, maxEntries, maxInflight int, timeout time.Duration) (*PublicCache, error) {
	if maxBytes <= 0 || maxBytes > 64<<20 || maxEntries <= 0 || maxEntries > 4096 || maxInflight <= 0 || maxInflight > 128 || timeout <= 0 || timeout > 400*time.Millisecond {
		return nil, errors.New("invalid public cache bounds")
	}
	return &PublicCache{entries: map[string]*list.Element{}, lru: list.New(), fills: map[string]*publicFill{}, maxBytes: maxBytes, maxEntries: maxEntries, maxInflight: maxInflight, timeout: timeout}, nil
}
func (c *PublicCache) remove(el *list.Element) {
	e := el.Value.(*publicEntry)
	c.bytes -= len(e.key) + len(e.body)
	delete(c.entries, e.key)
	c.lru.Remove(el)
}
func (c *PublicCache) Usage() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes, len(c.entries), len(c.fills)
}

type PublicCacheMetrics struct {
	Bytes, Entries, Inflight, MetadataBound      int
	Hits, Misses, Rejected, Cancelled, Evictions uint64
	ProcessGoroutines                            int
}

func (c *PublicCache) Metrics() PublicCacheMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return PublicCacheMetrics{Bytes: c.bytes, Entries: len(c.entries), Inflight: len(c.fills), MetadataBound: len(c.entries)*256 + len(c.fills)*256, Hits: c.hits, Misses: c.misses, Rejected: c.rejected, Cancelled: c.cancelled, Evictions: c.evictions, ProcessGoroutines: runtime.NumGoroutine()}
}
func PublicETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}
func (c *PublicCache) GetOrFetch(ctx context.Context, key string, ttl time.Duration, fetch func(context.Context) ([]byte, error)) ([]byte, error) {
	if c == nil || ctx == nil {
		return nil, errors.New("invalid public cache request")
	}
	if err := ctx.Err(); err != nil {
		c.mu.Lock()
		c.cancelled++
		c.mu.Unlock()
		return nil, err
	}
	if len(key) > 512 || ttl <= 0 || ttl > time.Hour || fetch == nil {
		return nil, errors.New("invalid public cache request")
	}
	c.mu.Lock()
	if el := c.entries[key]; el != nil {
		e := el.Value.(*publicEntry)
		if time.Now().Before(e.expires) {
			c.lru.MoveToFront(el)
			c.hits++
			b := append([]byte(nil), e.body...)
			c.mu.Unlock()
			return b, nil
		}
		c.remove(el)
	}
	f := c.fills[key]
	if f == nil {
		if len(c.fills) >= c.maxInflight {
			c.rejected++
			c.mu.Unlock()
			return nil, ErrCacheFillSaturated
		}
		c.misses++
		f = &publicFill{done: make(chan struct{})}
		c.fills[key] = f
		// Shared work retains values but no caller cancellation or deadline. Its
		// own deadline bounds it independently of the first waiter's lifetime.
		go func() {
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.timeout)
			defer cancel()
			body, err := guardedPublicFetch(work, fetch)
			if err == nil {
				err = work.Err()
			}
			if err == nil && (len(body) > 256<<10 || len(body)+len(key) > c.maxBytes) {
				err = ErrCacheBodyTooLarge
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if err == nil {
				body = append([]byte(nil), body...)
				for c.bytes+len(body)+len(key) > c.maxBytes || len(c.entries) >= c.maxEntries {
					c.remove(c.lru.Back())
					c.evictions++
				}
				e := &publicEntry{key: key, body: body, expires: time.Now().Add(time.Duration(float64(ttl) * (0.9 + rand.Float64()*0.2)))}
				c.entries[key] = c.lru.PushFront(e)
				c.bytes += len(body) + len(key)
			}
			f.body, f.err = body, err
			delete(c.fills, key)
			close(f.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		c.cancelled++
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			c.mu.Lock()
			c.cancelled++
			c.mu.Unlock()
			return nil, err
		}
		if f.err != nil {
			return nil, f.err
		}
		return append([]byte(nil), f.body...), nil
	}
}

var marketPublicCache, _ = NewPublicCache(64<<20, 4096, 128, 400*time.Millisecond)

func guardedPublicFetch(ctx context.Context, fetch func(context.Context) ([]byte, error)) (body []byte, err error) {
	defer func() {
		if recover() != nil {
			body = nil
			err = errors.New("DATA_UNAVAILABLE")
		}
	}()
	return fetch(ctx)
}

func MarketCacheMetrics() PublicCacheMetrics { return marketPublicCache.Metrics() }
