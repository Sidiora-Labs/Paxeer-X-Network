// Package internaltest provides real ephemeral infrastructure (Postgres, Redis)
// for the stats-workers integration tests — never fakes (house rule no_stub)
// and never containers. A migrated throwaway Postgres database (the stats
// schema, via the service's own goose migrations) and a live Redis client on
// the already running servers named by STATS_TEST_POSTGRES_DSN /
// STATS_TEST_REDIS_URL are provided per call and torn down through t.Cleanup;
// a helper whose variable is unset fails the test immediately. It is imported only by *_test.go files, so it
// adds nothing to the production binary and is invisible to the per-package
// coverage gate.
package internaltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/migrate"
)

// metadataSchemaDDL creates the minimal metadata.token_metadata table the search
// route joins against. In production this table is owned by the media/metadata
// service in the SAME database (invariant i2); here we create a faithful subset
// of the columns the stats search query reads.
const metadataSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS metadata;
CREATE TABLE IF NOT EXISTS metadata.token_metadata (
	token_address varchar(42) PRIMARY KEY,
	pool_address  varchar(42) NOT NULL,
	name          text,
	symbol        text,
	description   text,
	created_by    varchar(42) NOT NULL,
	created_at    bigint      NOT NULL
);`

// NewPostgres creates a throwaway database on the STATS_TEST_POSTGRES_DSN
// server, applies the stats-workers goose migrations (the stats schema), and
// returns a ready pgx pool. Both are torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := NewPostgresWithDSN(t)
	return pool
}

// NewPostgresWithDSN is NewPostgres but also returns the connection DSN, for
// callers (e.g. the app integration test) that build their own pool from config.
//
// The throwaway database is created on the STATS_TEST_POSTGRES_DSN server (the
// DSN's role needs CREATEDB), migrated, and dropped on cleanup.
func NewPostgresWithDSN(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv(postgresDSNEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers", postgresDSNEnv)
	}
	return newExternalPostgres(t, context.Background(), admin)
}

// postgresDSNEnv / redisURLEnv name the already-running Postgres / Redis the
// tests use.
const (
	postgresDSNEnv = "STATS_TEST_POSTGRES_DSN"
	redisURLEnv    = "STATS_TEST_REDIS_URL"
)

// newExternalPostgres creates a uniquely named database on the server behind
// admin, migrates it, and drops it (FORCE) when the test ends.
func newExternalPostgres(t *testing.T, ctx context.Context, admin string) (string, *pgxpool.Pool) {
	t.Helper()
	cfg, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", postgresDSNEnv, err)
	}
	var suffix [6]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "stats_test_" + hex.EncodeToString(suffix[:])
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", postgresDSNEnv, err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.ConnectConfig(context.Background(), cfg)
		if err != nil {
			t.Logf("drop test database: %v", err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("%s must be a postgres:// URL", postgresDSNEnv)
	}
	u.Path = "/" + name
	dsn := u.String()
	return dsn, migratedPool(t, ctx, dsn)
}

// migratedPool applies the stats-workers migrations to dsn and returns a pool
// closed on cleanup.
func migratedPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
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
	return pool
}

// EnsureMetadataSchema creates the metadata.token_metadata table used by the
// search route. Call this from tests that exercise GET /search.
func EnsureMetadataSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), metadataSchemaDDL); err != nil {
		t.Fatalf("create metadata schema: %v", err)
	}
}

// NewRedisURL returns the STATS_TEST_REDIS_URL server's URL (callers must
// namespace or tolerate shared keys).
func NewRedisURL(t *testing.T) string {
	t.Helper()
	external := os.Getenv(redisURLEnv)
	if external == "" {
		t.Fatalf("%s is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers", redisURLEnv)
	}
	return external
}

// NewRedis returns a client connected to the NewRedisURL server.
func NewRedis(t *testing.T) *goredis.Client {
	t.Helper()
	opt, err := goredis.ParseURL(NewRedisURL(t))
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := goredis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}
