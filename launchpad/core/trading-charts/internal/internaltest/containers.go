// Package internaltest provides real infrastructure (Postgres, Redis) for the
// trading-charts integration tests — never fakes (house rule no_stub) and never
// containers: the tests run against already running servers named by
// environment variables.
//
//   - PostgresEnv is a DSN whose role may CREATE DATABASE. Each call gets a
//     throwaway database with the trading-charts goose migrations applied (the
//     migrations create the fixed candles schema, so a schema per test would
//     collide); it is dropped WITH (FORCE) on cleanup.
//   - RedisEnv is a redis:// URL naming the logical database reserved for these
//     tests. It is flushed before and after each test, so tests sharing it must
//     not run concurrently.
//
// It is imported only by *_test.go files, so it adds nothing to the production
// binary.
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

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/migrate"
)

const (
	PostgresEnv = "CHARTS_TEST_POSTGRES_URL"
	RedisEnv    = "CHARTS_TEST_REDIS_URL"
)

// NewDatabase creates a throwaway database on the PostgresEnv server and
// returns its DSN. The database is dropped via t.Cleanup.
func NewDatabase(t *testing.T) string {
	t.Helper()
	admin := os.Getenv(PostgresEnv)
	if admin == "" {
		t.Fatalf("%s must name a PostgreSQL server whose role may CREATE DATABASE", PostgresEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "charts_test_" + hex.EncodeToString(suffix)
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect %s: %v", PostgresEnv, err)
	}
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("create throwaway database: %v", err)
	}
	_ = conn.Close(ctx)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		c, err := pgx.Connect(cleanup, admin)
		if err != nil {
			t.Errorf("drop %s: %v", name, err)
			return
		}
		defer c.Close(cleanup)
		if _, err = c.Exec(cleanup, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", PostgresEnv, err)
	}
	u.Path = "/" + name
	return u.String()
}

// OpenPool opens a bounded pgx pool on dsn, closed via t.Cleanup.
func OpenPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.MaxConns = 8
	cfg.MaxConnIdleTime = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// NewPostgres creates a throwaway database, applies the trading-charts goose
// migrations (the candles schema), and returns its DSN plus a ready pool.
func NewPostgres(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	dsn := NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrate.Up(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return dsn, OpenPool(t, dsn)
}

// NewRedisURL returns the RedisEnv URL after flushing its logical database;
// the database is flushed again on cleanup.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	uri := os.Getenv(RedisEnv)
	if uri == "" {
		t.Fatalf("%s must name the redis:// logical database reserved for these tests", RedisEnv)
	}
	opt, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatalf("parse %s: %v", RedisEnv, err)
	}
	flush := func() {
		client := goredis.NewClient(opt)
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Errorf("flush %s: %v", RedisEnv, err)
		}
	}
	flush()
	t.Cleanup(flush)
	return uri
}
