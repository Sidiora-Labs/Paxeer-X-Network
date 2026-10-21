import { randomBytes, randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from './support/identity.js';

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let poolModule: typeof import('../src/db/pool.js');
let walletsModule: typeof import('../src/db/wallets.js');

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `${idp.url}/rpc`;
  pg = await startPostgres();
  const index = await import('../src/index.js');
  poolModule = await import('../src/db/pool.js');
  walletsModule = await import('../src/db/wallets.js');
  app = await index.buildApp();
  await app.ready();
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
}, 120_000);

async function asUser(userId: string, method: 'GET' | 'POST', url: string, payload?: Record<string, unknown>) {
  return app.inject({
    method,
    url,
    headers: { authorization: `Bearer ${await idp.mintUserToken(userId)}` },
    ...(payload === undefined ? {} : { payload }),
  });
}

async function provision(userId: string): Promise<{ id: string; address: string }> {
  const res = await asUser(userId, 'POST', '/v1/wallet/provision');
  expect(res.statusCode).toBe(200);
  const { wallet } = res.json() as { wallet: { id: string; address: string } };
  return wallet;
}

describe('funded lane removal', () => {
  it.each([
    ['GET', '/v1/funded/tiers'],
    ['POST', '/v1/funded/provision'],
    ['GET', '/v1/funded/me'],
    ['POST', '/v1/funded/sign'],
    ['POST', '/v1/funded/send'],
    ['POST', '/v1/funded/sign-message'],
  ] as const)('%s %s no longer exists', async (method, url) => {
    const res = await asUser(randomUUID(), method, url, method === 'POST' ? {} : undefined);
    expect(res.statusCode).toBe(404);
  });

  it('drops the tier and whitelist tables and adds the archive column', async () => {
    const { rows } = await poolModule.query<{ tiers: string | null; whitelist: string | null; accounts: string | null }>(
      `select to_regclass('public.funded_tiers')::text as tiers,
              to_regclass('public.whitelist_entries')::text as whitelist,
              to_regclass('public.funded_accounts')::text as accounts`,
    );
    expect(rows[0]).toEqual({ tiers: null, whitelist: null, accounts: 'funded_accounts' });
    const column = await poolModule.query(
      `select 1 from information_schema.columns
        where table_name = 'wallets' and column_name = 'archived_at'`,
    );
    expect(column.rowCount).toBe(1);
  });
});

describe('archived wallets', () => {
  let fundedAddress: string;

  beforeAll(async () => {
    const holder = randomUUID();
    const wallet = await provision(holder);
    fundedAddress = wallet.address;
    await poolModule.query(`update wallets set kind = 'funded' where id = $1`, [wallet.id]);
    await poolModule.query(
      `insert into funded_accounts (wallet_id, tier_id, starting_value_usd, peak_value_usd)
       values ($1, 'starter_25k', 25000, 25000)`,
      [wallet.id],
    );
    const migration = await readFile(new URL('../migrations/006_archive_funded.sql', import.meta.url), 'utf8');
    await poolModule.query(migration);
    const { rows } = await poolModule.query<{ archived: boolean }>(
      `select archived_at is not null as archived from wallets where id = $1`,
      [wallet.id],
    );
    expect(rows[0]?.archived).toBe(true);
  }, 60_000);

  it('refuses to send to an archived wallet', async () => {
    const sender = randomUUID();
    await provision(sender);
    const res = await asUser(sender, 'POST', '/v1/wallet/send', {
      tx: { to: fundedAddress, value: '1' },
    });
    expect(res.statusCode).toBe(410);
    expect(res.json()).toMatchObject({ error: 'wallet_archived', address: fundedAddress });
  });

  it('answers 410 for every route on an archived standard wallet', async () => {
    const user = randomUUID();
    const wallet = await provision(user);
    await poolModule.query(`update wallets set archived_at = now() where id = $1`, [wallet.id]);

    const me = await asUser(user, 'GET', '/v1/wallet/me');
    expect(me.statusCode).toBe(410);
    expect(me.json()).toMatchObject({ error: 'wallet_archived', wallet_id: wallet.id, address: wallet.address });

    const again = await asUser(user, 'POST', '/v1/wallet/provision');
    expect(again.statusCode).toBe(410);
    expect(again.json()).toMatchObject({ error: 'wallet_archived', wallet_id: wallet.id });

    const sign = await asUser(user, 'POST', '/v1/wallet/sign-message', { message: 'hello' });
    expect(sign.statusCode).toBe(410);
    expect(sign.json()).toMatchObject({ error: 'wallet_archived' });

    expect(await walletsModule.findWalletByUserId(user)).toBeNull();
    expect(await walletsModule.findWalletByAddress(wallet.address)).toBeNull();
    expect(await walletsModule.archivedWalletGuard({ id: wallet.id })).toMatchObject({ status: 410 });
  });

  it('lets a live wallet through the archive guard', async () => {
    const user = randomUUID();
    const wallet = await provision(user);
    expect(await walletsModule.archivedWalletGuard({ id: wallet.id }, { address: wallet.address })).toBeNull();
    expect((await walletsModule.findWalletByUserId(user))?.id).toBe(wallet.id);
  });
});
