package publisher_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
)

const secret = "test-secret"

type received struct {
	events    []map[string]any
	sigValid  bool
	hadSigHdr bool
}

// recorder is an httptest webhook receiver that verifies the HMAC signature and
// captures the decoded batch. statusFn lets a test script per-attempt responses.
func recorder(t *testing.T, hits *[]received, mu *sync.Mutex, statusFn func(n int) int) *httptest.Server {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts := r.Header.Get("X-Sidiora-Timestamp")
		sig := r.Header.Get("X-Sidiora-Signature")
		rec := received{hadSigHdr: sig != ""}
		rec.sigValid = auth.VerifyWebhook(secret, ts, string(body), sig, time.Now(), 0) == nil
		var payload struct {
			Events []map[string]any `json:"events"`
		}
		_ = json.Unmarshal(body, &payload)
		rec.events = payload.Events
		mu.Lock()
		*hits = append(*hits, rec)
		mu.Unlock()

		code := http.StatusOK
		if statusFn != nil {
			code = statusFn(int(n.Add(1)))
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sampleEvents() []publisher.WebhookEvent {
	return []publisher.WebhookEvent{
		{EventName: "Swap", BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xabc", LogIndex: 0,
			Args: map[string]any{"amountIn": "100"}, TraceID: "0xabc:0"},
		{EventName: "MarketCreated", BlockNumber: 11, BlockTimestamp: 1700000001, TxHash: "0xdef", LogIndex: 1,
			Args: map[string]any{"pool": "0xpool"}},
	}
}

func TestBuildTargets(t *testing.T) {
	t.Parallel()
	// Per-URL secrets win; default fills the gap.
	tgts, err := publisher.BuildTargets([]string{"https://a", "https://b"}, "def", []string{"s1"})
	if err != nil {
		t.Fatalf("BuildTargets: %v", err)
	}
	if tgts[0].Secret != "s1" || tgts[1].Secret != "def" {
		t.Errorf("secrets = %q / %q, want s1 / def", tgts[0].Secret, tgts[1].Secret)
	}
	// No secret available -> error.
	if _, err := publisher.BuildTargets([]string{"https://a"}, "", nil); err == nil {
		t.Error("BuildTargets with no secret should error")
	}
}

func TestPublishBatchDeliversSigned(t *testing.T) {
	t.Parallel()
	var hits []received
	var mu sync.Mutex
	srv := recorder(t, &hits, &mu, nil)

	p := publisher.New(publisher.Options{
		Targets: []publisher.Target{{URL: srv.URL, Secret: secret}},
	})
	p.PublishBatch(context.Background(), sampleEvents())
	p.WaitForInflight(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 1 {
		t.Fatalf("got %d deliveries, want 1", len(hits))
	}
	if !hits[0].hadSigHdr || !hits[0].sigValid {
		t.Errorf("signature invalid: %+v", hits[0])
	}
	if len(hits[0].events) != 2 || hits[0].events[0]["eventName"] != "Swap" {
		t.Errorf("payload wrong: %+v", hits[0].events)
	}
	m := p.Snapshot()
	if m.TotalEventsPublished != 2 || m.TotalDeliveries != 1 || m.TotalFailures != 0 {
		t.Errorf("metrics = %+v", m)
	}
}

func TestPublishBatchMultipleTargets(t *testing.T) {
	t.Parallel()
	var h1, h2 []received
	var mu sync.Mutex
	a := recorder(t, &h1, &mu, nil)
	b := recorder(t, &h2, &mu, nil)
	p := publisher.New(publisher.Options{Targets: []publisher.Target{
		{URL: a.URL, Secret: secret}, {URL: b.URL, Secret: secret},
	}})
	p.PublishBatch(context.Background(), sampleEvents())
	p.Disconnect()
	mu.Lock()
	defer mu.Unlock()
	if len(h1) != 1 || len(h2) != 1 {
		t.Fatalf("deliveries a=%d b=%d, want 1 each", len(h1), len(h2))
	}
}

func TestPublishBatchNoOpPaths(t *testing.T) {
	t.Parallel()
	var hits []received
	var mu sync.Mutex
	srv := recorder(t, &hits, &mu, nil)
	p := publisher.New(publisher.Options{Targets: []publisher.Target{{URL: srv.URL, Secret: secret}}})

	p.PublishBatch(context.Background(), nil)            // empty events
	p.SetBackfillMode(true)                              // suppressed
	p.PublishBatch(context.Background(), sampleEvents()) // dropped (backfill)
	p.WaitForInflight(time.Second)
	mu.Lock()
	n := len(hits)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("expected 0 deliveries (empty + backfill), got %d", n)
	}

	// No targets at all is also a no-op.
	pEmpty := publisher.New(publisher.Options{})
	pEmpty.PublishBatch(context.Background(), sampleEvents())
	if pEmpty.Snapshot().TotalEventsPublished != 0 {
		t.Error("no-target publisher should not count events")
	}
}

func TestDeadLetterOnPermanentFailure(t *testing.T) {
	t.Parallel()
	var hits []received
	var mu sync.Mutex
	srv := recorder(t, &hits, &mu, func(int) int { return http.StatusInternalServerError })

	p := publisher.New(publisher.Options{
		Targets:  []publisher.Target{{URL: srv.URL, Secret: secret}},
		Attempts: 1, // no retry/backoff -> fast permanent failure
	})
	p.PublishBatch(context.Background(), sampleEvents())
	p.Disconnect()

	if p.DeadLetterCount() != 1 {
		t.Fatalf("DeadLetterCount = %d, want 1", p.DeadLetterCount())
	}
	dlq := p.DeadLetterQueue()
	if len(dlq) != 1 || dlq[0].TargetURL != srv.URL || len(dlq[0].Events) != 2 || dlq[0].LastError == "" {
		t.Errorf("dead-letter entry wrong: %+v", dlq[0])
	}
	m := p.Snapshot()
	if m.TotalFailures != 1 || m.TotalDeadLettered != 2 || m.TotalDeliveries != 0 {
		t.Errorf("metrics = %+v", m)
	}
	if drained := p.DrainDeadLetterQueue(); len(drained) != 1 {
		t.Errorf("drained = %d, want 1", len(drained))
	}
	if p.DeadLetterCount() != 0 {
		t.Error("queue not empty after drain")
	}
}

func TestRetryThenSucceed(t *testing.T) {
	t.Parallel()
	var hits []received
	var mu sync.Mutex
	// Fail the first attempt, succeed the second.
	srv := recorder(t, &hits, &mu, func(n int) int {
		if n == 1 {
			return http.StatusBadGateway
		}
		return http.StatusOK
	})
	p := publisher.New(publisher.Options{
		Targets:  []publisher.Target{{URL: srv.URL, Secret: secret}},
		Attempts: 2,
	})
	p.PublishBatch(context.Background(), sampleEvents())
	p.WaitForInflight(5 * time.Second)

	if p.DeadLetterCount() != 0 {
		t.Errorf("should have recovered, dead-letter = %d", p.DeadLetterCount())
	}
	if m := p.Snapshot(); m.TotalDeliveries != 1 {
		t.Errorf("deliveries = %d, want 1 after retry", m.TotalDeliveries)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 2 {
		t.Errorf("receiver hit %d times, want 2 (fail then success)", len(hits))
	}
}

func TestDedupViaRedis(t *testing.T) {
	url := internaltest.NewRedisURL(t)
	rdb, err := sharedredis.NewClient(url)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	var hits []received
	var mu sync.Mutex
	srv := recorder(t, &hits, &mu, nil)
	p := publisher.New(publisher.Options{
		Targets: []publisher.Target{{URL: srv.URL, Secret: secret}},
		Redis:   rdb,
	})

	ev := sampleEvents()
	p.PublishBatch(context.Background(), ev) // first: delivered + fingerprint reserved
	p.WaitForInflight(5 * time.Second)
	p.PublishBatch(context.Background(), ev) // identical batch: deduped, not delivered
	p.WaitForInflight(5 * time.Second)

	mu.Lock()
	n := len(hits)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("deliveries = %d, want 1 (second batch deduped)", n)
	}
	if m := p.Snapshot(); m.TotalDeduplicated == 0 {
		t.Errorf("TotalDeduplicated = 0, want >0")
	}
}

// TestPublishBatchFansOutToRedis exercises the realtime dual-delivery path: with
// NO webhook targets (a Redis-only deployment), every event must still be
// PUBLISHed to its indexer:<event> channel as the per-event envelope.
func TestPublishBatchFansOutToRedis(t *testing.T) {
	url := internaltest.NewRedisURL(t)
	rdb, err := sharedredis.NewClient(url)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	ctx := context.Background()
	sub := rdb.Subscribe(ctx, constants.ChannelSwap, constants.ChannelMarketCreated)
	if _, err := sub.Receive(ctx); err != nil { // wait for SUBSCRIBE to register
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()
	ch := sub.Channel()

	// No webhook targets — only the Redis path can deliver.
	p := publisher.New(publisher.Options{Redis: rdb})
	p.PublishBatch(ctx, sampleEvents())
	p.WaitForInflight(5 * time.Second)

	got := map[string]map[string]any{}
	timeout := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case msg := <-ch:
			var ev map[string]any
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				t.Fatalf("unmarshal redis event: %v", err)
			}
			got[msg.Channel] = ev
		case <-timeout:
			t.Fatalf("timed out; received %d of 2 channel messages", len(got))
		}
	}

	if ev := got[constants.ChannelSwap]; ev["eventName"] != "Swap" || ev["txHash"] != "0xabc" {
		t.Errorf("swap envelope wrong: %+v", ev)
	}
	if ev := got[constants.ChannelMarketCreated]; ev["eventName"] != "MarketCreated" {
		t.Errorf("market_created envelope wrong: %+v", ev)
	}
	if m := p.Snapshot(); m.TotalRedisPublishes != 2 {
		t.Errorf("TotalRedisPublishes = %d, want 2", m.TotalRedisPublishes)
	}
}
