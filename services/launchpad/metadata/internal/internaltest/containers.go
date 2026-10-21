// Package internaltest provides real ephemeral infrastructure (Postgres, Redis,
// MinIO, clamd) for the metadata service's integration tests — never fakes and
// never containers: every helper uses an already running server named by its
// METADATA_TEST_* variable (see external.go) and fails the test immediately
// when that variable is unset. It is imported only by *_test.go files, so it
// adds nothing to the production binary and is invisible to the per-package
// coverage gate.
package internaltest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/migrate"
)

// NewPostgres creates a throwaway database on the METADATA_TEST_DATABASE_URL
// server, applies the metadata goose migrations, creates the cross-schema indexer.pools foreign table (owned by the
// indexer service in prod; created here so cross-schema reads resolve), and
// returns a ready pgx pool. Torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := NewPostgresURL(t)
	return pool
}

// NewPostgresURL is NewPostgres that also returns the database's connection
// URL, for tests that build the whole service (app.New) from a config.
func NewPostgresURL(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()

	dsn := externalDatabase(t)
	if err := migrate.Up(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.MaxConns = 8
	cfg.MaxConnIdleTime = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Cross-schema dependency: indexer.pools is owned by the indexer service in
	// prod. Create the minimal shape the metadata reads target.
	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS indexer;
		CREATE TABLE IF NOT EXISTS indexer.pools (
			pool_address  varchar(42) PRIMARY KEY,
			token_address varchar(42) NOT NULL,
			creator       varchar(42) NOT NULL
		);`); err != nil {
		t.Fatalf("create indexer.pools: %v", err)
	}
	return pool, dsn
}

// SeedPool inserts a row into indexer.pools so the creator gate + reads resolve.
func SeedPool(t *testing.T, pool *pgxpool.Pool, poolAddr, tokenAddr, creator string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO indexer.pools (pool_address, token_address, creator) VALUES ($1,$2,$3)
		 ON CONFLICT (pool_address) DO UPDATE SET token_address = EXCLUDED.token_address, creator = EXCLUDED.creator`,
		poolAddr, tokenAddr, creator); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

// NewRedisURL returns the METADATA_TEST_REDIS_URL logical database (flushed
// after the test).
func NewRedisURL(t *testing.T) string {
	t.Helper()
	return externalRedis(t)
}

// MinIO is a running MinIO server with one throwaway bucket: its S3 endpoint and
// credentials (for tests that configure the whole service) and a ready client.
type MinIO struct {
	Endpoint, AccessKeyID, SecretAccessKey, Bucket string
	Store                                          *storage.Client
}

// NewStore creates a throwaway bucket on the METADATA_TEST_S3_ENDPOINT server
// and returns a ready storage.Client (real S3-compatible backend — no fakes).
func NewStore(t *testing.T, bucket string) *storage.Client {
	t.Helper()
	return NewMinIO(t, bucket).Store
}

// NewMinIO is NewStore that also returns the endpoint and credentials.
func NewMinIO(t *testing.T, bucket string) MinIO {
	t.Helper()
	return externalMinIO(t, bucket)
}

// NewClamd returns the host and TCP port of the real clamd named by
// METADATA_TEST_CLAMAV_ADDR for sharedhttp.ScanOptions / CLAMAV_HOST +
// CLAMAV_PORT, or ("", 0) when none is configured; callers then skip only the
// checks that need a clean scan. A clamd container is never started.
func NewClamd(t *testing.T) (string, int) {
	t.Helper()
	return externalClamd(t)
}
