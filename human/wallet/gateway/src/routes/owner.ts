import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';
import { createHash } from 'node:crypto';
import { createWalletClient, http, type Hex } from 'viem';
import { requireAuth } from '../middleware/auth.js';
import { ownsPrincipal } from '../middleware/principal.js';
import { parseDid } from '../auth/did.js';
import { hyperPaxeer } from '../chain.js';
import { env } from '../env.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  addBudget,
  addRule,
  claimPrincipal,
  deactivateBudget,
  deleteRule,
  findPrincipal,
  getPolicy,
  listActiveBudgets,
  listPrincipalsByOwner,
  listRules,
  setPrincipalFrozen,
  upsertPolicy,
  type AgentPrincipalRow,
  type PolicyPatch,
} from '../db/agents.js';
import {
  agentWalletUserId,
  findWalletByUserId,
  getSigningAccountForRow,
  logSignature,
  type WalletRow,
} from '../db/wallets.js';
import { encodeErc20Transfer, getErc20Balance, getNativeBalance } from '../chainReads.js';
import { query } from '../db/pool.js';
import { effectivePolicy } from '../policy/agent.js';
import {
  BudgetBody,
  FundAgentBody,
  PolicyPatchBody,
  RuleBody,
  SweepBody,
} from '../schemas/agent.js';

/**
 * Owner control plane.
 *
 * Authed with the owner's human Supabase JWT (requireAuth → req.user). Every
 * route operates on an agent the caller OWNS — ownership = the principal's
 * owner_user_id equals the caller's user id (see ownsPrincipal). This is the
 * surface where an owner adjusts the leash (policy/rules/budgets), funds and
 * sweeps the agent wallet, and hits the freeze kill switch.
 */

function hashRequest(payload: unknown): string {
  return createHash('sha256').update(JSON.stringify(payload)).digest('hex');
}

/** Resolve + authorise the :did param. Sends the error reply + returns null on failure. */
async function loadOwned(req: FastifyRequest, reply: FastifyReply): Promise<AgentPrincipalRow | null> {
  const { did } = req.params as { did: string };
  if (!parseDid(did)) {
    void reply.code(400).send({ error: 'malformed_did' });
    return null;
  }
  const principal = await findPrincipal(did);
  if (!principal) {
    void reply.code(404).send({ error: 'not_found', message: 'no such agent' });
    return null;
  }
  if (!ownsPrincipal(req, principal)) {
    void reply.code(403).send({ error: 'forbidden', message: 'you do not own this agent' });
    return null;
  }
  return principal;
}

/** Sign + broadcast a transfer from an owned wallet row (owner privileged path). */
async function sendFromWallet(
  wallet: WalletRow,
  args: { to: `0x${string}`; value?: bigint; data?: Hex },
): Promise<Hex> {
  return withWalletLock(wallet.address, async () => {
    const account = await getSigningAccountForRow(wallet);
    const client = createWalletClient({
      chain: hyperPaxeer,
      transport: http(env.HYPERPAXEER_RPC_URL),
      account: {
        address: account.address,
        type: 'local',
        source: 'paxeer-embedded',
        publicKey: '0x' as Hex,
        signTransaction: account.signTransaction,
        signMessage: account.signMessage,
        signTypedData: account.signTypedData,
      },
    });
    return client.sendTransaction({ to: args.to, value: args.value, data: args.data, chain: hyperPaxeer });
  });
}

function validateRuleValue(subject: string, value: string): string | null {
  const v = value.toLowerCase();
  if (subject === 'selector') return /^0x[0-9a-f]{8}$/.test(v) ? v : null;
  return /^0x[0-9a-f]{40}$/.test(v) ? v : null;
}

export async function ownerRoutes(app: FastifyInstance): Promise<void> {
  // ── Inventory ───────────────────────────────────────────────────────────────

  app.get('/v1/agents', { preHandler: requireAuth }, async (req, reply) => {
    const principals = await listPrincipalsByOwner(req.user!.id);
    const agents = await Promise.all(
      principals.map(async (p) => {
        const [wallet, policy] = await Promise.all([
          findWalletByUserId(agentWalletUserId(p.did), 'agent'),
          getPolicy(p.did),
        ]);
        const eff = effectivePolicy(policy);
        return {
          did: p.did,
          label: p.label,
          is_frozen: p.is_frozen,
          mode: eff.mode,
          address: wallet?.address ?? null,
          created_at: p.created_at,
          last_seen_at: p.last_seen_at,
        };
      }),
    );
    return reply.send({ agents });
  });

  app.get('/v1/agents/:did', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const [policy, rules, budgets, wallet] = await Promise.all([
      getPolicy(principal.did),
      listRules(principal.did),
      listActiveBudgets(principal.did),
      findWalletByUserId(agentWalletUserId(principal.did), 'agent'),
    ]);
    const eff = effectivePolicy(policy);
    const native = wallet ? await getNativeBalance(wallet.address).catch(() => null) : null;
    return reply.send({
      did: principal.did,
      label: principal.label,
      owner_user_id: principal.owner_user_id,
      is_frozen: principal.is_frozen,
      created_at: principal.created_at,
      last_seen_at: principal.last_seen_at,
      wallet: wallet
        ? { id: wallet.id, address: wallet.address, native_wei: native !== null ? native.toString() : null }
        : null,
      policy: {
        mode: eff.mode,
        max_tx_value_wei: eff.maxTxValueWei.toString(),
        max_daily_value_wei: eff.maxDailyValueWei.toString(),
        rate_limit_per_min: eff.rateLimitPerMin,
        max_approve_wei: eff.maxApproveWei.toString(),
        allow_native_transfer: eff.allowNativeTransfer,
        withdrawal_allowlist_only: eff.withdrawalAllowlistOnly,
        daily_reset_utc_hour: eff.dailyResetUtcHour,
      },
      rules: rules.map((r) => ({
        id: r.id,
        effect: r.effect,
        subject: r.subject,
        value: r.value,
        max_value_wei: r.max_value_wei !== null ? r.max_value_wei.toString() : null,
        note: r.note,
      })),
      budgets: budgets.map((b) => ({
        id: b.id,
        target_contract: b.target_contract,
        token: b.token,
        cap_wei: b.cap_wei.toString(),
        spent_wei: b.spent_wei.toString(),
        remaining_wei: (b.cap_wei - b.spent_wei).toString(),
        expires_at: b.expires_at,
      })),
    });
  });

  // ── Policy ──────────────────────────────────────────────────────────────────

  app.put('/v1/agents/:did/policy', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const parsed = PolicyPatchBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const p = parsed.data;

    const patch: PolicyPatch = {};
    if (p.mode !== undefined) patch.mode = p.mode;
    if (p.max_tx_value_wei !== undefined)
      patch.max_tx_value_wei = p.max_tx_value_wei === null ? null : BigInt(p.max_tx_value_wei);
    if (p.max_daily_value_wei !== undefined)
      patch.max_daily_value_wei = p.max_daily_value_wei === null ? null : BigInt(p.max_daily_value_wei);
    if (p.rate_limit_per_min !== undefined) patch.rate_limit_per_min = p.rate_limit_per_min;
    if (p.max_approve_wei !== undefined)
      patch.max_approve_wei = p.max_approve_wei === null ? null : BigInt(p.max_approve_wei);
    if (p.allow_native_transfer !== undefined) patch.allow_native_transfer = p.allow_native_transfer;
    if (p.withdrawal_allowlist_only !== undefined)
      patch.withdrawal_allowlist_only = p.withdrawal_allowlist_only;
    if (p.daily_reset_utc_hour !== undefined) patch.daily_reset_utc_hour = p.daily_reset_utc_hour;

    const updated = await upsertPolicy(principal.did, patch, req.user!.id);
    const eff = effectivePolicy(updated);
    return reply.send({
      did: principal.did,
      policy: {
        mode: eff.mode,
        max_tx_value_wei: eff.maxTxValueWei.toString(),
        max_daily_value_wei: eff.maxDailyValueWei.toString(),
        rate_limit_per_min: eff.rateLimitPerMin,
        max_approve_wei: eff.maxApproveWei.toString(),
        allow_native_transfer: eff.allowNativeTransfer,
        withdrawal_allowlist_only: eff.withdrawalAllowlistOnly,
        daily_reset_utc_hour: eff.dailyResetUtcHour,
      },
    });
  });

  // ── Kill switch ───────────────────────────────────────────────────────────

  app.post('/v1/agents/:did/freeze', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    await setPrincipalFrozen(principal.did, true);
    return reply.send({ did: principal.did, is_frozen: true });
  });

  app.post('/v1/agents/:did/unfreeze', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    await setPrincipalFrozen(principal.did, false);
    return reply.send({ did: principal.did, is_frozen: false });
  });

  // Claim an UNOWNED principal (e.g. a DID whose label wasn't a UUID).
  app.post('/v1/agents/:did/claim', { preHandler: requireAuth }, async (req, reply) => {
    const { did } = req.params as { did: string };
    if (!parseDid(did)) return reply.code(400).send({ error: 'malformed_did' });
    const principal = await findPrincipal(did);
    if (!principal) return reply.code(404).send({ error: 'not_found' });
    if (principal.owner_user_id && principal.owner_user_id !== req.user!.id) {
      return reply.code(409).send({ error: 'already_owned', message: 'agent is owned by another user' });
    }
    const ok = await claimPrincipal(did, req.user!.id);
    return reply.code(ok ? 200 : 409).send({ did, owner_user_id: ok ? req.user!.id : principal.owner_user_id });
  });

  // ── Rules ─────────────────────────────────────────────────────────────────

  app.get('/v1/agents/:did/rules', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const rules = await listRules(principal.did);
    return reply.send({
      did: principal.did,
      rules: rules.map((r) => ({
        id: r.id,
        effect: r.effect,
        subject: r.subject,
        value: r.value,
        max_value_wei: r.max_value_wei !== null ? r.max_value_wei.toString() : null,
        note: r.note,
      })),
    });
  });

  app.post('/v1/agents/:did/rules', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const parsed = RuleBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const value = validateRuleValue(parsed.data.subject, parsed.data.value);
    if (!value) {
      return reply
        .code(400)
        .send({ error: 'invalid_rule_value', message: `value must be a ${parsed.data.subject === 'selector' ? '4-byte selector' : '20-byte address'}` });
    }
    const rule = await addRule({
      did: principal.did,
      effect: parsed.data.effect,
      subject: parsed.data.subject,
      value,
      maxValueWei: parsed.data.max_value_wei != null ? BigInt(parsed.data.max_value_wei) : null,
      note: parsed.data.note ?? null,
      createdBy: req.user!.id,
    });
    return reply.code(201).send({
      rule: {
        id: rule.id,
        effect: rule.effect,
        subject: rule.subject,
        value: rule.value,
        max_value_wei: rule.max_value_wei !== null ? rule.max_value_wei.toString() : null,
        note: rule.note,
      },
    });
  });

  app.delete('/v1/agents/:did/rules/:ruleId', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const { ruleId } = req.params as { ruleId: string };
    if (!/^\d+$/.test(ruleId)) return reply.code(400).send({ error: 'invalid_rule_id' });
    const ok = await deleteRule(principal.did, ruleId);
    return reply.code(ok ? 200 : 404).send({ deleted: ok });
  });

  // ── Budgets ─────────────────────────────────────────────────────────────────

  app.get('/v1/agents/:did/budgets', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const budgets = await listActiveBudgets(principal.did);
    return reply.send({
      did: principal.did,
      budgets: budgets.map((b) => ({
        id: b.id,
        target_contract: b.target_contract,
        token: b.token,
        cap_wei: b.cap_wei.toString(),
        spent_wei: b.spent_wei.toString(),
        remaining_wei: (b.cap_wei - b.spent_wei).toString(),
        expires_at: b.expires_at,
      })),
    });
  });

  app.post('/v1/agents/:did/budgets', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const parsed = BudgetBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const expiresAt = new Date(Date.now() + parsed.data.expires_in_seconds * 1000);
    const budget = await addBudget({
      did: principal.did,
      targetContract: parsed.data.target_contract ?? null,
      token: parsed.data.token ?? null,
      capWei: BigInt(parsed.data.cap_wei),
      expiresAt,
      createdBy: req.user!.id,
    });
    return reply.code(201).send({
      budget: {
        id: budget.id,
        target_contract: budget.target_contract,
        token: budget.token,
        cap_wei: budget.cap_wei.toString(),
        expires_at: budget.expires_at,
      },
    });
  });

  app.delete('/v1/agents/:did/budgets/:budgetId', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const { budgetId } = req.params as { budgetId: string };
    if (!/^\d+$/.test(budgetId)) return reply.code(400).send({ error: 'invalid_budget_id' });
    const ok = await deactivateBudget(principal.did, budgetId);
    return reply.code(ok ? 200 : 404).send({ deactivated: ok });
  });

  // ── Treasury: fund / sweep the agent wallet ─────────────────────────────────

  app.post('/v1/agents/:did/fund', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const parsed = FundAgentBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });

    const ownerWallet = await findWalletByUserId(req.user!.id, 'standard');
    if (!ownerWallet) {
      return reply.code(400).send({ error: 'no_owner_wallet', message: 'provision your standard wallet first' });
    }
    const agentWallet = await findWalletByUserId(agentWalletUserId(principal.did), 'agent');
    if (!agentWallet) {
      return reply.code(400).send({ error: 'agent_wallet_missing', message: 'agent has not provisioned a wallet yet' });
    }
    const amount = BigInt(parsed.data.amount);

    let txHash: Hex;
    try {
      if (!parsed.data.token) {
        txHash = await sendFromWallet(ownerWallet, { to: agentWallet.address, value: amount });
      } else {
        const data = encodeErc20Transfer(agentWallet.address, amount);
        txHash = await sendFromWallet(ownerWallet, { to: parsed.data.token as `0x${string}`, value: 0n, data });
      }
    } catch (err) {
      req.log.error({ err }, 'fund agent failed');
      return reply.code(502).send({ error: 'send_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: req.user!.id,
      wallet_id: ownerWallet.id,
      address: ownerWallet.address,
      kind: 'transaction',
      to_address: parsed.data.token ?? agentWallet.address,
      value_wei: parsed.data.token ? 0n : amount,
      chain_id: env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ fund: parsed.data, did: principal.did }),
      tx_hash: txHash,
      ip: req.ip,
      user_agent: (req.headers['user-agent'] as string | undefined) ?? null,
    });

    return reply.send({ tx_hash: txHash, from: ownerWallet.address, to: agentWallet.address });
  });

  app.post('/v1/agents/:did/sweep', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const parsed = SweepBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });

    const agentWallet = await findWalletByUserId(agentWalletUserId(principal.did), 'agent');
    if (!agentWallet) {
      return reply.code(400).send({ error: 'agent_wallet_missing', message: 'agent has no wallet to sweep' });
    }

    // Destination defaults to the owner's standard wallet.
    let dest = parsed.data.to as `0x${string}` | undefined;
    if (!dest) {
      const ownerWallet = await findWalletByUserId(req.user!.id, 'standard');
      if (!ownerWallet) {
        return reply
          .code(400)
          .send({ error: 'no_destination', message: 'provide `to` or provision your standard wallet' });
      }
      dest = ownerWallet.address;
    }
    const amount = BigInt(parsed.data.amount);

    let txHash: Hex;
    try {
      if (!parsed.data.token) {
        txHash = await sendFromWallet(agentWallet, { to: dest, value: amount });
      } else {
        const data = encodeErc20Transfer(dest, amount);
        txHash = await sendFromWallet(agentWallet, { to: parsed.data.token as `0x${string}`, value: 0n, data });
      }
    } catch (err) {
      req.log.error({ err }, 'sweep agent failed');
      return reply.code(502).send({ error: 'send_failed', detail: (err as Error).message });
    }

    // Owner-privileged sweep: tag with principal_did for the agent's audit trail.
    await logSignature({
      user_id: agentWallet.user_id,
      wallet_id: agentWallet.id,
      address: agentWallet.address,
      kind: 'transaction',
      to_address: parsed.data.token ?? dest,
      value_wei: parsed.data.token ? 0n : amount,
      chain_id: env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ sweep: parsed.data, did: principal.did }),
      tx_hash: txHash,
      ip: req.ip,
      user_agent: (req.headers['user-agent'] as string | undefined) ?? null,
      principal_did: principal.did,
    });

    return reply.send({ tx_hash: txHash, from: agentWallet.address, to: dest });
  });

  // ── Activity ─────────────────────────────────────────────────────────────────

  app.get('/v1/agents/:did/activity', { preHandler: requireAuth }, async (req, reply) => {
    const principal = await loadOwned(req, reply);
    if (!principal) return;
    const { rows } = await query<{
      kind: string;
      to_address: string | null;
      value_wei: string;
      tx_hash: string | null;
      created_at: string;
    }>(
      `select kind, to_address, value_wei::text, tx_hash, created_at
         from wallet_signatures
        where principal_did = $1
        order by created_at desc
        limit 100`,
      [principal.did],
    );
    return reply.send({ did: principal.did, events: rows });
  });
}
