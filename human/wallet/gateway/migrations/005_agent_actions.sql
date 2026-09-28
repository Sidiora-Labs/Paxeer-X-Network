-- =============================================================================
-- Paxeer Embedded Wallet — agent durable actions (high-level intent lane)
--
-- Turns a one-shot agent signing request into a DURABLE, server-driven,
-- idempotent operation. One POST (e.g. layerx/deposit) records an action row;
-- a background worker owns the approve → confirm → call → confirm → verify
-- sequence, survives a client disconnect or a process restart, and is polled
-- via GET /v1/agent/actions/:id.
--
-- Design notes:
--   - The state machine lives in `status`; `phase` names the sub-step an error
--     happened in (for the interpretation-free error envelope). Both are TEXT
--     with CHECK constraints so an unknown value can never be written.
--   - Idempotency is a UNIQUE(did, idempotency_key): resubmitting the same
--     operation returns the existing action instead of allocating a new nonce.
--   - Every tx hash + nonce the wallet assigns is persisted the instant it is
--     known, so a crash mid-broadcast is always recoverable (never a blind
--     resend). `worker_locked_until` gives the worker/reconciler a lease so two
--     workers never drive the same action at once.
--
-- Idempotent + re-runnable: guarded with IF NOT EXISTS; the migration runner
-- (src/db/migrate.ts) holds a session advisory lock during apply.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) agent_actions — one row per high-level intent submission.
-- -----------------------------------------------------------------------------
create table if not exists agent_actions (
  id                   text primary key,               -- 'act_' + 24 hex
  did                  text not null references agent_principals(did) on delete cascade,
  wallet_id            uuid references wallets(id) on delete set null,
  wallet_address       text,                            -- the agent EOA driving the tx sequence
  kind                 text not null
                       check (kind in ('layerx_deposit', 'allowance_and_call')),

  -- Durable state machine. Non-terminal states are advanced by the worker;
  -- terminal states are final and never transitioned out of.
  status               text not null default 'received'
                       check (status in (
                         -- in-flight
                         'received', 'validating', 'awaiting_approval',
                         'approval_pending', 'approval_confirmed',
                         'simulating_call', 'call_pending', 'confirmed',
                         'reconciling',
                         -- terminal
                         'policy_denied', 'invalid_request', 'simulation_reverted',
                         'transaction_reverted', 'insufficient_balance',
                         'infrastructure_unavailable', 'failed_terminal'
                       )),
  -- Sub-step label for the error envelope (e.g. 'approval_confirmation').
  phase                text,

  -- Idempotency: the same (did, idempotency_key) always maps to this action.
  idempotency_key      text not null,

  -- The validated request as received (asset, amount, did_claim, spender, ...).
  request              jsonb not null,
  -- Resolved execution plan (token address, decimals, spender, contract,
  -- calldata, amount_wei) filled in during the `validating` phase.
  plan                 jsonb,

  -- Approval leg (only present when an approve was required).
  approval_tx_hash     text,
  approval_nonce       bigint,
  -- Primary call leg (depositUSDL / the arbitrary method call).
  call_tx_hash         text,
  call_nonce           bigint,

  -- Budget the policy gate reserved for this action; released iff nothing was
  -- broadcast and the action fails (wallet-owned rollback correctness).
  reserved_budget_id   text,
  reserved_value_wei   numeric(78, 0) not null default 0,

  -- The interpretation-free error envelope for the last unsuccessful step.
  error_code           text,
  error                jsonb,

  -- Authoritative LayerX credit result once verified (deposits row snapshot).
  credit_verified      boolean not null default false,
  credit               jsonb,

  -- Worker lease: a worker takes an action by stamping a short future lease so
  -- a second worker/reconciler skips it. Cleared when the worker yields.
  worker_locked_until  timestamptz,
  attempts             integer not null default 0,

  created_at           timestamptz not null default now(),
  updated_at           timestamptz not null default now(),
  terminal_at          timestamptz,

  constraint agent_actions_idem_unique unique (did, idempotency_key)
);

-- Worker pickup: cheapest scan for the next advanceable action.
create index if not exists agent_actions_worker_idx
  on agent_actions (status, worker_locked_until)
  where terminal_at is null;

create index if not exists agent_actions_did_idx
  on agent_actions (did, created_at desc);

-- Fast reverse lookup from an on-chain hash back to its action (reconciler).
create index if not exists agent_actions_call_tx_idx
  on agent_actions (call_tx_hash) where call_tx_hash is not null;
create index if not exists agent_actions_approval_tx_idx
  on agent_actions (approval_tx_hash) where approval_tx_hash is not null;

-- -----------------------------------------------------------------------------
-- 2) layerx_accounts — wallet-side mirror of the LayerX per-DID ledger head.
--    Populated read-only from the LayerX sequencer Postgres (jobs/layerxSync).
--    Gives the wallet an in-house DID → escrow/balance view joinable to
--    agent_principals (DID → agent wallet → owner) without a cross-service call
--    on the hot path.
-- -----------------------------------------------------------------------------
create table if not exists layerx_accounts (
  did             text primary key,
  evm_address     text,
  balance_usdx    numeric(78, 0) not null default 0,   -- micro-USDX (1 USDX = 1e6)
  escrow_usdx     numeric(78, 0) not null default 0,
  layerx_updated  timestamptz,                         -- accounts.updated_at from LayerX
  synced_at       timestamptz not null default now()
);

create index if not exists layerx_accounts_evm_idx
  on layerx_accounts (lower(evm_address));
