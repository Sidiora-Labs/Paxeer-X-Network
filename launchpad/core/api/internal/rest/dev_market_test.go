package rest_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
)

// The development market fixture runs the real development schema (the
// internaltest read subset plus indexer.cursors and indexer.swaps applied from
// core/indexer's own goose migration), a real Redis, and an in-process geth
// node (go-ethereum simulated backend) served over HTTP JSON-RPC. Pools are
// created by real transactions whose logs carry the EventEmitter
// MarketCreated topic, so every block hash and position the handlers resolve
// comes from an actual chain.

const (
	mktPoolA       = "0xa1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	mktPoolB       = "0xb2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	mktPoolOptical = "0xc3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
	mktPoolUnknown = "0xd4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
	mktTokenA      = "0x0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"
	mktTokenB      = "0x0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b"
	mktTokenC      = "0x0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c"
	mktCreator     = "0xcececececececececececececececececececece"
	mktTraderOne   = "0x1111111111111111111111111111111111111111"
	mktTraderTwo   = "0x2222222222222222222222222222222222222222"
	mktHolderOne   = "0x3333333333333333333333333333333333333333"
	mktHolderTwo   = "0x4444444444444444444444444444444444444444"
	mktHolderThree = "0x5555555555555555555555555555555555555555"
)

var mktMarketCreatedTopic = crypto.Keccak256Hash([]byte("MarketCreated(bytes32,address,address,address,address,uint256,uint256)"))

type mktChain struct {
	backend *simulated.Backend
	url     string
	key     *ecdsa.PrivateKey
	nonce   uint64
}

func newMktChain(t *testing.T) *mktChain {
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
	return &mktChain{backend: backend, url: "http://127.0.0.1:" + strconv.Itoa(port), key: key}
}

// emitMarketCreated sends a contract creation whose init code emits an
// unrelated LOG1 and then LOG2(MarketCreated, poolId), mined in its own block.
func (c *mktChain) emitMarketCreated(t *testing.T, poolID common.Hash) *types.Receipt {
	t.Helper()
	unrelated := crypto.Keccak256Hash([]byte("Unrelated()"))
	code := []byte{0x7f}
	code = append(code, unrelated.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x7f)
	code = append(code, poolID.Bytes()...)
	code = append(code, 0x7f)
	code = append(code, mktMarketCreatedTopic.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa2, 0x00)
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
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1337), Nonce: c.nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: 200000, Data: code})
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
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful || len(receipt.Logs) != 2 {
		t.Fatalf("market created receipt: %v %+v", err, receipt)
	}
	return receipt
}

func (c *mktChain) head(t *testing.T) *types.Header {
	t.Helper()
	header, err := c.backend.Client().HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return header
}

// mktApplyIndexerMigration runs core/indexer's goose statements for the tables
// the internaltest read subset does not create.
func mktApplyIndexerMigration(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	raw, err := os.ReadFile("../../../indexer/migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	applied := 0
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		statement := strings.SplitN(block, "-- +goose StatementEnd", 2)[0]
		for _, table := range tables {
			if strings.Contains(statement, `CREATE TABLE "indexer"."`+table+`"`) {
				if _, err = pool.Exec(context.Background(), statement); err != nil {
					t.Fatalf("indexer migration %s: %v", table, err)
				}
				applied++
			}
		}
	}
	if applied != len(tables) {
		t.Fatalf("indexer migration applied %d of %v", applied, tables)
	}
}

type mktMarketFixture struct {
	pool  *pgxpool.Pool
	rdb   *goredis.Client
	chain *mktChain
	srv   *httptest.Server
}

func newMktMarketFixture(t *testing.T, withRPC bool) mktMarketFixture {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	rdb := internaltest.NewRedis(t)
	mktApplyIndexerMigration(t, pool, "cursors", "swaps")
	chain := newMktChain(t)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	rpcURL := ""
	if withRPC {
		rpcURL = chain.url
	}
	router := chi.NewRouter()
	if err = rest.RegisterDevelopmentMarketReads(context.Background(), router, rest.DevelopmentMarketOptions{Store: store.New(pool, rdb), Admission: admission, ChainRPCURL: rpcURL}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return mktMarketFixture{pool: pool, rdb: rdb, chain: chain, srv: srv}
}

func (f mktMarketFixture) setCursor(t *testing.T, height uint64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.cursors(chain_id,last_processed_block,updated_at) VALUES(125,$1,'2026-10-13 10:00:00') ON CONFLICT(chain_id) DO UPDATE SET last_processed_block=EXCLUDED.last_processed_block`, int64(height)); err != nil {
		t.Fatal(err)
	}
}

func mktPoolID(address string) common.Hash {
	return common.HexToHash("0x" + strings.Repeat(address[2:4], 32))
}

func (f mktMarketFixture) createPool(t *testing.T, address, token, optical string, createdAt int64) *types.Receipt {
	t.Helper()
	receipt := f.chain.emitMarketCreated(t, mktPoolID(address))
	mixed := "0x" + strings.ToUpper(address[2:6]) + address[6:]
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$4,$5,7,$6,$7,$8)`, mixed, token, mktCreator, optical, mktPoolID(address).Hex(), createdAt, receipt.BlockNumber.Int64(), receipt.TxHash.Hex()); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func (f mktMarketFixture) insertState(t *testing.T, address string, receipt *types.Receipt, virtual, real, token string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.pool_state_snapshots(id,pool_id,virtual_reserve,real_reserve,token_reserve,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,$4,$5,'1',$6,1760000000,$7,1)`, receipt.TxHash.Hex()+"-1", mktPoolID(address).Hex(), virtual, real, token, receipt.BlockNumber.Int64(), receipt.TxHash.Hex()); err != nil {
		t.Fatal(err)
	}
}

func (f mktMarketFixture) insertSwap(t *testing.T, address, sender string, isBuy bool, amountIn, amountOut, fee string, block, timestamp int64, logIndex int) {
	t.Helper()
	txHash := "0x" + strings.Repeat("e", 62) + strconv.FormatInt(block%100+10, 10)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO indexer.swaps(id,pool_id,pool_address,sender,is_buy,amount_in,amount_out,fee,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'1',$9,$10,$11,$12)`, txHash+"-"+strconv.Itoa(logIndex), mktPoolID(address).Hex(), address, sender, isBuy, amountIn, amountOut, fee, block, timestamp, txHash, logIndex); err != nil {
		t.Fatal(err)
	}
}

func (f mktMarketFixture) insertPoolStats(t *testing.T, address, token, volume, change string, buys, sells, holders int, createdAt, updatedAt int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO stats.pool_stats(pool_address,token_address,price,price_change_24h,volume_24h,market_cap,buy_count_24h,sell_count_24h,holder_count,created_at,updated_at) VALUES($1,$2,'1000',$3,$4,'5000',$5,$6,$7,$8,$9)`, address, token, change, volume, buys, sells, holders, createdAt, updatedAt); err != nil {
		t.Fatal(err)
	}
}

func (f mktMarketFixture) insertHolder(t *testing.T, address, holder, balance string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO stats.pool_holders(pool_address,holder_address,balance,pct_of_supply,last_updated) VALUES($1,$2,$3,'0',1760000000)`, address, holder, balance); err != nil {
		t.Fatal(err)
	}
}

func (f mktMarketFixture) rank(t *testing.T, key string, members ...goredis.Z) {
	t.Helper()
	if err := f.rdb.ZAdd(context.Background(), "ranking:"+key, members...).Err(); err != nil {
		t.Fatal(err)
	}
}

func mktPost(t *testing.T, url, body string) v2Response {
	t.Helper()
	response, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return v2Response{status: response.StatusCode, header: response.Header, body: buf.Bytes()}
}

func mktData(envelope map[string]any) map[string]any { return envelope["data"].(map[string]any) }

func mktAssertWatermark(t *testing.T, raw any, header *types.Header) {
	t.Helper()
	wm, ok := raw.(map[string]any)
	if !ok || wm["height"] != header.Number.String() || wm["blockHash"] != strings.ToLower(header.Hash().Hex()) || wm["projectionRevision"] != header.Number.String() || wm["observedAt"] != "2026-10-13T10:00:00.000Z" || wm["integrity"] != "healthy" || wm["chainId"] != "125" {
		t.Fatalf("watermark %v, want height %s hash %s", raw, header.Number, header.Hash().Hex())
	}
}

func TestDevelopmentMarketReadsZeroPools(t *testing.T) {
	f := newMktMarketFixture(t, true)
	ctx := context.Background()
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/trending"), 503, "DEPENDENCY_UNAVAILABLE")
	head := f.chain.head(t)
	f.setCursor(t, head.Number.Uint64())
	if err := f.rdb.Set(ctx, "platform:metrics", `{"totalVolume24h":"0","totalVolume1h":"0","totalMarketCap":"0","totalFees24h":"0","totalTransactions24h":0,"totalTransactions1h":0,"totalBuys24h":0,"totalSells24h":0,"uniqueTraders24h":0,"totalTokensLaunched":0,"newTokens24h":0,"crossTokenSwaps24h":0,"updatedAt":1760000000}`, 0).Err(); err != nil {
		t.Fatal(err)
	}
	for _, sort := range []string{"new", "volume", "trending"} {
		envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort="+sort), "PoolListResponse")
		if items := mktData(envelope)["items"].([]any); len(items) != 0 || envelope["nextCursor"] != nil || envelope["revision"] != head.Number.String() {
			t.Fatalf("empty pool list %s: %v", sort, envelope)
		}
		mktAssertWatermark(t, envelope["watermark"], head)
	}
	for _, category := range []string{"trending", "breakout", "volume", "gainers", "losers", "movers", "unusual", "new"} {
		envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/"+category), "RankingsResponse")
		snapshot := mktData(envelope)["snapshot"].(map[string]any)
		if len(snapshot["items"].([]any)) != 0 || snapshot["totalEligible"] != "0" || snapshot["truncated"] != false || snapshot["category"] != category || envelope["nextCursor"] != nil {
			t.Fatalf("empty rankings %s: %v", category, snapshot)
		}
		mktAssertWatermark(t, snapshot["watermark"], head)
	}
	for _, category := range []string{"trader", "creator", "volume"} {
		for _, window := range []string{"24h", "7d", "30d"} {
			envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/"+category+"?window="+window), "LeaderboardsResponse")
			snapshot := mktData(envelope)["snapshot"].(map[string]any)
			if len(snapshot["entries"].([]any)) != 0 || envelope["nextCursor"] != nil || envelope["quality"] != "complete" {
				t.Fatalf("empty leaderboard %s/%s: %v", category, window, envelope)
			}
			mktAssertWatermark(t, snapshot["watermark"], head)
		}
	}
	stats := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats"), "HolderRewardsStatsResponse")
	if d := mktData(stats); d["cumulativeDistributedRaw"] != "0" || d["walletsRewarded"] != float64(0) {
		t.Fatalf("empty holder rewards stats: %v", d)
	}
	mktAssertWatermark(t, stats["watermark"], head)
	rewardPools := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools"), "HolderRewardsPoolsResponse")
	if len(mktData(rewardPools)["items"].([]any)) != 0 || rewardPools["nextCursor"] != nil {
		t.Fatalf("empty holder reward pools: %v", rewardPools)
	}
	platform := assertV2Success(t, getV2(t, f.srv.URL+"/v2/platform/metrics?window=24h"), "PlatformResponse")
	if d := mktData(platform); d["pools"] != "0" || d["volumeUsdlRaw"] != "0" {
		t.Fatalf("platform: %v", d)
	}
	batch := assertV2Success(t, mktPost(t, f.srv.URL+"/v2/pools/stats:batch", `{"poolAddresses":["`+mktPoolUnknown+`"]}`), "StatsBatchResponse")
	if d := mktData(batch); len(d["items"].([]any)) != 0 || len(d["missing"].([]any)) != 1 || d["missing"].([]any)[0] != mktPoolUnknown {
		t.Fatalf("batch of unknown pools: %v", d)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolUnknown), 404, "NOT_FOUND")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolUnknown+"/stats"), 404, "NOT_FOUND")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolUnknown+"/holders"), 404, "NOT_FOUND")

	// Request validation matches the production handlers.
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools?sort=new&chainId=125"), 422, "FIELD_UNKNOWN")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools?sort=new&cursor=bad"), 400, "CURSOR_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/top_volume"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/trending?limit=0"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/trending?limit=101"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/trending?window=24h"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/rankings/trending?cursor=opaque"), 400, "CURSOR_INVALID")
	assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/trending?limit=5"), "RankingsResponse")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/whales?window=24h"), 404, "NOT_FOUND")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=1y"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=size"), 422, "FIELD_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/0x12/stats"), 422, "FIELD_INVALID")
	assertV2Error(t, mktPost(t, f.srv.URL+"/v2/pools/stats:batch", `{"poolAddresses":[]}`), 422, "FIELD_INVALID")
}

func TestDevelopmentMarketReadsWithoutChainRPC(t *testing.T) {
	f := newMktMarketFixture(t, false)
	f.setCursor(t, f.chain.head(t).Number.Uint64())
	envelope := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=new"), "PoolListResponse")
	if envelope["watermark"] != nil || envelope["revision"] != nil || len(mktData(envelope)["items"].([]any)) != 0 {
		t.Fatalf("pool list without chain RPC: %v", envelope)
	}
	// No chain RPC is configuration, not an outage: non-retryable.
	for _, path := range []string{"/v2/rankings/trending", "/v2/leaderboards/trader?window=24h", "/v2/holder-rewards/stats"} {
		got := getV2(t, f.srv.URL+path)
		assertV2Error(t, got, 503, "DEPENDENCY_UNAVAILABLE")
		assertRetryable(t, got, false)
	}
}

func TestDevelopmentMarketReadsPools(t *testing.T) {
	f := newMktMarketFixture(t, true)
	now := time.Now().Unix()
	receiptA := f.createPool(t, mktPoolA, mktTokenA, "0x0000000000000000000000000000000000000000", now-2*86400)
	receiptB := f.createPool(t, mktPoolB, mktTokenB, "0x0000000000000000000000000000000000000000", now-3600)
	receiptC := f.createPool(t, mktPoolOptical, mktTokenC, "0x"+strings.Repeat("9", 40), now-60)
	head := f.chain.head(t)
	f.setCursor(t, head.Number.Uint64())
	f.insertState(t, mktPoolA, receiptA, "1000000000", "250000000", "800000000000000000000000000")
	f.insertState(t, mktPoolB, receiptB, "1000000000", "0", "900000000000000000000000000")
	f.insertState(t, mktPoolOptical, receiptC, "1000000000", "0", "900000000000000000000000000")
	// Pool A: 100 bps (latest qualifying swap is the token-side sell); pool B:
	// 250 bps; the optical pool's rate is not recoverable from paid input.
	f.insertSwap(t, mktPoolA, mktTraderOne, true, "1000000", "1000000000000000000", "10000", 20, now-100, 1)
	f.insertSwap(t, mktPoolA, mktTraderTwo, true, "2000000", "2000000000000000000", "20000", 21, now-90, 1)
	f.insertSwap(t, mktPoolA, mktTraderOne, false, "500000000000000000", "600000", "5000000000000000", 22, now-80, 1)
	f.insertSwap(t, mktPoolA, mktTraderTwo, true, "9000000", "9000000000000000000", "90000", 15, now-3*86400, 1)
	f.insertSwap(t, mktPoolB, mktTraderTwo, true, "400000", "1000", "10000", 23, now-70, 1)
	f.insertSwap(t, mktPoolOptical, mktTraderOne, true, "1000000", "1000", "10000", 24, now-60, 1)
	f.insertPoolStats(t, mktPoolA, mktTokenA, "3600000", "250", 2, 1, 3, now-2*86400, now-5)
	f.insertPoolStats(t, mktPoolB, mktTokenB, "400000", "-40", 1, 0, 1, now-3600, now-5)
	f.insertPoolStats(t, mktPoolOptical, mktTokenC, "1000000", "0", 1, 0, 1, now-60, now-5)
	f.rank(t, "trending", goredis.Z{Score: 9.5, Member: mktPoolA}, goredis.Z{Score: 3.25, Member: mktPoolB})
	f.rank(t, "top_volume", goredis.Z{Score: 3600000, Member: mktPoolA}, goredis.Z{Score: 400000, Member: mktPoolB})
	f.rank(t, "new", goredis.Z{Score: 3, Member: mktPoolOptical}, goredis.Z{Score: 2, Member: mktPoolB}, goredis.Z{Score: 1, Member: mktPoolA})

	// Pool detail: registry, indexed state, observed fee, chain positions.
	detail := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolA), "PoolDetailResponse")
	mktAssertWatermark(t, detail["watermark"], head)
	pool := mktData(detail)["pool"].(map[string]any)
	created := pool["createdPosition"].(map[string]any)
	if pool["feeBps"] != float64(100) || pool["effectivePricingUsdlReserveRaw"] != "1250000000" || pool["creator"] != mktCreator || pool["tokenAddress"] != mktTokenA {
		t.Fatalf("pool document: %v", pool)
	}
	if created["height"] != receiptA.BlockNumber.String() || created["blockHash"] != strings.ToLower(receiptA.BlockHash.Hex()) || created["transactionHash"] != strings.ToLower(receiptA.TxHash.Hex()) || created["logIndex"] != strconv.FormatUint(uint64(receiptA.Logs[1].Index), 10) || created["transactionIndex"] != strconv.FormatUint(uint64(receiptA.TransactionIndex), 10) {
		t.Fatalf("created position %v, receipt %+v", created, receiptA.Logs[1])
	}
	stats := mktData(detail)["stats"].(map[string]any)
	if stats["volume24hRaw"] != "3600000" || stats["buys24h"] != "2" || stats["sells24h"] != "1" || stats["holderCount"] != "3" || stats["realUsdlBalanceRaw"] != "250000000" || stats["priceChangeBps24h"] != "250" || stats["changeBaselineQuality"] != "complete" {
		t.Fatalf("pool stats: %v", stats)
	}
	if upper := stats["sourceUpperPosition"].(map[string]any); upper["logIndex"] != "1" || upper["blockHash"] != strings.ToLower(receiptA.BlockHash.Hex()) {
		t.Fatalf("source upper position: %v", upper)
	}
	token := mktData(detail)["token"].(map[string]any)
	if token["address"] != mktTokenA || token["decimalsQuality"] != "unavailable" || token["sourcePosition"] != nil {
		t.Fatalf("token: %v", token)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolOptical), 503, "DEPENDENCY_UNAVAILABLE")

	single := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolB+"/stats"), "StatsResponse")
	if s := mktData(single)["stats"].(map[string]any); s["priceChangeBps24h"] != "-40" || s["changeBaselineQuality"] != "insufficient_history" || s["realUsdlBalanceRaw"] != "0" {
		t.Fatalf("pool B stats: %v", s)
	}
	batch := assertV2Success(t, mktPost(t, f.srv.URL+"/v2/pools/stats:batch", `{"poolAddresses":["0x`+strings.ToUpper(mktPoolB[2:6])+mktPoolB[6:]+`","`+mktPoolUnknown+`","`+mktPoolA+`"]}`), "StatsBatchResponse")
	items, missing := mktData(batch)["items"].([]any), mktData(batch)["missing"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["poolAddress"] != mktPoolB || items[1].(map[string]any)["poolAddress"] != mktPoolA || len(missing) != 1 || missing[0] != mktPoolUnknown {
		t.Fatalf("stats batch: %v", mktData(batch))
	}

	// Pool list pages over the ranking ZSET and detects a re-ranked snapshot.
	first := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=trending&limit=1"), "PoolListResponse")
	if page := mktData(first)["items"].([]any); len(page) != 1 || page[0].(map[string]any)["poolAddress"] != mktPoolA || first["nextCursor"] == nil {
		t.Fatalf("pool list first page: %v", first)
	}
	cursor := first["nextCursor"].(string)
	second := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools?sort=trending&limit=1&cursor="+cursor), "PoolListResponse")
	if page := mktData(second)["items"].([]any); len(page) != 1 || page[0].(map[string]any)["poolAddress"] != mktPoolB || second["nextCursor"] != nil || page[0].(map[string]any)["feeBps"] != float64(250) {
		t.Fatalf("pool list second page: %v", second)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools?sort=volume&limit=1&cursor="+cursor), 409, "RESNAPSHOT_REQUIRED")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools?sort=new"), 503, "DEPENDENCY_UNAVAILABLE")

	// Rankings: ZSET categories and the stats-derived gainers/losers.
	volume := mktData(assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/volume"), "RankingsResponse"))["snapshot"].(map[string]any)
	if entries := volume["items"].([]any); len(entries) != 2 || entries[0].(map[string]any)["poolAddress"] != mktPoolA || entries[0].(map[string]any)["score"] != "3600000" || entries[1].(map[string]any)["rank"] != float64(2) || volume["totalEligible"] != "2" {
		t.Fatalf("volume ranking: %v", volume)
	}
	trending := mktData(assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/trending"), "RankingsResponse"))["snapshot"].(map[string]any)
	if trending["items"].([]any)[1].(map[string]any)["score"] != "3.25" {
		t.Fatalf("trending ranking: %v", trending)
	}
	gainers := mktData(assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/gainers"), "RankingsResponse"))["snapshot"].(map[string]any)
	losers := mktData(assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/losers"), "RankingsResponse"))["snapshot"].(map[string]any)
	if g := gainers["items"].([]any); len(g) != 1 || g[0].(map[string]any)["poolAddress"] != mktPoolA || g[0].(map[string]any)["score"] != "250" {
		t.Fatalf("gainers: %v", gainers)
	}
	if l := losers["items"].([]any); len(l) != 1 || l[0].(map[string]any)["poolAddress"] != mktPoolB || l[0].(map[string]any)["score"] != "-40" {
		t.Fatalf("losers: %v", losers)
	}

	// Leaderboards over indexer.swaps inside the window.
	wallets := func(envelope map[string]any) []string {
		out := []string{}
		for _, e := range mktData(envelope)["snapshot"].(map[string]any)["entries"].([]any) {
			entry := e.(map[string]any)
			out = append(out, entry["wallet"].(string)+"="+entry["metricRaw"].(string))
		}
		return out
	}
	trader := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=24h"), "LeaderboardsResponse")
	if got := strings.Join(wallets(trader), ","); got != mktTraderOne+"=1400000,"+mktTraderTwo+"=2400000" {
		t.Fatalf("trader leaderboard: %s", got)
	}
	if signed := mktData(trader)["snapshot"].(map[string]any)["entries"].([]any)[0].(map[string]any)["metricSigned"]; signed != "-1400000" {
		t.Fatalf("trader signed metric: %v", signed)
	}
	if got := strings.Join(wallets(assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/volume?window=7d"), "LeaderboardsResponse")), ","); got != mktTraderTwo+"=11400000,"+mktTraderOne+"=2600000" {
		t.Fatalf("volume leaderboard: %s", got)
	}
	creator := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/creator?window=24h&limit=1"), "LeaderboardsResponse")
	if got := strings.Join(wallets(creator), ","); got != mktCreator+"=50000" || creator["nextCursor"] != nil {
		t.Fatalf("creator leaderboard: %s %v", got, creator["nextCursor"])
	}
	paged := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=24h&limit=1"), "LeaderboardsResponse")
	nextPage := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=24h&limit=1&cursor="+paged["nextCursor"].(string)), "LeaderboardsResponse")
	if got := strings.Join(wallets(nextPage), ","); got != mktTraderTwo+"=2400000" || nextPage["nextCursor"] != nil {
		t.Fatalf("trader leaderboard page two: %s", got)
	}

	// Holders: keyset pages, pool contract excluded.
	f.insertHolder(t, mktPoolA, mktPoolA, "700000000000000000000000000")
	f.insertHolder(t, mktPoolA, mktHolderOne, "3000")
	f.insertHolder(t, mktPoolA, mktHolderTwo, "5000")
	f.insertHolder(t, mktPoolA, mktHolderThree, "3000")
	f.insertHolder(t, mktPoolA, mktTraderOne, "0")
	holders := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolA+"/holders?limit=2"), "HoldersResponse")
	holderPage := func(envelope map[string]any) string {
		out := []string{}
		for _, item := range mktData(envelope)["items"].([]any) {
			entry := item.(map[string]any)
			if entry["tokenAddress"] != mktTokenA || entry["share"] != nil {
				t.Fatalf("holder entry: %v", entry)
			}
			out = append(out, entry["wallet"].(string)+"="+entry["balanceRaw"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := holderPage(holders); got != mktHolderTwo+"=5000,"+mktHolderOne+"=3000" || holders["nextCursor"] == nil {
		t.Fatalf("holders page one: %s", got)
	}
	if excluded := mktData(holders)["excludedProtocolAddresses"].([]any); len(excluded) != 1 || excluded[0] != mktPoolA {
		t.Fatalf("excluded: %v", excluded)
	}
	mktAssertWatermark(t, holders["watermark"], head)
	holdersNext := assertV2Success(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolA+"/holders?limit=2&cursor="+holders["nextCursor"].(string)), "HoldersResponse")
	if got := holderPage(holdersNext); got != mktHolderThree+"=3000" || holdersNext["nextCursor"] != nil {
		t.Fatalf("holders page two: %s", got)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolB+"/holders?cursor="+holders["nextCursor"].(string)), 400, "CURSOR_INVALID")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/pools/"+mktPoolA+"/holders?offset=2"), 422, "FIELD_UNKNOWN")

	// Holder rewards over the development indexer tables.
	insertFeeDistribution(t, f.pool, mktPoolA, 2, "777", 30, 1)
	insertAirdropClaim(t, f.pool, mktPoolA, mktHolderOne, "100", 31, 1)
	rewards := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats"), "HolderRewardsStatsResponse")
	if d := mktData(rewards); d["cumulativeDistributedRaw"] != "777" || d["walletsRewarded"] != float64(1) {
		t.Fatalf("holder rewards stats: %v", d)
	}
	rewardPools := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=age"), "HolderRewardsPoolsResponse")
	if rows := mktData(rewardPools)["items"].([]any); len(rows) != 1 || rows[0].(map[string]any)["poolAddress"] != mktPoolA || rows[0].(map[string]any)["distributedRaw"] != "777" || rows[0].(map[string]any)["strategyActive"] != true {
		t.Fatalf("holder reward pools: %v", rewardPools)
	}

	// The watermark follows the indexer cursor.
	f.chain.backend.Commit()
	advanced := f.chain.head(t)
	f.setCursor(t, advanced.Number.Uint64())
	mktAssertWatermark(t, mktData(assertV2Success(t, getV2(t, f.srv.URL+"/v2/rankings/new"), "RankingsResponse"))["snapshot"].(map[string]any)["watermark"], advanced)
}

func TestFeeBpsFromSwap(t *testing.T) {
	cases := []struct {
		in, fee string
		bps     int
		ok      bool
	}{
		{"1000000", "10000", 100, true},
		{"1234567", "30864", 250, true},
		{"10000", "0", 0, true},
		{"9999", "99", 0, false},
		{"1000000", "10001", 0, false},
		{"1000000", "1000001", 0, false},
		{"abc", "1", 0, false},
	}
	for _, tc := range cases {
		bps, ok := store.FeeBpsFromSwap(tc.in, tc.fee)
		if bps != tc.bps || ok != tc.ok {
			t.Fatalf("FeeBpsFromSwap(%s,%s)=%d,%v want %d,%v", tc.in, tc.fee, bps, ok, tc.bps, tc.ok)
		}
	}
}

// Against the live chain 125 RPC the watermark carries the canonical hash of
// the block the development indexer cursor names.
func TestDevelopmentMarketReadsLiveChainWatermark(t *testing.T) {
	rpcURL := os.Getenv("NETWORK_RPC_URL")
	if rpcURL == "" {
		t.Skip("requires the live chain 125 RPC: set NETWORK_RPC_URL (e.g. https://api5.mainnet-beta.paxeer.network)")
	}
	pool := internaltest.NewPostgres(t)
	rdb := internaltest.NewRedis(t)
	mktApplyIndexerMigration(t, pool, "cursors", "swaps")
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	latest, err := client.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	height := latest - 5
	var block struct {
		Hash string `json:"hash"`
	}
	if err = client.Client().Call(&block, "eth_getBlockByNumber", hexutil.EncodeUint64(height), false); err != nil {
		t.Fatal(err)
	}
	admission, err := rest.NewAdmission(16, 16, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterDevelopmentMarketReads(context.Background(), router, rest.DevelopmentMarketOptions{Store: store.New(pool, rdb), Admission: admission, ChainRPCURL: rpcURL}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	f := mktMarketFixture{pool: pool, rdb: rdb, srv: srv}
	f.setCursor(t, height)
	snapshot := mktData(assertV2Success(t, getV2(t, srv.URL+"/v2/rankings/trending"), "RankingsResponse"))["snapshot"].(map[string]any)
	wm := snapshot["watermark"].(map[string]any)
	if wm["height"] != strconv.FormatUint(height, 10) || wm["blockHash"] != strings.ToLower(block.Hash) {
		t.Fatalf("live watermark %v, want %d %s", wm, height, block.Hash)
	}
}
