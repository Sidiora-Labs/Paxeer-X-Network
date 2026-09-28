import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { randomBytes, randomUUID } from 'node:crypto';
import { createServer as createHttpServer, type Server as HttpServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { FastifyInstance } from 'fastify';
import {
  keccak256,
  parseTransaction,
  recoverMessageAddress,
  recoverTransactionAddress,
  recoverTypedDataAddress,
  type Hex,
  type TypedDataDefinition,
} from 'viem';
import { startPostgres, type EphemeralPostgres } from '../support/postgres.js';
import {
  CHAIN_ID,
  NATIVE_PER_TX_CAP_WEI,
  startAttestorNetwork,
  startIdentity,
  type AttestorNetwork,
  type GeneratedKey,
  type Identity,
} from './attestors.js';

const RECIPIENT = '0x000000000000000000000000000000000000c0de';

const typedData = {
  domain: { name: 'Ether Mail', version: '1', chainId: CHAIN_ID, verifyingContract: '0xcccccccccccccccccccccccccccccccccccccccc' },
  types: {
    Person: [
      { name: 'name', type: 'string' },
      { name: 'wallet', type: 'address' },
    ],
    Mail: [
      { name: 'from', type: 'Person' },
      { name: 'to', type: 'Person' },
      { name: 'contents', type: 'string' },
    ],
  },
  primaryType: 'Mail',
  message: {
    from: { name: 'Cow', wallet: '0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826' },
    to: { name: 'Bob', wallet: '0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB' },
    contents: 'Hello, Bob!',
  },
} as const;

let identity: Identity;
let network: AttestorNetwork;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let rpcServer: HttpServer;
let rpcUrl: string;
const userId = randomUUID();
let token: string;
let key: GeneratedKey;

function startChainRpc(): Promise<void> {
  rpcServer = createHttpServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
      const reply = (payload: Record<string, unknown>): void => {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, ...payload }));
      };
      switch (body.method) {
        case 'eth_chainId':
          return reply({ result: `0x${CHAIN_ID.toString(16)}` });
        case 'eth_blockNumber':
          return reply({ result: '0x3e8' });
        case 'eth_call':
          return reply({ result: '0x' });
        case 'eth_estimateGas':
          return reply({ result: '0x5208' });
        case 'eth_getTransactionCount':
          return reply({ result: '0x0' });
        case 'eth_sendRawTransaction':
          return reply({ result: keccak256(body.params[0] as Hex) });
        default:
          return reply({ error: { code: -32601, message: 'method not found' } });
      }
    });
  });
  return new Promise((r) =>
    rpcServer.listen(0, '127.0.0.1', () => {
      rpcUrl = `http://127.0.0.1:${(rpcServer.address() as AddressInfo).port}`;
      r();
    }),
  );
}

const auth = (t: string) => ({ authorization: `Bearer ${t}` });

const txBody = (value: bigint) => ({
  tx: {
    to: RECIPIENT,
    value: value.toString(),
    gas: '21000',
    maxFeePerGas: '2000000000',
    maxPriorityFeePerGas: '1000000000',
    chainId: CHAIN_ID,
  },
});

beforeAll(async () => {
  identity = await startIdentity();
  network = await startAttestorNetwork({ identity });
  await startChainRpc();

  process.env.SUPABASE_URL = identity.url;
  process.env.HYPERPAXEER_RPC_URL = rpcUrl;
  process.env.HYPERPAXEER_CHAIN_ID = String(CHAIN_ID);
  process.env.RPC_URLS = rpcUrl;
  process.env.ATTESTOR_ENDPOINTS = network.nodes.map((n) => n.apiUrl).join(',');
  process.env.ATTESTOR_CLIENT_CERT_FILE = network.pki.clientCert;
  process.env.ATTESTOR_CLIENT_KEY_FILE = network.pki.clientKey;
  process.env.ATTESTOR_CA_FILE = network.pki.caFile;
  process.env.ATTESTOR_QUORUM = '3';
  process.env.ATTESTOR_HEALTH_INTERVAL_MS = '1000';
  process.env.ATTESTOR_TIMEOUT_MS = '300000';
  process.env.POLICY_MAX_TX_VALUE_WEI = (NATIVE_PER_TX_CAP_WEI * 10n).toString();
  process.env.POLICY_MAX_DAILY_VALUE_WEI = (NATIVE_PER_TX_CAP_WEI * 100n).toString();
  process.env.AGENT_JWT_SECRET = randomBytes(48).toString('base64url');
  process.env.LOG_LEVEL = 'warn';

  pg = await startPostgres();
  const { buildApp } = await import('../../src/index.js');
  app = await buildApp();
  await app.ready();

  token = await identity.mint(userId);
  const provisioned = await app.inject({ method: 'POST', url: '/v1/wallet/provision', headers: auth(token) });
  expect(provisioned.statusCode).toBe(200);

  key = await network.generate(`wallet:${userId}:secp256k1`, userId);
  expect(key.address).toMatch(/^0x[0-9a-fA-F]{40}$/);
  const { getPool } = await import('../../src/db/pool.js');
  const marked = await getPool().query(
    `update wallets set address = $1, attestor_key_id = $2, migrated_at = now() where user_id = $3 and kind = 'standard'`,
    [key.address, key.keyId, userId],
  );
  expect(marked.rowCount).toBe(1);
}, 900_000);

afterAll(async () => {
  await app?.close();
  const { closePool } = await import('../../src/db/pool.js');
  await closePool();
  await pg?.stop();
  await network?.stop();
  await identity?.close();
  await new Promise<void>((r) => (rpcServer ? rpcServer.close(() => r()) : r()));
}, 120_000);

describe('gateway against five real attestor daemons', () => {
  it('reports every readiness component up', async () => {
    const res = await app.inject({ method: 'GET', url: '/readyz' });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(200);
    expect(body.ready).toBe(true);
    expect(body.components.attestors).toMatchObject({ state: 'up', required: 3, healthy: 5 });
    expect(body.components.nonce_store.state).toBe('up');
    expect(body.components.rpc_pool).toMatchObject({ state: 'up', healthy: 1 });
    expect(body.components.rpc_pool.endpoints).toEqual([expect.objectContaining({ url: rpcUrl, state: 'healthy' })]);
    expect(body.components.identity_provider).toMatchObject({ state: 'up', keys: 1 });
  });

  it('answers the provisioned wallet with the attestor key address', async () => {
    const res = await app.inject({ method: 'GET', url: '/v1/wallet/me', headers: auth(token) });
    expect(res.statusCode).toBe(200);
    expect(res.json().wallet.address.toLowerCase()).toBe(key.address.toLowerCase());
  });

  it('signs a transaction through the quorum and the signature recovers the wallet', async () => {
    const res = await app.inject({ method: 'POST', url: '/v1/wallet/sign', headers: auth(token), payload: txBody(1000n) });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(200);
    const signed = body.signed_tx as Hex;
    const tx = parseTransaction(signed);
    expect(tx.chainId).toBe(CHAIN_ID);
    expect(tx.to?.toLowerCase()).toBe(RECIPIENT);
    expect(tx.value).toBe(1000n);
    const recovered = await recoverTransactionAddress({ serializedTransaction: signed as never });
    expect(recovered.toLowerCase()).toBe(key.address.toLowerCase());

    const { getPool } = await import('../../src/db/pool.js');
    const { rows } = await getPool().query<{ path: string; decision: string; attestor_audit: Array<{ node_id: string; audit_sequence: number }> }>(
      `select path, decision, attestor_audit from signing_audit where client_subject = $1 and route = '/v1/wallet/sign' order by id desc limit 1`,
      [userId],
    );
    expect(rows[0]).toMatchObject({ path: 'attestor', decision: 'signed' });
    expect(rows[0]!.attestor_audit).toHaveLength(3);
    for (const entry of rows[0]!.attestor_audit) {
      expect(network.nodes.map((n) => n.nodeId)).toContain(entry.node_id);
      expect(entry.audit_sequence).toBeGreaterThan(0);
    }
  }, 300_000);

  it('signs a personal message and the signature recovers the wallet', async () => {
    const message = `sign in to the wallet ${randomUUID()}`;
    const res = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers: auth(token), payload: { message } });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(200);
    const recovered = await recoverMessageAddress({ message, signature: body.signature as Hex });
    expect(recovered.toLowerCase()).toBe(key.address.toLowerCase());
  }, 300_000);

  it('signs typed data and the signature recovers the wallet', async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sign-typed-data',
      headers: auth(token),
      payload: { typed_data: typedData },
    });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(200);
    const recovered = await recoverTypedDataAddress({
      ...(typedData as unknown as TypedDataDefinition),
      signature: body.signature as Hex,
    });
    expect(recovered.toLowerCase()).toBe(key.address.toLowerCase());
  }, 300_000);

  it('refuses a token signed by a key outside the identity provider', async () => {
    const forged = await identity.mintForeign(userId);
    const res = await app.inject({ method: 'POST', url: '/v1/wallet/sign', headers: auth(forged), payload: txBody(1000n) });
    expect(res.statusCode).toBe(401);
    expect(res.json().error).toBe('unauthorized');
  });

  it('refuses a transaction over the attestor policy cap', async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sign',
      headers: auth(token),
      payload: txBody(NATIVE_PER_TX_CAP_WEI + 1n),
    });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(403);
    expect(body).toMatchObject({ error: 'attestor_policy_refused', category: 'policy', code: 'policy_denied' });
  }, 300_000);

  it('refuses an unsigned agent write', async () => {
    const res = await app.inject({ method: 'POST', url: '/v1/agent/send', payload: txBody(1000n) });
    expect(res.statusCode).toBe(401);
    expect(res.json().error).toBe('agent_signature_required');
  });

  it('answers not ready with a typed body once the attestor quorum is lost', async () => {
    for (const node of network.nodes.slice(0, 3)) await network.stopNode(node);
    const res = await app.inject({ method: 'GET', url: '/readyz' });
    const body = res.json();
    expect(res.statusCode).toBe(503);
    expect(body.error).toBe('not_ready');
    expect(body.ready).toBe(false);
    expect(body.components.attestors).toMatchObject({ state: 'down', required: 3, reason: 'attestor_quorum_unavailable' });
    expect(body.components.attestors.healthy).toBeLessThan(3);
    expect(body.components.nonce_store.state).toBe('up');
    expect(body.components.rpc_pool.state).toBe('up');
    expect(body.components.identity_provider.state).toBe('up');
  }, 120_000);
});
