// Package internaltest provides real ephemeral infrastructure (Postgres, Redis)
// for the indexer's integration tests — never fakes and never containers. A
// migrated throwaway Postgres database and a live Redis URL on the already
// running servers named by INDEXER_TEST_POSTGRES_URL / INDEXER_TEST_REDIS_URL
// are provided per call and torn down through t.Cleanup; a helper whose
// variable is unset fails the test immediately. It is imported only by *_test.go files, so it adds nothing
// to the production binary and is invisible to the per-package coverage gate.
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

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/migrate"
)

// NewPostgres creates a throwaway database on the ExternalPostgresEnv server,
// applies the indexer goose migrations, and returns the connection DSN plus a
// ready pgx pool. Both the database and the pool are torn down via t.Cleanup.
func NewPostgres(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv(ExternalPostgresEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers", ExternalPostgresEnv)
	}
	return externalPostgres(t, admin)
}

// NewRedisURL returns the ExternalRedisEnv URL; its logical database is
// flushed on cleanup.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv(ExternalRedisEnv)
	if base == "" {
		t.Fatalf("%s is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers", ExternalRedisEnv)
	}
	return externalRedis(t, base)
}

// The helpers run against an already running Postgres/Redis (real servers,
// never fakes, never containers):
//
//   - ExternalPostgresEnv is a DSN whose role may CREATE DATABASE. Each test
//     gets a throwaway database migrated with the indexer goose migrations
//     (fixed schema name "indexer"), dropped WITH (FORCE) on cleanup.
//   - ExternalRedisEnv is a redis:// URL naming the logical database reserved
//     for this module's tests; it is flushed on cleanup, so tests sharing it
//     must not run concurrently across packages (go test -p 1).
const (
	ExternalPostgresEnv = "INDEXER_TEST_POSTGRES_URL"
	ExternalRedisEnv    = "INDEXER_TEST_REDIS_URL"
)

func externalPostgres(t *testing.T, admin string) (string, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "indexer_test_" + hex.EncodeToString(suffix)
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect external postgres: %v", err)
	}
	defer conn.Close(context.Background())
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create throwaway database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c, err := pgx.Connect(cleanup, admin)
		if err != nil {
			t.Logf("drop throwaway database %s: %v", name, err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(cleanup, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop throwaway database %s: %v", name, err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()
	if err = migrate.Up(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
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
	return dsn, pool
}

func externalRedis(t *testing.T, base string) string {
	t.Helper()
	opt, err := goredis.ParseURL(base)
	if err != nil {
		t.Fatalf("parse external redis url: %v", err)
	}
	client := goredis.NewClient(opt)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Logf("flush external redis db: %v", err)
		}
		_ = client.Close()
	})
	return base
}
