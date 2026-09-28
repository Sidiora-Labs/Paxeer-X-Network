import type { FastifyInstance, FastifyRequest } from 'fastify';
import { createHash } from 'node:crypto';
import type { Hex } from 'viem';
import { requireAgent } from '../middleware/principal.js';
import { requireSignedAgentRequest } from '../agent/verify.js';
import { env } from '../env.js';
import {
  PRECOMPILE_ADDRESSES,
  TEE_FAMILIES,
  eip712,
  scheduler,
  streams,
  teeAttestor,
} from '../precompiles.js';
import {
  Eip712DomainSeparatorBody,
  Eip712HashBody,
  Eip712RecoverBody,
  RescheduleBody,
  ScheduleBody,
  SchedulerJobIdBody,
  StreamIdBody,
  StreamOpenBody,
  StreamUpdateRateBody,
  TeeRootBody,
  TeeVerifyBody,
} from '../schemas/agent.js';
import { executeAgentTransaction, getAgentWallet, type AgentTxInput } from './agentExec.js';
import type { AgentTxIntent } from '../policy/agent.js';

/**
 * Network-native precompile surface for agents.
 *
 *   Writes (scheduler.*, streams.*) run through executeAgentTransaction, so the
 *   full agent policy applies. Crucially, the withdrawal allowlist is extended
 *   to the EVENTUAL beneficiary: schedule()'s `target` and openStream()'s
 *   `payee` are threaded into intent.tokenRecipient. That closes the obvious
 *   bypass where an agent schedules/streams funds to a non-allowlisted sink.
 *
 *   Reads (getJob/pending/getStream/accrued, all of eip712 + teeAttestor) are
 *   pure eth_calls — no signing, no policy gate beyond requireAgent.
 */

function hashRequest(payload: unknown): string {
  return createHash('sha256').update(JSON.stringify(payload)).digest('hex');
}

function buildAudit(req: FastifyRequest, payload: unknown): {
  request_hash: string;
  ip: string | null;
  user_agent: string | null;
} {
  const xff = req.headers['x-forwarded-for'];
  const ip = typeof xff === 'string' && xff.length > 0 ? (xff.split(',')[0]?.trim() ?? req.ip) : req.ip;
  return {
    request_hash: hashRequest(payload),
    ip,
    user_agent: (req.headers['user-agent'] as string | undefined) ?? null,
  };
}

function resolveFamily(f: string | number): number {
  return typeof f === 'number' ? f : TEE_FAMILIES[f as keyof typeof TEE_FAMILIES];
}

export async function agentPrecompileRoutes(app: FastifyInstance): Promise<void> {
  // ── Scheduler (0x0905) ─────────────────────────────────────────────────────

  app.post('/v1/agent/precompiles/scheduler/schedule', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = ScheduleBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const { target, call_data, execute_at_block, gas_limit, deposit_wei } = parsed.data;
    const depositWei = deposit_wei !== undefined ? BigInt(deposit_wei) : undefined;

    const call = scheduler.schedule({
      target: target as `0x${string}`,
      callData: (call_data as Hex | undefined) ?? '0x',
      executeAtBlock: BigInt(execute_at_block),
      gasLimit: BigInt(gas_limit),
      depositWei,
    });
    const tx: AgentTxInput = {
      to: call.to,
      data: call.data,
      value: depositWei ?? 0n,
    };
    // Thread the eventual call target into the policy so the withdrawal
    // allowlist + denylist apply to where the scheduled job will ultimately act.
    const intent: AgentTxIntent = {
      kind: 'transaction',
      to: call.to,
      value: depositWei ?? 0n,
      data: call.data,
      tokenRecipient: target.toLowerCase(),
    };
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx,
      broadcast: true,
      audit: buildAudit(req, { schedule: parsed.data }),
      label: 'scheduler.schedule',
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/precompiles/scheduler/cancel', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = SchedulerJobIdBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const call = scheduler.cancel(BigInt(parsed.data.job_id));
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent: { kind: 'transaction', to: call.to, value: 0n, data: call.data },
      tx: { to: call.to, data: call.data, value: 0n },
      broadcast: true,
      audit: buildAudit(req, { cancel: parsed.data }),
      label: 'scheduler.cancel',
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/precompiles/scheduler/reschedule', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = RescheduleBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const call = scheduler.reschedule(BigInt(parsed.data.job_id), BigInt(parsed.data.new_block));
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent: { kind: 'transaction', to: call.to, value: 0n, data: call.data },
      tx: { to: call.to, data: call.data, value: 0n },
      broadcast: true,
      audit: buildAudit(req, { reschedule: parsed.data }),
      label: 'scheduler.reschedule',
    });
    return reply.code(res.status).send(res.body);
  });

  app.get('/v1/agent/precompiles/scheduler/job/:id', { preHandler: requireAgent }, async (req, reply) => {
    const { id } = req.params as { id: string };
    if (!/^\d{1,80}$/.test(id)) return reply.code(400).send({ error: 'invalid_job_id' });
    const job = await scheduler.getJob(BigInt(id)).catch((err) => {
      req.log.warn({ err }, 'getJob failed');
      return null;
    });
    if (!job) return reply.code(404).send({ error: 'not_found' });
    return reply.send({ job });
  });

  app.get('/v1/agent/precompiles/scheduler/pending', { preHandler: requireAgent }, async (req, reply) => {
    const wallet = await getAgentWallet(req.agent!.did);
    if (!wallet) return reply.code(404).send({ error: 'no_wallet', message: 'provision first' });
    const ids = await scheduler.pending(wallet.address).catch(() => []);
    return reply.send({ creator: wallet.address, job_ids: ids });
  });

  // ── PaymentStreams (0x0906) ─────────────────────────────────────────────────

  app.post('/v1/agent/precompiles/streams/open', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = StreamOpenBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const { payee, token, rate_per_second, start_time, stop_time, cap } = parsed.data;
    const call = streams.open({
      payee: payee as `0x${string}`,
      token: token as `0x${string}`,
      ratePerSecond: BigInt(rate_per_second),
      startTime: start_time !== undefined ? BigInt(start_time) : 0n,
      stopTime: stop_time !== undefined ? BigInt(stop_time) : 0n,
      cap: BigInt(cap),
    });
    // Stream funds flow token→payee up to `cap`: apply token + withdrawal gates.
    const intent: AgentTxIntent = {
      kind: 'transaction',
      to: call.to,
      value: 0n,
      data: call.data,
      tokenContract: token.toLowerCase(),
      tokenRecipient: payee.toLowerCase(),
      tokenAmount: BigInt(cap),
    };
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx: { to: call.to, data: call.data, value: 0n },
      broadcast: true,
      audit: buildAudit(req, { open: parsed.data }),
      label: 'streams.open',
    });
    return reply.code(res.status).send(res.body);
  });

  for (const [path, op] of [
    ['settle', (id: bigint) => streams.settle(id)],
    ['close', (id: bigint) => streams.close(id)],
  ] as const) {
    app.post(`/v1/agent/precompiles/streams/${path}`, { preHandler: requireSignedAgentRequest }, async (req, reply) => {
      const parsed = StreamIdBody.safeParse(req.body);
      if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
      const call = op(BigInt(parsed.data.stream_id));
      const res = await executeAgentTransaction({
        ctx: req.agent!,
        intent: { kind: 'transaction', to: call.to, value: 0n, data: call.data },
        tx: { to: call.to, data: call.data, value: 0n },
        broadcast: true,
        audit: buildAudit(req, { [path]: parsed.data }),
        label: `streams.${path}`,
      });
      return reply.code(res.status).send(res.body);
    });
  }

  app.post('/v1/agent/precompiles/streams/update-rate', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = StreamUpdateRateBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const call = streams.updateRate(BigInt(parsed.data.stream_id), BigInt(parsed.data.new_rate));
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent: { kind: 'transaction', to: call.to, value: 0n, data: call.data },
      tx: { to: call.to, data: call.data, value: 0n },
      broadcast: true,
      audit: buildAudit(req, { updateRate: parsed.data }),
      label: 'streams.updateRate',
    });
    return reply.code(res.status).send(res.body);
  });

  app.get('/v1/agent/precompiles/streams/stream/:id', { preHandler: requireAgent }, async (req, reply) => {
    const { id } = req.params as { id: string };
    if (!/^\d{1,80}$/.test(id)) return reply.code(400).send({ error: 'invalid_stream_id' });
    const stream = await streams.getStream(BigInt(id)).catch(() => null);
    if (!stream) return reply.code(404).send({ error: 'not_found' });
    return reply.send({ stream });
  });

  app.get('/v1/agent/precompiles/streams/accrued/:id', { preHandler: requireAgent }, async (req, reply) => {
    const { id } = req.params as { id: string };
    if (!/^\d{1,80}$/.test(id)) return reply.code(400).send({ error: 'invalid_stream_id' });
    const accrued = await streams.accrued(BigInt(id)).catch(() => null);
    if (accrued === null) return reply.code(502).send({ error: 'read_failed' });
    return reply.send({ stream_id: id, accrued });
  });

  // ── EIP-712 helper (0x0908) — pure views ────────────────────────────────────

  app.post('/v1/agent/precompiles/eip712/domain-separator', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = Eip712DomainSeparatorBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const { name, version, chain_id, verifying_contract } = parsed.data;
    const separator = await eip712.domainSeparator({
      name,
      version,
      chainId: BigInt(chain_id ?? env.HYPERPAXEER_CHAIN_ID),
      verifyingContract: verifying_contract as `0x${string}`,
    });
    return reply.send({ separator });
  });

  app.post('/v1/agent/precompiles/eip712/hash-typed-data', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = Eip712HashBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const digest = await eip712.hashTypedData(
      parsed.data.domain_separator as Hex,
      parsed.data.struct_hash as Hex,
    );
    return reply.send({ digest });
  });

  app.post('/v1/agent/precompiles/eip712/recover', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = Eip712RecoverBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const signer = await eip712.recoverTypedSigner(
      parsed.data.domain_separator as Hex,
      parsed.data.struct_hash as Hex,
      parsed.data.signature as Hex,
    );
    return reply.send({ signer });
  });

  // ── TEE Attestor (0x0907) — views ───────────────────────────────────────────

  app.post('/v1/agent/precompiles/tee/verify', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = TeeVerifyBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const family = resolveFamily(parsed.data.family);
    try {
      const att = parsed.data.expected_report_data
        ? await teeAttestor.verifyAndExpect(family, parsed.data.quote as Hex, parsed.data.expected_report_data as Hex)
        : await teeAttestor.verify(family, parsed.data.quote as Hex);
      return reply.send({ attestation: att });
    } catch (err) {
      // A failed attestation reverts on-chain → surface as a 422, not a 500.
      return reply.code(422).send({ error: 'attestation_failed', detail: (err as Error).message });
    }
  });

  app.post('/v1/agent/precompiles/tee/root-count', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = TeeRootBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const family = resolveFamily(parsed.data.family);
    const count = await teeAttestor.rootCount(family).catch(() => null);
    if (count === null) return reply.code(502).send({ error: 'read_failed' });
    return reply.send({ family, count });
  });

  app.post('/v1/agent/precompiles/tee/root-of', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = TeeRootBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    if (parsed.data.index === undefined) return reply.code(400).send({ error: 'index_required' });
    const family = resolveFamily(parsed.data.family);
    const root = await teeAttestor.rootOf(family, BigInt(parsed.data.index)).catch(() => null);
    if (root === null) return reply.code(502).send({ error: 'read_failed' });
    return reply.send({ family, index: parsed.data.index, root });
  });

  // Expose the precompile address book for client convenience.
  app.get('/v1/agent/precompiles', { preHandler: requireAgent }, async (_req, reply) => {
    return reply.send({ addresses: PRECOMPILE_ADDRESSES, tee_families: TEE_FAMILIES });
  });
}
