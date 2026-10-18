package readpolicy

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrReadCapacity = errors.New("public read capacity exhausted")

type readGroup struct {
	done    chan struct{}
	context context.Context
	cancel  context.CancelFunc
	waiters int
	value   [][]byte
	err     error
}
type cachedRead struct {
	value   [][]byte
	expires time.Time
	bytes   int64
}
type Coalescer struct {
	mu         sync.Mutex
	groups     map[string]*readGroup
	cache      map[string]cachedRead
	cacheBytes int64
	stats      CoalescingStats
}
type CoalescingStats struct {
	UpstreamGroups     uint64
	JoinedWaiters      uint64
	CacheHits          uint64
	CapacityRejections uint64
}

func (c *Coalescer) Stats() CoalescingStats { c.mu.Lock(); defer c.mu.Unlock(); return c.stats }
func NewCoalescer() *Coalescer {
	return &Coalescer{groups: make(map[string]*readGroup), cache: make(map[string]cachedRead)}
}
func cloneReads(in [][]byte) [][]byte {
	if in == nil {
		return nil
	}
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = append([]byte(nil), in[i]...)
	}
	return out
}
func (c *Coalescer) Do(ctx context.Context, key string, read func(context.Context) ([][]byte, error)) ([][]byte, error) {
	if ctx == nil || key == "" || len(key) > 256 || read == nil {
		return nil, errors.New("invalid public read group")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	now := time.Now()
	for k, v := range c.cache {
		if !now.Before(v.expires) {
			delete(c.cache, k)
			c.cacheBytes -= v.bytes
		}
	}
	if v, ok := c.cache[key]; ok {
		c.stats.CacheHits++
		out := cloneReads(v.value)
		c.mu.Unlock()
		return out, nil
	}
	g := c.groups[key]
	if g == nil {
		if len(c.groups) >= 128 {
			c.stats.CapacityRejections++
			c.mu.Unlock()
			return nil, ErrReadCapacity
		}
		c.stats.UpstreamGroups++
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultDeadline)
		g = &readGroup{done: make(chan struct{}), context: work, cancel: cancel}
		c.groups[key] = g
		go func(g *readGroup) {
			defer cancel()
			value, err := read(work)
			budgetError := work.Err()
			if err == nil && budgetError != nil {
				err = budgetError
			}
			if err != nil {
				value = nil
			}
			var size int64
			for _, v := range value {
				size += int64(len(v))
			}
			if size > MaxResponseBytes || len(value) > MaxBatch {
				value = nil
				err = errors.New("coalesced read exceeds payload budget")
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			g.value = cloneReads(value)
			g.err = err
			if c.groups[key] == g {
				delete(c.groups, key)
				if err == nil && size <= MaxResponseBytes-c.cacheBytes && len(c.cache) < 1024 {
					c.cache[key] = cachedRead{cloneReads(value), time.Now().Add(100 * time.Millisecond), size}
					c.cacheBytes += size
				}
			}
			close(g.done)
		}(g)
	} else {
		if g.context.Err() != nil || g.waiters >= 4096 {
			c.stats.CapacityRejections++
			c.mu.Unlock()
			return nil, ErrReadCapacity
		}
		c.stats.JoinedWaiters++
	}
	g.waiters++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		g.waiters--
		if g.waiters == 0 {
			select {
			case <-g.done:
			default:
				g.cancel()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return cloneReads(g.value), g.err
	case <-g.context.Done():
		select {
		case <-g.done:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return cloneReads(g.value), g.err
		default:
			return nil, g.context.Err()
		}
	}
}
