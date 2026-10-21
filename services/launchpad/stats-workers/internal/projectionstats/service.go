package projectionstats

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrUnavailable = errors.New("canonical stats unavailable")
var ErrNotFound = errors.New("canonical pool not found")

const Phase2DDL = `CREATE INDEX ON %[1]s.trade_facts(height,transaction_index,log_index);CREATE TABLE %[1]s.phase2_read_snapshots(id uuid PRIMARY KEY,pool text NOT NULL,side text NOT NULL,projection_version text NOT NULL,watermark jsonb NOT NULL,expires_at timestamptz NOT NULL);CREATE INDEX ON %[1]s.phase2_read_snapshots(expires_at);CREATE TABLE %[1]s.phase2_windows(pool text NOT NULL,seconds bigint NOT NULL,projection_version text NOT NULL,observed_at bigint NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(pool,seconds,projection_version));`

func (r *Repository) Phase2MigrationSQL() string { return fmt.Sprintf(Phase2DDL, r.schema) }

type ReadConfig struct {
	SourceSchema, SourceStream, ProjectionVersion string
	CursorKey                                     []byte
	CursorTTL                                     time.Duration
}
type ReadService struct {
	repository *Repository
	config     ReadConfig
}

func NewReadService(r *Repository, c ReadConfig) (*ReadService, error) {
	if r == nil || !schemaName.MatchString(c.SourceSchema) || c.SourceStream == "" || len(c.SourceStream) > 128 || c.ProjectionVersion == "" || len(c.ProjectionVersion) > 64 || len(c.CursorKey) < 32 || len(c.CursorKey) > 64 || c.CursorTTL < time.Minute || c.CursorTTL > time.Hour {
		return nil, ErrInvalid
	}
	c.CursorKey = append([]byte(nil), c.CursorKey...)
	return &ReadService{r, c}, nil
}
func (s *ReadService) source(name string) string {
	return pgx.Identifier{s.config.SourceSchema, name}.Sanitize()
}

type CanonicalWatermark struct {
	ChainID           string `json:"chainId"`
	SourceStream      string `json:"sourceStream"`
	Height            string `json:"height"`
	BlockHash         string `json:"blockHash"`
	ProjectionVersion string `json:"projectionVersion"`
	ComputedAt        int64  `json:"computedAt"`
}

func (s *ReadService) coherentTx(ctx context.Context, tx pgx.Tx, pool string, now int64) (CanonicalWatermark, error) {
	var w CanonicalWatermark
	var raw, typed int64
	var alive bool
	if now < 0 {
		return w, ErrInvalid
	}
	err := tx.QueryRow(ctx, "SELECT w.raw_complete_height,w.typed_complete_height,l.expires_at>clock_timestamp() FROM "+s.source("indexer_ingest_watermarks_v1")+" w JOIN "+s.source("indexer_ingest_leases_v1")+" l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1", s.config.SourceStream).Scan(&raw, &typed, &alive)
	if err != nil {
		return w, err
	}
	if raw < 0 || raw != typed || !alive {
		return w, ErrUnavailable
	}
	if pool != "" {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.source("indexer_canonical_creations_v1")+" WHERE pool=$1 AND height<=$2)", pool, typed).Scan(&exists)
		if err != nil {
			return w, err
		}
		if !exists {
			return w, ErrNotFound
		}
	}
	var complete bool
	err = tx.QueryRow(ctx, "SELECT block_hash,fetch_complete AND required_decode_complete FROM "+s.source("indexer_block_completeness_v1")+" WHERE chain_id=125 AND stream_id=$1 AND height=$2", s.config.SourceStream, typed).Scan(&w.BlockHash, &complete)
	if err != nil {
		return w, err
	}
	if !complete || !hash.MatchString(w.BlockHash) {
		return w, ErrUnavailable
	}
	var frozen bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.repository.schema+".accounting_pool_fences WHERE frozen AND ($1='' OR pool=$1))", pool).Scan(&frozen); err != nil {
		return w, err
	}
	if frozen {
		return w, ErrIntegrity
	}
	var healthy bool
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT NOT EXISTS(SELECT 1 FROM %s f WHERE f.chain_id=125 AND (f.payload->'position'->>'height')::numeric<=$2 AND ($3='' OR f.pool_address=$3) AND NOT EXISTS(SELECT 1 FROM %s.accounting_market_commits c WHERE c.projection_version=$1 AND c.source_fill_id=f.source_fill_id AND c.source_fact_digest=f.source_fact_digest AND c.fill=f.payload)) AND NOT EXISTS(SELECT 1 FROM %s.accounting_pool_fences WHERE frozen AND ($3='' OR pool=$3)) AND NOT EXISTS(SELECT 1 FROM %s.accounting_market_commits c WHERE c.projection_version=$1 AND (c.fill->'position'->>'height')::numeric<=$2 AND ($3='' OR c.pool=$3) AND NOT EXISTS(SELECT 1 FROM %s f WHERE f.chain_id=125 AND f.source_fill_id=c.source_fill_id AND f.source_fact_digest=c.source_fact_digest AND f.payload=c.fill))`, s.source("indexer_accounting_fills_v1"), s.repository.schema, s.repository.schema, s.repository.schema, s.source("indexer_accounting_fills_v1")), s.config.ProjectionVersion, typed, pool).Scan(&healthy)
	if err != nil {
		return w, err
	}
	if !healthy {
		return w, ErrUnavailable
	}
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT NOT EXISTS(SELECT 1 FROM %[1]s.accounting_market_commits c LEFT JOIN %[1]s.trade_facts t ON t.event_id='125:'||(c.fill->'position'->>'transactionHash')||':'||(c.fill->'position'->>'logIndex') AND t.projection_version=c.projection_version WHERE c.projection_version=$1 AND (c.fill->'position'->>'height')::numeric<=$2 AND ($3='' OR c.pool=$3) AND (t.event_id IS NULL OR t.pool IS DISTINCT FROM c.pool OR t.height IS DISTINCT FROM (c.fill->'position'->>'height')::numeric OR t.transaction_index IS DISTINCT FROM (c.fill->'position'->>'transactionIndex')::numeric OR t.log_index IS DISTINCT FROM (c.fill->'position'->>'logIndex')::numeric OR t.block_timestamp IS DISTINCT FROM (c.fill->>'blockTimestampSeconds')::bigint OR t.volume::text IS DISTINCT FROM c.fill->>'usdlAmountRaw' OR t.fee::text IS DISTINCT FROM c.fill->>'feeRaw' OR t.side IS DISTINCT FROM c.fill->>'side' OR t.actor IS DISTINCT FROM c.fill->'attribution'->>'transactionSender' OR t.fee_asset IS DISTINCT FROM CASE WHEN c.fill->>'side'='buy' THEN c.fill->'settlement'->>'address' ELSE c.fill->'token'->>'address' END OR t.fee_decimals IS DISTINCT FROM CASE WHEN c.fill->>'side'='buy' THEN (c.fill->'settlement'->>'decimals')::integer ELSE (c.fill->'token'->>'decimals')::integer END OR t.disposition IS DISTINCT FROM c.fill->>'feeDisposition'))`, s.repository.schema), s.config.ProjectionVersion, typed, pool).Scan(&healthy)
	if err != nil {
		return w, err
	}
	if !healthy {
		return w, ErrIntegrity
	}
	w.ChainID = "125"
	w.SourceStream = s.config.SourceStream
	w.Height = fmt.Sprint(typed)
	w.ProjectionVersion = s.config.ProjectionVersion
	w.ComputedAt = now
	return w, nil
}

type CanonicalTrade struct {
	Trade
	TransactionOrigin string  `json:"transactionOrigin"`
	RouterCaller      *string `json:"routerCaller"`
}
type SnapshotPage struct {
	Trades           []CanonicalTrade   `json:"trades"`
	NextCursor       *string            `json:"nextCursor"`
	Watermark        CanonicalWatermark `json:"watermark"`
	TraderPopulation string             `json:"traderPopulation"`
}

func (s *ReadService) encodeCompact(f Filter, id uuid.UUID, p Position) (string, error) {
	if f.Validate() != nil || p.Validate() != nil {
		return "", ErrInvalid
	}
	b := make([]byte, 1+16+96+16)
	b[0] = 2
	copy(b[1:17], id[:])
	offset := 17
	for _, v := range []string{p.Height, p.TransactionIndex, p.LogIndex} {
		n, err := Uint(v)
		if err != nil {
			return "", err
		}
		n.FillBytes(b[offset : offset+32])
		offset += 32
	}
	digest, err := hex.DecodeString(f.digest())
	if err != nil {
		return "", err
	}
	copy(b[offset:], digest[:16])
	mac := hmac.New(sha256.New, s.config.CursorKey)
	mac.Write(b)
	b = append(b, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *ReadService) decodeCompact(f Filter, token string) (uuid.UUID, []string, error) {
	var id uuid.UUID
	if len(token) > 256 || f.Validate() != nil {
		return id, nil, ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 161 || b[0] != 2 {
		return id, nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.config.CursorKey)
	mac.Write(b[:129])
	if !hmac.Equal(mac.Sum(nil), b[129:]) {
		return id, nil, ErrInvalid
	}
	digest, err := hex.DecodeString(f.digest())
	if err != nil || !hmac.Equal(b[113:129], digest[:16]) {
		return id, nil, ErrInvalid
	}
	copy(id[:], b[1:17])
	values := make([]string, 3)
	for i := range values {
		values[i] = new(big.Int).SetBytes(b[17+i*32 : 49+i*32]).String()
	}
	return id, values, nil
}
func (s *ReadService) Trades(ctx context.Context, pool, side string, limit int, cursor string, now int64) (SnapshotPage, error) {
	out := SnapshotPage{Trades: []CanonicalTrade{}, TraderPopulation: "sender_actor"}
	f := Filter{pool, side, s.config.ProjectionVersion}
	if f.Validate() != nil || limit < 1 || limit > 100 || now < 0 {
		return out, ErrInvalid
	}
	tx, err := s.repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	id := uuid.New()
	var position []string
	if cursor == "" {
		out.Watermark, err = s.coherentTx(ctx, tx, pool, now)
		if err != nil {
			return out, err
		}
		raw, err := json.Marshal(out.Watermark)
		if err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO "+s.repository.schema+".phase2_read_snapshots VALUES($1,$2,$3,$4,$5,to_timestamp($6::bigint::double precision)+$7::bigint*interval '1 second')", id, pool, side, s.config.ProjectionVersion, raw, now, int64(s.config.CursorTTL/time.Second)); err != nil {
			return out, err
		}
	} else {
		id, position, err = s.decodeCompact(f, cursor)
		if err != nil {
			return out, err
		}
		var raw []byte
		err = tx.QueryRow(ctx, "SELECT watermark FROM "+s.repository.schema+".phase2_read_snapshots WHERE id=$1 AND pool=$2 AND side=$3 AND projection_version=$4 AND expires_at>to_timestamp($5::bigint::double precision)", id, pool, side, s.config.ProjectionVersion, now).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrInvalid
		}
		if err != nil {
			return out, err
		}
		if json.Unmarshal(raw, &out.Watermark) != nil || out.Watermark.ChainID != "125" || out.Watermark.SourceStream != s.config.SourceStream || out.Watermark.ProjectionVersion != s.config.ProjectionVersion || !hash.MatchString(out.Watermark.BlockHash) {
			return out, ErrIntegrity
		}
		if _, err = Uint(out.Watermark.Height); err != nil {
			return out, ErrIntegrity
		}
	}
	var healthy bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.source("indexer_block_completeness_v1")+" WHERE chain_id=125 AND stream_id=$1 AND height=$2::bigint AND block_hash=$3 AND fetch_complete AND required_decode_complete) AND NOT EXISTS(SELECT 1 FROM "+s.repository.schema+".accounting_pool_fences WHERE pool=$4 AND frozen)", s.config.SourceStream, out.Watermark.Height, out.Watermark.BlockHash, pool).Scan(&healthy)
	if err != nil {
		return out, err
	}
	if !healthy {
		return out, ErrIntegrity
	}
	args := []any{pool, s.config.ProjectionVersion, side, out.Watermark.Height, limit + 1}
	predicate := ""
	if len(position) > 0 {
		predicate = " AND (t.height,t.transaction_index,t.log_index)<($6::numeric,$7::numeric,$8::numeric)"
		for _, v := range position {
			args = append(args, v)
		}
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT t.payload,c.fill FROM %s.trade_facts t JOIN %s.accounting_market_commits c ON c.projection_version=t.projection_version AND c.pool=t.pool AND c.fill->'position'->>'transactionHash'=t.payload->'position'->>'transactionHash' AND c.fill->'position'->>'blockHash'=t.payload->'position'->>'blockHash' AND (c.fill->'position'->>'height')::numeric=t.height AND (c.fill->'position'->>'transactionIndex')::numeric=t.transaction_index AND (c.fill->'position'->>'logIndex')::numeric=t.log_index WHERE t.pool=$1 AND t.projection_version=$2 AND ($3='' OR t.side=$3) AND t.height<=$4::numeric %s ORDER BY t.height DESC,t.transaction_index DESC,t.log_index DESC LIMIT $5`, s.repository.schema, s.repository.schema, predicate), args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw, original []byte
		var trade Trade
		if err = rows.Scan(&raw, &original); err != nil {
			rows.Close()
			return out, err
		}
		if json.Unmarshal(raw, &trade) != nil {
			rows.Close()
			return out, ErrIntegrity
		}
		fill, decodeErr := accounting.DecodeFillV1(original)
		if decodeErr != nil {
			rows.Close()
			return out, ErrIntegrity
		}
		expected, decodeErr := qualifiedInput(fill, "stats-read", s.config.ProjectionVersion)
		if decodeErr != nil || Digest(expected.Trade) != Digest(trade) {
			rows.Close()
			return out, ErrIntegrity
		}
		out.Trades = append(out.Trades, CanonicalTrade{Trade: trade, TransactionOrigin: fill.Attribution.TransactionSender, RouterCaller: fill.Attribution.RouterCaller})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Trades) > limit {
		out.Trades = out.Trades[:limit]
		value, err := s.encodeCompact(f, id, out.Trades[limit-1].Position)
		if err != nil {
			return out, err
		}
		out.NextCursor = &value
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

type CanonicalAssetFee struct {
	AssetFee
	ChainID                string  `json:"chainId"`
	FeeType                string  `json:"feeType"`
	QuoteValue             *string `json:"quoteValue"`
	QuoteUnavailableReason string  `json:"quoteUnavailableReason"`
}
type WindowSnapshot struct {
	PoolAddress                 string              `json:"poolAddress"`
	VolumeUSDLRaw               string              `json:"venueVolumeUsdlRaw"`
	PoolFillCount               string              `json:"poolFillCount"`
	UniqueActors                string              `json:"uniqueActors"`
	TraderPopulation            string              `json:"traderPopulation"`
	UserActionCount             *string             `json:"userActionCount"`
	UserActionUnavailableReason string              `json:"userActionUnavailableReason"`
	Fees                        []CanonicalAssetFee `json:"tradingFees"`
	WindowStart                 int64               `json:"windowStart"`
	WindowEnd                   int64               `json:"windowEnd"`
	ComputedAt                  int64               `json:"computedAt"`
	Watermark                   CanonicalWatermark  `json:"watermark"`
	Stale                       bool                `json:"stale"`
}

func (s *ReadService) Window(ctx context.Context, pool string, seconds, now int64) (WindowSnapshot, error) {
	var out WindowSnapshot
	if pool != "" && !address.MatchString(pool) || seconds < 1 || seconds > 86400 || now < 0 {
		return out, ErrInvalid
	}
	tx, err := s.repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	w, err := s.coherentTx(ctx, tx, pool, now)
	if err != nil {
		return out, err
	}
	out = WindowSnapshot{PoolAddress: pool, TraderPopulation: "sender_actor", UserActionUnavailableReason: "qualified_route_identity_unavailable", Fees: []CanonicalAssetFee{}, WindowStart: now - seconds, WindowEnd: now, ComputedAt: now, Watermark: w}
	args := []any{s.config.ProjectionVersion, w.Height, out.WindowStart, now, pool}
	where := `t.projection_version=$1 AND t.height<=$2::numeric AND t.block_timestamp>$3 AND t.block_timestamp<=$4 AND ($5='' OR t.pool=$5)`
	join := fmt.Sprintf(` FROM %s.trade_facts t JOIN %s.accounting_market_commits c ON c.projection_version=t.projection_version AND c.pool=t.pool AND c.fill->'position'->>'transactionHash'=t.payload->'position'->>'transactionHash' AND c.fill->'position'->>'blockHash'=t.payload->'position'->>'blockHash' AND (c.fill->'position'->>'height')::numeric=t.height AND (c.fill->'position'->>'transactionIndex')::numeric=t.transaction_index AND (c.fill->'position'->>'logIndex')::numeric=t.log_index JOIN %s p ON p.pool=t.pool AND p.height<=t.height WHERE `, s.repository.schema, s.repository.schema, s.source("indexer_canonical_creations_v1"))
	err = tx.QueryRow(ctx, "SELECT COALESCE(SUM(t.volume),0)::text,COUNT(*)::text,COUNT(DISTINCT t.actor)::text"+join+where, args...).Scan(&out.VolumeUSDLRaw, &out.PoolFillCount, &out.UniqueActors)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, "SELECT t.fee_asset,t.fee_decimals,t.disposition,SUM(t.fee)::text"+join+where+" GROUP BY t.fee_asset,t.fee_decimals,t.disposition ORDER BY t.fee_asset,t.fee_decimals,t.disposition", args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		fee := CanonicalAssetFee{ChainID: "125", FeeType: "trading_fee", QuoteUnavailableReason: "valuation_source_unavailable"}
		if err = rows.Scan(&fee.Asset, &fee.Decimals, &fee.Disposition, &fee.Raw); err != nil {
			rows.Close()
			return out, err
		}
		out.Fees = append(out.Fees, fee)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = out.validate(s.config, pool, seconds); err != nil {
		return out, err
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+s.repository.schema+".phase2_windows AS existing VALUES($1,$2,$3,$4,$5) ON CONFLICT(pool,seconds,projection_version) DO UPDATE SET observed_at=excluded.observed_at,payload=excluded.payload WHERE existing.observed_at<=excluded.observed_at", pool, seconds, s.config.ProjectionVersion, now, payload)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
func (s *ReadService) LastWindow(ctx context.Context, pool string, seconds int64) (WindowSnapshot, error) {
	var out WindowSnapshot
	if pool != "" && !address.MatchString(pool) || seconds < 1 || seconds > 86400 {
		return out, ErrInvalid
	}
	var raw []byte
	var frozen bool
	err := s.repository.pool.QueryRow(ctx, "SELECT payload,EXISTS(SELECT 1 FROM "+s.repository.schema+".accounting_pool_fences WHERE frozen AND ($1='' OR pool=$1)) FROM "+s.repository.schema+".phase2_windows WHERE pool=$1 AND seconds=$2 AND projection_version=$3", pool, seconds, s.config.ProjectionVersion).Scan(&raw, &frozen)
	if err != nil {
		return out, err
	}
	if frozen || json.Unmarshal(raw, &out) != nil || out.validate(s.config, pool, seconds) != nil {
		return out, ErrIntegrity
	}
	out.Stale = true
	return out, nil
}

func (s *ReadService) PruneExpiredSnapshots(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	tag, err := s.repository.pool.Exec(ctx, "DELETE FROM "+s.repository.schema+".phase2_read_snapshots WHERE id IN (SELECT id FROM "+s.repository.schema+".phase2_read_snapshots WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT $1 FOR UPDATE SKIP LOCKED)", limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (w WindowSnapshot) validate(config ReadConfig, pool string, seconds int64) error {
	if w.PoolAddress != pool || w.Watermark.ChainID != "125" || w.Watermark.SourceStream != config.SourceStream || w.Watermark.ProjectionVersion != config.ProjectionVersion || !hash.MatchString(w.Watermark.BlockHash) || w.WindowEnd < 0 || w.WindowStart != w.WindowEnd-seconds || w.ComputedAt != w.WindowEnd || w.Watermark.ComputedAt != w.ComputedAt || w.TraderPopulation != "sender_actor" || w.UserActionCount != nil || w.UserActionUnavailableReason == "" || w.Fees == nil {
		return ErrIntegrity
	}
	if _, err := Uint(w.Watermark.Height); err != nil {
		return ErrIntegrity
	}
	for _, amount := range []string{w.VolumeUSDLRaw, w.PoolFillCount, w.UniqueActors} {
		if len(amount) > 156 || !digits.MatchString(amount) {
			return ErrIntegrity
		}
	}
	actors, _ := new(big.Int).SetString(w.UniqueActors, 10)
	fills, _ := new(big.Int).SetString(w.PoolFillCount, 10)
	if actors.Cmp(fills) > 0 {
		return ErrIntegrity
	}
	seen := map[string]bool{}
	for _, f := range w.Fees {
		key := strings.Join([]string{f.Asset, fmt.Sprint(f.Decimals), f.Disposition}, ":")
		if seen[key] || !address.MatchString(f.Asset) || f.Decimals < 0 || f.Decimals > 255 || f.ChainID != "125" || f.FeeType != "trading_fee" || (f.Disposition != "distributed_usdl" && f.Disposition != "retained_token_reserve") || len(f.Raw) > 156 || !digits.MatchString(f.Raw) || f.QuoteValue != nil || f.QuoteUnavailableReason == "" {
			return ErrIntegrity
		}
		seen[key] = true
	}
	return nil
}
