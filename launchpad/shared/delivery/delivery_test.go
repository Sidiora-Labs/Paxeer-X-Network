package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureSubject = "kindle.chain125.swap"

func fixturePolicy() Policy {
	return Policy{Subjects: map[string]bool{fixtureSubject: true}, EventTypes: map[string]bool{"Swap": true}}
}
func fixtureEnvelope(id string) Envelope {
	p := json.RawMessage(`{"amountRaw":"9007199254740993123456789","pool":"0x0000000000000000000000000000000000000001"}`)
	return Envelope{EventID: id, EventType: "Swap", SchemaVersion: 1, ChainID: 125, SourceOrder: SourceOrder{"123", "4", "5"}, EmittedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), TraceID: "trace-fixture", PayloadHash: Hash(p), Payload: p}
}
func TestEnvelope(t *testing.T) {
	spaced := fixtureEnvelope("spaced")
	spaced.Payload = json.RawMessage(`{ "amountRaw" : "9007199254740993123456789" }`)
	spaced.PayloadHash = Hash(spaced.Payload)
	spacedWire, err := spaced.Encode(fixturePolicy(), fixtureSubject)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := Decode(spacedWire, fixturePolicy(), fixtureSubject)
	if err != nil || string(roundtrip.Payload) != string(spaced.Payload) {
		t.Fatal("raw payload whitespace changed", err)
	}

	e := fixtureEnvelope("event-1")
	wire, err := e.Encode(fixturePolicy(), fixtureSubject)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic serialized envelope bytes=%d; production p99 remains unmeasured", len(wire))
	d, err := Decode(wire, fixturePolicy(), fixtureSubject)
	if err != nil || string(d.Payload) != string(e.Payload) {
		t.Fatal("payload changed", err)
	}
	if !CanonicalAmount("9007199254740993123456789") || CanonicalAmount("01") || CanonicalAmount("-1") || CanonicalAmount(strings.Repeat("9", 79)) {
		t.Fatal("raw amount bounds")
	}
	if (SourceOrder{"10", "0", "0"}).Compare(SourceOrder{"9", "999", "999"}) != 1 {
		t.Fatal("lexical order used")
	}
	e.PayloadHash = strings.Repeat("0", 64)
	if !errors.Is(e.Validate(fixturePolicy(), fixtureSubject), ErrIntegrity) {
		t.Fatal("tamper accepted")
	}
	e = fixtureEnvelope("e")
	e.SchemaVersion = 2
	if !errors.Is(e.Validate(fixturePolicy(), fixtureSubject), ErrSchema) {
		t.Fatal("version accepted")
	}
	e = fixtureEnvelope("e")
	e.ChainID = 1
	if e.Validate(fixturePolicy(), fixtureSubject) == nil {
		t.Fatal("chain accepted")
	}
	e = fixtureEnvelope("e")
	if e.Validate(fixturePolicy(), "evil.subject") == nil {
		t.Fatal("subject accepted")
	}
}

type ackFunc func(context.Context) error

func (a ackFunc) Ack(ctx context.Context) error { return a(ctx) }

type publisherFunc func(context.Context, string, string, []byte) (PersistenceAck, error)

func (p publisherFunc) Publish(ctx context.Context, s, id string, b []byte) (PersistenceAck, error) {
	return p(ctx, s, id, b)
}

// Missing integration configuration fails instead of qualifying zero tests.
func TestPostgresDelivery(t *testing.T) {
	dsn, schema := os.Getenv("DELIVERY_TEST_DSN"), os.Getenv("DELIVERY_TEST_SCHEMA")
	if dsn == "" || !strings.HasPrefix(schema, "kd_") || !token.MatchString(schema) {
		t.Fatal("explicit private PostgreSQL DSN and kd_ namespace required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("refusing non-fixture database")
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal("exclusive schema creation:", err)
	}
	defer func() {
		cctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if _, err := pool.Exec(cctx, "DROP SCHEMA "+q+" CASCADE"); err != nil {
			t.Error("cleanup:", err)
		}
	}()
	ddl, err := SchemaDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(pool, schema, fixturePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "CREATE TABLE "+q+".source_events(id text PRIMARY KEY); CREATE TABLE "+q+".cursor(height integer NOT NULL); INSERT INTO "+q+".cursor VALUES(0); CREATE TABLE "+q+".effects(consumer text,generation text,event_id text)"); err != nil {
		t.Fatal(err)
	}
	produce := func(id string) error {
		return s.Produce(ctx, []Pending{{fixtureSubject, fixtureEnvelope(id)}}, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO "+q+".source_events VALUES($1)", id); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "UPDATE "+q+".cursor SET height=height+1")
			return err
		})
	}
	t.Run("OutboxSQLFailureRollsBackSourceAndCursor", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "ALTER TABLE "+q+".delivery_outbox ADD CONSTRAINT injected_failure CHECK(event_id<>'forced-failure')"); err != nil {
			t.Fatal(err)
		}
		if err := produce("forced-failure"); err == nil {
			t.Fatal("forced insert succeeded")
		}
		var events, cursor, outbox int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".source_events),(SELECT height FROM "+q+".cursor),(SELECT count(*) FROM "+q+".delivery_outbox)").Scan(&events, &cursor, &outbox); err != nil {
			t.Fatal(err)
		}
		if events != 0 || cursor != 0 || outbox != 0 {
			t.Fatalf("partial commit %d %d %d", events, cursor, outbox)
		}
		if err := produce("committed-before-relay"); err != nil {
			t.Fatal(err)
		}
		restarted, _ := NewStore(pool, schema, fixturePolicy())
		leases, err := restarted.Claim(ctx, "restart", 1, time.Minute)
		if err != nil || len(leases) != 1 {
			t.Fatal("lost committed outbox", leases, err)
		}
		if err = restarted.MarkPublished(ctx, leases[0], PersistenceAck{"fixture-stream", 1}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("HundredDuplicatesPerConsumerGenerationCommitOnce", func(t *testing.T) {
		wire, _ := fixtureEnvelope("swap-duplicate").Encode(fixturePolicy(), fixtureSubject)
		for _, identity := range [][2]string{{"stats", "g1"}, {"charts", "g1"}, {"stats", "g2"}} {
			consumer, generation := identity[0], identity[1]
			var count atomic.Int64
			var wg sync.WaitGroup
			errs := make(chan error, 100)
			for i := 0; i < 100; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- s.Consume(ctx, consumer, generation, fixtureSubject, wire, ackFunc(func(ctx context.Context) error {
						var n int
						if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".effects WHERE consumer=$1 AND generation=$2", consumer, generation).Scan(&n); err != nil {
							return err
						}
						if n != 1 {
							return errors.New("ack before visible commit")
						}
						count.Add(1)
						return nil
					}), func(ctx context.Context, tx pgx.Tx, e Envelope) error {
						_, err := tx.Exec(ctx, "INSERT INTO "+q+".effects VALUES($1,$2,$3)", consumer, generation, e.EventID)
						return err
					})
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if count.Load() != 100 {
				t.Fatal("missing acknowledgements", count.Load())
			}
		}
		var effects, inbox int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".effects),(SELECT count(*) FROM "+q+".delivery_inbox)").Scan(&effects, &inbox); err != nil {
			t.Fatal(err)
		}
		if effects != 3 || inbox != 3 {
			t.Fatal("duplicate effects", effects, inbox)
		}
	})
	t.Run("EffectRollbackAndAckLossRecovery", func(t *testing.T) {
		wire, _ := fixtureEnvelope("swap-retry").Encode(fixturePolicy(), fixtureSubject)
		acks := 0
		ack := ackFunc(func(context.Context) error { acks++; return errors.New("injected ack loss") })
		effect := func(ctx context.Context, tx pgx.Tx, e Envelope) error {
			_, err := tx.Exec(ctx, "INSERT INTO "+q+".effects VALUES('retry','g1',$1)", e.EventID)
			return err
		}
		err := s.Consume(ctx, "retry", "g1", fixtureSubject, wire, ack, func(ctx context.Context, tx pgx.Tx, e Envelope) error {
			if err := effect(ctx, tx, e); err != nil {
				return err
			}
			return errors.New("injected effect failure")
		})
		if err == nil || acks != 0 {
			t.Fatal("failed effect acknowledged")
		}
		var n int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".effects WHERE consumer='retry'").Scan(&n); err != nil || n != 0 {
			t.Fatal("rollback failed", n, err)
		}
		if err = s.Consume(ctx, "retry", "g1", fixtureSubject, wire, ack, effect); err == nil || acks != 1 {
			t.Fatal("ack loss absent")
		}
		if err = s.Consume(ctx, "retry", "g1", fixtureSubject, wire, ackFunc(func(context.Context) error { acks++; return nil }), effect); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".effects WHERE consumer='retry'").Scan(&n); err != nil || n != 1 {
			t.Fatal("ack loss duplicated effect", n, err)
		}
	})
	t.Run("ImmutableOutboxAndInboxIdentity", func(t *testing.T) {
		if err := produce("immutable"); err != nil {
			t.Fatal(err)
		}
		e := fixtureEnvelope("immutable")
		e.TraceID = "different"
		if err := s.Produce(ctx, []Pending{{fixtureSubject, e}}, func(context.Context, pgx.Tx) error { return nil }); !errors.Is(err, ErrIntegrity) {
			t.Fatal("conflict accepted", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE "+q+".delivery_outbox SET subject='evil' WHERE event_id='immutable'"); err == nil {
			t.Fatal("SQL identity changed")
		}
		e = fixtureEnvelope("swap-duplicate")
		e.TraceID = "different"
		wire, _ := e.Encode(fixturePolicy(), fixtureSubject)
		acked := false
		err := s.Consume(ctx, "stats", "g1", fixtureSubject, wire, ackFunc(func(context.Context) error { acked = true; return nil }), func(context.Context, pgx.Tx, Envelope) error { t.Fatal("conflicting effect applied"); return nil })
		if !errors.Is(err, ErrIntegrity) || acked {
			t.Fatal("inbox conflict accepted", err)
		}
		var recorded []byte
		var class string
		if err = pool.QueryRow(ctx, "SELECT envelope,failure_class FROM "+q+".delivery_quarantine WHERE consumer_id='stats' AND projection_generation='g1' AND envelope_hash=$1", Hash(wire)).Scan(&recorded, &class); err != nil || string(recorded) != string(wire) || class != string(IntegrityConflict) {
			t.Fatal("conflicting inbox identity not durably quarantined", class, err)
		}
	})
	t.Run("ExpiredLeaseFencesAndDurableQuarantine", func(t *testing.T) {
		leases, err := s.Claim(ctx, "relay-a", 1, time.Second)
		if err != nil || len(leases) != 1 {
			t.Fatal(leases, err)
		}
		first := leases[0]
		leases, err = s.Claim(ctx, "relay-b", 1, time.Second)
		if err != nil || len(leases) != 0 {
			t.Fatal("active lease reclaimed", leases, err)
		}
		if _, err = pool.Exec(ctx, "UPDATE "+q+".delivery_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE event_id=$1", first.EventID); err != nil {
			t.Fatal(err)
		}
		leases, err = s.Claim(ctx, "relay-b", 1, time.Minute)
		if err != nil || len(leases) != 1 {
			t.Fatal(leases, err)
		}
		second := leases[0]
		if first.Fence >= second.Fence || string(first.Wire) != string(second.Wire) {
			t.Fatal("changed immutable identity/fence")
		}
		if err = s.MarkPublished(ctx, first, PersistenceAck{"fixture-stream", 2}); !errors.Is(err, ErrLease) {
			t.Fatal("stale fence accepted", err)
		}
		if err = s.MarkPublished(ctx, second, PersistenceAck{}); err == nil {
			t.Fatal("absent persistence ack accepted")
		}
		limit := second.Attempts
		second.Attempts = 0 // Mutable lease snapshots cannot reset durable attempts.
		if err = s.Retry(ctx, second, Transient, limit, time.Second); err != nil {
			t.Fatal(err)
		}
		var state, class string
		var attempts int
		if err = pool.QueryRow(ctx, "SELECT state,failure_class,attempts FROM "+q+".delivery_outbox WHERE event_id=$1", second.EventID).Scan(&state, &class, &attempts); err != nil {
			t.Fatal(err)
		}
		stats, err := s.Stats(ctx)
		if err != nil || stats.LeaseExpiryCount != 1 || stats.QuarantinedRows != 1 || stats.PendingBytes == 0 {
			t.Fatal("lease/quarantine diagnostics missing", stats, err)
		}
		if state != "quarantined" || class != "transient" || attempts != 2 {
			t.Fatal("quarantine not durable", state, class, attempts)
		}
		restarted, _ := NewStore(pool, schema, fixturePolicy())
		leases, err = restarted.Claim(ctx, "relay-c", 1, time.Minute)
		if err != nil || len(leases) != 0 {
			t.Fatal("quarantine reentered delivery", leases, err)
		}
	})
	t.Run("UnknownVersionAndHashMismatchDurablyQuarantined", func(t *testing.T) {
		e := fixtureEnvelope("unknown")
		e.SchemaVersion = 2
		wire, _ := json.Marshal(e)
		acked := false
		ack := ackFunc(func(context.Context) error { acked = true; return nil })
		effect := func(context.Context, pgx.Tx, Envelope) error { t.Fatal("invalid envelope effect"); return nil }
		if err := s.Consume(ctx, "validator", "g1", fixtureSubject, wire, ack, effect); !errors.Is(err, ErrSchema) {
			t.Fatal(err)
		}
		e = fixtureEnvelope("invalid-hash")
		e.PayloadHash = strings.Repeat("0", 64)
		wire, _ = json.Marshal(e)
		if err := s.Consume(ctx, "validator", "g1", fixtureSubject, wire, ack, effect); !errors.Is(err, ErrIntegrity) {
			t.Fatal(err)
		}
		var quarantine, inbox int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".delivery_quarantine WHERE consumer_id='validator'),(SELECT count(*) FROM "+q+".delivery_inbox WHERE consumer_id='validator')").Scan(&quarantine, &inbox); err != nil {
			t.Fatal(err)
		}
		if quarantine != 2 || inbox != 0 || acked {
			t.Fatal("invalid envelope advanced inbox", quarantine, inbox, acked)
		}
	})
	t.Run("RelayRequiresPersistenceAck", func(t *testing.T) {
		if err := produce("relay-no-ack"); err != nil {
			t.Fatal(err)
		}
		// Injected publisher exercises the contract only, never real broker evidence.
		r := Relay{Store: s, Owner: "relay-test", Batch: 1, LeaseDuration: time.Minute, PublishTimeout: time.Second, RetryDelay: time.Second, MaxAttempts: 3, Publisher: publisherFunc(func(context.Context, string, string, []byte) (PersistenceAck, error) { return PersistenceAck{}, nil })}
		if err := r.RunOnce(ctx); err == nil {
			t.Fatal("missing ack accepted")
		}
		var state string
		if err := pool.QueryRow(ctx, "SELECT state FROM "+q+".delivery_outbox WHERE event_id='relay-no-ack'").Scan(&state); err != nil || state != "pending" {
			t.Fatal("published without ack", state, err)
		}
		if _, err := pool.Exec(ctx, "UPDATE "+q+".delivery_outbox SET available_at=clock_timestamp()-interval '1 second' WHERE event_id='relay-no-ack'"); err != nil {
			t.Fatal(err)
		}
		r.Publisher = publisherFunc(func(_ context.Context, subject, id string, wire []byte) (PersistenceAck, error) {
			expected, _ := fixtureEnvelope(id).Encode(fixturePolicy(), subject)
			if id != "relay-no-ack" || string(wire) != string(expected) {
				t.Fatal("changed relay identity")
			}
			return PersistenceAck{"fixture-stream", 3}, nil
		})
		if err := r.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SELECT state FROM "+q+".delivery_outbox WHERE event_id='relay-no-ack'").Scan(&state); err != nil || state != "published" {
			t.Fatal(state, err)
		}
	})
	t.Run("CanceledProducerAndConsumerWritesRollbackWithinBound", func(t *testing.T) {
		var before int
		if err := pool.QueryRow(ctx, "SELECT height FROM "+q+".cursor").Scan(&before); err != nil {
			t.Fatal(err)
		}
		for _, consumer := range []bool{false, true} {
			op, cancel := context.WithCancel(ctx)
			started := time.Now()
			acked := false
			blockUntilCanceled := func(tx pgx.Tx) error {
				timer := time.AfterFunc(50*time.Millisecond, cancel)
				defer timer.Stop()
				_, err := tx.Exec(op, "SELECT pg_sleep(10)")
				return err
			}
			var err error
			if consumer {
				wire, _ := fixtureEnvelope("cancel-consumer").Encode(fixturePolicy(), fixtureSubject)
				err = s.Consume(op, "cancel-consumer", "g1", fixtureSubject, wire, ackFunc(func(context.Context) error { acked = true; return nil }), func(op context.Context, tx pgx.Tx, e Envelope) error {
					if _, err := tx.Exec(op, "INSERT INTO "+q+".effects VALUES('cancel-consumer','g1',$1)", e.EventID); err != nil {
						return err
					}
					return blockUntilCanceled(tx)
				})
			} else {
				err = s.Produce(op, []Pending{{fixtureSubject, fixtureEnvelope("cancel-producer")}}, func(op context.Context, tx pgx.Tx) error {
					if _, err := tx.Exec(op, "INSERT INTO "+q+".source_events VALUES('cancel-producer'); UPDATE "+q+".cursor SET height=height+1"); err != nil {
						return err
					}
					if err := s.Enqueue(op, tx, fixtureSubject, fixtureEnvelope("cancel-producer")); err != nil {
						return err
					}
					return blockUntilCanceled(tx)
				})
			}
			if err == nil || op.Err() == nil || acked {
				t.Fatal("cancellation not safely observed", consumer, err, acked)
			}
			cancel()
			if time.Since(started) > 3*time.Second {
				t.Fatal("cancellation cleanup exceeded bound", consumer)
			}
			probe, stop := context.WithTimeout(ctx, 2*time.Second)
			if err = pool.Ping(probe); err != nil {
				stop()
				t.Fatal("pool unusable after cancellation", consumer, err)
			}
			var source, outbox, height, inbox, effects int
			err = pool.QueryRow(probe, "SELECT (SELECT count(*) FROM "+q+".source_events WHERE id='cancel-producer'),(SELECT count(*) FROM "+q+".delivery_outbox WHERE event_id='cancel-producer'),(SELECT height FROM "+q+".cursor),(SELECT count(*) FROM "+q+".delivery_inbox WHERE consumer_id='cancel-consumer'),(SELECT count(*) FROM "+q+".effects WHERE consumer='cancel-consumer')").Scan(&source, &outbox, &height, &inbox, &effects)
			stop()
			if err != nil {
				t.Fatal(err)
			}
			if source != 0 || outbox != 0 || height != before || inbox != 0 || effects != 0 {
				t.Fatal("partial cancellation state", consumer, source, outbox, height, inbox, effects)
			}
		}
	})
	t.Run("CrashedClaimsExhaustDurableBudgetBeforeAnotherPublication", func(t *testing.T) {
		if err := produce("crash-budget"); err != nil {
			t.Fatal(err)
		}
		expected, _ := fixtureEnvelope("crash-budget").Encode(fixturePolicy(), fixtureSubject)
		for i := 1; i <= 3; i++ {
			restarted, err := NewStore(pool, schema, fixturePolicy())
			if err != nil {
				t.Fatal(err)
			}
			leases, err := restarted.Claim(ctx, "crashed-relay", 1, time.Minute)
			if err != nil || len(leases) != 1 {
				t.Fatal("missing restarted claim", leases, err)
			}
			l := leases[0]
			if l.Attempts != i || string(l.Wire) != string(expected) {
				t.Fatal("durable attempt/identity changed", l.Attempts)
			}
			if _, err = pool.Exec(ctx, "UPDATE "+q+".delivery_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE event_id=$1", l.EventID); err != nil {
				t.Fatal(err)
			}
		}
		restarted, err := NewStore(pool, schema, fixturePolicy())
		if err != nil {
			t.Fatal(err)
		}
		publications := 0
		r := Relay{Store: restarted, Owner: "restarted-budget", Batch: 1, LeaseDuration: time.Minute, PublishTimeout: time.Second, RetryDelay: time.Second, MaxAttempts: 3, Publisher: publisherFunc(func(context.Context, string, string, []byte) (PersistenceAck, error) {
			publications++
			return PersistenceAck{"fixture-stream", 99}, nil
		})}
		if err = r.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if publications != 0 {
			t.Fatal("exhausted budget published again", publications)
		}
		var state string
		var wire []byte
		if err = pool.QueryRow(ctx, "SELECT state,envelope FROM "+q+".delivery_outbox WHERE event_id='crash-budget'").Scan(&state, &wire); err != nil {
			t.Fatal(err)
		}
		if state != "quarantined" || string(wire) != string(expected) {
			t.Fatal("exhaustion dropped/mutated row", state)
		}
		if err = r.RunOnce(ctx); err != nil || publications != 0 {
			t.Fatal("quarantine reentered publication", err, publications)
		}
	})

}
