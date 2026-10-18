package projectionstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"time"

	"github.com/jackc/pgx/v5"
)

// ExecutionPrice is a raw-unit ratio, not a post-swap reserve price or oracle.
// Decimal scales are explicit: human price=(numerator/10^quoteDecimals)/
// (denominator/10^baseDecimals). Zero output leaves price unknown.
type ExecutionPrice struct {
	Numerator     string   `json:"numerator"`
	Denominator   string   `json:"denominator"`
	BaseDecimals  int      `json:"baseDecimals"`
	QuoteDecimals int      `json:"quoteDecimals"`
	Source        string   `json:"source"`
	AmountBasis   string   `json:"amountBasis"`
	Position      Position `json:"position"`
}
type MinuteSnapshot struct {
	Start                  int64           `json:"start"`
	Revision               int64           `json:"revision"`
	ProjectionVersion      string          `json:"projectionVersion"`
	Close                  Trade           `json:"close"`
	Price                  *ExecutionPrice `json:"executionPrice"`
	PriceUnavailableReason *string         `json:"priceUnavailableReason"`
}

func (r *Repository) MinuteSnapshot(ctx context.Context, pool, version string, start int64, tokenDecimals int) (MinuteSnapshot, bool, error) {
	if !address.MatchString(pool) || len(version) == 0 || len(version) > 64 || start < 0 || start%60 != 0 || tokenDecimals < 0 || tokenDecimals > 255 {
		return MinuteSnapshot{}, false, ErrInvalid
	}
	s := MinuteSnapshot{Start: start, ProjectionVersion: version}
	var payload []byte
	e := r.pool.QueryRow(ctx, fmt.Sprintf(`SELECT revision,close_payload FROM %s.minute_buckets WHERE chain_id='125' AND pool=$1 AND interval_seconds=60 AND start=$2 AND projection_version=$3`, r.schema), pool, start, version).Scan(&s.Revision, &payload)
	if e == pgx.ErrNoRows {
		return MinuteSnapshot{}, false, nil
	}
	if e != nil {
		return MinuteSnapshot{}, false, e
	}
	if e = json.Unmarshal(payload, &s.Close); e != nil {
		return MinuteSnapshot{}, false, e
	}
	if s.Close.TokenAmountRaw == "0" {
		reason := "zero_token_quantity"
		s.PriceUnavailableReason = &reason
	} else {
		s.Price = &ExecutionPrice{s.Close.USDLAmountRaw, s.Close.TokenAmountRaw, tokenDecimals, 6, "gross_execution", "paid_input", s.Close.Position}
	}
	return s, true, nil
}

type AccountingSnapshot struct {
	PoolAddress       string                       `json:"poolAddress"`
	ProjectionVersion string                       `json:"projectionVersion"`
	BucketStart       int64                        `json:"bucketStart"`
	Revision          int64                        `json:"revision"`
	Watermark         accounting.SourcePosition    `json:"watermark"`
	PartitionPosition accounting.PartitionPosition `json:"partitionPosition"`
	VolumeUSDLRaw     string                       `json:"volumeUsdlRaw"`
	PoolFillCount     int64                        `json:"poolFillCount"`
	Fees              []AssetFee                   `json:"tradingFees"`
	Close             accounting.FillV1            `json:"close"`
}

func (r *Repository) AccountingSnapshot(ctx context.Context, pool, version string, start int64) (AccountingSnapshot, bool, error) {
	if !address.MatchString(pool) || version == "" || len(version) > 64 || start < 0 || start%60 != 0 {
		return AccountingSnapshot{}, false, ErrInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AccountingSnapshot{}, false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	out, found, err := r.AccountingSnapshotTx(ctx, tx, pool, version, start)
	if err != nil {
		return AccountingSnapshot{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AccountingSnapshot{}, false, err
	}
	return out, found, nil
}

func (r *Repository) AccountingSnapshotTx(ctx context.Context, tx pgx.Tx, pool, version string, start int64) (AccountingSnapshot, bool, error) {
	if tx == nil || !address.MatchString(pool) || version == "" || len(version) > 64 || start < 0 || start%60 != 0 {
		return AccountingSnapshot{}, false, ErrInvalid
	}
	var frozen bool
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT frozen FROM %s.accounting_pool_fences WHERE pool=$1`, r.schema), pool).Scan(&frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountingSnapshot{}, false, nil
	}
	if err != nil {
		return AccountingSnapshot{}, false, err
	}
	if frozen {
		return AccountingSnapshot{}, false, ErrIntegrity
	}
	out := AccountingSnapshot{PoolAddress: pool, ProjectionVersion: version, BucketStart: start, Fees: []AssetFee{}}
	var frontier, close []byte
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT b.revision,b.volume::text,b.buys+b.sells,
  (SELECT c.position FROM %[1]s.accounting_market_commits c WHERE c.pool=$1 AND c.projection_version=$2 ORDER BY c.sequence DESC LIMIT 1),
  (SELECT c.fill FROM %[1]s.accounting_market_commits c WHERE c.pool=$1 AND c.projection_version=$2 AND c.bucket_start=$3 ORDER BY (c.fill->'position'->>'height')::numeric DESC,(c.fill->'position'->>'transactionIndex')::numeric DESC,(c.fill->'position'->>'logIndex')::numeric DESC LIMIT 1)
 FROM %[1]s.minute_buckets b WHERE b.chain_id='125' AND b.pool=$1 AND b.projection_version=$2 AND b.start=$3`, r.schema), pool, version, start).Scan(&out.Revision, &out.VolumeUSDLRaw, &out.PoolFillCount, &frontier, &close)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountingSnapshot{}, false, nil
	}
	if err != nil {
		return AccountingSnapshot{}, false, err
	}
	if len(frontier) == 0 || len(close) == 0 || json.Unmarshal(frontier, &out.PartitionPosition) != nil || json.Unmarshal(close, &out.Close) != nil || accounting.ValidatePartitionPosition(out.PartitionPosition) != nil || accounting.ValidateFillV1(out.Close) != nil {
		return AccountingSnapshot{}, false, ErrIntegrity
	}
	out.Watermark = out.PartitionPosition.SourceOrder
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT asset,decimals,disposition,raw::text FROM %s.fee_groups WHERE chain_id='125' AND pool=$1 AND projection_version=$2 AND start=$3 ORDER BY asset,decimals,disposition`, r.schema), pool, version, start)
	if err != nil {
		return AccountingSnapshot{}, false, err
	}
	for rows.Next() {
		var fee AssetFee
		if err = rows.Scan(&fee.Asset, &fee.Decimals, &fee.Disposition, &fee.Raw); err != nil {
			rows.Close()
			return AccountingSnapshot{}, false, err
		}
		out.Fees = append(out.Fees, fee)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return AccountingSnapshot{}, false, err
	}
	return out, true, nil
}
