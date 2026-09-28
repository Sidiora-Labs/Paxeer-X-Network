/**
 * Per-wallet in-process serialisation.
 *
 * Critical for `/v1/wallet/send`: viem's `sendTransaction` internally fetches
 * the current nonce via `eth_getTransactionCount`, then broadcasts. If two
 * concurrent requests for the same wallet land on the same worker process
 * within ~one block time, both fetch nonce N, both broadcast — one succeeds,
 * the other fails with "nonce too low" and silently drops.
 *
 * `withWalletLock(addr, fn)` queues `fn` behind any prior in-flight calls
 * for the same key so nonces advance monotonically. Cheap: ~microseconds
 * overhead, zero allocations under contention beyond a single Promise.
 *
 * IMPORTANT — multi-process caveat:
 *   This is an *in-process* lock. With Node `cluster` mode (API_WORKERS>1),
 *   two workers can still race the same wallet. That's fine as long as RPC
 *   load-balancer + chain mempool de-duplicate identical-nonce txs (every
 *   sane EVM RPC does), at the cost of one wasted send round-trip per
 *   collision. If you need a true cross-process lock, swap this for a
 *   Postgres advisory lock keyed off the wallet id:
 *     SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
 *   That gives correctness at the cost of one DB round trip per send.
 */

// Tail of the promise chain per key. Each new caller awaits the current tail,
// then becomes the new tail. Tails are GC'd when the chain drains.
const tails = new Map<string, Promise<unknown>>();

export async function withWalletLock<T>(
  key: string,
  fn: () => Promise<T>,
): Promise<T> {
  const prev = tails.get(key) ?? Promise.resolve();
  // Run fn after prev settles (success OR failure — a thrown fn must not
  // block subsequent calls).
  const next = prev.then(fn, fn);
  // Store a swallowed version so a rejection up the chain doesn't propagate
  // forward and poison future calls.
  const swallowed = next.then(
    () => undefined,
    () => undefined,
  );
  tails.set(key, swallowed);
  try {
    return await next;
  } finally {
    // GC: if nothing queued behind us, drop the entry to keep the map
    // bounded. If a later caller raced ahead, leave their tail in place.
    if (tails.get(key) === swallowed) tails.delete(key);
  }
}

/** Test / introspection helper — current number of locked keys. */
export function _walletLockSize(): number {
  return tails.size;
}
