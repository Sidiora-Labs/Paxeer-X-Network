import type { FastifyRequest } from 'fastify';
import { createWalletClient, http, keccak256, parseTransaction, type Hex, type TransactionSerializable } from 'viem';
import { hyperPaxeer } from '../chain.js';
import { env } from '../env.js';
import { CustodyAuthorityError } from '../agent/authority.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  agentWalletUserId,
  findWalletByUserId,
  getMigrationAwareSigningAccountForRow,
  logSignature,
  type WalletRow,
} from '../db/wallets.js';
import { bindCustodyBudgetReservation, getPolicy, listRules, releaseBudget, releaseCustodyBudgetReservation } from '../db/agents.js';
import { evaluateAgent, type AgentTxIntent } from '../policy/agent.js';
import { AttestorError, AttestorTokenError, AttestorReauthorizationRequired, attestorErrorBody, attestorErrorStatus } from '../attestor/client.js';
import { getNonce, getNonceGap, resolveGas } from '../chainReads.js';

/**
 * Shared agent execution core.
 *
 * Every state-changing agent action funnels through here so the policy gate,
 * audit log, wallet-lock nonce serialisation, and budget-release-on-failure are
 * implemented EXACTLY once and reused by both the capability routes
 * (routes/agent.ts) and the precompile routes (routes/agentPrecompiles.ts).
 */

export type AgentCtx = NonNullable<FastifyRequest['agent']>;

export interface AgentTxInput {
  to?: `0x${string}`;
  value?: bigint;
  data?: Hex;
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
  nonce?: number;
  chainId?: number;
}

export interface AuditMeta {
  request_hash: string;
  ip: string | null;
  user_agent: string | null;
}

export interface ExecResult {
  status: number;
  body: Record<string, unknown>;
}

/** Resolve an already provisioned dedicated agent wallet. */
export async function ensureAgentWallet(ctx: AgentCtx): Promise<WalletRow> {
  const userId = agentWalletUserId(ctx.did);
  const row = await findWalletByUserId(userId, 'agent');
  if (!row) throw new Error('agent_wallet_not_provisioned');
  return row;
}

/** Read the agent's wallet without provisioning. */
export async function getAgentWallet(did: string): Promise<WalletRow | null> {
  return findWalletByUserId(agentWalletUserId(did), 'agent');
}

export function custodyRefusal(error: unknown): ExecResult | null {
  if (error instanceof CustodyAuthorityError) {
    return { status: 503, body: { error: error.code, replication_pending: true, message: error.message } };
  }
  if (error instanceof AttestorReauthorizationRequired) {
    return { status: 409, body: { error: error.code, method: '/v1/sign', key_id: error.keyId,
      signing_request: error.request, digest_domain: 'PXW:AGENT-REQUEST:v1',
      authorization_endpoint: '/v1/agent/signing-authorizations',
      authorization_id_header: 'x-agent-attestor-authorization-id' } };
  }
  return error instanceof AttestorError
    ? { status: attestorErrorStatus(error), body: attestorErrorBody(error) } : null;
}

function viemTxParams(tx: AgentTxInput): {
  to?: `0x${string}`;
  value?: bigint;
  data?: Hex;
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
  nonce?: number;
} {
  const out: ReturnType<typeof viemTxParams> = {};
  if (tx.to) out.to = tx.to;
  if (tx.value !== undefined) out.value = tx.value;
  if (tx.data) out.data = tx.data;
  if (tx.gas !== undefined) out.gas = tx.gas;
  if (tx.maxFeePerGas !== undefined) out.maxFeePerGas = tx.maxFeePerGas;
  if (tx.maxPriorityFeePerGas !== undefined) out.maxPriorityFeePerGas = tx.maxPriorityFeePerGas;
  if (tx.nonce !== undefined) out.nonce = tx.nonce;
  return out;
}

async function transactionForRequest(ctx: AgentCtx, wallet: WalletRow, tx: AgentTxInput): Promise<TransactionSerializable> {
  const params = viemTxParams(tx);
  const chainId = tx.chainId ?? env.HYPERPAXEER_CHAIN_ID;
  if (chainId !== wallet.chain_id || chainId !== env.HYPERPAXEER_CHAIN_ID) {
    throw new AttestorTokenError('agent_authorization_mismatch', 'transaction chain differs from the wallet');
  }
  const expectedNonce = tx.nonce ?? await getNonce(wallet.address);
  const approved = ctx.custody?.reauthorization;
  if (approved) {
    let offered: ReturnType<typeof parseTransaction>;
    try {
      const wire = JSON.parse(approved.body) as { kind?: unknown; transaction?: unknown };
      if (wire.kind !== 'evm_tx' || typeof wire.transaction !== 'string' || !/^[0-9a-f]+$/.test(wire.transaction)) throw new Error();
      offered = parseTransaction(`0x${wire.transaction}`);
    } catch {
      throw new AttestorTokenError('agent_authorization_mismatch', 'the approved envelope is not an unsigned EIP-1559 transaction');
    }
    if (offered.type !== 'eip1559' || offered.chainId !== chainId || offered.nonce !== expectedNonce
      || (offered.to?.toLowerCase() ?? null) !== (params.to?.toLowerCase() ?? null)
      || (offered.value ?? 0n) !== (params.value ?? 0n)
      || (offered.data?.toLowerCase() ?? '0x') !== (params.data?.toLowerCase() ?? '0x')
      || offered.gas === undefined || offered.maxFeePerGas === undefined || offered.maxPriorityFeePerGas === undefined
      || (params.gas !== undefined && offered.gas !== params.gas)
      || (params.maxFeePerGas !== undefined && offered.maxFeePerGas !== params.maxFeePerGas)
      || (params.maxPriorityFeePerGas !== undefined && offered.maxPriorityFeePerGas !== params.maxPriorityFeePerGas)
      || (offered.accessList?.length ?? 0) !== 0 || offered.r !== undefined || offered.s !== undefined) {
      throw new AttestorTokenError('agent_authorization_mismatch', 'the approved transaction differs from the requested operation or current nonce');
    }
    return { type: 'eip1559', chainId, nonce: expectedNonce, to: params.to, value: params.value ?? 0n,
      data: params.data, gas: offered.gas, maxFeePerGas: offered.maxFeePerGas,
      maxPriorityFeePerGas: offered.maxPriorityFeePerGas };
  }
  const gasParams = await resolveGas({ from: wallet.address, ...params });
  return { ...params, ...gasParams, type: 'eip1559', chainId, nonce: expectedNonce };
}

/**
 * Policy-gate, then sign (and optionally broadcast) a transaction from the
 * agent's wallet. Releases any budget reserved by the policy gate if the send
 * itself fails, so a failed broadcast never burns an owner's grant.
 */
export async function executeAgentTransaction(args: {
  ctx: AgentCtx;
  intent: AgentTxIntent;
  tx: AgentTxInput;
  broadcast: boolean;
  audit: AuditMeta;
  /** Human-readable op label for the audit log + response (e.g. 'scheduler.schedule'). */
  label?: string;
}): Promise<ExecResult> {
  const { ctx, intent, tx, broadcast, audit } = args;

  const wallet = await getAgentWallet(ctx.did);
  if (!wallet) return { status: 409, body: { error: 'agent_wallet_not_provisioned' } };
  const [policy, rules] = await Promise.all([getPolicy(ctx.did), listRules(ctx.did)]);
  const decision = await evaluateAgent({ principal: ctx.principal, policy, rules, intent });
  if (!decision.allow) {
    return { status: 403, body: { error: decision.code, message: decision.message, ...decision.detail } };
  }

  const chainId = tx.chainId ?? env.HYPERPAXEER_CHAIN_ID;
  const valueWei = tx.value ?? 0n;

  let txHash: Hex | null = null;
  let signedTx: Hex | null = null;
  let custodyBudgetReservationId: string | null = null;
  try {
    const signed = await withWalletLock(wallet.address, async () => {
      const account = await getMigrationAwareSigningAccountForRow(wallet, ctx.custody ? {
        scheme: 'agent_request', publicKey: ctx.principal.public_key, ...ctx.custody,
      } : undefined, undefined, undefined, {
        subject: ctx.did, route: ctx.custody?.origin.method ?? '/v1/agent', requestHash: audit.request_hash,
      });
      const transaction = await transactionForRequest(ctx, wallet, tx);
      if (decision.reservedBudgetId && ctx.custody) {
        custodyBudgetReservationId = await bindCustodyBudgetReservation({
          did: ctx.did, budgetId: decision.reservedBudgetId, valueWei: decision.reservedValueWei,
          requestNonce: ctx.custody.origin.nonce, transaction,
        });
      }
      const raw = await account.signTransaction(transaction);
      if (!broadcast) return { raw, hash: null };
      signedTx = raw;
      txHash = keccak256(raw);
      const client = createWalletClient({ chain: hyperPaxeer, transport: http(env.HYPERPAXEER_RPC_URL) });
      const hash = await client.sendRawTransaction({ serializedTransaction: raw });
      return { raw, hash };
    });
    txHash = signed.hash;
    signedTx = signed.raw;
  } catch (err) {
    // The send failed — give back any budget the gate reserved.
    if (decision.reservedBudgetId && signedTx === null) {
      if (custodyBudgetReservationId) await releaseCustodyBudgetReservation(custodyBudgetReservationId, ctx.did);
      else await releaseBudget(decision.reservedBudgetId, decision.reservedValueWei);
    }
    const refused = custodyRefusal(err);
    if (refused) return refused;
    // Diagnose the classic silent wedge: an unmined tx in the mempool blocks
    // every later nonce, so estimation runs against stale latest-state (e.g.
    // an allowance that "succeeded" but never mined) and reverts forever.
    // Naming it here turns an opaque send_failed into an actionable error.
    let detail = (err as Error).message;
    if (broadcast) {
      try {
        const gap = await getNonceGap(wallet.address);
        if (gap.stuck > 0) {
          detail +=
            ` | NONCE GAP: ${gap.stuck} unmined tx(s) in the mempool (latest nonce ${gap.latest}, pending ${gap.pending}).` +
            ` This wallet is wedged behind an unmined tx at nonce ${gap.latest}: prior broadcasts (e.g. an approve) have NOT taken effect on-chain,` +
            ` and nothing later can mine until nonce ${gap.latest} is filled or replaced (0-value self-send at that nonce with higher fees).`;
        }
      } catch {
        /* diagnosis is best-effort; never mask the original error */
      }
    }
    return {
      status: 502,
      body: { error: broadcast ? 'send_failed' : 'sign_failed', detail,
        ...(txHash ? { tx_hash: txHash, retry: 'poll_transaction', must_not_resubmit: true } : {}) },
    };
  }

  await logSignature({
    user_id: wallet.user_id,
    wallet_id: wallet.id,
    address: wallet.address,
    kind: 'transaction',
    to_address: tx.to ?? null,
    value_wei: valueWei,
    chain_id: chainId,
    request_hash: audit.request_hash,
    tx_hash: txHash,
    ip: audit.ip,
    user_agent: audit.user_agent,
    principal_did: ctx.did,
  });

  return {
    status: 200,
    body: broadcast
      ? { tx_hash: txHash, address: wallet.address, chain_id: chainId, op: args.label ?? 'send' }
      : { signed_tx: signedTx, address: wallet.address, chain_id: chainId, op: args.label ?? 'sign' },
  };
}

/** Policy-gate + sign an EIP-191 personal message. */
export async function executeAgentMessage(args: {
  ctx: AgentCtx;
  message: string;
  audit: AuditMeta;
}): Promise<ExecResult> {
  const { ctx, message, audit } = args;
  const [policy, rules] = await Promise.all([getPolicy(ctx.did), listRules(ctx.did)]);
  const decision = await evaluateAgent({
    principal: ctx.principal,
    policy,
    rules,
    intent: { kind: 'message' },
  });
  if (!decision.allow) {
    return { status: 403, body: { error: decision.code, message: decision.message } };
  }

  const wallet = await ensureAgentWallet(ctx);
  let signature: Hex;
  try {
    const account = await getMigrationAwareSigningAccountForRow(wallet, ctx.custody ? { scheme: 'agent_request', publicKey: ctx.principal.public_key, ...ctx.custody } : undefined, undefined, undefined, { subject: ctx.did, route: ctx.custody?.origin.method ?? '/v1/agent', requestHash: audit.request_hash });
    signature = await account.signMessage({ message });
  } catch (err) {
    const refused = custodyRefusal(err);
    if (refused) return refused;
    return { status: 502, body: { error: 'sign_failed', detail: (err as Error).message } };
  }

  await logSignature({
    user_id: wallet.user_id,
    wallet_id: wallet.id,
    address: wallet.address,
    kind: 'message',
    request_hash: audit.request_hash,
    ip: audit.ip,
    user_agent: audit.user_agent,
    principal_did: ctx.did,
  });

  return { status: 200, body: { signature, address: wallet.address } };
}

/** Policy-gate + sign EIP-712 typed data. */
export async function executeAgentTypedData(args: {
  ctx: AgentCtx;
  typedData: Record<string, unknown>;
  audit: AuditMeta;
}): Promise<ExecResult> {
  const { ctx, typedData, audit } = args;
  const [policy, rules] = await Promise.all([getPolicy(ctx.did), listRules(ctx.did)]);
  const decision = await evaluateAgent({
    principal: ctx.principal,
    policy,
    rules,
    intent: { kind: 'typed_data' },
  });
  if (!decision.allow) {
    return { status: 403, body: { error: decision.code, message: decision.message } };
  }

  const wallet = await ensureAgentWallet(ctx);
  let signature: Hex;
  try {
    const account = await getMigrationAwareSigningAccountForRow(wallet, ctx.custody ? { scheme: 'agent_request', publicKey: ctx.principal.public_key, ...ctx.custody } : undefined, undefined, undefined, { subject: ctx.did, route: ctx.custody?.origin.method ?? '/v1/agent', requestHash: audit.request_hash });
    // viem validates the typed-data shape; a malformed payload throws here.
    signature = await account.signTypedData(typedData as Parameters<typeof account.signTypedData>[0]);
  } catch (err) {
    const refused = custodyRefusal(err);
    if (refused) return refused;
    return { status: 400, body: { error: 'sign_typed_data_failed', detail: (err as Error).message } };
  }

  await logSignature({
    user_id: wallet.user_id,
    wallet_id: wallet.id,
    address: wallet.address,
    kind: 'typed_data',
    request_hash: audit.request_hash,
    ip: audit.ip,
    user_agent: audit.user_agent,
    principal_did: ctx.did,
  });

  return { status: 200, body: { signature, address: wallet.address } };
}
