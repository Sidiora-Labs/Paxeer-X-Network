package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"regexp"
)

type ReprocessRequest struct{ PublicationID, PreviousDigest, CorrectedDigest, DecoderVersion, OperatorID string }

func NewReprocessingAccountingConsumer(ctx context.Context, config SourceVerifiedAccountingConfig, request ReprocessRequest) (*AccountingConsumer, error) {
	if !config.SeriesV2 || config.Exact == nil || !regexp.MustCompile(`^reprocess_[a-zA-Z0-9_-]{1,100}$`).MatchString(config.Generation) || request.DecoderVersion == "" || request.OperatorID == "" {
		return nil, errors.New("charts: isolated audited reprocess generation required")
	}
	consumer, err := NewSourceVerifiedAccountingConsumer(config)
	if err != nil {
		return nil, err
	}
	if err := config.Exact.AuthorizeReprocess(ctx, config.ConsumerID, config.Generation, request.PublicationID, request.PreviousDigest, request.CorrectedDigest, request.DecoderVersion, request.OperatorID); err != nil {
		return nil, err
	}
	consumer.reprocess = &request
	return consumer, nil
}

func (c *AccountingConsumer) RefreshCompleteness(ctx context.Context, publication accounting.PublicationV1) error {
	if c == nil || c.config.SeriesCheckpoint == nil || c.config.AuthorizePublication == nil || accounting.ValidatePublicationV1(publication) != nil || publication.Fill == nil || publication.Facet != "market" {
		return ErrAccountingAuthority
	}
	cfg := c.config
	tx, err := cfg.Exact.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT epoch FROM `+cfg.Exact.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=$1 AND NOT frozen FOR UPDATE`, publication.Fill.PoolAddress).Scan(&epoch); err != nil {
		return err
	}
	if err = cfg.Authorize(ctx, tx, *publication.Fill); err != nil {
		return err
	}
	if err = cfg.AuthorizePublication(ctx, tx, publication); err != nil {
		return err
	}
	raw, err := json.Marshal(*publication.Fill)
	if err != nil {
		return err
	}
	var applied bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+cfg.Exact.Schema+`.chart_inbox WHERE chain_id='125' AND pool=$1 AND fill=$2::jsonb)`, publication.Fill.PoolAddress, raw).Scan(&applied)
	if err != nil {
		return err
	}
	if !applied {
		return ErrAccountingAuthority
	}
	checkpoint, err := cfg.SeriesCheckpoint(ctx, tx, publication)
	if err != nil {
		return err
	}
	unresolved, err := cfg.Exact.HasUnresolvedDeliveriesTx(ctx, tx)
	if err != nil {
		return err
	}
	checkpoint.HasGaps = checkpoint.HasGaps || unresolved
	var height uint64
	var timestamp int64
	var gaps bool
	err = tx.QueryRow(ctx, `SELECT watermark_height,watermark_timestamp,has_gaps FROM `+cfg.Exact.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1 FOR UPDATE`, publication.Fill.PoolAddress).Scan(&height, &timestamp, &gaps)
	if err != nil {
		return err
	}
	if height == checkpoint.Height && timestamp == checkpoint.Timestamp && gaps == checkpoint.HasGaps {
		return tx.Commit(ctx)
	}
	if _, err = cfg.Exact.AdvanceSeriesTx(ctx, tx, publication.Fill.PoolAddress, checkpoint.Height, checkpoint.Timestamp, checkpoint.HasGaps); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
