import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { SdkClient } from '@kindle/sdk';
import { sameRead, v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

// jsdom swaps AbortController while fetch stays undici, so run in node and install only the missing browser globals (see widgets/profile/leaderboard-panel.test.tsx).
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

type Responder = (method: string, url: string, body: string) => { status: number; body?: unknown; headers?: Record<string, string> };
type Served = { server: Server; origin: string; paths: string[] };

function serve(responder: Responder): Promise<Served> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(`${request.method} ${request.url ?? ''}`);
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(chunk as Buffer));
    request.on('end', () => {
      const answer = responder(request.method ?? 'GET', request.url ?? '', Buffer.concat(chunks).toString('utf8'));
      response.statusCode = answer.status;
      for (const [name, value] of Object.entries(answer.headers ?? {})) response.setHeader(name, value);
      if (answer.body === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(answer.body));
    });
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as AddressInfo;
      resolve({ server, origin: `http://127.0.0.1:${port}`, paths });
    });
  });
}

/**
 * The api origin replays recorded core/api v2 bytes (core-api-wire.json
 * `v2Reads`): IF_RANKINGS trending/new, IF_STATS_BATCH, IF_POOL_LIST and
 * IF_POOL_DETAIL for the real chain-125 pool and the frozen example pool.
 * Token metadata (media/metadata batch) answers typed rows.
 */
const { real: poolA, realToken: tokenA, example: poolB } = v2Reads.pools;
const tokenB = '0x' + '3'.repeat(40);
const metadata: Record<string, { token_address: string; name: string; symbol: string }> = {
  [tokenA]: { token_address: tokenA, name: 'The Lil Black Bull', symbol: 'LILANSEM' },
  [tokenB]: { token_address: tokenB, name: 'Chaotic Rocket', symbol: 'CHAROC' },
};
const recordedReads = [v2Reads.responses.rankingsTrending, v2Reads.responses.rankingsNew, v2Reads.responses.statsBatch, v2Reads.responses.poolListTrending, v2Reads.responses.poolListNew, v2Reads.responses.poolDetailReal, v2Reads.responses.poolDetailFirst];

const served = await serve((method, url, body) => {
  const read = recordedReads.find(entry => sameRead(entry, method, new URL(url, 'http://replay.invalid'), body));
  if (read) return { status: read.status, body: read.body, headers: read.headers };
  if (method === 'GET' && url.startsWith('/metadata/batch?addresses=')) {
    const tokens = decodeURIComponent(url.slice('/metadata/batch?addresses='.length)).split(',');
    return { status: 200, body: Object.fromEntries(tokens.map(t => [t, metadata[t] ?? null])) };
  }
  return { status: 404 };
});
const unserved = await serve(() => ({ status: 404 }));
process.env.NEXT_PUBLIC_DATA_API_URL = served.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = served.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { MarketClientContext } = await import('@/core/market/client');
const { default: TerminalLeftRail } = await import('./index');
const { clearRecentPools, recordRecentPool } = await import('../recent-pools');

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function clientFor(origin: string): SdkClient {
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
}

function mount(origin: string, node: React.ReactElement) {
  const queryClient: QueryClientType = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => {
    root.render(<QueryClientProvider client={queryClient}><MarketClientContext.Provider value={clientFor(origin)}>{node}</MarketClientContext.Provider></QueryClientProvider>);
  });
  return {
    container,
    unmount() { act(() => root.unmount()); queryClient.clear(); container.remove(); },
  };
}

async function waitFor(check: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

function section(container: HTMLElement, title: string): HTMLElement {
  const heading = Array.from(container.querySelectorAll('h2')).find(h => h.textContent === title);
  const found = heading?.closest('section');
  if (!found) throw new Error(`No section ${title}`);
  return found as HTMLElement;
}

function rowOf(scope: HTMLElement, address: string): HTMLElement {
  const row = scope.querySelector<HTMLElement>(`li[data-pool="${address}"]`);
  if (!row) throw new Error(`No row ${address}`);
  return row;
}

function radio(scope: HTMLElement, label: string): HTMLButtonElement {
  const button = Array.from(scope.querySelectorAll<HTMLButtonElement>('[role="radio"]')).find(b => b.textContent?.trim() === label);
  if (!button) throw new Error(`No segment ${label}`);
  return button;
}

describe('TerminalLeftRail against a real HTTP origin', () => {
  beforeAll(() => { window.localStorage.clear(); clearRecentPools(); });
  afterAll(async () => {
    for (const { server } of [served, unserved]) await new Promise<void>(resolve => server.close(() => resolve()));
    dom.window.close();
  });

  it('renders served trending and new pairs as terminal links from recorded v2 rankings, stats and pool listings', async () => {
    const { container, unmount } = mount(served.origin, <TerminalLeftRail activePool={poolB} />);
    const trending = section(container, 'Trending');
    await waitFor(() => trending.querySelectorAll('li[data-pool]').length === 2 && trending.textContent?.includes('LILANSEM') === true && trending.textContent?.includes('CHAROC') === true);

    const rowA = rowOf(trending, poolA);
    expect(rowA.querySelector('a')?.getAttribute('href')).toBe(`/terminal/${poolA}`);
    expect(rowA.textContent).toContain('The Lil Black Bull');
    expect(rowA.className).toContain('bg-surface-1');

    const rowB = rowOf(trending, poolB);
    expect(rowB.className).toContain('bg-surface-3');
    expect(rowB.querySelector('a')?.getAttribute('aria-current')).toBe('page');

    // The real pool's recorded 2.2.0 row: market cap is the total-supply valuation (10,098.226981 USDL), and the
    // 1h baseline is insufficient_history, so the cell says why instead of a number. The pre-2.2.0 example row
    // carries no window fields at all. No rail-wide "not available" notice remains for served fields.
    expect(rowA.textContent).toContain('$10.1K');
    expect(rowOf(trending, poolA).querySelector('[aria-label^="Change not available yet"]')?.getAttribute('aria-label')).toBe('Change not available yet: Not enough trades yet for a 1h change');
    expect(rowOf(trending, poolB).querySelector('[aria-label^="Change not available yet"]')?.getAttribute('aria-label')).toBe('Change not available yet: 1h change not available yet');
    expect(trending.textContent).not.toContain('Window changes are not available yet.');
    act(() => radio(trending, '5M').click());
    expect(rowOf(trending, poolA).querySelector('[aria-label^="Change not available yet"]')?.getAttribute('aria-label')).toBe('Change not available yet: Not enough trades yet for a 5m change');
    act(() => radio(trending, '30M').click());
    expect(rowOf(trending, poolA).querySelector('[aria-label^="Change not available yet"]')?.getAttribute('aria-label')).toContain('A 30-minute change is not available');

    const fresh = section(container, 'New pairs');
    await waitFor(() => fresh.querySelector(`li[data-pool="${poolB}"]`)?.textContent?.includes('CHAROC') === true);
    const rowC = rowOf(fresh, poolB);
    expect(rowC.querySelector('a')?.getAttribute('href')).toBe(`/terminal/${poolB}`);
    // Age from the IF_POOL_LIST registry createdAt (2025-10-09T08:53:20Z).
    expect(rowC.querySelector('time')).not.toBeNull();

    expect(served.paths).toEqual(expect.arrayContaining(['POST /v2/pools/stats:batch', 'GET /v2/pools?sort=trending&limit=100']));
    expect(served.paths.some(path => path.startsWith('GET /stats/'))).toBe(false);
    expect(container.innerHTML).not.toMatch(BORDER);
    unmount();
  });

  it('lists recently viewed pools with spotPriceWad prices (reserve price for a pre-2.2.0 row), removes one and clears all', async () => {
    window.localStorage.clear(); clearRecentPools();
    const { container, unmount } = mount(served.origin, <TerminalLeftRail activePool={null} />);
    const recent = section(container, 'Recently viewed');
    expect(recent.textContent).toContain('Coins you open show up here.');

    act(() => { recordRecentPool(poolA); recordRecentPool(poolB); });
    await waitFor(() => recent.querySelector(`li[data-pool="${poolB}"]`)?.textContent?.includes('CHAROC') === true && recent.querySelector(`li[data-pool="${poolA}"]`)?.textContent?.includes('$0.0') === true);
    const rows = Array.from(recent.querySelectorAll<HTMLElement>('li[data-pool]'));
    expect(rows.map(r => r.dataset.pool)).toEqual([poolB, poolA]);
    // The example pool's frozen pre-2.2.0 stats row has no spotPriceWad: its IF_POOL_DETAIL reserve price,
    // 1250 USDL effective reserve over 8e8 whole tokens = 0.0000015625 USDL.
    expect(rows[0].textContent).toContain('$0.0₅1562');
    // The real pool's 2.2.0 row: spotPriceWad 10098226981692 = 0.000010098226981692 USDL per whole token.
    expect(rows[1].textContent).toContain('$0.0₄1009');
    expect(rows[1].textContent).toContain('$10.1K');
    expect(served.paths).toEqual(expect.arrayContaining([`GET /v2/pools/${poolA}`, `GET /v2/pools/${poolB}`]));

    const remove = rows[0].querySelector<HTMLButtonElement>('button[aria-label="Remove CHAROC from recently viewed"]');
    act(() => remove?.click());
    expect(Array.from(recent.querySelectorAll<HTMLElement>('li[data-pool]')).map(r => r.dataset.pool)).toEqual([poolA]);
    expect(JSON.parse(window.localStorage.getItem('sidiora-terminal-recent') ?? '[]')).toEqual([poolA]);

    act(() => recent.querySelector<HTMLButtonElement>('button[aria-label="Recently viewed actions"]')?.click());
    const clear = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(item => item.textContent?.includes('Clear all'));
    expect(clear).toBeDefined();
    act(() => clear?.click());
    expect(recent.querySelectorAll('li[data-pool]').length).toBe(0);
    expect(recent.textContent).toContain('Coins you open show up here.');
    expect(container.innerHTML).not.toMatch(BORDER);
    unmount();
  });

  it('renders the real 404 of IF_RANKINGS as an unavailable panel naming the operation', async () => {
    const { container, unmount } = mount(unserved.origin, <TerminalLeftRail activePool={null} />);
    await waitFor(() => container.textContent?.includes('Trending is not available yet') === true && container.textContent?.includes('New pairs is not available yet') === true);
    expect(container.textContent).not.toContain('IF_RANKINGS');
    expect(container.textContent).not.toContain('IF_RANKINGS');
    expect(container.querySelectorAll('li[data-pool]').length).toBe(0);
    expect(unserved.paths).toEqual(expect.arrayContaining(['GET /v2/rankings/trending', 'GET /v2/rankings/new']));
    unmount();
  });
});
