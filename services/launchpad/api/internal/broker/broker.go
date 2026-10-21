// Package broker is the realtime fan-out spine of core/api. A single dedicated
// Redis subscriber consumes every channel the indexer/services publish on
// (shared/constants.Channels) and fans each event out to the matching client
// Subscriptions. It is transport-agnostic: both internal/ws and internal/sse
// drive Subscriptions identically (wait for Signal()/flush tick, then Drain()).
//
// Design for 500K concurrency (invariant i11): exactly ONE upstream Redis
// connection regardless of client count; per-subscription bounded buffers with
// slow-client eviction; latest-per-key coalescing of high-frequency state ticks.
// The fan-out marshals each frame ONCE and shares the bytes across all matching
// subscribers.
package broker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
)

// TransformFunc converts a raw channel payload into a client frame. It returns
// ok=false to drop a malformed payload. DefaultTransform is the production impl.
type TransformFunc func(channel string, payload []byte) (OutMessage, bool)

// Broker fans Redis pub/sub events out to client Subscriptions.
type Broker struct {
	rdb       *goredis.Client
	logger    *slog.Logger
	channels  []string
	transform TransformFunc

	mu        sync.RWMutex
	byChannel map[string]map[*Subscription]struct{}
	wildcard  map[*Subscription]struct{}

	nextID    atomic.Uint64
	delivered atomic.Uint64
	dropped   atomic.Uint64

	subCount atomic.Int64
}

// Options configures New.
type Options struct {
	// Redis is the dedicated subscriber client (the broker owns its lifecycle
	// only if created via NewFromURL; an injected client is closed by the caller).
	Redis *goredis.Client
	// Logger is required.
	Logger *slog.Logger
	// Channels overrides the subscribed channel set (defaults to all of
	// shared/constants.Channels).
	Channels []string
	// Transform overrides the payload->frame transform (defaults to
	// DefaultTransform).
	Transform TransformFunc
}

// New builds a Broker over an existing Redis client.
func New(opts Options) *Broker {
	chs := opts.Channels
	if len(chs) == 0 {
		chs = append(chs, constants.Channels...)
	}
	tf := opts.Transform
	if tf == nil {
		tf = DefaultTransform
	}
	return &Broker{
		rdb:       opts.Redis,
		logger:    opts.Logger,
		channels:  chs,
		transform: tf,
		byChannel: make(map[string]map[*Subscription]struct{}),
		wildcard:  make(map[*Subscription]struct{}),
	}
}

// Subscribe registers a new Subscription with the given filter and buffer size.
// The caller must Close it when the client disconnects.
func (b *Broker) Subscribe(filter Filter, bufSize int) *Subscription {
	s := newSubscription(b.nextID.Add(1), filter.normalized(), bufSize, b)
	b.mu.Lock()
	b.indexLocked(s)
	b.subCount.Add(1)
	b.mu.Unlock()
	return s
}

// Resubscribe atomically replaces a live subscription's filter and re-indexes
// it. It is how the WS/SSE transports apply incremental subscribe/unsubscribe
// frames without tearing down the connection. It is a no-op once the
// subscription is closed.
func (b *Broker) Resubscribe(s *Subscription, filter Filter) {
	if s == nil || s.broker != b {
		return
	}
	filter = filter.normalized()
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.closed.Load() {
		return
	}
	b.deindexLocked(s)
	s.filter.Store(&filter)
	b.indexLocked(s)
}

// indexLocked adds s to the routing index for its current filter (holds b.mu).
func (b *Broker) indexLocked(s *Subscription) {
	if s.filter.Load().All {
		b.wildcard[s] = struct{}{}
		return
	}
	for ch := range s.filter.Load().Channels {
		set, ok := b.byChannel[ch]
		if !ok {
			set = make(map[*Subscription]struct{})
			b.byChannel[ch] = set
		}
		set[s] = struct{}{}
	}
}

// deindexLocked removes s from the routing index (holds b.mu).
func (b *Broker) deindexLocked(s *Subscription) {
	delete(b.wildcard, s)
	for ch := range s.filter.Load().Channels {
		if set, ok := b.byChannel[ch]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(b.byChannel, ch)
			}
		}
	}
}

// unregister removes a subscription from the routing index.
func (b *Broker) unregister(s *Subscription) {
	b.mu.Lock()
	b.deindexLocked(s)
	b.mu.Unlock()
	b.subCount.Add(-1)
}

// Subscribers returns the current live subscription count (for metrics/health).
func (b *Broker) Subscribers() int64 { return b.subCount.Load() }

// Stats returns cumulative fan-out counters (delivered frames, dropped payloads).
func (b *Broker) Stats() (delivered, dropped uint64) {
	return b.delivered.Load(), b.dropped.Load()
}

// Dispatch transforms one raw channel payload and fans it out to all matching
// subscriptions. It is exported so it can be driven directly in unit tests
// (real code path, no Redis required) as well as from the Run loop.
func (b *Broker) Dispatch(channel string, payload []byte) {
	m, ok := b.transform(channel, payload)
	if !ok {
		b.dropped.Add(1)
		return
	}
	b.mu.RLock()
	recipients := make([]*Subscription, 0, len(b.byChannel[channel])+len(b.wildcard))
	for s := range b.byChannel[channel] {
		recipients = append(recipients, s)
	}
	for s := range b.wildcard {
		recipients = append(recipients, s)
	}
	b.mu.RUnlock()
	for _, s := range recipients {
		filter := s.filter.Load()
		if filter.wantsChannel(channel) && filter.wantsPool(m.pool) {
			s.deliver(m)
		}
	}
	b.delivered.Add(1)
}

// Run subscribes to the configured channels and dispatches messages until ctx
// is cancelled. go-redis transparently re-subscribes across reconnects, so a
// transient Redis blip never silently stops the stream. Run blocks; callers
// typically launch it in a goroutine.
func (b *Broker) Run(ctx context.Context) error {
	ps := b.rdb.Subscribe(ctx, b.channels...)
	defer func() { _ = ps.Close() }()

	// Confirm the subscription before consuming (surfaces an immediate Redis
	// failure rather than hanging).
	if _, err := ps.Receive(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	ch := ps.Channel()
	if b.logger != nil {
		b.logger.Info("broker subscribed", slog.Int("channels", len(b.channels)))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			b.Dispatch(msg.Channel, []byte(msg.Payload))
		}
	}
}
