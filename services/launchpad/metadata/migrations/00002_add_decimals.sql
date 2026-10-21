-- +goose Up
-- ADDITIVE (net-new, not in the TS drizzle schema): token decimals. The TS
-- metadata service hard-coded decimals=6 in every response; the product now
-- stores the real token Decimal as part of token DNA. Backwards-compatible —
-- existing rows default to 6, matching prior behaviour. Safe to apply against
-- the live DB after baselining goose to version 1.
ALTER TABLE metadata.token_metadata
	ADD COLUMN IF NOT EXISTS decimals integer NOT NULL DEFAULT 6;

-- +goose Down
ALTER TABLE metadata.token_metadata DROP COLUMN IF EXISTS decimals;
