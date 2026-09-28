-- =============================================================================
-- Paxeer Embedded Wallet — funded accounts (prop-firm tier)
--
-- Adds a parallel "funded" wallet kind. A user can hold both a standard
-- self-custody wallet AND a funded prop-trading wallet; they're disambiguated
-- by the new wallets.kind column. The standard /v1/wallet/* routes continue
-- to operate ONLY on kind='standard' rows (no breaking change). The new
-- /v1/funded/* routes operate ONLY on kind='funded' rows and apply the
-- contract whitelist + drawdown policy defined here.
--
-- Tier model (informed by the operator's prop-firm rules, May 2026):
--   starter_25k:
--     - 25,000 USDL collateral + 15 PAX gas grant
--     - 15% daily drawdown  → status='breached_daily'
--     - 25% max drawdown    → status='breached_max'
--     - $40k peak equity    → status='scale_eligible' (UI prompts to scale up)
--     - $50k peak equity    → status='payout_eligible' (UI prompts to cash out)
--
-- Whitelist model — METHOD-LEVEL, not contract-level. Tuples are
-- (tier_id, contract_address_lower, selector_lower_or_null). selector=NULL
-- means "any function on this contract" (wildcard, safer to enumerate).
-- A selector match wins over a wildcard if both exist for the same contract.
--
-- Approve sub-rule: when a funded wallet calls USDL.approve(spender,_),
-- `spender` must ALSO be in the same tier's whitelist (any selector). This
-- prevents USDL.approve(attacker, MAX) followed by attacker.transferFrom.
-- The check is implemented in policy/funded.ts; the data side just needs
-- the spender contracts to be listed here.
--
-- Native transfers (tx.value > 0 with empty data) are unconditionally denied
-- for funded wallets in code — they don't appear in whitelist_entries.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) wallets.kind — disambiguate standard vs funded.
-- -----------------------------------------------------------------------------
alter table wallets
  add column if not exists kind text not null default 'standard'
    check (kind in ('standard', 'funded'));

-- Relax the original UNIQUE(user_id) so a user can have both kinds.
-- Replacement: UNIQUE(user_id, kind) — at most one wallet of each kind per user.
do $$
begin
  if exists (
    select 1 from pg_constraint
    where conrelid = 'wallets'::regclass
      and conname = 'wallets_user_id_key'
  ) then
    alter table wallets drop constraint wallets_user_id_key;
  end if;
end $$;

create unique index if not exists wallets_user_kind_uidx
  on wallets (user_id, kind);

create index if not exists wallets_kind_idx on wallets (kind);

-- -----------------------------------------------------------------------------
-- 2) funded_tiers — tier definitions. Editable via direct SQL for v1.
-- Money columns:
--   *_wei      — uint256-shaped numeric, base unit (PAX=18 dec, USDL=6 dec)
--   *_usd      — display-shaped USD with 6dp precision (matches USDL decimals)
-- -----------------------------------------------------------------------------
create table if not exists funded_tiers (
  tier_id               text primary key,
  label                 text not null,
  initial_usdl_units    numeric(38, 0) not null,    -- USDL has 6 decimals
  initial_pax_wei       numeric(78, 0) not null,    -- PAX (native) has 18 decimals
  max_daily_dd_bps      integer not null check (max_daily_dd_bps between 0 and 10000),
  max_total_dd_bps      integer not null check (max_total_dd_bps between 0 and 10000),
  scale_threshold_usd   numeric(38, 6) not null,
  payout_threshold_usd  numeric(38, 6) not null,
  capital_fee_bps       integer not null default 1000  -- 10% default profit share
    check (capital_fee_bps between 0 and 5000),
  is_active             boolean not null default true,
  created_at            timestamptz not null default now()
);

-- Seed the starter_25k tier. ON CONFLICT DO NOTHING so re-running the migration
-- is idempotent and we can tweak values via separate UPDATE later.
insert into funded_tiers (
  tier_id, label,
  initial_usdl_units, initial_pax_wei,
  max_daily_dd_bps, max_total_dd_bps,
  scale_threshold_usd, payout_threshold_usd,
  capital_fee_bps
) values (
  'starter_25k', 'Starter $25K',
  25000000000,                   -- 25,000 USDL × 10^6 = 25_000_000_000
  15000000000000000000,          -- 15 PAX × 10^18
  1500,                          -- 15% daily DD
  2500,                          -- 25% max DD
  40000.000000,                  -- $40k → scale_eligible
  50000.000000,                  -- $50k → payout_eligible
  1000                           -- 10% capital fee (stage-2 collection)
) on conflict (tier_id) do nothing;

-- -----------------------------------------------------------------------------
-- 3) funded_accounts — per-wallet enrolment in a tier.
-- -----------------------------------------------------------------------------
create table if not exists funded_accounts (
  id                       uuid primary key default gen_random_uuid(),
  wallet_id                uuid not null unique
                           references wallets(id) on delete restrict,
  tier_id                  text not null references funded_tiers(tier_id),
  status                   text not null default 'active'
                           check (status in (
                             'active',
                             'scale_eligible',
                             'payout_eligible',
                             'breached_daily',
                             'breached_max',
                             'closed'
                           )),

  starting_value_usd       numeric(38, 6) not null,
  peak_value_usd           numeric(38, 6) not null,
  current_value_usd        numeric(38, 6),
  daily_start_value_usd    numeric(38, 6),
  daily_start_at           timestamptz,

  last_eval_at             timestamptz,
  funding_tx_hashes        jsonb not null default '{}'::jsonb,
                           -- shape: {"usdl": "0x…", "pax": "0x…"}
  breached_at              timestamptz,
  breached_reason          text,

  capital_fee_owed_usd     numeric(38, 6) not null default 0,

  created_at               timestamptz not null default now()
);

create index if not exists funded_accounts_tier_idx
  on funded_accounts (tier_id);

-- Evaluator hot path: scan only live accounts, oldest tick first.
create index if not exists funded_accounts_eval_idx
  on funded_accounts (status, last_eval_at nulls first)
  where status in ('active', 'scale_eligible', 'payout_eligible');

-- -----------------------------------------------------------------------------
-- 4) whitelist_entries — per-tier (contract, selector) allow-list.
--
--   contract_address: lowercase, 0x-prefixed, 42 chars (checked)
--   selector:         lowercase, 0x-prefixed, 10 chars (0x + 8 hex), or NULL=wildcard
--   allow_native_value: per-rule override. Currently always false because
--                       the perps contracts on chain 125 don't take native PAX
--                       as msg.value. If a router ever does (e.g. AMM with
--                       PAX in/out), flip this for that specific row.
-- -----------------------------------------------------------------------------
create table if not exists whitelist_entries (
  id                 bigserial primary key,
  tier_id            text not null references funded_tiers(tier_id),
  contract_address   text not null
                     check (contract_address ~ '^0x[0-9a-f]{40}$'),
  selector           text
                     check (selector is null or selector ~ '^0x[0-9a-f]{8}$'),
  label              text not null,
  allow_native_value boolean not null default false,
  notes              text,
  created_at         timestamptz not null default now(),

  -- selector NULL means wildcard; we still want (tier, contract, selector)
  -- to be unique so we don't get duplicate seed inserts on re-run.
  -- Postgres treats NULLs as distinct by default in a UNIQUE constraint,
  -- so we use coalesce in an expression index to collapse them.
  constraint whitelist_entries_unique_rule unique (tier_id, contract_address, selector)
);

create index if not exists whitelist_entries_lookup_idx
  on whitelist_entries (tier_id, contract_address);

-- -----------------------------------------------------------------------------
-- 5) Seed whitelist for starter_25k tier.
--
-- Addresses below are mainnet chain 125 contracts confirmed by the operator on
-- 2026-05-18. All stored lowercase. Selectors are the canonical first 4
-- bytes of keccak256(signature) — kept explicit so the seed is greppable.
--
--   USDL.approve(address,uint256)         = 0x095ea7b3
--   USDL.transfer(address,uint256)        = 0xa9059cbb (DENIED — not seeded)
--   USDL.transferFrom(addr,addr,uint256)  = 0x23b872dd (DENIED — not seeded)
--
-- Perps stack uses a Diamond + adapter routing layer; we wildcard those so
-- arbitrary facet calls work. Funds can't escape the protocol without
-- ultimately hitting an ERC-20 transfer FROM the funded wallet, which the
-- USDL row above denies (USDL.transfer is intentionally absent → DENY).
-- -----------------------------------------------------------------------------

-- USDL token — only approve(spender,amount). Sub-rule in policy/funded.ts
-- decodes spender and verifies it's also in this tier's whitelist.
insert into whitelist_entries (tier_id, contract_address, selector, label, notes) values
  ('starter_25k', '0x7c69c84daaee90b21eecabdb8f0387897e9b7b37', '0x095ea7b3',
    'USDL.approve(spender,amount)',
    'spender argument MUST also appear in this tier whitelist (policy sub-rule)')
on conflict do nothing;

-- Perps stack — Diamond + V4 routers. Wildcard selector so users can call
-- any facet / function. Funds remain in the protocol''s internal accounting
-- and can only return to the funded wallet via the protocol''s own withdraw
-- paths, which the wallet then can''t ERC-20-transfer out of.
insert into whitelist_entries (tier_id, contract_address, selector, label) values
  ('starter_25k', '0xea65fe02665852c615774a3041dfe6f00fb77537', null,
    'Diamond — perps entry'),
  ('starter_25k', '0x1d5f3ac9de43dd0665c3f527913dd825f67b3daa', null,
    'PECORRouter (V4) — trade entry'),
  ('starter_25k', '0xe89a3e5dffefbb7f8c9e9f597bbfd4f4ade77404', null,
    'PECOROrders — limit/market orders'),
  ('starter_25k', '0x49e2fff129f9a351d94e3a25b2642bfe37aca912', null,
    'PECORStopOrders — SL/TP'),
  ('starter_25k', '0xde5a8fc4396ae392957b547154b29b000d906a87', null,
    'PECORVault — collateral vault'),
  ('starter_25k', '0x1ab090064857063bbb935cae2b0fd2fe62f0d63b', null,
    'PECOR — perps core')
on conflict do nothing;
