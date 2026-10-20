package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
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
	// Control marks the lease of a permanently quarantined row: the relay
	// publishes its control message (ControlMessage) in place of the envelope.
	Control bool
}

// Control message headers: a permanently quarantined row is announced in its
// Perps-Seq place by a message on perps.<chain>.control carrying
// Perps-Control: skipped and the Perps-Seq of the skipped row.
const (
	HeaderControl  = "Perps-Control"
	ControlSkipped = "skipped"
)

// ControlMessage returns the subject, Nats-Msg-Id and body of the control
// message announcing that the row of l was skipped.
func ControlMessage(l Lease) (subject, msgID string, body []byte, err error) {
	tokens := strings.SplitN(l.Subject, ".", 3)
	if len(tokens) != 3 || tokens[0] != "perps" || !decimal.MatchString(tokens[1]) || l.PerpsSeq < 1 || !token.MatchString(l.EventID) {
		return "", "", nil, ErrIntegrity
	}
	seq := strconv.FormatInt(l.PerpsSeq, 10)
	return "perps." + tokens[1] + ".control", "control-" + seq, []byte(`{"perps_seq":"` + seq + `","event_id":"` + l.EventID + `"}`), nil
}

// openRow selects, as alias o, the rows still to be put on the stream: a
// pending row, and a permanently quarantined row whose control message has no
// broker position yet. The broker position of a quarantined row is that of
// its control message.
const openRow = "(o.state='pending' OR (o.state='quarantined' AND o.failure_class<>'transient' AND o.broker_sequence IS NULL))"

// MaxRetryDelay caps the exponential backoff of a transient failure unless
// the base retry delay is longer; a transient row quarantined by its attempt
// budget waits as long before Requeue returns it.
const MaxRetryDelay = time.Minute

// Claim leases up to limit pending rows in Perps-Seq order. It stops before
// the first pending row that is backing off or leased elsewhere, so no row is
// claimed ahead of an earlier pending one. Claim leaves quarantined rows
// alone; the relay claims through claim, which also stops at them.
func (s *Store) Claim(ctx context.Context, owner string, limit int, duration time.Duration) ([]Lease, error) {
	return s.claim(ctx, owner, limit, duration, false)
}

// claim is Claim; with relay it also leases the open permanently quarantined
// rows (openRow) as control leases and stops before every row quarantined as
// transient, so no row is claimed ahead of an earlier one that is still to be
// published or announced. Claims are serialised by a transaction advisory
// lock, so a claim sees every lease of an earlier one and replicas never
// lease two batches at once.
func (s *Store) claim(ctx context.Context, owner string, limit int, duration time.Duration, relay bool) ([]Lease, error) {
	if !token.MatchString(owner) || limit < 1 || limit > 1000 || duration < time.Second || duration > 5*time.Minute {
		return nil, errors.New("delivery: invalid lease bounds")
	}
	open := "o.state='pending'"
	if relay {
		open = openRow
	}
	out := []Lease{}
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", s.table("delivery_outbox")); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `WITH barrier AS (SELECT COALESCE(min(perps_seq),9223372036854775807) AS seq FROM `+s.table("delivery_outbox")+` o WHERE (`+open+` AND (o.available_at>clock_timestamp() OR o.lease_until>clock_timestamp())) OR ($4::boolean AND o.state='quarantined' AND o.failure_class='transient')),
 picked AS (SELECT event_id FROM `+s.table("delivery_outbox")+` o WHERE `+open+` AND o.available_at<=clock_timestamp() AND (o.lease_until IS NULL OR o.lease_until<=clock_timestamp()) AND o.perps_seq<(SELECT seq FROM barrier) ORDER BY o.perps_seq FOR UPDATE LIMIT $1),
 claimed AS (UPDATE `+s.table("delivery_outbox")+` o SET lease_expiry_count=o.lease_expiry_count+CASE WHEN o.lease_until<=clock_timestamp() THEN 1 ELSE 0 END,claimed_at=clock_timestamp(),lease_owner=$2,lease_until=clock_timestamp()+($3::bigint*interval '1 millisecond'),fence=o.fence+1,attempts=o.attempts+1 FROM picked WHERE o.event_id=picked.event_id RETURNING o.event_id,o.subject,o.envelope,o.perps_seq,o.fence,o.attempts,o.state='quarantined' AS control)
 SELECT event_id,subject,envelope,perps_seq,fence,attempts,control FROM claimed ORDER BY perps_seq`, limit, owner, duration.Milliseconds(), relay)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l := Lease{Owner: owner}
			if err = rows.Scan(&l.EventID, &l.Subject, &l.Wire, &l.PerpsSeq, &l.Fence, &l.Attempts, &l.Control); err != nil {
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
	return s.MarkPublishedBatch(ctx, []Lease{l}, []PersistenceAck{a})
}

// MarkPublishedBatch records acks[i] as the broker position of leases[i] in one
// transaction. Every row still fenced by its lease is recorded; ErrLease
// reports that at least one lease no longer matched its row. A pending row
// becomes published; a permanently quarantined row stays quarantined with the
// broker position of its control message, which closes it.
func (s *Store) MarkPublishedBatch(ctx context.Context, leases []Lease, acks []PersistenceAck) error {
	if len(leases) != len(acks) {
		return errors.New("delivery: one acknowledgement per lease required")
	}
	ids, owners, fences := leaseKeys(leases)
	streams, sequences := make([]string, len(acks)), make([]int64, len(acks))
	for i, a := range acks {
		if !token.MatchString(a.Stream) || a.Sequence == 0 || a.Sequence > 1<<63-1 {
			return errors.New("delivery: persistence acknowledgement required")
		}
		streams[i], sequences[i] = a.Stream, int64(a.Sequence)
	}
	return s.fencedUpdate(ctx, len(leases), "UPDATE "+s.table("delivery_outbox")+" o SET acknowledgement_latency_ms=GREATEST(0,EXTRACT(EPOCH FROM (clock_timestamp()-o.claimed_at))*1000)::bigint,state=CASE WHEN o.state='pending' THEN 'published' ELSE o.state END,broker_stream=l.stream,broker_sequence=l.seq,published_at=CASE WHEN o.state='pending' THEN clock_timestamp() END,lease_owner=NULL,lease_until=NULL,failure_class=CASE WHEN o.state='pending' THEN NULL ELSE o.failure_class END FROM unnest($1::text[],$2::text[],$3::bigint[],$4::text[],$5::bigint[]) AS l(event_id,owner,fence,stream,seq) WHERE o.event_id=l.event_id AND o.lease_owner=l.owner AND o.fence=l.fence AND o.lease_until>clock_timestamp() AND "+openRow, ids, owners, fences, pq.Array(streams), pq.Array(sequences))
}

// leaseKeys returns the event ids, owners and fences of leases as arrays.
func leaseKeys(leases []Lease) (ids, owners, fences any) {
	i, o, f := make([]string, len(leases)), make([]string, len(leases)), make([]int64, len(leases))
	for n, l := range leases {
		i[n], o[n], f[n] = l.EventID, l.Owner, l.Fence
	}
	return pq.Array(i), pq.Array(o), pq.Array(f)
}

// fencedUpdate runs one fenced update of want leased rows in one transaction,
// commits the rows it matched and reports ErrLease when it matched fewer.
func (s *Store) fencedUpdate(ctx context.Context, want int, query string, args ...any) error {
	var n int64
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return err
	}
	if n != int64(want) {
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
	return s.RetryBatch(ctx, []Lease{l}, []FailureClass{class}, maxAttempts, delay)
}

// RetryBatch applies Retry to every leases[i] with classes[i] in one
// transaction; ErrLease reports that at least one lease no longer matched.
// A row becomes available again after delay doubled per earlier attempt,
// capped at the larger of delay and MaxRetryDelay. A row of a non-transient
// class is quarantined permanently and is at once available to the relay for
// its control message. A control lease is never exhausted and keeps its class:
// its control message is retried with backoff until it persists.
func (s *Store) RetryBatch(ctx context.Context, leases []Lease, classes []FailureClass, maxAttempts int, delay time.Duration) error {
	if maxAttempts < 1 || maxAttempts > 100 || delay < time.Second || delay > time.Hour {
		return errors.New("delivery: invalid retry bounds")
	}
	if len(leases) != len(classes) {
		return errors.New("delivery: one failure class per lease required")
	}
	names := make([]string, len(classes))
	for i, class := range classes {
		switch class {
		case Transient, SchemaViolation, IntegrityConflict, Authorization:
		default:
			return errors.New("delivery: invalid failure class")
		}
		names[i] = string(class)
	}
	ids, owners, fences := leaseKeys(leases)
	// Exhaustion is based on the row, never the caller's mutable lease snapshot.
	return s.fencedUpdate(ctx, len(leases), "UPDATE "+s.table("delivery_outbox")+" o SET state=CASE WHEN o.state='quarantined' OR o.attempts >= $5::integer OR l.class <> 'transient' THEN 'quarantined' ELSE 'pending' END,failure_class=CASE WHEN o.state='quarantined' THEN o.failure_class ELSE l.class END,available_at=CASE WHEN o.state='pending' AND l.class <> 'transient' THEN clock_timestamp() ELSE clock_timestamp()+(LEAST($6::bigint*power(2::numeric,LEAST(GREATEST(o.attempts,1),40)-1),$7::bigint)::bigint*interval '1 millisecond') END,lease_owner=NULL,lease_until=NULL FROM unnest($1::text[],$2::text[],$3::bigint[],$4::text[]) AS l(event_id,owner,fence,class) WHERE o.event_id=l.event_id AND o.lease_owner=l.owner AND o.fence=l.fence AND o.lease_until>clock_timestamp() AND "+openRow, ids, owners, fences, pq.Array(names), maxAttempts, delay.Milliseconds(), max(delay, MaxRetryDelay).Milliseconds())
}

// ReleaseBatch returns leased rows to pending without counting their claim as
// an attempt, for the rows of a batch held back behind a failed predecessor.
// ErrLease reports that at least one lease no longer matched.
func (s *Store) ReleaseBatch(ctx context.Context, leases []Lease) error {
	ids, owners, fences := leaseKeys(leases)
	return s.fencedUpdate(ctx, len(leases), "UPDATE "+s.table("delivery_outbox")+" o SET attempts=GREATEST(o.attempts-1,0),lease_owner=NULL,lease_until=NULL FROM unnest($1::text[],$2::text[],$3::bigint[]) AS l(event_id,owner,fence) WHERE o.event_id=l.event_id AND o.lease_owner=l.owner AND o.fence=l.fence AND o.lease_until>clock_timestamp() AND "+openRow, ids, owners, fences)
}

// Requeue returns to pending, with a fresh attempt budget, every row
// quarantined as transient whose backoff has elapsed. Rows of a permanent
// failure class stay quarantined. A transient quarantined row holds back
// every later row of the relay until it is requeued and published.
func (s *Store) Requeue(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE "+s.table("delivery_outbox")+" SET state='pending',attempts=0 WHERE state='quarantined' AND failure_class='transient' AND available_at<=clock_timestamp()")
	return err
}

// Quarantined counts the quarantined outbox rows.
func (s *Store) Quarantined(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+s.table("delivery_outbox")+" WHERE state='quarantined'").Scan(&n)
	return n, err
}

// CheckAttemptBudget fences the claim against durable attempts before network
// publication. Claim increments the attempt counter; maxAttempts claims may
// publish. A reclaim after that budget preserves the original row in
// quarantine as transient, which Requeue returns after MaxRetryDelay.
func (s *Store) CheckAttemptBudget(ctx context.Context, l Lease, maxAttempts int) (bool, error) {
	allowed, err := s.CheckAttemptBudgetBatch(ctx, []Lease{l}, maxAttempts)
	if err != nil {
		return false, err
	}
	return len(allowed) == 1, nil
}

// CheckAttemptBudgetBatch applies CheckAttemptBudget to leases in one
// transaction and returns, in their order, the leases that may publish. A
// control lease has no attempt budget.
// ErrLease reports that at least one lease no longer matched its row; the
// returned leases and the quarantine of exhausted rows still hold.
func (s *Store) CheckAttemptBudgetBatch(ctx context.Context, leases []Lease, maxAttempts int) ([]Lease, error) {
	if maxAttempts < 1 || maxAttempts > 100 {
		return nil, errors.New("delivery: invalid retry bound")
	}
	ids, owners, fences := leaseKeys(leases)
	// overBudget reads the row before the update, as every SET expression does;
	// RETURNING reads it after, where an exhausted row is quarantined as transient.
	const overBudget = "(o.state='pending' AND o.attempts>$4::integer)"
	exhausted := make(map[string]bool, len(leases))
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "UPDATE "+s.table("delivery_outbox")+" o SET state=CASE WHEN "+overBudget+" THEN 'quarantined' ELSE o.state END,failure_class=CASE WHEN "+overBudget+" THEN 'transient' ELSE o.failure_class END,available_at=CASE WHEN "+overBudget+" THEN clock_timestamp()+($5::bigint*interval '1 millisecond') ELSE o.available_at END,lease_owner=CASE WHEN "+overBudget+" THEN NULL ELSE o.lease_owner END,lease_until=CASE WHEN "+overBudget+" THEN NULL ELSE o.lease_until END FROM unnest($1::text[],$2::text[],$3::bigint[]) AS l(event_id,owner,fence) WHERE o.event_id=l.event_id AND o.lease_owner=l.owner AND o.fence=l.fence AND o.lease_until>clock_timestamp() AND "+openRow+" RETURNING o.event_id,(o.state='quarantined' AND o.failure_class='transient')", ids, owners, fences, maxAttempts, MaxRetryDelay.Milliseconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var over bool
			if err = rows.Scan(&id, &over); err != nil {
				return err
			}
			exhausted[id] = over
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	allowed := make([]Lease, 0, len(leases))
	for _, l := range leases {
		if over, ok := exhausted[l.EventID]; ok && !over {
			allowed = append(allowed, l)
		}
	}
	if len(exhausted) != len(leases) {
		return allowed, ErrLease
	}
	return allowed, nil
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
