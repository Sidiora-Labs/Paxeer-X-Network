create table wallets (
  id                      uuid primary key,
  user_id                 uuid not null,
  address                 text not null unique,
  encrypted_private_key   text not null,
  key_version             smallint not null default 1,
  chain_id                integer not null default 125,
  created_at              timestamptz not null default now(),
  last_used_at            timestamptz,
  is_disabled             boolean not null default false,
  disabled_reason         text,
  kind                    text not null default 'standard' check (kind in ('standard', 'funded')),
  unique (user_id, kind)
);

create table funded_accounts (
  id         bigserial primary key,
  wallet_id  uuid not null unique references wallets(id) on delete cascade,
  tier_id    text not null,
  status     text not null default 'active'
);

insert into wallets (id, user_id, address, encrypted_private_key, key_version, chain_id, created_at, kind) values
  ('00000000-0000-4000-8000-000000000001', '10000000-0000-4000-8000-000000000001',
   '0x1111111111111111111111111111111111111111', 'v1:synthetic-iv-1:synthetic-ciphertext-1:synthetic-tag-1', 1, 125,
   '2026-05-01T00:00:00Z', 'standard'),
  ('00000000-0000-4000-8000-000000000002', '10000000-0000-4000-8000-000000000002',
   '0x2222222222222222222222222222222222222222', 'v1:synthetic-iv-2:synthetic-ciphertext-2:synthetic-tag-2', 1, 125,
   '2026-05-02T00:00:00Z', 'funded'),
  ('00000000-0000-4000-8000-000000000003', '10000000-0000-4000-8000-000000000003',
   '0x3333333333333333333333333333333333333333', 'v1:synthetic-iv-3:synthetic-ciphertext-3:synthetic-tag-3', 1, 1,
   '2026-05-03T00:00:00Z', 'standard');

insert into funded_accounts (wallet_id, tier_id) values ('00000000-0000-4000-8000-000000000002', 'starter_25k');
