import pg from 'pg';
import { NonceStore } from '../../src/nonce/store.js';
import { RpcPool } from '../../src/rpc/pool.js';

async function main(): Promise<void> {
  const url = process.env.NONCE_WORKER_DATABASE_URL;
  const rpcUrl = process.env.NONCE_WORKER_RPC_URL;
  const address = process.env.NONCE_WORKER_ADDRESS as `0x${string}` | undefined;
  const count = Number(process.env.NONCE_WORKER_COUNT);
  if (!url || !rpcUrl || !address || !Number.isInteger(count) || count <= 0) {
    throw new Error('nonce worker needs its database URL, RPC URL, address and count');
  }
  const pool = new pg.Pool({ connectionString: url, max: 10 });
  const rpc = new RpcPool({ urls: [rpcUrl], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
  const store = new NonceStore({
    pool,
    chainId: 125,
    pendingCount: (a) => rpc.getTransactionCount(a, 'pending'),
  });
  await new Promise<void>((resolve) => {
    process.once('message', () => resolve());
    process.send?.({ ready: true });
  });
  const nonces = await Promise.all(Array.from({ length: count }, () => store.allocate(address)));
  await pool.end();
  await new Promise<void>((resolve) => process.send?.({ pid: process.pid, nonces }, () => resolve()));
}

main().then(
  () => process.exit(0),
  (err: unknown) => {
    process.send?.({ error: err instanceof Error ? err.message : String(err) });
    process.exit(1);
  },
);
