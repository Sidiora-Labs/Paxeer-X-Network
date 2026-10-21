CREATE TABLE {{schema}}.delivery_archive_manifests (
 manifest_digest text PRIMARY KEY, source_stream text NOT NULL, from_height bigint NOT NULL,
 to_height bigint NOT NULL, raw_count bigint NOT NULL, final_block_hash text NOT NULL,
 manifest bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), CHECK(to_height>=from_height)
);
CREATE TABLE {{schema}}.delivery_rebuilds (
 consumer_id text NOT NULL, generation text NOT NULL, manifest_digest text NOT NULL REFERENCES {{schema}}.delivery_archive_manifests,
 next_height bigint NOT NULL, raw_count bigint NOT NULL DEFAULT 0, revision bigint NOT NULL DEFAULT 0,
 status text NOT NULL CHECK(status IN ('building','verified','active','retired')),
 projection_digest text, final_watermark jsonb, owner text NOT NULL, epoch bigint NOT NULL CHECK(epoch>0),
 lease_until timestamptz NOT NULL, PRIMARY KEY(consumer_id,generation)
);
CREATE TABLE {{schema}}.delivery_read_aliases (
 consumer_id text PRIMARY KEY, generation text NOT NULL, prior_generation text,
 revision bigint NOT NULL CHECK(revision>0), FOREIGN KEY(consumer_id,generation) REFERENCES {{schema}}.delivery_rebuilds
);
CREATE TABLE {{schema}}.delivery_cutover_history (
 consumer_id text NOT NULL, revision bigint NOT NULL, generation text NOT NULL, prior_generation text,
 changed_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(consumer_id,revision)
);
CREATE TABLE {{schema}}.delivery_generation_paths (
 consumer_id text NOT NULL, generation text NOT NULL, authoritative_path text NOT NULL CHECK(authoritative_path IN ('legacy','durable')),
 PRIMARY KEY(consumer_id,generation)
);
REVOKE ALL ON {{schema}}.delivery_archive_manifests,{{schema}}.delivery_rebuilds,{{schema}}.delivery_read_aliases,{{schema}}.delivery_cutover_history,{{schema}}.delivery_generation_paths FROM PUBLIC;
CREATE TABLE {{schema}}.delivery_shadow_observations (
 range_digest text NOT NULL REFERENCES {{schema}}.delivery_archive_manifests(manifest_digest),
 path text NOT NULL CHECK(path IN ('legacy','durable')), event_id text NOT NULL,
 payload_hash text NOT NULL, source_order jsonb NOT NULL,
 PRIMARY KEY(range_digest,path,event_id)
);
CREATE TABLE {{schema}}.delivery_shadow_comparisons (
 manifest_digest text PRIMARY KEY REFERENCES {{schema}}.delivery_archive_manifests(manifest_digest),
 event_digest text NOT NULL, event_count bigint NOT NULL CHECK(event_count>0),
 compared_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON {{schema}}.delivery_shadow_observations,{{schema}}.delivery_shadow_comparisons FROM PUBLIC;
CREATE TABLE {{schema}}.delivery_consumer_watermarks (
 consumer_id text NOT NULL,generation text NOT NULL,partition_kind text NOT NULL,partition_key text NOT NULL,
 broker_stream text NOT NULL,broker_sequence bigint NOT NULL CHECK(broker_sequence>0),
 source_order jsonb NOT NULL,position_sequence bigint NOT NULL CHECK(position_sequence>0),
 event_id text NOT NULL,envelope_hash text NOT NULL,committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,generation,partition_kind,partition_key)
);
REVOKE ALL ON {{schema}}.delivery_consumer_watermarks FROM PUBLIC;
