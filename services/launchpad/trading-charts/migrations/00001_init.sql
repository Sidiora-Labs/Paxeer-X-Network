-- +goose Up
-- +goose StatementBegin
-- core/trading-charts schema. Byte-identical DDL to the live `candles` Postgres
-- schema (drizzle candles/src/db/schema.ts) so the Go service strangler-cuts
-- over against the SAME database as the TS candles service (invariant i2).
CREATE SCHEMA IF NOT EXISTS "candles";
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "candles"."candles" (
    "pool_address"      varchar(42) NOT NULL,
    "timeframe"         varchar(4)  NOT NULL,
    "candle_start"      bigint      NOT NULL,
    "open"              text        NOT NULL,
    "high"              text        NOT NULL,
    "low"               text        NOT NULL,
    "close"             text        NOT NULL,
    "volume_usdl"       text        NOT NULL DEFAULT '0',
    "volume_token"      text        NOT NULL DEFAULT '0',
    "buy_volume_usdl"   text        NOT NULL DEFAULT '0',
    "sell_volume_usdl"  text        NOT NULL DEFAULT '0',
    "trade_count"       integer     NOT NULL DEFAULT 0,
    "unique_traders"    integer     NOT NULL DEFAULT 0,
    "large_trade_count" integer     NOT NULL DEFAULT 0,
    "last_trade_ts"     bigint      NOT NULL,
    "sequence_num"      bigint      NOT NULL,
    "mcap_open"         text        NOT NULL DEFAULT '0',
    "mcap_high"         text        NOT NULL DEFAULT '0',
    "mcap_low"          text        NOT NULL DEFAULT '0',
    "mcap_close"        text        NOT NULL DEFAULT '0',
    PRIMARY KEY ("pool_address", "timeframe", "candle_start")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_candles_seq" ON "candles"."candles" ("pool_address", "timeframe", "sequence_num");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "candles"."candle_traders" (
    "pool_address"   varchar(42) NOT NULL,
    "timeframe"      varchar(4)  NOT NULL,
    "candle_start"   bigint      NOT NULL,
    "trader_address" varchar(42) NOT NULL,
    PRIMARY KEY ("pool_address", "timeframe", "candle_start", "trader_address")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_ct_candle" ON "candles"."candle_traders" ("pool_address", "timeframe", "candle_start");
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "candles"."candle_cursors" (
    "pool_address"       varchar(42) NOT NULL,
    "timeframe"          varchar(4)  NOT NULL,
    "last_close"         text        NOT NULL,
    "last_candle_start"  bigint      NOT NULL,
    "last_sequence_num"  bigint      NOT NULL,
    PRIMARY KEY ("pool_address", "timeframe")
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "candles"."candle_cursors";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "candles"."candle_traders";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "candles"."candles";
-- +goose StatementEnd
-- +goose StatementBegin
DROP SCHEMA IF EXISTS "candles";
-- +goose StatementEnd
