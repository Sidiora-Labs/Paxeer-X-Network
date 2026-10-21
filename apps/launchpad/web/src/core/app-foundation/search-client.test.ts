import { describe, expect, it } from 'vitest';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { USER_SEARCH_OPERATION, checkedSearchMetadata, normalizedSearch, userSearchFailure } from './search-client';

describe('normalizedSearch', () => {
  it('lowercases a complete address and trims surrounding whitespace', () => {
    expect(normalizedSearch('  0xABCDEFabcdef0123456789ABCDEFabcdef012345 ')).toBe('0xabcdefabcdef0123456789abcdefabcdef012345');
  });
  it('keeps free text and rejects queries over 80 characters', () => {
    expect(normalizedSearch(' wired ')).toBe('wired');
    expect(() => normalizedSearch('x'.repeat(81))).toThrow(TypeError);
  });
});

describe('userSearchFailure', () => {
  it('maps the stats-workers 412 for type=profile onto an unavailable marker for the Users group', () => {
    const marker = userSearchFailure(new HttpError(412));
    expect(marker).toEqual({ state: 'unsupported', operation: USER_SEARCH_OPERATION, status: 412, message: 'HTTP 412' });
  });
  it('maps a missing route (404 as CapabilityUnavailableError) the same way', () => {
    const marker = userSearchFailure(new CapabilityUnavailableError('Capability unavailable', 404));
    expect(marker.state).toBe('unsupported');
    expect(marker.status).toBe(404);
    expect(marker.operation).toBe(USER_SEARCH_OPERATION);
  });
  it('rethrows every other failure so genuine errors never masquerade as unavailable', () => {
    expect(() => userSearchFailure(new HttpError(500))).toThrow(HttpError);
    expect(() => userSearchFailure(new TypeError('Failed to fetch'))).toThrow(TypeError);
  });
});

describe('checkedSearchMetadata', () => {
  const token = '0x0000000000000000000000000000000000000001';
  it('accepts a null entry and a well-formed entry keyed by the requested token', () => {
    const pool = '0x0000000000000000000000000000000000000002';
    const result = checkedSearchMetadata({ [token]: { token_address: token, pool_address: pool, name: 'Wired', symbol: 'WIRED' } }, [token]);
    expect(result[token]).toEqual({ token_address: token, pool_address: pool, name: 'Wired', symbol: 'WIRED' });
    expect(checkedSearchMetadata({ [token]: null }, [token])[token]).toBeNull();
  });
  it('rejects identities that were not requested or do not match', () => {
    const other = '0x0000000000000000000000000000000000000003';
    expect(() => checkedSearchMetadata({ [other]: null }, [token])).toThrow(TypeError);
    expect(() => checkedSearchMetadata({ [token]: { token_address: other } }, [token])).toThrow(TypeError);
  });
});
