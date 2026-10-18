package projectionstats

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"regexp"
	"sort"
	"strconv"
	"time"

	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DDL is a proposed stats-owned migration, executed only in private test
// namespaces until the serial migration coordinator registers it.
const DDL = `
CREATE TABLE %[1]s.canonical_blocks (height numeric(78,0) PRIMARY KEY,block_hash text NOT NULL UNIQUE,block_timestamp bigint NOT NULL);
CREATE TABLE %[1]s.canonical_transactions (transaction_hash text PRIMARY KEY,height numeric(78,0) NOT NULL REFERENCES %[1]s.canonical_blocks(height),transaction_index numeric(78,0) NOT NULL, UNIQUE(height,transaction_index));
CREATE TABLE %[1]s.canonical_positions (height numeric(78,0) NOT NULL,transaction_index numeric(78,0) NOT NULL,log_index numeric(78,0) NOT NULL,transaction_hash text NOT NULL REFERENCES %[1]s.canonical_transactions(transaction_hash),digest text NOT NULL,PRIMARY KEY(height,transaction_index,log_index),UNIQUE(transaction_hash,log_index));
CREATE TABLE %[1]s.canonical_pool_assets (pool text PRIMARY KEY,usdl text NOT NULL,token text NOT NULL,token_decimals integer NOT NULL);
CREATE TABLE %[1]s.inbox (
 chain_id text NOT NULL CHECK(chain_id='125'),transaction_hash text NOT NULL,log_index numeric(78,0) NOT NULL,
 consumer_version text NOT NULL,digest text NOT NULL,revision bigint,
 PRIMARY KEY(chain_id,transaction_hash,log_index,consumer_version));
CREATE TABLE %[1]s.trade_facts (
 event_id text NOT NULL,projection_version text NOT NULL,pool text NOT NULL,actor text NOT NULL,
 height numeric(78,0) NOT NULL,transaction_index numeric(78,0) NOT NULL,log_index numeric(78,0) NOT NULL,
 block_timestamp bigint NOT NULL CHECK(block_timestamp>=0),volume numeric(78,0) NOT NULL CHECK(volume>=0),
 fee numeric(78,0) NOT NULL CHECK(fee>=0),fee_asset text NOT NULL,fee_decimals integer NOT NULL,
 disposition text NOT NULL,side text NOT NULL,payload jsonb NOT NULL,revision bigint NOT NULL,
 PRIMARY KEY(event_id,projection_version));
CREATE INDEX ON %[1]s.trade_facts (projection_version,pool,height DESC,transaction_index DESC,log_index DESC);
CREATE TABLE %[1]s.minute_buckets (
 chain_id text NOT NULL CHECK(chain_id='125'),pool text NOT NULL,interval_seconds integer NOT NULL CHECK(interval_seconds=60),
 start bigint NOT NULL,projection_version text NOT NULL,volume numeric(156,0) NOT NULL CHECK(volume>=0),
 buys bigint NOT NULL,sells bigint NOT NULL,revision bigint NOT NULL,
 close_height numeric(78,0) NOT NULL,close_transaction_index numeric(78,0) NOT NULL,close_log_index numeric(78,0) NOT NULL,
 close_payload jsonb NOT NULL,
 PRIMARY KEY(chain_id,pool,interval_seconds,start,projection_version));
CREATE TABLE %[1]s.fee_groups (
 chain_id text NOT NULL CHECK(chain_id='125'),pool text NOT NULL,start bigint NOT NULL,projection_version text NOT NULL,
 asset text NOT NULL,decimals integer NOT NULL,fee_type text NOT NULL,disposition text NOT NULL,
 raw numeric(156,0) NOT NULL CHECK(raw>=0),revision bigint NOT NULL,
 PRIMARY KEY(chain_id,pool,start,projection_version,asset,decimals,fee_type,disposition));
CREATE TABLE %[1]s.outbox (
 event_id text NOT NULL,projection_version text NOT NULL,revision bigint NOT NULL,payload jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0,published boolean NOT NULL DEFAULT false,
 PRIMARY KEY(event_id,projection_version));
CREATE TABLE %[1]s.accounting_pool_fences (pool text PRIMARY KEY,frozen boolean NOT NULL DEFAULT false,evidence_digest text);
CREATE TABLE %[1]s.accounting_market_commits (
 publication_id text PRIMARY KEY,source_fill_id text NOT NULL,source_fact_digest text NOT NULL,
 projection_version text NOT NULL,pool text NOT NULL,bucket_start bigint NOT NULL,
 position jsonb NOT NULL,fill jsonb NOT NULL,sequence bigint NOT NULL CHECK(sequence>0),revision bigint NOT NULL,
 UNIQUE(source_fill_id,projection_version),UNIQUE(projection_version,pool,sequence));
CREATE TABLE %[1]s.accounting_inventory_facets (
 publication_id text PRIMARY KEY,source_fill_id text NOT NULL,source_fact_digest text NOT NULL,
 projection_version text NOT NULL,partition_key text NOT NULL,position jsonb NOT NULL,payload jsonb NOT NULL);`

var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type Repository struct {
	pool   *pgxpool.Pool
	schema string
}

func NewRepository(pool *pgxpool.Pool, schema string) (*Repository, error) {
	if pool == nil || !schemaName.MatchString(schema) {
		return nil, ErrInvalid
	}
	return &Repository{pool, pgx.Identifier{schema}.Sanitize()}, nil
}
func (r *Repository) MigrationSQL() string {
	return fmt.Sprintf(DDL, r.schema)
}

type ApplyInput struct {
	Trade                                               Trade
	Assets                                              PoolAssets
	FeeBPS                                              int
	BlockTimestamp, WatermarkTimestamp, FutureTolerance int64
	ConsumerVersion, ProjectionVersion                  string
}
type ApplyResult struct {
	Accepted bool
	Revision int64
}

// Apply commits inbox, immutable trade, bucket, grouped fee and notification
// together. A duplicate is acknowledged only after digest comparison. Broker
// adapters must acknowledge only after this call succeeds.
func (r *Repository) Apply(ctx context.Context, in ApplyInput) (ApplyResult, error) {
	accounting, e := Account(in.Trade, in.Assets, in.FeeBPS)
	if e != nil {
		return ApplyResult{}, e
	}
	return r.applyAccounted(ctx, in, accounting)
}

func (r *Repository) ApplyAuthorized(ctx context.Context, authority *CoreReceiptAuthority, trade Trade, consumerVersion, projectionVersion string) (ApplyResult, error) {
	accounting, e := authority.Account(trade)
	if e != nil {
		return ApplyResult{}, e
	}
	admitted := authority.admitted[trade.EventID]
	in := ApplyInput{trade, admitted.assets, 0, admitted.timestamp, admitted.timestamp, 0, consumerVersion, projectionVersion}
	return r.applyAccounted(ctx, in, accounting)
}

func (r *Repository) applyAccounted(ctx context.Context, in ApplyInput, accounting Accounting) (ApplyResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	result, err := r.applyAccountedTx(ctx, tx, in, accounting, nil, nil)
	if err != nil {
		return ApplyResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ApplyResult{}, err
	}
	return result, nil
}

func (r *Repository) ApplyTx(ctx context.Context, tx pgx.Tx, in ApplyInput) (ApplyResult, error) {
	if tx == nil {
		return ApplyResult{}, ErrInvalid
	}
	amounts, err := Account(in.Trade, in.Assets, in.FeeBPS)
	if err != nil {
		return ApplyResult{}, err
	}
	return r.applyAccountedTx(ctx, tx, in, amounts, nil, nil)
}

func (r *Repository) applyAccountedTx(ctx context.Context, tx pgx.Tx, in ApplyInput, accounting Accounting, policy *QualifiedAccountingPolicy, batch *statsBatch) (ApplyResult, error) {
	var e error
	if len(in.ConsumerVersion) == 0 || len(in.ConsumerVersion) > 64 || len(in.ProjectionVersion) == 0 || len(in.ProjectionVersion) > 64 {
		return ApplyResult{}, ErrInvalid
	}
	if e = ValidateTime(in.BlockTimestamp, in.WatermarkTimestamp, in.FutureTolerance); e != nil {
		return ApplyResult{}, e
	}
	start, _ := Minute(in.BlockTimestamp)
	t := in.Trade
	payload, e := json.Marshal(t)
	if e != nil {
		return ApplyResult{}, e
	}
	// Digest the timestamp and accounting configuration too: conflicting decoded
	// enrichment must not silently reuse an inbox key.
	full, _ := json.Marshal(struct {
		Trade      Trade
		Timestamp  int64
		Assets     PoolAssets
		BPS        int
		Projection string
	}{t, in.BlockTimestamp, in.Assets, in.FeeBPS, in.ProjectionVersion})
	if policy != nil {
		full, e = json.Marshal(struct {
			Trade      Trade
			Timestamp  int64
			Assets     PoolAssets
			Policy     QualifiedAccountingPolicy
			Projection string
		}{in.Trade, in.BlockTimestamp, in.Assets, *policy, in.ProjectionVersion})
		if e != nil {
			return ApplyResult{}, e
		}
	}
	digest := digestBytes(full)
	tag, e := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s.inbox(chain_id,transaction_hash,log_index,consumer_version,digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, r.schema), t.Position.ChainID, t.Position.TransactionHash, t.Position.LogIndex, in.ConsumerVersion, digest)
	if e != nil {
		return ApplyResult{}, e
	}
	if tag.RowsAffected() == 0 {
		var existing string
		var revision int64
		e = tx.QueryRow(ctx, fmt.Sprintf(`SELECT digest,revision FROM %[1]s.inbox WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3 AND consumer_version=$4`, r.schema), t.Position.ChainID, t.Position.TransactionHash, t.Position.LogIndex, in.ConsumerVersion).Scan(&existing, &revision)
		if e != nil {
			return ApplyResult{}, e
		}
		if existing != digest {
			return ApplyResult{}, ErrIntegrity
		}
		return ApplyResult{false, revision}, nil
	}
	if e = r.guardCanonical(ctx, tx, in); e != nil {
		return ApplyResult{}, e
	}
	buys, sells := 0, 1
	if t.Side == "buy" {
		buys, sells = 1, 0
	}
	var revision int64
	if batch != nil {
		revision, e = batch.enter(ctx, tx, r, in, accounting)
	} else {
		// Revision is pool-minute-local, monotonically incremented under the row lock.
		e = tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %[1]s.minute_buckets AS b VALUES('125',$1,60,$2,$3,$4,$5,$6,1,$7,$8,$9,$10)
 ON CONFLICT(chain_id,pool,interval_seconds,start,projection_version) DO UPDATE SET
 volume=b.volume+excluded.volume,buys=b.buys+excluded.buys,sells=b.sells+excluded.sells,revision=b.revision+1,
 close_payload=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_payload ELSE b.close_payload END,
 close_height=GREATEST(b.close_height,excluded.close_height),
 close_transaction_index=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_transaction_index ELSE b.close_transaction_index END,
 close_log_index=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_log_index ELSE b.close_log_index END RETURNING revision`, r.schema), t.PoolAddress, start, in.ProjectionVersion, accounting.TradedUSDLRaw, buys, sells, t.Position.Height, t.Position.TransactionIndex, t.Position.LogIndex, payload).Scan(&revision)
		if e != nil {
			return ApplyResult{}, e
		}
	}
	if e != nil {
		return ApplyResult{}, e
	}
	_, e = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s.trade_facts VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, r.schema), t.EventID, in.ProjectionVersion, t.PoolAddress, t.Sender, t.Position.Height, t.Position.TransactionIndex, t.Position.LogIndex, in.BlockTimestamp, accounting.TradedUSDLRaw, t.FeeRaw, t.FeeAssetAddress, t.FeeAssetDecimals, t.FeeDisposition, t.Side, payload, revision)
	if e != nil {
		return ApplyResult{}, e
	}
	if batch == nil {
		_, e = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s.fee_groups AS f VALUES('125',$1,$2,$3,$4,$5,'trading_fee',$6,$7,$8)
 ON CONFLICT(chain_id,pool,start,projection_version,asset,decimals,fee_type,disposition) DO UPDATE SET raw=f.raw+excluded.raw,revision=excluded.revision`, r.schema), t.PoolAddress, start, in.ProjectionVersion, t.FeeAssetAddress, t.FeeAssetDecimals, t.FeeDisposition, t.FeeRaw, revision)
		if e != nil {
			return ApplyResult{}, e
		}
	}
	notification, _ := json.Marshal(struct {
		EventID, Pool, ProjectionVersion string
		BucketStart, Revision            int64
	}{t.EventID, t.PoolAddress, in.ProjectionVersion, start, revision})
	_, e = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s.outbox(event_id,projection_version,revision,payload) VALUES($1,$2,$3,$4)`, r.schema), t.EventID, in.ProjectionVersion, revision, notification)
	if e != nil {
		return ApplyResult{}, e
	}
	_, e = tx.Exec(ctx, fmt.Sprintf(`UPDATE %[1]s.inbox SET revision=$1 WHERE chain_id=$2 AND transaction_hash=$3 AND log_index=$4 AND consumer_version=$5`, r.schema), revision, t.Position.ChainID, t.Position.TransactionHash, t.Position.LogIndex, in.ConsumerVersion)
	if e != nil {
		return ApplyResult{}, e
	}
	return ApplyResult{true, revision}, nil
}

// guardCanonical persists chain125 committed-finality identities independently
// of consumer/projection versions. ON CONFLICT plus subsequent comparisons
// serialize concurrent claims without relying on an earlier existence read.
func (r *Repository) guardCanonical(ctx context.Context, tx pgx.Tx, in ApplyInput) error {
	p := in.Trade.Position
	queries := []struct {
		sql  string
		args []any
		read string
		want []string
	}{
		{`INSERT INTO %[1]s.canonical_blocks VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, []any{p.Height, p.BlockHash, in.BlockTimestamp}, `SELECT block_hash,block_timestamp::text FROM %[1]s.canonical_blocks WHERE height=$1`, []string{p.BlockHash, fmt.Sprint(in.BlockTimestamp)}},
		{`INSERT INTO %[1]s.canonical_transactions VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, []any{p.TransactionHash, p.Height, p.TransactionIndex}, `SELECT height::text,transaction_index::text FROM %[1]s.canonical_transactions WHERE transaction_hash=$1`, []string{p.Height, p.TransactionIndex}},
		{`INSERT INTO %[1]s.canonical_positions VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, []any{p.Height, p.TransactionIndex, p.LogIndex, p.TransactionHash, EmittedDigest(in.Trade)}, `SELECT transaction_hash,digest FROM %[1]s.canonical_positions WHERE height=$1 AND transaction_index=$2 AND log_index=$3`, []string{p.TransactionHash, EmittedDigest(in.Trade)}},
		{`INSERT INTO %[1]s.canonical_pool_assets VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, []any{in.Trade.PoolAddress, in.Assets.USDL, in.Assets.Token, in.Assets.TokenDecimals}, `SELECT usdl,token,token_decimals::text FROM %[1]s.canonical_pool_assets WHERE pool=$1`, []string{in.Assets.USDL, in.Assets.Token, fmt.Sprint(in.Assets.TokenDecimals)}},
	}
	for i, q := range queries {
		if _, e := tx.Exec(ctx, fmt.Sprintf(q.sql, r.schema), q.args...); e != nil {
			var pe *pgconn.PgError
			if errors.As(e, &pe) && pe.Code == "23505" {
				return ErrIntegrity
			}
			return e
		}
		args := q.args[:1]
		if i == 2 {
			args = q.args[:3]
		}
		values := make([]string, len(q.want))
		dest := make([]any, len(values))
		for j := range values {
			dest[j] = &values[j]
		}
		if e := tx.QueryRow(ctx, fmt.Sprintf(q.read, r.schema), args...).Scan(dest...); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrIntegrity
			}
			return e
		}
		for j, v := range values {
			if v != q.want[j] {
				return ErrIntegrity
			}
		}
	}
	return nil
}

func (r *Repository) ApplyQualifiedTx(ctx context.Context, tx pgx.Tx, in QualifiedApplyInput) (ApplyResult, error) {
	if tx == nil || in.BlockTimestamp != in.Policy.blockTimestamp {
		return ApplyResult{}, ErrInvalid
	}
	amounts, err := AccountQualified(in.Trade, in.Assets, in.Policy)
	if err != nil {
		return ApplyResult{}, err
	}
	pools := []string{in.Trade.PoolAddress}
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT DISTINCT pool FROM %s.trade_facts WHERE height=$1::numeric AND transaction_index=$2::numeric AND log_index=$3::numeric ORDER BY pool`, r.schema), in.Trade.Position.Height, in.Trade.Position.TransactionIndex, in.Trade.Position.LogIndex)
	if err != nil {
		return ApplyResult{}, err
	}
	for rows.Next() {
		var pool string
		if err = rows.Scan(&pool); err != nil {
			rows.Close()
			return ApplyResult{}, err
		}
		pools = append(pools, pool)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return ApplyResult{}, err
	}
	pools = sortedStatsPools(pools)
	if err = r.lockAccountingPoolsTx(ctx, tx, pools, false, ""); err != nil {
		return ApplyResult{}, err
	}
	legacy := ApplyInput{Trade: in.Trade, Assets: in.Assets, BlockTimestamp: in.BlockTimestamp,
		WatermarkTimestamp: in.WatermarkTimestamp, FutureTolerance: in.FutureTolerance,
		ConsumerVersion: in.ConsumerVersion, ProjectionVersion: in.ProjectionVersion}
	result, err := r.applyAccountedTx(ctx, tx, legacy, amounts, &in.Policy, nil)
	if errors.Is(err, ErrIntegrity) {
		return ApplyResult{}, &AccountingIdentityConflict{Pools: pools}
	}
	return result, err
}

func (r *Repository) RecordMarketPublicationTx(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1, result ApplyResult) error {
	if tx == nil || accounting.ValidatePublicationV1(p) != nil || p.Fill == nil || p.Facet != "market" || !result.Accepted || result.Revision < 1 {
		return ErrInvalid
	}
	position, err := json.Marshal(p.Positions[0])
	if err != nil {
		return err
	}
	fill, err := json.Marshal(*p.Fill)
	if err != nil {
		return err
	}
	timestamp, err := strconv.ParseInt(p.Fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	start, _ := Minute(timestamp)
	_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.schema), p.PublicationID, p.SourceFillID, string(p.SourceFactDigest), p.Positions[0].ProjectionVersion, p.Fill.PoolAddress, start, position, fill, p.Positions[0].Sequence, result.Revision)
	if err != nil {
		return err
	}
	var observedAt time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		return err
	}
	update, err := canonicalMarketTradeStreamUpdate(p, observedAt)
	if err != nil {
		return err
	}
	return (delivery.StreamWriter{Schema: "api_stream"}).AppendTx(ctx, tx, update)
}

func (r *Repository) RecordInventoryPublicationTx(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1) error {
	if tx == nil || accounting.ValidatePublicationV1(p) != nil || p.Enrichment == nil || p.Facet != "inventory" {
		return ErrInvalid
	}
	position, err := json.Marshal(p.Positions[0])
	if err != nil {
		return err
	}
	payload, err := json.Marshal(*p.Enrichment)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.accounting_inventory_facets(publication_id,source_fill_id,source_fact_digest,projection_version,partition_key,position,payload) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, r.schema), p.PublicationID, p.SourceFillID, string(p.SourceFactDigest), p.Positions[0].ProjectionVersion, p.Positions[0].PartitionKey, position, payload)
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT source_fill_id=$2 AND source_fact_digest=$3 AND projection_version=$4 AND partition_key=$5 AND position=$6::jsonb AND payload=$7::jsonb FROM %s.accounting_inventory_facets WHERE publication_id=$1`, r.schema), p.PublicationID, p.SourceFillID, string(p.SourceFactDigest), p.Positions[0].ProjectionVersion, p.Positions[0].PartitionKey, position, payload).Scan(&same)
	if err == nil && !same {
		return ErrIntegrity
	}
	return err
}

type AccountingIdentityConflict struct{ Pools []string }

func (e *AccountingIdentityConflict) Error() string {
	return "stats: canonical accounting identity conflict"
}
func (e *AccountingIdentityConflict) Unwrap() error { return ErrIntegrity }

func sortedStatsPools(pools []string) []string {
	out := append([]string(nil), pools...)
	sort.Strings(out)
	unique := out[:0]
	for _, pool := range out {
		if len(unique) == 0 || unique[len(unique)-1] != pool {
			unique = append(unique, pool)
		}
	}
	return unique
}

func (r *Repository) lockAccountingPoolsTx(ctx context.Context, tx pgx.Tx, pools []string, freeze bool, evidence accounting.Digest) error {
	if tx == nil || len(pools) == 0 || len(pools) > 128 || freeze && !qualifiedDigest.MatchString(string(evidence)) {
		return ErrInvalid
	}
	for _, pool := range sortedStatsPools(pools) {
		if !address.MatchString(pool) {
			return ErrInvalid
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.accounting_pool_fences(pool) VALUES($1) ON CONFLICT DO NOTHING`, r.schema), pool); err != nil {
			return err
		}
		var frozen bool
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT frozen FROM %s.accounting_pool_fences WHERE pool=$1 FOR UPDATE`, r.schema), pool).Scan(&frozen); err != nil {
			return err
		}
		if !freeze && frozen {
			return ErrIntegrity
		}
		if freeze {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s.accounting_pool_fences SET frozen=true,evidence_digest=COALESCE(evidence_digest,$2) WHERE pool=$1`, r.schema), pool, string(evidence)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) FreezePoolsTx(ctx context.Context, tx pgx.Tx, pools []string, evidence accounting.Digest) error {
	return r.lockAccountingPoolsTx(ctx, tx, pools, true, evidence)
}

func canonicalMarketTradeStreamUpdate(p accounting.PublicationV1, observedAt time.Time) (delivery.StreamUpdate, error) {
	if accounting.ValidatePublicationV1(p) != nil || p.Facet != "market" || p.Fill == nil || observedAt.IsZero() || observedAt.Year() < 1 || observedAt.Year() > 9999 {
		return delivery.StreamUpdate{}, ErrInvalid
	}
	fill := p.Fill
	seconds, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil || seconds < 0 || seconds > 253402300799 {
		return delivery.StreamUpdate{}, ErrInvalid
	}
	occurredAt := time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05.000Z")
	feeAsset := fill.Token
	if fill.Side == "buy" {
		feeAsset = fill.Settlement
	}
	recipientQuality := "unavailable"
	refs := []string{}
	if fill.Attribution.Recipient != nil {
		recipientQuality = fill.Attribution.Quality
		for _, ref := range fill.Attribution.Evidence {
			refs = append(refs, ref.ArtifactSHA256)
		}
	}
	nullableAmount := func(raw *string) *interfaces.UInt256 {
		if raw == nil {
			return nil
		}
		value := interfaces.UInt256(*raw)
		return &value
	}
	paid := interfaces.UInt256(fill.PaidInputRaw)
	trade := interfaces.Trade{
		EventId: fill.SourceFillID, PoolAddress: fill.PoolAddress, Sender: fill.Attribution.TransactionSender,
		Recipient: fill.Attribution.Recipient, Side: fill.Side, TokenAmountRaw: interfaces.UInt256(fill.TokenAmountRaw),
		UsdlAmountRaw: interfaces.UInt256(fill.USDLAmountRaw),
		Position: interfaces.ChainPosition{ChainId: fill.Position.ChainID, Height: interfaces.UInt256(fill.Position.Height),
			TransactionIndex: interfaces.UInt256(fill.Position.TransactionIndex), LogIndex: interfaces.UInt256(fill.Position.LogIndex),
			TransactionHash: fill.Position.TransactionHash, BlockHash: fill.Position.BlockHash},
		FeeRaw: interfaces.UInt256(fill.FeeRaw), FeeAssetAddress: feeAsset.Address, FeeAssetDecimals: interfaces.SafeInteger(feeAsset.Decimals),
		FeeDisposition: fill.FeeDisposition, PaidInputRaw: &paid, PricedInputRaw: nullableAmount(fill.PricedInputRaw),
		AmmInputAfterFeeRaw: nullableAmount(fill.AMMInputAfterFeeRaw), InputDerivationQuality: fill.InputDerivationQuality,
		RecipientAttribution: recipientQuality, RecipientEvidenceRefs: refs, EmittingContract: fill.ActualEmitter, CodeEpoch: fill.Provenance.EmitterCodeEpoch,
	}
	payload, err := json.Marshal(trade)
	if err != nil {
		return delivery.StreamUpdate{}, err
	}
	if err = interfaces.ValidateJSON("Trade", payload); err != nil {
		return delivery.StreamUpdate{}, err
	}
	position := p.Positions[0]
	watermark, err := json.Marshal(interfaces.Watermark{ChainId: fill.Position.ChainID, Height: interfaces.UInt256(fill.Position.Height),
		BlockHash: fill.Position.BlockHash, ProjectionRevision: interfaces.PositiveUInt256(position.Sequence),
		ObservedAt: observedAt.UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"})
	if err != nil {
		return delivery.StreamUpdate{}, err
	}
	if err = interfaces.ValidateJSON("Watermark", watermark); err != nil {
		return delivery.StreamUpdate{}, err
	}
	return delivery.StreamUpdate{EventID: fill.SourceFillID, Topic: "market.trades." + fill.PoolAddress, Revision: position.Sequence,
		OccurredAt: occurredAt, Watermark: watermark, Payload: payload,
		Source: delivery.StreamSourceProof{Kind: "market_commit", Pool: fill.PoolAddress, ProjectionVersion: position.ProjectionVersion,
			Sequence: position.Sequence, SourceFillID: fill.SourceFillID, SourceFactDigest: string(p.SourceFactDigest)}}, nil
}
