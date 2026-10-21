-- +goose Up
-- +goose StatementBegin
-- Holder airdrop claims: one row per observed claim settlement. Rows come from
-- the native AirdropClaimed events (FeeAccumulator: pool/holder/amount/epoch,
-- FeesRouter: nftId/holder/amount) and from the EventEmitter FeeFlow mirror with
-- kind=4. pool_id/pool_address are the resolved pool identity (null when the
-- claim could not be attributed to a registered pool); event_name records which
-- emitted shape produced the row so duplicates across sources stay auditable.
CREATE TABLE "indexer"."airdrop_claims" (
	"id" text PRIMARY KEY NOT NULL,
	"pool_id" varchar(66),
	"pool_address" varchar(42),
	"nft_id" bigint,
	"holder" varchar(42) NOT NULL,
	"amount" text NOT NULL,
	"epoch" text,
	"event_name" varchar(32) NOT NULL,
	"block_number" bigint NOT NULL,
	"block_timestamp" bigint NOT NULL,
	"tx_hash" varchar(66) NOT NULL,
	"log_index" integer NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ac_pool_id" ON "indexer"."airdrop_claims" USING btree ("pool_id");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ac_holder" ON "indexer"."airdrop_claims" USING btree ("holder");
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX "idx_ac_block" ON "indexer"."airdrop_claims" USING btree ("block_number");
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "indexer"."airdrop_claims";
-- +goose StatementEnd
