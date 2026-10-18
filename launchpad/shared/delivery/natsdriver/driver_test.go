package natsdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func realBroker(t *testing.T) (*nats.Conn, jetstream.JetStream) {
	nc, js, _ := realBrokerProcess(t)
	return nc, js
}

func realBrokerProcess(t *testing.T) (*nats.Conn, jetstream.JetStream, *exec.Cmd) {
	t.Helper()
	binary := os.Getenv("NATS_SERVER_BINARY")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("nats-server")
		if err != nil {
			t.Fatal("real file-backed nats-server required: ", err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	config := fmt.Sprintf("host: 127.0.0.1\nport: %d\njetstream { store_dir: %q }\nauthorization { users: [{ user: adapter, password: adapter-test-secret }] }\n", port, filepath.Join(directory, "store"))
	configPath := filepath.Join(directory, "server.conf")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-c", configPath)
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	var processError error
	go func() { processError = command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
		_ = log.Close()
	})
	address := fmt.Sprintf("nats://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	var nc *nats.Conn
	for time.Now().Before(deadline) {
		nc, err = nats.Connect(address, nats.UserInfo("adapter", "adapter-test-secret"), nats.NoReconnect(), nats.Timeout(100*time.Millisecond))
		if err == nil {
			break
		}
		select {
		case <-done:
			t.Fatalf("real broker exited: %v", processError)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, command
}

func provision(t *testing.T, nc *nats.Conn, js jetstream.JetStream) (*Publisher, *Consumer, ConsumerConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := ConsumerConfig{Stream: "ADAPTER", Durable: "projection_v1", Subject: "accounting.canonical", Batch: 1, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: cfg.Stream, Subjects: []string{cfg.Subject}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	brokerCfg, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, cfg.Stream, brokerCfg); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(nc, cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(consumer.Close)
	return publisher, consumer, cfg
}

func TestRealFileBackedPublishDedupDurableAndDoubleAck(t *testing.T) {
	nc, js := realBroker(t)
	publisher, consumer, cfg := provision(t, nc, js)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	body := []byte(`{"immutable":"original bytes","amount":"123456789012345678901234567890"}`)
	first, err := publisher.Publish(ctx, cfg.Subject, "event_1", body)
	if err != nil || first.Stream != cfg.Stream || first.Sequence == 0 {
		t.Fatalf("persistence ack=%+v err=%v", first, err)
	}
	duplicate, err := publisher.Publish(ctx, cfg.Subject, "event_1", body)
	if err != nil || duplicate != first {
		t.Fatalf("dedup ack=%+v first=%+v err=%v", duplicate, first, err)
	}
	wrong, err := NewPublisher(nc, "WRONG_STREAM")
	if err != nil {
		t.Fatal(err)
	}
	if ack, err := wrong.Publish(ctx, cfg.Subject, "event_wrong", body); err == nil || ack.Sequence != 0 {
		t.Fatalf("wrong expected stream accepted: %+v %v", ack, err)
	}
	messages, err := consumer.Fetch(ctx)
	if err != nil || len(messages) != 1 {
		t.Fatalf("fetch=%d err=%v", len(messages), err)
	}
	original := messages[0]
	if original.EventID != "event_1" || original.Stream != first.Stream || original.Sequence != first.Sequence || !bytes.Equal(original.Wire, body) {
		t.Fatalf("original bytes or broker identity changed: %+v", original)
	}
	consumer.Close()
	reopened, err := NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var redelivery *Message
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		fetched, err := reopened.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(fetched) > 0 {
			redelivery = fetched[0]
			break
		}
	}
	if redelivery == nil || redelivery.Deliveries < 2 || !bytes.Equal(redelivery.Wire, body) {
		t.Fatal("unacknowledged message did not survive durable restart")
	}
	if err = redelivery.Ack(ctx); err != nil {
		t.Fatal(err)
	}
	durable, err := js.Consumer(ctx, cfg.Stream, cfg.Durable)
	if err != nil {
		t.Fatal(err)
	}
	info, err := durable.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumAckPending != 0 || info.AckFloor.Stream != first.Sequence {
		t.Fatalf("DoubleAck did not confirm broker receipt: %+v", info)
	}
	stream, err := js.Stream(ctx, cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	streamInfo, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if streamInfo.Config.Storage != jetstream.FileStorage || streamInfo.State.Msgs != 1 {
		t.Fatalf("dedup/file storage violated: %+v", streamInfo)
	}
}

func TestRealConsumerCancellationAndConfigurationFailure(t *testing.T) {
	nc, js := realBroker(t)
	publisher, consumer, cfg := provision(t, nc, js)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := consumer.Fetch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancel was not bounded")
	}
	cfg.MaxAckPending++
	if _, err := NewConsumer(context.Background(), nc, cfg); err == nil {
		t.Fatal("configuration mismatch accepted")
	}
	if ack, err := publisher.Publish(ctx, cfg.Subject, "cancelled", []byte("original")); err == nil || ack.Sequence != 0 {
		t.Fatalf("cancelled publish=%+v %v", ack, err)
	}
	if ack, err := publisher.Publish(context.Background(), "bad.*", "bad", []byte("original")); err == nil || ack.Sequence != 0 {
		t.Fatalf("wildcard subject accepted: %+v %v", ack, err)
	}
	consumer.Close()
	if _, err := consumer.Fetch(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed=%v", err)
	}
	if err := (&Message{}).Ack(context.Background()); err == nil {
		t.Fatal("synthetic message acknowledged")
	}
}

func TestRealTerminalBatchErrorIsReturned(t *testing.T) {
	nc, js := realBroker(t)
	_, consumer, cfg := provision(t, nc, js)
	cfg.FetchWait = time.Second
	consumer.Close()
	replacement, err := NewConsumer(context.Background(), nc, consumer.config)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	result := make(chan error, 1)
	go func() { _, err := replacement.Fetch(context.Background()); result <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	durable, err := js.Consumer(ctx, cfg.Stream, cfg.Durable)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Second)
	for {
		info, err := durable.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumWaiting > 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("pull did not reach real broker")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err = js.DeleteConsumer(ctx, cfg.Stream, cfg.Durable); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "consumer") {
			t.Fatalf("terminal batch error lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal error did not end fetch")
	}
}

const phase3Subject = "delivery.phase3.swap"

func phase3Policy() delivery.Policy {
	return delivery.Policy{Subjects: map[string]bool{phase3Subject: true}, EventTypes: map[string]bool{"Swap": true}}
}
func phase3Envelope(id string) delivery.Envelope {
	payload := json.RawMessage(`{"amountRaw":"9007199254740993123456789","pool":"0x0000000000000000000000000000000000000001"}`)
	return delivery.Envelope{EventID: id, EventType: "Swap", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: "1", TransactionIndex: "0", LogIndex: "1"}, EmittedAt: time.Unix(1791504000, 0).UTC(), TraceID: id, PayloadHash: delivery.Hash(payload), Payload: payload}
}
func phase3Database(t *testing.T, ctx context.Context, schema string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(os.Getenv("DELIVERY_TEST_DSN"))
	if err != nil || config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.Port != 25432 || config.ConnConfig.Database != "kindle_fixture" || !strings.HasPrefix(schema, "kd_") || !identifier.MatchString(schema) || len(schema) > 63 {
		t.Fatal("explicit private PostgreSQL fixture authority required")
	}
	config.MaxConns = 4
	if mode := os.Getenv("DELIVERY_PHASE3_PROCESS"); mode != "" {
		config.ConnConfig.RuntimeParams["application_name"] = "delivery-phase3-" + mode
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
func phase3Config() ConsumerConfig {
	return ConsumerConfig{Stream: "PHASE3", Durable: "phase3_projection", Subject: phase3Subject, Batch: 1, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 100, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
}
func phase3Produce(ctx context.Context, store *delivery.Store, pool *pgxpool.Pool, schema, id string) error {
	q := pgx.Identifier{schema}.Sanitize()
	return store.Produce(ctx, []delivery.Pending{{Subject: phase3Subject, Envelope: phase3Envelope(id)}}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO "+q+".source_events VALUES($1)", id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE "+q+".source_cursor SET height=height+1")
		return err
	})
}
func TestPhase3ProcessFixture(t *testing.T) {
	mode := os.Getenv("DELIVERY_PHASE3_PROCESS")
	if mode == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := os.Getenv("DELIVERY_PHASE3_SCHEMA")
	pool := phase3Database(t, ctx, schema)
	store, err := delivery.NewStore(pool, schema, phase3Policy())
	if err != nil {
		t.Fatal(err)
	}
	if mode == "produce" {
		if err = phase3Produce(ctx, store, pool, schema, "committed-before-publication"); err != nil {
			t.Fatal(err)
		}
	} else {
		nc, err := nats.Connect(os.Getenv("DELIVERY_PHASE3_NATS"), nats.UserInfo("adapter", "adapter-test-secret"), nats.NoReconnect())
		if err != nil {
			t.Fatal(err)
		}
		defer nc.Close()
		if mode == "relay" {
			publisher, err := NewPublisher(nc, "PHASE3")
			if err != nil {
				t.Fatal(err)
			}
			relay := delivery.Relay{Store: store, Publisher: publisher, Owner: "child-relay", Batch: 1, LeaseDuration: time.Second, PublishTimeout: 250 * time.Millisecond, RetryDelay: time.Second, MaxAttempts: 10}
			if err = relay.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
		} else if mode == "fetch" {
			consumer, err := NewConsumer(ctx, nc, phase3Config())
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			messages, err := consumer.Fetch(ctx)
			if err != nil || len(messages) != 1 {
				t.Fatalf("actual child delivery unavailable: %d %v", len(messages), err)
			}
			if messages[0].EventID != "consumer-disconnected" {
				t.Fatal("wrong child identity")
			}
		} else {
			t.Fatal("unknown interruption mode")
		}
	}
	if err = os.WriteFile(os.Getenv("DELIVERY_PHASE3_SIGNAL"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	t.Fatal("parent did not interrupt process")
}
func phase3Child(t *testing.T, ctx context.Context, nc *nats.Conn, schema, mode string) (*exec.Cmd, string) {
	t.Helper()
	directory := t.TempDir()
	signal := filepath.Join(directory, "ready")
	log, err := os.Create(filepath.Join(directory, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPhase3ProcessFixture$", "-test.count=1")
	command.Env = append(os.Environ(), "DELIVERY_PHASE3_PROCESS="+mode, "DELIVERY_PHASE3_SCHEMA="+schema, "DELIVERY_PHASE3_SIGNAL="+signal, "DELIVERY_PHASE3_NATS="+nc.ConnectedUrl())
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait(); _ = log.Close() })
	return command, signal
}
func phase3Wait(t *testing.T, ctx context.Context, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("bounded interruption observation unavailable", ctx.Err())
		case <-ticker.C:
		}
	}
}
func TestPhase3ActualRelayCrashAndConsumerRestart(t *testing.T) {
	nc, js, broker := realBrokerProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := "kd_phase3_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	pool := phase3Database(t, ctx, schema)
	q := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+q+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for _, ddl := range []func(string) (string, error){delivery.SchemaDDL, delivery.ReplayDDL} {
		sql, err := ddl(schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE "+q+".source_events(event_id text PRIMARY KEY);CREATE TABLE "+q+".source_cursor(height bigint NOT NULL);INSERT INTO "+q+".source_cursor VALUES(0);CREATE TABLE "+q+".effects(consumer_id text NOT NULL,event_id text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	store, err := delivery.NewStore(pool, schema, phase3Policy())
	if err != nil {
		t.Fatal(err)
	}
	cfg := phase3Config()
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: cfg.Stream, Subjects: []string{cfg.Subject}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	brokerConfig, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, cfg.Stream, brokerConfig); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(nc, cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	relay := delivery.Relay{Store: store, Publisher: publisher, Owner: "parent-relay", Batch: 1, LeaseDuration: time.Second, PublishTimeout: 250 * time.Millisecond, RetryDelay: time.Second, MaxAttempts: 10}
	consumer, err := NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(consumer.Close)
	consume := func(message *Message) error {
		return store.Consume(ctx, "projection", "g1", message.Subject, message.Wire, message, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
			_, err := tx.Exec(ctx, "INSERT INTO "+q+".effects VALUES('projection',$1)", e.EventID)
			return err
		})
	}
	fetch := func() *Message {
		var result *Message
		phase3Wait(t, ctx, func() bool {
			messages, err := consumer.Fetch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) == 1 {
				result = messages[0]
			}
			return result != nil
		})
		return result
	}
	t.Run("KillAfterCommitBeforePublication", func(t *testing.T) {
		child, signal := phase3Child(t, ctx, nc, schema, "produce")
		phase3Wait(t, ctx, func() bool { _, err := os.Stat(signal); return err == nil })
		info, err := stream.Info(ctx)
		if err != nil || info.State.Msgs != 0 {
			t.Fatal("event published before interruption", err)
		}
		if err = child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err = child.Wait(); err == nil {
			t.Fatal("child was not killed")
		}
		if err = relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		message := fetch()
		if message.EventID != "committed-before-publication" {
			t.Fatal("committed identity lost")
		}
		if err = consume(message); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("KillAfterBrokerAckBeforeDatabaseMark", func(t *testing.T) {
		if err := phase3Produce(ctx, store, pool, schema, "ack-before-mark"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "CREATE FUNCTION "+q+".interrupt_mark() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='published' AND NEW.event_id='ack-before-mark' THEN PERFORM pg_sleep(5);END IF;RETURN NEW;END $$;CREATE TRIGGER interrupt_mark BEFORE UPDATE ON "+q+".delivery_outbox FOR EACH ROW EXECUTE FUNCTION "+q+".interrupt_mark()"); err != nil {
			t.Fatal(err)
		}
		child, _ := phase3Child(t, ctx, nc, schema, "relay")
		phase3Wait(t, ctx, func() bool {
			info, err := stream.Info(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var marking bool
			if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name='delivery-phase3-relay' AND wait_event='PgSleep' AND query LIKE '%delivery_outbox%')").Scan(&marking); err != nil {
				t.Fatal(err)
			}
			return info.State.Msgs == 2 && marking
		})
		var pending int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".delivery_outbox WHERE event_id='ack-before-mark' AND state='pending'").Scan(&pending); err != nil || pending != 1 {
			t.Fatal("database mark preceded kill", pending, err)
		}
		if err := child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err == nil {
			t.Fatal("relay child survived kill")
		}
		if _, err := pool.Exec(ctx, "DROP TRIGGER interrupt_mark ON "+q+".delivery_outbox"); err != nil {
			t.Fatal(err)
		}
		if err := relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		first := fetch()
		second := fetch()
		if first.EventID != "ack-before-mark" || second.EventID != first.EventID || !bytes.Equal(first.Wire, second.Wire) || first.Sequence == second.Sequence {
			t.Fatal("actual duplicate broker deliveries not retained")
		}
		for _, message := range []*Message{first, second} {
			if err := consume(message); err != nil {
				t.Fatal(err)
			}
		}
		var effects int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".effects WHERE event_id='ack-before-mark'").Scan(&effects); err != nil || effects != 1 {
			t.Fatal("duplicate database effect", effects, err)
		}
	})
	t.Run("ConsumerDisconnectedBeyondProcessLifetime", func(t *testing.T) {
		consumer.Close()
		if err := phase3Produce(ctx, store, pool, schema, "consumer-disconnected"); err != nil {
			t.Fatal(err)
		}
		if err := relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		child, signal := phase3Child(t, ctx, nc, schema, "fetch")
		phase3Wait(t, ctx, func() bool { _, err := os.Stat(signal); return err == nil })
		if err := child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err == nil {
			t.Fatal("consumer process survived kill")
		}
		connection, err := nats.Connect(nc.ConnectedUrl(), nats.UserInfo("adapter", "adapter-test-secret"), nats.NoReconnect())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		consumer, err = NewConsumer(ctx, connection, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		message := fetch()
		if message.EventID != "consumer-disconnected" || message.Deliveries < 2 {
			t.Fatal("silent restart gap")
		}
		if err = consume(message); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("PausedBrokerRetainsDurableRetryAcrossRelayRestart", func(t *testing.T) {
		if err := phase3Produce(ctx, store, pool, schema, "broker-paused"); err != nil {
			t.Fatal(err)
		}
		if err := broker.Process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = broker.Process.Signal(syscall.SIGCONT) })
		publicationErr := relay.RunOnce(ctx)
		if err := broker.Process.Signal(syscall.SIGCONT); err != nil {
			t.Fatal(err)
		}
		if publicationErr == nil {
			t.Fatal("paused broker acknowledged publication")
		}
		var state, class string
		var attempts int
		var available time.Time
		if err := pool.QueryRow(ctx, "SELECT state,failure_class,attempts,available_at FROM "+q+".delivery_outbox WHERE event_id='broker-paused'").Scan(&state, &class, &attempts, &available); err != nil || state != "pending" || class != "transient" || attempts != 1 {
			t.Fatal("broker outage lost durable retry", state, class, attempts, err)
		}
		restarted, err := delivery.NewStore(pool, schema, phase3Policy())
		if err != nil {
			t.Fatal(err)
		}
		relay.Store = restarted
		phase3Wait(t, ctx, func() bool { return time.Now().After(available) })
		if err = relay.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		consumer, err = NewConsumer(ctx, nc, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		durable, err := js.Consumer(ctx, cfg.Stream, cfg.Durable)
		if err != nil {
			t.Fatal(err)
		}
		delivered := 0
		for {
			info, err := durable.Info(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if info.NumPending == 0 && info.NumAckPending == 0 {
				break
			}
			message := fetch()
			if message.EventID != "broker-paused" {
				t.Fatal("outage recovery changed identity")
			}
			if err = consume(message); err != nil {
				t.Fatal(err)
			}
			delivered++
		}
		if delivered < 1 {
			t.Fatal("outage recovery delivered no committed identity")
		}
	})
	var source, outbox, effects, cursor int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".source_events),(SELECT count(*) FROM "+q+".delivery_outbox WHERE state='published'),(SELECT count(*) FROM "+q+".effects),(SELECT height FROM "+q+".source_cursor)").Scan(&source, &outbox, &effects, &cursor); err != nil || source != 4 || outbox != 4 || effects != 4 || cursor != 4 {
		t.Fatal("committed identity reconciliation failed", source, outbox, effects, cursor, err)
	}
	var missing int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+q+".source_events s LEFT JOIN "+q+".delivery_outbox o USING(event_id) LEFT JOIN "+q+".effects e USING(event_id) WHERE o.event_id IS NULL OR e.event_id IS NULL").Scan(&missing); err != nil || missing != 0 {
		t.Fatal("silent committed identity gap", missing, err)
	}
}

func TestPhase3ActualOrderedPoisonAndHundredDuplicates(t *testing.T) {
	nc, js := realBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	schema := "kd_poison_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	pool := phase3Database(t, ctx, schema)
	q := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+q+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for _, ddl := range []func(string) (string, error){delivery.SchemaDDL, delivery.PartitionSchemaDDL, delivery.ReplayDDL} {
		sql, err := ddl(schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE "+q+".effects(consumer_id text NOT NULL,event_id text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	subject := "accounting.market.v1"
	policy := delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{subject: true}}
	store, err := delivery.NewStore(pool, schema, policy)
	if err != nil {
		t.Fatal(err)
	}
	partitions, err := delivery.NewPartitionStore(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	f := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: "7", TransactionHash: "0x" + strings.Repeat("a", 64), BlockHash: "0x" + strings.Repeat("b", 64)}, ActualEmitter: "0x" + strings.Repeat("1", 40), PoolAddress: "0x" + strings.Repeat("2", 40), Token: accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 18}, Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6}, Side: "buy", TokenAmountRaw: "332665999332665", USDLAmountRaw: "5000000000", PaidInputRaw: "5000000000", FeeRaw: "15000000", InputDerivationQuality: "receipt_incomplete", FeeDisposition: "distributed_usdl", Attribution: accounting.Attribution{TransactionSender: "0x" + strings.Repeat("5", 40), Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Execution: accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Provenance: accounting.Provenance{DeploymentID: "private-125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch-1", PoolCodeEpoch: "epoch-1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: "1700000000"}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, f.Provenance.DeploymentID)
	digest, err := accounting.FillDigest(f)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	position, err := partitions.AppendTx(ctx, tx, delivery.AppendInput{ProjectionVersion: "market-v1", PartitionKind: "market", PartitionKey: "125:" + f.PoolAddress, SourceEventID: f.SourceFillID, EffectIdentity: accounting.EffectIdentity(f.SourceFillID, "market", "market-v1", "", "market", "125:"+f.PoolAddress), PayloadDigest: digest, SourceOrder: f.Position})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	publication, err := accounting.NewMarketPublication(f, []accounting.PartitionPosition{position})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := accounting.CanonicalPublicationV1(publication)
	if err != nil {
		t.Fatal(err)
	}
	envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: subject, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: f.Position.Height, TransactionIndex: f.Position.TransactionIndex, LogIndex: f.Position.LogIndex}, EmittedAt: time.Unix(1700000000, 0).UTC(), TraceID: publication.PublicationID, PayloadHash: delivery.Hash(payload), Payload: payload}
	wire, err := envelope.Encode(policy, subject)
	if err != nil {
		t.Fatal(err)
	}
	cfg := phase3Config()
	cfg.Subject = subject
	cfg.AckWait = 10 * time.Second
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: cfg.Stream, Subjects: []string{subject}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	brokerConfig, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, cfg.Stream, brokerConfig); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(nc, cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	key := delivery.ConsumerKey{ConsumerID: "ordered", ProjectionGeneration: "g1", PartitionKind: position.PartitionKind, PartitionKey: position.PartitionKey}
	lease, err := partitions.Acquire(ctx, key, "poison-owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := delivery.Hash([]byte(position.EffectIdentity))
	err = partitions.BootstrapTx(ctx, tx, delivery.BootstrapInput{Lease: lease, CheckpointDigest: accounting.Digest(checkpoint), NextSequence: "1", PreviousEffectIdentity: ""}, func(ctx context.Context, tx pgx.Tx, in delivery.BootstrapInput) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT position FROM "+q+".delivery_partition_positions WHERE projection_version=$1 AND partition_kind=$2 AND partition_key=$3 AND sequence=1", position.ProjectionVersion, position.PartitionKind, position.PartitionKey).Scan(&raw); err != nil {
			return err
		}
		var recorded accounting.PartitionPosition
		if json.Unmarshal(raw, &recorded) != nil || recorded != position || string(in.CheckpointDigest) != delivery.Hash([]byte(recorded.EffectIdentity)) {
			return delivery.ErrIntegrity
		}
		return nil
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	hooks := delivery.ConsumeHooks{Guard: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) (delivery.GuardedPosition, error) {
		return partitions.CheckAndLockTx(ctx, tx, lease, position)
	}, Effect: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope, g delivery.GuardedPosition) error {
		_, err := tx.Exec(ctx, "INSERT INTO "+q+".effects VALUES('ordered',$1)", e.EventID)
		return err
	}, Advance: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope, g delivery.GuardedPosition) error {
		return partitions.AdvanceTx(ctx, tx, g)
	}}
	fetch := func() *Message {
		var result *Message
		phase3Wait(t, ctx, func() bool {
			messages, err := consumer.Fetch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) == 1 {
				result = messages[0]
			}
			return result != nil
		})
		return result
	}
	var publishedSequence uint64
	publishFresh := func(body []byte) {
		phase3Wait(t, ctx, func() bool {
			ack, err := publisher.Publish(ctx, subject, envelope.EventID, body)
			if err != nil {
				t.Fatal(err)
			}
			if ack.Sequence <= publishedSequence {
				return false
			}
			publishedSequence = ack.Sequence
			return true
		})
	}
	for i := 0; i < 100; i++ {
		publishFresh(wire)
		message := fetch()
		if err = store.ConsumeOrderedWithWatermark(ctx, "ordered", "g1", subject, message.Wire, message, hooks); err != nil {
			t.Fatal(err)
		}
	}
	var effects, next, watermark int64
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".effects),(SELECT next_sequence FROM "+q+".delivery_partition_offsets WHERE consumer_id='ordered'),(SELECT broker_sequence FROM "+q+".delivery_consumer_watermarks WHERE consumer_id='ordered')").Scan(&effects, &next, &watermark); err != nil || effects != 1 || next != 2 || watermark != 1 {
		t.Fatal("hundred network deliveries changed one committed effect", effects, next, watermark, err)
	}
	variants := []struct {
		name   string
		class  string
		change func(*delivery.Envelope)
	}{
		{"unknown-version", "inner_schema", func(e *delivery.Envelope) { e.SchemaVersion = 2 }},
		{"payload-hash-conflict", "identity_conflict", func(e *delivery.Envelope) { e.PayloadHash = strings.Repeat("0", 64) }},
		{"same-identity-different-wire", "partition_binding", func(e *delivery.Envelope) { e.TraceID = "conflicting-trace" }},
	}
	for index, variant := range variants {
		changed := envelope
		variant.change(&changed)
		poison, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		publishFresh(poison)
		message := fetch()
		if err = store.ConsumeOrderedWithWatermark(ctx, "ordered", "g1", subject, message.Wire, message, hooks); err == nil {
			t.Fatal("poison accepted", variant.name)
		}
		restarted, err := delivery.NewStore(pool, schema, policy)
		if err != nil {
			t.Fatal(err)
		}
		if err = restarted.ConsumeOrderedWithWatermark(ctx, "ordered", "g1", subject, message.Wire, message, hooks); err == nil {
			t.Fatal("restart accepted poison", variant.name)
		}
		var persisted []byte
		var class string
		if err = pool.QueryRow(ctx, "SELECT envelope,failure_class FROM "+q+".delivery_ordered_quarantine WHERE consumer_id='ordered' AND envelope_hash=$1", delivery.Hash(poison)).Scan(&persisted, &class); err != nil || !bytes.Equal(persisted, poison) || class != variant.class {
			t.Fatal("poison quarantine missing or changed", variant.name, class, err)
		}
		if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".effects),(SELECT next_sequence FROM "+q+".delivery_partition_offsets WHERE consumer_id='ordered'),(SELECT broker_sequence FROM "+q+".delivery_consumer_watermarks WHERE consumer_id='ordered')").Scan(&effects, &next, &watermark); err != nil || effects != 1 || next != 2 || watermark != 1 {
			t.Fatal("poison advanced committed boundary", variant.name, effects, next, watermark, err)
		}
		durable, err := js.Consumer(ctx, cfg.Stream, cfg.Durable)
		if err != nil {
			t.Fatal(err)
		}
		info, err := durable.Info(ctx)
		if err != nil || info.NumAckPending != index+1 {
			t.Fatal("quarantine acknowledged poison", info, err)
		}
	}
}
