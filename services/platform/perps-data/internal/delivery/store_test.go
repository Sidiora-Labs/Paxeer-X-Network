package delivery_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	fillSubject     = "perps.125.7.fill"
	orderSubject    = "perps.125.7.order"
	relayOwner      = "relay-test"
)

func testPolicy() delivery.Policy {
	return delivery.Policy{
		Subjects:   map[string]bool{fillSubject: true, orderSubject: true},
		EventTypes: map[string]bool{"fill": true, "order": true},
	}
}

func testEnvelope(id, kind string, height int) delivery.Envelope {
	p := json.RawMessage(`{"price":"115792089237316195423570985008687907853269984665640564039457584007913129639935","market":"7"}`)
	return delivery.Envelope{
		EventID: id, EventType: kind, SchemaVersion: 1, ChainID: 125,
		SourceOrder: delivery.SourceOrder{strconv.Itoa(height), "0", "1"},
		EmittedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		TraceID:     "trace-" + id, PayloadHash: delivery.Hash(p), Payload: p,
	}
}

type outboxRow struct {
	state, failureClass, brokerStream string
	attempts                          int
	fence, brokerSequence             int64
	availableInMS                     int64
	published                         bool
}

func readOutbox(t *testing.T, db *sql.DB, id string) outboxRow {
	t.Helper()
	var r outboxRow
	var failure, brokerStream sql.NullString
	var sequence sql.NullInt64
	err := db.QueryRow(`SELECT state,attempts,fence,failure_class,broker_stream,broker_sequence,published_at IS NOT NULL,
 (EXTRACT(EPOCH FROM (available_at-clock_timestamp()))*1000)::bigint FROM perps.delivery_outbox WHERE event_id=$1`, id).
		Scan(&r.state, &r.attempts, &r.fence, &failure, &brokerStream, &sequence, &r.published, &r.availableInMS)
	if err != nil {
		t.Fatalf("read outbox %s: %v", id, err)
	}
	r.failureClass, r.brokerStream, r.brokerSequence = failure.String, brokerStream.String, sequence.Int64
	return r
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func enqueueCommitted(t *testing.T, ctx context.Context, db *sql.DB, s *delivery.Store, subject string, envelopes ...delivery.Envelope) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envelopes {
		if _, err = s.Enqueue(ctx, tx, subject, e); err != nil {
			_ = tx.Rollback()
			t.Fatalf("Enqueue %s: %v", e.EventID, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func claimOne(t *testing.T, ctx context.Context, s *delivery.Store) delivery.Lease {
	t.Helper()
	leases, err := s.Claim(ctx, relayOwner, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(leases) != 1 {
		t.Fatalf("Claim returned %d leases, want 1", len(leases))
	}
	return leases[0]
}

func wantNoClaim(t *testing.T, ctx context.Context, s *delivery.Store) {
	t.Helper()
	leases, err := s.Claim(ctx, relayOwner, 10, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(leases) != 0 {
		t.Fatalf("Claim returned %d leases, want 0", len(leases))
	}
}

type jetStreamPublisher struct {
	js     jetstream.JetStream
	stream string
}

func (p jetStreamPublisher) Publish(ctx context.Context, subject, eventID string, wire []byte) (delivery.PersistenceAck, error) {
	ack, err := p.js.Publish(ctx, subject, wire, jetstream.WithMsgID(eventID), jetstream.WithExpectStream(p.stream))
	if err != nil {
		return delivery.PersistenceAck{}, err
	}
	return delivery.PersistenceAck{Stream: ack.Stream, Sequence: ack.Sequence}, nil
}

func (p jetStreamPublisher) PublishBatch(ctx context.Context, leases []delivery.Lease) []delivery.Publication {
	out := make([]delivery.Publication, len(leases))
	for i, l := range leases {
		if l.Control {
			out[i].Ack, out[i].Err = p.publishControl(ctx, l)
			continue
		}
		out[i].Ack, out[i].Err = p.Publish(ctx, l.Subject, l.EventID, l.Wire)
	}
	return out
}

func (p jetStreamPublisher) publishControl(ctx context.Context, l delivery.Lease) (delivery.PersistenceAck, error) {
	subject, id, body, err := delivery.ControlMessage(l)
	if err != nil {
		return delivery.PersistenceAck{}, err
	}
	msg := &nats.Msg{Subject: subject, Data: body, Header: nats.Header{}}
	msg.Header.Set(delivery.HeaderControl, delivery.ControlSkipped)
	msg.Header.Set("Perps-Seq", strconv.FormatInt(l.PerpsSeq, 10))
	ack, err := p.js.PublishMsg(ctx, msg, jetstream.WithMsgID(id), jetstream.WithExpectStream(p.stream))
	if err != nil {
		return delivery.PersistenceAck{}, err
	}
	return delivery.PersistenceAck{Stream: ack.Stream, Sequence: ack.Sequence}, nil
}

type brokerDelivery struct{ msg jetstream.Msg }

func (d brokerDelivery) Ack(ctx context.Context) error { return d.msg.DoubleAck(ctx) }

func startBroker(t *testing.T, ctx context.Context) jetstream.JetStream {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := stream.Config(125)
	cfg.Replicas = 1
	if _, err = js.CreateStream(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	return js
}

func fetchOne(t *testing.T, c jetstream.Consumer) jetstream.Msg {
	t.Helper()
	batch, err := c.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []jetstream.Msg
	for m := range batch.Messages() {
		msgs = append(msgs, m)
	}
	if batch.Error() != nil {
		t.Fatal(batch.Error())
	}
	if len(msgs) != 1 {
		t.Fatalf("fetched %d messages, want 1", len(msgs))
	}
	return msgs[0]
}

func ackPending(t *testing.T, ctx context.Context, c jetstream.Consumer) int {
	t.Helper()
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info.NumAckPending
}

func TestDeliveryStore(t *testing.T) {
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestDeliveryStore requires " + testDatabaseEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err = store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err = db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox, perps.delivery_inbox, perps.delivery_quarantine`); err != nil {
		t.Fatal(err)
	}
	s, err := delivery.NewStore(db, "perps", testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = delivery.NewStore(nil, "perps", testPolicy()); err == nil {
		t.Fatal("NewStore accepted a nil database")
	}
	if _, err = delivery.NewStore(db, "perps;drop", testPolicy()); err == nil {
		t.Fatal("NewStore accepted an invalid schema")
	}
	js := startBroker(t, ctx)

	t.Run("enqueue_in_tx", func(t *testing.T) {
		e := testEnvelope("enqueue-1", "fill", 100)
		if _, err := s.Enqueue(ctx, nil, fillSubject, e); err == nil || err.Error() != "delivery: transaction required" {
			t.Fatalf("nil tx: %v", err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Enqueue(ctx, tx, fillSubject, e); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE event_id=$1`, e.EventID); n != 0 {
			t.Fatalf("rolled back enqueue left %d rows", n)
		}
		enqueueCommitted(t, ctx, db, s, fillSubject, e)
		want, err := e.Encode(testPolicy(), fillSubject)
		if err != nil {
			t.Fatal(err)
		}
		var subject, hash string
		var version int
		var wire []byte
		if err = db.QueryRow(`SELECT subject,schema_version,payload_hash,envelope FROM perps.delivery_outbox WHERE event_id=$1`, e.EventID).Scan(&subject, &version, &hash, &wire); err != nil {
			t.Fatal(err)
		}
		if subject != fillSubject || version != 1 || hash != e.PayloadHash || !bytes.Equal(wire, want) {
			t.Fatalf("stored row = %q %d %q %q", subject, version, hash, wire)
		}
		if r := readOutbox(t, db, e.EventID); r.state != "pending" || r.attempts != 0 || r.fence != 0 || r.published {
			t.Fatalf("new row = %+v", r)
		}
		enqueueCommitted(t, ctx, db, s, fillSubject, e)
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE event_id=$1`, e.EventID); n != 1 {
			t.Fatalf("identical re-enqueue produced %d rows", n)
		}
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		changed := testEnvelope("enqueue-1", "fill", 101)
		if _, err = s.Enqueue(ctx, tx, fillSubject, changed); !errors.Is(err, delivery.ErrIntegrity) {
			t.Fatalf("changed envelope under same id: %v", err)
		}
		if _, err = s.Enqueue(ctx, tx, orderSubject, testEnvelope("enqueue-1", "fill", 100)); !errors.Is(err, delivery.ErrIntegrity) {
			t.Fatalf("same id on another subject: %v", err)
		}
		if _, err = s.Enqueue(ctx, tx, "perps.125.8.fill", testEnvelope("enqueue-2", "fill", 100)); !errors.Is(err, delivery.ErrAuthorization) {
			t.Fatalf("unapproved subject: %v", err)
		}
		tampered := testEnvelope("enqueue-3", "fill", 100)
		tampered.PayloadHash = delivery.Hash([]byte(`{}`))
		if _, err = s.Enqueue(ctx, tx, fillSubject, tampered); !errors.Is(err, delivery.ErrIntegrity) {
			t.Fatalf("payload hash mismatch: %v", err)
		}
		oversized := testEnvelope("enqueue-4", "fill", 100)
		oversized.Payload = json.RawMessage(`{"pad":"` + string(bytes.Repeat([]byte("a"), delivery.MaxEnvelopeBytes)) + `"}`)
		oversized.PayloadHash = delivery.Hash(oversized.Payload)
		if _, err = s.Enqueue(ctx, tx, fillSubject, oversized); err == nil || err.Error() != "delivery: invalid or oversized payload" {
			t.Fatalf("oversized envelope: %v", err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		lease := claimOne(t, ctx, s)
		if err = s.MarkPublished(ctx, lease, delivery.PersistenceAck{Stream: "cleanup", Sequence: 1}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("claim_mark_published", func(t *testing.T) {
		e := testEnvelope("claim-1", "fill", 200)
		enqueueCommitted(t, ctx, db, s, fillSubject, e)
		if _, err := s.Claim(ctx, relayOwner, 1, 500*time.Millisecond); err == nil || err.Error() != "delivery: invalid lease bounds" {
			t.Fatalf("short lease: %v", err)
		}
		if _, err := s.Claim(ctx, "bad owner", 1, time.Minute); err == nil {
			t.Fatal("Claim accepted an invalid owner")
		}
		lease := claimOne(t, ctx, s)
		want, err := e.Encode(testPolicy(), fillSubject)
		if err != nil {
			t.Fatal(err)
		}
		if lease.EventID != e.EventID || lease.Subject != fillSubject || lease.Owner != relayOwner || lease.Fence != 1 || lease.Attempts != 1 || !bytes.Equal(lease.Wire, want) {
			t.Fatalf("lease = %+v", lease)
		}
		wantNoClaim(t, ctx, s)
		if err = s.MarkPublished(ctx, lease, delivery.PersistenceAck{}); err == nil || err.Error() != "delivery: persistence acknowledgement required" {
			t.Fatalf("empty ack: %v", err)
		}
		if err = s.MarkPublished(ctx, lease, delivery.PersistenceAck{Stream: stream.Name, Sequence: 1 << 63}); err == nil {
			t.Fatal("MarkPublished accepted a sequence above int64")
		}
		stale := lease
		stale.Fence = 2
		if err = s.MarkPublished(ctx, stale, delivery.PersistenceAck{Stream: stream.Name, Sequence: 7}); !errors.Is(err, delivery.ErrLease) {
			t.Fatalf("stale fence: %v", err)
		}
		if err = s.MarkPublished(ctx, lease, delivery.PersistenceAck{Stream: stream.Name, Sequence: 7}); err != nil {
			t.Fatal(err)
		}
		r := readOutbox(t, db, e.EventID)
		if r.state != "published" || !r.published || r.brokerStream != stream.Name || r.brokerSequence != 7 || r.failureClass != "" {
			t.Fatalf("published row = %+v", r)
		}
		if err = s.MarkPublished(ctx, lease, delivery.PersistenceAck{Stream: stream.Name, Sequence: 8}); !errors.Is(err, delivery.ErrLease) {
			t.Fatalf("second MarkPublished: %v", err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE perps.delivery_outbox SET envelope=$2 WHERE event_id=$1`, e.EventID, []byte(`{}`)); err == nil {
			t.Fatal("stored envelope was changed")
		}
		wantNoClaim(t, ctx, s)
	})

	t.Run("retry_backoff", func(t *testing.T) {
		e := testEnvelope("retry-1", "fill", 300)
		enqueueCommitted(t, ctx, db, s, fillSubject, e)
		lease := claimOne(t, ctx, s)
		if err := s.Retry(ctx, lease, delivery.Transient, 3, 500*time.Millisecond); err == nil || err.Error() != "delivery: invalid retry bounds" {
			t.Fatalf("short delay: %v", err)
		}
		if err := s.Retry(ctx, lease, delivery.FailureClass("unknown"), 3, time.Second); err == nil || err.Error() != "delivery: invalid failure class" {
			t.Fatalf("unknown class: %v", err)
		}
		if err := s.Retry(ctx, lease, delivery.Transient, 3, 2*time.Second); err != nil {
			t.Fatal(err)
		}
		r := readOutbox(t, db, e.EventID)
		if r.state != "pending" || r.failureClass != "transient" || r.attempts != 1 || r.availableInMS < 1000 || r.availableInMS > 2000 {
			t.Fatalf("after transient retry = %+v", r)
		}
		if err := s.Retry(ctx, lease, delivery.Transient, 3, time.Second); !errors.Is(err, delivery.ErrLease) {
			t.Fatalf("retry of released lease: %v", err)
		}
		wantNoClaim(t, ctx, s)
		time.Sleep(time.Duration(r.availableInMS+100) * time.Millisecond)
		second := claimOne(t, ctx, s)
		if second.EventID != e.EventID || second.PerpsSeq != lease.PerpsSeq || second.Fence != 2 || second.Attempts != 2 {
			t.Fatalf("reclaim = %+v", second)
		}
		if err := s.Retry(ctx, second, delivery.Transient, 2, time.Second); err != nil {
			t.Fatal(err)
		}
		if r = readOutbox(t, db, e.EventID); r.state != "quarantined" || r.failureClass != "transient" || r.attempts != 2 {
			t.Fatalf("after exhausted retry = %+v", r)
		}
		poison := testEnvelope("retry-2", "fill", 301)
		enqueueCommitted(t, ctx, db, s, fillSubject, poison)
		lease = claimOne(t, ctx, s)
		if err := s.Retry(ctx, lease, delivery.SchemaViolation, 3, time.Second); err != nil {
			t.Fatal(err)
		}
		if r = readOutbox(t, db, poison.EventID); r.state != "quarantined" || r.failureClass != "schema_violation" || r.attempts != 1 {
			t.Fatalf("after schema violation = %+v", r)
		}
		wantNoClaim(t, ctx, s)
	})

	t.Run("consume_inbox_dedup", func(t *testing.T) {
		c, err := js.CreateOrUpdateConsumer(ctx, stream.Name, jetstream.ConsumerConfig{Durable: "consume-test", FilterSubject: orderSubject, AckPolicy: jetstream.AckExplicitPolicy, AckWait: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		e := testEnvelope("consume-1", "order", 400)
		wire, err := e.Encode(testPolicy(), orderSubject)
		if err != nil {
			t.Fatal(err)
		}
		// Two broker copies of one event model a redelivery after the duplicate window.
		for i := 0; i < 2; i++ {
			if _, err = js.Publish(ctx, orderSubject, wire); err != nil {
				t.Fatal(err)
			}
		}
		effects := 0
		effect := func(ctx context.Context, tx *sql.Tx, got delivery.Envelope) error {
			if got.EventID != e.EventID || !bytes.Equal(got.Payload, e.Payload) {
				return errors.New("effect received a different envelope")
			}
			effects++
			return nil
		}
		failing := func(context.Context, *sql.Tx, delivery.Envelope) error { return errors.New("projection failed") }

		first := fetchOne(t, c)
		if err = s.Consume(ctx, "consumer-a", "gen-1", orderSubject, first.Data(), brokerDelivery{first}, failing); err == nil || err.Error() != "projection failed" {
			t.Fatalf("failing effect: %v", err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_inbox`); n != 0 {
			t.Fatalf("failed effect left %d inbox rows", n)
		}
		if n := ackPending(t, ctx, c); n != 1 {
			t.Fatalf("ack pending after failed effect = %d, want 1", n)
		}
		if err = s.Consume(ctx, "consumer-a", "gen-1", orderSubject, first.Data(), brokerDelivery{first}, effect); err != nil {
			t.Fatal(err)
		}
		second := fetchOne(t, c)
		if err = s.Consume(ctx, "consumer-a", "gen-1", orderSubject, second.Data(), brokerDelivery{second}, effect); err != nil {
			t.Fatal(err)
		}
		if effects != 1 {
			t.Fatalf("effect ran %d times, want 1", effects)
		}
		if n := ackPending(t, ctx, c); n != 0 {
			t.Fatalf("ack pending after duplicate = %d, want 0", n)
		}
		var hash string
		if err = db.QueryRow(`SELECT envelope_hash FROM perps.delivery_inbox WHERE consumer_id='consumer-a' AND projection_generation='gen-1' AND event_id=$1`, e.EventID).Scan(&hash); err != nil || hash != delivery.Hash(wire) {
			t.Fatalf("inbox hash = %q, %v", hash, err)
		}
		if err = s.Consume(ctx, "consumer-a", "gen-2", orderSubject, wire, brokerDelivery{second}, effect); !errors.Is(err, jetstream.ErrMsgAlreadyAckd) {
			t.Fatalf("new generation over an acknowledged delivery: %v", err)
		}
		if effects != 2 || count(t, db, `SELECT count(*) FROM perps.delivery_inbox WHERE event_id=$1`, e.EventID) != 2 {
			t.Fatalf("new generation: effects %d", effects)
		}

		conflict := testEnvelope("consume-1", "order", 400)
		conflict.TraceID = "trace-other"
		conflictWire, err := conflict.Encode(testPolicy(), orderSubject)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = js.Publish(ctx, orderSubject, conflictWire); err != nil {
			t.Fatal(err)
		}
		third := fetchOne(t, c)
		if err = s.Consume(ctx, "consumer-a", "gen-1", orderSubject, third.Data(), brokerDelivery{third}, effect); !errors.Is(err, delivery.ErrIntegrity) {
			t.Fatalf("conflicting envelope: %v", err)
		}
		if effects != 2 {
			t.Fatalf("conflict ran the effect: %d", effects)
		}
		if n := ackPending(t, ctx, c); n != 1 {
			t.Fatalf("ack pending after conflict = %d, want 1", n)
		}
		var class string
		var quarantined []byte
		if err = db.QueryRow(`SELECT failure_class,envelope FROM perps.delivery_quarantine WHERE consumer_id='consumer-a' AND projection_generation='gen-1' AND envelope_hash=$1`, delivery.Hash(conflictWire)).Scan(&class, &quarantined); err != nil || class != string(delivery.IntegrityConflict) || !bytes.Equal(quarantined, conflictWire) {
			t.Fatalf("conflict quarantine = %q, %v", class, err)
		}
		if err = s.Consume(ctx, "consumer-a", "gen-1", "perps.125.9.order", wire, brokerDelivery{third}, effect); !errors.Is(err, delivery.ErrAuthorization) {
			t.Fatalf("unapproved subject: %v", err)
		}
		if err = db.QueryRow(`SELECT failure_class FROM perps.delivery_quarantine WHERE subject='perps.125.9.order'`).Scan(&class); err != nil || class != string(delivery.Authorization) {
			t.Fatalf("authorization quarantine = %q, %v", class, err)
		}
		if err = s.Consume(ctx, "consumer-a", "gen-1", orderSubject, []byte(`{"event_id":"x"`), brokerDelivery{third}, effect); err == nil {
			t.Fatal("malformed envelope accepted")
		}
		if err = db.QueryRow(`SELECT failure_class FROM perps.delivery_quarantine WHERE envelope_hash=$1`, delivery.Hash([]byte(`{"event_id":"x"`))).Scan(&class); err != nil || class != string(delivery.SchemaViolation) {
			t.Fatalf("schema quarantine = %q, %v", class, err)
		}
		if err = s.Consume(ctx, "consumer a", "gen-1", orderSubject, wire, brokerDelivery{third}, effect); err == nil || err.Error() != "delivery: invalid consumer configuration" {
			t.Fatalf("invalid consumer: %v", err)
		}
	})

	t.Run("relay_run_once", func(t *testing.T) {
		// The rows quarantined above hold back every later row of the relay.
		if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox`); err != nil {
			t.Fatal(err)
		}
		publisher := jetStreamPublisher{js: js, stream: stream.Name}
		relay := delivery.Relay{Store: s, Publisher: publisher, Owner: relayOwner, Batch: 10, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 3}
		invalid := relay
		invalid.PublishTimeout = invalid.LeaseDuration
		if err := invalid.RunOnce(ctx); err == nil || err.Error() != "delivery: invalid relay configuration" {
			t.Fatalf("invalid relay: %v", err)
		}
		before, err := js.Stream(ctx, stream.Name)
		if err != nil {
			t.Fatal(err)
		}
		info, err := before.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		base := info.State.LastSeq
		var envelopes []delivery.Envelope
		for i := 0; i < 5; i++ {
			envelopes = append(envelopes, testEnvelope("relay-"+strconv.Itoa(i), "fill", 500+i))
		}
		enqueueCommitted(t, ctx, db, s, fillSubject, envelopes...)
		if err = relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		var last int64
		for i, e := range envelopes {
			r := readOutbox(t, db, e.EventID)
			if r.state != "published" || r.brokerStream != stream.Name || r.brokerSequence != int64(base)+int64(i)+1 || r.attempts != 1 {
				t.Fatalf("relayed row %d = %+v", i, r)
			}
			if r.brokerSequence <= last {
				t.Fatalf("broker sequence %d not above %d", r.brokerSequence, last)
			}
			last = r.brokerSequence
			msg, err := before.GetMsg(ctx, uint64(r.brokerSequence))
			if err != nil {
				t.Fatal(err)
			}
			want, err := e.Encode(testPolicy(), fillSubject)
			if err != nil {
				t.Fatal(err)
			}
			if msg.Subject != fillSubject || msg.Header.Get(jetstream.MsgIDHeader) != e.EventID || !bytes.Equal(msg.Data, want) {
				t.Fatalf("broker message %d = %s %q", r.brokerSequence, msg.Subject, msg.Header.Get(jetstream.MsgIDHeader))
			}
		}
		if err = relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if info, err = before.Info(ctx); err != nil || info.State.LastSeq != base+5 {
			t.Fatalf("stream last sequence = %d, want %d (%v)", info.State.LastSeq, base+5, err)
		}
		// A republish of a relayed event id inside the duplicate window is acknowledged without a second entry.
		ack, err := publisher.Publish(ctx, fillSubject, envelopes[0].EventID, []byte(`{}`))
		if err != nil || ack.Sequence != base+1 {
			t.Fatalf("duplicate publish ack = %+v, %v", ack, err)
		}
	})

	t.Run("ordered_requeue", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox`); err != nil {
			t.Fatal(err)
		}
		var envelopes []delivery.Envelope
		for i := 0; i < 3; i++ {
			envelopes = append(envelopes, testEnvelope("ordered-"+strconv.Itoa(i), "fill", 700+i))
		}
		enqueueCommitted(t, ctx, db, s, fillSubject, envelopes...)
		leases, err := s.Claim(ctx, relayOwner, 3, 30*time.Second)
		if err != nil || len(leases) != 3 {
			t.Fatalf("claim: %d leases, %v", len(leases), err)
		}
		if err = s.Retry(ctx, leases[0], delivery.Transient, 2, time.Second); err != nil {
			t.Fatal(err)
		}
		if err = s.ReleaseBatch(ctx, leases[1:]); err != nil {
			t.Fatal(err)
		}
		if err = s.ReleaseBatch(ctx, leases[1:2]); !errors.Is(err, delivery.ErrLease) {
			t.Fatalf("second release: %v", err)
		}
		for _, e := range envelopes[1:] {
			if r := readOutbox(t, db, e.EventID); r.state != "pending" || r.attempts != 0 || r.availableInMS > 0 {
				t.Fatalf("released row = %+v", r)
			}
		}
		// The released rows wait behind the row backing off.
		wantNoClaim(t, ctx, s)
		first := readOutbox(t, db, envelopes[0].EventID)
		time.Sleep(time.Duration(first.availableInMS+100) * time.Millisecond)
		if leases, err = s.Claim(ctx, relayOwner, 3, 30*time.Second); err != nil || len(leases) != 3 {
			t.Fatalf("reclaim: %d leases, %v", len(leases), err)
		}
		for i, l := range leases {
			if l.EventID != envelopes[i].EventID || l.Attempts != []int{2, 1, 1}[i] {
				t.Fatalf("reclaim %d = %+v", i, l)
			}
		}
		if err = s.Retry(ctx, leases[0], delivery.Transient, 2, time.Second); err != nil {
			t.Fatal(err)
		}
		if err = s.ReleaseBatch(ctx, leases[1:]); err != nil {
			t.Fatal(err)
		}
		// The exhausted row backs off twice the delay in quarantine.
		r := readOutbox(t, db, envelopes[0].EventID)
		if r.state != "quarantined" || r.failureClass != "transient" || r.attempts != 2 || r.availableInMS < 1000 || r.availableInMS > 2000 {
			t.Fatalf("exhausted row = %+v", r)
		}
		if n, err := s.Quarantined(ctx); err != nil || n != 1 {
			t.Fatalf("quarantined = %d, %v", n, err)
		}
		stream0, err := js.Stream(ctx, stream.Name)
		if err != nil {
			t.Fatal(err)
		}
		base := streamState(t, ctx, stream0).LastSeq
		relay := delivery.Relay{Store: s, Publisher: jetStreamPublisher{js: js, stream: stream.Name}, Owner: relayOwner, Batch: 10, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 2}
		// A pass publishes nothing behind the quarantined row still backing off.
		if err = relay.Pass(ctx); err != nil {
			t.Fatal(err)
		}
		for _, e := range envelopes[1:] {
			if r := readOutbox(t, db, e.EventID); r.state != "pending" || r.attempts != 0 {
				t.Fatalf("row behind the quarantined row = %+v", r)
			}
		}
		time.Sleep(time.Duration(r.availableInMS+100) * time.Millisecond)
		if err = relay.Pass(ctx); err != nil {
			t.Fatal(err)
		}
		for i, e := range envelopes {
			if r := readOutbox(t, db, e.EventID); r.state != "published" || r.brokerSequence != int64(base)+int64(i)+1 || r.attempts != 1 {
				t.Fatalf("requeued row %d = %+v", i, r)
			}
		}
		if n, err := s.Quarantined(ctx); err != nil || n != 0 {
			t.Fatalf("quarantined after requeue = %d, %v", n, err)
		}
	})

	perps, err := js.Stream(ctx, stream.Name)
	if err != nil {
		t.Fatal(err)
	}
	// narrow refuses orderSubject, so an order row enqueued through s fails
	// to decode in its relay and is quarantined permanently.
	narrow, err := delivery.NewStore(db, "perps", delivery.Policy{Subjects: map[string]bool{fillSubject: true}, EventTypes: map[string]bool{"fill": true, "order": true}})
	if err != nil {
		t.Fatal(err)
	}
	replica := func(owner string, store *delivery.Store) delivery.Relay {
		return delivery.Relay{Store: store, Publisher: jetStreamPublisher{js: js, stream: stream.Name}, Owner: owner, Batch: 10, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 2}
	}
	const openRows = `SELECT count(*) FROM perps.delivery_outbox WHERE state='pending' OR (state='quarantined' AND failure_class<>'transient' AND broker_sequence IS NULL)`

	t.Run("exhausted_row_holds_batch", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox`); err != nil {
			t.Fatal(err)
		}
		head, next := testEnvelope("exhausted-0", "fill", 800), testEnvelope("exhausted-1", "fill", 801)
		enqueueCommitted(t, ctx, db, s, fillSubject, head, next)
		// The head row was reclaimed past its budget after leases expired.
		if _, err := db.ExecContext(ctx, `UPDATE perps.delivery_outbox SET attempts=5 WHERE event_id=$1`, head.EventID); err != nil {
			t.Fatal(err)
		}
		base := streamState(t, ctx, perps).LastSeq
		if err := replica("relay-a", s).RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if r := readOutbox(t, db, head.EventID); r.state != "quarantined" || r.failureClass != "transient" || r.attempts != 6 {
			t.Fatalf("exhausted row = %+v", r)
		}
		// Neither this pass nor another replica publishes a row behind it.
		if err := replica("relay-b", s).Pass(ctx); err != nil {
			t.Fatal(err)
		}
		if r := readOutbox(t, db, next.EventID); r.state != "pending" || r.attempts != 0 || r.published {
			t.Fatalf("row behind the exhausted row = %+v", r)
		}
		if last := streamState(t, ctx, perps).LastSeq; last != base {
			t.Fatalf("stream last sequence %d, want %d: a row overtook the exhausted one", last, base)
		}
	})

	t.Run("skipped_row_control_message", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox`); err != nil {
			t.Fatal(err)
		}
		first, skipped := testEnvelope("skip-0", "fill", 900), testEnvelope("skip-1", "order", 901)
		later := []delivery.Envelope{testEnvelope("skip-2", "fill", 902), testEnvelope("skip-3", "fill", 903)}
		enqueueCommitted(t, ctx, db, s, fillSubject, first)
		enqueueCommitted(t, ctx, db, s, orderSubject, skipped)
		enqueueCommitted(t, ctx, db, s, fillSubject, later...)
		base := streamState(t, ctx, perps).LastSeq
		a, b := replica("relay-a", narrow), replica("relay-b", narrow)
		// Two replicas race over the outbox until every row is closed.
		deadline := time.Now().Add(30 * time.Second)
		for count(t, db, openRows) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("outbox not closed")
			}
			errs := make(chan error, 2)
			for _, r := range []delivery.Relay{a, b} {
				go func() { errs <- r.Pass(ctx) }()
			}
			for range 2 {
				if err := <-errs; err != nil && !errors.Is(err, delivery.ErrAuthorization) {
					t.Fatalf("Pass: %v", err)
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		ids := []string{first.EventID, skipped.EventID, later[0].EventID, later[1].EventID}
		var skippedSeq int64
		for i, id := range ids {
			r := readOutbox(t, db, id)
			want := "published"
			if i == 1 {
				want = "quarantined"
				if r.failureClass != "schema_violation" || r.published {
					t.Fatalf("skipped row = %+v", r)
				}
			}
			if r.state != want || r.brokerStream != stream.Name || r.brokerSequence != int64(base)+int64(i)+1 {
				t.Fatalf("row %d = %+v", i, r)
			}
		}
		if err := db.QueryRow(`SELECT perps_seq FROM perps.delivery_outbox WHERE event_id=$1`, skipped.EventID).Scan(&skippedSeq); err != nil {
			t.Fatal(err)
		}
		msg, err := perps.GetMsg(ctx, base+2)
		if err != nil {
			t.Fatal(err)
		}
		seq := strconv.FormatInt(skippedSeq, 10)
		if msg.Subject != "perps.125.control" || msg.Header.Get(delivery.HeaderControl) != delivery.ControlSkipped || msg.Header.Get("Perps-Seq") != seq || msg.Header.Get(jetstream.MsgIDHeader) != "control-"+seq {
			t.Fatalf("control message = %s %v", msg.Subject, msg.Header)
		}
		if last := streamState(t, ctx, perps).LastSeq; last != base+4 {
			t.Fatalf("stream last sequence %d, want %d: one control message and three rows", last, base+4)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE state='quarantined' AND failure_class='schema_violation'`); n != 1 {
			t.Fatalf("%d permanently quarantined rows, want 1", n)
		}
	})

	t.Run("control_republish_after_lost_lease", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox`); err != nil {
			t.Fatal(err)
		}
		skipped, next := testEnvelope("lost-0", "order", 1000), testEnvelope("lost-1", "fill", 1001)
		enqueueCommitted(t, ctx, db, s, orderSubject, skipped)
		enqueueCommitted(t, ctx, db, s, fillSubject, next)
		base := streamState(t, ctx, perps).LastSeq
		if err := replica("relay-a", narrow).RunOnce(ctx); !errors.Is(err, delivery.ErrAuthorization) {
			t.Fatalf("RunOnce over the undecodable row: %v", err)
		}
		if r := readOutbox(t, db, next.EventID); r.state != "pending" || r.attempts != 0 {
			t.Fatalf("row behind the skipped row = %+v", r)
		}
		leases, err := narrow.RelayClaim(ctx, "relay-crashed", 10, time.Second)
		if err != nil || len(leases) != 2 || !leases[0].Control || leases[0].EventID != skipped.EventID || leases[1].Control {
			t.Fatalf("relay claim = %+v, %v", leases, err)
		}
		// The replica persists the control message and stops before recording it.
		res := jetStreamPublisher{js: js, stream: stream.Name}.PublishBatch(ctx, leases[:1])
		if res[0].Err != nil || res[0].Ack.Sequence != base+1 {
			t.Fatalf("control publication = %+v", res[0])
		}
		time.Sleep(1100 * time.Millisecond)
		if err := replica("relay-b", narrow).Pass(ctx); err != nil {
			t.Fatal(err)
		}
		if err := narrow.MarkPublished(ctx, leases[0], res[0].Ack); !errors.Is(err, delivery.ErrLease) {
			t.Fatalf("record by the expired lease: %v", err)
		}
		if r := readOutbox(t, db, skipped.EventID); r.state != "quarantined" || r.brokerSequence != int64(base)+1 {
			t.Fatalf("skipped row = %+v", r)
		}
		if r := readOutbox(t, db, next.EventID); r.state != "published" || r.brokerSequence != int64(base)+2 {
			t.Fatalf("row behind the skipped row = %+v", r)
		}
		if last := streamState(t, ctx, perps).LastSeq; last != base+2 {
			t.Fatalf("stream last sequence %d, want %d: the control message was stored twice", last, base+2)
		}
		if n := count(t, db, openRows); n != 0 {
			t.Fatalf("%d open rows", n)
		}
	})
}

func streamState(t *testing.T, ctx context.Context, s jetstream.Stream) jetstream.StreamState {
	t.Helper()
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info.State
}

func TestStore(t *testing.T) {
	dsn := os.Getenv(testDatabaseEnv)
	if dsn == "" {
		t.Fatal("TestStore requires " + testDatabaseEnv)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pq.ParseURL(dsn)
		if err != nil {
			t.Fatal(err)
		}
	}
	admin, err := store.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	name := "delivery_test_" + strings.ToLower(rand.Text())
	if _, err = admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + pq.QuoteIdentifier(name)); err != nil {
			t.Error(err)
		}
	})
	t.Setenv(testDatabaseEnv, dsn+" dbname="+name)
	t.Run("delivery", TestDeliveryStore)
	t.Run("monotonic_seq", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		db, err := store.Open(os.Getenv(testDatabaseEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		if err = store.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `TRUNCATE perps.delivery_outbox RESTART IDENTITY;
 ALTER TABLE perps.delivery_outbox ALTER COLUMN created_at SET DEFAULT transaction_timestamp()`); err != nil {
			t.Fatal(err)
		}
		s, err := delivery.NewStore(db, "perps", testPolicy())
		if err != nil {
			t.Fatal(err)
		}
		var envelopes []delivery.Envelope
		for block := 400; block < 410; block++ {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			for log := 0; log < 5; log++ {
				id := delivery.Hash([]byte(strconv.Itoa(block) + ":" + strconv.Itoa(log)))
				e := testEnvelope(id, "fill", block)
				e.SourceOrder.TransactionIndex = strconv.Itoa(log / 2)
				e.SourceOrder.LogIndex = strconv.Itoa(log)
				sequence, err := s.Enqueue(ctx, tx, fillSubject, e)
				if err != nil || sequence != int64(len(envelopes)+1) {
					t.Fatalf("enqueue %d: sequence %d, error %v", len(envelopes)+1, sequence, err)
				}
				var stored int64
				if err = tx.QueryRowContext(ctx, `SELECT perps_seq FROM perps.delivery_outbox WHERE event_id=$1`, id).Scan(&stored); err != nil || stored != sequence {
					t.Fatalf("stored sequence %d, want %d: %v", stored, sequence, err)
				}
				envelopes = append(envelopes, e)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		if n := count(t, db, `SELECT count(DISTINCT created_at) FROM perps.delivery_outbox WHERE perps_seq BETWEEN 1 AND 5`); n != 1 {
			t.Fatalf("first block has %d creation timestamps, want 1", n)
		}
		var hashOrdered bool
		if err = db.QueryRowContext(ctx, `SELECT array_agg(event_id ORDER BY event_id) = array_agg(event_id ORDER BY perps_seq) FROM perps.delivery_outbox WHERE perps_seq BETWEEN 1 AND 5`).Scan(&hashOrdered); err != nil || hashOrdered {
			t.Fatalf("hash order matches enqueue order: %t, %v", hashOrdered, err)
		}
		publisher := jetStreamPublisher{js: startBroker(t, ctx), stream: stream.Name}
		for offset := 0; offset < len(envelopes); {
			leases, err := s.Claim(ctx, relayOwner, 7, 30*time.Second)
			want := min(7, len(envelopes)-offset)
			if err != nil || len(leases) != want {
				t.Fatalf("claim at %d: %d leases, want %d: %v", offset, len(leases), want, err)
			}
			for i, lease := range leases {
				e := envelopes[offset+i]
				wire, err := e.Encode(testPolicy(), fillSubject)
				if err != nil {
					t.Fatal(err)
				}
				if lease.PerpsSeq != int64(offset+i+1) || lease.EventID != e.EventID || lease.Subject != fillSubject || lease.Fence != 1 || lease.Attempts != 1 || !bytes.Equal(lease.Wire, wire) {
					t.Fatalf("claim at %d: %+v", offset+i, lease)
				}
				ack, err := publisher.Publish(ctx, lease.Subject, lease.EventID, lease.Wire)
				if err != nil || ack.Stream != stream.Name || ack.Sequence != uint64(offset+i+1) {
					t.Fatalf("publish at %d: acknowledgement %+v, error %v", offset+i, ack, err)
				}
				if err = s.MarkPublished(ctx, lease, ack); err != nil {
					t.Fatal(err)
				}
				if row := readOutbox(t, db, lease.EventID); row.state != "published" || row.brokerStream != ack.Stream || row.brokerSequence != int64(ack.Sequence) {
					t.Fatalf("published row at %d: %+v", offset+i, row)
				}
			}
			offset += len(leases)
		}
		wantNoClaim(t, ctx, s)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if sequence, err := s.Enqueue(ctx, tx, fillSubject, envelopes[0]); err != nil || sequence != 1 {
			t.Fatalf("duplicate sequence %d, want 1: %v", sequence, err)
		}
		changed := envelopes[0]
		changed.SourceOrder.Height = "410"
		if sequence, err := s.Enqueue(ctx, tx, fillSubject, changed); !errors.Is(err, delivery.ErrIntegrity) || sequence != 0 {
			t.Fatalf("conflicting sequence %d, error %v, want 0 and ErrIntegrity", sequence, err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox`); n != 50 {
			t.Fatalf("outbox contains %d rows, want 50", n)
		}
		rolledBack := testEnvelope("rolled-back", "fill", 410)
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		sequence, err := s.Enqueue(ctx, tx, fillSubject, rolledBack)
		if err != nil || sequence != 53 {
			t.Fatalf("uncommitted sequence %d, want 53: %v", sequence, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox WHERE event_id=$1`, rolledBack.EventID); n != 0 {
			t.Fatalf("rolled back enqueue left %d rows", n)
		}
		enqueueCommitted(t, ctx, db, s, fillSubject, rolledBack)
		lease := claimOne(t, ctx, s)
		if lease.PerpsSeq != 54 || lease.EventID != rolledBack.EventID {
			t.Fatalf("claim after rollback = %+v, want sequence 54", lease)
		}
	})
}
