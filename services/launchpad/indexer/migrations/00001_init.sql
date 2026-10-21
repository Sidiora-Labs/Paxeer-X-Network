-- +goose Up
-- +goose StatementBegin
-- core/indexer schema. Byte-identical DDL to the live `indexer` Postgres schema
-- (drizzle 0000_demonic_lester.sql + 0001_add_router_to_swaps.sql + the
-- backfill_jobs table from 0001_add_meta_ag_and_backfill_jobs.sql) so the Go
-- service strangler-cuts over against the SAME database as the TS indexer
-- (invariant i2). Meta-AG tables are intentionally omitted (L4 — meta-ag is not
-- on the new chain).
CREATE SCHEMA IF NOT EXISTS "indexer";
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."cursors" (
	"chain_id" integer PRIMARY KEY NOT NULL,
	"last_processed_block" bigint NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."pools" (
	"pool_address" varchar(42) PRIMARY KEY NOT NULL,
	"token_address" varchar(42) NOT NULL,
	"creator" varchar(42) NOT NULL,
	"optical" varchar(42) NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"nft_id" bigint,
	"created_at" bigint NOT NULL,
	"created_block" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."swaps" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"pool_address" varchar(42) NOT NULL,
	"sender" varchar(42) NOT NULL,
	"router" varchar(42),
	"is_buy" boolean NOT NULL,
	"amount_in" text NOT NULL,
	"amount_out" text NOT NULL,
	"fee" text NOT NULL,
	"price" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."pool_state_snapshots" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"virtual_reserve" text NOT NULL,
	"real_reserve" text NOT NULL,
	"token_reserve" text NOT NULL,
	"price" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."fee_events" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"fee_amount" text NOT NULL,
	"protocol_cut" text NOT NULL,
	"pool_cut" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."fee_distributions" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"nft_id" bigint NOT NULL,
	"strategy" integer NOT NULL,
	"amount" text NOT NULL,
	"recipient" varchar(42) NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."fee_strategy_changes" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"nft_id" bigint NOT NULL,
	"old_strategy" integer NOT NULL,
	"new_strategy" integer NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."optical_executions" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66) NOT NULL,
	"optical" varchar(42) NOT NULL,
	"hook_name" text NOT NULL,
	"data" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."token_for_token_swaps" (
	"id" text PRIMARY KEY NOT NULL,
	"sender" varchar(42) NOT NULL,
	"token_in" varchar(42) NOT NULL,
	"token_out" varchar(42) NOT NULL,
	"pool_in" varchar(42) NOT NULL,
	"pool_out" varchar(42) NOT NULL,
	"amount_in" text NOT NULL,
	"intermediate_usdl" text NOT NULL,
	"amount_out" text NOT NULL,
	"fee_in" text NOT NULL,
	"fee_out" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."config_updates" (
	"id" text PRIMARY KEY NOT NULL,
	"key" varchar(66) NOT NULL,
	"old_value" text NOT NULL,
	"new_value" text NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE "indexer"."backfill_jobs" (
	"id" text PRIMARY KEY NOT NULL,
	"chain_id" integer NOT NULL,
	"from_block" bigint NOT NULL,
	"to_block" bigint NOT NULL,
	"last_processed_block" bigint NOT NULL,
	"total_blocks" bigint NOT NULL,
	"status" varchar(16) DEFAULT 'pending' NOT NULL,
	"error_message" text,
	"started_at" timestamp DEFAULT now() NOT NULL,
	"completed_at" timestamp,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX "idx_pools_token" ON "indexer"."pools" USING btree ("token_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pools_creator" ON "indexer"."pools" USING btree ("creator");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX "idx_pools_pool_id" ON "indexer"."pools" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pools_created_block" ON "indexer"."pools" USING btree ("created_block");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_swaps_pool_id" ON "indexer"."swaps" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_swaps_pool_address" ON "indexer"."swaps" USING btree ("pool_address");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_swaps_block" ON "indexer"."swaps" USING btree ("block_number");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_swaps_timestamp" ON "indexer"."swaps" USING btree ("block_timestamp");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_swaps_sender" ON "indexer"."swaps" USING btree ("sender");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX "idx_swaps_unique" ON "indexer"."swaps" USING btree ("block_number","log_index");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pss_pool_id" ON "indexer"."pool_state_snapshots" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_pss_block" ON "indexer"."pool_state_snapshots" USING btree ("block_number");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_fee_pool_id" ON "indexer"."fee_events" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_fd_pool_id" ON "indexer"."fee_distributions" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_tft_sender" ON "indexer"."token_for_token_swaps" USING btree ("sender");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_tft_token_in" ON "indexer"."token_for_token_swaps" USING btree ("token_in");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_tft_token_out" ON "indexer"."token_for_token_swaps" USING btree ("token_out");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_tft_block" ON "indexer"."token_for_token_swaps" USING btree ("block_number");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_tft_timestamp" ON "indexer"."token_for_token_swaps" USING btree ("block_timestamp");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX "idx_tft_unique" ON "indexer"."token_for_token_swaps" USING btree ("block_number","log_index");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_bfj_status" ON "indexer"."backfill_jobs" USING btree ("status");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_bfj_chain" ON "indexer"."backfill_jobs" USING btree ("chain_id");
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP SCHEMA IF EXISTS "indexer" CASCADE;
-- +goose StatementEnd
