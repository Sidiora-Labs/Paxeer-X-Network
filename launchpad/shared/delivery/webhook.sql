CREATE TABLE {{schema}}.delivery_webhook_cursors (
 target_id text NOT NULL, generation text NOT NULL, partition text NOT NULL,
 enqueued_sequence bigint NOT NULL DEFAULT 0, enqueued_event text NOT NULL DEFAULT '',
 committed_sequence bigint NOT NULL DEFAULT 0, committed_event text NOT NULL DEFAULT '',
 PRIMARY KEY(target_id,generation,partition), CHECK(committed_sequence>=0 AND enqueued_sequence>=committed_sequence)
);
CREATE TABLE {{schema}}.delivery_webhooks (
 target_id text NOT NULL, generation text NOT NULL, partition text NOT NULL, sequence bigint NOT NULL CHECK(sequence>0),
 position_identity text NOT NULL, event_id text NOT NULL REFERENCES {{schema}}.delivery_outbox(event_id), predecessor text NOT NULL,
 payload_hash text NOT NULL, wire_hash text NOT NULL, idempotency_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','delivered','quarantined')),
 attempts integer NOT NULL DEFAULT 0, allowed_attempts integer NOT NULL CHECK(allowed_attempts>0), available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_expiry_count bigint NOT NULL DEFAULT 0, claimed_at timestamptz, acknowledgement_latency_ms bigint, owner text, fence bigint NOT NULL DEFAULT 0, lease_until timestamptz,
 response_status integer, failure_class text, delivered_at timestamptz,
 PRIMARY KEY(target_id,generation,partition,sequence), UNIQUE(target_id,generation,partition,event_id),
 FOREIGN KEY(target_id,generation,partition) REFERENCES {{schema}}.delivery_webhook_cursors
);
CREATE INDEX delivery_webhook_pending ON {{schema}}.delivery_webhooks(target_id,available_at) WHERE state='pending';
CREATE FUNCTION {{schema}}.delivery_webhook_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.target_id,NEW.generation,NEW.partition,NEW.sequence,NEW.event_id,NEW.position_identity,NEW.predecessor,NEW.payload_hash,NEW.wire_hash,NEW.idempotency_key,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.target_id,OLD.generation,OLD.partition,OLD.sequence,OLD.event_id,OLD.position_identity,OLD.predecessor,OLD.payload_hash,OLD.wire_hash,OLD.idempotency_key,OLD.created_at)
 THEN RAISE EXCEPTION 'immutable webhook identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER delivery_webhook_immutable BEFORE UPDATE ON {{schema}}.delivery_webhooks FOR EACH ROW EXECUTE FUNCTION {{schema}}.delivery_webhook_immutable();
REVOKE ALL ON {{schema}}.delivery_webhooks,{{schema}}.delivery_webhook_cursors FROM PUBLIC;

CREATE TABLE {{schema}}.delivery_webhook_retry_audit (target_id text NOT NULL,generation text NOT NULL,partition text NOT NULL,sequence bigint NOT NULL,attempts_before integer NOT NULL,allowed_attempts integer NOT NULL,authorized_at timestamptz NOT NULL DEFAULT clock_timestamp());
REVOKE ALL ON {{schema}}.delivery_webhook_retry_audit FROM PUBLIC;
