package l17

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const ExportDDL = `CREATE TABLE portfolio_export_jobs (
 id uuid PRIMARY KEY,source_event text NOT NULL UNIQUE,wallet text NOT NULL,
 snapshot_id uuid NOT NULL REFERENCES portfolio_snapshots(id),format text NOT NULL CHECK(format IN ('jsonl','csv')),
 state text NOT NULL CHECK(state IN ('queued','running','complete','cancelled','failed','expired')),
 manifest jsonb,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz,
 expires_at timestamptz,fence bigint NOT NULL DEFAULT 0,lease_owner text,lease_until timestamptz,
 attempts integer NOT NULL DEFAULT 0,last_error text,cleaned_at timestamptz,
 CHECK(state<>'complete' OR (manifest IS NOT NULL AND expires_at IS NOT NULL)));
 CREATE INDEX portfolio_export_ready ON portfolio_export_jobs(state,created_at);
 CREATE TABLE portfolio_export_outbox (event_id text PRIMARY KEY,job_id uuid NOT NULL REFERENCES portfolio_export_jobs(id),kind text NOT NULL,published_at timestamptz);`

type ExportJob struct {
	ID       string          `json:"id"`
	Wallet   string          `json:"-"`
	Snapshot string          `json:"snapshotId"`
	Format   string          `json:"format"`
	State    string          `json:"state"`
	Manifest *ExportManifest `json:"manifest,omitempty"`
	Expires  *time.Time      `json:"expiresAt,omitempty"`
	Fence    int64           `json:"-"`
}

func (r Repository) CreateExportJob(ctx context.Context, wallet, verified, snapshot, format, source string) (ExportJob, error) {
	var out ExportJob
	if e := RequireOwner(wallet, verified); e != nil {
		return out, e
	}
	wallet, _ = Wallet(wallet)
	if !validUUID(snapshot) || len(source) < 1 || len(source) > 256 || (format != "jsonl" && format != "csv") {
		return out, ErrInvalid
	}
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer rollbackPortfolio(tx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "export:"+source); e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, "SELECT id::text,wallet,snapshot_id::text,format,state FROM portfolio_export_jobs WHERE source_event=$1", source).Scan(&out.ID, &out.Wallet, &out.Snapshot, &out.Format, &out.State)
	if e == nil {
		if out.Wallet != wallet || out.Snapshot != snapshot || out.Format != format {
			return out, ErrConflict
		}
		return out, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var available bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM portfolio_snapshots s JOIN portfolio_history_snapshots h ON h.id=s.id WHERE s.id=$1 AND wallet=$2 AND expires_at>clock_timestamp())", snapshot, wallet).Scan(&available); e != nil {
		return out, e
	}
	if !available {
		return out, ErrNotFound
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "export-wallet:"+wallet); e != nil {
		return out, e
	}
	var queued int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM portfolio_export_jobs WHERE wallet=$1 AND state IN ('queued','running')", wallet).Scan(&queued); e != nil {
		return out, e
	}
	if queued >= 2 {
		return out, ErrBusy
	}
	out.ID, e = opaqueID()
	if e != nil {
		return out, e
	}
	out.Wallet = wallet
	out.Snapshot = snapshot
	out.Format = format
	out.State = "queued"
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_export_jobs(id,source_event,wallet,snapshot_id,format,state) VALUES($1,$2,$3,$4,$5,'queued')", out.ID, source, wallet, snapshot, format); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_export_outbox VALUES($1,$2,'generate',NULL)", "generate:"+out.ID, out.ID); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (r Repository) ExportJob(ctx context.Context, id, wallet, verified string) (ExportJob, error) {
	var out ExportJob
	if e := RequireOwner(wallet, verified); e != nil {
		return out, e
	}
	wallet, _ = Wallet(wallet)
	if !validUUID(id) {
		return out, ErrNotFound
	}
	var raw []byte
	e := r.Pool.QueryRow(ctx, "SELECT id::text,wallet,snapshot_id::text,format,state,manifest,expires_at FROM portfolio_export_jobs WHERE id=$1 AND wallet=$2", id, wallet).Scan(&out.ID, &out.Wallet, &out.Snapshot, &out.Format, &out.State, &raw, &out.Expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if out.State == "complete" {
		var m ExportManifest
		if e = json.Unmarshal(raw, &m); e != nil {
			return out, e
		}
		out.Manifest = &m
	}
	return out, nil
}

func (r Repository) CancelExport(ctx context.Context, id, wallet, verified string) error {
	if !validUUID(id) {
		return ErrNotFound
	}
	if e := RequireOwner(wallet, verified); e != nil {
		return e
	}
	wallet, _ = Wallet(wallet)
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	tag, e := tx.Exec(ctx, "UPDATE portfolio_export_jobs SET state='cancelled',fence=fence+1,lease_owner=NULL,lease_until=NULL WHERE id=$1 AND wallet=$2 AND state IN ('queued','running','complete','cancelled')", id, wallet)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_export_outbox VALUES($1,$2,'delete',NULL) ON CONFLICT(event_id) DO NOTHING", "delete:"+id, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r Repository) OpenExport(ctx context.Context, dir, id, wallet, verified string) (*os.File, ExportManifest, error) {
	var manifest ExportManifest
	if e := RequireOwner(wallet, verified); e != nil {
		return nil, manifest, e
	}
	wallet, _ = Wallet(wallet)
	if !validUUID(id) {
		return nil, manifest, ErrNotFound
	}
	if e := privateExportDirectory(dir); e != nil {
		return nil, manifest, e
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return nil, manifest, e
	}
	defer rollbackPortfolio(tx)
	var raw []byte
	var fence int64
	var snapshot, format string
	e = tx.QueryRow(ctx, "SELECT manifest,fence,snapshot_id::text,format FROM portfolio_export_jobs WHERE id=$1 AND wallet=$2 AND state='complete' AND expires_at>clock_timestamp() FOR SHARE", id, wallet).Scan(&raw, &fence, &snapshot, &format)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, manifest, ErrNotFound
	}
	if e != nil {
		return nil, manifest, e
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		return nil, manifest, e
	}
	if manifest.Format != format || manifest.SnapshotID != snapshot || manifest.Rows < 0 || manifest.Rows > 1000000 || len(manifest.SHA256) != 64 || (format != "csv" && format != "jsonl") {
		return nil, manifest, ErrInvalid
	}
	base := filepath.Base(manifest.Object)
	expected := id + "-" + strconv.FormatInt(fence, 10) + "." + manifest.Format
	if base != manifest.Object || base != expected {
		return nil, manifest, ErrInvalid
	}
	info, e := os.Lstat(filepath.Join(dir, base))
	if e != nil {
		return nil, manifest, e
	}
	if !info.Mode().IsRegular() {
		return nil, manifest, ErrInvalid
	}
	file, e := os.Open(filepath.Join(dir, base))
	if e != nil {
		return nil, manifest, e
	}
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, manifest, ErrInvalid
	}
	hash := sha256.New()
	buffer := make([]byte, 65536)
	for {
		if e = ctx.Err(); e != nil {
			file.Close()
			return nil, manifest, e
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			return nil, manifest, readErr
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != manifest.SHA256 {
		file.Close()
		return nil, manifest, ErrConflict
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		file.Close()
		return nil, manifest, e
	}
	if e = tx.Commit(ctx); e != nil {
		file.Close()
		return nil, manifest, e
	}
	return file, manifest, nil
}
