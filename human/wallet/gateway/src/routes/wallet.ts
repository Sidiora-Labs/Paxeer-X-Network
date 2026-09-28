import type { FastifyInstance } from 'fastify';
import { requireAuth } from '../middleware/auth.js';
import {
  WalletArchivedError,
  archivedWalletGuard,
  findWalletByUserId,
  provisionWalletForUser,
} from '../db/wallets.js';
import { env } from '../env.js';

export async function walletRoutes(app: FastifyInstance): Promise<void> {
  /**
   * POST /v1/wallet/provision
   * Idempotent. Creates a wallet for the authenticated user if none exists.
   * Returns the public wallet record either way.
   */
  app.post('/v1/wallet/provision', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    try {
      const { wallet } = await provisionWalletForUser(userId, 'standard');
      return reply.send({ wallet });
    } catch (err) {
      if (err instanceof WalletArchivedError) return reply.code(err.refusal.status).send(err.refusal.body);
      req.log.error({ err, userId }, 'wallet provision failed');
      return reply
        .code(500)
        .send({ error: 'wallet_provision_failed', detail: (err as Error).message });
    }
  });

  /**
   * GET /v1/wallet/me
   * Returns the user's wallet (public fields only). 404 if not provisioned.
   */
  app.get('/v1/wallet/me', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      const refusal = await archivedWalletGuard({ userId, kind: 'standard' });
      if (refusal) return reply.code(refusal.status).send(refusal.body);
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }
    return reply.send({
      wallet: {
        id: wallet.id,
        address: wallet.address,
        chain_id: wallet.chain_id,
        created_at: wallet.created_at,
        last_used_at: wallet.last_used_at,
      },
      chain: {
        id: env.HYPERPAXEER_CHAIN_ID,
        rpc_url: env.HYPERPAXEER_RPC_URL,
        explorer_url: env.HYPERPAXEER_EXPLORER_URL ?? null,
      },
    });
  });
}
