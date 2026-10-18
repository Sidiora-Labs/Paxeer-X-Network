// Package internaltest provides real infrastructure (Postgres, Redis) for the
// social service's integration tests — never fakes and never containers: the
// helpers use the already running servers named by SOCIAL_TEST_POSTGRES_DSN /
// SOCIAL_TEST_REDIS_URL and fail the test immediately when those are unset.
// It is imported only by *_test.go files, so it adds nothing to the production
// binary.
package internaltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/migrate"
)

// PostgresDSNEnv / RedisURLEnv name the already running Postgres / Redis the
// tests use.
//
//   - PostgresDSNEnv is an admin DSN whose role may CREATE DATABASE. Each call
//     gets a throwaway database, dropped WITH (FORCE) on cleanup.
//   - RedisURLEnv is a redis:// URL that must name the logical database
//     reserved for these tests; it is flushed before and after each test, so
//     tests sharing it must not run concurrently.
const (
	PostgresDSNEnv = "SOCIAL_TEST_POSTGRES_DSN"
	RedisURLEnv    = "SOCIAL_TEST_REDIS_URL"
)

// NewPostgres creates a throwaway database on the PostgresDSNEnv server,
// applies the social goose migrations, creates the cross-schema indexer.pools
// table (owned by the indexer service in prod; created here so creator lookups
// resolve), and returns a ready pgx pool. Torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin := os.Getenv(PostgresDSNEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers", PostgresDSNEnv)
	}
	dsn := throwawayDatabase(t, admin)
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

	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS indexer;
		CREATE TABLE IF NOT EXISTS indexer.pools (
			pool_address  varchar(42) PRIMARY KEY,
			token_address varchar(42) NOT NULL,
			creator       varchar(42) NOT NULL,
			created_at    bigint NOT NULL
		);`); err != nil {
		t.Fatalf("create indexer.pools: %v", err)
	}
	return pool
}

// throwawayDatabase creates a uniquely named database on the server admin
// points at and drops it (terminating leftover sessions) at cleanup.
func throwawayDatabase(t *testing.T, admin string) string {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", PostgresDSNEnv, err)
	}
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "social_test_" + hex.EncodeToString(suffix)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", PostgresDSNEnv, err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		c, e := pgx.ConnectConfig(context.Background(), cfg)
		if e != nil {
			t.Logf("drop database %s: %v", name, e)
			return
		}
		defer c.Close(context.Background())
		if _, e = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Logf("drop database %s: %v", name, e)
		}
	})
	u, err := url.Parse(admin)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("%s must be a postgres:// URL", PostgresDSNEnv)
	}
	u.Path = "/" + name
	return u.String()
}

// SeedPool inserts a row into indexer.pools so creator-authorized deletes resolve.
func SeedPool(t *testing.T, pool *pgxpool.Pool, poolAddr, tokenAddr, creator string, createdAt int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO indexer.pools (pool_address, token_address, creator, created_at) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (pool_address) DO UPDATE SET token_address = EXCLUDED.token_address, creator = EXCLUDED.creator, created_at = EXCLUDED.created_at`,
		poolAddr, tokenAddr, creator, createdAt); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

// NewRedisURL returns the RedisURLEnv URL after flushing its logical database;
// the database is flushed again on cleanup.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	uri := os.Getenv(RedisURLEnv)
	if uri == "" {
		t.Fatalf("%s is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers", RedisURLEnv)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse %s: %v", RedisURLEnv, err)
	}
	pinned := strings.Trim(u.Path, "/")
	if pinned == "" {
		t.Fatalf("%s must name a logical database reserved for these tests (redis://:password@host:port/N)", RedisURLEnv)
	}
	if db, convErr := strconv.Atoi(pinned); convErr != nil || db < 0 || db > 15 {
		t.Fatalf("%s database %q is not 0..15", RedisURLEnv, pinned)
	}
	opt, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatalf("parse %s: %v", RedisURLEnv, err)
	}
	flush := func() {
		client := goredis.NewClient(opt)
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Errorf("flush %s: %v", RedisURLEnv, err)
		}
	}
	flush()
	t.Cleanup(flush)
	return uri
}
