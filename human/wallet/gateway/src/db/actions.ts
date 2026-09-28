import { randomBytes } from 'node:crypto';
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
