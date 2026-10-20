package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
)

// StaleAfter is the stale-frame bound of the application client: a frame
// whose data is older is never sent; the topic sends a fresh snapshot
// instead (contracts/data-services.md section 8).
const StaleAfter = 60 * time.Second

const (
	snapshotTimeout = 10 * time.Second
	reattachDelay   = time.Second
)

// Frame is one live frame of a topic as a Provider produces it.
type Frame struct {
	Topic string
	Data  json.RawMessage
}

// Provider serves the topic families of the hub (book, trade, stats, market,
// totals and chain). The hub calls Subscribed before every subscribe and
// Unsubscribed when a subscriber leaves, feeds every stream message to
// Message in stream order and runs Run for the timed frames. Load reads the
// state of a subscribed topic outside the hub lock and Held answers the
// subscribe snapshot from it without IO; Snapshot reads a fresh snapshot.
// Message returns the live frames a message causes and the topics that must
// send a fresh snapshot instead.
type Provider interface {
	Subscribed(topic string)
	Unsubscribed(topic string)
	Load(ctx context.Context, topic string) error
	Held(topic string) (json.RawMessage, bool)
	Prepare(ctx context.Context, subject string, env delivery.Envelope)
	Snapshot(ctx context.Context, topic string) (json.RawMessage, error)
	Message(ctx context.Context, subject string, env delivery.Envelope) ([]Frame, []string, error)
	Run(ctx context.Context, emit func(Frame)) error
}

// topicState is one topic of the gateway process: the snapshotId of the
// process, the dense seq of the last broadcast, the Perps-Seq of the last
// message applied to the topic and the subscribed sockets.
type topicState struct {
	snapshotID string
	seq        uint64
	perpsSeq   int64
	subs       map[*Socket]struct{}
}

// Hub fans the PERPS_V1 messages of one chain out to the subscribed sockets
// of the topic protocol, keyed by topic. Its Subscribe, Unsubscribe and
// Leave are the OnSubscribe, OnUnsubscribe and OnClose hooks of the Server.
type Hub struct {
	provider   Provider
	snapshotID string
	prefix     string
	logger     *slog.Logger

	// mu serialises message application, timed frames and subscribe
	// snapshots, so a snapshot and the deltas after it reach a socket in seq
	// order. Socket writes happen under it, so a slow socket stalls every
	// topic up to the write deadline.
	mu        sync.Mutex
	topics    map[string]*topicState
	applied   uint64
	attached  bool
	listeners map[*listener]struct{}
}

type listener struct {
	fn func(subject string, env delivery.Envelope)
}

// NewHub builds the Hub of chain for the process snapshotID; a nil logger
// selects slog.Default.
func NewHub(provider Provider, chain uint64, snapshotID string, logger *slog.Logger) (*Hub, error) {
	switch {
	case provider == nil:
		return nil, errors.New("ws: nil hub provider")
	case chain == 0:
		return nil, errors.New("ws: hub chain required")
	case snapshotID == "":
		return nil, errors.New("ws: empty snapshotId")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		provider:   provider,
		snapshotID: snapshotID,
		prefix:     "perps." + strconv.FormatUint(chain, 10) + ".",
		logger:     logger,
		topics:     map[string]*topicState{},
		listeners:  map[*listener]struct{}{},
	}, nil
}

// Listen calls fn with every applied message in stream order, under the hub
// lock, until stop is called.
func (h *Hub) Listen(fn func(subject string, env delivery.Envelope)) (stop func()) {
	l := &listener{fn: fn}
	h.mu.Lock()
	h.listeners[l] = struct{}{}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.listeners, l)
		h.mu.Unlock()
	}
}

// Subscribe sends the snapshot frame of f.Topic to s, carrying the seq of the
// last broadcast, and registers s for the deltas after it. The provider
// loads the topic state outside the hub lock, and the snapshot is answered
// from that state under it; only a load a message raced reads the snapshot
// under the lock. A snapshot that cannot be built closes s with 1013 so the
// client resyncs. The socket protocol calls Subscribe, Unsubscribe and Leave
// of one socket in turn, so s cannot leave while its state loads.
func (h *Hub) Subscribe(s *Socket, f ClientFrame) {
	h.mu.Lock()
	t := h.topics[f.Topic]
	if t == nil {
		t = &topicState{snapshotID: h.snapshotID, subs: map[*Socket]struct{}{}}
		h.topics[f.Topic] = t
	}
	_, held := t.subs[s]
	h.mu.Unlock()
	if !held {
		h.provider.Subscribed(f.Topic)
	}
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
	defer cancel()
	err := h.provider.Load(ctx, f.Topic)
	h.mu.Lock()
	_, in := t.subs[s]
	if held && !in {
		// A failed write dropped s from every topic and closed it meanwhile.
		h.mu.Unlock()
		return
	}
	var data json.RawMessage
	if err == nil {
		var ok bool
		if data, ok = h.provider.Held(f.Topic); !ok {
			data, err = h.provider.Snapshot(ctx, f.Topic)
		}
	}
	if err != nil {
		h.logger.Error("ws: subscribe snapshot", slog.String("topic", f.Topic), slog.Any("err", err))
		if !in {
			h.provider.Unsubscribed(f.Topic)
		}
		h.dropLocked(s)
		h.mu.Unlock()
		_ = s.CloseRetryAfter()
		return
	}
	t.subs[s] = struct{}{}
	// The snapshot is queued under the hub lock, ahead of every delta
	// broadcast after it, and encoded and written by the writer of s.
	frame := DataFrame{Topic: f.Topic, TS: time.Now().UnixMilli(), SnapshotID: t.snapshotID, Seq: t.seq, Snapshot: true, Data: data}
	queued := s.queue(outFrame{value: frame})
	if !queued {
		h.dropLocked(s)
	}
	h.mu.Unlock()
	if !queued {
		_ = s.conn.Close()
	}
}

// Unsubscribe removes s from topic.
func (h *Hub) Unsubscribe(s *Socket, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t := h.topics[topic]; t != nil {
		if _, held := t.subs[s]; held {
			delete(t.subs, s)
			h.provider.Unsubscribed(topic)
		}
	}
}

// Leave removes s from every topic.
func (h *Hub) Leave(s *Socket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(s)
}

func (h *Hub) dropLocked(s *Socket) {
	for topic, t := range h.topics {
		if _, held := t.subs[s]; held {
			delete(t.subs, s)
			h.provider.Unsubscribed(topic)
		}
	}
}

// Run consumes PERPS_V1 through an ordered ephemeral consumer and sends the
// timed frames of the provider until ctx ends. The first attach of the
// process starts after the stream head; every later attach, after a
// consumer failure or a new Run of the same hub, resumes from the stream
// sequence after the last applied message.
func (h *Hub) Run(ctx context.Context, nc *nats.Conn) error {
	if ctx == nil || nc == nil || nc.IsClosed() {
		return errors.New("ws: hub needs a context and an open connection")
	}
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	timed := make(chan error, 1)
	go func() { timed <- h.provider.Run(inner, h.emit) }()
	for {
		err := h.consume(inner, nc)
		if ctx.Err() != nil {
			break
		}
		h.logger.Error("ws: hub consumer", slog.Any("err", err))
		select {
		case <-ctx.Done():
		case err := <-timed:
			return fmt.Errorf("ws: hub timed frames: %w", err)
		case <-time.After(reattachDelay):
		}
		if ctx.Err() != nil {
			break
		}
	}
	cancel()
	<-timed
	return ctx.Err()
}

// consume attaches the consumer at the resume sequence and applies its
// messages until the consumer fails or ctx ends.
func (h *Hub) consume(ctx context.Context, nc *nats.Conn) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	start, err := h.resumeFrom(ctx, js)
	if err != nil {
		return err
	}
	c, err := natsdriver.NewConsumer(ctx, nc, natsdriver.ConsumerConfig{
		Stream:        stream.Name,
		Subject:       h.prefix + ">",
		StartSequence: start,
		Batch:         64,
		FetchWait:     time.Second,
	})
	if err != nil {
		return fmt.Errorf("ws: hub consumer: %w", err)
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, c.Close)
	defer stop()
	for {
		messages, err := c.Fetch(ctx)
		for _, m := range messages {
			if err := h.apply(ctx, m); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

// resumeFrom returns the stream sequence after the last applied message; on
// the first attach of the process that is the sequence after the stream head.
func (h *Hub) resumeFrom(ctx context.Context, js jetstream.JetStream) (uint64, error) {
	h.mu.Lock()
	attached, applied := h.attached, h.applied
	h.mu.Unlock()
	if attached {
		return applied + 1, nil
	}
	s, err := js.Stream(ctx, stream.Name)
	if err != nil {
		return 0, fmt.Errorf("ws: hub stream: %w", err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		return 0, fmt.Errorf("ws: hub stream: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.applied, h.attached = info.State.LastSeq, true
	return h.applied + 1, nil
}

// apply applies one message once. A stream sequence beyond the one after the
// last applied message is a range the hub cannot fill: every subscribed
// socket closes with 1013 and the client resyncs through REST.
func (h *Hub) apply(ctx context.Context, m *natsdriver.Message) error {
	seq := m.Sequence
	subject := m.Subject
	env, perpsSeq, err := h.decode(subject, m)
	if err == nil {
		h.provider.Prepare(ctx, subject, env)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if seq <= h.applied {
		return nil
	}
	if seq > h.applied+1 {
		h.logger.Error("ws: hub stream gap", slog.Uint64("applied", h.applied), slog.Uint64("next", seq))
		h.closeAllLocked()
	}
	h.applied = seq
	if err != nil {
		h.logger.Error("ws: hub message", slog.Uint64("stream_seq", seq), slog.Any("err", err))
		h.resnapshotLocked(ctx, h.watchedLocked(), 0)
		return nil
	}
	for l := range h.listeners {
		l.fn(subject, env)
	}
	frames, resnap, err := h.provider.Message(ctx, subject, env)
	if err != nil {
		h.logger.Error("ws: hub provider message", slog.String("subject", subject), slog.Any("err", err))
		frames, resnap = nil, h.watchedLocked()
	}
	if subject == h.prefix+"chain.rewind" {
		resnap = append(resnap, h.watchedLocked()...)
	}
	at, err := dataTime(env)
	if err != nil {
		h.logger.Error("ws: hub message time", slog.String("subject", subject), slog.Any("err", err))
	}
	stale := err != nil || time.Since(at) > StaleAfter
	for _, f := range frames {
		if stale {
			resnap = append(resnap, f.Topic)
			continue
		}
		h.broadcastLocked(f, at, perpsSeq)
	}
	slices.Sort(resnap)
	h.resnapshotLocked(ctx, slices.Compact(resnap), perpsSeq)
	return nil
}

// decode reads the delivery envelope of m, checked against its headers.
func (h *Hub) decode(subject string, m *natsdriver.Message) (delivery.Envelope, int64, error) {
	if !strings.HasPrefix(subject, h.prefix) {
		return delivery.Envelope{}, 0, fmt.Errorf("ws: subject %q outside %s>", subject, h.prefix)
	}
	headers := m.Header
	perpsSeq, err := strconv.ParseInt(headers.Get(publish.HeaderSeq), 10, 64)
	if err != nil || perpsSeq <= 0 {
		return delivery.Envelope{}, 0, fmt.Errorf("ws: invalid %s header %q", publish.HeaderSeq, headers.Get(publish.HeaderSeq))
	}
	policy := delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{headers.Get(publish.HeaderKind): true}}
	env, err := delivery.Decode(m.Wire, policy, subject)
	if err != nil {
		return delivery.Envelope{}, 0, err
	}
	if env.EventID != headers.Get(jetstream.MsgIDHeader) || env.SourceOrder.Height != headers.Get(publish.HeaderBlock) {
		return delivery.Envelope{}, 0, fmt.Errorf("ws: headers of %s differ from its envelope", env.EventID)
	}
	return env, perpsSeq, nil
}

// dataTime is the time of the data of env: the block_time of a fact row, the
// time of a block row, else the emission time of the envelope.
func dataTime(env delivery.Envelope) (time.Time, error) {
	var row map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &row); err != nil {
		return time.Time{}, err
	}
	for _, key := range []string{"block_time", "time"} {
		raw, ok := row[key]
		if !ok {
			continue
		}
		var at time.Time
		if err := json.Unmarshal(raw, &at); err != nil {
			return time.Time{}, fmt.Errorf("ws: %s of %s: %w", key, env.EventID, err)
		}
		return at, nil
	}
	return env.EmittedAt, nil
}

// emit sends one timed frame of the provider.
func (h *Hub) emit(f Frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t := h.topics[f.Topic]; t != nil {
		h.broadcastLocked(f, time.Now(), t.perpsSeq)
	}
}

// watchedLocked returns the topics with a subscriber.
func (h *Hub) watchedLocked() []string {
	var out []string
	for topic, t := range h.topics {
		if len(t.subs) > 0 {
			out = append(out, topic)
		}
	}
	return out
}

// broadcastLocked sends f as the next delta of its topic, stamped with the
// time of its data.
func (h *Hub) broadcastLocked(f Frame, at time.Time, perpsSeq int64) {
	t := h.topics[f.Topic]
	if t == nil || len(t.subs) == 0 {
		return
	}
	t.seq++
	t.perpsSeq = perpsSeq
	h.sendLocked(t, DataFrame{Topic: f.Topic, TS: at.UnixMilli(), SnapshotID: t.snapshotID, Seq: t.seq, Data: f.Data})
}

// resnapshotLocked rebuilds the snapshot of every subscribed topic of topics
// and sends it with snapshot true and the next seq. A topic whose snapshot
// cannot be built closes its sockets with 1013.
func (h *Hub) resnapshotLocked(ctx context.Context, topics []string, perpsSeq int64) {
	for _, topic := range topics {
		t := h.topics[topic]
		if t == nil || len(t.subs) == 0 {
			continue
		}
		data, err := h.provider.Snapshot(ctx, topic)
		if err != nil {
			h.logger.Error("ws: hub resnapshot", slog.String("topic", topic), slog.Any("err", err))
			for s := range t.subs {
				delete(t.subs, s)
				h.provider.Unsubscribed(topic)
				_ = s.CloseRetryAfter()
			}
			continue
		}
		t.seq++
		if perpsSeq > 0 {
			t.perpsSeq = perpsSeq
		}
		h.sendLocked(t, DataFrame{Topic: topic, TS: time.Now().UnixMilli(), SnapshotID: t.snapshotID, Seq: t.seq, Snapshot: true, Data: data})
	}
}

// sendLocked queues frame, encoded once, to every subscriber of t; a socket
// that is closed or whose queue is full leaves every topic and is closed.
func (h *Hub) sendLocked(t *topicState, frame DataFrame) {
	m, err := PrepareData(frame)
	if err != nil {
		h.logger.Error("ws: hub frame", slog.String("topic", frame.Topic), slog.Any("err", err))
		return
	}
	var failed []*Socket
	for s := range t.subs {
		if !s.queue(outFrame{prepared: m}) {
			failed = append(failed, s)
		}
	}
	for _, s := range failed {
		h.dropLocked(s)
		_ = s.conn.Close()
	}
}

// closeAllLocked closes every subscribed socket with 1013 and retry-after.
func (h *Hub) closeAllLocked() {
	closing := map[*Socket]struct{}{}
	for topic, t := range h.topics {
		for s := range t.subs {
			delete(t.subs, s)
			h.provider.Unsubscribed(topic)
			closing[s] = struct{}{}
		}
	}
	for s := range closing {
		_ = s.CloseRetryAfter()
	}
}
