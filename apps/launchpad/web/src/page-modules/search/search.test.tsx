import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { createRequire } from 'node:module';
import { afterAll, afterEach, describe, expect, it } from 'vitest';
import { SdkClient } from '@kindle/sdk';

// jsdom swaps AbortController while fetch stays undici, so run in node and install only the missing browser globals (as shell/ticker-strip.test.tsx does).
interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000/search?q=pax', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const pool = '0x' + 'a1'.repeat(20);
const token = '0x' + 'b2'.repeat(20);
const searchResponse = (items: unknown[]) => ({ requestId, generatedAt: '2026-10-13T05:09:44.463Z', nextCursor: null, quality: 'complete', revision: '1', watermark: null, data: { items } });

/** Real HTTP origin for the search, metadata and stats reads the search page makes. */
function serve(): Promise<{ server: Server; origin: string; paths: string[] }> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://local');
    paths.push(`${request.method} ${url.pathname}${url.search}`);
    request.resume();
    request.on('end', () => {
      let status = 404; let body: unknown = { code: 'NOT_FOUND', message: 'NOT_FOUND', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null };
      if (url.pathname === '/v2/search') {
        const q = url.searchParams.get('q'); const type = url.searchParams.get('type');
        status = 200;
        body = searchResponse(q === 'pax' && type === 'pool' ? [{ id: `125:${pool}`, kind: 'pool', label: 'Pax Coin', poolAddress: pool, tokenAddress: token, wallet: null, score: '1' }] : []);
      } else if (url.pathname === '/metadata/batch') {
        status = 200;
        body = { [token]: { token_address: token, pool_address: pool, name: 'Pax Coin', symbol: 'PAXC' } };
      }
      response.statusCode = status;
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(body));
    });
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => resolve({ server, origin: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, paths }));
  });
}

const served = await serve();
process.env.NEXT_PUBLIC_DATA_API_URL = served.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = served.origin;
process.env.NEXT_PUBLIC_GATEWAY_URL = served.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { AppRouterContext } = await import('next/dist/shared/lib/app-router-context.shared-runtime');
const { adaptForAppRouterInstance } = await import('next/dist/shared/lib/router/adapters');
const { default: singletonRouter } = await import('next/router');
const { MarketClientContext } = await import('@/core/market/client');
const { routeForPath } = await import('@/core/app-foundation/navigation');
const { SearchView } = await import('./index');

const client = new SdkClient({ apiOrigin: served.origin, identityOrigin: served.origin, mediaOrigin: served.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
let cleanup: (() => void) | null = null;

function mount(node: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AppRouterContext.Provider value={adaptForAppRouterInstance(singletonRouter)}>
          <MarketClientContext.Provider value={client}>{node}</MarketClientContext.Provider>
        </AppRouterContext.Provider>
      </QueryClientProvider>,
    );
  });
  cleanup = () => { act(() => root.unmount()); queryClient.clear(); container.remove(); };
  return container;
}

async function waitFor(check: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

function type(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}

describe('/search page', () => {
  afterEach(() => { cleanup?.(); cleanup = null; served.paths.length = 0; });
  afterAll(async () => {
    await new Promise<void>(resolve => served.server.close(() => resolve()));
    dom.window.close();
  });

  it('is a shell route, so /search?q= no longer renders the not-found page', () => {
    expect(routeForPath('/search')?.id).toBe('search');
  });

  it('runs the q query from the URL against the real search route and lists the matching coin', async () => {
    const container = mount(<SearchView initialQuery="pax" />);
    const input = container.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    expect(input.value).toBe('pax');
    await waitFor(() => container.textContent?.includes('Pax Coin') === true);
    expect(container.textContent).toContain('$PAXC');
    expect(container.textContent).toMatch(/1 coin, 0 users/);
    expect(served.paths).toEqual(expect.arrayContaining(['GET /v2/search?q=pax&type=pool&limit=10', 'GET /v2/search?q=pax&type=token&limit=10']));
    expect(container.textContent).not.toMatch(/Page not found|NETWORK_INTERRUPTED|IF_|no response/);
  });

  it('searches again as the viewer types and keeps the URL query in step', async () => {
    const container = mount(<SearchView initialQuery="" />);
    expect(container.textContent).toContain('Type at least two characters');
    const input = container.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    type(input, 'zz');
    await waitFor(() => container.textContent?.includes('No coins or users match "zz"') === true);
    expect(new URL(window.location.href).searchParams.get('q')).toBe('zz');
    expect(served.paths.some(path => path.startsWith('GET /v2/search?q=zz&type=pool'))).toBe(true);
  });
});
