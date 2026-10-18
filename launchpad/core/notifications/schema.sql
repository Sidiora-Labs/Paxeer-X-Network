CREATE SCHEMA IF NOT EXISTS discovery_notifications;
CREATE TABLE IF NOT EXISTS discovery_notifications.inbox (
 id text PRIMARY KEY, account_id text NOT NULL, trigger_id text NOT NULL, template_version text NOT NULL,
 document jsonb NOT NULL, created_at timestamptz NOT NULL, read_at timestamptz,
 UNIQUE(account_id,trigger_id,template_version)
);
CREATE INDEX IF NOT EXISTS notification_recipient_time ON discovery_notifications.inbox(account_id,created_at DESC,id);
CREATE TABLE IF NOT EXISTS discovery_notifications.outbox (
 id text PRIMARY KEY REFERENCES discovery_notifications.inbox(id) ON DELETE CASCADE,
 deliveries jsonb NOT NULL, next_at timestamptz NOT NULL, lease_owner text, lease_until timestamptz,
 fence bigint NOT NULL DEFAULT 0, terminal boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS notification_due ON discovery_notifications.outbox(next_at) WHERE NOT terminal;

CREATE TABLE IF NOT EXISTS discovery_notifications.deleted_accounts (
 account_key text PRIMARY KEY, deleted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
