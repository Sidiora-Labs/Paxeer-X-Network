package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotStore writes only the ranking-owned schema. D05/D12 remain read-only.
// Migrations and runtime permissions are serial-integration responsibilities.
type SnapshotStore struct {
	pool   *pgxpool.Pool
	schema string
}

func NewSnapshotStore(pool *pgxpool.Pool, schema string) (*SnapshotStore, error) {
	if pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) {
		return nil, fmt.Errorf("invalid ranking store configuration")
	}
	return &SnapshotStore{pool, schema}, nil
}
func (s *SnapshotStore) table(name string) string { return pgx.Identifier{s.schema, name}.Sanitize() }

// Save atomically inserts one immutable artifact and its outbox intent. Exact
// retries are idempotent; any attempt to overwrite a run identity is rejected.
func (s *SnapshotStore) Save(ctx context.Context, snapshot ranker.Snapshot) error {
	if snapshot.RunID() == "" || snapshot.InputDigest() == "" || snapshot.OutputDigest() == "" {
		return errors.New("incomplete snapshot")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO `+s.table("snapshots")+`(run_id,category,input_digest,output_digest,artifact) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, snapshot.RunID(), snapshot.Category(), snapshot.InputDigest(), snapshot.OutputDigest(), payload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT artifact=$3::jsonb FROM `+s.table("snapshots")+` WHERE run_id=$1 AND category=$2`, snapshot.RunID(), snapshot.Category(), payload).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return errors.New("immutable snapshot identity conflict")
		}
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO `+s.table("publication_outbox")+`(run_id,category,artifact) VALUES($1,$2,$3)`, snapshot.RunID(), snapshot.Category(), payload)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Read returns the durable artifact for cache rebuild, including completed empty
// lists. No rows is an explicit absence, never a successful empty market.
func (s *SnapshotStore) Read(ctx context.Context, runID, category string) (json.RawMessage, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT artifact FROM `+s.table("snapshots")+` WHERE run_id=$1 AND category=$2`, runID, category).Scan(&b)
	return json.RawMessage(b), err
}
