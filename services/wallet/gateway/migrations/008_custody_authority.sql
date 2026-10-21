create table wallet_custody_authority (
  singleton boolean primary key default true check (singleton),
  sequence numeric(20,0) not null default 0 check (sequence >= 0 and sequence <= 18446744073709551615),
  inventory_sequence numeric(20,0) not null default 0 check (inventory_sequence >= 0 and inventory_sequence <= 18446744073709551615),
  snapshot text,
  published_at timestamptz,
  acknowledged_participants integer not null default 0 check (acknowledged_participants in (0,3,5)),
  updated_at timestamptz not null default now()
);
insert into wallet_custody_authority (singleton) values (true);

create table custody_budget_reservations (
  id uuid primary key,
  did text not null references agent_principals(did),
  budget_id bigint not null references agent_budgets(id),
  request_nonce text not null check (request_nonce ~ '^[0-9a-f]{32}$'),
  unsigned_transaction text not null check (unsigned_transaction ~ '^0x[0-9a-f]+$'),
  value_wei numeric(78,0) not null check (value_wei > 0),
  created_at timestamptz not null default now(),
  unique (did, request_nonce)
);
