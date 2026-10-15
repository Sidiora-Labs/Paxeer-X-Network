// Package natsdriver publishes delivery envelopes to JetStream and consumes them
// through bounded durable pull consumers. It originates in Launch-x
// shared/delivery/natsdriver.
package natsdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var subjectName = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
var eventIdentity = regexp.MustCompile(`^(0|[1-9][0-9]{0,19}):(0|[1-9][0-9]{0,19}):0x[0-9a-f]{64}:(-1|0|[1-9][0-9]{0,9}):(0|[1-9][0-9]{0,9}):(0|[1-9][0-9]{0,9})$`)
var ErrClosed = errors.New("natsdriver: consumer closed")
var ErrEventID = errors.New("natsdriver: invalid event id")

const publishTimeout = 5 * time.Second

// MsgID maps the event id <chain>:<block>:<block_hash>:<tx_index>:<log_index>:<sub>
// to the lower-case hex SHA-256 of its bytes, the envelope event id and the
// Nats-Msg-Id of its publication. Equal event ids map to equal message ids, and a
// rewritten block hash maps to a new one.
func MsgID(eventID string) (string, error) {
	if !eventIdentity.MatchString(eventID) {
		return "", ErrEventID
	}
	sum := sha256.Sum256([]byte(eventID))
	return hex.EncodeToString(sum[:]), nil
}

type Publisher struct {
	js     jetstream.JetStream
	stream string
}

func NewPublisher(nc *nats.Conn, stream string) (*Publisher, error) {
	if nc == nil || nc.IsClosed() || !identifier.MatchString(stream) {
		return nil, errors.New("natsdriver: valid connection and stream required")
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return &Publisher{js: js, stream: stream}, nil
}

func (p *Publisher) Publish(ctx context.Context, subject, eventID string, wire []byte) (delivery.PersistenceAck, error) {
	var result delivery.PersistenceAck
	if p == nil || p.js == nil || ctx == nil || !identifier.MatchString(eventID) || len(subject) > 256 || !subjectName.MatchString(subject) || len(wire) == 0 || len(wire) > delivery.MaxEnvelopeBytes {
		return result, errors.New("natsdriver: invalid publication")
	}
	bounded, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	ack, err := p.js.Publish(bounded, subject, append([]byte(nil), wire...), jetstream.WithMsgID(eventID), jetstream.WithExpectStream(p.stream))
	if err != nil {
		if errors.Is(err, nats.ErrAuthorization) {
			return result, delivery.ErrAuthorization
		}
		return result, err
	}
	if ack == nil || ack.Stream != p.stream || ack.Sequence == 0 || ack.Sequence > 1<<63-1 {
		return result, errors.New("natsdriver: invalid persistence acknowledgement")
	}
	return delivery.PersistenceAck{Stream: ack.Stream, Sequence: ack.Sequence}, nil
}

type ConsumerConfig struct {
	Stream, Durable, Subject                     string
	Batch, MaxAckPending, MaxWaiting, MaxDeliver int
	FetchWait, AckWait                           time.Duration
}

func (c ConsumerConfig) validate() error {
	if !identifier.MatchString(c.Stream) || !identifier.MatchString(c.Durable) || len(c.Subject) > 256 || !subjectName.MatchString(c.Subject) || c.Batch < 1 || c.Batch > 64 || c.MaxAckPending < c.Batch || c.MaxAckPending > 10000 || c.MaxWaiting < 1 || c.MaxWaiting > 32 || c.MaxDeliver < 1 || c.MaxDeliver > 100 || c.FetchWait < 10*time.Millisecond || c.FetchWait > time.Second || c.AckWait < time.Second || c.AckWait > time.Hour {
		return errors.New("natsdriver: invalid bounded durable configuration")
	}
	return nil
}

func (c ConsumerConfig) BrokerConsumerConfig() (jetstream.ConsumerConfig, error) {
	if err := c.validate(); err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	return jetstream.ConsumerConfig{
		Name: c.Durable, Durable: c.Durable, DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: c.AckWait, MaxDeliver: c.MaxDeliver,
		FilterSubject: c.Subject, ReplayPolicy: jetstream.ReplayInstantPolicy,
		MaxAckPending: c.MaxAckPending, MaxWaiting: c.MaxWaiting,
		MaxRequestBatch: c.Batch, MaxRequestExpires: c.FetchWait,
	}, nil
}

type Consumer struct {
	consumer  jetstream.Consumer
	config    ConsumerConfig
	ctx       context.Context
	cancel    context.CancelFunc
	fetch     chan struct{}
	closeOnce sync.Once
}

func NewConsumer(ctx context.Context, nc *nats.Conn, cfg ConsumerConfig) (*Consumer, error) {
	if ctx == nil || nc == nil || nc.IsClosed() {
		return nil, errors.New("natsdriver: context and connection required")
	}
	expected, err := cfg.BrokerConsumerConfig()
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	stream, err := js.Stream(ctx, cfg.Stream)
	if err != nil {
		return nil, err
	}
	streamInfo, err := stream.Info(ctx)
	if err != nil {
		return nil, err
	}
	if streamInfo.Config.Storage != jetstream.FileStorage {
		return nil, errors.New("natsdriver: file-backed stream required")
	}
	consumer, err := js.Consumer(ctx, cfg.Stream, cfg.Durable)
	if err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return nil, err
	}
	actual := info.Config
	if info.Stream != cfg.Stream || info.Name != cfg.Durable || actual.Durable != expected.Durable || actual.AckPolicy != expected.AckPolicy || actual.AckWait != expected.AckWait || actual.MaxDeliver != expected.MaxDeliver || actual.DeliverPolicy != expected.DeliverPolicy || actual.ReplayPolicy != expected.ReplayPolicy || actual.FilterSubject != expected.FilterSubject || len(actual.FilterSubjects) != 0 || actual.MaxAckPending != expected.MaxAckPending || actual.MaxWaiting != expected.MaxWaiting || actual.MaxRequestBatch != expected.MaxRequestBatch || actual.MaxRequestExpires != expected.MaxRequestExpires || actual.DeliverSubject != "" || actual.MemoryStorage || actual.HeadersOnly || actual.InactiveThreshold != 0 || len(actual.BackOff) != 0 {
		return nil, errors.New("natsdriver: durable configuration differs from reviewed configuration")
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	return &Consumer{consumer: consumer, config: cfg, ctx: lifecycle, cancel: cancel, fetch: make(chan struct{}, 1)}, nil
}

type Message struct {
	Subject    string
	EventID    string
	Wire       []byte
	Stream     string
	Sequence   uint64
	Deliveries uint64
	msg        jetstream.Msg
}

func (m *Message) Ack(ctx context.Context) error {
	if m == nil || m.msg == nil || ctx == nil {
		return errors.New("natsdriver: actual broker message and context required")
	}
	bounded, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	return m.msg.DoubleAck(bounded)
}

func (c *Consumer) Fetch(ctx context.Context) ([]*Message, error) {
	if c == nil || c.consumer == nil || ctx == nil {
		return nil, errors.New("natsdriver: invalid consumer")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, ErrClosed
	case c.fetch <- struct{}{}:
	}
	defer func() { <-c.fetch }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.ctx.Err() != nil {
		return nil, ErrClosed
	}
	bounded, cancel := context.WithTimeout(ctx, c.config.FetchWait)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	batch, err := c.consumer.Fetch(c.config.Batch, jetstream.FetchContext(bounded))
	if err != nil {
		return nil, err
	}
	messages := make([]*Message, 0, c.config.Batch)
	var invalid error
	for msg := range batch.Messages() {
		metadata, metaErr := msg.Metadata()
		if metaErr != nil {
			invalid = errors.Join(invalid, metaErr)
			continue
		}
		body := msg.Data()
		eventID := msg.Headers().Get("Nats-Msg-Id")
		if metadata.Stream != c.config.Stream || metadata.Sequence.Stream == 0 || metadata.Sequence.Stream > 1<<63-1 || msg.Subject() != c.config.Subject || !identifier.MatchString(eventID) || len(body) == 0 || len(body) > delivery.MaxEnvelopeBytes {
			invalid = errors.Join(invalid, errors.New("natsdriver: invalid broker message"))
			continue
		}
		messages = append(messages, &Message{Subject: msg.Subject(), EventID: eventID, Wire: append([]byte(nil), body...), Stream: metadata.Stream, Sequence: metadata.Sequence.Stream, Deliveries: metadata.NumDelivered, msg: msg})
	}
	terminal := batch.Error()
	if ctx.Err() != nil {
		return messages, errors.Join(ctx.Err(), terminal, invalid)
	}
	if c.ctx.Err() != nil {
		return messages, errors.Join(ErrClosed, terminal, invalid)
	}
	if errors.Is(terminal, context.DeadlineExceeded) && bounded.Err() == context.DeadlineExceeded {
		terminal = nil
	}
	if invalid != nil || terminal != nil {
		return messages, errors.Join(invalid, terminal)
	}
	return messages, nil
}

func (c *Consumer) Run(ctx context.Context, consume func(context.Context, *Message) error) error {
	if consume == nil {
		return errors.New("natsdriver: transactional consumer required")
	}
	for {
		messages, err := c.Fetch(ctx)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := consume(ctx, msg); err != nil {
				return fmt.Errorf("natsdriver: transactional consume: %w", err)
			}
		}
	}
}

func (c *Consumer) Close() {
	if c != nil && c.cancel != nil {
		c.closeOnce.Do(c.cancel)
	}
}

var _ delivery.PersistentPublisher = (*Publisher)(nil)
var _ delivery.Delivery = (*Message)(nil)

// BrokerPosition is the broker identity of one delivered message.
type BrokerPosition struct {
	Stream                       string
	Sequence                     uint64
	Subject, EventID, WireSHA256 string
}

func (m *Message) BrokerPosition() (BrokerPosition, error) {
	var position BrokerPosition
	if m == nil || m.msg == nil {
		return position, errors.New("natsdriver: actual broker message required")
	}
	metadata, err := m.msg.Metadata()
	if err != nil || metadata == nil || !identifier.MatchString(metadata.Stream) || metadata.Sequence.Stream == 0 || metadata.Sequence.Stream > 1<<63-1 {
		return position, errors.New("natsdriver: actual broker metadata unavailable")
	}
	wire := m.msg.Data()
	eventID := m.msg.Headers().Get("Nats-Msg-Id")
	if !identifier.MatchString(eventID) || !subjectName.MatchString(m.msg.Subject()) || len(wire) == 0 || len(wire) > delivery.MaxEnvelopeBytes {
		return position, delivery.ErrIntegrity
	}
	return BrokerPosition{Stream: metadata.Stream, Sequence: metadata.Sequence.Stream, Subject: m.msg.Subject(), EventID: eventID, WireSHA256: delivery.Hash(wire)}, nil
}
