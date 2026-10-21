import { randomBytes, randomUUID } from 'node:crypto';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import Fastify from 'fastify';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let base: string;
let index: typeof import('../src/index.js');
let poolModule: typeof import('../src/db/pool.js');
let sponsorApp: FastifyInstance;

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `${idp.url}/rpc`;
  process.env.SPONSOR_PRIVATE_KEY_FILE = '';
  delete process.env.WALLET_GAS_STATION_URL;
  pg = await startPostgres();
  index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  app = await index.buildApp();
  await app.listen({ port: 0, host: '127.0.0.1' });
  const address = app.server.address() as AddressInfo;
  base = `http://127.0.0.1:${address.port}`;
  sponsorApp = Fastify();
  const { walletSponsorRoutes } = await import('../src/routes/wallet.js');
  const { RpcPool } = await import('../src/rpc/pool.js');
  await sponsorApp.register(walletSponsorRoutes, {
    stationUrl: idp.url,
    rpc: new RpcPool({
      urls: [`${idp.url}/rpc`],
      chainId: 125,
      lagThresholdBlocks: 0,
      timeoutMs: 1_000,
      healthIntervalMs: 1_000,
    }),
  });
  await sponsorApp.ready();
}, 120_000);

afterAll(async () => {
  await app?.close();
  await sponsorApp?.close();
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
    expect(res.status).toBe(200);
    const provisioned = (await res.json()) as { wallet: { id: string; address: string } };
    expect(res.headers.get('x-served-by')).toBe('paxeer-wallet-gateway');
    const me = await fetch(`${base}/v1/wallet/me`, {
      headers: { authorization: `Bearer ${token}` },
    });
    expect(me.status).toBe(200);
    expect(await me.json()).toMatchObject({
      wallet: { id: provisioned.wallet.id, address: provisioned.wallet.address },
    });
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

describe('sponsorship identity boundary', () => {
  async function provisionCaller() {
    const userId = randomUUID();
    const token = await idp.mintUserToken(userId);
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/provision',
      headers: { authorization: `Bearer ${token}` },
    });
    expect(response.statusCode).toBe(200);
    const wallet = response.json().wallet as { id: string; address: string };
    return { userId, token, wallet };
  }

  async function statusRequest(account: string) {
    const sponsor = privateKeyToAccount(generatePrivateKey());
    return {
      account,
      sponsor: sponsor.address,
      quoteNonce: '0',
      relayerSignature: await sponsor.signMessage({ message: account }),
    };
  }

  it('starts with the retired sponsor input discarded', async () => {
    const { env } = await import('../src/env.js');
    expect(Object.hasOwn(env, 'SPONSOR_PRIVATE_KEY_FILE')).toBe(false);
    expect(Object.hasOwn(env, 'WALLET_GAS_STATION_URL')).toBe(false);
    const { wallet, token } = await provisionCaller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${token}` },
      payload: await statusRequest(wallet.address),
    });
    expect(response.statusCode).toBe(503);
    expect(response.json()).toEqual({ error: 'gas_station_unconfigured', status: 'pending' });
    expect(response.headers['x-served-by']).toBe('paxeer-wallet-gateway');
  });

  it('keeps provisioning idempotent when RPC and sponsorship are unavailable', async () => {
    const { userId, wallet, token } = await provisionCaller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/provision',
      headers: { authorization: `Bearer ${token}` },
    });
    expect(response.statusCode).toBe(200);
    expect(response.json().wallet).toMatchObject(wallet);
    const { rows } = await poolModule
      .getPool()
      .query<{ count: string }>(
        "select count(*)::text as count from wallets where user_id = $1 and kind = 'standard'",
        [userId]
      );
    expect(rows).toEqual([{ count: '1' }]);
  });

  it('refuses a caller requesting another wallet identity', async () => {
    const caller = await provisionCaller();
    const other = await provisionCaller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${caller.token}` },
      payload: await statusRequest(other.wallet.address),
    });
    expect(response.statusCode).toBe(403);
    expect(response.json()).toEqual({ error: 'wallet_owner_mismatch', status: 'pending' });
  });

  it('refuses sponsorship status when the chain identity cannot be read', async () => {
    const { wallet, token } = await provisionCaller();
    const response = await sponsorApp.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${token}` },
      payload: await statusRequest(wallet.address),
    });
    expect(response.statusCode).toBe(503);
    expect(response.json()).toEqual({ error: 'wallet_identity_unavailable', status: 'pending' });
  });

  it('requires authentication before processing sponsored submissions', async () => {
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/submit',
      payload: {},
    });
    expect(response.statusCode).toBe(401);
    expect(response.json()).toEqual({ error: 'missing or malformed Authorization header' });
  });

  it('refuses an authenticated malformed sponsored submission', async () => {
    const { token } = await provisionCaller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/submit',
      headers: { authorization: `Bearer ${token}` },
      payload: {},
    });
    expect(response.statusCode).toBe(400);
    expect(response.json().error).toBe('invalid_body');
    expect(response.json().issues.map((issue: { path: string[] }) => issue.path)).toEqual([
      ['chain_id'],
      ['account'],
      ['to'],
      ['data'],
      ['value'],
      ['construction'],
      ['account_signature'],
      ['relayer_signature'],
      ['authorization'],
      ['quote_decimals'],
    ]);
  });
});
