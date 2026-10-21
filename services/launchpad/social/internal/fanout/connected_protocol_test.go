package fanout_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnectedReadMarkersCommitMonotonicallyWithDurableControlIntent(t *testing.T) {
	store := durableFixture(t)
	ctx := context.Background()
	if _, err := store.Pool.Exec(ctx, fanout.ConnectedSchemaSQL); err != nil {
		t.Fatal(err)
	}
	first := sendDM(t, store, "connected-first")
	second := sendDM(t, store, "connected-second")
	if _, err := store.Revise(ctx, addr(0xaa), second.ID, 1, "", true); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkReadConnected(ctx, addr(0xbb), first.ConversationID, 1000); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkReadConnected(ctx, addr(0xbb), first.ConversationID, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkReadConnected(ctx, addr(0xcc), first.ConversationID, 1); err != fanout.ErrNotFound {
		t.Fatalf("third wallet read: %v", err)
	}
	var through, count int64
	if err := store.Pool.QueryRow(ctx, `SELECT read_sequence FROM chat_members WHERE conversation_id=$1 AND wallet=$2`, first.ConversationID, addr(0xbb)).Scan(&through); err != nil {
		t.Fatal(err)
	}
	if through != 1 {
		t.Fatalf("read through=%d", through)
	}
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_control_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate read events=%d", count)
	}
	var payload []byte
	if err := store.Pool.QueryRow(ctx, `SELECT payload FROM chat_control_outbox`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event map[string]string
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if len(event) != 3 || event["through_sequence"] != "1" || event["conversation_id"] != first.ConversationID || event["type"] != "read_marker" {
		t.Fatalf("unexpected minimized event: %v", event)
	}
	page, err := store.History(ctx, addr(0xbb), first.ConversationID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.ReadThrough != 1 {
		t.Fatalf("history marker=%d", page.ReadThrough)
	}
}
func TestConnectedConstructorRejectsMissingProductionProviders(t *testing.T) {
	if _, err := fanout.NewConnected(fanout.Deps{}); err != fanout.ErrUnavailable {
		t.Fatalf("missing production providers accepted: %v", err)
	}
}

func TestSocialPublisherProcessHelper(t *testing.T) {
	mode := os.Getenv("SOCIAL_PUBLISHER_CHILD_MODE")
	if mode == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("SOCIAL_PUBLISHER_CHILD_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	nc, err := nats.Connect(os.Getenv("SOCIAL_TEST_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	publisher, err := natsdriver.NewPublisher(nc, os.Getenv("SOCIAL_PUBLISHER_CHILD_STREAM"))
	if err != nil {
		t.Fatal(err)
	}
	broker := fanout.SocialBroker{Publisher: publisher, Subject: os.Getenv("SOCIAL_PUBLISHER_CHILD_SUBJECT"), Stream: os.Getenv("SOCIAL_PUBLISHER_CHILD_STREAM")}
	store := fanout.DurableStore{Pool: pool}
	_, err = store.DrainOutbox(ctx, 100, func(ctx context.Context, id string, channels []string, payload []byte) error {
		if err := broker.Publish(ctx, id, channels, payload); err != nil {
			return err
		}
		if mode == "crash-after-ack" {
			fmt.Fprintln(os.Stdout, "BROKER_PERSISTED")
			select {
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestConnectedRealR3PublisherKillRestartAndConsumerInbox(t *testing.T) {
	brokerURL := os.Getenv("SOCIAL_TEST_NATS_URL")
	parsed, err := url.Parse(brokerURL)
	if err != nil || parsed.Scheme != "nats" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") {
		t.Fatal("SOCIAL_TEST_NATS_URL must identify the allocated real local three-node JetStream fixture")
	}
	store := durableFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if _, err = store.Pool.Exec(ctx, fanout.ConnectedSchemaSQL); err != nil {
		t.Fatal(err)
	}
	message := sendDM(t, store, "crash-restart")
	nc, err := nats.Connect(brokerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	stream, subject := "SOCIAL_"+suffix, "fixture.social."+suffix
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{subject}, Storage: jetstream.FileStorage, Replicas: 3, Duplicates: time.Second, MaxAge: time.Hour, MaxBytes: 8 * 1024 * 1024, MaxMsgs: 1000}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := js.DeleteStream(cleanup, stream); err != nil {
			t.Error(err)
		}
	})
	environment := append(os.Environ(), "SOCIAL_PUBLISHER_CHILD_DSN="+store.Pool.Config().ConnConfig.ConnString(), "SOCIAL_PUBLISHER_CHILD_STREAM="+stream, "SOCIAL_PUBLISHER_CHILD_SUBJECT="+subject)
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSocialPublisherProcessHelper$")
	child.Env = append(environment, "SOCIAL_PUBLISHER_CHILD_MODE=crash-after-ack")
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	persisted := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "BROKER_PERSISTED" {
				persisted <- true
				return
			}
		}
		persisted <- false
	}()
	select {
	case ok := <-persisted:
		if !ok {
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatal("publisher did not reach actual broker persistence acknowledgement", stderr.String())
		}
	case <-ctx.Done():
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal(ctx.Err())
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err == nil {
		t.Fatal("killed publisher exited successfully")
	}
	select {
	case <-time.After(1100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	restarted := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSocialPublisherProcessHelper$")
	restarted.Env = append(environment, "SOCIAL_PUBLISHER_CHILD_MODE=drain")
	if output, err := restarted.CombinedOutput(); err != nil {
		t.Fatalf("publisher restart: %v %s", err, output)
	}
	cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: "social_" + suffix, Subject: subject, Batch: 1, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
	brokerCfg, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, stream, brokerCfg); err != nil {
		t.Fatal(err)
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	var received []*natsdriver.Message
	for len(received) == 0 {
		received, err = consumer.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(received) != 1 {
		t.Fatalf("broker received=%d", len(received))
	}
	if err = store.ConsumeSocialBroker(ctx, received[0], subject, stream); err != nil {
		t.Fatal(err)
	}
	var retried []*natsdriver.Message
	for len(retried) == 0 {
		retried, err = consumer.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(retried) != 1 || retried[0].EventID != received[0].EventID {
		t.Fatal("publisher retry did not preserve event identity")
	}
	if err = store.ConsumeSocialBroker(ctx, retried[0], subject, stream); err != nil {
		t.Fatal(err)
	}
	var inbox, live, pending int
	if err = store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM chat_broker_inbox),(SELECT count(*) FROM chat_live_outbox),(SELECT count(*) FROM chat_outbox WHERE delivered_at IS NULL)`).Scan(&inbox, &live, &pending); err != nil {
		t.Fatal(err)
	}
	if inbox != 1 || live != 2 || pending != 0 {
		t.Fatalf("reconciliation inbox=%d live=%d pending=%d", inbox, live, pending)
	}
	history, err := store.History(ctx, addr(0xbb), message.ConversationID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Messages) != 1 || history.Messages[0].ID != message.ID {
		t.Fatal("disconnected recipient history did not reconcile to the single persisted ID")
	}

	publisher, err := natsdriver.NewPublisher(nc, stream)
	if err != nil {
		t.Fatal(err)
	}
	broker := fanout.SocialBroker{Publisher: publisher, Stream: stream, Subject: subject}
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	forgedID := message.ID + ":foreign-channel"
	if err = broker.Publish(ctx, forgedID, []string{store.ChannelNamespace + "pool:" + addr(1)}, payload); err != nil {
		t.Fatal(err)
	}
	var forged []*natsdriver.Message
	for len(forged) == 0 {
		forged, err = consumer.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(forged) != 1 || store.ConsumeSocialBroker(ctx, forged[0], subject, stream) == nil {
		t.Fatal("private message admitted to a forged public channel")
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_live_outbox WHERE channel=$1`, store.ChannelNamespace+"pool:"+addr(1)).Scan(&live); err != nil || live != 0 {
		t.Fatal("unauthorized live outbox payload", live, err)
	}
}

func realPolicyChat(t *testing.T) (*fanout.DurableStore, map[string]context.Context, *interactions.PGPolicy) {
	t.Helper()
	store := durableFixture(t)
	ctx := context.Background()
	schema := store.Pool.Config().ConnConfig.RuntimeParams["search_path"]
	for _, path := range []string{"spec/production-system/integration/migrations/330000.sql", "spec/production-system/integration/migrations/350000.sql", "spec/production-system/integration/migrations/370000.sql", "spec/production-system/integration/migrations/380000.sql", "spec/production-system/handoffs/L37/phase2/schema-proposal.sql"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", path))
		if err != nil {
			t.Fatal(err)
		}
		ddl := regexp.MustCompile(`(?m)^SET LOCAL search_path[^\n]*\n`).ReplaceAllString(string(raw), "")
		if _, err = store.Pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("real policy schema %s: %v", path, err)
		}
	}
	if _, err := store.Pool.Exec(ctx, fanout.ConnectedSchemaSQL+`CREATE TABLE pools(pool_address text PRIMARY KEY,creator text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	policy := &interactions.PGPolicy{DB: store.Pool, Accounts: pgx.Identifier{schema, "account_state"}, Pools: pgx.Identifier{schema, "pools"}, Sessions: pgx.Identifier{schema, "identity_sessions"}, IdentityAccounts: pgx.Identifier{schema, "identity_accounts"}, WalletLinks: pgx.Identifier{schema, "identity_wallet_links"}, SessionGuard: pgx.Identifier{schema, "social_session_live"}, AccountGuard: pgx.Identifier{schema, "social_account_active"}, PoolGuard: pgx.Identifier{schema, "social_pool_creator"}}
	guards, err := policy.GuardDDL()
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range guards {
		if _, err = store.Pool.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	contexts := map[string]context.Context{}
	for _, wallet := range []string{addr(0xaa), addr(0xbb), addr(0xcc)} {
		account, session := uuid.NewString(), uuid.NewString()
		hash := sha256.Sum256([]byte(session))
		now := time.Now().UTC()
		if _, err = store.Pool.Exec(ctx, `INSERT INTO identity_accounts VALUES($1,0,$2)`, account, now); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Pool.Exec(ctx, `INSERT INTO identity_wallet_links VALUES($1,$2,125,$3,'private','verified',$4)`, uuid.NewString(), account, wallet, now); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Pool.Exec(ctx, `INSERT INTO identity_sessions(session_id,account_id,wallet,auth_epoch,access_hash,csrf_hash,issued_at,access_expires_at,refresh_expires_at,reauthenticated_at,last_seen_at,device_label) VALUES($1,$2,$3,0,$4,$4,$5,$6,$6,$5,$5,'chat-adversarial')`, session, account, wallet, hash[:], now, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Pool.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,'{}','{}');`, account, wallet); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Pool.Exec(ctx, `INSERT INTO interaction_preferences(wallet,inbox) VALUES($1,'everyone')`, wallet); err != nil {
			t.Fatal(err)
		}
		principal := fanout.Principal{Wallet: wallet, SessionID: session, ExpiresAt: now.Add(time.Hour)}
		principal.Validate = func(ctx context.Context) error { return policy.ValidateSession(ctx, wallet, session) }
		contexts[wallet] = fanout.WithPrincipal(ctx, principal)
	}
	store.Policy = policy.SendPolicy
	store.ReadPolicy = policy.ReadChatPolicy
	if _, err = store.Pool.Exec(ctx, `INSERT INTO pools VALUES($1,$2)`, addr(1), addr(0xbb)); err != nil {
		t.Fatal(err)
	}
	return store, contexts, policy
}

func TestAdversarialRealPolicyPrivacyRevocationAndUnavailable(t *testing.T) {
	store, contexts, _ := realPolicyChat(t)
	a, b, c := addr(0xaa), addr(0xbb), addr(0xcc)
	in := fanout.SendInput{Actor: a, Target: b, Kind: "dm", ClientID: "actual-policy", Content: "private actual policy text"}
	message, err := store.Send(contexts[a], in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.History(contexts[b], b, message.ConversationID, "", 50)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ClientID != in.ClientID {
		t.Fatal(page, err)
	}
	if _, err = store.History(contexts[c], c, message.ConversationID, "", 50); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatalf("third wallet history: %v", err)
	}
	if _, err = store.History(contexts[c], c, "absent", "", 50); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatalf("unknown history: %v", err)
	}
	if _, err = store.History(contexts[c], c, message.ConversationID, page.Next, 50); !errors.Is(err, fanout.ErrCursor) {
		t.Fatalf("foreign cursor: %v", err)
	}
	if err = store.MarkReadConnected(contexts[c], c, message.ConversationID, 100); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatalf("foreign read marker: %v", err)
	}
	if _, err = store.Pool.Exec(context.Background(), `ALTER TABLE safety_bans RENAME TO interrupted_safety_bans`); err != nil {
		t.Fatal(err)
	}
	in.ClientID = "moderation-unavailable"
	if _, err = store.Send(contexts[a], in); err == nil || errors.Is(err, interactions.ErrDenied) {
		t.Fatal("unavailable moderation must remain a retryable dependency failure", err)
	}
	poolInput := fanout.SendInput{Actor: a, Target: addr(1), Kind: "pool", ClientID: "pool-unavailable", Content: "unavailable room policy"}
	if _, err = store.Send(contexts[a], poolInput); err == nil || errors.Is(err, interactions.ErrDenied) {
		t.Fatal("unavailable pool moderation must remain retryable", err)
	}
	var messages, outbox int
	if err = store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM chat_messages),(SELECT count(*) FROM chat_outbox)`).Scan(&messages, &outbox); err != nil || messages != 1 || outbox != 1 {
		t.Fatal(messages, outbox, err)
	}
	if _, err = store.Pool.Exec(context.Background(), `ALTER TABLE interrupted_safety_bans RENAME TO safety_bans`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Pool.Exec(context.Background(), `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE wallet=$1`, b); err != nil {
		t.Fatal(err)
	}
	if _, err = store.History(contexts[b], b, message.ConversationID, "", 50); err == nil {
		t.Fatal("revoked real session retrieved private history")
	}
	if err = store.MarkReadConnected(contexts[b], b, message.ConversationID, 100); err == nil {
		t.Fatal("revoked real session changed marker")
	}
}

func TestAdversarialUncertainRealPostgresCommitRetriesOneMessage(t *testing.T) {
	store, contexts, _ := realPolicyChat(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var socketsMu sync.Mutex
	var sockets []net.Conn
	var workers sync.WaitGroup
	t.Cleanup(func() {
		_ = listener.Close()
		socketsMu.Lock()
		for _, socket := range sockets {
			_ = socket.Close()
		}
		socketsMu.Unlock()
		workers.Wait()
	})
	upstreamAddress := net.JoinHostPort(store.Pool.Config().ConnConfig.Host, strconv.Itoa(int(store.Pool.Config().ConnConfig.Port)))
	dropped := make(chan struct{}, 1)
	go func() {
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			upstream, e := net.DialTimeout("tcp", upstreamAddress, 2*time.Second)
			if e != nil {
				_ = client.Close()
				return
			}
			socketsMu.Lock()
			sockets = append(sockets, client, upstream)
			socketsMu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				defer upstream.Close()
				go func() { _, _ = io.Copy(upstream, client) }()
				for {
					header := make([]byte, 5)
					if _, e := io.ReadFull(upstream, header); e != nil {
						return
					}
					length := binary.BigEndian.Uint32(header[1:])
					if length < 4 || length > 1<<20 {
						return
					}
					body := make([]byte, int(length)-4)
					if _, e := io.ReadFull(upstream, body); e != nil {
						return
					}
					if header[0] == 'C' && string(body) == "COMMIT\x00" {
						dropped <- struct{}{}
						return
					}
					if _, e := client.Write(append(header, body...)); e != nil {
						return
					}
				}
			}()
		}
	}()
	config := store.Pool.Config().Copy()
	config.ConnConfig.Host = "127.0.0.1"
	config.ConnConfig.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
	config.ConnConfig.TLSConfig = nil
	config.ConnConfig.Fallbacks = nil
	proxyPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer proxyPool.Close()
	interrupted := *store
	interrupted.Pool = proxyPool
	ctx, cancel := context.WithTimeout(contexts[addr(0xaa)], 10*time.Second)
	defer cancel()
	in := fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: "lost-commit-response", Content: "commit survived transport loss"}
	if _, err = interrupted.Send(ctx, in); !errors.Is(err, fanout.ErrCommitUncertain) {
		t.Fatalf("uncertain actual commit: %v", err)
	}
	select {
	case <-dropped:
	default:
		t.Fatal("real COMMIT response was not intercepted")
	}
	retry, err := store.Send(contexts[addr(0xaa)], in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.History(contexts[addr(0xbb)], addr(0xbb), retry.ConversationID, "", 50)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != retry.ID || page.Messages[0].ClientID != in.ClientID {
		t.Fatal(page, err)
	}
	var messages, outbox int
	if err = store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM chat_messages),(SELECT count(*) FROM chat_outbox)`).Scan(&messages, &outbox); err != nil || messages != 1 || outbox != 1 {
		t.Fatal(messages, outbox, err)
	}
}
