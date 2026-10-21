package consumer_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/holdings"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/projectionstats"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/util"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// realLegacyPool is ../projectionstats/testdata/real_pool_125.json: receipts of
// the first real Sidiora pool on Paxeer chain 125 (creation, buy, sell).
type realLegacyPool struct {
	Pool          string `json:"pool"`
	Token         string `json:"token"`
	USDL          string `json:"usdl"`
	EventEmitter  string `json:"eventEmitter"`
	Factory       string `json:"factory"`
	TokenDecimals uint8  `json:"tokenDecimals"`
	Blocks        map[string]struct {
		Hash      string `json:"hash"`
		Timestamp int64  `json:"timestamp"`
	} `json:"blocks"`
	Transactions []struct {
		Hash             string `json:"hash"`
		From             string `json:"from"`
		BlockNumber      int64  `json:"blockNumber"`
		BlockHash        string `json:"blockHash"`
		TransactionIndex int64  `json:"transactionIndex"`
		Logs             []struct {
			LogIndex int64    `json:"logIndex"`
			Address  string   `json:"address"`
			Topics   []string `json:"topics"`
			Data     string   `json:"data"`
		} `json:"logs"`
	} `json:"transactions"`
}

const legacyStream = "paxeer-125-committed"
const legacyVersion = "stats-market-v1"
const legacySwapTopic = "0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc"

// legacyBridgeFixture is a throwaway database holding the real chain-125 pool's
// canonical source rows (creation, raw logs, committed fills) and a bridge
// wired to the production SwapConsumer and HolderTracker.
type legacyBridgeFixture struct {
	pg       *pgxpool.Pool
	st       *store.Store
	bridge   *consumer.CanonicalLegacyBridge
	real     realLegacyPool
	deployer string
	// sellPrice is the post-trade spot price of the sell fill; buyTokens is
	// the token amount the buy fill credited to the deployer.
	sellPrice, buyTokens string
	exec                 func(sql string, args ...any)
}

func newLegacyBridgeFixture(t *testing.T, batch int) legacyBridgeFixture {
	t.Helper()
	ctx := context.Background()
	_, pg := internaltest.NewPostgresWithDSN(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pg.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v: %.160s", err, sql)
		}
	}
	exec("CREATE SCHEMA indexer")
	for _, id := range []string{"60000", "80000", "190001"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "production-system", "integration", "migrations", id+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	repo, err := projectionstats.NewRepository(pg, "stats")
	if err != nil {
		t.Fatal(err)
	}
	exec(repo.MigrationSQL())
	raw, err := os.ReadFile(filepath.Join("..", "projectionstats", "testdata", "real_pool_125.json"))
	if err != nil {
		t.Fatal(err)
	}
	var real realLegacyPool
	if err = json.Unmarshal(raw, &real); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,$1,'indexer-0',1,clock_timestamp()+interval '1 hour')`, legacyStream)
	exec(`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,33963761,33963761)`, legacyStream)
	provenance := accounting.Provenance{DeploymentID: "paxeer125-a35ff7c6f532d2f9", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "event-emitter-1", PoolCodeEpoch: "pool-1", ReceiptSHA256: strings.Repeat("a", 64), HeaderSHA256: strings.Repeat("b", 64)}
	provenanceRaw, _ := json.Marshal(provenance)
	sequence := 0
	var sellPrice, deployer, buyTokens string
	for i, tx := range real.Transactions {
		timestamp := real.Blocks[strconv.FormatInt(tx.BlockNumber, 10)].Timestamp
		for _, l := range tx.Logs {
			topics, _ := json.Marshal(l.Topics)
			data, _ := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
			exec(`INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,$2,$3,$4,$5,$6,$7,$8)`, tx.Hash, l.LogIndex, tx.BlockNumber, tx.TransactionIndex, tx.BlockHash, l.Address, string(topics), data)
		}
		if i == 0 {
			deployer = tx.From
			exec(`INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,14,$10,$11,'{}')`, real.Token, real.Pool, tx.From, real.Factory, "0x0000000000000000000000000000000000000000", tx.BlockNumber, tx.BlockHash, tx.Hash, tx.TransactionIndex, timestamp, string(provenanceRaw))
		}
		for _, l := range tx.Logs {
			if l.Address != real.EventEmitter || l.Topics[0] != legacySwapTopic {
				continue
			}
			data, _ := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
			word := func(i int) string { return new(big.Int).SetBytes(data[i*32 : (i+1)*32]).String() }
			fill := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(tx.BlockNumber, 10), TransactionIndex: strconv.FormatInt(tx.TransactionIndex, 10), LogIndex: strconv.FormatInt(l.LogIndex, 10), TransactionHash: tx.Hash, BlockHash: tx.BlockHash},
				ActualEmitter: l.Address, PoolAddress: real.Pool, Token: accounting.Asset{Address: real.Token, Decimals: real.TokenDecimals}, Settlement: accounting.Asset{Address: real.USDL, Decimals: 6},
				PaidInputRaw: word(1), FeeRaw: word(3), InputDerivationQuality: "receipt_incomplete",
				Attribution: accounting.Attribution{TransactionSender: tx.From, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
				Execution:   accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
				Provenance:  provenance, BlockTimestampSeconds: strconv.FormatInt(timestamp, 10)}
			if word(0) == "1" {
				fill.Side, fill.TokenAmountRaw, fill.USDLAmountRaw, fill.FeeDisposition = "buy", word(2), word(1), "distributed_usdl"
				buyTokens = word(2)
			} else {
				fill.Side, fill.TokenAmountRaw, fill.USDLAmountRaw, fill.FeeDisposition = "sell", word(1), word(2), "retained_token_reserve"
				sellPrice = word(4)
			}
			fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, provenance.DeploymentID)
			digest, err := accounting.FillDigest(fill)
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(fill)
			sequence++
			exec(`INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,$4,$5,$6,'{}',$7,$8,1)`, "publication-"+fill.SourceFillID, fill.SourceFillID, string(digest), legacyVersion, real.Pool, timestamp/60*60, string(payload), sequence)
		}
	}

	st := store.New(pg)
	rdb := internaltest.NewRedis(t)
	market, err := consumer.NewMarketConsumer(st, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	holder := consumer.NewHolderTracker(st, rdb, discardLogger(), 20*time.Millisecond)
	t.Cleanup(holder.Close)
	bridge := &consumer.CanonicalLegacyBridge{Pool: pg, ProjectionSchema: "stats", SourceSchema: "indexer", SourceStream: legacyStream, ProjectionVersion: legacyVersion, Market: market, Swap: consumer.NewSwapConsumer(st, rdb, discardLogger()), Holder: holder, Batch: batch}
	return legacyBridgeFixture{pg: pg, st: st, bridge: bridge, real: real, deployer: deployer, sellPrice: sellPrice, buyTokens: buyTokens, exec: exec}
}

func TestCanonicalLegacyBridgeReplaysRealPaxeerPool(t *testing.T) {
	ctx := context.Background()
	f := newLegacyBridgeFixture(t, 100)
	pg, st, bridge, real, deployer, sellPrice, exec := f.pg, f.st, f.bridge, f.real, f.deployer, f.sellPrice, f.exec
	applied, err := bridge.RunOnce(ctx)
	if err != nil || applied != 3 {
		t.Fatal(applied, err)
	}
	stats, err := st.GetPoolStats(ctx, real.Pool)
	if err != nil || stats == nil {
		t.Fatal(stats, err)
	}
	if stats.CreatedAt != real.Blocks["33963716"].Timestamp || stats.CreatorAddress == nil || *stats.CreatorAddress != deployer || stats.TokenAddress != real.Token || stats.Price != sellPrice {
		t.Fatal(stats.CreatedAt, stats.CreatorAddress, stats.TokenAddress, stats.Price, sellPrice)
	}
	// The platform aggregate behind GET /v2/platform/metrics: its market cap
	// is the pool's total-supply valuation at the sell's spot price,
	// floor(10098226981692*1e15/1e18)=10098226981 USDL raw (the same figure as
	// the v2 PoolStats totalSupplyValuationUsdlRaw); both fills and the pool's
	// creation fall inside the 24h window.
	platform, err := st.PlatformMetrics(ctx, real.Blocks["33963749"].Timestamp+60)
	if err != nil || sellPrice != "10098226981692" || platform.TotalMarketCap != "10098226981" || platform.TotalTransactions24h != 2 || platform.NewTokens24h != 1 || platform.TotalTokensLaunched != 1 || platform.TotalBuys24h != 1 || platform.TotalSells24h != 1 {
		t.Fatal(platform, err)
	}
	transactions, err := st.ListTransactions(ctx, real.Pool, 10, 0, "")
	if err != nil || len(transactions) != 2 {
		t.Fatal(transactions, err)
	}
	for _, row := range transactions {
		if row.Sender != deployer || (row.IsBuy && (row.AmountIn != "100000000" || row.Fee != "3000000")) || (!row.IsBuy && row.AmountOut != "47278912") {
			t.Fatal(row)
		}
	}
	holders, err := st.ListHolders(ctx, real.Pool, 10, 0)
	if err != nil || len(holders) != 1 || holders[0].HolderAddress != deployer || holders[0].Balance != "4803406952560" {
		t.Fatal(holders, err)
	}
	// /v1/pools/{pool}/holders (proxied by core-api in production) serves the
	// Transfer-authoritative holders with quality and watermark once the
	// bridge has created the pool's stats context.
	ddl, err := holdings.SchemaSQL("stats_transfer_holdings")
	if err != nil {
		t.Fatal(err)
	}
	exec(ddl)
	holderRepo, err := holdings.NewRepository(pg, "stats_transfer_holdings")
	if err != nil {
		t.Fatal(err)
	}
	if err = holderRepo.Discover(ctx, real.Token, real.TokenDecimals, strconv.FormatInt(real.Transactions[0].BlockNumber, 10), true); err != nil {
		t.Fatal(err)
	}
	var lastTransfer holdings.Transfer
	for _, tx := range real.Transactions {
		for _, l := range tx.Logs {
			if l.Address != real.Token || l.Topics[0] != "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" {
				continue
			}
			amount, _ := new(big.Int).SetString(strings.TrimPrefix(l.Data, "0x"), 16)
			lastTransfer = holdings.Transfer{ChainID: "125", Token: real.Token, TransactionHash: tx.Hash, BlockHash: tx.BlockHash, Height: strconv.FormatInt(tx.BlockNumber, 10), TransactionIndex: strconv.FormatInt(tx.TransactionIndex, 10), LogIndex: strconv.FormatInt(l.LogIndex, 10), From: "0x" + l.Topics[1][26:], To: "0x" + l.Topics[2][26:], Amount: amount.String()}
			if _, err = holderRepo.Apply(ctx, lastTransfer); err != nil {
				t.Fatal(err)
			}
		}
	}
	router := chi.NewRouter()
	httpapi.RegisterTransferHolders(router, st, holderRepo)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/v1/pools/"+real.Pool+"/holders?limit=10", nil))
	var holdersBody struct {
		Quality   string `json:"quality"`
		Watermark *struct {
			Height, BlockHash, Integrity string
		} `json:"watermark"`
		Data struct {
			Items []struct {
				Wallet     string `json:"wallet"`
				BalanceRaw string `json:"balanceRaw"`
			} `json:"items"`
			Coverage string `json:"coverage"`
		} `json:"data"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &holdersBody) != nil || holdersBody.Quality != "complete" || holdersBody.Watermark == nil || holdersBody.Watermark.Height != lastTransfer.Height || holdersBody.Watermark.BlockHash != lastTransfer.BlockHash || holdersBody.Watermark.Integrity != "healthy" || holdersBody.Data.Coverage != "complete" || len(holdersBody.Data.Items) != 2 {
		t.Fatal(response.Code, response.Body.String())
	}
	balances := map[string]string{}
	for _, item := range holdersBody.Data.Items {
		balances[item.Wallet] = item.BalanceRaw
	}
	if balances[deployer] != "4803406952560" || balances[real.Pool] != "995196593047440" {
		t.Fatal(balances)
	}
	// Replay is idempotent: the pool_transactions inbox excludes applied fills.
	if applied, err = bridge.RunOnce(ctx); err != nil || applied != 0 {
		t.Fatal(applied, err)
	}
	// Tampered EventEmitter evidence stops the bridge instead of skipping.
	exec(`DELETE FROM stats.pool_transactions WHERE tx_hash=$1`, real.Transactions[2].Hash)
	exec(`UPDATE indexer.indexer_raw_logs SET data=overlay(data placing '\x01'::bytea from 64 for 1) WHERE transaction_hash=$1 AND log_index=2`, real.Transactions[2].Hash)
	if _, err = bridge.RunOnce(ctx); !errors.Is(err, consumer.ErrLegacyBridgeIntegrity) {
		t.Fatal(err)
	}
	// An expired ingest lease is an unavailable source.
	exec(`UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()-interval '1 second'`)
	if _, err = bridge.RunOnce(ctx); !errors.Is(err, consumer.ErrAccountingSourceUnavailable) {
		t.Fatal(err)
	}
}

// legacySnapshot reads the pool's inbox rows and the deployer's swap-ledger
// balance in one REPEATABLE READ snapshot, the way a reader deriving holders
// freshness from the inbox would see them together.
func legacySnapshot(t *testing.T, f legacyBridgeFixture) (inbox []string, balance *string) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pg.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT id FROM stats.pool_transactions WHERE pool_address=$1 ORDER BY id`, f.real.Pool)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		inbox = append(inbox, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	var b string
	err = tx.QueryRow(ctx, `SELECT balance FROM stats.pool_holders WHERE pool_address=$1 AND holder_address=$2`, f.real.Pool, f.deployer).Scan(&b)
	switch {
	case err == nil:
		balance = &b
	case !errors.Is(err, pgx.ErrNoRows):
		t.Fatal(err)
	}
	return inbox, balance
}

func countRows(t *testing.T, f legacyBridgeFixture, table string) int {
	t.Helper()
	var n int
	if err := f.pg.QueryRow(context.Background(), `SELECT count(*)::int FROM stats.`+table+` WHERE pool_address=$1`, f.real.Pool).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A fill whose holder apply fails leaves no inbox row and none of its derived
// legacy writes, is retried (still first in chain order) on every pass while
// the failure persists, and projects exactly once when it is lifted.
func TestCanonicalLegacyBridgeHolderFailureRollsBackFillAndRetries(t *testing.T) {
	ctx := context.Background()
	f := newLegacyBridgeFixture(t, 100)
	// A real constraint on the throwaway database: the deployer's swap-ledger
	// holder row cannot be written, so ApplyHolderDelta fails after the inbox
	// insert, volume bucket, price snapshot and pool_stats writes of the same fill.
	f.exec(`ALTER TABLE stats.pool_holders ADD CONSTRAINT legacy_bridge_test_block CHECK (holder_address <> '` + f.deployer + `')`)
	initial, err := f.st.GetPoolStats(ctx, f.real.Pool)
	if err != nil || initial != nil {
		t.Fatal(initial, err)
	}
	var bootstrapUpdatedAt int64
	bootstrap := func(s *store.PoolStatsRow) int64 {
		if bootstrapUpdatedAt == 0 {
			bootstrapUpdatedAt = s.UpdatedAt
		}
		return bootstrapUpdatedAt
	}
	for pass := 0; pass < 2; pass++ {
		applied, err := f.bridge.RunOnce(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "legacy_bridge_test_block" {
			t.Fatalf("pass %d: %v", pass, err)
		}
		// Pass 0 bootstraps the pool (creation, separately committed); no
		// fill is counted on either pass.
		if want := map[int]int{0: 1, 1: 0}[pass]; applied != want {
			t.Fatalf("pass %d applied %d", pass, applied)
		}
		if inbox, balance := legacySnapshot(t, f); len(inbox) != 0 || balance != nil {
			t.Fatalf("pass %d: fill partially visible: inbox=%v balance=%v", pass, inbox, balance)
		}
		for _, table := range []string{"volume_buckets", "bucket_traders", "price_snapshots", "pool_holders"} {
			if n := countRows(t, f, table); n != 0 {
				t.Fatalf("pass %d: %s has %d rows after rollback", pass, table, n)
			}
		}
		// pool_stats keeps the creation bootstrap: the fill's price, volume
		// and counters were rolled back with its inbox row.
		stats, err := f.st.GetPoolStats(ctx, f.real.Pool)
		if err != nil || stats == nil || stats.Volume24h != "0" || stats.BuyCount24h != 0 || stats.SellCount24h != 0 || stats.Price != stats.High24h || stats.Price != stats.Low24h || stats.UpdatedAt != bootstrap(stats) {
			t.Fatalf("pass %d: pool_stats changed by rolled-back fill: %+v %v", pass, stats, err)
		}
	}
	f.exec(`ALTER TABLE stats.pool_holders DROP CONSTRAINT legacy_bridge_test_block`)
	applied, err := f.bridge.RunOnce(ctx)
	if err != nil || applied != 2 {
		t.Fatal(applied, err)
	}
	inbox, balance := legacySnapshot(t, f)
	if len(inbox) != 2 || balance == nil || *balance != "4803406952560" {
		t.Fatal(inbox, balance)
	}
	var buys, sells int
	if err = f.pg.QueryRow(ctx, `SELECT coalesce(sum(buy_count),0)::int,coalesce(sum(sell_count),0)::int FROM stats.volume_buckets WHERE pool_address=$1`, f.real.Pool).Scan(&buys, &sells); err != nil || buys != 1 || sells != 1 {
		t.Fatal(buys, sells, err)
	}
	stats, err := f.st.GetPoolStats(ctx, f.real.Pool)
	if err != nil || stats == nil || stats.Price != f.sellPrice {
		t.Fatal(stats, err)
	}
	if applied, err = f.bridge.RunOnce(ctx); err != nil || applied != 0 {
		t.Fatal(applied, err)
	}
	if _, balance = legacySnapshot(t, f); balance == nil || *balance != "4803406952560" {
		t.Fatal(balance)
	}
}

// While a fill's holder write is held back (another session owns the
// per-holder advisory lock), the fill's inbox row is not visible to other
// sessions; once it commits, the inbox row and the holder row appear in the
// same snapshot, one fill at a time in chain order.
func TestCanonicalLegacyBridgeInboxVisibleOnlyWithHolderRow(t *testing.T) {
	ctx := context.Background()
	f := newLegacyBridgeFixture(t, 1)
	// With Batch 1 the first pass bootstraps the pool and projects only the
	// buy. Hold the deployer's per-holder advisory lock across it.
	blocker, err := f.pg.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	if _, err = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, util.HashToInt64("holder-"+f.real.Pool+"-"+f.deployer)); err != nil {
		t.Fatal(err)
	}
	type result struct {
		applied int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		applied, err := f.bridge.RunOnce(ctx)
		done <- result{applied, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err = f.pg.QueryRow(ctx, `SELECT count(*)::int FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("bridge finished without reaching the holder write: %+v", r)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge never blocked on the holder advisory lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The bridge has written the inbox row, bucket and pool_stats for the buy
	// in its open transaction and is waiting to apply the holder delta.
	if inbox, balance := legacySnapshot(t, f); len(inbox) != 0 || balance != nil {
		t.Fatalf("inbox visible before holder change: inbox=%v balance=%v", inbox, balance)
	}
	if n := countRows(t, f, "volume_buckets"); n != 0 {
		t.Fatalf("derived write visible before commit: %d buckets", n)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.applied != 2 {
		t.Fatal(r)
	}
	inbox, balance := legacySnapshot(t, f)
	if len(inbox) != 1 || balance == nil || *balance != f.buyTokens {
		t.Fatal(inbox, balance, f.buyTokens)
	}
	applied, err := f.bridge.RunOnce(ctx)
	if err != nil || applied != 1 {
		t.Fatal(applied, err)
	}
	if inbox, balance = legacySnapshot(t, f); len(inbox) != 2 || balance == nil || *balance != "4803406952560" {
		t.Fatal(inbox, balance)
	}
}
