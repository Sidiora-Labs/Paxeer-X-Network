-- +goose Up
-- +goose StatementBegin
-- core/pnl-tracker schema. Mirrors the live `pnl` Postgres schema (drizzle
-- pnl/src/db/schema.ts) so the Go service strangler-cuts over against the SAME
-- database as the TS pnl service (invariant i2). All money/amount columns are
-- text holding uint256/decimal strings (invariant i1 — never float). The realized
-- PnL column is a SIGNED decimal string (it can begin with '-').
CREATE SCHEMA IF NOT EXISTS "pnl";
-- +goose StatementEnd

-- +goose StatementBegin
-- user_positions: one folded position per (user, pool). Aggregates are
-- accumulated by the swap consumer / reconciler via average-cost basis math.
CREATE TABLE "pnl"."user_positions" (
    "user_address"        varchar(42) NOT NULL,
    "pool_address"        varchar(42) NOT NULL,
    "token_address"       varchar(42) NOT NULL DEFAULT '',
    "total_usdl_spent"    text        NOT NULL DEFAULT '0',
    "total_tokens_bought" text        NOT NULL DEFAULT '0',
    "total_usdl_received" text        NOT NULL DEFAULT '0',
    "total_tokens_sold"   text        NOT NULL DEFAULT '0',
    "avg_cost_basis"      text        NOT NULL DEFAULT '0',
    "current_holdings"    text        NOT NULL DEFAULT '0',
    "realized_pnl_usdl"   text        NOT NULL DEFAULT '0',
    "first_buy_ts"        bigint,
    "last_trade_ts"       bigint      NOT NULL DEFAULT 0,
    "trade_count"         integer     NOT NULL DEFAULT 0,
    PRIMARY KEY ("user_address", "pool_address")
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_up_user" ON "pnl"."user_positions" USING btree ("user_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_up_pool" ON "pnl"."user_positions" USING btree ("pool_address");
-- +goose StatementEnd

-- +goose StatementBegin
-- user_trades: per-swap fold rows, the idempotent source the positions fold from
-- (id = txHash-logIndex). Inserts are ON CONFLICT DO NOTHING so webhook
-- redelivery and the reconciler never double-count (invariant i9).
CREATE TABLE "pnl"."user_trades" (
    "id"              text        PRIMARY KEY,
    "user_address"    varchar(42) NOT NULL,
    "pool_address"    varchar(42) NOT NULL,
    "token_address"   varchar(42) NOT NULL DEFAULT '',
    "is_buy"          boolean     NOT NULL,
    "usdl_amount"     text        NOT NULL,
    "token_amount"    text        NOT NULL,
    "price"           text        NOT NULL DEFAULT '0',
    "fee"             text        NOT NULL DEFAULT '0',
    "block_number"    bigint      NOT NULL DEFAULT 0,
    "block_timestamp" bigint      NOT NULL,
    "tx_hash"         varchar(66) NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_ut_user"      ON "pnl"."user_trades" USING btree ("user_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ut_user_pool" ON "pnl"."user_trades" USING btree ("user_address", "pool_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ut_ts"        ON "pnl"."user_trades" USING btree ("block_timestamp");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ut_block"     ON "pnl"."user_trades" USING btree ("block_number");
-- +goose StatementEnd

-- +goose StatementBegin
-- pnl_cards: minted PnL share cards. The snapshot is the immutable JSON captured
-- at mint time (CardSnapshot). short_code is the public referral handle.
CREATE TABLE "pnl"."pnl_cards" (
    "card_id"       text        PRIMARY KEY,
    "short_code"    varchar(16) NOT NULL UNIQUE,
    "owner_address" varchar(42) NOT NULL,
    "pool_address"  varchar(42) NOT NULL,
    "token_address" varchar(42) NOT NULL DEFAULT '',
    "snapshot"      jsonb       NOT NULL,
    "created_at"    bigint      NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_cards_owner" ON "pnl"."pnl_cards" USING btree ("owner_address");
-- +goose StatementEnd

-- +goose StatementBegin
-- referrals: a sharer's short_code -> address binding (created when a card mints).
CREATE TABLE "pnl"."referrals" (
    "short_code"     varchar(16) PRIMARY KEY,
    "sharer_address" varchar(42) NOT NULL,
    "card_id"        text        NOT NULL,
    "created_at"     bigint      NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_ref_sharer" ON "pnl"."referrals" USING btree ("sharer_address");
-- +goose StatementEnd

-- +goose StatementBegin
-- referral_events: the attribution funnel (view / click / wallet_bind /
-- conversion). wallet_bind + conversion are deduplicated per (short_code, wallet)
-- by a partial unique index so the same viewer can't be counted twice.
CREATE TABLE "pnl"."referral_events" (
    "id"             bigserial   PRIMARY KEY,
    "short_code"     varchar(16) NOT NULL,
    "event_type"     varchar(16) NOT NULL,
    "wallet_address" varchar(42),
    "card_id"        text,
    "credited"       boolean     NOT NULL DEFAULT false,
    "created_at"     bigint      NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_re_short" ON "pnl"."referral_events" USING btree ("short_code");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX "uq_re_bind" ON "pnl"."referral_events" ("short_code", "wallet_address", "event_type")
    WHERE "event_type" IN ('wallet_bind', 'conversion');
-- +goose StatementEnd

-- +goose StatementBegin
-- reconciler_cursor: a single-row cursor for the idempotent backfill worker. It
-- tracks the highest indexer.swaps block already folded into pnl positions.
CREATE TABLE "pnl"."reconciler_cursor" (
    "id"             integer PRIMARY KEY DEFAULT 1,
    "last_block"     bigint  NOT NULL DEFAULT 0,
    "last_log_index" bigint  NOT NULL DEFAULT -1,
    "last_swap_id"   text    NOT NULL DEFAULT '',
    "updated_at"     bigint  NOT NULL DEFAULT 0,
    CONSTRAINT "reconciler_cursor_singleton" CHECK ("id" = 1)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO "pnl"."reconciler_cursor" ("id", "last_block", "last_log_index", "last_swap_id", "updated_at")
VALUES (1, 0, -1, '', 0)
ON CONFLICT ("id") DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."reconciler_cursor";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."referral_events";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."referrals";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."pnl_cards";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."user_trades";
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS "pnl"."user_positions";
-- +goose StatementEnd
-- +goose StatementBegin
DROP SCHEMA IF EXISTS "pnl";
-- +goose StatementEnd
