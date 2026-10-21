import { describe, it, expect } from 'vitest';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { actionFailureMessage, isNotServedError, isSignedOutError, isUnsupported, loadable, toUnsupported, unsupported } from './unsupported';
import { AccountApiError } from '@/widgets/profile/account-transport';

describe('unsupported markers', () => {
  it('maps CapabilityUnavailableError to the operation and its status', () => {
    const marker = toUnsupported('IF_POOL_DETAIL', new CapabilityUnavailableError('Unavailable endpoint /v2/pools/0x1', 404));
    expect(marker).toEqual({ state: 'unsupported', operation: 'IF_POOL_DETAIL', status: 404, message: 'Unavailable endpoint /v2/pools/0x1' });
    expect(toUnsupported('IF_X', new CapabilityUnavailableError())?.status).toBe(501);
  });
  it('maps HttpError 404, 501, 503 and 412 and keeps the envelope code', () => {
    for (const status of [404, 501, 503, 412]) {
      const marker = toUnsupported('IF_LEADERBOARDS', new HttpError(status, { code: 'DEPENDENCY_UNAVAILABLE', message: 'x', requestId: 'r', retryable: status === 503, retryAfterMs: null, fieldErrors: [], details: null }));
      expect(marker).toEqual({ state: 'unsupported', operation: 'IF_LEADERBOARDS', status, message: 'DEPENDENCY_UNAVAILABLE' });
    }
    expect(toUnsupported('IF_LEADERBOARDS', new HttpError(422))?.message).toBeUndefined();
  });
  it('refuses to classify other failures as unsupported', () => {
    expect(toUnsupported('IF_STATS', new HttpError(500))).toBeNull();
    expect(toUnsupported('IF_STATS', new HttpError(422))).toBeNull();
    expect(toUnsupported('IF_STATS', new Error('Ranking integrity quarantined'))).toBeNull();
    expect(toUnsupported('IF_STATS', new TypeError('Invalid v2 contract'))).toBeNull();
  });
  it('recognises its own markers only', () => {
    expect(isUnsupported(unsupported('IF_HOLDERS', null, 'Stats host not configured'))).toBe(true);
    expect(isUnsupported({ state: 'ready', data: [] })).toBe(false);
    expect(isUnsupported({ state: 'unsupported' })).toBe(false);
    expect(isUnsupported(null)).toBe(false);
  });
  it('adapts query results into Loadable states', () => {
    const refetch = () => Promise.resolve() as never;
    expect(loadable({ status: 'pending', data: undefined, error: null, refetch }, 'IF_FEED')).toEqual({ state: 'loading' });
    expect(loadable({ status: 'success', data: [1], error: null, refetch }, 'IF_FEED')).toEqual({ state: 'ready', data: [1] });
    expect(loadable({ status: 'error', data: undefined, error: new HttpError(503), refetch }, 'IF_FEED')).toMatchObject({ state: 'unsupported', status: 503 });
    const failed = loadable({ status: 'error', data: undefined, error: new Error('boom'), refetch }, 'IF_FEED');
    expect(failed.state).toBe('error');
    if (failed.state === 'error') { expect(failed.error.message).toBe('boom'); expect(typeof failed.retry).toBe('function'); }
  });
});

describe('signed-out failures', () => {
  it('treats 401/403 and a missing session as signed out, everything else as not', () => {
    expect(isSignedOutError(new HttpError(401))).toBe(true);
    expect(isSignedOutError(new HttpError(403))).toBe(true);
    expect(isSignedOutError(new CapabilityUnavailableError('denied', 401))).toBe(true);
    expect(isSignedOutError(new TypeError('Session required'))).toBe(true);
    expect(isSignedOutError(new TypeError('Access token required'))).toBe(true);
    expect(isSignedOutError(Object.assign(new Error('unauthenticated'), { status: 401 }))).toBe(true);
    expect(isSignedOutError(new HttpError(500))).toBe(false);
    expect(isSignedOutError(new HttpError(404))).toBe(false);
    expect(isSignedOutError(new TypeError('Invalid IF_X query'))).toBe(false);
    expect(isSignedOutError(new DOMException('Request timed out', 'TimeoutError'))).toBe(false);
    expect(isSignedOutError(null)).toBe(false);
  });
});

describe('not-served failures (503 retryable:false)', () => {
  const envelope = (retryable: boolean) => ({ code: 'DEPENDENCY_UNAVAILABLE', message: 'no backing service', requestId: 'r', retryable }) as unknown as ConstructorParameters<typeof HttpError>[1];
  it('reads retryable:false from an SDK envelope or a transport error, and treats 501 as not served', () => {
    expect(isNotServedError(new HttpError(503, envelope(false)))).toBe(true);
    expect(isNotServedError(new AccountApiError(503, 'DEPENDENCY_UNAVAILABLE', [], false))).toBe(true);
    expect(isNotServedError(new CapabilityUnavailableError())).toBe(true);
    expect(isNotServedError(new HttpError(501))).toBe(true);
  });
  it('keeps a transient or unlabelled 503 and other statuses as ordinary failures', () => {
    expect(isNotServedError(new HttpError(503, envelope(true)))).toBe(false);
    expect(isNotServedError(new AccountApiError(503, 'DEPENDENCY_UNAVAILABLE', [], true, 1000))).toBe(false);
    expect(isNotServedError(new AccountApiError(503, 'HTTP_503'))).toBe(false);
    expect(isNotServedError(new AccountApiError(500, 'INTERNAL', [], false))).toBe(false);
    expect(isNotServedError(null)).toBe(false);
  });
  it('words an action failure plainly', () => {
    expect(actionFailureMessage('Portfolio export', new AccountApiError(503, 'DEPENDENCY_UNAVAILABLE', [], false))).toBe('Portfolio export is not available yet');
    expect(actionFailureMessage('Portfolio export', new AccountApiError(429, 'RATE_LIMITED'))).toBe('Too many requests, try again in a moment');
  });
});
