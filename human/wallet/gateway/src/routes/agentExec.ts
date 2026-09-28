import type { FastifyRequest } from 'fastify';
import { createWalletClient, http, type Hex, type TransactionSerializable } from 'viem';
import { hyperPaxeer } from '../chain.js';
import { env } from '../env.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  agentWalletUserId,
  findWalletByUserId,
  getSigningAccountForRow,
  logSignature,
  provisionWalletForUser,
  type WalletRow,
} from '../db/wallets.js';
import { getPolicy, listRules, releaseBudget, setPrincipalWallet } from '../db/agents.js';
import { evaluateAgent, type AgentTxIntent } from '../policy/agent.js';
import { getNonceGap, resolveGas } from '../chainReads.js';

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

/** Find-or-create the agent's dedicated kind='agent' wallet and link it. */
export async function ensureAgentWallet(ctx: AgentCtx): Promise<WalletRow> {
  const userId = agentWalletUserId(ctx.did);
  const { row } = await provisionWalletForUser(userId, 'agent');
  if (!ctx.principal.wallet_id) {
    await setPrincipalWallet(ctx.did, row.id).catch(() => undefined);
  }
  return row;
}

/** Read the agent's wallet without provisioning. */
export async function getAgentWallet(did: string): Promise<WalletRow | null> {
  return findWalletByUserId(agentWalletUserId(did), 'agent');
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

  const [policy, rules] = await Promise.all([getPolicy(ctx.did), listRules(ctx.did)]);
  const decision = await evaluateAgent({ principal: ctx.principal, policy, rules, intent });
  if (!decision.allow) {
    return { status: 403, body: { error: decision.code, message: decision.message, ...decision.detail } };
  }

  const wallet = await ensureAgentWallet(ctx);
  const chainId = tx.chainId ?? env.HYPERPAXEER_CHAIN_ID;
  const valueWei = tx.value ?? 0n;

  let txHash: Hex | null = null;
  let signedTx: Hex | null = null;
  try {
    if (broadcast) {
      txHash = await withWalletLock(wallet.address, async () => {
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
        const params = viemTxParams(tx);
        const gasParams = await resolveGas({
          from: account.address,
          to: params.to,
          data: params.data,
          value: params.value,
          gas: params.gas,
          maxFeePerGas: params.maxFeePerGas,
          maxPriorityFeePerGas: params.maxPriorityFeePerGas,
        });
        return client.sendTransaction({ ...params, ...gasParams, chain: hyperPaxeer });
      });
    } else {
      const account = await getSigningAccountForRow(wallet);
      const params = viemTxParams(tx);
      const gasParams = await resolveGas({
        from: account.address,
        to: params.to,
        data: params.data,
        value: params.value,
        gas: params.gas,
        maxFeePerGas: params.maxFeePerGas,
        maxPriorityFeePerGas: params.maxPriorityFeePerGas,
      });
      signedTx = await account.signTransaction({
        ...params,
        ...gasParams,
        chainId,
      } as TransactionSerializable);
    }
  } catch (err) {
    // The send failed — give back any budget the gate reserved.
    if (decision.reservedBudgetId) {
      await releaseBudget(decision.reservedBudgetId, decision.reservedValueWei);
    }
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
      body: { error: broadcast ? 'send_failed' : 'sign_failed', detail },
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
    const account = await getSigningAccountForRow(wallet);
    signature = await account.signMessage({ message });
  } catch (err) {
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
    const account = await getSigningAccountForRow(wallet);
    // viem validates the typed-data shape; a malformed payload throws here.
    signature = await account.signTypedData(typedData as Parameters<typeof account.signTypedData>[0]);
  } catch (err) {
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
