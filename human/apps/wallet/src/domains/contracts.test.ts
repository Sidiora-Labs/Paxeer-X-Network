import { describe, expect, it } from 'vitest';
import { parseNativeRouteEvent, storageEnvelopeParser } from './platform';
import { parseServerRequestMeta, upstreamEnvelopeParser } from './server-edge';
import { parseRouteQuery } from './shell';
import {
  parseAddress,
  parseBaseUnitAmount,
  parseBoundedString,
  parseHttpsUrl,
} from './shared';

describe('canonical domain boundary contracts', () => {
  it('parses addresses and decimal-safe base-unit amounts', () => {
    expect(parseAddress('0x1111111111111111111111111111111111111111').ok).toBe(
      true,
    );
    const amount = parseBaseUnitAmount('1000000000000000000');
    expect(amount.ok && amount.value).toBe(1_000_000_000_000_000_000n);
    expect(parseBaseUnitAmount('1.5').ok).toBe(false);
  });

  it('accepts only bounded credential-free HTTPS URLs', () => {
    expect(parseHttpsUrl('https://paxscan.io/tx/0x01').ok).toBe(true);
    expect(parseHttpsUrl('http://paxscan.io').ok).toBe(false);
    expect(parseHttpsUrl('https://user:secret@paxscan.io').ok).toBe(false);
    expect(
      parseHttpsUrl('https://unknown.example', {
        allowedOrigins: new Set(['https://paxscan.io']),
      }).ok,
    ).toBe(false);
  });

  it('parses versioned storage and rejects incompatible versions', () => {
    const parser = storageEnvelopeParser(2, (input) =>
      parseBoundedString(input, { maxLength: 32 }),
    );
    expect(
      parser({ version: 2, writtenAt: 1_700_000_000_000, value: 'USD' }).ok,
    ).toBe(true);
    expect(
      parser({ version: 1, writtenAt: 1_700_000_000_000, value: 'USD' }).ok,
    ).toBe(false);
  });

  it('parses native, query, server, and upstream boundaries', () => {
    expect(parseNativeRouteEvent({ kind: 'deep-link', route: '/send' }).ok).toBe(
      true,
    );
    expect(
      parseNativeRouteEvent({
        kind: 'deep-link',
        route: 'https://evil.example/send',
      }).ok,
    ).toBe(false);
    expect(
      parseRouteQuery({
        name: 'tx-detail',
        value: `0x${'1'.repeat(64)}`,
      }).ok,
    ).toBe(true);
    expect(parseRouteQuery({ name: 'tx-detail' }).ok).toBe(false);
    expect(
      parseServerRequestMeta({
        method: 'POST',
        contentType: 'application/json',
        contentLength: 512,
        correlationId: 'request-1234',
      }).ok,
    ).toBe(true);
    const upstream = upstreamEnvelopeParser((input) =>
      parseBoundedString(input, { maxLength: 32 }),
    );
    expect(
      upstream({
        status: 200,
        contentType: 'application/json',
        body: 'validated',
      }).ok,
    ).toBe(true);
  });
});
