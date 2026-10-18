-- +goose Up
-- Ports @sidiora/livestream drizzle/0000_new_strong_guy.sql 1:1 (livestream.streams).
-- Produces a schema byte-identical to the TS service so the Go service can run
-- against the SAME Postgres during strangler cutover (L3/L13, invariant i2).
CREATE SCHEMA livestream;

CREATE TABLE livestream.streams (
	id                 text PRIMARY KEY NOT NULL,
	pool_address       varchar(42) NOT NULL,
	creator_wallet     varchar(42) NOT NULL,
	title              text NOT NULL,
	livepeer_stream_id text NOT NULL,
	stream_key         text NOT NULL,
	playback_id        text NOT NULL,
	rtmp_url           text NOT NULL,
	playback_url       text NOT NULL,
	is_live            boolean DEFAULT false NOT NULL,
	viewer_count       bigint DEFAULT 0 NOT NULL,
	started_at         bigint,
	ended_at           bigint,
	created_at         bigint NOT NULL
);

CREATE INDEX idx_streams_pool ON livestream.streams USING btree (pool_address);
CREATE INDEX idx_streams_creator ON livestream.streams USING btree (creator_wallet);
CREATE INDEX idx_streams_live ON livestream.streams USING btree (is_live);

-- +goose Down
DROP TABLE IF EXISTS livestream.streams;
DROP SCHEMA IF EXISTS livestream;
