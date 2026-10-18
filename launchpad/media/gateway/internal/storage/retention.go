package storage

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/url"
	"strings"
	"time"

	sharedstorage "github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/jackc/pgx/v5"
)

type LineageObject struct {
	Hash, Key, Kind string
	RetainUntil     time.Time
	LegalHold       bool
}

func (a *Authority) RegisterLineage(ctx context.Context, object LineageObject) error {
	if !digest.MatchString(object.Hash) || object.RetainUntil.IsZero() || (object.Kind != "original" && object.Kind != "variant" && object.Kind != "temporary" && object.Kind != "recording") {
		return ErrConflict
	}
	if strings.ContainsAny(object.Key, "%\\\r\n") || strings.Contains(object.Key, "..") || len(object.Key) > 1024 {
		return ErrConflict
	}
	// PostgreSQL timestamptz keeps microseconds and pgx truncates on encode;
	// normalise first so an idempotent re-registration of the same instant
	// compares equal to the stored retain_until.
	object.RetainUntil = object.RetainUntil.Truncate(time.Microsecond)
	prefix := map[string]string{"original": "private/", "variant": "approved/", "temporary": "temporary/", "recording": "private/recordings/"}[object.Kind]
	if !strings.HasPrefix(object.Key, prefix) || (object.Kind == "variant" && (!ValidDeliveryKey(object.Key) || strings.Split(object.Key, "/")[1] != object.Hash)) {
		return ErrConflict
	}
	var allowed bool
	if e := a.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.metadata("assets")+" WHERE hash=$1 AND state='APPROVED' AND scanner_engine<>'' AND scanner_revision<>'')", object.Hash).Scan(&allowed); e != nil {
		return ErrUnavailable
	}
	if !allowed {
		return ErrRestricted
	}
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "media:"+object.Hash); e != nil {
		return ErrUnavailable
	}
	initialRevision := int64(1)
	initialState := "approved"
	if a.productionViews {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.table("restrictions")+" WHERE hash=$1)", object.Hash).Scan(&exists); e != nil {
			return ErrUnavailable
		}
		if !exists {
			var status string
			e = tx.QueryRow(ctx, "SELECT v.revision,v.status FROM "+a.metadata("assets")+" asset JOIN "+a.safety("safety_visibility")+" v ON (v.target_type='token' AND v.target_id=asset.pool) OR (v.target_type='asset' AND v.target_id=asset.id::text) WHERE asset.hash=$1 AND v.revision>0 ORDER BY v.revision DESC LIMIT 1", object.Hash).Scan(&initialRevision, &status)
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrRestricted
			}
			if e != nil {
				return ErrUnavailable
			}
			if status == "removed" {
				initialState = "blocked"
			} else if status != "restored" {
				return ErrRestricted
			}
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("restrictions")+"(hash,revision,state) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", object.Hash, initialRevision, initialState); e != nil {
		return ErrUnavailable
	}
	var state string
	if e = tx.QueryRow(ctx, "SELECT state FROM "+a.table("restrictions")+" WHERE hash=$1 FOR UPDATE", object.Hash).Scan(&state); e != nil {
		return ErrUnavailable
	}
	var tombstone bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.table("tombstones")+" WHERE hash=$1)", object.Hash).Scan(&tombstone); e != nil {
		return ErrUnavailable
	}
	if state == "deleted" || tombstone {
		return ErrRestricted
	}
	if object.Kind == "original" {
		var now time.Time
		if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
			return ErrUnavailable
		}
		if object.RetainUntil.Before(now.Add(30 * 24 * time.Hour)) {
			return ErrConflict
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("lineage")+"(hash,object_key,kind,legal_hold,retain_until) VALUES($1,$2,$3,$4,$5) ON CONFLICT(hash,object_key) DO NOTHING", object.Hash, object.Key, object.Kind, object.LegalHold, object.RetainUntil); e != nil {
		return ErrUnavailable
	}
	var kind string
	var hold bool
	var until time.Time
	if e = tx.QueryRow(ctx, "SELECT kind,legal_hold,retain_until FROM "+a.table("lineage")+" WHERE hash=$1 AND object_key=$2", object.Hash, object.Key).Scan(&kind, &hold, &until); e != nil {
		return ErrUnavailable
	}
	if kind != object.Kind || hold != object.LegalHold || !until.Equal(object.RetainUntil) {
		return ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrUnavailable
	}
	return nil
}

type DeleteWorker struct {
	Authority *Authority
	Stores    map[string]*sharedstorage.Client
}

func (w DeleteWorker) RunOne(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if w.Authority == nil || len(w.Stores) == 0 {
		return false, ErrUnavailable
	}
	a := w.Authority
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return false, ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var id, hash string
	e = tx.QueryRow(ctx, "SELECT id::text,hash FROM "+a.table("operations")+" WHERE kind='delete' AND state IN ('requested','accepted') ORDER BY requested_at,id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(&id, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ErrUnavailable
	}
	var tombstone bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.table("tombstones")+" WHERE hash=$1)", hash).Scan(&tombstone); e != nil {
		return true, ErrUnavailable
	}
	if !tombstone {
		return true, ErrRestricted
	}
	rows, e := tx.Query(ctx, "SELECT object_key,kind FROM "+a.table("lineage")+" WHERE hash=$1 AND NOT legal_hold AND removed_at IS NULL"+a.currentHoldPredicate()+" ORDER BY object_key LIMIT 100", hash)
	if e != nil {
		return true, ErrUnavailable
	}
	type entry struct{ key, kind string }
	var keys []entry
	for rows.Next() {
		var object entry
		if e = rows.Scan(&object.key, &object.kind); e != nil {
			rows.Close()
			return true, ErrUnavailable
		}
		keys = append(keys, object)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return true, ErrUnavailable
	}
	for _, object := range keys {
		key := object.key
		store := w.Stores[object.kind]
		if store == nil {
			return true, ErrUnavailable
		}
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		e = store.Delete(bounded, key)
		cancel()
		if e != nil {
			return true, ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE "+a.table("lineage")+" SET removed_at=clock_timestamp() WHERE hash=$1 AND object_key=$2 AND NOT legal_hold", hash, key); e != nil {
			return true, ErrUnavailable
		}
	}
	var remaining int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM "+a.table("lineage")+" WHERE hash=$1 AND removed_at IS NULL", hash).Scan(&remaining); e != nil {
		return true, ErrUnavailable
	}
	state := "accepted"
	proof := ""
	if remaining == 0 {
		state = "complete"
		proof = "all-lineage-objects-deleted"
	}
	if _, e = tx.Exec(ctx, "UPDATE "+a.table("operations")+" SET state=$2,proof=$3,attempts=attempts+1,updated_at=clock_timestamp() WHERE id=$1", id, state, proof); e != nil {
		return true, ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return true, ErrUnavailable
	}
	return true, nil
}

func (w DeleteWorker) ExpireOriginal(ctx context.Context) (bool, error) {
	if w.Authority == nil || w.Stores["original"] == nil {
		return false, ErrUnavailable
	}
	a := w.Authority
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return false, ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var hash, key string
	e = tx.QueryRow(ctx, "SELECT hash,object_key FROM "+a.table("lineage")+" WHERE kind='original' AND NOT legal_hold AND removed_at IS NULL AND retain_until<=clock_timestamp()"+a.currentHoldPredicate()+" ORDER BY retain_until LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(&hash, &key)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	e = w.Stores["original"].Delete(bounded, key)
	cancel()
	if e != nil {
		return true, ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE "+a.table("lineage")+" SET removed_at=clock_timestamp() WHERE hash=$1 AND object_key=$2", hash, key); e != nil {
		return true, ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return true, ErrUnavailable
	}
	return true, nil
}

func (a *Authority) PresignDownload(ctx context.Context, client *s3.PresignClient, bucket, key string, ttl time.Duration) (string, error) {
	if client == nil || bucket == "" {
		return "", ErrUnavailable
	}
	if e := a.AuthorizeDownload(ctx, key, ttl); e != nil {
		return "", e
	}
	signed, e := client.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if e != nil || signed == nil {
		return "", ErrUnavailable
	}
	parsed, e := url.Parse(signed.URL)
	if e != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", ErrUnavailable
	}
	return signed.URL, nil
}

func (a *Authority) currentHoldPredicate() string {
	if !a.productionViews {
		return ""
	}
	return " AND NOT EXISTS(SELECT 1 FROM " + a.metadata("assets") + " asset JOIN " + a.metadata("asset_details_v2") + " detail ON detail.asset_id=asset.id WHERE asset.hash=" + a.table("lineage") + ".hash AND detail.investigation_hold)"
}
