package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

type Store struct {
	db     *sql.DB
	schema string
	policy Policy
}

func NewStore(db *sql.DB, schema string, policy Policy) (*Store, error) {
	if db == nil || !token.MatchString(schema) || len(policy.Subjects) == 0 || len(policy.EventTypes) == 0 {
		return nil, errors.New("delivery: invalid store configuration")
	}
	// Copy configured maps so callers cannot change admission policy after creation.
	copyPolicy := Policy{Subjects: map[string]bool{}, Patterns: append([]string(nil), policy.Patterns...), EventTypes: map[string]bool{}}
	for k, v := range policy.Subjects {
		copyPolicy.Subjects[k] = v
	}
	for k, v := range policy.EventTypes {
		copyPolicy.EventTypes[k] = v
	}
	return &Store{db: db, schema: pq.QuoteIdentifier(schema), policy: copyPolicy}, nil
}

func (s *Store) mutate(ctx context.Context, effect func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = effect(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) table(name string) string { return s.schema + "." + name }

// database/sql rolls a transaction back when its context is cancelled, and a
// rollback after commit returns sql.ErrTxDone, so the error carries nothing.
func rollback(tx *sql.Tx) { _ = tx.Rollback() }

// EnqueueTx enqueues e as Store.Enqueue does, for a writer that holds only
// the transaction: the block write of the indexer.
func EnqueueTx(ctx context.Context, tx *sql.Tx, schema string, policy Policy, subject string, e Envelope) (int64, error) {
	if !token.MatchString(schema) {
		return 0, errors.New("delivery: invalid store configuration")
	}
	return (&Store{schema: pq.QuoteIdentifier(schema), policy: policy}).Enqueue(ctx, tx, subject, e)
}

// Enqueue participates in a service-owned transaction; it never commits alone.
func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, subject string, e Envelope) (int64, error) {
	if tx == nil {
		return 0, errors.New("delivery: transaction required")
	}
	wire, err := e.Encode(s.policy, subject)
	if err != nil {
		return 0, err
	}
	var sequence int64
	err = tx.QueryRowContext(ctx, "INSERT INTO "+s.table("delivery_outbox")+"(event_id,schema_version,subject,payload_hash,envelope) VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_id) DO NOTHING RETURNING perps_seq", e.EventID, int64(e.SchemaVersion), subject, e.PayloadHash, wire).Scan(&sequence)
	if err == nil {
		return sequence, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var old []byte
	var oldSubject string
	if err = tx.QueryRowContext(ctx, "SELECT perps_seq,subject,envelope FROM "+s.table("delivery_outbox")+" WHERE event_id=$1", e.EventID).Scan(&sequence, &oldSubject, &old); err != nil {
		return 0, err
	}
	if oldSubject != subject || !bytes.Equal(old, wire) {
		return 0, ErrIntegrity
	}
	return sequence, nil
}

type Lease struct {
	EventID, Subject, Owner string
	Wire                    []byte
	PerpsSeq                int64
	Fence                   int64
	Attempts                int
}

func (s *Store) Claim(ctx context.Context, owner string, limit int, duration time.Duration) ([]Lease, error) {
	if !token.MatchString(owner) || limit < 1 || limit > 1000 || duration < time.Second || duration > 5*time.Minute {
		return nil, errors.New("delivery: invalid lease bounds")
	}
	out := []Lease{}
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `WITH picked AS (SELECT event_id FROM `+s.table("delivery_outbox")+` WHERE state='pending' AND available_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY perps_seq FOR UPDATE SKIP LOCKED LIMIT $1),
 claimed AS (UPDATE `+s.table("delivery_outbox")+` o SET lease_expiry_count=o.lease_expiry_count+CASE WHEN o.lease_until<=clock_timestamp() THEN 1 ELSE 0 END,claimed_at=clock_timestamp(),lease_owner=$2,lease_until=clock_timestamp()+($3::bigint*interval '1 millisecond'),fence=o.fence+1,attempts=o.attempts+1 FROM picked WHERE o.event_id=picked.event_id RETURNING o.event_id,o.subject,o.envelope,o.perps_seq,o.fence,o.attempts)
 SELECT event_id,subject,envelope,perps_seq,fence,attempts FROM claimed ORDER BY perps_seq`, limit, owner, duration.Milliseconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l := Lease{Owner: owner}
			if err = rows.Scan(&l.EventID, &l.Subject, &l.Wire, &l.PerpsSeq, &l.Fence, &l.Attempts); err != nil {
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
	return s.mutate(ctx, func(tx *sql.Tx) error {
		return fenced(tx.ExecContext(ctx, "UPDATE "+s.table("delivery_outbox")+" SET acknowledgement_latency_ms=GREATEST(0,EXTRACT(EPOCH FROM (clock_timestamp()-claimed_at))*1000)::bigint,state='published',broker_stream=$4,broker_sequence=$5,published_at=clock_timestamp(),lease_owner=NULL,lease_until=NULL,failure_class=NULL WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending'", l.EventID, l.Owner, l.Fence, a.Stream, int64(a.Sequence)))
	})
}

// fenced reports ErrLease unless the update matched exactly the leased row.
func fenced(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLease
	}
	return nil
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
	return s.mutate(ctx, func(tx *sql.Tx) error {
		return fenced(tx.ExecContext(ctx, "UPDATE "+s.table("delivery_outbox")+" SET state=CASE WHEN attempts >= $4::integer OR $5::text <> 'transient' THEN 'quarantined' ELSE 'pending' END,failure_class=$5::text,available_at=clock_timestamp()+($6::bigint*interval '1 millisecond'),lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending'", l.EventID, l.Owner, l.Fence, maxAttempts, string(class), delay.Milliseconds()))
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
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "UPDATE "+s.table("delivery_outbox")+" SET state=CASE WHEN attempts>$4::integer THEN 'quarantined' ELSE state END,failure_class=CASE WHEN attempts>$4::integer THEN 'transient' ELSE failure_class END,lease_owner=CASE WHEN attempts>$4::integer THEN NULL ELSE lease_owner END,lease_until=CASE WHEN attempts>$4::integer THEN NULL ELSE lease_until END WHERE event_id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() AND state='pending' RETURNING attempts>$4::integer", l.EventID, l.Owner, l.Fence, maxAttempts).Scan(&exhausted)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLease
		}
		return err
	})
	if err != nil {
		return false, err
	}
	return !exhausted, nil
}

// Delivery Ack must synchronously acknowledge the durable broker delivery.
type Delivery interface{ Ack(context.Context) error }

func (s *Store) Consume(ctx context.Context, consumer, generation, subject string, wire []byte, d Delivery, effect func(context.Context, *sql.Tx, Envelope) error) error {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	res, err := tx.ExecContext(ctx, "INSERT INTO "+s.table("delivery_inbox")+"(consumer_id,projection_generation,event_id,envelope_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", consumer, generation, e.EventID, Hash(wire))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		if err = effect(ctx, tx, e); err != nil {
			return err
		}
	} else {
		var existing string
		if err = tx.QueryRowContext(ctx, "SELECT envelope_hash FROM "+s.table("delivery_inbox")+" WHERE consumer_id=$1 AND projection_generation=$2 AND event_id=$3", consumer, generation, e.EventID).Scan(&existing); err != nil {
			return err
		}
		if existing != Hash(wire) {
			rollback(tx)
			if qerr := s.quarantineConsumer(ctx, consumer, generation, subject, wire, IntegrityConflict); qerr != nil {
				return errors.Join(ErrIntegrity, qerr)
			}
			return ErrIntegrity
		}
	}
	if err = tx.Commit(); err != nil {
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
	return s.mutate(cleanup, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(cleanup, "INSERT INTO "+s.table("delivery_quarantine")+"(consumer_id,projection_generation,envelope_hash,subject,envelope,failure_class) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", consumer, generation, Hash(wire), subject, wire, string(class))
		if err != nil {
			return err
		}
		var recordedSubject, recordedClass string
		var recordedWire []byte
		if err = tx.QueryRowContext(cleanup, "SELECT subject,envelope,failure_class FROM "+s.table("delivery_quarantine")+" WHERE consumer_id=$1 AND projection_generation=$2 AND envelope_hash=$3", consumer, generation, Hash(wire)).Scan(&recordedSubject, &recordedWire, &recordedClass); err != nil {
			return err
		}
		if recordedSubject != subject || recordedClass != string(class) || !bytes.Equal(recordedWire, wire) {
			return ErrIntegrity
		}
		return nil
	})
}
