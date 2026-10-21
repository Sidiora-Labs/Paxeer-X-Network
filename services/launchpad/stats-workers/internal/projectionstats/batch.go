package projectionstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/jackc/pgx/v5"
)

type BatchApplyError struct {
	Index int
	Cause error
}

func (e *BatchApplyError) Error() string {
	return fmt.Sprintf("stats batch input%d: %v", e.Index, e.Cause)
}
func (e *BatchApplyError) Unwrap() error { return e.Cause }

type batchBucketKey struct {
	pool, version string
	start         int64
}
type batchFeeKey struct {
	bucket             batchBucketKey
	asset, disposition string
	decimals           int
}
type batchBucket struct {
	volume                *big.Int
	buys, sells, revision int64
	close                 Trade
}
type batchFee struct {
	raw      *big.Int
	revision int64
}
type statsBatch struct {
	buckets map[batchBucketKey]*batchBucket
	fees    map[batchFeeKey]*batchFee
}

func newStatsBatch() *statsBatch {
	return &statsBatch{map[batchBucketKey]*batchBucket{}, map[batchFeeKey]*batchFee{}}
}
func (b *statsBatch) enter(ctx context.Context, tx pgx.Tx, r *Repository, in ApplyInput, a Accounting) (int64, error) {
	start, err := Minute(in.BlockTimestamp)
	if err != nil {
		return 0, err
	}
	key := batchBucketKey{in.Trade.PoolAddress, in.ProjectionVersion, start}
	bucket := b.buckets[key]
	if bucket == nil {
		bucket = &batchBucket{volume: new(big.Int), close: in.Trade}
		err = tx.QueryRow(ctx, "SELECT revision FROM "+r.schema+".minute_buckets WHERE chain_id='125' AND pool=$1 AND interval_seconds=60 AND start=$2 AND projection_version=$3 FOR UPDATE", key.pool, start, key.version).Scan(&bucket.revision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		b.buckets[key] = bucket
	}
	if bucket.revision == 1<<63-1 {
		return 0, ErrIntegrity
	}
	bucket.revision++
	volume, err := Uint(a.TradedUSDLRaw)
	if err != nil {
		return 0, err
	}
	bucket.volume.Add(bucket.volume, volume)
	if in.Trade.Side == "buy" {
		bucket.buys++
	} else {
		bucket.sells++
	}
	if in.Trade.Position.Compare(bucket.close.Position) > 0 {
		bucket.close = in.Trade
	}
	fk := batchFeeKey{key, in.Trade.FeeAssetAddress, in.Trade.FeeDisposition, in.Trade.FeeAssetDecimals}
	fee := b.fees[fk]
	if fee == nil {
		fee = &batchFee{raw: new(big.Int)}
		b.fees[fk] = fee
	}
	raw, err := Uint(in.Trade.FeeRaw)
	if err != nil {
		return 0, err
	}
	fee.raw.Add(fee.raw, raw)
	fee.revision = bucket.revision
	return bucket.revision, nil
}
func (b *statsBatch) flush(ctx context.Context, tx pgx.Tx, r *Repository) error {
	keys := make([]batchBucketKey, 0, len(b.buckets))
	for key := range b.buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, c := keys[i], keys[j]
		if a.pool != c.pool {
			return a.pool < c.pool
		}
		if a.version != c.version {
			return a.version < c.version
		}
		return a.start < c.start
	})
	for _, key := range keys {
		v := b.buckets[key]
		payload, err := json.Marshal(v.close)
		if err != nil {
			return err
		}
		p := v.close.Position
		_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.minute_buckets AS b VALUES('125',$1,60,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(chain_id,pool,interval_seconds,start,projection_version) DO UPDATE SET volume=b.volume+excluded.volume,buys=b.buys+excluded.buys,sells=b.sells+excluded.sells,revision=excluded.revision,close_payload=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_payload ELSE b.close_payload END,close_height=GREATEST(b.close_height,excluded.close_height),close_transaction_index=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_transaction_index ELSE b.close_transaction_index END,close_log_index=CASE WHEN (excluded.close_height,excluded.close_transaction_index,excluded.close_log_index)>(b.close_height,b.close_transaction_index,b.close_log_index) THEN excluded.close_log_index ELSE b.close_log_index END`, r.schema), key.pool, key.start, key.version, v.volume.String(), v.buys, v.sells, v.revision, p.Height, p.TransactionIndex, p.LogIndex, payload)
		if err != nil {
			return err
		}
	}
	feeKeys := make([]batchFeeKey, 0, len(b.fees))
	for key := range b.fees {
		feeKeys = append(feeKeys, key)
	}
	sort.Slice(feeKeys, func(i, j int) bool { return fmt.Sprint(feeKeys[i]) < fmt.Sprint(feeKeys[j]) })
	for _, key := range feeKeys {
		v := b.fees[key]
		_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.fee_groups AS f VALUES('125',$1,$2,$3,$4,$5,'trading_fee',$6,$7,$8) ON CONFLICT(chain_id,pool,start,projection_version,asset,decimals,fee_type,disposition) DO UPDATE SET raw=f.raw+excluded.raw,revision=excluded.revision`, r.schema), key.bucket.pool, key.bucket.start, key.bucket.version, key.asset, key.decimals, key.disposition, v.raw.String(), v.revision)
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) ApplyQualifiedBatchTx(ctx context.Context, tx pgx.Tx, inputs []QualifiedApplyInput) ([]ApplyResult, error) {
	if tx == nil || len(inputs) < 1 || len(inputs) > 512 {
		return nil, ErrInvalid
	}
	amounts := make([]Accounting, len(inputs))
	pools := make([]string, 0, len(inputs))
	for i, in := range inputs {
		if in.BlockTimestamp != in.Policy.blockTimestamp {
			return nil, &BatchApplyError{i, ErrInvalid}
		}
		var err error
		amounts[i], err = AccountQualified(in.Trade, in.Assets, in.Policy)
		if err != nil {
			return nil, &BatchApplyError{i, err}
		}
		pools = append(pools, in.Trade.PoolAddress)
	}
	heights, indices, logs := make([]string, len(inputs)), make([]string, len(inputs)), make([]string, len(inputs))
	for i, in := range inputs {
		heights[i] = in.Trade.Position.Height
		indices[i] = in.Trade.Position.TransactionIndex
		logs[i] = in.Trade.Position.LogIndex
	}
	rows, err := tx.Query(ctx, "SELECT DISTINCT t.height::text,t.transaction_index::text,t.log_index::text,t.pool FROM "+r.schema+".trade_facts t JOIN unnest($1::text[],$2::text[],$3::text[]) q(height,transaction_index,log_index) ON t.height=q.height::numeric AND t.transaction_index=q.transaction_index::numeric AND t.log_index=q.log_index::numeric", heights, indices, logs)
	if err != nil {
		return nil, err
	}
	existing := map[string][]string{}
	for rows.Next() {
		var h, i, l, pool string
		if err = rows.Scan(&h, &i, &l, &pool); err != nil {
			rows.Close()
			return nil, err
		}
		key := h + ":" + i + ":" + l
		existing[key] = append(existing[key], pool)
		pools = append(pools, pool)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err := r.lockAccountingPoolsTx(ctx, tx, sortedStatsPools(pools), false, ""); err != nil {
		return nil, err
	}
	batch := newStatsBatch()
	results := make([]ApplyResult, len(inputs))
	for i, in := range inputs {
		legacy := ApplyInput{Trade: in.Trade, Assets: in.Assets, BlockTimestamp: in.BlockTimestamp, WatermarkTimestamp: in.WatermarkTimestamp, FutureTolerance: in.FutureTolerance, ConsumerVersion: in.ConsumerVersion, ProjectionVersion: in.ProjectionVersion}
		result, err := r.applyAccountedTx(ctx, tx, legacy, amounts[i], &in.Policy, batch)
		if errors.Is(err, ErrIntegrity) {
			p := in.Trade.Position
			affected := append([]string{in.Trade.PoolAddress}, existing[p.Height+":"+p.TransactionIndex+":"+p.LogIndex]...)
			return nil, &BatchApplyError{i, &AccountingIdentityConflict{Pools: sortedStatsPools(affected)}}
		}
		if err != nil {
			return nil, &BatchApplyError{i, err}
		}
		results[i] = result
	}
	if err := batch.flush(ctx, tx, r); err != nil {
		return nil, err
	}
	return results, nil
}
func (r *Repository) ApplyAuthorizedBatch(ctx context.Context, authority *CoreReceiptAuthority, trades []Trade, consumerVersion, projectionVersion string) ([]ApplyResult, error) {
	if authority == nil || len(trades) < 1 || len(trades) > 512 {
		return nil, ErrInvalid
	}
	amounts := make([]Accounting, len(trades))
	pools := make([]string, 0, len(trades))
	for i, trade := range trades {
		var err error
		amounts[i], err = authority.Account(trade)
		if err != nil {
			return nil, err
		}
		pools = append(pools, trade.PoolAddress)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = r.lockAccountingPoolsTx(ctx, tx, sortedStatsPools(pools), false, ""); err != nil {
		return nil, err
	}
	batch := newStatsBatch()
	results := make([]ApplyResult, len(trades))
	for i, trade := range trades {
		admitted := authority.admitted[trade.EventID]
		in := ApplyInput{Trade: trade, Assets: admitted.assets, BlockTimestamp: admitted.timestamp, WatermarkTimestamp: admitted.timestamp, ConsumerVersion: consumerVersion, ProjectionVersion: projectionVersion}
		results[i], err = r.applyAccountedTx(ctx, tx, in, amounts[i], nil, batch)
		if err != nil {
			return nil, err
		}
	}
	if err = batch.flush(ctx, tx, r); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return results, nil
}
