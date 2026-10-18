package projectionstats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
)

// Public market statistics (the market.stats.<pool> topic of the core-api
// stream projection). core-api serves GET /v2/pools/{pool}, GET
// /v2/pools/{pool}/stats, POST /v2/pools/stats:batch and /v2/snapshots from
// this topic and never computes statistics itself, so the stats worker is the
// only production producer.
//
// Every document is computed inside one REPEATABLE READ snapshot from committed
// canonical sources only:
//   - the canonical watermark (ReadService.coherentTx: complete typed height,
//     live ingest lease, no frozen pool, every indexer fill committed here);
//   - committed market fills (trade_facts joined with accounting_market_commits);
//   - the authorized EventEmitter's raw Swap / PoolStateUpdated logs (spot
//     price and syncReserves resets) from the indexer raw log table;
//   - canonical pool creations and Transfer-authoritative holdings.
//
// The document is anchored at the latest committed canonical market event
// (fill or pool creation) at or below the watermark: sourceUpperPosition is
// that event's chain position, the watermark carries its height and block hash,
// and the rolling 24h window is [anchorSecond+1-86400, anchorSecond+1).

const (
	publicStatsWindowSeconds = 86400
	publicStatsTopicPrefix   = "market.stats."
	publicStatsLockKey       = "stats:public-market-stats:v1"
	publicStatsMaxSyncEvents = 256
	publicStatsMaxReversals  = 1000
)

// EventEmitter topic0 values (keccak256 of the canonical event signatures).
const (
	swapEventTopic        = "0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc" // Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)
	poolStateUpdatedTopic = "0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd" // PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)
)

// PublicStatsConfig binds the projector to its canonical sources and sink.
type PublicStatsConfig struct {
	Reads        *ReadService
	HolderSchema string
	StreamSchema string
	Batch        int
}

// PublicStatsProjector appends market.stats.<pool> frames to the core-api
// stream projection whenever a pool's committed statistics change.
type PublicStatsProjector struct {
	reads   *ReadService
	holders string
	stream  string
	batch   int
}

func NewPublicStatsProjector(c PublicStatsConfig) (*PublicStatsProjector, error) {
	if c.Reads == nil || c.Reads.repository == nil || !schemaName.MatchString(c.HolderSchema) || !schemaName.MatchString(c.StreamSchema) || c.Batch < 1 || c.Batch > 100 {
		return nil, ErrInvalid
	}
	return &PublicStatsProjector{reads: c.Reads, holders: c.HolderSchema, stream: c.StreamSchema, batch: c.Batch}, nil
}

// PublicStatsResult reports one refresh pass.
type PublicStatsResult struct {
	Pools    int
	Appended int
	Prices   int
	Anchor   *Position
}

// RefreshOnce recomputes every canonical pool's public statistics and appends a
// frame for each pool whose document changed. Pools are processed in bounded
// pages, each page in its own snapshot transaction.
func (p *PublicStatsProjector) RefreshOnce(ctx context.Context, now time.Time) (PublicStatsResult, error) {
	var out PublicStatsResult
	if p == nil || ctx == nil || now.IsZero() {
		return out, ErrInvalid
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		page, err := p.refreshPage(ctx, after, now)
		if err != nil {
			return out, err
		}
		out.Pools += page.pools
		out.Appended += page.appended
		out.Prices += page.prices
		if page.anchor != nil {
			anchor := *page.anchor
			out.Anchor = &anchor
		}
		if !page.more {
			return out, nil
		}
		after = page.last
	}
}

type publicStatsPage struct {
	pools, appended, prices int
	more                    bool
	last                    string
	anchor                  *Position
}

type publicAnchor struct {
	Position
	timestamp int64
}

func (p *PublicStatsProjector) table(schema, name string) string {
	return pgx.Identifier{schema, name}.Sanitize()
}

func (p *PublicStatsProjector) refreshPage(ctx context.Context, after string, now time.Time) (publicStatsPage, error) {
	var page publicStatsPage
	tx, err := p.reads.repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", publicStatsLockKey); err != nil {
		return page, err
	}
	watermark, err := p.reads.coherentTx(ctx, tx, "", now.Unix())
	if err != nil {
		return page, err
	}
	anchor, found, err := p.anchorTx(ctx, tx, watermark)
	if err != nil || !found {
		return page, err
	}
	page.anchor = &anchor.Position
	var observedAt time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		return page, err
	}
	rows, err := tx.Query(ctx, "SELECT pool,token,lower(creator),block_timestamp FROM "+p.reads.source("indexer_canonical_creations_v1")+" WHERE height<=$1 AND pool>$2 ORDER BY pool LIMIT $3", anchor.Height, after, p.batch+1)
	if err != nil {
		return page, err
	}
	type creation struct {
		pool, token, creator string
		created              int64
	}
	pools := []creation{}
	for rows.Next() {
		var c creation
		if err = rows.Scan(&c.pool, &c.token, &c.creator, &c.created); err != nil {
			rows.Close()
			return page, err
		}
		if !address.MatchString(c.pool) || !address.MatchString(c.token) || !address.MatchString(c.creator) || c.created < 0 {
			rows.Close()
			return page, ErrIntegrity
		}
		pools = append(pools, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(pools) > p.batch {
		pools = pools[:p.batch]
		page.more = true
	}
	emitters, err := p.emittersTx(ctx, tx)
	if err != nil {
		return page, err
	}
	for _, c := range pools {
		doc, last, err := p.poolStatsTx(ctx, tx, anchor, emitters, c.pool, c.token, c.creator, c.created)
		if err != nil {
			return page, fmt.Errorf("stats public pool %s: %w", c.pool, err)
		}
		appended, err := p.appendTx(ctx, tx, anchor, doc, observedAt)
		if err != nil {
			return page, fmt.Errorf("stats public pool %s: %w", c.pool, err)
		}
		if appended {
			page.appended++
		}
		if last != nil {
			priced, err := p.appendPriceTx(ctx, tx, *last, observedAt)
			if err != nil {
				return page, fmt.Errorf("stats public price %s: %w", c.pool, err)
			}
			if priced {
				page.prices++
			}
		}
		page.pools++
		page.last = c.pool
	}
	if err = tx.Commit(ctx); err != nil {
		return page, err
	}
	return page, nil
}

// anchorTx selects the latest committed canonical market event (fill or pool
// creation) at or below the coherent watermark and proves its block complete.
func (p *PublicStatsProjector) anchorTx(ctx context.Context, tx pgx.Tx, w CanonicalWatermark) (publicAnchor, bool, error) {
	var a publicAnchor
	schema := p.reads.repository.schema
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT height::text,transaction_index::text,log_index::text,transaction_hash,block_hash,block_timestamp FROM (
 SELECT t.height,t.transaction_index,t.log_index,t.payload->'position'->>'transactionHash' AS transaction_hash,t.payload->'position'->>'blockHash' AS block_hash,t.block_timestamp
  FROM %[1]s.trade_facts t JOIN %[1]s.accounting_market_commits c ON c.projection_version=t.projection_version AND c.pool=t.pool AND c.fill->'position'->>'transactionHash'=t.payload->'position'->>'transactionHash' AND (c.fill->'position'->>'logIndex')::numeric=t.log_index
  WHERE t.projection_version=$1 AND t.height<=$2::numeric
 UNION ALL
 SELECT k.height::numeric,k.transaction_index::numeric,k.log_index::numeric,k.transaction_hash,k.block_hash,k.block_timestamp FROM %[2]s k WHERE k.height<=$2::bigint
) a ORDER BY height DESC,transaction_index DESC,log_index DESC LIMIT 1`, schema, p.reads.source("indexer_canonical_creations_v1")), p.reads.config.ProjectionVersion, w.Height).Scan(&a.Height, &a.TransactionIndex, &a.LogIndex, &a.TransactionHash, &a.BlockHash, &a.timestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	a.ChainID = "125"
	a.TransactionHash, a.BlockHash = strings.ToLower(a.TransactionHash), strings.ToLower(a.BlockHash)
	if a.Validate() != nil || a.timestamp < 0 {
		return a, false, ErrIntegrity
	}
	var complete bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+p.reads.source("indexer_block_completeness_v1")+" WHERE chain_id=125 AND stream_id=$1 AND height=$2::bigint AND block_hash=$3 AND fetch_complete AND required_decode_complete)", p.reads.config.SourceStream, a.Height, a.BlockHash).Scan(&complete); err != nil {
		return a, false, err
	}
	if !complete {
		return a, false, ErrUnavailable
	}
	return a, true, nil
}

// emittersTx returns the EventEmitter addresses that produced committed fills;
// only their raw logs are admitted as price / reserve evidence.
func (p *PublicStatsProjector) emittersTx(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT fill->>'actualEmitter' FROM "+p.reads.repository.schema+".accounting_market_commits WHERE projection_version=$1 ORDER BY 1", p.reads.config.ProjectionVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var emitter string
		if err = rows.Scan(&emitter); err != nil {
			return nil, err
		}
		if !address.MatchString(emitter) {
			return nil, ErrIntegrity
		}
		out = append(out, emitter)
	}
	return out, rows.Err()
}

// PublicPoolStats is the frozen IF_POOLSTATS document.
type PublicPoolStats struct {
	PoolAddress                 string          `json:"poolAddress"`
	Volume24hRaw                string          `json:"volume24hRaw"`
	Buys24h                     string          `json:"buys24h"`
	Sells24h                    string          `json:"sells24h"`
	HolderCount                 *string         `json:"holderCount"`
	RealUsdlBalanceRaw          string          `json:"realUsdlBalanceRaw"`
	IndicativeTvlUsdlRaw        *string         `json:"indicativeTvlUsdlRaw"`
	ValuationMethod             *string         `json:"valuationMethod"`
	WindowStart                 string          `json:"windowStart"`
	WindowEnd                   string          `json:"windowEnd"`
	SourceUpperPosition         Position        `json:"sourceUpperPosition"`
	Watermark                   PublicWatermark `json:"watermark"`
	TotalSupplyValuationUsdlRaw *string         `json:"totalSupplyValuationUsdlRaw"`
	CirculatingMarketCapUsdlRaw *string         `json:"circulatingMarketCapUsdlRaw"`
	SupplyValuationQuality      string          `json:"supplyValuationQuality"`
	PriceChangeBps24h           *string         `json:"priceChangeBps24h"`
	ValuationChangeUsdlRaw      *string         `json:"valuationChangeUsdlRaw"`
	ChangeBaselineQuality       string          `json:"changeBaselineQuality"`
	// Interface 2.2.0 additive fields (see IF_POOLSTATS semantics). They are
	// optional on the wire: poolStatsTx always sets them, and a document
	// decoded from a frame committed before 2.2.0 re-serializes without them.
	PriceChangeBps5m        *string `json:"priceChangeBps5m"`
	PriceChangeBps1h        *string `json:"priceChangeBps1h"`
	PriceChangeBps6h        *string `json:"priceChangeBps6h"`
	ChangeBaselineQuality5m string  `json:"changeBaselineQuality5m,omitempty"`
	ChangeBaselineQuality1h string  `json:"changeBaselineQuality1h,omitempty"`
	ChangeBaselineQuality6h string  `json:"changeBaselineQuality6h,omitempty"`
	SpotPriceWad            *string `json:"spotPriceWad"`
	SpotPriceUsdlRaw        *string `json:"spotPriceUsdlRaw"`
	UniqueTraders24h        string  `json:"uniqueTraders24h,omitempty"`
	TxCount24h              string  `json:"txCount24h,omitempty"`
	TotalSupplyRaw          *string `json:"totalSupplyRaw"`
	CirculatingSupplyRaw    *string `json:"circulatingSupplyRaw"`
	CirculationModel        *string `json:"circulationModel"`
	Top10HolderShareBps     *int64  `json:"top10HolderShareBps"`
	CreatorHoldingShareBps  *int64  `json:"creatorHoldingShareBps"`
}

// PublicWatermark is the frozen Watermark object.
type PublicWatermark struct {
	ChainID            string `json:"chainId"`
	Height             string `json:"height"`
	BlockHash          string `json:"blockHash"`
	ProjectionRevision string `json:"projectionRevision"`
	ObservedAt         string `json:"observedAt"`
	Integrity          string `json:"integrity"`
}

func publicTime(seconds int64) string {
	return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05.000Z")
}

type priceEvent struct {
	height, transactionIndex, logIndex *big.Int
	timestamp                          int64
	price                              *big.Int
}

func (e priceEvent) after(o *priceEvent) bool {
	if o == nil {
		return true
	}
	if c := e.height.Cmp(o.height); c != 0 {
		return c > 0
	}
	if c := e.transactionIndex.Cmp(o.transactionIndex); c != 0 {
		return c > 0
	}
	return e.logIndex.Cmp(o.logIndex) > 0
}

func abiWords(data []byte, n int) ([]*big.Int, error) {
	if len(data) != 32*n {
		return nil, ErrIntegrity
	}
	out := make([]*big.Int, n)
	for i := range out {
		out[i] = new(big.Int).SetBytes(data[i*32 : (i+1)*32])
	}
	return out, nil
}

func poolTopic(pool string) string {
	return "0x000000000000000000000000" + strings.TrimPrefix(pool, "0x")
}

func positionInts(height, transactionIndex, logIndex string) (*big.Int, *big.Int, *big.Int, error) {
	h, err := Uint(height)
	if err != nil {
		return nil, nil, nil, ErrIntegrity
	}
	t, err := Uint(transactionIndex)
	if err != nil {
		return nil, nil, nil, ErrIntegrity
	}
	l, err := Uint(logIndex)
	if err != nil {
		return nil, nil, nil, ErrIntegrity
	}
	return h, t, l, nil
}

// swapPriceTx decodes the authorized EventEmitter Swap log at a committed fill's
// position and returns the post-trade spot price it carries.
func (p *PublicStatsProjector) swapPriceTx(ctx context.Context, tx pgx.Tx, fill accounting.FillV1, blockTimestamp int64) (priceEvent, error) {
	var e priceEvent
	var emitter, blockHash string
	var topicsRaw []byte
	var data []byte
	err := tx.QueryRow(ctx, "SELECT emitter,block_hash,topics,data FROM "+p.reads.source("indexer_raw_logs_v1")+" WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2::bigint AND height=$3::bigint AND transaction_index=$4::bigint", fill.Position.TransactionHash, fill.Position.LogIndex, fill.Position.Height, fill.Position.TransactionIndex).Scan(&emitter, &blockHash, &topicsRaw, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrIntegrity
	}
	if err != nil {
		return e, err
	}
	var topics []string
	if json.Unmarshal(topicsRaw, &topics) != nil || len(topics) != 3 || strings.ToLower(topics[0]) != swapEventTopic || strings.ToLower(topics[1]) != poolTopic(fill.PoolAddress) || strings.ToLower(emitter) != fill.ActualEmitter || strings.ToLower(blockHash) != fill.Position.BlockHash {
		return e, ErrIntegrity
	}
	words, err := abiWords(data, 7)
	if err != nil {
		return e, err
	}
	isBuy := words[0].Sign() != 0
	output := fill.USDLAmountRaw
	if isBuy {
		output = fill.TokenAmountRaw
	}
	if words[0].Cmp(big.NewInt(1)) > 0 || isBuy != (fill.Side == "buy") || words[1].String() != fill.PaidInputRaw || words[2].String() != output || words[3].String() != fill.FeeRaw || !words[5].IsInt64() || words[5].Int64() != blockTimestamp || words[6].String() != fill.Position.Height {
		return e, ErrIntegrity
	}
	if e.height, e.transactionIndex, e.logIndex, err = positionInts(fill.Position.Height, fill.Position.TransactionIndex, fill.Position.LogIndex); err != nil {
		return e, err
	}
	e.timestamp = blockTimestamp
	e.price = words[4]
	return e, nil
}

type syncEvent struct {
	priceEvent
	realReserve *big.Int
}

// syncEventsTx returns the pool's PoolStateUpdated (syncReserves) events at or
// below the anchor, oldest first.
func (p *PublicStatsProjector) syncEventsTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, emitters []string, pool string) ([]syncEvent, error) {
	if len(emitters) == 0 {
		return []syncEvent{}, nil
	}
	rows, err := tx.Query(ctx, "SELECT height::text,transaction_index::text,log_index::text,topics,data FROM "+p.reads.source("indexer_raw_logs_v1")+" WHERE chain_id=125 AND emitter=ANY($1) AND lower(topics->>0)=$2 AND lower(topics->>1)=$3 AND (height,transaction_index,log_index)<=($4::bigint,$5::bigint,$6::bigint) ORDER BY height,transaction_index,log_index LIMIT $7", emitters, poolStateUpdatedTopic, poolTopic(pool), anchor.Height, anchor.TransactionIndex, anchor.LogIndex, publicStatsMaxSyncEvents+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []syncEvent{}
	for rows.Next() {
		var height, transactionIndex, logIndex string
		var topicsRaw, data []byte
		if err = rows.Scan(&height, &transactionIndex, &logIndex, &topicsRaw, &data); err != nil {
			return nil, err
		}
		var topics []string
		if json.Unmarshal(topicsRaw, &topics) != nil || len(topics) != 2 {
			return nil, ErrIntegrity
		}
		words, err := abiWords(data, 6)
		if err != nil {
			return nil, err
		}
		var e syncEvent
		if e.height, e.transactionIndex, e.logIndex, err = positionInts(height, transactionIndex, logIndex); err != nil {
			return nil, err
		}
		if !words[4].IsInt64() || words[5].Cmp(e.height) != 0 {
			return nil, ErrIntegrity
		}
		e.timestamp = words[4].Int64()
		e.price = words[3]
		e.realReserve = words[1]
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > publicStatsMaxSyncEvents {
		return nil, ErrUnavailable
	}
	return out, nil
}

// committedFill is a committed market fill with its stats commit identity.
type committedFill struct {
	fill      accounting.FillV1
	timestamp int64
	sequence  int64
	digest    string
}

// latestFillTx returns the latest committed fill of the pool at or below the
// anchor, optionally restricted to fills strictly before a block timestamp.
func (p *PublicStatsProjector) latestFillTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, pool string, before int64) (*committedFill, error) {
	var raw []byte
	var out committedFill
	schema := p.reads.repository.schema
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT c.fill,t.block_timestamp,c.sequence,c.source_fact_digest FROM %[1]s.trade_facts t JOIN %[1]s.accounting_market_commits c ON c.projection_version=t.projection_version AND c.pool=t.pool AND c.fill->'position'->>'transactionHash'=t.payload->'position'->>'transactionHash' AND (c.fill->'position'->>'logIndex')::numeric=t.log_index WHERE t.projection_version=$1 AND t.pool=$2 AND (t.height,t.transaction_index,t.log_index)<=($3::numeric,$4::numeric,$5::numeric) AND ($6::bigint<0 OR t.block_timestamp<$6::bigint) ORDER BY t.height DESC,t.transaction_index DESC,t.log_index DESC LIMIT 1`, schema), p.reads.config.ProjectionVersion, pool, anchor.Height, anchor.TransactionIndex, anchor.LogIndex, before).Scan(&raw, &out.timestamp, &out.sequence, &out.digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fill, err := accounting.DecodeFillV1(raw)
	if err != nil || fill.PoolAddress != pool || out.sequence < 1 {
		return nil, ErrIntegrity
	}
	if fill.BlockTimestampSeconds != strconv.FormatInt(out.timestamp, 10) {
		return nil, ErrIntegrity
	}
	out.fill = fill
	return &out, nil
}

// realUsdlTx books the pool's real USDL backing exactly as SidioraPool does:
// buys add paidInput-fee, sells remove amountOut, and syncReserves resets the
// balance to the emitted realReserve.
func (p *PublicStatsProjector) realUsdlTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, pool string, lastSync *syncEvent) (string, error) {
	from := []any{"-1", "0", "0"}
	base := new(big.Int)
	if lastSync != nil {
		from = []any{lastSync.height.String(), lastSync.transactionIndex.String(), lastSync.logIndex.String()}
		base.Set(lastSync.realReserve)
	}
	var delta string
	var sides *bool
	err := tx.QueryRow(ctx, "SELECT COALESCE(SUM(CASE WHEN fill->>'side'='buy' THEN (fill->>'paidInputRaw')::numeric-(fill->>'feeRaw')::numeric WHEN fill->>'side'='sell' THEN -(fill->>'usdlAmountRaw')::numeric END),0)::text,bool_and(fill->>'side' IN ('buy','sell')) FROM "+p.reads.repository.schema+".accounting_market_commits WHERE projection_version=$1 AND pool=$2 AND ((fill->'position'->>'height')::numeric,(fill->'position'->>'transactionIndex')::numeric,(fill->'position'->>'logIndex')::numeric)>($3::numeric,$4::numeric,$5::numeric) AND ((fill->'position'->>'height')::numeric,(fill->'position'->>'transactionIndex')::numeric,(fill->'position'->>'logIndex')::numeric)<=($6::numeric,$7::numeric,$8::numeric)", p.reads.config.ProjectionVersion, pool, from[0], from[1], from[2], anchor.Height, anchor.TransactionIndex, anchor.LogIndex).Scan(&delta, &sides)
	if err != nil {
		return "", err
	}
	if sides != nil && !*sides {
		return "", ErrIntegrity
	}
	d, ok := new(big.Int).SetString(delta, 10)
	if !ok {
		return "", ErrIntegrity
	}
	total := base.Add(base, d)
	if total.Sign() < 0 || total.BitLen() > 256 {
		return "", ErrIntegrity
	}
	return total.String(), nil
}

// anchorHoldings is the Transfer-authoritative holdings state of a token at a
// stats anchor: current balances with every post-anchor transfer reversed.
type anchorHoldings struct {
	count       int64
	decimals    int
	supply      *big.Int
	poolBalance *big.Int
	creator     *big.Int
	top10       *big.Int
}

// holdingsAtAnchorTx reconstructs the token's holdings at the anchor
// (inclusive_indexed_supply_v1), or returns nil when holdings coverage cannot
// prove them: incomplete/quarantined coverage, holdings behind the anchor, or
// too many post-anchor transfers to reverse. top10 and creator exclude the
// pool contract's own balance (the bonding-curve token reserve).
func (p *PublicStatsProjector) holdingsAtAnchorTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, token, pool, creator string) (*anchorHoldings, error) {
	var coverage, supplyRaw string
	var decimals int
	var last []byte
	err := tx.QueryRow(ctx, "SELECT coverage,last_event,supply::text,decimals FROM "+p.table(p.holders, "tokens")+" WHERE token=$1", token).Scan(&coverage, &last, &supplyRaw, &decimals)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if coverage != "complete" {
		return nil, nil
	}
	supply, err := Uint(supplyRaw)
	if err != nil || decimals < 0 || decimals > 255 {
		return nil, ErrIntegrity
	}
	lastPosition := []string{"-1", "0", "0"}
	if len(last) > 0 && string(last) != "null" {
		var event struct {
			Height           string `json:"height"`
			TransactionIndex string `json:"transactionIndex"`
			LogIndex         string `json:"logIndex"`
		}
		if json.Unmarshal(last, &event) != nil {
			return nil, ErrIntegrity
		}
		if _, _, _, err = positionInts(event.Height, event.TransactionIndex, event.LogIndex); err != nil {
			return nil, err
		}
		lastPosition = []string{event.Height, event.TransactionIndex, event.LogIndex}
	}
	transfers := p.reads.source("indexer_token_transfers_v1")
	var behind bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+transfers+" WHERE chain_id=125 AND token=$1 AND (height,transaction_index,log_index)>($2::bigint,$3::bigint,$4::bigint) AND (height,transaction_index,log_index)<=($5::bigint,$6::bigint,$7::bigint))", token, lastPosition[0], lastPosition[1], lastPosition[2], anchor.Height, anchor.TransactionIndex, anchor.LogIndex).Scan(&behind); err != nil {
		return nil, err
	}
	if behind {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "SELECT from_address,to_address,amount_raw::text FROM "+transfers+" WHERE chain_id=125 AND token=$1 AND (height,transaction_index,log_index)>($2::bigint,$3::bigint,$4::bigint) AND (height,transaction_index,log_index)<=($5::bigint,$6::bigint,$7::bigint) LIMIT $8", token, anchor.Height, anchor.TransactionIndex, anchor.LogIndex, lastPosition[0], lastPosition[1], lastPosition[2], publicStatsMaxReversals+1)
	if err != nil {
		return nil, err
	}
	const zero = "0x0000000000000000000000000000000000000000"
	deltas := map[string]*big.Int{}
	reversals := 0
	for rows.Next() {
		var from, to, amount string
		if err = rows.Scan(&from, &to, &amount); err != nil {
			rows.Close()
			return nil, err
		}
		value, e := Uint(amount)
		if e != nil {
			rows.Close()
			return nil, ErrIntegrity
		}
		from, to = strings.ToLower(from), strings.ToLower(to)
		// Reversing a post-anchor transfer gives the sender back and takes it
		// from the recipient; reversing a mint (burn) removes (restores) supply.
		if from != zero {
			if deltas[from] == nil {
				deltas[from] = new(big.Int)
			}
			deltas[from].Add(deltas[from], value)
		} else {
			supply.Sub(supply, value)
		}
		if to != zero {
			if deltas[to] == nil {
				deltas[to] = new(big.Int)
			}
			deltas[to].Sub(deltas[to], value)
		} else {
			supply.Add(supply, value)
		}
		reversals++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if reversals > publicStatsMaxReversals {
		return nil, nil
	}
	if supply.Sign() < 0 || supply.BitLen() > 256 {
		return nil, ErrIntegrity
	}
	out := &anchorHoldings{decimals: decimals, supply: supply, poolBalance: new(big.Int), creator: new(big.Int), top10: new(big.Int)}
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+p.table(p.holders, "balances")+" WHERE token=$1 AND balance>0", token).Scan(&out.count); err != nil {
		return nil, err
	}
	current := func(wallet string) (*big.Int, error) {
		var raw string
		err := tx.QueryRow(ctx, "SELECT balance::text FROM "+p.table(p.holders, "balances")+" WHERE token=$1 AND address=$2", token, wallet).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return new(big.Int), nil
		}
		if err != nil {
			return nil, err
		}
		value, err := Uint(raw)
		if err != nil {
			return nil, ErrIntegrity
		}
		return value, nil
	}
	// Balances at the anchor of every wallet a post-anchor transfer touched.
	historical := map[string]*big.Int{}
	for wallet, delta := range deltas {
		now, err := current(wallet)
		if err != nil {
			return nil, err
		}
		then := new(big.Int).Add(now, delta)
		if then.Sign() < 0 {
			return nil, ErrIntegrity
		}
		historical[wallet] = then
		if now.Sign() > 0 && then.Sign() == 0 {
			out.count--
		}
		if now.Sign() == 0 && then.Sign() > 0 {
			out.count++
		}
	}
	if out.count < 0 {
		return nil, ErrIntegrity
	}
	at := func(wallet string) (*big.Int, error) {
		if v, ok := historical[wallet]; ok {
			return new(big.Int).Set(v), nil
		}
		return current(wallet)
	}
	if out.poolBalance, err = at(pool); err != nil {
		return nil, err
	}
	if creator != pool {
		if out.creator, err = at(creator); err != nil {
			return nil, err
		}
	}
	// The ten largest non-pool balances at the anchor: wallets untouched after
	// the anchor keep their current balance, so they are among the current
	// top 10+len(deltas); touched wallets use their reconstructed balance.
	candidates := []*big.Int{}
	for wallet, v := range historical {
		if wallet != pool && v.Sign() > 0 {
			candidates = append(candidates, v)
		}
	}
	top, err := tx.Query(ctx, "SELECT address,balance::text FROM "+p.table(p.holders, "balances")+" WHERE token=$1 AND balance>0 AND address<>$2 ORDER BY balance DESC,address ASC LIMIT $3", token, pool, 10+len(historical))
	if err != nil {
		return nil, err
	}
	for top.Next() {
		var wallet, raw string
		if err = top.Scan(&wallet, &raw); err != nil {
			top.Close()
			return nil, err
		}
		if _, touched := historical[wallet]; touched {
			continue
		}
		value, e := Uint(raw)
		if e != nil {
			top.Close()
			return nil, ErrIntegrity
		}
		candidates = append(candidates, value)
	}
	top.Close()
	if err = top.Err(); err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Cmp(candidates[j]) > 0 })
	for i := 0; i < len(candidates) && i < 10; i++ {
		out.top10.Add(out.top10, candidates[i])
	}
	if out.poolBalance.Cmp(out.supply) > 0 || new(big.Int).Add(out.poolBalance, out.top10).Cmp(out.supply) > 0 {
		return nil, ErrIntegrity
	}
	return out, nil
}

// priceChangeBps is the signed basis-point change truncated toward zero.
func priceChangeBps(base, current *big.Int) (*string, bool) {
	if base == nil || current == nil || base.Sign() <= 0 {
		return nil, false
	}
	change := new(big.Int).Sub(current, base)
	change.Mul(change, big.NewInt(10000))
	change.Quo(change, base)
	value := change.String()
	return &value, true
}

func latestOf(events ...*priceEvent) *priceEvent {
	var out *priceEvent
	for _, e := range events {
		if e != nil && e.after(out) {
			out = e
		}
	}
	return out
}

// shareBps is floor(10000*part/whole), or nil when whole is zero.
func shareBps(part, whole *big.Int) (*int64, error) {
	if whole.Sign() <= 0 {
		return nil, nil
	}
	v := new(big.Int).Mul(part, big.NewInt(10000))
	v.Quo(v, whole)
	if v.Sign() < 0 || v.Cmp(big.NewInt(10000)) > 0 {
		return nil, ErrIntegrity
	}
	n := v.Int64()
	return &n, nil
}

// wadValue is floor(amount*priceWad/10^18): the USDL raw value of a raw token
// amount at a pool getPrice() mark.
func wadValue(amount, priceWad *big.Int) *string {
	v := new(big.Int).Mul(amount, priceWad)
	v.Quo(v, publicWad)
	s := v.String()
	return &s
}

var publicWad = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// publicChangeWindows are the shorter rolling price-change windows; each ends
// at windowEnd like the 24h window.
var publicChangeWindows = []int64{300, 3600, 21600}

// circulationModel names the circulating supply rule of the valuation fields.
const circulationModel = "supply_minus_pool_reserve_v1"

// windowChange applies the priceChangeBps24h rule to the window starting at
// start: compare the latest price event at the anchor with the latest price
// event strictly before start.
func (p *PublicStatsProjector) windowChange(ctx context.Context, tx pgx.Tx, anchor publicAnchor, pool string, syncs []syncEvent, current *priceEvent, created, start int64) (*string, string, error) {
	var baseline *priceEvent
	for i := range syncs {
		if syncs[i].timestamp < start {
			baseline = &syncs[i].priceEvent
		}
	}
	before, err := p.latestFillTx(ctx, tx, anchor, pool, start)
	if err != nil {
		return nil, "", err
	}
	if before != nil {
		e, err := p.swapPriceTx(ctx, tx, before.fill, before.timestamp)
		if err != nil {
			return nil, "", err
		}
		baseline = latestOf(baseline, &e)
	}
	switch {
	case current == nil:
		// No price-moving event since creation: the price is unchanged.
		zero := "0"
		if created >= start {
			return &zero, "insufficient_history", nil
		}
		return &zero, "complete", nil
	case baseline != nil:
		change, ok := priceChangeBps(baseline.price, current.price)
		if !ok {
			return change, "unavailable", nil
		}
		return change, "complete", nil
	default:
		// Every price-moving event lies inside the window and the creation
		// price is not carried by a canonical event.
		return nil, "insufficient_history", nil
	}
}

func (p *PublicStatsProjector) poolStatsTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, emitters []string, pool, token, creator string, created int64) (PublicPoolStats, *committedFill, error) {
	end := anchor.timestamp + 1
	start := end - publicStatsWindowSeconds
	doc := PublicPoolStats{PoolAddress: pool, WindowStart: publicTime(start), WindowEnd: publicTime(end), SourceUpperPosition: anchor.Position, SupplyValuationQuality: "unavailable"}
	schema := p.reads.repository.schema
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(SUM(t.volume),0)::text,COUNT(*) FILTER(WHERE t.side='buy')::text,COUNT(*) FILTER(WHERE t.side='sell')::text,COUNT(DISTINCT t.actor)::text,COUNT(*)::text FROM %[1]s.trade_facts t JOIN %[1]s.accounting_market_commits c ON c.projection_version=t.projection_version AND c.pool=t.pool AND c.fill->'position'->>'transactionHash'=t.payload->'position'->>'transactionHash' AND (c.fill->'position'->>'logIndex')::numeric=t.log_index WHERE t.projection_version=$1 AND t.pool=$2 AND (t.height,t.transaction_index,t.log_index)<=($3::numeric,$4::numeric,$5::numeric) AND t.block_timestamp>=$6 AND t.block_timestamp<$7`, schema), p.reads.config.ProjectionVersion, pool, anchor.Height, anchor.TransactionIndex, anchor.LogIndex, start, end).Scan(&doc.Volume24hRaw, &doc.Buys24h, &doc.Sells24h, &doc.UniqueTraders24h, &doc.TxCount24h)
	if err != nil {
		return doc, nil, err
	}
	syncs, err := p.syncEventsTx(ctx, tx, anchor, emitters, pool)
	if err != nil {
		return doc, nil, err
	}
	var lastSync *syncEvent
	for i := range syncs {
		lastSync = &syncs[i]
	}
	if doc.RealUsdlBalanceRaw, err = p.realUsdlTx(ctx, tx, anchor, pool, lastSync); err != nil {
		return doc, nil, err
	}
	var current *priceEvent
	if lastSync != nil {
		current = &lastSync.priceEvent
	}
	last, err := p.latestFillTx(ctx, tx, anchor, pool, -1)
	if err != nil {
		return doc, nil, err
	}
	if last != nil {
		e, err := p.swapPriceTx(ctx, tx, last.fill, last.timestamp)
		if err != nil {
			return doc, nil, err
		}
		current = latestOf(current, &e)
	}
	if doc.PriceChangeBps24h, doc.ChangeBaselineQuality, err = p.windowChange(ctx, tx, anchor, pool, syncs, current, created, start); err != nil {
		return doc, nil, err
	}
	changes := []**string{&doc.PriceChangeBps5m, &doc.PriceChangeBps1h, &doc.PriceChangeBps6h}
	qualities := []*string{&doc.ChangeBaselineQuality5m, &doc.ChangeBaselineQuality1h, &doc.ChangeBaselineQuality6h}
	for i, seconds := range publicChangeWindows {
		if *changes[i], *qualities[i], err = p.windowChange(ctx, tx, anchor, pool, syncs, current, created, end-seconds); err != nil {
			return doc, nil, err
		}
	}
	holdings, err := p.holdingsAtAnchorTx(ctx, tx, anchor, token, pool, creator)
	if err != nil {
		return doc, nil, err
	}
	decimals := -1
	if holdings != nil {
		count := strconv.FormatInt(holdings.count, 10)
		doc.HolderCount = &count
		decimals = holdings.decimals
		supply := holdings.supply.String()
		doc.TotalSupplyRaw = &supply
		circulating := new(big.Int).Sub(holdings.supply, holdings.poolBalance)
		circulatingRaw := circulating.String()
		doc.CirculatingSupplyRaw = &circulatingRaw
		model := circulationModel
		doc.CirculationModel = &model
		if doc.Top10HolderShareBps, err = shareBps(holdings.top10, circulating); err != nil {
			return doc, nil, err
		}
		if doc.CreatorHoldingShareBps, err = shareBps(holdings.creator, circulating); err != nil {
			return doc, nil, err
		}
	} else if last != nil {
		decimals = int(last.fill.Token.Decimals)
	}
	if current != nil {
		wad := current.price.String()
		doc.SpotPriceWad = &wad
		if decimals >= 0 && decimals <= 18 {
			// floor(floor(E*10^18/T)*10^d/10^18) == floor(E*10^d/T) for d<=18.
			v := new(big.Int).Mul(current.price, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
			v.Quo(v, publicWad)
			raw := v.String()
			doc.SpotPriceUsdlRaw = &raw
		}
		if holdings != nil {
			doc.TotalSupplyValuationUsdlRaw = wadValue(holdings.supply, current.price)
			doc.CirculatingMarketCapUsdlRaw = wadValue(new(big.Int).Sub(holdings.supply, holdings.poolBalance), current.price)
			doc.SupplyValuationQuality = "verified"
		}
	}
	return doc, last, nil
}

type publicStatsFrame struct {
	EventID             string          `json:"eventId"`
	Topic               string          `json:"topic"`
	Sequence            string          `json:"sequence"`
	Revision            string          `json:"revision"`
	OccurredAt          string          `json:"occurredAt"`
	Watermark           PublicWatermark `json:"watermark"`
	Payload             PublicPoolStats `json:"payload"`
	Coalesced           bool            `json:"coalesced"`
	SourceSequenceFirst *string         `json:"sourceSequenceFirst"`
	SourceSequenceLast  *string         `json:"sourceSequenceLast"`
}

// statsContent is the document identity used for change detection: everything
// except the observation time.
func statsContent(doc PublicPoolStats) ([]byte, error) {
	doc.Watermark = PublicWatermark{ChainID: doc.Watermark.ChainID, Height: doc.Watermark.Height, BlockHash: doc.Watermark.BlockHash}
	return json.Marshal(doc)
}

// ValidatePublicStatsFrame applies the frozen StatsEvent schema plus the
// cross-field bindings core-api enforces before serving a market.stats topic.
func ValidatePublicStatsFrame(wire []byte) error {
	if len(wire) == 0 || len(wire) > 256*1024 || interfaces.ValidateJSON("StatsEvent", wire) != nil {
		return ErrIntegrity
	}
	var f publicStatsFrame
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&f) != nil {
		return ErrIntegrity
	}
	start, err := time.Parse("2006-01-02T15:04:05.000Z", f.Payload.WindowStart)
	if err != nil {
		return ErrIntegrity
	}
	end, err := time.Parse("2006-01-02T15:04:05.000Z", f.Payload.WindowEnd)
	if err != nil || end.Sub(start) != publicStatsWindowSeconds*time.Second {
		return ErrIntegrity
	}
	if f.Topic != publicStatsTopicPrefix+f.Payload.PoolAddress || f.Watermark != f.Payload.Watermark || f.Watermark.Integrity != "healthy" || f.Revision != f.Watermark.ProjectionRevision || f.Payload.SourceUpperPosition.Height != f.Watermark.Height || f.Payload.SourceUpperPosition.BlockHash != f.Watermark.BlockHash || f.Coalesced || f.SourceSequenceFirst != nil || f.SourceSequenceLast != nil {
		return ErrIntegrity
	}
	return nil
}

// appendTx appends a market.stats frame when the pool's document differs from
// the topic's current frame. The frame is committed through
// api_stream.append_market_stats, which re-derives the window aggregates from
// the committed fills and owns the topic sequence.
func (p *PublicStatsProjector) appendTx(ctx context.Context, tx pgx.Tx, anchor publicAnchor, doc PublicPoolStats, observedAt time.Time) (bool, error) {
	topic := publicStatsTopicPrefix + doc.PoolAddress
	writer := delivery.StreamWriter{Schema: p.stream}
	prior, err := writer.MarketProjectionHeadTx(ctx, tx, topic)
	if err != nil {
		return false, err
	}
	revision := new(big.Int).SetInt64(1)
	if len(prior) > 0 {
		var previous publicStatsFrame
		if ValidatePublicStatsFrame(prior) != nil || json.Unmarshal(prior, &previous) != nil || previous.Topic != topic {
			return false, ErrIntegrity
		}
		previousRevision, err := Uint(previous.Revision)
		if err != nil {
			return false, ErrIntegrity
		}
		// A frame anchored at an older chain position never replaces a newer one.
		ph, pt, pl, err := positionInts(previous.Payload.SourceUpperPosition.Height, previous.Payload.SourceUpperPosition.TransactionIndex, previous.Payload.SourceUpperPosition.LogIndex)
		if err != nil {
			return false, err
		}
		ah, at, al, err := positionInts(anchor.Height, anchor.TransactionIndex, anchor.LogIndex)
		if err != nil {
			return false, err
		}
		if (priceEvent{height: ph, transactionIndex: pt, logIndex: pl}).after(&priceEvent{height: ah, transactionIndex: at, logIndex: al}) {
			return false, nil
		}
		was, err := statsContent(previous.Payload)
		if err != nil {
			return false, err
		}
		doc.Watermark = PublicWatermark{ChainID: "125", Height: anchor.Height, BlockHash: anchor.BlockHash}
		next, err := statsContent(doc)
		if err != nil {
			return false, err
		}
		if bytes.Equal(was, next) {
			return false, nil
		}
		revision.Add(previousRevision, big.NewInt(1))
	}
	doc.Watermark = PublicWatermark{ChainID: "125", Height: anchor.Height, BlockHash: anchor.BlockHash, ProjectionRevision: revision.String(), ObservedAt: observedAt.UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	frame := publicStatsFrame{EventID: "stats:" + doc.PoolAddress + ":" + revision.String(), Topic: topic, Sequence: "1", Revision: revision.String(), OccurredAt: publicTime(anchor.timestamp), Watermark: doc.Watermark, Payload: doc}
	wire, err := json.Marshal(frame)
	if err != nil {
		return false, err
	}
	if err = ValidatePublicStatsFrame(wire); err != nil {
		return false, err
	}
	watermark, err := json.Marshal(doc.Watermark)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return false, err
	}
	update := delivery.StreamUpdate{EventID: frame.EventID, Topic: topic, Revision: frame.Revision, OccurredAt: frame.OccurredAt, Watermark: watermark, Payload: payload,
		Source: delivery.StreamSourceProof{Kind: "stats_commit", Pool: doc.PoolAddress, ProjectionVersion: p.reads.config.ProjectionVersion}}
	if err = writer.AppendTx(ctx, tx, update); err != nil {
		return false, err
	}
	return true, nil
}

// PublicPrice is the frozen IF Price document (gross execution of the latest
// committed fill: paid USDL input / token output for buys, USDL output / token
// input for sells, in human units).
type PublicPrice struct {
	Numerator   string            `json:"numerator"`
	Denominator string            `json:"denominator"`
	BaseAsset   publicAsset       `json:"baseAsset"`
	QuoteAsset  publicAsset       `json:"quoteAsset"`
	Source      publicPriceSource `json:"source"`
}

type publicAsset struct {
	Kind         string  `json:"kind"`
	ChainID      string  `json:"chainId"`
	Address      string  `json:"address"`
	CurrencyCode *string `json:"currencyCode"`
	Decimals     int     `json:"decimals"`
}

type publicPriceSource struct {
	Kind            string   `json:"kind"`
	AmountBasis     string   `json:"amountBasis"`
	SourceEventID   string   `json:"sourceEventId"`
	Position        Position `json:"position"`
	ProviderID      *string  `json:"providerId"`
	ObservedAt      string   `json:"observedAt"`
	SourceReference *string  `json:"sourceReference"`
}

type publicPriceFrame struct {
	EventID             string          `json:"eventId"`
	Topic               string          `json:"topic"`
	Sequence            string          `json:"sequence"`
	Revision            string          `json:"revision"`
	OccurredAt          string          `json:"occurredAt"`
	Watermark           PublicWatermark `json:"watermark"`
	Payload             PublicPrice     `json:"payload"`
	Coalesced           bool            `json:"coalesced"`
	SourceSequenceFirst *string         `json:"sourceSequenceFirst"`
	SourceSequenceLast  *string         `json:"sourceSequenceLast"`
}

// PublicPriceOf renders the gross-execution price of a committed fill.
func PublicPriceOf(fill accounting.FillV1) (PublicPrice, error) {
	token, err := Uint(fill.TokenAmountRaw)
	if err != nil || token.Sign() == 0 {
		return PublicPrice{}, ErrIntegrity
	}
	usdl, err := Uint(fill.USDLAmountRaw)
	if err != nil {
		return PublicPrice{}, ErrIntegrity
	}
	scale := func(decimals uint8) *big.Int {
		return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	}
	ratio := new(big.Rat).SetFrac(new(big.Int).Mul(usdl, scale(fill.Token.Decimals)), new(big.Int).Mul(token, scale(fill.Settlement.Decimals)))
	occurred, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return PublicPrice{}, ErrIntegrity
	}
	return PublicPrice{Numerator: ratio.Num().String(), Denominator: ratio.Denom().String(),
		BaseAsset:  publicAsset{Kind: "erc20", ChainID: "125", Address: fill.Token.Address, Decimals: int(fill.Token.Decimals)},
		QuoteAsset: publicAsset{Kind: "erc20", ChainID: "125", Address: fill.Settlement.Address, Decimals: int(fill.Settlement.Decimals)},
		Source: publicPriceSource{Kind: "gross_execution", AmountBasis: "paid_input", SourceEventID: "125:" + fill.Position.TransactionHash + ":" + fill.Position.LogIndex,
			Position: Position(fill.Position), ObservedAt: publicTime(occurred)}}, nil
}

// ValidatePublicPriceFrame applies the frozen MarketPriceEvent schema plus the
// topic and watermark bindings.
func ValidatePublicPriceFrame(wire []byte) error {
	if len(wire) == 0 || len(wire) > 256*1024 || interfaces.ValidateJSON("MarketPriceEvent", wire) != nil {
		return ErrIntegrity
	}
	var f publicPriceFrame
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&f) != nil {
		return ErrIntegrity
	}
	if !strings.HasPrefix(f.Topic, "market.price.0x") || f.Watermark.Integrity != "healthy" || f.Revision != f.Watermark.ProjectionRevision || f.Payload.Source.Position.Height != f.Watermark.Height || f.Payload.Source.Position.BlockHash != f.Watermark.BlockHash || f.Coalesced || f.SourceSequenceFirst != nil || f.SourceSequenceLast != nil {
		return ErrIntegrity
	}
	return nil
}

// appendPriceTx appends the market.price frame of the pool's latest committed
// fill; the revision is the fill's market partition sequence, so a fill is
// published at most once and never behind a newer one.
func (p *PublicStatsProjector) appendPriceTx(ctx context.Context, tx pgx.Tx, last committedFill, observedAt time.Time) (bool, error) {
	topic := "market.price." + last.fill.PoolAddress
	writer := delivery.StreamWriter{Schema: p.stream}
	prior, err := writer.MarketProjectionHeadTx(ctx, tx, topic)
	if err != nil {
		return false, err
	}
	if len(prior) > 0 {
		var previous publicPriceFrame
		if ValidatePublicPriceFrame(prior) != nil || json.Unmarshal(prior, &previous) != nil || previous.Topic != topic {
			return false, ErrIntegrity
		}
		revision, err := strconv.ParseInt(previous.Revision, 10, 64)
		if err != nil {
			return false, ErrIntegrity
		}
		if revision >= last.sequence {
			return false, nil
		}
	}
	price, err := PublicPriceOf(last.fill)
	if err != nil {
		return false, err
	}
	revision := strconv.FormatInt(last.sequence, 10)
	watermark := PublicWatermark{ChainID: "125", Height: last.fill.Position.Height, BlockHash: last.fill.Position.BlockHash, ProjectionRevision: revision, ObservedAt: observedAt.UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	frame := publicPriceFrame{EventID: "price:" + price.Source.SourceEventID, Topic: topic, Sequence: "1", Revision: revision, OccurredAt: price.Source.ObservedAt, Watermark: watermark, Payload: price}
	wire, err := json.Marshal(frame)
	if err != nil {
		return false, err
	}
	if err = ValidatePublicPriceFrame(wire); err != nil {
		return false, err
	}
	watermarkRaw, err := json.Marshal(watermark)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(price)
	if err != nil {
		return false, err
	}
	update := delivery.StreamUpdate{EventID: frame.EventID, Topic: topic, Revision: revision, OccurredAt: frame.OccurredAt, Watermark: watermarkRaw, Payload: payload,
		Source: delivery.StreamSourceProof{Kind: "price_commit", Pool: last.fill.PoolAddress, ProjectionVersion: p.reads.config.ProjectionVersion, Sequence: revision, SourceFillID: last.fill.SourceFillID, SourceFactDigest: last.digest}}
	if err = writer.AppendTx(ctx, tx, update); err != nil {
		return false, err
	}
	return true, nil
}

// PoolTopicID is the EventEmitter poolId topic of a pool (its left-padded
// address, as SidioraPool derives it).
func PoolTopicID(pool string) string { return poolTopic(pool) }
