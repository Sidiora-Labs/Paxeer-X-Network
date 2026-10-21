-- Proposed serial migration, not a registered migration. No production writes.
CREATE SCHEMA IF NOT EXISTS stats_search;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE TABLE stats_search.generation (singleton boolean PRIMARY KEY CHECK(singleton), generation text NOT NULL, watermark bigint NOT NULL CHECK(watermark>0));
CREATE TABLE stats_search.documents (
 generation text NOT NULL, chain_id text NOT NULL CHECK(chain_id='125'),
 pool_address text NOT NULL CHECK(pool_address ~ '^0x[0-9a-f]{40}$'),
 token_address text NOT NULL CHECK(token_address ~ '^0x[0-9a-f]{40}$'),
 name text NOT NULL, name_key text NOT NULL, ticker_key text NOT NULL,
 creation_event_id text NOT NULL, revision bigint NOT NULL CHECK(revision>0),
 metadata_revision bigint NOT NULL CHECK(metadata_revision>0), visibility_revision bigint NOT NULL CHECK(visibility_revision>0),
 pool_status text NOT NULL CHECK(pool_status IN ('active','paused')),
 canonical boolean NOT NULL, quarantined boolean NOT NULL,
 PRIMARY KEY(generation,pool_address));
-- Current visibility is generation-independent, so rollback cannot resurrect tombstones.
CREATE TABLE stats_search.visibility (pool_address text PRIMARY KEY, revision bigint NOT NULL CHECK(revision>0), public boolean NOT NULL, tombstone boolean NOT NULL);
CREATE INDEX search_token_exact ON stats_search.documents(generation,token_address,pool_address);
CREATE INDEX search_name_trgm ON stats_search.documents USING gin(name_key gin_trgm_ops);
CREATE INDEX search_ticker_trgm ON stats_search.documents USING gin(ticker_key gin_trgm_ops);
CREATE INDEX search_pool_prefix ON stats_search.documents(pool_address text_pattern_ops);
CREATE INDEX search_token_prefix ON stats_search.documents(token_address text_pattern_ops);

CREATE SEQUENCE stats_search.revision_v2;
CREATE TABLE stats_search.generations_v2(generation text PRIMARY KEY CHECK(length(generation) BETWEEN 1 AND 128),state text NOT NULL CHECK(state IN ('building','active','retained')),source_watermark bigint NOT NULL CHECK(source_watermark>0),source_observed_at timestamptz NOT NULL,row_count bigint,digest text,validated_at timestamptz,activated_at timestamptz);
CREATE UNIQUE INDEX search_one_active_v2 ON stats_search.generations_v2(state) WHERE state='active';
CREATE TABLE stats_search.active_v2(singleton boolean PRIMARY KEY CHECK(singleton),generation text NOT NULL REFERENCES stats_search.generations_v2(generation),revision bigint NOT NULL CHECK(revision>0),privacy_revision bigint NOT NULL CHECK(privacy_revision>0));
CREATE TABLE stats_search.authority_v2(pool_address text PRIMARY KEY CHECK(pool_address ~ '^0x[0-9a-f]{40}$'),visibility_revision bigint NOT NULL CHECK(visibility_revision>0),public boolean NOT NULL,tombstone boolean NOT NULL);
CREATE TABLE stats_search.profile_tombstones_v2(wallet text PRIMARY KEY CHECK(wallet ~ '^0x[0-9a-f]{40}$'),revision bigint NOT NULL CHECK(revision>0));
CREATE TABLE stats_search.preferences_v2(wallet text PRIMARY KEY CHECK(wallet ~ '^0x[0-9a-f]{40}$'),revision bigint NOT NULL CHECK(revision>0),payload jsonb NOT NULL);
CREATE TABLE stats_search.documents_v2(
 generation text NOT NULL REFERENCES stats_search.generations_v2(generation),pool_address text NOT NULL CHECK(pool_address ~ '^0x[0-9a-f]{40}$'),token_address text NOT NULL CHECK(token_address ~ '^0x[0-9a-f]{40}$'),
 name_key text NOT NULL,ticker_key text NOT NULL,creator text NOT NULL CHECK(creator ~ '^0x[0-9a-f]{40}$'),created_at timestamptz NOT NULL,pool_status text NOT NULL CHECK(pool_status IN ('active','paused')),visibility_revision bigint NOT NULL CHECK(visibility_revision>0),revision bigint NOT NULL CHECK(revision>0),metrics_at timestamptz,
 liquidity_raw numeric(78,0) CHECK(liquidity_raw>=0 AND liquidity_raw<=115792089237316195423570985008687907853269984665640564039457584007913129639935),volume_1h numeric(78,0) CHECK(volume_1h>=0),volume_24h numeric(78,0) CHECK(volume_24h>=0),volume_7d numeric(78,0) CHECK(volume_7d>=0),verified_quote boolean,risk_flags text[] NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(generation,pool_address));
CREATE INDEX search_token_v2 ON stats_search.documents_v2(generation,token_address,pool_address);
CREATE INDEX search_name_v2 ON stats_search.documents_v2 USING gin(name_key gin_trgm_ops);
CREATE INDEX search_ticker_v2 ON stats_search.documents_v2 USING gin(ticker_key gin_trgm_ops);
CREATE INDEX search_creator_v2 ON stats_search.documents_v2(generation,creator,pool_address);
CREATE INDEX search_created_v2 ON stats_search.documents_v2(generation,created_at,pool_address);
CREATE INDEX search_pool_prefix_v2 ON stats_search.documents_v2(pool_address text_pattern_ops);
CREATE INDEX search_token_prefix_v2 ON stats_search.documents_v2(token_address text_pattern_ops);
CREATE INDEX search_liquidity_v2 ON stats_search.documents_v2(generation,liquidity_raw,pool_address);

CREATE TABLE stats_search.source_documents_v2(pool_address text PRIMARY KEY CHECK(pool_address ~ '^0x[0-9a-f]{40}$'),payload jsonb NOT NULL);

CREATE TABLE stats_search.profile_documents_v2(wallet text PRIMARY KEY CHECK(wallet ~ '^0x[0-9a-f]{40}$'),display_name text NOT NULL,name_key text NOT NULL,revision bigint NOT NULL CHECK(revision>=0),updated_at timestamptz NOT NULL);
CREATE INDEX search_profile_name_v2 ON stats_search.profile_documents_v2 USING gin(name_key gin_trgm_ops);
CREATE INDEX search_profile_prefix_v2 ON stats_search.profile_documents_v2(wallet text_pattern_ops);
