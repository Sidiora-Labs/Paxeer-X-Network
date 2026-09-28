-- =============================================================================
-- 003 — Dedupe whitelist + fix NULL-selector uniqueness
--
-- Root cause this migration is fixing:
--
--   When 002_funded_accounts.sql first ran on a multi-worker cluster (12
--   workers via node:cluster), every worker raced through `runMigrations()`
--   before any of them had written to the `_migrations` ledger. One worker
--   won the ledger insert and survived; the other 11 crashed on the ledger's
--   primary-key violation. BUT — before they crashed, each had already
--   executed every INSERT in 002, which means each whitelist row was inserted
--   12 times.
--
--   The UNIQUE (tier_id, contract_address, selector) constraint did dedupe
--   the entries whose `selector` was NOT NULL (USDL.approve), but Postgres
--   treats NULL = NULL as UNKNOWN by default, so wildcard entries (selector
--   IS NULL) were never recognised as duplicates and all 12 rows landed.
--
--   Result on the live DB (2026-05-18): 11 surplus copies of every wildcard
--   row, single copy of USDL.approve as intended.
--
-- This migration:
--   1. Drops the broken UNIQUE constraint.
--   2. Deletes duplicate whitelist rows, keeping the lowest id per
--      (tier_id, contract_address, COALESCE(selector, '')) — i.e. NULL is
--      treated as a value for dedup purposes.
--   3. Recreates the UNIQUE constraint with NULLS NOT DISTINCT (Postgres 15+
--      semantics; chain 125 prod runs postgres:16) so NULL selectors collide
--      the same way values do.
--
-- The migrate.ts upgrade in this same PR adds a Postgres session-scoped
-- advisory lock around the runMigrations() loop so the boot race that
-- caused this can never happen again, regardless of how many workers
-- start in parallel.
-- =============================================================================

-- 1) Drop the constraint so step 2 can delete freely without violating it.
alter table whitelist_entries
  drop constraint if exists whitelist_entries_unique_rule;

-- 2) Dedupe. Self-join on logical equality (treating NULL as ''); keep the
-- lowest id; delete the rest. Safe even if there are no dupes (no-op on
-- fresh setups that ran 002 cleanly).
delete from whitelist_entries a
  using whitelist_entries b
  where a.tier_id = b.tier_id
    and a.contract_address = b.contract_address
    and coalesce(a.selector, '') = coalesce(b.selector, '')
    and a.id > b.id;

-- 3) Recreate UNIQUE with NULLS NOT DISTINCT so wildcards dedupe correctly
-- on future inserts. Postgres 15+ syntax — confirmed against
-- `postgres:16-alpine` in `docker-compose.yml`.
alter table whitelist_entries
  add constraint whitelist_entries_unique_rule
  unique nulls not distinct (tier_id, contract_address, selector);
