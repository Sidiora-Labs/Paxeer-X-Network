// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { HttpError, Operations, SdkClient, StreamTransportError } from '@kindle/sdk';
import { resolvePoolSnapshot } from '@/core/market/pool-resolver';
import { isNotFoundError } from '@/core/user-facing-error';
import { CoinNotFound } from '@/widgets/trading/token-market-layout';
import { FreshnessNotice } from './freshness-notice';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

const mounted: Array<{ root: Root; container: HTMLElement }> = [];
function mount(node: React.ReactElement) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mounted.push({ root, container });
  return container;
}
afterEach(() => {
  for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); }
  vi.unstubAllEnvs();
});

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const LEAKS = /NETWORK_INTERRUPTED|transport |IF_[A-Z]|GET \/|\{p\}|no response/;
const unknownAddress = '0x0000000000000000000000000000000000000001';
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';

/** The resolver over a fetcher answering like production for an address no pool or token lives at: 404 from the pool detail, empty search. */
function resolverDeps(fetcher: typeof fetch) {
  const origin = 'http://127.0.0.1:9';
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'wss://stream.test', approvedStreamOrigins: ['wss://stream.test'], production: false }, fetcher);
  return { operations: new Operations(client) };
}
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
const nowhere: typeof fetch = async input => {
  const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url);
  if (url.pathname === '/v2/search') return json(200, { requestId, generatedAt: '2026-10-13T00:00:00.000Z', nextCursor: null, quality: 'complete', revision: '1', watermark: null, data: { items: [] } });
  return json(404, { code: 'POOL_NOT_FOUND', message: 'POOL_NOT_FOUND', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null });
};

describe('coin page for an address with no coin', () => {
  it('classifies the resolver answer for an unknown address as not found, and a transport failure as not', async () => {
    const missing = await resolvePoolSnapshot(unknownAddress, resolverDeps(nowhere)).catch((error: unknown) => error);
    expect(isNotFoundError(missing)).toBe(true);
    const offline: typeof fetch = async () => { throw new TypeError('Failed to fetch'); };
    const outage = await resolvePoolSnapshot(unknownAddress, resolverDeps(offline)).catch((error: unknown) => error);
    expect(isNotFoundError(outage)).toBe(false);
    expect(isNotFoundError(new StreamTransportError('NETWORK_INTERRUPTED'))).toBe(false);
    expect(isNotFoundError(new HttpError(503))).toBe(false);
  });

  it('renders a neutral "Coin not found" state with a Browse coins link and no error tone', () => {
    const container = mount(<CoinNotFound poolAddress={unknownAddress} />);
    expect(container.querySelector('h2')?.textContent).toBe('Coin not found');
    const browse = Array.from(container.querySelectorAll('a')).find(link => link.textContent === 'Browse coins');
    expect(browse?.getAttribute('href')).toBe('/explore');
    expect(container.querySelector('[role="alert"]')).toBeNull();
    expect(container.innerHTML).not.toMatch(/text-danger|bg-danger|Reconnect|unavailable/);
    expect(container.innerHTML).not.toMatch(BORDER);
    expect(container.textContent).not.toMatch(LEAKS);
  });
});

describe('FreshnessNotice', () => {
  it('keeps the transport state and code out of the visible text', () => {
    const container = mount(<FreshnessNotice freshness="unavailable" stream="stale" error="NETWORK_INTERRUPTED" onReconnect={() => {}} />);
    expect(container.textContent).toContain('Live updates are off');
    expect(container.textContent).not.toMatch(LEAKS);
    expect(container.querySelector('[data-transport]')).toBeNull();
  });

  it('omits the developer detail in production builds', () => {
    vi.stubEnv('NODE_ENV', 'production');
    const container = mount(<FreshnessNotice freshness="stale" stream="stale" error="NETWORK_INTERRUPTED" onReconnect={() => {}} />);
    expect(container.textContent).toContain('Prices may be out of date');
    expect(container.innerHTML).not.toMatch(/NETWORK_INTERRUPTED|data-transport/);
  });
});
