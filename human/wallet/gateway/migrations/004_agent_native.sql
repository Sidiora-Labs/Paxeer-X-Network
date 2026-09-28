-- =============================================================================
-- Paxeer Embedded Wallet — agent-native lane
--
-- Adds first-class support for AGENTS as principals, distinct from the human
-- (Supabase) users that the standard + funded lanes serve.
--
-- Identity model:
--   - An agent authenticates with its own ed25519 DID key (challenge/verify,
--     see src/auth/did.ts + src/routes/agentAuth.ts), NOT a borrowed human JWT.
--   - DID shape is did:matrix:<label>:<keyfp> where keyfp = hex(pubkey)[:16].
--     In hosted Matrix the <label> is the owner's Supabase user_id, which is
--     how an agent is bound to the human who owns/controls it.
--   - Each agent gets a DEDICATED kind='agent' wallet (own EOA). The owner
--     funds and sweeps it; the agent's blast radius is bounded by that wallet's
--     balance AND the per-agent policy below.
--
-- Control model (the owner-adjustable leash + hard off-limits):
--   - agent_policies      — one row per agent: mode + soft caps + hard switches.
--   - agent_policy_rules  — allow/deny lists (contracts, selectors, tokens,
--                           peer addresses) + the withdrawal allowlist.
--   - agent_budgets       — time-boxed spend grants ("up to X to contract Y
--                           until T").
--   - agent_principals.is_frozen — the instant kill switch.
--
-- Safe by default: new principals are created FROZEN + read_only until the
-- owner raises the leash (env AGENT_DEFAULT_FROZEN / AGENT_DEFAULT_MODE).
--
-- Idempotent: every statement guards with IF [NOT] EXISTS or a DO block, and
-- the migration runner (src/db/migrate.ts) holds a session advisory lock so a
-- 12-worker cluster boot can't double-apply.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) wallets.kind — allow the new 'agent' kind alongside standard + funded.
--    The original CHECK was added inline in 002 (system-named); drop ANY check
--    constraint on wallets whose definition mentions `kind`, then re-add a
--    named one that includes 'agent'. Re-runnable: the loop also drops the
--    constraint we add below, so a second apply is a clean drop+add.
-- -----------------------------------------------------------------------------
do $$
declare c record;
begin
  for c in
    select con.conname
      from pg_constraint con
      join pg_class rel on rel.oid = con.conrelid
     where rel.relname = 'wallets'
       and con.contype = 'c'
       and pg_get_constraintdef(con.oid) ilike '%kind%'
  loop
    execute format('alter table wallets drop constraint %I', c.conname);
  end loop;
end $$;

alter table wallets
  add constraint wallets_kind_check check (kind in ('standard', 'funded', 'agent'));

-- -----------------------------------------------------------------------------
-- 2) agent_principals — one row per agent DID.
--    owner_user_id is the Supabase user that controls this agent. It is set
--    when the DID label parses as a UUID and AGENT_BIND_OWNER_FROM_DID is on,
--    or via an explicit owner claim. NULL = unowned (no human can manage it
--    through the control plane until claimed).
-- -----------------------------------------------------------------------------
create table if not exists agent_principals (
  did               text primary key,
  owner_user_id     uuid,
  label             text not null,                 -- did:matrix:<label>:<fp>
  key_fingerprint   text not null,                 -- hex(pubkey)[:16]; must equal DID fp segment
  public_key        text not null,                 -- 64-hex ed25519 public key (full, for verify)
  wallet_id         uuid references wallets(id) on delete set null,
  is_frozen         boolean not null default true, -- instant kill switch
  created_at        timestamptz not null default now(),
  last_seen_at      timestamptz
);

create index if not exists agent_principals_owner_idx
  on agent_principals (owner_user_id);
create index if not exists agent_principals_wallet_idx
  on agent_principals (wallet_id);

-- -----------------------------------------------------------------------------
-- 3) agent_policies — the owner-adjustable leash. One row per principal.
--    NULL numeric caps mean "fall back to the env AGENT_DEFAULT_* value".
--    Hard switches default to the safe posture (no native transfer, withdrawal
--    allowlist enforced).
-- -----------------------------------------------------------------------------
create table if not exists agent_policies (
  did                        text primary key
                             references agent_principals(did) on delete cascade,
  mode                       text not null default 'read_only'
                             check (mode in ('read_only', 'trade_only', 'full')),
  max_tx_value_wei           numeric(78, 0),
  max_daily_value_wei        numeric(78, 0),
  rate_limit_per_min         integer check (rate_limit_per_min is null or rate_limit_per_min > 0),
  max_approve_wei            numeric(78, 0),
  allow_native_transfer      boolean not null default false,
  withdrawal_allowlist_only  boolean not null default true,
  daily_reset_utc_hour       integer not null default 0
                             check (daily_reset_utc_hour between 0 and 23),
  updated_at                 timestamptz not null default now(),
  updated_by                 uuid
);

-- -----------------------------------------------------------------------------
-- 4) agent_policy_rules — allow/deny lists + withdrawal allowlist.
--
--   effect  : 'allow' | 'deny'
--   subject : what `value` identifies —
--               'contract'   call target address (gates `to`)
--               'selector'   4-byte method selector (0x + 8 hex)
--               'token'      ERC-20 token contract the agent may touch
--               'address'    counterparty/peer address (recipient)
--               'withdrawal' an address the agent is permitted to send funds to
--   value      : lowercased address or selector
--   max_value_wei : optional per-rule cap (e.g. per-token daily ceiling)
--
-- deny rules always beat allow rules (enforced in policy/agent.ts).
-- -----------------------------------------------------------------------------
create table if not exists agent_policy_rules (
  id             bigserial primary key,
  did            text not null references agent_principals(did) on delete cascade,
  effect         text not null check (effect in ('allow', 'deny')),
  subject        text not null check (subject in ('contract', 'selector', 'token', 'address', 'withdrawal')),
  value          text not null,
  max_value_wei  numeric(78, 0),
  note           text,
  created_at     timestamptz not null default now(),
  created_by     uuid,
  constraint agent_policy_rules_unique
    unique nulls not distinct (did, effect, subject, value)
);

create index if not exists agent_policy_rules_lookup_idx
  on agent_policy_rules (did, subject, effect);

-- -----------------------------------------------------------------------------
-- 5) agent_budgets — time-boxed spend grants. An owner can grant an agent an
--    allowance to a specific target/token that auto-expires. spent_wei is
--    advanced atomically as the agent spends (policy/agent.ts).
--    target_contract NULL = any target; token NULL = native PAX value.
-- -----------------------------------------------------------------------------
create table if not exists agent_budgets (
  id               bigserial primary key,
  did              text not null references agent_principals(did) on delete cascade,
  target_contract  text,
  token            text,
  cap_wei          numeric(78, 0) not null check (cap_wei >= 0),
  spent_wei        numeric(78, 0) not null default 0 check (spent_wei >= 0),
  expires_at       timestamptz not null,
  active           boolean not null default true,
  created_at       timestamptz not null default now(),
  created_by       uuid
);

create index if not exists agent_budgets_active_idx
  on agent_budgets (did, active, expires_at);

-- -----------------------------------------------------------------------------
-- 6) agent_auth_challenges — single-use nonce store for DID challenge/verify.
--    Rows are consumed on verify and expire on AGENT_CHALLENGE_TTL_SECONDS.
-- -----------------------------------------------------------------------------
create table if not exists agent_auth_challenges (
  nonce        text primary key,
  did          text not null,
  expires_at   timestamptz not null,
  consumed_at  timestamptz,
  created_at   timestamptz not null default now()
);

create index if not exists agent_auth_challenges_expiry_idx
  on agent_auth_challenges (expires_at);

-- -----------------------------------------------------------------------------
-- 7) wallet_signatures.principal_did — tag agent-originated signatures so the
--    owner control plane can show per-agent activity and the policy engine can
--    aggregate per-agent daily spend independently of human spend.
-- -----------------------------------------------------------------------------
alter table wallet_signatures
  add column if not exists principal_did text;

create index if not exists wallet_signatures_principal_did_idx
  on wallet_signatures (principal_did, created_at desc)
  where principal_did is not null;
