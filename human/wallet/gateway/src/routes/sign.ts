import type { FastifyInstance } from 'fastify';
import { createHash } from 'node:crypto';
import { createWalletClient, http, type Hex, type TransactionSerializable } from 'viem';
import { requireAuth } from '../middleware/auth.js';
import {
  findWalletByUserId,
  getSigningAccountForUser,
  logSignature,
} from '../db/wallets.js';
import { evaluate } from '../policy/index.js';
import { hyperPaxeer } from '../chain.js';
import { resolveGas } from '../chainReads.js';
import { env } from '../env.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  SignTxBody,
  SendTxBody,
  SignMessageBody,
  type TxRequest,
} from '../schemas/tx.js';

// Body schemas are defined in `src/schemas/tx.ts` so both /v1/wallet/* and
// /v1/funded/* share the same hardened validators (length caps, strict
// envelopes, charsets). Update there to update both routes simultaneously.

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

function hashRequest(payload: unknown): string {
  return createHash('sha256').update(JSON.stringify(payload)).digest('hex');
}

function clientIp(headers: Record<string, string | string[] | undefined>, fallback: string | null): string | null {
  const xff = headers['x-forwarded-for'];
  if (typeof xff === 'string' && xff.length > 0) {
    return xff.split(',')[0]?.trim() ?? fallback;
  }
  return fallback;
}

// -----------------------------------------------------------------------------
// Routes
// -----------------------------------------------------------------------------

export async function signRoutes(app: FastifyInstance): Promise<void> {
  /**
   * POST /v1/wallet/sign
   * Sign an EVM transaction (does NOT broadcast). Returns the serialized,
   * signed tx hex which the caller can broadcast wherever they like.
   */
  app.post('/v1/wallet/sign', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;

    const decision = await evaluate({ user_id: userId, value_wei: valueWei });
    if (!decision.allow) {
      return reply.code(403).send({ error: decision.code, message: decision.message });
    }

    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }

    let signed: Hex;
    try {
      const account = await getSigningAccountForUser(userId);
      const txParams = buildTxParams(tx);
      // Sign-only never round-trips the RPC inside viem, so pull the gas limit
      // + EIP-1559 fees from the chain per transaction (caller values win).
      const gasParams = await resolveGas({
        from: account.address,
        to: txParams.to,
        data: txParams.data,
        value: txParams.value,
        gas: txParams.gas,
        maxFeePerGas: txParams.maxFeePerGas,
        maxPriorityFeePerGas: txParams.maxPriorityFeePerGas,
      });
      signed = await account.signTransaction({
        ...txParams,
        ...gasParams,
        chainId: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      } as TransactionSerializable);
    } catch (err) {
      req.log.error({ err, userId }, 'sign_transaction failed');
      return reply.code(500).send({ error: 'sign_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'transaction',
      to_address: tx.to ?? null,
      value_wei: valueWei,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ tx }),
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({
      signed_tx: signed,
      address: wallet.address,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
    });
  });

  /**
   * POST /v1/wallet/send
   * Sign + broadcast in one call. Returns the tx hash from the RPC.
   */
  app.post('/v1/wallet/send', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SendTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;

    const decision = await evaluate({ user_id: userId, value_wei: valueWei });
    if (!decision.allow) {
      return reply.code(403).send({ error: decision.code, message: decision.message });
    }

    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }

    let txHash: Hex;
    try {
      // Serialise sends per wallet address: viem fetches the nonce inside
      // sendTransaction, so two concurrent calls for the same wallet would
      // both grab nonce N and one would silently drop. See util/walletLock.ts.
      txHash = await withWalletLock(wallet.address, async () => {
        const account = await getSigningAccountForUser(userId);
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
        const params = buildTxParams(tx);
        const gasParams = await resolveGas({
          from: account.address,
          to: params.to,
          data: params.data,
          value: params.value,
          gas: params.gas,
          maxFeePerGas: params.maxFeePerGas,
          maxPriorityFeePerGas: params.maxPriorityFeePerGas,
        });
        return client.sendTransaction({
          ...params,
          ...gasParams,
          chain: hyperPaxeer,
        });
      });
    } catch (err) {
      req.log.error({ err, userId }, 'send_transaction failed');
      return reply.code(500).send({ error: 'send_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'transaction',
      to_address: tx.to ?? null,
      value_wei: valueWei,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ tx }),
      tx_hash: txHash,
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({
      tx_hash: txHash,
      address: wallet.address,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
    });
  });

  /**
   * POST /v1/wallet/sign-message
   * EIP-191 personal_sign over an arbitrary message string. Useful for SIWE
   * and login-with-wallet flows on non-Paxeer dapps.
   */
  app.post('/v1/wallet/sign-message', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignMessageBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;

    const decision = await evaluate({ user_id: userId, value_wei: 0n });
    if (!decision.allow) {
      return reply.code(403).send({ error: decision.code, message: decision.message });
    }

    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }

    let signature: Hex;
    try {
      const account = await getSigningAccountForUser(userId);
      signature = await account.signMessage({ message: parsed.data.message });
    } catch (err) {
      req.log.error({ err, userId }, 'sign_message failed');
      return reply.code(500).send({ error: 'sign_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'message',
      request_hash: hashRequest({ message: parsed.data.message }),
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({ signature, address: wallet.address });
  });
}

// -----------------------------------------------------------------------------
// Internal — convert wire-format tx (strings, optional fields) to viem shape.
// -----------------------------------------------------------------------------

function buildTxParams(tx: TxRequest): {
  to?: `0x${string}`;
  value?: bigint;
  data?: `0x${string}`;
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
  nonce?: number;
} {
  const out: ReturnType<typeof buildTxParams> = {};
  if (tx.to) out.to = tx.to as `0x${string}`;
  if (tx.value !== undefined) out.value = BigInt(tx.value);
  if (tx.data) out.data = tx.data as `0x${string}`;
  if (tx.gas !== undefined) out.gas = BigInt(tx.gas);
  if (tx.maxFeePerGas !== undefined) out.maxFeePerGas = BigInt(tx.maxFeePerGas);
  if (tx.maxPriorityFeePerGas !== undefined)
    out.maxPriorityFeePerGas = BigInt(tx.maxPriorityFeePerGas);
  if (tx.nonce !== undefined) out.nonce = tx.nonce;
  return out;
}
