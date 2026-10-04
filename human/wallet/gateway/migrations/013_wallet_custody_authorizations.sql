create table if not exists wallet_custody_authorizations (
  id text primary key check (id ~ '^0x[0-9a-f]{64}$'),
  user_id uuid not null,
  wallet_id uuid not null references wallets(id),
  address text not null check (address ~ '^0x[0-9a-f]{40}$'),
  chain_id bigint not null check (chain_id > 0),
  attestor_key_id text not null check (length(attestor_key_id) between 1 and 4096),
  custody text not null check (length(custody) <= 65538 and custody ~ '^0x([0-9a-f]{2})+$'),
  state text not null check (state in ('signing_unknown','signed')),
  signature text check (signature is null or signature ~ '^0x[0-9a-f]{130}$'),
  evidence jsonb check (evidence is null or (jsonb_typeof(evidence) = 'object' and octet_length(evidence::text) <= 32768)),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  completed_at timestamptz,
  check ((state = 'signed') = (signature is not null)),
  check ((state = 'signed') = (evidence is not null)),
  check ((state = 'signed') = (completed_at is not null))
);
create index if not exists wallet_custody_authorizations_owner on wallet_custody_authorizations(user_id,created_at);

create or replace function preserve_wallet_custody_authorization() returns trigger as $$
begin
  if tg_op = 'UPDATE' then
    if new.id is distinct from old.id or new.user_id is distinct from old.user_id
      or new.wallet_id is distinct from old.wallet_id or new.address is distinct from old.address
      or new.chain_id is distinct from old.chain_id or new.attestor_key_id is distinct from old.attestor_key_id
      or new.custody is distinct from old.custody or new.created_at is distinct from old.created_at then
      raise exception 'wallet custody authorization identity is immutable';
    end if;
    if old.state = 'signed' and (new.state is distinct from old.state
      or new.signature is distinct from old.signature or new.evidence is distinct from old.evidence
      or new.completed_at is distinct from old.completed_at) then
      raise exception 'wallet custody authorization proof is immutable';
    end if;
  end if;
  if not exists (select 1 from wallets where id=new.wallet_id and user_id=new.user_id
    and lower(address)=new.address and chain_id=new.chain_id and attestor_key_id=new.attestor_key_id
    and not is_disabled and archived_at is null and kind='standard') then
    raise exception 'wallet custody authorization owner binding differs';
  end if;
  return new;
end;
$$ language plpgsql;
drop trigger if exists wallet_custody_authorization_binding on wallet_custody_authorizations;
create trigger wallet_custody_authorization_binding before insert or update on wallet_custody_authorizations
  for each row execute function preserve_wallet_custody_authorization();
