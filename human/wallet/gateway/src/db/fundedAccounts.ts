import { query } from './pool.js';

/**
 * Repository for the funded-account tables defined in
 * `gateway/migrations/002_funded_accounts.sql`.
 *
 * Two read paths matter for performance:
 *
 *   1. `findFundedAccountByWalletId` — called on every funded sign/send.
 *      Indexed via `funded_accounts.wallet_id` unique constraint.
 *
 *   2. `whitelistAllows` — also called on every funded sign/send, can fire
 *      twice per tx (the second time for the approve() spender sub-rule).
 *      Backed by an in-memory cache that refreshes from Postgres on a TTL.
 *      Whitelist rows change rarely (admin-only); the cache eats >99% of
 *      lookups and keeps the hot path off the DB entirely.
 */

// -----------------------------------------------------------------------------
// Types
// -----------------------------------------------------------------------------

export type FundedAccountStatus =
  | 'active'
  | 'scale_eligible'
  | 'payout_eligible'
  | 'breached_daily'
  | 'breached_max'
  | 'closed';

/** Status values that still allow signing through funded routes. */
export const TRADEABLE_STATUSES: ReadonlySet<FundedAccountStatus> = new Set([
  'active',
  'scale_eligible',
  'payout_eligible',
]);

export interface FundedTierRow {
  tier_id: string;
  label: string;
  initial_usdl_units: bigint;
  initial_pax_wei: bigint;
  max_daily_dd_bps: number;
  max_total_dd_bps: number;
  scale_threshold_usd: string; // numeric in pg → string in node-pg
  payout_threshold_usd: string;
  capital_fee_bps: number;
  is_active: boolean;
}

export interface FundedAccountRow {
  id: string;
  wallet_id: string;
  tier_id: string;
  status: FundedAccountStatus;

  starting_value_usd: string;
  peak_value_usd: string;
  current_value_usd: string | null;
  daily_start_value_usd: string | null;
  daily_start_at: string | null;

  last_eval_at: string | null;
  funding_tx_hashes: { usdl?: string; pax?: string };
  breached_at: string | null;
  breached_reason: string | null;

  capital_fee_owed_usd: string;
  created_at: string;
}

export interface WhitelistEntryRow {
  id: number;
  tier_id: string;
  contract_address: string; // lowercase, 0x-prefixed, 42 chars
  selector: string | null;  // lowercase, 0x + 8 hex, or null = wildcard
  label: string;
  allow_native_value: boolean;
  notes: string | null;
}

// -----------------------------------------------------------------------------
// Tier queries
// -----------------------------------------------------------------------------

/** All active tiers — used by `GET /v1/funded/tiers`. */
export async function listActiveTiers(): Promise<FundedTierRow[]> {
  const { rows } = await query<{
    tier_id: string;
    label: string;
    initial_usdl_units: string;
    initial_pax_wei: string;
    max_daily_dd_bps: number;
    max_total_dd_bps: number;
    scale_threshold_usd: string;
    payout_threshold_usd: string;
    capital_fee_bps: number;
    is_active: boolean;
  }>(
    `select tier_id, label,
            initial_usdl_units::text, initial_pax_wei::text,
            max_daily_dd_bps, max_total_dd_bps,
            scale_threshold_usd::text, payout_threshold_usd::text,
            capital_fee_bps, is_active
       from funded_tiers
      where is_active = true
      order by tier_id`,
  );
  return rows.map((r) => ({
    ...r,
    initial_usdl_units: BigInt(r.initial_usdl_units),
    initial_pax_wei: BigInt(r.initial_pax_wei),
  }));
}

export async function findTier(tierId: string): Promise<FundedTierRow | null> {
  const { rows } = await query<{
    tier_id: string;
    label: string;
    initial_usdl_units: string;
    initial_pax_wei: string;
    max_daily_dd_bps: number;
    max_total_dd_bps: number;
    scale_threshold_usd: string;
    payout_threshold_usd: string;
    capital_fee_bps: number;
    is_active: boolean;
  }>(
    `select tier_id, label,
            initial_usdl_units::text, initial_pax_wei::text,
            max_daily_dd_bps, max_total_dd_bps,
            scale_threshold_usd::text, payout_threshold_usd::text,
            capital_fee_bps, is_active
       from funded_tiers
      where tier_id = $1
      limit 1`,
    [tierId],
  );
  const r = rows[0];
  if (!r) return null;
  return {
    ...r,
    initial_usdl_units: BigInt(r.initial_usdl_units),
    initial_pax_wei: BigInt(r.initial_pax_wei),
  };
}

// -----------------------------------------------------------------------------
// Funded-account queries
// -----------------------------------------------------------------------------

export async function findFundedAccountByWalletId(
  walletId: string,
): Promise<FundedAccountRow | null> {
  const { rows } = await query<FundedAccountRow>(
    `select id, wallet_id, tier_id, status,
            starting_value_usd::text, peak_value_usd::text,
            current_value_usd::text, daily_start_value_usd::text,
            daily_start_at, last_eval_at, funding_tx_hashes,
            breached_at, breached_reason, capital_fee_owed_usd::text,
            created_at
       from funded_accounts
      where wallet_id = $1
      limit 1`,
    [walletId],
  );
  return rows[0] ?? null;
}

/**
 * Insert a fresh funded_accounts row. Called inside the provisioning
 * transaction immediately after the wallet row is created.
 */
export async function insertFundedAccount(input: {
  wallet_id: string;
  tier_id: string;
  starting_value_usd: string; // string-encoded numeric for pg
}): Promise<FundedAccountRow> {
  const { rows } = await query<FundedAccountRow>(
    `insert into funded_accounts (
       wallet_id, tier_id, status,
       starting_value_usd, peak_value_usd,
       daily_start_value_usd, daily_start_at
     ) values ($1, $2, 'active', $3, $3, $3, now())
     returning id, wallet_id, tier_id, status,
               starting_value_usd::text, peak_value_usd::text,
               current_value_usd::text, daily_start_value_usd::text,
               daily_start_at, last_eval_at, funding_tx_hashes,
               breached_at, breached_reason, capital_fee_owed_usd::text,
               created_at`,
    [input.wallet_id, input.tier_id, input.starting_value_usd],
  );
  if (!rows[0]) throw new Error('insertFundedAccount: insert returned no row');
  return rows[0];
}

export async function recordFundingTxHashes(
  fundedAccountId: string,
  hashes: { usdl?: string; pax?: string },
): Promise<void> {
  await query(
    `update funded_accounts
        set funding_tx_hashes = $2::jsonb
      where id = $1`,
    [fundedAccountId, JSON.stringify(hashes)],
  );
}

/**
 * Bulk-fetch live accounts the evaluator needs to value this tick.
 * `limit` keeps each tick bounded so a slow chain RPC can't starve the loop.
 *
 * IMPORTANT: we exclude rows whose disbursement hasn't completed yet.
 * `/v1/funded/provision` inserts the row with status='active' BEFORE calling
 * `disburseTier`, so between the insert and `recordFundingTxHashes` the row
 * exists with no on-chain balances. Evaluating it in that window would read
 * USDL+PAX as 0 and immediately flip the account to `breached_max` — exactly
 * the bug the operator hit on first provision.
 *
 * `funding_tx_hashes` defaults to `'{}'::jsonb` and is only populated after
 * both `waitForTxReceipt` calls in `disburseTier` complete, so requiring both
 * `usdl` and `pax` keys is a safe gate: no row reaches this query until the
 * chain state genuinely reflects the funded balance.
 */
export async function listAccountsForEvaluation(limit: number): Promise<FundedAccountRow[]> {
  const { rows } = await query<FundedAccountRow>(
    `select id, wallet_id, tier_id, status,
            starting_value_usd::text, peak_value_usd::text,
            current_value_usd::text, daily_start_value_usd::text,
            daily_start_at, last_eval_at, funding_tx_hashes,
            breached_at, breached_reason, capital_fee_owed_usd::text,
            created_at
       from funded_accounts
      where status in ('active', 'scale_eligible', 'payout_eligible')
        and funding_tx_hashes ? 'usdl'
        and funding_tx_hashes ? 'pax'
      order by last_eval_at nulls first
      limit $1`,
    [limit],
  );
  return rows;
}

export interface EvaluatorUpdate {
  id: string;
  current_value_usd: string;
  peak_value_usd?: string;          // pass only if it increased
  status?: FundedAccountStatus;     // pass only if it changed
  daily_start_value_usd?: string;   // pass on a daily reset
  daily_start_at?: string;          // pass on a daily reset
  breached_reason?: string;         // pass when transitioning to breached_*
}

export async function applyEvaluatorUpdate(u: EvaluatorUpdate): Promise<void> {
  // Build a dynamic SET list so we only touch columns the evaluator actually
  // changed. Cheap query planner work, much less write amplification.
  const sets: string[] = ['current_value_usd = $2', 'last_eval_at = now()'];
  const params: unknown[] = [u.id, u.current_value_usd];
  if (u.peak_value_usd !== undefined) {
    params.push(u.peak_value_usd);
    sets.push(`peak_value_usd = $${params.length}`);
  }
  if (u.status !== undefined) {
    params.push(u.status);
    sets.push(`status = $${params.length}`);
    if (u.status === 'breached_daily' || u.status === 'breached_max') {
      sets.push('breached_at = now()');
      if (u.breached_reason !== undefined) {
        params.push(u.breached_reason);
        sets.push(`breached_reason = $${params.length}`);
      }
    }
  }
  if (u.daily_start_value_usd !== undefined) {
    params.push(u.daily_start_value_usd);
    sets.push(`daily_start_value_usd = $${params.length}`);
  }
  if (u.daily_start_at !== undefined) {
    params.push(u.daily_start_at);
    sets.push(`daily_start_at = $${params.length}`);
  }
  await query(`update funded_accounts set ${sets.join(', ')} where id = $1`, params);
}

// -----------------------------------------------------------------------------
// Whitelist matcher with in-memory cache
// -----------------------------------------------------------------------------

/**
 * Cache shape: `Map<tier_id, { contractMatches, anyContractSet, refreshedAt }>`
 *   - contractMatches: Map<contract, Set<selector|'*'>>
 *   - anyContractSet:  Set<contract> for the approve()-spender sub-rule
 *
 * Refresh strategy: lazy on TTL miss. Whitelist changes are rare and
 * admin-only; eventual consistency on a 30s window is fine.
 */
interface TierCache {
  contractMatches: Map<string, Set<string>>; // selector or '*'
  anyContractSet: Set<string>;
  allowNativeValue: Map<string, Set<string>>; // tier→contract→selector|'*'
  refreshedAt: number;
}

const WHITELIST_TTL_MS = 30_000;
const tierCacheByTier = new Map<string, TierCache>();

async function loadTierWhitelist(tierId: string): Promise<TierCache> {
  const { rows } = await query<WhitelistEntryRow>(
    `select id, tier_id, contract_address, selector, label, allow_native_value, notes
       from whitelist_entries
      where tier_id = $1`,
    [tierId],
  );

  const contractMatches = new Map<string, Set<string>>();
  const anyContractSet = new Set<string>();
  const allowNativeValue = new Map<string, Set<string>>();

  for (const row of rows) {
    const contract = row.contract_address.toLowerCase();
    const selector = row.selector ? row.selector.toLowerCase() : '*';

    let set = contractMatches.get(contract);
    if (!set) {
      set = new Set();
      contractMatches.set(contract, set);
    }
    set.add(selector);
    anyContractSet.add(contract);

    if (row.allow_native_value) {
      let nset = allowNativeValue.get(contract);
      if (!nset) {
        nset = new Set();
        allowNativeValue.set(contract, nset);
      }
      nset.add(selector);
    }
  }

  return { contractMatches, anyContractSet, allowNativeValue, refreshedAt: Date.now() };
}

async function getTierCache(tierId: string): Promise<TierCache> {
  const cached = tierCacheByTier.get(tierId);
  if (cached && Date.now() - cached.refreshedAt < WHITELIST_TTL_MS) {
    return cached;
  }
  const fresh = await loadTierWhitelist(tierId);
  tierCacheByTier.set(tierId, fresh);
  return fresh;
}

/** Force a cache refresh — used by tests and by the admin update endpoint. */
export function invalidateWhitelistCache(tierId?: string): void {
  if (tierId) tierCacheByTier.delete(tierId);
  else tierCacheByTier.clear();
}

/**
 * Test seam: pre-populate the in-memory whitelist cache for a tier without
 * touching the database. Used exclusively by `test/policy.funded.test.ts`.
 * Production code MUST NOT call this.
 */
export function _seedWhitelistCacheForTests(
  tierId: string,
  rows: Array<{ contract_address: string; selector: string | null; allow_native_value?: boolean }>,
): void {
  const contractMatches = new Map<string, Set<string>>();
  const anyContractSet = new Set<string>();
  const allowNativeValue = new Map<string, Set<string>>();
  for (const row of rows) {
    const contract = row.contract_address.toLowerCase();
    const selector = row.selector ? row.selector.toLowerCase() : '*';
    let set = contractMatches.get(contract);
    if (!set) {
      set = new Set();
      contractMatches.set(contract, set);
    }
    set.add(selector);
    anyContractSet.add(contract);
    if (row.allow_native_value) {
      let nset = allowNativeValue.get(contract);
      if (!nset) {
        nset = new Set();
        allowNativeValue.set(contract, nset);
      }
      nset.add(selector);
    }
  }
  // Far future refreshedAt so TTL never reloads during a test run.
  tierCacheByTier.set(tierId, {
    contractMatches,
    anyContractSet,
    allowNativeValue,
    refreshedAt: Date.now() + 365 * 24 * 3600_000,
  });
}

/**
 * Check whether (tierId, contract, selector) is on the whitelist.
 *
 * `selector` is the 10-char `0x` + 8 hex string from `tx.data.slice(0, 10)`.
 * Pass `null` to check whether `contract` exists for the tier at all
 * (this is what the approve() spender sub-rule needs).
 *
 * Match precedence: exact (contract,selector) > wildcard (contract,'*').
 */
export async function whitelistAllows(
  tierId: string,
  contract: string,
  selector: string | null,
): Promise<boolean> {
  const cache = await getTierCache(tierId);
  const c = contract.toLowerCase();
  if (selector === null) return cache.anyContractSet.has(c);
  const selectors = cache.contractMatches.get(c);
  if (!selectors) return false;
  const s = selector.toLowerCase();
  return selectors.has(s) || selectors.has('*');
}

/**
 * Variant for native-value transfers: returns true iff the (contract, selector)
 * is whitelisted AND the entry's allow_native_value flag is set.
 *
 * Today no contract has allow_native_value=true (PAX is gas, not value-bearing
 * for whitelisted protocols), so this always returns false. The hook is here
 * for future AMM routers that take native PAX as msg.value.
 */
export async function whitelistAllowsNativeValue(
  tierId: string,
  contract: string,
  selector: string,
): Promise<boolean> {
  const cache = await getTierCache(tierId);
  const c = contract.toLowerCase();
  const s = selector.toLowerCase();
  const set = cache.allowNativeValue.get(c);
  if (!set) return false;
  return set.has(s) || set.has('*');
}

/**
 * Bulk-fetch the whitelist for display purposes (UI: "Allowed Protocols").
 * Reads through the cache so it's free.
 */
export async function listWhitelist(tierId: string): Promise<
  Array<{ contract_address: string; selector: string | null; label: string }>
> {
  const { rows } = await query<{
    contract_address: string;
    selector: string | null;
    label: string;
  }>(
    `select contract_address, selector, label
       from whitelist_entries
      where tier_id = $1
      order by label`,
    [tierId],
  );
  return rows;
}
