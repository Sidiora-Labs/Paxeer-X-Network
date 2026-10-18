package consumer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The live Paxeer pool the production chart must serve: created by the
// SidioraFactory in creationTx, then one buy and one sell.
const (
	livePool        = "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	liveToken       = "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	liveUSDL        = "0x85fcd13735f4309833a503ee804ea32395851479"
	liveCreator     = "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154"
	liveCreationTx  = "0xe200486fb275ad363101dd09455b23842f1a002b839062732230fd4c908cbfca"
	liveBuyTx       = "0x3d28a4d088c87b4eca7507e74b47bdc01ae30ff4b1e478dc404a1caa9921a503"
	liveSellTx      = "0xf5f9ed1136e71f03ffd44ae7f80ee192998b80fccd96ad3f75824fa9ef3c2845"
	liveActivation  = 32226215
	liveStream      = "canonical"
	liveProjection  = "market-v1"
	liveIndexerFrom = 33963700
	liveCreationCut = 33963720
)

var marketCreatedTopic = crypto.Keccak256Hash([]byte("MarketCreated(address,address,address,uint256,address)"))

type liveSwap struct {
	log     *types.Log
	receipt *types.Receipt
	side    string
	token   string
	usdl    string
	fee     string
}

// productionMigrations applies the production schema files the charts
// consumer reads and writes (indexer source views, candles, api_stream).
func productionMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path unavailable")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "spec", "production-system", "integration", "migrations")
	if _, err := pool.Exec(ctx, "CREATE SCHEMA indexer; CREATE SCHEMA candles"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"60000.sql", "80000.sql", "80001.sql", "100000.sql", "100001.sql", "150002.sql", "150003.sql", "190001.sql"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(raw)); err != nil {
			tx.Rollback(ctx)
			t.Fatalf("apply %s: %v", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func receiptDigest(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func liveFill(t *testing.T, s liveSwap, header chainread.Block, decimals uint8) accounting.FillV1 {
	t.Helper()
	trader := liveCreator
	f := accounting.FillV1{
		Version:       accounting.FillVersion,
		Position:      accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(s.log.BlockNumber, 10), TransactionIndex: strconv.FormatUint(uint64(s.log.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(s.log.Index), 10), TransactionHash: s.log.TxHash.Hex(), BlockHash: s.log.BlockHash.Hex()},
		ActualEmitter: strings.ToLower(s.log.Address.Hex()), PoolAddress: livePool,
		Token: accounting.Asset{Address: liveToken, Decimals: decimals}, Settlement: accounting.Asset{Address: liveUSDL, Decimals: 6},
		Side: s.side, TokenAmountRaw: s.token, USDLAmountRaw: s.usdl, FeeRaw: s.fee, InputDerivationQuality: "receipt_incomplete",
		Attribution:           accounting.Attribution{TransactionSender: liveCreator, Trader: &trader, Quality: "exact_receipt", Evidence: []accounting.EvidenceRef{{Kind: "receipt", ArtifactSHA256: receiptDigest(t, s.receipt), ArtifactID: s.log.TxHash.Hex()}}},
		Execution:             accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Provenance:            accounting.Provenance{DeploymentID: "paxeer-sidiora-live", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch-1", PoolCodeEpoch: "epoch-1", ReceiptSHA256: receiptDigest(t, s.receipt), HeaderSHA256: receiptDigest(t, header)},
		BlockTimestampSeconds: strconv.FormatInt(header.Time, 10),
	}
	if s.side == "buy" {
		f.PaidInputRaw = s.usdl
		f.FeeDisposition = "distributed_usdl"
	} else {
		f.PaidInputRaw = s.token
		f.FeeDisposition = "retained_token_reserve"
	}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, f.Provenance.DeploymentID)
	if err := accounting.ValidateFillV1(f); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestSourceVerifiedBootstrapFromPoolCreationLive runs the production charts
// consumer over the real Paxeer pool: the indexer stream begins at the
// deployment activation height (1.7M blocks before the pool existed), the pool
// is bootstrapped from its own creation, and both real trades become exact
// candles through a real JetStream broker and the production schema.
func TestSourceVerifiedBootstrapFromPoolCreationLive(t *testing.T) {
	rpcURL := os.Getenv("NETWORK_RPC_URL")
	if rpcURL == "" {
		t.Fatal("NETWORK_RPC_URL must name the Paxeer RPC")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool := internaltest.OpenPool(t, internaltest.NewDatabase(t))
	productionMigrations(t, ctx, pool)
	rpc, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()

	receipt := func(hash string) *types.Receipt {
		t.Helper()
		r, err := rpc.TransactionReceipt(ctx, common.HexToHash(hash))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	creationReceipt, buyReceipt, sellReceipt := receipt(liveCreationTx), receipt(liveBuyTx), receipt(liveSellTx)
	var creation *types.Log
	for _, l := range creationReceipt.Logs {
		if len(l.Topics) == 4 && l.Topics[0] == marketCreatedTopic && l.Topics[2] == common.BytesToHash(common.HexToAddress(livePool).Bytes()) {
			creation = l
		}
	}
	if creation == nil {
		t.Fatal("MarketCreated log for the live pool not found")
	}
	swapOf := func(r *types.Receipt) *types.Log {
		for _, l := range r.Logs {
			if len(l.Topics) > 1 && l.Topics[0] == swapEventTopic && l.Topics[1] == common.BytesToHash(common.HexToAddress(livePool).Bytes()) {
				return l
			}
		}
		t.Fatal("Swap log for the live pool not found")
		return nil
	}
	swaps := []liveSwap{
		{log: swapOf(buyReceipt), receipt: buyReceipt, side: "buy", token: "9606813905120", usdl: "100000000", fee: "3000000"},
		{log: swapOf(sellReceipt), receipt: sellReceipt, side: "sell", token: "4803406952560", usdl: "47278912", fee: "144102208576"},
	}
	last := int64(swaps[1].log.BlockNumber)
	if int64(creation.BlockNumber) <= liveIndexerFrom || int64(creation.BlockNumber) > liveCreationCut || int64(swaps[0].log.BlockNumber) <= liveCreationCut {
		t.Fatal("live heights moved outside the fixture commit layout")
	}
	tokenAddress := common.HexToAddress(liveToken)
	rawDecimals, err := rpc.CallContract(ctx, ethereum.CallMsg{To: &tokenAddress, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}, nil)
	if err != nil || len(rawDecimals) != 32 {
		t.Fatal("token decimals unavailable", err)
	}
	decimals := uint8(new(big.Int).SetBytes(rawDecimals).Uint64())

	// Indexer source facts exactly as the production indexer commits them.
	headers := map[int64]chainread.Block{}
	for h := int64(liveIndexerFrom); h <= last; h++ {
		block, err := chainread.Reader{RPC: rpc}.Block(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		headers[h] = block
	}
	rawCount := map[int64]int{}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql[:min(len(sql), 80)], err)
		}
	}
	for _, r := range []*types.Receipt{creationReceipt, buyReceipt, sellReceipt} {
		for _, l := range r.Logs {
			topics := make([]string, len(l.Topics))
			for i, topic := range l.Topics {
				topics[i] = topic.Hex()
			}
			encoded, _ := json.Marshal(topics)
			exec(`INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,$2,$3,$4,$5,$6,$7,$8)`, l.TxHash.Hex(), int64(l.Index), int64(l.BlockNumber), int64(l.TxIndex), l.BlockHash.Hex(), strings.ToLower(l.Address.Hex()), encoded, l.Data)
			rawCount[int64(l.BlockNumber)]++
		}
	}
	for h := int64(liveIndexerFrom); h <= last; h++ {
		exec(`INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete) VALUES(125,$1,$2,$3,$4,true,true)`, liveStream, h, headers[h].Hash, rawCount[h])
	}
	commit := func(id string, previous, lastHeight int64, hash string) {
		count := 0
		for h := previous + 1; h <= lastHeight; h++ {
			count += rawCount[h]
		}
		exec(`INSERT INTO indexer.indexer_ingest_commits(chain_id,stream_id,batch_id,previous_height,last_complete_height,block_hash,record_count,outbox_count) VALUES(125,$1,$2,$3,$4,$5,$6,0)`, liveStream, id, previous, lastHeight, hash, count)
	}
	commit("activation", liveActivation-1, liveIndexerFrom, headers[liveIndexerFrom].Hash)
	commit("creation", liveIndexerFrom, liveCreationCut, headers[liveCreationCut].Hash)
	commit("trades", liveCreationCut, last, headers[last].Hash)
	exec(`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,$2,$2)`, liveStream, last)
	exec(`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,$1,'indexer-live',1,clock_timestamp()+interval '1 hour')`, liveStream)
	nftID := new(big.Int).SetBytes(creation.Data[:32]).String()
	optical := strings.ToLower(common.BytesToAddress(creation.Data[32:64]).Hex())
	exec(`INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'{}','{}')`, liveToken, livePool, liveCreator, strings.ToLower(creation.Address.Hex()), optical, nftID, int64(creation.BlockNumber), creation.BlockHash.Hex(), creation.TxHash.Hex(), int64(creation.TxIndex), int64(creation.Index), headers[int64(creation.BlockNumber)].Time)

	indexerPartitions, err := delivery.NewPartitionStore(pool, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"market": true}}
	var fills []accounting.FillV1
	var wires [][]byte
	for _, s := range swaps {
		fill := liveFill(t, s, headers[int64(s.log.BlockNumber)], decimals)
		digest, err := accounting.FillDigest(fill)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(fill)
		exec(`INSERT INTO indexer.indexer_accounting_fills(source_fill_id,source_fact_digest,chain_id,transaction_hash,log_index,pool_address,payload) VALUES($1,$2,125,$3,$4,$5,$6)`, fill.SourceFillID, string(digest), fill.Position.TransactionHash, int64(s.log.Index), livePool, body)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		position, err := indexerPartitions.AppendTx(ctx, tx, delivery.AppendInput{ProjectionVersion: liveProjection, PartitionKind: "market", PartitionKey: "125:" + livePool, SourceEventID: fill.SourceFillID, EffectIdentity: accounting.EffectIdentity(fill.SourceFillID, "market", liveProjection, "", "market", "125:"+livePool), PayloadDigest: digest, SourceOrder: fill.Position})
		if err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		publication, err := accounting.NewMarketPublication(fill, []accounting.PartitionPosition{position})
		if err != nil {
			t.Fatal(err)
		}
		payload, err := accounting.CanonicalPublicationV1(publication)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO indexer.indexer_ingest_outbox(chain_id,transaction_hash,log_index,event_kind,payload) VALUES(125,$1,$2,'accounting.market.v1',$3)`, fill.Position.TransactionHash, int64(s.log.Index), payload)
		envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: "market", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: fill.Position.Height, TransactionIndex: fill.Position.TransactionIndex, LogIndex: fill.Position.LogIndex}, EmittedAt: time.Now().UTC(), TraceID: "charts_live_bootstrap", PayloadHash: delivery.Hash(payload), Payload: payload}
		wire, err := envelope.Encode(policy, accounting.MarketSubject)
		if err != nil {
			t.Fatal(err)
		}
		fills = append(fills, fill)
		wires = append(wires, wire)
	}

	exact, err := store.NewExact(pool, "candles")
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := delivery.NewStore(pool, "candles", policy)
	if err != nil {
		t.Fatal(err)
	}
	partitions, err := delivery.NewPartitionStore(pool, "candles")
	if err != nil {
		t.Fatal(err)
	}
	consumerFor := func(generation string) *AccountingConsumer {
		t.Helper()
		c, err := NewSourceVerifiedAccountingConsumer(SourceVerifiedAccountingConfig{Pool: pool, Deliveries: deliveries, Partitions: partitions, Exact: exact, ConsumerID: "charts", Generation: generation, ProjectionVersion: liveProjection, Owner: "charts-live", SourceStream: liveStream, SourcePartitionSchema: "indexer", RPC: rpc, MaxReplayBlocks: 4096, MaxReplayFills: 10000, SeriesV2: true, StreamWriter: &delivery.StreamWriter{Schema: "api_stream"}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Fail-closed bootstrap evidence, checked on an isolated generation.
	probe := consumerFor("probe")
	exec(`ALTER TABLE indexer.indexer_canonical_creations RENAME TO indexer_canonical_creations_held`)
	exec(`CREATE TABLE indexer.indexer_canonical_creations (LIKE indexer.indexer_canonical_creations_held INCLUDING ALL)`)
	exec(`CREATE OR REPLACE VIEW indexer.indexer_canonical_creations_v1 AS SELECT token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload FROM indexer.indexer_canonical_creations`)
	if _, err = probe.config.Lease(ctx, fills[0]); !errors.Is(err, ErrAccountingSourceUnavailable) {
		t.Fatal("bootstrap without an indexed pool creation was not refused as unavailable", err)
	}
	exec(`DROP VIEW indexer.indexer_canonical_creations_v1`)
	exec(`DROP TABLE indexer.indexer_canonical_creations`)
	exec(`ALTER TABLE indexer.indexer_canonical_creations_held RENAME TO indexer_canonical_creations`)
	exec(`CREATE VIEW indexer.indexer_canonical_creations_v1 AS SELECT token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload FROM indexer.indexer_canonical_creations`)
	gap := int64(creation.BlockNumber) + 1
	exec(`UPDATE indexer.indexer_block_completeness SET fetch_complete=false WHERE chain_id=125 AND stream_id=$1 AND height=$2`, liveStream, gap)
	if _, err = probe.config.Lease(ctx, fills[0]); !errors.Is(err, ErrAccountingAuthority) {
		t.Fatal("bootstrap over an incompletely ingested replay range was not refused", err)
	}
	exec(`UPDATE indexer.indexer_block_completeness SET fetch_complete=true WHERE chain_id=125 AND stream_id=$1 AND height=$2`, liveStream, gap)

	// Real broker delivery through the production consumer.
	broker := newFaultBroker(t)
	nc := broker.start()
	publisher, reader := faultProvision(t, ctx, nc, accounting.MarketSubject, "charts")
	for _, wire := range wires {
		var envelope delivery.Envelope
		if err = json.Unmarshal(wire, &envelope); err != nil {
			t.Fatal(err)
		}
		if _, err = publisher.Publish(ctx, accounting.MarketSubject, envelope.EventID, wire); err != nil {
			t.Fatal(err)
		}
	}
	live := consumerFor("live")
	consumed := 0
	for consumed < len(wires) {
		for _, message := range faultFetch(t, ctx, reader) {
			if err = live.Consume(ctx, message); err != nil {
				t.Fatalf("consume %d: %v", consumed, err)
			}
			consumed++
		}
	}
	minute := headers[int64(swaps[0].log.BlockNumber)].Time / 60 * 60
	bars, err := engine.ReadSeries(ctx, exact, livePool, "1m", minute-600, minute+600, false)
	if err != nil {
		t.Fatal(err)
	}
	var trades uint64
	for _, bar := range bars {
		trades += bar.Trades
	}
	if len(bars) == 0 || trades != 2 || bars[0].Start != minute || bars[0].Synthetic {
		raw, _ := json.Marshal(bars)
		t.Fatalf("live pool candles: %s", raw)
	}
	var revisions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM candles.chart_exact_v1 WHERE chain_id='125' AND pool=$1 AND interval='1m'`, livePool).Scan(&revisions); err != nil || revisions != len(bars) {
		t.Fatal("canonical chart_exact_v1 read model does not hold the live candles", revisions, err)
	}
	var watermark int64
	if err = pool.QueryRow(ctx, `SELECT watermark_height FROM candles.chart_series_clock WHERE chain_id='125' AND pool=$1`, livePool).Scan(&watermark); err != nil || watermark != last {
		t.Fatal("series clock not advanced to the indexer watermark", watermark, err)
	}
	raw, _ := json.Marshal(bars[0])
	t.Logf("live 1m candle: %s", raw)
	t.Logf("bootstrap anchored at creation height %d; stream began at %d", creation.BlockNumber, liveActivation-1)
}
