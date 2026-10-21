import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { fork } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import pg from 'pg';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';

const here = dirname(fileURLToPath(import.meta.url));
const gatewayDir = resolve(here, '..');
const ADDRESS = '0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed' as const;

let pgServer: EphemeralPostgres;
let pool: pg.Pool;
let rpcServer: Server;
let rpcUrl: string;
let pendingCount = 7;

type StoreModule = typeof import('../src/nonce/store.js');
type PoolModule = typeof import('../src/rpc/pool.js');
type LockModule = typeof import('../src/agent/actions/nonceLock.js');
let storeMod: StoreModule;
let poolMod: PoolModule;
let lockMod: LockModule;

function startRpc(): Promise<void> {
  rpcServer = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string };
      let result: unknown;
      if (body.method === 'eth_getTransactionCount') result = `0x${pendingCount.toString(16)}`;
      else if (body.method === 'eth_blockNumber') result = '0x64';
      else {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }));
        return;
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, result }));
    });
  });
  return new Promise((r) =>
    rpcServer.listen(0, '127.0.0.1', () => {
      rpcUrl = `http://127.0.0.1:${(rpcServer.address() as AddressInfo).port}`;
      r();
    }),
  );
}

function makeStore(): InstanceType<StoreModule['NonceStore']> {
  const rpc = new poolMod.RpcPool({ urls: [rpcUrl], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
  return new storeMod.NonceStore({ pool, chainId: 125, pendingCount: (a) => rpc.getTransactionCount(a, 'pending') });
}

interface WorkerResult {
  pid: number;
  nonces: number[];
}

function spawnWorker(address: string, count: number): { ready: Promise<void>; go: () => void; done: Promise<WorkerResult> } {
  const child = fork(join(here, 'support', 'nonce-worker.ts'), [], {
    cwd: gatewayDir,
    execArgv: ['--import', 'tsx'],
    env: {
      ...process.env,
      NONCE_WORKER_DATABASE_URL: pgServer.url,
      NONCE_WORKER_RPC_URL: rpcUrl,
      NONCE_WORKER_ADDRESS: address,
      NONCE_WORKER_COUNT: String(count),
    },
    stdio: ['ignore', 'inherit', 'inherit', 'ipc'],
  });
  let readyResolve: () => void = () => undefined;
  const ready = new Promise<void>((r) => (readyResolve = r));
  const done = new Promise<WorkerResult>((resolveDone, reject) => {
    let result: WorkerResult | null = null;
    child.on('message', (m: { ready?: boolean; error?: string } & Partial<WorkerResult>) => {
      if (m.ready) readyResolve();
      else if (m.error) reject(new Error(m.error));
      else if (m.nonces && m.pid) result = { pid: m.pid, nonces: m.nonces };
    });
    child.on('exit', (code) => {
      if (code === 0 && result) resolveDone(result);
      else reject(new Error(`nonce worker exited with ${code}`));
    });
  });
  return { ready, go: () => child.send({ go: true }), done };
}

beforeAll(async () => {
  pgServer = await startPostgres();
  pool = new pg.Pool({ connectionString: pgServer.url, max: 20 });
  await startRpc();
  storeMod = await import('../src/nonce/store.js');
  poolMod = await import('../src/rpc/pool.js');
  lockMod = await import('../src/agent/actions/nonceLock.js');
}, 120_000);

afterAll(async () => {
  await pool?.end();
  await new Promise<void>((r) => (rpcServer ? rpcServer.close(() => r()) : r()));
  await pgServer?.stop();
});

describe('NonceStore', () => {
  it('allocates a dense, gap-free, duplicate-free sequence across two processes', async () => {
    pendingCount = 7;
    const perWorker = 25;
    const a = spawnWorker(ADDRESS, perWorker);
    const b = spawnWorker(ADDRESS, perWorker);
    await Promise.all([a.ready, b.ready]);
    a.go();
    b.go();
    const [ra, rb] = await Promise.all([a.done, b.done]);
    expect(ra.pid).not.toBe(rb.pid);
    expect(ra.nonces).toHaveLength(perWorker);
    expect(rb.nonces).toHaveLength(perWorker);
    const all = [...ra.nonces, ...rb.nonces].sort((x, y) => x - y);
    expect(new Set(all).size).toBe(all.length);
    expect(all).toEqual(Array.from({ length: perWorker * 2 }, (_, i) => 7 + i));
    const { rows } = await pool.query<{ next_nonce: string; needs_reconcile: boolean }>(
      'select next_nonce::text, needs_reconcile from nonce_allocations where address = $1',
      [ADDRESS.toLowerCase()],
    );
    expect(rows[0]).toEqual({ next_nonce: String(7 + perWorker * 2), needs_reconcile: false });
  }, 120_000);

  it('reconciles against the chain on first use and after a broadcast failure', async () => {
    const address = '0x00000000000000000000000000000000000000a1';
    const store = makeStore();
    pendingCount = 40;
    expect(await store.allocate(address)).toBe(40);
    pendingCount = 99;
    expect(await store.allocate(address)).toBe(41);
    await store.withLock(address, async (lease) => {
      expect(await lease.next()).toBe(42);
      await lease.markForReconcile();
    });
    pendingCount = 42;
    expect(await store.allocate(address)).toBe(42);
    expect(await store.allocate(address)).toBe(43);
  });

  it('rolls back an allocation when the work under the lock throws', async () => {
    const address = '0x00000000000000000000000000000000000000a2';
    const store = makeStore();
    pendingCount = 5;
    await expect(
      store.withLock(address, async (lease) => {
        await lease.next();
        throw new Error('signing refused');
      }),
    ).rejects.toThrow('signing refused');
    expect(await store.allocate(address)).toBe(5);
  });

  it('routes the distributed wallet lock through the shared store with mutual exclusion', async () => {
    const address = '0x00000000000000000000000000000000000000a3';
    const store = makeStore();
    pendingCount = 11;
    expect(await store.allocate(address)).toBe(11);
    let inside = 0;
    let maxInside = 0;
    const work = async (): Promise<string> => {
      inside += 1;
      maxInside = Math.max(maxInside, inside);
      await new Promise((r) => setTimeout(r, 25));
      inside -= 1;
      return 'ok';
    };
    const results = await Promise.all([
      lockMod.withDistributedWalletLock(address, work, store),
      lockMod.withDistributedWalletLock(address, work, store),
      lockMod.withDistributedWalletLock(address, work, store),
    ]);
    expect(results).toEqual(['ok', 'ok', 'ok']);
    expect(maxInside).toBe(1);
    const { rows } = await pool.query<{ needs_reconcile: boolean }>(
      'select needs_reconcile from nonce_allocations where address = $1',
      [address],
    );
    expect(rows[0]?.needs_reconcile).toBe(true);
    pendingCount = 14;
    expect(await store.allocate(address)).toBe(14);
    await expect(
      lockMod.withDistributedWalletLock(address, async () => {
        throw new Error('broadcast rejected');
      }, store),
    ).rejects.toThrow('broadcast rejected');
    const after = await pool.query<{ needs_reconcile: boolean }>(
      'select needs_reconcile from nonce_allocations where address = $1',
      [address],
    );
    expect(after.rows[0]?.needs_reconcile).toBe(true);
  });
});
