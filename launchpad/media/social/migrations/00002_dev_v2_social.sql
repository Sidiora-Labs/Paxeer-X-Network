-- +goose Up
-- Stores for the v2 social surface of the runtimes that apply the embedded
-- migrations: development (NODE_ENV != production) and the production platform
-- runtime (RUNTIME_PROFILE=platform). The bound-document production runtimes
-- (container, owned_local) use the coordinator migrations and never run goose.
--
-- 1. Durable chat: the table DDL of production migrations 360000 + 360001
--    (chat_conversations, chat_members incl. muted/notification_enabled,
--    chat_messages, chat_outbox, chat_control_outbox) in the "chat" schema,
--    which the development runtime pool puts first on its search_path, as the
--    production pool does. fanout.DurableStore runs over it unchanged.
-- 2. Interactions: the table DDL of production migrations 370000 + 370001 that
--    interactions.Store and its comment projection read and write
--    (interaction_comments, reactions, mutations, aggregate revisions,
--    comment moderation, outbox, erased wallets, visibility revisions).
-- 3. Net-new v2 resources no service owned before: callouts, bounties (+
--    submissions, acceptances), reports.
-- 4. dev_runtime_keys: the chat replay-cursor HMAC key, generated once by the
--    service and persisted so cursors survive restarts.
--
-- Every statement is IF NOT EXISTS so re-running against a database that
-- already has a table (for example one created by hand) is a no-op.
CREATE SCHEMA IF NOT EXISTS chat;

-- ── 1. durable chat ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.chat_conversations (
	id              text PRIMARY KEY,
	kind            text NOT NULL CHECK (kind IN ('dm', 'pool')),
	next_sequence   bigint NOT NULL DEFAULT 0 CHECK (next_sequence >= 0),
	history_version bigint NOT NULL DEFAULT 0,
	retained_from   bigint NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS chat.chat_members (
	conversation_id      text NOT NULL REFERENCES chat.chat_conversations (id),
	wallet               text NOT NULL,
	read_sequence        bigint NOT NULL DEFAULT 0,
	muted                boolean NOT NULL DEFAULT false,
	notification_enabled boolean NOT NULL DEFAULT false,
	PRIMARY KEY (conversation_id, wallet)
);
CREATE INDEX IF NOT EXISTS chat_members_wallet ON chat.chat_members (wallet, conversation_id);
CREATE TABLE IF NOT EXISTS chat.chat_messages (
	id              text PRIMARY KEY,
	conversation_id text NOT NULL REFERENCES chat.chat_conversations (id),
	sender          text NOT NULL,
	client_id       text NOT NULL,
	content         text NOT NULL,
	reply_to        text NOT NULL DEFAULT '',
	sequence        bigint NOT NULL CHECK (sequence > 0),
	revision        bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
	created_at      timestamptz NOT NULL,
	edited_at       timestamptz,
	deleted_at      timestamptz,
	UNIQUE (sender, client_id),
	UNIQUE (conversation_id, sequence)
);
CREATE TABLE IF NOT EXISTS chat.chat_outbox (
	event_id     text PRIMARY KEY,
	message_id   text NOT NULL REFERENCES chat.chat_messages (id),
	revision     bigint NOT NULL,
	payload      jsonb NOT NULL,
	channels     jsonb NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	delivered_at timestamptz,
	attempts     integer NOT NULL DEFAULT 0,
	UNIQUE (message_id, revision)
);
CREATE INDEX IF NOT EXISTS chat_outbox_pending ON chat.chat_outbox (created_at, event_id) WHERE delivered_at IS NULL;
CREATE TABLE IF NOT EXISTS chat.chat_control_outbox (
	event_id     text PRIMARY KEY,
	wallet       text NOT NULL,
	payload      jsonb NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	delivered_at timestamptz
);
CREATE INDEX IF NOT EXISTS chat_control_pending ON chat.chat_control_outbox (created_at, event_id) WHERE delivered_at IS NULL;

-- ── 2. interactions (comments, likes) ───────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.interaction_comments (
	id         text PRIMARY KEY,
	pool       text NOT NULL,
	author     text NOT NULL,
	parent     text REFERENCES chat.interaction_comments (id),
	body       text NOT NULL,
	depth      int CHECK (depth BETWEEN 1 AND 4),
	removed    boolean NOT NULL DEFAULT false,
	revision   bigint NOT NULL DEFAULT 1,
	created_at timestamptz NOT NULL DEFAULT now(),
	edited_at  timestamptz
);
CREATE INDEX IF NOT EXISTS interaction_comments_pool ON chat.interaction_comments (pool, id DESC);
CREATE TABLE IF NOT EXISTS chat.interaction_reactions (
	actor  text,
	target text REFERENCES chat.interaction_comments (id),
	kind   text CHECK (kind = 'like'),
	PRIMARY KEY (actor, target, kind)
);
CREATE INDEX IF NOT EXISTS interaction_reactions_target ON chat.interaction_reactions (target);
CREATE TABLE IF NOT EXISTS chat.interaction_mutations (
	actor   text,
	key     text,
	payload bytea,
	PRIMARY KEY (actor, key)
);
CREATE TABLE IF NOT EXISTS chat.interaction_aggregate_revisions (
	aggregate_id text PRIMARY KEY,
	revision     bigint NOT NULL CHECK (revision > 0)
);
CREATE TABLE IF NOT EXISTS chat.interaction_comment_moderation (
	target     text REFERENCES chat.interaction_comments (id),
	revision   bigint NOT NULL,
	actor      text NOT NULL,
	reason     text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (target, revision)
);
CREATE TABLE IF NOT EXISTS chat.interaction_outbox (
	id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	actor        text NOT NULL,
	action       text NOT NULL,
	target       text NOT NULL,
	aggregate_id text NOT NULL,
	revision     bigint NOT NULL CHECK (revision > 0),
	removed      boolean NOT NULL,
	visibility   text NOT NULL CHECK (visibility = 'public'),
	UNIQUE (aggregate_id, revision)
);
CREATE TABLE IF NOT EXISTS chat.interaction_erased_wallets (
	wallet           text PRIMARY KEY,
	deletion_case_id uuid NOT NULL,
	policy_version   text NOT NULL,
	erased_at        timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS chat.interaction_visibility_revision (
	scope    text PRIMARY KEY,
	revision bigint NOT NULL CHECK (revision > 0)
);

-- ── 3. callouts, bounties, reports ──────────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.v2_callouts (
	callout_id        uuid PRIMARY KEY,
	author            text NOT NULL CHECK (author ~ '^0x[0-9a-f]{40}$'),
	client_callout_id uuid NOT NULL,
	pool              text NOT NULL CHECK (pool ~ '^0x[0-9a-f]{40}$'),
	body              text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
	direction         text NOT NULL CHECK (direction IN ('bullish', 'bearish', 'neutral')),
	disclosure        text NOT NULL CHECK (disclosure IN ('organic', 'sponsored', 'creator')),
	revision          bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
	removed           boolean NOT NULL DEFAULT false,
	created_at        timestamptz NOT NULL DEFAULT date_trunc('milliseconds', clock_timestamp()),
	UNIQUE (author, client_callout_id)
);
CREATE INDEX IF NOT EXISTS v2_callouts_pool ON chat.v2_callouts (pool, created_at DESC, callout_id DESC);

CREATE TABLE IF NOT EXISTS chat.v2_bounties (
	bounty_id       uuid PRIMARY KEY,
	creator         text NOT NULL CHECK (creator ~ '^0x[0-9a-f]{40}$'),
	idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
	request_hash    text NOT NULL,
	pool            text CHECK (pool IS NULL OR pool ~ '^0x[0-9a-f]{40}$'),
	title           text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
	description     text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 10000),
	asset           text NOT NULL CHECK (asset ~ '^0x[0-9a-f]{40}$'),
	budget_raw      numeric(78, 0) NOT NULL CHECK (budget_raw > 0),
	state           text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'funding', 'open', 'reviewing', 'paid', 'cancelled', 'expired')),
	revision        bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
	deadline_at     timestamptz,
	created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
	UNIQUE (creator, idempotency_key)
);
CREATE INDEX IF NOT EXISTS v2_bounties_list ON chat.v2_bounties (created_at DESC, bounty_id DESC);
CREATE INDEX IF NOT EXISTS v2_bounties_state ON chat.v2_bounties (state, created_at DESC, bounty_id DESC);
CREATE INDEX IF NOT EXISTS v2_bounties_pool ON chat.v2_bounties (pool, created_at DESC, bounty_id DESC);

CREATE TABLE IF NOT EXISTS chat.v2_bounty_submissions (
	submission_id        uuid PRIMARY KEY,
	bounty_id            uuid NOT NULL REFERENCES chat.v2_bounties (bounty_id),
	author               text NOT NULL CHECK (author ~ '^0x[0-9a-f]{40}$'),
	client_submission_id uuid NOT NULL,
	body                 text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 5000),
	evidence_urls        jsonb NOT NULL CHECK (jsonb_typeof(evidence_urls) = 'array'),
	state                text NOT NULL DEFAULT 'submitted' CHECK (state IN ('submitted', 'accepted', 'rejected', 'withdrawn')),
	award_raw            numeric(78, 0) CHECK (award_raw IS NULL OR award_raw > 0),
	created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
	UNIQUE (author, client_submission_id)
);
CREATE INDEX IF NOT EXISTS v2_bounty_submissions_bounty ON chat.v2_bounty_submissions (bounty_id, created_at DESC, submission_id DESC);

CREATE TABLE IF NOT EXISTS chat.v2_bounty_acceptances (
	creator         text NOT NULL,
	idempotency_key text NOT NULL,
	request_hash    text NOT NULL,
	submission_id   uuid NOT NULL REFERENCES chat.v2_bounty_submissions (submission_id),
	created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (creator, idempotency_key)
);

CREATE TABLE IF NOT EXISTS chat.v2_reports (
	report_id       uuid PRIMARY KEY,
	reporter        text NOT NULL CHECK (reporter ~ '^0x[0-9a-f]{40}$'),
	idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
	request_hash    text NOT NULL,
	target_type     text NOT NULL CHECK (target_type IN ('message', 'comment', 'stream', 'profile', 'token')),
	target_id       text NOT NULL CHECK (char_length(target_id) BETWEEN 1 AND 160),
	reason          text NOT NULL CHECK (reason IN ('spam', 'abuse', 'fraud', 'copyright', 'other')),
	reason_text     text NOT NULL CHECK (char_length(reason_text) BETWEEN 1 AND 256),
	evidence        text NOT NULL CHECK (char_length(evidence) <= 4000),
	state           text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'reviewing', 'resolved', 'dismissed')),
	created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
	UNIQUE (reporter, idempotency_key)
);
CREATE INDEX IF NOT EXISTS v2_reports_state ON chat.v2_reports (state, created_at);

-- ── 4. runtime keys ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS chat.dev_runtime_keys (
	name       text PRIMARY KEY,
	value      bytea NOT NULL CHECK (length(value) >= 32),
	created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS chat.dev_runtime_keys;
DROP TABLE IF EXISTS chat.v2_reports;
DROP TABLE IF EXISTS chat.v2_bounty_acceptances;
DROP TABLE IF EXISTS chat.v2_bounty_submissions;
DROP TABLE IF EXISTS chat.v2_bounties;
DROP TABLE IF EXISTS chat.v2_callouts;
DROP TABLE IF EXISTS chat.interaction_visibility_revision;
DROP TABLE IF EXISTS chat.interaction_erased_wallets;
DROP TABLE IF EXISTS chat.interaction_outbox;
DROP TABLE IF EXISTS chat.interaction_comment_moderation;
DROP TABLE IF EXISTS chat.interaction_aggregate_revisions;
DROP TABLE IF EXISTS chat.interaction_mutations;
DROP TABLE IF EXISTS chat.interaction_reactions;
DROP TABLE IF EXISTS chat.interaction_comments;
DROP TABLE IF EXISTS chat.chat_control_outbox;
DROP TABLE IF EXISTS chat.chat_outbox;
DROP TABLE IF EXISTS chat.chat_messages;
DROP TABLE IF EXISTS chat.chat_members;
DROP TABLE IF EXISTS chat.chat_conversations;
