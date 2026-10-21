-- +goose Up
-- Ports @media_microservices/chat drizzle schema (chat schema: pool_messages +
-- dm_conversations + dm_messages + chat_bans + chat_reports) 1:1 so the Go
-- service runs against the SAME Postgres during strangler cutover (L3/L13,
-- invariant i2), PLUS the net-new social surfaces the OG TS never had:
-- comments (threaded per-pool feed) + comment_likes + follows (followers graph).
--
-- NB: against the live DB (already created by the TS/drizzle service for the
-- chat.* tables) goose must be BASELINED so the legacy tables are
-- recorded-but-not-rerun; on a fresh DB (tests) it runs in full. The new
-- comments/comment_likes/follows tables use CREATE TABLE IF NOT EXISTS so a
-- baselined live DB can still gain them.
CREATE SCHEMA IF NOT EXISTS chat;

-- ── Ported 1:1 from the TS chat service ──────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.pool_messages (
	id           text PRIMARY KEY NOT NULL,
	pool_address varchar(42) NOT NULL,
	sender       varchar(42) NOT NULL,
	content      text NOT NULL,
	reply_to_id  text,
	deleted      boolean DEFAULT false NOT NULL,
	edited_at    bigint,
	created_at   bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS chat.dm_conversations (
	id              text PRIMARY KEY NOT NULL,
	wallet_a        varchar(42) NOT NULL,
	wallet_b        varchar(42) NOT NULL,
	last_message_at bigint
);

CREATE TABLE IF NOT EXISTS chat.dm_messages (
	id              text PRIMARY KEY NOT NULL,
	conversation_id text NOT NULL,
	sender          varchar(42) NOT NULL,
	content         text NOT NULL,
	created_at      bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS chat.chat_bans (
	id           text PRIMARY KEY NOT NULL,
	wallet       varchar(42) NOT NULL,
	pool_address varchar(42),
	reason       text,
	banned_by    varchar(42) NOT NULL,
	expires_at   bigint,
	created_at   bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS chat.chat_reports (
	id          text PRIMARY KEY NOT NULL,
	message_id  text NOT NULL,
	reported_by varchar(42) NOT NULL,
	reason      text NOT NULL,
	status      varchar(20) DEFAULT 'pending' NOT NULL,
	created_at  bigint NOT NULL
);

-- ── NEW: threaded per-pool comments feed (sign-free) ─────────────────────────
CREATE TABLE IF NOT EXISTS chat.comments (
	id           text PRIMARY KEY NOT NULL,
	pool_address varchar(42) NOT NULL,
	author       varchar(42) NOT NULL,
	content      text NOT NULL,
	parent_id    text,
	deleted      boolean DEFAULT false NOT NULL,
	edited_at    bigint,
	created_at   bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS chat.comment_likes (
	comment_id text NOT NULL,
	wallet     varchar(42) NOT NULL,
	created_at bigint NOT NULL,
	CONSTRAINT chat_comment_likes_pk PRIMARY KEY (comment_id, wallet)
);

-- ── NEW: followers graph (sign-free) ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.follows (
	follower   varchar(42) NOT NULL,
	followee   varchar(42) NOT NULL,
	created_at bigint NOT NULL,
	CONSTRAINT chat_follows_pk PRIMARY KEY (follower, followee)
);

CREATE INDEX IF NOT EXISTS idx_cb_wallet ON chat.chat_bans USING btree (wallet);
CREATE INDEX IF NOT EXISTS idx_cb_pool ON chat.chat_bans USING btree (pool_address);
CREATE INDEX IF NOT EXISTS idx_cr_message ON chat.chat_reports USING btree (message_id);
CREATE INDEX IF NOT EXISTS idx_cr_status ON chat.chat_reports USING btree (status);
CREATE INDEX IF NOT EXISTS idx_dc_wallet_a ON chat.dm_conversations USING btree (wallet_a);
CREATE INDEX IF NOT EXISTS idx_dc_wallet_b ON chat.dm_conversations USING btree (wallet_b);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dc_pair ON chat.dm_conversations USING btree (wallet_a, wallet_b);
CREATE INDEX IF NOT EXISTS idx_dm_conv ON chat.dm_messages USING btree (conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_pm_pool ON chat.pool_messages USING btree (pool_address, created_at);
CREATE INDEX IF NOT EXISTS idx_pm_sender ON chat.pool_messages USING btree (sender);
CREATE INDEX IF NOT EXISTS idx_cm_pool ON chat.comments USING btree (pool_address, created_at);
CREATE INDEX IF NOT EXISTS idx_cm_author ON chat.comments USING btree (author);
CREATE INDEX IF NOT EXISTS idx_cm_parent ON chat.comments USING btree (parent_id);
CREATE INDEX IF NOT EXISTS idx_cl_comment ON chat.comment_likes USING btree (comment_id);
CREATE INDEX IF NOT EXISTS idx_fl_follower ON chat.follows USING btree (follower);
CREATE INDEX IF NOT EXISTS idx_fl_followee ON chat.follows USING btree (followee);

-- +goose Down
DROP TABLE IF EXISTS chat.follows;
DROP TABLE IF EXISTS chat.comment_likes;
DROP TABLE IF EXISTS chat.comments;
DROP TABLE IF EXISTS chat.chat_reports;
DROP TABLE IF EXISTS chat.chat_bans;
DROP TABLE IF EXISTS chat.dm_messages;
DROP TABLE IF EXISTS chat.dm_conversations;
DROP TABLE IF EXISTS chat.pool_messages;
DROP SCHEMA IF EXISTS chat;
