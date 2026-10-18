-- name: GetCursor :one
SELECT chain_id, last_processed_block, updated_at
FROM indexer.cursors
WHERE chain_id = $1;

-- name: UpsertCursor :exec
INSERT INTO indexer.cursors (chain_id, last_processed_block, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (chain_id)
DO UPDATE SET last_processed_block = EXCLUDED.last_processed_block,
              updated_at = now();

-- name: InsertPool :exec
INSERT INTO indexer.pools (
    pool_address, token_address, creator, optical, pool_id,
    nft_id, created_at, created_block, tx_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (pool_address) DO NOTHING;

-- name: GetPoolByPoolID :one
SELECT pool_address, token_address, creator, optical, pool_id,
       nft_id, created_at, created_block, tx_hash
FROM indexer.pools
WHERE pool_id = $1
LIMIT 1;

-- name: GetPoolByAddress :one
SELECT pool_address, token_address, creator, optical, pool_id,
       nft_id, created_at, created_block, tx_hash
FROM indexer.pools
WHERE lower(pool_address) = lower(@pool_address::varchar)
LIMIT 1;

-- name: GetPoolByNftID :one
SELECT pool_address, token_address, creator, optical, pool_id,
       nft_id, created_at, created_block, tx_hash
FROM indexer.pools
WHERE nft_id = @nft_id::bigint
LIMIT 1;

-- name: GetPoolCount :one
SELECT count(*)::bigint FROM indexer.pools;

-- name: GetSwapCount :one
SELECT count(*)::bigint FROM indexer.swaps;

-- name: InsertSwap :exec
INSERT INTO indexer.swaps (
    id, pool_id, pool_address, sender, router, is_buy,
    amount_in, amount_out, fee, price,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (id) DO NOTHING;

-- name: InsertPoolStateSnapshot :exec
INSERT INTO indexer.pool_state_snapshots (
    id, pool_id, virtual_reserve, real_reserve, token_reserve, price,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO NOTHING;

-- name: InsertFeeEvent :exec
INSERT INTO indexer.fee_events (
    id, pool_id, fee_amount, protocol_cut, pool_cut,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO NOTHING;

-- name: InsertFeeDistribution :exec
INSERT INTO indexer.fee_distributions (
    id, pool_id, nft_id, strategy, amount, recipient,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO NOTHING;

-- name: InsertFeeStrategyChange :exec
INSERT INTO indexer.fee_strategy_changes (
    id, pool_id, nft_id, old_strategy, new_strategy,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO NOTHING;

-- name: InsertAirdropClaim :exec
INSERT INTO indexer.airdrop_claims (
    id, pool_id, pool_address, nft_id, holder, amount, epoch, event_name,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO NOTHING;

-- name: InsertOpticalExecution :exec
INSERT INTO indexer.optical_executions (
    id, pool_id, optical, hook_name, data,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO NOTHING;

-- name: InsertTokenForTokenSwap :exec
INSERT INTO indexer.token_for_token_swaps (
    id, sender, token_in, token_out, pool_in, pool_out,
    amount_in, intermediate_usdl, amount_out, fee_in, fee_out,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (id) DO NOTHING;

-- name: InsertConfigUpdate :exec
INSERT INTO indexer.config_updates (
    id, key, old_value, new_value,
    block_number, block_timestamp, tx_hash, log_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING;

-- name: GetActiveBackfillJob :one
SELECT id, chain_id, from_block, to_block, last_processed_block,
       total_blocks, status, error_message, started_at, completed_at, updated_at
FROM indexer.backfill_jobs
WHERE status = 'running'
LIMIT 1;

-- name: InsertBackfillJob :exec
INSERT INTO indexer.backfill_jobs (
    id, chain_id, from_block, to_block, last_processed_block,
    total_blocks, status
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateBackfillProgress :exec
UPDATE indexer.backfill_jobs
SET last_processed_block = $2, updated_at = now()
WHERE id = $1;

-- name: CompleteBackfillJob :exec
UPDATE indexer.backfill_jobs
SET status = 'completed', completed_at = now(), updated_at = now()
WHERE id = $1;

-- name: FailBackfillJob :exec
UPDATE indexer.backfill_jobs
SET status = 'failed', error_message = $2, updated_at = now()
WHERE id = $1;

-- Canonical v1 immutable read order; migration proposal is L06/ingestion.sql.
-- name: ListCanonicalRawLogs :many
SELECT chain_id, transaction_hash, log_index, height, transaction_index,
       block_hash, emitter, topics, data
FROM indexer.indexer_raw_logs
WHERE chain_id = $1 AND height >= $2 AND height <= $3
ORDER BY height, transaction_index, log_index
LIMIT $4;
