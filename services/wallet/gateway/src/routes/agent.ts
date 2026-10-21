import type { FastifyInstance, FastifyRequest } from 'fastify';
import { createHash } from 'node:crypto';
import type { Hex } from 'viem';
import { parseAgentReauthorization } from '../attestor/client.js';
import { verifyEd25519 } from '../auth/did.js';
import { retainAgentAuthorization } from '../db/authorizations.js';
import { requireAgent } from '../middleware/principal.js';
import { agentRequestDigest, requireSignedAgentRequest } from '../agent/verify.js';
import { WalletArchivedError, archivedWalletGuard } from '../db/wallets.js';
import { env } from '../env.js';
import { query } from '../db/pool.js';
import { getPolicy } from '../db/agents.js';
import { effectivePolicy } from '../policy/agent.js';
import {
  encodeErc20Approve,
  encodeErc20Transfer,
  getErc20Allowance,
  getErc20Balance,
  getErc20Metadata,
  getNativeBalance,
  getNonce,
  getReceipt,
  simulate,
} from '../chainReads.js';
import {
  AgentApproveBody,
  AgentSendTxBody,
  AgentSignMessageBody,
  AgentSignTxBody,
  AgentSignTypedDataBody,
  AgentSimulateBody,
  AgentTransferBody,
} from '../schemas/agent.js';
import {
  ensureAgentWallet,
  custodyRefusal,
  executeAgentMessage,
  executeAgentTransaction,
  executeAgentTypedData,
  getAgentWallet,
  type AgentTxInput,
} from './agentExec.js';
import { provisionAccount, ProvisionError } from '../provision/state.js';
import { provisionDepsFromEnv } from './wallet.js';
import type { AgentTxIntent } from '../policy/agent.js';

function hashRequest(payload: unknown): string {
  return createHash('sha256').update(JSON.stringify(payload)).digest('hex');
}

function audit(req: FastifyRequest, payload: unknown): { request_hash: string; ip: string | null; user_agent: string | null } {
  return {
    request_hash: hashRequest(payload),
    ip: req.ip,
    user_agent: (req.headers['user-agent'] as string | undefined) ?? null,
  };
}

/** Convert a validated wire tx (string fields) into the bigint AgentTxInput. */
function toAgentTxInput(tx: {
  to?: string;
  value?: string;
  data?: string;
  gas?: string;
  maxFeePerGas?: string;
  maxPriorityFeePerGas?: string;
  nonce?: number;
  chainId?: number;
}): AgentTxInput {
  const out: AgentTxInput = {};
  if (tx.to) out.to = tx.to as `0x${string}`;
  if (tx.value !== undefined) out.value = BigInt(tx.value);
  if (tx.data) out.data = tx.data as Hex;
  if (tx.gas !== undefined) out.gas = BigInt(tx.gas);
  if (tx.maxFeePerGas !== undefined) out.maxFeePerGas = BigInt(tx.maxFeePerGas);
  if (tx.maxPriorityFeePerGas !== undefined) out.maxPriorityFeePerGas = BigInt(tx.maxPriorityFeePerGas);
  if (tx.nonce !== undefined) out.nonce = tx.nonce;
  if (tx.chainId !== undefined) out.chainId = tx.chainId;
  return out;
}

export async function agentRoutes(app: FastifyInstance): Promise<void> {
  app.post('/v1/agent/signing-authorizations', { preHandler: requireSignedAgentRequest, bodyLimit: 131_072 }, async (req, reply) => {
    try {
      const authorization = parseAgentReauthorization(req.body);
      const now = Math.floor(Date.now() / 1000);
      if (authorization.expiry <= now || authorization.expiry - now > env.AGENT_REQUEST_MAX_TTL_SECONDS) {
        return reply.code(400).send({ error: 'agent_attestor_authorization_expired' });
      }
      const wire = JSON.parse(authorization.body) as { key_id?: unknown; origin?: { did?: unknown } };
      const wallet = await getAgentWallet(req.agent!.did);
      if (!wallet || typeof wire.key_id !== 'string' || wire.origin?.did !== req.agent!.did) {
        return reply.code(403).send({ error: 'agent_attestor_authorization_mismatch' });
      }
      const owned = await query('select 1 from wallets where id = $1 and attestor_key_id = $2 and kind = $3 and archived_at is null and is_disabled = false', [wallet.id, wire.key_id, 'agent']);
      if (owned.rows.length !== 1) return reply.code(403).send({ error: 'agent_attestor_authorization_mismatch' });
      const digest = agentRequestDigest({ method: '/v1/sign', keyId: wire.key_id,
        nonce: Buffer.from(authorization.nonce, 'hex'), expiry: BigInt(authorization.expiry),
        body: Buffer.from(authorization.body, 'utf8') });
      if (!verifyEd25519(req.agent!.principal.public_key, digest, authorization.signature)) {
        return reply.code(403).send({ error: 'agent_attestor_authorization_bad_signature' });
      }
      const id = await retainAgentAuthorization(req.agent!.did, authorization);
      return reply.code(201).send({ authorization_id: id, expires_at: authorization.expiry });
    } catch {
      return reply.code(400).send({ error: 'agent_attestor_authorization_invalid' });
    }
  });

  // ── Wallet lifecycle ──────────────────────────────────────────────────────

  app.post('/v1/agent/provision', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    try {
      const deps = provisionDepsFromEnv();
      if (!deps) return reply.code(503).send({ error: 'provisioning_unavailable' });
      const body = req.body as { bind_signature?: unknown } | null;
      if (body?.bind_signature !== undefined && (typeof body.bind_signature !== 'string'
        || !/^(0x)?[0-9a-fA-F]{128}$/.test(body.bind_signature))) {
        return reply.code(400).send({ error: 'invalid_bind_signature' });
      }
      const provision = await provisionAccount(deps, {
        kind: 'agent', did: req.agent!.did, custody: req.agent!.custody,
        agentSignature: body?.bind_signature as string | undefined,
      });
      return reply.code(provision.state === 'active' ? 200 : 202).send({
        wallet: { id: provision.walletId, address: provision.address, chain_id: env.HYPERPAXEER_CHAIN_ID, kind: 'agent' },
        provision,
      });
    } catch (err) {
      const refusal = custodyRefusal(err);
      if (refusal) return reply.code(refusal.status).send(refusal.body);
      if (err instanceof ProvisionError) return reply.code(err.status).send({ error: err.code, message: err.message });
      if (err instanceof WalletArchivedError) return reply.code(err.refusal.status).send(err.refusal.body);
      req.log.error({ err, did: req.agent!.did }, 'agent provision failed');
      return reply.code(500).send({ error: 'provision_failed', detail: (err as Error).message });
    }
  });

  app.get('/v1/agent/me', { preHandler: requireAgent }, async (req, reply) => {
    const agent = req.agent!;
    const wallet = await getAgentWallet(agent.did);
    const eff = effectivePolicy(await getPolicy(agent.did));
    return reply.send({
      did: agent.did,
      owner_user_id: agent.ownerUserId,
      is_frozen: agent.principal.is_frozen,
      wallet: wallet ? { id: wallet.id, address: wallet.address, chain_id: wallet.chain_id } : null,
      policy: {
        mode: eff.mode,
        allow_native_transfer: eff.allowNativeTransfer,
        withdrawal_allowlist_only: eff.withdrawalAllowlistOnly,
        max_tx_value_wei: eff.maxTxValueWei.toString(),
        max_daily_value_wei: eff.maxDailyValueWei.toString(),
        max_approve_wei: eff.maxApproveWei.toString(),
        rate_limit_per_min: eff.rateLimitPerMin,
      },
      chain: { id: env.HYPERPAXEER_CHAIN_ID, rpc_url: env.HYPERPAXEER_RPC_URL },
    });
  });

  // ── Reads ─────────────────────────────────────────────────────────────────

  app.get('/v1/agent/balances', { preHandler: requireAgent }, async (req, reply) => {
    const wallet = await getAgentWallet(req.agent!.did);
    if (!wallet) return reply.code(404).send({ error: 'no_wallet', message: 'provision first' });

    const tokensParam = (req.query as { tokens?: string }).tokens;
    const tokenList = (tokensParam ?? '')
      .split(',')
      .map((t) => t.trim().toLowerCase())
      .filter((t) => /^0x[0-9a-f]{40}$/.test(t));

    const native = await getNativeBalance(wallet.address).catch(() => null);
    const tokens = await Promise.all(
      tokenList.map(async (token) => {
        const [bal, meta] = await Promise.all([
          getErc20Balance(token as `0x${string}`, wallet.address).catch(() => null),
          getErc20Metadata(token as `0x${string}`),
        ]);
        return {
          token,
          symbol: meta.symbol,
          decimals: meta.decimals,
          balance: bal !== null ? bal.toString() : null,
        };
      }),
    );

    return reply.send({
      address: wallet.address,
      native_wei: native !== null ? native.toString() : null,
      tokens,
    });
  });

  app.get('/v1/agent/nonce', { preHandler: requireAgent }, async (req, reply) => {
    const wallet = await getAgentWallet(req.agent!.did);
    if (!wallet) return reply.code(404).send({ error: 'no_wallet', message: 'provision first' });
    const nonce = await getNonce(wallet.address);
    return reply.send({ address: wallet.address, nonce });
  });

  app.post('/v1/agent/simulate', { preHandler: requireAgent }, async (req, reply) => {
    const parsed = AgentSimulateBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const wallet = await ensureAgentWallet(req.agent!);
    const tx = toAgentTxInput(parsed.data.tx);
    const result = await simulate({
      from: wallet.address,
      to: tx.to,
      data: tx.data,
      value: tx.value,
    });
    return reply.send({
      ok: result.ok,
      gas: result.gas !== null ? result.gas.toString() : null,
      return_data: result.returnData,
      error: result.error,
    });
  });

  app.get('/v1/agent/allowance', { preHandler: requireAgent }, async (req, reply) => {
    const q = req.query as { token?: string; spender?: string };
    if (!q.token || !/^0x[0-9a-fA-F]{40}$/.test(q.token) || !q.spender || !/^0x[0-9a-fA-F]{40}$/.test(q.spender)) {
      return reply.code(400).send({ error: 'invalid_query', message: 'token and spender must be addresses' });
    }
    const wallet = await getAgentWallet(req.agent!.did);
    if (!wallet) return reply.code(404).send({ error: 'no_wallet', message: 'provision first' });
    const allowance = await getErc20Allowance(
      q.token as `0x${string}`,
      wallet.address,
      q.spender as `0x${string}`,
    );
    return reply.send({ token: q.token.toLowerCase(), spender: q.spender.toLowerCase(), allowance: allowance.toString() });
  });

  app.get('/v1/agent/tx/:hash', { preHandler: requireAgent }, async (req, reply) => {
    const { hash } = req.params as { hash: string };
    if (!/^0x[0-9a-fA-F]{64}$/.test(hash)) {
      return reply.code(400).send({ error: 'invalid_hash' });
    }
    const receipt = await getReceipt(hash as Hex);
    if (!receipt) return reply.code(404).send({ error: 'not_found', message: 'receipt not available yet' });
    return reply.send(receipt);
  });

  app.get('/v1/agent/activity', { preHandler: requireAgent }, async (req, reply) => {
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
        limit 50`,
      [req.agent!.did],
    );
    return reply.send({ did: req.agent!.did, events: rows });
  });

  // ── Signing (policy-gated) ──────────────────────────────────────────────────

  app.post('/v1/agent/sign', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentSignTxBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    if (parsed.data.tx.to) {
      const refusal = await archivedWalletGuard({ address: parsed.data.tx.to });
      if (refusal) return reply.code(refusal.status).send(refusal.body);
    }
    const tx = toAgentTxInput(parsed.data.tx);
    const intent: AgentTxIntent = { kind: 'transaction', to: tx.to, value: tx.value, data: tx.data };
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx,
      broadcast: false,
      audit: audit(req, { tx: parsed.data.tx }),
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/send', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentSendTxBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    if (parsed.data.tx.to) {
      const refusal = await archivedWalletGuard({ address: parsed.data.tx.to });
      if (refusal) return reply.code(refusal.status).send(refusal.body);
    }
    const tx = toAgentTxInput(parsed.data.tx);
    const intent: AgentTxIntent = { kind: 'transaction', to: tx.to, value: tx.value, data: tx.data };
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx,
      broadcast: true,
      audit: audit(req, { tx: parsed.data.tx }),
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/sign-message', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentSignMessageBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const res = await executeAgentMessage({
      ctx: req.agent!,
      message: parsed.data.message,
      audit: audit(req, { message: parsed.data.message }),
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/sign-typed-data', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentSignTypedDataBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const res = await executeAgentTypedData({
      ctx: req.agent!,
      typedData: parsed.data.typedData as Record<string, unknown>,
      audit: audit(req, { typedData: parsed.data.typedData }),
    });
    return reply.code(res.status).send(res.body);
  });

  // ── High-level token ops (structured policy intent) ─────────────────────────

  app.post('/v1/agent/transfer', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentTransferBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const { token, to, amount } = parsed.data;
    const transferRefusal = await archivedWalletGuard({ address: to });
    if (transferRefusal) return reply.code(transferRefusal.status).send(transferRefusal.body);
    const amountWei = BigInt(amount);

    let tx: AgentTxInput;
    let intent: AgentTxIntent;
    if (!token) {
      // Native PAX transfer.
      tx = { to: to as `0x${string}`, value: amountWei };
      intent = { kind: 'transaction', to, value: amountWei };
    } else {
      // ERC-20 transfer.
      const data = encodeErc20Transfer(to as `0x${string}`, amountWei);
      tx = { to: token as `0x${string}`, value: 0n, data };
      intent = {
        kind: 'transaction',
        to: token,
        value: 0n,
        data,
        tokenContract: token,
        tokenRecipient: to,
        tokenAmount: amountWei,
      };
    }

    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx,
      broadcast: true,
      audit: audit(req, { transfer: parsed.data }),
      label: token ? 'erc20.transfer' : 'native.transfer',
    });
    return reply.code(res.status).send(res.body);
  });

  app.post('/v1/agent/approve', { preHandler: requireSignedAgentRequest }, async (req, reply) => {
    const parsed = AgentApproveBody.safeParse(req.body);
    if (!parsed.success) return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    const { token, spender, amount } = parsed.data;
    const approveRefusal = await archivedWalletGuard({ address: spender });
    if (approveRefusal) return reply.code(approveRefusal.status).send(approveRefusal.body);
    const amountWei = BigInt(amount);
    const data = encodeErc20Approve(spender as `0x${string}`, amountWei);
    const tx: AgentTxInput = { to: token as `0x${string}`, value: 0n, data };
    const intent: AgentTxIntent = {
      kind: 'transaction',
      to: token,
      value: 0n,
      data,
      tokenContract: token,
      tokenRecipient: spender,
      tokenAmount: amountWei,
      isApprove: true,
    };
    const res = await executeAgentTransaction({
      ctx: req.agent!,
      intent,
      tx,
      broadcast: true,
      audit: audit(req, { approve: parsed.data }),
      label: 'erc20.approve',
    });
    return reply.code(res.status).send(res.body);
  });
}
