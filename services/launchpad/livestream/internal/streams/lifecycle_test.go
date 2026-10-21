package streams

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *LifecycleStore {
	t.Helper()
	dsn := os.Getenv("L40_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L40_TEST_POSTGRES_DSN is required; integration cannot silently skip")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("kd_29e4009f8b95235d0159d804_%d", time.Now().UnixNano())
	s, e := NewLifecycleStore(p, schema, 3, 20)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		p.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c, x := context.WithTimeout(context.Background(), 5*time.Second)
		defer x()
		_, e := p.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
		if e != nil {
			t.Error(e)
		}
		var present bool
		if e = p.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&present); e != nil || present {
			t.Error("schema cleanup not confirmed", present, e)
		}
		p.Close()
	})
	if _, e = p.Exec(ctx, s.SchemaSQL()); e != nil {
		t.Fatal(e)
	}
	return s
}
func intent(key string) (CreateIntent, CreatorDecision) {
	w := "0x1111111111111111111111111111111111111111"
	p := "0x2222222222222222222222222222222222222222"
	return CreateIntent{w, p, "A real stream request", key}, CreatorDecision{w, p, true, time.Now().Add(time.Minute)}
}
func TestPersistentIdempotencyAndRestart(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("one")
	o, e := s.Create(ctx, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	s2, e := NewLifecycleStore(s.pool, s.schema, 3, 20)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s2.Create(ctx, r, d, true)
	if e != nil || again.ID != o.ID {
		t.Fatal(again, e)
	}
	r.Title = "altered"
	if _, e = s.Create(ctx, r, d, true); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	var n, a int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_operations").Scan(&n); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_audit").Scan(&a); e != nil {
		t.Fatal(e)
	}
	if n != 1 || a != 1 {
		t.Fatal(n, a)
	}
}
func TestConcurrentAdmissionAtMostThree(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, d := intent(fmt.Sprint(i))
			_, e := s.Create(context.Background(), r, d, true)
			if e == nil {
				mu.Lock()
				count++
				mu.Unlock()
			} else if !errors.Is(e, ErrQuota) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if count != 3 {
		t.Fatal(count)
	}
}
func TestProviderTruthOrderingAndTerminationRetry(t *testing.T) {
	s := fixture(t)
	c := context.Background()
	r, d := intent("lifecycle")
	o, e := s.Create(c, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	if o.State != "REQUESTED" {
		t.Fatal(o)
	}
	o, e = s.BeginProvision(c, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginProvision(c, o.ID); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	o, e = s.BindProvider(c, o.ID, "provider_1")
	if e != nil {
		t.Fatal(e)
	}
	adopt, e := s.BindProvider(c, o.ID, "provider_1")
	if e != nil || adopt.Revision != o.Revision {
		t.Fatal(adopt, e)
	}
	if _, e = s.BindProvider(c, o.ID, "provider_2"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	o, e = s.Transition(c, o.ID, o.Wallet, "start", o.Revision, ActionDecision{o.Wallet, o.ID, "start", o.Revision, true, time.Now().Add(time.Minute)})
	if e != nil || o.State != "READY" {
		t.Fatal(o, e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	ev := ProviderEvent{"ev1", "provider_1", "LIVE", now}
	out, e := s.Observe(c, ev)
	if e != nil || out != "applied" {
		t.Fatal(out, e)
	}
	out, e = s.Observe(c, ev)
	if e != nil || out != "duplicate" {
		t.Fatal(out, e)
	}
	equivalent := ev
	equivalent.ObservedAt = equivalent.ObservedAt.In(time.FixedZone("fixture", 3600))
	if out, e := s.Observe(c, equivalent); e != nil || out != "duplicate" {
		t.Fatal(out, e)
	}
	ev.State = "ENDED"
	if _, e = s.Observe(c, ev); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	o, e = s.Get(c, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.Transition(c, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, o.ID, "end", o.Revision, true, time.Now().Add(time.Minute)})
	if e != nil || o.State != "ENDING" {
		t.Fatal(o, e)
	}
	if e = s.TerminationFailed(c, o.ID); e != nil {
		t.Fatal(e)
	}
	o, e = s.Get(c, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	if o.State != "ENDING" || o.RetryAt == nil {
		t.Fatal(o)
	}
	out, e = s.Observe(c, ProviderEvent{"ev2", "provider_1", "LIVE", now.Add(time.Second)})
	if e != nil || out != "ignored_stale_or_terminal" {
		t.Fatal(out, e)
	}
	_, e = s.Observe(c, ProviderEvent{"ev3", "provider_1", "ENDED", now.Add(2 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Observe(c, ProviderEvent{"ev4", "provider_1", "LIVE", now.Add(3 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.Get(c, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	if o.State != "ENDED" || o.PlaybackAllowed() {
		t.Fatal(o)
	}
}
func TestUnknownProviderQuarantineAndSuspension(t *testing.T) {
	s := fixture(t)
	c := context.Background()
	out, e := s.Observe(c, ProviderEvent{"unknown_event", "unknown_provider", "LIVE", time.Now()})
	if e != nil || out != "quarantined_unknown" {
		t.Fatal(out, e)
	}
	r, d := intent("suspend")
	o, e := s.Create(c, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.BindProvider(c, o.ID, "provider_1")
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.Suspend(c, o.ID, SuspensionDecision{o.ID, o.Revision, "moderator_1", "policy_abuse", true, time.Now().Add(time.Minute)})
	if e != nil || o.State != "SUSPENDED" || o.PlaybackAllowed() {
		t.Fatal(o, e)
	}
	out, e = s.Observe(c, ProviderEvent{"late", "provider_1", "LIVE", time.Now()})
	if e != nil || out != "ignored_stale_or_terminal" {
		t.Fatal(out, e)
	}
}
func TestDeniedInvalidUnavailableZeroEffects(t *testing.T) {
	s := fixture(t)
	c := context.Background()
	r, d := intent("denied")
	d.Allowed = false
	if _, e := s.Create(c, r, d, true); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	d.Allowed = true
	if _, e := s.Create(c, r, d, false); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	r.Title = strings.Repeat("x", 121)
	if _, e := s.Create(c, r, d, true); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	var n int
	if e := s.pool.QueryRow(c, "SELECT count(*) FROM "+s.schema+".stream_operations").Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 0 {
		t.Fatal(n)
	}
}
func TestWebhookAuthenticationBoundsAndSignedTimestamp(t *testing.T) {
	secret := strings.Repeat("k", 32)
	now := time.Now().Truncate(time.Second)
	raw := fmt.Sprintf(`{"timestamp":%d,"event":"stream.started"}`, now.Unix())
	signature := func(body string, ts int64) string {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(body))
		return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(m.Sum(nil)))
	}
	cases := []struct {
		name, body, header, key string
		status                  int
	}{{"valid", raw, signature(raw, now.Unix()), secret, 200}, {"missing_secret", raw, "", "", 503}, {"unsigned", raw, "", secret, 401}, {"wrong_signature", raw, "t=1,v1=00", secret, 401}, {"overflow", strings.Repeat("x", (1<<20)+1), "", secret, 413}, {"header_timestamp_substitution", raw, signature(raw, now.Unix()+1), secret, 401}, {"expired", raw, signature(raw, now.Unix()), secret, 401}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/hook", strings.NewReader(tc.body))
			r.Header.Set("Livepeer-Signature", tc.header)
			w := httptest.NewRecorder()
			clock := now
			if tc.name == "expired" {
				clock = now.Add(301 * time.Second)
			}
			_, ok := VerifyProviderWebhook(w, r, tc.key, clock, false)
			if w.Code != tc.status || ok != (tc.status == 200) {
				t.Fatal(w.Code, ok)
			}
		})
	}
}
func TestLegacyMutationsFailClosedWithoutDependencies(t *testing.T) {
	h := New(Deps{})
	for _, fn := range []func(*httptest.ResponseRecorder){func(w *httptest.ResponseRecorder) { h.create(w, httptest.NewRequest("POST", "/streams", nil)) }, func(w *httptest.ResponseRecorder) { h.goLive(w, httptest.NewRequest("POST", "/start", nil)) }, func(w *httptest.ResponseRecorder) { h.end(w, httptest.NewRequest("POST", "/end", nil)) }, func(w *httptest.ResponseRecorder) { h.webhook(w, httptest.NewRequest("POST", "/hook", nil)) }, func(w *httptest.ResponseRecorder) { h.heartbeat(w, httptest.NewRequest("POST", "/beat", nil)) }} {
		w := httptest.NewRecorder()
		fn(w)
		if w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
}

func TestActualHTTPProvider500RetainsEndingAndRetry(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("http500")
	o, e := s.Create(ctx, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.BindProvider(ctx, o.ID, "provider_500")
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.Transition(ctx, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, o.ID, "end", o.Revision, true, time.Now().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/stream/provider_500/terminate" {
			t.Error(r.Method, r.URL.Path)
		}
		w.WriteHeader(500)
		w.Write([]byte("private-provider-key"))
	}))
	defer server.Close()
	client := livepeer.New("public-fixture-key", server.URL, nil)
	if e = s.ReconcileTermination(ctx, o.ID, client); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	o, e = s.Get(ctx, o.ID)
	if e != nil || o.State != "ENDING" || o.RetryAt == nil {
		t.Fatal(o, e)
	}
}
func TestWrongTargetAndExpiredActionDecisions(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("action_binding")
	o, e := s.Create(ctx, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Suspend(ctx, o.ID, SuspensionDecision{"another", o.Revision, "moderator", "abuse", true, time.Now().Add(time.Minute)}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if _, e = s.Transition(ctx, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, "another", "end", o.Revision, true, time.Now().Add(time.Minute)}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if _, e = s.Transition(ctx, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, o.ID, "end", o.Revision, true, time.Now().Add(-time.Second)}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, o.ID)
	if e != nil || got.Revision != o.Revision || got.State != "REQUESTED" {
		t.Fatal(got, e)
	}
}
func TestLegacyReadsFailClosedWithoutPlayback(t *testing.T) {
	h := New(Deps{})
	for _, f := range []func(http.ResponseWriter, *http.Request){h.listLive, h.listByPool, h.getByID} {
		w := httptest.NewRecorder()
		f(w, httptest.NewRequest("GET", "/streams", nil))
		if w.Code != 503 || strings.Contains(w.Body.String(), "playbackUrl") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestSuspensionTerminationAcknowledgementReleasesQuota(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("suspended_ack")
	o, e := s.Create(ctx, r, d, true)
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.BindProvider(ctx, o.ID, "provider_suspended")
	if e != nil {
		t.Fatal(e)
	}
	o, e = s.Suspend(ctx, o.ID, SuspensionDecision{o.ID, o.Revision, "moderator", "abuse", true, time.Now().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.TerminationFailed(ctx, o.ID); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	if out, e := s.Observe(ctx, ProviderEvent{"terminated_suspended", "provider_suspended", "ENDED", now}); e != nil || out != "applied" {
		t.Fatal(out, e)
	}
	if out, e := s.Observe(ctx, ProviderEvent{"late_live_suspended", "provider_suspended", "LIVE", now.Add(time.Second)}); e != nil || out != "ignored_stale_or_terminal" {
		t.Fatal(out, e)
	}
	o, e = s.Get(ctx, o.ID)
	if e != nil || o.State != "ENDED" || o.RetryAt != nil || o.PlaybackAllowed() {
		t.Fatal(o, e)
	}
	for i := 0; i < 3; i++ {
		r, d := intent(fmt.Sprint("after_terminal", i))
		if _, e := s.Create(ctx, r, d, true); e != nil {
			t.Fatal(e)
		}
	}
}

func TestProviderEventUnserializableTimeRejectedBeforeInbox(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, stamp := range []time.Time{time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if _, e := s.Observe(ctx, ProviderEvent{"invalid_time", "provider_1", "LIVE", stamp}); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_inbox").Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
func TestCreatorDecisionExpiryWhileWaitingForAdmission(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer rollbackBounded(tx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", s.schema+":admission"); e != nil {
		t.Fatal(e)
	}
	r, d := intent("expire_waiting")
	d.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	result := make(chan error, 1)
	go func() { _, e := s.Create(ctx, r, d, true); result <- e }()
	time.Sleep(150 * time.Millisecond)
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_operations").Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}

func TestRequestedOutboxCommitsExactlyOnce(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("outbox_once")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(ctx, r, d, false); err != nil {
		t.Fatal(err)
	}
	var count int
	var operationID, revision, state string
	err = s.pool.QueryRow(ctx, "SELECT count(*),min(payload->>'operationId'),min(payload->>'revision'),min(payload->>'state') FROM "+s.schema+".stream_outbox WHERE operation_id=$1 AND kind='provision'", o.ID).Scan(&count, &operationID, &revision, &state)
	if err != nil || count != 1 || operationID != o.ID || revision != "1" || state != "REQUESTED" {
		t.Fatal(count, operationID, revision, state, err)
	}
}

func TestRequestedOutboxFailureRollsBackAdmission(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "ALTER TABLE "+s.schema+".stream_outbox ADD CONSTRAINT reject_provision CHECK(kind<>'provision')"); err != nil {
		t.Fatal(err)
	}
	r, d := intent("outbox_rollback")
	if _, err := s.Create(ctx, r, d, true); err == nil {
		t.Fatal("outbox failure accepted")
	}
	var operations, audits int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_operations").Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_audit").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if operations != 0 || audits != 0 {
		t.Fatal(operations, audits)
	}
}

func TestUnsignedStartedCannotEnterInboxOrChangePersistedState(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("unsigned-provider-start")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.BindProvider(ctx, o.ID, "unsigned_provider")
	if err != nil {
		t.Fatal(err)
	}
	h := New(Deps{Lifecycle: s, WebhookSecret: strings.Repeat("k", 32)})
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(`{"event":"stream.started","stream":{"id":"unsigned_provider"}}`))
	response := httptest.NewRecorder()
	h.webhook(response, request)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
	current, err := s.Get(ctx, o.ID)
	if err != nil || current.Revision != o.Revision || current.State != "READY" {
		t.Fatal(current, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_inbox", s.schema)).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestDelayedStartAfterTerminalObservationRecordsDisagreementOnce(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("terminal-disagreement")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.BindProvider(ctx, o.ID, "terminal_provider")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = s.Observe(ctx, ProviderEvent{"terminal_idle", o.ProviderID, "ENDED", now}); err != nil {
		t.Fatal(err)
	}
	event := ProviderEvent{"delayed_started", o.ProviderID, "LIVE", now.Add(-time.Second)}
	metrics := &LifecycleMetrics{}
	for i := 0; i < 2; i++ {
		outcome, err := s.Observe(ctx, event)
		if err != nil {
			t.Fatal(err)
		}
		metrics.ObserveWebhook(outcome)
	}
	current, err := s.Get(ctx, o.ID)
	if err != nil || current.State != "ENDED" || current.Revision != o.Revision+1 {
		t.Fatal(current, err)
	}
	if metrics.disagreement.Load() != 1 || metrics.duplicate.Load() != 1 {
		t.Fatal("replayed disagreement inflated metrics")
	}
}
