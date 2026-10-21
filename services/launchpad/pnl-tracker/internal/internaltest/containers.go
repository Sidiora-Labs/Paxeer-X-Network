// Package internaltest provides real ephemeral infrastructure (Postgres, Redis)
// for the pnl-tracker integration tests — never fakes (HARD rule no_stub) and
// never containers. A migrated throwaway Postgres database (the pnl schema, via
// the service's own goose migrations) and a live Redis client on the already
// running servers named by PNL_TEST_POSTGRES_DSN / PNL_TEST_REDIS_URL are
// provided per call and torn down through t.Cleanup; a helper whose variable
// is unset fails the test immediately. The Ensure* helpers create faithful subsets of the
// cross-schema tables pnl reads in production (indexer.pools/indexer.swaps,
// stats.pool_stats, metadata.token_metadata) which are owned by other services
// in the SAME database (invariant i2). It is imported only by *_test.go files, so
// it adds nothing to the production binary and is invisible to the coverage gate.
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

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/migrate"
)

// indexerSchemaDDL creates the subset of the indexer schema pnl reads: pools
// (pool -> token resolution) and swaps (the reconciler's backfill source).
const indexerSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS indexer;
CREATE TABLE IF NOT EXISTS indexer.pools (
	pool_address  varchar(42) PRIMARY KEY,
	token_address varchar(42) NOT NULL,
	creator       varchar(42),
	pool_id       text,
	created_at    bigint
);
CREATE TABLE IF NOT EXISTS indexer.swaps (
	id              text        PRIMARY KEY,
	pool_id         text,
	pool_address    varchar(42) NOT NULL,
	sender          varchar(42) NOT NULL,
	router          varchar(42),
	is_buy          boolean     NOT NULL,
	amount_in       text        NOT NULL,
	amount_out      text        NOT NULL,
	fee             text        NOT NULL DEFAULT '0',
	price           text        NOT NULL DEFAULT '0',
	block_number    bigint      NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL,
	log_index       integer     NOT NULL DEFAULT 0
);`

// statsSchemaDDL creates the subset of stats.pool_stats pnl reads for market
// context (price WAD, market cap USDL, 24h change) on cards + portfolio.
const statsSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS stats;
CREATE TABLE IF NOT EXISTS stats.pool_stats (
	pool_address     varchar(42) PRIMARY KEY,
	token_address    varchar(42) NOT NULL,
	price            text        NOT NULL DEFAULT '0',
	market_cap       text        NOT NULL DEFAULT '0',
	price_change_24h text        NOT NULL DEFAULT '0'
);`

// metadataSchemaDDL creates the subset of metadata.token_metadata pnl reads for
// token name/symbol on cards + portfolio.
const metadataSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS metadata;
CREATE TABLE IF NOT EXISTS metadata.token_metadata (
	token_address varchar(42) PRIMARY KEY,
	pool_address  varchar(42),
	name          text,
	symbol        text,
	created_at    bigint
);`

// NewPostgres creates a throwaway database on the PNL_TEST_POSTGRES_DSN server,
// applies the pnl-tracker goose migrations, and returns a ready pgx pool.
// Database + pool are torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := NewPostgresWithDSN(t)
	return pool
}

// NewPostgresWithDSN is NewPostgres but also returns the connection DSN, for
// callers (e.g. the app integration test) that build their own pool from config.
//
// The throwaway database is created on the PNL_TEST_POSTGRES_DSN server (the
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
	postgresDSNEnv = "PNL_TEST_POSTGRES_DSN"
	redisURLEnv    = "PNL_TEST_REDIS_URL"
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
	name := "pnl_test_" + hex.EncodeToString(suffix[:])
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

// migratedPool applies the pnl-tracker migrations to dsn and returns a pool
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

// EnsureIndexerSchema creates indexer.pools + indexer.swaps (pool->token
// resolution + reconciler source). Call from tests that fold swaps.
func EnsureIndexerSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), indexerSchemaDDL); err != nil {
		t.Fatalf("create indexer schema: %v", err)
	}
}

// EnsureStatsSchema creates stats.pool_stats. Call from tests that read market
// context (cards / portfolio).
func EnsureStatsSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), statsSchemaDDL); err != nil {
		t.Fatalf("create stats schema: %v", err)
	}
}

// EnsureMetadataSchema creates metadata.token_metadata. Call from tests that read
// token name/symbol.
func EnsureMetadataSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), metadataSchemaDDL); err != nil {
		t.Fatalf("create metadata schema: %v", err)
	}
}

// NewRedisURL returns the PNL_TEST_REDIS_URL server's URL (callers must
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
