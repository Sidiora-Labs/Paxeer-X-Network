import { randomBytes } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';
import { reserveBudget } from './agents.js';
import type { PoolClient } from 'pg';
import type { AgentOriginalRequest, AgentReauthorization } from '../attestor/client.js';
import { query, withTransaction } from './pool.js';
import type { ErrorEnvelope } from '../agent/actions/errors.js';

/**
 * Repository for `agent_actions` (migrations/005_agent_actions.sql) — the
 * durable state machine behind the high-level intent lane.
 *
 * Every mutation persists the exact tx hashes / nonces the wallet assigned so a
 * crash mid-flight is always recoverable without a blind resend. Idempotency is
 * enforced at the DB via UNIQUE(did, idempotency_key): re-submitting the same
 * operation returns the existing row instead of allocating a new nonce.
 */

export type ActionKind = 'layerx_deposit' | 'allowance_and_call';

export type ActionStatus =
  | 'received'
  | 'validating'
  | 'awaiting_approval'
  | 'approval_pending'
  | 'approval_confirmed'
  | 'simulating_call'
  | 'call_pending'
  | 'confirmed'
  | 'reconciling'
  | 'policy_denied'
  | 'invalid_request'
  | 'simulation_reverted'
  | 'transaction_reverted'
  | 'insufficient_balance'
  | 'infrastructure_unavailable'
  | 'failed_terminal';

/** Non-terminal states the worker/reconciler may still advance. */
export const NON_TERMINAL: ActionStatus[] = [
  'received',
  'validating',
  'awaiting_approval',
  'approval_pending',
  'approval_confirmed',
  'simulating_call',
  'call_pending',
  'reconciling',
];

export interface ActionRow {
  id: string;
  did: string;
  wallet_id: string | null;
  wallet_address: string | null;
  kind: ActionKind;
  status: ActionStatus;
  phase: string | null;
  idempotency_key: string;
  request: Record<string, unknown>;
  plan: Record<string, unknown> | null;
  approval_tx_hash: string | null;
  approval_nonce: number | null;
  call_tx_hash: string | null;
  call_nonce: number | null;
  reserved_budget_id: string | null;
  reserved_value_wei: string;
  error_code: string | null;
  error: ErrorEnvelope | null;
  credit_verified: boolean;
  credit: Record<string, unknown> | null;
  worker_locked_until: string | null;
  attempts: number;
  created_at: string;
  updated_at: string;
  terminal_at: string | null;
}

const COLS = `id, did, wallet_id, wallet_address, kind, status, phase, idempotency_key,
  request, plan, approval_tx_hash, approval_nonce, call_tx_hash, call_nonce,
  reserved_budget_id, reserved_value_wei::text, error_code, error,
  credit_verified, credit, worker_locked_until, attempts, created_at, updated_at, terminal_at`;

function newActionId(): string {
  return `act_${randomBytes(12).toString('hex')}`;
}

/**
 * Idempotent create. If (did, idempotency_key) already exists, the EXISTING row
 * is returned untouched (never a new nonce / new action). `created` tells the
 * caller whether this was a fresh insert so the route can 200-replay vs 201.
 */
export async function createOrGetAction(args: {
  did: string;
  walletId: string | null;
  walletAddress: string | null;
  kind: ActionKind;
  idempotencyKey: string;
  request: Record<string, unknown>;
}): Promise<{ row: ActionRow; created: boolean }> {
  return withTransaction(async (client) => {
    const existing = await client.query<ActionRow>(
      `select ${COLS} from agent_actions where did = $1 and idempotency_key = $2 limit 1`,
      [args.did, args.idempotencyKey],
    );
    if (existing.rows[0]) return { row: existing.rows[0], created: false };

    const id = newActionId();
    const { rows } = await client.query<ActionRow>(
      `insert into agent_actions (id, did, wallet_id, wallet_address, kind, idempotency_key, request, status, phase)
       values ($1, $2, $3, $4, $5, $6, $7, 'received', 'intake')
       on conflict (did, idempotency_key) do nothing
       returning ${COLS}`,
      [
        id,
        args.did,
        args.walletId,
        args.walletAddress,
        args.kind,
        args.idempotencyKey,
        JSON.stringify(args.request),
      ],
    );
    // A racing insert won the conflict — pick up their row.
    if (!rows[0]) {
      const racer = await client.query<ActionRow>(
        `select ${COLS} from agent_actions where did = $1 and idempotency_key = $2 limit 1`,
        [args.did, args.idempotencyKey],
      );
      return { row: racer.rows[0]!, created: false };
    }
    return { row: rows[0], created: true };
  });
}

export async function getAction(id: string): Promise<ActionRow | null> {
  const { rows } = await query<ActionRow>(`select ${COLS} from agent_actions where id = $1`, [id]);
  return rows[0] ?? null;
}

/** Read an action scoped to its owning DID (route authorization). */
export async function getActionForDid(id: string, did: string): Promise<ActionRow | null> {
  const { rows } = await query<ActionRow>(
    `select ${COLS} from agent_actions where id = $1 and did = $2`,
    [id, did],
  );
  return rows[0] ?? null;
}

export interface ActionPatch {
  status?: ActionStatus;
  phase?: string | null;
  plan?: Record<string, unknown> | null;
  approval_tx_hash?: string | null;
  approval_nonce?: number | null;
  call_tx_hash?: string | null;
  call_nonce?: number | null;
  reserved_budget_id?: string | null;
  reserved_value_wei?: bigint;
  error_code?: string | null;
  error?: ErrorEnvelope | null;
  credit_verified?: boolean;
  credit?: Record<string, unknown> | null;
  wallet_id?: string | null;
  wallet_address?: string | null;
}

const TERMINAL_SET = new Set<ActionStatus>([
  'confirmed',
  'policy_denied',
  'invalid_request',
  'simulation_reverted',
  'transaction_reverted',
  'insufficient_balance',
  'infrastructure_unavailable',
  'failed_terminal',
]);

/** Patch an action, stamping terminal_at when it enters a terminal status. */
export async function updateAction(id: string, patch: ActionPatch): Promise<ActionRow> {
  const cols: string[] = [];
  const vals: unknown[] = [id];
  const push = (col: string, val: unknown): void => {
    vals.push(val);
    cols.push(`${col} = $${vals.length}`);
  };

  if (patch.status !== undefined) push('status', patch.status);
  if (patch.phase !== undefined) push('phase', patch.phase);
  if (patch.plan !== undefined) push('plan', patch.plan === null ? null : JSON.stringify(patch.plan));
  if (patch.approval_tx_hash !== undefined) push('approval_tx_hash', patch.approval_tx_hash);
  if (patch.approval_nonce !== undefined) push('approval_nonce', patch.approval_nonce);
  if (patch.call_tx_hash !== undefined) push('call_tx_hash', patch.call_tx_hash);
  if (patch.call_nonce !== undefined) push('call_nonce', patch.call_nonce);
  if (patch.reserved_budget_id !== undefined) push('reserved_budget_id', patch.reserved_budget_id);
  if (patch.reserved_value_wei !== undefined)
    push('reserved_value_wei', patch.reserved_value_wei.toString());
  if (patch.error_code !== undefined) push('error_code', patch.error_code);
  if (patch.error !== undefined) push('error', patch.error === null ? null : JSON.stringify(patch.error));
  if (patch.credit_verified !== undefined) push('credit_verified', patch.credit_verified);
  if (patch.credit !== undefined) push('credit', patch.credit === null ? null : JSON.stringify(patch.credit));
  if (patch.wallet_id !== undefined) push('wallet_id', patch.wallet_id);
  if (patch.wallet_address !== undefined) push('wallet_address', patch.wallet_address);

  const setTerminal = patch.status !== undefined && TERMINAL_SET.has(patch.status);
  const terminalClause = setTerminal ? ', terminal_at = coalesce(terminal_at, now())' : '';

  const { rows } = await query<ActionRow>(
    `update agent_actions set ${cols.join(', ')}, updated_at = now()${terminalClause}
      where id = $1 returning ${COLS}`,
    vals,
  );
  return rows[0]!;
}

/**
 * Grace window during which a plan-less row is treated as intake-in-progress
 * rather than claimable: the submit route COMMITS the insert first and only
 * then builds + persists the plan (async chain reads), so a fresh row with
 * plan=NULL is mid-intake — claiming it would terminally fail it with
 * "action has no execution plan". After the grace window a plan-less row is a
 * genuinely orphaned intake (route crashed mid-plan) and the worker may
 * terminal it.
 */
const INTAKE_GRACE_SECONDS = 60;

/**
 * Atomically claim ONE advanceable action for this worker by stamping a lease.
 * `SELECT ... FOR UPDATE SKIP LOCKED` guarantees two workers never take the
 * same row. Returns null when nothing is due. The lease auto-expires so a
 * crashed worker's action becomes claimable again after `leaseMs`.
 */
export async function claimNextAction(leaseMs: number): Promise<ActionRow | null> {
  return withTransaction(async (client) => {
    const { rows } = await client.query<{ id: string }>(
      `select id from agent_actions
        where terminal_at is null
          and status = any($1::text[])
          and error_code is distinct from 'AGENT_REAUTHORIZATION_REQUIRED'
          and (worker_locked_until is null or worker_locked_until < now())
          and (plan is not null or created_at < now() - ($2 || ' seconds')::interval)
        order by updated_at asc
        for update skip locked
        limit 1`,
      [NON_TERMINAL, String(INTAKE_GRACE_SECONDS)],
    );
    const id = rows[0]?.id;
    if (!id) return null;
    const claimed = await client.query<ActionRow>(
      `update agent_actions
          set worker_locked_until = now() + ($2 || ' milliseconds')::interval,
              attempts = attempts + 1,
              updated_at = now()
        where id = $1
        returning ${COLS}`,
      [id, String(leaseMs)],
    );
    return claimed.rows[0]!;
  });
}

/** Release a worker lease (lets the next tick pick the action up immediately). */
export async function releaseLease(id: string): Promise<void> {
  await query(`update agent_actions set worker_locked_until = null, updated_at = now() where id = $1`, [
    id,
  ]).catch(() => undefined);
}

/** Non-terminal actions whose lease is stale — the reconciler's work list. */
export async function listStalledActions(limit = 50): Promise<ActionRow[]> {
  const { rows } = await query<ActionRow>(
    `select ${COLS} from agent_actions
      where terminal_at is null
        and error_code is distinct from 'AGENT_REAUTHORIZATION_REQUIRED'
        and (worker_locked_until is null or worker_locked_until < now())
      order by updated_at asc
      limit $1`,
    [limit],
  );
  return rows;
}

/**
 * Confirmed LayerX deposits whose sequencer credit hasn't landed yet. The
 * layerx-sync worker backfills `credit_verified` from the LayerX deposits table
 * without changing the (already terminal) confirmed status.
 */
export async function listUncreditedLayerxDeposits(limit = 100): Promise<ActionRow[]> {
  const { rows } = await query<ActionRow>(
    `select ${COLS} from agent_actions
      where kind = 'layerx_deposit'
        and status = 'confirmed'
        and credit_verified = false
        and call_tx_hash is not null
      order by terminal_at asc
      limit $1`,
    [limit],
  );
  return rows;
}

/** Flip an already-confirmed deposit to credit-verified with the LayerX snapshot. */
export async function markCreditVerified(
  id: string,
  credit: Record<string, unknown>,
): Promise<void> {
  await query(
    `update agent_actions set credit_verified = true, credit = $2, updated_at = now() where id = $1`,
    [id, JSON.stringify(credit)],
  );
}


export async function retainActionCustodyAuthorization(
  id: string,
  did: string,
  custody: { origin: AgentOriginalRequest; reauthorization?: AgentReauthorization },
): Promise<ActionRow> {
  return withTransaction(async (client) => {
    const selected = await client.query<ActionRow>(`select ${COLS} from agent_actions where id = $1 and did = $2 for update`, [id, did]);
    const row = selected.rows[0];
    if (!row || row.terminal_at !== null || row.error_code !== 'AGENT_REAUTHORIZATION_REQUIRED' || custody.origin.did !== did) throw new Error('action authority cannot be replaced');
    const original = row.request._custody as { origin?: AgentOriginalRequest } | undefined;
    if (!original?.origin || original.origin.method !== custody.origin.method
      || original.origin.did !== custody.origin.did || original.origin.body !== custody.origin.body) {
      throw new Error('reauthorization differs from the durable original request');
    }
    if (!custody.reauthorization || typeof row.error?.cause.signing_request !== 'string') {
      throw new Error('reauthorization requires an outstanding transaction');
    }
    const offered = JSON.parse(custody.reauthorization.body) as Record<string, unknown>;
    const challenged = JSON.parse(row.error.cause.signing_request) as Record<string, unknown>;
    const offeredOrigin = offered.origin as AgentOriginalRequest | undefined;
    if (!offeredOrigin || offeredOrigin.method !== original.origin.method
      || offeredOrigin.did !== original.origin.did || offeredOrigin.body !== original.origin.body) {
      throw new Error('reauthorization original request changed');
    }
    delete offered.origin;
    delete challenged.origin;
    if (!isDeepStrictEqual(offered, challenged)) {
      throw new Error('reauthorization must sign the exact outstanding transaction');
    }
    const updated = await client.query<ActionRow>(
      `update agent_actions set request = jsonb_set(request, '{_custody}', $3::jsonb), error_code = null, error = null, updated_at = now()
        where id = $1 and did = $2 returning ${COLS}`, [id, did, JSON.stringify(custody)],
    );
    if (!updated.rows[0]) throw new Error('action authority update failed');
    return updated.rows[0];
  });
}

export type ActionLeg = 'approval' | 'call';

export interface ActionTransactionDraft {
  chainId: number;
  to: `0x${string}`;
  data: `0x${string}`;
  value: string;
  nonce: number;
  gas: string;
  maxFeePerGas: string;
  maxPriorityFeePerGas: string;
  replaces: `0x${string}` | null;
}

export interface ActionSignedTransaction {
  draft: ActionTransactionDraft;
  raw: `0x${string}`;
  hash: `0x${string}`;
}

export interface ActionLegExecution {
  draft?: ActionTransactionDraft;
  current?: ActionSignedTransaction;
  history?: ActionSignedTransaction[];
}

export function actionLegExecution(row: ActionRow, leg: ActionLeg): ActionLegExecution {
  const execution = row.request._execution as Partial<Record<ActionLeg, ActionLegExecution>> | undefined;
  return execution?.[leg] ?? {};
}

async function lockedAction(client: PoolClient, id: string): Promise<ActionRow> {
  const selected = await client.query<ActionRow>(`select ${COLS} from agent_actions where id = $1 for update`, [id]);
  const row = selected.rows[0];
  if (!row || row.terminal_at !== null) throw new Error('action is no longer advanceable');
  return row;
}

export async function retainActionTransactionDraft(
  id: string, leg: ActionLeg, draft: ActionTransactionDraft,
): Promise<ActionTransactionDraft> {
  return withTransaction(async (client) => {
    const row = await lockedAction(client, id);
    const state = actionLegExecution(row, leg);
    if (state.draft) return state.draft;
    const currentHash = leg === 'approval' ? row.approval_tx_hash : row.call_tx_hash;
    if (currentHash !== draft.replaces) throw new Error('action transaction changed while preparing a draft');
    if (!row.wallet_address) throw new Error('action wallet is missing');
    await client.query(
      `insert into custody_signing_reservations (address, chain_id, action_id, nonce) values ($1, $2, $3, $4)
       on conflict (address, chain_id) do nothing`,
      [row.wallet_address.toLowerCase(), draft.chainId, id, draft.nonce],
    );
    const reservation = await client.query<{ action_id: string; nonce: string }>(
      'select action_id, nonce::text from custody_signing_reservations where address = $1 and chain_id = $2',
      [row.wallet_address.toLowerCase(), draft.chainId],
    );
    if (reservation.rows[0]?.action_id !== id || reservation.rows[0].nonce !== String(draft.nonce)) {
      throw new Error('wallet nonce is already reserved');
    }
    const next = { ...state, draft };
    await client.query(
      `update agent_actions set request = jsonb_set(request, '{_execution}',
        coalesce(request->'_execution', '{}'::jsonb) || jsonb_build_object($2::text, $3::jsonb)), updated_at = now()
        where id = $1`, [id, leg, JSON.stringify(next)],
    );
    return draft;
  });
}

export async function retainActionSignedTransaction(
  id: string, leg: ActionLeg, signed: ActionSignedTransaction,
): Promise<ActionSignedTransaction> {
  return withTransaction(async (client) => {
    const row = await lockedAction(client, id);
    const state = actionLegExecution(row, leg);
    if (state.current?.hash === signed.hash) return state.current;
    if (!state.draft || !isDeepStrictEqual(state.draft, signed.draft)) {
      throw new Error('signed transaction differs from the retained draft');
    }
    const currentHash = leg === 'approval' ? row.approval_tx_hash : row.call_tx_hash;
    if (currentHash !== signed.draft.replaces) throw new Error('action transaction changed before signature retention');
    const reserved = await client.query(
      'update custody_signing_reservations set signed_hash = $3 where action_id = $1 and nonce = $2 and signed_hash is null',
      [id, signed.draft.nonce, signed.hash],
    );
    if (reserved.rowCount !== 1) throw new Error('signed transaction lost its durable nonce reservation');
    const next: ActionLegExecution = {
      current: signed,
      history: [...(state.history ?? []), ...(state.current ? [state.current] : [])],
    };
    await client.query(
      `insert into wallet_signatures
        (user_id, wallet_id, address, kind, to_address, value_wei, chain_id, request_hash, tx_hash, principal_did)
        select w.user_id, w.id, w.address, 'transaction', $1, $2, $3, $4, $5, $6
        from wallets w where w.id = $7`,
      [signed.draft.to, signed.draft.value, signed.draft.chainId,
        `action:${id}:${leg}:${signed.hash}`, signed.hash, row.did, row.wallet_id],
    );
    await client.query(
      `update agent_actions set request = jsonb_set(request, '{_execution}',
        coalesce(request->'_execution', '{}'::jsonb) || jsonb_build_object($2::text, $3::jsonb)) #- '{_custody,reauthorization}',
        ${leg}_tx_hash = $4, ${leg}_nonce = $5, status = $6, phase = $7,
        error_code = null, error = null, updated_at = now() where id = $1`,
      [id, leg, JSON.stringify(next), signed.hash, signed.draft.nonce, `${leg}_pending`, `${leg}_confirmation`],
    );
    return signed;
  });
}

export async function releaseActionBudget(id: string): Promise<void> {
  await withTransaction(async (client) => {
    const row = await lockedAction(client, id);
    if (row.approval_tx_hash || row.call_tx_hash || !row.reserved_budget_id || BigInt(row.reserved_value_wei) <= 0n) return;
    await client.query('update agent_budgets set spent_wei = greatest(0, spent_wei - $2) where id = $1',
      [row.reserved_budget_id, row.reserved_value_wei]);
    await client.query('update agent_actions set reserved_budget_id = null, reserved_value_wei = 0 where id = $1', [id]);
  });
}

export async function releaseActionNonce(id: string, nonce: number): Promise<void> {
  await query('delete from custody_signing_reservations where action_id = $1 and nonce = $2', [id, nonce]);
}

export async function reserveActionBudget(id: string, args: Parameters<typeof reserveBudget>[0]): Promise<string | null> {
  return withTransaction(async (client) => {
    const row = await lockedAction(client, id);
    if (row.did !== args.did) throw new Error('budget principal differs from action');
    const prior = row.request._budget as typeof args | undefined;
    const budgetRequest = { ...args, valueWei: args.valueWei.toString() };
    if (row.reserved_budget_id) {
      if (!isDeepStrictEqual(prior, budgetRequest)) throw new Error('action budget intent changed');
      return row.reserved_budget_id;
    }
    const budgetId = await reserveBudget(args, client);
    if (!budgetId) return null;
    await client.query(
      `update agent_actions set reserved_budget_id = $2, reserved_value_wei = $3,
        request = jsonb_set(request, '{_budget}', $4::jsonb), updated_at = now() where id = $1`,
      [id, budgetId, args.valueWei.toString(), JSON.stringify(budgetRequest)],
    );
    return budgetId;
  });
}

export async function releaseUnsignedActionNonce(id: string): Promise<void> {
  await query('delete from custody_signing_reservations where action_id = $1 and signed_hash is null', [id]);
}
