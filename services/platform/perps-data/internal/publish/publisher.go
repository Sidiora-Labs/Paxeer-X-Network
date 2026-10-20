// Package publish ensures the PERPS_V1 JetStream stream and drains the perps
// delivery outbox onto it in Perps-Seq order through delivery.Relay.
package publish

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
)

// Message headers beside Nats-Msg-Id (contracts/data-services.md section 3).
const (
	HeaderSeq   = "Perps-Seq"
	HeaderBlock = "Perps-Block"
	HeaderKind  = "Perps-Kind"
)

// schema is the Postgres schema of the delivery tables (migrations/0003_delivery.sql).
const schema = "perps"

// ErrDrift reports a live PERPS_V1 configuration that differs from stream.Config.
var ErrDrift = errors.New("stream drift")

var owner = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// EnsureStream creates PERPS_V1 from stream.Config(chain) when it is absent and
// refuses with ErrDrift when the live configuration differs from it.
func EnsureStream(ctx context.Context, nc *nats.Conn, chain uint64) error {
	if ctx == nil || nc == nil || nc.IsClosed() || chain == 0 {
		return errors.New("publish: context, open connection and chain required")
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	want := stream.Config(chain)
	s, err := js.Stream(ctx, want.Name)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		s, err = js.CreateStream(ctx, want)
	}
	if err != nil {
		return err
	}
	info, err := s.Info(ctx)
	if err != nil {
		return err
	}
	return drift(info.Config, want)
}

func drift(have, want jetstream.StreamConfig) error {
	type field struct {
		name       string
		have, want any
		equal      bool
	}
	for _, f := range []field{
		{"replicas", have.Replicas, want.Replicas, have.Replicas == want.Replicas},
		{"storage", have.Storage, want.Storage, have.Storage == want.Storage},
		{"retention", have.Retention, want.Retention, have.Retention == want.Retention},
		{"subjects", have.Subjects, want.Subjects, slices.Equal(have.Subjects, want.Subjects)},
		{"max_age", have.MaxAge, want.MaxAge, have.MaxAge == want.MaxAge},
		{"discard", have.Discard, want.Discard, have.Discard == want.Discard},
		{"deny_delete", have.DenyDelete, want.DenyDelete, have.DenyDelete == want.DenyDelete},
		{"deny_purge", have.DenyPurge, want.DenyPurge, have.DenyPurge == want.DenyPurge},
		{"duplicates", have.Duplicates, want.Duplicates, have.Duplicates == want.Duplicates},
		{"max_msg_size", have.MaxMsgSize, want.MaxMsgSize, have.MaxMsgSize == want.MaxMsgSize},
	} {
		if !f.equal {
			return fmt.Errorf("%w: %s %v want %v", ErrDrift, f.name, f.have, f.want)
		}
	}
	return nil
}

// Config bounds one Publisher. The relay fields carry the bounds of delivery.Relay.
type Config struct {
	Owner                                     string
	Batch, MaxAttempts                        int
	LeaseDuration, PublishTimeout, RetryDelay time.Duration
	// IdleInterval separates two relay passes that both succeeded.
	IdleInterval time.Duration
	// Logger receives failed passes; nil selects slog.Default.
	Logger *slog.Logger
}

func (c Config) validate() error {
	if !owner.MatchString(c.Owner) || c.Batch < 1 || c.Batch > 1000 || c.MaxAttempts < 1 || c.MaxAttempts > 100 ||
		c.LeaseDuration < time.Second || c.LeaseDuration > 5*time.Minute || c.PublishTimeout <= 0 || c.PublishTimeout >= c.LeaseDuration ||
		c.RetryDelay < time.Second || c.RetryDelay > time.Hour || c.IdleInterval < time.Millisecond || c.IdleInterval > time.Minute {
		return errors.New("publish: invalid publisher configuration")
	}
	return nil
}

// Publisher drains perps.delivery_outbox onto PERPS_V1.
type Publisher struct {
	relay  delivery.Relay
	idle   time.Duration
	logger *slog.Logger
}

// New builds a Publisher over the outbox of db; policy is the server-owned
// subject and event type admission of the outbox store.
func New(db *sql.DB, nc *nats.Conn, policy delivery.Policy, cfg Config) (*Publisher, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if nc == nil || nc.IsClosed() {
		return nil, errors.New("publish: open connection required")
	}
	store, err := delivery.NewStore(db, schema, policy)
	if err != nil {
		return nil, err
	}
	// The acknowledgement timeout frees the pending entry of a message whose
	// acknowledgement never arrives.
	js, err := jetstream.New(nc, jetstream.WithPublishAsyncTimeout(cfg.PublishTimeout))
	if err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	sink := &sink{js: js, policy: policy}
	return &Publisher{
		relay: delivery.Relay{
			Store: store, Publisher: sink, Owner: cfg.Owner, Batch: cfg.Batch, MaxAttempts: cfg.MaxAttempts,
			LeaseDuration: cfg.LeaseDuration, PublishTimeout: cfg.PublishTimeout, RetryDelay: cfg.RetryDelay,
		},
		idle:   cfg.IdleInterval,
		logger: logger,
	}, nil
}

// Run loops delivery.Relay.RunOnce until ctx ends and returns ctx.Err(). A failed
// pass waits RetryDelay, so a row put back by Retry is available again and is
// claimed first on the next pass, keeping publication in Perps-Seq order.
func (p *Publisher) Run(ctx context.Context) error {
	if p == nil || ctx == nil {
		return errors.New("publish: publisher and context required")
	}
	for {
		wait := p.idle
		if err := p.relay.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p.logger.Error("perps publisher pass failed", slog.Any("err", err))
			wait = p.relay.RetryDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// sink is the delivery.BatchPublisher of the Publisher: it pipelines the
// messages of a batch on one connection, each carrying Nats-Msg-Id, Perps-Seq,
// Perps-Block and Perps-Kind, and awaits their JetStream persistence
// acknowledgements. The client never republishes a message on its own, so a
// failed message is retried by the relay and never lands behind a later one
// of its batch.
type sink struct {
	js     jetstream.JetStream
	policy delivery.Policy
}

func (s *sink) PublishBatch(ctx context.Context, leases []delivery.Lease) []delivery.Publication {
	out := make([]delivery.Publication, len(leases))
	futures := make([]jetstream.PubAckFuture, len(leases))
	for i, l := range leases {
		futures[i], out[i].Err = s.send(l)
	}
	for i, f := range futures {
		if f == nil {
			continue
		}
		select {
		case ack := <-f.Ok():
			if ack == nil || ack.Stream != stream.Name || ack.Sequence == 0 || ack.Sequence > 1<<63-1 {
				out[i].Err = errors.New("publish: invalid persistence acknowledgement")
				continue
			}
			out[i].Ack = delivery.PersistenceAck{Stream: ack.Stream, Sequence: ack.Sequence}
		case err := <-f.Err():
			out[i].Err = classify(err)
		case <-ctx.Done():
			out[i].Err = ctx.Err()
		}
	}
	return out
}

func (s *sink) send(l delivery.Lease) (jetstream.PubAckFuture, error) {
	e, err := delivery.Decode(l.Wire, s.policy, l.Subject)
	if err != nil {
		return nil, err
	}
	if e.EventID != l.EventID || l.PerpsSeq < 1 {
		return nil, delivery.ErrIntegrity
	}
	msg := &nats.Msg{Subject: l.Subject, Data: append([]byte(nil), l.Wire...), Header: nats.Header{}}
	msg.Header.Set(HeaderSeq, strconv.FormatInt(l.PerpsSeq, 10))
	msg.Header.Set(HeaderBlock, e.SourceOrder.Height)
	msg.Header.Set(HeaderKind, e.EventType)
	f, err := s.js.PublishMsgAsync(msg, jetstream.WithMsgID(l.EventID), jetstream.WithExpectStream(stream.Name), jetstream.WithRetryAttempts(0))
	return f, classify(err)
}

func classify(err error) error {
	if errors.Is(err, nats.ErrAuthorization) {
		return delivery.ErrAuthorization
	}
	return err
}
