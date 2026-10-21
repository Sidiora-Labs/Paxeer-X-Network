import { describe, expect, it } from 'vitest';
import { CapabilityUnavailableError, HttpError, StreamTransportError } from '@kindle/sdk';
import { PoolNotFoundError } from '@/core/market/pool-resolver';
import { copyKeyForCode, errorTechnicalDetail, isNotFoundError, isRawErrorText, plainCopy, plainText, reportUserFacingError, technicalDetail, toUserFacingError, UserFacingError, userFacingMessage } from './user-facing-error';

const leak = /NETWORK_INTERRUPTED|TRANSPORT_ERROR|IF_[A-Z]|HTTP \d{3}|GET \/|no response|\{p\}|_[A-Z]{3,}/;

describe('userFacingMessage', () => {
  it('rewords real SDK transport and HTTP failures into plain sentences', () => {
    expect(userFacingMessage(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(plainCopy.network);
    expect(userFacingMessage(new HttpError(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'x', retryable: true, retryAfterMs: 1000 } as never))).toBe(plainCopy.notAvailable);
    expect(userFacingMessage(new HttpError(500))).toBe(plainCopy.server);
    expect(userFacingMessage(new HttpError(429))).toBe(plainCopy.rateLimited);
    expect(userFacingMessage(new HttpError(401))).toBe(plainCopy.signIn);
    expect(userFacingMessage(new CapabilityUnavailableError())).toBe(plainCopy.notAvailable);
    expect(userFacingMessage(new TypeError('Failed to fetch'))).toBe(plainCopy.network);
    expect(userFacingMessage(new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation"))).toBe(plainCopy.network);
    expect(userFacingMessage(new DOMException('The operation was aborted.', 'AbortError'))).toBe(plainCopy.timeout);
  });

  it('gives livestream and gateway codes their own plain wording', () => {
    expect(copyKeyForCode('BROADCAST_NOT_PROVISIONED')).toBe('broadcastNotProvisioned');
    expect(copyKeyForCode('STATE_CONFLICT')).toBe('stateConflict');
    expect(copyKeyForCode('AUTH_EXPIRED')).toBe('sessionExpired');
    expect(copyKeyForCode('upstream_unavailable')).toBe('upstreamUnavailable');
    expect(copyKeyForCode('REVISION_CONFLICT')).toBe('conflict');
    expect(copyKeyForCode('FIELD_INVALID')).toBe('invalid');
    expect(plainText('BROADCAST_NOT_PROVISIONED')).toBe('Going live is not available yet');
    expect(plainText('upstream_unavailable')).toBe(plainCopy.upstreamUnavailable);
    expect(userFacingMessage({ status: 401, code: 'AUTH_EXPIRED' })).toBe(plainCopy.sessionExpired);
    for (const key of Object.keys(plainCopy) as (keyof typeof plainCopy)[]) expect(isRawErrorText(plainCopy[key])).toBe(false);
  });

  it('keeps sentences written for people and falls back for unknown machine text', () => {
    expect(userFacingMessage(new Error('Enter a complete address to search by address.'))).toBe('Enter a complete address to search by address.');
    expect(userFacingMessage(new Error('SOMETHING_ODD'), 'Fallback.')).toBe('Fallback.');
    expect(userFacingMessage(null, 'Fallback.')).toBe('Fallback.');
    expect(userFacingMessage('NETWORK_INTERRUPTED')).toBe(plainCopy.network);
  });

  it('treats internal vocabulary as machine text even when it reads like a sentence', () => {
    for (const raw of ['Ranking integrity quarantined', 'Statistics identity disagreement', 'Approved network input is unavailable', 'Invalid chat response', 'Session verification failed', 'upstream returned 503', 'Pool statistics are not served by this API version.']) {
      expect(isRawErrorText(raw), raw).toBe(true);
      expect(userFacingMessage(new Error(raw), 'Fallback')).toBe('Fallback');
    }
  });

  it('passes UserFacingError wording through and keeps the original as cause', () => {
    const original = new Error('Ranking integrity quarantined');
    const wrapped = toUserFacingError(original, "Can't load rankings right now");
    expect(wrapped).toBeInstanceOf(UserFacingError);
    expect(wrapped.message).toBe("Can't load rankings right now");
    expect(wrapped.cause).toBe(original);
    expect(userFacingMessage(wrapped)).toBe("Can't load rankings right now");
    const errors: unknown[][] = [];
    const log = console.error;
    console.error = (...args: unknown[]) => { errors.push(args); };
    try { expect(reportUserFacingError(original, 'Try again', 'ranking')).toBe('Try again'); } finally { console.error = log; }
    expect(errors).toEqual([['[ranking]', original]]);
  });

  it('never returns a code, an operation id or a path', () => {
    const inputs: unknown[] = [
      new StreamTransportError('NETWORK_INTERRUPTED'), new HttpError(502), new HttpError(404, undefined, 'NOT_FOUND'),
      new Error('HTTP 503: DEPENDENCY_UNAVAILABLE'), new Error('IF_POOL_STATS.athUsdlRaw'), new Error('GET /bff/token/{p}'),
      'capability_unavailable', 'Trending is unavailable: NETWORK_INTERRUPTED', new PoolNotFoundError('0x0000000000000000000000000000000000000001'),
    ];
    for (const input of inputs) expect(userFacingMessage(input)).not.toMatch(leak);
  });
});

describe('plainText and isRawErrorText', () => {
  it('flags codes, operation ids, request paths and HTTP prefixes', () => {
    for (const raw of ['NETWORK_INTERRUPTED', 'HTTP 404', 'IF_CANDLES.trades (not split by side)', 'GET /bff/token/{p}', 'capability_unavailable', 'Load failed']) expect(isRawErrorText(raw)).toBe(true);
    for (const plain of ['The market feed could not be reached.', 'No pools match this search.']) expect(isRawErrorText(plain)).toBe(false);
  });
  it('maps HTTP-prefixed messages by envelope code first, then by status', () => {
    expect(plainText('HTTP 503: RATE_LIMITED')).toBe(plainCopy.rateLimited);
    expect(plainText('HTTP 502')).toBe(plainCopy.server);
    expect(plainText(undefined, 'Nothing yet.')).toBe('Nothing yet.');
  });
});

describe('isNotFoundError', () => {
  it('is true for the resolver 404 and HTTP 404, false for transport failures', () => {
    expect(isNotFoundError(new PoolNotFoundError('0x0000000000000000000000000000000000000001'))).toBe(true);
    expect(isNotFoundError(new HttpError(404))).toBe(true);
    expect(isNotFoundError(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(false);
    expect(isNotFoundError(new HttpError(503))).toBe(false);
  });
});

describe('technicalDetail', () => {
  it('never produces developer detail for the screen', () => {
    expect(technicalDetail('IF_POOL_STATS', null, 'development')).toBeNull();
    expect(technicalDetail('IF_POOL_STATS', 404, 'production')).toBeNull();
    expect(errorTechnicalDetail(new StreamTransportError('NETWORK_INTERRUPTED'), 'development')).toBeNull();
  });
});
