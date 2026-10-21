import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';

// Same harness as widgets/profile/leaderboard-panel.test.tsx: node environment with the browser
// globals installed from a real jsdom window so Node's fetch and AbortController stay coherent.
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

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');

type Responder = (url: string) => { status: number; body?: unknown };
type Served = { server: Server; origin: string; paths: string[] };

function serve(responder: Responder): Promise<Served> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(request.url ?? '');
    const answer = responder(request.url ?? '');
    response.statusCode = answer.status;
    if (answer.body === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(answer.body));
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as AddressInfo;
      resolve({ server, origin: `http://127.0.0.1:${port}`, paths });
    });
  });
}

const profiles = await serve(() => ({ status: 404 }));
process.env.NEXT_PUBLIC_USER_API_URL = profiles.origin;
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { defineChain } = await import('viem');
const { MarketClientContext } = await import('@/core/market/client');
const { TrendingWidget, trendingEmptyTitles, trendingEmptyBody } = await import('./trending-widget');
const { ScreenerWidget, screenerEmpty } = await import('./screener-widget');
const { LeaderboardWidget, leaderboardEmptyTitles, leaderboardEmptyBody } = await import('./leaderboard-widget');
const { CalloutsWidget, calloutsEmpty } = await import('./callouts-widget');

type Watermark = V2ContractTypes['Watermark'];
type RankingsResponse = V2ContractTypes['RankingsResponse'];
type LeaderboardsResponse = V2ContractTypes['LeaderboardsResponse'];
type FeedPage = V2ContractTypes['ConnectedDiscoveryFeedPage'];

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };
const ERROR_COPY = /did not load|could not load|couldn't load|failed|IF_[A-Z_]+|returned an empty/i;

/** The empty first page core/api answers before any pool exists (dev_market.go rankings handler). */
function emptyRanking(category: string): RankingsResponse {
  return {
    requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'partial', revision: '7', watermark,
    data: { snapshot: { snapshotId: '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b', category: category as RankingsResponse['data']['snapshot']['category'], modelVersion: 'dev-v1', computedAt: '2026-10-12T15:00:00.000Z', watermark, items: [], totalEligible: '0', truncated: false, replace: true } },
  };
}

function emptyBoard(category: 'trader' | 'creator' | 'volume', window: '24h' | '7d' | '30d'): LeaderboardsResponse {
  return {
    requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'complete', revision: '7', watermark,
    data: { snapshot: { snapshotId: '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b', category, window, methodologyVersion: 'pnl-v1', watermark, entries: [] } },
  };
}

const emptyFeed: FeedPage = { Algorithm: 'chronological', Cursor: '', Entries: [], FallbackReason: '', Personalized: false, Snapshot: 'snap-1' };

function emptyOrigin(url: string): { status: number; body?: unknown } {
  const ranking = /^\/v2\/rankings\/([a-z]+)$/.exec(url);
  if (ranking) return { status: 200, body: emptyRanking(ranking[1]) };
  const board = /^\/v2\/leaderboards\/(trader|creator|volume)\?(.*)$/.exec(url);
  if (board) return { status: 200, body: emptyBoard(board[1] as 'trader', new URLSearchParams(board[2]).get('window') as '24h') };
  if (url.startsWith('/v2/discovery/feed')) return { status: 200, body: emptyFeed };
  return { status: 404 };
}

function clientFor(origin: string): SdkClient {
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
}

function mount(origin: string, node: React.ReactElement): { root: Root; container: HTMLDivElement; queryClient: QueryClientType } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          <MarketClientContext.Provider value={clientFor(origin)}>{node}</MarketClientContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return { root, container, queryClient };
}

function unmount(root: Root, container: HTMLDivElement, queryClient: QueryClientType) {
  act(() => root.unmount());
  queryClient.clear();
  container.remove();
}

async function waitFor(check: () => boolean, timeoutMs = 4000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

function expectEmptyNotError(container: HTMLElement) {
  expect(container.textContent ?? '').not.toMatch(ERROR_COPY);
  expect(container.querySelector('[role="alert"]')).toBeNull();
  expect(container.textContent).not.toContain('unavailable right now');
}

const props = { columnId: 'home-1', filter: '', density: 'comfortable' as const, view: 'list' as const };

describe('home widgets on a successful empty answer', () => {
  let empty: Served;
  let failing: Served;

  beforeAll(async () => {
    empty = await serve(emptyOrigin);
    failing = await serve(() => ({ status: 500 }));
  });
  afterAll(async () => {
    for (const { server } of [empty, failing, profiles]) await new Promise<void>(resolve => server.close(() => resolve()));
    dom.window.close();
  });

  it('Trending renders the empty state with a Create coin action and no quality warning', async () => {
    const { root, container, queryClient } = mount(empty.origin, <TrendingWidget {...props} />);
    await waitFor(() => container.textContent?.includes(trendingEmptyTitles.trending) === true);
    expect(container.textContent).toContain(trendingEmptyBody);
    expect(container.querySelector('a[href="/create"]')?.textContent).toBe('Create coin');
    expect(container.textContent).not.toContain('snapshot quality');
    expectEmptyNotError(container);
    expect(empty.paths).toContain('/v2/rankings/trending');
    unmount(root, container, queryClient);
  });

  it('Screener New renders "No coins launched yet" with a Create coin action', async () => {
    const { root, container, queryClient } = mount(empty.origin, <ScreenerWidget {...props} />);
    await waitFor(() => container.textContent?.includes(screenerEmpty.new.title) === true);
    expect(screenerEmpty.new.title).toBe('No coins launched yet');
    expect(container.textContent).toContain(screenerEmpty.new.body);
    expect(container.querySelector('a[href="/create"]')?.textContent).toBe('Create coin');
    expectEmptyNotError(container);
    unmount(root, container, queryClient);
  });

  it('Leaderboard renders "No traders yet"', async () => {
    const { root, container, queryClient } = mount(empty.origin, <LeaderboardWidget {...props} />);
    await waitFor(() => container.textContent?.includes(leaderboardEmptyTitles.trader) === true);
    expect(leaderboardEmptyTitles.trader).toBe('No traders yet');
    expect(container.textContent).toContain(leaderboardEmptyBody('24h'));
    expectEmptyNotError(container);
    unmount(root, container, queryClient);
  });

  it('Callouts renders "No callouts yet"', async () => {
    const { root, container, queryClient } = mount(empty.origin, <CalloutsWidget {...props} />);
    await waitFor(() => container.textContent?.includes(calloutsEmpty.latest.title) === true);
    expect(container.textContent).toContain(calloutsEmpty.latest.body);
    expectEmptyNotError(container);
    unmount(root, container, queryClient);
  });

  it('keeps the error state with a retry for a real 5xx failure', async () => {
    const { root, container, queryClient } = mount(failing.origin, <TrendingWidget {...props} />);
    await waitFor(() => container.textContent?.includes("Couldn't load trending coins") === true, 8000);
    expect(Array.from(container.querySelectorAll('button')).some(button => /try again|retry/i.test(button.textContent ?? ''))).toBe(true);
    expect(container.textContent).not.toContain(trendingEmptyTitles.trending);
    // The failure is a compact chip inside the column frame: the tab row stays, no centred panel replaces the body.
    const chip = container.querySelector('[data-state="error"]');
    expect(chip).not.toBeNull();
    expect(chip?.textContent).toContain('Try again');
    expect(container.querySelector('[role="radiogroup"], [role="tablist"], [aria-label="Ranking"]')).not.toBeNull();
    expect(container.querySelector('h3')).toBeNull();
    expect(container.textContent).not.toMatch(/Failed to fetch|unavailable right now/);
    unmount(root, container, queryClient);
  });

  it('renders the leaderboard failure as a chip under its tab row', async () => {
    const { root, container, queryClient } = mount(failing.origin, <LeaderboardWidget {...props} />);
    await waitFor(() => container.querySelector('[data-state="error"], [data-state="unavailable"]') !== null, 8000);
    expect(container.querySelector('h3')).toBeNull();
    expect(container.textContent).toContain('Traders');
    expect(container.textContent).not.toMatch(/Failed to fetch|unavailable right now/);
    unmount(root, container, queryClient);
  });
});
