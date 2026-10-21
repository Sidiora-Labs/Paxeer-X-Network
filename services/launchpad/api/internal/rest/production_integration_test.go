package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func productionReadsFixture(t *testing.T) (*pgxpool.Pool, *store.AccountingReadModel, *broker.PostgresStream, string, json.RawMessage) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	ctx := context.Background()
	schema := fmt.Sprintf("l22_stream_%d", time.Now().UnixNano())
	migration, err := os.ReadFile("../../../../spec/production-system/integration/migrations/150002.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, strings.ReplaceAll(string(migration), "api_stream", schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg := store.AccountingReadConfig{ChartsSchema: "candles", StatsSchema: "stats", ChartsDeliverySchema: "charts_delivery", StatsDeliverySchema: "stats_delivery", StreamSchema: schema, ProjectionVersion: "v1", ProjectionGeneration: "g1", ChartsConsumerID: "charts", StatsConsumerID: "stats", SourceStream: "chain125-canonical", SourcePartitionSchema: "indexer"}
	model, err := store.NewAccountingReadModel(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := broker.NewPostgresStream(pool, schema, bytes.Repeat([]byte{9}, 32), sharedauth.HTTPStreamAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples struct {
		Endpoints map[string]struct {
			Successes []struct {
				Body struct {
					Data struct {
						Stats json.RawMessage `json:"stats"`
					} `json:"data"`
				} `json:"body"`
			} `json:"successes"`
		} `json:"endpoints"`
	}
	if err = json.Unmarshal(raw, &examples); err != nil {
		t.Fatal(err)
	}
	successes := examples.Endpoints["IF_STATS"].Successes
	if len(successes) == 0 {
		t.Fatal("frozen stats example required")
	}
	var stats map[string]json.RawMessage
	if err = json.Unmarshal(successes[0].Body.Data.Stats, &stats); err != nil {
		t.Fatal(err)
	}
	stats["volume24hRaw"] = json.RawMessage(`"1000000"`)
	stats["windowStart"] = json.RawMessage(`"2026-10-08T00:00:00.000Z"`)
	stats["windowEnd"] = json.RawMessage(`"2026-10-09T00:00:00.000Z"`)
	stats["realUsdlBalanceRaw"] = json.RawMessage(`"115792089237316195423570985008687907853269984665640564039457584007913129639935"`)
	payload, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Pool string `json:"poolAddress"`
	}
	if err = json.Unmarshal(payload, &identity); err != nil {
		t.Fatal(err)
	}
	frame := broker.Frame{EventID: "l22_stats_1", Topic: "market.stats." + identity.Pool, Sequence: "1", Revision: "1", OccurredAt: "2026-10-12T00:00:00.000Z", Watermark: stats["watermark"], Payload: payload}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = stream.AppendProjectionTx(ctx, tx, frame); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return pool, model, stream, identity.Pool, payload
}
func TestProductionCanonicalStatsAndSnapshotResume(t *testing.T) {
	pool, model, stream, address, payload := productionReadsFixture(t)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission, RegisterSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	response, err := http.Get(srv.URL + "/v2/pools/0x" + strings.ToUpper(address[2:]) + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || interfaces.ValidateJSON("StatsResponse", body) != nil {
		t.Fatal(response.StatusCode, string(body), err)
	}
	var got struct {
		Data struct {
			Stats json.RawMessage `json:"stats"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(body, &got) != nil {
		t.Fatal(string(body))
	}
	var expectedStats, actualStats map[string]json.RawMessage
	json.Unmarshal(payload, &expectedStats)
	json.Unmarshal(got.Data.Stats, &actualStats)
	if string(actualStats["realUsdlBalanceRaw"]) != string(expectedStats["realUsdlBalanceRaw"]) || got.RequestID != response.Header.Get("X-Request-ID") || response.Header.Get("X-Schema-Version") != "2" {
		t.Fatal(string(body), response.Header)
	}
	endpoint := srv.URL + "/v2/snapshots?topics=market.stats." + address
	response, err = http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || interfaces.ValidateJSON("TopicSnapshotReadResponse", body) != nil {
		t.Fatal(response.StatusCode, string(body), err)
	}
	var snap struct {
		Data struct {
			Snapshot struct {
				ID     string `json:"snapshotId"`
				Cursor string `json:"resumeCursor"`
				Topics []struct {
					Sequence string          `json:"sequence"`
					Payload  json.RawMessage `json:"payload"`
				} `json:"topics"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &snap) != nil || len(snap.Data.Snapshot.Topics) != 1 {
		t.Fatal(string(body))
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	for sequence := 2; sequence <= 3; sequence++ {
		var stats map[string]json.RawMessage
		if json.Unmarshal(payload, &stats) != nil {
			t.Fatal("stats payload")
		}
		var watermark map[string]json.RawMessage
		if json.Unmarshal(stats["watermark"], &watermark) != nil {
			t.Fatal("watermark")
		}
		watermark["projectionRevision"] = json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(sequence)))
		watermarkRaw, marshalErr := json.Marshal(watermark)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		stats["watermark"] = watermarkRaw
		stats["realUsdlBalanceRaw"] = json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(sequence)))
		updated, marshalErr := json.Marshal(stats)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		frame := broker.Frame{EventID: fmt.Sprintf("l22_stats_%d", sequence), Topic: "market.stats." + address, Sequence: fmt.Sprint(sequence), Revision: fmt.Sprint(sequence), OccurredAt: "2026-10-12T00:00:00.000Z", Watermark: watermarkRaw, Payload: updated}
		if err = stream.AppendProjectionTx(context.Background(), tx, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(endpoint + "&cursor=" + snap.Data.Snapshot.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 {
		t.Fatal(response.StatusCode, string(body), err)
	}
	var resumed productionSnapshotResponse
	if json.Unmarshal(body, &resumed) != nil || resumed.Data.Snapshot.ID != snap.Data.Snapshot.ID || len(resumed.Data.Snapshot.Topics) != 1 || resumed.Data.Snapshot.Topics[0].Sequence != snap.Data.Snapshot.Topics[0].Sequence || !bytes.Equal(resumed.Data.Snapshot.Topics[0].Payload, snap.Data.Snapshot.Topics[0].Payload) {
		t.Fatal("snapshot continuation changed", string(body))
	}
}

type productionSnapshotResponse struct {
	Data struct {
		Snapshot struct {
			ID     string `json:"snapshotId"`
			Topics []struct {
				Sequence string          `json:"sequence"`
				Payload  json.RawMessage `json:"payload"`
			} `json:"topics"`
		} `json:"snapshot"`
	} `json:"data"`
}

func TestProductionGeneratedSDKUsesActualHTTPAndExactAmounts(t *testing.T) {
	_, model, stream, address, _ := productionReadsFixture(t)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	sdk, err := filepath.Abs("../../../../../sdk/launchpad/packages/sdk/dist/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(sdk); err != nil {
		t.Fatalf("coordinator-built real generated SDK required: %v", err)
	}
	script := `import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {SdkClient,Operations,HttpError}=await import(pathToFileURL(process.argv[1]).href);
const origin=process.argv[2],pool=process.argv[3],socket=origin.replace(/^http/,'ws');
const client=new SdkClient({apiOrigin:origin,identityOrigin:origin,mediaOrigin:origin,streamOrigin:socket,approvedStreamOrigins:[socket],production:false});
const operations=new Operations(client);
const response=await operations.call('IF_STATS',{path:{poolAddress:pool}});
assert.equal(response.data.stats.realUsdlBalanceRaw,'115792089237316195423570985008687907853269984665640564039457584007913129639935');
assert.equal(response.data.stats.volume24hRaw,'1000000');
const batch=await operations.call('IF_STATS_BATCH',{body:{poolAddresses:[pool]}});
assert.equal(batch.data.items[0].realUsdlBalanceRaw,response.data.stats.realUsdlBalanceRaw);
await assert.rejects(client.requestV2('/v2/pools/0x'+ '0'.repeat(40)+'/stats','StatsResponse'),error=>error instanceof HttpError && error.status===422 && error.code==='FIELD_INVALID');
console.log('actual HTTP generated SDK exact uint256 and USDL6; typed FIELD_INVALID');`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script, sdk, srv.URL, address)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual generated SDK HTTP path: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestProductionRealStoreSingleflightCancellationAndFillRecovery(t *testing.T) {
	pool := internaltest.NewPostgres(t)
	insertStats(t, pool, poolAddr, "1000000", 1)
	cache, err := rest.NewPublicCache(1024*1024, 8, 1, 400*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, "LOCK TABLE stats.pool_stats IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	var fetches atomic.Int32
	entered := make(chan struct{})
	fetch := func(ctx context.Context) ([]byte, error) {
		if fetches.Add(1) == 1 {
			close(entered)
		}
		// A bounded read-only transaction against the locked table, the shape
		// every committed-table read model uses.
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		var price string
		err = tx.QueryRow(ctx, "SELECT price FROM stats.pool_stats WHERE pool_address=$1", poolAddr).Scan(&price)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrPoolNotFound
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"price": price})
	}
	firstCtx, cancelFirst := context.WithCancel(ctx)
	first := make(chan error, 1)
	go func() { _, err := cache.GetOrFetch(firstCtx, "real-store-hot-pool", time.Second, fetch); first <- err }()
	<-entered
	cancelFirst()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("cancelled leader did not exit promptly")
	}
	var wg sync.WaitGroup
	var cancelled, successful atomic.Int32
	ready := make(chan struct{}, 1000)
	begin := make(chan struct{})
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			waiter, cancel := context.WithCancel(ctx)
			defer cancel()
			if i%2 == 0 {
				cancel()
			}
			ready <- struct{}{}
			<-begin
			body, err := cache.GetOrFetch(waiter, "real-store-hot-pool", time.Second, fetch)
			if errors.Is(err, context.Canceled) {
				cancelled.Add(1)
				return
			}
			if err == nil && bytes.Contains(body, []byte(`"price":"1000000"`)) {
				successful.Add(1)
				return
			}
			t.Errorf("actual store waiter: %v %s", err, body)
		}(i)
	}
	for i := 0; i < 1000; i++ {
		<-ready
	}
	close(begin)
	if err = lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	metrics := cache.Metrics()
	if fetches.Load() != 1 || cancelled.Load() != 500 || successful.Load() != 500 || metrics.Inflight != 0 || metrics.Cancelled != 501 || metrics.Bytes > 1024*1024 {
		t.Fatal(fetches.Load(), cancelled.Load(), successful.Load(), metrics)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("cancelled fetch retained PostgreSQL connection", pool.Stat())
	}
}

func TestProductionPrivateSnapshotDenialIsUncacheableAndRedactsTokens(t *testing.T) {
	_, model, stream, _, _ := productionReadsFixture(t)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission, RegisterSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	before := rest.MarketCacheMetrics()
	for _, token := range []string{"first-private-session-token", "second-private-session-token"} {
		request := httptest.NewRequest("GET", "/v2/snapshots?topics=private.portfolio.0x1111111111111111111111111111111111111111", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code < 400 || response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), token) || response.Header().Get("ETag") != "" {
			t.Fatal(response.Code, response.Header(), response.Body.String())
		}
	}
	after := rest.MarketCacheMetrics()
	if after.Hits != before.Hits || after.Misses != before.Misses || after.Entries != before.Entries {
		t.Fatal("private snapshots entered public cache", before, after)
	}
}

func TestProductionHTTPAdmissionSurgePreservesReserveAndRejectsExplicitly(t *testing.T) {
	pool, model, stream, address, _ := productionReadsFixture(t)
	admission, err := rest.NewAdmission(10, 10, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Admission: admission}); err != nil {
		t.Fatal(err)
	}
	if err = rest.RegisterAccountingV2WithAdmission(router, model, bytes.Repeat([]byte{1}, 32), nil, admission); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	held, err := admission.Acquire(context.Background(), rest.AdmissionHistory)
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	var rejected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/v2/pools/" + address + "/stats"
			if i%10 == 0 {
				path = "/v2/pools/" + address + "/trades?limit=50"
			}
			request := httptest.NewRequest("GET", path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var body rest.APIError
			if response.Code != 503 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != "DEPENDENCY_UNAVAILABLE" || !body.Retryable || response.Header().Get("Retry-After") != "1" {
				t.Errorf("surge response: %d %s", response.Code, response.Body.String())
				return
			}
			rejected.Add(1)
		}(i)
	}
	wg.Wait()
	stats := admission.Stats()
	if rejected.Load() != 1000 || stats.Rejected != 1000 || stats.Waiting != 0 || stats.PublicUsed != 8 || stats.Used != 8 {
		t.Fatal(rejected.Load(), stats)
	}
	health, err := admission.Acquire(context.Background(), rest.AdmissionHealth)
	if err != nil {
		t.Fatal("health reserve", err)
	}
	recovery, err := admission.Acquire(context.Background(), rest.AdmissionRecovery)
	if err != nil {
		t.Fatal("recovery reserve", err)
	}
	health()
	recovery()
	held()
	code, body, _ := getJSON(t, srv.URL+"/v2/pools/"+address+"/stats")
	if code != 200 || body["data"] == nil {
		t.Fatal("surge recovery", code, body)
	}
	if stats := admission.Stats(); stats.Used != 0 || stats.Waiting != 0 {
		t.Fatal(stats)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("surge leaked a database connection", pool.Stat())
	}
}
