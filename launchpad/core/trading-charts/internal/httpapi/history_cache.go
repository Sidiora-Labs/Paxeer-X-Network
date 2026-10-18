package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type PostgresHistoryRevision struct {
	Pool   *pgxpool.Pool
	Schema string
}

func (s PostgresHistoryRevision) Read(ctx context.Context, r HistoryRequest) (string, error) {
	if err := validateHistoryRequest(r); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if s.Pool == nil || !chartSchemaPattern.MatchString(s.Schema) {
		return "", errors.New("revision authority unavailable")
	}
	rows, err := s.Pool.Query(ctx, "SELECT start::text,snapshot->>'revision' FROM "+pgx.Identifier{s.Schema, "chart_exact_v1"}.Sanitize()+" WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4 ORDER BY start LIMIT 5001", r.Symbol, r.Timeframe, r.From, r.To)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items := [][2]string{}
	for rows.Next() {
		var start, revision string
		if err = rows.Scan(&start, &revision); err != nil {
			return "", err
		}
		if len(start) > 12 || len(revision) > 78 || !integerPattern.MatchString(start) || !integerPattern.MatchString(revision) || revision == "0" {
			return "", errors.New("invalid revision source")
		}
		epoch, parseErr := strconv.ParseInt(start, 10, 64)
		if parseErr != nil || epoch < r.From || epoch >= r.To || (len(items) > 0 && items[len(items)-1][0] == start) {
			return "", errors.New("invalid revision candle identity")
		}
		items = append(items, [2]string{start, revision})
		if len(items) > MaxHistoryBars {
			return "", errors.New("history revision range exceeds bound")
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

var chartSchemaPattern = poolSchemaPattern()

func poolSchemaPattern() *regexp.Regexp { return regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`) }

type HistoricalCachePermit struct {
	RevisionDigest         string
	InvalidationEvidenceID string
	Complete               bool
	MaxAge                 time.Duration
}
type chartCacheValue struct {
	cacheState  string
	originNanos int64
	Scope       string    `json:"scope"`
	Watermark   []byte    `json:"watermark"`
	Body        []byte    `json:"body"`
	ETag        string    `json:"etag"`
	Policy      string    `json:"policy"`
	Expiry      time.Time `json:"expiry"`
}

func historyRequestKey(r HistoryRequest, mode, algorithm, endpoint string, permit *HistoricalCachePermit) string {
	filter, _ := json.Marshal([]any{"chart-history-v3", "125", r, mode, algorithm, endpoint, permit})
	filterHash := sha256.Sum256(filter)
	return hex.EncodeToString(filterHash[:])
}
func chartETag(key string, watermark, body []byte) string {
	raw, _ := json.Marshal([]any{key, watermark, body})
	digest := sha256.Sum256(raw)
	return `"` + hex.EncodeToString(digest[:]) + `"`
}
func validChartCache(value chartCacheValue, key string, r HistoryRequest, mode, algorithm string) bool {
	if value.Scope != key || len(value.Body) == 0 || len(value.Body) > 16*1024*1024 || !value.Expiry.After(time.Now()) || value.Expiry.After(time.Now().Add(60*time.Second)) || value.ETag != chartETag(key, value.Watermark, value.Body) {
		return false
	}
	if interfaces.ValidateJSON("Watermark", value.Watermark) != nil {
		return false
	}
	var watermark interfaces.Watermark
	if json.Unmarshal(value.Watermark, &watermark) != nil || watermark.Integrity != "healthy" {
		return false
	}
	if value.Policy != "private, no-cache" {
		validPolicy := false
		for i := 1; i <= 60; i++ {
			validPolicy = validPolicy || value.Policy == "public, max-age="+strconv.Itoa(i)+", must-revalidate"
		}
		if !validPolicy {
			return false
		}
	}
	var body struct {
		Status       string          `json:"s"`
		Mode         string          `json:"mode"`
		Algorithm    string          `json:"algorithmVersion"`
		Snapshot     string          `json:"snapshotId"`
		Watermark    json.RawMessage `json:"watermark"`
		Times        []int64         `json:"t"`
		Open         []float64       `json:"o"`
		High         []float64       `json:"h"`
		Low          []float64       `json:"l"`
		Close        []float64       `json:"c"`
		Volume       []float64       `json:"v"`
		PriceHistory *struct {
			Times []int64   `json:"t"`
			Open  []float64 `json:"o"`
			High  []float64 `json:"h"`
			Low   []float64 `json:"l"`
			Close []float64 `json:"c"`
		} `json:"priceHistory"`
	}
	if json.Unmarshal(value.Body, &body) != nil || body.Mode != mode || body.Algorithm != algorithm || body.Snapshot == "" || !bytes.Equal(body.Watermark, value.Watermark) || len(body.Times) > MaxHistoryBars || (r.Countback > 0 && len(body.Times) > r.Countback) {
		return false
	}
	if body.Status == "unavailable" && mode == "total_supply_valuation" && body.PriceHistory != nil {
		if len(body.PriceHistory.Times) != len(body.Times) {
			return false
		}
		for i, n := range body.Times {
			if body.PriceHistory.Times[i] != n {
				return false
			}
		}
		body.Open, body.High, body.Low, body.Close = body.PriceHistory.Open, body.PriceHistory.High, body.PriceHistory.Low, body.PriceHistory.Close
	} else if (len(body.Times) == 0 && body.Status != "no_data") || (len(body.Times) > 0 && body.Status != "ok") {
		return false
	}
	for i, start := range body.Times {
		if start < r.From || start >= r.To || (i > 0 && body.Times[i-1] >= start) {
			return false
		}
	}
	for _, array := range [][]float64{body.Open, body.High, body.Low, body.Close, body.Volume} {
		if len(array) != len(body.Times) {
			return false
		}
		for _, n := range array {
			if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				return false
			}
		}
	}
	return true
}

type chartFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   chartCacheValue
	err     error
}
type HistoryCache struct {
	Redis    *redis.Client
	Revision PostgresHistoryRevision
	origin   chan struct{}
	mu       sync.Mutex
	flights  map[string]*chartFlight
	entries  map[string]chartCacheValue
	fetches  atomic.Uint64
}

func NewHistoryCache(rdb *redis.Client, revision PostgresHistoryRevision, maxOrigins int) (*HistoryCache, error) {
	if maxOrigins < 1 || maxOrigins > 32 {
		return nil, errors.New("invalid bounded history concurrency")
	}
	return &HistoryCache{Redis: rdb, Revision: revision, origin: make(chan struct{}, maxOrigins), flights: map[string]*chartFlight{}, entries: map[string]chartCacheValue{}}, nil
}
func (c *HistoryCache) OriginFetches() uint64 { return c.fetches.Load() }
func (c *HistoryCache) run(ctx context.Context, key string, load func(context.Context) (chartCacheValue, error)) (chartCacheValue, error) {
	if ctx.Err() != nil {
		return chartCacheValue{}, ctx.Err()
	}
	c.mu.Lock()
	flight := c.flights[key]
	leader := flight == nil
	if flight == nil {
		if len(c.flights) >= 256 {
			c.mu.Unlock()
			return chartCacheValue{}, errors.New("history admission exhausted")
		}
		worker, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		flight = &chartFlight{done: make(chan struct{}), cancel: cancel}
		c.flights[key] = flight
		go func() {
			value, err := load(worker)
			if worker.Err() != nil {
				err = worker.Err()
			}
			cancel()
			c.mu.Lock()
			flight.value = value
			flight.err = err
			delete(c.flights, key)
			close(flight.done)
			c.mu.Unlock()
		}()
	}
	flight.waiters++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		flight.waiters--
		if flight.waiters == 0 {
			flight.cancel()
		}
		c.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return chartCacheValue{}, ctx.Err()
	case <-flight.done:
		value := flight.value
		if !leader && value.cacheState == "miss" {
			value.cacheState = "coalesced"
			value.originNanos = 0
		}
		return value, flight.err
	}
}
func (c *HistoryCache) read(ctx context.Context, r HistoryRequest, mode, algorithm string, permit *HistoricalCachePermit, source CanonicalCandleClient, supply *RPCSupplySource) (chartCacheValue, error) {
	if err := validateHistoryRequest(r); err != nil {
		return chartCacheValue{}, err
	}
	if (mode != "price" && mode != "total_supply_valuation") || algorithm == "" || len(algorithm) > 160 || c.origin == nil {
		return chartCacheValue{}, errors.New("invalid history cache authority")
	}
	requestKey := historyRequestKey(r, mode, algorithm, source.Endpoint, permit)
	return c.run(ctx, requestKey, func(ctx context.Context) (chartCacheValue, error) {
		select {
		case c.origin <- struct{}{}:
		case <-ctx.Done():
			return chartCacheValue{}, ctx.Err()
		}
		defer func() { <-c.origin }()
		version, err := c.Revision.Read(ctx, r)
		if err != nil {
			return chartCacheValue{}, err
		}
		key := "chart:v3:" + requestKey + ":" + version
		c.mu.Lock()
		cached, ok := c.entries[key]
		c.mu.Unlock()
		if ok && validChartCache(cached, key, r, mode, algorithm) {
			cached.cacheState, cached.originNanos = "hit", 0
			return cached, nil
		}
		redisHealthy := c.Redis != nil
		lockKey := key + ":fill"
		owner := uuid.NewString()
		locked := false
		if redisHealthy {
			value, e := c.Redis.Get(ctx, key).Bytes()
			if e == nil && len(value) <= 16*1024*1024 && json.Unmarshal(value, &cached) == nil && validChartCache(cached, key, r, mode, algorithm) {
				cached.cacheState, cached.originNanos = "hit", 0
				return cached, nil
			}
			if e != nil && !errors.Is(e, redis.Nil) {
				redisHealthy = false
			}
		}
		if redisHealthy {
			for {
				locked, err = c.Redis.SetNX(ctx, lockKey, owner, 3*time.Second).Result()
				if err != nil {
					redisHealthy = false
					break
				}
				if locked {
					break
				}
				select {
				case <-ctx.Done():
					return chartCacheValue{}, ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
				raw, e := c.Redis.Get(ctx, key).Bytes()
				if e == nil && len(raw) <= 16*1024*1024 && json.Unmarshal(raw, &cached) == nil && validChartCache(cached, key, r, mode, algorithm) {
					cached.cacheState, cached.originNanos = "hit", 0
					return cached, nil
				}
				if e != nil && !errors.Is(e, redis.Nil) {
					redisHealthy = false
					break
				}
			}
		}
		if locked {
			defer func() {
				release, done := context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer done()
				_ = c.Redis.Eval(release, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`, []string{lockKey}, owner).Err()
			}()
		}

		c.fetches.Add(1)
		originStarted := time.Now()
		page, err := source.Read(ctx, r)
		originNanos := time.Since(originStarted).Nanoseconds()
		if err != nil {
			return chartCacheValue{}, err
		}
		if page.Algorithm != algorithm {
			return chartCacheValue{}, errors.New("history algorithm scope mismatch")
		}
		body, err := valuationHistory(ctx, page, supply, mode)
		if err != nil {
			return chartCacheValue{}, err
		}
		after, err := c.Revision.Read(ctx, r)
		if err != nil || after != version {
			return chartCacheValue{}, errors.New("history revision changed during fill")
		}
		watermark, _ := json.Marshal(page.Watermark)
		policy := "private, no-cache"
		ttl := 500 * time.Millisecond
		if page.Complete && permit != nil && permit.Complete && permit.RevisionDigest == version && permit.InvalidationEvidenceID != "" && permit.MaxAge >= time.Second && permit.MaxAge <= 60*time.Second {
			ttl = permit.MaxAge
			policy = "public, max-age=" + strconv.FormatInt(int64(ttl/time.Second), 10) + ", must-revalidate"
		}
		value := chartCacheValue{cacheState: "miss", originNanos: originNanos, Scope: key, Watermark: watermark, Body: body, ETag: chartETag(key, watermark, body), Policy: policy, Expiry: time.Now().Add(ttl)}
		if ctx.Err() != nil || !validChartCache(value, key, r, mode, algorithm) {
			return chartCacheValue{}, errors.New("invalid canonical history cache fill")
		}
		c.mu.Lock()
		if len(c.entries) >= 128 {
			keys := make([]string, 0, len(c.entries))
			for k := range c.entries {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			delete(c.entries, keys[0])
		}
		c.entries[key] = value
		c.mu.Unlock()
		if redisHealthy {
			raw, _ := json.Marshal(value)
			if len(raw) <= 16*1024*1024 {
				_ = c.Redis.Set(ctx, key, raw, ttl).Err()
			}
		}
		return value, nil
	})
}

type ProductionHistoryHandler struct {
	Source           CanonicalCandleClient
	Supply           *RPCSupplySource
	Cache            *HistoryCache
	Algorithm        string
	HistoricalPermit *HistoricalCachePermit
}

func (h ProductionHistoryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.RawQuery) > 16384 {
		historyError(w, 422, "FIELD_INVALID", "history query too large", false)
		return
	}
	q, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", "malformed history query", false)
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "price"
	}
	if len(q["mode"]) > 1 || (mode != "price" && mode != "total_supply_valuation") {
		historyError(w, 422, "FIELD_INVALID", "unsupported chart mode", false)
		return
	}
	q.Del("mode")
	r, err := parseHistory(q)
	if err != nil || q.Has("cursor") {
		historyError(w, 422, "FIELD_INVALID", "invalid versioned history request", false)
		return
	}
	if r.From == 0 && r.Countback > 0 && r.To > maxHistorySpan {
		r.From = r.To - maxHistorySpan
	}
	if h.Cache == nil || h.Algorithm == "" {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "history cache authority unavailable", true)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 3500*time.Millisecond)
	defer cancel()
	value, err := h.Cache.read(ctx, r, mode, h.Algorithm, h.HistoricalPermit, h.Source, h.Supply)
	if err != nil || ctx.Err() != nil {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "canonical history unavailable", true)
		return
	}
	w.Header().Set("X-Chart-Cache", value.cacheState)
	if value.cacheState == "miss" {
		w.Header().Set("Server-Timing", fmt.Sprintf("chart-origin;dur=%.6f", float64(value.originNanos)/1e6))
	}
	w.Header().Set("ETag", value.ETag)
	w.Header().Set("Cache-Control", value.Policy)
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	if req.Header.Get("If-None-Match") == value.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(value.Body)
}
