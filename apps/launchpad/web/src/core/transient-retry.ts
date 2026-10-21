/**
 * Shared retry policy for reads whose failure the server marks as transient.
 *
 * core/api answers a dependency blip (redeploy, read-model reconnect) with
 * `503 DEPENDENCY_UNAVAILABLE {"retryable":true,"retryAfterMs":1000}` and a
 * request budget overrun with `429 RATE_LIMITED`. Those are not answers the
 * viewer should see: the query keeps its loading (or last-good) state and is
 * retried after the server's delay, growing per attempt and capped, and only
 * when the budget is spent does the hook fall to its usual failure state.
 *
 * Every other failure keeps the caller's own retry count (most reads: none),
 * so a 4xx, a contract validation error or an honest "not served" is surfaced
 * exactly as before.
 */
import { StreamTransportError } from '@kindle/sdk';
import { PolicyBlockedError, policyBlockedOrigin } from './policy-block';

/** Retries after the first failed attempt; 5 attempts in all at most. */
export const transientRetryLimit = 4;
/** Upper bound for a single wait, whatever the server asks for. */
export const transientRetryCapMs = 15_000;
/** Wait used when a transient failure carries no server delay (network drop, bare 429). */
export const transientRetryFallbackMs = 1_000;
/**
 * Wall-time budget of the waits the query layer adds for a server-advised
 * transient failure (retryable 503 / 429): a retry is scheduled only while the
 * sum of all waits so far, including it, stays within this budget.
 */
export const transientRetryBudgetMs = 15_000;
/**
 * Wall-time budget for a network-class failure the query layer owns (a plain
 * fetch that threw): the first user-visible unavailable state must appear
 * within about 3 s, after which the query's own schedule (refetchInterval,
 * focus, reconnect) keeps trying in the background.
 */
export const networkRetryBudgetMs = 2_000;

/**
 * Who owns the retries of a failure:
 *  - `server`    a retryable 503 / 429: the query layer retries on the server's delay within transientRetryBudgetMs;
 *  - `network`   a fetch that threw (no SDK below it): the query layer retries within networkRetryBudgetMs;
 *  - `transport` the SDK already retried the idempotent read before reporting NETWORK_INTERRUPTED: the query layer adds none;
 *  - `policy`    the Content-Security-Policy refused the connection: a configuration fault, never retried;
 *  - `none`      anything else: the caller's own retry count.
 */
export type RetryOwner = 'server' | 'network' | 'transport' | 'policy' | 'none';

interface RetryHint { transient: boolean; delayMs: number | null; owner: RetryOwner }

const notTransient: RetryHint = { transient: false, delayMs: null, owner: 'none' };
const policyBlock: RetryHint = { transient: false, delayMs: null, owner: 'policy' };

function finiteDelay(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
}

const fetchFailure = /failed to fetch|load failed|networkerror|network request failed/i;

/** True for a failure that looks like the connection never completed (SDK interruption or a fetch TypeError). */
export function isNetworkClassError(error: unknown): boolean {
  if (error instanceof StreamTransportError) return error.code === 'NETWORK_INTERRUPTED';
  return error instanceof TypeError && fetchFailure.test(error.message);
}

/**
 * Classify a failure. Transient when:
 *  - the error envelope says `retryable: true` and names `retryAfterMs`
 *    (SDK HttpError carries it as `envelope`, transports that decode the body
 *    themselves carry `retryable` / `retryAfterMs` on the error);
 *  - the status is 429 and the envelope does not say `retryable: false`;
 *  - the connection dropped (SDK NETWORK_INTERRUPTED or a fetch TypeError),
 *    unless the page's CSP refused it (core/policy-block.ts): a policy block
 *    is a configuration fault, not transient.
 */
export function retryHint(error: unknown): RetryHint {
  if (error instanceof PolicyBlockedError) return policyBlock;
  if (isNetworkClassError(error)) {
    if (policyBlockedOrigin() !== null) return policyBlock;
    return { transient: true, delayMs: null, owner: error instanceof StreamTransportError ? 'transport' : 'network' };
  }
  if (error instanceof StreamTransportError) return notTransient;
  if (typeof error !== 'object' || error === null) return notTransient;
  const carrier = error as { status?: unknown; envelope?: unknown; retryable?: unknown; retryAfterMs?: unknown };
  const envelope = typeof carrier.envelope === 'object' && carrier.envelope !== null ? carrier.envelope as { retryable?: unknown; retryAfterMs?: unknown } : carrier;
  const status = typeof carrier.status === 'number' ? carrier.status : null;
  if (status === null) return notTransient;
  if (envelope.retryable === false) return notTransient;
  const delayMs = finiteDelay(envelope.retryAfterMs);
  if (envelope.retryable === true && delayMs !== null) return { transient: true, delayMs, owner: 'server' };
  if (status === 429) return { transient: true, delayMs, owner: 'server' };
  return notTransient;
}

export function isTransientError(error: unknown): boolean {
  return retryHint(error).transient;
}

/** True when the page's Content-Security-Policy refused the failed connection. */
export function isPolicyBlockedError(error: unknown): boolean {
  return retryHint(error).owner === 'policy';
}

/** Server delay (or the fallback) doubled per failure, capped at transientRetryCapMs. */
export function transientRetryDelay(failureCount: number, error: unknown): number {
  const base = retryHint(error).delayMs ?? transientRetryFallbackMs;
  return Math.min(transientRetryCapMs, Math.max(0, base) * 2 ** Math.max(0, failureCount));
}

/** Total wait the query layer will have spent once the retry after `failureCount` failures has waited. */
export function transientRetryElapsed(failureCount: number, error: unknown): number {
  let total = 0;
  for (let n = 0; n <= failureCount; n++) total += transientRetryDelay(n, error);
  return total;
}

/** React Query's own default backoff for non-transient retries. */
function defaultDelay(failureCount: number): number {
  return Math.min(1000 * 2 ** failureCount, 30_000);
}

/** Typed with `Error` (React Query's default error type) so spreading it never widens a query's error to unknown. */
export interface TransientRetryOptions {
  retry: (failureCount: number, error: Error) => boolean;
  retryDelay: (failureCount: number, error: Error) => number;
}

/** Whether the query layer retries after `failureCount` failures (React Query passes 0 after the first). */
export function shouldRetry(failureCount: number, error: unknown, otherRetries = 0): boolean {
  const hint = retryHint(error);
  switch (hint.owner) {
    case 'policy':
    case 'transport':
      return false;
    case 'network':
      return failureCount < transientRetryLimit && transientRetryElapsed(failureCount, error) <= networkRetryBudgetMs;
    case 'server':
      return failureCount < transientRetryLimit && transientRetryElapsed(failureCount, error) <= transientRetryBudgetMs;
    default:
      return failureCount < otherRetries;
  }
}

/**
 * React Query `retry` / `retryDelay` pair, the one owner of query-layer
 * retries: server-advised transient failures retry on the server's delay
 * within transientRetryBudgetMs; a plain fetch that threw retries within
 * networkRetryBudgetMs; an SDK NETWORK_INTERRUPTED is not retried again (the
 * SDK transport already retried the idempotent read); a CSP block is never
 * retried; anything else retries `otherRetries` times (0 for the typed market
 * reads). Background refetching stays on the query's own schedule.
 */
export function transientRetry(otherRetries = 0): TransientRetryOptions {
  return {
    retry: (failureCount, error) => shouldRetry(failureCount, error, otherRetries),
    retryDelay: (failureCount, error) => isTransientError(error) ? transientRetryDelay(failureCount, error) : defaultDelay(failureCount),
  };
}

/** The policy for reads that otherwise never retry. */
export const retryTransientOnly: TransientRetryOptions = transientRetry(0);

export interface RetryFields { retryable: boolean | null; retryAfterMs: number | null }

/**
 * Read `retryable` / `retryAfterMs` from a failed response's JSON error body,
 * for transports that do not go through the SDK. Consumes the body; a body
 * that is not a JSON object yields nulls.
 */
export async function readRetryFields(response: Response): Promise<RetryFields> {
  const body: unknown = await response.json().catch(() => null);
  if (typeof body !== 'object' || body === null) return { retryable: null, retryAfterMs: null };
  const row = body as { retryable?: unknown; retryAfterMs?: unknown };
  return { retryable: typeof row.retryable === 'boolean' ? row.retryable : null, retryAfterMs: finiteDelay(row.retryAfterMs) };
}
