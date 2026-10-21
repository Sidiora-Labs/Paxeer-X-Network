create table if not exists nonce_outstanding (
  address     text not null check (address = lower(address)),
  nonce       bigint not null check (nonce >= 0),
  tx_hash     text check (tx_hash is null or tx_hash ~ '^0x[0-9a-f]{64}$'),
  member      text check (member is null or tx_hash is not null),
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  primary key (address, nonce)
);

alter table nonce_allocations add column if not exists latest_seen bigint not null default 0 check (latest_seen >= 0);
