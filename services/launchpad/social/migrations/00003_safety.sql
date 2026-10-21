-- +goose Up
-- Moderation (safety) stores for the runtimes that apply these embedded
-- migrations (development, and the production platform runtime): the table
-- DDL of production migrations 380000 + 380001 (safety cases, append-only
-- actions, bans, appeals, outbox, visibility, moderator session grants and
-- scopes, and the voice room tables safety.Store and voice.Control read), in
-- the "chat" schema. Every object is created only when absent, so a database
-- that already carries the coordinator migrations is unchanged.
-- +goose StatementBegin
DO $$
BEGIN
	IF to_regclass('chat.safety_cases') IS NOT NULL THEN
		RETURN;
	END IF;
	CREATE TABLE chat.safety_cases (
	 id uuid PRIMARY KEY, target_type text NOT NULL, target_id text NOT NULL,
	 pool_id text NOT NULL DEFAULT '', affected_wallet text NOT NULL DEFAULT '',
	 reporter text NOT NULL, category text NOT NULL, description text NOT NULL,
	 state text NOT NULL DEFAULT 'open', revision bigint NOT NULL DEFAULT 1,
	 created_at timestamptz NOT NULL DEFAULT now(),
	 UNIQUE(reporter,target_type,target_id), CHECK(revision>0),
	 CHECK(length(target_id)<=160 AND length(description)<=4000)
	);
	CREATE TABLE chat.safety_actions (
	 id uuid PRIMARY KEY, case_id uuid REFERENCES chat.safety_cases(id), actor text NOT NULL,
	 session_id text NOT NULL, actor_role text NOT NULL, actor_scope jsonb NOT NULL, kind text NOT NULL, reason text NOT NULL,
	 policy_version text NOT NULL, revision bigint NOT NULL, evidence_hash text NOT NULL,
	 created_at timestamptz NOT NULL DEFAULT now(), CHECK(length(reason)<=1000)
	);
	CREATE TABLE chat.safety_bans (
	 id uuid PRIMARY KEY, wallet text NOT NULL, scope text NOT NULL,
	 resource text NOT NULL, actor text NOT NULL, expires_at timestamptz,
	 revoked_at timestamptz, revision bigint NOT NULL DEFAULT 1,
	 CHECK(scope IN ('global','pool','dm','voice'))
	);
	CREATE TABLE chat.safety_appeals (
	 id uuid PRIMARY KEY, action_id uuid NOT NULL UNIQUE REFERENCES chat.safety_actions(id),
	 appellant text NOT NULL, reason text NOT NULL, state text NOT NULL DEFAULT 'submitted',
	 revision bigint NOT NULL DEFAULT 1, reviewer text, decided_at timestamptz
	);
	CREATE TABLE chat.safety_outbox (
	 id uuid PRIMARY KEY, kind text NOT NULL, entity_id text NOT NULL,
	 revision bigint NOT NULL, payload jsonb NOT NULL,
	 created_at timestamptz NOT NULL DEFAULT now(), published_at timestamptz
	);
	CREATE TABLE chat.voice_rooms (
	 id uuid PRIMARY KEY, pool_id text NOT NULL, host_wallet text NOT NULL,
	 state text NOT NULL DEFAULT 'draft', revision bigint NOT NULL DEFAULT 1,
	 provider_session text NOT NULL DEFAULT '', start_key text NOT NULL UNIQUE,
	 recording text NOT NULL DEFAULT 'off', updated_at timestamptz NOT NULL DEFAULT now(),
	 CHECK(state IN ('draft','live','ending','ended')),
	 CHECK(recording='off')
	);
	CREATE TABLE chat.voice_participants (
	 room_id uuid NOT NULL REFERENCES chat.voice_rooms(id), wallet text NOT NULL,
	 role text NOT NULL DEFAULT 'listener', muted boolean NOT NULL DEFAULT false,
	 revoked boolean NOT NULL DEFAULT false, grant_state text NOT NULL DEFAULT 'active', revision bigint NOT NULL DEFAULT 1,
	 PRIMARY KEY(room_id,wallet),
	 CHECK(role IN ('listener','requested_speaker','speaker','moderator','host'))
	);
	CREATE FUNCTION chat.safety_append_only() RETURNS trigger LANGUAGE plpgsql AS $f$
	BEGIN RAISE EXCEPTION 'safety actions are append only'; END $f$;
	CREATE TRIGGER safety_actions_immutable BEFORE UPDATE OR DELETE ON chat.safety_actions
	FOR EACH ROW EXECUTE FUNCTION chat.safety_append_only();
	CREATE TABLE chat.safety_visibility (
	 target_type text NOT NULL, target_id text NOT NULL, revision bigint NOT NULL,
	 status text NOT NULL CHECK(status IN ('removed','restored')),
	 case_id uuid NOT NULL REFERENCES chat.safety_cases(id), PRIMARY KEY(target_type,target_id)
	);
	CREATE TABLE chat.safety_action_bans (
	 action_id uuid NOT NULL REFERENCES chat.safety_actions(id), ban_id uuid NOT NULL REFERENCES chat.safety_bans(id),
	 PRIMARY KEY(action_id,ban_id)
	);
	CREATE TABLE chat.voice_recordings (
	 id uuid PRIMARY KEY,room_id uuid NOT NULL REFERENCES chat.voice_rooms(id),
	 state text NOT NULL CHECK(state IN ('consent_pending','starting','recording','stopping','stopped')),
	 purpose text NOT NULL CHECK(length(purpose) BETWEEN 1 AND 1000),
	 policy_version text NOT NULL CHECK(length(policy_version) BETWEEN 1 AND 160),
	 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),provider_id text NOT NULL DEFAULT '',
	 deleted_at timestamptz,delete_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
	);
	CREATE UNIQUE INDEX voice_recordings_active ON chat.voice_recordings(room_id) WHERE state IN ('consent_pending','starting','recording','stopping');
	CREATE TABLE chat.voice_consents (
	 recording_id uuid NOT NULL REFERENCES chat.voice_recordings(id),wallet text NOT NULL,agreed boolean NOT NULL,
	 purpose text NOT NULL,policy_version text NOT NULL,session_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
	 PRIMARY KEY(recording_id,wallet)
	);
	CREATE INDEX voice_reconciliation_pending ON chat.voice_rooms(updated_at,id) WHERE state IN ('draft','live','ending');
	CREATE INDEX voice_revocations_pending ON chat.voice_participants(room_id,wallet) WHERE grant_state='revoking';
	CREATE INDEX voice_recording_deletion ON chat.voice_recordings(delete_at,id) WHERE state='stopped';
	CREATE INDEX safety_bans_active_lookup ON chat.safety_bans(wallet,scope,resource) WHERE revoked_at IS NULL;
	CREATE TABLE chat.safety_moderator_sessions (
	 session_id uuid PRIMARY KEY,account_id uuid NOT NULL,wallet text NOT NULL,
	 role text NOT NULL CHECK(role IN ('reviewer','room_moderator','global_administrator')),
	 expires_at timestamptz NOT NULL,step_up_at timestamptz
	);
	CREATE TABLE chat.safety_moderator_scopes (
	 session_id uuid NOT NULL REFERENCES chat.safety_moderator_sessions(session_id),pool_id text NOT NULL DEFAULT '',case_id uuid REFERENCES chat.safety_cases(id),
	 CHECK(pool_id<>'' OR case_id IS NOT NULL)
	);
	CREATE INDEX safety_moderator_scope_session ON chat.safety_moderator_scopes(session_id);
	CREATE TABLE chat.voice_ejections (
	 room_id uuid NOT NULL REFERENCES chat.voice_rooms(id),wallet text NOT NULL,actor text NOT NULL,
	 session_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(room_id,wallet)
	);
	CREATE TABLE chat.voice_recording_events (
	 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,recording_id uuid NOT NULL REFERENCES chat.voice_recordings(id),
	 room_id uuid NOT NULL REFERENCES chat.voice_rooms(id),actor text NOT NULL,session_id text NOT NULL,
	 kind text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
	);
	CREATE TRIGGER voice_recording_events_immutable BEFORE UPDATE OR DELETE ON chat.voice_recording_events
	 FOR EACH ROW EXECUTE FUNCTION chat.safety_append_only();
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS chat.voice_recording_events;
DROP TABLE IF EXISTS chat.voice_ejections;
DROP TABLE IF EXISTS chat.safety_moderator_scopes;
DROP TABLE IF EXISTS chat.safety_moderator_sessions;
DROP TABLE IF EXISTS chat.voice_consents;
DROP TABLE IF EXISTS chat.voice_recordings;
DROP TABLE IF EXISTS chat.safety_action_bans;
DROP TABLE IF EXISTS chat.safety_visibility;
DROP TABLE IF EXISTS chat.voice_participants;
DROP TABLE IF EXISTS chat.voice_rooms;
DROP TABLE IF EXISTS chat.safety_outbox;
DROP TABLE IF EXISTS chat.safety_appeals;
DROP TABLE IF EXISTS chat.safety_bans;
DROP TABLE IF EXISTS chat.safety_actions;
DROP TABLE IF EXISTS chat.safety_cases;
DROP FUNCTION IF EXISTS chat.safety_append_only();
