import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';
import { requireAgent } from '../middleware/principal.js';
import { requireSignedAgentRequest } from '../agent/verify.js';
import { env } from '../env.js';
import { AllowanceAndCallBody, ActionIdParam, LayerxDepositBody } from '../schemas/agent.js';
import { ensureAgentWallet } from './agentExec.js';
import {
  createOrGetAction,
  retainActionCustodyAuthorization,
  getActionForDid,
  updateAction,
  type ActionRow,
} from '../db/actions.js';
import {
  buildAllowanceAndCallPlan,
  buildLayerxDepositPlan,
  PlanError,
  type ActionPlan,
} from '../agent/actions/plan.js';
import { buildErrorEnvelope, type ErrorEnvelope } from '../agent/actions/errors.js';

/**
 * High-level intent routes — the durable-action lane.
 *
 * One POST creates (idempotently) a durable action; the wallet owns the whole
 * approve → confirm → call → confirm → verify sequence server-side via the
 * background worker (agent/actions/worker.ts). The agent polls GET
 * /v1/agent/actions/:id. Resubmitting the same idempotency_key returns the
 * SAME action — never a second nonce, never a repeated deposit.
 */

const TERMINAL_SUCCESS = new Set(['confirmed']);
const TERMINAL_FAILURE = new Set([
  'policy_denied',
  'invalid_request',
  'simulation_reverted',
  'transaction_reverted',
  'insufficient_balance',
  'infrastructure_unavailable',
  'failed_terminal',
]);

/** Shape an action row into the stable public response. */
function serialize(row: ActionRow): Record<string, unknown> {
  const terminal = row.terminal_at !== null;
  const success = TERMINAL_SUCCESS.has(row.status);
  const failed = TERMINAL_FAILURE.has(row.status);
  return {
    ok: success ? true : failed ? false : null,
    action_id: row.id,
    kind: row.kind,
    status: row.status,
    phase: row.phase,
    terminal,
    approval: row.approval_tx_hash
      ? { tx_hash: row.approval_tx_hash, nonce: row.approval_nonce }
      : null,
    call: row.call_tx_hash ? { tx_hash: row.call_tx_hash, nonce: row.call_nonce } : null,
    credit_verified: row.credit_verified,
    credit: row.credit,
    // The interpretation-free envelope for the last unsuccessful step (present
    // for both terminal failures AND in-flight POLL_ACTION/RETRY states).
    error: row.error,
    poll: { endpoint: `/v1/agent/actions/${row.id}`, after_ms: terminal ? null : 1_000 },
    created_at: row.created_at,
    updated_at: row.updated_at,
    terminal_at: row.terminal_at,
  };
}

export async function agentActionRoutes(app: FastifyInstance): Promise<void> {
  // ── LayerX deposit ─────────────────────────────────────────────────────────
  app.post('/v1/agent/actions/layerx/deposit', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    if (!env.LAYERX_VAULT_ADDRESS) {
      return reply
        .code(503)
        .send({ error: 'layerx_disabled', message: 'LAYERX_VAULT_ADDRESS not configured' });
    }
    const parsed = LayerxDepositBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const body = parsed.data;

    return submit(app, req, reply, {
      did: req.agent!.did,
      kind: 'layerx_deposit',
      idempotencyKey: body.idempotency_key,
      request: { asset: body.asset, amount: body.amount, did_claim: body.did_claim },
      buildPlan: () => buildLayerxDepositPlan({ amount: body.amount, didClaim: body.did_claim }),
    });
  });

  // ── Generic allowance-and-call ──────────────────────────────────────────────
  app.post('/v1/agent/actions/allowance-and-call', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AllowanceAndCallBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const body = parsed.data;

    return submit(app, req, reply, {
      did: req.agent!.did,
      kind: 'allowance_and_call',
      idempotencyKey: body.idempotency_key,
      request: {
        token: body.token,
        amount: body.amount,
        spender: body.spender,
        contract: body.contract,
        method: body.method,
        args: body.args,
      },
      buildPlan: () =>
        buildAllowanceAndCallPlan({
          token: body.token,
          amount: body.amount,
          spender: body.spender,
          contract: body.contract,
          method: body.method,
          args: body.args,
        }),
    });
  });

  // ── Poll ────────────────────────────────────────────────────────────────────
  app.get('/v1/agent/actions/:action_id', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = ActionIdParam.safeParse(req.params);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_action_id', issues: parsed.error.issues });
    }
    const row = await getActionForDid(parsed.data.action_id, req.agent!.did);
    if (!row) return reply.code(404).send({ error: 'not_found', message: 'no such action for this agent' });
    return reply.send(serialize(row));
  });
}

/**
 * Idempotent submit: create-or-get the action, build + persist its plan on first
 * creation, and hand it to the worker. A replay returns the existing action's
 * current state; a plan-building failure marks the action invalid_request and
 * returns the envelope (with an action_id to poll).
 */
async function submit(
  app: FastifyInstance,
  req: FastifyRequest,
  reply: FastifyReply,
  args: {
    did: string;
    kind: 'layerx_deposit' | 'allowance_and_call';
    idempotencyKey: string;
    request: Record<string, unknown>;
    buildPlan: () => Promise<ActionPlan> | ActionPlan;
  },
): Promise<unknown> {
  let walletId: string | null = null;
  let walletAddress: string | null = null;
  try {
    const wallet = await ensureAgentWallet(req.agent!);
    walletId = wallet.id;
    walletAddress = wallet.address;
  } catch (err) {
    req.log.error({ err, did: args.did }, 'agent action: wallet provision failed');
    return reply.code(500).send({ error: 'provision_failed', detail: (err as Error).message });
  }

  const { row, created } = await createOrGetAction({
    did: args.did,
    walletId,
    walletAddress,
    kind: args.kind,
    idempotencyKey: args.idempotencyKey,
    request: { ...args.request, ...(req.agent?.custody ? { _custody: req.agent.custody } : {}) },
  });

  // Replay: the same operation was already submitted — return its state as-is.
  // NEVER re-plan or re-nonce.
  if (!created) {
    if (req.agent?.custody?.reauthorization && row.terminal_at === null) {
      try {
        const authorized = await retainActionCustodyAuthorization(row.id, args.did, req.agent.custody);
        return reply.code(200).send({ replay: true, ...serialize(authorized) });
      } catch {
        return reply.code(409).send({ error: 'action_authorization_mismatch' });
      }
    }
    return reply.code(200).send({ replay: true, ...serialize(row) });
  }

  // First creation: build + persist the execution plan. Semantic plan failures
  // are terminal invalid_request with an action_id to poll.
  try {
    const plan = await args.buildPlan();
    const updated = await updateAction(row.id, {
      plan: plan as unknown as Record<string, unknown>,
      phase: 'validation',
      wallet_id: walletId,
      wallet_address: walletAddress,
    });
    // 202: accepted + owned server-side; the worker advances it from here.
    return reply.code(202).send(serialize(updated));
  } catch (err) {
    if (err instanceof PlanError) {
      const envelope: ErrorEnvelope = buildErrorEnvelope({
        actionId: row.id,
        phase: 'validation',
        code: err.code,
        message: err.message,
      });
      const updated = await updateAction(row.id, {
        status: 'invalid_request',
        phase: 'validation',
        error_code: err.code,
        error: envelope,
      });
      return reply.code(400).send(serialize(updated));
    }
    // Unexpected: leave the action for the worker to retry rather than lose it.
    req.log.warn({ err: (err as Error).message, action: row.id }, 'agent action: plan build error');
    return reply.code(202).send(serialize(row));
  }
}
