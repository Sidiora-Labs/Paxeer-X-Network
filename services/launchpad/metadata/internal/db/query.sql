-- query.sql — sqlc query definitions for media/metadata. Ported from the TS
-- index.ts drizzle calls. All token-address matches are case-insensitive
-- (LOWER(...)); writes are lowercased by the handler so reads stay stable.

-- name: GetPoolByToken :one
-- Cross-schema read (indexer.pools) — verifies the token exists + creator gate.
SELECT pool_address, token_address, creator
FROM indexer.pools
WHERE LOWER(token_address) = $1
LIMIT 1;

-- name: GetTokenMetadata :one
SELECT * FROM metadata.token_metadata
WHERE LOWER(token_address) = $1
LIMIT 1;

-- name: GetTokenImages :many
SELECT * FROM metadata.token_images
WHERE LOWER(token_address) = $1;

-- name: GetImageByType :one
SELECT * FROM metadata.token_images
WHERE LOWER(token_address) = $1 AND image_type = $2
LIMIT 1;

-- name: ListTokenMetadataByAddrs :many
SELECT * FROM metadata.token_metadata
WHERE LOWER(token_address) = ANY(sqlc.arg(addrs)::text[]);

-- name: ListTokenImagesByAddrs :many
SELECT * FROM metadata.token_images
WHERE LOWER(token_address) = ANY(sqlc.arg(addrs)::text[]);

-- name: ListPoolsByTokenAddrs :many
SELECT pool_address, token_address, creator FROM indexer.pools
WHERE LOWER(token_address) = ANY(sqlc.arg(addrs)::text[]);

-- name: UpsertTokenMetadata :exec
INSERT INTO metadata.token_metadata (
	token_address, pool_address, name, symbol, description,
	website, twitter, telegram, discord, custom_tags,
	decimals, created_by, created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5,
	$6, $7, $8, $9, $10,
	$11, $12, $13, $14
)
ON CONFLICT (token_address) DO UPDATE SET
	name        = EXCLUDED.name,
	symbol      = EXCLUDED.symbol,
	description = EXCLUDED.description,
	website     = EXCLUDED.website,
	twitter     = EXCLUDED.twitter,
	telegram    = EXCLUDED.telegram,
	discord     = EXCLUDED.discord,
	custom_tags = EXCLUDED.custom_tags,
	decimals    = EXCLUDED.decimals,
	updated_at  = EXCLUDED.updated_at;

-- name: UpsertTokenImage :exec
INSERT INTO metadata.token_images (
	id, token_address, image_type, storage_key, mime_type, size_bytes, uploaded_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (id) DO UPDATE SET
	storage_key = EXCLUDED.storage_key,
	mime_type   = EXCLUDED.mime_type,
	size_bytes  = EXCLUDED.size_bytes,
	uploaded_at = EXCLUDED.uploaded_at;
