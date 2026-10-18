package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Development-mode discovery reads over the narrative_discovery schema owned by
// core/discovery. Nothing creates or writes that schema outside production
// (the development goose migrations do not include it, and discovery writes
// require an identity authority the development gateway does not provide), so
// an absent relation is the development statement "nothing has been
// published": it reads as zero rows, never as an error.

// DevelopmentDiscoveryMaxScan bounds how many source rows one feed page may
// inspect while skipping entries that are not publicly resolvable.
const DevelopmentDiscoveryMaxScan = 500

// DiscoveryKey is an exclusive keyset position in descending publication order.
type DiscoveryKey struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func undefinedRelation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "42P01" || pg.Code == "3F000")
}

// DevelopmentDiscoveryDB exposes the shared pool as database/sql so the
// core/discovery store and callout service (which are database/sql based) can
// resolve sources with their own production code.
func (s *Store) DevelopmentDiscoveryDB() *sql.DB {
	if s == nil || s.pool == nil {
		return nil
	}
	return stdlib.OpenDBFromPool(s.pool)
}

// DevelopmentDiscoverySources returns up to limit source references in
// descending (published_at, event_id) order strictly after the keyset position.
// The returned keys are the positions of the returned rows.
func (s *Store) DevelopmentDiscoverySources(ctx context.Context, after *DiscoveryKey, limit int) ([]discovery.SourceRef, []DiscoveryKey, error) {
	if limit < 1 || limit > DevelopmentDiscoveryMaxScan {
		return nil, nil, ErrDevelopmentScope
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	var at *time.Time
	id := ""
	if after != nil {
		at, id = &after.At, after.ID
	}
	rows, err := tx.Query(ctx, `SELECT document,published_at,event_id FROM narrative_discovery.sources
 WHERE $1::timestamptz IS NULL OR (published_at,event_id)<($1::timestamptz,$2::text)
 ORDER BY published_at DESC,event_id DESC LIMIT $3`, at, id, limit)
	if err != nil {
		if undefinedRelation(err) {
			return []discovery.SourceRef{}, []DiscoveryKey{}, nil
		}
		return nil, nil, err
	}
	defer rows.Close()
	refs := []discovery.SourceRef{}
	keys := []DiscoveryKey{}
	for rows.Next() {
		var raw []byte
		var key DiscoveryKey
		if err = rows.Scan(&raw, &key.At, &key.ID); err != nil {
			return nil, nil, err
		}
		var ref discovery.SourceRef
		if len(raw) > PublicJSONMaxBytes || json.Unmarshal(raw, &ref) != nil || ref.Validate() != nil {
			return nil, nil, ErrDataIntegrity
		}
		refs = append(refs, ref)
		keys = append(keys, key)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	return refs, keys, tx.Commit(ctx)
}

// DevelopmentDiscoverySource reads one source reference by id. found is false
// when no such source exists (including when the schema does not exist).
func (s *Store) DevelopmentDiscoverySource(ctx context.Context, id string) (discovery.SourceRef, bool, error) {
	var ref discovery.SourceRef
	if id == "" || len(id) > 160 {
		return ref, false, ErrDevelopmentScope
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return ref, false, err
	}
	defer cancel()
	defer rollbackRead(tx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT document FROM narrative_discovery.sources WHERE id=$1`, id).Scan(&raw)
	if err != nil {
		if undefinedRelation(err) || errors.Is(err, pgx.ErrNoRows) {
			return ref, false, nil
		}
		return ref, false, err
	}
	if len(raw) > PublicJSONMaxBytes || json.Unmarshal(raw, &ref) != nil || ref.Validate() != nil {
		return ref, false, ErrDataIntegrity
	}
	return ref, true, tx.Commit(ctx)
}

// DevelopmentPoolCallouts returns up to limit live (approved, not deleted)
// callouts for the pool in descending (CreatedAt, id) order strictly after the
// keyset position.
func (s *Store) DevelopmentPoolCallouts(ctx context.Context, pool string, after *DiscoveryKey, limit int) ([]discovery.Callout, []DiscoveryKey, error) {
	if normalized, err := Address(pool); err != nil || normalized != pool || limit < 1 || limit > 101 {
		return nil, nil, ErrDevelopmentScope
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	var at *time.Time
	id := ""
	if after != nil {
		at, id = &after.At, after.ID
	}
	rows, err := tx.Query(ctx, `SELECT document,(document->>'CreatedAt')::timestamptz AS created,id FROM narrative_discovery.callouts
 WHERE lower(document->>'PoolAddress')=$1 AND document->>'ModerationStatus'='approved' AND NOT COALESCE((document->>'Deleted')::boolean,false)
 AND ($2::timestamptz IS NULL OR ((document->>'CreatedAt')::timestamptz,id)<($2::timestamptz,$3::text))
 ORDER BY created DESC,id DESC LIMIT $4`, pool, at, id, limit)
	if err != nil {
		if undefinedRelation(err) {
			return []discovery.Callout{}, []DiscoveryKey{}, nil
		}
		return nil, nil, err
	}
	defer rows.Close()
	out := []discovery.Callout{}
	keys := []DiscoveryKey{}
	for rows.Next() {
		var raw []byte
		var key DiscoveryKey
		if err = rows.Scan(&raw, &key.At, &key.ID); err != nil {
			return nil, nil, err
		}
		var c discovery.Callout
		if len(raw) > PublicJSONMaxBytes || json.Unmarshal(raw, &c) != nil || c.ID != key.ID {
			return nil, nil, ErrDataIntegrity
		}
		out = append(out, c)
		keys = append(keys, key)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, keys, tx.Commit(ctx)
}
