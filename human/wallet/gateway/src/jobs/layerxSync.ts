import type { FastifyBaseLogger } from 'fastify';
import cluster from 'node:cluster';
import { env } from '../env.js';
import { getPool } from '../db/pool.js';
import { getDepositByTx, layerxEnabled, listAccountsUpdatedSince } from '../layerx/db.js';
import { latestMirroredAt, upsertLayerxAccount } from '../db/layerxMirror.js';
import { listUncreditedLayerxDeposits, markCreditVerified } from '../db/actions.js';

/**
 * LayerX mirror-sync + credit-backfill worker (deep ecosystem integration).
 *
 * We own the network, the wallet, LayerX and the agents, so instead of
 * event-scraping we read the LayerX sequencer Postgres directly to:
 *   1. mirror the per-DID escrow/balance head into layerx_accounts (an in-house
 *      DID → agent-wallet → owner view, no cross-service call on the hot path), and
 *   2. backfill credit_verified on confirmed layerx_deposit actions once the
 *      sequencer's deposit watcher has credited them (deposits.deposit_tx match).
 *
 * Multi-worker safe: only the primary (or worker 1 in cluster mode) runs the
 * sync, guarded by a Postgres advisory lock so exactly one syncs per tick even
 * across processes. Read-only against LayerX; degrades to a no-op when
 * LAYER_X_DB_URI is unset.
 */

export interface LayerxSyncHandle {
  stop: () => void;
}

const SYNC_LOCK_KEY = 'paxeer:layerx-sync';

export function startLayerxSync(log: FastifyBaseLogger): LayerxSyncHandle | null {
  if (!layerxEnabled()) {
    log.warn('[layerx-sync] LAYER_X_DB_URI unset — mirror + credit backfill disabled');
    return null;
  }

  let stopped = false;
  let timer: NodeJS.Timeout | null = null;

  const tick = async (): Promise<void> => {
    if (stopped) return;
    try {
      await withSyncLock(async () => {
        await syncAccounts(log);
        await backfillCredits(log);
      });
    } catch (err) {
      log.warn({ err: (err as Error).message }, '[layerx-sync] tick error');
    } finally {
      if (!stopped) timer = setTimeout(() => void tick(), env.LAYERX_SYNC_INTERVAL_MS);
    }
  };

  log.info('[layerx-sync] started');
  timer = setTimeout(() => void tick(), 5_000); // small initial delay after boot

  return {
    stop: () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    },
  };
}

/** Pull LayerX accounts touched since our cursor and upsert into the mirror. */
async function syncAccounts(log: FastifyBaseLogger): Promise<void> {
  const cursor = await latestMirroredAt();
  const accounts = await listAccountsUpdatedSince(cursor, 500);
  for (const a of accounts) {
    await upsertLayerxAccount({
      did: a.did,
      evmAddress: a.evm_address,
      balanceUsdx: a.balance_usdx,
      escrowUsdx: a.escrow_usdx,
      layerxUpdated: a.updated_at,
    });
  }
  if (accounts.length > 0) log.info({ n: accounts.length }, '[layerx-sync] mirrored accounts');
}

/** Flip confirmed deposits to credit-verified once the LayerX credit lands. */
async function backfillCredits(log: FastifyBaseLogger): Promise<void> {
  const pending = await listUncreditedLayerxDeposits(100);
  let flipped = 0;
  for (const action of pending) {
    if (!action.call_tx_hash) continue;
    const deposit = await getDepositByTx(action.call_tx_hash).catch(() => null);
    if (deposit) {
      await markCreditVerified(action.id, {
        did: deposit.did,
        amount_usdx: deposit.amount_usdx,
        deposit_tx: deposit.deposit_tx,
        credited_at: deposit.created_at,
      });
      flipped++;
    }
  }
  if (flipped > 0) log.info({ n: flipped }, '[layerx-sync] backfilled deposit credits');
}

/**
 * Run `fn` only if we win a non-blocking advisory lock, so exactly one worker
 * across the cluster syncs per tick. Others skip cleanly.
 */
async function withSyncLock(fn: () => Promise<void>): Promise<void> {
  // Cheap pre-filter: in cluster mode only worker id 1 even tries.
  if (cluster.isWorker && cluster.worker && cluster.worker.id !== 1) return;

  const client = await getPool().connect();
  try {
    const { rows } = await client.query<{ locked: boolean }>(
      'select pg_try_advisory_lock(hashtextextended($1, 0)) as locked',
      [SYNC_LOCK_KEY],
    );
    if (!rows[0]?.locked) return;
    try {
      await fn();
    } finally {
      await client
        .query('select pg_advisory_unlock(hashtextextended($1, 0))', [SYNC_LOCK_KEY])
        .catch(() => undefined);
    }
  } finally {
    client.release();
  }
}
