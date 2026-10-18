-- +goose Up
-- Development parity with coordinator migration 380002: the column-scoped
-- versioned view of the moderation bans media/livestream reads
-- (streams.RestrictionReader). In production the coordinator owns it.
CREATE OR REPLACE VIEW chat.safety_bans_v1 AS SELECT wallet,scope,resource,expires_at,revoked_at FROM chat.safety_bans;

-- +goose Down
DROP VIEW IF EXISTS chat.safety_bans_v1;
