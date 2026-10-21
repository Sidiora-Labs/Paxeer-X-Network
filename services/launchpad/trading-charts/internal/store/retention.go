package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

func RetentionFloor(now time.Time, interval string) (time.Time, error) {
	if interval == "1m" {
		return now.UTC().AddDate(0, 0, -365), nil
	}
	switch interval {
	case "5m", "15m", "1h", "4h", "1d", "1w":
		return now.UTC().AddDate(-5, 0, 0), nil
	}
	return time.Time{}, errors.New("unsupported_resolution")
}
func (s *ExactStore) DeleteRetainedRange(ctx context.Context, pool, interval string, from, to int64) (int64, error) {
	if !archiveAddress.MatchString(pool) || from < 0 || to <= from {
		return 0, ErrIntegrity
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return 0, err
	}
	floor, err := RetentionFloor(now, interval)
	if err != nil {
		return 0, err
	}
	if to > floor.Unix() {
		return 0, errors.New("retention_floor_not_elapsed")
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE `+s.Schema+`.chart_replay_references IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return 0, err
	}
	var safe bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_archive_checkpoints WHERE pool=$1 AND from_time<=$2 AND to_time>=$3) AND NOT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_replay_references WHERE pool=$1 AND from_time<$3 AND to_time>$2 AND expires_at>clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_outbox WHERE chain_id='125' AND pool=$1 AND interval=$4 AND start>=$2 AND start<$3 AND published_at IS NULL)`, pool, from, to, interval).Scan(&safe)
	if err != nil {
		return 0, err
	}
	if !safe {
		if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_recoverability_incidents(pool,reason,evidence_digest) VALUES($1,'retention_blocked_missing_archive_or_live_reference',$2)`, pool, bytesDigest([]byte(pool+interval+floor.String()))); err != nil {
			return 0, err
		}
		if err = tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, errors.New("retention_archive_or_reference_guard")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM `+s.Schema+`.chart_traders WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4`, pool, interval, from, to); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM `+s.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4`, pool, interval, from, to)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
func (s *ExactStore) HoldReplayRange(ctx context.Context, id, pool string, from, to int64, ttl time.Duration) error {
	if id == "" || len(id) > 128 || !archiveAddress.MatchString(pool) || from < 0 || to <= from || ttl < time.Second || ttl > 24*time.Hour {
		return ErrIntegrity
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `LOCK TABLE `+s.Schema+`.chart_replay_references IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_replay_references VALUES($1,$2,$3,$4,clock_timestamp()+$5::bigint*interval '1 millisecond') ON CONFLICT(reference_id) DO UPDATE SET expires_at=EXCLUDED.expires_at WHERE chart_replay_references.pool=EXCLUDED.pool AND chart_replay_references.from_time=EXCLUDED.from_time AND chart_replay_references.to_time=EXCLUDED.to_time`, id, pool, from, to, ttl.Milliseconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrIntegrity
	}
	return tx.Commit(ctx)
}
func (s *ExactStore) RecordRecoverabilityIncident(ctx context.Context, pool, reason, digest string) error {
	if s == nil || !archiveAddress.MatchString(pool) || reason == "" || len(reason) > 128 || !archiveDigest.MatchString(digest) {
		return ErrIntegrity
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_recoverability_incidents(pool,reason,evidence_digest) VALUES($1,$2,$3)`, pool, reason, digest)
	return err
}

type RetentionDryRunReport struct {
	Pool              string    `json:"pool"`
	Interval          string    `json:"interval"`
	From              int64     `json:"from"`
	To                int64     `json:"to"`
	EligibleRows      int64     `json:"eligibleRows"`
	ManifestDigests   []string  `json:"manifestDigests"`
	ArchiveFillCount  uint64    `json:"archiveFillCount"`
	ArchiveBytes      int64     `json:"archiveBytes"`
	VerifiedAt        time.Time `json:"verifiedAt"`
	DeletionPerformed bool      `json:"deletionPerformed"`
}

func (s *ExactStore) RetentionDryRun(ctx context.Context, reader ArchiveReader, pool, interval string, from, to int64, manifests []ArchiveManifest) (RetentionDryRunReport, error) {
	report := RetentionDryRunReport{Pool: pool, Interval: interval, From: from, To: to, ManifestDigests: []string{}}
	if s == nil || reader.Exact != s || reader.Objects == nil || reader.BlockTime == nil || !archiveAddress.MatchString(pool) || from < 0 || to <= from || len(manifests) < 1 || len(manifests) > 10000 {
		return report, ErrIntegrity
	}
	ordered := append([]ArchiveManifest(nil), manifests...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FromHeight < ordered[j].FromHeight })
	var previousHeight int64 = -1
	stream := ""
	for _, manifest := range ordered {
		if manifest.Validate() != nil || stream != "" && (manifest.SourceStream != stream || manifest.FromHeight != previousHeight+1) {
			return report, ErrIntegrity
		}
		stream = manifest.SourceStream
		previousHeight = manifest.ToHeight
		fills, err := reader.Read(ctx, manifest)
		if err != nil {
			return report, err
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			return report, err
		}
		report.ManifestDigests = append(report.ManifestDigests, bytesDigest(raw))
		report.ArchiveFillCount += uint64(len(fills))
		report.ArchiveBytes += manifest.ByteLength
	}
	first := ordered[0]
	last := ordered[len(ordered)-1]
	firstTime, err := reader.BlockTime(ctx, first.FromHeight, first.Blocks[0].Hash)
	if err != nil {
		return report, err
	}
	lastTime, err := reader.BlockTime(ctx, last.ToHeight, last.Blocks[len(last.Blocks)-1].Hash)
	if err != nil {
		return report, err
	}
	if firstTime > from || lastTime < to {
		return report, errors.New("retention_archive_time_coverage_missing")
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&report.VerifiedAt); err != nil {
		return report, err
	}
	floor, err := RetentionFloor(report.VerifiedAt, interval)
	if err != nil {
		return report, err
	}
	if to > floor.Unix() {
		return report, errors.New("retention_floor_not_elapsed")
	}
	manifestWire, err := json.Marshal(report.ManifestDigests)
	if err != nil {
		return report, err
	}
	var safe bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_archive_checkpoints WHERE pool=$1 AND from_time<=$2 AND to_time>=$3 AND manifest_digest=$5) AND NOT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_replay_references WHERE pool=$1 AND from_time<$3 AND to_time>$2 AND expires_at>clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_outbox WHERE chain_id='125' AND pool=$1 AND interval=$4 AND start>=$2 AND start<$3 AND published_at IS NULL)`, pool, from, to, interval, bytesDigest(manifestWire)).Scan(&safe)
	if err != nil {
		return report, err
	}
	if !safe {
		return report, errors.New("retention_verified_archive_or_reference_guard")
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+s.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4`, pool, interval, from, to).Scan(&report.EligibleRows); err != nil {
		return report, err
	}
	return report, tx.Commit(ctx)
}
