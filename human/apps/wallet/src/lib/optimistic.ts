/**
 * Optimistic Update — Persisted Pending Send
 *
 * Problem: SendPage fires the event, but PortfolioPage is unmounted
 * (user sees TransferSuccess). By the time they navigate back,
 * the in-memory event is lost.
 *
 * Solution: Persist the pending send to sessionStorage.
 * PortfolioPage reads + clears it on mount, applies the optimistic
 * subtraction, then schedules a cache-busted reconcile.
 *
 * Flow:
 *   1. SendPage calls storePendingSend() after successful tx
 *   2. User sees TransferSuccess, taps "Done", navigates to PortfolioPage
 *   3. PortfolioPage calls consumePendingSend() on mount
 *      → returns the event if one exists, clears it from storage
 *   4. PortfolioPage applies optimistic subtraction to displayed balance
 *   5. After RECONCILE_DELAY_MS, fetches with ?fresh=true to confirm
 */

import {
  pendingSendRepository,
  type PendingSendRecord,
} from '@/platform/storage/repositories';

export type OptimisticSendEvent = PendingSendRecord;

export const RECONCILE_DELAY_MS = 60000;
const MAX_AGE_MS = 120000; // ignore pending sends older than 2 minutes

/**
 * Store a pending send in sessionStorage.
 * Called by SendPage after a successful transaction.
 */
export function storePendingSend(event: OptimisticSendEvent): void {
  pendingSendRepository.write(event);
}

/**
 * Read and clear the pending send from sessionStorage.
 * Called by PortfolioPage on mount. Returns null if none or expired.
 */
export function consumePendingSend(): OptimisticSendEvent | null {
  const event = pendingSendRepository.read();
  pendingSendRepository.remove();
  if (!event || Date.now() - event.timestamp > MAX_AGE_MS) return null;
  return event;
}

/**
 * Subtract a human-readable amount from a raw balance string.
 * Returns the new raw balance as a string.
 * If subtraction would go negative, returns "0".
 */
export function subtractFromRawBalance(
  rawBalance: string,
  amount: string,
  decimals: number,
): string {
  try {
    const current = BigInt(rawBalance || '0');
    const parts = amount.split('.');
    const whole = BigInt(parts[0] || '0') * BigInt(10) ** BigInt(decimals);
    let frac = BigInt(0);
    if (parts[1]) {
      const fracStr = parts[1].padEnd(decimals, '0').slice(0, decimals);
      frac = BigInt(fracStr);
    }
    const subtracted = whole + frac;
    const result = current - subtracted;
    return result > BigInt(0) ? result.toString() : '0';
  } catch {
    return rawBalance;
  }
}
