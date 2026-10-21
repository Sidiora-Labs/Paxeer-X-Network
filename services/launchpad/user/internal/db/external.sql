-- external.sql — READ-ONLY foreign tables modeled for sqlc type-checking ONLY.
-- These tables are owned by OTHER services (their migrations), never by
-- media/user. Listed here purely so sqlc can type the cross-schema reads in
-- query.sql. Only the columns this service reads are declared. (L3 cross-schema)

CREATE SCHEMA indexer;

CREATE TABLE indexer.pools (
	pool_address  varchar(42) PRIMARY KEY,
	token_address varchar(42) NOT NULL,
	creator       varchar(42) NOT NULL,
	created_at    bigint NOT NULL
);
