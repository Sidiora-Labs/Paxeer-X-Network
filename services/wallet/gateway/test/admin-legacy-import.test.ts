import { randomBytes, randomUUID } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';
import { Journal, applyPlans, planAll, type LegacyWallet } from '../../ceremony/legacy-import/src/importer.js';

const ADMIN_TOKEN = randomBytes(32).toString('hex');

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let baseUrl: string;
let journalDir: string;
let poolModule: typeof import('../src/db/pool.js');

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `${idp.url}/rpc`;
  process.env.GATEWAY_ADMIN_TOKEN = ADMIN_TOKEN;
  pg = await startPostgres();
  const index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  app = await index.buildApp();
  baseUrl = await app.listen({ host: '127.0.0.1', port: 0 });
  journalDir = mkdtempSync(join(tmpdir(), 'legacy-import-journal-'));
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
  if (journalDir) rmSync(journalDir, { recursive: true, force: true });
}, 120_000);

function legacyWallet(kind: 'standard' | 'funded'): LegacyWallet {
  return {
    id: randomUUID(),
    user_id: randomUUID(),
    address: `0x${randomBytes(20).toString('hex').toUpperCase()}`,
    encrypted_private_key: randomBytes(48).toString('base64'),
    key_version: 1,
    chain_id: 125,
    kind,
    funded: kind === 'funded',
    is_disabled: false,
    disabled_reason: null,
    created_at: '2025-01-02T03:04:05.000Z',
  };
}

describe('POST /v1/admin/legacy-import', () => {
  it('refuses a missing or wrong admin token before reading the body', async () => {
    const [plan] = planAll([legacyWallet('standard')], 125, new Set());
    for (const authorization of [undefined, 'Bearer wrong', `Basic ${ADMIN_TOKEN}`]) {
      const res = await app.inject({
        method: 'POST',
        url: '/v1/admin/legacy-import',
        headers: authorization === undefined ? {} : { authorization },
        payload: plan?.record ?? {},
      });
      expect(res.statusCode).toBe(401);
    }
    const { rows } = await poolModule.query('select 1 from wallets where id = $1', [plan?.wallet.id]);
    expect(rows).toHaveLength(0);
  });

  it('refuses records whose key ids, custody or chain do not match the wallet', async () => {
    const [plan] = planAll([legacyWallet('standard')], 125, new Set());
    const record = plan?.record;
    if (!record) throw new Error('standard wallet must plan an import');
    const send = (payload: Record<string, unknown>) =>
      app.inject({ method: 'POST', url: '/v1/admin/legacy-import', headers: { authorization: `Bearer ${ADMIN_TOKEN}` }, payload });
    expect((await send({ ...record, attestor_key_id: `wallet:${randomUUID()}:secp256k1` })).statusCode).toBe(400);
    expect((await send({ ...record, identity_key_id: 'wallet:other:ed25519' })).statusCode).toBe(400);
    expect((await send({ ...record, custody: 'archived' })).statusCode).toBe(400);
    expect((await send({ ...record, encrypted_private_key: '' })).statusCode).toBe(400);
    expect((await send({ ...record, chain_id: 1 })).statusCode).toBe(422);
    const { rows } = await poolModule.query('select 1 from wallets where id = $1', [record.legacy_wallet_id]);
    expect(rows).toHaveLength(0);
  });

  it('imports through the ceremony tool, archives funded wallets and answers 409 on a duplicate', async () => {
    const standard = legacyWallet('standard');
    const funded = legacyWallet('funded');
    const plans = planAll([standard, funded], 125, new Set());
    const journal = new Journal(join(journalDir, 'journal.jsonl'));
    const results = await applyPlans(plans, { url: baseUrl, token: ADMIN_TOKEN }, journal);
    expect(results.map((r) => r.outcome)).toEqual(['imported', 'archived']);

    const { rows } = await poolModule.query<{
      id: string; user_id: string; address: string; kind: string; attestor_key_id: string;
      binding_state: string; archived: boolean; migrated_at: string | null; encrypted_private_key: string;
    }>(
      `select id, user_id::text as user_id, address, kind, attestor_key_id, binding_state,
              archived_at is not null as archived, migrated_at, encrypted_private_key
         from wallets where id = any($1::uuid[]) order by kind desc`,
      [[standard.id, funded.id]],
    );
    expect(rows).toEqual([
      {
        id: standard.id, user_id: standard.user_id, address: standard.address.toLowerCase(), kind: 'standard',
        attestor_key_id: `wallet:${standard.id}:secp256k1`, binding_state: 'unbound', archived: false,
        migrated_at: null, encrypted_private_key: standard.encrypted_private_key,
      },
      {
        id: funded.id, user_id: funded.user_id, address: funded.address.toLowerCase(), kind: 'funded',
        attestor_key_id: `wallet:${funded.id}:secp256k1`, binding_state: 'unbound', archived: true,
        migrated_at: null, encrypted_private_key: funded.encrypted_private_key,
      },
    ]);

    const duplicate = await app.inject({
      method: 'POST',
      url: '/v1/admin/legacy-import',
      headers: { authorization: `Bearer ${ADMIN_TOKEN}` },
      payload: plans[0]?.record ?? {},
    });
    expect(duplicate.statusCode).toBe(409);

    const freshJournal = new Journal(join(journalDir, 'second.jsonl'));
    const rerun = await applyPlans(planAll([standard], 125, new Set()), { url: baseUrl, token: ADMIN_TOKEN }, freshJournal);
    expect(rerun.map((r) => r.outcome)).toEqual(['already_imported']);
    expect(freshJournal.ids.has(standard.id)).toBe(true);
  });
});
