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
let sendError: string | null = null;

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
      if (body.method === 'eth_sendRawTransaction' && sendError) {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, error: { code: -32000, message: sendError } }));
        return;
      }
      if (body.method === 'eth_getTransactionCount') result = `0x${pendingCount.toString(16)}`;
      else if (body.method === 'eth_sendRawTransaction') result = `0x${'ab'.repeat(32)}`;
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

function makeRpc(): InstanceType<PoolModule['RpcPool']> {
  return new poolMod.RpcPool({ urls: [rpcUrl], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
}

function makeStore(): InstanceType<StoreModule['NonceStore']> {
  const rpc = makeRpc();
  return new storeMod.NonceStore({ pool, chainId: 125, pendingCount: (a) => rpc.getTransactionCount(a, 'latest') });
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

  it('gives two sends in one block consecutive nonces while latest has not moved', async () => {
    const address = '0x00000000000000000000000000000000000000a1';
    const store = makeStore();
    const rpc = makeRpc();
    pendingCount = 40;
    sendError = null;
    const send = () => lockMod.withDistributedWalletLock(address, async (lease) => {
      const nonce = await lease.next();
      await rpc.sendRawTransaction('0x02');
      return nonce;
    }, store);
    expect(await send()).toBe(40);
    expect(await send()).toBe(41);
  });

  it('frees the nonce of a send the node rejects and keeps one the node already pools', async () => {
    const address = '0x00000000000000000000000000000000000000a4';
    const store = makeStore();
    const rpc = makeRpc();
    pendingCount = 9;
    const attempt = () => store.withLock(address, async (lease) => {
      const nonce = await lease.next();
      try {
        await rpc.sendRawTransaction('0x02');
      } catch (err) {
        if (err instanceof poolMod.TransactionRejectedError) await lease.release(nonce);
        return { nonce, error: err };
      }
      return { nonce, error: null };
    });
    sendError = 'insufficient funds for gas * price + value';
    const rejected = await attempt();
    expect(rejected.nonce).toBe(9);
    expect(rejected.error).toBeInstanceOf(poolMod.TransactionRejectedError);
    sendError = 'already known';
    const pooled = await attempt();
    expect(pooled.nonce).toBe(9);
    expect(pooled.error).toBeInstanceOf(poolMod.RpcResponseError);
    expect(pooled.error).not.toBeInstanceOf(poolMod.TransactionRejectedError);
    sendError = null;
    expect(await store.allocate(address)).toBe(10);
  });

  it('uses latest after a reconnect once latest has advanced past the stored next nonce', async () => {
    const address = '0x00000000000000000000000000000000000000a5';
    pendingCount = 5;
    expect(await makeStore().allocate(address)).toBe(5);
    expect(await makeStore().allocate(address)).toBe(6);
    pendingCount = 30;
    expect(await makeStore().allocate(address)).toBe(30);
    pendingCount = 12;
    expect(await makeStore().allocate(address)).toBe(31);
    const { rows } = await pool.query<{ next_nonce: string }>(
      'select next_nonce::text from nonce_allocations where address = $1', [address]);
    expect(rows[0]?.next_nonce).toBe('32');
  });

  it('reserves an orchestrator leg nonce under the wallet lock and keeps a replacement at its nonce', async () => {
    const address = '0x00000000000000000000000000000000000000a6';
    const store = makeStore();
    const { legNonce } = await import('../src/agent/actions/orchestrator.js');
    pendingCount = 50;
    expect(await lockMod.withDistributedWalletLock(address, (lease) => legNonce(lease), store)).toBe(50);
    expect(await store.allocate(address)).toBe(51);
    expect(await lockMod.withDistributedWalletLock(address, (lease) => legNonce(lease, { nonce: 50 }), store)).toBe(50);
    expect(await store.allocate(address)).toBe(52);
  });

  it('refuses a client nonce below latest and advances past a broadcast client nonce', async () => {
    const address = '0x00000000000000000000000000000000000000a7';
    const store = makeStore();
    pendingCount = 5;
    expect(await store.allocate(address)).toBe(5);
    const refused = await store.withLock(address, (lease) => lease.checkClientNonce(3)).catch((err: unknown) => err);
    expect(refused).toBeInstanceOf(storeMod.NonceBelowLatestError);
    expect(refused).toMatchObject({ nonce: 3, latest: 5 });
    await store.withLock(address, async (lease) => {
      await lease.checkClientNonce(9);
      await lease.advance(9);
    });
    await store.withLock(address, (lease) => lease.advance(2));
    expect(await store.allocate(address)).toBe(10);
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
    pendingCount = 14;
    expect(await store.allocate(address)).toBe(14);
    await expect(
      lockMod.withDistributedWalletLock(address, async (lease) => {
        await lease.next();
        throw new Error('signing refused');
      }, store),
    ).rejects.toThrow('signing refused');
    expect(await store.allocate(address)).toBe(15);
  });
});
