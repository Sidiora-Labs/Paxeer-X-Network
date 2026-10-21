-- +goose Up
-- Development-only identity store for media/gateway. The statements are the identity
-- tables of production migrations 330000, 330001 and 330002
-- (spec/production-system/integration/migrations), made schema-agnostic: they run
-- with search_path pinned to the gateway_identity schema. Production keeps its own
-- registry-managed copies in the users schema; this file is never applied there.
-- The users.* SECURITY DEFINER functions and *_v2 views are omitted: the gateway's
-- identity code does not call them, and other services verify sessions through
-- the gateway's /internal/v2/identity introspection outside production.
CREATE TABLE identity_accounts(account_id uuid PRIMARY KEY, auth_epoch bigint NOT NULL DEFAULT 0 CHECK(auth_epoch>=0), created_at timestamptz NOT NULL);
CREATE TABLE identity_wallet_links(link_id uuid PRIMARY KEY,account_id uuid NOT NULL REFERENCES identity_accounts,chain_id integer NOT NULL CHECK(chain_id=125),wallet text NOT NULL CHECK(wallet ~ '^0x[0-9a-f]{40}$'),visibility text NOT NULL CHECK(visibility IN ('private','public_optin')),state text NOT NULL CHECK(state IN ('pending','verified','revoked')),linked_at timestamptz, CHECK((state='pending')=(linked_at IS NULL)));
CREATE UNIQUE INDEX identity_wallet_owner ON identity_wallet_links(chain_id,wallet) WHERE state='verified';
CREATE TABLE identity_challenges(challenge_id uuid PRIMARY KEY,wallet text NOT NULL,nonce_hash bytea NOT NULL UNIQUE,message text NOT NULL,message_hash bytea NOT NULL,domain text NOT NULL,uri text NOT NULL,purpose text NOT NULL CHECK(purpose IN ('login','link_wallet','step_up')),account_id uuid REFERENCES identity_accounts,session_id uuid,link_id uuid REFERENCES identity_wallet_links,issued_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,consumed_at timestamptz,consumption_id uuid,CHECK(expires_at=issued_at+interval '300 seconds'));
CREATE TABLE identity_sessions(session_id uuid PRIMARY KEY,account_id uuid NOT NULL REFERENCES identity_accounts,wallet text NOT NULL,auth_epoch bigint NOT NULL,access_hash bytea NOT NULL UNIQUE,csrf_hash bytea NOT NULL,issued_at timestamptz NOT NULL,access_expires_at timestamptz NOT NULL,refresh_expires_at timestamptz NOT NULL,reauthenticated_at timestamptz NOT NULL,last_seen_at timestamptz NOT NULL,device_label text NOT NULL CHECK(length(device_label)<=80),revoked_at timestamptz);
CREATE TABLE identity_refresh(refresh_hash bytea PRIMARY KEY,salt text NOT NULL,session_id uuid NOT NULL REFERENCES identity_sessions,parent_hash bytea,issued_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,consumed_at timestamptz);
CREATE TABLE identity_tickets(ticket_hash bytea PRIMARY KEY,session_id uuid NOT NULL REFERENCES identity_sessions,origin text NOT NULL,endpoint text NOT NULL,issued_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,consumed_at timestamptz);
CREATE TABLE identity_limits(key_hash bytea PRIMARY KEY,window_at timestamptz NOT NULL,requests integer NOT NULL);
CREATE TABLE identity_outbox(event_id uuid PRIMARY KEY,account_id uuid NOT NULL REFERENCES identity_accounts,session_id uuid,action text NOT NULL,created_at timestamptz NOT NULL);
CREATE INDEX identity_pending_wallet ON identity_challenges(wallet,expires_at) WHERE consumed_at IS NULL;
CREATE TABLE identity_reauth(proof_hash bytea PRIMARY KEY,account_id uuid NOT NULL REFERENCES identity_accounts,session_id uuid NOT NULL REFERENCES identity_sessions,action text NOT NULL CHECK(action='wallet_unlink'),issued_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,consumed_at timestamptz);
ALTER TABLE identity_tickets ADD COLUMN binding jsonb;
CREATE TABLE identity_challenge_requests(wallet text NOT NULL,request_id uuid NOT NULL,body_hash bytea NOT NULL,response jsonb NOT NULL,retain_until timestamptz NOT NULL,PRIMARY KEY(wallet,request_id));
ALTER TABLE identity_challenges DROP CONSTRAINT identity_challenges_purpose_check;
ALTER TABLE identity_challenges ADD CONSTRAINT identity_challenges_purpose_check CHECK(purpose IN ('login','link_wallet','step_up','step_up:account.delete'));
ALTER TABLE identity_reauth DROP CONSTRAINT identity_reauth_action_check;
ALTER TABLE identity_reauth ADD CONSTRAINT identity_reauth_action_check CHECK(action IN ('wallet_unlink','account.delete'));
CREATE TABLE identity_owner_requests(account_id uuid NOT NULL REFERENCES identity_accounts,request_id uuid NOT NULL,operation text NOT NULL,body_hash bytea NOT NULL,response jsonb NOT NULL,retain_until timestamptz NOT NULL,PRIMARY KEY(account_id,request_id,operation));
CREATE TABLE identity_revisions(account_id uuid PRIMARY KEY REFERENCES identity_accounts,revision bigint NOT NULL CHECK(revision>0));
CREATE TABLE identity_revision_inbox(event_id text PRIMARY KEY,account_id uuid NOT NULL,revision bigint NOT NULL CHECK(revision>0),payload_hash text NOT NULL);
CREATE TABLE identity_remote_revisions(account_id uuid PRIMARY KEY,revision bigint NOT NULL CHECK(revision>0));
-- +goose StatementBegin
CREATE FUNCTION identity_verified_at_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.linked_at IS NOT NULL AND OLD.linked_at IS DISTINCT FROM NEW.linked_at THEN RAISE EXCEPTION 'verified timestamp immutable' USING ERRCODE='23514'; END IF; RETURN NEW; END $$;
-- +goose StatementEnd
CREATE TRIGGER identity_verified_at_immutable BEFORE UPDATE OF linked_at ON identity_wallet_links FOR EACH ROW EXECUTE FUNCTION identity_verified_at_immutable();

-- +goose Down
DROP TABLE IF EXISTS identity_remote_revisions, identity_revision_inbox, identity_revisions, identity_owner_requests, identity_challenge_requests, identity_reauth, identity_outbox, identity_limits, identity_tickets, identity_refresh, identity_sessions, identity_challenges, identity_wallet_links, identity_accounts CASCADE;
DROP FUNCTION IF EXISTS identity_verified_at_immutable();
