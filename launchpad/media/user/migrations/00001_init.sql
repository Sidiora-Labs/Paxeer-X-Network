-- +goose Up
-- Ports @media_microservices/users drizzle schema (users schema: user_profiles +
-- watchlists + user_images) into a single goose migration. Produces a schema
-- compatible with the prior TS service so the Go service can run against the
-- SAME Postgres during strangler cutover (L3/L13, invariant i2).
--
-- NB: against the live DB (already created by the TS/drizzle service) goose must
-- be BASELINED to version 1 so this migration is recorded-but-not-rerun; on a
-- fresh DB (tests) it runs in full.
CREATE SCHEMA users;

CREATE TABLE users.user_profiles (
	wallet_address varchar(42) PRIMARY KEY NOT NULL,
	display_name   text,
	bio            text,
	twitter        text,
	telegram       text,
	discord        text,
	website        text,
	created_at     bigint NOT NULL,
	updated_at     bigint NOT NULL
);

CREATE TABLE users.watchlists (
	wallet_address varchar(42) NOT NULL,
	pool_address   varchar(42) NOT NULL,
	added_at       bigint NOT NULL,
	CONSTRAINT users_watchlists_pk PRIMARY KEY (wallet_address, pool_address)
);

CREATE TABLE users.user_images (
	id             text PRIMARY KEY NOT NULL,
	wallet_address varchar(42) NOT NULL,
	image_type     varchar(10) NOT NULL,
	storage_key    text NOT NULL,
	mime_type      varchar(30) NOT NULL,
	size_bytes     integer NOT NULL,
	uploaded_at    bigint NOT NULL
);

CREATE INDEX idx_wl_wallet ON users.watchlists USING btree (wallet_address);
CREATE INDEX idx_ui_wallet ON users.user_images USING btree (wallet_address);
CREATE UNIQUE INDEX idx_ui_unique ON users.user_images USING btree (wallet_address, image_type);

-- +goose Down
DROP TABLE IF EXISTS users.user_images;
DROP TABLE IF EXISTS users.watchlists;
DROP TABLE IF EXISTS users.user_profiles;
DROP SCHEMA IF EXISTS users;
