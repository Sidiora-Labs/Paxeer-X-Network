import type { FastifyBaseLogger } from 'fastify';
import { env } from '../../env.js';
import { claimNextAction, releaseLease } from '../../db/actions.js';
import { advanceAction } from './orchestrator.js';

/**
 * Background action worker + reconciler (one loop, both jobs).
 *
 * Because `agent_actions` is durable and `claimNextAction` selects ANY
 * non-terminal row whose lease has expired, this single loop provides BOTH:
 *   - forward progress for freshly-submitted actions, and
 *   - persistent reconciliation after a process restart / crash (on boot it
 *     simply finds the orphaned in-flight rows and resumes them — the tx hashes
 *     and nonces were persisted, so it never blind-resends).
 *
 * `SELECT ... FOR UPDATE SKIP LOCKED` + a per-row lease means N workers across
 * N processes cooperate safely: each grabs a different action, and a crashed
 * worker's lease expires so its action is picked up again.
 *
 * The loop is intentionally serial per tick (claim one, advance it, repeat a
 * bounded number of times) to keep RPC pressure predictable; scale by running
 * more workers, not by fanning out unbounded concurrency here.
 */

export interface WorkerHandle {
  stop: () => void;
}

const MAX_PER_TICK = 8;

export function startActionWorker(log: FastifyBaseLogger): WorkerHandle {
  let stopped = false;
  let timer: NodeJS.Timeout | null = null;

  const leaseMs = Math.max(env.ACTION_RECEIPT_TIMEOUT_MS, env.ACTION_WORKER_INTERVAL_MS * 4);

  const tick = async (): Promise<void> => {
    if (stopped) return;
    try {
      for (let i = 0; i < MAX_PER_TICK; i++) {
        const row = await claimNextAction(leaseMs);
        if (!row) break;
        try {
          await advanceAction(row);
        } finally {
          // Yield the lease so the very next tick can immediately continue the
          // action (waits happen INSIDE advanceAction, bounded by the receipt
          // timeout, so holding the lease across ticks is unnecessary).
          await releaseLease(row.id);
        }
      }
    } catch (err) {
      log.warn({ err: (err as Error).message }, '[action-worker] tick error');
    } finally {
      if (!stopped) timer = setTimeout(() => void tick(), env.ACTION_WORKER_INTERVAL_MS);
    }
  };

  log.info('[action-worker] started');
  timer = setTimeout(() => void tick(), env.ACTION_WORKER_INTERVAL_MS);

  return {
    stop: () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    },
  };
}
