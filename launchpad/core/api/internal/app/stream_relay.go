package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// streamRelayDocument is the bound STREAM_RELAY_CONFIG document of the
// API_ROLE=rest process: the api_stream outbox relay that publishes every
// committed api_stream frame (written by stats-workers, trading-charts,
// pnl-tracker and ranking-algo through the api_stream.append_* functions) to
// the JetStream subject named by its topic, where API_ROLE=sockets consumes it
// (broker.PostgresStream.RunCommitted). Durations are nanoseconds, as in
// STREAM_CONSUMER_CONFIG.
type streamRelayDocument struct {
	// Routes maps the committed topic families to the streams capturing them;
	// every family delivery.ParseCommittedStreamFrame admits must be routed.
	Routes []delivery.StreamRoute `json:"routes"`
	// Epoch/Region are the restoration fence (recovery.assert_epoch) every
	// relay mutation asserts in its transaction.
	Epoch  int64  `json:"epoch"`
	Region string `json:"region"`

	Batch          int           `json:"batch"`
	MaxAttempts    int           `json:"maxAttempts"`
	LeaseDuration  time.Duration `json:"leaseDuration"`
	PublishTimeout time.Duration `json:"publishTimeout"`
	RetryDelay     time.Duration `json:"retryDelay"`
	// IdleInterval is the poll period once the outbox is drained.
	IdleInterval time.Duration `json:"idleInterval"`
	// MaxLag is the readiness budget for the oldest unpublished frame.
	MaxLag time.Duration `json:"maxLag"`

	Concurrency     int     `json:"concurrency"`
	EventsPerSecond float64 `json:"eventsPerSecond"`
	Burst           int     `json:"burst"`
}

// streamRelay is the running relay plus its readiness state.
type streamRelay struct {
	relay  *delivery.StreamOutboxRelay
	guard  *queue.EpochGuard
	pool   *pgxpool.Pool
	nc     *nats.Conn
	doc    streamRelayDocument
	mu     sync.Mutex
	runErr error
}

func (d streamRelayDocument) validate() error {
	if d.Epoch < 1 || d.Region == "" || d.Batch < 1 || d.Batch > 1000 || d.MaxAttempts < 1 || d.MaxAttempts > 100 ||
		d.LeaseDuration < time.Second || d.LeaseDuration > 5*time.Minute || d.PublishTimeout <= 0 || d.PublishTimeout >= d.LeaseDuration ||
		d.RetryDelay < time.Second || d.RetryDelay > time.Hour || d.IdleInterval < 50*time.Millisecond || d.IdleInterval > 10*time.Second ||
		d.MaxLag < time.Second || d.MaxLag > time.Hour || d.Concurrency < 1 || d.Concurrency > 64 ||
		math.IsNaN(d.EventsPerSecond) || d.EventsPerSecond <= 0 || d.EventsPerSecond > 10000 || d.Burst < 1 || d.Burst > 1000 {
		return errors.New("stream relay configuration out of bounds")
	}
	return nil
}

// readStreamRelayDocument loads STREAM_RELAY_CONFIG; (nil, nil) when the
// document is not configured, an error when it is partially configured or
// invalid.
func readStreamRelayDocument(c *config.Config) (*streamRelayDocument, error) {
	if c.StreamRelayFile == "" && c.StreamRelaySHA256 == "" {
		return nil, nil
	}
	var d streamRelayDocument
	if err := config.ReadBoundDocument(c.StreamRelayFile, c.StreamRelaySHA256, &d); err != nil {
		return nil, err
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// connectRuntimeNATS opens the authenticated TLS broker connection both API
// roles use (NATS_URL tls://, NATS_CREDENTIALS user credentials file).
func connectRuntimeNATS(c *config.Config) (*nats.Conn, error) {
	if c.NATSCredentials == "" || !strings.HasPrefix(c.NATSURL, "tls://") {
		return nil, errors.New("authenticated TLS broker required")
	}
	return nats.Connect(c.NATSURL, nats.UserCredentials(c.NATSCredentials), nats.Timeout(2*time.Second), nats.Name(c.RuntimeInstanceID), nats.MaxReconnects(-1))
}

// verifyRelayStreams checks, before the relay publishes, that every routed
// stream exists with the persistence RunCommitted requires (Replicas=3, file
// storage, MaxAge >= 72h) and captures every routed subject filter.
func verifyRelayStreams(ctx context.Context, nc *nats.Conn, routes []delivery.StreamRoute) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	for _, route := range routes {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		stream, err := js.Stream(bounded, route.Stream)
		var info *jetstream.StreamInfo
		if err == nil {
			info, err = stream.Info(bounded)
		}
		cancel()
		if err != nil {
			return fmt.Errorf("stream relay: stream %s unavailable: %w", route.Stream, err)
		}
		if info.Config.Replicas != 3 || info.Config.Storage != jetstream.FileStorage || info.Config.MaxAge < 72*time.Hour {
			return fmt.Errorf("stream relay: stream %s is not R3 file storage with a 72h replay window", route.Stream)
		}
		for _, filter := range route.Subjects {
			captured := false
			for _, subject := range info.Config.Subjects {
				if filterWithin(filter, subject) {
					captured = true
					break
				}
			}
			if !captured {
				return fmt.Errorf("stream relay: stream %s does not capture %s", route.Stream, filter)
			}
		}
	}
	return nil
}

// filterWithin reports whether every subject matching inner also matches outer.
func filterWithin(inner, outer string) bool {
	in, out := strings.Split(inner, "."), strings.Split(outer, ".")
	for i, t := range out {
		if t == ">" {
			return len(in) > i
		}
		if i >= len(in) || in[i] == ">" || (t != "*" && in[i] != t) {
			return false
		}
	}
	return len(in) == len(out)
}

// newStreamRelay builds the relay over an open broker connection and
// exercises its database authority once: the restoration fence must admit a
// guarded transaction and the publication ledger must be readable.
func newStreamRelay(ctx context.Context, pool *pgxpool.Pool, nc *nats.Conn, owner string, d streamRelayDocument) (*streamRelay, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	guard, err := queue.NewEpochGuard("recovery", d.Epoch, d.Region)
	if err != nil {
		return nil, err
	}
	budget, err := delivery.NewWorkBudget(d.Concurrency, d.EventsPerSecond, d.Burst)
	if err != nil {
		return nil, err
	}
	relay, err := delivery.NewStreamOutboxRelay(pool, nc, delivery.StreamOutboxConfig{Owner: owner, Routes: d.Routes, Batch: d.Batch, MaxAttempts: d.MaxAttempts, LeaseDuration: d.LeaseDuration, PublishTimeout: d.PublishTimeout, RetryDelay: d.RetryDelay}, guard.Assert, budget)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := pool.Begin(bounded)
	if err != nil {
		return nil, err
	}
	err = guard.Assert(bounded, tx)
	_ = tx.Rollback(bounded)
	if err != nil {
		return nil, err
	}
	if _, err = relay.Stats(bounded); err != nil {
		return nil, fmt.Errorf("stream relay: publication ledger unavailable: %w", err)
	}
	return &streamRelay{relay: relay, guard: guard, pool: pool, nc: nc, doc: d}, nil
}

// Run drains the outbox until ctx ends: RunOnce publishes up to one batch;
// an empty outbox waits IdleInterval, a failure backs off (bounded) and is
// reported through Ready until a later pass succeeds.
func (s *streamRelay) Run(ctx context.Context, logger *slog.Logger) {
	backoff := s.doc.IdleInterval
	for ctx.Err() == nil {
		before, statsErr := s.relay.Stats(ctx)
		err := s.relay.RunOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.runErr = errors.Join(statsErr, err)
		s.mu.Unlock()
		wait := s.doc.IdleInterval
		if err != nil || statsErr != nil {
			if logger != nil {
				logger.Error("stream relay pass failed", slog.Any("err", errors.Join(statsErr, err)))
			}
			wait = backoff
			if backoff *= 2; backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		} else {
			backoff = s.doc.IdleInterval
			if before.PendingRows > int64(s.doc.Batch) {
				continue
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Ready fails while the broker is disconnected, the last pass failed, or the
// outbox holds a gap, a quarantined frame or a frame older than MaxLag.
func (s *streamRelay) Ready(ctx context.Context) error {
	if !s.nc.IsConnected() {
		return errors.New("stream relay broker disconnected")
	}
	s.mu.Lock()
	runErr := s.runErr
	s.mu.Unlock()
	if runErr != nil {
		return errors.New("stream relay pass failing")
	}
	return s.relay.Ready(ctx, s.doc.MaxLag)
}
