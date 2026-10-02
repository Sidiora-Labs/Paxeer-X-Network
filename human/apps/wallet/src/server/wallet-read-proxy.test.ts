import { describe, expect, it } from 'vitest';
import { NextRequest } from 'next/server';
import { GET as owned } from '../app/api/pns/api/v1/addresses:lookup/route';
import { GET as lookup } from '../app/api/pns/api/v1/domains:lookup/route';
import { GET as domain } from '../app/api/pns/api/v1/domains/[name]/route';
import { GET as events } from '../app/api/pns/api/v1/domains/[name]/events/route';
import { GET as addressName } from '../app/api/pns/api/v1/addresses/[address]/route';
import { GET as points } from '../app/api/points/balance/[address]/route';
import { GET as rates } from '../app/api/fx/latest/USD/route';
import { HttpBoundaryError, timeoutSignal } from './http';
import {
  proxyWalletRead,
  walletReadRequest,
  walletReadResponse,
  type WalletReadRoute,
} from './wallet-read-proxy';

const ORIGIN = 'https://api-mainnet-beta.paxeer.network';
const ADDRESS = '0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de';
const PNS = 'https://paxeer-name-service-production.up.railway.app';

interface RouteCase {
  route: WalletReadRoute;
  path: string;
  parameters: Record<string, string>;
  upstream: string;
}

const CASES: RouteCase[] = [
  {
    route: 'pns-owned',
    path: `/wallet/api/pns/api/v1/addresses:lookup?address=${ADDRESS}&owned_by=true&only_active=true&sort=registration_date&order=DESC`,
    parameters: {},
    upstream: `${PNS}/api/v1/addresses:lookup?address=${ADDRESS}&owned_by=true&only_active=true&sort=registration_date&order=DESC`,
  },
  {
    route: 'pns-domain',
    path: '/wallet/api/pns/api/v1/domains/example.pax',
    parameters: { name: 'example.pax' },
    upstream: `${PNS}/api/v1/domains/example.pax`,
  },
  {
    route: 'pns-events',
    path: '/wallet/api/pns/api/v1/domains/example.pax/events?order=DESC',
    parameters: { name: 'example.pax' },
    upstream: `${PNS}/api/v1/domains/example.pax/events?order=DESC`,
  },
  {
    route: 'pns-address',
    path: `/wallet/api/pns/api/v1/addresses/${ADDRESS}`,
    parameters: { address: ADDRESS },
    upstream: `${PNS}/api/v1/addresses/${ADDRESS}`,
  },
  {
    route: 'pns-lookup',
    path: '/wallet/api/pns/api/v1/domains:lookup?name=example.pax&only_active=true',
    parameters: {},
    upstream: `${PNS}/api/v1/domains:lookup?name=example.pax&only_active=true`,
  },
  {
    route: 'points-balance',
    path: `/wallet/api/points/balance/${ADDRESS}`,
    parameters: { address: ADDRESS },
    upstream: `https://sidiora-points-indexer-production.up.railway.app/points/balance/${ADDRESS}`,
  },
  {
    route: 'fx-usd',
    path: '/wallet/api/fx/latest/USD',
    parameters: {},
    upstream: 'https://open.er-api.com/v6/latest/USD',
  },
];

describe('finite wallet provider requests', () => {
  it.each(CASES)('binds $route to its actual provider without forwarding credentials', (entry) => {
    const incoming = new NextRequest(`${ORIGIN}${entry.path}`, {
      headers: {
        Authorization: 'Bearer must-not-be-forwarded',
        Cookie: 'session=must-not-be-forwarded',
        Origin: ORIGIN,
        'X-Agent-Signature': 'must-not-be-forwarded',
        'X-Forwarded-Host': 'untrusted.invalid',
      },
    });
    const outgoing = walletReadRequest(incoming, entry.route, entry.parameters);
    expect(outgoing.url).toBe(entry.upstream);
    expect(outgoing.method).toBe('GET');
    expect([...outgoing.headers.entries()]).toEqual([['accept', 'application/json']]);
    expect(outgoing.credentials).toBe('omit');
    expect(outgoing.redirect).toBe('error');
    expect(outgoing.cache).toBe('no-store');
  });

  it.each(CASES)('rejects query-based upstream selection for $route', (entry) => {
    const url = new URL(entry.path, ORIGIN);
    url.searchParams.set('url', 'https://untrusted.invalid/private');
    expect(() => walletReadRequest(new NextRequest(url), entry.route, entry.parameters))
      .toThrow(HttpBoundaryError);
  });

  it('rejects duplicate allowlisted query keys and oversized query values', () => {
    for (const query of ['name=one.pax&name=two.pax', `name=${'a'.repeat(257)}`]) {
      expect(() => walletReadRequest(
        new NextRequest(`${ORIGIN}/wallet/api/pns/api/v1/domains:lookup?${query}`),
        'pns-lookup',
      )).toThrow(HttpBoundaryError);
    }
  });

  it('keeps query text as query data, including encoded delimiters', () => {
    const url = new URL('/wallet/api/pns/api/v1/domains:lookup', ORIGIN);
    const name = 'https://untrusted.invalid/path?order=ASC#fragment';
    url.searchParams.set('name', name);
    const outgoing = new URL(walletReadRequest(new NextRequest(url), 'pns-lookup').url);
    expect(outgoing.origin).toBe(PNS);
    expect(outgoing.pathname).toBe('/api/v1/domains:lookup');
    expect([...outgoing.searchParams]).toEqual([['name', name]]);
    expect(outgoing.hash).toBe('');
  });

  it.each(['..', '.', 'a/b', 'a\\b', '%2e%2e', '%252f', 'a?b', 'a#b', '\u0000', 'a'.repeat(97)])(
    'rejects traversal, encoded separators and oversized path segment %j',
    (name) => {
      expect(() => walletReadRequest(
        new NextRequest(`${ORIGIN}/wallet/api/pns/api/v1/domains/value`),
        'pns-domain',
        { name },
      )).toThrow(HttpBoundaryError);
    },
  );

  it('rejects missing and extra dynamic parameters and forged route identifiers', () => {
    const request = new NextRequest(`${ORIGIN}/wallet/api/fx/latest/USD`);
    expect(() => walletReadRequest(request, 'pns-domain')).toThrow(HttpBoundaryError);
    expect(() => walletReadRequest(request, 'pns-domain', { name: 'example.pax', url: PNS }))
      .toThrow(HttpBoundaryError);
    expect(() => walletReadRequest(request, 'fx-usd', { name: 'example.pax' }))
      .toThrow(HttpBoundaryError);
    expect(() => Reflect.apply(walletReadRequest, undefined, [request, 'constructor']))
      .toThrow(HttpBoundaryError);
  });

  it('propagates parent cancellation into the real outbound request', () => {
    const controller = new AbortController();
    const incoming = new NextRequest(`${ORIGIN}/wallet/api/fx/latest/USD`, {
      signal: controller.signal,
    });
    const timed = timeoutSignal(incoming.signal, 10_000);
    try {
      const outgoing = walletReadRequest(incoming, 'fx-usd', {}, timed.signal);
      expect(outgoing.signal.aborted).toBe(false);
      controller.abort();
      expect(outgoing.signal.aborted).toBe(true);
    } finally {
      timed.dispose();
    }
  });
});

describe('wallet provider response boundary', () => {
  it.each([200, 404, 429, 503])('preserves real response bytes and HTTP status %i', async (status) => {
    const body = status === 200 ? '{"found":false,"balance":null}' : '{"error":"provider_refused"}';
    const response = await walletReadResponse(new Response(body, {
      status,
      headers: {
        'Content-Type': 'application/json; charset=utf-8',
        'Set-Cookie': 'provider-cookie=not-forwarded',
      },
    }));
    expect(response.status).toBe(status);
    expect(await response.text()).toBe(body);
    expect(response.headers.get('cache-control')).toBe('no-store');
    expect(response.headers.has('set-cookie')).toBe(false);
  });

  it('refuses non-JSON content and actual oversized bodies without content-length', async () => {
    await expect(walletReadResponse(new Response('<html>unavailable</html>', {
      headers: { 'Content-Type': 'text/html' },
    }))).rejects.toMatchObject({ status: 502, code: 'UPSTREAM_TYPE_INVALID' });
    await expect(walletReadResponse(new Response('x'.repeat(1_048_577), {
      headers: { 'Content-Type': 'application/json' },
    }))).rejects.toMatchObject({ status: 502, code: 'UPSTREAM_TOO_LARGE' });
  });
});

describe('real Next wallet read handlers', () => {
  const handlers: Array<{ name: string; call: (request: NextRequest) => Promise<Response> }> = [
    { name: 'owned', call: owned },
    { name: 'lookup', call: lookup },
    { name: 'domain', call: (request) => domain(request, { params: Promise.resolve({ name: 'example.pax' }) }) },
    { name: 'events', call: (request) => events(request, { params: Promise.resolve({ name: 'example.pax' }) }) },
    { name: 'address', call: (request) => addressName(request, { params: Promise.resolve({ address: ADDRESS }) }) },
    { name: 'points', call: (request) => points(request, { params: Promise.resolve({ address: ADDRESS }) }) },
    { name: 'rates', call: rates },
  ];

  it.each(handlers)('$name refuses an unknown query before contacting a provider', async ({ call }) => {
    const response = await call(new NextRequest(`${ORIGIN}/wallet/api/read?url=https://untrusted.invalid`));
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ error: { code: 'QUERY_INVALID' } });
  });

  it.each(handlers)('$name refuses mutation methods without contacting a provider', async ({ call }) => {
    const response = await call(new NextRequest(`${ORIGIN}/wallet/api/read`, { method: 'POST' }));
    expect(response.status).toBe(405);
    expect(await response.json()).toMatchObject({ error: { code: 'METHOD_NOT_ALLOWED' } });
  });

  it('returns a bounded path refusal from the production proxy', async () => {
    const response = await proxyWalletRead(
      new NextRequest(`${ORIGIN}/wallet/api/pns/api/v1/domains/value`),
      'pns-domain',
      { name: '../admin' },
    );
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ error: { code: 'PATH_INVALID' } });
  });
});
