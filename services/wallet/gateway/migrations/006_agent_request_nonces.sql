create table if not exists agent_request_nonces (
  did         text not null,
  nonce       text not null check (nonce ~ '^[0-9a-f]{32}$'),
  expires_at  timestamptz not null,
  created_at  timestamptz not null default now(),
  primary key (did, nonce)
);

create index if not exists agent_request_nonces_expiry_idx
  on agent_request_nonces (expires_at);
