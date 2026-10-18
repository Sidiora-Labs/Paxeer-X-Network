package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AssetRecord is private audit state; object keys are never returned for blocked
// media. Quarantine policy and rejection TTL are enforced by the storage owner.
type AssetRecord struct {
	ID, Pool, Hash, ObjectKey, Engine, Signatures string
	Size                                          int64
}

func (s *ProductionStore) CreateAsset(ctx context.Context, a AssetRecord) error {
	if !uuidRE.MatchString(a.ID) || !addressRE.MatchString(a.Pool) || a.Size < 1 || a.Size > 6<<20 || len(a.Hash) != 64 {
		return ErrValidation
	}
	if _, e := hex.DecodeString(a.Hash); e != nil {
		return ErrValidation
	}
	if a.ObjectKey != "quarantine/"+a.ID {
		return ErrValidation
	}
	_, e := s.Pool.Exec(ctx, "INSERT INTO "+s.table("assets")+"(id,pool,state,hash,size,object_key,scanner_engine,scanner_revision) VALUES($1,$2,'CREATED',$3,$4,$5,'','') ON CONFLICT(id) DO NOTHING", a.ID, a.Pool, a.Hash, a.Size, a.ObjectKey)
	if e != nil {
		return e
	}
	var old AssetRecord
	e = s.Pool.QueryRow(ctx, "SELECT id,pool,hash,size,object_key FROM "+s.table("assets")+" WHERE id=$1", a.ID).Scan(&old.ID, &old.Pool, &old.Hash, &old.Size, &old.ObjectKey)
	if e != nil {
		return e
	}
	if old.Pool != a.Pool || old.Hash != a.Hash || old.Size != a.Size {
		return ErrConflict
	}
	return nil
}
func (s *ProductionStore) TransitionAsset(ctx context.Context, id, from, to string) error {
	valid := map[string]string{"CREATED": "UPLOADING", "UPLOADING": "QUARANTINED", "QUARANTINED": "VALIDATING", "VALIDATING": "REJECTED", "REJECTED": "DELETED", "APPROVED": "DELETED"}
	if valid[from] != to || !uuidRE.MatchString(id) {
		return ErrValidation
	}
	tag, e := s.Pool.Exec(ctx, "UPDATE "+s.table("assets")+" SET state=$3 WHERE id=$1 AND state=$2", id, from, to)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

// RasterWorker is an integration boundary requiring a separate process with
// 256MiB memory and five-second wall deadline. No in-process production default.
type RasterWorker interface {
	Decode(context.Context, []byte) (raster.Raster, error)
}
type ImmutablePublisher interface {
	PutImmutable(context.Context, string, []byte) error
}

// Promote is retryable across copy/commit interruption. The publisher must reject
// an existing key with different bytes and must expose only validated variants.
// Scanner unavailable leaves VALIDATING private and the previous image active.
func (s *ProductionStore) Promote(ctx context.Context, id string, original []byte, variants map[int][]byte, scan sharedhttp.ScanResult, pub ImmutablePublisher) error {
	if !uuidRE.MatchString(id) || pub == nil || len(original) > 6<<20 {
		return ErrValidation
	}
	original = bytes.Clone(original)
	ownedVariants := make(map[int][]byte, 3)
	if len(variants) != 3 {
		return ErrValidation
	}
	for _, n := range []int{256, 512, 1024} {
		b := variants[n]
		if len(b) == 0 || len(b) > 6<<20 {
			return ErrValidation
		}
		ownedVariants[n] = bytes.Clone(b)
	}
	variants = ownedVariants
	digestOriginal := sha256.Sum256(original)
	if scan.ContentHash != hex.EncodeToString(digestOriginal[:]) || scan.Size != int64(len(original)) {
		return ErrForbidden
	}
	if !scan.Clean || scan.Verdict != sharedhttp.ScanClean || scan.Engine == "" || scan.SignatureRevision == "" {
		return ErrForbidden
	}
	if s.RasterWorker == nil {
		return raster.ErrUnavailable
	}
	decoded, err := s.RasterWorker.Decode(ctx, original)
	if err != nil {
		return err
	}
	for _, n := range []int{256, 512, 1024} {
		if !bytes.Equal(variants[n], decoded.Variants[n]) {
			return ErrValidation
		}
	}
	if len(variants) != 3 {
		return ErrValidation
	}
	for _, n := range []int{256, 512, 1024} {
		if len(variants[n]) == 0 || len(variants[n]) > 6<<20 {
			return ErrValidation
		}
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		tx.Rollback(c)
	}()
	var pool, state, hash string
	if e = tx.QueryRow(ctx, "SELECT pool FROM "+s.table("assets")+" WHERE id=$1", id).Scan(&pool); e != nil {
		return e
	}
	var creator string
	if e = tx.QueryRow(ctx, "SELECT creator FROM "+s.table("pools")+" WHERE pool=$1 FOR UPDATE", pool).Scan(&creator); e != nil {
		return e
	}
	var size int64
	e = tx.QueryRow(ctx, "SELECT pool,state,hash,size FROM "+s.table("assets")+" WHERE id=$1 FOR UPDATE", id).Scan(&pool, &state, &hash, &size)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrMissing
	}
	if e != nil {
		return e
	}
	h := sha256.Sum256(original)
	if hex.EncodeToString(h[:]) != hash || int64(len(original)) != size {
		return ErrConflict
	}
	if state != "APPROVED" && state != "VALIDATING" {
		return ErrConflict
	}
	var blocked bool
	e = tx.QueryRow(ctx, "SELECT blocked FROM "+s.table("documents")+" WHERE pool=$1 FOR SHARE", pool).Scan(&blocked)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if blocked {
		return ErrForbidden
	}
	keys := map[string]string{}
	for _, n := range []int{256, 512, 1024} {
		b := variants[n]
		digest := sha256.Sum256(b)
		key := fmt.Sprintf("approved/%s/%d/%s.png", hash, n, hex.EncodeToString(digest[:]))
		keys[strconv.Itoa(n)] = key
		if e = pub.PutImmutable(ctx, key, b); e != nil {
			return e
		}
	}
	var connected bool
	if e = tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", s.table("asset_details_v2")).Scan(&connected); e != nil {
		return e
	}
	if connected {
		encoded, err := json.Marshal(keys)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE "+s.table("asset_details_v2")+" SET variants=$2::jsonb WHERE asset_id=$1 AND mime=$3 AND width=$4 AND height=$5", id, string(encoded), http.DetectContentType(original), decoded.Width, decoded.Height)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
	}
	_, e = tx.Exec(ctx, "UPDATE "+s.table("assets")+" SET state='APPROVED',object_key=$2,scanner_engine=$3,scanner_revision=$4 WHERE id=$1", id, "approved/"+hash, scan.Engine, scan.SignatureRevision)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// ApprovedReference removes storage identity under active moderation.
func (s *ProductionStore) ApprovedReference(ctx context.Context, id string) (string, error) {
	var key string
	e := s.Pool.QueryRow(ctx, "SELECT a.object_key FROM "+s.table("assets")+" a JOIN "+s.table("documents")+" d ON d.pool=a.pool WHERE a.id=$1 AND a.state='APPROVED' AND NOT d.blocked", id).Scan(&key)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	return strings.TrimSpace(key), e
}
