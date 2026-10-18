package auth

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestPhase2AuditDurableBridgeAndRealBroker(t *testing.T) {
	pool, schema := phase2AuthDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	subject := "security.audit." + strings.ReplaceAll(uuid.NewString(), "-", "")
	policy := delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{"security_audit": true}}
	ddl, err := delivery.SchemaDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	store, err := delivery.NewStore(pool, schema, policy)
	if err != nil {
		t.Fatal(err)
	}
	ledger := Ledger{Pool: pool, Environment: "test", AuditKey: bytes.Repeat([]byte("z"), 32)}
	for _, action := range []string{"sign_in", "refresh_reuse", "wallet.link", "revocation", "account.export", "account.delete", "permission_denied"} {
		err = ledger.RecordDenial(ctx, AuditEvent{RequestID: uuid.NewString(), Actor: goldenAddr, Action: action, Result: "denied", PolicyVersion: "2", RequestDigest: strings.Repeat("a", 64)})
		if err != nil {
			t.Fatal(err)
		}
	}
	bridge := AuditBridge{Pool: pool, Delivery: store, Subject: subject, EventType: "security_audit"}
	if staged, err := bridge.Stage(ctx, 100); err != nil || staged != 7 {
		t.Fatal("staging", staged, err)
	}
	if staged, err := bridge.Stage(ctx, 100); err != nil || staged != 0 {
		t.Fatal("duplicate staging", staged, err)
	}
	stats, err := store.Stats(ctx)
	if err != nil || stats.PendingRows != 7 {
		t.Fatal("durable pending audit", stats, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	directory := t.TempDir()
	config := filepath.Join(directory, "nats.conf")
	if err = os.WriteFile(config, []byte(fmt.Sprintf("host: 127.0.0.1\nport: %d\njetstream { store_dir: %q }\nauthorization { users: [{user: auth, password: auth-fixture-secret}] }\n", port, filepath.Join(directory, "store"))), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(directory, "nats.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command := exec.Command("nats-server", "-c", config)
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		t.Fatal("real nats-server required", err)
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	defer func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	var nc *nats.Conn
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		select {
		case <-done:
			t.Fatal("owned broker exited")
		default:
		}
		nc, err = nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", port), nats.UserInfo("auth", "auth-fixture-secret"), nats.Timeout(100*time.Millisecond))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream := "AUTH_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{subject}, Storage: jetstream.FileStorage, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	pub, err := natsdriver.NewPublisher(nc, stream)
	if err != nil {
		t.Fatal(err)
	}
	relay := delivery.Relay{Store: store, Publisher: pub, Owner: "auth_fixture", Batch: 10, LeaseDuration: 5 * time.Second, PublishTimeout: time.Second, RetryDelay: time.Second, MaxAttempts: 5}
	if err = relay.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, natsdriver.ConsumerConfig{Stream: stream, Durable: "audit_fixture", Subject: subject, Batch: 10, MaxAckPending: 20, MaxWaiting: 1, MaxDeliver: 5, FetchWait: time.Second, AckWait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	sink := AuditSink{Delivery: store, Consumer: "audit_fixture", Generation: "v1", Subject: subject, EventType: "security_audit", SinkSchema: schema}
	messages, err := consumer.Fetch(ctx)
	if err != nil || len(messages) != 7 {
		t.Fatal("durable audit broker fetch", len(messages), err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE security_audit_sink RENAME TO security_audit_sink_offline`); err != nil {
		t.Fatal(err)
	}
	if err = sink.Consume(ctx, messages[0].Wire, messages[0]); err == nil {
		t.Fatal("unavailable audit sink acknowledged")
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE security_audit_sink_offline RENAME TO security_audit_sink`); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if err = sink.Consume(ctx, message.Wire, message); err != nil {
			t.Fatal(err)
		}
	}
	replay, err := natsdriver.NewConsumer(ctx, nc, natsdriver.ConsumerConfig{Stream: stream, Durable: "audit_replay", Subject: subject, Batch: 10, MaxAckPending: 20, MaxWaiting: 1, MaxDeliver: 5, FetchWait: time.Second, AckWait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	replayed, err := replay.Fetch(ctx)
	if err != nil || len(replayed) != 7 {
		t.Fatal("real replay deliveries", len(replayed), err)
	}
	replayBefore, err := ReadAuthStateSnapshot(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	replayStarted := time.Now()
	for _, message := range replayed {
		if err = sink.Consume(ctx, message.Wire, message); err != nil {
			t.Fatal("inbox duplicate", err)
		}
	}
	replayDuration := time.Since(replayStarted)
	replayAfter, err := ReadAuthStateSnapshot(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = CompareAuthRecovery(replayBefore, replayAfter); err != nil {
		t.Fatal("audit replay changed immutable intent", err)
	}
	t.Logf("L34_PHASE4_AUDIT_REPLAY requirements=D11_028 deliveries=%d duration_ns=%d before_records=%d after_records=%d before_sha256=%s after_sha256=%s", len(replayed), replayDuration.Nanoseconds(), replayBefore.AuditRecords, replayAfter.AuditRecords, replayBefore.AuditSHA256, replayAfter.AuditSHA256)
	var audits, inbox int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM security_audit_sink),(SELECT count(*) FROM delivery_inbox)`).Scan(&audits, &inbox); err != nil || audits != 7 || inbox != 7 {
		t.Fatal(audits, inbox, err)
	}
	var containsPrivate bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM security_audit_sink WHERE convert_from(payload,'UTF8') LIKE '%signature%' OR convert_from(payload,'UTF8') LIKE '%token%' OR convert_from(payload,'UTF8') LIKE $1)`, "%"+goldenAddr+"%").Scan(&containsPrivate); err != nil || containsPrivate {
		t.Fatal("private audit data", err)
	}
	if count, _, err := bridge.Pending(ctx); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	t.Log("D11_028 audit_intents=7 broker_persisted=7 sink_rows=7 inbox_rows=7 duplicate_deliveries=7")
}

func TestPhase2VersionedBodylessDelete(t *testing.T) {
	req, _ := signedOperation(t)
	req.Envelope.Method = "DELETE"
	req.Envelope.Path = "/v1/me/watchlist/" + goldenAddr
	emptyDigest, err := BodyDigestV2(nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Envelope.BodyHash = emptyDigest
	if req.Envelope.Validate(time.Now(), "test", req.Actor, req.Action, "DELETE", req.Envelope.Path, nil) == nil {
		t.Fatal("v1 accepted empty JSON")
	}
	req.Envelope.Version = 2
	if err = req.Envelope.Validate(time.Now(), "test", req.Actor, req.Action, "DELETE", req.Envelope.Path, nil); err != nil {
		t.Fatal("v2 exact empty delete", err)
	}
	statement, err := req.Envelope.Statement()
	if err != nil || !strings.HasPrefix(statement, "KindleLaunch signed operation v2\n") {
		t.Fatal(statement, err)
	}
	if req.Envelope.Validate(time.Now(), "test", req.Actor, req.Action, "DELETE", req.Envelope.Path, []byte(`{}`)) == nil {
		t.Fatal("synthetic JSON body authorized")
	}
	req.Envelope.Method = "POST"
	if req.Envelope.Validate(time.Now(), "test", req.Actor, req.Action, "POST", req.Envelope.Path, nil) == nil {
		t.Fatal("empty-body POST accepted")
	}
}

func TestPhase2BothEthereumRecoveryValues(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := crypto.PubkeyToAddress(key.PublicKey).Hex()
	seen := map[byte]bool{}
	for index := 0; index < 128 && len(seen) < 2; index++ {
		message := fmt.Sprintf("real recovery value %d", index)
		signature, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
		if err != nil {
			t.Fatal(err)
		}
		signature[64] += 27
		result, err := VerifyEOA(wallet, message, hexutil.Encode(signature), nil)
		if err != nil || result.Method != "eip191" || result.Wallet != strings.ToLower(wallet) {
			t.Fatal(signature[64], result, err)
		}
		seen[signature[64]] = true
		if _, err = VerifyEOA(wallet, message+" altered", hexutil.Encode(signature), nil); err == nil {
			t.Fatal("altered message accepted")
		}
	}
	if !seen[27] || !seen[28] {
		t.Fatal("both recovery values not exercised", seen)
	}
}

func TestPhase2AuditRuntimeCannotRewriteHistory(t *testing.T) {
	pool, schema := phase2AuthDatabase(t)
	ctx := context.Background()
	role := "audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN"); err != nil {
		t.Fatal("real fixture role creation required", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP OWNED BY "+quoted); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(context.Background(), "DROP ROLE "+quoted); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, "GRANT USAGE ON SCHEMA "+pgx.Identifier{schema}.Sanitize()+" TO "+quoted+";GRANT INSERT,SELECT ON security_audit_outbox TO "+quoted+";GRANT USAGE ON SEQUENCE security_audit_outbox_id_seq TO "+quoted); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE security_audit_outbox SET result='rewritten'`, `DELETE FROM security_audit_outbox`, `TRUNCATE security_audit_outbox`} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+quoted); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, statement)
		_ = tx.Rollback(ctx)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "42501" {
			t.Fatal("audit runtime write was not denied", statement, err)
		}
	}
	t.Log("D11_028 runtime_update_denied=1 runtime_delete_denied=1 runtime_truncate_denied=1")
}
