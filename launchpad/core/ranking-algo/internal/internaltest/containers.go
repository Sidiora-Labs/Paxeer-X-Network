// Package internaltest provides real ephemeral infrastructure (Postgres, Redis)
// for the ranking-algo integration tests — never fakes (house rule no_stub)
// and never containers: the helpers use the already running servers named by
// RANKING_TEST_POSTGRES_DSN / RANKING_TEST_REDIS_URL and fail the test
// immediately when those are unset. ranking-algo owns no schema of its own, so this helper
// creates the minimal, faithful subset of the read-only tables it queries
// (stats.pool_stats, indexer.pools) using the same column names/types as the
// live schemas (stats/src/db/schema.ts, indexer pools). It is imported only by
// *_test.go files, so it adds nothing to the production binary and is invisible
// to the per-package coverage gate.
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
)

// readSchemaDDL creates the read-only tables ranking-algo queries. The columns
// and types match the live stats.pool_stats and indexer.pools tables 1:1 for the
// fields the rankers read (invariant i2: same DB as the TS services).
const readSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS stats;
CREATE TABLE stats.pool_stats (
	pool_address       varchar(42) PRIMARY KEY,
	volume_24h         text    NOT NULL DEFAULT '0',
	volume_1h          text    NOT NULL DEFAULT '0',
	volume_5m          text    NOT NULL DEFAULT '0',
	market_cap         text    NOT NULL DEFAULT '0',
	price_change_24h   text    NOT NULL DEFAULT '0',
	buy_count_24h      integer NOT NULL DEFAULT 0,
	sell_count_24h     integer NOT NULL DEFAULT 0,
	unique_traders_24h integer NOT NULL DEFAULT 0,
	holder_count       integer NOT NULL DEFAULT 0,
	updated_at         bigint  NOT NULL
);
CREATE SCHEMA IF NOT EXISTS indexer;
CREATE TABLE indexer.pools (
	pool_address varchar(42) PRIMARY KEY,
	created_at   bigint NOT NULL
);
`

// NewPostgres creates a throwaway database on the PostgresDSNEnv server,
// creates the read-only stats.pool_stats and indexer.pools tables, and returns
// a ready pgx pool. Both are torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := NewPostgresWithDSN(t)
	return pool
}

// NewPostgresWithDSN is NewPostgres but also returns the connection DSN, for
// callers (e.g. the app integration test) that build their own pool from config.
func NewPostgresWithDSN(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv(PostgresDSNEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers", PostgresDSNEnv)
	}
	dsn := NewExternalDatabase(t, admin)
	return dsn, readSchemaPool(t, context.Background(), dsn)
}

// PostgresDSNEnv / RedisURLEnv name the already-running Postgres / Redis the
// tests use (containers are never started).
const (
	PostgresDSNEnv = "RANKING_TEST_POSTGRES_DSN"
	RedisURLEnv    = "RANKING_TEST_REDIS_URL"
)

// NewExternalDatabase creates a uniquely named database on the server behind
// admin (the role needs CREATEDB), drops it (FORCE) when the test ends and
// returns its DSN.
func NewExternalDatabase(t *testing.T, admin string) string {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", PostgresDSNEnv, err)
	}
	var suffix [6]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "ranking_test_" + hex.EncodeToString(suffix[:])
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", PostgresDSNEnv, err)
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
		t.Fatalf("%s must be a postgres:// URL", PostgresDSNEnv)
	}
	u.Path = "/" + name
	return u.String()
}

// readSchemaPool opens a pool on dsn (closed on cleanup) and creates the
// read-only tables the legacy rankers query.
func readSchemaPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
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

	if _, err := pool.Exec(ctx, readSchemaDDL); err != nil {
		t.Fatalf("create read schema: %v", err)
	}
	return pool
}

// redisLockKey serialises every Redis-backed test on the reserved logical
// database: go test runs packages in parallel processes and the ranker and
// httpapi tests read and write the same ranking:* keys.
const redisLockKey = "ranking-test:exclusive"

// releaseRedisLock deletes the lock only while it still holds this test's token.
var releaseRedisLock = goredis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

// NewRedisURL returns the RedisURLEnv server's URL after taking exclusive use
// of its logical database for the rest of the test (released on cleanup) and
// clearing every key a previous test left there, so each test starts from an
// empty database whatever ran before it in this or another package.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	external := os.Getenv(RedisURLEnv)
	if external == "" {
		t.Fatalf("%s is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers", RedisURLEnv)
	}
	opt, err := goredis.ParseURL(external)
	if err != nil {
		t.Fatalf("parse %s: %v", RedisURLEnv, err)
	}
	rdb := goredis.NewClient(opt)
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	owner := hex.EncodeToString(token[:])
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Minute)
	for {
		acquired, err := rdb.SetNX(ctx, redisLockKey, owner, 5*time.Minute).Result()
		if err != nil {
			_ = rdb.Close()
			t.Fatalf("lock %s: %v", RedisURLEnv, err)
		}
		if acquired {
			break
		}
		if time.Now().After(deadline) {
			_ = rdb.Close()
			t.Fatalf("%s stayed locked by another test for 5 minutes", RedisURLEnv)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = releaseRedisLock.Run(context.Background(), rdb, []string{redisLockKey}, owner).Err()
		_ = rdb.Close()
	})
	iter := rdb.Scan(ctx, 0, "*", 1000).Iterator()
	for iter.Next(ctx) {
		if key := iter.Val(); key != redisLockKey {
			if err = rdb.Del(ctx, key).Err(); err != nil {
				t.Fatalf("clear %s: %v", RedisURLEnv, err)
			}
		}
	}
	if err = iter.Err(); err != nil {
		t.Fatalf("clear %s: %v", RedisURLEnv, err)
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
