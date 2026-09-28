alter table wallets
  add column if not exists archived_at timestamptz;

update wallets
   set archived_at = now()
 where archived_at is null
   and (kind = 'funded'
        or id in (select wallet_id from funded_accounts));

create index if not exists wallets_archived_idx
  on wallets (archived_at)
  where archived_at is not null;

alter table if exists funded_accounts
  drop constraint if exists funded_accounts_tier_id_fkey;

drop table if exists whitelist_entries;
drop table if exists funded_tiers;
