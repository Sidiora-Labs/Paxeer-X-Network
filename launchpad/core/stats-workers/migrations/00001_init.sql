-- +goose Up
-- +goose StatementBegin
-- core/stats-workers schema. Byte-identical DDL to the live `stats` Postgres
-- schema (drizzle stats/src/db/schema.ts, the cumulative state after drizzle
-- migrations 0000-0003) so the Go service strangler-cuts over against the SAME
-- database as the TS stats service (invariant i2). All money/amount columns are
-- text (invariant i1 — never float).
CREATE SCHEMA IF NOT EXISTS "stats";
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."pool_stats" (
    "pool_address"             varchar(42) PRIMARY KEY,
    "token_address"            varchar(42) NOT NULL,
    "price"                    text        NOT NULL DEFAULT '0',
    "price_change_1m"          text        NOT NULL DEFAULT '0',
    "price_change_5m"          text        NOT NULL DEFAULT '0',
    "price_change_15m"         text        NOT NULL DEFAULT '0',
    "price_change_1h"          text        NOT NULL DEFAULT '0',
    "price_change_24h"         text        NOT NULL DEFAULT '0',
    "price_change_dollar_1m"   text        NOT NULL DEFAULT '0',
    "price_change_dollar_5m"   text        NOT NULL DEFAULT '0',
    "price_change_dollar_15m"  text        NOT NULL DEFAULT '0',
    "price_change_dollar_1h"   text        NOT NULL DEFAULT '0',
    "price_change_dollar_24h"  text        NOT NULL DEFAULT '0',
    "high_24h"                 text        NOT NULL DEFAULT '0',
    "low_24h"                  text        NOT NULL DEFAULT '0',
    "volume_24h"               text        NOT NULL DEFAULT '0',
    "volume_1h"                text        NOT NULL DEFAULT '0',
    "volume_5m"                text        NOT NULL DEFAULT '0',
    "market_cap"               text        NOT NULL DEFAULT '0',
    "buy_count_24h"            integer     NOT NULL DEFAULT 0,
    "sell_count_24h"           integer     NOT NULL DEFAULT 0,
    "unique_traders_24h"       integer     NOT NULL DEFAULT 0,
    "holder_count"             integer     NOT NULL DEFAULT 0,
    "top10_concentration"      text        NOT NULL DEFAULT '0',
    "creator_holdings_pct"     text        NOT NULL DEFAULT '0',
    "risk_rating"              integer     NOT NULL DEFAULT 50,
    "risk_factors"             text        DEFAULT '[]',
    "creator_address"          varchar(42),
    "created_at"               bigint      NOT NULL,
    "updated_at"               bigint      NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_ps_volume_24h"   ON "stats"."pool_stats" USING btree ("volume_24h");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ps_price_change" ON "stats"."pool_stats" USING btree ("price_change_24h");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ps_mcap"         ON "stats"."pool_stats" USING btree ("market_cap");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."price_snapshots" (
    "pool_address" varchar(42) NOT NULL,
    "minute_ts"    bigint      NOT NULL,
    "price"        text        NOT NULL,
    PRIMARY KEY ("pool_address", "minute_ts")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."pool_holders" (
    "pool_address"   varchar(42) NOT NULL,
    "holder_address" varchar(42) NOT NULL,
    "balance"        text        NOT NULL,
    "pct_of_supply"  text        NOT NULL,
    "last_updated"   bigint      NOT NULL,
    PRIMARY KEY ("pool_address", "holder_address")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_ph_balance" ON "stats"."pool_holders" USING btree ("pool_address", "balance");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."pool_transactions" (
    "id"              text        PRIMARY KEY,
    "pool_address"    varchar(42) NOT NULL,
    "sender"          varchar(42) NOT NULL,
    "is_buy"          boolean     NOT NULL,
    "amount_in"       text        NOT NULL,
    "amount_out"      text        NOT NULL,
    "price"           text        NOT NULL,
    "fee"             text        NOT NULL,
    "block_timestamp" bigint      NOT NULL,
    "tx_hash"         varchar(66) NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_pt_pool"   ON "stats"."pool_transactions" USING btree ("pool_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pt_ts"     ON "stats"."pool_transactions" USING btree ("block_timestamp");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pt_sender" ON "stats"."pool_transactions" USING btree ("sender");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."cross_token_swaps" (
    "id"                text        PRIMARY KEY,
    "sender"            varchar(42) NOT NULL,
    "token_in"          varchar(42) NOT NULL,
    "token_out"         varchar(42) NOT NULL,
    "pool_in"           varchar(42) NOT NULL,
    "pool_out"          varchar(42) NOT NULL,
    "amount_in"         text        NOT NULL,
    "intermediate_usdl" text        NOT NULL,
    "amount_out"        text        NOT NULL,
    "fee_in"            text        NOT NULL,
    "fee_out"           text        NOT NULL,
    "block_timestamp"   bigint      NOT NULL,
    "tx_hash"           varchar(66) NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_cts_sender"    ON "stats"."cross_token_swaps" USING btree ("sender");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_cts_token_in"  ON "stats"."cross_token_swaps" USING btree ("token_in");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_cts_token_out" ON "stats"."cross_token_swaps" USING btree ("token_out");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_cts_ts"        ON "stats"."cross_token_swaps" USING btree ("block_timestamp");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."volume_buckets" (
    "pool_address" varchar(42) NOT NULL,
    "bucket_start" bigint      NOT NULL,
    "volume_usdl"  text        NOT NULL DEFAULT '0',
    "buy_count"    integer     NOT NULL DEFAULT 0,
    "sell_count"   integer     NOT NULL DEFAULT 0,
    "high_price"   text        NOT NULL DEFAULT '0',
    "low_price"    text        NOT NULL DEFAULT '0',
    PRIMARY KEY ("pool_address", "bucket_start")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "stats"."bucket_traders" (
    "pool_address" varchar(42) NOT NULL,
    "bucket_start" bigint      NOT NULL,
    "bucket_size"  integer     NOT NULL DEFAULT 60,
    "trader"       varchar(42) NOT NULL,
    PRIMARY KEY ("pool_address", "bucket_start", "bucket_size", "trader")
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."bucket_traders";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."volume_buckets";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."cross_token_swaps";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."pool_transactions";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."pool_holders";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."price_snapshots";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "stats"."pool_stats";
-- +goose StatementEnd
-- +goose StatementBegin
DROP SCHEMA IF EXISTS "stats";
-- +goose StatementEnd
