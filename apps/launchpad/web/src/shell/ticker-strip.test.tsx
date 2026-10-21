import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, afterEach, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

// jsdom swaps AbortController while fetch stays undici, so run in node and install only the missing browser globals (see widgets/terminal/left-rail/left-rail.render.test.tsx).
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

type Answer = { status: number; body?: unknown; headers?: Record<string, string> };
type ApiError = V2ContractTypes['Error'];

/** Real HTTP origin answering a scripted sequence per path; the last answer repeats. */
function serve(): Promise<{ server: Server; origin: string; paths: string[]; script: Map<string, Answer[]> }> {
  const paths: string[] = [];
  const script = new Map<string, Answer[]>();
  const server = createServer((request, response) => {
    const url = request.url ?? '';
    paths.push(`${request.method} ${url}`);
    request.resume();
    request.on('end', () => {
      const queue = script.get(url.split('?')[0]);
      const answer: Answer = queue && queue.length > 0 ? (queue.length > 1 ? queue.shift()! : queue[0]) : { status: 404 };
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
      resolve({ server, origin: `http://127.0.0.1:${port}`, paths, script });
    });
  });
}

const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const watermark = { chainId: '125', height: '33921208', blockHash: '0x' + '2'.repeat(64), projectionRevision: '33921208', observedAt: '2026-10-13T05:09:44.188Z', integrity: 'healthy' };
// Shape of the production 200 (`GET /v2/leaderboards/trader?window=24h&limit=20`) right after the redeploy.
const leaderboard = {
  data: { snapshot: { category: 'trader', entries: [], methodologyVersion: 'dev-indexer-swaps-v1', snapshotId: '2881b873-06ec-5544-a753-30a471366389', watermark, window: '24h' } },
  generatedAt: '2026-10-13T05:09:44.463Z', nextCursor: null, quality: 'complete', requestId, revision: '33921208', watermark,
};
const envelope = (code: string, retryable: boolean, retryAfterMs: number | null): ApiError => ({ code, message: code, requestId, retryable, retryAfterMs, fieldErrors: [], details: null });
// What core/api answers while a dependency is reconnecting (WriteV2APIError: 503 + Retry-After). Retry-After 0 keeps the SDK's own retries fast here.
const dependencyBlip: Answer = { status: 503, body: envelope('DEPENDENCY_UNAVAILABLE', true, 40), headers: { 'Retry-After': '0' } };
const ok: Answer = { status: 200, body: leaderboard };
// IF_PLATFORM envelope as core/api serves it today (volume, pools, active wallets), and with the upcoming optional fields.
const metrics = { data: { volumeUsdlRaw: '1250000000', pools: '12', activeWallets: '7' }, generatedAt: '2026-10-13T05:09:44.463Z', nextCursor: null, quality: 'complete', requestId, revision: '33921208', watermark };
const metricsUpcoming = { ...metrics, data: { ...metrics.data, totalMarketCapUsdlRaw: '9000000000', trades: '42', newPools: '3' } };
const PLATFORM = '/v2/platform/metrics';

const served = await serve();
process.env.NEXT_PUBLIC_DATA_API_URL = served.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = served.origin;
process.env.NEXT_PUBLIC_GATEWAY_URL = served.origin;
process.env.NEXT_PUBLIC_PNL_API_URL = served.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { MarketClientContext } = await import('@/core/market/client');
const { default: TickerStrip, marqueeCopies } = await import('./ticker-strip');
const { default: PlatformMetricsFooter } = await import('./platform-metrics');

const LEADERBOARD = '/v2/leaderboards/trader';
const ERROR_COPY = /Couldn't load|did not load|unavailable right now|not available yet/;

function clientFor(origin: string): SdkClient {
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
}

let cleanup: (() => void) | null = null;

/**
 * Mount with a client whose defaults never retry, so any retry observed comes
 * from the shared per-read policy, and record every text the strip ever shows.
 */
function mount(node: React.ReactElement) {
  const queryClient: QueryClientType = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const seen: string[] = [];
  const observer = new window.MutationObserver(() => { seen.push(container.textContent ?? ''); });
  observer.observe(container, { childList: true, subtree: true, characterData: true });
  const root: Root = createRoot(container);
  act(() => {
    root.render(<QueryClientProvider client={queryClient}><MarketClientContext.Provider value={clientFor(served.origin)}>{node}</MarketClientContext.Provider></QueryClientProvider>);
  });
  cleanup = () => { observer.disconnect(); act(() => root.unmount()); queryClient.clear(); container.remove(); };
  return { container, seen };
}

async function waitFor(check: () => boolean, timeoutMs = 4000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

const settle = (ms: number) => act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); });
const hits = (path: string) => served.paths.filter(entry => entry.split('?')[0] === `GET ${path}`).length;

describe('shell reads against a real HTTP origin that answers a transient failure first', () => {
  afterEach(() => { cleanup?.(); cleanup = null; served.paths.length = 0; served.script.clear(); window.localStorage.clear(); });
  afterAll(async () => {
    await new Promise<void>(resolve => served.server.close(() => resolve()));
    dom.window.close();
  });

  it('keeps the top earners strip loading through retryable 503s and lands on the ready state without ever showing an error', async () => {
    // Three 503s exhaust the SDK's in-transport retries, so the 200 is only reached by the shared React Query retry.
    served.script.set(LEADERBOARD, [dependencyBlip, dependencyBlip, dependencyBlip, ok]);
    const { container, seen } = mount(<TickerStrip />);
    expect(container.querySelector('[aria-busy="true"]')?.textContent).toContain('Loading top earners');
    await waitFor(() => container.textContent?.includes('No top earners in the last 24h') === true);
    expect(hits(LEADERBOARD)).toBe(4);
    expect(served.paths.find(entry => entry.startsWith(`GET ${LEADERBOARD}?`))).toBe(`GET ${LEADERBOARD}?window=24h&limit=20`);
    expect(seen.length).toBeGreaterThan(0);
    expect(seen.filter(text => ERROR_COPY.test(text))).toEqual([]);
    expect(container.querySelector('[aria-busy="true"]')).toBeNull();
  });

  it('keeps the "Open top earners" link whole in the empty state, with a short message for narrow screens', async () => {
    served.script.set(LEADERBOARD, [ok]);
    const { container } = mount(<TickerStrip />);
    await waitFor(() => container.querySelector('[data-ticker-empty]') !== null);
    const empty = container.querySelector<HTMLElement>('[data-ticker-empty]')!;
    const link = Array.from(empty.querySelectorAll('a')).find(anchor => anchor.textContent === 'Open top earners')!;
    expect(link.getAttribute('href')).toBeTruthy();
    const linkClasses = link.className.split(/\s+/);
    expect(linkClasses).toEqual(expect.arrayContaining(['shrink-0', 'whitespace-nowrap']));
    expect(linkClasses).not.toContain('truncate');
    // Only the message span may truncate; the link is never inside it.
    expect(empty.className.split(/\s+/)).not.toContain('truncate');
    const message = empty.querySelector('.truncate')!;
    expect(message.contains(link)).toBe(false);
    const narrow = message.querySelector('.sm\\:hidden');
    const wide = message.querySelector('.sm\\:inline');
    expect(narrow?.textContent).toBe('None in 24h');
    expect(wide?.textContent).toBe('No top earners in the last 24h');
    expect(wide?.className.split(/\s+/)).toContain('hidden');
  });

  it('waits out a 429 RATE_LIMITED with retryAfterMs and then renders the ready state', async () => {
    served.script.set(LEADERBOARD, [{ status: 429, body: envelope('RATE_LIMITED', true, 60), headers: { 'Retry-After': '1' } }, ok]);
    const started = Date.now();
    const { container, seen } = mount(<TickerStrip />);
    await waitFor(() => container.textContent?.includes('No top earners in the last 24h') === true);
    expect(Date.now() - started).toBeGreaterThanOrEqual(60);
    expect(hits(LEADERBOARD)).toBe(2);
    expect(seen.filter(text => ERROR_COPY.test(text))).toEqual([]);
  });

  it('renders a non-retryable 4xx as the quiet error status once, without retrying', async () => {
    served.script.set(LEADERBOARD, [{ status: 422, body: envelope('FIELD_INVALID', false, null) }]);
    const { container } = mount(<TickerStrip />);
    await waitFor(() => container.textContent?.includes("Couldn't load top earners.") === true);
    await settle(300);
    expect(hits(LEADERBOARD)).toBe(1);
    const status = container.querySelectorAll('[role="status"]');
    expect(status).toHaveLength(1);
    expect(container.querySelector('[role="alert"]')).toBeNull();
    expect(status[0].className).toContain('text-text-faint');
    expect(status[0].className).not.toMatch(/danger|warning/);
    const retry = Array.from(container.querySelectorAll('button')).find(button => button.textContent === 'Retry');
    expect(retry).toBeDefined();

    served.script.set(LEADERBOARD, [ok]);
    act(() => { retry!.click(); });
    await waitFor(() => container.textContent?.includes('No top earners in the last 24h') === true);
    expect(hits(LEADERBOARD)).toBe(2);
  });

  it('gives up after the bounded number of attempts and only then leaves the loading state', async () => {
    const quick: Answer = { status: 429, body: envelope('RATE_LIMITED', true, 5) };
    served.script.set(LEADERBOARD, [quick]);
    const { container, seen } = mount(<TickerStrip />);
    await waitFor(() => container.textContent?.includes("Couldn't load top earners.") === true);
    // 1 attempt + 4 transient retries; 429 is never retried inside the SDK transport.
    expect(hits(LEADERBOARD)).toBe(5);
    const firstError = seen.findIndex(text => ERROR_COPY.test(text));
    expect(seen.slice(0, firstError).every(text => text.includes('Loading top earners'))).toBe(true);
    await settle(200);
    expect(hits(LEADERBOARD)).toBe(5);
  });

  it('renders a single ranked trader as exactly one pill, never repeated to fill the strip', async () => {
    // Production 200 after the 2026-10-13 live test: one ranked wallet, net -52.72 USDL.
    const wallet = '0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154';
    const one = { ...leaderboard, data: { snapshot: { ...leaderboard.data.snapshot, entries: [{ metricRaw: '52721088', metricSigned: '-52721088', quality: 'complete', rank: 1, wallet }] } } };
    served.script.set(LEADERBOARD, [{ status: 200, body: one }]);
    const { container } = mount(<TickerStrip />);
    await waitFor(() => container.querySelector(`a[href="/profile/${wallet}"]`) !== null);
    await settle(100);
    const pills = container.querySelectorAll(`a[href="/profile/${wallet}"]`);
    expect(pills).toHaveLength(1);
    expect(pills[0].textContent).toContain('52.72');
    expect(container.querySelectorAll('section[aria-label="Market ticker"] ul')).toHaveLength(1);
    expect(container.querySelector('.sl-marquee')).toBeNull();
    expect(container.querySelector('[data-ticker-scrolling]')).toBeNull();
    expect(container.textContent).toContain('Top earners, 1 entry.');
  });

  it('only loops a list wider than the strip, with one trailing copy at most', () => {
    expect(marqueeCopies(1, 1600, 180)).toBe(1);
    expect(marqueeCopies(1, 120, 180)).toBe(1);
    expect(marqueeCopies(0, 1600, 0)).toBe(1);
    expect(marqueeCopies(3, 1600, 540)).toBe(1);
    expect(marqueeCopies(3, 0, 540)).toBe(1);
    expect(marqueeCopies(12, 1600, 2160)).toBe(2);
    expect(marqueeCopies(12, 400, 2160)).toBe(2);
  });

  it('keeps the platform activity footer loading through a retryable 503 and renders the served v2 metrics', async () => {
    served.script.set(PLATFORM, [{ status: 503, body: envelope('DEPENDENCY_UNAVAILABLE', true, 40), headers: { 'Retry-After': '0' } }, { status: 200, body: metrics }]);
    const { container, seen } = mount(<PlatformMetricsFooter />);
    await waitFor(() => container.querySelector('dl[aria-label="Platform activity"]') !== null);
    expect(hits(PLATFORM)).toBeGreaterThanOrEqual(2);
    expect(served.paths).toContain(`GET ${PLATFORM}?window=24h`);
    expect(served.paths.some(path => path.startsWith('GET /platform/metrics'))).toBe(false);
    const values = Object.fromEntries(Array.from(container.querySelectorAll('dl[aria-label="Platform activity"] > div')).map(row => [row.querySelector('dt')?.textContent, row.querySelector('dd')]));
    expect(values['24h volume']?.textContent).toBe('1.25K USDL');
    expect(values['24h traders']?.textContent).toBe('7');
    expect(values['Coins launched']?.textContent).toBe('12');
    // A producer older than 2.2.0 sends none of these: the unavailable treatment, never a number.
    for (const label of ['Market cap', '24h trades', 'New today']) {
      expect(values[label]?.hasAttribute('data-unavailable')).toBe(true);
      expect(values[label]?.textContent).toBe('—not available yet');
    }
    expect(seen.filter(text => ERROR_COPY.test(text.replace(/not available yet/g, '')))).toEqual([]);
  });

  it('renders every footer figure from the recorded 2.2.0 IF_PLATFORM bytes', async () => {
    const recorded = v2Reads.responses.platform24h;
    served.script.set(PLATFORM, [{ status: recorded.status, body: recorded.body, headers: recorded.headers }]);
    const { container } = mount(<PlatformMetricsFooter />);
    await waitFor(() => container.querySelector('dl[aria-label="Platform activity"]') !== null);
    const values = Object.fromEntries(Array.from(container.querySelectorAll('dl[aria-label="Platform activity"] > div')).map(row => [row.querySelector('dt')?.textContent, row.querySelector('dd')?.textContent]));
    expect(values).toEqual({ '24h volume': '147.28 USDL', 'Market cap': '10.1K USDL', '24h trades': '2', '24h traders': '1', 'Coins launched': '2', 'New today': '1' });
    expect(container.querySelector('[data-unavailable]')).toBeNull();
  });

  it('renders market cap, trades and new today once IF_PLATFORM serves them', async () => {
    served.script.set(PLATFORM, [{ status: 200, body: metricsUpcoming }]);
    const { container } = mount(<PlatformMetricsFooter />);
    await waitFor(() => container.querySelector('dl[aria-label="Platform activity"]') !== null);
    const values = Object.fromEntries(Array.from(container.querySelectorAll('dl[aria-label="Platform activity"] > div')).map(row => [row.querySelector('dt')?.textContent, row.querySelector('dd')]));
    expect(values['Market cap']?.textContent).toBe('9K USDL');
    expect(values['24h trades']?.textContent).toBe('42');
    expect(values['New today']?.textContent).toBe('3');
    expect(container.querySelector('[data-unavailable]')).toBeNull();
  });

  it('renders a non-retryable platform metrics failure at once', async () => {
    served.script.set(PLATFORM, [{ status: 400, body: envelope('QUERY_INVALID', false, null) }]);
    const { container } = mount(<PlatformMetricsFooter />);
    await waitFor(() => container.textContent?.includes("Couldn't load stats.") === true);
    await settle(200);
    expect(hits(PLATFORM)).toBe(1);
  });
});
