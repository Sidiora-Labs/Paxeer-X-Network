create table if not exists wallet_custody_submissions (
  id text primary key check (id ~ '^0x[0-9a-f]{64}$'),
  user_id uuid not null,
  wallet_id uuid not null references wallets(id),
  address text not null check (address ~ '^0x[0-9a-f]{40}$'),
  chain_id bigint not null check (chain_id > 0),
  nonce bigint not null check (nonce >= 0),
  custody text not null check (length(custody) <= 65538 and custody ~ '^0x([0-9a-f]{2})+$'),
  signature text not null check (signature ~ '^0x[0-9a-f]{130}$'),
  unsigned_tx text not null check (length(unsigned_tx) <= 65538 and unsigned_tx ~ '^0x([0-9a-f]{2})+$'),
  raw_tx text check (raw_tx is null or (length(raw_tx) <= 65538 and raw_tx ~ '^0x([0-9a-f]{2})+$')),
  tx_hash text check (tx_hash is null or tx_hash ~ '^0x[0-9a-f]{64}$'),
  state text not null default 'pending' check (state in ('pending','confirmed','reverted')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((raw_tx is null) = (tx_hash is null))
);
create unique index if not exists wallet_custody_pending_owner on wallet_custody_submissions(address,chain_id) where state='pending';
create unique index if not exists wallet_custody_nonce_owner on wallet_custody_submissions(address,chain_id,nonce);
