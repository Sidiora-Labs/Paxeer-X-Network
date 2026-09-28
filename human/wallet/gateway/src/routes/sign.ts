import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';
import { createHash, randomUUID } from 'node:crypto';
import type { Pool, PoolClient } from 'pg';
import { z } from 'zod';
import type { Hex, TransactionSerializableEIP1559, TypedDataDefinition } from 'viem';
import { requireAuth } from '../middleware/auth.js';
import {
  getSigningAccountForRow,
  loadWalletForSigning,
  logSignature,
  type SignatureLogInput,
  type SigningWallet,
} from '../db/wallets.js';
import { getPool } from '../db/pool.js';
import { evaluate } from '../policy/index.js';
import { env } from '../env.js';
import {
  AttestorError,
  AttestorQuorumError,
  attestorClientFromConfig,
  attestorErrorBody,
  attestorErrorStatus,
  attestorSigner,
  type AttestorClient,
  type NodeAudit,
  type SignResult,
} from '../attestor/client.js';
import { RpcPool, RpcResponseError, sharedRpcPool } from '../rpc/pool.js';
import { NonceStore, sharedNonceStore } from '../nonce/store.js';
import {
  RateLimitedError,
  RateLimiter,
  rateLimitBody,
  recordSigningAudit,
  type AuditKind,
  type AuditPath,
} from '../audit.js';
import { SignTxBody, SendTxBody, SignMessageBody, type TxRequest } from '../schemas/tx.js';

export interface SignRoutesOptions {
  pool?: Pool;
  attestors?: AttestorClient | null;
  rpc?: RpcPool;
  nonces?: NonceStore;
  limiter?: RateLimiter;
}

const TypedDataField = z.object({ name: z.string().min(1).max(256), type: z.string().min(1).max(256) }).strict();

export const SignTypedDataBody = z
  .object({
    typed_data: z
      .object({
        domain: z.record(z.unknown()),
        types: z.record(z.array(TypedDataField).max(256)),
        primaryType: z.string().min(1).max(256),
        message: z.record(z.unknown()),
      })
      .strict(),
  })
  .strict();

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

function bearerToken(req: FastifyRequest): string {
  const header = req.headers.authorization ?? '';
  return header.slice('Bearer '.length).trim();
}

let sharedAttestors: AttestorClient | null | undefined;

function defaultAttestors(): AttestorClient | null {
  if (sharedAttestors !== undefined) return sharedAttestors;
  sharedAttestors = attestorClientFromConfig(env);
  sharedAttestors?.start();
  return sharedAttestors;
}

interface SignedValue {
  value: Hex;
  attestor: SignResult | null;
}

interface WalletSigner {
  path: AuditPath;
  address: `0x${string}`;
  signTransaction(tx: TransactionSerializableEIP1559): Promise<SignedValue>;
  signMessage(message: string): Promise<SignedValue>;
  signTypedData(td: TypedDataDefinition): Promise<SignedValue>;
}

async function walletSigner(
  sw: SigningWallet,
  attestors: AttestorClient | null,
  token: string,
): Promise<WalletSigner> {
  if (sw.migratedAt !== null) {
    if (!attestors) {
      throw new AttestorQuorumError('attestor_unconfigured', 'wallet is migrated and no attestor endpoints are configured');
    }
    if (!sw.attestorKeyId) {
      throw new AttestorQuorumError('attestor_key_missing', 'migrated wallet carries no attestor key id');
    }
    const signer = attestorSigner(
      attestors,
      { keyId: sw.attestorKeyId, address: sw.row.address, chainId: env.HYPERPAXEER_CHAIN_ID },
      { scheme: 'supabase_jwt', token },
    );
    return {
      path: 'attestor',
      address: sw.row.address,
      async signTransaction(tx) {
        const r = await signer.signTransaction(tx);
        return { value: r.value, attestor: r.result };
      },
      async signMessage(message) {
        const r = await signer.signMessage(message);
        return { value: r.value, attestor: r.result };
      },
      async signTypedData(td) {
        const r = await signer.signTypedData(td);
        return { value: r.value, attestor: r.result };
      },
    };
  }
  const account = await getSigningAccountForRow(sw.row);
  return {
    path: 'envelope',
    address: account.address,
    async signTransaction(tx) {
      return { value: await account.signTransaction(tx), attestor: null };
    },
    async signMessage(message) {
      return { value: await account.signMessage({ message }), attestor: null };
    },
    async signTypedData(td) {
      return { value: await account.signTypedData(td as never), attestor: null };
    },
  };
}

class RouteRefusal extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly body: Record<string, unknown>,
    readonly headers: Record<string, string> = {},
  ) {
    super(code);
    this.name = 'RouteRefusal';
  }
}

interface SigningContext {
  requestId: string;
  subject: string;
  route: string;
  kind: AuditKind;
  requestHash: string;
}

interface WalletStage {
  client: PoolClient;
  sw: SigningWallet;
  signer: WalletSigner;
}

interface Completed {
  status: number;
  body: Record<string, unknown>;
  decision: 'signed' | 'broadcast' | 'broadcast_failed';
  path: AuditPath;
  account: string;
  walletId: string;
  attestor: SignResult | null;
  txHash: string | null;
  nonce: number | null;
  reasonCode: string | null;
  signatureLog?: SignatureLogInput;
}

export async function signRoutes(app: FastifyInstance, opts: SignRoutesOptions = {}): Promise<void> {
  const pool = opts.pool ?? getPool();
  const rpc = opts.rpc ?? sharedRpcPool();
  const nonces = opts.nonces ?? sharedNonceStore();
  const limiter =
    opts.limiter ??
    new RateLimiter({
      pool,
      clientPerMinute: env.RATE_LIMIT_CLIENT_PER_MINUTE,
      accountPerMinute: env.RATE_LIMIT_ACCOUNT_PER_MINUTE,
    });
  const attestors = opts.attestors !== undefined ? opts.attestors : defaultAttestors();

  async function run(
    req: FastifyRequest,
    reply: FastifyReply,
    ctx: SigningContext,
    valueWei: bigint,
    work: (stage: WalletStage) => Promise<Completed>,
  ): Promise<FastifyReply> {
    let account: string | null = null;
    let walletId: string | null = null;
    let path: AuditPath = 'none';
    const refuse = async (
      status: number,
      reasonCode: string,
      body: Record<string, unknown>,
      headers: Record<string, string> = {},
    ): Promise<FastifyReply> => {
      await recordSigningAudit(pool, {
        requestId: ctx.requestId,
        clientSubject: ctx.subject,
        account,
        walletId,
        route: ctx.route,
        kind: ctx.kind,
        path,
        decision: 'refused',
        reasonCode,
        requestHash: ctx.requestHash,
        sessionId: null,
        attestorAudit: [],
        txHash: null,
        nonce: null,
      });
      for (const [k, v] of Object.entries(headers)) void reply.header(k, v);
      return reply.code(status).send({ ...body, request_id: ctx.requestId });
    };

    try {
      await limiter.consume('client', ctx.subject);
    } catch (err) {
      if (err instanceof RateLimitedError) {
        return refuse(429, 'rate_limited_client', rateLimitBody(err), {
          'retry-after': String(err.retryAfterSeconds),
        });
      }
      throw err;
    }

    const decision = await evaluate({ user_id: ctx.subject, value_wei: valueWei });
    if (!decision.allow) {
      return refuse(403, `policy_${decision.code.toLowerCase()}`, { error: decision.code, message: decision.message });
    }

    const client = await pool.connect();
    let completed: Completed;
    try {
      await client.query('BEGIN');
      const sw = await loadWalletForSigning(client, ctx.subject);
      if (!sw) throw new RouteRefusal(404, 'no_wallet', { error: 'no_wallet', message: 'wallet not provisioned' });
      account = sw.row.address;
      walletId = sw.row.id;
      path = sw.migratedAt !== null ? 'attestor' : 'envelope';
      if (sw.row.is_disabled) {
        throw new RouteRefusal(403, 'wallet_disabled', {
          error: 'WALLET_DISABLED',
          message: sw.row.disabled_reason ?? 'wallet disabled',
        });
      }
      try {
        await limiter.consume('account', sw.row.address);
      } catch (err) {
        if (err instanceof RateLimitedError) {
          throw new RouteRefusal(429, 'rate_limited_account', rateLimitBody(err), {
            'retry-after': String(err.retryAfterSeconds),
          });
        }
        throw err;
      }
      const signer = await walletSigner(sw, attestors, bearerToken(req));
      completed = await work({ client, sw, signer });
      await client.query('COMMIT');
    } catch (err) {
      await client.query('ROLLBACK').catch(() => undefined);
      client.release();
      if (err instanceof RouteRefusal) return refuse(err.status, err.code, err.body, err.headers);
      if (err instanceof AttestorError) {
        return refuse(attestorErrorStatus(err), `${err.category}:${err.code}`, attestorErrorBody(err));
      }
      if (err instanceof RateLimitedError) {
        return refuse(429, `rate_limited_${err.scope}`, rateLimitBody(err));
      }
      req.log.error({ err, route: ctx.route }, 'signing failed');
      return refuse(500, 'internal', {
        error: ctx.route.endsWith('/send') ? 'send_failed' : 'sign_failed',
        detail: (err as Error).message,
      });
    }
    client.release();

    if (completed.signatureLog) await logSignature(completed.signatureLog);
    await recordSigningAudit(pool, {
      requestId: ctx.requestId,
      clientSubject: ctx.subject,
      account: completed.account,
      walletId: completed.walletId,
      route: ctx.route,
      kind: ctx.kind,
      path: completed.path,
      decision: completed.decision,
      reasonCode: completed.reasonCode,
      requestHash: ctx.requestHash,
      sessionId: completed.attestor?.sessionId ?? null,
      attestorAudit: completed.attestor?.audit ?? ([] as NodeAudit[]),
      txHash: completed.txHash,
      nonce: completed.nonce,
    });
    void reply.header('x-request-id', ctx.requestId);
    return reply.code(completed.status).send(completed.body);
  }

  async function prepareTx(
    signer: WalletSigner,
    tx: TxRequest,
  ): Promise<Omit<TransactionSerializableEIP1559, 'nonce'>> {
    const params = buildTxParams(tx);
    try {
      await rpc.simulate({ from: signer.address, to: params.to, data: params.data, value: params.value });
    } catch (err) {
      if (err instanceof RpcResponseError) {
        throw new RouteRefusal(422, 'simulation_failed', {
          error: 'simulation_failed',
          message: err.message,
        });
      }
      throw err;
    }
    const gas = await rpc.prepareGas({
      from: signer.address,
      to: params.to,
      data: params.data,
      value: params.value,
      gas: params.gas,
      maxFeePerGas: params.maxFeePerGas,
      maxPriorityFeePerGas: params.maxPriorityFeePerGas,
    });
    return {
      type: 'eip1559',
      chainId: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      to: params.to,
      value: params.value,
      data: params.data,
      gas: gas.gas,
      maxFeePerGas: gas.maxFeePerGas,
      maxPriorityFeePerGas: gas.maxPriorityFeePerGas,
    };
  }

  const ipOf = (req: FastifyRequest): string | null =>
    clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip);

  app.post('/v1/wallet/sign', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;
    const chainId = tx.chainId ?? env.HYPERPAXEER_CHAIN_ID;
    const requestHash = hashRequest({ tx });
    return run(
      req,
      reply,
      { requestId: randomUUID(), subject: userId, route: '/v1/wallet/sign', kind: 'transaction', requestHash },
      valueWei,
      async ({ sw, signer }) => {
        const prepared = await prepareTx(signer, tx);
        const nonce = tx.nonce ?? (await rpc.getTransactionCount(signer.address, 'pending'));
        const signed = await signer.signTransaction({ ...prepared, nonce });
        const signatureLog: SignatureLogInput = {
          user_id: userId,
          wallet_id: sw.row.id,
          address: sw.row.address,
          kind: 'transaction',
          to_address: tx.to ?? null,
          value_wei: valueWei,
          chain_id: chainId,
          request_hash: requestHash,
          ip: ipOf(req),
          user_agent: req.headers['user-agent'] ?? null,
        };
        return {
          status: 200,
          body: { signed_tx: signed.value, address: sw.row.address, chain_id: chainId },
          decision: 'signed',
          path: signer.path,
          account: sw.row.address,
          walletId: sw.row.id,
          attestor: signed.attestor,
          txHash: null,
          nonce,
          reasonCode: null,
          signatureLog,
        };
      },
    );
  });

  app.post('/v1/wallet/send', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SendTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;
    const chainId = tx.chainId ?? env.HYPERPAXEER_CHAIN_ID;
    const requestHash = hashRequest({ tx });
    return run(
      req,
      reply,
      { requestId: randomUUID(), subject: userId, route: '/v1/wallet/send', kind: 'transaction', requestHash },
      valueWei,
      async ({ sw, signer }) => {
        const prepared = await prepareTx(signer, tx);
        const outcome = await nonces.withLock(sw.row.address, async (lease) => {
          const nonce = tx.nonce ?? (await lease.next());
          const signed = await signer.signTransaction({ ...prepared, nonce });
          if (tx.nonce !== undefined) await lease.markForReconcile();
          try {
            const hash = await rpc.sendRawTransaction(signed.value);
            return { ok: true as const, hash, nonce, signed };
          } catch (err) {
            await lease.markForReconcile();
            return { ok: false as const, error: err as Error, nonce, signed };
          }
        });
        if (!outcome.ok) {
          req.log.error({ err: outcome.error, userId }, 'send_transaction broadcast failed');
          return {
            status: 500,
            body: { error: 'send_failed', detail: outcome.error.message },
            decision: 'broadcast_failed',
            path: signer.path,
            account: sw.row.address,
            walletId: sw.row.id,
            attestor: outcome.signed.attestor,
            txHash: null,
            nonce: outcome.nonce,
            reasonCode: outcome.error instanceof RpcResponseError ? `rpc_${outcome.error.code}` : 'rpc_unavailable',
          };
        }
        const signatureLog: SignatureLogInput = {
          user_id: userId,
          wallet_id: sw.row.id,
          address: sw.row.address,
          kind: 'transaction',
          to_address: tx.to ?? null,
          value_wei: valueWei,
          chain_id: chainId,
          request_hash: requestHash,
          tx_hash: outcome.hash,
          ip: ipOf(req),
          user_agent: req.headers['user-agent'] ?? null,
        };
        return {
          status: 200,
          body: { tx_hash: outcome.hash, address: sw.row.address, chain_id: chainId },
          decision: 'broadcast',
          path: signer.path,
          account: sw.row.address,
          walletId: sw.row.id,
          attestor: outcome.signed.attestor,
          txHash: outcome.hash,
          nonce: outcome.nonce,
          reasonCode: null,
          signatureLog,
        };
      },
    );
  });

  app.post('/v1/wallet/sign-message', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignMessageBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const requestHash = hashRequest({ message: parsed.data.message });
    return run(
      req,
      reply,
      { requestId: randomUUID(), subject: userId, route: '/v1/wallet/sign-message', kind: 'message', requestHash },
      0n,
      async ({ sw, signer }) => {
        const signed = await signer.signMessage(parsed.data.message);
        const signatureLog: SignatureLogInput = {
          user_id: userId,
          wallet_id: sw.row.id,
          address: sw.row.address,
          kind: 'message',
          request_hash: requestHash,
          ip: ipOf(req),
          user_agent: req.headers['user-agent'] ?? null,
        };
        return {
          status: 200,
          body: { signature: signed.value, address: sw.row.address },
          decision: 'signed',
          path: signer.path,
          account: sw.row.address,
          walletId: sw.row.id,
          attestor: signed.attestor,
          txHash: null,
          nonce: null,
          reasonCode: null,
          signatureLog,
        };
      },
    );
  });

  app.post('/v1/wallet/sign-typed-data', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignTypedDataBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const td = parsed.data.typed_data as unknown as TypedDataDefinition;
    const requestHash = hashRequest({ typed_data: parsed.data.typed_data });
    return run(
      req,
      reply,
      { requestId: randomUUID(), subject: userId, route: '/v1/wallet/sign-typed-data', kind: 'typed_data', requestHash },
      0n,
      async ({ sw, signer }) => {
        const signed = await signer.signTypedData(td);
        const signatureLog: SignatureLogInput = {
          user_id: userId,
          wallet_id: sw.row.id,
          address: sw.row.address,
          kind: 'typed_data',
          request_hash: requestHash,
          ip: ipOf(req),
          user_agent: req.headers['user-agent'] ?? null,
        };
        return {
          status: 200,
          body: { signature: signed.value, address: sw.row.address },
          decision: 'signed',
          path: signer.path,
          account: sw.row.address,
          walletId: sw.row.id,
          attestor: signed.attestor,
          txHash: null,
          nonce: null,
          reasonCode: null,
          signatureLog,
        };
      },
    );
  });
}

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
