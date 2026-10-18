package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

var regexpReprocessGeneration = regexp.MustCompile(`^reprocess_[a-zA-Z0-9_-]{1,100}$`)

var ErrSourceWatermarkRegression = errors.New("source_watermark_regression")

func SeriesSchemaDDL(schema string) (string, error) {
	if !schemaName.MatchString(schema) {
		return "", errors.New("invalid_schema")
	}
	return fmt.Sprintf(`
CREATE TABLE %[1]s.chart_series_clock(chain_id text NOT NULL,pool text NOT NULL,revision bigint NOT NULL CHECK(revision>0),watermark_height bigint NOT NULL CHECK(watermark_height>=0),watermark_timestamp bigint NOT NULL CHECK(watermark_timestamp>=0),has_gaps boolean NOT NULL,PRIMARY KEY(chain_id,pool));
CREATE TABLE %[1]s.chart_rollup_pending(chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,child_revision bigint NOT NULL,PRIMARY KEY(chain_id,pool,interval,start));
CREATE TABLE %[1]s.chart_rollup_lineage(chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,child_digest text NOT NULL,source_watermark bigint NOT NULL,algorithm text NOT NULL,PRIMARY KEY(chain_id,pool,interval,start));
CREATE TABLE %[1]s.chart_delivery_attempts(consumer_id text NOT NULL,generation text NOT NULL,publication_id text NOT NULL,wire_digest text NOT NULL,source_sequence bigint NOT NULL,attempt_count bigint NOT NULL DEFAULT 1,first_seen timestamptz NOT NULL DEFAULT clock_timestamp(),last_attempt timestamptz NOT NULL DEFAULT clock_timestamp(),last_category text NOT NULL,resolved boolean NOT NULL DEFAULT false,PRIMARY KEY(consumer_id,generation,publication_id));
CREATE TABLE %[1]s.chart_reprocess_audit(id bigserial PRIMARY KEY,consumer_id text NOT NULL,generation text NOT NULL,publication_id text NOT NULL,previous_digest text NOT NULL,corrected_digest text NOT NULL,decoder_version text NOT NULL,operator_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE %[1]s.chart_stream_sources(chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,revision numeric(20,0) NOT NULL CHECK(revision>0),event_id text NOT NULL UNIQUE,snapshot jsonb NOT NULL,payload jsonb NOT NULL,watermark jsonb NOT NULL,occurred_at text NOT NULL,projection_version text NOT NULL,source_sequence bigint NOT NULL CHECK(source_sequence>0),source_fill_id text NOT NULL,source_fact_digest text NOT NULL CHECK(source_fact_digest ~ '^[0-9a-f]{64}$'),PRIMARY KEY(chain_id,pool,interval,start,revision));
CREATE FUNCTION %[1]s.chart_stream_source_immutable() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$ BEGIN RAISE EXCEPTION 'immutable canonical candle source'; END $$;
CREATE TRIGGER chart_stream_source_immutable BEFORE UPDATE OR DELETE ON %[1]s.chart_stream_sources FOR EACH ROW EXECUTE FUNCTION %[1]s.chart_stream_source_immutable();
CREATE TABLE %[1]s.chart_recoverability_incidents(id bigserial PRIMARY KEY,pool text NOT NULL,reason text NOT NULL,evidence_digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
`, schema), nil
}
func (s *ExactStore) AdvanceSeriesTx(ctx context.Context, tx pgx.Tx, pool string, height uint64, timestamp int64, gaps bool) (uint64, error) {
	if s == nil || tx == nil || height > uint64(^uint64(0)>>1) || timestamp < 0 {
		return 0, ErrIntegrity
	}
	unresolved, err := s.HasUnresolvedDeliveriesTx(ctx, tx)
	if err != nil {
		return 0, err
	}
	gaps = gaps || unresolved
	var revision int64
	err = tx.QueryRow(ctx, `INSERT INTO `+s.Schema+`.chart_series_clock(chain_id,pool,revision,watermark_height,watermark_timestamp,has_gaps) VALUES('125',$1,1,$2,$3,$4) ON CONFLICT(chain_id,pool) DO UPDATE SET revision=chart_series_clock.revision+1,watermark_height=EXCLUDED.watermark_height,watermark_timestamp=EXCLUDED.watermark_timestamp,has_gaps=EXCLUDED.has_gaps WHERE chart_series_clock.watermark_height<=EXCLUDED.watermark_height AND chart_series_clock.watermark_timestamp<=EXCLUDED.watermark_timestamp AND chart_series_clock.revision<9223372036854775807 RETURNING revision`, pool, int64(height), timestamp, gaps).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSourceWatermarkRegression
	}
	return uint64(revision), err
}
func (s *ExactStore) InvalidateRollupTx(ctx context.Context, tx pgx.Tx, pool, interval string, start int64, revision uint64) error {
	if revision > uint64(^uint64(0)>>1) {
		return ErrIntegrity
	}
	_, err := tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_rollup_pending VALUES('125',$1,$2,$3,$4) ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET child_revision=GREATEST(chart_rollup_pending.child_revision,EXCLUDED.child_revision)`, pool, interval, start, int64(revision))
	return err
}
func (s *ExactStore) SeriesSnapshots(ctx context.Context, pool, interval string, from, to int64, limit int) ([]json.RawMessage, json.RawMessage, uint64, error) {
	if limit < 1 || limit > 10000 || from < 0 || to <= from {
		return nil, nil, 0, ErrIntegrity
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, 0, err
	}
	defer tx.Rollback(ctx)
	var revision, height uint64
	var timestamp int64
	var gaps bool
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT revision,watermark_height,watermark_timestamp,has_gaps,clock_timestamp() FROM `+s.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1`, pool).Scan(&revision, &height, &timestamp, &gaps, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return []json.RawMessage{}, nil, 0, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	var frozen bool
	if err = tx.QueryRow(ctx, `SELECT frozen FROM `+s.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=$1`, pool).Scan(&frozen); err != nil {
		return nil, nil, 0, err
	}
	if frozen {
		return nil, nil, 0, ErrIntegrity
	}
	unresolved, err := s.HasUnresolvedDeliveriesTx(ctx, tx)
	if err != nil {
		return nil, nil, 0, err
	}
	gaps = gaps || unresolved
	var seed json.RawMessage
	err = tx.QueryRow(ctx, `SELECT snapshot FROM `+s.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start<$3 ORDER BY start DESC LIMIT 1`, pool, interval, from).Scan(&seed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT e.snapshot,EXISTS(SELECT 1 FROM `+s.Schema+`.chart_rollup_pending p WHERE p.chain_id=e.chain_id AND p.pool=e.pool AND p.interval=e.interval AND p.start=e.start) FROM `+s.Schema+`.chart_exact e WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4 ORDER BY start LIMIT $5`, pool, interval, from, to, limit+1)
	if err != nil {
		return nil, nil, 0, err
	}
	out := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		var pending bool
		if err = rows.Scan(&raw, &pending); err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		raw, err = refreshSeriesSnapshot(raw, height, timestamp, gaps || pending, now)
		if err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		out = append(out, raw)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	if len(out) > limit {
		return nil, nil, 0, errors.New("series_range_exceeds_bound")
	}
	if len(seed) > 0 {
		seed, err = refreshSeriesSnapshot(seed, height, timestamp, gaps, now)
		if err != nil {
			return nil, nil, 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, 0, err
	}
	return out, seed, revision, nil
}
func (s *ExactStore) RecordAttempt(ctx context.Context, consumer, generation, publication, digest string, sequence int64, category string, resolved bool) error {
	if consumer == "" || generation == "" || publication == "" || !archiveDigest.MatchString(digest) || sequence < 1 || len(category) > 80 {
		return ErrIntegrity
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_delivery_attempts(consumer_id,generation,publication_id,wire_digest,source_sequence,last_category,resolved) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(consumer_id,generation,publication_id) DO UPDATE SET attempt_count=chart_delivery_attempts.attempt_count+1,last_attempt=clock_timestamp(),last_category=CASE WHEN chart_delivery_attempts.resolved THEN chart_delivery_attempts.last_category ELSE EXCLUDED.last_category END,resolved=chart_delivery_attempts.resolved OR EXCLUDED.resolved WHERE chart_delivery_attempts.wire_digest=EXCLUDED.wire_digest`, consumer, generation, publication, digest, sequence, category, resolved)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrIntegrity
	}
	return nil
}
func (s *ExactStore) AuthorizeReprocess(ctx context.Context, consumer, generation, publication, oldDigest, newDigest, decoder, operator string) error {
	if consumer == "" || publication == "" || !regexpReprocessGeneration.MatchString(generation) || decoder == "" || len(decoder) > 128 || operator == "" || len(operator) > 128 || !archiveDigest.MatchString(oldDigest) || !archiveDigest.MatchString(newDigest) {
		return ErrIntegrity
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var priorDecoder string
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT decoder_version FROM `+s.Schema+`.chart_reprocess_audit WHERE consumer_id=$1 AND publication_id=$2 ORDER BY id DESC LIMIT 1),'accounting-fill-v1')`, consumer, publication).Scan(&priorDecoder)
	if err != nil {
		return err
	}
	if oldDigest == newDigest {
		if decoder == priorDecoder {
			return ErrIntegrity
		}
		return errors.New("registered_decoder_change_authority_required")
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_delivery_attempts WHERE consumer_id=$1 AND publication_id=$2 AND wire_digest=$3 AND NOT resolved)`, consumer, publication, oldDigest).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrIntegrity
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_reprocess_audit(consumer_id,generation,publication_id,previous_digest,corrected_digest,decoder_version,operator_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, consumer, generation, publication, oldDigest, newDigest, decoder, operator)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ExactStore) RecordAttemptResultTx(ctx context.Context, tx pgx.Tx, consumer, generation, publication, category string, resolved bool) error {
	tag, err := tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_delivery_attempts SET last_attempt=clock_timestamp(),last_category=CASE WHEN resolved AND NOT $5 THEN last_category ELSE $4 END,resolved=resolved OR $5 WHERE consumer_id=$1 AND generation=$2 AND publication_id=$3`, consumer, generation, publication, category, resolved)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrIntegrity
	}
	return nil
}
func (s *ExactStore) RecordAttemptResult(ctx context.Context, consumer, generation, publication, category string, resolved bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.RecordAttemptResultTx(ctx, tx, consumer, generation, publication, category, resolved); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ExactStore) WaitRetryBackoff(ctx context.Context, consumer, generation, publication string) error {
	var attempts int64
	if err := s.Pool.QueryRow(ctx, `SELECT attempt_count FROM `+s.Schema+`.chart_delivery_attempts WHERE consumer_id=$1 AND generation=$2 AND publication_id=$3`, consumer, generation, publication).Scan(&attempts); err != nil {
		return err
	}
	if attempts <= 1 {
		return nil
	}
	shift := attempts - 2
	if shift > 6 {
		shift = 6
	}
	timer := time.NewTimer(10 * time.Millisecond * time.Duration(1<<uint(shift)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func refreshSeriesSnapshot(raw json.RawMessage, height uint64, timestamp int64, gaps bool, now time.Time) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var end int64
	if err := json.Unmarshal(fields["intervalEnd"], &end); err != nil {
		return nil, err
	}
	if end <= 0 {
		return nil, ErrIntegrity
	}
	status := "interval_elapsed"
	switch {
	case gaps:
		status = "backfill_incomplete"
	case now.Unix() < end:
		status = "live_interval"
	case timestamp >= end:
		status = "complete_through_watermark"
	}
	fields["sourceWatermarkHeight"], _ = json.Marshal(height)
	fields["sourceWatermarkTimestamp"], _ = json.Marshal(timestamp)
	fields["completenessStatus"], _ = json.Marshal(status)
	return json.Marshal(fields)
}

func (s *ExactStore) HasUnresolvedDeliveriesTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	if s == nil || tx == nil {
		return false, ErrIntegrity
	}
	var unresolved bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_delivery_attempts WHERE NOT resolved)`).Scan(&unresolved)
	return unresolved, err
}

func (s *ExactStore) CheckReprocess(ctx context.Context, consumer, generation, publication, digest string) error {
	if s == nil || !regexpReprocessGeneration.MatchString(generation) || !archiveDigest.MatchString(digest) {
		return ErrIntegrity
	}
	var authorized bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_reprocess_audit WHERE consumer_id=$1 AND generation=$2 AND publication_id=$3 AND corrected_digest=$4)`, consumer, generation, publication, digest).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return ErrIntegrity
	}
	return nil
}
