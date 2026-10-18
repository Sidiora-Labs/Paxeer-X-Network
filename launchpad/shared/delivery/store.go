package delivery

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

func SchemaDDL(schema string) (string, error) {
	if !token.MatchString(schema) {
		return "", errors.New("delivery: invalid schema")
	}
	return strings.ReplaceAll(schemaSQL, "{{schema}}", pgx.Identifier{schema}.Sanitize()), nil
}

type Store struct {
	pool       *pgxpool.Pool
	schema     string
	policy     Policy
	epochGuard func(context.Context, pgx.Tx) error
}

func NewStore(pool *pgxpool.Pool, schema string, policy Policy) (*Store, error) {
	if pool == nil || !token.MatchString(schema) || len(policy.Subjects) == 0 || len(policy.EventTypes) == 0 {
		return nil, errors.New("delivery: invalid store configuration")
	}
	// Copy configured maps so callers cannot change admission policy after creation.
	copyPolicy := Policy{Subjects: map[string]bool{}, EventTypes: map[string]bool{}}
	for k, v := range policy.Subjects {
		copyPolicy.Subjects[k] = v
	}
	for k, v := range policy.EventTypes {
		copyPolicy.EventTypes[k] = v
	}
	return &Store{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), policy: copyPolicy}, nil
}
func NewStoreWithEpochGuard(pool *pgxpool.Pool, schema string, policy Policy, guard func(context.Context, pgx.Tx) error) (*Store, error) {
	if guard == nil {
		return nil, errors.New("delivery: recovery epoch guard required")
	}
	s, err := NewStore(pool, schema, policy)
	if err != nil {
		return nil, err
	}
	s.epochGuard = guard
	return s, nil
}
func (s *Store) assertEpochTx(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return errors.New("delivery: transaction required")
	}
	if s.epochGuard == nil {
		return nil
	}
	return s.epochGuard(ctx, tx)
}
func (s *Store) mutate(ctx context.Context, effect func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	if err = effect(tx); err != nil {
		return err
	}
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) table(name string) string { return s.schema + "." + name }

const rollbackTimeout = time.Second

// Cleanup must survive cancellation of the operation but cannot wait forever.
// pgx closes a connection when rollback fails; pgxpool releases and discards that
// closed/unsynchronized connection rather than returning it to another borrower.
func rollbackBounded(tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

type Pending struct {
	Subject  string
	Envelope Envelope
}

// Produce owns the transaction boundary. Source rows and cursor updates belong
// in effect; any effect or outbox error rolls the entire transaction back.
// External network effects must not run in the callback.
func (s *Store) Produce(ctx context.Context, events []Pending, effect func(context.Context, pgx.Tx) error) error {
	if len(events) == 0 || len(events) > 1000 || effect == nil {
		return errors.New("delivery: invalid produce batch")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	if err = effect(ctx, tx); err != nil {
		return err
	}
	for _, e := range events {
		if err = s.Enqueue(ctx, tx, e.Subject, e.Envelope); err != nil {
			return err
		}
	}
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Enqueue participates in a service-owned transaction; it never commits alone.
func (s *Store) Enqueue(ctx context.Context, tx pgx.Tx, subject string, e Envelope) error {
	if tx == nil {
		return errors.New("delivery: transaction required")
	}
	if err := s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	wire, err := e.Encode(s.policy, subject)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_outbox")+"(event_id,schema_version,subject,payload_hash,envelope) VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_id) DO NOTHING", e.EventID, e.SchemaVersion, subject, e.PayloadHash, wire)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return s.assertEpochTx(ctx, tx)
	}
	var old []byte
	var oldSubject string
	if err = tx.QueryRow(ctx, "SELECT subject,envelope FROM "+s.table("delivery_outbox")+" WHERE event_id=$1", e.EventID).Scan(&oldSubject, &old); err != nil {
		return err
	}
	if oldSubject != subject || !bytes.Equal(old, wire) {
		return ErrIntegrity
	}
	return s.assertEpochTx(ctx, tx)
}

type Lease struct {
	EventID, Subject, Owner string
	Wire                    []byte
	Fence                   int64
	Attempts                int
}

func (s *Store) Claim(ctx context.Context, owner string, limit int, duration time.Duration) ([]Lease, error) {
	if !token.MatchString(owner) || limit < 1 || limit > 1000 || duration < time.Second || duration > 5*time.Minute {
		return nil, errors.New("delivery: invalid lease bounds")
	}
	out := []Lease{}
	err := s.mutate(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH picked AS (SELECT event_id FROM `+s.table("delivery_outbox")+` WHERE state='pending' AND available_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT $1)
 UPDATE `+s.table("delivery_outbox")+` o SET lease_expiry_count=o.lease_expiry_count+CASE WHEN o.lease_until<=clock_timestamp() THEN 1 ELSE 0 END,claimed_at=clock_timestamp(),lease_owner=$2,lease_until=clock_timestamp()+($3::bigint*interval '1 millisecond'),fence=o.fence+1,attempts=o.attempts+1 FROM picked WHERE o.event_id=picked.event_id RETURNING o.event_id,o.subject,o.envelope,o.fence,o.attempts`, limit, owner, duration.Milliseconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l := Lease{Owner: owner}
			if err = rows.Scan(&l.EventID, &l.Subject, &l.Wire, &l.Fence, &l.Attempts); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type PersistenceAck struct {
	Stream   string
	Sequence uint64
}

func (s *Store) MarkPublished(ctx context.Context, l Lease, a PersistenceAck) error {
	if !token.MatchString(a.Stream) || a.Sequence == 0 || a.Sequence > 1<<63-1 {
		return errors.New("delivery: persistence acknowledgement required")
	}
	return s.mutate(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE "+s.table("delivery_outbox")+" SET acknowledgement_latency_ms=GREATEST(0,EXTRACT(EPOCH FROM (clock_timestamp()-claimed_at))*1000)::bigint,state='published',broker_stream=$4,broker_sequence=$5,published_at=clock_timestamp(),lease_owner=NULL,lease_until=NULL,failure_class=NULL WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending'", l.EventID, l.Owner, l.Fence, a.Stream, int64(a.Sequence))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}

type FailureClass string

const (
	Transient         FailureClass = "transient"
	SchemaViolation   FailureClass = "schema_violation"
	IntegrityConflict FailureClass = "integrity_conflict"
	Authorization     FailureClass = "authorization"
)

func (s *Store) Retry(ctx context.Context, l Lease, class FailureClass, maxAttempts int, delay time.Duration) error {
	if maxAttempts < 1 || maxAttempts > 100 || delay < time.Second || delay > time.Hour {
		return errors.New("delivery: invalid retry bounds")
	}
	switch class {
	case Transient, SchemaViolation, IntegrityConflict, Authorization:
	default:
		return errors.New("delivery: invalid failure class")
	}
	// Exhaustion is based on the row, never the caller's mutable lease snapshot.
	return s.mutate(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE "+s.table("delivery_outbox")+" SET state=CASE WHEN attempts >= $4 OR $5 <> 'transient' THEN 'quarantined' ELSE 'pending' END,failure_class=$5,available_at=clock_timestamp()+($6::bigint*interval '1 millisecond'),lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending'", l.EventID, l.Owner, l.Fence, maxAttempts, string(class), delay.Milliseconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}

// CheckAttemptBudget fences the claim against durable attempts before network
// publication. Claim increments the attempt counter; maxAttempts claims may
// publish. A reclaim after that budget preserves the original row in quarantine.
func (s *Store) CheckAttemptBudget(ctx context.Context, l Lease, maxAttempts int) (bool, error) {
	if maxAttempts < 1 || maxAttempts > 100 {
		return false, errors.New("delivery: invalid retry bound")
	}
	var exhausted bool
	err := s.mutate(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "UPDATE "+s.table("delivery_outbox")+" SET state=CASE WHEN attempts>$4 THEN 'quarantined' ELSE state END,failure_class=CASE WHEN attempts>$4 THEN 'transient' ELSE failure_class END,lease_owner=CASE WHEN attempts>$4 THEN NULL ELSE lease_owner END,lease_until=CASE WHEN attempts>$4 THEN NULL ELSE lease_until END WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending' RETURNING attempts>$4", l.EventID, l.Owner, l.Fence, maxAttempts).Scan(&exhausted)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLease
		}
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return !exhausted, nil
}

// Delivery Ack must synchronously acknowledge the durable broker delivery.
type Delivery interface{ Ack(context.Context) error }

func (s *Store) Consume(ctx context.Context, consumer, generation, subject string, wire []byte, d Delivery, effect func(context.Context, pgx.Tx, Envelope) error) error {
	if !token.MatchString(consumer) || !token.MatchString(generation) || d == nil || effect == nil {
		return errors.New("delivery: invalid consumer configuration")
	}
	wire = append([]byte(nil), wire...)
	e, err := Decode(wire, s.policy, subject)
	if err != nil {
		class := SchemaViolation
		if errors.Is(err, ErrIntegrity) {
			class = IntegrityConflict
		}
		if errors.Is(err, ErrAuthorization) {
			class = Authorization
		}
		if qerr := s.quarantineConsumer(ctx, consumer, generation, subject, wire, class); qerr != nil {
			return errors.Join(err, qerr)
		}
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_inbox")+"(consumer_id,projection_generation,event_id,envelope_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", consumer, generation, e.EventID, Hash(wire))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		if err = effect(ctx, tx, e); err != nil {
			return err
		}
	} else {
		var existing string
		if err = tx.QueryRow(ctx, "SELECT envelope_hash FROM "+s.table("delivery_inbox")+" WHERE consumer_id=$1 AND projection_generation=$2 AND event_id=$3", consumer, generation, e.EventID).Scan(&existing); err != nil {
			return err
		}
		if existing != Hash(wire) {
			rollbackBounded(tx)
			if qerr := s.quarantineConsumer(ctx, consumer, generation, subject, wire, IntegrityConflict); qerr != nil {
				return errors.Join(ErrIntegrity, qerr)
			}
			return ErrIntegrity
		}
	}
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Even duplicates are acknowledged only after verifying committed inbox identity.
	return d.Ack(ctx)
}

func (s *Store) quarantineConsumer(ctx context.Context, consumer, generation, subject string, wire []byte, class FailureClass) error {
	if len(wire) > MaxEnvelopeBytes || len(subject) > 256 || !subjectToken.MatchString(subject) {
		return errors.New("delivery: bounded quarantine evidence required")
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.mutate(cleanup, func(tx pgx.Tx) error {
		_, err := tx.Exec(cleanup, "INSERT INTO "+s.table("delivery_quarantine")+"(consumer_id,projection_generation,envelope_hash,subject,envelope,failure_class) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", consumer, generation, Hash(wire), subject, wire, string(class))
		if err != nil {
			return err
		}
		var recordedSubject, recordedClass string
		var recordedWire []byte
		if err = tx.QueryRow(cleanup, "SELECT subject,envelope,failure_class FROM "+s.table("delivery_quarantine")+" WHERE consumer_id=$1 AND projection_generation=$2 AND envelope_hash=$3", consumer, generation, Hash(wire)).Scan(&recordedSubject, &recordedWire, &recordedClass); err != nil {
			return err
		}
		if recordedSubject != subject || recordedClass != string(class) || !bytes.Equal(recordedWire, wire) {
			return ErrIntegrity
		}
		return nil
	})
}

// Stats returns bounded aggregate diagnostics; event IDs and payloads never
// become metric labels. Quarantined rows remain included in unpublished runway.
type Stats struct {
	PendingRows                 int64
	PendingBytes                int64
	QuarantinedRows             int64
	OldestUnpublishedAgeSeconds float64
	LeaseExpiryCount            int64
	MaxAcknowledgementLatencyMS int64
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='pending'),
 COALESCE(sum(octet_length(envelope)) FILTER(WHERE state<>'published'),0)::bigint,
 count(*) FILTER(WHERE state='quarantined'),
 COALESCE(EXTRACT(EPOCH FROM (clock_timestamp()-min(created_at) FILTER(WHERE state<>'published'))),0)::double precision,
 COALESCE(sum(lease_expiry_count),0)::bigint,
 COALESCE(max(acknowledgement_latency_ms),0)::bigint FROM `+s.table("delivery_outbox")).Scan(&out.PendingRows, &out.PendingBytes, &out.QuarantinedRows, &out.OldestUnpublishedAgeSeconds, &out.LeaseExpiryCount, &out.MaxAcknowledgementLatencyMS)
	return out, err
}

type ConsumeHooks struct {
	Guard   func(context.Context, pgx.Tx, Envelope) (GuardedPosition, error)
	Effect  func(context.Context, pgx.Tx, Envelope, GuardedPosition) error
	Advance func(context.Context, pgx.Tx, Envelope, GuardedPosition) error
	Freeze  func(context.Context, pgx.Tx, []string) error
}

const (
	InnerSchema      FailureClass = "inner_schema"
	Authority        FailureClass = "authority"
	IdentityConflict FailureClass = "identity_conflict"
	PartitionBinding FailureClass = "partition_binding"
)

type IntegrityFailure struct {
	Class          FailureClass
	AffectedPools  []string
	EvidenceDigest accounting.Digest
	Cause          error
}

func (e *IntegrityFailure) Error() string {
	return "delivery: rejected ordered publication: " + string(e.Class)
}
func (e *IntegrityFailure) Unwrap() error { return e.Cause }

type QuarantineInput struct {
	ConsumerID, Generation, Subject, PublicationID string
	Wire                                           []byte
	WireSHA256                                     accounting.Digest
	Class                                          FailureClass
	AffectedPools                                  []string
	EvidenceDigest                                 accounting.Digest
	Lease                                          *PartitionLease
}

func poisonClass(c FailureClass) bool {
	switch c {
	case InnerSchema, Authority, IdentityConflict, PartitionBinding:
		return true
	}
	return false
}
func retryWholeTransaction(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && (e.Code == "40001" || e.Code == "40P01")
}
func (s *Store) ConsumeOrdered(ctx context.Context, consumer, generation, subject string, wire []byte, d Delivery, h ConsumeHooks) error {
	if !token.MatchString(consumer) || !token.MatchString(generation) || d == nil || h.Guard == nil || h.Effect == nil || h.Advance == nil {
		return errors.New("delivery: ordered hooks required")
	}
	wire = append([]byte(nil), wire...)
	e, err := Decode(wire, s.policy, subject)
	if err != nil {
		if len(wire) <= MaxEnvelopeBytes && len(subject) <= 256 && subjectToken.MatchString(subject) {
			class := InnerSchema
			if errors.Is(err, ErrIntegrity) {
				class = IdentityConflict
			}
			if errors.Is(err, ErrAuthorization) {
				class = Authority
			}
			publicationID := e.EventID
			if !token.MatchString(publicationID) {
				publicationID = ""
			}
			qerr := s.QuarantineAfterRollback(ctx, QuarantineInput{ConsumerID: consumer, Generation: generation, Subject: subject, PublicationID: publicationID, Wire: wire, WireSHA256: accounting.Digest(Hash(wire)), Class: class, EvidenceDigest: accounting.Digest(Hash(wire))}, nil)
			if qerr != nil {
				return errors.Join(err, qerr)
			}
		}
		return err
	}
	partitions := &PartitionStore{pool: s.pool, schema: s.schema}
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		var g GuardedPosition
		tx, beginErr := s.pool.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		err = func() error {
			if epochErr := s.assertEpochTx(ctx, tx); epochErr != nil {
				return epochErr
			}
			publication, decodeErr := accounting.DecodePublicationV1(e.Payload)
			if decodeErr != nil {
				return &IntegrityFailure{Class: InnerSchema, Cause: decodeErr}
			}
			var guardErr error
			g, guardErr = h.Guard(ctx, tx, e)
			if guardErr != nil {
				return guardErr
			}
			if g.Lease.Key.ConsumerID != consumer || g.Lease.Key.ProjectionGeneration != generation {
				return &IntegrityFailure{Class: PartitionBinding, Cause: ErrIntegrity}
			}
			pools := []string{}
			if publication.Fill != nil {
				pools = append(pools, publication.Fill.PoolAddress)
			}
			bindingError := func() error {
				return &IntegrityFailure{Class: PartitionBinding, AffectedPools: pools, EvidenceDigest: accounting.Digest(Hash(e.Payload)), Cause: ErrIntegrity}
			}
			if publication.PublicationID != e.EventID || subject != "accounting."+publication.Facet+".v1" {
				return bindingError()
			}
			matched := false
			for _, p := range publication.Positions {
				if reflect.DeepEqual(p, g.Position) {
					matched = true
					break
				}
			}
			if !matched || g.Position.SourceOrder.ChainID != "125" || g.Position.SourceOrder.Height != e.SourceOrder.Height || g.Position.SourceOrder.TransactionIndex != e.SourceOrder.TransactionIndex || g.Position.SourceOrder.LogIndex != e.SourceOrder.LogIndex {
				return bindingError()
			}
			checked, checkErr := partitions.CheckAndLockTx(ctx, tx, g.Lease, g.Position)
			if checkErr != nil {
				return checkErr
			}
			if checked.DuplicateCommitted != g.DuplicateCommitted {
				return bindingError()
			}
			tag, insertErr := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_inbox")+"(consumer_id,projection_generation,event_id,envelope_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", consumer, generation, e.EventID, Hash(wire))
			if insertErr != nil {
				return insertErr
			}
			if tag.RowsAffected() == 1 {
				if g.DuplicateCommitted {
					return bindingError()
				}
				if effectErr := h.Effect(ctx, tx, e, g); effectErr != nil {
					return effectErr
				}
			} else {
				var existing string
				if readErr := tx.QueryRow(ctx, "SELECT envelope_hash FROM "+s.table("delivery_inbox")+" WHERE consumer_id=$1 AND projection_generation=$2 AND event_id=$3", consumer, generation, e.EventID).Scan(&existing); readErr != nil {
					return readErr
				}
				if existing != Hash(wire) || !g.DuplicateCommitted {
					return bindingError()
				}
			}
			if advanceErr := h.Advance(ctx, tx, e, g); advanceErr != nil {
				return advanceErr
			}
			if epochErr := s.assertEpochTx(ctx, tx); epochErr != nil {
				return epochErr
			}
			if fenceErr := partitions.verifyCommitTx(ctx, tx, g); fenceErr != nil {
				return fenceErr
			}
			return tx.Commit(ctx)
		}()
		rollbackBounded(tx)
		if err == nil {
			return d.Ack(ctx)
		}
		var integrity *IntegrityFailure
		if errors.As(err, &integrity) {
			if !poisonClass(integrity.Class) {
				return err
			}
			evidence := integrity.EvidenceDigest
			if evidence == "" {
				evidence = accounting.Digest(Hash(wire))
			}
			in := QuarantineInput{ConsumerID: consumer, Generation: generation, Subject: subject, PublicationID: e.EventID, Wire: wire, WireSHA256: accounting.Digest(Hash(wire)), Class: integrity.Class, AffectedPools: append([]string(nil), integrity.AffectedPools...), EvidenceDigest: evidence}
			if validConsumerKey(g.Lease.Key) {
				lease := g.Lease
				in.Lease = &lease
			}
			if qerr := s.QuarantineAfterRollback(ctx, in, h.Freeze); qerr != nil {
				return errors.Join(err, qerr)
			}
			return err
		}
		if !retryWholeTransaction(err) || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
func (s *Store) QuarantineAfterRollback(ctx context.Context, in QuarantineInput, freeze func(context.Context, pgx.Tx, []string) error) error {
	if !token.MatchString(in.ConsumerID) || !token.MatchString(in.Generation) || len(in.Subject) > 256 || !subjectToken.MatchString(in.Subject) || len(in.Wire) > MaxEnvelopeBytes || string(in.WireSHA256) != Hash(in.Wire) || !partitionDigest.MatchString(string(in.EvidenceDigest)) || !poisonClass(in.Class) || len(in.PublicationID) > 128 || len(in.AffectedPools) > 128 {
		return errors.New("delivery: invalid quarantine evidence")
	}
	pools := append([]string(nil), in.AffectedPools...)
	sort.Strings(pools)
	unique := pools[:0]
	for _, pool := range pools {
		if !partitionAddress.MatchString(pool) {
			return errors.New("delivery: invalid affected pool")
		}
		if len(unique) == 0 || unique[len(unique)-1] != pool {
			unique = append(unique, pool)
		}
	}
	if in.Class == IdentityConflict && len(unique) > 0 && freeze == nil {
		return errors.New("delivery: identity conflict freeze required")
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(cleanup)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	if err = s.assertEpochTx(cleanup, tx); err != nil {
		return err
	}
	args := []any{in.ConsumerID, in.Generation}
	predicate := "consumer_id=$1 AND projection_generation=$2"
	if in.Lease != nil {
		if !validConsumerKey(in.Lease.Key) || in.Lease.Key.ConsumerID != in.ConsumerID || in.Lease.Key.ProjectionGeneration != in.Generation {
			return ErrLease
		}
		args = append(args, in.Lease.Key.PartitionKind, in.Lease.Key.PartitionKey)
		predicate = consumerPredicate
	}
	rows, err := tx.Query(cleanup, "SELECT partition_kind,partition_key FROM "+s.table("delivery_partition_leases")+" WHERE "+predicate+" ORDER BY partition_kind,partition_key LIMIT 1001 FOR UPDATE", args...)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var kind, key string
		if err = rows.Scan(&kind, &key); err != nil {
			rows.Close()
			return err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count > 1000 {
		return errors.New("delivery: quarantine lock scope exceeded")
	}
	rows, err = tx.Query(cleanup, "SELECT partition_kind,partition_key FROM "+s.table("delivery_partition_offsets")+" WHERE "+predicate+" ORDER BY partition_kind,partition_key LIMIT 1001 FOR UPDATE", args...)
	if err != nil {
		return err
	}
	count = 0
	for rows.Next() {
		var kind, key string
		if err = rows.Scan(&kind, &key); err != nil {
			rows.Close()
			return err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count > 1000 {
		return errors.New("delivery: quarantine lock scope exceeded")
	}
	if _, err = tx.Exec(cleanup, "INSERT INTO "+s.table("delivery_ordered_quarantine")+"(consumer_id,projection_generation,envelope_hash,failure_class,subject,publication_id,envelope,affected_pools,evidence_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING", in.ConsumerID, in.Generation, string(in.WireSHA256), string(in.Class), in.Subject, in.PublicationID, in.Wire, unique, string(in.EvidenceDigest)); err != nil {
		return err
	}
	var recordedSubject, recordedPublication, recordedEvidence string
	var recordedWire []byte
	var recordedPools []string
	if err = tx.QueryRow(cleanup, "SELECT subject,publication_id,envelope,affected_pools,evidence_digest FROM "+s.table("delivery_ordered_quarantine")+" WHERE consumer_id=$1 AND projection_generation=$2 AND envelope_hash=$3 AND failure_class=$4 FOR UPDATE", in.ConsumerID, in.Generation, string(in.WireSHA256), string(in.Class)).Scan(&recordedSubject, &recordedPublication, &recordedWire, &recordedPools, &recordedEvidence); err != nil {
		return err
	}
	if recordedSubject != in.Subject || recordedPublication != in.PublicationID || !bytes.Equal(recordedWire, in.Wire) || recordedEvidence != string(in.EvidenceDigest) || !reflect.DeepEqual(recordedPools, unique) {
		return ErrIntegrity
	}
	if freeze != nil && len(unique) > 0 {
		if err = freeze(cleanup, tx, unique); err != nil {
			return err
		}
	}
	if err = s.assertEpochTx(cleanup, tx); err != nil {
		return err
	}
	return tx.Commit(cleanup)
}

func (s *Store) OriginalEnvelopeTx(ctx context.Context, tx pgx.Tx, eventID string) (string, Envelope, error) {
	if tx == nil || !token.MatchString(eventID) {
		return "", Envelope{}, ErrSchema
	}
	var wire []byte
	var subject string
	if err := tx.QueryRow(ctx, "SELECT subject,envelope FROM "+s.table("delivery_outbox")+" WHERE event_id=$1", eventID).Scan(&subject, &wire); err != nil {
		return "", Envelope{}, err
	}
	envelope, err := Decode(wire, s.policy, subject)
	if err != nil {
		return "", Envelope{}, err
	}
	if envelope.EventID != eventID {
		return "", Envelope{}, ErrIntegrity
	}
	return subject, envelope, nil
}

type BrokerPosition struct {
	Stream                       string
	Sequence                     uint64
	Subject, EventID, WireSHA256 string
}
type BrokerDelivery interface {
	Delivery
	BrokerPosition() (BrokerPosition, error)
}

func (s *Store) ConsumeOrderedWithWatermark(ctx context.Context, consumer, generation, subject string, wire []byte, d BrokerDelivery, h ConsumeHooks) error {
	if d == nil || h.Advance == nil {
		return errors.New("delivery: actual broker metadata and ordered advance required")
	}
	broker, err := d.BrokerPosition()
	if err != nil {
		return err
	}
	if broker.Subject != subject || broker.WireSHA256 != Hash(wire) || !token.MatchString(broker.Stream) || broker.Sequence == 0 || broker.Sequence > 1<<63-1 {
		return ErrIntegrity
	}
	advance := h.Advance
	h.Advance = func(ctx context.Context, tx pgx.Tx, e Envelope, g GuardedPosition) error {
		if e.EventID != broker.EventID {
			return ErrIntegrity
		}
		if err := advance(ctx, tx, e, g); err != nil {
			return err
		}
		if g.DuplicateCommitted {
			return nil
		}
		sequence, err := strconv.ParseInt(g.Position.Sequence, 10, 64)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(e.SourceOrder)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_consumer_watermarks")+`(consumer_id,generation,partition_kind,partition_key,broker_stream,broker_sequence,source_order,position_sequence,event_id,envelope_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(consumer_id,generation,partition_kind,partition_key) DO UPDATE SET broker_sequence=EXCLUDED.broker_sequence,source_order=EXCLUDED.source_order,position_sequence=EXCLUDED.position_sequence,event_id=EXCLUDED.event_id,envelope_hash=EXCLUDED.envelope_hash,committed_at=clock_timestamp() WHERE delivery_consumer_watermarks.broker_stream=EXCLUDED.broker_stream AND delivery_consumer_watermarks.position_sequence=EXCLUDED.position_sequence-1 AND delivery_consumer_watermarks.broker_sequence<EXCLUDED.broker_sequence`, consumer, generation, g.Position.PartitionKind, g.Position.PartitionKey, broker.Stream, int64(broker.Sequence), raw, sequence, e.EventID, Hash(wire))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrIntegrity
		}
		return nil
	}
	return s.ConsumeOrdered(ctx, consumer, generation, subject, wire, d, h)
}

type AdmissionPolicy struct {
	DatabaseCapacityBytes, DatabaseReserveBytes, MaxBacklogBytes, MaxBatchBytes int64
	MaxConcurrentBatches                                                        int64
}
type AdmissionSnapshot struct{ DatabaseBytes, BacklogBytes, ReservedConcurrentBytes int64 }

func (s *Store) CheckIngestAdmissionTx(ctx context.Context, tx pgx.Tx, policy AdmissionPolicy, batchBytes int64) (AdmissionSnapshot, error) {
	var out AdmissionSnapshot
	if tx == nil || policy.DatabaseCapacityBytes <= 0 || policy.DatabaseReserveBytes <= 0 || policy.DatabaseReserveBytes >= policy.DatabaseCapacityBytes || policy.MaxBacklogBytes <= 0 || policy.MaxBatchBytes < 1 || policy.MaxConcurrentBatches < 1 || policy.MaxConcurrentBatches > 64 || batchBytes < 0 || batchBytes > policy.MaxBatchBytes || policy.MaxBatchBytes > policy.MaxBacklogBytes/policy.MaxConcurrentBatches {
		return out, errors.New("delivery: bounded ingestion admission configuration required")
	}
	if err := s.assertEpochTx(ctx, tx); err != nil {
		return out, err
	}
	out.ReservedConcurrentBytes = policy.MaxBatchBytes * policy.MaxConcurrentBatches
	err := tx.QueryRow(ctx, "SELECT pg_database_size(current_database()),COALESCE(sum(octet_length(envelope)) FILTER(WHERE state<>'published'),0)::bigint FROM "+s.table("delivery_outbox")).Scan(&out.DatabaseBytes, &out.BacklogBytes)
	if err != nil {
		return out, err
	}
	if out.DatabaseBytes > policy.DatabaseCapacityBytes-policy.DatabaseReserveBytes-out.ReservedConcurrentBytes || out.BacklogBytes > policy.MaxBacklogBytes-out.ReservedConcurrentBytes {
		return out, errors.New("delivery: ingestion paused at reserved database or outbox capacity")
	}
	return out, nil
}
