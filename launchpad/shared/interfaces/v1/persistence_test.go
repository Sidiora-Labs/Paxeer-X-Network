package v1

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestPersistenceSignedBigintBoundary(t *testing.T) {
	p, err := NewCanonicalPayload([]byte(`{"fixture":true}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []uint64{MaxPersistenceCounter, MaxPersistenceCounter + 1} {
		wantOverflow := value > MaxPersistenceCounter
		intent := OutboxIntent{
			EventID: "event", EventType: "swap.committed", SchemaVersion: 1, ChainID: 125,
			Generation: "v1", Partition: "pool", PartitionSequence: value,
			EmittedAt: time.Now(), TraceID: "trace", Subject: "sidiora.v1.swap", Payload: p,
		}
		input := ConsumerInput{Key: InboxKey{Consumer: "charts", Generation: "v1", EventID: "event"}, Payload: p, Partition: "pool", PartitionSequence: value}
		fence := CheckpointFence{Lease: "lease", PreviousHeight: value, Generation: "v1"}
		checks := map[string]error{
			"counter":           ValidatePersistenceCounter(value),
			"outbox":            intent.Validate(),
			"consumer":          input.Validate(),
			"checkpoint":        fence.Validate(),
			"partition":         (PartitionWatermark{Partition: "pool", Sequence: value}).Validate(),
			"snapshot sequence": (SnapshotWatermark{Generation: "v1", Partitions: []PartitionWatermark{{Partition: "pool", Sequence: value}}}).Validate(),
			"snapshot block":    (SnapshotWatermark{Generation: "v1", BlockNumber: value}).Validate(),
			"ingest height":     (IngestBatch{BatchID: "batch", Fence: CheckpointFence{Lease: "lease", Generation: "v1"}, LastCompleteHeight: value}).Validate(),
			"ingest count":      (IngestBatch{BatchID: "batch", Fence: CheckpointFence{Lease: "lease", Generation: "v1"}, ExpectedRecords: value}).Validate(),
			"commit count":      (IngestCommit{BatchID: "batch", Records: value, CommittedAt: time.Now()}).Validate(),
			"publish ack":       (PublishAck{Stream: "events", Sequence: value}).Validate(),
		}
		for name, got := range checks {
			if wantOverflow && !errors.Is(got, ErrPersistenceOverflow) {
				t.Errorf("%s: overflow error = %v", name, got)
			}
			if !wantOverflow && got != nil {
				t.Errorf("%s: signed BIGINT maximum refused: %v", name, got)
			}
		}
	}
	if err := (PartitionWatermark{Partition: "pool", Sequence: 0}).Validate(); err != nil {
		t.Fatal("initial zero snapshot watermark refused", err)
	}
}

func TestCanonicalPayloadOwnershipAndIntegrity(t *testing.T) {
	encoded := []byte(`{"event_id":"125:fixture:1"}`)
	want := sha256.Sum256(encoded)
	p, err := NewCanonicalPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = '!'
	copy := p.Bytes()
	copy[0] = '!'
	if p.Hash() != want || p.Validate() != nil || p.Bytes()[0] != '{' {
		t.Fatal("caller mutation changed canonical stored bytes or hash")
	}
	if _, err := VerifyCanonicalPayload(p.Bytes(), want); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCanonicalPayload(encoded, want); err == nil {
		t.Fatal("producer hash mismatch accepted")
	}
	p.bytes[0] = '!'
	if p.Validate() == nil {
		t.Fatal("corrupted stored payload accepted")
	}
}

func TestCanonicalPayloadBounds(t *testing.T) {
	for _, input := range [][]byte{nil, {}, make([]byte, MaxOutboxPayloadBytes+1)} {
		if _, err := NewCanonicalPayload(input); err == nil {
			t.Fatal("invalid length accepted")
		}
	}
	if _, err := NewCanonicalPayload(make([]byte, MaxOutboxPayloadBytes)); err != nil {
		t.Fatal(err)
	}
	if (CanonicalPayload{}).Validate() == nil {
		t.Fatal("zero-value payload accepted")
	}
}

func TestOutboxRequiredIdentity(t *testing.T) {
	p, err := NewCanonicalPayload([]byte(`{"fixture":true}`))
	if err != nil {
		t.Fatal(err)
	}
	valid := OutboxIntent{
		EventID: "event-1", EventType: "swap.committed", SchemaVersion: 1,
		ChainID: 125, Generation: "v1", Partition: "pool:1", PartitionSequence: 1,
		EmittedAt: time.Now(), TraceID: "trace-1", Subject: "sidiora.v1.swap", Payload: p,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*OutboxIntent){
		func(i *OutboxIntent) { i.EventID = "" },
		func(i *OutboxIntent) { i.EventType = "" },
		func(i *OutboxIntent) { i.SchemaVersion = 0 },
		func(i *OutboxIntent) { i.ChainID = 1 },
		func(i *OutboxIntent) { i.Generation = "" },
		func(i *OutboxIntent) { i.Partition = "" },
		func(i *OutboxIntent) { i.PartitionSequence = 0 },
		func(i *OutboxIntent) { i.EmittedAt = time.Time{} },
		func(i *OutboxIntent) { i.TraceID = "" },
		func(i *OutboxIntent) { i.Subject = "" },
		func(i *OutboxIntent) { i.Payload = CanonicalPayload{} },
	}
	for _, mutate := range mutations {
		i := valid
		mutate(&i)
		if i.Validate() == nil {
			t.Fatal("missing required envelope identity accepted")
		}
	}
}

func TestInboxIdentityScope(t *testing.T) {
	k := InboxKey{Consumer: "charts", Generation: "v1", EventID: "event-1"}
	if k.Validate() != nil {
		t.Fatal("valid inbox key refused")
	}
	for _, invalid := range []InboxKey{{Generation: "v1", EventID: "event-1"}, {Consumer: "charts", EventID: "event-1"}, {Consumer: "charts", Generation: "v1"}} {
		if invalid.Validate() == nil {
			t.Fatal("incomplete inbox key accepted")
		}
	}
}
