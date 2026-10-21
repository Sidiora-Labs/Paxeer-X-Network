package projectionstats

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/holdings"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// realPool125 is testdata/real_pool_125.json: the receipts of the first real
// Sidiora pool on Paxeer chain 125 (creation, one buy, one sell) and the pool's
// on-chain getReserves() at each block, fetched from the public RPC.
type realPool125 struct {
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
	Transactions []realTransaction `json:"transactions"`
	ReservesAt   map[string]struct {
		VirtualUSDL  string `json:"virtualUsdl"`
		RealUSDL     string `json:"realUsdl"`
		TokenReserve string `json:"tokenReserve"`
	} `json:"reservesAt"`
}

type realTransaction struct {
	Hash             string    `json:"hash"`
	From             string    `json:"from"`
	BlockNumber      int64     `json:"blockNumber"`
	BlockHash        string    `json:"blockHash"`
	TransactionIndex int64     `json:"transactionIndex"`
	Logs             []realLog `json:"logs"`
}

type realLog struct {
	LogIndex int64    `json:"logIndex"`
	Address  string   `json:"address"`
	Topics   []string `json:"topics"`
	Data     string   `json:"data"`
}

const (
	publicTestStream     = "paxeer-125-committed"
	publicTestVersion    = "stats-market-v1"
	publicTestHolders    = "stats_transfer_holdings"
	transferTopic        = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	publicTestDeployment = "paxeer125-a35ff7c6f532d2f9"
)

func migrationSQL(t *testing.T, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "launchpad", "spec", "production-system", "integration", "migrations", id+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type publicFixture struct {
	t        *testing.T
	ctx      context.Context
	pg       *pgxpool.Pool
	repo     *Repository
	holdings *holdings.Repository
	real     realPool125
	sequence int64
}

func newPublicFixture(t *testing.T) *publicFixture {
	t.Helper()
	ctx := context.Background()
	_, pg := internaltest.NewPostgresWithDSN(t)
	f := &publicFixture{t: t, ctx: ctx, pg: pg}
	raw, err := os.ReadFile(filepath.Join("testdata", "real_pool_125.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f.real); err != nil {
		t.Fatal(err)
	}
	f.exec("CREATE SCHEMA indexer")
	for _, id := range []string{"60000", "80000", "190001", "150002", "150004"} {
		f.exec(strings.ReplaceAll(migrationSQL(t, id), `"indexer".`, "indexer."))
	}
	if f.repo, err = NewRepository(pg, "stats"); err != nil {
		t.Fatal(err)
	}
	// The registered stats migrations, not the repository DDL helpers: 130000
	// (projection tables + accounting_market_commits_v1), 130001 (phase-2) and
	// 150005 (trade_facts_v1) create the owner-interface views that
	// api_stream.append_market_stats (150004) reads.
	for _, id := range []string{"130000", "130001", "150005"} {
		f.exec(migrationSQL(t, id))
	}
	ddl, err := holdings.SchemaSQL(publicTestHolders)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(ddl)
	if f.holdings, err = holdings.NewRepository(pg, publicTestHolders); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,$1,'indexer-0',1,clock_timestamp()+interval '1 hour')`, publicTestStream)
	f.exec(`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,0,0)`, publicTestStream)
	return f
}

func (f *publicFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pg.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("%v: %.200s", err, sql)
	}
}

func (f *publicFixture) block(height int64, hash string) {
	f.t.Helper()
	f.exec(`INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete) VALUES(125,$1,$2,$3,0,true,true) ON CONFLICT DO NOTHING`, publicTestStream, height, hash)
}

func (f *publicFixture) advance(height int64, hash string) {
	f.t.Helper()
	f.block(height, hash)
	f.exec(`UPDATE indexer.indexer_ingest_watermarks SET raw_complete_height=$2,typed_complete_height=$2 WHERE chain_id=125 AND stream_id=$1`, publicTestStream, height)
}

func (f *publicFixture) rawLog(tx realTransaction, l realLog) {
	f.t.Helper()
	topics, _ := json.Marshal(l.Topics)
	data, err := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,$2,$3,$4,$5,$6,$7,$8)`, tx.Hash, l.LogIndex, tx.BlockNumber, tx.TransactionIndex, tx.BlockHash, l.Address, string(topics), data)
}

func (f *publicFixture) provenance() accounting.Provenance {
	return accounting.Provenance{DeploymentID: publicTestDeployment, ABIRevision: "sidiora-v1", EmitterCodeEpoch: "event-emitter-1", PoolCodeEpoch: "pool-1", ReceiptSHA256: strings.Repeat("a", 64), HeaderSHA256: strings.Repeat("b", 64)}
}

// creation records the canonical creation fact at the factory's launch log.
func (f *publicFixture) creation(tx realTransaction, logIndex int64) {
	f.t.Helper()
	timestamp := f.real.Blocks[strconv.FormatInt(tx.BlockNumber, 10)].Timestamp
	position := accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(tx.BlockNumber, 10), TransactionIndex: strconv.FormatInt(tx.TransactionIndex, 10), LogIndex: strconv.FormatInt(logIndex, 10), TransactionHash: tx.Hash, BlockHash: tx.BlockHash}
	payload, _ := json.Marshal(map[string]any{"token": f.real.Token, "pool": f.real.Pool, "creator": tx.From, "factory": f.real.Factory, "position": position, "blockTimestampSeconds": strconv.FormatInt(timestamp, 10), "metadata": map[string]any{"name": "fixture"}})
	provenance, _ := json.Marshal(f.provenance())
	f.exec(`INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10,$11,$12,$13)`, f.real.Token, f.real.Pool, tx.From, f.real.Factory, "0x0000000000000000000000000000000000000000", tx.BlockNumber, tx.BlockHash, tx.Hash, tx.TransactionIndex, logIndex, timestamp, string(provenance), string(payload))
}

// fill derives the canonical fill of an EventEmitter Swap log exactly as the
// indexer's qualified receipt adapter does, records it as an indexer fact and
// commits it to the stats projection through the production accounting path.
func (f *publicFixture) fill(tx realTransaction, l realLog, timestamp int64) accounting.FillV1 {
	f.t.Helper()
	data, err := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
	if err != nil || len(data) != 7*32 || l.Topics[0] != swapEventTopic {
		f.t.Fatal("swap log", err)
	}
	word := func(i int) string { return new(big.Int).SetBytes(data[i*32 : (i+1)*32]).String() }
	fill := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(tx.BlockNumber, 10), TransactionIndex: strconv.FormatInt(tx.TransactionIndex, 10), LogIndex: strconv.FormatInt(l.LogIndex, 10), TransactionHash: tx.Hash, BlockHash: tx.BlockHash},
		ActualEmitter: l.Address, PoolAddress: f.real.Pool, Token: accounting.Asset{Address: f.real.Token, Decimals: f.real.TokenDecimals}, Settlement: accounting.Asset{Address: f.real.USDL, Decimals: 6},
		PaidInputRaw: word(1), FeeRaw: word(3), InputDerivationQuality: "receipt_incomplete",
		Attribution: accounting.Attribution{TransactionSender: tx.From, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Execution:   accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Provenance:  f.provenance(), BlockTimestampSeconds: strconv.FormatInt(timestamp, 10)}
	if word(0) == "1" {
		fill.Side, fill.TokenAmountRaw, fill.USDLAmountRaw, fill.FeeDisposition = "buy", word(2), word(1), "distributed_usdl"
	} else {
		fill.Side, fill.TokenAmountRaw, fill.USDLAmountRaw, fill.FeeDisposition = "sell", word(1), word(2), "retained_token_reserve"
	}
	fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, publicTestDeployment)
	if err = accounting.ValidateFillV1(fill); err != nil {
		f.t.Fatal(err)
	}
	digest, err := accounting.FillDigest(fill)
	if err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(fill)
	f.exec(`INSERT INTO indexer.indexer_accounting_fills(source_fill_id,source_fact_digest,chain_id,transaction_hash,log_index,pool_address,payload) VALUES($1,$2,125,$3,$4,$5,$6)`, fill.SourceFillID, string(digest), tx.Hash, l.LogIndex, f.real.Pool, string(payload))
	in, err := qualifiedInput(fill, "stats-consumer-v1", publicTestVersion)
	if err != nil {
		f.t.Fatal(err)
	}
	dbtx, err := f.pg.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer dbtx.Rollback(f.ctx)
	result, err := f.repo.ApplyQualifiedTx(f.ctx, dbtx, in)
	if err != nil || !result.Accepted {
		f.t.Fatal(result, err)
	}
	f.sequence++
	position, _ := json.Marshal(accounting.PartitionPosition{ProjectionVersion: publicTestVersion, PartitionKind: "market", PartitionKey: "125:" + f.real.Pool, Sequence: strconv.FormatInt(f.sequence, 10), SourceEventID: fill.SourceFillID, EffectIdentity: fill.SourceFillID, PayloadDigest: digest, SourceOrder: fill.Position})
	start, _ := Minute(timestamp)
	if _, err = dbtx.Exec(f.ctx, `INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, "publication-"+fill.SourceFillID, fill.SourceFillID, string(digest), publicTestVersion, f.real.Pool, start, string(position), string(payload), f.sequence, result.Revision); err != nil {
		f.t.Fatal(err)
	}
	if err = dbtx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	return fill
}

// transfer records a token Transfer log as a canonical transfer fact and
// applies it to the Transfer-authoritative holdings projection.
func (f *publicFixture) transfer(tx realTransaction, l realLog) {
	f.t.Helper()
	from, to := "0x"+l.Topics[1][26:], "0x"+l.Topics[2][26:]
	amount, ok := new(big.Int).SetString(strings.TrimPrefix(l.Data, "0x"), 16)
	if !ok {
		f.t.Fatal("transfer amount")
	}
	payload, _ := json.Marshal(map[string]any{"token": l.Address, "from": from, "to": to, "amountRaw": amount.String()})
	provenance, _ := json.Marshal(f.provenance())
	f.exec(`INSERT INTO indexer.indexer_token_transfers(chain_id,transaction_hash,log_index,token,from_address,to_address,amount_raw,height,transaction_index,block_hash,provenance,payload) VALUES(125,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tx.Hash, l.LogIndex, l.Address, from, to, amount.String(), tx.BlockNumber, tx.TransactionIndex, tx.BlockHash, string(provenance), string(payload))
	if _, err := f.holdings.Apply(f.ctx, holdings.Transfer{ChainID: "125", Token: l.Address, TransactionHash: tx.Hash, BlockHash: tx.BlockHash, Height: strconv.FormatInt(tx.BlockNumber, 10), TransactionIndex: strconv.FormatInt(tx.TransactionIndex, 10), LogIndex: strconv.FormatInt(l.LogIndex, 10), From: from, To: to, Amount: amount.String()}); err != nil {
		f.t.Fatal(err)
	}
}

// ingestReal loads the three real receipts the way the production pipeline
// commits them: raw logs, completeness, creation, fills and transfers.
func (f *publicFixture) ingestReal() map[string]accounting.FillV1 {
	f.t.Helper()
	if err := f.holdings.Discover(f.ctx, f.real.Token, f.real.TokenDecimals, strconv.FormatInt(f.real.Transactions[0].BlockNumber, 10), true); err != nil {
		f.t.Fatal(err)
	}
	fills := map[string]accounting.FillV1{}
	for i, tx := range f.real.Transactions {
		f.block(tx.BlockNumber, tx.BlockHash)
		for _, l := range tx.Logs {
			f.rawLog(tx, l)
		}
		if i == 0 {
			f.creation(tx, 14)
		}
		for _, l := range tx.Logs {
			if l.Address == f.real.Token && l.Topics[0] == transferTopic {
				f.transfer(tx, l)
			}
			if l.Address == f.real.EventEmitter && l.Topics[0] == swapEventTopic && l.Topics[1] == poolTopic(f.real.Pool) {
				fill := f.fill(tx, l, f.real.Blocks[strconv.FormatInt(tx.BlockNumber, 10)].Timestamp)
				fills[fill.Side] = fill
			}
		}
	}
	return fills
}

func (f *publicFixture) readService() *ReadService {
	f.t.Helper()
	reads, err := NewReadService(f.repo, ReadConfig{SourceSchema: "indexer", SourceStream: publicTestStream, ProjectionVersion: publicTestVersion, CursorKey: []byte(strings.Repeat("k", 32)), CursorTTL: 5 * time.Minute})
	if err != nil {
		f.t.Fatal(err)
	}
	return reads
}

func (f *publicFixture) projector() *PublicStatsProjector {
	f.t.Helper()
	p, err := NewPublicStatsProjector(PublicStatsConfig{Reads: f.readService(), HolderSchema: publicTestHolders, StreamSchema: "api_stream", Batch: 50})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

type storedStatsFrame struct {
	sequence int64
	frame    publicStatsFrame
	wire     []byte
	outbox   int
}

func (f *publicFixture) frame(pool string) storedStatsFrame {
	f.t.Helper()
	var out storedStatsFrame
	if err := f.pg.QueryRow(f.ctx, "SELECT sequence,frame,(SELECT count(*) FROM api_stream.stream_delivery_outbox o WHERE o.partition_id=p.partition_id) FROM api_stream.stream_projection p WHERE topic=$1", "market.stats."+pool).Scan(&out.sequence, &out.wire, &out.outbox); err != nil {
		f.t.Fatal(err)
	}
	if err := ValidatePublicStatsFrame(out.wire); err != nil {
		f.t.Fatal(err, string(out.wire))
	}
	if err := json.Unmarshal(out.wire, &out.frame); err != nil {
		f.t.Fatal(err)
	}
	// core-api's PublicProjections admission: StatsEvent schema, topic and
	// sequence binding, non-null watermark and revision.
	var generic struct {
		Topic, Sequence, Revision string
		Watermark                 json.RawMessage
	}
	if interfaces.ValidateJSON("StatsEvent", out.wire) != nil || json.Unmarshal(out.wire, &generic) != nil || generic.Topic != "market.stats."+pool || generic.Sequence != strconv.FormatInt(out.sequence, 10) || string(generic.Watermark) == "null" || generic.Revision == "" {
		f.t.Fatal("core-api admission", string(out.wire))
	}
	return out
}

// assertPrice checks the committed market.price frame against the fill it
// prices: gross execution in human units, revision = market commit sequence.
func (f *publicFixture) assertPrice(fill accounting.FillV1, revision string, sequence int64) {
	f.t.Helper()
	var wire []byte
	var stored int64
	if err := f.pg.QueryRow(f.ctx, "SELECT sequence,frame FROM api_stream.stream_projection WHERE topic=$1", "market.price."+f.real.Pool).Scan(&stored, &wire); err != nil {
		f.t.Fatal(err)
	}
	if err := ValidatePublicPriceFrame(wire); err != nil {
		f.t.Fatal(err, string(wire))
	}
	var frame publicPriceFrame
	if err := json.Unmarshal(wire, &frame); err != nil {
		f.t.Fatal(err)
	}
	usdl, _ := new(big.Int).SetString(fill.USDLAmountRaw, 10)
	token, _ := new(big.Int).SetString(fill.TokenAmountRaw, 10)
	numerator, _ := new(big.Int).SetString(frame.Payload.Numerator, 10)
	denominator, _ := new(big.Int).SetString(frame.Payload.Denominator, 10)
	// numerator/denominator == (usdl/1e6)/(token/1e6) for the 6-decimal token.
	if new(big.Int).Mul(numerator, token).Cmp(new(big.Int).Mul(denominator, usdl)) != 0 || stored != sequence || frame.Revision != revision || frame.Watermark.Height != fill.Position.Height || frame.Payload.Source.Position != Position(fill.Position) || frame.Payload.BaseAsset.Address != f.real.Token || frame.Payload.QuoteAsset.Address != f.real.USDL {
		f.t.Fatal(string(wire))
	}
}

func encodeWords(values ...*big.Int) string {
	var b strings.Builder
	b.WriteString("0x")
	for _, v := range values {
		b.WriteString(fmt.Sprintf("%064x", v))
	}
	return b.String()
}

func TestPublicMarketStatsFromRealPaxeerPool(t *testing.T) {
	f := newPublicFixture(t)
	projector := f.projector()
	now := time.Unix(f.real.Blocks["33963749"].Timestamp+60, 0)

	// A coherent watermark without any canonical market event publishes nothing.
	f.advance(1, "0x"+strings.Repeat("0", 63)+"1")
	result, err := projector.RefreshOnce(f.ctx, now)
	if err != nil || result.Appended != 0 || result.Anchor != nil {
		t.Fatal(result, err)
	}

	fills := f.ingestReal()
	f.advance(33963761, f.real.Blocks["33963761"].Hash)
	result, err = projector.RefreshOnce(f.ctx, now)
	if err != nil || result.Pools != 1 || result.Appended != 1 || result.Prices != 1 {
		t.Fatal(result, err)
	}
	stored := f.frame(f.real.Pool)
	f.assertPrice(fills["sell"], "2", 1)
	doc := stored.frame.Payload
	sell := fills["sell"]
	buy := fills["buy"]
	reserves := f.real.ReservesAt["33963749"]
	if doc.RealUsdlBalanceRaw != reserves.RealUSDL || doc.RealUsdlBalanceRaw != "49721088" {
		t.Fatalf("booked real USDL %s, on-chain getReserves %s", doc.RealUsdlBalanceRaw, reserves.RealUSDL)
	}
	volume := new(big.Int)
	volume.SetString(buy.USDLAmountRaw, 10)
	sold, _ := new(big.Int).SetString(sell.USDLAmountRaw, 10)
	volume.Add(volume, sold)
	if doc.Volume24hRaw != volume.String() || doc.Buys24h != "1" || doc.Sells24h != "1" {
		t.Fatal(doc)
	}
	if doc.HolderCount == nil || *doc.HolderCount != "2" {
		t.Fatal("holders", doc.HolderCount)
	}
	if doc.SourceUpperPosition != Position(sell.Position) || stored.frame.Watermark.Height != "33963749" || stored.frame.Watermark.BlockHash != f.real.Blocks["33963749"].Hash {
		t.Fatal(doc.SourceUpperPosition, stored.frame.Watermark)
	}
	end := f.real.Blocks["33963749"].Timestamp + 1
	if doc.WindowEnd != publicTime(end) || doc.WindowStart != publicTime(end-86400) || stored.frame.OccurredAt != publicTime(end-1) {
		t.Fatal(doc.WindowStart, doc.WindowEnd, stored.frame.OccurredAt)
	}
	// The pool was created inside the window and its creation price is not a
	// canonical event: no fabricated change.
	if doc.PriceChangeBps24h != nil || doc.ChangeBaselineQuality != "insufficient_history" {
		t.Fatal(doc.PriceChangeBps24h, doc.ChangeBaselineQuality)
	}
	if stored.sequence != 1 || stored.outbox != 1 || stored.frame.Revision != "1" || stored.frame.EventID != "stats:"+f.real.Pool+":1" {
		t.Fatal(stored.sequence, stored.outbox, stored.frame.Revision, stored.frame.EventID)
	}
	// Interface 2.2.0 fields, hand-computed from the real receipts: the
	// deployer bought 9606813905120 and sold 4803406952560 tokens, so it holds
	// the whole circulating supply 1e15-995196593047440=4803406952560 while the
	// pool holds the bonding-curve reserve. The sell's Swap price is the pool's
	// getPrice()=floor((virtual+real)*1e18/tokenReserve) at block 33963749.
	effective, _ := new(big.Int).SetString(reserves.VirtualUSDL, 10)
	booked, _ := new(big.Int).SetString(reserves.RealUSDL, 10)
	tokenReserve, _ := new(big.Int).SetString(reserves.TokenReserve, 10)
	effective.Add(effective, booked)
	onChainPrice := new(big.Int).Div(new(big.Int).Mul(effective, big.NewInt(1e18)), tokenReserve)
	if onChainPrice.String() != "10098226981692" || doc.SpotPriceWad == nil || *doc.SpotPriceWad != onChainPrice.String() {
		t.Fatal("spot wad", doc.SpotPriceWad, onChainPrice)
	}
	// One whole token (1e6 raw) costs floor(10049721088*1e6/995196593047440)=10 USDL raw.
	if doc.SpotPriceUsdlRaw == nil || *doc.SpotPriceUsdlRaw != "10" || new(big.Int).Div(new(big.Int).Mul(effective, big.NewInt(1e6)), tokenReserve).String() != "10" {
		t.Fatal("spot usdl", doc.SpotPriceUsdlRaw)
	}
	if doc.UniqueTraders24h != "1" || doc.TxCount24h != "2" {
		t.Fatal(doc.UniqueTraders24h, doc.TxCount24h)
	}
	if doc.TotalSupplyRaw == nil || *doc.TotalSupplyRaw != "1000000000000000" || doc.CirculatingSupplyRaw == nil || *doc.CirculatingSupplyRaw != "4803406952560" || doc.CirculationModel == nil || *doc.CirculationModel != "supply_minus_pool_reserve_v1" {
		t.Fatal(doc.TotalSupplyRaw, doc.CirculatingSupplyRaw, doc.CirculationModel)
	}
	if doc.Top10HolderShareBps == nil || *doc.Top10HolderShareBps != 10000 || doc.CreatorHoldingShareBps == nil || *doc.CreatorHoldingShareBps != 10000 {
		t.Fatal(doc.Top10HolderShareBps, doc.CreatorHoldingShareBps)
	}
	// floor(1e15*10098226981692/1e18)=10098226981; floor(4803406952560*10098226981692/1e18)=48505893.
	if doc.SupplyValuationQuality != "verified" || doc.TotalSupplyValuationUsdlRaw == nil || *doc.TotalSupplyValuationUsdlRaw != "10098226981" || doc.CirculatingMarketCapUsdlRaw == nil || *doc.CirculatingMarketCapUsdlRaw != "48505893" || doc.ValuationChangeUsdlRaw != nil || doc.ValuationMethod != nil || doc.IndicativeTvlUsdlRaw != nil {
		t.Fatal(doc.SupplyValuationQuality, doc.TotalSupplyValuationUsdlRaw, doc.CirculatingMarketCapUsdlRaw)
	}
	// Every shorter window also starts after creation with no earlier price event.
	for _, w := range []struct {
		change  *string
		quality string
	}{{doc.PriceChangeBps5m, doc.ChangeBaselineQuality5m}, {doc.PriceChangeBps1h, doc.ChangeBaselineQuality1h}, {doc.PriceChangeBps6h, doc.ChangeBaselineQuality6h}} {
		if w.change != nil || w.quality != "insufficient_history" {
			t.Fatal(w.change, w.quality)
		}
	}
	firstWire := stored.wire

	// Unchanged sources append nothing.
	if result, err = projector.RefreshOnce(f.ctx, now.Add(time.Minute)); err != nil || result.Appended != 0 || result.Prices != 0 {
		t.Fatal(result, err)
	}

	// A wallet-to-wallet transfer after the anchor moves holdings ahead of the
	// anchor; the count at the anchor is reconstructed by reversal, so the
	// document is unchanged.
	deployer := f.real.Transactions[0].From
	later := realTransaction{Hash: "0x" + strings.Repeat("e", 64), From: deployer, BlockNumber: 33963800, BlockHash: "0x" + strings.Repeat("d", 64)}
	recipient := "0x" + strings.Repeat("7", 40)
	transfer := realLog{LogIndex: 0, Address: f.real.Token, Topics: []string{transferTopic, "0x000000000000000000000000" + deployer[2:], "0x000000000000000000000000" + recipient[2:]}, Data: encodeWords(big.NewInt(1000))}
	f.rawLog(later, transfer)
	f.transfer(later, transfer)
	f.advance(33963800, later.BlockHash)
	if result, err = projector.RefreshOnce(f.ctx, now.Add(2*time.Minute)); err != nil || result.Appended != 0 {
		t.Fatal(result, err)
	}
	if again := f.frame(f.real.Pool); string(again.wire) != string(firstWire) {
		t.Fatal("restart/replay changed the anchored document", string(again.wire))
	}

	// A buy 25 hours later rolls the window: the real trades fall out, the sell
	// becomes the 24h price baseline and the booked balance moves by paid-fee.
	ts := f.real.Blocks["33963749"].Timestamp + 25*3600
	nextHeight := int64(33963749 + 500000)
	next := realTransaction{Hash: "0x" + strings.Repeat("f", 64), From: deployer, BlockNumber: nextHeight, BlockHash: "0x" + strings.Repeat("c", 64)}
	sellLog := f.real.Transactions[2].Logs[2]
	sellData, _ := hex.DecodeString(sellLog.Data[2:])
	previousPrice := new(big.Int).SetBytes(sellData[4*32 : 5*32])
	nextPrice := new(big.Int).Div(new(big.Int).Mul(previousPrice, big.NewInt(10123)), big.NewInt(10000))
	swap := realLog{LogIndex: 1, Address: f.real.EventEmitter, Topics: []string{swapEventTopic, poolTopic(f.real.Pool), "0x000000000000000000000000" + deployer[2:]}, Data: encodeWords(big.NewInt(1), big.NewInt(1000000), big.NewInt(95000000), big.NewInt(30000), nextPrice, big.NewInt(ts), big.NewInt(nextHeight))}
	tokensOut := realLog{LogIndex: 0, Address: f.real.Token, Topics: []string{transferTopic, poolTopic(f.real.Pool), "0x000000000000000000000000" + deployer[2:]}, Data: encodeWords(big.NewInt(95000000))}
	f.rawLog(next, tokensOut)
	f.rawLog(next, swap)
	f.transfer(next, tokensOut)
	f.block(nextHeight, next.BlockHash)
	nextFill := f.fill(next, swap, ts)
	f.advance(nextHeight+10, "0x"+strings.Repeat("b", 64))
	result, err = projector.RefreshOnce(f.ctx, time.Unix(ts+30, 0))
	if err != nil || result.Appended != 1 || result.Prices != 1 {
		t.Fatal(result, err)
	}
	stored = f.frame(f.real.Pool)
	doc = stored.frame.Payload
	if doc.Volume24hRaw != "1000000" || doc.Buys24h != "1" || doc.Sells24h != "0" || doc.RealUsdlBalanceRaw != strconv.Itoa(49721088+1000000-30000) {
		t.Fatal(doc)
	}
	expectedChange := new(big.Int).Sub(nextPrice, previousPrice)
	expectedChange.Mul(expectedChange, big.NewInt(10000)).Quo(expectedChange, previousPrice)
	if expectedChange.Sign() <= 0 || doc.PriceChangeBps24h == nil || *doc.PriceChangeBps24h != expectedChange.String() || doc.ChangeBaselineQuality != "complete" {
		t.Fatal(doc.PriceChangeBps24h, doc.ChangeBaselineQuality)
	}
	if doc.HolderCount == nil || *doc.HolderCount != "3" {
		t.Fatal("holders", doc.HolderCount)
	}
	if stored.sequence != 2 || stored.outbox != 2 || stored.frame.Revision != "2" || stored.frame.Watermark.Height != strconv.FormatInt(nextHeight, 10) {
		t.Fatal(stored.sequence, stored.outbox, stored.frame.Revision, stored.frame.Watermark)
	}
	f.assertPrice(nextFill, "3", 2)
	// Every window's baseline is the sell, 25h before: all four changes agree.
	for _, w := range []struct {
		change  *string
		quality string
	}{{doc.PriceChangeBps5m, doc.ChangeBaselineQuality5m}, {doc.PriceChangeBps1h, doc.ChangeBaselineQuality1h}, {doc.PriceChangeBps6h, doc.ChangeBaselineQuality6h}} {
		if w.change == nil || *w.change != expectedChange.String() || w.quality != "complete" {
			t.Fatal(w.change, w.quality)
		}
	}
	// The pool paid 95000000 tokens to the deployer, who had sent 1000 to the
	// recipient: circulating 4803406952560+95000000, creator holds all but 1000.
	circulating := big.NewInt(4803406952560 + 95000000)
	if doc.TxCount24h != "1" || doc.UniqueTraders24h != "1" || *doc.TotalSupplyRaw != "1000000000000000" || *doc.CirculatingSupplyRaw != circulating.String() || *doc.Top10HolderShareBps != 10000 || *doc.CreatorHoldingShareBps != 9999 {
		t.Fatal(doc.TxCount24h, *doc.CirculatingSupplyRaw, *doc.Top10HolderShareBps, *doc.CreatorHoldingShareBps)
	}
	wadOf := func(amount *big.Int, price *big.Int) string {
		return new(big.Int).Div(new(big.Int).Mul(amount, price), big.NewInt(1e18)).String()
	}
	if *doc.SpotPriceWad != nextPrice.String() || *doc.SpotPriceUsdlRaw != new(big.Int).Div(new(big.Int).Mul(nextPrice, big.NewInt(1e6)), big.NewInt(1e18)).String() || *doc.TotalSupplyValuationUsdlRaw != wadOf(big.NewInt(1e15), nextPrice) || *doc.CirculatingMarketCapUsdlRaw != wadOf(circulating, nextPrice) || doc.SupplyValuationQuality != "verified" {
		t.Fatal(*doc.SpotPriceWad, *doc.SpotPriceUsdlRaw, *doc.TotalSupplyValuationUsdlRaw, *doc.CirculatingMarketCapUsdlRaw)
	}

	// A third buy two hours later separates the windows: 5m and 1h compare
	// with the previous buy, 6h and 24h still with the sell 27h earlier.
	ts3 := ts + 2*3600
	height3 := nextHeight + 1000
	third := realTransaction{Hash: "0x" + strings.Repeat("a", 63) + "1", From: deployer, BlockNumber: height3, BlockHash: "0x" + strings.Repeat("9", 64)}
	price3 := new(big.Int).Div(new(big.Int).Mul(nextPrice, big.NewInt(10050)), big.NewInt(10000))
	swap3 := realLog{LogIndex: 1, Address: f.real.EventEmitter, Topics: []string{swapEventTopic, poolTopic(f.real.Pool), "0x000000000000000000000000" + deployer[2:]}, Data: encodeWords(big.NewInt(1), big.NewInt(2000000), big.NewInt(190000000), big.NewInt(60000), price3, big.NewInt(ts3), big.NewInt(height3))}
	tokensOut3 := realLog{LogIndex: 0, Address: f.real.Token, Topics: []string{transferTopic, poolTopic(f.real.Pool), "0x000000000000000000000000" + deployer[2:]}, Data: encodeWords(big.NewInt(190000000))}
	f.rawLog(third, tokensOut3)
	f.rawLog(third, swap3)
	f.transfer(third, tokensOut3)
	f.block(height3, third.BlockHash)
	thirdFill := f.fill(third, swap3, ts3)
	f.advance(height3+10, "0x"+strings.Repeat("8", 64))
	if result, err = projector.RefreshOnce(f.ctx, time.Unix(ts3+30, 0)); err != nil || result.Appended != 1 || result.Prices != 1 {
		t.Fatal(result, err)
	}
	stored = f.frame(f.real.Pool)
	doc = stored.frame.Payload
	bps := func(base, current *big.Int) string {
		v := new(big.Int).Sub(current, base)
		return v.Mul(v, big.NewInt(10000)).Quo(v, base).String()
	}
	short, long := bps(nextPrice, price3), bps(previousPrice, price3)
	if short == long || *doc.PriceChangeBps5m != short || *doc.PriceChangeBps1h != short || *doc.PriceChangeBps6h != long || *doc.PriceChangeBps24h != long || doc.ChangeBaselineQuality5m != "complete" || doc.ChangeBaselineQuality1h != "complete" || doc.ChangeBaselineQuality6h != "complete" || doc.ChangeBaselineQuality != "complete" {
		t.Fatal(short, long, *doc.PriceChangeBps5m, *doc.PriceChangeBps1h, *doc.PriceChangeBps6h, *doc.PriceChangeBps24h)
	}
	circulating.Add(circulating, big.NewInt(190000000))
	if doc.Volume24hRaw != "3000000" || doc.Buys24h != "2" || doc.TxCount24h != "2" || doc.UniqueTraders24h != "1" || *doc.SpotPriceWad != price3.String() || *doc.CirculatingSupplyRaw != circulating.String() || *doc.CirculatingMarketCapUsdlRaw != wadOf(circulating, price3) || *doc.CreatorHoldingShareBps != 9999 {
		t.Fatal(doc)
	}
	if stored.sequence != 3 || stored.frame.Revision != "3" {
		t.Fatal(stored.sequence, stored.frame.Revision)
	}
	f.assertPrice(thirdFill, "4", 3)

	// A frozen pool fails closed: the projector refuses and appends nothing.
	f.exec(`INSERT INTO stats.accounting_pool_fences(pool,frozen) VALUES($1,true) ON CONFLICT(pool) DO UPDATE SET frozen=true`, f.real.Pool)
	if _, err = projector.RefreshOnce(f.ctx, time.Unix(ts3+60, 0)); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	f.exec("UPDATE stats.accounting_pool_fences SET frozen=false WHERE pool=$1", f.real.Pool)
	// An expired ingest lease is an unavailable source, never stale success.
	f.exec("UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()-interval '1 second'")
	if _, err = projector.RefreshOnce(f.ctx, time.Unix(ts3+60, 0)); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if final := f.frame(f.real.Pool); final.sequence != 3 || final.outbox != 3 {
		t.Fatal(final.sequence, final.outbox)
	}
}

func TestPublicMarketStatsRejectsForgedSwapEvidence(t *testing.T) {
	f := newPublicFixture(t)
	fills := f.ingestReal()
	f.advance(33963761, f.real.Blocks["33963761"].Hash)
	// Tamper with the raw Swap log behind the committed sell: its amounts no
	// longer match the committed fill, so the price evidence is rejected.
	sell := fills["sell"]
	f.exec("UPDATE indexer.indexer_raw_logs SET data=overlay(data placing '\\x01'::bytea from 64 for 1) WHERE transaction_hash=$1 AND log_index=$2", sell.Position.TransactionHash, 2)
	if _, err := f.projector().RefreshOnce(f.ctx, time.Unix(f.real.Blocks["33963749"].Timestamp+60, 0)); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	var count int
	if err := f.pg.QueryRow(f.ctx, "SELECT count(*) FROM api_stream.stream_delivery_outbox").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestValidatePublicStatsFrameBindings(t *testing.T) {
	pool := "0x" + strings.Repeat("1", 40)
	position := Position{ChainID: "125", Height: "10", TransactionIndex: "0", LogIndex: "1", TransactionHash: "0x" + strings.Repeat("2", 64), BlockHash: "0x" + strings.Repeat("3", 64)}
	watermark := PublicWatermark{ChainID: "125", Height: "10", BlockHash: position.BlockHash, ProjectionRevision: "1", ObservedAt: publicTime(100), Integrity: "healthy"}
	doc := PublicPoolStats{PoolAddress: pool, Volume24hRaw: "0", Buys24h: "0", Sells24h: "0", RealUsdlBalanceRaw: "0", WindowStart: publicTime(1), WindowEnd: publicTime(86401), SourceUpperPosition: position, Watermark: watermark, SupplyValuationQuality: "unavailable", ChangeBaselineQuality: "unavailable"}
	frame := publicStatsFrame{EventID: "stats:" + pool + ":1", Topic: "market.stats." + pool, Sequence: "1", Revision: "1", OccurredAt: publicTime(86400), Watermark: watermark, Payload: doc}
	wire, _ := json.Marshal(frame)
	if err := ValidatePublicStatsFrame(wire); err != nil {
		t.Fatal(err, string(wire))
	}
	for name, mutate := range map[string]func(*publicStatsFrame){
		"window":    func(f *publicStatsFrame) { f.Payload.WindowStart = publicTime(2) },
		"revision":  func(f *publicStatsFrame) { f.Revision = "2" },
		"upper":     func(f *publicStatsFrame) { f.Payload.SourceUpperPosition.Height = "9" },
		"watermark": func(f *publicStatsFrame) { f.Payload.Watermark.ObservedAt = publicTime(101) },
		"topic":     func(f *publicStatsFrame) { f.Topic = "market.stats.0x" + strings.Repeat("4", 40) },
	} {
		bad := frame
		mutate(&bad)
		raw, _ := json.Marshal(bad)
		if ValidatePublicStatsFrame(raw) == nil {
			t.Fatal(name)
		}
	}
}

// The committed append functions re-derive the bindings themselves: a frame
// whose aggregates or price differ from the committed fills is refused by the
// database even when it is schema-valid.
func TestCommittedMarketAppendRejectsForgedFrames(t *testing.T) {
	f := newPublicFixture(t)
	fills := f.ingestReal()
	f.advance(33963761, f.real.Blocks["33963761"].Hash)
	if result, err := f.projector().RefreshOnce(f.ctx, time.Unix(f.real.Blocks["33963749"].Timestamp+60, 0)); err != nil || result.Appended != 1 || result.Prices != 1 {
		t.Fatal(result, err)
	}
	stored := f.frame(f.real.Pool)
	writer := delivery.StreamWriter{Schema: "api_stream"}
	attempt := func(update delivery.StreamUpdate) error {
		tx, err := f.pg.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		return writer.AppendTx(f.ctx, tx, update)
	}
	doc := stored.frame.Payload
	doc.Watermark.ProjectionRevision = "2"
	doc.Volume24hRaw = "147278913"
	watermark, _ := json.Marshal(doc.Watermark)
	payload, _ := json.Marshal(doc)
	if err := attempt(delivery.StreamUpdate{EventID: "stats:" + f.real.Pool + ":2", Topic: "market.stats." + f.real.Pool, Revision: "2", OccurredAt: stored.frame.OccurredAt, Watermark: watermark, Payload: payload, Source: delivery.StreamSourceProof{Kind: "stats_commit", Pool: f.real.Pool, ProjectionVersion: publicTestVersion}}); err == nil || !strings.Contains(err.Error(), "stats window aggregate differs") {
		t.Fatal(err)
	}
	doc.Volume24hRaw = stored.frame.Payload.Volume24hRaw
	doc.RealUsdlBalanceRaw = "49721089"
	payload, _ = json.Marshal(doc)
	if err := attempt(delivery.StreamUpdate{EventID: "stats:" + f.real.Pool + ":2", Topic: "market.stats." + f.real.Pool, Revision: "2", OccurredAt: stored.frame.OccurredAt, Watermark: watermark, Payload: payload, Source: delivery.StreamSourceProof{Kind: "stats_commit", Pool: f.real.Pool, ProjectionVersion: publicTestVersion}}); err == nil || !strings.Contains(err.Error(), "booked real USDL") {
		t.Fatal(err)
	}
	sell := fills["sell"]
	price, err := PublicPriceOf(sell)
	if err != nil {
		t.Fatal(err)
	}
	price.Numerator += "0"
	digest, _ := accounting.FillDigest(sell)
	priceWatermark, _ := json.Marshal(PublicWatermark{ChainID: "125", Height: sell.Position.Height, BlockHash: sell.Position.BlockHash, ProjectionRevision: "2", ObservedAt: publicTime(1), Integrity: "healthy"})
	pricePayload, _ := json.Marshal(price)
	if err := attempt(delivery.StreamUpdate{EventID: "price:" + price.Source.SourceEventID, Topic: "market.price." + f.real.Pool, Revision: "2", OccurredAt: price.Source.ObservedAt, Watermark: priceWatermark, Payload: pricePayload, Source: delivery.StreamSourceProof{Kind: "price_commit", Pool: f.real.Pool, ProjectionVersion: publicTestVersion, Sequence: "2", SourceFillID: sell.SourceFillID, SourceFactDigest: string(digest)}}); err == nil || !strings.Contains(err.Error(), "price differs from committed gross execution") {
		t.Fatal(err)
	}
}

// A market.stats head committed before interface 2.2.0 carries none of the
// additive fields; the projector still admits it as the prior frame and
// supersedes it with a complete document at the next revision.
func TestPublicMarketStatsSupersedesPre220Frame(t *testing.T) {
	f := newPublicFixture(t)
	projector := f.projector()
	f.ingestReal()
	f.advance(33963761, f.real.Blocks["33963761"].Hash)
	now := time.Unix(f.real.Blocks["33963749"].Timestamp+60, 0)
	if result, err := projector.RefreshOnce(f.ctx, now); err != nil || result.Appended != 1 {
		t.Fatal(result, err)
	}
	complete := f.frame(f.real.Pool)
	f.exec(`UPDATE api_stream.stream_projection SET frame=convert_to(jsonb_set(convert_from(frame,'UTF8')::jsonb,'{payload}',(convert_from(frame,'UTF8')::jsonb->'payload')-ARRAY['priceChangeBps5m','priceChangeBps1h','priceChangeBps6h','changeBaselineQuality5m','changeBaselineQuality1h','changeBaselineQuality6h','spotPriceWad','spotPriceUsdlRaw','uniqueTraders24h','txCount24h','totalSupplyRaw','circulatingSupplyRaw','circulationModel','top10HolderShareBps','creatorHoldingShareBps'])::text,'UTF8') WHERE topic=$1`, "market.stats."+f.real.Pool)
	legacy := f.frame(f.real.Pool)
	if strings.Contains(string(legacy.wire), "spotPriceWad") || legacy.frame.Payload.TxCount24h != "" {
		t.Fatal(string(legacy.wire))
	}
	if result, err := projector.RefreshOnce(f.ctx, now.Add(time.Minute)); err != nil || result.Appended != 1 {
		t.Fatal(result, err)
	}
	upgraded := f.frame(f.real.Pool)
	doc := upgraded.frame.Payload
	if upgraded.frame.Revision != "2" || doc.TxCount24h != "2" || doc.SpotPriceWad == nil || *doc.SpotPriceWad != *complete.frame.Payload.SpotPriceWad || doc.Volume24hRaw != complete.frame.Payload.Volume24hRaw {
		t.Fatal(string(upgraded.wire))
	}
	if result, err := projector.RefreshOnce(f.ctx, now.Add(2*time.Minute)); err != nil || result.Appended != 0 {
		t.Fatal(result, err)
	}
}
