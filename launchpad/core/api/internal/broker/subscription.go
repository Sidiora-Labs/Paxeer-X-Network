package broker

import (
	"strings"
	"sync"
	"sync/atomic"
)

// Filter selects which events a Subscription receives.
//
//   - All: when true the subscriber receives every channel (a wildcard).
//   - Channels: the explicit allowlist of Redis channels when All is false. An
//     empty set with All=false means "nothing" — the state of a freshly
//     connected client that has not yet sent a subscribe frame.
//   - Pools: the set of pool addresses of interest. Empty means "all pools".
//     Global events (those carrying no pool) are always delivered regardless of
//     this set.
type Filter struct {
	All      bool
	Channels map[string]struct{}
	Pools    map[string]struct{}
}

// normalized snapshots caller-owned maps and canonicalizes routing keys.
func (f Filter) normalized() Filter {
	out := Filter{All: f.All, Channels: make(map[string]struct{}, len(f.Channels)), Pools: make(map[string]struct{}, len(f.Pools))}
	for channel := range f.Channels {
		out.Channels[channel] = struct{}{}
	}
	for pool := range f.Pools {
		out.Pools[strings.ToLower(pool)] = struct{}{}
	}
	return out
}

// wantsChannel reports whether the filter includes channel.
func (f Filter) wantsChannel(channel string) bool {
	if f.All {
		return true
	}
	_, ok := f.Channels[channel]
	return ok
}

// wantsPool reports whether the filter includes pool. An empty pool (a global
// event) always matches; an empty Pools set matches every pool.
func (f Filter) wantsPool(pool string) bool {
	if pool == "" || len(f.Pools) == 0 {
		return true
	}
	_, ok := f.Pools[pool]
	return ok
}

// Subscription is one client's view of the fan-out. It holds a bounded buffer
// of pending frames split into two lanes:
//
//   - queue: ordered MUST-DELIVER frames (discrete events such as swaps and
//     market_created) that are never dropped while the buffer has room.
//   - pending: COALESCED frames (high-frequency state such as candle/pool_state
//     ticks) keyed by a coalesce key, so only the latest value per key survives
//     until the next flush — the RAF-style backpressure the spec calls for.
//
// When the combined buffer would exceed maxBuf the subscription is marked
// evicted: the transport then sends an error frame and closes the connection
// rather than letting fan-out block or grow unbounded (invariant i11).
//
// All transports drive a Subscription the same way: wait on Signal()/a flush
// ticker, then Drain() the batch and write it on the single connection writer.
type Subscription struct {
	id          uint64
	filter      atomic.Pointer[Filter]
	maxBuf      int
	queuedBytes int

	mu      sync.Mutex
	queue   [][]byte
	pending map[string][]byte
	order   []string

	signal  chan struct{}
	evicted atomic.Bool
	closed  atomic.Bool

	broker *Broker
}

func newSubscription(id uint64, filter Filter, maxBuf int, b *Broker) *Subscription {
	if maxBuf <= 0 || maxBuf > 128 {
		maxBuf = 128
	}
	s := &Subscription{
		id:      id,
		maxBuf:  maxBuf,
		pending: make(map[string][]byte),
		signal:  make(chan struct{}, 1),
		broker:  b,
	}
	s.filter.Store(&filter)
	return s
}

// ID returns the subscription's broker-unique id.
func (s *Subscription) ID() uint64 { return s.id }

// Signal returns a channel that receives a tick whenever must-deliver data is
// enqueued or the subscription is evicted. Coalesced frames do NOT signal; they
// are picked up on the transport's flush ticker.
func (s *Subscription) Signal() <-chan struct{} { return s.signal }

// Evicted reports whether the subscription overflowed its buffer.
func (s *Subscription) Evicted() bool { return s.evicted.Load() }

// size returns the current buffered frame count (caller holds s.mu).
func (s *Subscription) size() int { return len(s.queue) + len(s.pending) }

// deliver enqueues a frame produced by the broker. It never blocks: on overflow
// the subscription is evicted instead. Coalesced frames overwrite the latest
// value for their key.
func (s *Subscription) deliver(m OutMessage) {
	if s.closed.Load() || s.evicted.Load() {
		return
	}
	if !s.mu.TryLock() {
		s.evict()
		return
	}
	if s.closed.Load() || s.evicted.Load() {
		s.mu.Unlock()
		return
	}
	oldBytes := 0
	if old, ok := s.pending[m.coalesceKey]; ok {
		oldBytes = len(old)
	}
	if s.queuedBytes-oldBytes+len(m.bytes) > 256*1024 {
		s.mu.Unlock()
		s.evict()
		return
	}
	if m.coalesceKey != "" {
		if _, exists := s.pending[m.coalesceKey]; !exists {
			if s.size() >= s.maxBuf {
				s.mu.Unlock()
				s.evict()
				return
			}
			s.order = append(s.order, m.coalesceKey)
		}
		s.queuedBytes += len(m.bytes) - oldBytes
		s.pending[m.coalesceKey] = m.bytes
		s.mu.Unlock()
		return
	}
	if s.size() >= s.maxBuf {
		s.mu.Unlock()
		s.evict()
		return
	}
	s.queuedBytes += len(m.bytes)
	s.queue = append(s.queue, m.bytes)
	s.mu.Unlock()
	s.notify()
}

// evict marks the subscription as a slow consumer and wakes the writer so it can
// send an error frame and tear down.
func (s *Subscription) evict() {
	if s.evicted.CompareAndSwap(false, true) {
		s.notify()
	}
}

// notify performs a non-blocking wake of the transport writer.
func (s *Subscription) notify() {
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// Drain atomically returns all buffered frames: ordered must-deliver frames
// first, then the latest value of each coalesced key in arrival order. The
// buffer is reset. It returns nil when empty.
func (s *Subscription) Drain() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 && len(s.pending) == 0 {
		return nil
	}
	s.queuedBytes = 0
	out := s.queue
	s.queue = nil
	for _, k := range s.order {
		if b, ok := s.pending[k]; ok {
			out = append(out, b)
		}
	}
	if len(s.pending) > 0 {
		s.pending = make(map[string][]byte)
	}
	s.order = s.order[:0]
	return out
}

// Close removes the subscription from the broker. It is idempotent.
func (s *Subscription) Close() {
	if s.closed.CompareAndSwap(false, true) {
		s.broker.unregister(s)
		s.mu.Lock()
		s.queue = nil
		clear(s.pending)
		s.order = nil
		s.queuedBytes = 0
		s.mu.Unlock()
	}
}

// Occupancy returns bounded buffered frames and bytes.
func (s *Subscription) Occupancy() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size(), s.queuedBytes
}
