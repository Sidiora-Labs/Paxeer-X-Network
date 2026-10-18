-- query.sql — sqlc query definitions for media/user. Ported from the TS
-- index.ts drizzle calls. All wallet/creator matches are case-insensitive
-- (LOWER(...)); writes are lowercased by the handler so reads stay stable.

-- name: GetUserProfile :one
SELECT * FROM users.user_profiles
WHERE LOWER(wallet_address) = $1
LIMIT 1;

-- name: GetUserImages :many
SELECT * FROM users.user_images
WHERE LOWER(wallet_address) = $1;

-- name: GetImageByType :one
SELECT * FROM users.user_images
WHERE LOWER(wallet_address) = $1 AND image_type = $2
LIMIT 1;

-- name: ListCreatedPools :many
-- Cross-schema read (indexer.pools) — pools created by this wallet.
SELECT pool_address, token_address, created_at FROM indexer.pools
WHERE LOWER(creator) = $1
ORDER BY created_at DESC;

-- name: UpsertUserProfile :exec
INSERT INTO users.user_profiles (
	wallet_address, display_name, bio, twitter, telegram, discord, website,
	created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (wallet_address) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	bio          = EXCLUDED.bio,
	twitter      = EXCLUDED.twitter,
	telegram     = EXCLUDED.telegram,
	discord      = EXCLUDED.discord,
	website      = EXCLUDED.website,
	updated_at   = EXCLUDED.updated_at;

-- name: UpsertUserImage :exec
INSERT INTO users.user_images (
	id, wallet_address, image_type, storage_key, mime_type, size_bytes, uploaded_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (id) DO UPDATE SET
	storage_key = EXCLUDED.storage_key,
	mime_type   = EXCLUDED.mime_type,
	size_bytes  = EXCLUDED.size_bytes,
	uploaded_at = EXCLUDED.uploaded_at;

-- name: GetWatchlist :many
SELECT pool_address, added_at FROM users.watchlists
WHERE LOWER(wallet_address) = $1
ORDER BY added_at DESC;

-- name: AddWatchlist :exec
INSERT INTO users.watchlists (wallet_address, pool_address, added_at)
VALUES ($1, $2, $3)
ON CONFLICT (wallet_address, pool_address) DO NOTHING;

-- name: RemoveWatchlist :exec
DELETE FROM users.watchlists
WHERE LOWER(wallet_address) = $1 AND LOWER(pool_address) = $2;
