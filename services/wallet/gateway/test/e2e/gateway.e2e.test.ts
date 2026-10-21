import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { randomBytes, randomUUID } from 'node:crypto';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { FastifyInstance } from 'fastify';
import {
  parseTransaction,
  recoverMessageAddress,
  recoverTransactionAddress,
  recoverTypedDataAddress,
  type Hex,
  type TypedDataDefinition,
} from 'viem';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { startPostgres, type EphemeralPostgres } from '../support/postgres.js';
import { ADDR, TestChain, chainMainAccountId } from '../support/chain.js';
import {
  CHAIN_ID,
  NATIVE_PER_TX_CAP_WEI,
  startAttestorNetwork,
  startIdentity,
  type AttestorNetwork,
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
let chain: TestChain;
let workDir: string;
let sponsorAddress: string;
const userId = randomUUID();
let token: string;
let key: { keyId: string; address: `0x${string}` };

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
  chain = new TestChain(CHAIN_ID);
  await chain.start();
  network = await startAttestorNetwork({ identity, rpcUrl: chain.url });

  workDir = mkdtempSync(join(tmpdir(), 'gateway-e2e-sponsor-'));
  const sponsorKey = generatePrivateKey();
  sponsorAddress = privateKeyToAccount(sponsorKey).address.toLowerCase();
  chain.balances.set(sponsorAddress, 10n ** 21n);
  const sponsorFile = join(workDir, 'sponsor.key');
  writeFileSync(sponsorFile, sponsorKey.slice(2), { mode: 0o600 });

  process.env.SUPABASE_URL = identity.url;
  process.env.HYPERPAXEER_RPC_URL = chain.url;
  process.env.HYPERPAXEER_CHAIN_ID = String(CHAIN_ID);
  process.env.RPC_URLS = chain.url;
  process.env.SPONSOR_PRIVATE_KEY_FILE = sponsorFile;
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
  expect(provisioned.statusCode, provisioned.body).toBe(200);
  const body = provisioned.json() as { wallet: { address: `0x${string}`; did: string; main_account_id: string; binding_state: string }; provisioning: { state: string } };
  expect(body.provisioning.state).toBe('active');
  expect(body.wallet.binding_state).toBe('bound');
  expect(body.wallet.address).toMatch(/^0x[0-9a-fA-F]{40}$/);
  const address = body.wallet.address.toLowerCase();
  const pub = chain.bindings.get(address)!;
  expect(pub).toMatch(/^[0-9a-f]{64}$/);
  expect(body.wallet.did).toBe(`did:layerx:${pub}`);
  expect(body.wallet.main_account_id).toBe(chainMainAccountId(pub));
  expect(chain.bindNonces.get(address)).toBe(1n);
  expect(chain.sent.filter((t) => t.to === address).map((t) => t.from)).toEqual([sponsorAddress]);
  const binds = chain.sent.filter((t) => t.from === address);
  expect(binds.map((t) => t.to)).toEqual([ADDR]);
  expect(chain.receipts.get(binds[0]!.hash)!.status).toBe('0x1');

  const { getPool } = await import('../../src/db/pool.js');
  const { rows } = await getPool().query<{ attestor_key_id: string; migrated_at: Date | null; encrypted_private_key: string | null }>(
    `select attestor_key_id, migrated_at, encrypted_private_key from wallets where user_id = $1 and kind = 'standard'`,
    [userId],
  );
  expect(rows).toHaveLength(1);
  expect(rows[0]!.attestor_key_id).toBe(`wallet:${userId}:standard:secp256k1:0`);
  expect(rows[0]!.migrated_at).not.toBeNull();
  expect(rows[0]!.encrypted_private_key).toBeNull();
  key = { keyId: rows[0]!.attestor_key_id, address: body.wallet.address };
  chain.balances.set(address, (chain.balances.get(address) ?? 0n) + NATIVE_PER_TX_CAP_WEI * 2n);
}, 900_000);

afterAll(async () => {
  await app?.close();
  const { provisionDepsFromEnv } = await import('../../src/routes/wallet.js');
  if (app) provisionDepsFromEnv()?.attestors?.close();
  const { closePool } = await import('../../src/db/pool.js');
  await closePool();
  await pg?.stop();
  await network?.stop();
  await identity?.close();
  await chain?.stop();
  if (workDir) rmSync(workDir, { recursive: true, force: true });
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
    expect(body.components.rpc_pool.endpoints).toEqual([expect.objectContaining({ url: chain.url, state: 'healthy' })]);
    expect(body.components.identity_provider).toMatchObject({ state: 'up', keys: 1 });
  });

  it('answers the provisioned wallet with the attestor key address', async () => {
    const res = await app.inject({ method: 'GET', url: '/v1/wallet/me', headers: auth(token) });
    expect(res.statusCode).toBe(200);
    expect(res.json().wallet.address.toLowerCase()).toBe(key.address.toLowerCase());
  });

  it('signs a transaction through the quorum and the signature recovers the wallet', async () => {
    const res = await app.inject({ method: 'POST', url: '/v1/wallet/sign', headers: auth(await identity.mint(userId)), payload: txBody(1000n) });
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
    const res = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers: auth(await identity.mint(userId)), payload: { message } });
    const body = res.json();
    expect(res.statusCode, JSON.stringify(body)).toBe(200);
    const recovered = await recoverMessageAddress({ message, signature: body.signature as Hex });
    expect(recovered.toLowerCase()).toBe(key.address.toLowerCase());
  }, 300_000);

  it('signs typed data and the signature recovers the wallet', async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/wallet/sign-typed-data',
      headers: auth(await identity.mint(userId)),
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
      headers: auth(await identity.mint(userId)),
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

  it('mounts custody and constrained digest adapters under real wallet authentication', async () => {
    for (const url of ['/v1/wallet/sign-custody', '/v1/wallet/sign-digest']) {
      const unauthenticated = await app.inject({ method: 'POST', url, payload: {} });
      expect(unauthenticated.statusCode).toBe(401);
    }
  });

  it('refuses opaque custody bytes and bare digest before requesting a signing session', async () => {
    const headers = auth(await identity.mint(userId));
    const custody = await app.inject({ method: 'POST', url: '/v1/wallet/sign-custody', headers,
      payload: { custody: '0x4c583a435553544f44593a7631' + '00'.repeat(32) } });
    expect(custody.statusCode).toBe(400);
    const digest = await app.inject({ method: 'POST', url: '/v1/wallet/sign-digest', headers,
      payload: { digest: '0x' + '01'.repeat(32) } });
    expect(digest.statusCode).toBe(400);
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


describe('wallet custody boundary against the gateway database', () => {
  it('refuses a migrated row before touching its retained legacy envelope', async () => {
    const { findWalletByUserId, getSigningAccountForRow, WalletMigratedError } = await import('../../src/db/wallets.js');
    const row = await findWalletByUserId(userId);
    expect(row).not.toBeNull();
    await expect(getSigningAccountForRow(row!)).rejects.toBeInstanceOf(WalletMigratedError);
  });

  it('rechecks custody when an already-created legacy signing handle is used', async () => {
    const { provisionWalletForUser, getSigningAccountForRow, WalletMigratedError } = await import('../../src/db/wallets.js');
    const { getPool } = await import('../../src/db/pool.js');
    const { row } = await provisionWalletForUser(randomUUID(), 'standard');
    const account = await getSigningAccountForRow(row);
    await getPool().query('update wallets set migrated_at = now(), attestor_key_id = $2 where id = $1', [row.id, key.keyId]);
    await expect(account.signMessage({ message: 'migration must revoke this stale handle' })).rejects.toBeInstanceOf(WalletMigratedError);
    const { rows } = await getPool().query('select encrypted_private_key from wallets where id = $1', [row.id]);
    expect(rows[0].encrypted_private_key).not.toBeNull();
  });

  it('refuses null-envelope legacy signing without decrypting', async () => {
    const { provisionWalletForUser, getSigningAccountForRow, WalletMigratedError } = await import('../../src/db/wallets.js');
    const { getPool } = await import('../../src/db/pool.js');
    const { row } = await provisionWalletForUser(randomUUID(), 'standard');
    await getPool().query('update wallets set encrypted_private_key = null where id = $1', [row.id]);
    await expect(getSigningAccountForRow(row)).rejects.toBeInstanceOf(WalletMigratedError);
  });
});
