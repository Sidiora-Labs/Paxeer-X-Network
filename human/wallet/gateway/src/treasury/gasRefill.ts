import type { Hex } from 'viem';
import { env } from '../env.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  TREASURY_LOCK_KEY,
  readPaxBalance,
  treasuryNativeTransfer,
  waitForTxReceipt,
} from './index.js';

/**
 * Auto top-up: before signing a funded-wallet tx, ensure the wallet holds
 * enough native PAX to pay gas. If it doesn't, the treasury sends a refill
 * and we wait for inclusion before letting the user's tx proceed.
 *
 * Why pre-sign instead of post-fail-retry:
 *   - viem fails fast with "insufficient funds" before broadcast if PAX is
 *     too low to cover gas*gasPrice. We'd burn an extra RPC round trip and
 *     surface a confusing 500 to the user.
 *   - The treasury knows its inventory and can refill in a known-bounded
 *     window (one block, ~2s on chain 125). User-perceived latency stays
 *     under ~3-4s worst case.
 *
 * In-flight de-duplication:
 *   - Two concurrent sign requests for the same funded wallet would both
 *     trigger a refill if we didn't dedupe. We coalesce on `lower(address)`
 *     so at most one refill is pending per wallet at any time. Subsequent
 *     callers await the same Promise.
 *
 * Failure mode:
 *   - If the refill tx itself fails (treasury empty, RPC down), the function
 *     throws GasRefillFailedError. The route layer turns it into a clear 503
 *     so the user knows it's a system issue, not their tx.
 */

const inflight = new Map<string, Promise<RefillResult>>();

export interface RefillResult {
  refilled: boolean;
  /** PAX balance observed BEFORE the refill (or current if not refilled). */
  before_wei: bigint;
  /** Tx hash if a refill landed; null if no refill was needed. */
  tx_hash: Hex | null;
}

export class GasRefillFailedError extends Error {
  constructor(
    message: string,
    public readonly walletAddress: string,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = 'GasRefillFailedError';
  }
}

/**
 * Ensure a funded wallet has enough PAX to pay gas. Returns once balance is
 * confirmed at or above threshold (either it already was, or a refill landed).
 *
 * Safe to call from any code path — fully idempotent under concurrency.
 */
export async function ensureGas(walletAddress: `0x${string}`): Promise<RefillResult> {
  const key = walletAddress.toLowerCase();
  const existing = inflight.get(key);
  if (existing) return existing;

  const p = (async (): Promise<RefillResult> => {
    const before = await readPaxBalance(walletAddress);
    if (before >= env.FUNDED_GAS_REFILL_THRESHOLD_WEI) {
      return { refilled: false, before_wei: before, tx_hash: null };
    }

    // Below threshold → refill via the treasury, serialised on TREASURY_LOCK_KEY.
    let txHash: Hex;
    try {
      txHash = await withWalletLock(TREASURY_LOCK_KEY, () =>
        treasuryNativeTransfer({
          to: walletAddress,
          amount: env.FUNDED_GAS_REFILL_AMOUNT_WEI,
        }),
      );
      // Wait for inclusion so the user's subsequent tx sees the new balance.
      await waitForTxReceipt(txHash, 15_000);
    } catch (err) {
      throw new GasRefillFailedError(
        `failed to refill PAX for ${walletAddress}: ${err instanceof Error ? err.message : String(err)}`,
        walletAddress,
        err,
      );
    }
    return { refilled: true, before_wei: before, tx_hash: txHash };
  })();

  inflight.set(key, p);
  try {
    return await p;
  } finally {
    // Drop the entry whether the refill succeeded or failed. A failed refill
    // means the NEXT request triggers a fresh attempt rather than re-surfacing
    // the same error (the treasury may have been topped up in the meantime).
    inflight.delete(key);
  }
}
