/**
 * An address no coin lives at: the BFF answers 404 and the live market topics for that pool
 * cannot exist (production answers `/v2/snapshots` with 503 DEPENDENCY_UNAVAILABLE). The coin page
 * must render "Coin not found" and issue no market subscription or poll for that pool, before or
 * after the 404. Runs the real TokenMarketLayout against a real local HTTP origin.
 */
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createRequire } from 'node:module';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const unknownPool = '0x00000000000000000000000000000000000000a1';
const notServed = { code: 'DEPENDENCY_UNAVAILABLE', message: 'no backing service in development', retryable: false, requestId: 'req-dev' };

interface Seen { method: string; url: string; at: number }
const seen: Seen[] = [];

const server = createServer((request, response) => {
  const url = request.url ?? '';
  const method = request.method ?? 'GET';
  const reply = (status: number, payload?: unknown) => {
    response.setHeader('Access-Control-Allow-Origin', '*');
    response.statusCode = status;
    if (payload === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(payload));
  };
  if (method === 'OPTIONS') {
    response.setHeader('Access-Control-Allow-Origin', '*');
    response.setHeader('Access-Control-Allow-Methods', 'GET,POST');
    response.setHeader('Access-Control-Allow-Headers', '*');
    response.statusCode = 204; response.end(); return;
  }
  seen.push({ method, url, at: Date.now() });
  if (url === `/v2/pools/${unknownPool}`) {
    // core/api's recorded IF_POOL_DETAIL 404 NOT_FOUND body; slow, so anything the page starts before resolution settles has time to reach the origin.
    setTimeout(() => reply(404, v2Reads.responses.poolDetailNotFound.body), 250);
    return;
  }
  if (url.startsWith('/v2/search')) return reply(200, { requestId: '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', generatedAt: new Date().toISOString(), nextCursor: null, quality: 'complete', revision: '1', watermark: null, data: { items: [] } });
  if (url.startsWith('/v2/snapshots')) return reply(503, notServed);
  return reply(503, notServed);
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;
process.env.NEXT_PUBLIC_STREAM_ORIGIN = origin.replace(/^http/, 'ws');

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { SdkClient } = await import('@kindle/sdk');
const { MarketClientProvider } = await import('@/page-modules/token/market-session-context');
const { default: TokenMarketLayout } = await import('./token-market-layout');

async function waitFor(check: () => boolean, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}
const pause = (ms: number) => act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); });

let root: Root | undefined;
afterAll(() => { if (root) act(() => root!.unmount()); server.close(); });
beforeAll(() => { seen.length = 0; });

describe('coin page for an unknown address against a real origin', () => {
  it('renders "Coin not found" and never polls /v2/snapshots or anything else once the 404 verdict is in', async () => {
    const stream = origin.replace(/^http/, 'ws');
    const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    act(() => { root!.render(<QueryClientProvider client={queryClient}><MarketClientProvider client={client}><TokenMarketLayout poolAddress={unknownPool} /></MarketClientProvider></QueryClientProvider>); });

    await waitFor(() => container.querySelector('[data-coin-state="not-found"]') !== null);
    // Resolution is the IF_POOL_DETAIL 404 followed by the two /v2/search lookups; once the verdict renders,
    // every later request is traffic the not-found page caused.
    const verdictAt = Date.now();
    expect(container.querySelector('h2')?.textContent).toBe('Coin not found');
    expect(seen.filter(row => row.url === `/v2/pools/${unknownPool}`)).toHaveLength(1);
    expect(seen.some(row => row.url.startsWith('/bff/'))).toBe(false);

    // The production page fired about a dozen snapshot polls in ten seconds; give retries and
    // backoff timers well past their first window.
    await pause(2500);
    expect(seen.filter(row => row.url.startsWith('/v2/snapshots'))).toEqual([]);
    expect(seen.filter(row => row.at >= verdictAt)).toEqual([]);
    expect(container.querySelector('[data-coin-state="not-found"]')).not.toBeNull();
    expect(container.querySelector('[role="alert"]')).toBeNull();
  }, 15_000);
});
