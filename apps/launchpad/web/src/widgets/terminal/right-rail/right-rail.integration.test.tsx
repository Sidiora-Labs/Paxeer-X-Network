import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';

// Node environment with real jsdom globals (see widgets/profile/leaderboard-panel.test.tsx): jsdom's
// AbortSignal is rejected by Node's fetch, so the SDK would fail before reaching the local origin.
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

type Watermark = V2ContractTypes['Watermark'];
type PoolStats = V2ContractTypes['PoolStats'];
type Candle = V2ContractTypes['Candle'];
type Responder = (method: string, url: string) => { status: number; body?: unknown };
type Served = { server: Server; origin: string; paths: string[] };

function serve(responder: Responder): Promise<Served> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(`${request.method} ${request.url ?? ''}`);
    request.resume();
    request.on('end', () => {
      const answer = responder(request.method ?? 'GET', request.url ?? '');
      response.statusCode = answer.status;
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

// Legacy stats, metadata and profile hosts answer 404 so nothing leaves the machine; the SDK-routed
// reads (IF_SEARCH, IF_STATS_BATCH, IF_CANDLES) go to the per-test origin through MarketClientContext.
const absent = await serve(() => ({ status: 404 }));
process.env.NEXT_PUBLIC_DATA_API_URL = absent.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = absent.origin;
process.env.NEXT_PUBLIC_GATEWAY_URL = absent.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { MarketClientContext } = await import('@/core/market/client');
const { SimilarTokensList } = await import('./similar-tokens-list');
const { WindowStatsCard } = await import('./window-stats-card');

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const snapshotId = '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b';
const current = '0x' + 'aa'.repeat(20);
const poolB = '0x' + 'bb'.repeat(20);
const poolC = '0x' + 'cc'.repeat(20);
const tokenB = '0x' + 'b1'.repeat(20);
const tokenC = '0x' + 'c1'.repeat(20);
const usdl = '0x' + 'ef'.repeat(20);
const observedAt = '2026-10-13T12:00:00.000Z';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt };
const chainPosition = (height: number) => ({ blockHash: '0x' + height.toString(16).padStart(64, '0'), chainId: '125' as const, height: String(height), logIndex: '0', transactionHash: '0x' + (height + 1).toString(16).padStart(64, '0'), transactionIndex: '0' });

function stats(pool: string, liquidity: string, volume: string, mcap: string): PoolStats {
  return {
    poolAddress: pool, buys24h: '3', sells24h: '2', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: null, totalSupplyValuationUsdlRaw: mcap, holderCount: '9', indicativeTvlUsdlRaw: null,
    priceChangeBps24h: '0', realUsdlBalanceRaw: liquidity, sourceUpperPosition: chainPosition(4200), supplyValuationQuality: 'verified', valuationChangeUsdlRaw: null, valuationMethod: null,
    volume24hRaw: volume, watermark, windowEnd: '2026-10-13T12:00:00.000Z', windowStart: '2026-10-12T12:00:00.000Z',
  };
}

const price = (numerator: string) => ({
  numerator, denominator: '1000000',
  baseAsset: { address: tokenB, chainId: '125', currencyCode: null, decimals: 6, kind: 'erc20' as const },
  quoteAsset: { address: usdl, chainId: '125', currencyCode: null, decimals: 6, kind: 'erc20' as const },
  source: { amountBasis: 'paid_input' as const, kind: 'gross_execution' as const, observedAt, position: null, providerId: null, sourceEventId: null, sourceReference: null },
});

function candle(start: string, interval: Candle['interval']): Candle {
  return {
    poolAddress: current, interval, start, coverage: 'complete', synthetic: false, seriesModelVersion: 'v1', snapshotId, revision: '1',
    firstPosition: chainPosition(100), lastPosition: chainPosition(102), open: price('1000000'), close: price('1250000'), high: price('1250000'), low: price('1000000'),
    buyVolumeUsdlRaw: '5000000', sellVolumeUsdlRaw: '2000000', volumeUsdlRaw: '7000000', volumeTokenRaw: '10', trades: '3',
    uniqueTraderCount: null, largeTradeCount: null, largeTradePolicyId: null, largeTradeThresholdUsdlRaw: null,
  };
}

const envelope = (data: unknown) => ({ requestId, generatedAt: observedAt, nextCursor: null, quality: 'complete', revision: null, watermark: null, data });

function clientFor(origin: string): SdkClient {
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
}

function mount(origin: string, node: React.ReactElement): { root: Root; container: HTMLDivElement; queryClient: QueryClientType } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(<QueryClientProvider client={queryClient}><MarketClientContext.Provider value={clientFor(origin)}>{node}</MarketClientContext.Provider></QueryClientProvider>);
  });
  return { root, container, queryClient };
}

function unmount(root: Root, container: HTMLDivElement, queryClient: QueryClientType) {
  act(() => root.unmount());
  queryClient.clear();
  container.remove();
}

async function waitFor(check: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

describe('right rail against a real HTTP origin', () => {
  let market: Served;
  let unserved: Served;

  beforeAll(async () => {
    unserved = await serve(() => ({ status: 404 }));
    market = await serve((method, url) => {
      if (method === 'GET' && url.startsWith('/v2/search?')) {
        const query = new URLSearchParams(url.split('?')[1]);
        if (query.get('type') !== 'pool' || query.get('q') !== 'DOLL' || query.get('limit') !== '10') return { status: 422 };
        return { status: 200, body: envelope({ items: [
          { id: `pool:${current}`, kind: 'pool', label: 'Doge Troll', poolAddress: current, score: '1', tokenAddress: null, wallet: null },
          { id: `pool:${poolC}`, kind: 'pool', label: 'Pink Doll', poolAddress: poolC, score: '0.8', tokenAddress: tokenC, wallet: null },
          { id: `pool:${poolB}`, kind: 'pool', label: 'Dollhouse', poolAddress: poolB, score: '0.7', tokenAddress: tokenB, wallet: null },
        ] }) };
      }
      if (method === 'POST' && url === '/v2/pools/stats:batch') {
        return { status: 200, body: { ...envelope({ items: [stats(poolB, '62000000000', '5000000', '28000000000'), stats(poolC, '37000000000', '1000', '1000000')], missing: [] }), watermark } };
      }
      if (method === 'GET' && url.startsWith(`/v2/pools/${current}/candles?`)) {
        const query = new URLSearchParams(url.split('?')[1]);
        const interval = query.get('interval') as Candle['interval'];
        const items = interval === '1h' ? [candle(new Date(Math.floor(Date.now() / 3_600_000) * 3_600_000).toISOString(), '1h')] : [];
        return { status: 200, body: { ...envelope({ items, seriesModelVersion: 'v1', snapshotId }), watermark } };
      }
      return { status: 404 };
    });
  });

  afterAll(() => { market.server.close(); unserved.server.close(); absent.server.close(); });

  it('lists similar pools from IF_SEARCH and IF_STATS_BATCH, excluding the open pool, sorted by liquidity', async () => {
    const view = mount(market.origin, <SimilarTokensList poolAddress={current} symbol="DOLL" sort="liq" />);
    await waitFor(() => view.container.querySelectorAll('li a').length === 2 && !view.container.querySelector('ul[aria-busy]'));
    const links = Array.from(view.container.querySelectorAll('li a')).map(link => link.getAttribute('href'));
    expect(links).toEqual([`/terminal/${poolB}`, `/terminal/${poolC}`]);
    expect(view.container.textContent).toContain('62K');
    expect(view.container.textContent).toContain('28K');
    expect(view.container.textContent).not.toContain(current);
    expect(market.paths.some(path => path.startsWith('POST /v2/pools/stats:batch'))).toBe(true);
    expect(BORDER.test(view.container.innerHTML)).toBe(false);
    unmount(view.root, view.container, view.queryClient);
  });

  it('renders the real unavailable state when search is not served', async () => {
    const view = mount(unserved.origin, <SimilarTokensList poolAddress={current} symbol="DOLL" sort="liq" />);
    await waitFor(() => /not available yet/.test(view.container.textContent ?? ''));
        expect(view.container.textContent).not.toContain('IF_SEARCH');
    unmount(view.root, view.container, view.queryClient);
  });

  it('derives the 24H window from served candles and states fill coverage', async () => {
    const view = mount(market.origin, <WindowStatsCard poolAddress={current} fills={[]} stats={null} />);
    await waitFor(() => /Net vol/.test(view.container.textContent ?? '') && /5\.00/.test(view.container.textContent ?? ''));
    const text = view.container.textContent ?? '';
    expect(text).toContain('7.00');
    expect(text).toContain('+3.00');
    expect(text).toContain('Buy and sell counts are still loading for this window.');
    expect(text).toContain('+25');
    expect(market.paths.filter(path => path.includes('/candles?')).length).toBeGreaterThanOrEqual(4);
    expect(BORDER.test(view.container.innerHTML)).toBe(false);
    unmount(view.root, view.container, view.queryClient);
  });

  it('shows dashes when candles are not served, keeping the operation out of the visible text', async () => {
    const view = mount(unserved.origin, <WindowStatsCard poolAddress={current} fills={[]} stats={null} />);
    await waitFor(() => /not available yet/.test(view.container.textContent ?? ''));
        expect(view.container.textContent).not.toContain('IF_CANDLES');
    expect(view.container.querySelectorAll('[aria-label^="Not available"]').length).toBe(4);
    unmount(view.root, view.container, view.queryClient);
  });
});
