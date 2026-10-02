import type { FastifyInstance, FastifyRequest } from 'fastify';
import { readFileSync } from 'node:fs';
import { privateKeyToAccount } from 'viem/accounts';
import { requireAuth } from '../middleware/auth.js';
import {
  WalletArchivedError,
  archivedWalletGuard,
  findWalletByUserId,
  provisionWalletForUser,
} from '../db/wallets.js';
import { getPool } from '../db/pool.js';
import { env } from '../env.js';
import { sharedRpcPool } from '../rpc/pool.js';
import { sharedNonceStore } from '../nonce/store.js';
import { AttestorRefusal, AttestorUnavailable, attestorDaemonFromConfig, readKernelAvailability } from '../provision/bind.js';
import { ProvisionError, ProvisionRefusedError, provisionAccount, produceIdentityBinding, type ProvisionDeps } from '../provision/state.js';

export interface WalletRoutesOptions {
  provision?: ProvisionDeps | null;
}

let sharedDeps: ProvisionDeps | null | undefined;

export function provisionDepsFromEnv(): ProvisionDeps | null {
  if (sharedDeps !== undefined) return sharedDeps;
  const attestors = attestorDaemonFromConfig(env);
  if (!attestors) {
    sharedDeps = null;
    return sharedDeps;
  }
  const sponsor = env.SPONSOR_PRIVATE_KEY_FILE
    ? privateKeyToAccount(`0x${readFileSync(env.SPONSOR_PRIVATE_KEY_FILE, 'utf8').trim().replace(/^0x/, '')}`)
    : null;
  sharedDeps = {
    pool: getPool(),
    rpc: sharedRpcPool(),
    attestors,
    nonces: sharedNonceStore(),
    sponsor,
    chainId: env.HYPERPAXEER_CHAIN_ID,
    gasCapWei: env.ACCOUNT_SETUP_GAS_CAP_WEI,
    receiptTimeoutMs: 60_000,
    receiptPollMs: 250,
    ...(env.WALLET_IDENTITY_BINDING_TENANT && env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE ? {
      identityBinding: {
        issuer: `${env.SUPABASE_URL.replace(/\/$/, '')}/auth/v1`,
        tenant: env.WALLET_IDENTITY_BINDING_TENANT,
        privateKeyFile: env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE,
      },
    } : {}),
  };
  return sharedDeps;
}

function bearerToken(req: FastifyRequest): string {
  return (req.headers.authorization ?? '').replace(/^Bearer\s+/i, '').trim();
}

interface UnifiedColumns {
  did: string | null;
  main_account_id: string | null;
  binding_state: string;
}

async function unifiedColumns(walletId: string): Promise<UnifiedColumns> {
  const { rows } = await getPool().query<UnifiedColumns>(
    `select did, main_account_id, binding_state from wallets where id = $1`,
    [walletId],
  );
  return rows[0] ?? { did: null, main_account_id: null, binding_state: 'unbound' };
}

export async function walletRoutes(app: FastifyInstance, opts: WalletRoutesOptions = {}): Promise<void> {
  const deps = (): ProvisionDeps | null => (opts.provision !== undefined ? opts.provision : provisionDepsFromEnv());

  app.post('/v1/wallet/provision', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    const provision = deps();
    try {
      if (!provision) {
        if (env.WALLET_IDENTITY_BINDING_TENANT) {
          throw new ProvisionError('attestors_unconfigured', 503, 'identity provisioning requires configured attestors');
        }
        const { wallet } = await provisionWalletForUser(userId, 'standard');
        return reply.send({ wallet, identityBinding: null });
      }
      const out = await provisionAccount(provision, { kind: 'standard', userId, token: bearerToken(req) });
      const wallet = await findWalletByUserId(userId);
      const identityBinding = await produceIdentityBinding(provision, userId, out);
      return reply.send({
        identityBinding,
        wallet: {
          id: out.walletId,
          address: out.address,
          chain_id: wallet?.chain_id ?? env.HYPERPAXEER_CHAIN_ID,
          kind: 'standard',
          created_at: wallet?.created_at ?? null,
          last_used_at: wallet?.last_used_at ?? null,
          did: out.did,
          main_account_id: out.mainAccountId,
          binding_state: out.state === 'active' ? 'bound' : 'pending',
        },
        provisioning: { state: out.state, awaiting: out.awaiting },
      });
    } catch (err) {
      if (err instanceof WalletArchivedError) return reply.code(err.refusal.status).send(err.refusal.body);
      if (err instanceof ProvisionRefusedError) {
        return reply.code(409).send({ error: 'binding_refused', message: err.message, bound_did: err.boundDid });
      }
      if (err instanceof ProvisionError) {
        return reply.code(err.status).send({ error: err.code, message: err.message });
      }
      if (err instanceof AttestorRefusal) {
        return reply.code(err.status === 401 || err.status === 403 ? err.status : 502).send({
          error: 'attestor_refused',
          category: err.category,
          code: err.code,
        });
      }
      if (err instanceof AttestorUnavailable) {
        return reply.code(503).send({ error: 'attestor_unavailable', message: err.message });
      }
      req.log.error({ err, userId }, 'wallet provision failed');
      return reply
        .code(500)
        .send({ error: 'wallet_provision_failed', detail: (err as Error).message });
    }
  });

  app.get('/v1/wallet/me', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      const refusal = await archivedWalletGuard({ userId, kind: 'standard' });
      if (refusal) return reply.code(refusal.status).send(refusal.body);
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }
    const unified = await unifiedColumns(wallet.id);
    const provision = deps();
    const kernel = await readKernelAvailability(provision ? provision.rpc : sharedRpcPool());
    return reply.send({
      wallet: {
        id: wallet.id,
        address: wallet.address,
        chain_id: wallet.chain_id,
        created_at: wallet.created_at,
        last_used_at: wallet.last_used_at,
        did: unified.did,
        main_account_id: unified.main_account_id,
        binding_state: unified.binding_state,
      },
      chain: {
        id: env.HYPERPAXEER_CHAIN_ID,
        rpc_url: env.HYPERPAXEER_RPC_URL,
        explorer_url: env.HYPERPAXEER_EXPLORER_URL ?? null,
      },
      kernel,
    });
  });
}
