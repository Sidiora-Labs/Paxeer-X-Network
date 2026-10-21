package l17

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var workerName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type ExportWorker struct {
	Repository Repository
	Directory  string
	Owner      string
}

func (w ExportWorker) RunOnce(ctx context.Context) error {
	if w.Repository.Pool == nil {
		return ErrUnavailable
	}
	if !workerName.MatchString(w.Owner) {
		return ErrInvalid
	}
	info, e := os.Lstat(w.Directory)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalid
	}
	if _, e = w.Repository.Pool.Exec(ctx, "WITH exhausted AS (SELECT id FROM portfolio_export_jobs WHERE state='running' AND lease_until<clock_timestamp() AND attempts>=3 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 100) UPDATE portfolio_export_jobs j SET state='failed',last_error='lease_attempts_exhausted',fence=fence+1,lease_owner=NULL,lease_until=NULL FROM exhausted e WHERE j.id=e.id"); e != nil {
		return e
	}
	tx, e := w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	var job ExportJob
	e = tx.QueryRow(ctx, `WITH candidate AS (SELECT id FROM portfolio_export_jobs WHERE (state='queued' OR (state='running' AND lease_until<clock_timestamp())) AND attempts<3 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE portfolio_export_jobs j SET state='running',fence=fence+1,lease_owner=$1,lease_until=clock_timestamp()+interval '5 minutes',attempts=attempts+1 FROM candidate c WHERE j.id=c.id RETURNING j.id::text,j.wallet,j.snapshot_id::text,j.format,j.fence`, w.Owner).Scan(&job.ID, &job.Wallet, &job.Snapshot, &job.Format, &job.Fence)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	for oldFence := int64(1); oldFence < job.Fence; oldFence++ {
		base := job.ID + "-" + strconv.FormatInt(oldFence, 10)
		for _, extension := range []string{"partial", job.Format} {
			if e = os.Remove(filepath.Join(w.Directory, base+"."+extension)); e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				var active bool
				e := w.Repository.Pool.QueryRow(work, "SELECT EXISTS(SELECT 1 FROM portfolio_export_jobs WHERE id=$1 AND state='running' AND fence=$2 AND lease_owner=$3 AND lease_until>clock_timestamp())", job.ID, job.Fence, w.Owner).Scan(&active)
				if e != nil || !active {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-watched }()
	manifest, e := w.Repository.exportHistoryObject(work, w.Directory, job.Wallet, job.Wallet, job.Snapshot, job.Format, job.ID+"-"+strconv.FormatInt(job.Fence, 10), time.Now())
	if e != nil {
		_, dbErr := w.Repository.Pool.Exec(ctx, "UPDATE portfolio_export_jobs SET state=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,last_error='export_failed',lease_owner=NULL,lease_until=NULL WHERE id=$1 AND fence=$2 AND state='running' AND lease_owner=$3 AND lease_until>clock_timestamp()", job.ID, job.Fence, w.Owner)
		return errors.Join(e, dbErr)
	}
	target := filepath.Join(w.Directory, manifest.Object)
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(target)
		}
	}()
	manifest.ExpiresAt = time.Now().UTC().Add(15 * time.Minute)
	raw, e := json.Marshal(manifest)
	if e != nil {
		return e
	}
	tx, e = w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	tag, e := tx.Exec(ctx, "UPDATE portfolio_export_jobs SET state='complete',manifest=$4,completed_at=clock_timestamp(),expires_at=$5,lease_owner=NULL,lease_until=NULL WHERE id=$1 AND fence=$2 AND lease_owner=$3 AND state='running' AND lease_until>clock_timestamp()", job.ID, job.Fence, w.Owner, raw, manifest.ExpiresAt)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_export_outbox SET published_at=clock_timestamp() WHERE event_id=$1", "generate:"+job.ID); e != nil {
		return e
	}
	cleanup = false
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return nil
}

func (w ExportWorker) CleanupOnce(ctx context.Context) error {
	if e := privateExportDirectory(w.Directory); e != nil {
		return e
	}
	if !workerName.MatchString(w.Owner) {
		return ErrInvalid
	}
	tx, e := w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	rows, e := tx.Query(ctx, "SELECT id::text,format FROM portfolio_export_jobs WHERE cleaned_at IS NULL AND (state IN ('cancelled','failed','expired') OR (state='complete' AND expires_at<=clock_timestamp())) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 100")
	if e != nil {
		return e
	}
	type object struct{ ID, Format string }
	objects := []object{}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.ID, &o.Format); e != nil {
			rows.Close()
			return e
		}
		objects = append(objects, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, o := range objects {
		if !validUUID(o.ID) || (o.Format != "csv" && o.Format != "jsonl") {
			return ErrInvalid
		}
		matches, e := filepath.Glob(filepath.Join(w.Directory, o.ID+"-*"))
		if e != nil {
			return e
		}
		for _, name := range matches {
			if e = os.Remove(name); e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE portfolio_export_jobs SET state=CASE WHEN state='complete' THEN 'expired' ELSE state END,cleaned_at=clock_timestamp() WHERE id=$1", o.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE portfolio_export_outbox SET published_at=clock_timestamp() WHERE event_id=$1", "delete:"+o.ID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
