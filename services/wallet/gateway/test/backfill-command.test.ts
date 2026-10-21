import { spawn } from 'node:child_process';
import { randomBytes, randomUUID } from 'node:crypto';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { newAgentKey } from './support/identity.js';
import { TestChain } from './support/chain.js';
import { CHAIN_ID, startAttestorNetwork, startIdentity, type AttestorNetwork, type Identity } from './e2e/attestors.js';

const here = dirname(fileURLToPath(import.meta.url));
const gatewayDir = resolve(here, '..');
const entry = join(gatewayDir, 'src/jobs/backfillAccounts.ts');
const tsx = join(gatewayDir, 'node_modules/.bin/tsx');
const LONG = 900_000;

const ids = {
  agentWithoutPrincipal: '00000000-0000-4000-8000-000000000001',
  alreadyBound: '00000000-0000-4000-8000-000000000002',
  chainFault: '00000000-0000-4000-8000-000000000003',
  agent: '00000000-0000-4000-8000-000000000004',
  standard: '00000000-0000-4000-8000-000000000005',
};

let identity: Identity;
let chain: TestChain;
let net: AttestorNetwork;
let pg: EphemeralPostgres;
let workDir: string;
let sponsorAddress: string;
let sponsorFile: string;
let poolModule: typeof import('../src/db/pool.js');
let walletsModule: typeof import('../src/db/wallets.js');
const sensitive: string[] = [];

interface Run {
  code: number | null;
  stdout: string;
  stderr: string;
}

function baseEnv(): NodeJS.ProcessEnv {
  return {
    PATH: process.env.PATH,
    HOME: process.env.HOME,
    NODE_ENV: 'test',
    LOG_LEVEL: 'error',
    SUPABASE_URL: identity.url,
    DATABASE_URL: pg.url,
    HYPERPAXEER_RPC_URL: chain.url,
    RPC_URLS: chain.url,
    HYPERPAXEER_CHAIN_ID: String(CHAIN_ID),
    WALLET_MASTER_KEY: process.env.WALLET_MASTER_KEY,
    CORS_ORIGINS: 'http://localhost:3000',
    SPONSOR_PRIVATE_KEY_FILE: sponsorFile,
    ACCOUNT_SETUP_GAS_CAP_WEI: '1000000000000000',
  };
}

function attestorEnv(): NodeJS.ProcessEnv {
  return {
    ATTESTOR_ENDPOINTS: net.nodes.map((n) => n.apiUrl).join(','),
    ATTESTOR_CLIENT_CERT_FILE: net.pki.clientCert,
    ATTESTOR_CLIENT_KEY_FILE: net.pki.clientKey,
    ATTESTOR_CA_FILE: net.pki.caFile,
    ATTESTOR_QUORUM: '3',
    ATTESTOR_TIMEOUT_MS: String(LONG),
  };
}

function runEntry(args: string[], env: NodeJS.ProcessEnv): Promise<Run> {
  return new Promise((resolveRun, reject) => {
    const child = spawn(tsx, [entry, ...args], { cwd: gatewayDir, env, stdio: ['ignore', 'pipe', 'pipe'] });
    const out: Buffer[] = [];
    const err: Buffer[] = [];
    child.stdout.on('data', (c: Buffer) => out.push(c));
    child.stderr.on('data', (c: Buffer) => err.push(c));
    child.once('error', reject);
    child.once('close', (code) =>
      resolveRun({ code, stdout: Buffer.concat(out).toString('utf8'), stderr: Buffer.concat(err).toString('utf8') }),
    );
  });
}

function totals(stdout: string): Record<string, number> {
  const map: Record<string, number> = {};
  for (const line of stdout.trim().split('\n')) {
    const m = /^backfill total outcome=([a-z_]+) count=(\d+)$/.exec(line);
    if (m) map[m[1]!] = Number(m[2]);
  }
  return map;
}

function expectNoIdentifiers(run: Run): void {
  const text = run.stdout + run.stderr;
  expect(text).not.toMatch(/0x[0-9a-fA-F]{40}/);
  expect(text).not.toMatch(/did:/);
  expect(text).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
  expect(text).not.toMatch(/eyJ[0-9A-Za-z_-]+\./);
  for (const s of sensitive) expect(text.toLowerCase()).not.toContain(s.toLowerCase());
}

async function insertMigrated(id: string, userId: string, kind: 'standard' | 'agent'): Promise<`0x${string}`> {
  const keyId = `wallet:${userId}:${kind}:secp256k1:0`;
  const key = await net.generate(keyId, userId);
  await poolModule.getPool().query(
    `insert into wallets (id, user_id, address, encrypted_private_key, key_version, chain_id, kind, migrated_at, attestor_key_id)
     values ($1, $2, $3, null, 1, $4, $5, now(), $6)`,
    [id, userId, key.address, CHAIN_ID, kind, keyId],
  );
  sensitive.push(id, userId, key.address, keyId);
  return key.address;
}

async function walletRow(id: string): Promise<{ did: string | null; binding_state: string }> {
  const { rows } = await poolModule.getPool().query(`select did, binding_state from wallets where id = $1`, [id]);
  return rows[0] as { did: string | null; binding_state: string };
}

beforeAll(async () => {
  workDir = mkdtempSync(join(tmpdir(), 'backfill-command-'));
  identity = await startIdentity();
  chain = new TestChain(CHAIN_ID);
  await chain.start();
  net = await startAttestorNetwork({ identity, rpcUrl: chain.url, readyTimeoutMs: 120_000 });
  const sponsorKey = generatePrivateKey();
  sponsorAddress = privateKeyToAccount(sponsorKey).address.toLowerCase();
  chain.balances.set(sponsorAddress, 10n ** 21n);
  sponsorFile = join(workDir, 'sponsor.key');
  writeFileSync(sponsorFile, sponsorKey.slice(2));
  process.env.SUPABASE_URL = identity.url;
  process.env.RPC_URLS = chain.url;
  process.env.HYPERPAXEER_CHAIN_ID = String(CHAIN_ID);
  pg = await startPostgres();
  poolModule = await import('../src/db/pool.js');
  walletsModule = await import('../src/db/wallets.js');
}, LONG);

afterAll(async () => {
  await poolModule?.closePool();
  await pg?.stop();
  await net?.stop();
  await chain?.stop();
  await identity?.close();
  if (workDir) rmSync(workDir, { recursive: true, force: true });
}, 120_000);

describe('backfill command', () => {
  it('backfill command refuses bad arguments and a start without the attestor configuration', async () => {
    const usage = await runEntry(['--batch-size', '0'], { ...baseEnv(), ...attestorEnv() });
    expect(usage.code).toBe(2);
    expect(usage.stdout).toBe('');
    expect(usage.stderr).toContain('--batch-size takes a positive integer');

    const unknown = await runEntry(['--batch', '5'], { ...baseEnv(), ...attestorEnv() });
    expect(unknown.code).toBe(2);

    const refused = await runEntry(['--batch-size', '2'], baseEnv());
    expect(refused.code).toBe(1);
    expect(refused.stdout).toBe('');
    expect(refused.stderr).toContain('refusing to start without the attestor configuration');
    const cursor = await poolModule.getPool().query(`select 1 from account_backfill_cursor`);
    expect(cursor.rows).toHaveLength(0);
  }, LONG);

  it('backfill command generates identities in bounded batches, prints only totals and sends no top-up or binding', async () => {
    const pool = poolModule.getPool();

    const orphan = newAgentKey('orphan');
    await insertMigrated(ids.agentWithoutPrincipal, walletsModule.agentWalletUserId(orphan.did), 'agent');

    const boundOwner = randomUUID();
    const boundAddress = await insertMigrated(ids.alreadyBound, boundOwner, 'standard');
    const foreignKey = randomBytes(32).toString('hex');
    chain.bindings.set(boundAddress.toLowerCase(), foreignKey);
    chain.bindingsByDid.set(foreignKey, boundAddress.toLowerCase());

    await insertMigrated(ids.chainFault, randomUUID(), 'standard');

    const agent = newAgentKey('backfill');
    await insertMigrated(ids.agent, walletsModule.agentWalletUserId(agent.did), 'agent');
    await pool.query(
      `insert into agent_principals (did, label, key_fingerprint, public_key, wallet_id) values ($1, $2, $3, $4, $5)`,
      [agent.did, 'backfill', agent.publicKeyHex.slice(0, 16), agent.publicKeyHex, ids.agent],
    );
    sensitive.push(agent.did, agent.publicKeyHex, orphan.did);

    const standardOwner = randomUUID();
    await insertMigrated(ids.standard, standardOwner, 'standard');

    const sentBefore = chain.sent.length;
    const sponsorBefore = chain.balances.get(sponsorAddress);
    const env = { ...baseEnv(), ...attestorEnv() };

    const first = await runEntry(['--batch-size', '2', '--max-batches', '1'], env);
    expect(first.code, first.stderr).toBe(3);
    expect(first.stdout.trim().split('\n')).toEqual([
      'backfill total outcome=bound count=0',
      'backfill total outcome=awaiting_owner count=0',
      'backfill total outcome=awaiting_agent_signature count=0',
      'backfill total outcome=refused count=1',
      'backfill total outcome=skipped count=1',
      'backfill total outcome=failed count=0',
      'backfill batches=1 done=false',
    ]);
    expectNoIdentifiers(first);
    const resumed = await pool.query(`select last_wallet_id::text from account_backfill_cursor where name = 'unified_account'`);
    expect(resumed.rows[0].last_wallet_id).toBe(ids.alreadyBound);

    chain.fail('getUnifiedAccount');
    const rest = await runEntry(['--batch-size=2', '--max-batches=10'], env);
    expect(rest.code, rest.stderr).toBe(0);
    expect(rest.stdout.trim().split('\n')).toEqual([
      'backfill total outcome=bound count=0',
      'backfill total outcome=awaiting_owner count=1',
      'backfill total outcome=awaiting_agent_signature count=1',
      'backfill total outcome=refused count=0',
      'backfill total outcome=skipped count=0',
      'backfill total outcome=failed count=1',
      'backfill batches=2 done=true',
    ]);
    expectNoIdentifiers(rest);

    expect(chain.sent.length).toBe(sentBefore);
    expect(chain.balances.get(sponsorAddress)).toBe(sponsorBefore);
    expect(chain.bindings.size).toBe(1);
    const events = await pool.query(`select event from account_setup_audit where event in ('topup', 'bind')`);
    expect(events.rows).toHaveLength(0);
    const provisioned = await pool.query(`select state, topup_raw_tx, bind_raw_tx from account_provisioning`);
    for (const row of provisioned.rows) {
      expect(row.topup_raw_tx).toBeNull();
      expect(row.bind_raw_tx).toBeNull();
      expect(['identity', 'refused']).toContain(row.state);
    }

    const standard = await walletRow(ids.standard);
    expect(standard.did).toMatch(/^did:layerx:[0-9a-f]{64}$/);
    expect(standard.binding_state).toBe('pending');
    expect(await walletRow(ids.agent)).toEqual({ did: `did:layerx:${agent.publicKeyHex}`, binding_state: 'pending' });
    expect((await walletRow(ids.alreadyBound)).binding_state).toBe('refused');
    expect((await walletRow(ids.agentWithoutPrincipal)).did).toBeNull();

    const reset = await pool.query(`select last_wallet_id from account_backfill_cursor where name = 'unified_account'`);
    expect(reset.rows[0].last_wallet_id).toBeNull();

    const again = await runEntry(['--batch-size', '2'], env);
    expect(again.code, again.stderr).toBe(0);
    expect(totals(again.stdout)).toEqual({
      bound: 0,
      awaiting_owner: 0,
      awaiting_agent_signature: 0,
      refused: 0,
      skipped: 1,
      failed: 0,
    });
    expect(again.stdout).toContain('backfill batches=1 done=true');
    expectNoIdentifiers(again);
    expect(chain.sent.length).toBe(sentBefore);
  }, LONG);
});
