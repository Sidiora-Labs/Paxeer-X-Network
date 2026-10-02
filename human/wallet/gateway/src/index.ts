import Fastify, { type FastifyInstance } from 'fastify';
import cors from '@fastify/cors';
import sensible from '@fastify/sensible';
import cluster from 'node:cluster';
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { env } from './env.js';
import { closePool, getPool } from './db/pool.js';
import { runMigrations } from './db/migrate.js';
import { walletRoutes } from './routes/wallet.js';
import { signRoutes } from './routes/sign.js';
import { readinessRoutes } from './routes/readiness.js';
import { agentAuthRoutes } from './routes/agentAuth.js';
import { agentRoutes } from './routes/agent.js';
import { agentsRoutes } from './routes/agents.js';
import { agentActionRoutes } from './routes/agentActions.js';
import { agentPrecompileRoutes } from './routes/agentPrecompiles.js';
import { ownerRoutes } from './routes/owner.js';
import { agentLaneEnabled } from './auth/agentToken.js';
import { installRawBodyParser } from './agent/verify.js';
import { startActionWorker, type WorkerHandle } from './agent/actions/worker.js';
import { startLayerxSync, type LayerxSyncHandle } from './jobs/layerxSync.js';
import { closeLayerxPool } from './layerx/db.js';

/** Response header that names this service on every response it sends. */
export const SERVED_BY_HEADER = 'x-served-by';
export const SERVED_BY = 'paxeer-wallet-gateway';

/**
 * Build the Paxeer Embedded Wallet API: connect and migrate the database,
 * then register every route. Background workers and the listener are started
 * by main(), so tests drive the returned instance through inject().
 */
export async function buildApp(): Promise<FastifyInstance> {
  const app = Fastify({
    logger: {
      level: env.LOG_LEVEL,
      transport:
        env.NODE_ENV === 'development'
          ? { target: 'pino-pretty', options: { translateTime: 'HH:MM:ss', colorize: true } }
          : undefined,
    },
    trustProxy: true,
    bodyLimit: 1_048_576, // 1 MB
  });

  // ---- Database: connect, then run pending migrations --------------------
  // We do this BEFORE any routes are served so /healthz only returns OK when
  // the DB is genuinely reachable and the schema is up to date.
  await getPool().query('select 1');
  app.log.info('[db] connected');
  await runMigrations();
  app.log.info('[db] migrations applied');

  app.addHook('onSend', async (_req, reply, payload) => {
    void reply.header(SERVED_BY_HEADER, SERVED_BY);
    return payload;
  });

  installRawBodyParser(app);

  await app.register(sensible);
  await app.register(cors, {
    origin: env.CORS_ORIGINS,
    credentials: true,
    allowedHeaders: [
      'Content-Type',
      'Authorization',
      'X-Agent-Key',
      'X-Agent-Nonce',
      'X-Agent-Expires',
      'X-Agent-Signature',
      'X-Agent-Attestor-Authorization-Id',
      'X-Agent-Attestor-Authorization',
    ],
    methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
  });

  // Health
  app.get('/healthz', async () => ({
    ok: true,
    service: 'paxeer-wallet-api',
    version: '0.1.0',
    chain_id: env.HYPERPAXEER_CHAIN_ID,
  }));

  await app.register(readinessRoutes);

  // v1 routes
  await app.register(walletRoutes);
  await app.register(signRoutes);

  // Agent-native lane: DID auth + dedicated kind='agent' wallets, the agent
  // capability surface, network-native precompiles, and the owner control
  // plane. The read token is minted by the auth routes; every value-moving
  // agent route requires the agent's end-to-end request signature. The owner
  // control plane and the claim route (human-JWT authed) are always mounted.
  await app.register(agentAuthRoutes);
  await app.register(agentRoutes);
  await app.register(agentActionRoutes);
  await app.register(agentPrecompileRoutes);
  await app.register(ownerRoutes);
  await app.register(agentsRoutes);
  if (agentLaneEnabled()) {
    app.log.info('[agent] agent-native lane enabled');
  } else {
    app.log.warn('[agent] AGENT_JWT_SECRET unset — agent auth routes return 503 (owner control plane still mounted)');
  }

  // Global error handler — never leak internal details to clients in prod.
  app.setErrorHandler((err, req, reply) => {
    req.log.error({ err }, 'unhandled error');
    const statusCode =
      (err as Error & { statusCode?: number }).statusCode ?? reply.statusCode ?? 500;
    void reply.code(statusCode >= 400 && statusCode < 600 ? statusCode : 500).send({
      error: 'internal_error',
      message:
        env.NODE_ENV === 'development' ? (err as Error).message : 'unexpected error',
    });
  });

  return app;
}

async function main(): Promise<void> {
  let app: FastifyInstance;
  try {
    app = await buildApp();
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error('[db] fatal — could not connect / migrate', err);
    process.exit(1);
  }

  // Durable-action worker: drives the high-level intent state machine
  // (approve → confirm → call → confirm → verify) server-side AND reconciles
  // in-flight actions after a restart. Persisted tx hashes/nonces make resume
  // safe — it never blind-resends. Runs in every worker; per-row leases +
  // SELECT FOR UPDATE SKIP LOCKED keep the cluster from double-driving a row.
  const actionWorker: WorkerHandle = startActionWorker(app.log);

  // LayerX mirror-sync + credit-backfill (read-only against the sequencer DB).
  // No-op when LAYER_X_DB_URI is unset; single-syncs per tick via advisory lock.
  const layerxSync: LayerxSyncHandle | null = startLayerxSync(app.log);

  // Graceful shutdown — drain Fastify, then close the pg pool so the process
  // exits cleanly on SIGTERM/SIGINT (Docker sends SIGTERM on `stop`).
  const shutdown = async (signal: string): Promise<void> => {
    app.log.info(`[shutdown] received ${signal}`);
    try {
      actionWorker.stop();
      layerxSync?.stop();
      await app.close();
      await closeLayerxPool();
      await closePool();
      process.exit(0);
    } catch (err) {
      app.log.error({ err }, '[shutdown] error');
      process.exit(1);
    }
  };
  process.on('SIGTERM', () => void shutdown('SIGTERM'));
  process.on('SIGINT', () => void shutdown('SIGINT'));

  try {
    await app.listen({ port: env.PORT, host: '0.0.0.0' });
    app.log.info(
      `paxeer-wallet-api listening on :${env.PORT} (chain_id=${env.HYPERPAXEER_CHAIN_ID})`,
    );
  } catch (err) {
    app.log.error({ err }, 'failed to start');
    process.exit(1);
  }
}

function isEntrypoint(): boolean {
  const entry = process.argv[1];
  if (!entry) return false;
  try {
    return realpathSync(entry) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

// -----------------------------------------------------------------------------
// Cluster bootstrap
//
// When API_WORKERS > 1 and we're the primary, fork N workers and supervise
// them. Each worker runs main() independently, opening its own DB pool,
// JWKS cache, and HTTP listener. Node's cluster module installs an OS-level
// round-robin TCP accept on the listening socket — no extra load balancer
// needed inside the container.
//
// On worker exit, we fork a replacement so a crash doesn't degrade capacity.
// SIGTERM from `docker stop` propagates from primary -> workers automatically;
// each worker runs its own graceful shutdown (see SIGTERM handler in main).
//
// API_WORKERS=1 (the default) bypasses cluster entirely — useful in dev,
// tests, and any environment where pid 1 must be the Node process itself.
// -----------------------------------------------------------------------------
function bootstrap(): void {
  if (env.API_WORKERS > 1 && cluster.isPrimary) {
    // eslint-disable-next-line no-console
    console.log(`[cluster] primary ${process.pid} forking ${env.API_WORKERS} workers`);
    for (let i = 0; i < env.API_WORKERS; i++) cluster.fork();

    cluster.on('exit', (worker, code, signal) => {
      // eslint-disable-next-line no-console
      console.error(
        `[cluster] worker ${worker.process.pid} exited (code=${code} signal=${signal}) — respawning`,
      );
      cluster.fork();
    });

    // Propagate signals: graceful shutdown of every worker.
    const broadcast = (sig: NodeJS.Signals): void => {
      for (const id in cluster.workers) cluster.workers[id]?.kill(sig);
    };
    process.on('SIGTERM', () => broadcast('SIGTERM'));
    process.on('SIGINT', () => broadcast('SIGINT'));
    return;
  }
  main().catch((err) => {
    // eslint-disable-next-line no-console
    console.error('fatal:', err);
    process.exit(1);
  });
}

if (isEntrypoint()) bootstrap();
