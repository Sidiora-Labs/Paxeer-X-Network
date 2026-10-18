package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type StreamDelivery struct {
	Frame  []byte
	Cursor string
}

func TrustedHTTPOrigin(r *http.Request, allowed map[string]bool, proxies []string) (string, error) {
	if origin := r.Header.Get("Origin"); origin != "" {
		if allowed[origin] {
			return origin, nil
		}
		return "", sharedauth.ErrScopeDenied
	}
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		return "", sharedauth.ErrScopeDenied
	}
	secure := r.TLS != nil
	if !secure && r.Header.Get("X-Forwarded-Proto") == "https" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return "", sharedauth.ErrScopeDenied
		}
		ip := net.ParseIP(host)
		for _, cidr := range proxies {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				return "", sharedauth.ErrScopeDenied
			}
			if network.Contains(ip) {
				secure = true
			}
		}
	}
	if !secure {
		return "", sharedauth.ErrScopeDenied
	}
	for origin, ok := range allowed {
		u, err := url.Parse(origin)
		if ok && err == nil && u.Scheme == "https" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && strings.EqualFold(u.Host, r.Host) {
			return origin, nil
		}
	}
	return "", sharedauth.ErrScopeDenied
}

type topicShard struct {
	mu     sync.Mutex
	topics map[string]map[chan struct{}]struct{}
}
type TopicFanout struct {
	shards        [32]topicShard
	degraded      atomic.Bool
	interruptions atomic.Uint64
}

func NewTopicFanout() *TopicFanout {
	f := &TopicFanout{}
	f.degraded.Store(true)
	for i := range f.shards {
		f.shards[i].topics = make(map[string]map[chan struct{}]struct{})
	}
	return f
}
func (f *TopicFanout) Degraded() bool        { return f.degraded.Load() }
func (f *TopicFanout) Interruptions() uint64 { return f.interruptions.Load() }

func (f *TopicFanout) shard(topic string) *topicShard {
	var h uint32 = 2166136261
	for i := range topic {
		h = (h ^ uint32(topic[i])) * 16777619
	}
	return &f.shards[h%32]
}
func (f *TopicFanout) Watch(topics []string) (<-chan struct{}, func(), error) {
	topics, err := CanonicalTopics(topics)
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan struct{}, 1)
	for _, topic := range topics {
		s := f.shard(topic)
		s.mu.Lock()
		if s.topics[topic] == nil {
			s.topics[topic] = make(map[chan struct{}]struct{})
		}
		s.topics[topic][ch] = struct{}{}
		s.mu.Unlock()
	}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			for _, topic := range topics {
				s := f.shard(topic)
				s.mu.Lock()
				delete(s.topics[topic], ch)
				if len(s.topics[topic]) == 0 {
					delete(s.topics, topic)
				}
				s.mu.Unlock()
			}
		})
	}, nil
}
func (f *TopicFanout) committed(topic string) {
	s := f.shard(topic)
	s.mu.Lock()
	recipients := make([]chan struct{}, 0, len(s.topics[topic]))
	for ch := range s.topics[topic] {
		recipients = append(recipients, ch)
	}
	s.mu.Unlock()
	for _, ch := range recipients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *PostgresStream) ConsumeCommitted(ctx context.Context, c *natsdriver.Consumer, f *TopicFanout) error {
	if c == nil || f == nil {
		return ErrStreamUnavailable
	}
	for {
		messages, err := c.Fetch(ctx)
		if err != nil {
			return err
		}
		f.degraded.Store(false)
		for _, msg := range messages {
			frame, err := parseStreamV2(msg.Wire)
			if err != nil {
				return err
			}
			if msg.Subject != frame.Topic || msg.EventID != delivery.Hash([]byte(frame.EventID)) {
				return ErrFrame
			}
			bounded, cancel := context.WithTimeout(ctx, time.Second)
			var raw []byte
			var topic, partition, sequence string
			err = s.pool.QueryRow(bounded, "SELECT o.frame,p.topic,p.partition_id::text,o.sequence::text FROM "+s.table("stream_delivery_outbox")+" o JOIN "+s.table("stream_projection")+" p USING(partition_id) WHERE o.event_id=$1", frame.EventID).Scan(&raw, &topic, &partition, &sequence)
			cancel()
			if err != nil {
				return err
			}
			if !bytes.Equal(raw, msg.Wire) || topic != frame.Topic || sequence != frame.Sequence || partition == "" {
				return ErrFrame
			}
			f.committed(frame.Topic)
			if err = msg.Ack(ctx); err != nil {
				return err
			}
		}
	}
}
func (s *PostgresStream) RunCommitted(ctx context.Context, nc *nats.Conn, cfg natsdriver.ConsumerConfig, f *TopicFanout) error {
	if f != nil {
		f.degraded.Store(true)
		defer f.degraded.Store(true)
	}
	if nc == nil || f == nil {
		return ErrStreamUnavailable
	}
	if _, err := cfg.BrokerConsumerConfig(); err != nil {
		return err
	}
	if _, err := CanonicalTopics([]string{cfg.Subject}); err != nil {
		return err
	}
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		js, err := jetstream.New(nc)
		if err == nil {
			bounded, cancel := context.WithTimeout(ctx, time.Second)
			stream, e := js.Stream(bounded, cfg.Stream)
			if e == nil {
				info, e := stream.Info(bounded)
				if e != nil {
					err = e
				} else if info.Config.Replicas != 3 || info.Config.Storage != jetstream.FileStorage || info.Config.MaxAge < 72*time.Hour {
					err = ErrFrame
				}
			} else {
				err = e
			}
			cancel()
		}
		if errors.Is(err, ErrFrame) {
			f.degraded.Store(true)
			return err
		}
		if err == nil {
			var consumer *natsdriver.Consumer
			bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
			consumer, err = natsdriver.NewConsumer(bounded, nc, cfg)
			cancel()
			if err == nil {
				backoff = time.Second
				err = s.ConsumeCommitted(ctx, consumer, f)
				consumer.Close()
			}
		}
		f.degraded.Store(true)
		f.interruptions.Add(1)
		if errors.Is(err, ErrFrame) || errors.Is(err, ErrSequence) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (s *PostgresStream) PublicSnapshot(ctx context.Context, topics []string) (json.RawMessage, error) {
	if !PublicTopics(topics) {
		return nil, sharedauth.ErrScopeDenied
	}
	b := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: append([]string(nil), topics...), FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://anonymous.invalid", Endpoint: "/v2/stream"}
	return s.Snapshot(ctx, sharedauth.StreamPrincipal{}, b)
}
func (s *PostgresStream) PublicResume(ctx context.Context, topics []string, token string) (*PostgresReplay, error) {
	if !PublicTopics(topics) {
		return nil, sharedauth.ErrScopeDenied
	}
	b := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: append([]string(nil), topics...), FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://anonymous.invalid", Endpoint: "/v2/stream"}
	return s.Resume(ctx, sharedauth.StreamPrincipal{}, b, token)
}

type CheckpointTracker struct {
	replay  *PostgresReplay
	cursor  CursorV2
	hashes  map[string]string
	heights []string
}

func (r *PostgresReplay) WrittenCheckpoints() *CheckpointTracker {
	c := r.Cursor
	c.Offsets = make(map[string]string, len(r.Cursor.Offsets))
	for p, offset := range r.Cursor.Offsets {
		c.Offsets[p] = offset
	}
	return &CheckpointTracker{replay: r, cursor: c, hashes: make(map[string]string)}
}
func (t *CheckpointTracker) Advance(raw []byte) (string, error) {
	f, err := parseStreamV2(raw)
	if err != nil {
		return "", err
	}
	if len(f.Watermark) > 0 && string(f.Watermark) != "null" {
		var w struct {
			Height    string `json:"height"`
			BlockHash string `json:"blockHash"`
			Integrity string `json:"integrity"`
		}
		if json.Unmarshal(f.Watermark, &w) != nil || w.Integrity != "healthy" {
			return "", ErrFrame
		}
		if hash, ok := t.hashes[w.Height]; ok && !strings.EqualFold(hash, w.BlockHash) {
			return "", ErrFrame
		}
		if _, ok := t.hashes[w.Height]; !ok {
			if len(t.heights) == 1024 {
				delete(t.hashes, t.heights[0])
				t.heights = t.heights[1:]
			}
			t.heights = append(t.heights, w.Height)
			t.hashes[w.Height] = w.BlockHash
		}
	}
	for partition, topic := range t.replay.partitions {
		if topic != f.Topic {
			continue
		}
		current := t.cursor.Offsets[partition]
		if compare(f.Sequence, current) <= 0 {
			return "", ErrSequence
		}
		next := new(big.Int)
		next.SetString(current, 10)
		next.Add(next, big.NewInt(1))
		if f.Sequence != next.String() && (!f.Coalesced || f.First == nil || *f.First != next.String() || f.Last == nil || *f.Last != f.Sequence) {
			return "", ErrSequence
		}
		t.cursor.Offsets[partition] = f.Sequence
		return SignCursorV2(t.cursor, t.replay.source.key, time.Now())
	}
	return "", ErrFrame
}
func (t *CheckpointTracker) Token() (string, error) {
	return SignCursorV2(t.cursor, t.replay.source.key, time.Now())
}

type LiveQueue struct {
	must      [][]byte
	states    map[string][][]byte
	order     []string
	emitted   map[string]time.Time
	latest    map[string]string
	revisions map[string]string
	bytes     int
	metrics   *Lifecycle
}

func NewLiveQueue() *LiveQueue {
	return &LiveQueue{states: make(map[string][][]byte), emitted: make(map[string]time.Time), latest: make(map[string]string), revisions: make(map[string]string)}
}
func (q *LiveQueue) BindMetrics(l *Lifecycle) { q.metrics = l }
func (q *LiveQueue) Close() {
	if q.metrics != nil {
		frames, size := q.Occupancy()
		q.metrics.queueFrames.Add(-int64(frames))
		q.metrics.queueBytes.Add(-int64(size))
		q.metrics = nil
	}
	q.must = nil
	clear(q.states)
	q.order = nil
	q.bytes = 0
	clear(q.emitted)
	clear(q.latest)
	clear(q.revisions)
}
func replaceableTopic(topic string) bool {
	return strings.HasPrefix(topic, "market.price.") || strings.HasPrefix(topic, "market.stats.") || strings.HasPrefix(topic, "market.candles.") || strings.HasPrefix(topic, "market.rankings.")
}
func replacementKey(f Frame) string {
	if !strings.HasPrefix(f.Topic, "market.candles.") {
		return f.Topic
	}
	var payload struct {
		Start string `json:"start"`
	}
	if json.Unmarshal(f.Payload, &payload) != nil {
		return ""
	}
	return f.Topic + "\x00" + payload.Start
}
func (q *LiveQueue) reject(err error) error {
	if q.metrics != nil {
		if errors.Is(err, ErrStale) {
			q.metrics.staleRevisions.Add(1)
		}
		if errors.Is(err, ErrSequence) {
			q.metrics.sequenceGaps.Add(1)
		}
	}
	return err
}

func (q *LiveQueue) Add(raw []byte) error {
	f, err := parseStreamV2(raw)
	if err != nil {
		return err
	}
	if previous, ok := q.latest[f.Topic]; ok {
		expected, _ := new(big.Int).SetString(previous, 10)
		expected.Add(expected, big.NewInt(1))
		first := f.Sequence
		if f.First != nil {
			first = *f.First
		}
		if first != expected.String() {
			return q.reject(ErrSequence)
		}
	} else if len(q.latest) >= 20 {
		return ErrStreamUnavailable
	}
	state := replaceableTopic(f.Topic)
	key := replacementKey(f)
	if state {
		if previous, ok := q.revisions[key]; ok {
			if compare(f.Revision, previous) <= 0 {
				return q.reject(ErrStale)
			}
		} else if len(q.revisions) >= 128 {
			return ErrStreamUnavailable
		}
	}
	pending := q.states[f.Topic]
	var old []byte
	candidate := raw
	if state && len(pending) > 0 {
		previous, err := parseStreamV2(pending[len(pending)-1])
		if err != nil {
			return err
		}
		if compare(f.Sequence, previous.Sequence) <= 0 {
			return q.reject(ErrSequence)
		}
		expected, _ := new(big.Int).SetString(previous.Sequence, 10)
		expected.Add(expected, big.NewInt(1))
		first := f.Sequence
		if f.First != nil {
			first = *f.First
		}
		if first != expected.String() {
			return q.reject(ErrSequence)
		}
		if replacementKey(f) == replacementKey(previous) {
			if compare(f.Revision, previous.Revision) <= 0 {
				return q.reject(ErrStale)
			}
			old = pending[len(pending)-1]
			first := previous.Sequence
			if previous.First != nil {
				first = *previous.First
			}
			last := f.Sequence
			f.Coalesced = true
			f.First = &first
			f.Last = &last
			candidate, err = json.Marshal(f)
			if err != nil {
				return err
			}
		}
	}
	count, _ := q.Occupancy()
	if old == nil {
		count++
	}
	if count > 128 || q.bytes-len(old)+len(candidate) > 256*1024 {
		if q.metrics != nil {
			q.metrics.evictions.Add(1)
		}
		return ErrStreamUnavailable
	}
	if q.metrics != nil {
		if old == nil {
			q.metrics.queueFrames.Add(1)
		} else {
			q.metrics.coalesced.Add(1)
		}
		q.metrics.queueBytes.Add(int64(len(candidate) - len(old)))
	}
	q.bytes += len(candidate) - len(old)
	q.latest[f.Topic] = f.Sequence
	if state {
		q.revisions[key] = f.Revision
	}
	if state {
		if len(pending) == 0 {
			q.order = append(q.order, f.Topic)
		}
		if old == nil {
			pending = append(pending, candidate)
		} else {
			pending[len(pending)-1] = candidate
		}
		q.states[f.Topic] = pending
	} else {
		q.must = append(q.must, candidate)
	}
	return nil
}
func (q *LiveQueue) Drain(now time.Time) [][]byte {
	out := q.must
	q.must = nil
	for _, raw := range out {
		q.bytes -= len(raw)
	}
	keep := q.order[:0]
	for _, topic := range q.order {
		pending := q.states[topic]
		if now.Sub(q.emitted[topic]) < 500*time.Millisecond {
			keep = append(keep, topic)
			continue
		}
		raw := pending[0]
		out = append(out, raw)
		q.bytes -= len(raw)
		if len(pending) == 1 {
			delete(q.states, topic)
		} else {
			q.states[topic] = pending[1:]
			keep = append(keep, topic)
		}
		q.emitted[topic] = now
	}
	q.order = keep
	if q.metrics != nil {
		q.metrics.queueFrames.Add(-int64(len(out)))
		for _, raw := range out {
			q.metrics.queueBytes.Add(-int64(len(raw)))
		}
	}
	return out
}
func (q *LiveQueue) Occupancy() (int, int) {
	count := len(q.must)
	for _, pending := range q.states {
		count += len(pending)
	}
	return count, q.bytes
}

func (r *PostgresReplay) DeliveryBatch(ctx context.Context) ([]StreamDelivery, error) {
	checkpoint := r.Cursor
	checkpoint.Offsets = make(map[string]string, len(r.Cursor.Offsets))
	for p, sequence := range r.Cursor.Offsets {
		checkpoint.Offsets[p] = sequence
	}
	frames, err := r.Batch(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StreamDelivery, 0, len(frames))
	for _, raw := range frames {
		f, err := parseStreamV2(raw)
		if err != nil {
			return nil, err
		}
		found := false
		for partition, topic := range r.partitions {
			if topic == f.Topic {
				checkpoint.Offsets[partition] = f.Sequence
				found = true
				break
			}
		}
		if !found {
			return nil, ErrFrame
		}
		token, err := SignCursorV2(checkpoint, r.source.key, time.Now())
		if err != nil {
			return nil, err
		}
		out = append(out, StreamDelivery{Frame: raw, Cursor: token})
	}
	return out, nil
}

func SnapshotRoute(topics []string) string {
	return "/v2/snapshots?topics=" + url.QueryEscape(strings.Join(topics, ","))
}

func StreamFailure(err error) string {
	switch {
	case errors.Is(err, sharedauth.ErrScopeDenied):
		return "AUTH_REQUIRED"
	case errors.Is(err, ErrReplayLimit):
		return "REPLAY_LIMIT"
	case errors.Is(err, ErrExpired):
		return "RESUME_EXPIRED"
	case errors.Is(err, ErrCursor), errors.Is(err, ErrSequence):
		return "RESUME_INVALID"
	case errors.Is(err, ErrFrame):
		return "INTEGRITY_QUARANTINED"
	default:
		return "DEPENDENCY_UNAVAILABLE"
	}
}

func StreamControl(op string, subscriptionID, cursor, code string, topics []string, fields map[string]any) ([]byte, error) {
	body := map[string]any{"op": op, "subscriptionId": nil, "cursor": nil, "code": nil, "snapshotUrl": nil, "watermark": nil}
	if subscriptionID != "" {
		body["subscriptionId"] = subscriptionID
	}
	if cursor != "" {
		body["cursor"] = cursor
	}
	if code != "" {
		body["code"] = code
	}
	if op == "resnapshot_required" {
		body["snapshotUrl"] = SnapshotRoute(topics)
	}
	for k, v := range fields {
		if _, reserved := body[k]; reserved {
			return nil, ErrFrame
		}
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil || interfaces.ValidateJSON("StreamControl", raw) != nil {
		return nil, ErrFrame
	}
	return raw, nil
}
