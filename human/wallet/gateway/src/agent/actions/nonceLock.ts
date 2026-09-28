import { getPool } from '../../db/pool.js';
import { withWalletLock } from '../../util/walletLock.js';

/**
 * Distributed per-wallet nonce lock.
 *
 * The existing `withWalletLock` (util/walletLock.ts) only serialises within ONE
 * worker process. With API_WORKERS=12 two workers can still fetch the same
 * nonce and double-broadcast. For the durable-action lane — where a wedged
 * nonce silently blocks every later send — that is not acceptable, so we take a
 * TRUE cross-process lock: a Postgres session advisory lock keyed on the wallet
 * address, held on a DEDICATED checked-out client for the duration of `fn`.
 *
 * Layering: we wrap the in-process lock INSIDE the advisory lock so the common
 * same-worker case never even round-trips Postgres twice, while cross-worker
 * correctness is guaranteed by the advisory lock.
 *
 * The lock auto-releases if the client disconnects (crash), so a dead worker
 * can never wedge a wallet forever.
 */
const LOCK_NAMESPACE = 'paxeer:agent-nonce';

export async function withDistributedWalletLock<T>(
  address: string,
  fn: () => Promise<T>,
): Promise<T> {
  const key = `${LOCK_NAMESPACE}:${address.toLowerCase()}`;
  const client = await getPool().connect();
  try {
    await client.query('select pg_advisory_lock(hashtextextended($1, 0))', [key]);
    // Nest the in-process lock so two concurrent same-worker callers don't both
    // proceed the instant the advisory lock is granted to this process.
    return await withWalletLock(address.toLowerCase(), fn);
  } finally {
    await client
      .query('select pg_advisory_unlock(hashtextextended($1, 0))', [key])
      .catch(() => undefined);
    client.release();
  }
}
