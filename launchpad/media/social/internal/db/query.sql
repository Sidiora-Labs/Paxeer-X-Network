-- query.sql — sqlc query definitions for media/social. Ports the TS chat
-- drizzle calls (pool chat, DMs, moderation) and adds the net-new comments +
-- followers surfaces. All wallet/pool values are normalized to lowercase by the
-- handlers on the way in, so matches are EXACT (no LOWER()) to keep the btree
-- indexes sargable under the 500K-concurrency bar.

-- ── Pool chat messages ───────────────────────────────────────────────────────

-- name: ListPoolMessages :many
SELECT id, pool_address, sender, content, reply_to_id, edited_at, created_at
FROM chat.pool_messages
WHERE pool_address = sqlc.arg('pool_address') AND deleted = false
  AND (sqlc.narg('before')::text IS NULL OR id < sqlc.narg('before'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim');

-- name: GetPoolMessage :one
SELECT id, sender, deleted FROM chat.pool_messages
WHERE id = $1 AND pool_address = $2
LIMIT 1;

-- name: InsertPoolMessage :exec
INSERT INTO chat.pool_messages (
	id, pool_address, sender, content, reply_to_id, deleted, created_at
) VALUES ($1, $2, $3, $4, $5, false, $6);

-- name: UpdatePoolMessageContent :exec
UPDATE chat.pool_messages SET content = $2, edited_at = $3 WHERE id = $1;

-- name: SoftDeletePoolMessage :exec
UPDATE chat.pool_messages SET deleted = true WHERE id = $1;

-- ── Direct messages ──────────────────────────────────────────────────────────

-- name: UpsertDmConversation :exec
INSERT INTO chat.dm_conversations (id, wallet_a, wallet_b, last_message_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET last_message_at = EXCLUDED.last_message_at;

-- name: InsertDmMessage :exec
INSERT INTO chat.dm_messages (id, conversation_id, sender, content, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListDmConversations :many
SELECT id, wallet_a, wallet_b, last_message_at FROM chat.dm_conversations
WHERE wallet_a = $1 OR wallet_b = $1
ORDER BY last_message_at DESC NULLS LAST;

-- name: GetDmConversation :one
SELECT id, wallet_a, wallet_b, last_message_at FROM chat.dm_conversations
WHERE id = $1
LIMIT 1;

-- name: ListDmMessages :many
SELECT id, conversation_id, sender, content, created_at FROM chat.dm_messages
WHERE conversation_id = sqlc.arg('conversation_id')
  AND (sqlc.narg('before')::text IS NULL OR id < sqlc.narg('before'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim');

-- ── Moderation: bans & reports ───────────────────────────────────────────────

-- name: InsertBan :exec
INSERT INTO chat.chat_bans (id, wallet, pool_address, reason, banned_by, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: DeleteBan :exec
DELETE FROM chat.chat_bans WHERE id = $1;

-- name: ListBans :many
SELECT id, wallet, pool_address, reason, banned_by, expires_at, created_at
FROM chat.chat_bans
ORDER BY created_at DESC;

-- name: ActiveBans :many
-- Mirrors the TS checkBan: a NULL pool narg matches GLOBAL bans only; a set pool
-- narg matches global OR that pool (because pool_address = NULL is never true).
SELECT id, reason FROM chat.chat_bans
WHERE wallet = sqlc.arg('wallet')
  AND (expires_at IS NULL OR expires_at > sqlc.arg('now'))
  AND (pool_address IS NULL OR pool_address = sqlc.narg('pool_address'));

-- name: InsertReport :exec
INSERT INTO chat.chat_reports (id, message_id, reported_by, reason, status, created_at)
VALUES ($1, $2, $3, $4, 'pending', $5);

-- name: ListReportsByStatus :many
SELECT id, message_id, reported_by, reason, status, created_at
FROM chat.chat_reports
WHERE status = $1
ORDER BY created_at DESC;

-- name: UpdateReportStatus :exec
UPDATE chat.chat_reports SET status = $2 WHERE id = $1;

-- ── Comments feed (threaded, sign-free) ──────────────────────────────────────

-- name: InsertComment :exec
INSERT INTO chat.comments (id, pool_address, author, content, parent_id, deleted, created_at)
VALUES ($1, $2, $3, $4, $5, false, $6);

-- name: ListComments :many
SELECT
	c.id, c.pool_address, c.author, c.content, c.parent_id, c.edited_at, c.created_at,
	(SELECT count(*) FROM chat.comment_likes l WHERE l.comment_id = c.id) AS like_count
FROM chat.comments c
WHERE c.pool_address = sqlc.arg('pool_address') AND c.deleted = false
  AND (sqlc.narg('before')::text IS NULL OR c.id < sqlc.narg('before'))
ORDER BY c.created_at DESC
LIMIT sqlc.arg('lim');

-- name: GetComment :one
SELECT id, pool_address, author, deleted FROM chat.comments
WHERE id = $1
LIMIT 1;

-- name: UpdateCommentContent :exec
UPDATE chat.comments SET content = $2, edited_at = $3 WHERE id = $1;

-- name: SoftDeleteComment :exec
UPDATE chat.comments SET deleted = true WHERE id = $1;

-- name: LikeComment :exec
INSERT INTO chat.comment_likes (comment_id, wallet, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (comment_id, wallet) DO NOTHING;

-- name: UnlikeComment :exec
DELETE FROM chat.comment_likes WHERE comment_id = $1 AND wallet = $2;

-- ── Followers graph (sign-free) ──────────────────────────────────────────────

-- name: Follow :exec
INSERT INTO chat.follows (follower, followee, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (follower, followee) DO NOTHING;

-- name: Unfollow :exec
DELETE FROM chat.follows WHERE follower = $1 AND followee = $2;

-- name: ListFollowers :many
SELECT follower, created_at FROM chat.follows
WHERE followee = $1
ORDER BY created_at DESC;

-- name: ListFollowing :many
SELECT followee, created_at FROM chat.follows
WHERE follower = $1
ORDER BY created_at DESC;

-- name: CountFollowers :one
SELECT count(*) FROM chat.follows WHERE followee = $1;

-- name: CountFollowing :one
SELECT count(*) FROM chat.follows WHERE follower = $1;

-- name: IsFollowing :one
SELECT EXISTS (
	SELECT 1 FROM chat.follows WHERE follower = $1 AND followee = $2
) AS following;

-- ── Cross-schema: indexer.pools (creator lookup for delete authorization) ─────

-- name: GetPoolCreator :one
SELECT creator FROM indexer.pools WHERE pool_address = $1 LIMIT 1;
