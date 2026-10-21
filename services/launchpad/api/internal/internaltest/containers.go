// Package internaltest provides real ephemeral infrastructure (Postgres, Redis)
// for the core/api integration tests — never fakes (house rule no_stub) and
// never containers: the helpers use the already running servers named by
// CORE_API_TEST_POSTGRES_URL / CORE_API_TEST_REDIS_URL and fail the test
// immediately when those are unset. core/api owns NO schema of its own: it READS the schemas the
// core/* services write (candles, stats, indexer). This helper creates the
// faithful read-only subset of those tables core/api queries, using the same
// column names/types as the live schemas (invariant i2) so the snapshot REST
// reads exercise real SQL against a real database.
//
// It is imported only by *_test.go files, so it adds nothing to the production
// binary and is invisible to the per-package coverage gate.
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
)

// readSchemaDDL creates the read-only tables core/api queries. Columns/types
// match the live candles.candles, stats.pool_stats, stats.pool_holders,
// stats.pool_transactions, indexer.pools, indexer.pool_state_snapshots, indexer.fee_distributions,
// indexer.fee_strategy_changes, indexer.airdrop_claims and
// indexer.indexer_canonical_creations tables 1:1 for the fields the gateway
// reads (invariant i2). The committed-heads tables come from the live
// 60001 migration, which the production read fixtures apply verbatim.
const readSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS candles;
CREATE TABLE candles.candles (
	pool_address      varchar(42) NOT NULL,
	timeframe         varchar(4)  NOT NULL,
	candle_start      bigint      NOT NULL,
	open              text        NOT NULL,
	high              text        NOT NULL,
	low               text        NOT NULL,
	close             text        NOT NULL,
	volume_usdl       text        NOT NULL DEFAULT '0',
	volume_token      text        NOT NULL DEFAULT '0',
	buy_volume_usdl   text        NOT NULL DEFAULT '0',
	sell_volume_usdl  text        NOT NULL DEFAULT '0',
	trade_count       integer     NOT NULL DEFAULT 0,
	unique_traders    integer     NOT NULL DEFAULT 0,
	large_trade_count integer     NOT NULL DEFAULT 0,
	last_trade_ts     bigint      NOT NULL,
	sequence_num      bigint      NOT NULL,
	mcap_open         text        NOT NULL DEFAULT '0',
	mcap_high         text        NOT NULL DEFAULT '0',
	mcap_low          text        NOT NULL DEFAULT '0',
	mcap_close        text        NOT NULL DEFAULT '0',
	PRIMARY KEY (pool_address, timeframe, candle_start)
);
CREATE INDEX idx_candles_seq ON candles.candles (pool_address, timeframe, sequence_num);

CREATE SCHEMA IF NOT EXISTS stats;
CREATE TABLE stats.pool_stats (
	pool_address            varchar(42) PRIMARY KEY,
	token_address           varchar(42) NOT NULL,
	price                   text    NOT NULL DEFAULT '0',
	price_change_1m         text    NOT NULL DEFAULT '0',
	price_change_5m         text    NOT NULL DEFAULT '0',
	price_change_15m        text    NOT NULL DEFAULT '0',
	price_change_1h         text    NOT NULL DEFAULT '0',
	price_change_24h        text    NOT NULL DEFAULT '0',
	price_change_dollar_1m  text    NOT NULL DEFAULT '0',
	price_change_dollar_5m  text    NOT NULL DEFAULT '0',
	price_change_dollar_15m text    NOT NULL DEFAULT '0',
	price_change_dollar_1h  text    NOT NULL DEFAULT '0',
	price_change_dollar_24h text    NOT NULL DEFAULT '0',
	high_24h                text    NOT NULL DEFAULT '0',
	low_24h                 text    NOT NULL DEFAULT '0',
	volume_24h              text    NOT NULL DEFAULT '0',
	volume_1h               text    NOT NULL DEFAULT '0',
	volume_5m               text    NOT NULL DEFAULT '0',
	market_cap              text    NOT NULL DEFAULT '0',
	buy_count_24h           integer NOT NULL DEFAULT 0,
	sell_count_24h          integer NOT NULL DEFAULT 0,
	unique_traders_24h      integer NOT NULL DEFAULT 0,
	holder_count            integer NOT NULL DEFAULT 0,
	top10_concentration     text    NOT NULL DEFAULT '0',
	creator_holdings_pct    text    NOT NULL DEFAULT '0',
	risk_rating             integer NOT NULL DEFAULT 50,
	risk_factors            text    DEFAULT '[]',
	creator_address         varchar(42),
	created_at              bigint  NOT NULL,
	updated_at              bigint  NOT NULL
);
CREATE TABLE stats.pool_holders (
	pool_address   varchar(42) NOT NULL,
	holder_address varchar(42) NOT NULL,
	balance        text        NOT NULL,
	pct_of_supply  text        NOT NULL,
	last_updated   bigint      NOT NULL,
	PRIMARY KEY (pool_address, holder_address)
);
CREATE INDEX idx_ph_balance ON stats.pool_holders (pool_address, balance);
CREATE TABLE stats.pool_transactions (
	id              text        PRIMARY KEY,
	pool_address    varchar(42) NOT NULL,
	sender          varchar(42) NOT NULL,
	is_buy          boolean     NOT NULL,
	amount_in       text        NOT NULL,
	amount_out      text        NOT NULL,
	price           text        NOT NULL,
	fee             text        NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL
);
CREATE INDEX idx_pt_pool ON stats.pool_transactions (pool_address);
CREATE INDEX idx_pt_ts ON stats.pool_transactions (block_timestamp);

CREATE SCHEMA IF NOT EXISTS indexer;
CREATE TABLE indexer.pools (
	pool_address  varchar(42) PRIMARY KEY NOT NULL,
	token_address varchar(42) NOT NULL,
	creator       varchar(42) NOT NULL,
	optical       varchar(42) NOT NULL,
	pool_id       varchar(66) NOT NULL,
	nft_id        bigint,
	created_at    bigint      NOT NULL,
	created_block bigint      NOT NULL,
	tx_hash       varchar(66) NOT NULL
);
CREATE TABLE indexer.pool_state_snapshots (
	id              text        PRIMARY KEY NOT NULL,
	pool_id         varchar(66) NOT NULL,
	virtual_reserve text        NOT NULL,
	real_reserve    text        NOT NULL,
	token_reserve   text        NOT NULL,
	price           text        NOT NULL,
	block_number    bigint      NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL,
	log_index       integer     NOT NULL
);
CREATE TABLE indexer.indexer_canonical_creations (
	token text PRIMARY KEY, pool text NOT NULL UNIQUE, creator text NOT NULL,
	factory text NOT NULL, optical text NOT NULL, nft_id numeric(78,0) NOT NULL,
	height bigint NOT NULL CHECK(height>=0), block_hash text NOT NULL,
	transaction_hash text NOT NULL, transaction_index bigint NOT NULL CHECK(transaction_index BETWEEN 0 AND 4294967295),
	log_index bigint NOT NULL CHECK(log_index BETWEEN 0 AND 4294967295),
	block_timestamp bigint NOT NULL CHECK(block_timestamp>0), provenance jsonb NOT NULL, payload jsonb NOT NULL,
	UNIQUE(transaction_hash,log_index)
);
CREATE VIEW indexer.indexer_canonical_creations_v1 AS SELECT token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload FROM indexer.indexer_canonical_creations;
CREATE TABLE indexer.fee_distributions (
	id              text        PRIMARY KEY NOT NULL,
	pool_id         varchar(66) NOT NULL,
	nft_id          bigint      NOT NULL,
	strategy        integer     NOT NULL,
	amount          text        NOT NULL,
	recipient       varchar(42) NOT NULL,
	block_number    bigint      NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL,
	log_index       integer     NOT NULL
);
CREATE INDEX idx_fd_pool_id ON indexer.fee_distributions (pool_id);
CREATE TABLE indexer.fee_strategy_changes (
	id              text        PRIMARY KEY NOT NULL,
	pool_id         varchar(66) NOT NULL,
	nft_id          bigint      NOT NULL,
	old_strategy    integer     NOT NULL,
	new_strategy    integer     NOT NULL,
	block_number    bigint      NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL,
	log_index       integer     NOT NULL
);
CREATE TABLE indexer.airdrop_claims (
	id              text        PRIMARY KEY NOT NULL,
	pool_id         varchar(66),
	pool_address    varchar(42),
	nft_id          bigint,
	holder          varchar(42) NOT NULL,
	amount          text        NOT NULL,
	epoch           text,
	event_name      varchar(32) NOT NULL,
	block_number    bigint      NOT NULL,
	block_timestamp bigint      NOT NULL,
	tx_hash         varchar(66) NOT NULL,
	log_index       integer     NOT NULL
);
CREATE INDEX idx_ac_pool_id ON indexer.airdrop_claims (pool_id);
CREATE INDEX idx_ac_holder ON indexer.airdrop_claims (holder);
CREATE INDEX idx_ac_block ON indexer.airdrop_claims (block_number);
`

// NewPostgres creates a throwaway database on the ExternalPostgresEnv server,
// creates the read-only schema core/api queries, and returns a ready pgx pool.
// The database and pool are torn down via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := NewPostgresWithDSN(t)
	return pool
}

// NewPostgresWithDSN is NewPostgres but also returns the DSN, for callers that
// build their own pool from config (e.g. the app integration test).
func NewPostgresWithDSN(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv(ExternalPostgresEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers", ExternalPostgresEnv)
	}
	return externalPostgres(t, admin)
}

// NewRedisURL returns the ExternalRedisEnv URL (which must name a logical
// database); that database is flushed on cleanup.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv(ExternalRedisEnv)
	if base == "" {
		t.Fatalf("%s is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers", ExternalRedisEnv)
	}
	return externalRedis(t, base)
}

// NewRedis returns a client connected to the NewRedisURL database.
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

// The helpers run against an already running Postgres/Redis (real servers,
// never fakes, never containers):
//
//   - ExternalPostgresEnv is a DSN whose role may CREATE DATABASE. Each test
//     gets a throwaway database (the read schema creates fixed schema names, so
//     a schema per test would collide), dropped WITH (FORCE) on cleanup.
//   - ExternalRedisEnv is a redis:// URL that must name a logical database
//     (redis://.../5); every test is pinned to that database, so a shared
//     server's other databases are never touched. It is flushed on cleanup.
//     Pub/sub channels are global in Redis, so tests sharing one server must
//     not run concurrently.
const (
	ExternalPostgresEnv = "CORE_API_TEST_POSTGRES_URL"
	ExternalRedisEnv    = "CORE_API_TEST_REDIS_URL"
)

func externalPostgres(t *testing.T, admin string) (string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "core_api_test_" + hex.EncodeToString(suffix)
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect %s: %v", ExternalPostgresEnv, err)
	}
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("create throwaway database: %v", err)
	}
	_ = conn.Close(ctx)
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Logf("drop %s: %v", name, err)
			return
		}
		defer c.Close(ctx)
		if _, err = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", ExternalPostgresEnv, err)
	}
	u.Path = "/" + name
	dsn := u.String()
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
	return dsn, pool
}

func externalRedis(t *testing.T, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", ExternalRedisEnv, err)
	}
	pinned := strings.Trim(u.Path, "/")
	if pinned == "" {
		t.Fatalf("%s must name a logical database reserved for these tests (redis://:password@host:port/N)", ExternalRedisEnv)
	}
	if db, convErr := strconv.Atoi(pinned); convErr != nil || db < 0 || db > 15 {
		t.Fatalf("%s database %q is not 0..15", ExternalRedisEnv, pinned)
	}
	uri := u.String()
	opt, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatalf("parse %s: %v", ExternalRedisEnv, err)
	}
	t.Cleanup(func() {
		rdb := goredis.NewClient(opt)
		defer rdb.Close()
		_ = rdb.FlushDB(context.Background()).Err()
	})
	return uri
}
