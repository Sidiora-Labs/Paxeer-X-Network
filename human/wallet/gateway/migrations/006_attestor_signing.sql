alter table wallets add column if not exists migrated_at timestamptz;
alter table wallets add column if not exists attestor_key_id text;

do $$
begin
  if not exists (
    select 1 from pg_constraint where conname = 'wallets_migrated_key_id_chk'
  ) then
    alter table wallets
      add constraint wallets_migrated_key_id_chk
      check (migrated_at is null or attestor_key_id is not null);
  end if;
end
$$;

create unique index if not exists wallets_attestor_key_id_uidx
  on wallets (attestor_key_id) where attestor_key_id is not null;

create table if not exists nonce_allocations (
  address          text primary key check (address = lower(address)),
  chain_id         integer not null,
  next_nonce       bigint not null check (next_nonce >= 0),
  needs_reconcile  boolean not null default true,
  reconciled_at    timestamptz,
  updated_at       timestamptz not null default now()
);

create table if not exists rate_limit_windows (
  scope         text not null check (scope in ('client', 'account')),
  subject       text not null,
  window_start  timestamptz not null,
  hits          integer not null check (hits > 0),
  primary key (scope, subject, window_start)
);

create index if not exists rate_limit_windows_start_idx on rate_limit_windows (window_start);

create table if not exists signing_audit (
  id               bigserial primary key,
  created_at       timestamptz not null default now(),
  request_id       uuid not null,
  client_subject   text not null,
  account          text,
  wallet_id        uuid references wallets(id) on delete set null,
  route            text not null,
  kind             text not null check (kind in ('transaction', 'message', 'typed_data')),
  path             text not null check (path in ('attestor', 'envelope', 'none')),
  decision         text not null check (decision in ('signed', 'refused', 'broadcast', 'broadcast_failed')),
  reason_code      text,
  request_hash     text not null,
  session_id       uuid,
  attestor_audit   jsonb not null default '[]'::jsonb,
  tx_hash          text,
  nonce            bigint
);

create index if not exists signing_audit_client_idx on signing_audit (client_subject, created_at desc);
create index if not exists signing_audit_account_idx on signing_audit (account, created_at desc);
create index if not exists signing_audit_request_idx on signing_audit (request_id);
