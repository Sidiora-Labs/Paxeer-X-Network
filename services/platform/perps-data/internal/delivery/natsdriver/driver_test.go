package natsdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
)

const (
	testStream  = "PERPS_V1"
	fillSubject = "perps.125.7.fill"
	forgedAck   = "forged.ack"
)

var (
	hashA = "0xaa" + strings.Repeat("0", 62)
	hashB = "0xbb" + strings.Repeat("0", 62)
)

func inProcessBroker(t *testing.T) *nats.Conn {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("in-process broker not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func createStream(t *testing.T, nc *nats.Conn) jetstream.Stream {
	t.Helper()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: testStream, Subjects: []string{"perps.>"}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: 2 * time.Minute, Discard: jetstream.DiscardNew, DenyDelete: true, DenyPurge: true})
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func streamMessages(t *testing.T, stream jetstream.Stream) uint64 {
	t.Helper()
	info, err := stream.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}

func envelopeWire(t *testing.T, eventID string) (string, []byte) {
	t.Helper()
	token, err := MsgID(eventID)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"size_raw":"9007199254740993123456789"}`)
	e := delivery.Envelope{EventID: token, EventType: "fill", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: "500", TransactionIndex: "2", LogIndex: "0"}, EmittedAt: time.Unix(1791504000, 0).UTC(), TraceID: token, PayloadHash: delivery.Hash(payload), Payload: payload}
	wire, err := e.Encode(delivery.Policy{Subjects: map[string]bool{fillSubject: true}, EventTypes: map[string]bool{"fill": true}}, fillSubject)
	if err != nil {
		t.Fatal(err)
	}
	return token, wire
}

// forgeAck answers every request on forgedAck with the given acknowledgement body,
// standing in for a broker that misreports persistence.
func forgeAck(t *testing.T, nc *nats.Conn, body string) {
	t.Helper()
	sub, err := nc.Subscribe(forgedAck, func(m *nats.Msg) { _ = m.Respond([]byte(body)) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err = nc.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestNATSDriver(t *testing.T) {
	ctx := context.Background()

	t.Run("publish_ack", func(t *testing.T) {
		nc := inProcessBroker(t)
		stream := createStream(t, nc)
		publisher, err := NewPublisher(nc, testStream)
		if err != nil {
			t.Fatal(err)
		}
		token, wire := envelopeWire(t, "125:500:"+hashA+":2:0:0")
		ack, err := publisher.Publish(ctx, fillSubject, token, wire)
		if err != nil {
			t.Fatal(err)
		}
		if ack != (delivery.PersistenceAck{Stream: testStream, Sequence: 1}) {
			t.Fatalf("ack = %+v", ack)
		}
		raw, err := stream.GetMsg(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if raw.Subject != fillSubject || raw.Header.Get(jetstream.MsgIDHeader) != token || string(raw.Data) != string(wire) {
			t.Fatalf("stored message = %s %q %q", raw.Subject, raw.Header.Get(jetstream.MsgIDHeader), raw.Data)
		}
		for _, bad := range []struct{ subject, id string }{{"perps..fill", token}, {fillSubject, "125:500:" + hashA + ":2:0:0"}, {fillSubject, ""}} {
			if _, err = publisher.Publish(ctx, bad.subject, bad.id, wire); err == nil || err.Error() != "natsdriver: invalid publication" {
				t.Fatalf("publish %q %q: %v", bad.subject, bad.id, err)
			}
		}
		if _, err = publisher.Publish(ctx, fillSubject, token, nil); err == nil || err.Error() != "natsdriver: invalid publication" {
			t.Fatalf("empty wire: %v", err)
		}
		if _, err = NewPublisher(nc, "perps.v1"); err == nil || err.Error() != "natsdriver: valid connection and stream required" {
			t.Fatalf("invalid stream name: %v", err)
		}
		if got := streamMessages(t, stream); got != 1 {
			t.Fatalf("stream holds %d messages", got)
		}
	})

	t.Run("refuses_foreign_stream", func(t *testing.T) {
		nc := inProcessBroker(t)
		stream := createStream(t, nc)
		foreign, err := NewPublisher(nc, "FOREIGN")
		if err != nil {
			t.Fatal(err)
		}
		token, wire := envelopeWire(t, "125:500:"+hashA+":2:0:0")
		_, err = foreign.Publish(ctx, fillSubject, token, wire)
		var apiErr *jetstream.APIError
		if !errors.As(err, &apiErr) || apiErr.ErrorCode != jetstream.ErrorCode(server.JSStreamNotMatchErr) {
			t.Fatalf("expected stream mismatch refusal, got %v", err)
		}
		if got := streamMessages(t, stream); got != 0 {
			t.Fatalf("stream holds %d messages after refusal", got)
		}
		forgeAck(t, nc, `{"stream":"FOREIGN","seq":9}`)
		publisher, err := NewPublisher(nc, testStream)
		if err != nil {
			t.Fatal(err)
		}
		ack, err := publisher.Publish(ctx, forgedAck, token, wire)
		if err == nil || err.Error() != "natsdriver: invalid persistence acknowledgement" || ack != (delivery.PersistenceAck{}) {
			t.Fatalf("foreign acknowledgement accepted: %+v %v", ack, err)
		}
	})

	t.Run("refuses_zero_sequence", func(t *testing.T) {
		nc := inProcessBroker(t)
		forgeAck(t, nc, `{"stream":"`+testStream+`","seq":0}`)
		publisher, err := NewPublisher(nc, testStream)
		if err != nil {
			t.Fatal(err)
		}
		token, wire := envelopeWire(t, "125:500:"+hashA+":2:0:0")
		ack, err := publisher.Publish(ctx, forgedAck, token, wire)
		if err == nil || err.Error() != "natsdriver: invalid persistence acknowledgement" || ack != (delivery.PersistenceAck{}) {
			t.Fatalf("zero sequence accepted: %+v %v", ack, err)
		}
	})

	t.Run("dedup_msg_id", func(t *testing.T) {
		first := "125:500:" + hashA + ":2:0:0"
		rewritten := "125:500:" + hashB + ":2:0:0"
		sum := sha256.Sum256([]byte(first))
		if got, err := MsgID(first); err != nil || got != hex.EncodeToString(sum[:]) {
			t.Fatalf("MsgID(%q) = %q, %v", first, got, err)
		}
		blockEvent := "125:500:" + hashA + ":-1:0:0"
		blockSum := sha256.Sum256([]byte(blockEvent))
		if got, err := MsgID(blockEvent); err != nil || got != hex.EncodeToString(blockSum[:]) {
			t.Fatalf("MsgID(%q) = %q, %v", blockEvent, got, err)
		}
		for _, bad := range []string{
			"",
			"125:500:" + strings.ToUpper(hashA) + ":2:0:0",
			"125:500:" + hashA[2:] + ":2:0:0",
			"125:500:" + hashA + ":2:0",
			"125:0500:" + hashA + ":2:0:0",
			"125:500:" + hashA + ":2:0:0:1",
			"125:-1:" + hashA + ":2:0:0",
			"125:500:" + hashA + ":-2:0:0",
			"125:500:" + hashA + ":-0:0:0",
			"125:500:" + hashA + ":-01:0:0",
			"125:500:" + hashA + ":2:-1:0",
			"125:500:" + hashA + ":2:0:-1",
		} {
			if got, err := MsgID(bad); !errors.Is(err, ErrEventID) || got != "" {
				t.Fatalf("MsgID(%q) = %q, %v", bad, got, err)
			}
		}

		nc := inProcessBroker(t)
		stream := createStream(t, nc)
		publisher, err := NewPublisher(nc, testStream)
		if err != nil {
			t.Fatal(err)
		}
		tokenA, wireA := envelopeWire(t, first)
		tokenB, wireB := envelopeWire(t, rewritten)
		if tokenA == tokenB {
			t.Fatal("rewritten block hash maps to the same message id")
		}
		ack1, err := publisher.Publish(ctx, fillSubject, tokenA, wireA)
		if err != nil {
			t.Fatal(err)
		}
		ack2, err := publisher.Publish(ctx, fillSubject, tokenA, wireA)
		if err != nil {
			t.Fatal(err)
		}
		if ack1 != (delivery.PersistenceAck{Stream: testStream, Sequence: 1}) || ack2 != ack1 {
			t.Fatalf("duplicate acknowledgements %+v %+v", ack1, ack2)
		}
		ack3, err := publisher.Publish(ctx, fillSubject, tokenB, wireB)
		if err != nil {
			t.Fatal(err)
		}
		if ack3 != (delivery.PersistenceAck{Stream: testStream, Sequence: 2}) {
			t.Fatalf("rewritten acknowledgement %+v", ack3)
		}
		if got := streamMessages(t, stream); got != 2 {
			t.Fatalf("stream holds %d messages, want 2", got)
		}
		raw, err := stream.GetMsg(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if raw.Header.Get(jetstream.MsgIDHeader) != tokenB || string(raw.Data) != string(wireB) {
			t.Fatalf("second entry carries %q", raw.Header.Get(jetstream.MsgIDHeader))
		}
	})

	t.Run("consumer_fetch", func(t *testing.T) {
		nc := inProcessBroker(t)
		createStream(t, nc)
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatal(err)
		}
		cfg := ConsumerConfig{Stream: testStream, Durable: "gateway_v1", Subject: fillSubject, Batch: 2, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
		brokerCfg, err := cfg.BrokerConsumerConfig()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = js.CreateConsumer(ctx, testStream, brokerCfg); err != nil {
			t.Fatal(err)
		}
		differing := cfg
		differing.MaxDeliver = 11
		if _, err = NewConsumer(ctx, nc, differing); err == nil || err.Error() != "natsdriver: durable configuration differs from reviewed configuration" {
			t.Fatalf("differing durable accepted: %v", err)
		}
		invalid := cfg
		invalid.Batch = 65
		if _, err = invalid.BrokerConsumerConfig(); err == nil || err.Error() != "natsdriver: invalid bounded durable configuration" {
			t.Fatalf("unbounded batch accepted: %v", err)
		}
		consumer, err := NewConsumer(ctx, nc, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(consumer.Close)

		empty, err := consumer.Fetch(ctx)
		if err != nil || len(empty) != 0 {
			t.Fatalf("empty fetch = %d messages, %v", len(empty), err)
		}

		publisher, err := NewPublisher(nc, testStream)
		if err != nil {
			t.Fatal(err)
		}
		tokenA, wireA := envelopeWire(t, "125:500:"+hashA+":2:0:0")
		tokenB, wireB := envelopeWire(t, "125:500:"+hashB+":2:0:0")
		for _, m := range []struct {
			token string
			wire  []byte
		}{{tokenA, wireA}, {tokenB, wireB}} {
			if _, err = publisher.Publish(ctx, fillSubject, m.token, m.wire); err != nil {
				t.Fatal(err)
			}
		}
		messages, err := consumer.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 2 {
			t.Fatalf("fetched %d messages", len(messages))
		}
		for i, want := range []struct {
			token string
			wire  []byte
		}{{tokenA, wireA}, {tokenB, wireB}} {
			m := messages[i]
			if m.Subject != fillSubject || m.EventID != want.token || string(m.Wire) != string(want.wire) || m.Stream != testStream || m.Sequence != uint64(i+1) || m.Deliveries != 1 {
				t.Fatalf("message %d = %+v", i, m)
			}
			position, err := m.BrokerPosition()
			if err != nil {
				t.Fatal(err)
			}
			if position != (BrokerPosition{Stream: testStream, Sequence: uint64(i + 1), Subject: fillSubject, EventID: want.token, WireSHA256: delivery.Hash(want.wire)}) {
				t.Fatalf("position %d = %+v", i, position)
			}
			if err = m.Ack(ctx); err != nil {
				t.Fatal(err)
			}
		}
		info, err := js.Consumer(ctx, testStream, cfg.Durable)
		if err != nil {
			t.Fatal(err)
		}
		state, err := info.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.AckFloor.Stream != 2 || state.NumAckPending != 0 || state.NumPending != 0 {
			t.Fatalf("consumer state ack floor %d pending %d remaining %d", state.AckFloor.Stream, state.NumAckPending, state.NumPending)
		}

		tokenC, wireC := envelopeWire(t, "125:501:"+hashA+":0:0:0")
		if _, err = publisher.Publish(ctx, fillSubject, tokenC, wireC); err != nil {
			t.Fatal(err)
		}
		refusal := errors.New("projection refused")
		var consumed []string
		err = consumer.Run(ctx, func(_ context.Context, m *Message) error {
			consumed = append(consumed, m.EventID)
			return refusal
		})
		if !errors.Is(err, refusal) || err.Error() != "natsdriver: transactional consume: projection refused" || len(consumed) != 1 || consumed[0] != tokenC {
			t.Fatalf("run = %v consumed %v", err, consumed)
		}
		if err = consumer.Run(ctx, nil); err == nil || err.Error() != "natsdriver: transactional consumer required" {
			t.Fatalf("nil consume accepted: %v", err)
		}

		consumer.Close()
		if _, err = consumer.Fetch(ctx); !errors.Is(err, ErrClosed) {
			t.Fatalf("fetch after close = %v", err)
		}
	})

	t.Run("ordered_start_sequence", func(t *testing.T) {
		nc := inProcessBroker(t)
		createStream(t, nc)
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatal(err)
		}
		const markSubject = "perps.125.7.mark"
		published := []struct{ subject, eventID string }{
			{fillSubject, "125:500:" + hashA + ":2:0:0"},
			{markSubject, "125:500:" + hashA + ":3:0:0"},
			{fillSubject, "125:501:" + hashB + ":0:0:0"},
		}
		tokens := make([]string, len(published))
		for i, p := range published {
			token, wire := envelopeWire(t, p.eventID)
			msg := &nats.Msg{Subject: p.subject, Data: wire, Header: nats.Header{}}
			msg.Header.Set("Perps-Seq", strconv.Itoa(i+1))
			ack, err := js.PublishMsg(ctx, msg, jetstream.WithMsgID(token), jetstream.WithExpectStream(testStream))
			if err != nil {
				t.Fatal(err)
			}
			if ack.Sequence != uint64(i+1) {
				t.Fatalf("publication %d stored at %d", i, ack.Sequence)
			}
			tokens[i] = token
		}
		cfg := ConsumerConfig{Stream: testStream, Subject: "perps.125.>", StartSequence: 2, Batch: 4, FetchWait: 250 * time.Millisecond}
		for _, bad := range []func(*ConsumerConfig){
			func(c *ConsumerConfig) { c.Durable = "gateway_v1" },
			func(c *ConsumerConfig) { c.MaxAckPending = 4 },
		} {
			refused := cfg
			bad(&refused)
			if _, err = NewConsumer(ctx, nc, refused); err == nil || err.Error() != "natsdriver: invalid bounded durable configuration" {
				t.Fatalf("ordered config %+v accepted: %v", refused, err)
			}
		}
		consumer, err := NewConsumer(ctx, nc, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(consumer.Close)
		var messages []*Message
		for deadline := time.Now().Add(5 * time.Second); len(messages) < 2 && time.Now().Before(deadline); {
			fetched, err := consumer.Fetch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			messages = append(messages, fetched...)
		}
		if len(messages) != 2 {
			t.Fatalf("fetched %d messages, want 2", len(messages))
		}
		if more, err := consumer.Fetch(ctx); err != nil || len(more) != 0 {
			t.Fatalf("fetch past the stream head = %d messages, %v", len(more), err)
		}
		for i, m := range messages {
			n := i + 1
			if m.Sequence != uint64(n+1) || m.Subject != published[n].subject || m.EventID != tokens[n] || m.Stream != testStream {
				t.Fatalf("message %d = %+v", i, m)
			}
			if m.Header.Get(jetstream.MsgIDHeader) != tokens[n] || m.Header.Get("Perps-Seq") != strconv.Itoa(n+1) {
				t.Fatalf("message %d headers = %v", i, m.Header)
			}
			if err = m.Ack(ctx); err == nil {
				t.Fatalf("ordered message %d acknowledged", i)
			}
		}
	})
}
