alter table wallets add column if not exists did text;
alter table wallets add column if not exists main_account_id text;
alter table wallets add column if not exists layerx_key_id text;
alter table wallets add column if not exists binding_state text not null default 'unbound';
alter table wallets alter column encrypted_private_key drop not null;

do $$
begin
  if not exists (
    select 1 from pg_constraint where conname = 'wallets_key_material_chk'
  ) then
    alter table wallets
      add constraint wallets_key_material_chk
      check (encrypted_private_key is not null or attestor_key_id is not null);
  end if;
  if not exists (
    select 1 from pg_constraint where conname = 'wallets_binding_state_chk'
  ) then
    alter table wallets
      add constraint wallets_binding_state_chk
      check (binding_state in ('unbound', 'pending', 'bound', 'refused'));
  end if;
end
$$;

create unique index if not exists wallets_did_uidx on wallets (did) where did is not null;

create table if not exists account_provisioning (
  id                  bigserial primary key,
  user_id             uuid not null,
  kind                text not null,
  wallet_id           uuid references wallets(id) on delete set null,
  state               text not null default 'new'
                        check (state in ('new', 'evm_key', 'identity', 'bind_signed', 'funded', 'bind_sent', 'active', 'refused')),
  evm_key_generation  integer not null default 0,
  ed_key_generation   integer not null default 0,
  evm_key_id          text,
  ed_key_id           text,
  ed_public_key       text,
  did                 text,
  main_account_id     text,
  bind_nonce          bigint,
  bind_signature      text,
  topup_raw_tx        text,
  topup_tx_hash       text,
  topup_value_wei     numeric,
  bind_gas            bigint,
  bind_max_fee_wei    numeric,
  bind_raw_tx         text,
  bind_tx_hash        text,
  refusal_reason      text,
  created_at          timestamptz not null default now(),
  updated_at          timestamptz not null default now(),
  unique (user_id, kind)
);

create table if not exists account_setup_audit (
  id              bigserial primary key,
  created_at      timestamptz not null default now(),
  wallet_id       uuid references wallets(id) on delete set null,
  address         text not null,
  event           text not null check (event in ('topup', 'bind', 'refusal', 'backfill')),
  tx_hash         text,
  value_wei       numeric,
  gas             bigint,
  max_fee_wei     numeric,
  outcome         text,
  reason          text
);

create unique index if not exists account_setup_audit_topup_uidx
  on account_setup_audit (lower(address)) where event = 'topup';

create table if not exists account_backfill_cursor (
  name            text primary key,
  last_wallet_id  uuid,
  updated_at      timestamptz not null default now()
);
