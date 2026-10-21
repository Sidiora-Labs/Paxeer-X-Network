package natsdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func epochBrokerFixture(t *testing.T) (EpochMemberAuthority, *nats.Conn, jetstream.JetStream) {
	t.Helper()
	binary := os.Getenv("NATS_SERVER_BINARY")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("nats-server")
		if err != nil {
			t.Fatal("real nats-server required: ", err)
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
	user := func(name, password string) map[string]any { return map[string]any{"user": name, "password": password} }
	config := map[string]any{"host": "127.0.0.1", "port": port, "jetstream": map[string]any{"store_dir": filepath.Join(directory, "store")}, "system_account": "SYS", "accounts": map[string]any{
		"SYS": map[string]any{"users": []any{user("system-operator", "test-system-secret")}},
		"APP": map[string]any{"jetstream": "enabled", "users": []any{user("epoch-three", "test-epoch-three-secret"), user("epoch-four", "test-epoch-four-secret")}},
	}}
	original, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	config["accounts"].(map[string]any)["APP"].(map[string]any)["users"] = []any{user("epoch-four", "test-epoch-four-secret")}
	replacement, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "server.conf")
	if err = os.WriteFile(configPath, original, 0600); err != nil {
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
	go func() { _ = command.Wait(); close(done) }()
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
	var control *nats.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		control, err = nats.Connect(address, nats.UserInfo("system-operator", "test-system-secret"), nats.NoReconnect(), nats.Timeout(100*time.Millisecond))
		if err == nil {
			break
		}
		select {
		case <-done:
			t.Fatal("actual broker startup failed; log ", log.Name())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(control.Close)
	current, err := nats.Connect(address, nats.UserInfo("epoch-four", "test-epoch-four-secret"), nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(current.Close)
	js, err := jetstream.New(current)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ConsumerConfig{Stream: "RECOVERY_PROBE", Durable: "epoch_probe", Subject: "recovery.epoch.probe", Batch: 1, MaxAckPending: 16, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: cfg.Stream, Subjects: []string{cfg.Subject}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: time.Minute}); err != nil {
		t.Fatal(err)
	}
	brokerCfg, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, cfg.Stream, brokerCfg); err != nil {
		t.Fatal(err)
	}
	member := EpochMemberAuthority{Name: "member-one", URL: address, ServerID: control.ConnectedServerId(), Control: control, ConfigPath: configPath, ExistingSHA256: epochSHA(original), Replacement: replacement, ReplacementSHA256: epochSHA(replacement), Credentials: []EpochCredential{{ID: "epoch-three", Username: "epoch-three", Password: "test-epoch-three-secret"}}, Probe: cfg}
	return member, current, js
}

func TestEpochRealBrokerRevokesAuthenticationPublishAndAck(t *testing.T) {
	member, _, current := epochBrokerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := validateEpochMember(member); err != nil {
		t.Fatal(err)
	}
	probes, err := prepareEpochProbes(ctx, member, "real-cutover")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range probes {
			p.conn.Close()
		}
	}()
	evidence, err := revokeEpochMember(ctx, member, probes)
	if err != nil || len(evidence) != 1 || !evidence[0].AuthenticationDenied || !evidence[0].PublishDenied || !evidence[0].AckDenied {
		t.Fatalf("broker epoch fence evidence=%+v err=%v", evidence, err)
	}
	if ack, err := current.Publish(ctx, member.Probe.Subject, []byte("current-epoch-real-message"), jetstream.WithExpectStream(member.Probe.Stream)); err != nil || ack == nil || ack.Sequence == 0 {
		t.Fatalf("surviving current credential failed: %+v %v", ack, err)
	}
	persisted, err := os.ReadFile(member.ConfigPath)
	if err != nil || epochSHA(persisted) != member.ReplacementSHA256 {
		t.Fatal("actual server config not durably replaced", err)
	}
	if !probes[0].conn.IsClosed() {
		t.Fatal("revoked connection survived")
	}
	if conn, err := connectEpochCredential(member, member.Credentials[0]); !errors.Is(err, nats.ErrAuthorization) {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("old credential not explicitly rejected", err)
	}
}

func TestEpochPinnedConfigurationChangeCannotReload(t *testing.T) {
	member, _, _ := epochBrokerFixture(t)
	if err := os.WriteFile(member.ConfigPath, []byte("unreviewed change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceEpochConfig(member); err == nil {
		t.Fatal("changed original config accepted")
	}
	old, err := connectEpochCredential(member, member.Credentials[0])
	if err != nil {
		t.Fatal("rejected attempt still altered live authentication", err)
	}
	old.Close()
	member.Replacement[0] ^= 1
	if err = validateEpochMember(member); err == nil {
		t.Fatal("changed pinned replacement accepted")
	}
}

func epochPostgresFixture(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("BROKER_EPOCH_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("DELIVERY_TEST_DSN")
	}
	if dsn == "" {
		t.Fatal("explicit actual fixture PostgreSQL DSN required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("refusing non-fixture PostgreSQL")
	}
	cfg.MaxConns = 3
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	schema := fmt.Sprintf("kd_epoch_%x", time.Now().UnixNano())
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy", "launchpad", "production", "recovery", "epoch.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := queue.RenderEpochSQL(schema, string(sql))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	})
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+quoted+".recovery_control(singleton,epoch,region,revision,checkpoint_sha256) VALUES(true,3,'region-primary',1,$1)", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+quoted+".recovery_proposals(proposal_id,expected_epoch,next_region,revision,checkpoint_sha256,state,actor) VALUES('actual-cutover',3,'region-recovery',2,$1,'validated','fixture-operator')", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	return pool, schema
}

func TestEpochRealPostgresPersistsFenceBeforeExplicitActivation(t *testing.T) {
	member, _, _ := epochBrokerFixture(t)
	pool, schema := epochPostgresFixture(t)
	controller, err := NewBrokerEpochController(pool, schema, 3, "region-primary", []EpochMemberAuthority{member})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "SELECT "+quoted+".activate_fenced_v2('actual-cutover','fixture-operator')"); err == nil {
		t.Fatal("activation without actual broker fence accepted")
	}
	result, err := controller.Fence(ctx, "actual-cutover", "fixture-operator")
	if err != nil {
		t.Fatal(err)
	}
	var state, proof string
	var epoch int64
	if err = pool.QueryRow(ctx, "SELECT state,proof_sha256 FROM "+quoted+".recovery_broker_fences WHERE proposal_id='actual-cutover'").Scan(&state, &proof); err != nil {
		t.Fatal(err)
	}
	if state != "verified" || proof != result.ProofSHA256 || len(result.Evidence) != 1 {
		t.Fatal("actual fence evidence missing", state, proof)
	}
	if err = pool.QueryRow(ctx, "SELECT epoch FROM "+quoted+".recovery_control WHERE singleton").Scan(&epoch); err != nil || epoch != 3 {
		t.Fatal("controller autonomously activated", epoch, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE "+quoted+".recovery_proposals SET fence_proof_sha256=$1 WHERE proposal_id='actual-cutover'", result.ProofSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "SELECT "+quoted+".activate_fenced_v2('actual-cutover','fixture-operator')"); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT epoch FROM "+quoted+".recovery_control WHERE singleton").Scan(&epoch); err != nil || epoch != 4 {
		t.Fatal("explicit activation failed", epoch, err)
	}
	if _, err = controller.Fence(ctx, "actual-cutover", "fixture-operator"); err == nil {
		t.Fatal("stale epoch controller accepted")
	}
}

func TestEpochRealPostgresFailureCannotCertifyFence(t *testing.T) {
	member, _, _ := epochBrokerFixture(t)
	pool, schema := epochPostgresFixture(t)
	controller, err := NewBrokerEpochController(pool, schema, 3, "region-primary", []EpochMemberAuthority{member})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(member.ConfigPath, []byte("unreviewed configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = controller.Fence(ctx, "actual-cutover", "fixture-operator"); err == nil {
		t.Fatal("unreviewed reload certified")
	}
	var state string
	var proof *string
	if err = pool.QueryRow(ctx, "SELECT state,proof_sha256 FROM "+pgx.Identifier{schema}.Sanitize()+".recovery_broker_fences WHERE proposal_id='actual-cutover'").Scan(&state, &proof); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || proof != nil {
		t.Fatal("failed broker fence became verified", state, proof)
	}
}
