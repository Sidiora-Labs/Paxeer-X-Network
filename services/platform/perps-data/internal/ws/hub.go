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

// OutageAfter is how long the stream may stay unanswered before every
// subscribed socket closes with 1013, so its client resyncs instead of
// showing frozen books and tapes beside live timed frames.
const OutageAfter = 10 * time.Second

const (
	snapshotTimeout = 10 * time.Second
	// messageTimeout bounds the reads Message makes under the hub lock for a
	// message whose Prepare could not make them.
	messageTimeout = 5 * time.Second
	reattachDelay  = time.Second
	// probeAfter is how long the consumer may deliver nothing before the hub
	// asks the broker for the stream; probeTimeout bounds every stream
	// request of the hub.
	probeAfter   = 5 * time.Second
	probeTimeout = 2 * time.Second
	// loadAttempts is how often a subscribe reads the topic state outside the
	// hub lock before a load that messages keep racing reads its snapshot
	// under it.
	loadAttempts = 3
	// resnapshotWorkers bounds the snapshots one resync reads at once.
	resnapshotWorkers = 8
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
// Prepare runs outside the hub lock before the Message of the same message,
// for the messages of different families at once and for those of one
// family in stream order. Message returns the live frames a message causes
// and the topics that must send a fresh snapshot instead.
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
// process, the dense seq of the last broadcast and the subscribed sockets.
type topicState struct {
	snapshotID string
	seq        uint64
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
	// order. Frames are only queued under it; socket closes, stream requests
	// and resync snapshots happen outside it.
	mu      sync.Mutex
	topics  map[string]*topicState
	applied uint64
	// perpsSeq is the highest Perps-Seq applied, zero until the first
	// message after an attach of a new stream.
	perpsSeq int64
	// created is the creation time of the stream the hub resumes on; a
	// stream created anew is a history the hub cannot resume.
	created   time.Time
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
// from that state under it; a load a message raced is read again, and only
// after loadAttempts raced loads is the snapshot read under the lock, within
// snapshotTimeout. A snapshot that cannot be built closes s with 1013 so the
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
	var (
		data json.RawMessage
		err  error
		in   bool
	)
	for attempt := 1; ; attempt++ {
		err = h.provider.Load(ctx, f.Topic)
		h.mu.Lock()
		if _, in = t.subs[s]; held && !in {
			// A failed write dropped s from every topic and closed it meanwhile.
			h.mu.Unlock()
			return
		}
		if err != nil {
			break
		}
		var ok bool
		if data, ok = h.provider.Held(f.Topic); ok {
			break
		}
		if attempt == loadAttempts {
			data, err = h.provider.Snapshot(ctx, f.Topic)
			break
		}
		h.mu.Unlock()
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
// sequence after the last applied message, unless the stream was created
// anew or lost the messages the hub applied: then every subscribed socket
// closes with 1013 and the hub starts after the new stream head. A stream
// that stays unanswered for OutageAfter closes every subscribed socket with
// 1013 once.
func (h *Hub) Run(ctx context.Context, nc *nats.Conn) error {
	if ctx == nil || nc == nil || nc.IsClosed() {
		return errors.New("ws: hub needs a context and an open connection")
	}
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	timed := make(chan error, 1)
	go func() { timed <- h.provider.Run(inner, h.emit) }()
	o := &outage{heard: time.Now()}
	for {
		err := h.consume(inner, nc, o)
		if ctx.Err() != nil {
			break
		}
		h.logger.Error("ws: hub consumer", slog.Any("err", err))
		o.unheard(h)
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

// outage tracks how long the stream has gone unanswered: heard is when the
// broker last answered for it, and down is set once that outage closed the
// subscribed sockets. Run and the fetch loop of its consume use it in turn.
type outage struct {
	heard time.Time
	down  bool
}

func (o *outage) answered() { o.heard, o.down = time.Now(), false }

// unheard closes every subscribed socket with 1013 once per outage, when the
// stream has gone unanswered for OutageAfter.
func (o *outage) unheard(h *Hub) {
	if o.down || time.Since(o.heard) < OutageAfter {
		return
	}
	o.down = true
	h.logger.Error("ws: hub stream unanswered", slog.Duration("for", time.Since(o.heard)))
	h.closeAll()
}

// consume attaches the consumer at the resume sequence and applies its
// messages until the consumer fails or ctx ends. A consumer that delivers
// nothing for probeAfter has the stream asked for, so an outage the
// consumer reports only as empty fetches is seen.
func (h *Hub) consume(ctx context.Context, nc *nats.Conn, o *outage) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	start, err := h.resumeFrom(ctx, js)
	if err != nil {
		return err
	}
	o.answered()
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
	// The fetch loop decodes and prepares every message as it arrives and
	// queues it; the apply loop applies the queue in stream order, each
	// message once its Prepare is done.
	queue := make(chan *prepared, preparedQueue)
	fetchErr := make(chan error, 1)
	var preparing sync.WaitGroup
	go func() {
		defer close(queue)
		families := map[string]chan struct{}{}
		for {
			messages, err := c.Fetch(ctx)
			switch {
			case len(messages) > 0:
				o.answered()
			case err == nil && time.Since(o.heard) >= probeAfter:
				if h.reachable(ctx, js) {
					o.answered()
				} else {
					o.unheard(h)
				}
			}
			for _, m := range messages {
				p := h.prepare(ctx, m, families, &preparing)
				select {
				case queue <- p:
				case <-ctx.Done():
					fetchErr <- ctx.Err()
					return
				}
			}
			if err != nil {
				fetchErr <- err
				return
			}
		}
	}()
	defer preparing.Wait()
	for p := range queue {
		<-p.ready
		h.apply(ctx, p)
	}
	return <-fetchErr
}

// reachable reports whether the broker answers for the stream within
// probeTimeout.
func (h *Hub) reachable(ctx context.Context, js jetstream.JetStream) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := js.Stream(ctx, stream.Name)
	return err == nil
}

// preparedQueue bounds the messages fetched ahead of the apply loop.
const preparedQueue = 256

// prepared is a fetched message decoded for apply; ready is closed once its
// provider Prepare is done.
type prepared struct {
	m        *natsdriver.Message
	env      delivery.Envelope
	perpsSeq int64
	err      error
	ready    chan struct{}
}

// prepare decodes m and runs the provider Prepare of a message the hub has
// not applied yet outside the hub lock, in a goroutine of its own. The
// messages of one family, perps.<chain>.<family>, prepare one after another
// in stream order, each after the one before it in families; the families
// prepare side by side, so a message waits for the reads of its own family
// only.
func (h *Hub) prepare(ctx context.Context, m *natsdriver.Message, families map[string]chan struct{}, preparing *sync.WaitGroup) *prepared {
	env, perpsSeq, err := h.decode(m.Subject, m)
	p := &prepared{m: m, env: env, perpsSeq: perpsSeq, err: err, ready: make(chan struct{})}
	h.mu.Lock()
	applied := h.applied
	h.mu.Unlock()
	if err != nil || m.Sequence <= applied {
		close(p.ready)
		return p
	}
	family, _, _ := strings.Cut(strings.TrimPrefix(m.Subject, h.prefix), ".")
	prev := families[family]
	families[family] = p.ready
	preparing.Go(func() {
		defer close(p.ready)
		if prev != nil {
			<-prev
		}
		h.provider.Prepare(ctx, m.Subject, env)
	})
	return p
}

// resumeFrom returns the stream sequence after the last applied message; on
// the first attach of the process, and on a stream created anew or whose
// last sequence is below the last applied message, that is the sequence
// after the stream head, and the hub forgets the Perps-Seq it applied. A
// stream that went back closes every subscribed socket with 1013.
func (h *Hub) resumeFrom(ctx context.Context, js jetstream.JetStream) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	s, err := js.Stream(ctx, stream.Name)
	if err != nil {
		return 0, fmt.Errorf("ws: hub stream: %w", err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		return 0, fmt.Errorf("ws: hub stream: %w", err)
	}
	h.mu.Lock()
	var closing []*Socket
	reset := h.attached && (!info.Created.Equal(h.created) || info.State.LastSeq < h.applied)
	if reset {
		h.logger.Error("ws: hub stream went back", slog.Uint64("applied", h.applied), slog.Uint64("last", info.State.LastSeq),
			slog.Time("created", info.Created), slog.Time("was_created", h.created))
		closing = h.dropAllLocked()
	}
	if !h.attached || reset {
		h.applied, h.perpsSeq, h.created, h.attached = info.State.LastSeq, 0, info.Created, true
	}
	start := h.applied + 1
	h.mu.Unlock()
	closeLater(closing)
	return start, nil
}

// apply applies one message once. A stream sequence beyond the one after the
// last applied message is a range the hub cannot fill: every subscribed
// socket closes with 1013 and the client resyncs through REST. The topics
// the message leaves out of date are resynced outside the hub lock before
// the next message is applied.
func (h *Hub) apply(ctx context.Context, p *prepared) {
	seq := p.m.Sequence
	h.mu.Lock()
	if seq <= h.applied {
		h.mu.Unlock()
		return
	}
	var closing []*Socket
	if seq > h.applied+1 {
		h.logger.Error("ws: hub stream gap", slog.Uint64("applied", h.applied), slog.Uint64("next", seq))
		closing = h.dropAllLocked()
	}
	h.applied = seq
	var resnap []string
	if p.err != nil {
		h.logger.Error("ws: hub message", slog.Uint64("stream_seq", seq), slog.Any("err", p.err))
		resnap = h.watchedLocked()
	} else {
		resnap = h.messageLocked(ctx, p.m.Subject, p.env, p.perpsSeq)
	}
	h.mu.Unlock()
	closeLater(closing)
	h.resnapshot(ctx, resnap)
}

// messageLocked applies one decoded message: it calls the listeners, sends
// the live frames of the provider and returns the subscribed topics that
// must resync instead. A Perps-Seq that is not the one after the highest
// applied is a hole or a late message of the producer, which the stream
// sequence cannot show, so every subscribed topic resyncs.
func (h *Hub) messageLocked(ctx context.Context, subject string, env delivery.Envelope, perpsSeq int64) []string {
	for l := range h.listeners {
		l.fn(subject, env)
	}
	var resnap []string
	if h.perpsSeq > 0 && perpsSeq != h.perpsSeq+1 {
		h.logger.Error("ws: hub Perps-Seq out of order", slog.Int64("applied", h.perpsSeq), slog.Int64("next", perpsSeq), slog.String("subject", subject))
		resnap = h.watchedLocked()
	}
	h.perpsSeq = max(h.perpsSeq, perpsSeq)
	mctx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()
	frames, more, err := h.provider.Message(mctx, subject, env)
	if err != nil {
		h.logger.Error("ws: hub provider message", slog.String("subject", subject), slog.Any("err", err))
		frames, more = nil, h.watchedLocked()
	}
	resnap = append(resnap, more...)
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
		h.broadcastLocked(f, at)
	}
	resnap = slices.DeleteFunc(resnap, func(topic string) bool {
		t := h.topics[topic]
		return t == nil || len(t.subs) == 0
	})
	slices.Sort(resnap)
	return slices.Compact(resnap)
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
	h.broadcastLocked(f, time.Now())
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
func (h *Hub) broadcastLocked(f Frame, at time.Time) {
	t := h.topics[f.Topic]
	if t == nil || len(t.subs) == 0 {
		return
	}
	t.seq++
	h.sendLocked(t, DataFrame{Topic: f.Topic, TS: at.UnixMilli(), SnapshotID: t.snapshotID, Seq: t.seq, Data: f.Data})
}

// resnapshot reads a fresh snapshot of every topic of topics outside the hub
// lock, resnapshotWorkers at a time and each within snapshotTimeout, and
// sends it to the sockets the topic holds then, with snapshot true and the
// next seq. It runs on the apply loop between two messages, so no message
// changes the topic state while its snapshot is read; a timed frame sent
// meanwhile is a whole frame the snapshot supersedes. A topic whose snapshot
// cannot be read closes its sockets with 1013.
func (h *Hub) resnapshot(ctx context.Context, topics []string) {
	if len(topics) == 0 {
		return
	}
	data := make([]json.RawMessage, len(topics))
	errs := make([]error, len(topics))
	slots := make(chan struct{}, resnapshotWorkers)
	var reads sync.WaitGroup
	for i, topic := range topics {
		slots <- struct{}{}
		reads.Go(func() {
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
			defer cancel()
			data[i], errs[i] = h.provider.Snapshot(ctx, topic)
		})
	}
	reads.Wait()
	h.mu.Lock()
	var closing []*Socket
	for i, topic := range topics {
		t := h.topics[topic]
		if t == nil || len(t.subs) == 0 {
			continue
		}
		if errs[i] != nil {
			h.logger.Error("ws: hub resnapshot", slog.String("topic", topic), slog.Any("err", errs[i]))
			for s := range t.subs {
				delete(t.subs, s)
				h.provider.Unsubscribed(topic)
				closing = append(closing, s)
			}
			continue
		}
		t.seq++
		h.sendLocked(t, DataFrame{Topic: topic, TS: time.Now().UnixMilli(), SnapshotID: t.snapshotID, Seq: t.seq, Snapshot: true, Data: data[i]})
	}
	h.mu.Unlock()
	closeLater(closing)
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

// closeAll closes every subscribed socket with 1013 and retry-after.
func (h *Hub) closeAll() {
	h.mu.Lock()
	closing := h.dropAllLocked()
	h.mu.Unlock()
	closeLater(closing)
}

// dropAllLocked removes every subscribed socket from every topic and returns
// each once.
func (h *Hub) dropAllLocked() []*Socket {
	seen := map[*Socket]struct{}{}
	var out []*Socket
	for topic, t := range h.topics {
		for s := range t.subs {
			delete(t.subs, s)
			h.provider.Unsubscribed(topic)
			if _, dup := seen[s]; !dup {
				seen[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	return out
}

// closeLater closes every socket of sockets with 1013 and retry-after
// without waiting for the close frames.
func closeLater(sockets []*Socket) {
	for _, s := range sockets {
		s.closeLater()
	}
}
