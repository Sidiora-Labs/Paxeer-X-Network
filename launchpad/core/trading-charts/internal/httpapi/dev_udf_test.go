package httpapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

// The development UDF fixture uses the fixture PostgreSQL (historyPostgres:
// an owned database migrated with the real candles schema plus core/indexer's
// own indexer.pools DDL), the fixture Redis for the real candle builder, and
// an in-process geth node (go-ethereum simulated backend) served over HTTP
// JSON-RPC, on which the real SidioraERC20 artifact is deployed and a real
// transaction emits the EventEmitter MarketCreated log.

const devFixtureRedis = "redis://:kindle_fixture_password@127.0.0.1:26379/0"

const devPool = "0x00000000000000000000000000000000000d0d01"

type devChain struct {
	backend *simulated.Backend
	url     string
	key     *ecdsa.PrivateKey
	nonce   uint64
}

func newDevChain(t *testing.T) *devChain {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	funds := new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)
	backend := simulated.NewBackend(types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: funds}}, func(n *node.Config, _ *ethconfig.Config) {
		n.HTTPHost = "127.0.0.1"
		n.HTTPPort = port
		n.HTTPModules = []string{"eth"}
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	return &devChain{backend: backend, url: "http://127.0.0.1:" + strconv.Itoa(port), key: key}
}

func (c *devChain) send(t *testing.T, to *common.Address, data []byte, gas uint64) *types.Receipt {
	t.Helper()
	ctx := context.Background()
	client := c.backend.Client()
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	feeCap := new(big.Int).Add(tip, new(big.Int).Mul(head.BaseFee, big.NewInt(2)))
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1337), Nonce: c.nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: gas, To: to, Data: data})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(1337)), c.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	c.nonce++
	c.backend.Commit()
	receipt, err := client.TransactionReceipt(ctx, signed.Hash())
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt: %v %+v", err, receipt)
	}
	return receipt
}

// deployToken deploys the committed SidioraERC20 artifact (the launchpad's
// fixed-supply token) with the given constructor arguments.
func (c *devChain) deployToken(t *testing.T, name, symbol string, supply *big.Int) common.Address {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/artifacts/contracts/core/SidioraERC20.sol/SidioraERC20.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		ABI      json.RawMessage `json:"abi"`
		Bytecode string          `json:"bytecode"`
	}
	if err = json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		t.Fatal(err)
	}
	args, err := parsed.Pack("", name, symbol, supply, crypto.PubkeyToAddress(c.key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	code := append(common.FromHex(artifact.Bytecode), args...)
	receipt := c.send(t, nil, code, 3_000_000)
	if receipt.ContractAddress == (common.Address{}) {
		t.Fatal("token not deployed")
	}
	return receipt.ContractAddress
}

// emitMarketCreated sends a contract creation whose init code emits an
// unrelated LOG1 and then LOG2(MarketCreated, poolId), mined in its own block.
func (c *devChain) emitMarketCreated(t *testing.T, poolID common.Hash) *types.Receipt {
	t.Helper()
	unrelated := crypto.Keccak256Hash([]byte("Unrelated()"))
	code := []byte{0x7f}
	code = append(code, unrelated.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x7f)
	code = append(code, poolID.Bytes()...)
	code = append(code, 0x7f)
	code = append(code, developmentMarketCreatedTopic.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa2, 0x00)
	receipt := c.send(t, nil, code, 200_000)
	if len(receipt.Logs) != 2 {
		t.Fatalf("market created logs = %d", len(receipt.Logs))
	}
	return receipt
}

// applyIndexerPools runs core/indexer's own goose statement for indexer.pools.
func applyIndexerPools(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	raw, err := os.ReadFile("../../../indexer/migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `CREATE SCHEMA IF NOT EXISTS "indexer"`); err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		statement := strings.SplitN(block, "-- +goose StatementEnd", 2)[0]
		if strings.Contains(statement, `CREATE TABLE "indexer"."pools"`) {
			if _, err = pool.Exec(context.Background(), statement); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("indexer.pools DDL not found")
}

type devFixture struct {
	pool   *pgxpool.Pool
	st     *store.Store
	router *chi.Mux
	chain  *devChain
	rpc    *rpc.Client
}

func newDevFixture(t *testing.T, withChain bool) devFixture {
	t.Helper()
	pool := historyPostgres(t)
	applyIndexerPools(t, pool)
	f := devFixture{pool: pool, st: store.New(pool), router: chi.NewRouter()}
	if withChain {
		f.chain = newDevChain(t)
		client, err := rpc.Dial(f.chain.url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		f.rpc = client
	}
	rdb := goredis.NewClient(parseRedisOpts(devFixtureRedis))
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	RegisterDevelopmentUDF(f.router, SymbolsHandler{Development: &DevelopmentPoolMetadataSource{Pool: pool, Chain: f.rpc}}, DevelopmentHistoryHandler{Source: PostgresHistorySource{Pool: pool}})
	RegisterWebhook(f.router, WebhookDeps{Builder: engine.New(pool, rdb, f.st, logger), Logger: logger, Secret: whSecret})
	return f
}

func (f devFixture) get(t *testing.T, path string, header ...string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Code != http.StatusNotModified {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
		}
	}
	return rec, body
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDevelopmentUDFZeroPoolsGivesNoData(t *testing.T) {
	f := newDevFixture(t, false)
	for _, mode := range []string{"", "price", "total_supply_valuation"} {
		path := "/v2/udf/history?symbol=" + devPool + "&resolution=1&from=0&to=6000"
		if mode != "" {
			path += "&mode=" + mode
		}
		rec, body := f.get(t, path)
		if rec.Code != 200 || body["s"] != "no_data" || body["valuationAvailable"] != true || body["algorithmVersion"] != DevelopmentSeriesModelVersion || body["watermark"] != nil {
			t.Fatalf("%s: %d %s", mode, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Schema-Version") != "2" || rec.Header().Get("Cache-Control") != "private, no-cache" || rec.Header().Get("ETag") == "" {
			t.Fatalf("%s headers %v", mode, rec.Header())
		}
		want := mode
		if want == "" {
			want = "price"
		}
		// The production document generator, run over an empty canonical page
		// with the same identity, must give byte-identical output.
		production, err := valuationHistory(context.Background(), CanonicalCandlePage{Items: []interfaces.Candle{}, SnapshotID: body["snapshotId"].(string), Algorithm: DevelopmentSeriesModelVersion, Complete: true}, nil, want)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(production, rec.Body.Bytes()) {
			t.Fatalf("%s: development %s != production %s", mode, rec.Body.String(), production)
		}
		etag := rec.Header().Get("ETag")
		again, _ := f.get(t, path, "If-None-Match", etag)
		if again.Code != http.StatusNotModified {
			t.Fatalf("conditional = %d", again.Code)
		}
	}
	// Countback-only requests (TradingView's first page) are also no_data.
	rec, body := f.get(t, "/v2/udf/history?symbol="+devPool+"&resolution=1D&to=1800000000&countback=300&mode=total_supply_valuation")
	if rec.Code != 200 || body["s"] != "no_data" {
		t.Fatalf("countback: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDevelopmentUDFRejectsInvalidRequests(t *testing.T) {
	f := newDevFixture(t, false)
	base := "/v2/udf/history?symbol=" + devPool + "&resolution=1&from=0&to=600"
	for _, q := range []string{base + "&mode=mcap", base + "&mode=price&mode=price", base + "&cursor=abc", "/v2/udf/history?symbol=nope&resolution=1&from=0&to=600", base + "&chainId=1", base + "&extra=1", "/v2/udf/history?symbol=" + devPool + "&resolution=7&from=0&to=600"} {
		rec, body := f.get(t, q)
		if rec.Code != 422 || body["code"] != "FIELD_INVALID" {
			t.Fatalf("%s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
}

func TestDevelopmentUDFHistoryFromWebhookCandles(t *testing.T) {
	f := newDevFixture(t, false)
	// A new pool's first swaps arrive through the signed indexer webhook.
	events := []map[string]any{}
	for i, price := range []string{"1000000000000000000", "3000000000000000000", "2000000000000000000"} {
		events = append(events, map[string]any{"eventName": "Swap", "blockNumber": 100 + i, "blockTimestamp": 1760000000 + int64(i)*60, "txHash": "0x" + strings.Repeat(strconv.Itoa(i+1), 64), "logIndex": 0, "args": map[string]any{"poolAddress": devPool, "sender": "0x00000000000000000000000000000000000000aa", "isBuy": true, "amountIn": "2500000", "amountOut": "1000", "fee": "1", "price": price}})
	}
	raw, _ := json.Marshal(map[string]any{"events": events})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, signedRequest(raw))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"processed":3`) {
		t.Fatalf("webhook: %d %s", rec.Code, rec.Body.String())
	}
	path := "/v2/udf/history?symbol=" + devPool + "&resolution=1&from=1759999980&to=1760000300"
	rec, body := f.get(t, path+"&mode=price")
	if rec.Code != 200 || body["s"] != "ok" {
		t.Fatalf("price: %d %s", rec.Code, rec.Body.String())
	}
	want := []string{"algorithmVersion", "c", "h", "l", "mode", "o", "s", "snapshotId", "t", "v", "valuationAvailable", "valuationBasis", "valuations", "watermark"}
	if got := sortedKeys(body); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("price keys %v", got)
	}
	times := body["t"].([]any)
	closes := body["c"].([]any)
	volumes := body["v"].([]any)
	if len(times) != 3 || times[0].(float64) != 1760000000-1760000000%60 || closes[1].(float64) != 3 || closes[2].(float64) != 2 || volumes[0].(float64) != 2.5 {
		t.Fatalf("price bars %s", rec.Body.String())
	}
	valuations := body["valuations"].([]any)
	if len(valuations) != 3 || body["valuationAvailable"] != false {
		t.Fatalf("valuations %s", rec.Body.String())
	}
	for _, v := range valuations {
		m := v.(map[string]any)
		if m["available"] != false || m["valuationBasis"] != "total_supply_valuation" || m["reason"] != developmentSupplyUnavailable {
			t.Fatalf("valuation %v", m)
		}
	}
	snapshot := body["snapshotId"].(string)
	_, repeat := f.get(t, path+"&mode=price")
	if repeat["snapshotId"] != snapshot {
		t.Fatalf("snapshot not stable: %v %v", repeat["snapshotId"], snapshot)
	}

	// total_supply_valuation: the development candles carry no price source
	// position, so supply cannot be bound and the production "unavailable"
	// document (with price history) is returned.
	rec, body = f.get(t, path+"&mode=total_supply_valuation")
	if rec.Code != 200 || body["s"] != "unavailable" || body["valuationAvailable"] != false || body["mode"] != "total_supply_valuation" {
		t.Fatalf("valuation: %d %s", rec.Code, rec.Body.String())
	}
	want = []string{"algorithmVersion", "mode", "priceHistory", "s", "snapshotId", "t", "v", "valuationAvailable", "valuationBasis", "valuations", "watermark"}
	if got := sortedKeys(body); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("valuation keys %v", got)
	}
	history := body["priceHistory"].(map[string]any)
	if len(history["c"].([]any)) != 3 || history["c"].([]any)[1].(float64) != 3 {
		t.Fatalf("price history %v", history)
	}

	// Countback returns the latest bars in ascending order.
	rec, body = f.get(t, "/v2/udf/history?symbol="+devPool+"&resolution=1&to=1760000300&countback=2")
	if rec.Code != 200 || len(body["t"].([]any)) != 2 || body["t"].([]any)[0].(float64) >= body["t"].([]any)[1].(float64) || body["c"].([]any)[1].(float64) != 2 {
		t.Fatalf("countback: %s", rec.Body.String())
	}

	// A further swap changes the stored rows and therefore the snapshot.
	more, _ := json.Marshal(map[string]any{"events": []map[string]any{{"eventName": "Swap", "blockNumber": 200, "blockTimestamp": 1760000200, "txHash": "0x" + strings.Repeat("9", 64), "logIndex": 0, "args": map[string]any{"poolAddress": devPool, "sender": "0x00000000000000000000000000000000000000bb", "isBuy": false, "amountIn": "10", "amountOut": "1000000", "fee": "1", "price": "500000000000000000"}}}})
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, signedRequest(more))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	_, body = f.get(t, path+"&mode=price")
	if body["snapshotId"] == snapshot || len(body["t"].([]any)) != 4 {
		t.Fatalf("after new swap: %v", body)
	}
}

func TestDevelopmentUDFIntegrityFailure(t *testing.T) {
	f := newDevFixture(t, false)
	if err := f.st.InsertCandle(context.Background(), store.CandleRow{PoolAddress: devPool, Timeframe: "1m", CandleStart: 60, Open: "xx", High: "xx", Low: "xx", Close: "xx", VolumeUsdl: "0", VolumeToken: "0", BuyVolumeUsdl: "0", SellVolumeUsdl: "0", TradeCount: 1, UniqueTraders: 1, LastTradeTs: 60, McapOpen: "0", McapHigh: "0", McapLow: "0", McapClose: "0"}); err != nil {
		t.Fatal(err)
	}
	rec, body := f.get(t, "/v2/udf/history?symbol="+devPool+"&resolution=1&from=0&to=120")
	if rec.Code != 503 || body["code"] != "INTEGRITY_QUARANTINED" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestDevelopmentUDFConfigAndTime(t *testing.T) {
	f := newDevFixture(t, false)
	rec, body := f.get(t, "/v2/udf/config")
	if rec.Code != 200 || body["supports_time"] != true || len(body["supported_resolutions"].([]any)) != len(supportedResolutions()) {
		t.Fatalf("config %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/v2/udf/time", nil)
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var now int64
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &now) != nil || now < time.Now().Unix()-5 {
		t.Fatalf("time %d %s", rec.Code, rec.Body.String())
	}
}

func TestDevelopmentUDFSymbolsFromIndexerAndChain(t *testing.T) {
	f := newDevFixture(t, true)
	supply, _ := new(big.Int).SetString("1000000000000000000000000000", 10)
	token := f.chain.deployToken(t, "Dev Chart Token", "DCT", supply)
	poolID := common.HexToHash("0x" + strings.Repeat("d1", 32))
	receipt := f.chain.emitMarketCreated(t, poolID)
	mixedPool := "0x" + strings.ToUpper(devPool[2:8]) + devPool[8:]
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$3,$4,1,$5,$6,$7)`, mixedPool, strings.ToLower(token.Hex()), "0x00000000000000000000000000000000000000cc", poolID.Hex(), int64(1760000000), receipt.BlockNumber.Int64(), receipt.TxHash.Hex()); err != nil {
		t.Fatal(err)
	}

	resolver := &DevelopmentPoolMetadataSource{Pool: f.pool, Chain: f.rpc}
	resolved, err := resolver.Resolve(context.Background(), devPool)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resolved)
	if err = interfaces.ValidateJSON("Token", raw); err != nil {
		t.Fatalf("Token contract: %v %s", err, raw)
	}
	pos := resolved.SourcePosition
	if resolved.Name != "Dev Chart Token" || resolved.Symbol != "DCT" || *resolved.Decimals != 6 || string(*resolved.TotalSupplyRaw) != supply.String() || pos == nil || string(pos.Height) != receipt.BlockNumber.String() || pos.BlockHash != strings.ToLower(receipt.BlockHash.Hex()) || string(pos.LogIndex) != strconv.FormatUint(uint64(receipt.Logs[1].Index), 10) || pos.TransactionHash != strings.ToLower(receipt.TxHash.Hex()) {
		t.Fatalf("resolved %s", raw)
	}

	rec, body := f.get(t, "/v2/udf/symbols?symbol="+devPool)
	if rec.Code != 200 || body["full_name"] != "DCT/USDL" || body["description"] != "Dev Chart Token" || body["tokenAddress"] != strings.ToLower(token.Hex()) || body["baseDecimals"].(float64) != 6 || body["quoteDecimals"].(float64) != 6 || body["metadataRevision"] != string(resolved.MetadataRevision) || body["chainId"] != "125" || body["poolAddress"] != devPool {
		t.Fatalf("symbols %d %s", rec.Code, rec.Body.String())
	}
	// The revision is stable across head movement (a later block).
	f.chain.backend.Commit()
	_, again := f.get(t, "/v2/udf/symbols?symbol="+devPool)
	if again["metadataRevision"] != body["metadataRevision"] {
		t.Fatalf("revision moved with head: %v", again)
	}

	rec, body = f.get(t, "/v2/udf/symbols?symbol=0x00000000000000000000000000000000000d0d02")
	if rec.Code != 404 || body["code"] != "NOT_FOUND" {
		t.Fatalf("unknown pool %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = f.get(t, "/v2/udf/symbols?symbol=nope")
	if rec.Code != 422 {
		t.Fatalf("invalid symbol %d", rec.Code)
	}
}

func TestDevelopmentUDFSymbolsRejectsConflictingCreationRecord(t *testing.T) {
	f := newDevFixture(t, true)
	token := f.chain.deployToken(t, "Conflict", "CFL", big.NewInt(1000))
	receipt := f.chain.emitMarketCreated(t, common.HexToHash("0x"+strings.Repeat("e1", 32)))
	// The indexer row names a different poolId than the log in its creation
	// transaction: the position cannot be resolved and nothing is invented.
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$3,$4,1,1,$5,$6)`, devPool, strings.ToLower(token.Hex()), "0x00000000000000000000000000000000000000cc", "0x"+strings.Repeat("e2", 32), receipt.BlockNumber.Int64(), receipt.TxHash.Hex()); err != nil {
		t.Fatal(err)
	}
	rec, body := f.get(t, "/v2/udf/symbols?symbol="+devPool)
	if rec.Code != 503 || body["code"] != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestDevelopmentUDFSymbolsWithoutChain(t *testing.T) {
	f := newDevFixture(t, false)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$2,$2,$3,1,1,1,$3)`, devPool, "0x00000000000000000000000000000000000000cc", "0x"+strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	rec, body := f.get(t, "/v2/udf/symbols?symbol="+devPool)
	if rec.Code != 503 || body["code"] != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
