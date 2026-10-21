import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { fork } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import pg from 'pg';
import { keccak256, toHex } from 'viem';
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
let sendHangs = false;
let hashLookupDown = false;
const knownHashes = new Set<string>();

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
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
      let result: unknown;
      if (body.method === 'eth_sendRawTransaction' && sendHangs) return;
      if (body.method === 'eth_getTransactionByHash' && hashLookupDown) {
        res.writeHead(503);
        res.end();
        return;
      }
      if (body.method === 'eth_sendRawTransaction' && sendError) {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, error: { code: -32000, message: sendError } }));
        return;
      }
      if (body.method === 'eth_getTransactionCount') result = `0x${pendingCount.toString(16)}`;
      else if (body.method === 'eth_sendRawTransaction') {
        result = keccak256(body.params[0] as `0x${string}`);
        knownHashes.add(result as string);
      }
      else if (body.method === 'eth_blockNumber') result = '0x64';
      else if (body.method === 'eth_getTransactionByHash') {
        const hash = String(body.params[0]).toLowerCase();
        result = knownHashes.has(hash) ? { hash } : null;
      }
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

function makeRpc(timeoutMs = 5_000): InstanceType<PoolModule['RpcPool']> {
  return new poolMod.RpcPool({ urls: [rpcUrl], chainId: 125, lagThresholdBlocks: 20, timeoutMs, healthIntervalMs: 60_000 });
}

function makeStore(): InstanceType<StoreModule['NonceStore']> {
  const rpc = makeRpc();
  return new storeMod.NonceStore({
    pool,
    chainId: 125,
    pendingCount: (a) => rpc.getTransactionCount(a, 'latest'),
    lookupTransaction: (h, m) => rpc.lookupTransaction(h, m),
  });
}

let allocations = 0;

/** Reserve a nonce and record the hash of a signed transaction the members know, as every reservation is held. */
function allocate(store: InstanceType<StoreModule['NonceStore']>, address: string): Promise<number> {
  return store.withLock(address, async (lease) => {
    const nonce = await lease.next();
    const hash = keccak256(toHex(`allocation ${++allocations}`));
    knownHashes.add(hash);
    await lease.track(nonce, hash, null);
    return nonce;
  });
}

/** Make every outstanding nonce of an address older than the broadcast grace period. */
async function age(address: string): Promise<void> {
  await pool.query(
    `update nonce_outstanding set created_at = now() - make_interval(secs => $2::double precision / 1000 + 1)
      where address = $1`,
    [address, (await import('../src/chain.js')).BROADCAST_GRACE_MS],
  );
}

/** Reserve a nonce and broadcast under the lock through the helper the send, owner and orchestrator paths share. */
function lockedSend(
  store: InstanceType<StoreModule['NonceStore']>,
  address: string,
  raw: `0x${string}`,
  rpc = makeRpc(),
): Promise<{ nonce: number; error: unknown }> {
  return store.withLock(address, async (lease) => {
    const nonce = await lease.next();
    const sent = await storeMod.broadcastReserved(lease, nonce, raw, (r) => rpc.sendRawTransactionVia(r));
    return { nonce, error: sent.error };
  });
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
      await lease.track(nonce, await rpc.sendRawTransaction('0x02'), null);
      return nonce;
    }, store);
    expect(await send()).toBe(40);
    expect(await send()).toBe(41);
  });

  it('frees the nonce of a send the node rejects and takes already known as the accepted send', async () => {
    const address = '0x00000000000000000000000000000000000000a4';
    const store = makeStore();
    pendingCount = 9;
    sendError = 'insufficient funds for gas * price + value';
    const rejected = await lockedSend(store, address, '0x02a4');
    expect(rejected.nonce).toBe(9);
    expect(rejected.error).toBeInstanceOf(poolMod.TransactionRejectedError);
    sendError = 'already known';
    expect(await lockedSend(store, address, '0x02a5')).toEqual({ nonce: 9, error: null });
    sendError = null;
    expect(await allocate(store, address)).toBe(10);
  });

  it('uses latest after a reconnect once latest has advanced past the stored next nonce', async () => {
    const address = '0x00000000000000000000000000000000000000a5';
    pendingCount = 5;
    expect(await allocate(makeStore(), address)).toBe(5);
    expect(await allocate(makeStore(), address)).toBe(6);
    pendingCount = 30;
    expect(await allocate(makeStore(), address)).toBe(30);
    pendingCount = 12;
    expect(await allocate(makeStore(), address)).toBe(31);
    const { rows } = await pool.query<{ next_nonce: string }>(
      'select next_nonce::text from nonce_allocations where address = $1', [address]);
    expect(rows[0]?.next_nonce).toBe('32');
  });

  it('reserves an orchestrator leg nonce under the wallet lock and keeps a replacement at its nonce', async () => {
    const address = '0x00000000000000000000000000000000000000a6';
    const store = makeStore();
    const { legNonce } = await import('../src/agent/actions/orchestrator.js');
    pendingCount = 50;
    expect(await lockMod.withDistributedWalletLock(address, async (lease) => {
      const nonce = await legNonce(lease);
      await lease.track(nonce, keccak256('0x02a6'), null);
      return nonce;
    }, store)).toBe(50);
    expect(await allocate(store, address)).toBe(51);
    expect(await lockMod.withDistributedWalletLock(address, (lease) => legNonce(lease, { nonce: 50 }), store)).toBe(50);
    expect(await allocate(store, address)).toBe(52);
  });

  it('refuses a client nonce below latest and keeps a broadcast client nonce outstanding while filling the gap below it', async () => {
    const address = '0x00000000000000000000000000000000000000a7';
    const store = makeStore();
    pendingCount = 5;
    expect(await allocate(store, address)).toBe(5);
    const refused = await store.withLock(address, (lease) => lease.checkClientNonce(3)).catch((err: unknown) => err);
    expect(refused).toBeInstanceOf(storeMod.NonceBelowLatestError);
    expect(refused).toMatchObject({ nonce: 3, latest: 5 });
    await store.withLock(address, async (lease) => {
      await lease.checkClientNonce(9);
      knownHashes.add(keccak256('0x0209'));
      await lease.track(9, keccak256('0x0209'), null);
    });
    await store.withLock(address, (lease) => lease.track(2, keccak256('0x0202'), null));
    expect(await allocate(store, address)).toBe(6);
    expect(await allocate(store, address)).toBe(7);
    expect(await allocate(store, address)).toBe(8);
    expect(await allocate(store, address)).toBe(10);
  });

  it('leaves no gap after a sign-only reservation that is never broadcast', async () => {
    const address = '0x00000000000000000000000000000000000000b1';
    const store = makeStore();
    pendingCount = 20;
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(20);
    expect(await store.withLock(address, async (lease) => {
      const nonce = await lease.next();
      await lease.release(nonce);
      return nonce;
    })).toBe(20);
    expect(await allocate(store, address)).toBe(20);
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(21);
  });

  it('frees the nonce of a send the node answers as underpriced', async () => {
    const address = '0x00000000000000000000000000000000000000b2';
    const store = makeStore();
    pendingCount = 30;
    sendError = 'transaction underpriced';
    const first = await lockedSend(store, address, '0x02aa');
    sendError = null;
    expect(first.nonce).toBe(30);
    expect(first.error).toBeInstanceOf(poolMod.TransactionRejectedError);
    expect(await allocate(store, address)).toBe(30);
  });

  it('frees a timed-out send whose transaction no node knows and keeps one that mined or pooled', async () => {
    const address = '0x00000000000000000000000000000000000000b3';
    const store = makeStore();
    const slow = makeRpc(150);
    pendingCount = 40;
    sendHangs = true;
    const lost = await lockedSend(store, address, '0x02b1', slow);
    expect(lost.nonce).toBe(40);
    expect(lost.error).not.toBeInstanceOf(poolMod.TransactionRejectedError);
    sendHangs = false;
    await age(address);
    expect(await allocate(store, address)).toBe(40);

    sendHangs = true;
    const mined = await lockedSend(store, address, '0x02b2', slow);
    expect(mined.nonce).toBe(41);
    sendHangs = false;
    pendingCount = 42;
    expect(await allocate(store, address)).toBe(42);

    sendHangs = true;
    const pooled = await lockedSend(store, address, '0x02b3', slow);
    sendHangs = false;
    expect(pooled.nonce).toBe(43);
    knownHashes.add(keccak256('0x02b3'));
    expect(await allocate(store, address)).toBe(44);
    const { rows } = await pool.query<{ nonce: string; tx_hash: string | null }>(
      'select nonce::text, tx_hash from nonce_outstanding where address = $1 order by nonce', [address]);
    expect(rows).toEqual([
      { nonce: '42', tx_hash: expect.stringMatching(/^0x[0-9a-f]{64}$/) },
      { nonce: '43', tx_hash: keccak256('0x02b3') },
      { nonce: '44', tx_hash: expect.stringMatching(/^0x[0-9a-f]{64}$/) },
    ]);
  });

  it('brings next back to latest once the chain passes every outstanding nonce', async () => {
    const address = '0x00000000000000000000000000000000000000b4';
    const store = makeStore();
    pendingCount = 50;
    expect(await allocate(store, address)).toBe(50);
    expect(await allocate(store, address)).toBe(51);
    pendingCount = 52;
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(52);
    const { rows } = await pool.query('select 1 from nonce_outstanding where address = $1', [address]);
    expect(rows).toHaveLength(0);
  });

  it('validates and records a client-chosen agent nonce and reports the reservation-aware next', async () => {
    const address = '0x00000000000000000000000000000000000000b5';
    const store = makeStore();
    pendingCount = 60;
    expect(await allocate(store, address)).toBe(60);
    const below = await store.withLock(address, (lease) => lease.reserveClientNonce(59)).catch((err: unknown) => err);
    expect(below).toBeInstanceOf(storeMod.NonceBelowLatestError);
    const taken = await store.withLock(address, (lease) => lease.reserveClientNonce(60)).catch((err: unknown) => err);
    expect(taken).toBeInstanceOf(storeMod.NonceOutstandingError);
    expect(taken).toMatchObject({ nonce: 60 });
    await store.withLock(address, async (lease) => {
      await lease.reserveClientNonce(61);
      await lease.track(61, keccak256('0x02b5'), null);
    });
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(62);
    expect(await allocate(store, address)).toBe(62);
    const { custodyRefusal } = await import('../src/routes/agentExec.js');
    expect(custodyRefusal(below)).toEqual({ status: 409, body: { error: 'nonce_below_latest', nonce: 59, latest: 60 } });
    expect(custodyRefusal(taken)).toEqual({ status: 409, body: { error: 'nonce_outstanding', nonce: 60 } });
  });

  it('reconciles a timed-out owner leg broadcast on the next allocation', async () => {
    const address = '0x00000000000000000000000000000000000000c1';
    const store = makeStore();
    const slow = makeRpc(150);
    pendingCount = 70;
    sendHangs = true;
    const leg = await store.withLock(address, async (lease) => {
      const nonce = await lease.next();
      return { nonce, ...(await storeMod.broadcastReserved(lease, nonce, '0x02c1', (r) => slow.sendRawTransactionVia(r))) };
    });
    sendHangs = false;
    expect(leg.nonce).toBe(70);
    expect(leg.hash).toBeNull();
    const { rows } = await pool.query<{ tx_hash: string }>(
      'select tx_hash from nonce_outstanding where address = $1 and nonce = 70', [address]);
    expect(rows[0]?.tx_hash).toBe(keccak256('0x02c1'));
    await age(address);
    expect(await allocate(store, address)).toBe(70);
  });

  it('releases an accepted broadcast the pool later evicted', async () => {
    const address = '0x00000000000000000000000000000000000000c2';
    const store = makeStore();
    pendingCount = 80;
    sendError = null;
    const accepted = await lockedSend(store, address, '0x02c2');
    expect(accepted).toEqual({ nonce: 80, error: null });
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(81);
    knownHashes.delete(keccak256('0x02c2'));
    await age(address);
    expect(await allocate(store, address)).toBe(80);
  });

  it('keeps a nonce accepted on one member and unknown on another inside the grace period and releases it after', async () => {
    const address = '0x00000000000000000000000000000000000000c4';
    const memberB = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on('data', (c: Buffer) => chunks.push(c));
      req.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string };
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify(body.method === 'eth_getTransactionByHash'
          ? { jsonrpc: '2.0', id: body.id, result: null }
          : { jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }));
      });
    });
    await new Promise<void>((r) => memberB.listen(0, '127.0.0.1', () => r()));
    const urlB = `http://127.0.0.1:${(memberB.address() as AddressInfo).port}`;
    try {
      const both = new poolMod.RpcPool({ urls: [rpcUrl, urlB], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
      const latest = makeRpc();
      const store = new storeMod.NonceStore({
        pool,
        chainId: 125,
        pendingCount: (a) => latest.getTransactionCount(a, 'latest'),
        lookupTransaction: (h, m) => both.lookupTransaction(h, m),
      });
      pendingCount = 100;
      sendError = null;
      const accepted = await lockedSend(store, address, '0x02c4', both);
      expect(accepted).toEqual({ nonce: 100, error: null });
      const { rows } = await pool.query<{ member: string }>(
        'select member from nonce_outstanding where address = $1 and nonce = 100', [address]);
      expect(rows[0]?.member).toBe(rpcUrl);
      await store.withLock(address, async (lease) => {
        await lease.reserveClientNonce(103);
        knownHashes.add(keccak256('0x02c7'));
        await lease.track(103, keccak256('0x02c7'), null);
      });
      hashLookupDown = true;
      expect(await allocate(store, address)).toBe(101);
      await age(address);
      const partitioned = await allocate(store, address).catch((err: unknown) => err);
      expect(partitioned).toBeInstanceOf(storeMod.NonceUnresolvedError);
      expect(partitioned).toMatchObject({ nonce: 100, members: [rpcUrl] });
      const { rows: held } = await pool.query<{ nonce: string }>(
        'select nonce::text from nonce_outstanding where address = $1 order by nonce_outstanding.nonce', [address]);
      expect(held.map((r) => r.nonce)).toEqual(['100', '101', '103']);
      pendingCount = 102;
      expect(await allocate(store, address)).toBe(102);
      await expect(allocate(store, address)).rejects.toBeInstanceOf(storeMod.NonceUnresolvedError);
      hashLookupDown = false;
      expect(await allocate(store, address)).toBe(104);
      knownHashes.delete(keccak256('0x02c7'));
      await age(address);
      expect(await allocate(store, address)).toBe(103);
    } finally {
      hashLookupDown = false;
      await new Promise<void>((r) => memberB.close(() => r()));
    }
  });

  it('hands out a released nonce below a still outstanding one before anything higher', async () => {
    const address = '0x00000000000000000000000000000000000000c3';
    const store = makeStore();
    pendingCount = 90;
    expect(await allocate(store, address)).toBe(90);
    expect(await allocate(store, address)).toBe(91);
    expect(await allocate(store, address)).toBe(92);
    await store.withLock(address, (lease) => lease.release(91));
    expect(await allocate(store, address)).toBe(91);
    expect(await allocate(store, address)).toBe(93);
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
    expect(await allocate(store, address)).toBe(5);
  });

  it('routes the distributed wallet lock through the shared store with mutual exclusion', async () => {
    const address = '0x00000000000000000000000000000000000000a3';
    const store = makeStore();
    pendingCount = 11;
    expect(await allocate(store, address)).toBe(11);
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
    expect(await allocate(store, address)).toBe(14);
    await expect(
      lockMod.withDistributedWalletLock(address, async (lease) => {
        await lease.next();
        throw new Error('signing refused');
      }, store),
    ).rejects.toThrow('signing refused');
    expect(await allocate(store, address)).toBe(15);
  });

  it('records the hash of a sign-only transaction and resolves its nonce by that hash', async () => {
    const address = '0x00000000000000000000000000000000000000d1';
    const store = makeStore();
    pendingCount = 110;
    sendError = null;
    const signOnly = (raw: `0x${string}`) => store.withLock(address, async (lease) => {
      const nonce = await lease.next();
      await lease.track(nonce, keccak256(raw), null);
      return nonce;
    });
    expect(await signOnly('0x02d0')).toBe(110);
    expect(await lockedSend(store, address, '0x02d1')).toEqual({ nonce: 111, error: null });
    const { rows } = await pool.query<{ tx_hash: string }>(
      'select tx_hash from nonce_outstanding where address = $1 and nonce = 110', [address]);
    expect(rows[0]?.tx_hash).toBe(keccak256('0x02d0'));
    knownHashes.add(keccak256('0x02d0'));
    await age(address);
    expect(await allocate(store, address)).toBe(112);
    knownHashes.delete(keccak256('0x02d0'));
    await age(address);
    expect(await allocate(store, address)).toBe(110);
    await expect(store.withLock(address, (lease) => lease.next())).rejects.toThrow(/neither tracked/);
    expect(await store.withLock(address, (lease) => lease.peek())).toBe(113);
  });

  it('does not take a not found from the accepting member as final while another member knows the transaction', async () => {
    const address = '0x00000000000000000000000000000000000000d2';
    const knownOnB = new Set<string>();
    const memberB = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on('data', (c: Buffer) => chunks.push(c));
      req.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify(body.method === 'eth_getTransactionByHash'
          ? { jsonrpc: '2.0', id: body.id, result: knownOnB.has(String(body.params[0]).toLowerCase()) ? { hash: body.params[0] } : null }
          : { jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }));
      });
    });
    await new Promise<void>((r) => memberB.listen(0, '127.0.0.1', () => r()));
    const urlB = `http://127.0.0.1:${(memberB.address() as AddressInfo).port}`;
    try {
      const both = new poolMod.RpcPool({ urls: [rpcUrl, urlB], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
      const latest = makeRpc();
      const store = new storeMod.NonceStore({
        pool,
        chainId: 125,
        pendingCount: (a) => latest.getTransactionCount(a, 'latest'),
        lookupTransaction: (h, m) => both.lookupTransaction(h, m),
      });
      pendingCount = 120;
      sendError = null;
      expect(await lockedSend(store, address, '0x02d2', both)).toEqual({ nonce: 120, error: null });
      knownHashes.delete(keccak256('0x02d2'));
      knownOnB.add(keccak256('0x02d2'));
      await age(address);
      expect(await allocate(store, address)).toBe(121);
      knownOnB.clear();
      await age(address);
      expect(await allocate(store, address)).toBe(120);
    } finally {
      await new Promise<void>((r) => memberB.close(() => r()));
    }
  });

  it('seeds a wallet from latest and refuses a send that a pooled transaction at that nonce underprices with 409', async () => {
    const address = '0x00000000000000000000000000000000000000d3';
    const store = makeStore();
    pendingCount = 130;
    sendError = 'replacement transaction underpriced';
    try {
      const sent = await lockedSend(store, address, '0x02d9');
      expect(sent.nonce).toBe(130);
      expect(sent.error).toBeInstanceOf(poolMod.NonceConflictError);
    } finally {
      sendError = null;
    }
    const { rows } = await pool.query<{ nonce: string; tx_hash: string }>(
      'select nonce::text, tx_hash from nonce_outstanding where address = $1', [address]);
    expect(rows).toEqual([{ nonce: '130', tx_hash: keccak256('0x02d9') }]);
    const { custodyRefusal } = await import('../src/routes/agentExec.js');
    expect(custodyRefusal(new poolMod.NonceConflictError(-32000, 'replacement transaction underpriced', null))).toEqual({
      status: 409, body: { error: 'nonce_conflict', detail: 'replacement transaction underpriced', retry: 'prepare' } });
    await age(address);
    expect(await allocate(store, address)).toBe(130);
  });

  it('takes nonce too low as success only when our hash is known and as a conflict otherwise', async () => {
    const address = '0x00000000000000000000000000000000000000d5';
    const store = makeStore();
    pendingCount = 140;
    sendError = 'nonce too low: next nonce 150, tx nonce 140';
    try {
      const unknown = await lockedSend(store, address, '0x02e0');
      expect(unknown.nonce).toBe(140);
      expect(unknown.error).toBeInstanceOf(poolMod.NonceConflictError);
      knownHashes.add(keccak256('0x02e1'));
      expect(await lockedSend(store, address, '0x02e1')).toEqual({ nonce: 141, error: null });
    } finally {
      sendError = null;
    }
    const { rows } = await pool.query<{ nonce: string; tx_hash: string }>(
      'select nonce::text, tx_hash from nonce_outstanding where address = $1 order by nonce_outstanding.nonce', [address]);
    expect(rows).toEqual([
      { nonce: '140', tx_hash: keccak256('0x02e0') },
      { nonce: '141', tx_hash: keccak256('0x02e1') },
    ]);
  });

  it('retries a timed-out send on another member with the same bytes and takes already known as our hash', async () => {
    const address = '0x00000000000000000000000000000000000000d7';
    const sentToB: string[] = [];
    const memberB = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on('data', (c: Buffer) => chunks.push(c));
      req.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
        if (body.method === 'eth_sendRawTransaction') sentToB.push(String(body.params[0]));
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify(body.method === 'eth_sendRawTransaction'
          ? { jsonrpc: '2.0', id: body.id, error: { code: -32000, message: 'already known' } }
          : { jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }));
      });
    });
    await new Promise<void>((r) => memberB.listen(0, '127.0.0.1', () => r()));
    const urlB = `http://127.0.0.1:${(memberB.address() as AddressInfo).port}`;
    const store = makeStore();
    pendingCount = 160;
    sendHangs = true;
    try {
      const both = new poolMod.RpcPool({ urls: [rpcUrl, urlB], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 150, healthIntervalMs: 60_000 });
      const sent = await store.withLock(address, async (lease) => {
        const nonce = await lease.next();
        return { nonce, ...(await storeMod.broadcastReserved(lease, nonce, '0x02f9', (r) => both.sendRawTransactionVia(r))) };
      });
      expect(sent).toEqual({ nonce: 160, hash: keccak256('0x02f9'), error: null });
      expect(sentToB).toEqual(['0x02f9']);
    } finally {
      sendHangs = false;
      await new Promise<void>((r) => memberB.close(() => r()));
    }
    const { rows } = await pool.query<{ nonce: string; tx_hash: string }>(
      'select nonce::text, tx_hash from nonce_outstanding where address = $1', [address]);
    expect(rows).toEqual([{ nonce: '160', tx_hash: keccak256('0x02f9') }]);
  });

  it('broadcasts a setup transaction under the lock, freeing a rejected nonce and holding a conflicting one', async () => {
    const address = '0x00000000000000000000000000000000000000d6';
    const store = makeStore();
    const rpc = makeRpc();
    const { sendSetupTransaction, ProvisionError } = await import('../src/provision/state.js');
    const persisted: Array<string | null> = [];
    const send = (raw: `0x${string}`) => sendSetupTransaction({ nonces: store, rpc }, address, async () => raw,
      async (signed) => { persisted.push(signed); });
    pendingCount = 150;
    sendError = 'insufficient funds for gas * price + value';
    const rejected = await send('0x02f0').catch((err: unknown) => err);
    expect(rejected).toBeInstanceOf(ProvisionError);
    expect(rejected).toMatchObject({ code: 'setup_send_rejected', status: 502 });
    expect(persisted).toEqual(['0x02f0', null]);
    sendError = 'replacement transaction underpriced';
    const conflict = await send('0x02f1').catch((err: unknown) => err);
    expect(conflict).toMatchObject({ code: 'nonce_conflict', status: 409 });
    expect(persisted.slice(2)).toEqual(['0x02f1', null]);
    sendError = null;
    const sent = await send('0x02f2');
    expect(sent).toEqual({ kind: 'sent', raw: '0x02f2', hash: keccak256('0x02f2'), error: null });
    const { rows } = await pool.query<{ nonce: string; tx_hash: string | null; member: string | null }>(
      'select nonce::text, tx_hash, member from nonce_outstanding where address = $1 order by nonce_outstanding.nonce', [address]);
    expect(rows).toEqual([
      { nonce: '150', tx_hash: keccak256('0x02f1'), member: null },
      { nonce: '151', tx_hash: keccak256('0x02f2'), member: rpcUrl },
    ]);
    sendHangs = true;
    const slow = makeRpc(150);
    try {
      const kept = await sendSetupTransaction({ nonces: store, rpc: slow }, address, async () => '0x02f3',
        async (signed) => { persisted.push(signed); });
      expect(kept.kind).toBe('kept');
      expect(kept.error).not.toBeNull();
    } finally {
      sendHangs = false;
    }
    expect(persisted.at(-1)).toBe('0x02f3');
    expect(await allocate(store, address)).toBe(153);
  });
});
