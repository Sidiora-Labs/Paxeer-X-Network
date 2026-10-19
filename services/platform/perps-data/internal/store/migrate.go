// Package store opens the perps data Postgres database and applies the
// embedded migrations of schema perps.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	// Registers the postgres driver used by Open.
	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/migrations"
)

// ErrMigrationChanged reports a recorded migration whose sha256 no longer
// matches the embedded file, or a recorded migration that is no longer embedded.
var ErrMigrationChanged = errors.New("changed")

const (
	bootstrapSQL = `CREATE SCHEMA IF NOT EXISTS perps;
CREATE TABLE IF NOT EXISTS perps.schema_migrations (
    file text PRIMARY KEY,
    sha256 bytea NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`
	lockSQL     = `SELECT pg_advisory_xact_lock(hashtext('perps.schema_migrations'))`
	recordedSQL = `SELECT file, sha256 FROM perps.schema_migrations`
	lookupSQL   = `SELECT sha256 FROM perps.schema_migrations WHERE file = $1`
	recordSQL   = `INSERT INTO perps.schema_migrations (file, sha256) VALUES ($1, $2)`
)

type migration struct {
	file string
	body string
	sum  [sha256.Size]byte
}

// Open connects to the Postgres database named by databaseURL through lib/pq
// and checks that it answers.
func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, errors.Join(fmt.Errorf("ping postgres: %w", err), db.Close())
	}
	return db, nil
}

// Migrate applies the embedded migrations of schema perps in lexical order,
// one transaction per file, and records each file with its sha256 in
// perps.schema_migrations. It refuses to apply anything when a recorded file
// differs from the embedded one.
func Migrate(ctx context.Context, db *sql.DB) error {
	files, err := embedded()
	if err != nil {
		return err
	}
	if err := inTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, bootstrapSQL)
		return err
	}); err != nil {
		return fmt.Errorf("create perps.schema_migrations: %w", err)
	}
	recorded, err := recordedSums(ctx, db)
	if err != nil {
		return err
	}
	if err := verify(files, recorded); err != nil {
		return err
	}
	for _, m := range files {
		if _, ok := recorded[m.file]; ok {
			continue
		}
		if err := inTx(ctx, db, func(tx *sql.Tx) error { return apply(ctx, tx, m) }); err != nil {
			return err
		}
	}
	return nil
}

func embedded() ([]migration, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	files := make([]migration, 0, len(names))
	for _, name := range names {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		files = append(files, migration{file: name, body: string(body), sum: sha256.Sum256(body)})
	}
	return files, nil
}

func recordedSums(ctx context.Context, db *sql.DB) (map[string][]byte, error) {
	rows, err := db.QueryContext(ctx, recordedSQL)
	if err != nil {
		return nil, fmt.Errorf("read perps.schema_migrations: %w", err)
	}
	defer rows.Close()
	recorded := map[string][]byte{}
	for rows.Next() {
		var file string
		var sum []byte
		if err := rows.Scan(&file, &sum); err != nil {
			return nil, fmt.Errorf("scan perps.schema_migrations: %w", err)
		}
		recorded[file] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read perps.schema_migrations: %w", err)
	}
	return recorded, nil
}

func verify(files []migration, recorded map[string][]byte) error {
	embeddedSums := make(map[string][sha256.Size]byte, len(files))
	for _, m := range files {
		embeddedSums[m.file] = m.sum
		if sum, ok := recorded[m.file]; ok && !bytes.Equal(sum, m.sum[:]) {
			return fmt.Errorf("migration %s %w", m.file, ErrMigrationChanged)
		}
	}
	for file := range recorded {
		if _, ok := embeddedSums[file]; !ok {
			return fmt.Errorf("migration %s %w: not embedded", file, ErrMigrationChanged)
		}
	}
	return nil
}

func apply(ctx context.Context, tx *sql.Tx, m migration) error {
	var sum []byte
	switch err := tx.QueryRowContext(ctx, lookupSQL, m.file).Scan(&sum); {
	case err == nil:
		if !bytes.Equal(sum, m.sum[:]) {
			return fmt.Errorf("migration %s %w", m.file, ErrMigrationChanged)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("look up migration %s: %w", m.file, err)
	}
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("apply migration %s: %w", m.file, err)
	}
	if _, err := tx.ExecContext(ctx, recordSQL, m.file, m.sum[:]); err != nil {
		return fmt.Errorf("record migration %s: %w", m.file, err)
	}
	return nil
}

func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, lockSQL); err != nil {
		return errors.Join(fmt.Errorf("lock perps.schema_migrations: %w", err), tx.Rollback())
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
