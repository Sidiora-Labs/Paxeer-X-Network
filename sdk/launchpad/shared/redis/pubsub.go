package redis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Publisher publishes JSON messages to Redis channels (parity with TS
// createPublisher). Channel names come from the shared constants package so the
// wire contract matches the TS services (invariant i5).
type Publisher struct {
	rdb *redis.Client
}

// NewPublisher builds a Publisher from a redis:// URL.
func NewPublisher(redisURL string) (*Publisher, error) {
	c, err := NewClient(redisURL)
	if err != nil {
		return nil, err
	}
	return &Publisher{rdb: c}, nil
}

// Publish JSON-encodes data and publishes it to channel.
func (p *Publisher) Publish(ctx context.Context, channel string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("redis: publish encode: %w", err)
	}
	if err := p.rdb.Publish(ctx, channel, b).Err(); err != nil {
		return fmt.Errorf("redis: publish %q: %w", channel, err)
	}
	return nil
}

// Close releases the publisher connection.
func (p *Publisher) Close() error { return p.rdb.Close() }

// PublishJSON publishes pre-marshaled JSON bytes to channel on an EXISTING
// client. It is the push-first counterpart to a cache write: a worker calls it
// right after a successful cache SET so subscribers receive the fresh snapshot
// over the realtime spine instead of polling.
//
// It is best-effort and fire-and-forget in spirit — the returned error is for
// logging only and MUST NOT fail the originating operation (the cache write is
// the durable source of truth; the publish is just the "this changed" signal).
// A nil client is a no-op so services without Redis still run.
func PublishJSON(ctx context.Context, rdb *redis.Client, channel string, payload []byte) error {
	if rdb == nil {
		return nil
	}
	if err := rdb.Publish(ctx, channel, payload).Err(); err != nil {
		return fmt.Errorf("redis: publish %q: %w", channel, err)
	}
	return nil
}

// Subscriber consumes JSON messages from Redis channels (parity with TS
// createSubscriber, which uses a dedicated connection).
type Subscriber struct {
	rdb *redis.Client
}

// NewSubscriber builds a Subscriber from a redis:// URL.
func NewSubscriber(redisURL string) (*Subscriber, error) {
	c, err := NewClient(redisURL)
	if err != nil {
		return nil, err
	}
	return &Subscriber{rdb: c}, nil
}

// Subscribe subscribes to channel and invokes handler for each raw message
// payload until ctx is cancelled. It returns once subscription is confirmed; the
// receive loop runs in a goroutine. handler errors are returned via errc (buffered).
func (s *Subscriber) Subscribe(ctx context.Context, channel string, handler func(ctx context.Context, payload []byte) error) (errc <-chan error, err error) {
	ps := s.rdb.Subscribe(ctx, channel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, fmt.Errorf("redis: subscribe %q: %w", channel, err)
	}
	out := make(chan error, 1)
	ch := ps.Channel()
	go func() {
		defer ps.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if herr := handler(ctx, []byte(msg.Payload)); herr != nil {
					select {
					case out <- herr:
					default:
					}
				}
			}
		}
	}()
	return out, nil
}

// Close releases the subscriber connection.
func (s *Subscriber) Close() error { return s.rdb.Close() }
