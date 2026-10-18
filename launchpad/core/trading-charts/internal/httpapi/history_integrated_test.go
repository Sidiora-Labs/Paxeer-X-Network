package httpapi

import (
	"context"
	"encoding/json"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProductionTotalSupplyValuation(t *testing.T) {
	address := "0x1111111111111111111111111111111111111111"
	for _, decimals := range []interfaces.SafeInteger{6, 18} {
		for _, units := range []string{"1000", "1000000000"} {
			position := interfaces.ChainPosition{ChainId: "125", Height: "123", BlockHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			price := interfaces.Price{Numerator: "2", Denominator: "1", BaseAsset: interfaces.AssetRef{Address: &address, Decimals: &decimals}, Source: interfaces.PriceSource{Position: &position}}
			amount, _ := new(big.Int).SetString(units, 10)
			amount.Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
			supply := SupplySnapshot{Token: address, Decimals: int(decimals), TotalSupplyRaw: amount.String(), SourceBlock: "123", SourceHash: position.BlockHash, Alignment: "canonical_block_end"}
			got, err := totalSupplyValuation(price, supply)
			expected, _ := new(big.Int).SetString(units, 10)
			expected.Mul(expected, big.NewInt(2))
			if err != nil || got.Cmp(new(big.Rat).SetInt(expected)) != 0 {
				t.Fatalf("%s decimals%d: %v %v", units, decimals, got, err)
			}
			supply.SourceBlock = "124"
			if _, err = totalSupplyValuation(price, supply); err == nil {
				t.Fatal("unaligned historical supply accepted")
			}
		}
	}
}
func TestProductionUnavailableSupplyPreservesPrice(t *testing.T) {
	price := interfaces.Price{Numerator: "2", Denominator: "1"}
	bar := interfaces.Candle{Start: "2026-10-12T00:00:00.000Z", Open: price, High: price, Low: price, Close: price, VolumeUsdlRaw: "1000000"}
	raw, err := valuationHistory(context.Background(), CanonicalCandlePage{Items: []interfaces.Candle{bar}}, nil, "price")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Status    string    `json:"s"`
		Available bool      `json:"valuationAvailable"`
		Close     []float64 `json:"c"`
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Available || len(body.Close) != 1 || body.Close[0] != 2 {
		t.Fatalf("price availability lost: %s", raw)
	}
}
func TestProductionRevisionChangesOnHistoricalCorrection(t *testing.T) {
	pool := historyPostgres(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, "CREATE SCHEMA production_revision; CREATE TABLE production_revision.chart_exact(chain_id text,pool text,interval text,start bigint,snapshot jsonb); CREATE VIEW production_revision.chart_exact_v1 AS SELECT * FROM production_revision.chart_exact")
	if err != nil {
		t.Fatal(err)
	}
	request := HistoryRequest{Symbol: "0x1111111111111111111111111111111111111111", Timeframe: "1m", From: 1, To: 120}
	_, err = pool.Exec(ctx, "INSERT INTO production_revision.chart_exact VALUES('125',$1,'1m',60,'{\"revision\":\"1\"}')", request.Symbol)
	if err != nil {
		t.Fatal(err)
	}
	authority := PostgresHistoryRevision{Pool: pool, Schema: "production_revision"}
	before, err := authority.Read(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, "UPDATE production_revision.chart_exact SET snapshot='{\"revision\":\"2\"}' WHERE pool=$1", request.Symbol)
	if err != nil {
		t.Fatal(err)
	}
	after, err := authority.Read(ctx, request)
	if err != nil || before == after {
		t.Fatalf("correction invisible: %s %s %v", before, after, err)
	}
}
func TestProductionConcurrentCanonicalHistory(t *testing.T) {
	endpoint := os.Getenv("L11_CANONICAL_ENDPOINT")
	dsn := os.Getenv("L11_HISTORY_POSTGRES_DSN")
	redisURL := os.Getenv("L11_HISTORY_REDIS_URL")
	if endpoint == "" || dsn == "" || redisURL == "" {
		t.Fatal("real canonical API, revision PostgreSQL and Redis fixtures required")
	}
	from, err := strconv.ParseInt(os.Getenv("L11_HISTORY_FROM"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	to, err := strconv.ParseInt(os.Getenv("L11_HISTORY_TO"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	request := HistoryRequest{Symbol: os.Getenv("L11_HISTORY_POOL"), Timeframe: "1m", From: from, To: to}
	if !poolAddressPattern.MatchString(request.Symbol) || to <= from {
		t.Fatal("invalid actual history fixture scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(options)
	defer rdb.Close()
	if err = rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	cache, err := NewHistoryCache(rdb, PostgresHistoryRevision{Pool: pool, Schema: "candles"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 1000)
	var workers sync.WaitGroup
	for i := 0; i < 1000; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := cache.read(ctx, request, "price", os.Getenv("L11_HISTORY_ALGORITHM"), nil, CanonicalCandleClient{Endpoint: endpoint}, nil)
			if err != nil {
				failures <- err
			}
		}()
	}
	close(start)
	workers.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if calls := cache.OriginFetches(); calls > 2 {
		t.Fatalf("1000 identical requests produced %d origins", calls)
	}
}

func TestProductionCachePoisoningAndETagIsolation(t *testing.T) {
	endpoint, dsn, redisURL := os.Getenv("L11_CANONICAL_ENDPOINT"), os.Getenv("L11_HISTORY_POSTGRES_DSN"), os.Getenv("L11_HISTORY_REDIS_URL")
	if endpoint == "" || dsn == "" || redisURL == "" {
		t.Fatal("real canonical API, PostgreSQL and Redis required")
	}
	from, err := strconv.ParseInt(os.Getenv("L11_HISTORY_FROM"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	to, err := strconv.ParseInt(os.Getenv("L11_HISTORY_TO"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	request := HistoryRequest{Symbol: os.Getenv("L11_HISTORY_POOL"), Timeframe: "1m", From: from, To: to}
	algorithm := os.Getenv("L11_HISTORY_ALGORITHM")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(options)
	defer rdb.Close()
	if err = rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	revision := PostgresHistoryRevision{Pool: pool, Schema: "candles"}
	cache, err := NewHistoryCache(rdb, revision, 2)
	if err != nil {
		t.Fatal(err)
	}
	source := CanonicalCandleClient{Endpoint: endpoint}
	original, err := cache.read(ctx, request, "price", algorithm, nil, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !validChartCache(original, original.Scope, request, "price", algorithm) {
		t.Fatal("actual origin value invalid")
	}
	otherPool := "0x2222222222222222222222222222222222222222"
	if request.Symbol == otherPool {
		otherPool = "0x3333333333333333333333333333333333333333"
	}
	variants := []struct {
		request                   HistoryRequest
		mode, algorithm, endpoint string
		permit                    *HistoricalCachePermit
	}{
		{request, "total_supply_valuation", algorithm, endpoint, nil},
		{request, "price", algorithm + "-different", endpoint, nil},
		{HistoryRequest{Symbol: otherPool, Timeframe: request.Timeframe, From: from, To: to}, "price", algorithm, endpoint, nil},
		{HistoryRequest{Symbol: request.Symbol, Timeframe: "5m", From: from, To: to}, "price", algorithm, endpoint, nil},
		{HistoryRequest{Symbol: request.Symbol, Timeframe: request.Timeframe, From: from + 1, To: to}, "price", algorithm, endpoint, nil},
		{HistoryRequest{Symbol: request.Symbol, Timeframe: request.Timeframe, From: from, To: to, Countback: 1}, "price", algorithm, endpoint, nil},
		{request, "price", algorithm, endpoint + "/other", nil},
		{request, "price", algorithm, endpoint, &HistoricalCachePermit{Complete: true, RevisionDigest: "other", InvalidationEvidenceID: "other", MaxAge: time.Second}},
	}
	for _, variant := range variants {
		key := historyRequestKey(variant.request, variant.mode, variant.algorithm, variant.endpoint, variant.permit)
		if key == historyRequestKey(request, "price", algorithm, endpoint, nil) || validChartCache(original, "chart:v3:"+key+":revision", variant.request, variant.mode, variant.algorithm) {
			t.Fatal("cache scope admitted transplanted value", variant)
		}
	}
	version, err := revision.Read(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	key := "chart:v3:" + historyRequestKey(request, "price", algorithm, endpoint, nil) + ":" + version
	poisoned := original
	poisoned.Body = []byte(`{"s":"no_data"}`)
	poisoned.Expiry = time.Now().Add(time.Second)
	raw, err := json.Marshal(poisoned)
	if err != nil {
		t.Fatal(err)
	}
	if err = rdb.Set(ctx, key, raw, 2*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.entries = map[string]chartCacheValue{}
	cache.mu.Unlock()
	before := cache.OriginFetches()
	recovered, err := cache.read(ctx, request, "price", algorithm, nil, source, nil)
	if err != nil || cache.OriginFetches() != before+1 || recovered.ETag != original.ETag {
		t.Fatal("poisoned cache did not recover from actual origin", err, cache.OriginFetches(), recovered.ETag)
	}
	handler := ProductionHistoryHandler{Source: source, Cache: cache, Algorithm: algorithm}
	query := "/v2/udf/history?symbol=" + otherPool + "&resolution=1&from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	req := httptest.NewRequest("GET", query, nil)
	req.Header.Set("If-None-Match", original.ETag)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code == http.StatusNotModified {
		t.Fatal("ETag from one pool returned cross-pool304")
	}
}

func TestProductionMalformedExactPriceNeverReturnsSuccessfulZero(t *testing.T) {
	price := interfaces.Price{Numerator: "2", Denominator: "1"}
	for _, raw := range []string{"NaN", "", "01", "-1", "+1", "1.0", strings.Repeat("9", 513)} {
		invalid := price
		invalid.Numerator = interfaces.PriceComponent(raw)
		bar := interfaces.Candle{Start: "2026-10-12T00:00:00.000Z", Open: invalid, High: price, Low: price, Close: price, VolumeUsdlRaw: "1000000"}
		if body, err := valuationHistory(context.Background(), CanonicalCandlePage{Items: []interfaces.Candle{bar}}, nil, "price"); err == nil || body != nil {
			t.Fatal("malformed price became successful candle", raw, string(body))
		}
	}
	price.Numerator = interfaces.PriceComponent("1" + strings.Repeat("0", 511))
	price.Denominator = interfaces.PositivePriceComponent("1" + strings.Repeat("0", 511))
	rat, err := exactPrice(price)
	if err != nil || rat.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatal("frozen arbitrary-precision price lost", rat, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if body, err := valuationHistory(cancelled, CanonicalCandlePage{}, nil, "price"); err == nil || body != nil {
		t.Fatal("cancellation became successful no_data", string(body), err)
	}
}

func TestProductionCacheRejectsMalformedValues(t *testing.T) {
	request := HistoryRequest{Symbol: udfPool, Timeframe: "1m", From: 0, To: 120}
	watermark := &interfaces.Watermark{ChainId: "125", Height: "1", BlockHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Integrity: "healthy", ObservedAt: "2026-10-12T00:00:00.000Z", ProjectionRevision: "1"}
	price := interfaces.Price{Numerator: "2", Denominator: "1"}
	bar := interfaces.Candle{Start: "1970-01-01T00:01:00.000Z", Open: price, High: price, Low: price, Close: price, VolumeUsdlRaw: "1000000"}
	page := CanonicalCandlePage{Items: []interfaces.Candle{bar}, Algorithm: "algorithm-v1", SnapshotID: "11111111-1111-4111-8111-111111111111", Watermark: watermark}
	body, err := valuationHistory(context.Background(), page, nil, "price")
	if err != nil {
		t.Fatal(err)
	}
	watermarkJSON, err := json.Marshal(watermark)
	if err != nil {
		t.Fatal(err)
	}
	key := "chart:v3:" + historyRequestKey(request, "price", page.Algorithm, "http://canonical.example", nil) + ":revision"
	value := chartCacheValue{Scope: key, Watermark: watermarkJSON, Body: body, Policy: "private, no-cache", Expiry: time.Now().Add(time.Second)}
	value.ETag = chartETag(key, value.Watermark, value.Body)
	if !validChartCache(value, key, request, "price", page.Algorithm) {
		t.Fatal("typed adapter output rejected", string(body))
	}
	for _, field := range []string{"s", "mode", "algorithmVersion", "snapshotId", "t", "o", "v"} {
		var poisoned map[string]any
		if err := json.Unmarshal(body, &poisoned); err != nil {
			t.Fatal(err)
		}
		switch field {
		case "s":
			poisoned[field] = "no_data"
		case "mode":
			poisoned[field] = "total_supply_valuation"
		case "algorithmVersion":
			poisoned[field] = "different"
		case "snapshotId":
			poisoned[field] = ""
		case "t":
			poisoned[field] = []int64{120}
		case "o":
			poisoned[field] = []float64{}
		case "v":
			poisoned[field] = []float64{-1}
		}
		candidate := value
		candidate.Body, err = json.Marshal(poisoned)
		if err != nil {
			t.Fatal(err)
		}
		candidate.ETag = chartETag(key, candidate.Watermark, candidate.Body)
		if validChartCache(candidate, key, request, "price", page.Algorithm) {
			t.Fatal("semantically poisoned cache accepted", field)
		}
	}
	for _, field := range []string{"scope", "etag", "expiry", "policy", "watermark"} {
		candidate := value
		switch field {
		case "scope":
			candidate.Scope = "different"
		case "etag":
			candidate.ETag = `"replayed"`
		case "expiry":
			candidate.Expiry = time.Now().Add(24 * time.Hour)
		case "policy":
			candidate.Policy = "public, max-age=31536000"
		case "watermark":
			candidate.Watermark = []byte("null")
			candidate.ETag = chartETag(key, candidate.Watermark, candidate.Body)
		}
		if validChartCache(candidate, key, request, "price", page.Algorithm) {
			t.Fatal("invalid cache envelope accepted", field)
		}
	}
}

func TestProductionOriginTelemetryUsesActualFetch(t *testing.T) {
	endpoint, dsn := os.Getenv("L11_CANONICAL_ENDPOINT"), os.Getenv("L11_HISTORY_POSTGRES_DSN")
	if endpoint == "" || dsn == "" {
		t.Fatal("real canonical API and PostgreSQL required")
	}
	from, err := strconv.ParseInt(os.Getenv("L11_HISTORY_FROM"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	to, err := strconv.ParseInt(os.Getenv("L11_HISTORY_TO"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	request := HistoryRequest{Symbol: os.Getenv("L11_HISTORY_POOL"), Timeframe: "1m", From: from, To: to}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache, err := NewHistoryCache(nil, PostgresHistoryRevision{Pool: pool, Schema: "candles"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	algorithm := os.Getenv("L11_HISTORY_ALGORITHM")
	source := CanonicalCandleClient{Endpoint: endpoint}
	first, err := cache.read(ctx, request, "price", algorithm, nil, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.cacheState != "miss" || first.originNanos <= 0 || cache.OriginFetches() != 1 {
		t.Fatal("actual origin fetch omitted timing", first.cacheState, first.originNanos, cache.OriginFetches())
	}
	second, err := cache.read(ctx, request, "price", algorithm, nil, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.cacheState != "hit" || second.originNanos != 0 || cache.OriginFetches() != 1 {
		t.Fatal("cache hit fabricated new origin timing", second.cacheState, second.originNanos, cache.OriginFetches())
	}
	requestURL := "/v2/udf/history?symbol=" + request.Symbol + "&resolution=1&from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	response := httptest.NewRecorder()
	(ProductionHistoryHandler{Source: source, Cache: cache, Algorithm: algorithm}).ServeHTTP(response, httptest.NewRequest("GET", requestURL, nil))
	if response.Code != 200 || response.Header().Get("X-Chart-Cache") != "hit" || response.Header().Get("Server-Timing") != "" {
		t.Fatal("hit published origin timing", response.Code, response.Header())
	}
}
