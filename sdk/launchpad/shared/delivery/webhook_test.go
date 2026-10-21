package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type webhookAdminCredential struct{ timestamp, signature string }
type webhookAdminContextKey struct{}

func verifyWebhookFixtureAdmin(ctx context.Context, action string) error {
	credential, _ := ctx.Value(webhookAdminContextKey{}).(webhookAdminCredential)
	return verifyWebhookFixtureSignature(strings.Repeat("admin-secret", 4), credential.timestamp, action, credential.signature, time.Now(), time.Minute)
}
func TestWebhookTargetsResumeIndependentlyPostgresTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DELIVERY_TEST_DSN"))
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("explicit private DELIVERY_TEST_DSN required")
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := "kd_webhook_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	}()
	for _, ddl := range []func(string) (string, error){SchemaDDL, WebhookDDL} {
		sql, err := ddl(schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(pool, schema, fixturePolicy())
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("target-secret", 4)
	t.Setenv("WEBHOOK_FIXTURE_SECRET", secret)
	var outage atomic.Bool
	outage.Store(true)
	var mutex sync.Mutex
	seen := map[string]map[string]string{"a": {}, "b": {}}
	keys := map[string]string{}
	receiver := func(target string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(io.LimitReader(r.Body, MaxEnvelopeBytes+1))
			if err != nil || verifyWebhookFixtureSignature(secret, r.Header.Get("X-Sidiora-Timestamp"), string(body), r.Header.Get("X-Sidiora-Signature"), time.Now(), time.Minute) != nil {
				w.WriteHeader(401)
				return
			}
			var envelope Envelope
			if json.Unmarshal(body, &envelope) != nil {
				w.WriteHeader(400)
				return
			}
			mutex.Lock()
			prior, exists := keys[target+envelope.EventID]
			if exists && prior != r.Header.Get("Idempotency-Key") {
				t.Error("idempotency changed")
			}
			keys[target+envelope.EventID] = r.Header.Get("Idempotency-Key")
			mutex.Unlock()
			if target == "a" && outage.Load() {
				w.WriteHeader(503)
				return
			}
			mutex.Lock()
			seen[target][envelope.EventID] = Hash(body)
			mutex.Unlock()
			w.WriteHeader(204)
		})
	}
	a := httptest.NewTLSServer(receiver("a"))
	defer a.Close()
	b := httptest.NewTLSServer(receiver("b"))
	defer b.Close()
	roots := x509.NewCertPool()
	roots.AddCert(a.Certificate())
	roots.AddCert(b.Certificate())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	targets := []WebhookTarget{{"a", a.URL, "WEBHOOK_FIXTURE_SECRET", map[string]bool{fixtureSubject: true}}, {"b", b.URL, "WEBHOOK_FIXTURE_SECRET", map[string]bool{fixtureSubject: true}}}
	bridge, err := NewWebhookBridge(store, targets, client, secrets.EnvProvider{}, verifyWebhookFixtureAdmin, "relay-one", 10*time.Second, time.Second, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := fixtureEnvelope("webhook-first")
	first.SourceOrder.LogIndex = "1"
	second := fixtureEnvelope("webhook-second")
	second.SourceOrder.LogIndex = "2"
	for i, e := range []Envelope{first, second} {
		if err = store.Enqueue(ctx, tx, fixtureSubject, e); err != nil {
			t.Fatal(err)
		}
		previous := ""
		if i == 1 {
			previous = "position-first"
		}
		if err = bridge.StageTx(ctx, tx, "generation", "market:pool", int64(i+1), []string{"position-first", "position-second"}[i], previous, fixtureSubject, e); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_ = bridge.RunOnce(ctx)
	_ = bridge.RunOnce(ctx)
	var committedA, committedB int64
	if err = pool.QueryRow(ctx, "SELECT max(committed_sequence) FILTER(WHERE target_id='a'),max(committed_sequence) FILTER(WHERE target_id='b') FROM "+pgx.Identifier{schema}.Sanitize()+".delivery_webhook_cursors").Scan(&committedA, &committedB); err != nil || committedA != 0 || committedB != 2 {
		t.Fatal("target outage blocked independent cursor", committedA, committedB, err)
	}
	bridge, err = NewWebhookBridge(store, targets, client, secrets.EnvProvider{}, verifyWebhookFixtureAdmin, "relay-restarted", 10*time.Second, time.Second, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	outage.Store(false)
	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-deadline.C:
	}
	if err = bridge.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err = bridge.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(seen["a"]) != 2 || len(seen["b"]) != 2 {
		t.Fatal("restart lost delivery", seen)
	}
	for id, hash := range seen["a"] {
		if seen["b"][id] != hash {
			t.Fatal("target payload changed", id)
		}
	}
	if _, err = bridge.Inspect(ctx, "a", 10); err == nil {
		t.Fatal("unauthenticated inspection accepted")
	}
	if err = bridge.Retry(ctx, "a", "generation", "market:pool", 1); err == nil {
		t.Fatal("unauthenticated retry accepted")
	}
	var retryAudits int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema}.Sanitize()+".delivery_webhook_retry_audit").Scan(&retryAudits); err != nil || retryAudits != 0 {
		t.Fatal("unauthorized retry changed audit", retryAudits, err)
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	credential := webhookAdminCredential{stamp, signWebhookBody(strings.Repeat("admin-secret", 4), stamp, "webhook.inspect")}
	adminCtx := context.WithValue(ctx, webhookAdminContextKey{}, credential)
	metadata, err := bridge.Inspect(adminCtx, "a", 10)
	if err != nil || len(metadata) != 2 || metadata[0].Attempts < 2 {
		t.Fatal("durable attempts lost", metadata, err)
	}
}

func verifyWebhookFixtureSignature(secret, timestamp, body, signature string, now time.Time, window time.Duration) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds < now.Add(-window).Unix() || seconds > now.Add(window).Unix() || !strings.HasPrefix(signature, "sha256=") {
		return errors.New("signature unavailable")
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(body))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("invalid HMAC")
	}
	return nil
}
