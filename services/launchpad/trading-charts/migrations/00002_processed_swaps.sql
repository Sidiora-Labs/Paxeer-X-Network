-- +goose Up
-- +goose StatementBegin
-- Idempotency ledger for swap folding. The indexer dual-delivers every event
-- over BOTH Redis pub/sub and signed webhooks for redundancy, and trading-charts
-- consumes BOTH paths (Redis swap consumer + webhook receiver), so a swap can
-- arrive twice. The candle builder claims (tx_hash, log_index) inside the SAME
-- transaction as the candle upsert and skips the fold when the row already
-- exists, so volume / trade_count are never double-counted (money invariant
-- i1/i9). Net-new additive table — the TS candles schema has no equivalent, so
-- IF NOT EXISTS keeps the strangler cutover (invariant i2) safe.
CREATE TABLE IF NOT EXISTS "candles"."processed_swaps" (
    "tx_hash"      varchar(66) NOT NULL,
    "log_index"    integer     NOT NULL,
    "processed_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("tx_hash", "log_index")
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "candles"."processed_swaps";
-- +goose StatementEnd
