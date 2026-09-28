-- =============================================================================
-- Paxeer Embedded Wallet — initial schema (local Postgres)
--
-- Run automatically on API boot by `src/db/migrate.ts`.
-- Idempotent: every statement is guarded with `IF NOT EXISTS`.
--
-- Key design notes:
--   - `user_id` holds the Supabase `auth.users.id` UUID. No cross-DB FK
--     constraint (different database) — we trust the UUID because it comes
--     from a locally-verified JWT.
--   - All tables live in `public` because this database exists ONLY for
--     wallets. No public-internet roles connect to it, so there is no RLS
--     exposure risk. Access is strictly application-level.
--   - `encrypted_private_key` is ciphertext only. Plaintext is never written
--     or logged anywhere.
-- =============================================================================

-- gen_random_uuid() lives in pgcrypto on older Postgres; on 16+ it's built-in.
create extension if not exists pgcrypto;

-- -----------------------------------------------------------------------------
-- wallets
-- -----------------------------------------------------------------------------
create table if not exists wallets (
  id                      uuid primary key default gen_random_uuid(),
  user_id                 uuid not null unique,
  address                 text not null unique,
  encrypted_private_key   text not null,
  key_version             smallint not null default 1,
  chain_id                integer not null default 125,
  created_at              timestamptz not null default now(),
  last_used_at            timestamptz,
  is_disabled             boolean not null default false,
  disabled_reason         text
);

create index if not exists wallets_user_id_idx on wallets (user_id);
create index if not exists wallets_address_idx on wallets (address);

-- -----------------------------------------------------------------------------
-- wallet_signatures — audit log of every signing event. No key material.
-- Useful for rate limiting, anomaly detection, support, compliance.
-- -----------------------------------------------------------------------------
create table if not exists wallet_signatures (
  id              bigserial primary key,
  user_id         uuid not null,
  wallet_id       uuid not null references wallets(id) on delete cascade,
  address         text not null,
  kind            text not null check (kind in ('transaction', 'message', 'typed_data')),
  to_address      text,
  value_wei       numeric(78, 0) not null default 0,   -- up to uint256
  chain_id        integer,
  request_hash    text not null,
  tx_hash         text,
  ip              inet,
  user_agent      text,
  created_at      timestamptz not null default now()
);

create index if not exists wallet_signatures_user_id_idx on wallet_signatures (user_id, created_at desc);
create index if not exists wallet_signatures_wallet_id_idx on wallet_signatures (wallet_id, created_at desc);
create index if not exists wallet_signatures_tx_hash_idx on wallet_signatures (tx_hash) where tx_hash is not null;

-- -----------------------------------------------------------------------------
-- touch trigger — bump wallets.last_used_at whenever a signature is recorded.
-- -----------------------------------------------------------------------------
create or replace function touch_wallet_last_used() returns trigger as $$
begin
  update wallets set last_used_at = now() where id = new.wallet_id;
  return new;
end;
$$ language plpgsql;

drop trigger if exists wallet_signatures_touch_wallet on wallet_signatures;
create trigger wallet_signatures_touch_wallet
  after insert on wallet_signatures
  for each row execute function touch_wallet_last_used();
