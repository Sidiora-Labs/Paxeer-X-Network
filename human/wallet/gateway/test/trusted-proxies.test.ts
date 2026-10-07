import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';

let pg: EphemeralPostgres;
let app: FastifyInstance;
let poolModule: typeof import('../src/db/pool.js');
let envModule: typeof import('../src/env.js');

beforeAll(async () => {
  process.env.LAYERX_TRUSTED_PROXIES = '127.0.0.1/32, 10.0.0.0/8, fd00::/8';
  process.env.LOG_LEVEL = 'error';
  pg = await startPostgres();
  envModule = await import('../src/env.js');
  poolModule = await import('../src/db/pool.js');
  const index = await import('../src/index.js');
  app = await index.buildApp();
  app.get('/client-ip', async (req) => ({ ip: req.ip }));
  await app.ready();
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
}, 120_000);

describe('LAYERX_TRUSTED_PROXIES parsing', () => {
  it('reads the configured list from the environment', () => {
    expect(envModule.env.LAYERX_TRUSTED_PROXIES).toEqual(['127.0.0.1/32', '10.0.0.0/8', 'fd00::/8']);
  });

  it('accepts bare addresses and CIDR ranges of both families', () => {
    expect(envModule.parseTrustedProxies(' 100.64.0.1 ,::1,fd12:3456::/48,')).toEqual([
      '100.64.0.1',
      '::1',
      'fd12:3456::/48',
    ]);
    expect(envModule.parseTrustedProxies('')).toEqual([]);
  });

  it('refuses names, bad prefixes and malformed entries', () => {
    for (const bad of ['wallet-gateway.example', '10.0.0.0/33', 'fd00::/129', '10.0.0.0/8/1', '10.0.0.0/', '10.0.0.0/x']) {
      expect(() => envModule.parseTrustedProxies(bad)).toThrow(/LAYERX_TRUSTED_PROXIES/);
    }
  });
});

describe('client address behind trusted proxies', () => {
  it('takes the rightmost untrusted X-Forwarded-For hop', async () => {
    const res = await app.inject({
      method: 'GET',
      url: '/client-ip',
      remoteAddress: '127.0.0.1',
      headers: { 'x-forwarded-for': '198.51.100.7, 203.0.113.9, 10.1.2.3' },
    });
    expect(res.json()).toEqual({ ip: '203.0.113.9' });
  });

  it('ignores X-Forwarded-For from a peer that is not a trusted proxy', async () => {
    const res = await app.inject({
      method: 'GET',
      url: '/client-ip',
      remoteAddress: '192.0.2.44',
      headers: { 'x-forwarded-for': '203.0.113.9' },
    });
    expect(res.json()).toEqual({ ip: '192.0.2.44' });
  });

  it('answers the plain health path', async () => {
    const res = await app.inject({ method: 'GET', url: '/healthz' });
    expect(res.statusCode).toBe(200);
    expect(res.json()).toMatchObject({ ok: true });
  });
});
