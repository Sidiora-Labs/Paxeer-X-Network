create table if not exists custody_signing_reservations (
  address text not null,
  chain_id bigint not null,
  action_id text not null references agent_actions(id),
  nonce bigint not null check (nonce >= 0),
  signed_hash text check (signed_hash is null or signed_hash ~ '^0x[0-9a-f]{64}$'),
  created_at timestamptz not null default now(),
  primary key (address, chain_id)
);
