-- =============================================================================
-- Paxeer Embedded Wallet — database schema
--
-- Run this once in the Supabase SQL editor (Dashboard -> SQL editor -> New).
-- Idempotent: safe to re-run.
-- =============================================================================

-- Wallets: one EVM EOA per Supabase user. Private key is stored encrypted
-- (AES-256-GCM envelope, see gateway/src/crypto.ts). Service-role only.
create table if not exists public.wallets (
  id                      uuid primary key default gen_random_uuid(),
  user_id                 uuid not null unique references auth.users(id) on delete cascade,
  address                 text not null unique,
  encrypted_private_key   text not null,
  key_version             smallint not null default 1,
  chain_id                integer not null default 125,
  created_at              timestamptz not null default now(),
  last_used_at            timestamptz,
  -- soft lifecycle flags
  is_disabled             boolean not null default false,
  disabled_reason         text
);

create index if not exists wallets_user_id_idx on public.wallets (user_id);
create index if not exists wallets_address_idx on public.wallets (address);

-- Audit log of every signature produced. No key material recorded.
-- Useful for: rate limiting, anomaly detection, support, compliance.
create table if not exists public.wallet_signatures (
  id              bigserial primary key,
  user_id         uuid not null references auth.users(id) on delete cascade,
  wallet_id       uuid not null references public.wallets(id) on delete cascade,
  address         text not null,
  kind            text not null check (kind in ('transaction', 'message', 'typed_data')),
  -- summary fields for fast queries / policy checks
  to_address      text,
  value_wei       numeric(78, 0) default 0,
  chain_id        integer,
  request_hash    text not null,
  tx_hash         text,
  ip              inet,
  user_agent      text,
  created_at      timestamptz not null default now()
);

create index if not exists wallet_signatures_user_id_created_idx
  on public.wallet_signatures (user_id, created_at desc);
create index if not exists wallet_signatures_wallet_id_created_idx
  on public.wallet_signatures (wallet_id, created_at desc);

-- =============================================================================
-- Row-level security
--
-- The wallets table contains material that, if ever exposed, would compromise
-- user funds. RLS is enabled with NO policies, which means: only the
-- service_role key (which bypasses RLS) can read/write. anon and authenticated
-- roles get nothing — even with a valid JWT.
--
-- All access flows through the API server, which authenticates the user via
-- Supabase Auth and then performs DB ops with the service_role client.
-- =============================================================================

alter table public.wallets enable row level security;
alter table public.wallet_signatures enable row level security;

-- (intentionally no policies — service_role bypasses RLS by design)

-- =============================================================================
-- Triggers
-- =============================================================================

create or replace function public.set_last_used_at()
returns trigger
language plpgsql
as $$
begin
  update public.wallets
    set last_used_at = now()
    where id = new.wallet_id;
  return new;
end;
$$;

drop trigger if exists tg_set_last_used_at on public.wallet_signatures;
create trigger tg_set_last_used_at
  after insert on public.wallet_signatures
  for each row execute function public.set_last_used_at();
