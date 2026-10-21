import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { createHash, randomBytes, randomUUID, X509Certificate } from 'node:crypto';
import { request as httpsRequest } from 'node:https';
import { createServer as createNetServer } from 'node:net';
import { mkdtempSync, openSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { decodeFunctionData } from 'viem';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { newAgentKey, startIdentityProvider, type IdentityProvider } from './support/identity.js';
import { ADDR, BIND_SELECTOR, TestChain, addrAbiJson, chainMainAccountId, repoRoot } from './support/chain.js';
import { KERNEL_POLICY } from './e2e/attestors.js';

const here = dirname(fileURLToPath(import.meta.url));
const attestorDir = resolve(here, '../../attestor');
const bindVectors = JSON.parse(readFileSync(join(repoRoot, 'layerxproof/testdata/paxeer_bind_vectors.json'), 'utf8')) as {
  public_key: string;
  did: string;
  main_account_name: string;
  main_account_id: string;
  binds: Array<{ name: string; chain_id: string; evm_address: string; nonce: string; message: string; signature: string; valid: boolean }>;
};

const CHAIN_ID = 125;
const LONG = 900_000;

function freePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const srv = createNetServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (!addr || typeof addr === 'string') return reject(new Error('no port'));
      srv.close(() => resolvePort(addr.port));
    });
  });
}

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let chain: TestChain;
let workDir: string;
let daemons: ChildProcess[] = [];
let sponsorAddress: string;
let poolModule: typeof import('../src/db/pool.js');
let walletsModule: typeof import('../src/db/wallets.js');
let bindModule: typeof import('../src/provision/bind.js');
let stateModule: typeof import('../src/provision/state.js');
let backfillModule: typeof import('../src/provision/backfill.js');
let routesModule: typeof import('../src/routes/wallet.js');
let clientTls: { cert: Buffer; key: Buffer; ca: Buffer };
const endpoints: string[] = [];

function ossl(args: string[]): void {
  execFileSync('openssl', args, { stdio: 'pipe' });
}

function issueCertificates(dir: string, names: string[]): void {
  const p = (n: string): string => join(dir, n);
  const ec = ['-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1', '-nodes'];
  ossl(['req', '-x509', ...ec, '-keyout', p('ca.key'), '-out', p('ca.crt'), '-days', '1', '-subj', '/CN=provisioning-test-ca']);
  writeFileSync(p('leaf.ext'), 'subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth,clientAuth\n');
  for (const name of names) {
    ossl(['req', ...ec, '-keyout', p(`${name}.key`), '-out', p(`${name}.csr`), '-subj', `/CN=${name}`]);
    ossl(['x509', '-req', '-in', p(`${name}.csr`), '-CA', p('ca.crt'), '-CAkey', p('ca.key'), '-CAcreateserial', '-out', p(`${name}.crt`), '-days', '1', '-extfile', p('leaf.ext')]);
  }
}

function spkiPin(certPath: string): string {
  const cert = new X509Certificate(readFileSync(certPath));
  return createHash('sha256').update(cert.publicKey.export({ type: 'spki', format: 'der' })).digest('hex');
}

function health(url: string): Promise<{ ready: boolean }> {
  return new Promise((resolveHealth) => {
    const req = httpsRequest(new URL('/health', url), { method: 'GET', ...clientTls, timeout: 5_000 }, (res) => {
      const chunks: Buffer[] = [];
      res.on('data', (c: Buffer) => chunks.push(c));
      res.on('end', () => {
        try {
          resolveHealth(JSON.parse(Buffer.concat(chunks).toString('utf8')) as { ready: boolean });
        } catch {
          resolveHealth({ ready: false });
        }
      });
    });
    req.on('error', () => resolveHealth({ ready: false }));
    req.on('timeout', () => req.destroy());
    req.end();
  });
}

beforeAll(async () => {
  workDir = mkdtempSync(join(tmpdir(), 'provisioning-'));
  const bin = join(workDir, 'attestor');
  execFileSync('go', ['build', '-o', bin, './cmd/attestor'], { cwd: attestorDir, stdio: 'pipe', timeout: 600_000 });

  idp = await startIdentityProvider();
  chain = new TestChain(CHAIN_ID);
  await chain.start();

  const nodes = ['node-1', 'node-2', 'node-3', 'node-4', 'node-5'];
  issueCertificates(workDir, [...nodes, 'gateway']);
  clientTls = {
    cert: readFileSync(join(workDir, 'gateway.crt')),
    key: readFileSync(join(workDir, 'gateway.key')),
    ca: readFileSync(join(workDir, 'ca.crt')),
  };
  const policyFile = join(workDir, 'policy.json');
  writeFileSync(
    policyFile,
    JSON.stringify({
      version: 1,
      defaults: {
        chain_id: CHAIN_ID,
        kinds: ['evm_tx', 'lx_bind'],
        rate_per_minute: 1000,
        caps: { native: { per_transaction: '1000000000000000000', daily: '10000000000000000000' } },
        selectors: { [ADDR]: [BIND_SELECTOR] },
      },
    }),
  );
  const kernelPolicyFile = join(workDir, 'kernel-policy.json');
  writeFileSync(kernelPolicyFile, JSON.stringify(KERNEL_POLICY));
  const apiPorts = await Promise.all(nodes.map(() => freePort()));
  const peerPorts = await Promise.all(nodes.map(() => freePort()));
  const pins = nodes.map((n) => spkiPin(join(workDir, `${n}.crt`)));
  nodes.forEach((node, i) => {
    const others = nodes.map((n, j) => ({ n, j })).filter((x) => x.j !== i);
    const keyFile = join(workDir, `${node}.store`);
    writeFileSync(keyFile, randomBytes(32).toString('hex'));
    const log = openSync(join(workDir, `${node}.log`), 'w');
    const child = spawn(bin, [], {
      env: {
        PATH: process.env.PATH ?? '',
        ATTESTOR_NODE_ID: node,
        ATTESTOR_REGION: `region-${i + 1}`,
        ATTESTOR_LISTEN_ADDR: `127.0.0.1:${apiPorts[i]}`,
        ATTESTOR_PEER_LISTEN_ADDR: `127.0.0.1:${peerPorts[i]}`,
        ATTESTOR_PEERS: others.map((x) => `${x.n}=127.0.0.1:${peerPorts[x.j]}`).join(','),
        ATTESTOR_PEER_PINS: others.map((x) => `${x.n}=${pins[x.j]}`).join(','),
        ATTESTOR_NODE_KEY_FILE: keyFile,
        ATTESTOR_DATA_DIR: join(workDir, `${node}-data`),
        ATTESTOR_CHAIN_ID: String(CHAIN_ID),
        ATTESTOR_JWKS_URL: `${idp.url}/auth/v1/.well-known/jwks.json`,
        ATTESTOR_JWT_ISSUER: `${idp.url}/auth/v1`,
        ATTESTOR_JWT_AUDIENCE: 'authenticated',
        ATTESTOR_POLICY_FILE: policyFile,
        ATTESTOR_TLS_CERT_FILE: join(workDir, `${node}.crt`),
        ATTESTOR_TLS_KEY_FILE: join(workDir, `${node}.key`),
        ATTESTOR_TLS_CA_FILE: join(workDir, 'ca.crt'),
        ATTESTOR_OPERATOR_CA_FILE: join(workDir, 'ca.crt'),
        ATTESTOR_KERNEL_POLICY_FILE: kernelPolicyFile,
        ATTESTOR_RPC_URL: chain.url,
        ATTESTOR_ACTIVITY_TYPES: '0x10005',
      },
      stdio: ['ignore', log, log],
    });
    daemons.push(child);
    endpoints.push(`https://127.0.0.1:${apiPorts[i]}`);
  });
  const deadline = Date.now() + 120_000;
  for (;;) {
    const states = await Promise.all(endpoints.map((u) => health(u)));
    if (states.every((s) => s.ready)) break;
    if (Date.now() > deadline) {
      const logs = nodes.map((n) => readFileSync(join(workDir, `${n}.log`), 'utf8')).join('\n');
      throw new Error(`attestors did not become ready: ${logs}`);
    }
    await new Promise((r) => setTimeout(r, 300));
  }

  const sponsorKey = generatePrivateKey();
  sponsorAddress = privateKeyToAccount(sponsorKey).address.toLowerCase();
  chain.balances.set(sponsorAddress, 10n ** 21n);
  const sponsorFile = join(workDir, 'sponsor.key');
  writeFileSync(sponsorFile, sponsorKey.slice(2));

  process.env.SUPABASE_URL = idp.url;
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = chain.url;
  process.env.HYPERPAXEER_CHAIN_ID = String(CHAIN_ID);
  process.env.ATTESTOR_ENDPOINTS = endpoints.join(',');
  process.env.ATTESTOR_CLIENT_CERT_FILE = join(workDir, 'gateway.crt');
  process.env.ATTESTOR_CLIENT_KEY_FILE = join(workDir, 'gateway.key');
  process.env.ATTESTOR_CA_FILE = join(workDir, 'ca.crt');
  process.env.ATTESTOR_QUORUM = '3';
  process.env.ATTESTOR_TIMEOUT_MS = String(LONG);
  process.env.SPONSOR_PRIVATE_KEY_FILE = sponsorFile;
  process.env.ACCOUNT_SETUP_GAS_CAP_WEI = '1000000000000000';
  pg = await startPostgres();
  const index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  walletsModule = await import('../src/db/wallets.js');
  bindModule = await import('../src/provision/bind.js');
  stateModule = await import('../src/provision/state.js');
  backfillModule = await import('../src/provision/backfill.js');
  routesModule = await import('../src/routes/wallet.js');
  app = await index.buildApp();
  await app.ready();
}, LONG);

afterAll(async () => {
  await app?.close();
  routesModule?.provisionDepsFromEnv()?.attestors?.close();
  await poolModule?.closePool();
  await pg?.stop();
  for (const d of daemons) d.kill('SIGTERM');
  await chain?.stop();
  await idp?.stop();
  if (workDir) rmSync(workDir, { recursive: true, force: true });
}, 120_000);

async function asUser(userId: string, method: 'GET' | 'POST', url: string) {
  return app.inject({ method, url, headers: { authorization: `Bearer ${await idp.mintUserToken(userId)}` } });
}

function deps() {
  const d = routesModule.provisionDepsFromEnv();
  if (!d) throw new Error('provisioning is not configured');
  return d;
}

async function provisionRow(userId: string, kind = 'standard') {
  const { rows } = await poolModule.getPool().query(
    `select * from account_provisioning where user_id = $1 and kind = $2`,
    [userId, kind],
  );
  return rows[0] as Record<string, unknown>;
}

function sentTo(address: string) {
  return chain.sent.filter((t) => t.to === address.toLowerCase());
}

function sentFrom(address: string) {
  return chain.sent.filter((t) => t.from === address.toLowerCase());
}

describe('provisioning derivations', () => {
  it('provisioning derives the DID, main account and bind message of the published vectors', () => {
    expect(bindModule.didFromPublicKey(bindVectors.public_key)).toBe(bindVectors.did);
    expect(bindModule.mainAccountName(bindVectors.did)).toBe(bindVectors.main_account_name);
    expect(bindModule.mainAccountId(bindVectors.did)).toBe(bindVectors.main_account_id);
    for (const v of bindVectors.binds) {
      const message = bindModule.bindMessage(BigInt(v.chain_id), `0x${v.evm_address}`, BigInt(v.nonce));
      expect(bindModule.verifyEd25519(bindVectors.public_key, message, v.signature)).toBe(v.valid);
      if (v.valid) expect(message.toString('hex')).toBe(v.message);
    }
    const calldata = bindModule.bindLayerXCalldata(bindVectors.public_key, bindVectors.binds[1]!.signature);
    const decoded = decodeFunctionData({ abi: addrAbiJson, data: calldata });
    expect(decoded.functionName).toBe('bindLayerX');
    expect(decoded.args).toEqual([`0x${bindVectors.public_key}`, `0x${bindVectors.binds[1]!.signature}`]);
    expect(calldata.slice(0, 10)).toBe('0xdd9aa628');
  });
});

describe('provisioning flow', () => {
  it('provisioning creates both keys, tops up the exact binding gas and confirms the binding before active', async () => {
    const user = randomUUID();
    const res = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(res.statusCode, res.body).toBe(200);
    const body = res.json() as { wallet: { id: string; address: `0x${string}`; did: string; main_account_id: string; binding_state: string }; provisioning: { state: string } };
    expect(body.provisioning.state).toBe('active');
    expect(body.wallet.binding_state).toBe('bound');
    const address = body.wallet.address.toLowerCase();
    const pub = chain.bindings.get(address)!;
    expect(pub).toMatch(/^[0-9a-f]{64}$/);
    expect(body.wallet.did).toBe(`did:layerx:${pub}`);
    expect(body.wallet.main_account_id).toBe(chainMainAccountId(pub));
    expect(chain.bindNonces.get(address)).toBe(1n);

    const row = await provisionRow(user);
    expect(row.state).toBe('active');
    expect(row.evm_key_id).toBe(`wallet:${user}:standard:secp256k1:0`);
    expect(row.ed_key_id).toBe(`account:${user}:standard:ed25519:0`);
    const required = BigInt(row.bind_gas as string) * BigInt(row.bind_max_fee_wei as string);
    const topups = sentTo(address);
    expect(topups).toHaveLength(1);
    expect(topups[0]!.from).toBe(sponsorAddress);
    expect(topups[0]!.value).toBe(required);
    const binds = sentFrom(address);
    expect(binds).toHaveLength(1);
    expect(binds[0]!.to).toBe(ADDR);
    expect(chain.receipts.get(binds[0]!.hash)!.status).toBe('0x1');
    expect(chain.balances.get(address)).toBe(0n);

    const wallet = await poolModule.getPool().query(
      `select migrated_at, attestor_key_id, encrypted_private_key, did, main_account_id, layerx_key_id, binding_state from wallets where id = $1`,
      [body.wallet.id],
    );
    expect(wallet.rows[0]).toMatchObject({
      attestor_key_id: row.evm_key_id,
      encrypted_private_key: null,
      did: body.wallet.did,
      main_account_id: body.wallet.main_account_id,
      layerx_key_id: row.ed_key_id,
      binding_state: 'bound',
    });
    expect(wallet.rows[0].migrated_at).not.toBeNull();
    const audit = await poolModule.getPool().query(
      `select event, tx_hash, value_wei::text from account_setup_audit where address = $1 order by id`,
      [address],
    );
    expect(audit.rows.map((r) => r.event)).toEqual(['topup', 'bind']);
    expect(audit.rows[0].value_wei).toBe(required.toString());

    const me = await asUser(user, 'GET', '/v1/wallet/me');
    expect(me.statusCode).toBe(200);
    expect(me.json()).toMatchObject({
      wallet: { id: body.wallet.id, address: body.wallet.address, chain_id: CHAIN_ID, did: body.wallet.did, main_account_id: body.wallet.main_account_id, binding_state: 'bound' },
      chain: { id: CHAIN_ID },
      kernel: { state: 'unavailable' },
    });
    expect((me.json() as { wallet: Record<string, unknown> }).wallet.created_at).toBeTruthy();

    const sentBefore = chain.sent.length;
    const again = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(again.statusCode).toBe(200);
    expect(again.json()).toMatchObject({ wallet: { id: body.wallet.id, did: body.wallet.did, binding_state: 'bound' } });
    expect(chain.sent.length).toBe(sentBefore);
  }, LONG);

  it('provisioning resumes after a failure at each step without a second key, top-up or binding', async () => {
    const user = randomUUID();
    const d = deps();
    const token = async () => idp.mintUserToken(user);

    chain.fail('getUnifiedAccount');
    await expect(stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() })).rejects.toThrow(/getUnifiedAccount/);
    let row = await provisionRow(user);
    expect(row.state).toBe('identity');
    const walletId = row.wallet_id as string;
    const did = row.did as string;

    chain.fail('eth_estimateGas');
    await expect(stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() })).rejects.toThrow(/eth_estimateGas/);
    row = await provisionRow(user);
    expect(row.state).toBe('bind_signed');
    expect(row.bind_signature).toMatch(/^[0-9a-f]{128}$/);

    chain.fail('eth_sendRawTransaction', 'apply-then-error');
    chain.fail('eth_getTransactionByHash');
    await expect(stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() })).rejects.toThrow();
    row = await provisionRow(user);
    expect(row.state).toBe('bind_signed');
    expect(row.topup_tx_hash).toMatch(/^0x[0-9a-f]{64}$/);
    const address = (await poolModule.getPool().query(`select address from wallets where id = $1`, [walletId])).rows[0].address.toLowerCase();
    expect(sentTo(address)).toHaveLength(1);

    chain.fail('eth_sendRawTransaction', 'apply-then-error');
    chain.fail('eth_getTransactionByHash');
    await expect(stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() })).rejects.toThrow();
    row = await provisionRow(user);
    expect(row.state).toBe('bind_sent');
    expect(sentTo(address)).toHaveLength(1);
    expect(sentFrom(address)).toHaveLength(1);

    chain.fail('eth_getTransactionReceipt');
    await expect(stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() })).rejects.toThrow(/eth_getTransactionReceipt/);
    const done = await stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() });
    expect(done.state).toBe('active');
    expect(done.walletId).toBe(walletId);
    expect(done.did).toBe(did);

    row = await provisionRow(user);
    expect(row.evm_key_generation).toBe(0);
    expect(row.ed_key_generation).toBe(0);
    expect(sentTo(address)).toHaveLength(1);
    expect(sentFrom(address)).toHaveLength(1);
    expect(chain.bindings.get(address)).toBe(did.slice('did:layerx:'.length));
    expect(chain.bindNonces.get(address)).toBe(1n);
    const topups = await poolModule.getPool().query(`select count(*)::int as n from account_setup_audit where address = $1 and event = 'topup'`, [address]);
    expect(topups.rows[0].n).toBe(1);
    const wallets = await poolModule.getPool().query(`select count(*)::int as n from wallets where user_id = $1`, [user]);
    expect(wallets.rows[0].n).toBe(1);

    const again = await stateModule.provisionAccount(d, { kind: 'standard', userId: user, token: await token() });
    expect(again.state).toBe('active');
    expect(sentFrom(address)).toHaveLength(1);
  }, LONG);

  it('provisioning refuses and records an address already bound to a different DID', async () => {
    const user = randomUUID();
    chain.fail('getUnifiedAccount');
    const first = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(first.statusCode).toBe(500);
    const row = await provisionRow(user);
    expect(row.state).toBe('identity');
    const address = (await poolModule.getPool().query(`select address from wallets where id = $1`, [row.wallet_id])).rows[0].address.toLowerCase();
    const foreign = randomBytes(32).toString('hex');
    chain.bindings.set(address, foreign);
    chain.bindingsByDid.set(foreign, address);

    const refused = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(refused.statusCode).toBe(409);
    expect(refused.json()).toMatchObject({ error: 'binding_refused', bound_did: `did:layerx:${foreign}` });
    const after = await provisionRow(user);
    expect(after.state).toBe('refused');
    expect(after.refusal_reason).toMatch(/different DID/);
    const audit = await poolModule.getPool().query(`select event, outcome from account_setup_audit where address = $1`, [address]);
    expect(audit.rows).toEqual([{ event: 'refusal', outcome: `did:layerx:${foreign}` }]);
    expect(sentTo(address)).toHaveLength(0);
    expect(sentFrom(address)).toHaveLength(0);
    const me = await asUser(user, 'GET', '/v1/wallet/me');
    expect(me.json()).toMatchObject({ wallet: { binding_state: 'refused' } });

    const again = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(again.statusCode).toBe(409);
    expect(chain.bindings.get(address)).toBe(foreign);
  }, LONG);

  it('provisioning backfills migrated and agent wallets in bounded batches and completes each with its own key', async () => {
    const d = deps();
    const pool = poolModule.getPool();

    const owner = randomUUID();
    const evmKeyId = `wallet:${owner}:standard:secp256k1:0`;
    const key = await d.attestors!.generate(evmKeyId, 'secp256k1', owner);
    const migrated = await pool.query(
      `insert into wallets (user_id, address, encrypted_private_key, key_version, chain_id, kind, migrated_at, attestor_key_id)
       values ($1, $2, null, 1, $3, 'standard', now(), $4) returning id::text`,
      [owner, key.address, CHAIN_ID, evmKeyId],
    );
    const migratedId = migrated.rows[0].id as string;

    const legacyUser = randomUUID();
    const legacy = await walletsModule.provisionWalletForUser(legacyUser, 'standard');

    const agent = newAgentKey('provisioning');
    const agentWallet = await walletsModule.provisionWalletForUser(walletsModule.agentWalletUserId(agent.did), 'agent');
    await pool.query(
      `insert into agent_principals (did, label, key_fingerprint, public_key, wallet_id) values ($1, $2, $3, $4, $5)`,
      [agent.did, 'provisioning', agent.publicKeyHex.slice(0, 16), agent.publicKeyHex, agentWallet.row.id],
    );

    const first = await backfillModule.backfillAccounts(d, { batchSize: 1, maxBatches: 1 });
    expect(first.outcomes).toHaveLength(1);
    expect(first.done).toBe(false);
    const rest = await backfillModule.backfillAccounts(d, { batchSize: 1, maxBatches: 10 });
    expect(rest.done).toBe(true);
    const outcomes = [...first.outcomes, ...rest.outcomes];
    expect(outcomes.map((o) => o.wallet_id).sort()).toEqual([migratedId, agentWallet.row.id].sort());
    const byId = new Map(outcomes.map((o) => [o.wallet_id, o]));
    expect(byId.get(migratedId)).toMatchObject({ outcome: 'awaiting_owner', kind: 'standard' });
    expect(byId.get(migratedId)!.did).toMatch(/^did:layerx:[0-9a-f]{64}$/);
    expect(byId.get(agentWallet.row.id)).toMatchObject({
      outcome: 'awaiting_agent_signature',
      kind: 'agent',
      did: `did:layerx:${agent.publicKeyHex}`,
      main_account_id: chainMainAccountId(agent.publicKeyHex),
    });
    const serialised = JSON.stringify(outcomes);
    expect(serialised).not.toContain(agentWallet.row.encrypted_private_key);
    for (const o of outcomes) {
      expect(Object.keys(o).sort()).toEqual(['address', 'did', 'kind', 'main_account_id', 'outcome', 'reason', 'wallet_id']);
    }
    const legacyRow = await pool.query(`select did from wallets where id = $1`, [legacy.row.id]);
    expect(legacyRow.rows[0].did).toBeNull();
    const cursor = await pool.query(`select last_wallet_id::text from account_backfill_cursor where name = 'unified_account'`);
    expect(cursor.rows[0].last_wallet_id).toBe(rest.cursor);
    const sentBefore = chain.sent.length;
    const empty = await backfillModule.backfillAccounts(d, { batchSize: 5, maxBatches: 1 });
    expect(empty.outcomes).toHaveLength(0);
    expect(chain.sent.length).toBe(sentBefore);

    const layerxKey = (await pool.query(`select layerx_key_id from wallets where id = $1`, [migratedId])).rows[0].layerx_key_id;
    const completed = await asUser(owner, 'POST', '/v1/wallet/provision');
    expect(completed.statusCode, completed.body).toBe(200);
    expect(completed.json()).toMatchObject({ wallet: { id: migratedId, did: byId.get(migratedId)!.did, binding_state: 'bound' } });
    expect((await pool.query(`select layerx_key_id from wallets where id = $1`, [migratedId])).rows[0].layerx_key_id).toBe(layerxKey);
    expect(chain.bindings.get(key.address!.toLowerCase())).toBe(byId.get(migratedId)!.did!.slice('did:layerx:'.length));

    const pending = await stateModule.provisionAccount(d, { kind: 'agent', did: agent.did });
    expect(pending.awaiting).toBe('agent_signature');
    const message = Buffer.from(pending.bindMessage!.slice(2), 'hex');
    const impostor = newAgentKey('impostor');
    await expect(
      stateModule.provisionAccount(d, { kind: 'agent', did: agent.did, agentSignature: impostor.sign(message) }),
    ).rejects.toMatchObject({ code: 'agent_signature_invalid' });
    const agentAddress = agentWallet.row.address.toLowerCase();
    expect(chain.bindings.has(agentAddress)).toBe(false);
    expect(sentTo(agentAddress)).toHaveLength(0);

    const bound = await stateModule.provisionAccount(d, { kind: 'agent', did: agent.did, agentSignature: agent.sign(message) });
    expect(bound.state).toBe('active');
    expect(bound.did).toBe(`did:layerx:${agent.publicKeyHex}`);
    expect(chain.bindings.get(agentAddress)).toBe(agent.publicKeyHex);
    expect(sentTo(agentAddress)).toHaveLength(1);
    expect(sentFrom(agentAddress)).toHaveLength(1);
    const agentRow = await pool.query(`select binding_state, layerx_key_id, did from wallets where id = $1`, [agentWallet.row.id]);
    expect(agentRow.rows[0]).toEqual({ binding_state: 'bound', layerx_key_id: null, did: `did:layerx:${agent.publicKeyHex}` });
  }, LONG);
});
