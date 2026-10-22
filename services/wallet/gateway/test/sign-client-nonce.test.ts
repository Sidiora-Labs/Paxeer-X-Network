import { randomBytes, randomUUID } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { keccak256 } from 'viem';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let rpcServer: Server;
let latestCount = 7;
let poolModule: typeof import('../src/db/pool.js');

beforeAll(async () => {
  rpcServer = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string };
      const result = body.method === 'eth_getTransactionCount' ? `0x${latestCount.toString(16)}`
        : body.method === 'eth_blockNumber' ? '0x64'
          : body.method === 'eth_call' ? '0x'
            : body.method === 'eth_getTransactionByHash' ? null : undefined;
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify(result === undefined
        ? { jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }
        : { jsonrpc: '2.0', id: body.id, result }));
    });
  });
  await new Promise<void>((r) => rpcServer.listen(0, '127.0.0.1', () => r()));
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `http://127.0.0.1:${(rpcServer.address() as AddressInfo).port}`;
  pg = await startPostgres();
  const index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  app = await index.buildApp();
  await app.ready();
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
  await new Promise<void>((r) => (rpcServer ? rpcServer.close(() => r()) : r()));
}, 120_000);

async function asUser(userId: string, url: string, payload: Record<string, unknown>) {
  return app.inject({ method: 'POST', url, headers: { authorization: `Bearer ${await idp.mintUserToken(userId)}` }, payload });
}

const tx = (nonce?: number) => ({
  to: '0x00000000000000000000000000000000000000e1', value: '1', gas: '21000', maxFeePerGas: '2000000000',
  maxPriorityFeePerGas: '1000000000', ...(nonce === undefined ? {} : { nonce }),
});

describe('/v1/wallet/sign with a client nonce', () => {
  it('reserves the client nonce with the signed hash, refuses it while outstanding and below latest, and returns the broadcast deadline', async () => {
    const user = randomUUID();
    const provisioned = await asUser(user, '/v1/wallet/provision', {});
    expect(provisioned.statusCode).toBe(200);
    const { address } = (provisioned.json() as { wallet: { address: string } }).wallet;
    latestCount = 7;
    const before = Date.now();
    const signed = await asUser(user, '/v1/wallet/sign', { tx: tx(9) });
    expect(signed.statusCode).toBe(200);
    const body = signed.json() as { signed_tx: `0x${string}`; nonce: number; broadcast_deadline: string };
    expect(body.nonce).toBe(9);
    const { BROADCAST_GRACE_MS } = await import('../src/chain.js');
    expect(Date.parse(body.broadcast_deadline)).toBeGreaterThanOrEqual(before + BROADCAST_GRACE_MS);
    const { rows } = await poolModule.query<{ nonce: string; tx_hash: string }>(
      'select nonce::text as nonce, tx_hash from nonce_outstanding where address = $1', [address.toLowerCase()]);
    expect(rows).toEqual([{ nonce: '9', tx_hash: keccak256(body.signed_tx) }]);
    const again = await asUser(user, '/v1/wallet/sign', { tx: tx(9) });
    expect(again.statusCode).toBe(409);
    expect(again.json()).toEqual({ error: 'nonce_outstanding', nonce: 9, request_id: expect.any(String) });
    const below = await asUser(user, '/v1/wallet/sign', { tx: tx(6) });
    expect(below.statusCode).toBe(409);
    expect(below.json()).toEqual({ error: 'nonce_below_latest', nonce: 6, latest: 7, request_id: expect.any(String) });
    const gateway = await asUser(user, '/v1/wallet/sign', { tx: tx() });
    expect(gateway.statusCode).toBe(200);
    expect((gateway.json() as { nonce: number }).nonce).toBe(7);
  });
});
