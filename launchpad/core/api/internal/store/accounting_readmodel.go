package store

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting/sourcebootstrap"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxCanonicalCandles = 2000
const MaxCanonicalTrades = 100
const maxAccountingPageBytes = 256 << 10

var ErrAccountingNotReady = errors.New("accounting snapshot not coherent")
var accountingSchema = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var accountingToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var accountingDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// maxVerifiedCheckpoints bounds the in-process set of offsets checkpoints
// already re-derived from indexer evidence.
const maxVerifiedCheckpoints = 1 << 16

// AccountingReadConfig binds the read model to the stats and charts delivery
// consumers. The per-pool source bootstrap checkpoint each consumer stores in
// delivery_partition_offsets.checkpoint_digest is re-derived from the indexer
// evidence (shared/accounting/sourcebootstrap, the producers' own definition),
// so SourceStream and SourcePartitionSchema must equal the consumers' values.
type AccountingReadConfig struct {
	ChartsSchema, StatsSchema, ChartsDeliverySchema, StatsDeliverySchema string
	StreamSchema                                                         string
	ProjectionVersion, ProjectionGeneration                              string
	ChartsConsumerID, StatsConsumerID                                    string
	SourceStream, SourcePartitionSchema                                  string
}
type AccountingReadModel struct {
	pool     *pgxpool.Pool
	config   AccountingReadConfig
	mu       sync.Mutex
	verified map[string]bool
}

func NewAccountingReadModel(pool *pgxpool.Pool, c AccountingReadConfig) (*AccountingReadModel, error) {
	if pool == nil {
		return nil, ErrAccountingNotReady
	}
	for _, s := range []string{c.ChartsSchema, c.StatsSchema, c.ChartsDeliverySchema, c.StatsDeliverySchema, c.SourcePartitionSchema} {
		if !accountingSchema.MatchString(s) {
			return nil, ErrAccountingNotReady
		}
	}
	for _, value := range []string{c.ProjectionGeneration, c.ChartsConsumerID, c.StatsConsumerID} {
		if !accountingToken.MatchString(value) {
			return nil, ErrAccountingNotReady
		}
	}
	if c.StreamSchema != "" && !accountingSchema.MatchString(c.StreamSchema) {
		return nil, ErrAccountingNotReady
	}
	if c.ChartsConsumerID == c.StatsConsumerID || c.ProjectionVersion == "" || len(c.ProjectionVersion) > 64 || c.SourceStream == "" || len(c.SourceStream) > 128 {
		return nil, ErrAccountingNotReady
	}
	return &AccountingReadModel{pool: pool, config: c, verified: map[string]bool{}}, nil
}

type AccountingPageRequest struct {
	Pool, Interval, Side string
	From, To             int64
	Limit                int
	AfterStart           *int64
	AfterSequence        *int64
	ExpectedSnapshot     string
	SnapshotSequence     *int64
	CursorExpires        int64
	SnapshotObservedAt   string
	AfterOrder           *accounting.SourcePosition
	AfterEventID         string
}
type AccountingPage struct {
	Items                                    []json.RawMessage
	SnapshotID, SeriesModelVersion, Revision string
	Watermark                                json.RawMessage
	GeneratedAt                              string
	NextStart                                *int64
	NextSequence                             *int64
	NextOrder                                *accounting.SourcePosition
	NextEventID                              string
}
type accountingVector struct {
	checkpoint string
	position   accounting.PartitionPosition
	sequence   int64
}

// expectedCheckpoint re-derives, from the indexer evidence visible in tx, the
// source bootstrap checkpoint the charts or stats consumer wrote for pool.
func (s *AccountingReadModel) expectedCheckpoint(ctx context.Context, tx pgx.Tx, charts bool, pool string) (string, error) {
	var digest accounting.Digest
	if charts {
		e, err := sourcebootstrap.LoadCharts(ctx, tx, s.config.SourcePartitionSchema, s.config.SourceStream, s.config.ProjectionVersion, pool)
		if err != nil {
			return "", err
		}
		digest = e.Checkpoint()
	} else {
		e, err := sourcebootstrap.LoadStats(ctx, tx, s.config.SourcePartitionSchema, s.config.SourceStream, s.config.ProjectionVersion, pool)
		if err != nil {
			return "", err
		}
		digest = e.Checkpoint()
	}
	return string(digest), nil
}

// verifyCheckpoint admits the offsets row's checkpoint only when it equals the
// digest re-derived from the indexer evidence. A row's checkpoint is immutable
// (delivery.PartitionStore.BootstrapTx), so a verified (consumer, pool,
// checkpoint) binding is remembered and later reads skip the evidence queries.
func (s *AccountingReadModel) verifyCheckpoint(ctx context.Context, tx pgx.Tx, charts bool, consumer, deliverySchema, pool, stored string) error {
	key := deliverySchema + "\x00" + consumer + "\x00" + s.config.ProjectionGeneration + "\x00" + pool + "\x00" + stored
	s.mu.Lock()
	known := s.verified[key]
	s.mu.Unlock()
	if known {
		return nil
	}
	expected, err := s.expectedCheckpoint(ctx, tx, charts, pool)
	if errors.Is(err, sourcebootstrap.ErrUnavailable) || errors.Is(err, sourcebootstrap.ErrIntegrity) {
		return ErrDataIntegrity
	}
	if err != nil {
		return err
	}
	if expected != stored {
		return ErrDataIntegrity
	}
	s.mu.Lock()
	if len(s.verified) >= maxVerifiedCheckpoints {
		clear(s.verified)
	}
	s.verified[key] = true
	s.mu.Unlock()
	return nil
}

func (s *AccountingReadModel) vector(ctx context.Context, tx pgx.Tx, pool, consumer string, charts bool, deliverySchema string, pinned *int64) (accountingVector, error) {
	v := accountingVector{}
	var raw []byte
	q := pgx.Identifier{deliverySchema}.Sanitize()
	err := tx.QueryRow(ctx, `SELECT o.checkpoint_digest,c.sequence,c.position FROM `+q+`.delivery_partition_offsets_v1 o JOIN `+q+`.delivery_partition_commits_v1 c USING(consumer_id,projection_generation,partition_kind,partition_key) WHERE o.consumer_id=$1 AND o.projection_generation=$2 AND o.partition_kind='market' AND o.partition_key=$3 AND c.sequence=COALESCE($4::bigint,o.next_sequence-1) AND c.sequence<o.next_sequence AND ($4::bigint IS NOT NULL OR c.effect_identity=o.previous_effect_identity)`, consumer, s.config.ProjectionGeneration, "125:"+pool, pinned).Scan(&v.checkpoint, &v.sequence, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrAccountingNotReady
	}
	if err != nil {
		return v, err
	}
	if len(raw) > accounting.MaxWireBytes || json.Unmarshal(raw, &v.position) != nil || accounting.ValidatePartitionPosition(v.position) != nil || v.position.Sequence != strconv.FormatInt(v.sequence, 10) || v.position.ProjectionVersion != s.config.ProjectionVersion || v.position.PartitionKind != "market" || v.position.PartitionKey != "125:"+pool {
		return v, ErrDataIntegrity
	}
	if err = s.verifyCheckpoint(ctx, tx, charts, consumer, deliverySchema, pool, v.checkpoint); err != nil {
		return v, err
	}
	var quarantined bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+q+`.delivery_ordered_quarantine_v1 WHERE consumer_id=$1 AND projection_generation=$2 AND $3=ANY(affected_pools))`, consumer, s.config.ProjectionGeneration, pool).Scan(&quarantined)
	if err != nil {
		return v, err
	}
	if quarantined {
		return v, ErrDataIntegrity
	}
	return v, nil
}
func (s *AccountingReadModel) snapshot(ctx context.Context, tx pgx.Tx, r AccountingPageRequest) (AccountingPage, accountingVector, error) {
	out := AccountingPage{Items: []json.RawMessage{}, SeriesModelVersion: s.config.ProjectionVersion}
	v, err := s.vector(ctx, tx, r.Pool, s.config.ChartsConsumerID, true, s.config.ChartsDeliverySchema, r.SnapshotSequence)
	if err != nil {
		return out, v, err
	}
	stats, err := s.vector(ctx, tx, r.Pool, s.config.StatsConsumerID, false, s.config.StatsDeliverySchema, r.SnapshotSequence)
	if err != nil {
		return out, v, err
	}
	a, _ := json.Marshal(v.position)
	b, _ := json.Marshal(stats.position)
	if v.sequence != stats.sequence || string(a) != string(b) {
		return out, v, ErrAccountingNotReady
	}
	var frozen bool
	err = tx.QueryRow(ctx, `SELECT frozen FROM `+pgx.Identifier{s.config.ChartsSchema}.Sanitize()+`.chart_pool_fences_v1 WHERE chain_id='125' AND pool=$1`, r.Pool).Scan(&frozen)
	if err != nil {
		return out, v, err
	}
	if frozen {
		return out, v, ErrDataIntegrity
	}
	err = tx.QueryRow(ctx, `SELECT frozen FROM `+pgx.Identifier{s.config.StatsSchema}.Sanitize()+`.accounting_pool_fences_v1 WHERE pool=$1`, r.Pool).Scan(&frozen)
	if err != nil {
		return out, v, err
	}
	if frozen {
		return out, v, ErrDataIntegrity
	}
	fills, err := s.sourceFills(ctx, tx, r.Pool, []string{v.position.SourceEventID}, v.sequence)
	if err != nil {
		return out, v, err
	}
	latest, ok := fills[v.position.SourceEventID]
	if !ok || latest.Position != v.position.SourceOrder {
		return out, v, ErrDataIntegrity
	}
	digest, err := accounting.FillDigest(latest)
	if err != nil || digest != v.position.PayloadDigest {
		return out, v, ErrDataIntegrity
	}
	var observed time.Time
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&observed); err != nil {
		return out, v, err
	}
	out.GeneratedAt = observed.UTC().Format("2006-01-02T15:04:05.000Z")
	out.Revision = strconv.FormatInt(v.sequence, 10)
	identity, _ := json.Marshal([]any{"public-accounting-snapshot-v2", s.config.ProjectionGeneration, v.checkpoint, stats.checkpoint, v.position})
	out.SnapshotID = uuid.NewSHA1(uuid.NameSpaceOID, identity).String()
	if r.ExpectedSnapshot != "" && r.ExpectedSnapshot != out.SnapshotID {
		return out, v, ErrAccountingNotReady
	}
	watermarkObserved := out.GeneratedAt
	if r.ExpectedSnapshot != "" {
		if _, parseErr := time.Parse("2006-01-02T15:04:05.000Z", r.SnapshotObservedAt); parseErr != nil {
			return out, v, ErrDataIntegrity
		}
		watermarkObserved = r.SnapshotObservedAt
	}
	out.Watermark, err = json.Marshal(map[string]any{"chainId": "125", "height": v.position.SourceOrder.Height, "blockHash": v.position.SourceOrder.BlockHash, "projectionRevision": out.Revision, "observedAt": watermarkObserved, "integrity": "healthy"})
	if err == nil {
		err = interfaces.ValidateJSON("Watermark", out.Watermark)
	}
	return out, v, err
}
func validateAccountingRequest(r AccountingPageRequest, candles bool) error {
	pool, err := Address(r.Pool)
	if err != nil || pool != r.Pool {
		return ErrDataIntegrity
	}
	max := MaxCanonicalTrades
	if candles {
		max = MaxCanonicalCandles
		if r.From < 0 || r.To <= r.From || r.To > 253402300799999 {
			return ErrDataIntegrity
		}
		switch r.Interval {
		case "1m", "5m", "15m", "1h", "4h", "1d", "1w":
		default:
			return ErrDataIntegrity
		}
	}
	if r.SnapshotSequence != nil && (*r.SnapshotSequence < 1 || r.ExpectedSnapshot == "") {
		return ErrDataIntegrity
	}
	if r.Limit < 1 || r.Limit > max || r.Side != "" && r.Side != "buy" && r.Side != "sell" {
		return ErrDataIntegrity
	}
	return nil
}
func (s *AccountingReadModel) sourceFills(ctx context.Context, tx pgx.Tx, pool string, ids []string, maxSequence int64) (map[string]accounting.FillV1, error) {
	result := map[string]accounting.FillV1{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > MaxCanonicalCandles*4 {
		return nil, ErrDataIntegrity
	}
	rows, err := tx.Query(ctx, `SELECT source_fill_id,source_fact_digest,fill FROM `+pgx.Identifier{s.config.StatsSchema}.Sanitize()+`.accounting_market_commits_v1 WHERE pool=$1 AND projection_version=$2 AND source_fill_id=ANY($3) AND sequence<=$4 LIMIT $5`, pool, s.config.ProjectionVersion, ids, maxSequence, len(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var id, digest string
		var raw []byte
		if err = rows.Scan(&id, &digest, &raw); err != nil {
			return nil, err
		}
		total += len(raw)
		if total > maxAccountingPageBytes {
			return nil, ErrDataIntegrity
		}
		f, err := accounting.DecodeFillV1(raw)
		if err != nil || f.SourceFillID != id || f.PoolAddress != pool {
			return nil, ErrDataIntegrity
		}
		actual, err := accounting.FillDigest(f)
		if err != nil || string(actual) != digest {
			return nil, ErrDataIntegrity
		}
		result[id] = f
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(ids) {
		return nil, ErrDataIntegrity
	}
	return result, nil
}

type accountingRational struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}
type accountingPriceSource struct {
	SourceFillID  string                    `json:"sourceFillId"`
	Position      accounting.SourcePosition `json:"position"`
	ActualEmitter string                    `json:"actualEmitter"`
	Provenance    accounting.Provenance     `json:"provenance"`
}
type accountingCandle struct {
	PoolAddress       string                `json:"poolAddress"`
	Interval          string                `json:"interval"`
	Start             int64                 `json:"start"`
	Open              accountingRational    `json:"open"`
	High              accountingRational    `json:"high"`
	Low               accountingRational    `json:"low"`
	Close             accountingRational    `json:"close"`
	VolumeUSDLRaw     string                `json:"volumeUsdlRaw"`
	VolumeTokenRaw    string                `json:"volumeTokenRaw"`
	BuyVolumeUSDLRaw  string                `json:"buyVolumeUsdlRaw"`
	SellVolumeUSDLRaw string                `json:"sellVolumeUsdlRaw"`
	Trades            uint64                `json:"trades"`
	Revision          uint64                `json:"revision"`
	Token             accounting.Asset      `json:"token"`
	Settlement        accounting.Asset      `json:"settlement"`
	OpenSource        accountingPriceSource `json:"openSource"`
	HighSource        accountingPriceSource `json:"highSource"`
	LowSource         accountingPriceSource `json:"lowSource"`
	CloseSource       accountingPriceSource `json:"closeSource"`
}

func canonicalPrice(p accountingRational, source accountingPriceSource, f accounting.FillV1) (map[string]any, error) {
	if source.SourceFillID != f.SourceFillID || source.Position != f.Position || source.ActualEmitter != f.ActualEmitter || source.Provenance != f.Provenance {
		return nil, ErrDataIntegrity
	}
	if len(p.Numerator) > 1024 || len(p.Denominator) > 1024 {
		return nil, ErrDataIntegrity
	}
	n, ok := new(big.Int).SetString(p.Numerator, 10)
	if !ok || n.Sign() < 0 {
		return nil, ErrDataIntegrity
	}
	d, ok := new(big.Int).SetString(p.Denominator, 10)
	if !ok || d.Sign() <= 0 || len(p.Denominator) > 1024 {
		return nil, ErrDataIntegrity
	}
	token, _ := accounting.Uint(f.TokenAmountRaw)
	quote, _ := accounting.Uint(f.USDLAmountRaw)
	if token.Sign() == 0 {
		return nil, ErrDataIntegrity
	}
	expected := new(big.Rat).SetFrac(new(big.Int).Mul(quote, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(f.Token.Decimals)), nil)), new(big.Int).Mul(token, big.NewInt(1000000)))
	if expected.Cmp(new(big.Rat).SetFrac(n, d)) != 0 {
		return nil, ErrDataIntegrity
	}
	timestamp, err := strconv.ParseInt(f.BlockTimestampSeconds, 10, 64)
	if err != nil || timestamp > 253402300799 {
		return nil, ErrDataIntegrity
	}
	asset := func(a accounting.Asset) map[string]any {
		return map[string]any{"kind": "erc20", "chainId": "125", "address": a.Address, "currencyCode": nil, "decimals": a.Decimals}
	}
	return map[string]any{"numerator": p.Numerator, "denominator": p.Denominator, "baseAsset": asset(f.Token), "quoteAsset": asset(f.Settlement), "source": map[string]any{"kind": "gross_execution", "amountBasis": "paid_input", "sourceEventId": f.SourceFillID, "position": f.Position, "providerId": nil, "observedAt": time.Unix(timestamp, 0).UTC().Format("2006-01-02T15:04:05.000Z"), "sourceReference": f.Provenance.ReceiptSHA256}}, nil
}
func (s *AccountingReadModel) Candles(ctx context.Context, r AccountingPageRequest) (AccountingPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := validateAccountingRequest(r, true); err != nil {
		return AccountingPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AccountingPage{}, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return AccountingPage{}, err
	}
	out, vector, err := s.snapshot(ctx, tx, r)
	if err != nil {
		return out, err
	}
	after := (r.From + 999) / 1000
	if r.AfterStart != nil {
		if *r.AfterStart*1000 < r.From || *r.AfterStart*1000 >= r.To {
			return out, ErrDataIntegrity
		}
		after = *r.AfterStart
	}
	rows, err := tx.Query(ctx, `SELECT start,snapshot FROM `+pgx.Identifier{s.config.ChartsSchema}.Sanitize()+`.chart_exact_v1 WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4 ORDER BY start ASC LIMIT $5`, r.Pool, r.Interval, after, (r.To+999)/1000, r.Limit+1)
	if err != nil {
		return out, err
	}
	candles := []accountingCandle{}
	ids := []string{}
	seen := map[string]bool{}
	total := 0
	for rows.Next() {
		var raw []byte
		var start int64
		var c accountingCandle
		if err = rows.Scan(&start, &raw); err != nil {
			rows.Close()
			return out, err
		}
		total += len(raw)
		if total > maxAccountingPageBytes || json.Unmarshal(raw, &c) != nil || c.Start != start || c.PoolAddress != r.Pool || c.Interval != r.Interval || c.Trades == 0 || c.Revision == 0 {
			rows.Close()
			return out, ErrDataIntegrity
		}
		candles = append(candles, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(candles) > r.Limit {
		next := candles[r.Limit].Start
		out.NextStart = &next
		candles = candles[:r.Limit]
	}
	for _, c := range candles {
		for _, source := range []accountingPriceSource{c.OpenSource, c.HighSource, c.LowSource, c.CloseSource} {
			if !seen[source.SourceFillID] {
				ids = append(ids, source.SourceFillID)
				seen[source.SourceFillID] = true
			}
		}
	}
	fills, err := s.sourceFills(ctx, tx, r.Pool, ids, vector.sequence)
	if err != nil {
		return out, err
	}
	for _, c := range candles {
		prices := make([]map[string]any, 4)
		sources := []accountingPriceSource{c.OpenSource, c.HighSource, c.LowSource, c.CloseSource}
		for i, p := range []accountingRational{c.Open, c.High, c.Low, c.Close} {
			f, ok := fills[sources[i].SourceFillID]
			if !ok || f.Token != c.Token || f.Settlement != c.Settlement {
				return out, ErrDataIntegrity
			}
			prices[i], err = canonicalPrice(p, sources[i], f)
			if err != nil {
				return out, err
			}
		}
		item := map[string]any{"poolAddress": c.PoolAddress, "interval": c.Interval, "start": time.Unix(c.Start, 0).UTC().Format("2006-01-02T15:04:05.000Z"), "open": prices[0], "high": prices[1], "low": prices[2], "close": prices[3], "volumeUsdlRaw": c.VolumeUSDLRaw, "volumeTokenRaw": c.VolumeTokenRaw, "buyVolumeUsdlRaw": c.BuyVolumeUSDLRaw, "sellVolumeUsdlRaw": c.SellVolumeUSDLRaw, "trades": strconv.FormatUint(c.Trades, 10), "revision": strconv.FormatUint(c.Revision, 10), "synthetic": false, "coverage": "partial", "firstPosition": c.OpenSource.Position, "lastPosition": c.CloseSource.Position, "seriesModelVersion": out.SeriesModelVersion, "snapshotId": out.SnapshotID, "uniqueTraderCount": nil, "largeTradeCount": nil, "largeTradePolicyId": nil, "largeTradeThresholdUsdlRaw": nil}
		raw, err := json.Marshal(item)
		if err != nil {
			return out, err
		}
		if err = interfaces.ValidateJSON("Candle", raw); err != nil {
			return out, ErrDataIntegrity
		}
		out.Items = append(out.Items, raw)
	}
	if err = tx.Commit(ctx); err != nil {
		return AccountingPage{}, err
	}
	return out, nil
}
func (s *AccountingReadModel) Trades(ctx context.Context, r AccountingPageRequest) (AccountingPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := validateAccountingRequest(r, false); err != nil {
		return AccountingPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AccountingPage{}, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return AccountingPage{}, err
	}
	out, vector, err := s.snapshot(ctx, tx, r)
	if err != nil {
		return out, err
	}
	if r.AfterOrder != nil {
		boundaryFills, boundaryErr := s.sourceFills(ctx, tx, r.Pool, []string{r.AfterEventID}, vector.sequence)
		if boundaryErr != nil {
			return out, boundaryErr
		}
		boundary, ok := boundaryFills[r.AfterEventID]
		if !ok || r.AfterOrder.ChainID != boundary.Position.ChainID || r.AfterOrder.Height != boundary.Position.Height || r.AfterOrder.TransactionIndex != boundary.Position.TransactionIndex || r.AfterOrder.LogIndex != boundary.Position.LogIndex {
			return out, ErrDataIntegrity
		}
		r.AfterOrder = &boundary.Position
	}
	if r.AfterSequence != nil && (*r.AfterSequence <= 0 || *r.AfterSequence > vector.sequence) {
		return out, ErrDataIntegrity
	}
	rows, err := tx.Query(ctx, `SELECT fill,source_fact_digest,sequence FROM `+pgx.Identifier{s.config.StatsSchema}.Sanitize()+`.accounting_market_commits_v1 WHERE pool=$1 AND projection_version=$2 AND sequence<=$3 AND ($4::bigint IS NULL OR sequence<$4) AND ($5='' OR fill->>'side'=$5) ORDER BY sequence DESC LIMIT $6`, r.Pool, s.config.ProjectionVersion, vector.sequence, r.AfterSequence, r.Side, r.Limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	total := 0
	lastSequence := int64(0)
	var lastOrder accounting.SourcePosition
	var lastEventID string
	for rows.Next() {
		var raw []byte
		var digest string
		var sequence int64
		if err = rows.Scan(&raw, &digest, &sequence); err != nil {
			return out, err
		}
		total += len(raw)
		if total > maxAccountingPageBytes || sequence < 1 || (lastSequence != 0 && sequence >= lastSequence) {
			return out, ErrDataIntegrity
		}
		lastSequence = sequence
		if len(out.Items) == r.Limit {
			next := sequence + 1
			out.NextSequence = &next
			out.NextOrder = &lastOrder
			out.NextEventID = lastEventID
			break
		}
		f, err := accounting.DecodeFillV1(raw)
		if err != nil || f.PoolAddress != r.Pool {
			return out, ErrDataIntegrity
		}
		if r.AfterOrder != nil {
			cmp, compareErr := accounting.CompareSourcePosition(f.Position, *r.AfterOrder)
			if compareErr != nil || cmp > 0 || cmp == 0 && f.SourceFillID >= r.AfterEventID {
				return out, ErrDataIntegrity
			}
		}
		if lastEventID != "" {
			cmp, compareErr := accounting.CompareSourcePosition(f.Position, lastOrder)
			if compareErr != nil || cmp > 0 || cmp == 0 && f.SourceFillID >= lastEventID {
				return out, ErrDataIntegrity
			}
		}
		lastOrder = f.Position
		lastEventID = f.SourceFillID
		actual, err := accounting.FillDigest(f)
		if err != nil || string(actual) != digest {
			return out, ErrDataIntegrity
		}
		feeAsset := f.Token
		if f.Side == "buy" {
			feeAsset = f.Settlement
		}
		refs := []string{}
		recipientQuality := "unavailable"
		if f.Attribution.Recipient != nil {
			recipientQuality = f.Attribution.Quality
			for _, ref := range f.Attribution.Evidence {
				refs = append(refs, ref.ArtifactSHA256)
			}
		}
		item := map[string]any{"eventId": f.SourceFillID, "poolAddress": f.PoolAddress, "sender": f.Attribution.TransactionSender, "recipient": f.Attribution.Recipient, "side": f.Side, "tokenAmountRaw": f.TokenAmountRaw, "usdlAmountRaw": f.USDLAmountRaw, "position": f.Position, "feeRaw": f.FeeRaw, "feeAssetAddress": feeAsset.Address, "feeAssetDecimals": feeAsset.Decimals, "feeDisposition": f.FeeDisposition, "paidInputRaw": f.PaidInputRaw, "pricedInputRaw": f.PricedInputRaw, "ammInputAfterFeeRaw": f.AMMInputAfterFeeRaw, "inputDerivationQuality": f.InputDerivationQuality, "recipientAttribution": recipientQuality, "recipientEvidenceRefs": refs, "emittingContract": f.ActualEmitter, "codeEpoch": f.Provenance.EmitterCodeEpoch}
		itemRaw, err := json.Marshal(item)
		if err != nil {
			return out, err
		}
		if err = interfaces.ValidateJSON("Trade", itemRaw); err != nil {
			return out, ErrDataIntegrity
		}
		out.Items = append(out.Items, itemRaw)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AccountingPage{}, err
	}
	return out, nil
}

type PublicProjection struct {
	Topic      string          `json:"topic"`
	Sequence   string          `json:"sequence"`
	Revision   string          `json:"revision"`
	Watermark  json.RawMessage `json:"watermark"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt string          `json:"occurredAt"`
}

func (s *AccountingReadModel) PublicProjections(ctx context.Context, topics []string) ([]PublicProjection, error) {
	if s == nil || !accountingSchema.MatchString(s.config.StreamSchema) || len(topics) < 1 || len(topics) > 100 {
		return nil, ErrAccountingNotReady
	}
	for _, topic := range topics {
		if len(topic) > 200 || !(regexp.MustCompile(`^market\.stats\.0x[0-9a-f]{40}$`).MatchString(topic) || regexp.MustCompile(`^market\.rankings\.(trending|breakout|volume|gainers|losers|movers|unusual|new)$`).MatchString(topic)) {
			return nil, ErrDataIntegrity
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT topic,sequence::text,frame FROM "+pgx.Identifier{s.config.StreamSchema, "stream_projection"}.Sanitize()+" WHERE topic=ANY($1) ORDER BY topic", topics)
	if err != nil {
		return nil, err
	}
	out := []PublicProjection{}
	total := 0
	for rows.Next() {
		var topic, sequence string
		var raw []byte
		var p PublicProjection
		if err = rows.Scan(&topic, &sequence, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		total += len(raw)
		name := "StatsEvent"
		if regexp.MustCompile(`^market\.rankings\.`).MatchString(topic) {
			name = "RankingEvent"
		}
		if total > PublicJSONMaxBytes || interfaces.ValidateJSON(name, raw) != nil || json.Unmarshal(raw, &p) != nil || p.Topic != topic || p.Sequence != sequence || p.Watermark == nil || string(p.Watermark) == "null" || p.Revision == "" {
			rows.Close()
			return nil, ErrDataIntegrity
		}
		if name == "StatsEvent" {
			var stats struct {
				Pool      string        `json:"poolAddress"`
				Watermark ReadWatermark `json:"watermark"`
			}
			var watermark ReadWatermark
			if json.Unmarshal(p.Payload, &stats) != nil || json.Unmarshal(p.Watermark, &watermark) != nil || "market.stats."+stats.Pool != topic || !sameWatermark(stats.Watermark, watermark) {
				rows.Close()
				return nil, ErrDataIntegrity
			}
		}
		if name == "StatsEvent" {
			if err = validatePublicStats(p); err != nil {
				rows.Close()
				return nil, err
			}
		}
		out = append(out, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *AccountingReadModel) TopicSnapshotAt(ctx context.Context, topics []string, offsets map[string]string, snapshotID, cursor string) (json.RawMessage, error) {
	if s == nil || !accountingSchema.MatchString(s.config.StreamSchema) || len(offsets) < 1 || len(offsets) > 20 {
		return nil, ErrAccountingNotReady
	}
	if _, err := uuid.Parse(snapshotID); err != nil {
		return nil, ErrDataIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return nil, err
	}
	entries := []any{}
	total := 0
	allowed := map[string]bool{}
	for _, topic := range topics {
		allowed[topic] = true
	}
	partitions := []string{}
	for partition := range offsets {
		partitions = append(partitions, partition)
	}
	sort.Strings(partitions)
	for _, partition := range partitions {
		sequence := offsets[partition]
		id, parseErr := strconv.ParseInt(partition, 10, 64)
		seq, seqErr := strconv.ParseInt(sequence, 10, 64)
		if parseErr != nil || seqErr != nil || id < 1 || seq < 1 {
			return nil, ErrDataIntegrity
		}
		var raw []byte
		var p PublicProjection
		if err = tx.QueryRow(ctx, "SELECT frame FROM "+pgx.Identifier{s.config.StreamSchema, "stream_delivery_outbox"}.Sanitize()+" WHERE partition_id=$1 AND sequence=$2", id, seq).Scan(&raw); err != nil {
			return nil, err
		}
		total += len(raw)
		if total > PublicJSONMaxBytes || interfaces.ValidateJSON("StreamEvent", raw) != nil || json.Unmarshal(raw, &p) != nil || p.Sequence != sequence || !allowed[p.Topic] || validateSnapshotFrame(p, raw) != nil {
			return nil, ErrDataIntegrity
		}
		entries = append(entries, map[string]any{"topic": p.Topic, "sequence": p.Sequence, "revision": p.Revision, "watermark": p.Watermark, "payload": p.Payload})
	}
	var observed time.Time
	if err = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&observed); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"snapshotId": snapshotID, "resumeCursor": cursor, "generatedAt": observed.UTC().Format("2006-01-02T15:04:05.000Z"), "topics": entries})
	if err != nil || len(raw) > PublicJSONMaxBytes || interfaces.ValidateJSON("TopicSnapshot", raw) != nil {
		return nil, ErrDataIntegrity
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

func rollbackRead(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func validatePublicStats(p PublicProjection) error {
	var stats struct {
		Pool        string                    `json:"poolAddress"`
		RealBalance string                    `json:"realUsdlBalanceRaw"`
		Buys        string                    `json:"buys24h"`
		Sells       string                    `json:"sells24h"`
		Holders     *string                   `json:"holderCount"`
		Start       string                    `json:"windowStart"`
		End         string                    `json:"windowEnd"`
		Upper       accounting.SourcePosition `json:"sourceUpperPosition"`
		Watermark   ReadWatermark             `json:"watermark"`
		// Interface 2.2.0 additive fields; absent from frames committed earlier.
		TxCount       *string `json:"txCount24h"`
		UniqueTraders *string `json:"uniqueTraders24h"`
		Supply        *string `json:"totalSupplyRaw"`
		Circulating   *string `json:"circulatingSupplyRaw"`
	}
	if json.Unmarshal(p.Payload, &stats) != nil {
		return ErrDataIntegrity
	}
	for _, value := range []string{stats.RealBalance, stats.Buys, stats.Sells} {
		if _, err := ParseUInt256(value); err != nil {
			return ErrDataIntegrity
		}
	}
	// The window counts are one fold of the committed fills: every fill is a
	// buy or a sell, and each distinct trader made at least one of them.
	if stats.TxCount != nil {
		count, _ := new(big.Int).SetString(*stats.TxCount, 10)
		buys, _ := new(big.Int).SetString(stats.Buys, 10)
		sells, _ := new(big.Int).SetString(stats.Sells, 10)
		if count == nil || count.Cmp(new(big.Int).Add(buys, sells)) != 0 {
			return ErrDataIntegrity
		}
		if stats.UniqueTraders != nil {
			traders, _ := new(big.Int).SetString(*stats.UniqueTraders, 10)
			if traders == nil || traders.Cmp(count) > 0 {
				return ErrDataIntegrity
			}
		}
	}
	if stats.Supply != nil && stats.Circulating != nil {
		supply, _ := new(big.Int).SetString(*stats.Supply, 10)
		circulating, _ := new(big.Int).SetString(*stats.Circulating, 10)
		if supply == nil || circulating == nil || circulating.Cmp(supply) > 0 {
			return ErrDataIntegrity
		}
	}
	if stats.Holders != nil {
		if _, err := ParseUInt256(*stats.Holders); err != nil {
			return ErrDataIntegrity
		}
	}
	start, err := time.Parse("2006-01-02T15:04:05.000Z", stats.Start)
	if err != nil {
		return ErrDataIntegrity
	}
	end, err := time.Parse("2006-01-02T15:04:05.000Z", stats.End)
	if err != nil || end.Sub(start) != 24*time.Hour || stats.Upper.ChainID != "125" || stats.Upper.Height != string(stats.Watermark.Height) || stats.Upper.BlockHash != stats.Watermark.BlockHash || p.Revision != string(stats.Watermark.ProjectionRevision) {
		return ErrDataIntegrity
	}
	return nil
}

func validateSnapshotFrame(p PublicProjection, raw []byte) error {
	parts := strings.Split(p.Topic, ".")
	if len(parts) < 3 {
		return ErrDataIntegrity
	}
	families := map[string]string{"market.stats": "StatsEvent", "market.trades": "MarketTradesEvent", "market.price": "MarketPriceEvent", "market.candles": "CandleEvent", "market.rankings": "RankingEvent", "private.notifications": "NotificationsEvent", "private.orders": "OrdersEvent", "private.portfolio": "PortfolioEvent", "private.rewards": "RewardsEvent"}
	name := families[parts[0]+"."+parts[1]]
	if name == "" {
		if parts[0] == "streams" && parts[2] == "status" {
			name = "StreamsEvent"
		} else if parts[0] == "bounties" && parts[2] == "updates" {
			name = "BountiesEvent"
		} else {
			return ErrDataIntegrity
		}
	}
	if interfaces.ValidateJSON(name, raw) != nil {
		return ErrDataIntegrity
	}
	if name == "StatsEvent" {
		return validatePublicStats(p)
	}
	return nil
}
