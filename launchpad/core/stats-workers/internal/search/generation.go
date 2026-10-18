package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type GenerationProof struct {
	Generation       string
	SourceWatermark  int64
	SourceObservedAt time.Time
	Rows             int64
	Digest           string
}

func GenerationDigest(ctx context.Context, tx pgx.Tx, generation string) (int64, string, error) {
	rows, err := tx.Query(ctx, "SELECT pool_address,payload::text FROM stats_search.documents_v2 WHERE generation=$1 ORDER BY pool_address", generation)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	h := sha256.New()
	var count int64
	for rows.Next() {
		var pool, payload string
		if err = rows.Scan(&pool, &payload); err != nil {
			return 0, "", err
		}
		h.Write([]byte(pool))
		h.Write([]byte{0})
		h.Write([]byte(payload))
		h.Write([]byte{0})
		count++
	}
	if err = rows.Err(); err != nil {
		return 0, "", err
	}
	return count, hex.EncodeToString(h.Sum(nil)), nil
}
func (s Store) BeginGeneration(ctx context.Context, generation string, sourceWatermark int64, observedAt time.Time) error {
	if s.Pool == nil || !sourceName.MatchString(generation) || sourceWatermark < 1 || observedAt.IsZero() || observedAt.Location() != time.UTC || observedAt.After(time.Now()) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	observedAt = observedAt.Truncate(time.Microsecond)
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer rollbackSearch(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='30s';SET LOCAL lock_timeout='100ms'"); err != nil {
		return err
	}
	var active string
	if err = tx.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton FOR UPDATE").Scan(&active); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO stats_search.generations_v2(generation,state,source_watermark,source_observed_at) VALUES($1,'building',$2,$3)", generation, sourceWatermark, observedAt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO stats_search.documents_v2 SELECT $1,pool_address,payload->>'tokenAddress',payload->>'nameKey',lower(payload->>'canonicalTicker'),payload->>'creator',(payload->>'createdAt')::timestamptz,payload->>'poolStatus',(payload->>'visibilityDecisionRevision')::bigint,nextval('stats_search.revision_v2'),(payload->>'metricsAt')::timestamptz,(payload->>'liquidityRaw')::numeric,(payload->'volumes'->>'1h')::numeric,(payload->'volumes'->>'24h')::numeric,(payload->'volumes'->>'7d')::numeric,(payload->>'verifiedQuoteAsset')::boolean,ARRAY(SELECT jsonb_array_elements_text(COALESCE(NULLIF(payload->'riskFlags','null'::jsonb),'[]'::jsonb))),payload FROM stats_search.source_documents_v2`, generation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s Store) ActivateGeneration(ctx context.Context, proof GenerationProof) error {
	if s.Pool == nil || !sourceName.MatchString(proof.Generation) || proof.SourceWatermark < 1 || proof.Rows < 0 || len(proof.Digest) != 64 || proof.SourceObservedAt.IsZero() || proof.SourceObservedAt.Location() != time.UTC {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(proof.Digest); err != nil {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer rollbackSearch(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='30s';SET LOCAL lock_timeout='100ms'"); err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton FOR UPDATE").Scan(&current); err != nil {
		return err
	}
	var watermark int64
	var observed time.Time
	var state string
	var validated *time.Time
	if err = tx.QueryRow(ctx, "SELECT state,source_watermark,source_observed_at,validated_at FROM stats_search.generations_v2 WHERE generation=$1 FOR UPDATE", proof.Generation).Scan(&state, &watermark, &observed, &validated); err != nil {
		return err
	}
	if state != "building" && state != "retained" || watermark != proof.SourceWatermark || !observed.Equal(proof.SourceObservedAt.Truncate(time.Microsecond)) || observed.After(time.Now()) || state == "building" && time.Since(observed) > 5*time.Second || state == "retained" && validated == nil {
		return fmt.Errorf("%w: generation catch-up or authority boundary", ErrInvalid)
	}
	if state == "building" {
		var mismatch bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS((SELECT pool_address,payload FROM stats_search.documents_v2 WHERE generation=$1 EXCEPT SELECT pool_address,payload FROM stats_search.source_documents_v2) UNION ALL (SELECT pool_address,payload FROM stats_search.source_documents_v2 EXCEPT SELECT pool_address,payload FROM stats_search.documents_v2 WHERE generation=$1))`, proof.Generation).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch {
			return fmt.Errorf("%w: source authority sample mismatch", ErrInvalid)
		}
	}
	count, digest, err := GenerationDigest(ctx, tx, proof.Generation)
	if err != nil {
		return err
	}
	if count != proof.Rows || digest != proof.Digest {
		return fmt.Errorf("%w: generation count/digest", ErrInvalid)
	}
	if _, err = tx.Exec(ctx, "UPDATE stats_search.generations_v2 SET state='retained' WHERE generation=$1", current); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE stats_search.generations_v2 SET state='active',row_count=$2,digest=$3,validated_at=clock_timestamp(),activated_at=clock_timestamp() WHERE generation=$1", proof.Generation, count, digest); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE stats_search.active_v2 SET generation=$1,revision=nextval('stats_search.revision_v2'),privacy_revision=privacy_revision+1 WHERE singleton", proof.Generation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s Store) AdvanceGenerationBoundary(ctx context.Context, generation string, watermark int64, observed time.Time) error {
	if s.Pool == nil || !sourceName.MatchString(generation) || watermark < 1 || observed.IsZero() || observed.Location() != time.UTC || observed.After(time.Now()) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	result, err := s.Pool.Exec(ctx, `UPDATE stats_search.generations_v2 SET source_watermark=$2,source_observed_at=$3 WHERE generation=$1 AND state='building' AND source_watermark<=$2 AND source_observed_at<=$3`, generation, watermark, observed)
	if err == nil && result.RowsAffected() != 1 {
		return ErrInvalid
	}
	return err
}
