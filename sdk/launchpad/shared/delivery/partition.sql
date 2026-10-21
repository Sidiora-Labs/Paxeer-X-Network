CREATE TABLE {{schema}}.delivery_partition_producers (
 projection_version text NOT NULL, partition_kind text NOT NULL, partition_key text NOT NULL,
 last_sequence bigint NOT NULL DEFAULT 0 CHECK(last_sequence>=0), last_effect_identity text NOT NULL DEFAULT '',
 last_source_order bytea,
 PRIMARY KEY(projection_version,partition_kind,partition_key)
);
CREATE TABLE {{schema}}.delivery_partition_positions (
 projection_version text NOT NULL, partition_kind text NOT NULL, partition_key text NOT NULL,
 sequence bigint NOT NULL CHECK(sequence>0), effect_identity text NOT NULL, position bytea NOT NULL,
 PRIMARY KEY(projection_version,partition_kind,partition_key,sequence),
 UNIQUE(projection_version,partition_kind,partition_key,effect_identity)
);
CREATE TABLE {{schema}}.delivery_partition_leases (
 consumer_id text NOT NULL, projection_generation text NOT NULL, partition_kind text NOT NULL, partition_key text NOT NULL,
 owner text NOT NULL, epoch bigint NOT NULL CHECK(epoch>0), expires_at timestamptz NOT NULL,
 PRIMARY KEY(consumer_id,projection_generation,partition_kind,partition_key)
);
CREATE TABLE {{schema}}.delivery_partition_offsets (
 consumer_id text NOT NULL, projection_generation text NOT NULL, partition_kind text NOT NULL, partition_key text NOT NULL,
 checkpoint_digest text NOT NULL CHECK(checkpoint_digest ~ '^[0-9a-f]{64}$'),
 initial_sequence bigint NOT NULL CHECK(initial_sequence>0), initial_predecessor text NOT NULL,
 next_sequence bigint NOT NULL CHECK(next_sequence>0), previous_effect_identity text NOT NULL,
 PRIMARY KEY(consumer_id,projection_generation,partition_kind,partition_key),
 FOREIGN KEY(consumer_id,projection_generation,partition_kind,partition_key) REFERENCES {{schema}}.delivery_partition_leases
);
CREATE TABLE {{schema}}.delivery_partition_commits (
 consumer_id text NOT NULL, projection_generation text NOT NULL, partition_kind text NOT NULL, partition_key text NOT NULL,
 sequence bigint NOT NULL CHECK(sequence>0), effect_identity text NOT NULL, position bytea NOT NULL,
 PRIMARY KEY(consumer_id,projection_generation,partition_kind,partition_key,sequence),
 UNIQUE(consumer_id,projection_generation,partition_kind,partition_key,effect_identity),
 FOREIGN KEY(consumer_id,projection_generation,partition_kind,partition_key) REFERENCES {{schema}}.delivery_partition_offsets
);
CREATE TABLE {{schema}}.delivery_ordered_quarantine (
 consumer_id text NOT NULL, projection_generation text NOT NULL, envelope_hash text NOT NULL,
 failure_class text NOT NULL CHECK(failure_class IN ('inner_schema','authority','identity_conflict','partition_binding')),
 subject text NOT NULL, publication_id text NOT NULL, envelope bytea NOT NULL CHECK(octet_length(envelope)<=1048576),
 affected_pools text[] NOT NULL, evidence_digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,projection_generation,envelope_hash,failure_class)
);
REVOKE ALL ON {{schema}}.delivery_ordered_quarantine FROM PUBLIC;
