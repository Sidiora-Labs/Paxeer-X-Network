package broker

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func publicPriceFrame(t *testing.T, sequence string) Frame {
	t.Helper()
	asset := map[string]any{"kind": "erc20", "chainId": "125", "address": "0x" + strings.Repeat("1", 40), "currencyCode": nil, "decimals": 18}
	quote := map[string]any{"kind": "erc20", "chainId": "125", "address": "0x" + strings.Repeat("2", 40), "currencyCode": nil, "decimals": 6}
	at := "2026-10-12T00:00:00.000Z"
	payload, err := json.Marshal(map[string]any{"numerator": sequence, "denominator": "1", "baseAsset": asset, "quoteAsset": quote, "source": map[string]any{"kind": "post_swap_spot", "amountBasis": "effective_reserves", "sourceEventId": nil, "position": nil, "providerId": nil, "observedAt": at, "sourceReference": nil}})
	if err != nil {
		t.Fatal(err)
	}
	return Frame{EventID: uuid.NewString(), Topic: "market.price.0x" + strings.Repeat("1", 40), Sequence: sequence, Revision: sequence, OccurredAt: at, Watermark: json.RawMessage("null"), Payload: payload}
}

func priceWire(t *testing.T, sequence string) []byte {
	t.Helper()
	raw, err := json.Marshal(publicPriceFrame(t, sequence))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLiveQueueCoalescesCoverageAtTwoHertz(t *testing.T) {
	q := NewLiveQueue()
	now := time.Unix(1800000000, 0)
	if err := q.Add(priceWire(t, "1")); err != nil {
		t.Fatal(err)
	}
	if got := q.Drain(now); len(got) != 1 {
		t.Fatal(len(got))
	}
	if err := q.Add(priceWire(t, "2")); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(priceWire(t, "3")); err != nil {
		t.Fatal(err)
	}
	if got := q.Drain(now.Add(499 * time.Millisecond)); len(got) != 0 {
		t.Fatal("state emitted above2Hz")
	}
	got := q.Drain(now.Add(500 * time.Millisecond))
	if len(got) != 1 {
		t.Fatal(len(got))
	}
	f, err := parseStreamV2(got[0])
	if err != nil || !f.Coalesced || f.Sequence != "3" || f.First == nil || *f.First != "2" || f.Last == nil || *f.Last != "3" {
		t.Fatal(f, err)
	}
	if frames, size := q.Occupancy(); frames != 0 || size != 0 {
		t.Fatal(frames, size)
	}
}

func TestLiveQueueNeverCoalescesPrivateTransitions(t *testing.T) {
	q := NewLiveQueue()
	for i := 1; i <= 128; i++ {
		f := notificationStreamFrame(t, strconv.Itoa(i))
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if err = q.Add(raw); err != nil {
			t.Fatal(err)
		}
	}
	f := notificationStreamFrame(t, "129")
	raw, _ := json.Marshal(f)
	if q.Add(raw) == nil {
		t.Fatal("129th transition admitted")
	}
	frames, size := q.Occupancy()
	if frames != 128 || size > 256*1024 {
		t.Fatal(frames, size)
	}
	if out := q.Drain(time.Now()); len(out) != 128 {
		t.Fatal("private transition silently dropped", len(out))
	}
}

func TestPublicSnapshotReplayBarrierAndWrittenCursor(t *testing.T) {
	ctx, s := postgresStreamStorage(t)
	first := publicPriceFrame(t, "1")
	commitStreamFrame(t, ctx, s, first)
	raw, err := s.PublicSnapshot(ctx, []string{first.Topic})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		ResumeCursor string `json:"resumeCursor"`
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	second := publicPriceFrame(t, "2")
	commitStreamFrame(t, ctx, s, second)
	replay, err := s.PublicResume(ctx, []string{first.Topic}, snapshot.ResumeCursor)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	third := publicPriceFrame(t, "3")
	commitStreamFrame(t, ctx, s, third)
	written := replay.WrittenCheckpoints()
	batch, err := replay.DeliveryBatch(ctx)
	if err != nil || len(batch) != 1 || replay.Machine.State != Live {
		t.Fatal(len(batch), replay.Machine.State, err)
	}
	f, err := parseStreamV2(batch[0].Frame)
	if err != nil || f.EventID != second.EventID {
		t.Fatal(f, err)
	}
	token, err := written.Advance(batch[0].Frame)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := VerifyCursorV2(token, s.key, replay.Cursor.Scope, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range cursor.Offsets {
		if offset != "2" {
			t.Fatal("cursor acknowledged unwritten live frame", offset)
		}
	}
	batch, err = replay.DeliveryBatch(ctx)
	if err != nil || len(batch) != 1 {
		t.Fatal(len(batch), err)
	}
	f, err = parseStreamV2(batch[0].Frame)
	if err != nil || f.EventID != third.EventID {
		t.Fatal(f, err)
	}
	if _, err = s.PublicResume(ctx, []string{"private.notifications.0x" + strings.Repeat("1", 40)}, token); err == nil {
		t.Fatal("public cursor granted private scope")
	}
	if _, err = s.pool.Exec(ctx, "UPDATE "+s.table("stream_generation")+" SET generation=gen_random_uuid() WHERE singleton"); err != nil {
		t.Fatal(err)
	}
	if err = replay.Check(context.Background()); !errors.Is(err, ErrCursor) {
		t.Fatal("generation replacement did not resync", err)
	}
}

func TestV2ExpiredResumeReturnsExpired(t *testing.T) {
	ctx, s := postgresStreamStorage(t)
	f := publicPriceFrame(t, "1")
	commitStreamFrame(t, ctx, s, f)
	raw, err := s.PublicSnapshot(ctx, []string{f.Topic})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		ResumeCursor string `json:"resumeCursor"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		t.Fatal("snapshot")
	}
	replay, err := s.PublicResume(ctx, []string{f.Topic}, snapshot.ResumeCursor)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	c := replay.Cursor
	now := time.Now()
	c.IssuedAt = now.Add(-73 * time.Hour).Unix()
	c.ExpiresAt = now.Add(-time.Hour).Unix()
	token, err := SignCursorV2(c, s.key, time.Unix(c.IssuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicResume(ctx, []string{f.Topic}, token); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
}

func TestWrittenCheckpointPreservesUnwrittenPartitions(t *testing.T) {
	ctx, s := postgresStreamStorage(t)
	a := publicPriceFrame(t, "1")
	b := publicPriceFrame(t, "1")
	b.Topic = "market.price.0x" + strings.Repeat("3", 40)
	commitStreamFrame(t, ctx, s, a)
	commitStreamFrame(t, ctx, s, b)
	topics := []string{a.Topic, b.Topic}
	raw, err := s.PublicSnapshot(ctx, topics)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		ResumeCursor string `json:"resumeCursor"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		t.Fatal("snapshot")
	}
	for _, seq := range []string{"2", "3"} {
		commitStreamFrame(t, ctx, s, publicPriceFrame(t, seq))
	}
	b2 := publicPriceFrame(t, "2")
	b2.Topic = b.Topic
	commitStreamFrame(t, ctx, s, b2)
	replay, err := s.PublicResume(ctx, topics, snapshot.ResumeCursor)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	written := replay.WrittenCheckpoints()
	batch, err := replay.DeliveryBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	q := NewLiveQueue()
	l := NewLifecycle()
	q.BindMetrics(l)
	defer q.Close()
	var token string
	for _, delivery := range batch {
		f, e := parseStreamV2(delivery.Frame)
		if e != nil {
			t.Fatal(e)
		}
		if f.Topic == b.Topic {
			token, e = written.Advance(delivery.Frame)
			if e != nil {
				t.Fatal(e)
			}
		} else {
			if e = q.Add(delivery.Frame); e != nil {
				t.Fatal(e)
			}
		}
	}
	cursor, err := VerifyCursorV2(token, s.key, replay.Cursor.Scope, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for partition, topic := range replay.partitions {
		want := "1"
		if topic == b.Topic {
			want = "2"
		}
		if cursor.Offsets[partition] != want {
			t.Fatal("unwritten partition acknowledged", topic, cursor.Offsets[partition])
		}
	}
	metrics := l.QueueMetrics()
	if metrics.Frames != 1 || metrics.Coalesced != 1 || metrics.Bytes == 0 {
		t.Fatal(metrics)
	}
	out := q.Drain(time.Now())
	if len(out) != 1 {
		t.Fatal(len(out))
	}
	token, err = written.Advance(out[0])
	if err != nil {
		t.Fatal(err)
	}
	cursor, err = VerifyCursorV2(token, s.key, replay.Cursor.Scope, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for partition, topic := range replay.partitions {
		want := "3"
		if topic == b.Topic {
			want = "2"
		}
		if cursor.Offsets[partition] != want {
			t.Fatal(topic, cursor.Offsets[partition])
		}
	}
	if metrics = l.QueueMetrics(); metrics.Frames != 0 || metrics.Bytes != 0 {
		t.Fatal(metrics)
	}
	c := replay.Cursor
	now := time.Now()
	c.IssuedAt = now.Add(-71 * time.Hour).Unix()
	c.ExpiresAt = now.Add(time.Hour).Unix()
	token, err = SignCursorV2(c, s.key, time.Unix(c.IssuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := s.PublicResume(ctx, topics, token)
	if err != nil {
		t.Fatal("71hour cursor rejected", err)
	}
	retained.Close()
}

func TestLiveQueueRejectsHiddenSequenceGapAndStaleReplacement(t *testing.T) {
	q := NewLiveQueue()
	if err := q.Add(priceWire(t, "10")); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(priceWire(t, "12")); !errors.Is(err, ErrSequence) {
		t.Fatal("coalescing concealed missing durable identity", err)
	}
	f := publicPriceFrame(t, "11")
	f.Revision = "9"
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Add(raw); !errors.Is(err, ErrStale) {
		t.Fatal("stale state replaced revision10", err)
	}
	f.Revision = "10"
	raw, err = json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Add(raw); !errors.Is(err, ErrStale) {
		t.Fatal("equal revision replaced state", err)
	}
	f.Revision = "11"
	raw, err = json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Add(raw); err != nil {
		t.Fatal(err)
	}
	out := q.Drain(time.Now())
	if len(out) != 1 {
		t.Fatal(len(out))
	}
	got, err := parseStreamV2(out[0])
	if err != nil || got.Revision != "11" || got.First == nil || *got.First != "10" || got.Last == nil || *got.Last != "11" {
		t.Fatal(got, err)
	}
}

func TestTopicFanoutSaturatedWatcherDoesNotBlockOtherTopic(t *testing.T) {
	f := NewTopicFanout()
	a := publicPriceFrame(t, "1").Topic
	b := "market.price.0x" + strings.Repeat("2", 40)
	stalled, stopA, err := f.Watch([]string{a})
	if err != nil {
		t.Fatal(err)
	}
	defer stopA()
	healthy, stopB, err := f.Watch([]string{b})
	if err != nil {
		t.Fatal(err)
	}
	defer stopB()
	for i := 0; i < 10000; i++ {
		f.committed(a)
	}
	f.committed(b)
	select {
	case <-healthy:
	default:
		t.Fatal("saturated watcher blocked healthy topic")
	}
	select {
	case <-stalled:
	default:
		t.Fatal("commit wakeup missing")
	}
	stopA()
	stopB()
	for i := range f.shards {
		if len(f.shards[i].topics) != 0 {
			t.Fatal("watch allocation leaked")
		}
	}
}

func TestLiveQueueRetainsRevisionAndSequenceAfterDrain(t *testing.T) {
	q := NewLiveQueue()
	if err := q.Add(priceWire(t, "10")); err != nil {
		t.Fatal(err)
	}
	if len(q.Drain(time.Now())) != 1 {
		t.Fatal("initial state missing")
	}
	f := publicPriceFrame(t, "11")
	f.Revision = "9"
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Add(raw); !errors.Is(err, ErrStale) {
		t.Fatal("drained revision was forgotten", err)
	}
	if err = q.Add(priceWire(t, "12")); !errors.Is(err, ErrSequence) {
		t.Fatal("drained sequence was forgotten", err)
	}
	if err = q.Add(priceWire(t, "11")); err != nil {
		t.Fatal("higher correction rejected", err)
	}
}

func TestRejectedStateMetricsRemainDistinctFromQueueEviction(t *testing.T) {
	l := NewLifecycle()
	q := NewLiveQueue()
	q.BindMetrics(l)
	defer q.Close()
	if err := q.Add(priceWire(t, "10")); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(priceWire(t, "12")); !errors.Is(err, ErrSequence) {
		t.Fatal(err)
	}
	f := publicPriceFrame(t, "11")
	f.Revision = "9"
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Add(raw); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	m := l.QueueMetrics()
	if m.SequenceGapRejects != 1 || m.StaleRevisionRejects != 1 || m.SlowConsumerEvictions != 0 || m.Frames != 1 {
		t.Fatal(m)
	}
}

func correctedCandleFrame(t *testing.T, sequence, revision string) []byte {
	t.Helper()
	f := validTestFrame(t)
	f.EventID = uuid.NewString()
	f.Sequence = sequence
	f.Revision = revision
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	price := publicPriceFrame(t, revision).Payload
	for _, field := range []string{"open", "high", "low", "close"} {
		payload[field] = price
	}
	var err error
	payload["revision"], err = json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	f.Payload, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCorrectedCandleTenStaleNineThenEleven(t *testing.T) {
	q := NewLiveQueue()
	l := NewLifecycle()
	q.BindMetrics(l)
	defer q.Close()
	now := time.Now()
	if err := q.Add(correctedCandleFrame(t, "10", "10")); err != nil {
		t.Fatal(err)
	}
	first := q.Drain(now)
	if len(first) != 1 {
		t.Fatal("revision10 missing")
	}
	if err := q.Add(correctedCandleFrame(t, "11", "9")); !errors.Is(err, ErrStale) {
		t.Fatal("revision9 overwrote revision10", err)
	}
	if len(q.Drain(now.Add(500*time.Millisecond))) != 0 {
		t.Fatal("stale candle delivered")
	}
	if err := q.Add(correctedCandleFrame(t, "11", "11")); err != nil {
		t.Fatal(err)
	}
	correction := q.Drain(now.Add(time.Second))
	if len(correction) != 1 {
		t.Fatal("revision11 missing")
	}
	f, err := parseStreamV2(correction[0])
	if err != nil || f.Revision != "11" || f.Sequence != "11" || l.QueueMetrics().StaleRevisionRejects != 1 {
		t.Fatal(f, err, l.QueueMetrics())
	}
}
