import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError, StreamTransportError, type V2ContractTypes } from '@kindle/sdk';
import { isPolicyBlockedError, isTransientError, networkRetryBudgetMs, readRetryFields, retryTransientOnly, transientRetry, transientRetryBudgetMs, transientRetryCapMs, transientRetryDelay, transientRetryLimit } from './transient-retry';
import { clearPolicyViolations, installPolicyViolationMonitor, policyBlockedOrigin, PolicyBlockedError, recordPolicyViolation } from './policy-block';
import { plainCopy, userFacingMessage } from './user-facing-error';
import { dataApiUrl } from './sdk-config';
import { loadable } from './market/unsupported';
import { AccountApiError } from '@/widgets/profile/account-transport';
import { LivestreamApiError } from './clients/livestream-api';

const envelope = (code: string, retryable: boolean, retryAfterMs: number | null): V2ContractTypes['Error'] => ({ code, message: code, requestId: 'r', retryable, retryAfterMs, fieldErrors: [], details: null });

describe('transient retry policy', () => {
  it('treats server-marked retryable failures with a delay, 429s and dropped connections as transient', () => {
    expect(isTransientError(new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', true, 1000)))).toBe(true);
    expect(isTransientError(new HttpError(429, envelope('RATE_LIMITED', true, 1000)))).toBe(true);
    expect(isTransientError(new HttpError(429))).toBe(true);
    expect(isTransientError(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(true);
    expect(isTransientError(new AccountApiError(503, 'DEPENDENCY_UNAVAILABLE', [], true, 1000))).toBe(true);
    expect(isTransientError(new LivestreamApiError(503, true, 1000))).toBe(true);
  });

  it('leaves every other failure to the caller', () => {
    expect(isTransientError(new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', false, null)))).toBe(false);
    expect(isTransientError(new HttpError(503, envelope('PROJECTION_UNAVAILABLE', true, null)))).toBe(false);
    expect(isTransientError(new HttpError(503))).toBe(false);
    expect(isTransientError(new HttpError(429, envelope('RATE_LIMITED', false, null)))).toBe(false);
    expect(isTransientError(new HttpError(422, envelope('FIELD_INVALID', false, null)))).toBe(false);
    expect(isTransientError(new HttpError(404))).toBe(false);
    expect(isTransientError(new CapabilityUnavailableError('Unavailable endpoint /v2/x', 404))).toBe(false);
    expect(isTransientError(new AccountApiError(401, 'AUTH_EXPIRED'))).toBe(false);
    expect(isTransientError(new LivestreamApiError(501))).toBe(false);
    expect(isTransientError(new Error('Leaderboard identity or rank disagreement'))).toBe(false);
    expect(isTransientError(new StreamTransportError('STREAM_CLOSED'))).toBe(false);
  });

  it('waits the server delay, doubling per failure, never past the cap', () => {
    const blip = new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', true, 1000));
    expect([0, 1, 2, 3, 4].map(n => transientRetryDelay(n, blip))).toEqual([1000, 2000, 4000, 8000, transientRetryCapMs]);
    expect(transientRetryDelay(0, new HttpError(429, envelope('RATE_LIMITED', true, 60_000)))).toBe(transientRetryCapMs);
    expect(transientRetryDelay(0, new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(1000);
  });

  it('bounds transient attempts and keeps the caller count for the rest', () => {
    const blip = new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', true, 1000));
    const attempts = (retry: (n: number, e: Error) => boolean, error: Error) => { let n = 0; while (retry(n, error)) n++; return n; };
    expect(attempts(retryTransientOnly.retry, blip)).toBe(transientRetryLimit);
    expect(attempts(retryTransientOnly.retry, new HttpError(422))).toBe(0);
    expect(attempts(transientRetry(1).retry, new HttpError(422))).toBe(1);
    expect(attempts(transientRetry(1).retry, blip)).toBe(transientRetryLimit);
  });

  it('reads retry fields from a failed response body', async () => {
    expect(await readRetryFields(new Response(JSON.stringify(envelope('DEPENDENCY_UNAVAILABLE', true, 1000)), { status: 503 }))).toEqual({ retryable: true, retryAfterMs: 1000 });
    expect(await readRetryFields(new Response('<html>bad gateway</html>', { status: 502 }))).toEqual({ retryable: null, retryAfterMs: null });
  });

  it('keeps last-good data when a refetch ran out of transient retries, but not for other failures', () => {
    const refetch = () => Promise.resolve() as never;
    const blip = new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', true, 1000));
    expect(loadable({ status: 'error', data: [1], error: blip, refetch }, 'IF_FEED')).toEqual({ state: 'ready', data: [1] });
    expect(loadable({ status: 'error', data: undefined, error: blip, refetch }, 'IF_FEED')).toMatchObject({ state: 'unsupported', status: 503 });
    expect(loadable({ status: 'error', data: [1], error: new HttpError(422), refetch }, 'IF_FEED').state).toBe('error');
  });
});

/**
 * Wall-clock behaviour through a real React Query client: how long a read
 * stays loading before its failure becomes visible, and how many requests it
 * made. The query function is the test's own failing read; fake timers only
 * move the clock.
 */
async function timeToFailure(error: Error, options = transientRetry(1)): Promise<{ attempts: number; elapsedMs: number }> {
  const client = new QueryClient();
  let attempts = 0;
  const started = Date.now();
  let settledAt: number | null = null;
  const run = client.fetchQuery({ queryKey: ['wall', error.message, Math.random()], queryFn: async () => { attempts++; throw error; }, ...options })
    .catch(() => { settledAt = Date.now(); });
  for (let waited = 0; settledAt === null && waited < 120_000; waited += 100) await vi.advanceTimersByTimeAsync(100);
  await run;
  client.clear();
  return { attempts, elapsedMs: (settledAt ?? Number.POSITIVE_INFINITY) - started };
}

describe('retry ownership and wall-time budget', () => {
  afterEach(() => { vi.useRealTimers(); clearPolicyViolations(); });

  it('adds no query-layer retries to an SDK network interruption (the SDK transport already retried the read)', async () => {
    vi.useFakeTimers();
    const outcome = await timeToFailure(new StreamTransportError('NETWORK_INTERRUPTED'));
    expect(outcome).toEqual({ attempts: 1, elapsedMs: 0 });
    // Still transient for display: last-good data is kept while background refetches continue.
    expect(isTransientError(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(true);
  });

  it('surfaces a query-owned fetch failure within the network budget, well under 3 s', async () => {
    vi.useFakeTimers();
    const outcome = await timeToFailure(new TypeError('Failed to fetch'));
    expect(outcome.attempts).toBe(2);
    expect(outcome.elapsedMs).toBeLessThanOrEqual(networkRetryBudgetMs);
    expect(outcome.elapsedMs).toBeLessThan(3_000);
  });

  it('bounds server-advised retries by wall time as well as attempts', async () => {
    vi.useFakeTimers();
    const blip = new HttpError(503, envelope('DEPENDENCY_UNAVAILABLE', true, 1000));
    expect(await timeToFailure(blip)).toEqual({ attempts: 5, elapsedMs: 15_000 });
    // A long server delay hits the wall budget before the attempt limit: 15 s cap, then nothing more fits.
    const slow = new HttpError(429, envelope('RATE_LIMITED', true, 60_000));
    const outcome = await timeToFailure(slow);
    expect(outcome.attempts).toBe(2);
    expect(outcome.elapsedMs).toBeLessThanOrEqual(transientRetryBudgetMs);
  });

  it('classifies a CSP-refused connection as a non-retried configuration fault', async () => {
    vi.useFakeTimers();
    const dataOrigin = new URL(dataApiUrl('/')).origin;
    expect(policyBlockedOrigin()).toBeNull();
    expect(userFacingMessage(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(plainCopy.network);

    // The browser reports the block as a securitypolicyviolation event on the document.
    const doc = new EventTarget();
    installPolicyViolationMonitor(doc);
    doc.dispatchEvent(Object.assign(new Event('securitypolicyviolation'), { blockedURI: `${dataOrigin}/v2/pools`, effectiveDirective: 'connect-src' }));
    expect(policyBlockedOrigin()).toBe(dataOrigin);

    for (const error of [new StreamTransportError('NETWORK_INTERRUPTED'), new TypeError('Failed to fetch')]) {
      expect(isPolicyBlockedError(error)).toBe(true);
      expect(isTransientError(error)).toBe(false);
      expect(userFacingMessage(error)).toBe(plainCopy.configuration);
      expect(await timeToFailure(error)).toEqual({ attempts: 1, elapsedMs: 0 });
    }
    expect(userFacingMessage(new PolicyBlockedError(dataOrigin))).toBe(plainCopy.configuration);

    // The violation ages out: a later failure is an outage again.
    vi.advanceTimersByTime(31_000);
    expect(policyBlockedOrigin()).toBeNull();
    expect(isPolicyBlockedError(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(false);
  });

  it('ignores violations of hosts the bundle never contacts, and flags a URL outside the allowed origins', () => {
    recordPolicyViolation({ blockedURI: 'https://beacon.third-party.example/collect', effectiveDirective: 'connect-src' });
    recordPolicyViolation({ blockedURI: new URL(dataApiUrl('/')).origin, effectiveDirective: 'img-src' });
    expect(policyBlockedOrigin()).toBeNull();
    expect(policyBlockedOrigin('https://api.kindlelaunch.com/v2/pools')).toBe('https://api.kindlelaunch.com');
    expect(policyBlockedOrigin(dataApiUrl('/v2/pools'))).toBeNull();
  });
});
