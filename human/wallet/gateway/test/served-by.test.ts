import { randomBytes, randomUUID } from 'node:crypto';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let base: string;
let index: typeof import('../src/index.js');
let poolModule: typeof import('../src/db/pool.js');

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `${idp.url}/rpc`;
  pg = await startPostgres();
  index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  app = await index.buildApp();
  await app.listen({ port: 0, host: '127.0.0.1' });
  const address = app.server.address() as AddressInfo;
  base = `http://127.0.0.1:${address.port}`;
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
}, 120_000);

describe('served-by header', () => {
  it('names the gateway as paxeer-wallet-gateway', () => {
    expect(index.SERVED_BY_HEADER).toBe('x-served-by');
    expect(index.SERVED_BY).toBe('paxeer-wallet-gateway');
  });

  it('is set on a liveness answer over a real connection', async () => {
    const res = await fetch(`${base}/healthz`);
    expect(res.status).toBe(200);
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
    expect(await res.json()).toMatchObject({ ok: true });
  });

  it('is set on the readiness answer whatever its status', async () => {
    const res = await fetch(`${base}/readyz`);
    expect([200, 503]).toContain(res.status);
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });

  it('is set on an authenticated wallet answer', async () => {
    const token = await idp.mintUserToken(randomUUID());
    const res = await fetch(`${base}/v1/wallet/provision`, {
      method: 'POST',
      headers: { authorization: `Bearer ${token}` },
    });
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
    const me = await fetch(`${base}/v1/wallet/me`, { headers: { authorization: `Bearer ${token}` } });
    expect(me.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });

  it('is set on a refused request without a token', async () => {
    const res = await fetch(`${base}/v1/wallet/me`);
    expect(res.status).toBe(401);
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });

  it('is set on an unknown route', async () => {
    const res = await fetch(`${base}/no-such-route`);
    expect(res.status).toBe(404);
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });

  it('is set on a malformed request body', async () => {
    const token = await idp.mintUserToken(randomUUID());
    const res = await fetch(`${base}/v1/wallet/send`, {
      method: 'POST',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
      body: '{not json',
    });
    expect(res.status).toBe(400);
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });

  it('is set on a CORS preflight answer', async () => {
    const res = await fetch(`${base}/v1/wallet/me`, {
      method: 'OPTIONS',
      headers: {
        origin: 'http://localhost:3000',
        'access-control-request-method': 'GET',
        'access-control-request-headers': 'authorization',
      },
    });
    expect(res.status).toBeLessThan(300);
    expect(res.headers.get('access-control-allow-origin')).toBe('http://localhost:3000');
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
  });
});
