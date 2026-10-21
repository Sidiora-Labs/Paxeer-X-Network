import React, { act } from 'react';
import { createRequire } from 'node:module';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, describe, expect, it } from 'vitest';
import { SdkClient } from '@kindle/sdk';
import { serveRecorded, v2Reads } from '@/core/test-fixtures/market-production/v2-reads';
import { changeReason } from '@/core/market/pool-stats-display';

// Node environment with a real jsdom window installed (same recipe as terminal/left-rail/left-rail.render.test.tsx).
interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000/explore', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

/**
 * The api origin replays the recorded core/api 2.2.0 bytes (core-api-wire.json `v2Reads`): IF_RANKINGS
 * trending, IF_STATS_BATCH for the real chain-125 pool and the frozen pre-2.2.0 example pool, and IF_POOL_LIST.
 * Token metadata and candles (the sparkline) are not served here; names fall back to short addresses.
 */
const { real, example } = v2Reads.pools;
const replay = await serveRecorded([v2Reads.responses.rankingsTrending, v2Reads.responses.statsBatch, v2Reads.responses.poolListTrending, v2Reads.responses.poolDetailReal, v2Reads.responses.poolDetailFirst]);
process.env.NEXT_PUBLIC_DATA_API_URL = replay.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { MarketClientContext } = await import('@/core/market/client');
const { useExploreRows } = await import('./use-explore-rows');
const { ExploreTable } = await import('./explore-table');
const { ExploreGrid } = await import('./explore-grid');
const { columnSpecs, defaultSettings } = await import('./column-settings');

function Harness({ view }: { view: 'table' | 'grid' }) {
  const state = useExploreRows('trending');
  if (state.stats.state !== 'ready' || state.rows.length === 0) return <p>loading</p>;
  return view === 'table'
    ? <ExploreTable rows={state.rows} sort={null} onSort={() => undefined} columns={defaultSettings.columns} density="default" isStarred={() => false} onToggleStar={() => undefined} loading={false} hasMore={false} loadingMore={false} moreError={false} onLoadMore={() => undefined} empty={null} disclosure={null} caption="Trending coins" />
    : <ExploreGrid rows={state.rows} isStarred={() => false} onToggleStar={() => undefined} loading={false} hasMore={false} loadingMore={false} moreError={false} onLoadMore={() => undefined} empty={null} disclosure={null} label="Trending coins" />;
}

function mount(node: React.ReactElement) {
  const queryClient: QueryClientType = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const client = new SdkClient({ apiOrigin: replay.origin, identityOrigin: replay.origin, mediaOrigin: replay.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => { root.render(<QueryClientProvider client={queryClient}><MarketClientContext.Provider value={client}>{node}</MarketClientContext.Provider></QueryClientProvider>); });
  return { container, unmount() { act(() => root.unmount()); queryClient.clear(); container.remove(); } };
}

async function waitFor(check: () => boolean, timeoutMs = 4000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

/** Cells by column id (every default column is visible, in columnSpecs order) for the row whose coin link points at `pool`. */
function cells(container: HTMLElement, pool: string): Record<string, HTMLElement> {
  const headers = columnSpecs.map(spec => spec.id);
  const row = Array.from(container.querySelectorAll('tbody tr')).find(tr => tr.querySelector(`a[href$="${pool}"]`));
  if (!row) throw new Error(`No row for ${pool}`);
  return Object.fromEntries(Array.from(row.querySelectorAll('td')).map((td, index) => [headers[index], td as HTMLElement]));
}

afterAll(async () => { await replay.close(); dom.window.close(); });

describe('explore rows from the recorded 2.2.0 v2 reads', () => {
  it('renders market cap, traders, transactions and the window-change reasons in the table', async () => {
    const view = mount(<Harness view="table" />);
    await waitFor(() => view.container.querySelectorAll('tbody tr').length === 2 && (view.container.textContent ?? '').includes('$10.1K'));
    const row = cells(view.container, real);
    // totalSupplyValuationUsdlRaw 10098226981 (price x total supply), circulating 48.51 USDL on hover.
    expect(row.mcap.textContent).toBe('$10.1K');
    expect(row.mcap.querySelector('[title]')?.getAttribute('title')).toBe('Circulating market cap 48.51 USDL');
    expect(row.txns.textContent).toBe('2');
    expect(row.traders.textContent).toBe('1');
    // Every baseline of the real pool is insufficient_history: a dash with the reason, never a number.
    expect(row.change1h.querySelector('[aria-label]')?.getAttribute('aria-label')).toBe(`Change: ${changeReason('1h', 'insufficient_history')}`);
    expect(row.change6h.querySelector('[aria-label]')?.getAttribute('aria-label')).toBe(`Change: ${changeReason('6h', 'insufficient_history')}`);
    expect(row.change24h.querySelector('[aria-label]')?.getAttribute('aria-label')).toBe(`Change: ${changeReason('24h', 'insufficient_history')}`);
    // The frozen pre-2.2.0 example row has no window fields, so its cells say the field is not served.
    const legacy = cells(view.container, example);
    expect(legacy.change1h.querySelector('[aria-label]')?.getAttribute('aria-label')).toBe(`Change: ${changeReason('1h', undefined)}`);
    expect(legacy.traders.textContent).toBe('—');
    expect(view.container.textContent).not.toMatch(/not available yet/);
    view.unmount();
  });

  it('renders the price and the top-10 and dev shares on the explore cards', async () => {
    const view = mount(<Harness view="grid" />);
    await waitFor(() => view.container.querySelectorAll('li').length >= 2 && (view.container.textContent ?? '').includes('Top 10100%'));
    const card = Array.from(view.container.querySelectorAll('li')).find(li => li.querySelector(`a[href$="${real}"]`));
    const stats = Object.fromEntries(Array.from(card?.querySelectorAll('dl div') ?? []).map(div => [div.querySelector('dt')?.textContent, div.querySelector('dd')?.textContent]));
    expect(stats).toMatchObject({ Price: '$0.0₄1009', 'Top 10': '100%', Creator: '100%', 'Txns 24h': '2', 'Traders 24h': '1' });
    view.unmount();
  });
});
