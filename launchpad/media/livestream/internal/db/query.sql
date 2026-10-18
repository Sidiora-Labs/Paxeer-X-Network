-- query.sql — sqlc query definitions for media/livestream (streams CRUD + the
-- cross-schema creator lookup). Ported 1:1 from routes/streams.ts drizzle calls.

-- name: GetPoolCreator :one
-- Cross-schema read (indexer.pools) used to verify the caller owns the pool.
SELECT creator FROM indexer.pools WHERE pool_address = $1 LIMIT 1;

-- name: CountActiveStreamsByWallet :one
SELECT count(*) FROM livestream.streams
WHERE creator_wallet = $1 AND is_live = true;

-- name: CreateStream :exec
INSERT INTO livestream.streams (
	id, pool_address, creator_wallet, title,
	livepeer_stream_id, stream_key, playback_id, rtmp_url, playback_url,
	is_live, viewer_count, started_at, ended_at, created_at
) VALUES (
	$1, $2, $3, $4,
	$5, $6, $7, $8, $9,
	false, 0, NULL, NULL, $10
);

-- name: GetStreamByID :one
SELECT * FROM livestream.streams WHERE id = $1 LIMIT 1;

-- name: GetStreamLiveStatus :one
SELECT id, is_live FROM livestream.streams WHERE id = $1 LIMIT 1;

-- name: SetStreamLive :exec
UPDATE livestream.streams SET is_live = true, started_at = $2 WHERE id = $1;

-- name: EndStream :exec
UPDATE livestream.streams SET is_live = false, ended_at = $2 WHERE id = $1;

-- name: SetStreamLiveByLivepeerID :exec
UPDATE livestream.streams SET is_live = true, started_at = $2 WHERE livepeer_stream_id = $1;

-- name: SetStreamIdleByLivepeerID :exec
UPDATE livestream.streams SET is_live = false, ended_at = $2 WHERE livepeer_stream_id = $1;

-- name: UpdateViewerCount :exec
UPDATE livestream.streams SET viewer_count = $2 WHERE id = $1;

-- name: ListPoolStreams :many
-- When live_only is true, only currently-live streams are returned; otherwise
-- all streams for the pool. (Collapses the TS conditional drizzle query.)
SELECT id, pool_address, creator_wallet, title, playback_url, playback_id,
       is_live, viewer_count, started_at, ended_at, created_at
FROM livestream.streams
WHERE pool_address = sqlc.arg(pool_address)
  AND (NOT sqlc.arg(live_only)::boolean OR is_live = true)
ORDER BY created_at DESC
LIMIT 20;

-- name: ListLiveStreams :many
SELECT id, pool_address, creator_wallet, title, playback_url, playback_id,
       is_live, viewer_count, started_at, created_at
FROM livestream.streams
WHERE is_live = true
ORDER BY started_at DESC
LIMIT 50;
