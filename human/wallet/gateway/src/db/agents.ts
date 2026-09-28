import { query, withTransaction } from './pool.js';
import { env } from '../env.js';

/**
 * Repository for the agent-native tables (migrations/004_agent_native.sql).
 *
 * Plaintext key material never lives here — agent wallets reuse the encrypted
 * `wallets` rows via db/wallets.ts. This module owns principals, the policy
 * leash, allow/deny rules, time-boxed budgets, and the auth-challenge nonce
 * store. All queries are strictly parameterised.
 */

// -----------------------------------------------------------------------------
// Types
// -----------------------------------------------------------------------------

export type AgentMode = 'read_only' | 'trade_only' | 'full';
export type RuleEffect = 'allow' | 'deny';
export type RuleSubject = 'contract' | 'selector' | 'token' | 'address' | 'withdrawal';

export interface AgentPrincipalRow {
  did: string;
  owner_user_id: string | null;
  label: string;
  key_fingerprint: string;
  public_key: string;
  wallet_id: string | null;
  is_frozen: boolean;
  created_at: string;
  last_seen_at: string | null;
}

export interface AgentPolicyRow {
  did: string;
  mode: AgentMode;
  max_tx_value_wei: bigint | null;
  max_daily_value_wei: bigint | null;
  rate_limit_per_min: number | null;
  max_approve_wei: bigint | null;
  allow_native_transfer: boolean;
  withdrawal_allowlist_only: boolean;
  daily_reset_utc_hour: number;
  updated_at: string;
  updated_by: string | null;
}

export interface AgentPolicyRuleRow {
  id: string;
  did: string;
  effect: RuleEffect;
  subject: RuleSubject;
  value: string;
  max_value_wei: bigint | null;
  note: string | null;
  created_at: string;
  created_by: string | null;
}

export interface AgentBudgetRow {
  id: string;
  did: string;
  target_contract: string | null;
  token: string | null;
  cap_wei: bigint;
  spent_wei: bigint;
  expires_at: string;
  active: boolean;
  created_at: string;
  created_by: string | null;
}

function toBigIntOrNull(v: unknown): bigint | null {
  if (v === null || v === undefined || v === '') return null;
  return BigInt(String(v));
}

// -----------------------------------------------------------------------------
// Auth challenges (single-use nonce store)
// -----------------------------------------------------------------------------

/** Persist a freshly-minted challenge nonce. */
export async function createChallenge(did: string, nonce: string, ttlSeconds: number): Promise<void> {
  await query(
    `insert into agent_auth_challenges (nonce, did, expires_at)
     values ($1, $2, now() + ($3 || ' seconds')::interval)`,
    [nonce, did, String(ttlSeconds)],
  );
}

/**
 * Atomically consume a challenge: mark it used IFF it exists, matches the DID,
 * is unexpired, and hasn't been consumed. Returns true on success (single-use).
 */
export async function consumeChallenge(nonce: string, did: string): Promise<boolean> {
  const { rowCount } = await query(
    `update agent_auth_challenges
        set consumed_at = now()
      where nonce = $1
        and did = $2
        and consumed_at is null
        and expires_at > now()`,
    [nonce, did],
  );
  return (rowCount ?? 0) > 0;
}

/** Opportunistic cleanup of expired/consumed challenges. Best-effort. */
export async function purgeExpiredChallenges(): Promise<void> {
  await query(
    `delete from agent_auth_challenges where expires_at < now() - interval '1 hour'`,
  ).catch(() => undefined);
}

// -----------------------------------------------------------------------------
// Principals
// -----------------------------------------------------------------------------

export async function findPrincipal(did: string): Promise<AgentPrincipalRow | null> {
  const { rows } = await query<AgentPrincipalRow>(
    `select did, owner_user_id, label, key_fingerprint, public_key, wallet_id,
            is_frozen, created_at, last_seen_at
       from agent_principals where did = $1 limit 1`,
    [did],
  );
  return rows[0] ?? null;
}

export async function listPrincipalsByOwner(ownerUserId: string): Promise<AgentPrincipalRow[]> {
  const { rows } = await query<AgentPrincipalRow>(
    `select did, owner_user_id, label, key_fingerprint, public_key, wallet_id,
            is_frozen, created_at, last_seen_at
       from agent_principals where owner_user_id = $1 order by created_at desc`,
    [ownerUserId],
  );
  return rows;
}

/**
 * Upsert a principal on a successful DID verify. Creates the row (with the
 * safe-by-default frozen posture + a default policy) on first sight, otherwise
 * bumps last_seen_at. It never records an owner.
 *
 * Principal + default policy are written in one transaction so a principal can
 * never exist without a policy row.
 */
export async function upsertPrincipalOnVerify(args: {
  did: string;
  label: string;
  keyFingerprint: string;
  publicKey: string;
}): Promise<AgentPrincipalRow> {
  return withTransaction(async (client) => {
    const { rows } = await client.query<AgentPrincipalRow>(
      `insert into agent_principals (did, label, key_fingerprint, public_key, is_frozen)
       values ($1, $2, $3, $4, $5)
       on conflict (did) do update
         set last_seen_at = now()
       returning did, owner_user_id, label, key_fingerprint, public_key, wallet_id,
                 is_frozen, created_at, last_seen_at`,
      [args.did, args.label, args.keyFingerprint, args.publicKey, env.AGENT_DEFAULT_FROZEN],
    );
    // Ensure a default policy row exists for this principal.
    await client.query(
      `insert into agent_policies (did, mode) values ($1, $2)
       on conflict (did) do nothing`,
      [args.did, env.AGENT_DEFAULT_MODE],
    );
    // last_seen_at on a brand-new row is null; set it.
    await client.query(`update agent_principals set last_seen_at = now() where did = $1`, [args.did]);
    return rows[0]!;
  });
}

export async function setPrincipalWallet(did: string, walletId: string): Promise<void> {
  await query(`update agent_principals set wallet_id = $2 where did = $1`, [did, walletId]);
}

export async function setPrincipalFrozen(did: string, frozen: boolean): Promise<void> {
  await query(`update agent_principals set is_frozen = $2 where did = $1`, [did, frozen]);
}

/** Record the owner of an UNOWNED principal. Returns false if it already has an owner. */
export async function claimPrincipal(did: string, ownerUserId: string): Promise<boolean> {
  const { rowCount } = await query(
    `update agent_principals set owner_user_id = $2
      where did = $1 and owner_user_id is null`,
    [did, ownerUserId],
  );
  return (rowCount ?? 0) > 0;
}

// -----------------------------------------------------------------------------
// Policies
// -----------------------------------------------------------------------------

export async function getPolicy(did: string): Promise<AgentPolicyRow | null> {
  const { rows } = await query<{
    did: string;
    mode: AgentMode;
    max_tx_value_wei: string | null;
    max_daily_value_wei: string | null;
    rate_limit_per_min: number | null;
    max_approve_wei: string | null;
    allow_native_transfer: boolean;
    withdrawal_allowlist_only: boolean;
    daily_reset_utc_hour: number;
    updated_at: string;
    updated_by: string | null;
  }>(
    `select did, mode, max_tx_value_wei::text, max_daily_value_wei::text,
            rate_limit_per_min, max_approve_wei::text, allow_native_transfer,
            withdrawal_allowlist_only, daily_reset_utc_hour, updated_at, updated_by
       from agent_policies where did = $1 limit 1`,
    [did],
  );
  const r = rows[0];
  if (!r) return null;
  return {
    ...r,
    max_tx_value_wei: toBigIntOrNull(r.max_tx_value_wei),
    max_daily_value_wei: toBigIntOrNull(r.max_daily_value_wei),
    max_approve_wei: toBigIntOrNull(r.max_approve_wei),
  };
}

export interface PolicyPatch {
  mode?: AgentMode;
  max_tx_value_wei?: bigint | null;
  max_daily_value_wei?: bigint | null;
  rate_limit_per_min?: number | null;
  max_approve_wei?: bigint | null;
  allow_native_transfer?: boolean;
  withdrawal_allowlist_only?: boolean;
  daily_reset_utc_hour?: number;
}

/** Upsert the policy row for a principal, applying only the provided fields. */
export async function upsertPolicy(
  did: string,
  patch: PolicyPatch,
  updatedBy: string | null,
): Promise<AgentPolicyRow> {
  // Build a dynamic SET list over the provided keys only.
  const cols: string[] = [];
  const vals: unknown[] = [did];
  const push = (col: string, val: unknown): void => {
    vals.push(val);
    cols.push(`${col} = $${vals.length}`);
  };
  if (patch.mode !== undefined) push('mode', patch.mode);
  if (patch.max_tx_value_wei !== undefined)
    push('max_tx_value_wei', patch.max_tx_value_wei === null ? null : patch.max_tx_value_wei.toString());
  if (patch.max_daily_value_wei !== undefined)
    push('max_daily_value_wei', patch.max_daily_value_wei === null ? null : patch.max_daily_value_wei.toString());
  if (patch.rate_limit_per_min !== undefined) push('rate_limit_per_min', patch.rate_limit_per_min);
  if (patch.max_approve_wei !== undefined)
    push('max_approve_wei', patch.max_approve_wei === null ? null : patch.max_approve_wei.toString());
  if (patch.allow_native_transfer !== undefined) push('allow_native_transfer', patch.allow_native_transfer);
  if (patch.withdrawal_allowlist_only !== undefined)
    push('withdrawal_allowlist_only', patch.withdrawal_allowlist_only);
  if (patch.daily_reset_utc_hour !== undefined) push('daily_reset_utc_hour', patch.daily_reset_utc_hour);

  vals.push(updatedBy);
  const updatedByPlaceholder = `$${vals.length}`;

  // Ensure the row exists, then patch. on-conflict updates the supplied cols.
  await query(
    `insert into agent_policies (did, mode) values ($1, $2)
     on conflict (did) do nothing`,
    [did, patch.mode ?? env.AGENT_DEFAULT_MODE],
  );
  const setClause = [...cols, `updated_by = ${updatedByPlaceholder}`, 'updated_at = now()'].join(', ');
  await query(`update agent_policies set ${setClause} where did = $1`, vals);
  const updated = await getPolicy(did);
  return updated!;
}

// -----------------------------------------------------------------------------
// Rules (allow/deny + withdrawal allowlist)
// -----------------------------------------------------------------------------

export async function listRules(did: string): Promise<AgentPolicyRuleRow[]> {
  const { rows } = await query<{
    id: string;
    did: string;
    effect: RuleEffect;
    subject: RuleSubject;
    value: string;
    max_value_wei: string | null;
    note: string | null;
    created_at: string;
    created_by: string | null;
  }>(
    `select id::text, did, effect, subject, value, max_value_wei::text, note, created_at, created_by
       from agent_policy_rules where did = $1 order by id asc`,
    [did],
  );
  return rows.map((r) => ({ ...r, max_value_wei: toBigIntOrNull(r.max_value_wei) }));
}

export async function addRule(args: {
  did: string;
  effect: RuleEffect;
  subject: RuleSubject;
  value: string;
  maxValueWei?: bigint | null;
  note?: string | null;
  createdBy?: string | null;
}): Promise<AgentPolicyRuleRow> {
  const { rows } = await query<{ id: string }>(
    `insert into agent_policy_rules (did, effect, subject, value, max_value_wei, note, created_by)
     values ($1, $2, $3, $4, $5, $6, $7)
     on conflict (did, effect, subject, value) do update
       set max_value_wei = excluded.max_value_wei, note = excluded.note
     returning id::text`,
    [
      args.did,
      args.effect,
      args.subject,
      args.value.toLowerCase(),
      args.maxValueWei != null ? args.maxValueWei.toString() : null,
      args.note ?? null,
      args.createdBy ?? null,
    ],
  );
  const all = await listRules(args.did);
  return all.find((r) => r.id === rows[0]!.id)!;
}

export async function deleteRule(did: string, ruleId: string): Promise<boolean> {
  const { rowCount } = await query(`delete from agent_policy_rules where did = $1 and id = $2`, [
    did,
    ruleId,
  ]);
  return (rowCount ?? 0) > 0;
}

// -----------------------------------------------------------------------------
// Budgets (time-boxed spend grants; pre-charge + release-on-failure)
// -----------------------------------------------------------------------------

export async function listActiveBudgets(did: string): Promise<AgentBudgetRow[]> {
  const { rows } = await query<{
    id: string;
    did: string;
    target_contract: string | null;
    token: string | null;
    cap_wei: string;
    spent_wei: string;
    expires_at: string;
    active: boolean;
    created_at: string;
    created_by: string | null;
  }>(
    `select id::text, did, target_contract, token, cap_wei::text, spent_wei::text,
            expires_at, active, created_at, created_by
       from agent_budgets
      where did = $1 and active and expires_at > now()
      order by id asc`,
    [did],
  );
  return rows.map((r) => ({ ...r, cap_wei: BigInt(r.cap_wei), spent_wei: BigInt(r.spent_wei) }));
}

export async function addBudget(args: {
  did: string;
  targetContract?: string | null;
  token?: string | null;
  capWei: bigint;
  expiresAt: Date;
  createdBy?: string | null;
}): Promise<AgentBudgetRow> {
  const { rows } = await query<{ id: string }>(
    `insert into agent_budgets (did, target_contract, token, cap_wei, expires_at, created_by)
     values ($1, $2, $3, $4, $5, $6)
     returning id::text`,
    [
      args.did,
      args.targetContract ? args.targetContract.toLowerCase() : null,
      args.token ? args.token.toLowerCase() : null,
      args.capWei.toString(),
      args.expiresAt.toISOString(),
      args.createdBy ?? null,
    ],
  );
  const all = await listActiveBudgets(args.did);
  return all.find((b) => b.id === rows[0]!.id)!;
}

export async function deactivateBudget(did: string, budgetId: string): Promise<boolean> {
  const { rowCount } = await query(
    `update agent_budgets set active = false where did = $1 and id = $2`,
    [did, budgetId],
  );
  return (rowCount ?? 0) > 0;
}

/**
 * Atomically reserve `valueWei` against the MOST SPECIFIC active, unexpired
 * budget that covers (targetContract, token) and still has headroom. Returns
 * the charged budget id, or null if no matching budget could absorb the spend.
 *
 * Specificity order: both scoped > target-scoped > token-scoped > unscoped.
 */
export async function reserveBudget(args: {
  did: string;
  targetContract: string | null;
  token: string | null;
  valueWei: bigint;
}): Promise<string | null> {
  const { rows } = await query<{ id: string }>(
    `update agent_budgets b
        set spent_wei = spent_wei + $4
      where b.id = (
        select id from agent_budgets
         where did = $1 and active and expires_at > now()
           and (target_contract is null or target_contract = $2)
           and (token is null or token = $3)
           and spent_wei + $4 <= cap_wei
         order by ((target_contract is not null)::int + (token is not null)::int) desc, id asc
         limit 1
      )
      returning id::text`,
    [
      args.did,
      args.targetContract ? args.targetContract.toLowerCase() : null,
      args.token ? args.token.toLowerCase() : null,
      args.valueWei.toString(),
    ],
  );
  return rows[0]?.id ?? null;
}

/** Release a previously reserved budget amount (called when a send fails). */
export async function releaseBudget(budgetId: string, valueWei: bigint): Promise<void> {
  await query(
    `update agent_budgets set spent_wei = greatest(0, spent_wei - $2) where id = $1`,
    [budgetId, valueWei.toString()],
  ).catch(() => undefined);
}

// -----------------------------------------------------------------------------
// Spend + rate aggregates (per-agent, keyed on wallet_signatures.principal_did)
// -----------------------------------------------------------------------------

/** Count agent signing requests in the last 60s (rate-limit window). */
export async function countAgentSignaturesLastMinute(did: string): Promise<number> {
  const { rows } = await query<{ count: string }>(
    `select count(*)::text as count
       from wallet_signatures
      where principal_did = $1 and created_at >= now() - interval '60 seconds'`,
    [did],
  );
  return Number(rows[0]?.count ?? '0');
}

/** Sum native-value tx spend for this agent since `windowStartIso`. */
export async function sumAgentDailyValue(did: string, windowStartIso: string): Promise<bigint> {
  const { rows } = await query<{ total: string }>(
    `select coalesce(sum(value_wei), 0)::text as total
       from wallet_signatures
      where principal_did = $1 and kind = 'transaction' and created_at >= $2`,
    [did, windowStartIso],
  );
  return BigInt(rows[0]?.total ?? '0');
}
