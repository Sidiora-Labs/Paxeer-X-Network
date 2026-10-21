-- +goose Up
-- Ports @sidiora/metadata drizzle/0000_tricky_clint_barton.sql 1:1 (metadata
-- schema: token_metadata + token_images). Produces a schema byte-identical to
-- the TS service so the Go service can run against the SAME Postgres during
-- strangler cutover (L3/L13, invariant i2).
--
-- NB: against the live DB (already created by the TS/drizzle service) goose must
-- be BASELINED to version 1 so this migration is recorded-but-not-rerun; on a
-- fresh DB (tests) it runs in full.
CREATE SCHEMA metadata;

CREATE TABLE metadata.token_images (
	id            text PRIMARY KEY NOT NULL,
	token_address varchar(42) NOT NULL,
	image_type    varchar(10) NOT NULL,
	storage_key   text NOT NULL,
	mime_type     varchar(30) NOT NULL,
	size_bytes    integer NOT NULL,
	uploaded_at   bigint NOT NULL
);

CREATE TABLE metadata.token_metadata (
	token_address varchar(42) PRIMARY KEY NOT NULL,
	pool_address  varchar(42) NOT NULL,
	name          text,
	symbol        text,
	description   text,
	website       text,
	twitter       text,
	telegram      text,
	discord       text,
	custom_tags   text DEFAULT '[]',
	created_by    varchar(42) NOT NULL,
	created_at    bigint NOT NULL,
	updated_at    bigint NOT NULL
);

CREATE INDEX idx_ti_token ON metadata.token_images USING btree (token_address);
CREATE UNIQUE INDEX idx_ti_unique ON metadata.token_images USING btree (token_address, image_type);
CREATE INDEX idx_tm_pool ON metadata.token_metadata USING btree (pool_address);
CREATE INDEX idx_tm_creator ON metadata.token_metadata USING btree (created_by);

-- +goose Down
DROP TABLE IF EXISTS metadata.token_metadata;
DROP TABLE IF EXISTS metadata.token_images;
DROP SCHEMA IF EXISTS metadata;
