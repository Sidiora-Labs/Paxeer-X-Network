package projectionstats

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"math/big"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// isolatedRedis is a throwaway redis-server process (never a container) owned
// by one test, so the test can genuinely restart it.
type isolatedRedis struct {
	binary, port string
	cmd          *exec.Cmd
}

func startIsolatedRedis(t *testing.T, binary string) *isolatedRedis {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	server := &isolatedRedis{binary: binary, port: port}
	if err = server.Start(); err != nil {
		t.Fatal("real isolated Redis required", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Error(err)
		}
	})
	return server
}

func (r *isolatedRedis) URL() string { return "redis://127.0.0.1:" + r.port + "/0" }

func (r *isolatedRedis) Start() error {
	r.cmd = exec.Command(r.binary, "--bind", "127.0.0.1", "--port", r.port, "--save", "", "--appendonly", "no", "--daemonize", "no")
	if err := r.cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+r.port, 200*time.Millisecond); err == nil {
			return conn.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("redis-server on port %s did not accept connections", r.port)
}

func (r *isolatedRedis) Stop() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	cmd := r.cmd
	r.cmd = nil
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

func TestPhase2PublicAbsentServiceIsUnavailable(t *testing.T) {
	router := chi.NewRouter()
	PublicHandlers{}.Register(router)
	for _, path := range []string{"/v2/stats/platform", "/v2/stats/0x1111111111111111111111111111111111111111", "/v2/stats/0x1111111111111111111111111111111111111111/transactions"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 503 {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
}

func TestPhase2IntegratedCanonicalHTTPAgainstSource(t *testing.T) {
	dsn := os.Getenv("L13_PHASE2_POSTGRES_DSN")
	stream := os.Getenv("L13_PHASE2_SOURCE_STREAM")
	version := os.Getenv("L13_PHASE2_PROJECTION_VERSION")
	if dsn == "" || stream == "" || version == "" {
		t.Fatal("real admitted canonical producer/stats fixture required: L13_PHASE2_POSTGRES_DSN, L13_PHASE2_SOURCE_STREAM, L13_PHASE2_PROJECTION_VERSION")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	repo, err := NewRepository(connection, "stats")
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	service, err := NewReadService(repo, ReadConfig{SourceSchema: "indexer", SourceStream: stream, ProjectionVersion: version, CursorKey: key, CursorTTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var pool string
	if err = connection.QueryRow(ctx, "SELECT pool FROM stats.accounting_market_commits WHERE projection_version=$1 GROUP BY pool HAVING count(*)>=2 ORDER BY pool LIMIT 1", version).Scan(&pool); err != nil {
		t.Fatal("real canonical pool with at least two fills required", err)
	}
	router := chi.NewRouter()
	PublicHandlers{Service: service}.Register(router)
	request := func(path string, expected int) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil).WithContext(ctx))
		if response.Code != expected {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	request("/v2/stats/not-an-address/transactions", 400)
	request("/v2/stats/"+pool+"/transactions?limit=101", 400)
	request("/v2/stats/"+pool+"/transactions?direction=asc", 400)
	request("/v2/stats/0x0000000000000000000000000000000000000000/transactions", 404)
	var first, next SnapshotPage
	if err = json.Unmarshal(request("/v2/stats/"+pool+"/transactions?limit=1", 200), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Trades) != 1 || first.NextCursor == nil || len(*first.NextCursor) > 256 {
		t.Fatal(first)
	}
	if err = json.Unmarshal(request("/v2/stats/"+pool+"/transactions?limit=100&cursor="+url.QueryEscape(*first.NextCursor), 200), &next); err != nil {
		t.Fatal(err)
	}
	if next.Watermark != first.Watermark || len(next.Trades) == 0 || first.Trades[0].Position.Compare(next.Trades[0].Position) <= 0 {
		t.Fatal(first, next)
	}
	var actual WindowSnapshot
	if err = json.Unmarshal(request("/v2/stats/"+pool+"?windowSeconds=86400", 200), &actual); err != nil {
		t.Fatal(err)
	}
	rows, err := connection.Query(ctx, "SELECT payload FROM indexer.indexer_accounting_fills_v1 WHERE chain_id=125 AND pool_address=$1 AND (payload->'position'->>'height')::numeric<=$2::numeric", pool, actual.Watermark.Height)
	if err != nil {
		t.Fatal(err)
	}
	volume := new(big.Int)
	fees := map[string]*big.Int{}
	actors := map[string]bool{}
	count := int64(0)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		timestamp, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if timestamp <= actual.WindowStart || timestamp > actual.WindowEnd {
			continue
		}
		value, ok := new(big.Int).SetString(fill.USDLAmountRaw, 10)
		if !ok {
			rows.Close()
			t.Fatal("source USDL amount")
		}
		volume.Add(volume, value)
		count++
		actors[fill.Attribution.TransactionSender] = true
		asset := fill.Token
		if fill.Side == "buy" {
			asset = fill.Settlement
		}
		key := fmt.Sprintf("%s:%d:%s", asset.Address, asset.Decimals, fill.FeeDisposition)
		if fees[key] == nil {
			fees[key] = new(big.Int)
		}
		fee, ok := new(big.Int).SetString(fill.FeeRaw, 10)
		if !ok {
			rows.Close()
			t.Fatal("source fee amount")
		}
		fees[key].Add(fees[key], fee)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 1 || actual.VolumeUSDLRaw != volume.String() || actual.PoolFillCount != strconv.FormatInt(count, 10) || actual.UniqueActors != strconv.Itoa(len(actors)) || len(actual.Fees) != len(fees) || actual.Stale {
		t.Fatal(actual, volume, count, actors, fees)
	}
	for _, fee := range actual.Fees {
		key := fmt.Sprintf("%s:%d:%s", fee.Asset, fee.Decimals, fee.Disposition)
		if fees[key] == nil || fee.Raw != fees[key].String() {
			t.Fatal(fee, fees)
		}
	}
	if actual.TraderPopulation != "sender_actor" || actual.UserActionCount != nil || actual.UserActionUnavailableReason == "" {
		t.Fatal("unsupported routed action was invented", actual)
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual canonical HTTP source reference: %s", encoded)
}

func TestPhase3RedisRestartAndCanonicalDependencyOutage(t *testing.T) {
	dsn, stream, version, redisServer := os.Getenv("L13_PHASE2_POSTGRES_DSN"), os.Getenv("L13_PHASE2_SOURCE_STREAM"), os.Getenv("L13_PHASE2_PROJECTION_VERSION"), os.Getenv("L13_PHASE3_REDIS_SERVER")
	if dsn == "" || stream == "" || version == "" || redisServer == "" {
		t.Fatal("admitted canonical PostgreSQL fixture (L13_PHASE2_POSTGRES_DSN, L13_PHASE2_SOURCE_STREAM, L13_PHASE2_PROJECTION_VERSION) and L13_PHASE3_REDIS_SERVER (path to a redis-server binary, run as a throwaway local process; tests never start containers) required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connection, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	repo, err := NewRepository(connection, "stats")
	if err != nil {
		t.Fatal(err)
	}
	sourceName := fmt.Sprintf("l13_phase3_source_%d", time.Now().UnixNano())
	source := pgx.Identifier{sourceName}.Sanitize()
	if _, err = connection.Exec(ctx, "CREATE SCHEMA "+source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := connection.Exec(cleanup, "DROP SCHEMA "+source+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for _, view := range []string{"indexer_ingest_watermarks_v1", "indexer_ingest_leases_v1", "indexer_canonical_creations_v1", "indexer_block_completeness_v1", "indexer_accounting_fills_v1"} {
		if _, err = connection.Exec(ctx, "CREATE VIEW "+source+"."+pgx.Identifier{view}.Sanitize()+" AS SELECT * FROM indexer."+pgx.Identifier{view}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewReadService(repo, ReadConfig{SourceSchema: sourceName, SourceStream: stream, ProjectionVersion: version, CursorKey: []byte(strings.Repeat("k", 32)), CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().Unix()
	fresh, err := service.Window(ctx, "", 86400, observed)
	if err != nil || fresh.Stale || fresh.PoolFillCount == "0" {
		t.Fatal("actual coherent canonical projection required", fresh, err)
	}
	raw, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	container := startIsolatedRedis(t, redisServer)
	options, err := goredis.ParseURL(container.URL())
	if err != nil {
		t.Fatal(err)
	}
	cache := goredis.NewClient(options)
	defer cache.Close()
	key := "stats:phase3:" + sourceName
	if err = cache.Set(ctx, key, raw, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err = container.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = container.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err = cache.Get(ctx, key).Result(); !errors.Is(err, goredis.Nil) {
		t.Fatal("cache did not lose volatile state on genuine restart", err)
	}
	if _, err = connection.Exec(ctx, "DROP VIEW "+source+".indexer_ingest_watermarks_v1"); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	PublicHandlers{Service: service, Clock: func() time.Time { return time.Unix(observed+60, 0) }}.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/stats/platform?windowSeconds=86400", nil).WithContext(ctx))
	var stale WindowSnapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &stale) != nil || !stale.Stale || stale.ComputedAt != fresh.ComputedAt || stale.Watermark != fresh.Watermark || stale.VolumeUSDLRaw != fresh.VolumeUSDLRaw || stale.PoolFillCount != fresh.PoolFillCount || !reflect.DeepEqual(stale.Fees, fresh.Fees) {
		t.Fatal("dependency outage manufactured fresh or incoherent totals", response.Code, response.Body.String(), fresh)
	}
	if _, err = connection.Exec(ctx, "CREATE VIEW "+source+".indexer_ingest_watermarks_v1 AS SELECT * FROM indexer.indexer_ingest_watermarks_v1"); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := service.Window(ctx, "", 86400, observed)
	if err != nil || rebuilt.Stale || rebuilt.VolumeUSDLRaw != fresh.VolumeUSDLRaw || rebuilt.PoolFillCount != fresh.PoolFillCount || !reflect.DeepEqual(rebuilt.Fees, fresh.Fees) {
		t.Fatal("durable state lost during Redis restart", rebuilt, err)
	}
	if _, err = connection.Exec(ctx, "DELETE FROM stats.phase2_windows WHERE pool='' AND seconds=86400 AND projection_version=$1", version); err != nil {
		t.Fatal(err)
	}
	if _, err = connection.Exec(ctx, "DROP VIEW "+source+".indexer_ingest_watermarks_v1"); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/stats/platform?windowSeconds=86400", nil).WithContext(ctx))
	if response.Code != 503 {
		t.Fatal("missing durable snapshot fabricated success", response.Code, response.Body.String())
	}
	t.Logf("real Redis restart: fresh=%s stale=%s unavailable_status=%d", raw, mustPhase3JSON(t, stale), response.Code)
}

func mustPhase3JSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPhase3CanonicalOpticalFeesAgainstIndependentIntegers(t *testing.T) {
	dsn := os.Getenv("L13_PHASE2_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("actual canonical optical-adjustment source fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	rows, err := connection.Query(ctx, `SELECT payload FROM indexer.indexer_accounting_fills_v1 WHERE payload->>'inputDerivationQuality'='exact' AND (payload->>'pricedInputRaw')::numeric<(payload->>'paidInputRaw')::numeric AND payload->'execution'->>'feeBps' IS NOT NULL ORDER BY (payload->'position'->>'height')::numeric,(payload->'position'->>'transactionIndex')::numeric,(payload->'position'->>'logIndex')::numeric LIMIT 100`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	sides := map[string]bool{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			t.Fatal(err)
		}
		in, err := qualifiedInput(fill, "phase3-optical", "phase3-optical")
		if err != nil {
			t.Fatal(err)
		}
		got, err := AccountQualified(in.Trade, in.Assets, in.Policy)
		if err != nil {
			t.Fatal(err)
		}
		if fill.PricedInputRaw == nil || fill.Execution.FeeBPS == nil || fill.Execution.ReplayEvidenceDigest == nil {
			t.Fatal("missing optical/fee provenance")
		}
		priced, ok := new(big.Int).SetString(*fill.PricedInputRaw, 10)
		if !ok {
			t.Fatal("priced input")
		}
		paid, ok := new(big.Int).SetString(fill.PaidInputRaw, 10)
		if !ok {
			t.Fatal("paid input")
		}
		fee := new(big.Int).Quo(new(big.Int).Mul(priced, big.NewInt(int64(*fill.Execution.FeeBPS))), big.NewInt(10000))
		reserve := new(big.Int).Set(paid)
		asset, decimals := fill.Token.Address, int(fill.Token.Decimals)
		volume := fill.USDLAmountRaw
		if fill.Side == "buy" {
			reserve.Sub(reserve, fee)
			asset, decimals = fill.Settlement.Address, int(fill.Settlement.Decimals)
			volume = fill.PaidInputRaw
		}
		if fill.FeeRaw != fee.String() || got.TradedUSDLRaw != volume || got.InputReserveIncrementRaw != reserve.String() || got.FeeGroup.AssetAddress != asset || got.FeeGroup.AssetDecimals != decimals {
			t.Fatal(fill.SourceFillID, got, fee, reserve, volume, asset)
		}
		sides[fill.Side] = true
		t.Logf("canonical optical source=%s paid=%s priced=%s fee=%s reserve=%s asset=%s volume=%s", fill.SourceFillID, paid, priced, fee, reserve, asset, volume)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !sides["buy"] || !sides["sell"] {
		t.Fatal("actual negative optical buy and sell receipts with proven fee rates are required", sides)
	}
}
