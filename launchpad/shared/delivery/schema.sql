-- Service-owned schema template. Serial migration coordinator substitutes {{schema}}.
CREATE TABLE {{schema}}.delivery_outbox (
 event_id text PRIMARY KEY,
 schema_version integer NOT NULL,
 subject text NOT NULL,
 payload_hash text NOT NULL,
 envelope bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','published','quarantined')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_owner text,
 lease_until timestamptz,
 fence bigint NOT NULL DEFAULT 0,
 lease_expiry_count bigint NOT NULL DEFAULT 0,
 claimed_at timestamptz,
 acknowledgement_latency_ms bigint,
 broker_stream text,
 broker_sequence bigint,
 published_at timestamptz,
 failure_class text,
 CHECK ((state='published') = (published_at IS NOT NULL)),
 CHECK (broker_sequence IS NULL OR broker_sequence>0)
);
CREATE INDEX delivery_pending ON {{schema}}.delivery_outbox(available_at,created_at) WHERE state='pending';
CREATE FUNCTION {{schema}}.delivery_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.event_id IS DISTINCT FROM OLD.event_id OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
 OR NEW.subject IS DISTINCT FROM OLD.subject OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash
 OR NEW.envelope IS DISTINCT FROM OLD.envelope OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
 RAISE EXCEPTION 'immutable delivery envelope'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER delivery_immutable BEFORE UPDATE ON {{schema}}.delivery_outbox FOR EACH ROW EXECUTE FUNCTION {{schema}}.delivery_immutable();
CREATE TABLE {{schema}}.delivery_inbox (
 consumer_id text NOT NULL,
 projection_generation text NOT NULL,
 event_id text NOT NULL,
 envelope_hash text NOT NULL,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,projection_generation,event_id)
);
CREATE TABLE {{schema}}.delivery_quarantine (
 consumer_id text NOT NULL,
 projection_generation text NOT NULL,
 envelope_hash text NOT NULL,
 subject text NOT NULL,
 envelope bytea NOT NULL,
 failure_class text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,projection_generation,envelope_hash)
);

REVOKE ALL ON {{schema}}.delivery_quarantine FROM PUBLIC;
