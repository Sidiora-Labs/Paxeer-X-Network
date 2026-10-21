package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnavailable = errors.New("media authority unavailable")
var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type Authority struct {
	Pool                                 *pgxpool.Pool
	Schema, MetadataSchema, SafetySchema string
	productionViews                      bool
}

func NewAuthority(pool *pgxpool.Pool, schema, metadata, safety string) (*Authority, error) {
	if pool == nil || !schemaName.MatchString(schema) || !schemaName.MatchString(metadata) || !schemaName.MatchString(safety) {
		return nil, ErrUnavailable
	}
	return &Authority{Pool: pool, Schema: schema, MetadataSchema: metadata, SafetySchema: safety}, nil
}
func (a *Authority) table(name string) string { return pgx.Identifier{a.Schema, name}.Sanitize() }
func (a *Authority) metadata(name string) string {
	if a.productionViews {
		name += "_v2"
	}
	return pgx.Identifier{a.MetadataSchema, name}.Sanitize()
}
func (a *Authority) safety(name string) string {
	if a.productionViews {
		name += "_v2"
	}
	return pgx.Identifier{a.SafetySchema, name}.Sanitize()
}
func ValidDeliveryKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] != "approved" || !digest.MatchString(parts[1]) || (parts[2] != "256" && parts[2] != "512" && parts[2] != "1024") || !strings.HasSuffix(parts[3], ".png") {
		return false
	}
	return digest.MatchString(strings.TrimSuffix(parts[3], ".png"))
}
func (a *Authority) Allowed(ctx context.Context, key string) error {
	if !ValidDeliveryKey(key) {
		return ErrRestricted
	}
	parts := strings.Split(key, "/")
	if a.productionViews {
		var assetID string
		err := a.Pool.QueryRow(ctx, "SELECT id::text FROM "+a.metadata("assets")+" WHERE hash=$1 AND state='APPROVED' AND scanner_engine<>'' AND scanner_revision<>'' ORDER BY id LIMIT 1", parts[1]).Scan(&assetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRestricted
		}
		if err != nil {
			return ErrUnavailable
		}
		if err = a.ImportApprovedLineage(ctx, assetID); err != nil {
			return err
		}
	}
	var allowed bool
	query := `SELECT EXISTS(SELECT 1 FROM ` + a.metadata("assets") + ` asset JOIN ` + a.metadata("pools") + ` pool ON pool.pool=asset.pool LEFT JOIN ` + a.metadata("documents") + ` doc ON doc.pool=asset.pool LEFT JOIN ` + a.table("restrictions") + ` restriction ON restriction.hash=asset.hash WHERE asset.hash=$1 AND asset.object_key='approved/'||asset.hash AND asset.state='APPROVED' AND asset.scanner_engine<>'' AND asset.scanner_revision<>'' AND NOT COALESCE(doc.blocked,false) AND COALESCE(restriction.state,'approved')='approved' AND EXISTS(SELECT 1 FROM ` + a.table("lineage") + ` l WHERE l.hash=asset.hash AND l.object_key=$2 AND l.kind='variant' AND l.removed_at IS NULL) AND NOT EXISTS(SELECT 1 FROM ` + a.table("tombstones") + ` t WHERE t.hash=asset.hash) AND NOT EXISTS(SELECT 1 FROM ` + a.metadata("assets") + ` denied LEFT JOIN ` + a.metadata("documents") + ` denied_doc ON denied_doc.pool=denied.pool LEFT JOIN ` + a.safety("safety_visibility") + ` denied_v ON (denied_v.target_type='token' AND denied_v.target_id=denied.pool) OR (denied_v.target_type='asset' AND denied_v.target_id=denied.id::text) WHERE denied.hash=asset.hash AND (denied.state='DELETED' OR COALESCE(denied_doc.blocked,false) OR denied_v.status='removed')) AND EXISTS(SELECT 1 FROM ` + a.safety("safety_visibility") + ` v WHERE v.target_type='token' AND v.target_id=pool.pool AND v.status='restored' AND v.revision<=COALESCE(doc.moderation_revision,0)))`
	if e := a.Pool.QueryRow(ctx, query, parts[1], key).Scan(&allowed); e != nil {
		return ErrUnavailable
	}
	if !allowed {
		return ErrRestricted
	}
	return nil
}
func (a *Authority) SchemaDDL() string {
	return fmt.Sprintf(`CREATE TABLE %s(hash text PRIMARY KEY CHECK(hash~'^[0-9a-f]{64}$'),revision bigint NOT NULL CHECK(revision>0),state text NOT NULL CHECK(state IN ('approved','blocked','deleted')),updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE %s(id uuid PRIMARY KEY,hash text NOT NULL REFERENCES %s(hash),revision bigint NOT NULL,kind text NOT NULL CHECK(kind IN ('purge','delete')),state text NOT NULL CHECK(state IN ('requested','accepted','complete','failed')),provider_id text NOT NULL DEFAULT '',proof text NOT NULL DEFAULT '',attempts integer NOT NULL DEFAULT 0,requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(hash,revision,kind));
CREATE TABLE %s(hash text NOT NULL REFERENCES %s(hash),object_key text NOT NULL,kind text NOT NULL CHECK(kind IN ('original','variant','temporary','recording')),legal_hold boolean NOT NULL DEFAULT false,retain_until timestamptz NOT NULL,removed_at timestamptz,PRIMARY KEY(hash,object_key));
CREATE TABLE %s(hash text PRIMARY KEY,revision bigint NOT NULL CHECK(revision>0),deleted_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE %s(id bigserial PRIMARY KEY,kind text NOT NULL,hash text NOT NULL,revision bigint NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(kind,hash,revision));`, a.table("restrictions"), a.table("operations"), a.table("restrictions"), a.table("lineage"), a.table("restrictions"), a.table("tombstones"), a.table("outbox"))
}
func (a *Authority) Restrict(ctx context.Context, hash string, revision int64, state string) (string, error) {
	if !digest.MatchString(hash) || revision < 1 || (state != "approved" && state != "blocked" && state != "deleted") {
		return "", ErrConflict
	}
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return "", ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "media:"+hash); e != nil {
		return "", ErrUnavailable
	}
	var previous int64
	var old string
	e = tx.QueryRow(ctx, "SELECT revision,state FROM "+a.table("restrictions")+" WHERE hash=$1", hash).Scan(&previous, &old)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return "", ErrUnavailable
	}
	var tombstoneRevision int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(max(revision),0) FROM "+a.table("tombstones")+" WHERE hash=$1", hash).Scan(&tombstoneRevision); e != nil {
		return "", ErrUnavailable
	}
	deleted := tombstoneRevision > 0
	if revision < tombstoneRevision {
		return "", ErrConflict
	}
	if revision < previous || (revision == previous && old != state) || ((old == "deleted" || deleted) && state != "deleted") {
		return "", ErrConflict
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("restrictions")+"(hash,revision,state) VALUES($1,$2,$3) ON CONFLICT(hash) DO UPDATE SET revision=EXCLUDED.revision,state=EXCLUDED.state,updated_at=clock_timestamp()", hash, revision, state); e != nil {
		return "", ErrUnavailable
	}
	var id string
	if state != "approved" {
		kind := "purge"
		if state == "deleted" {
			kind = "delete"
		}
		id = uuid.NewString()
		if e = tx.QueryRow(ctx, "INSERT INTO "+a.table("operations")+"(id,hash,revision,kind,state) VALUES($1,$2,$3,$4,'requested') ON CONFLICT(hash,revision,kind) DO UPDATE SET hash=EXCLUDED.hash RETURNING id::text", id, hash, revision, kind).Scan(&id); e != nil {
			return "", ErrUnavailable
		}
		if state == "deleted" {
			if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("operations")+"(id,hash,revision,kind,state) VALUES($1,$2,$3,'purge','requested') ON CONFLICT(hash,revision,kind) DO NOTHING", uuid.NewString(), hash, revision); e != nil {
				return "", ErrUnavailable
			}
			if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("tombstones")+"(hash,revision) VALUES($1,$2) ON CONFLICT(hash) DO UPDATE SET revision=GREATEST("+a.table("tombstones")+".revision,EXCLUDED.revision)", hash, revision); e != nil {
				return "", ErrUnavailable
			}
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("outbox")+"(kind,hash,revision,payload) VALUES($1,$2,$3,jsonb_build_object('hash',$2::text,'revision',$3::bigint,'state',$4::text)) ON CONFLICT DO NOTHING", "restriction", hash, revision, state); e != nil {
		return "", ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return "", ErrUnavailable
	}
	return id, nil
}

type Operation struct {
	ID          string    `json:"operationId"`
	Hash        string    `json:"-"`
	Revision    int64     `json:"revision"`
	Kind        string    `json:"kind"`
	State       string    `json:"state"`
	ProviderID  string    `json:"-"`
	Proof       string    `json:"-"`
	RequestedAt time.Time `json:"requestedAt"`
}

func (a *Authority) Operation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	if _, e := uuid.Parse(id); e != nil {
		return op, ErrConflict
	}
	e := a.Pool.QueryRow(ctx, "SELECT id::text,hash,revision,kind,state,provider_id,proof,requested_at FROM "+a.table("operations")+" WHERE id=$1", id).Scan(&op.ID, &op.Hash, &op.Revision, &op.Kind, &op.State, &op.ProviderID, &op.Proof, &op.RequestedAt)
	if e != nil {
		return op, ErrUnavailable
	}
	return op, nil
}
func (a *Authority) AuthorizeDownload(ctx context.Context, key string, ttl time.Duration) error {
	if ttl <= 0 || ttl > 15*time.Minute {
		return ErrRestricted
	}
	return a.Allowed(ctx, key)
}
func (a *Authority) ReplayTombstones(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("restrictions")+"(hash,revision,state) SELECT hash,revision,'deleted' FROM "+a.table("tombstones")+" ON CONFLICT(hash) DO UPDATE SET state='deleted',revision=GREATEST("+a.table("restrictions")+".revision,EXCLUDED.revision)"); e != nil {
		return ErrUnavailable
	}
	rows, e := tx.Query(ctx, "SELECT t.hash,t.revision FROM "+a.table("tombstones")+" t WHERE NOT EXISTS(SELECT 1 FROM "+a.table("operations")+" o WHERE o.hash=t.hash AND o.revision=t.revision AND o.kind='delete') OR NOT EXISTS(SELECT 1 FROM "+a.table("operations")+" o WHERE o.hash=t.hash AND o.revision=t.revision AND o.kind='purge') ORDER BY t.hash LIMIT 256")
	if e != nil {
		return ErrUnavailable
	}
	type removed struct {
		hash     string
		revision int64
	}
	var missing []removed
	for rows.Next() {
		var item removed
		if e = rows.Scan(&item.hash, &item.revision); e != nil {
			rows.Close()
			return ErrUnavailable
		}
		missing = append(missing, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return ErrUnavailable
	}
	for _, item := range missing {
		for _, kind := range []string{"delete", "purge"} {
			if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("operations")+"(id,hash,revision,kind,state) VALUES($1,$2,$3,$4,'requested') ON CONFLICT(hash,revision,kind) DO NOTHING", uuid.NewString(), item.hash, item.revision, kind); e != nil {
				return ErrUnavailable
			}
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrUnavailable
	}
	return nil
}

func (a *Authority) ImportApprovedLineage(ctx context.Context, assetID string) error {
	if _, e := uuid.Parse(assetID); e != nil {
		return ErrConflict
	}
	var hash, state string
	var raw []byte
	var created time.Time
	var hold bool
	query := "SELECT a.hash,a.state,d.variants,d.created_at,d.investigation_hold FROM " + a.metadata("assets") + " a JOIN " + a.metadata("asset_details_v2") + " d ON d.asset_id=a.id WHERE a.id=$1 AND a.scanner_engine<>'' AND a.scanner_revision<>''"
	if e := a.Pool.QueryRow(ctx, query, assetID).Scan(&hash, &state, &raw, &created, &hold); e != nil {
		return ErrUnavailable
	}
	if state != "APPROVED" {
		return ErrRestricted
	}
	var variants map[string]string
	if e := json.Unmarshal(raw, &variants); e != nil || len(variants) != 3 {
		return ErrConflict
	}
	for _, size := range []string{"256", "512", "1024"} {
		key := variants[size]
		if !ValidDeliveryKey(key) || !strings.HasPrefix(key, "approved/"+hash+"/"+size+"/") {
			return ErrConflict
		}
	}
	for _, size := range []string{"256", "512", "1024"} {
		if e := a.RegisterLineage(ctx, LineageObject{Hash: hash, Key: variants[size], Kind: "variant", LegalHold: hold, RetainUntil: created.Add(365 * 24 * time.Hour)}); e != nil {
			return e
		}
	}
	if e := a.importQuarantineOriginal(ctx, assetID, hash, created, hold); e != nil {
		return e
	}
	return nil
}

func NewConnectedAuthority(pool *pgxpool.Pool, schema, metadata, safety string) (*Authority, error) {
	authority, err := NewAuthority(pool, schema, metadata, safety)
	if err != nil {
		return nil, err
	}
	authority.productionViews = true
	return authority, nil
}

func (a *Authority) RestrictionReceiptSchemaDDL() string {
	return "CREATE TABLE " + a.table("restriction_receipts") + "(event_id text PRIMARY KEY,payload_hash text NOT NULL CHECK(payload_hash~'^[0-9a-f]{64}$'),applied_at timestamptz NOT NULL DEFAULT clock_timestamp());"
}

type RestrictionVisibility struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Status     string `json:"status"`
	CaseID     string `json:"caseId"`
	Revision   int64  `json:"revision"`
}
type RestrictionEvent struct {
	ID       string
	Revision int64
	Payload  []byte
}

func (a *Authority) IntakeRestrictions(ctx context.Context, evict func(context.Context, string) error) (int, error) {
	if !a.productionViews || evict == nil {
		return 0, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, e := a.Pool.Query(ctx, "SELECT o.id::text,o.revision,o.payload FROM "+a.safety("safety_outbox")+" o WHERE o.kind='visibility_changed' AND NOT EXISTS(SELECT 1 FROM "+a.table("restriction_receipts")+" r WHERE r.event_id=o.id::text) ORDER BY o.created_at,o.id LIMIT 32")
	if e != nil {
		return 0, ErrUnavailable
	}
	var events []RestrictionEvent
	for rows.Next() {
		var event RestrictionEvent
		if e = rows.Scan(&event.ID, &event.Revision, &event.Payload); e != nil {
			rows.Close()
			return 0, ErrUnavailable
		}
		events = append(events, event)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, ErrUnavailable
	}
	for i, event := range events {
		var visibility RestrictionVisibility
		if json.Unmarshal(event.Payload, &visibility) != nil || visibility.Revision != event.Revision || event.Revision < 1 || (visibility.TargetType != "token" && visibility.TargetType != "asset") || (visibility.Status != "removed" && visibility.Status != "restored") {
			return i, ErrConflict
		}
		column := "pool"
		if visibility.TargetType == "asset" {
			column = "id::text"
		}
		assets, e := a.Pool.Query(ctx, "SELECT DISTINCT hash FROM "+a.metadata("assets")+" WHERE "+column+"=$1 ORDER BY hash", visibility.TargetID)
		if e != nil {
			return i, ErrUnavailable
		}
		var hashes []string
		for assets.Next() {
			var hash string
			if e = assets.Scan(&hash); e != nil {
				assets.Close()
				return i, ErrUnavailable
			}
			hashes = append(hashes, hash)
		}
		e = assets.Err()
		assets.Close()
		if e != nil {
			return i, ErrUnavailable
		}
		for _, hash := range hashes {
			state := "approved"
			if visibility.Status == "removed" {
				state = "blocked"
			}
			if _, e = a.Restrict(ctx, hash, event.Revision, state); e != nil {
				if !errors.Is(e, ErrConflict) {
					return i, e
				}
				var stale bool
				if e = a.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.table("restrictions")+" WHERE hash=$1 AND (revision>$2 OR state='deleted'))", hash, event.Revision).Scan(&stale); e != nil {
					return i, ErrUnavailable
				}
				if !stale {
					return i, ErrConflict
				}
			}
			keys, e := a.Pool.Query(ctx, "SELECT object_key FROM "+a.table("lineage")+" WHERE hash=$1 AND kind='variant' ORDER BY object_key", hash)
			if e != nil {
				return i, ErrUnavailable
			}
			var variants []string
			for keys.Next() {
				var key string
				if e = keys.Scan(&key); e != nil {
					keys.Close()
					return i, ErrUnavailable
				}
				variants = append(variants, key)
			}
			e = keys.Err()
			keys.Close()
			if e != nil {
				return i, ErrUnavailable
			}
			for _, key := range variants {
				if e = evict(ctx, key); e != nil {
					return i, e
				}
			}
		}
		payloadHash := fmt.Sprintf("%x", sha256.Sum256(event.Payload))
		if _, e = a.Pool.Exec(ctx, "INSERT INTO "+a.table("restriction_receipts")+"(event_id,payload_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", event.ID, payloadHash); e != nil {
			return i, ErrUnavailable
		}
		var recorded string
		if e = a.Pool.QueryRow(ctx, "SELECT payload_hash FROM "+a.table("restriction_receipts")+" WHERE event_id=$1", event.ID).Scan(&recorded); e != nil {
			return i, ErrUnavailable
		}
		if recorded != payloadHash {
			return i, ErrConflict
		}
	}
	return len(events), nil
}

func (a *Authority) importQuarantineOriginal(ctx context.Context, assetID, hash string, created time.Time, hold bool) error {
	if _, e := uuid.Parse(assetID); e != nil || !digest.MatchString(hash) || created.IsZero() {
		return ErrConflict
	}
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "media:"+hash); e != nil {
		return ErrUnavailable
	}
	var admitted bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+a.metadata("assets")+" WHERE id=$1 AND hash=$2 AND state='APPROVED' AND scanner_engine<>'' AND scanner_revision<>'') AND NOT EXISTS(SELECT 1 FROM "+a.table("tombstones")+" WHERE hash=$2)", assetID, hash).Scan(&admitted); e != nil {
		return ErrUnavailable
	}
	if !admitted {
		return ErrRestricted
	}
	key := "quarantine/" + assetID
	if _, e = tx.Exec(ctx, "INSERT INTO "+a.table("lineage")+"(hash,object_key,kind,retain_until,legal_hold) VALUES($1,$2,'original',$3,$4) ON CONFLICT(hash,object_key) DO NOTHING", hash, key, created.Add(30*24*time.Hour), hold); e != nil {
		return ErrUnavailable
	}
	var kind string
	var retain time.Time
	if e = tx.QueryRow(ctx, "SELECT kind,retain_until FROM "+a.table("lineage")+" WHERE hash=$1 AND object_key=$2", hash, key).Scan(&kind, &retain); e != nil {
		return ErrUnavailable
	}
	if kind != "original" || !retain.Equal(created.Add(30*24*time.Hour)) {
		return ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrUnavailable
	}
	return nil
}

func (a *Authority) ImportApprovedBatch(ctx context.Context) (int, error) {
	rows, e := a.Pool.Query(ctx, "SELECT a.id::text FROM "+a.metadata("assets")+" a JOIN "+a.metadata("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.state='APPROVED' AND a.scanner_engine<>'' AND a.scanner_revision<>'' AND NOT EXISTS(SELECT 1 FROM "+a.table("lineage")+" l WHERE l.hash=a.hash AND l.object_key='quarantine/'||a.id::text AND l.kind='original') AND NOT EXISTS(SELECT 1 FROM "+a.table("tombstones")+" t WHERE t.hash=a.hash) ORDER BY d.created_at,a.id LIMIT 32")
	if e != nil {
		return 0, ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, ErrUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, ErrUnavailable
	}
	for index, id := range ids {
		if e = a.ImportApprovedLineage(ctx, id); e != nil {
			return index, e
		}
	}
	return len(ids), nil
}

func (a *Authority) IntakeDeletedAssets(ctx context.Context) (int, error) {
	rows, e := a.Pool.Query(ctx, "SELECT DISTINCT asset.hash,COALESCE(restriction.revision,0)+1 FROM "+a.metadata("assets")+" asset LEFT JOIN "+a.table("restrictions")+" restriction ON restriction.hash=asset.hash WHERE asset.state='DELETED' AND NOT EXISTS(SELECT 1 FROM "+a.table("tombstones")+" t WHERE t.hash=asset.hash) ORDER BY asset.hash LIMIT 32")
	if e != nil {
		return 0, ErrUnavailable
	}
	type deletion struct {
		hash     string
		revision int64
	}
	var deletions []deletion
	for rows.Next() {
		var d deletion
		if e = rows.Scan(&d.hash, &d.revision); e != nil {
			rows.Close()
			return 0, ErrUnavailable
		}
		deletions = append(deletions, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, ErrUnavailable
	}
	for index, d := range deletions {
		if _, e = a.Restrict(ctx, d.hash, d.revision, "deleted"); e != nil {
			return index, e
		}
	}
	return len(deletions), nil
}
