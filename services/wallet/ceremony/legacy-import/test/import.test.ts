import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer as createHttpServer, type Server } from 'node:http';
import { createServer as createNetServer, type AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import pg from 'pg';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { Journal, applyPlans, planAll, readLegacyWallets, reportLine, summary, type CustodyRecord } from '../src/importer.js';

const W1 = '00000000-0000-4000-8000-000000000001';
const W2 = '00000000-0000-4000-8000-000000000002';
const W3 = '00000000-0000-4000-8000-000000000003';
const TOKEN = 'test-admin-token';

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createNetServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const port = (srv.address() as AddressInfo).port;
      srv.close(() => resolve(port));
    });
  });
}

function runAsOwner(bin: string, args: string[]): void {
  const isRoot = process.getuid?.() === 0;
  const [cmd, argv] = isRoot ? ['runuser', ['-u', 'postgres', '--', bin, ...args]] : [bin, args];
  const res = spawnSync(cmd, argv, { encoding: 'utf8' });
  if (res.status !== 0) throw new Error(`${bin} failed with ${res.status}: ${res.stderr}`);
}

let pgDir = '';
let pgDataDir = '';
let pool: pg.Pool;
let gateway: Server;
let gatewayUrl = '';
const received = new Map<string, CustodyRecord>();
let posts = 0;
let scratch = '';

beforeAll(async () => {
  const binDir = process.env.PG_BIN_DIR ?? '/usr/lib/postgresql/16/bin';
  if (!existsSync(join(binDir, 'initdb'))) throw new Error(`postgres binaries not found under ${binDir}; set PG_BIN_DIR`);
  pgDir = mkdtempSync(join(tmpdir(), 'legacy-import-pg-'));
  pgDataDir = join(pgDir, 'data');
  if (process.getuid?.() === 0) {
    chmodSync(pgDir, 0o777);
    execFileSync('chown', ['postgres', pgDir]);
  }
  const port = await freePort();
  runAsOwner(join(binDir, 'initdb'), ['-D', pgDataDir, '-U', 'postgres', '-A', 'trust', '--no-sync', '-E', 'UTF8']);
  runAsOwner(join(binDir, 'pg_ctl'), [
    '-D', pgDataDir, '-l', join(pgDir, 'postgres.log'), '-w',
    '-o', `-p ${port} -c listen_addresses=127.0.0.1 -c unix_socket_directories=${pgDir} -c fsync=off`, 'start',
  ]);
  pool = new pg.Pool({ connectionString: `postgres://postgres@127.0.0.1:${port}/postgres` });
  await pool.query(readFileSync(new URL('legacy-schema.sql', import.meta.url), 'utf8'));

  gateway = createHttpServer((req, res) => {
    let body = '';
    req.on('data', (c: Buffer) => (body += c.toString()));
    req.on('end', () => {
      if (req.method !== 'POST' || req.url !== '/v1/admin/legacy-import') return void res.writeHead(404).end();
      if (req.headers.authorization !== `Bearer ${TOKEN}`) return void res.writeHead(401).end();
      posts++;
      const rec = JSON.parse(body) as CustodyRecord;
      if (received.has(rec.legacy_wallet_id)) return void res.writeHead(409).end('{"error":"exists"}');
      received.set(rec.legacy_wallet_id, rec);
      res.writeHead(201, { 'content-type': 'application/json' }).end('{"ok":true}');
    });
  });
  await new Promise<void>((r) => gateway.listen(0, '127.0.0.1', r));
  gatewayUrl = `http://127.0.0.1:${(gateway.address() as AddressInfo).port}`;
  scratch = mkdtempSync(join(tmpdir(), 'legacy-import-journal-'));
});

afterAll(async () => {
  await pool?.end();
  await new Promise<void>((r) => (gateway ? gateway.close(() => r()) : r()));
  if (pgDataDir) runAsOwner(join(process.env.PG_BIN_DIR ?? '/usr/lib/postgresql/16/bin', 'pg_ctl'), ['-D', pgDataDir, '-m', 'immediate', '-w', 'stop']);
  if (pgDir) rmSync(pgDir, { recursive: true, force: true });
  if (scratch) rmSync(scratch, { recursive: true, force: true });
});

describe('legacy wallet import', () => {
  it('maps the three legacy rows and reports without key ciphertext', async () => {
    const wallets = await readLegacyWallets(pool);
    expect(wallets.map((w) => w.id)).toEqual([W1, W2, W3]);
    const plans = planAll(wallets, 125, new Set());
    expect(plans.map((p) => p.action)).toEqual(['import', 'archive', 'refuse']);
    expect(plans[0]!.record).toMatchObject({
      legacy_wallet_id: W1,
      address: '0x1111111111111111111111111111111111111111',
      kind: 'standard',
      custody: 'live',
      binding_state: 'unbound',
      attestor_key_id: `wallet:${W1}:secp256k1`,
      identity_key_id: `wallet:${W1}:ed25519`,
      created_at: '2026-05-01T00:00:00.000Z',
    });
    expect(plans[1]!.record).toMatchObject({ kind: 'funded', custody: 'archived' });
    expect(plans[2]!.reason).toBe('chain 1 is not the target chain 125');
    expect(summary(plans)).toEqual({ total: 3, import: 1, archive: 1, refuse: 1, already_imported: 0 });
    const report = plans.map(reportLine).join('\n');
    expect(report).not.toContain('synthetic-ciphertext');
    expect(report).toContain('"key_ciphertext":"present"');
  });

  it('applies through the gateway admin API and refuses to import a wallet twice', async () => {
    const journalPath = join(scratch, 'journal.jsonl');
    const admin = { url: gatewayUrl, token: TOKEN };

    const first = await applyPlans(planAll(await readLegacyWallets(pool), 125, new Journal(journalPath).ids), admin, new Journal(journalPath));
    expect(first.map((r) => r.outcome)).toEqual(['imported', 'archived', 'refused']);
    expect(posts).toBe(2);
    expect(received.get(W1)!.encrypted_private_key).toBe('v1:synthetic-iv-1:synthetic-ciphertext-1:synthetic-tag-1');
    expect(received.has(W3)).toBe(false);

    const journal = new Journal(journalPath);
    expect([...journal.ids].sort()).toEqual([W1, W2]);
    const again = planAll(await readLegacyWallets(pool), 125, journal.ids);
    expect(again.map((p) => p.action)).toEqual(['already_imported', 'already_imported', 'refuse']);
    const second = await applyPlans(again, admin, journal);
    expect(second.map((r) => r.outcome)).toEqual(['already_imported', 'already_imported', 'refused']);
    expect(posts).toBe(2);

    const freshPath = join(scratch, 'fresh.jsonl');
    const third = await applyPlans(planAll(await readLegacyWallets(pool), 125, new Set()), admin, new Journal(freshPath));
    expect(third.map((r) => r.outcome)).toEqual(['already_imported', 'already_imported', 'refused']);
    expect(posts).toBe(4);
    expect([...new Journal(freshPath).ids].sort()).toEqual([W1, W2]);
  });
});
