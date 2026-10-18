package auth

import (
	"bytes"
	"context"
	"encoding/json"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type identityHTTPFixture struct {
	*identityFixture
	server *httptest.Server
}

func ownerHTTPFixture(t *testing.T) *identityHTTPFixture {
	t.Helper()
	f := fixtureIdentity(t)
	if _, err := f.pool.Exec(f.ctx, IdentitySerialDDL+IdentityOwnerDDL); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := f.pool.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	ddl, err := delivery.SchemaDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, ddl); err != nil {
		t.Fatal(err)
	}
	store, err := delivery.NewStore(f.pool, schema, delivery.Policy{Subjects: map[string]bool{IdentityRevisionSubject: true}, EventTypes: map[string]bool{IdentityRevisionEventType: true}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.events = &IdentityEventDelivery{store, IdentityRevisionSubject, IdentityRevisionEventType}
	t.Setenv("KINDLE_OWNER_HTTP_TEST_KEY", strings.Repeat("s", 48))
	now := f.s.now()
	tokens, err := NewIdentityTokens("https://kindle.test", "kindle-owner-v2", "owner", []SigningKey{{ID: "owner", SecretReference: "KINDLE_OWNER_HTTP_TEST_KEY", ActiveAt: now.Add(-time.Minute), RetireAt: now.Add(time.Hour)}}, EnvironmentIdentitySecrets{}, f.s.now)
	if err != nil {
		t.Fatal(err)
	}
	f.s.tokens = tokens
	router := chi.NewRouter()
	if err = f.s.RegisterV2Routes(router, IdentityHTTPConfig{ServiceToken: strings.Repeat("t", 32)}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &identityHTTPFixture{f, server}
}
func (f *identityHTTPFixture) request(t *testing.T, method, path, token, key string, data any) (int, []byte, http.Header) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://kindle.test")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body, response.Header
}
func (f *identityHTTPFixture) loginHTTP(t *testing.T) (Credentials, Principal) {
	t.Helper()
	challenge := f.challenge(t, f.wallet)
	status, raw, headers := f.request(t, "POST", "/v2/auth/sessions", "", "", LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "HTTP owner"})
	if status != 200 {
		t.Fatalf("login %d %s", status, raw)
	}
	var response Envelope[LoginData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != headers.Get("X-Request-Id") {
		t.Fatal("request correlation differs")
	}
	p, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, response.Data.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return Credentials{LoginData: response.Data}, p
}
func TestIdentityOwnerHTTPLinkRetryAndIsolation(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, p := f.loginHTTP(t)
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	walletB := strings.ToLower(crypto.PubkeyToAddress(keyB.PublicKey).Hex())
	request := WalletLinkChallengeRequest{walletB, "125", "private"}
	key := uuid.NewString()
	status, body, _ := f.request(t, "POST", "/v2/me/wallet-links/challenges", credentials.AccessToken, key, request)
	if status != 201 {
		t.Fatalf("link challenge %d %s", status, body)
	}
	var response Envelope[LinkChallengeData]
	if err = json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	status, retry, _ := f.request(t, "POST", "/v2/me/wallet-links/challenges", credentials.AccessToken, key, request)
	if status != 201 {
		t.Fatalf("retry %d %s", status, retry)
	}
	var replay Envelope[LinkChallengeData]
	if err = json.Unmarshal(retry, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Data.LinkID != response.Data.LinkID || replay.Data.Challenge.ChallengeID != response.Data.Challenge.ChallengeID {
		t.Fatal("idempotent link generated new authority")
	}
	changed := request
	changed.Visibility = "public_optin"
	status, _, _ = f.request(t, "POST", "/v2/me/wallet-links/challenges", credentials.AccessToken, key, changed)
	if status != 409 {
		t.Fatalf("changed request %d", status)
	}
	proof := proofIdentity(t, keyB, response.Data.Challenge)
	verifyKey := uuid.NewString()
	verify := map[string]any{"challengeId": response.Data.Challenge.ChallengeID, "proof": proof}
	path := "/v2/me/wallet-links/" + response.Data.LinkID + ":verify"
	status, body, _ = f.request(t, "POST", path, credentials.AccessToken, verifyKey, verify)
	if status != 200 {
		t.Fatalf("verify %d %s", status, body)
	}
	status, retry, _ = f.request(t, "POST", path, credentials.AccessToken, verifyKey, verify)
	if status != 200 {
		t.Fatalf("verify retry %d %s", status, retry)
	}
	challengeB := f.challenge(t, walletB)
	linked, err := f.s.Login(f.ctx, LoginRequest{challengeB.ChallengeID, proofIdentity(t, keyB, challengeB), "wallet B"})
	if err != nil {
		t.Fatal(err)
	}
	principalB, err := f.s.AuthenticateJWT(f.ctx, f.s.tokens, linked.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if principalB.AccountID() != p.AccountID() {
		t.Fatal("wallet switch changed account")
	}
	status, body, _ = f.request(t, "GET", "/v2/me/wallet-links", linked.AccessToken, "", nil)
	if status != 200 {
		t.Fatalf("list %d %s", status, body)
	}
	var links Envelope[struct {
		Items []WalletLink `json:"items"`
	}]
	if err = json.Unmarshal(body, &links); err != nil {
		t.Fatal(err)
	}
	if len(links.Data.Items) != 2 {
		t.Fatalf("links %d", len(links.Data.Items))
	}
	var events int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM delivery_outbox`).Scan(&events); err != nil || events != 4 {
		t.Fatalf("transactional events %d %v", events, err)
	}
}
func TestIdentityOwnerHTTPConcurrentChallengeAndRateLimit(t *testing.T) {
	f := ownerHTTPFixture(t)
	challenge := f.challenge(t, f.wallet)
	req := LoginRequest{challenge.ChallengeID, proofIdentity(t, f.key, challenge), "concurrent"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 100)
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := http.NewRequest("POST", f.server.URL+"/v2/auth/sessions", bytes.NewReader(raw))
			if e != nil {
				errs <- e
				return
			}
			r.Header.Set("Origin", "https://kindle.test")
			response, e := f.server.Client().Do(r)
			if e != nil {
				errs <- e
				return
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			codes <- response.StatusCode
		}()
	}
	wg.Wait()
	close(codes)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	ok, denied := 0, 0
	for code := range codes {
		switch code {
		case 200:
			ok++
		case 401:
			denied++
		default:
			t.Fatalf("unexpected %d", code)
		}
	}
	if ok != 1 || denied != 99 {
		t.Fatalf("success=%d denied=%d", ok, denied)
	}
	for i := 1; i < 10; i++ {
		status, body, _ := f.request(t, "POST", "/v2/auth/challenges", "", uuid.NewString(), ChallengeRequest{f.wallet, "https://kindle.test"})
		if status != 201 {
			t.Fatalf("challenge %d %s", status, body)
		}
	}
	status, body, headers := f.request(t, "POST", "/v2/auth/challenges", "", uuid.NewString(), ChallengeRequest{f.wallet, "https://kindle.test"})
	if status != 429 || headers.Get("Retry-After") == "" {
		t.Fatalf("limit %d %s retry=%s", status, body, headers.Get("Retry-After"))
	}
}

func TestIdentityOwnerRealJetStreamRevisionDedup(t *testing.T) {
	f := ownerHTTPFixture(t)
	_, p := f.loginHTTP(t)
	binary := os.Getenv("NATS_SERVER_BINARY")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("nats-server")
		if err != nil {
			t.Fatal("real file-backed nats-server required", err)
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
	dir := t.TempDir()
	log, err := os.Create(filepath.Join(dir, "broker.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-js", "-sd", filepath.Join(dir, "jetstream"), "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { command.Wait(); close(done) }()
	t.Cleanup(func() {
		command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			command.Process.Kill()
			<-done
		}
		log.Close()
	})
	var nc *nats.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		nc, err = nats.Connect("nats://127.0.0.1:"+strconv.Itoa(port), nats.NoReconnect(), nats.Timeout(50*time.Millisecond))
		if err == nil {
			break
		}
		select {
		case <-done:
			t.Fatal("owned broker exited")
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream := "IDENTITY_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{IdentityRevisionSubject}, Storage: jetstream.FileStorage, Replicas: 1, Duplicates: time.Minute}); err != nil {
		t.Fatal(err)
	}
	publisher, err := natsdriver.NewPublisher(nc, stream)
	if err != nil {
		t.Fatal(err)
	}
	relay := delivery.Relay{Store: f.s.events.Store, Publisher: publisher, Owner: "identity_owner", Batch: 10, LeaseDuration: 10 * time.Second, PublishTimeout: 2 * time.Second, RetryDelay: time.Second, MaxAttempts: 3}
	if err = relay.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for _, durable := range []string{"identity_a", "identity_b"} {
		cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: durable, Subject: IdentityRevisionSubject, Batch: 1, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 3, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
		broker, err := cfg.BrokerConsumerConfig()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = js.CreateConsumer(ctx, stream, broker); err != nil {
			t.Fatal(err)
		}
		consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(consumer.Close)
		messages, err := consumer.Fetch(ctx)
		if err != nil || len(messages) != 1 {
			t.Fatalf("real broker fetch messages=%d err=%v", len(messages), err)
		}
		message := messages[0]
		if err = f.s.ConsumeIdentityRevision(ctx, "identity_projection", "g1", message.Subject, message.Wire, message); err != nil {
			t.Fatal(err)
		}
	}
	var count, revision int64
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM identity_revision_inbox`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("revision inbox %d %v", count, err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT revision FROM identity_remote_revisions WHERE account_id=$1`, p.AccountID()).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("revision effect %d %v", revision, err)
	}
	measurementConfig := natsdriver.ConsumerConfig{Stream: stream, Durable: "identity_measurement", Subject: IdentityRevisionSubject, Batch: 1, MaxAckPending: 4, MaxWaiting: 1, MaxDeliver: 3, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
	broker, err := measurementConfig.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateConsumer(ctx, stream, broker); err != nil {
		t.Fatal(err)
	}
	sourceRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	_, sourceDigest, err := identityMeasurementSources(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := os.ReadFile(filepath.Join(sourceRoot, "spec/production-system/integration/contracts/v2/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	topology, err := json.Marshal(measurementConfig)
	if err != nil {
		t.Fatal(err)
	}
	binding := IdentityMeasurementBinding{CandidateSHA256: sourceDigest, InterfaceSHA256: measurementHash(contract), TopologySHA256: measurementHash(topology), Region: "loopback", RunID: uuid.NewString()}
	measurement, err := f.s.MeasureIdentityReplay(ctx, nc, measurementConfig, "identity_projection", "g1", 1, binding)
	if err != nil || measurement.Fetched != 1 || measurement.Committed != 1 || measurement.InboxCount != 1 || len(measurement.InboxSHA256) != 64 {
		t.Fatalf("actual identity replay measurement failed: %+v %v", measurement, err)
	}
}

func TestIdentityOwnerCookieBootstrapAndChatTicketReplay(t *testing.T) {
	f := ownerHTTPFixture(t)
	credentials, principal := f.loginHTTP(t)
	request, err := http.NewRequest("GET", f.server.URL+"/v2/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://kindle.test")
	request.AddCookie(&http.Cookie{Name: "__Host-kindle_access", Value: credentials.AccessToken})
	request.AddCookie(&http.Cookie{Name: "__Host-kindle_csrf", Value: strings.Repeat("f", 64)})
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var current Envelope[sharedauth.IdentityPrincipal]
	if response.StatusCode != 200 || json.Unmarshal(body, &current) != nil || current.Data.SessionID != principal.SessionID() || current.Data.AccountID != principal.AccountID() || current.Data.Wallet != principal.Wallet() || response.Header.Get("X-CSRF-Token") != "" {
		t.Fatalf("bootstrap identity or CSRF authority invalid: %d %s", response.StatusCode, body)
	}
	topics := []string{"private.chat." + principal.Wallet()}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: "https://kindle.test", Endpoint: "/v2/stream"}
	ticket, err := f.s.IssueChatStreamTicket(f.ctx, principal, binding)
	if err != nil {
		t.Fatal(err)
	}
	changed := binding
	changed.Origin = "https://attacker.test"
	if _, err = f.s.ConsumeChatStreamTicket(f.ctx, ticket, changed); err == nil {
		t.Fatal("chat ticket changed origin accepted")
	}
	admitted, err := f.s.ConsumeChatStreamTicket(f.ctx, ticket, binding)
	if err != nil || admitted.SessionID != principal.SessionID() {
		t.Fatalf("chat identity: %+v %v", admitted, err)
	}
	if _, err = f.s.ConsumeChatStreamTicket(f.ctx, ticket, binding); err == nil {
		t.Fatal("chat ticket replay accepted")
	}
	if err = f.s.Logout(f.ctx, principal, true); err != nil {
		t.Fatal(err)
	}
	if err = f.s.CheckStreamSession(f.ctx, admitted); err == nil {
		t.Fatal("chat session survived authoritative logout")
	}
}
