// Package publisher fans decoded events out to downstream consumers over TWO
// independent delivery paths so a failure in either still reaches the consumer:
//
//  1. Redis pub/sub — each event is PUBLISHed to its indexer:<event> channel for
//     low-latency realtime fan-out (consumed by core/api's broker and the
//     trading-charts candle builder). Best-effort, fire-and-forget.
//  2. HMAC-SHA256-signed webhooks — the batch is POSTed to every configured
//     target for durable, retried delivery (parity with the TS
//     publishers/event-publisher.ts).
//
// The signature scheme is byte-identical to the TS receivers (and the shared Go
// auth.SignWebhook): "sha256=" + hex(HMAC_SHA256(secret, timestamp + "." +
// body)) over the RAW body, with an X-Sidiora-Timestamp header the receiver
// uses for its ±300s replay window (invariant i3). Webhook delivery is
// fire-and-forget with exponential backoff; permanent failures land in a bounded
// dead-letter queue. An optional Redis client provides batch dedup on restart
// (invariant i4/i9) and is also the transport for path 1. Because both paths can
// deliver the same event, consumers MUST be idempotent on (txHash, logIndex).
package publisher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
)

// eventChannel maps an indexer event name to its Redis pub/sub channel for the
// realtime fan-out path. Events with no realtime channel (e.g. TokenForTokenSwap)
// are intentionally absent — they reach consumers via the webhook path only.
var eventChannel = map[string]string{
	"MarketCreated":      constants.ChannelMarketCreated,
	"Swap":               constants.ChannelSwap,
	"PoolStateUpdated":   constants.ChannelPoolStateUpdated,
	"FeeRecorded":        constants.ChannelFeeRecorded,
	"FeeDistributed":     constants.ChannelFeeDistributed,
	"FeeStrategyChanged": constants.ChannelFeeStrategyChanged,
	"OpticalExecuted":    constants.ChannelOpticalExecuted,
	"ConfigUpdated":      constants.ChannelConfigUpdated,
}

const (
	headerTimestamp = "X-Sidiora-Timestamp"
	headerSignature = "X-Sidiora-Signature"

	httpTimeout    = 10 * time.Second
	defaultRetries = 3
	dedupTTL       = 600 * time.Second
	dlqCap         = 10000
)

// WebhookEvent is the wire shape of a single fanned-out event. Field names
// (and JSON tags) match the TS WebhookEvent exactly so a Go indexer can feed TS
// consumers and vice-versa.
type WebhookEvent struct {
	EventName      string         `json:"eventName"`
	BlockNumber    int64          `json:"blockNumber"`
	BlockTimestamp int64          `json:"blockTimestamp"`
	TxHash         string         `json:"txHash"`
	LogIndex       int            `json:"logIndex"`
	Args           map[string]any `json:"args"`
	TraceID        string         `json:"traceId,omitempty"`
}

// Target is one configured downstream consumer with its signing secret.
type Target struct {
	URL    string
	Secret string
}

// DeadLetterEntry records a permanently failed batch delivery.
type DeadLetterEntry struct {
	Events    []WebhookEvent `json:"events"`
	TargetURL string         `json:"targetUrl"`
	FailedAt  int64          `json:"failedAt"`
	Attempts  int            `json:"attempts"`
	LastError string         `json:"lastError"`
}

// Metrics is a snapshot of publisher counters for the /status endpoint.
type Metrics struct {
	TotalEventsPublished int64 `json:"totalEventsPublished"`
	TotalDeliveries      int64 `json:"totalDeliveries"`
	TotalFailures        int64 `json:"totalFailures"`
	TotalDeadLettered    int64 `json:"totalDeadLettered"`
	TotalDeduplicated    int64 `json:"totalDeduplicated"`
	TotalRedisPublishes  int64 `json:"totalRedisPublishes"`
	TotalRedisFailures   int64 `json:"totalRedisFailures"`
	CurrentInflight      int64 `json:"currentInflight"`
}

// BuildTargets pairs each URL with a signing secret: per-URL secret when
// provided (its length must equal urls — validated upstream in config), else
// the default secret. Errors when a URL has no secret (the receiver would 401).
func BuildTargets(urls []string, defaultSecret string, perURL []string) ([]Target, error) {
	out := make([]Target, 0, len(urls))
	for i, u := range urls {
		secret := defaultSecret
		if i < len(perURL) && perURL[i] != "" {
			secret = perURL[i]
		}
		if secret == "" {
			return nil, fmt.Errorf("publisher: no HMAC secret configured for %s (set WEBHOOK_HMAC_SECRET or WEBHOOK_HMAC_SECRETS[%d])", u, i)
		}
		out = append(out, Target{URL: u, Secret: secret})
	}
	return out, nil
}

// Options configures a Publisher.
type Options struct {
	Targets []Target
	Logger  *slog.Logger
	// Redis is optional; when set it provides batch dedup on restart.
	Redis *goredis.Client
	// Attempts overrides the per-target retry count (default 3).
	Attempts int
	// now is overridable for deterministic tests.
	now func() time.Time
}

// Publisher delivers signed webhook batches.
type Publisher struct {
	targets    []Target
	logger     *slog.Logger
	redis      *goredis.Client
	httpClient *http.Client
	attempts   int
	now        func() time.Time

	backfill atomic.Bool
	inflight atomic.Int64
	wg       sync.WaitGroup

	dlqMu sync.Mutex
	dlq   []DeadLetterEntry

	m struct {
		published   atomic.Int64
		deliveries  atomic.Int64
		failures    atomic.Int64
		deadLetters atomic.Int64
		deduped     atomic.Int64
		redisPub    atomic.Int64
		redisFail   atomic.Int64
	}
}

// New builds a Publisher. A non-positive Attempts defaults to 3.
func New(opts Options) *Publisher {
	attempts := opts.Attempts
	if attempts < 1 {
		attempts = defaultRetries
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}
	targets := make([]Target, 0, len(opts.Targets))
	for _, t := range opts.Targets {
		if t.URL != "" {
			targets = append(targets, t)
		}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if len(targets) == 0 {
		logger.Warn("publisher: no webhook URLs configured — events will not be dispatched")
	}
	return &Publisher{
		targets:    targets,
		logger:     logger,
		redis:      opts.Redis,
		httpClient: &http.Client{Timeout: httpTimeout},
		attempts:   attempts,
		now:        now,
	}
}

// SetBackfillMode toggles webhook suppression during backfill (parity with TS
// setBackfillMode — backfilled events are not re-fanned to live consumers).
func (p *Publisher) SetBackfillMode(enabled bool) {
	p.backfill.Store(enabled)
	p.logger.Info("publisher: backfill mode toggled", slog.Bool("backfillMode", enabled))
}

// PublishBatch fans events out over both delivery paths (Redis pub/sub +
// signed webhooks). It returns once each delivery goroutine has been scheduled
// (fire-and-forget); the indexing loop is never blocked on Redis or retry
// backoff.
func (p *Publisher) PublishBatch(ctx context.Context, events []WebhookEvent) {
	if len(events) == 0 || p.backfill.Load() {
		return
	}

	// Idempotency guard (i4): skip a batch we've already fanned out (restart
	// redelivery) so neither path re-fans it. Best-effort — Redis errors fall
	// through to delivery.
	if p.redis != nil && p.isDuplicate(ctx, events) {
		p.m.deduped.Add(int64(len(events)))
		p.logger.Info("publisher: duplicate batch skipped (i4)", slog.Int("count", len(events)))
		return
	}

	// Path 1: realtime fan-out via Redis pub/sub. One goroutine per batch so the
	// per-event publish order is preserved; tracked by wg for graceful drain.
	if p.redis != nil {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.publishRedis(ctx, events)
		}()
	}

	// Path 2: durable signed webhooks. No targets => Redis-only deployment.
	if len(p.targets) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		p.logger.Error("publisher: encode payload failed; batch dropped", slog.String("err", err.Error()))
		return
	}
	p.m.published.Add(int64(len(events)))

	for _, t := range p.targets {
		p.inflight.Add(1)
		p.wg.Add(1)
		go func(target Target) {
			defer p.wg.Done()
			defer p.inflight.Add(-1)
			if err := p.sendWithRetry(ctx, target, payload); err != nil {
				p.m.failures.Add(1)
				p.m.deadLetters.Add(int64(len(events)))
				p.pushDeadLetter(DeadLetterEntry{
					Events:    events,
					TargetURL: target.URL,
					FailedAt:  p.now().UnixMilli(),
					Attempts:  p.attempts,
					LastError: err.Error(),
				})
				p.logger.Error("publisher: delivery failed — moved to dead-letter queue",
					slog.String("url", target.URL), slog.Int("eventCount", len(events)), slog.String("err", err.Error()))
				return
			}
			p.m.deliveries.Add(1)
		}(t)
	}
}

// publishRedis PUBLISHes each event to its realtime Redis channel (the
// dual-delivery path). The message body is the per-event envelope — byte-shape
// identical to one element of the webhook batch — so a consumer can process
// either path interchangeably. Failures are logged and counted, never fatal:
// the webhook path is the durable backstop.
func (p *Publisher) publishRedis(ctx context.Context, events []WebhookEvent) {
	for i := range events {
		ch, ok := eventChannel[events[i].EventName]
		if !ok {
			continue
		}
		body, err := json.Marshal(events[i])
		if err != nil {
			p.logger.Error("publisher: encode redis event failed",
				slog.String("event", events[i].EventName), slog.String("err", err.Error()))
			continue
		}
		if err := p.redis.Publish(ctx, ch, body).Err(); err != nil {
			p.m.redisFail.Add(1)
			p.logger.Warn("publisher: redis publish failed",
				slog.String("channel", ch), slog.String("err", err.Error()))
			continue
		}
		p.m.redisPub.Add(1)
	}
}

// isDuplicate computes the batch fingerprint and reserves it in Redis (SET NX).
// Returns true when the key already existed (duplicate batch).
func (p *Publisher) isDuplicate(ctx context.Context, events []WebhookEvent) bool {
	h := sha256.New()
	for i, e := range events {
		if i > 0 {
			h.Write([]byte(","))
		}
		fmt.Fprintf(h, "%s:%d", e.TxHash, e.LogIndex)
	}
	fp := hex.EncodeToString(h.Sum(nil))[:16]
	key := "webhook:dedup:" + fp
	set, err := p.redis.SetNX(ctx, key, "1", dedupTTL).Result()
	if err != nil {
		return false // Redis unavailable — proceed without dedup.
	}
	return !set
}

// sendWithRetry delivers payload to one target with exponential backoff,
// re-signing per attempt so the timestamp stays inside the replay window.
func (p *Publisher) sendWithRetry(ctx context.Context, target Target, payload []byte) error {
	var lastErr error
	for i := 0; i < p.attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		timestamp := strconv.FormatInt(p.now().Unix(), 10)
		signature := auth.SignWebhook(target.Secret, timestamp, string(payload))

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerTimestamp, timestamp)
		req.Header.Set(headerSignature, signature)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			p.logger.Warn("publisher: delivery attempt failed",
				slog.String("url", target.URL), slog.Int("attempt", i+1), slog.String("err", err.Error()))
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("non-2xx status %d", resp.StatusCode)
			p.logger.Warn("publisher: non-OK webhook response",
				slog.String("url", target.URL), slog.Int("status", resp.StatusCode), slog.Int("attempt", i+1))
		}
		if i < p.attempts-1 {
			backoff := time.Duration(1<<uint(i)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("delivery failed")
	}
	return fmt.Errorf("delivery to %s failed after %d attempts: %w", target.URL, p.attempts, lastErr)
}

func (p *Publisher) pushDeadLetter(e DeadLetterEntry) {
	p.dlqMu.Lock()
	defer p.dlqMu.Unlock()
	p.dlq = append(p.dlq, e)
	if len(p.dlq) > dlqCap {
		p.dlq = p.dlq[len(p.dlq)-dlqCap:]
	}
}

// DeadLetterQueue returns a copy of the current dead-letter entries.
func (p *Publisher) DeadLetterQueue() []DeadLetterEntry {
	p.dlqMu.Lock()
	defer p.dlqMu.Unlock()
	out := make([]DeadLetterEntry, len(p.dlq))
	copy(out, p.dlq)
	return out
}

// DeadLetterCount returns the number of dead-letter entries.
func (p *Publisher) DeadLetterCount() int {
	p.dlqMu.Lock()
	defer p.dlqMu.Unlock()
	return len(p.dlq)
}

// DrainDeadLetterQueue empties and returns the dead-letter queue.
func (p *Publisher) DrainDeadLetterQueue() []DeadLetterEntry {
	p.dlqMu.Lock()
	defer p.dlqMu.Unlock()
	drained := p.dlq
	p.dlq = nil
	return drained
}

// Snapshot returns the current metrics.
func (p *Publisher) Snapshot() Metrics {
	return Metrics{
		TotalEventsPublished: p.m.published.Load(),
		TotalDeliveries:      p.m.deliveries.Load(),
		TotalFailures:        p.m.failures.Load(),
		TotalDeadLettered:    p.m.deadLetters.Load(),
		TotalDeduplicated:    p.m.deduped.Load(),
		TotalRedisPublishes:  p.m.redisPub.Load(),
		TotalRedisFailures:   p.m.redisFail.Load(),
		CurrentInflight:      p.inflight.Load(),
	}
}

// WaitForInflight blocks until all in-flight deliveries finish or timeout
// elapses (graceful drain — invariant i7).
func (p *Publisher) WaitForInflight(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		p.logger.Warn("publisher: timed out waiting for inflight webhooks", slog.Int64("inflight", p.inflight.Load()))
	}
}

// Disconnect waits for in-flight deliveries to drain (default 10s).
func (p *Publisher) Disconnect() { p.WaitForInflight(httpTimeout) }
