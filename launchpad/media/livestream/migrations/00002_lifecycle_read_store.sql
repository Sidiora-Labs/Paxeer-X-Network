-- +goose Up
-- Development-only (goose runs only when NODE_ENV != production): the lifecycle
-- read store tables, byte-for-byte the registered production migrations
-- 400000 and 400001 (spec/production-system/integration/migrations).
CREATE TABLE IF NOT EXISTS livestream.stream_operations(
 id text PRIMARY KEY,wallet text NOT NULL,pool text NOT NULL,title text NOT NULL,idem_key text NOT NULL,request_hash text NOT NULL,
 state text NOT NULL CHECK(state IN ('REQUESTED','PROVISIONING','READY','LIVE','ENDING','ENDED','FAILED','SUSPENDED')),
 intent text NOT NULL DEFAULT 'create',provider_id text UNIQUE,revision bigint NOT NULL DEFAULT 1,
 provider_time timestamptz,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),retry_at timestamptz,
 last_reason text NOT NULL DEFAULT '',UNIQUE(wallet,idem_key));
 CREATE TABLE IF NOT EXISTS livestream.stream_inbox(event_id text PRIMARY KEY,request_hash text NOT NULL,provider_id text NOT NULL,
 observed_at timestamptz NOT NULL,received_at timestamptz NOT NULL DEFAULT clock_timestamp(),outcome text NOT NULL);
 CREATE TABLE IF NOT EXISTS livestream.stream_audit(id bigserial PRIMARY KEY,operation_id text NOT NULL,actor text NOT NULL,reason text NOT NULL,
 revision bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
 CREATE TABLE IF NOT EXISTS livestream.stream_outbox(id bigserial PRIMARY KEY,operation_id text NOT NULL REFERENCES livestream.stream_operations(id),revision bigint NOT NULL,kind text NOT NULL CHECK(kind IN ('provision','start','end','suspend','provider_state')),payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(operation_id,revision,kind));
ALTER TABLE "livestream".stream_operations ADD COLUMN IF NOT EXISTS terminal_at timestamptz;
 CREATE TABLE IF NOT EXISTS "livestream".stream_credentials(operation_id text PRIMARY KEY REFERENCES "livestream".stream_operations(id),provider_id text NOT NULL,ciphertext text NOT NULL,key_ref text NOT NULL,revision bigint NOT NULL CHECK(revision>0),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
 CREATE TABLE IF NOT EXISTS "livestream".stream_worker_attempts(operation_id text PRIMARY KEY REFERENCES "livestream".stream_operations(id),attempt bigint NOT NULL DEFAULT 0,lease_until timestamptz NOT NULL DEFAULT '-infinity',retry_at timestamptz NOT NULL DEFAULT '-infinity',disagreement_at timestamptz,last_reason text NOT NULL DEFAULT '',owner text NOT NULL DEFAULT '',create_sent boolean NOT NULL DEFAULT false);
 CREATE TABLE IF NOT EXISTS "livestream".stream_usage(provider_event text PRIMARY KEY,operation_id text NOT NULL REFERENCES "livestream".stream_operations(id),viewer_minutes numeric NOT NULL CHECK(viewer_minutes>=0),transcode_minutes numeric NOT NULL CHECK(transcode_minutes>=0),storage_bytes numeric NOT NULL CHECK(storage_bytes>=0),delivery_bytes numeric NOT NULL CHECK(delivery_bytes>=0),cost_units numeric NOT NULL CHECK(cost_units>=0),received_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE IF NOT EXISTS "livestream".stream_key_rotations(old_stream_id text PRIMARY KEY REFERENCES "livestream".stream_operations(id),new_stream_id text UNIQUE REFERENCES "livestream".stream_operations(id),wallet text NOT NULL,status text NOT NULL CHECK(status IN('revoking','provisioning','ready')),revision bigint NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE IF NOT EXISTS "livestream".stream_budget_alerts(period_start date NOT NULL,threshold integer NOT NULL CHECK(threshold IN(50,80,100)),currency text NOT NULL,spent_units numeric NOT NULL,budget_units numeric NOT NULL,approval_reference text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(period_start,threshold,currency));
REVOKE ALL ON "livestream"."stream_credentials" FROM PUBLIC;
REVOKE ALL ON "livestream"."stream_worker_attempts" FROM PUBLIC;
REVOKE ALL ON "livestream"."stream_usage" FROM PUBLIC;
REVOKE ALL ON "livestream"."stream_key_rotations" FROM PUBLIC;
REVOKE ALL ON "livestream"."stream_budget_alerts" FROM PUBLIC;
CREATE TABLE IF NOT EXISTS "livestream".stream_budget_alerts(period_start date NOT NULL,threshold integer NOT NULL CHECK(threshold IN(50,80,100)),currency text NOT NULL,spent_units numeric NOT NULL,budget_units numeric NOT NULL,approval_reference text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),delivered_at timestamptz,retry_at timestamptz NOT NULL DEFAULT '-infinity',attempt integer NOT NULL DEFAULT 0,PRIMARY KEY(period_start,threshold,currency));
 ALTER TABLE "livestream".stream_budget_alerts ADD COLUMN IF NOT EXISTS delivered_at timestamptz;
 ALTER TABLE "livestream".stream_budget_alerts ADD COLUMN IF NOT EXISTS retry_at timestamptz NOT NULL DEFAULT '-infinity';
 ALTER TABLE "livestream".stream_budget_alerts ADD COLUMN IF NOT EXISTS attempt integer NOT NULL DEFAULT 0;
 CREATE TABLE IF NOT EXISTS "livestream".stream_vendor_usage(period_start date NOT NULL,vendor_user_id text NOT NULL,viewer_minutes numeric NOT NULL CHECK(viewer_minutes>=0),delivery_minutes numeric NOT NULL CHECK(delivery_minutes>=0),transcode_minutes numeric NOT NULL CHECK(transcode_minutes>=0),storage_minutes numeric NOT NULL CHECK(storage_minutes>=0),delivery_bytes numeric,storage_bytes numeric,cost_units numeric NOT NULL CHECK(cost_units>=0),query_from timestamptz NOT NULL,query_to timestamptz NOT NULL,fetched_at timestamptz NOT NULL DEFAULT clock_timestamp(),source_revision text NOT NULL,PRIMARY KEY(period_start,vendor_user_id));
REVOKE ALL ON livestream.stream_vendor_usage FROM PUBLIC;
CREATE TABLE IF NOT EXISTS "livestream".stream_playback_privacy(operation_id text PRIMARY KEY REFERENCES "livestream".stream_operations(id),private boolean NOT NULL,revision bigint NOT NULL CHECK(revision>0));
REVOKE ALL ON livestream.stream_playback_privacy FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS livestream.stream_playback_privacy, livestream.stream_vendor_usage, livestream.stream_budget_alerts, livestream.stream_key_rotations, livestream.stream_usage, livestream.stream_worker_attempts, livestream.stream_credentials, livestream.stream_outbox, livestream.stream_audit, livestream.stream_inbox, livestream.stream_operations;
