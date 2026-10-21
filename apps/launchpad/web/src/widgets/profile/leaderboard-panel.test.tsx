import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';

// The jsdom test environment swaps the global AbortController/AbortSignal for jsdom's own classes
// while fetch stays Node's undici, which rejects a foreign signal, so every SDK request would fail
// before it left the process. Run in the node environment instead and install only the browser
// globals Node lacks from a real jsdom window, keeping one coherent fetch/AbortController/URL stack
// exactly as a browser has.
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

/** A real HTTP origin standing in for core/api (and media/user) so the SDK's own 404/503 handling runs end to end. */
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

// Identity lookups on media/user must not leave the machine during a unit test: point the user host at
// an origin that answers like production for wallets with no profile row. `GET /v2/profiles:batch`
// answers 200 and omits them (profiles_http.go profilesBatchGet); anything else is 404 NOT_FOUND.
const profiles = await serve(url => url.startsWith('/v2/profiles:batch?')
  ? { status: 200, body: { data: { items: [] }, generatedAt: new Date().toISOString(), nextCursor: null, quality: 'complete', requestId: '0b6f2c1e-4d3a-4b5c-8d7e-9f0a1b2c3d4e', revision: null, watermark: null } }
  : { status: 404, body: { code: 'NOT_FOUND', message: 'NOT_FOUND', requestId: '0b6f2c1e-4d3a-4b5c-8d7e-9f0a1b2c3d4f', retryable: false, retryAfterMs: null, fieldErrors: [], details: null } });
process.env.NEXT_PUBLIC_GATEWAY_URL = profiles.origin;
const { MarketClientContext } = await import('@/core/market/client');
const panelModule = await import('./leaderboard-panel');
const { LeaderboardPanel, leaderboardValue } = panelModule;

type Watermark = V2ContractTypes['Watermark'];
type LeaderboardsResponse = V2ContractTypes['LeaderboardsResponse'];
type ApiError = V2ContractTypes['Error'];

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const walletA = '0x' + 'a1'.repeat(20);
const walletB = '0x' + 'b2'.repeat(20);
const walletC = '0x' + 'c3'.repeat(20);
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };

function board(category: 'trader' | 'volume', window: '24h' | '7d' | '30d'): LeaderboardsResponse {
  return {
    requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'complete', revision: '7', watermark,
    data: { snapshot: {
      snapshotId: '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b', category, window, methodologyVersion: 'pnl-v1', watermark,
      entries: [
        { rank: 1, wallet: walletA, metricRaw: '189384120000', quality: 'complete' },
        { rank: 2, wallet: walletB, metricRaw: '153149680000', quality: 'partial' },
        { rank: 3, wallet: walletC, metricRaw: '0', quality: 'unavailable' },
      ],
    } },
  };
}

function parseBoardPath(url: string): { category: 'trader' | 'volume'; window: '24h' | '7d' | '30d' } | null {
  const match = /^\/v2\/leaderboards\/(trader|volume)\?(.*)$/.exec(url);
  if (!match) return null;
  const window = new URLSearchParams(match[2]).get('window');
  if (window !== '24h' && window !== '7d' && window !== '30d') return null;
  return { category: match[1] as 'trader' | 'volume', window };
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
      <QueryClientProvider client={queryClient}>
        <MarketClientContext.Provider value={clientFor(origin)}>{node}</MarketClientContext.Provider>
      </QueryClientProvider>,
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

function buttonNamed(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll('button')).find(button => button.textContent?.trim() === text);
}

describe('LeaderboardPanel against a real HTTP origin', () => {
  let unserved: Served;
  let degraded: Served;
  let served: Served;

  beforeAll(async () => {
    unserved = await serve(() => ({ status: 404 }));
    const envelope: ApiError = { code: 'DEPENDENCY_UNAVAILABLE', message: 'discovery runtime not configured', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null };
    degraded = await serve(() => ({ status: 503, body: envelope }));
    served = await serve(url => {
      const parsed = parseBoardPath(url);
      return parsed ? { status: 200, body: board(parsed.category, parsed.window) } : { status: 404 };
    });
  });
  afterAll(async () => {
    for (const { server } of [unserved, degraded, served, profiles]) await new Promise<void>(resolve => server.close(() => resolve()));
    dom.window.close();
  });

  it('exports the panel as default and named', () => {
    expect(panelModule.default).toBe(LeaderboardPanel);
  });

  it('renders the real 404 as an unavailable leaderboard with a retry that issues a new request', async () => {
    const { root, container, queryClient } = mount(unserved.origin, <LeaderboardPanel />);
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    await waitFor(() => container.textContent?.includes('Leaderboard is not available yet') === true);
    expect(container.querySelector('[data-operation]')).toBeNull();
    expect(container.textContent).not.toMatch(/IF_LEADERBOARDS|\/v2\/leaderboards|status 404/);
    expect(container.querySelector('ol')).toBeNull();
    expect(unserved.paths).toEqual(['/v2/leaderboards/trader?window=7d&limit=25']);

    const retry = buttonNamed(container, 'Try again');
    expect(retry).toBeDefined();
    act(() => retry?.click());
    await waitFor(() => unserved.paths.length === 2);
    expect(unserved.paths[1]).toBe('/v2/leaderboards/trader?window=7d&limit=25');
    await waitFor(() => container.textContent?.includes('Leaderboard is not available yet') === true);
    unmount(root, container, queryClient);
  });

  it('renders a 503 envelope as unavailable in plain words, never sample rows or the envelope code', async () => {
    const { root, container, queryClient } = mount(degraded.origin, <LeaderboardPanel />);
    await waitFor(() => container.textContent?.includes('Leaderboard is not available yet') === true);
    expect(container.querySelector('[data-operation]')).toBeNull();
    expect(container.textContent).not.toMatch(/DEPENDENCY_UNAVAILABLE|IF_LEADERBOARDS/);
    expect(container.querySelector('ol')).toBeNull();
    expect(container.innerHTML).not.toMatch(BORDER);
    unmount(root, container, queryClient);
  });

  it('renders a served empty snapshot as the empty state, never an error', async () => {
    const empty = await serve(url => {
      const parsed = parseBoardPath(url);
      if (!parsed) return { status: 404 };
      const response = board(parsed.category, parsed.window);
      return { status: 200, body: { ...response, data: { snapshot: { ...response.data.snapshot, entries: [] } } } };
    });
    try {
      const { root, container, queryClient } = mount(empty.origin, <LeaderboardPanel />);
      await waitFor(() => container.textContent?.includes('No traders ranked yet') === true);
      expect(container.textContent).toContain('Rankings show up once people start trading');
      expect(container.querySelector('[role="alert"]')).toBeNull();
      expect(container.textContent).not.toMatch(/did not load|could not load|failed|IF_LEADERBOARDS/);
      unmount(root, container, queryClient);
    } finally {
      await new Promise<void>(resolve => empty.server.close(() => resolve()));
    }
  });

  it('ranks served rows, formats the metric as USDL, links each row to the public profile and refetches on window and metric changes', async () => {
    const { root, container, queryClient } = mount(served.origin, <LeaderboardPanel />);
    await waitFor(() => container.querySelectorAll('ol li').length === 3);
    const links = Array.from(container.querySelectorAll<HTMLAnchorElement>('ol li a'));
    expect(links.map(link => link.getAttribute('href'))).toEqual([`/profile/${walletA}`, `/profile/${walletB}`, `/profile/${walletC}`]);
    expect(links[0].textContent).toContain('+189,384.12 USDL');
    expect(links[1].textContent).toContain('+153,149.68 USDL');
    expect(links[1].textContent).toContain('partial');
    expect(links[2].textContent).toContain('No value yet');
    expect(links[2].textContent).not.toContain('USDL');
    expect(container.textContent).toContain('Rank 1');
    expect(container.textContent).toContain('Updated');
    expect(container.innerHTML).not.toMatch(BORDER);
    expect(served.paths.filter(path => path.startsWith('/v2/leaderboards'))).toEqual(['/v2/leaderboards/trader?window=7d&limit=25']);

    // Wallets without a published profile fall back to their short address once media/user answers 404.
    await waitFor(() => container.textContent?.includes('0xa1a1…a1a1') === true);
    expect(container.textContent).toContain('0xb2b2…b2b2');
    // The labels above are the fallback shown while a lookup is pending too, so wait for the lookup itself:
    // one batch request names every row's wallet, and no per-wallet read (which would 404) is made.
    await waitFor(() => profiles.paths.some(path => path.startsWith('/v2/profiles:batch?') && path.includes(walletA) && path.includes(walletB)));
    expect(profiles.paths.some(path => path.startsWith('/v3/profiles/') || path.startsWith('/v1/profiles/'))).toBe(false);

    const windowTrigger = container.querySelector<HTMLButtonElement>('button[aria-label="Window"]');
    expect(windowTrigger?.textContent).toContain('7d');
    act(() => windowTrigger?.click());
    const option24h = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(option => option.textContent?.trim() === '24h');
    expect(option24h).toBeDefined();
    act(() => option24h?.click());
    await waitFor(() => served.paths.includes('/v2/leaderboards/trader?window=24h&limit=25'));
    await waitFor(() => container.querySelectorAll('ol li').length === 3);
    expect(container.querySelector<HTMLButtonElement>('button[aria-label="Window"]')?.textContent).toContain('24h');

    const metricTrigger = container.querySelector<HTMLButtonElement>('button[aria-label="Metric"]');
    act(() => metricTrigger?.click());
    const volume = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(option => option.textContent?.trim() === 'Volume');
    act(() => volume?.click());
    await waitFor(() => served.paths.includes('/v2/leaderboards/volume?window=24h&limit=25'));
    await waitFor(() => container.querySelectorAll('ol li').length === 3);
    expect(container.querySelector('ol li a')?.textContent).toContain('189,384.12 USDL');
    expect(container.querySelector('ol li a')?.textContent).not.toContain('+189,384.12');
    unmount(root, container, queryClient);
  });

  it('explains the missing rewards category on the Top rewards tab without issuing a request', async () => {
    const before = served.paths.length;
    const { root, container, queryClient } = mount(served.origin, <LeaderboardPanel />);
    const rewardsTab = buttonNamed(container, 'Top rewards');
    expect(rewardsTab?.getAttribute('role')).toBe('tab');
    act(() => rewardsTab?.click());
    expect(container.textContent).toContain('Top rewards are not available yet');
    expect(container.querySelector('[data-operation]')).toBeNull();
    expect(container.textContent).not.toContain('no response');
    expect(container.querySelector('[role="tabpanel"]')?.getAttribute('aria-labelledby')).toBe(rewardsTab?.id);
    expect(served.paths.slice(before).some(path => path.includes('rewards'))).toBe(false);
    unmount(root, container, queryClient);
  });

  it('formats metric values exactly from the raw USDL integer', () => {
    expect(leaderboardValue({ rank: 4, wallet: walletA, metricRaw: '5', quality: 'complete' }, 'profit')).toBe('< 0.01 USDL');
    expect(leaderboardValue({ rank: 4, wallet: walletA, metricRaw: '2500000', quality: 'complete' }, 'volume')).toBe('2.50 USDL');
    expect(leaderboardValue({ rank: 4, wallet: walletA, metricRaw: '2500000', quality: 'unavailable' }, 'volume')).toBeNull();
  });
});
