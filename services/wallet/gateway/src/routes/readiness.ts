import type { FastifyInstance } from 'fastify';
import type { Pool } from 'pg';
import { env } from '../env.js';
import { getPool } from '../db/pool.js';
import { attestorClientFromConfig, type AttestorClient } from '../attestor/client.js';
import { isHealthy } from '../attestor/quorum.js';
import { RpcPool, rpcPoolFromConfig, type EndpointState } from '../rpc/pool.js';

export type ComponentState = 'up' | 'down';

export interface AttestorComponent {
  state: ComponentState;
  required: number;
  healthy: number;
  nodes: Array<{ endpoint: string; node_id: string | null; healthy: boolean; error: string | null }>;
  reason: string | null;
}

export interface NonceStoreComponent {
  state: ComponentState;
  reason: string | null;
}

export interface RpcPoolComponent {
  state: ComponentState;
  healthy: number;
  endpoints: Array<{ url: string; state: EndpointState; head: string | null; error: string | null }>;
  reason: string | null;
}

export interface IdentityComponent {
  state: ComponentState;
  keys: number;
  reason: string | null;
}

export interface ReadinessReport {
  ready: boolean;
  components: {
    attestors: AttestorComponent;
    nonce_store: NonceStoreComponent;
    rpc_pool: RpcPoolComponent;
    identity_provider: IdentityComponent;
  };
}

export interface ReadinessOptions {
  attestors: AttestorClient | null;
  pool: Pool;
  rpc: RpcPool;
  jwksUrl: string;
  timeoutMs: number;
}

function reasonOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function withTimeout<T>(work: Promise<T>, timeoutMs: number, label: string): Promise<T> {
  let timer: NodeJS.Timeout;
  const expiry = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${label} did not answer within ${timeoutMs} ms`)), timeoutMs);
  });
  return Promise.race([work, expiry]).finally(() => clearTimeout(timer));
}

async function checkAttestors(client: AttestorClient | null, timeoutMs: number): Promise<AttestorComponent> {
  if (!client) {
    return { state: 'down', required: env.ATTESTOR_QUORUM, healthy: 0, nodes: [], reason: 'attestor_unconfigured' };
  }
  try {
    await withTimeout(client.refreshHealth(), timeoutMs, 'attestor health');
  } catch (err) {
    return { state: 'down', required: client.quorum, healthy: 0, nodes: [], reason: reasonOf(err) };
  }
  const nodes = client.health();
  const healthy = nodes.filter((n) => isHealthy(n, client.quorum)).length;
  const up = healthy >= client.quorum;
  return {
    state: up ? 'up' : 'down',
    required: client.quorum,
    healthy,
    nodes: nodes.map((n) => ({
      endpoint: n.endpoint,
      node_id: n.nodeId,
      healthy: isHealthy(n, client.quorum),
      error: n.lastError,
    })),
    reason: up ? null : 'attestor_quorum_unavailable',
  };
}

async function checkNonceStore(pool: Pool, timeoutMs: number): Promise<NonceStoreComponent> {
  try {
    await withTimeout(pool.query('select 1 from nonce_allocations limit 1'), timeoutMs, 'nonce store');
    return { state: 'up', reason: null };
  } catch (err) {
    return { state: 'down', reason: reasonOf(err) };
  }
}

async function checkRpcPool(rpc: RpcPool, timeoutMs: number): Promise<RpcPoolComponent> {
  try {
    await withTimeout(rpc.checkHealth(), timeoutMs, 'rpc pool');
  } catch (err) {
    return { state: 'down', healthy: 0, endpoints: [], reason: reasonOf(err) };
  }
  const endpoints = rpc.status().map((e) => ({
    url: e.url,
    state: e.state,
    head: e.head === null ? null : e.head.toString(),
    error: e.lastError,
  }));
  const healthy = rpc.healthyCount();
  return {
    state: healthy > 0 ? 'up' : 'down',
    healthy,
    endpoints,
    reason: healthy > 0 ? null : 'rpc_pool_unavailable',
  };
}

async function checkIdentity(jwksUrl: string, timeoutMs: number): Promise<IdentityComponent> {
  try {
    const res = await fetch(jwksUrl, { signal: AbortSignal.timeout(timeoutMs), headers: { accept: 'application/json' } });
    if (!res.ok) return { state: 'down', keys: 0, reason: `jwks answered ${res.status}` };
    const body = (await res.json()) as { keys?: unknown };
    const keys = Array.isArray(body.keys) ? body.keys.length : 0;
    if (keys === 0) return { state: 'down', keys: 0, reason: 'jwks_empty' };
    return { state: 'up', keys, reason: null };
  } catch (err) {
    return { state: 'down', keys: 0, reason: reasonOf(err) };
  }
}

export async function checkReadiness(opts: ReadinessOptions): Promise<ReadinessReport> {
  const [attestors, nonceStore, rpcPool, identity] = await Promise.all([
    checkAttestors(opts.attestors, opts.timeoutMs),
    checkNonceStore(opts.pool, opts.timeoutMs),
    checkRpcPool(opts.rpc, opts.timeoutMs),
    checkIdentity(opts.jwksUrl, opts.timeoutMs),
  ]);
  const ready =
    attestors.state === 'up' && nonceStore.state === 'up' && rpcPool.state === 'up' && identity.state === 'up';
  return {
    ready,
    components: { attestors, nonce_store: nonceStore, rpc_pool: rpcPool, identity_provider: identity },
  };
}

export function identityJwksUrl(supabaseUrl: string): string {
  return `${supabaseUrl.replace(/\/$/, '')}/auth/v1/.well-known/jwks.json`;
}

export async function readinessRoutes(app: FastifyInstance, opts: Partial<ReadinessOptions> = {}): Promise<void> {
  const resolved: ReadinessOptions = {
    attestors: opts.attestors !== undefined ? opts.attestors : attestorClientFromConfig(env),
    pool: opts.pool ?? getPool(),
    rpc: opts.rpc ?? rpcPoolFromConfig(env),
    jwksUrl: opts.jwksUrl ?? identityJwksUrl(env.SUPABASE_URL),
    timeoutMs: opts.timeoutMs ?? Math.max(env.ATTESTOR_TIMEOUT_MS, env.RPC_TIMEOUT_MS),
  };
  if (opts.attestors === undefined) app.addHook('onClose', async () => resolved.attestors?.stop());

  app.get('/readyz', async (_req, reply) => {
    const report = await checkReadiness(resolved);
    if (report.ready) return reply.code(200).send(report);
    return reply.code(503).send({ error: 'not_ready', ...report });
  });
}
