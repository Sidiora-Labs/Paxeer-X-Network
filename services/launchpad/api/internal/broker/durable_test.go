package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestVersionedFrame(t *testing.T) {
	f := validTestFrame(t)
	raw, _ := json.Marshal(f)
	got, err := ParseFrame("1", raw)
	if err != nil || got.Sequence != f.Sequence {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"2", ""} {
		if _, err := ParseFrame(bad, raw); err == nil {
			t.Fatal("version accepted")
		}
	}
	for _, seq := range []string{"01", "-1", "9223372036854775808"} {
		f.Sequence = seq
		raw, _ = json.Marshal(f)
		if _, err := ParseFrame("1", raw); err == nil {
			t.Fatal(seq)
		}
	}
	if _, err := ParseFrame("1", []byte(`{}`)); err == nil {
		t.Fatal("missing fields")
	}
}
func TestTopicCorrections(t *testing.T) {
	tr := NewRevisionTracker(4)
	f := Frame{Topic: "a", Sequence: "1", Revision: "10"}
	if err := tr.Apply(f, "candle"); err != nil {
		t.Fatal(err)
	}
	f.Sequence = "2"
	f.Revision = "9"
	if !errors.Is(tr.Apply(f, "candle"), ErrStale) {
		t.Fatal("stale accepted")
	}
	f.Revision = "11"
	if err := tr.Apply(f, "candle"); err != nil {
		t.Fatal(err)
	}
	f.Topic = "b"
	f.Sequence = "1"
	if err := tr.Apply(f, "candle"); err != nil {
		t.Fatal("global gap false positive", err)
	}
}
func TestCursorScopeExpiryTampering(t *testing.T) {
	now := time.Unix(1800000000, 0)
	key := []byte(strings.Repeat("k", 32))
	scope := CursorScope{"125", "alice", "3", "epoch", "1", "filter"}
	c := Cursor{scope, map[string]string{"p": "9007199254740993"}, now.Unix(), now.Add(72 * time.Hour).Unix()}
	token, err := SignCursor(c, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCursor(token, key, scope, now.Add(71*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCursor(token, key, scope, now.Add(73*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		wrong := scope
		switch i {
		case 0:
			wrong.Chain = "1"
		case 1:
			wrong.Subject = "bob"
		case 2:
			wrong.PermissionEpoch = "4"
		case 3:
			wrong.ProjectionEpoch = "other"
		case 4:
			wrong.Schema = "2"
		case 5:
			wrong.FilterHash = "expanded"
		}
		if _, err := VerifyCursor(token, key, wrong, now); err == nil {
			t.Fatal(i)
		}
	}
	if _, err := VerifyCursor("a"+token, key, scope, now); err == nil {
		t.Fatal("tamper")
	}
}
func TestBoundedSubscription(t *testing.T) {
	for _, size := range []int{1, 2048, 256*1024 + 1} {
		b := New(Options{})
		s := b.Subscribe(Filter{}, 10000)
		defer s.Close()
		for i := 0; i < 200; i++ {
			s.deliver(OutMessage{bytes: make([]byte, size)})
		}
		f, n := s.Occupancy()
		if f > 128 || n > 256*1024 || !s.Evicted() {
			t.Fatal(f, n, s.Evicted())
		}
	}
	b := New(Options{})
	s := b.Subscribe(Filter{}, 128)
	defer s.Close()
	s.deliver(OutMessage{coalesceKey: "a", bytes: make([]byte, 200000)})
	s.deliver(OutMessage{coalesceKey: "a", bytes: make([]byte, 250000)})
	f, n := s.Occupancy()
	if f != 1 || n != 250000 {
		t.Fatal(f, n)
	}
	s.Drain()
	f, n = s.Occupancy()
	if f != 0 || n != 0 {
		t.Fatal(f, n)
	}
}
func TestResumeTransitionsAndLimits(t *testing.T) {
	m := NewResumeMachine()
	m.Authenticate(true)
	if err := m.Begin("10", "12"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"11", "12"} {
		if err := m.Replay(s, 200); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Complete(); err != nil || m.State != Live {
		t.Fatal(err, m.State)
	}
	m = NewResumeMachine()
	m.Authenticate(true)
	m.Begin("10", "12")
	if err := m.Replay("10", 1); err == nil || m.State != ResyncRequired {
		t.Fatal("snapshot overlap accepted")
	}
	m = NewResumeMachine()
	m.Authenticate(true)
	m.Begin("0", "1")
	if err := m.Replay("1", 10*1024*1024+1); err == nil {
		t.Fatal("unbounded replay")
	}
}

// boundaryDelivery tests callback/ack ordering only; this is not broker
// persistence evidence and never substitutes for a real JetStream fixture.
type boundaryDelivery struct {
	raw     []byte
	acked   bool
	retried bool
}

func (d *boundaryDelivery) Payload() []byte             { return d.raw }
func (d *boundaryDelivery) StreamSequence() uint64      { return 1 }
func (d *boundaryDelivery) Partition() string           { return "p" }
func (d *boundaryDelivery) Ack(context.Context) error   { d.acked = true; return nil }
func (d *boundaryDelivery) Retry(context.Context) error { d.retried = true; return nil }

type boundaryConsumer struct {
	d      *boundaryDelivery
	closed bool
	sent   bool
}

func (c *boundaryConsumer) Next(context.Context) (DurableDelivery, error) {
	if c.sent {
		return nil, io.EOF
	}
	c.sent = true
	return c.d, nil
}
func (c *boundaryConsumer) Close() error { c.closed = true; return nil }
func TestDurableAcknowledgementBoundary(t *testing.T) {
	for _, reject := range []bool{false, true} {
		f := validTestFrame(t)
		raw, _ := json.Marshal(f)
		d := &boundaryDelivery{raw: raw}
		c := &boundaryConsumer{d: d}
		err := ConsumeDurable(context.Background(), c, "1", func(_ context.Context, _ Frame, p string, s uint64) error {
			if d.acked || p != "p" || s != 1 {
				t.Fatal("ack before processing")
			}
			if reject {
				return ErrStale
			}
			return nil
		})
		if err == nil || !c.closed || d.acked == reject || d.retried != reject {
			t.Fatal(err, c, d)
		}
	}
}

func TestV2CursorScopeSeparationAndBound(t *testing.T) {
	now := time.Unix(1800000000, 0)
	key := []byte(strings.Repeat("k", 32))
	scope := CursorScopeV2{Chain: "125", Subject: "account", SessionID: "session", PermissionEpoch: "3", ProjectionEpoch: "projection", SchemaMajor: 2, FilterHash: "topics-filter-digest", SnapshotID: "snapshot"}
	c := CursorV2{FormatVersion: 1, Scope: scope, Offsets: map[string]string{"p1": "9007199254740993", "p2": "20"}, IssuedAt: now.Unix(), ExpiresAt: now.Add(72 * time.Hour).Unix()}
	token, err := SignCursorV2(c, key, now)
	if err != nil || len(token) > 2048 {
		t.Fatal(token, err)
	}
	decoded, err := VerifyCursorV2(token, key, scope, now)
	if err != nil || decoded.Offsets["p1"] != "9007199254740993" {
		t.Fatal(decoded, err)
	}
	for _, mutate := range []func(*CursorScopeV2){
		func(s *CursorScopeV2) { s.SessionID = "other" },
		func(s *CursorScopeV2) { s.PermissionEpoch = "4" },
		func(s *CursorScopeV2) { s.FilterHash = "expanded" },
		func(s *CursorScopeV2) { s.SnapshotID = "other" },
		func(s *CursorScopeV2) { s.ProjectionEpoch = "new" },
		func(s *CursorScopeV2) { s.SchemaMajor = 1 },
	} {
		wrong := scope
		mutate(&wrong)
		if _, err := VerifyCursorV2(token, key, wrong, now); err == nil {
			t.Fatal("foreign scope accepted", wrong)
		}
	}
	if _, err := VerifyCursorV2(token, key, scope, now.Add(72*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	if _, err := VerifyCursorV2(strings.Repeat("a", 2049), key, scope, now); err == nil {
		t.Fatal("oversized cursor")
	}
	legacyScope := CursorScope{"125", "account", "3", "projection", "1", "topics-filter-digest"}
	legacy, err := SignCursor(Cursor{legacyScope, map[string]string{"p1": "1"}, now.Unix(), now.Add(time.Hour).Unix()}, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCursorV2(legacy, key, scope, now); err == nil {
		t.Fatal("v1 cursor reinterpreted")
	}
	if _, err := VerifyCursor(token, key, legacyScope, now); err == nil {
		t.Fatal("v2 cursor accepted in v1")
	}
	c.Scope.SchemaMajor = 1
	if _, err := SignCursorV2(c, key, now); err == nil {
		t.Fatal("wrong schema signed")
	}
}

func TestPartitionReplayContiguousBarrier(t *testing.T) {
	snapshot := map[string]string{"a": "9007199254740993", "b": "20"}
	barrier := map[string]string{"a": "9007199254740995", "b": "21"}
	m, err := NewPartitionReplay(snapshot, barrier)
	if err != nil {
		t.Fatal(err)
	}
	snapshot["a"] = "0"
	barrier["b"] = "100"
	for _, step := range []struct{ partition, sequence string }{{"a", "9007199254740994"}, {"b", "21"}, {"a", "9007199254740995"}} {
		if m.State != Replaying {
			t.Fatal("premature live", m.State)
		}
		if err := m.Replay(step.partition, step.sequence, 100); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Complete(); err != nil || m.State != Live {
		t.Fatal(err, m.State)
	}
	offsets := m.Offsets()
	offsets["a"] = "0"
	if m.Offsets()["a"] != "9007199254740995" {
		t.Fatal("offsets exposed mutable state")
	}
	for _, sequence := range []string{"10", "12"} {
		m, err = NewPartitionReplay(map[string]string{"a": "10"}, map[string]string{"a": "12"})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Replay("a", sequence, 1); err == nil || m.State != ResyncRequired {
			t.Fatal("overlap or gap accepted", sequence)
		}
	}
	m, err = NewPartitionReplay(map[string]string{"a": "10"}, map[string]string{"a": "12"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Complete(); err == nil || m.State != ResyncRequired {
		t.Fatal("incomplete barrier advertised live")
	}
	for _, pair := range []struct{ snapshot, barrier map[string]string }{
		{map[string]string{"a": "1"}, map[string]string{"b": "1"}},
		{map[string]string{"a": "2"}, map[string]string{"a": "1"}},
		{map[string]string{"a": "01"}, map[string]string{"a": "2"}},
	} {
		if _, err := NewPartitionReplay(pair.snapshot, pair.barrier); err == nil {
			t.Fatal("invalid partition vector accepted")
		}
	}
}
