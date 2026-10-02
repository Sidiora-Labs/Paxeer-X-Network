create table if not exists agent_signing_authorizations (
  id text primary key check (id ~ '^[0-9a-f]{64}$'),
  did text not null references agent_principals(did),
  authorization jsonb not null,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);
create index if not exists agent_signing_authorizations_expiry on agent_signing_authorizations (did, expires_at);
