// Package publish ensures the PERPS_V1 JetStream stream and drains the perps
// delivery outbox onto it in Perps-Seq order through delivery.Relay.
package publish

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

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
	var s jetstream.Stream
	// A concurrent creator, or a cluster peer whose view lags the stream, makes
	// CreateStream answer stream name already in use; the live stream is then
	// read again and held to the same drift check.
	for range 5 {
		s, err = js.Stream(ctx, want.Name)
		if !errors.Is(err, jetstream.ErrStreamNotFound) {
			break
		}
		s, err = js.CreateStream(ctx, want)
		if !errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
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

// limit is a stream limit as the server reports it: 0 sets no limit, -1.
func limit(v int64) int64 {
	if v == 0 {
		return -1
	}
	return v
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
		{"max_bytes", have.MaxBytes, limit(want.MaxBytes), have.MaxBytes == limit(want.MaxBytes)},
		{"max_msgs", have.MaxMsgs, limit(want.MaxMsgs), have.MaxMsgs == limit(want.MaxMsgs)},
		{"max_msgs_per_subject", have.MaxMsgsPerSubject, limit(want.MaxMsgsPerSubject), have.MaxMsgsPerSubject == limit(want.MaxMsgsPerSubject)},
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
	// MaxHeadAge is the largest age of the oldest open outbox row of a ready
	// Publisher.
	MaxHeadAge time.Duration
	// Logger receives failed passes; nil selects slog.Default.
	Logger *slog.Logger
}

func (c Config) validate() error {
	if !owner.MatchString(c.Owner) || c.Batch < 1 || c.Batch > 1000 || c.MaxAttempts < 1 || c.MaxAttempts > 100 ||
		c.LeaseDuration < time.Second || c.LeaseDuration > 5*time.Minute || c.PublishTimeout <= 0 || c.PublishTimeout >= c.LeaseDuration ||
		c.RetryDelay < time.Second || c.RetryDelay > time.Hour || c.IdleInterval < time.Millisecond || c.IdleInterval > time.Minute ||
		c.MaxHeadAge < time.Second || c.MaxHeadAge > time.Hour {
		return errors.New("publish: invalid publisher configuration")
	}
	return nil
}

// Publisher drains perps.delivery_outbox onto PERPS_V1.
type Publisher struct {
	relay       delivery.Relay
	idle        time.Duration
	maxHeadAge  time.Duration
	logger      *slog.Logger
	headAge     prometheus.GaugeFunc
	quarantined prometheus.Counter
}

// headAgeTimeout bounds the outbox read of one scrape of perps_outbox_head_age_seconds.
const headAgeTimeout = 2 * time.Second

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
	p := &Publisher{
		relay: delivery.Relay{
			Store: store, Publisher: sink, Owner: cfg.Owner, Batch: cfg.Batch, MaxAttempts: cfg.MaxAttempts,
			LeaseDuration: cfg.LeaseDuration, PublishTimeout: cfg.PublishTimeout, RetryDelay: cfg.RetryDelay,
		},
		idle:       cfg.IdleInterval,
		maxHeadAge: cfg.MaxHeadAge,
		logger:     logger,
		quarantined: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "perps_outbox_quarantined_total",
			Help: "Outbox rows quarantined permanently and announced by a control message.",
		}),
	}
	p.headAge = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "perps_outbox_head_age_seconds",
		Help: "Age of the oldest outbox row not yet published or announced; NaN when the outbox cannot be read.",
	}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), headAgeTimeout)
		defer cancel()
		age, err := store.HeadAge(ctx)
		if err != nil {
			return math.NaN()
		}
		return age.Seconds()
	})
	p.relay.OnQuarantine = func(q delivery.Quarantine) {
		p.quarantined.Inc()
		p.logger.Error("perps outbox row quarantined permanently", slog.Int64("perps_seq", q.PerpsSeq), slog.String("event_id", q.EventID), slog.String("class", string(q.Class)))
	}
	return p, nil
}

// Collectors returns perps_outbox_head_age_seconds and
// perps_outbox_quarantined_total for registration by the owning service.
func (p *Publisher) Collectors() []prometheus.Collector {
	return []prometheus.Collector{p.headAge, p.quarantined}
}

// Ready reports an error while the oldest open outbox row, pending or
// quarantined and not yet announced, is older than MaxHeadAge: the relay is
// stalled on the head of the outbox. It reports ready again once that row
// drains.
func (p *Publisher) Ready(ctx context.Context) error {
	age, err := p.relay.Store.HeadAge(ctx)
	if err != nil {
		return fmt.Errorf("read outbox head: %w", err)
	}
	if age > p.maxHeadAge {
		return fmt.Errorf("oldest open outbox row %s old, bound %s", age, p.maxHeadAge)
	}
	return nil
}

// Run loops delivery.Relay.Pass until ctx ends and returns ctx.Err(). A failed
// pass waits RetryDelay; a row put back by Retry holds back every later row
// until it is published, keeping publication in Perps-Seq order.
func (p *Publisher) Run(ctx context.Context) error {
	if p == nil || ctx == nil {
		return errors.New("publish: publisher and context required")
	}
	for {
		wait := p.idle
		if err := p.relay.Pass(ctx); err != nil {
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

// sink is the delivery.BatchPublisher of the Publisher: it publishes the
// first message of a batch and awaits its JetStream persistence
// acknowledgement, then pipelines the rest on one connection, each carrying
// Nats-Msg-Id, Perps-Seq, Perps-Block and Perps-Kind, or, for a control
// lease, the delivery.ControlMessage with Perps-Control and Perps-Seq. Every pipelined message
// expects the stream sequence of its predecessor as the last sequence
// (Nats-Expected-Last-Sequence), so the broker refuses every message behind
// one that did not persist and none lands out of Perps-Seq order. The first
// message carries no expectation, so the retry of a message that persisted
// without its acknowledgement is answered as a duplicate. The client never
// republishes a message on its own.
type sink struct {
	js     jetstream.JetStream
	policy delivery.Policy
}

// errPredecessor reports a message held back behind a failed one.
var errPredecessor = errors.New("publish: earlier message of the batch not persisted")

func (s *sink) PublishBatch(ctx context.Context, leases []delivery.Lease) []delivery.Publication {
	out := make([]delivery.Publication, len(leases))
	if len(leases) == 0 {
		return out
	}
	f, err := s.send(leases[0])
	if err == nil {
		out[0] = await(ctx, f)
	} else {
		out[0].Err = err
	}
	if out[0].Err != nil {
		for i := 1; i < len(out); i++ {
			out[i].Err = errPredecessor
		}
		return out
	}
	futures := make([]jetstream.PubAckFuture, len(leases))
	last := out[0].Ack.Sequence
	for i := 1; i < len(leases); i++ {
		if futures[i], out[i].Err = s.send(leases[i], jetstream.WithExpectLastSequence(last+uint64(i)-1)); out[i].Err != nil {
			// The broker refuses every later message on its expectation;
			// they are not sent.
			for j := i + 1; j < len(out); j++ {
				out[j].Err = errPredecessor
			}
			break
		}
	}
	for i, f := range futures {
		if f != nil {
			out[i] = await(ctx, f)
		}
	}
	return out
}

func await(ctx context.Context, f jetstream.PubAckFuture) delivery.Publication {
	select {
	case ack := <-f.Ok():
		if ack == nil || ack.Stream != stream.Name || ack.Sequence == 0 || ack.Sequence > 1<<63-1 {
			return delivery.Publication{Err: errors.New("publish: invalid persistence acknowledgement")}
		}
		return delivery.Publication{Ack: delivery.PersistenceAck{Stream: ack.Stream, Sequence: ack.Sequence}}
	case err := <-f.Err():
		return delivery.Publication{Err: classify(err)}
	case <-ctx.Done():
		return delivery.Publication{Err: ctx.Err()}
	}
}

func (s *sink) send(l delivery.Lease, opts ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	msg, id, err := s.message(l)
	if err != nil {
		return nil, err
	}
	opts = append(opts, jetstream.WithMsgID(id), jetstream.WithExpectStream(stream.Name), jetstream.WithRetryAttempts(0))
	f, err := s.js.PublishMsgAsync(msg, opts...)
	return f, classify(err)
}

// message builds the broker message of l and its Nats-Msg-Id.
func (s *sink) message(l delivery.Lease) (*nats.Msg, string, error) {
	if l.Control {
		subject, id, body, err := delivery.ControlMessage(l)
		if err != nil {
			return nil, "", err
		}
		msg := &nats.Msg{Subject: subject, Data: body, Header: nats.Header{}}
		msg.Header.Set(delivery.HeaderControl, delivery.ControlSkipped)
		msg.Header.Set(HeaderSeq, strconv.FormatInt(l.PerpsSeq, 10))
		return msg, id, nil
	}
	e, err := delivery.Decode(l.Wire, s.policy, l.Subject)
	if err != nil {
		return nil, "", err
	}
	if e.EventID != l.EventID || l.PerpsSeq < 1 {
		return nil, "", delivery.ErrIntegrity
	}
	msg := &nats.Msg{Subject: l.Subject, Data: append([]byte(nil), l.Wire...), Header: nats.Header{}}
	msg.Header.Set(HeaderSeq, strconv.FormatInt(l.PerpsSeq, 10))
	msg.Header.Set(HeaderBlock, e.SourceOrder.Height)
	msg.Header.Set(HeaderKind, e.EventType)
	return msg, l.EventID, nil
}

// errCodeMessageTooLarge is the JetStream API error code of a message larger
// than the max_msg_size of the stream.
const errCodeMessageTooLarge jetstream.ErrorCode = 10054

func classify(err error) error {
	if errors.Is(err, nats.ErrAuthorization) {
		return delivery.ErrAuthorization
	}
	var api *jetstream.APIError
	if errors.Is(err, nats.ErrMaxPayload) || (errors.As(err, &api) && api.ErrorCode == errCodeMessageTooLarge) {
		return fmt.Errorf("%w: %w", delivery.ErrOversized, err)
	}
	return err
}
