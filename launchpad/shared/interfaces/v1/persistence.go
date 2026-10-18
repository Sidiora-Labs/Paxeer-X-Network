package v1

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const MaxOutboxPayloadBytes = 1 << 20
const MaxPersistenceCounter uint64 = 1<<63 - 1

var ErrInvalidPersistenceRecord = errors.New("invalid persistence record")
var ErrPersistenceOverflow = errors.New("persistence counter exceeds signed BIGINT")

func ValidatePersistenceCounter(value uint64) error {
	if value > MaxPersistenceCounter {
		return ErrPersistenceOverflow
	}
	return nil
}

type PersistenceEventID string
type PersistenceConsumerID string
type PersistenceGeneration string
type PersistencePartition string
type PersistenceLeaseToken string
type PersistenceHash [sha256.Size]byte

// CanonicalPayload contains bounded, already schema-validated envelope bytes.
// The generated event codec, rather than the persistence layer, owns decoding.
type CanonicalPayload struct {
	bytes []byte
	hash  PersistenceHash
}

func NewCanonicalPayload(encoded []byte) (CanonicalPayload, error) {
	if len(encoded) == 0 || len(encoded) > MaxOutboxPayloadBytes {
		return CanonicalPayload{}, ErrInvalidPersistenceRecord
	}
	return CanonicalPayload{bytes: append([]byte(nil), encoded...), hash: sha256.Sum256(encoded)}, nil
}

// VerifyCanonicalPayload is the consumer boundary for a producer-supplied hash.
func VerifyCanonicalPayload(encoded []byte, expected PersistenceHash) (CanonicalPayload, error) {
	payload, err := NewCanonicalPayload(encoded)
	if err != nil || payload.hash != expected {
		return CanonicalPayload{}, ErrInvalidPersistenceRecord
	}
	return payload, nil
}

func (p CanonicalPayload) Bytes() []byte         { return append([]byte(nil), p.bytes...) }
func (p CanonicalPayload) Hash() PersistenceHash { return p.hash }

func (p CanonicalPayload) Validate() error {
	if len(p.bytes) == 0 || len(p.bytes) > MaxOutboxPayloadBytes || sha256.Sum256(p.bytes) != p.hash {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type PersistenceSourceOrder struct {
	BlockNumber      uint64
	TransactionIndex uint32
	LogIndex         uint32
}

type OutboxIntent struct {
	EventID           PersistenceEventID
	EventType         string
	SchemaVersion     uint32
	ChainID           uint64
	Generation        PersistenceGeneration
	Partition         PersistencePartition
	PartitionSequence uint64
	SourceOrder       PersistenceSourceOrder
	EmittedAt         time.Time
	TraceID           string
	Subject           string
	Payload           CanonicalPayload
}

func (i OutboxIntent) Validate() error {
	if err := ValidatePersistenceCounter(i.PartitionSequence); err != nil {
		return err
	}
	if err := ValidatePersistenceCounter(i.SourceOrder.BlockNumber); err != nil {
		return err
	}
	if i.EventID == "" || i.EventType == "" || i.SchemaVersion == 0 || i.ChainID != 125 ||
		i.Generation == "" || i.Partition == "" || i.PartitionSequence == 0 ||
		i.EmittedAt.IsZero() || i.TraceID == "" || i.Subject == "" {
		return ErrInvalidPersistenceRecord
	}
	return i.Payload.Validate()
}

// PersistenceTransaction exposes SQL work but no transaction lifecycle methods. All
// repositories used by an effect must be bound to this same scope, never a pool.
type PersistenceTransaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type EffectResult struct {
	Outbox []OutboxIntent
}

type InboxKey struct {
	Consumer   PersistenceConsumerID
	Generation PersistenceGeneration
	EventID    PersistenceEventID
}

func (k InboxKey) Validate() error {
	if k.Consumer == "" || k.Generation == "" || k.EventID == "" {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type ConsumerInput struct {
	Key               InboxKey
	Payload           CanonicalPayload
	Partition         PersistencePartition
	PartitionSequence uint64
	SourceOrder       PersistenceSourceOrder
}

func (i ConsumerInput) Validate() error {
	if err := ValidatePersistenceCounter(i.PartitionSequence); err != nil {
		return err
	}
	if err := ValidatePersistenceCounter(i.SourceOrder.BlockNumber); err != nil {
		return err
	}
	if i.Partition == "" || i.PartitionSequence == 0 {
		return ErrInvalidPersistenceRecord
	}
	if err := i.Key.Validate(); err != nil {
		return err
	}
	return i.Payload.Validate()
}

type ApplyResult struct {
	Duplicate bool
}

type ApplyEffect func(context.Context, PersistenceTransaction, ConsumerInput) (EffectResult, error)

// ConsumerUnit commits inbox uniqueness, effects, checkpoint and returned
// outbox intents together. Success means committed, including a hash-matched
// already-committed duplicate. Unknown commit outcomes must return an error.
type ConsumerUnit interface {
	Apply(context.Context, ConsumerInput, ApplyEffect) (ApplyResult, error)
}

type CheckpointFence struct {
	Lease          PersistenceLeaseToken
	PreviousHeight uint64
	Generation     PersistenceGeneration
}

func (f CheckpointFence) Validate() error {
	if err := ValidatePersistenceCounter(f.PreviousHeight); err != nil {
		return err
	}
	if f.Lease == "" || f.Generation == "" {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type IngestBatch struct {
	BatchID            string
	Fence              CheckpointFence
	LastCompleteHeight uint64
	LastBlockHash      PersistenceHash
	ExpectedRecords    uint64
}

func (b IngestBatch) Validate() error {
	for _, value := range []uint64{b.LastCompleteHeight, b.ExpectedRecords} {
		if err := ValidatePersistenceCounter(value); err != nil {
			return err
		}
	}
	if err := b.Fence.Validate(); err != nil {
		return err
	}
	if b.BatchID == "" || b.LastCompleteHeight < b.Fence.PreviousHeight {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type IngestCommit struct {
	BatchID            string
	PreviousHeight     uint64
	LastCompleteHeight uint64
	Records            uint64
	OutboxRecords      uint64
	CommittedAt        time.Time
}

func (c IngestCommit) Validate() error {
	for _, value := range []uint64{c.PreviousHeight, c.LastCompleteHeight, c.Records, c.OutboxRecords} {
		if err := ValidatePersistenceCounter(value); err != nil {
			return err
		}
	}
	if c.BatchID == "" || c.CommittedAt.IsZero() || c.LastCompleteHeight < c.PreviousHeight {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type IngestEffect func(context.Context, PersistenceTransaction, IngestBatch) (EffectResult, error)

// CommitBatch validates completeness, expected event intents and checkpoint CAS
// in the same transaction as the raw and typed rows. Network publication is not
// part of this transaction.
type IngestUnit interface {
	CommitBatch(context.Context, IngestBatch, IngestEffect) (IngestCommit, error)
}

type BrokerDelivery interface {
	Input() ConsumerInput
	Ack(context.Context) error
	Retry(context.Context, time.Duration) error
}

// Only the runner receives the delivery handle. It acknowledges after Apply
// returns committed success; effects never receive Ack. Ack failure redelivers.
type ConsumerRunner interface {
	Process(context.Context, BrokerDelivery, ApplyEffect) error
}

type LeasedOutbox struct {
	Intent    OutboxIntent
	Lease     PersistenceLeaseToken
	ExpiresAt time.Time
	Attempt   uint32
}

type PublishAck struct {
	Stream    string
	Sequence  uint64
	Duplicate bool
}

func (a PublishAck) Validate() error {
	if err := ValidatePersistenceCounter(a.Sequence); err != nil {
		return err
	}
	if a.Stream == "" || a.Sequence == 0 {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

// Claim persists fenced leases before network I/O and preserves partition
// order. MarkPublished requires the current lease, immutable hash and a broker
// persistence acknowledgement; a stale lease must fail its compare-and-swap.
type OutboxRelayStore interface {
	Claim(context.Context, uint32, time.Duration) ([]LeasedOutbox, error)
	MarkPublished(context.Context, PersistenceEventID, PersistenceLeaseToken, PersistenceHash, PublishAck) error
	ReleaseForRetry(context.Context, PersistenceEventID, PersistenceLeaseToken, time.Time, string) error
}

type PartitionWatermark struct {
	Partition PersistencePartition
	Sequence  uint64
}

func (w PartitionWatermark) Validate() error {
	if err := ValidatePersistenceCounter(w.Sequence); err != nil {
		return err
	}
	if w.Partition == "" {
		return ErrInvalidPersistenceRecord
	}
	return nil
}

type SnapshotWatermark struct {
	Generation  PersistenceGeneration
	BlockNumber uint64
	BlockHash   PersistenceHash
	Partitions  []PartitionWatermark
}

func (w SnapshotWatermark) Validate() error {
	if err := ValidatePersistenceCounter(w.BlockNumber); err != nil {
		return err
	}
	if w.Generation == "" {
		return ErrInvalidPersistenceRecord
	}
	seen := make(map[PersistencePartition]struct{}, len(w.Partitions))
	for _, partition := range w.Partitions {
		if err := partition.Validate(); err != nil {
			return err
		}
		if _, exists := seen[partition.Partition]; exists {
			return ErrInvalidPersistenceRecord
		}
		seen[partition.Partition] = struct{}{}
	}
	return nil
}

type SnapshotTransaction interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// SnapshotUnit reads business rows and the checkpoint vector from a single
// read-only repeatable-read transaction. It does not retain a transaction
// across pages. The watermark is from this same transaction, not a cache.
type SnapshotUnit interface {
	Read(context.Context, func(context.Context, SnapshotTransaction, SnapshotWatermark) error) error
}
