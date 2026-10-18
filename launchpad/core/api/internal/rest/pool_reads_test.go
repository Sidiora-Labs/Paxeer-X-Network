package rest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const (
	poolReadsSecond    = "0x2222222222222222222222222222222222222222"
	poolReadsStateless = "0x5555555555555555555555555555555555555555"
	poolReadsUnknown   = "0x9999999999999999999999999999999999999999"
	poolReadsCreator   = "0xa1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"
	poolReadsWatermark = `{"chainId":"125","height":"1","blockHash":"0x1111111111111111111111111111111111111111111111111111111111111111","projectionRevision":"1","observedAt":"2026-10-09T00:00:00.000Z","integrity":"healthy"}`
)

type poolReadsFixture struct {
	pool  *pgxpool.Pool
	model *store.AccountingReadModel
	rdb   *goredis.Client
	first string
	srv   *httptest.Server
}

func poolReadsPosition(height int64, logIndex int64) accounting.SourcePosition {
	return accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(height, 10), TransactionIndex: "3", LogIndex: strconv.FormatInt(logIndex, 10), TransactionHash: "0x" + strings.Repeat("ab", 32), BlockHash: "0x" + strings.Repeat("cd", 32)}
}

func insertRegistryPool(t *testing.T, pool *pgxpool.Pool, address, token string, height int64, withMetadata bool) {
	t.Helper()
	position := poolReadsPosition(height, height)
	position.TransactionHash = "0x" + strings.Repeat(address[2:4], 32)
	fact := map[string]any{"token": token, "pool": address, "creator": poolReadsCreator, "factory": "0x" + strings.Repeat("f", 40), "optical": "0x" + strings.Repeat("0", 39) + "1", "nftId": "7", "position": position, "blockTimestampSeconds": "1760000000", "provenance": accounting.Provenance{DeploymentID: "fixture125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch1", PoolCodeEpoch: "epoch1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}}
	if withMetadata {
		fact["metadata"] = map[string]any{"emitter": "0x" + strings.Repeat("e", 40), "name": "Sidiora Fixture", "symbol": "SIDF", "decimals": 18, "totalSupply": "1000000000000000000000000000", "position": position, "provenance": fact["provenance"]}
	} else {
		fact["metadata"] = nil
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	provenance, _ := json.Marshal(fact["provenance"])
	_, err = pool.Exec(context.Background(), `INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,7,$6,$7,$8,3,$9,1760000000,$10,$11)`, token, address, poolReadsCreator, fact["factory"], fact["optical"], height, position.BlockHash, position.TransactionHash, height, provenance, payload)
	if err != nil {
		t.Fatal(err)
	}
}

func insertPoolState(t *testing.T, pool *pgxpool.Pool, address, token string) {
	t.Helper()
	ctx := context.Background()
	poolID := "0x" + strings.Repeat(address[2:4], 32)
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$4,$5,1760000000,1,$6)`, "0x"+strings.ToUpper(address[2:6])+address[6:], token, poolReadsCreator, "0x"+strings.Repeat("0", 39)+"1", poolID, "0x"+strings.Repeat(address[2:4], 32)); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []struct {
		block                  int64
		virtual, real, reserve string
	}{{5, "1", "2", "3"}, {10, "1000000000", "250000000", "800000000000000000000000000"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO indexer.pool_state_snapshots(id,pool_id,virtual_reserve,real_reserve,token_reserve,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,$4,$5,'1',$6,1760000000,$7,1)`, poolID+"-"+strconv.FormatInt(snapshot.block, 10), poolID, snapshot.virtual, snapshot.real, snapshot.reserve, snapshot.block, "0x"+strings.Repeat("9", 64)); err != nil {
			t.Fatal(err)
		}
	}
}

func insertQualifiedFill(t *testing.T, pool *pgxpool.Pool, address string, sequence int, feeBps uint16) {
	t.Helper()
	position := poolReadsPosition(int64(sequence)+20, int64(sequence))
	position.TransactionHash = "0x" + strings.Repeat(address[2:4], 32)
	fill := accounting.FillV1{Version: accounting.FillVersion, Position: position, ActualEmitter: address, PoolAddress: address, Side: "buy", TokenAmountRaw: "1000000000000000000", USDLAmountRaw: "1000000", PaidInputRaw: "1000000", FeeRaw: "3000", Execution: accounting.ExecutionPolicy{Quality: "qualified_execution", FeeBPS: &feeBps, Evidence: []accounting.EvidenceRef{}}, Attribution: accounting.Attribution{TransactionSender: poolReadsCreator, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Provenance: accounting.Provenance{DeploymentID: "fixture125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch1", PoolCodeEpoch: "epoch1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: "1760000000"}
	fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, fill.Provenance.DeploymentID)
	raw, err := json.Marshal(fill)
	if err != nil {
		t.Fatal(err)
	}
	partition, _ := json.Marshal(accounting.PartitionPosition{ProjectionVersion: "v1", PartitionKind: "market", PartitionKey: "125:" + address, Sequence: strconv.Itoa(sequence), SourceEventID: fill.SourceFillID, SourceOrder: fill.Position})
	if _, err = pool.Exec(context.Background(), `INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,'v1',$4,1760000000,$5,$6,$7,$7)`, "pub-"+address+"-"+strconv.Itoa(sequence), fill.SourceFillID, strings.Repeat("e", 64), address, partition, raw, sequence); err != nil {
		t.Fatal(err)
	}
}

func appendRankingFrame(t *testing.T, pool *pgxpool.Pool, stream *broker.PostgresStream, category, snapshotID string, pools []string) {
	t.Helper()
	items := []map[string]any{}
	for i, address := range pools {
		items = append(items, map[string]any{"poolAddress": address, "category": category, "rank": i + 1, "score": strconv.Itoa(100 - i), "modelVersion": "fixture", "computedAt": "2026-10-09T00:00:00.000Z"})
	}
	payload, err := json.Marshal(map[string]any{"snapshotId": snapshotID, "category": category, "modelVersion": "fixture", "computedAt": "2026-10-09T00:00:00.000Z", "watermark": json.RawMessage(poolReadsWatermark), "items": items, "totalEligible": strconv.Itoa(len(items)), "truncated": false, "replace": true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = stream.AppendProjectionTx(ctx, tx, broker.Frame{EventID: "ranking_" + category + "_1", Topic: "market.rankings." + category, Sequence: "1", Revision: "1", OccurredAt: "2026-10-12T00:00:00.000Z", Watermark: json.RawMessage(poolReadsWatermark), Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func newPoolReadsFixture(t *testing.T, statsOrigin string) poolReadsFixture {
	t.Helper()
	pool, model, stream, first, _ := productionReadsFixture(t)
	ctx := context.Background()
	for _, name := range []string{"80001", "80002", "100000", "130000"} {
		raw, err := os.ReadFile("../../../../spec/production-system/integration/migrations/" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("registered migration %s: %v", name, err)
		}
	}
	tokenFirst := "0x" + strings.Repeat("3", 40)
	tokenSecond := "0x" + strings.Repeat("4", 40)
	tokenStateless := "0x" + strings.Repeat("6", 40)
	insertRegistryPool(t, pool, first, tokenFirst, 11, true)
	insertRegistryPool(t, pool, poolReadsSecond, tokenSecond, 12, false)
	insertRegistryPool(t, pool, poolReadsStateless, tokenStateless, 13, true)
	insertPoolState(t, pool, first, tokenFirst)
	insertPoolState(t, pool, poolReadsSecond, tokenSecond)
	insertQualifiedFill(t, pool, first, 1, 30)
	insertQualifiedFill(t, pool, first, 2, 100)
	insertQualifiedFill(t, pool, poolReadsSecond, 1, 100)
	appendRankingFrame(t, pool, stream, "new", uuid.NewString(), []string{poolReadsSecond, first})
	appendRankingFrame(t, pool, stream, "trending", uuid.NewString(), []string{first, poolReadsStateless})
	rdb := internaltest.NewRedis(t)
	st := store.New(pool, rdb)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Store: st, StatsOrigin: statsOrigin, Admission: admission, RegisterSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return poolReadsFixture{pool: pool, model: model, rdb: rdb, first: first, srv: srv}
}

type v2Response struct {
	status int
	header http.Header
	body   []byte
}

func getV2(t *testing.T, url string) v2Response {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return v2Response{status: response.StatusCode, header: response.Header, body: body}
}

func assertV2Error(t *testing.T, got v2Response, status int, code string) {
	t.Helper()
	var failure struct {
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if got.status != status || json.Unmarshal(got.body, &failure) != nil || failure.Code != code || interfaces.ValidateJSON("Error", got.body) != nil || got.header.Get("X-Schema-Version") != "2" || failure.RequestID != got.header.Get("X-Request-ID") {
		t.Fatalf("status=%d want %d code=%q want %q body=%s", got.status, status, failure.Code, code, string(got.body))
	}
}

func assertV2Success(t *testing.T, got v2Response, contract string) map[string]any {
	t.Helper()
	if got.status != 200 {
		t.Fatalf("status=%d body=%s", got.status, string(got.body))
	}
	if err := interfaces.ValidateJSON(contract, got.body); err != nil {
		t.Fatalf("%s contract violation: %v body=%s", contract, err, string(got.body))
	}
	var envelope map[string]any
	if json.Unmarshal(got.body, &envelope) != nil {
		t.Fatal(string(got.body))
	}
	if envelope["requestId"] != got.header.Get("X-Request-ID") || got.header.Get("X-Schema-Version") != "2" || got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("envelope metadata mismatch: %v %v", envelope["requestId"], got.header)
	}
	return envelope
}

func TestPoolDetailProductionRead(t *testing.T) {
	f := newPoolReadsFixture(t, "")
	cases := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"unknown pool", "/v2/pools/" + poolReadsUnknown, 404, "NOT_FOUND"},
		{"invalid address", "/v2/pools/0xshort", 422, "FIELD_INVALID"},
		{"zero address", "/v2/pools/0x" + strings.Repeat("0", 40), 422, "FIELD_INVALID"},
		{"query rejected", "/v2/pools/" + f.first + "?limit=1", 400, "QUERY_INVALID"},
		{"registry without reserves or fee", "/v2/pools/" + poolReadsStateless, 503, "DEPENDENCY_UNAVAILABLE"},
		{"registry without stats projection", "/v2/pools/" + poolReadsSecond, 503, "DEPENDENCY_UNAVAILABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertV2Error(t, getV2(t, f.srv.URL+tc.path), tc.status, tc.code)
		})
	}
	for _, path := range []string{"/v2/pools/" + f.first, "/v2/pools/0x" + strings.ToUpper(f.first[2:])} {
		envelope := assertV2Success(t, getV2(t, f.srv.URL+path), "PoolDetailResponse")
		data := envelope["data"].(map[string]any)
		pool := data["pool"].(map[string]any)
		token := data["token"].(map[string]any)
		stats := data["stats"].(map[string]any)
		position := pool["createdPosition"].(map[string]any)
		if pool["poolAddress"] != f.first || pool["tokenAddress"] != "0x"+strings.Repeat("3", 40) || pool["creator"] != poolReadsCreator || pool["chainId"] != "125" {
			t.Fatalf("pool identity: %v", pool)
		}
		if position["height"] != "11" || position["logIndex"] != "11" || position["transactionIndex"] != "3" || position["blockHash"] != "0x"+strings.Repeat("cd", 32) || position["transactionHash"] != "0x"+strings.Repeat(f.first[2:4], 32) {
			t.Fatalf("created position: %v", position)
		}
		if pool["virtualUsdlReserveRaw"] != "1000000000" || pool["realUsdlBalanceRaw"] != "250000000" || pool["effectivePricingUsdlReserveRaw"] != "1250000000" || pool["tokenReserveRaw"] != "800000000000000000000000000" {
			t.Fatalf("latest reserves: %v", pool)
		}
		if pool["quoteAssetAddress"] != strings.ToLower(profile.USDLAddress) || pool["quoteAssetDecimals"].(float64) != 6 || pool["feeBps"].(float64) != 100 {
			t.Fatalf("quote asset and fee: %v", pool)
		}
		if token["address"] != "0x"+strings.Repeat("3", 40) || token["name"] != "Sidiora Fixture" || token["symbol"] != "SIDF" || token["decimals"].(float64) != 18 || token["decimalsQuality"] != "verified" || token["totalSupplyRaw"] != "1000000000000000000000000000" || token["supplyQuality"] != "verified" || token["sourcePosition"] == nil || token["logoAssetId"] != nil || token["metadataRevision"] != "0" {
			t.Fatalf("token: %v", token)
		}
		if stats["poolAddress"] != f.first || envelope["revision"] != "1" || envelope["watermark"] == nil || envelope["nextCursor"] != nil {
			t.Fatalf("stats and envelope: %v", envelope)
		}
	}
}

func TestPoolListProductionRead(t *testing.T) {
	f := newPoolReadsFixture(t, "")
	errorCases := []struct {
		name   string
		query  string
		status int
		code   string
	}{
		{"missing sort", "", 422, "FIELD_INVALID"},
		{"unknown sort", "?sort=graduated", 422, "FIELD_INVALID"},
		{"unknown field", "?sort=new&offset=1", 422, "FIELD_UNKNOWN"},
		{"duplicate field", "?sort=new&sort=new", 422, "FIELD_UNKNOWN"},
		{"limit below bound", "?sort=new&limit=0", 422, "FIELD_INVALID"},
		{"limit above bound", "?sort=new&limit=101", 422, "FIELD_INVALID"},
		{"empty cursor", "?sort=new&cursor=", 400, "CURSOR_INVALID"},
		{"malformed cursor", "?sort=new&cursor=not-a-cursor", 400, "CURSOR_INVALID"},
		{"replaced snapshot", "?sort=new&cursor=" + base64.RawURLEncoding.EncodeToString([]byte(`{"snapshotId":"`+uuid.NewString()+`","offset":1}`)), 409, "RESNAPSHOT_REQUIRED"},
		{"ranking without projection", "?sort=volume", 503, "DEPENDENCY_UNAVAILABLE"},
		{"ranking referencing stateless pool", "?sort=trending", 503, "DEPENDENCY_UNAVAILABLE"},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools"+tc.query), tc.status, tc.code)
		})
	}
	full := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=new"), "PoolListResponse")
	items := full["data"].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["poolAddress"] != poolReadsSecond || items[1].(map[string]any)["poolAddress"] != f.first || full["nextCursor"] != nil {
		t.Fatalf("full page: %s", string(mustMarshal(full)))
	}
	second := items[0].(map[string]any)["createdPosition"].(map[string]any)
	if second["height"] != "12" {
		t.Fatalf("second pool position: %v", second)
	}
	page := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=new&limit=1"), "PoolListResponse")
	pageItems := page["data"].(map[string]any)["items"].([]any)
	cursor, _ := page["nextCursor"].(string)
	if len(pageItems) != 1 || pageItems[0].(map[string]any)["poolAddress"] != poolReadsSecond || cursor == "" {
		t.Fatalf("first page: %s", string(mustMarshal(page)))
	}
	next := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=new&limit=1&cursor="+cursor), "PoolListResponse")
	nextItems := next["data"].(map[string]any)["items"].([]any)
	if len(nextItems) != 1 || nextItems[0].(map[string]any)["poolAddress"] != f.first || next["nextCursor"] != nil || next["revision"] != "1" || next["watermark"] == nil {
		t.Fatalf("second page: %s", string(mustMarshal(next)))
	}
}

func mustMarshal(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func TestHoldersProductionReadValidation(t *testing.T) {
	unreachable := httptest.NewServer(http.NotFoundHandler())
	origin := unreachable.URL
	unreachable.Close()
	for _, variant := range []struct {
		name   string
		origin string
	}{{"origin unconfigured", ""}, {"origin unreachable", origin}} {
		t.Run(variant.name, func(t *testing.T) {
			f := newPoolReadsFixture(t, variant.origin)
			cases := []struct {
				name   string
				path   string
				status int
				code   string
			}{
				{"invalid address", "/v2/pools/0xshort/holders", 422, "FIELD_INVALID"},
				{"unknown field", "/v2/pools/" + f.first + "/holders?offset=1", 422, "FIELD_UNKNOWN"},
				{"limit above bound", "/v2/pools/" + f.first + "/holders?limit=101", 422, "FIELD_INVALID"},
				{"cursor too long", "/v2/pools/" + f.first + "/holders?cursor=" + strings.Repeat("a", 257), 422, "FIELD_INVALID"},
				{"empty cursor", "/v2/pools/" + f.first + "/holders?cursor=", 400, "CURSOR_INVALID"},
				{"dependency unavailable", "/v2/pools/" + f.first + "/holders?limit=10", 503, "DEPENDENCY_UNAVAILABLE"},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					assertV2Error(t, getV2(t, f.srv.URL+tc.path), tc.status, tc.code)
				})
			}
		})
	}
}

func TestHoldersProductionReadLiveOrigin(t *testing.T) {
	origin := os.Getenv("STATS_SERVICE_URL")
	pool := os.Getenv("STATS_SERVICE_POOL")
	if origin == "" || pool == "" {
		t.Skip("requires a live stats-workers origin: set STATS_SERVICE_URL and STATS_SERVICE_POOL")
	}
	f := newPoolReadsFixture(t, origin)
	envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools/"+pool+"/holders?limit=5"), "HoldersResponse")
	data := envelope["data"].(map[string]any)
	if _, ok := data["items"].([]any); !ok || data["coverage"] == nil {
		t.Fatalf("holders data: %v", data)
	}
}

func TestPlatformMetricsProductionRead(t *testing.T) {
	f := newPoolReadsFixture(t, "")
	cases := []struct {
		name   string
		query  string
		status int
		code   string
	}{
		{"missing window", "", 422, "FIELD_INVALID"},
		{"unknown window", "?window=1h", 422, "FIELD_INVALID"},
		{"unknown field", "?window=24h&chainId=125", 422, "FIELD_UNKNOWN"},
		{"window without aggregate", "?window=7d", 501, "FEATURE_UNAVAILABLE"},
		{"aggregate absent", "?window=24h", 503, "DEPENDENCY_UNAVAILABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertV2Error(t, getV2(t, f.srv.URL+"/v2/platform/metrics"+tc.query), tc.status, tc.code)
		})
	}
	ctx := context.Background()
	if err := f.rdb.Set(ctx, "platform:metrics", `{"totalVolume24h":"123456","totalVolume1h":"1000","totalMarketCap":"999","totalFees24h":"12","totalTransactions24h":9,"totalTransactions1h":2,"totalBuys24h":5,"totalSells24h":4,"uniqueTraders24h":3,"totalTokensLaunched":7,"newTokens24h":1,"crossTokenSwaps24h":0,"updatedAt":1760000000}`, 0).Err(); err != nil {
		t.Fatal(err)
	}
	envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/platform/metrics?window=24h"), "PlatformResponse")
	data := envelope["data"].(map[string]any)
	if data["volumeUsdlRaw"] != "123456" || data["pools"] != "7" || data["activeWallets"] != "3" || envelope["generatedAt"] != "2025-10-09T08:53:20.000Z" || envelope["watermark"] != nil || envelope["revision"] != nil || envelope["quality"] != "partial" {
		t.Fatalf("platform envelope: %s", string(mustMarshal(envelope)))
	}
	// Interface 2.2.0 window totals of the same aggregate.
	if data["totalMarketCapUsdlRaw"] != "999" || data["trades"] != "9" || data["newPools"] != "1" || len(data) != 6 {
		t.Fatalf("platform 2.2.0 totals: %s", string(mustMarshal(data)))
	}
	// An aggregate written before the totals existed serves the three
	// original fields only (they are optional on the wire).
	if err := f.rdb.Set(ctx, "platform:metrics", `{"totalVolume24h":"123456","uniqueTraders24h":3,"totalTokensLaunched":7,"updatedAt":1760000000}`, 0).Err(); err != nil {
		t.Fatal(err)
	}
	envelope = assertV2Success(t, getV2(t, f.srv.URL+"/v2/platform/metrics?window=24h"), "PlatformResponse")
	if data = envelope["data"].(map[string]any); len(data) != 3 || data["volumeUsdlRaw"] != "123456" || data["pools"] != "7" || data["activeWallets"] != "3" {
		t.Fatalf("pre-2.2.0 aggregate: %s", string(mustMarshal(data)))
	}
	for _, bad := range []string{`"totalMarketCap":"1.5"`, `"totalMarketCap":"-1"`, `"totalTransactions24h":-1`, `"newTokens24h":-2`} {
		if err := f.rdb.Set(ctx, "platform:metrics", `{"totalVolume24h":"123456","uniqueTraders24h":3,"totalTokensLaunched":7,"updatedAt":1760000000,`+bad+`}`, 0).Err(); err != nil {
			t.Fatal(err)
		}
		assertV2Error(t, getV2(t, f.srv.URL+"/v2/platform/metrics?window=24h"), 503, "INTEGRITY_QUARANTINED")
	}
	if err := f.rdb.Set(ctx, "platform:metrics", `{"totalVolume24h":"12.5","uniqueTraders24h":3,"totalTokensLaunched":7,"updatedAt":1760000000}`, 0).Err(); err != nil {
		t.Fatal(err)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/platform/metrics?window=24h"), 503, "INTEGRITY_QUARANTINED")
}
