import { randomUUID } from 'node:crypto';
import Fastify, { type FastifyInstance } from 'fastify';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';

let identity: IdentityProvider;
let database: EphemeralPostgres;
let app: FastifyInstance;
let configuration: typeof import('../src/env.js');
let wallets: typeof import('../src/db/wallets.js');
let pool: typeof import('../src/db/pool.js');

beforeAll(async () => {
  delete process.env.SPONSOR_PRIVATE_KEY_FILE;
  delete process.env.WALLET_GAS_STATION_URL;
  identity = await startIdentityProvider();
  process.env.SUPABASE_URL = identity.url;
  database = await startPostgres();
  configuration = await import('../src/env.js');
  wallets = await import('../src/db/wallets.js');
  pool = await import('../src/db/pool.js');
  const { walletSponsorRoutes } = await import('../src/routes/wallet.js');
  app = Fastify();
  await app.register(walletSponsorRoutes);
  await app.ready();
}, 120_000);

afterAll(async () => {
  try {
    await app?.close();
  } finally {
    try {
      await pool?.closePool();
    } finally {
      try {
        await database?.stop();
      } finally {
        await identity?.stop();
      }
    }
  }
}, 120_000);

async function caller() {
  const userId = randomUUID();
  const { wallet } = await wallets.provisionWalletForUser(userId);
  const token = await identity.mintUserToken(userId);
  const relayer = privateKeyToAccount(generatePrivateKey());
  const status = {
    account: wallet.address,
    sponsor: relayer.address,
    quoteNonce: '0',
    relayerSignature: await relayer.signMessage({ message: wallet.address }),
  };
  return { wallet, token, status };
}

describe('routes sponsorship through the gas station', () => {
  it('starts without a sponsor key and keeps the sponsored route paths', () => {
    expect(configuration.loadEnv()).toBe(configuration.env);
    expect(Object.hasOwn(configuration.env, 'SPONSOR_PRIVATE_KEY_FILE')).toBe(false);
    expect(configuration.env.WALLET_GAS_STATION_URL).toBeUndefined();
    expect(app.hasRoute({ method: 'POST', url: '/v1/wallet/sponsored/submit' })).toBe(true);
    expect(app.hasRoute({ method: 'POST', url: '/v1/wallet/sponsored/status' })).toBe(true);
  });

  it('discards the retired sponsor key input instead of validating or reading it', async () => {
    process.env.SPONSOR_PRIVATE_KEY_FILE = '';
    try {
      vi.resetModules();
      const { env } = await import('../src/env.js');
      expect(Object.hasOwn(env, 'SPONSOR_PRIVATE_KEY_FILE')).toBe(false);
      expect(env.WALLET_GAS_STATION_URL).toBeUndefined();
    } finally {
      delete process.env.SPONSOR_PRIVATE_KEY_FILE;
    }
  });

  it('preserves the relayer endpoint without adding a sponsor key', async () => {
    const endpoint = new URL('/quote', identity.url).href;
    process.env.WALLET_GAS_STATION_URL = endpoint;
    try {
      vi.resetModules();
      const { env } = await import('../src/env.js');
      expect(env.WALLET_GAS_STATION_URL).toBe(endpoint);
      expect(Object.hasOwn(env, 'SPONSOR_PRIVATE_KEY_FILE')).toBe(false);
    } finally {
      delete process.env.WALLET_GAS_STATION_URL;
    }
  });

  it.each(['submit', 'status'] as const)(
    'requires authentication before sponsored %s',
    async (operation) => {
      const response = await app.inject({
        method: 'POST',
        url: `/v1/wallet/sponsored/${operation}`,
        payload: {},
      });
      expect(response.statusCode).toBe(401);
      expect(response.json()).toEqual({ error: 'missing or malformed Authorization header' });
    }
  );

  it('refuses an unconfigured relayer without falling back to a gateway sponsor', async () => {
    const owner = await caller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${owner.token}` },
      payload: owner.status,
    });
    expect(response.statusCode).toBe(503);
    expect(response.json()).toEqual({ error: 'gas_station_unconfigured', status: 'pending' });
  });

  it('refuses a caller with no wallet before consulting the relayer', async () => {
    const owner = await caller();
    const token = await identity.mintUserToken(randomUUID());
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${token}` },
      payload: owner.status,
    });
    expect(response.statusCode).toBe(403);
    expect(response.json()).toEqual({ error: 'wallet_owner_mismatch', status: 'pending' });
  });

  it('refuses sponsorship for another wallet before consulting the relayer', async () => {
    const owner = await caller();
    const other = await caller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/status',
      headers: { authorization: `Bearer ${owner.token}` },
      payload: other.status,
    });
    expect(response.statusCode).toBe(403);
    expect(response.json()).toEqual({ error: 'wallet_owner_mismatch', status: 'pending' });
  });

  it('requires the complete sponsored construction for an authenticated submit', async () => {
    const owner = await caller();
    const response = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sponsored/submit',
      headers: { authorization: `Bearer ${owner.token}` },
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
