import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';

// Same harness as home-empty-states.test.tsx (and widgets/profile/leaderboard-panel.test.tsx): node environment with the browser
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
const { TrendingWidget, trendingEmptyTitles } = await import('./trending-widget');
const { ScreenerWidget, screenerEmpty } = await import('./screener-widget');
const { derivePanelState } = await import('./panel-state');
const { ColumnChrome } = await import('./column-chrome');
const { Column } = await import('./column');
const { DashboardBoard, columnSlotStyle, boardVisibleClass, compactHomeQuery } = await import('./dashboard-board');
const { timeframes, timeframeSegmentsMinWidth } = await import('./trending-widget');
const { HomeFeed, homeFeedTabs } = await import('./home-feed');
const { default: HomeModule } = await import('@/page-modules/home');
const { addColumn, defaultLayout, resetLayoutStore, setLayout } = await import('./layout-store');

type Watermark = V2ContractTypes['Watermark'];
type RankingsResponse = V2ContractTypes['RankingsResponse'];

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };

/** The empty first page core/api answers before any pool exists (dev_market.go rankings handler). */
function emptyRanking(category: string): RankingsResponse {
  return {
    requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'partial', revision: '7', watermark,
    data: { snapshot: { snapshotId: '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b', category: category as RankingsResponse['data']['snapshot']['category'], modelVersion: 'dev-v1', computedAt: '2026-10-12T15:00:00.000Z', watermark, items: [], totalEligible: '0', truncated: false, replace: true } },
  };
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

const props = { columnId: 'home-1', filter: '', density: 'comfortable' as const, view: 'list' as const };
const ERROR_TITLE = /Couldn't load (trending coins|coins)/;

/** A ranking origin whose answer can be switched and held open, to observe the panel mid-refetch. */
function switchable() {
  let mode: 'fail' | 'empty' = 'fail';
  let gate: Promise<void> = Promise.resolve();
  let open = () => undefined as void;
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(request.url ?? '');
    void gate.then(() => {
      const ranking = /^\/v2\/rankings\/([a-z]+)$/.exec(request.url ?? '');
      if (!ranking || mode === 'fail') { response.statusCode = 500; response.end(); return; }
      response.statusCode = 200;
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(emptyRanking(ranking[1])));
    });
  });
  return {
    paths,
    server,
    setMode(next: 'fail' | 'empty') { mode = next; },
    hold() { gate = new Promise<void>(resolve => { open = resolve; }); },
    release() { open(); gate = Promise.resolve(); },
    listen(): Promise<string> {
      return new Promise(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
    },
  };
}

type PanelSnapshot = { skeleton: boolean; error: boolean; empty: boolean };

function panelOf(container: HTMLElement, emptyTitle: string): PanelSnapshot {
  const text = container.textContent ?? '';
  return {
    skeleton: container.querySelector('[aria-busy="true"]') !== null || /Loading (coins|screener)/.test(text),
    error: ERROR_TITLE.test(text),
    empty: text.includes(emptyTitle),
  };
}

/** Record the panel after every DOM mutation, so a one-frame skeleton between two error renders is caught. */
function recordPanels(container: HTMLElement, emptyTitle: string): { seen: PanelSnapshot[]; stop(): void } {
  const seen: PanelSnapshot[] = [panelOf(container, emptyTitle)];
  const observer = new MutationObserver(() => { seen.push(panelOf(container, emptyTitle)); });
  observer.observe(container, { childList: true, subtree: true, characterData: true, attributes: true });
  return { seen, stop: () => observer.disconnect() };
}

describe('panel state derivation', () => {
  const failure = { state: 'error' as const, error: new Error('boom'), retry: () => undefined };
  const unsupported = { state: 'unsupported' as const, operation: 'IF_RANKINGS', status: 503, message: 'not served' };
  const ready = { state: 'ready' as const, data: 1 };

  it('shows the skeleton only while loading with nothing settled', () => {
    expect(derivePanelState(true, ready, null)).toEqual({ state: 'loading' });
    expect(derivePanelState(true, failure, null)).toEqual({ state: 'loading' });
  });

  it('keeps a settled error or unsupported state through a refetch that has no data yet', () => {
    expect(derivePanelState(true, { state: 'loading' }, failure)).toBe(failure);
    expect(derivePanelState(true, { state: 'loading' }, unsupported)).toBe(unsupported);
  });

  it('replaces the settled state as soon as the refetch settles', () => {
    expect(derivePanelState(false, ready, failure)).toBe(ready);
    expect(derivePanelState(false, unsupported, failure)).toBe(unsupported);
    expect(derivePanelState(true, { state: 'loading' }, ready)).toEqual({ state: 'loading' });
  });
});

describe('ranking panels render exactly one state across refetches', () => {
  const origin = switchable();
  let url = '';

  beforeAll(async () => { url = await origin.listen(); });
  afterAll(async () => {
    for (const server of [origin.server, profiles.server]) await new Promise<void>(resolve => server.close(() => resolve()));
    dom.window.close();
  });

  for (const [name, Widget, emptyTitle] of [
    ['Trending', TrendingWidget, trendingEmptyTitles.trending],
    ['Screener', ScreenerWidget, screenerEmpty.new.title],
  ] as const) {
    it(`${name}: skeleton, then error; a background refetch keeps the error, then the data replaces it`, async () => {
      origin.setMode('fail');
      origin.hold();
      const { root, container, queryClient } = mount(url, <Widget {...props} />);
      const record = recordPanels(container, emptyTitle);
      await waitFor(() => origin.paths.length > 0);
      expect(panelOf(container, emptyTitle)).toEqual({ skeleton: true, error: false, empty: false });
      origin.release();
      await waitFor(() => panelOf(container, emptyTitle).error);

      // Background refetch (poll / focus) of a query that has no data: React Query flips it back to pending.
      origin.setMode('empty');
      origin.hold();
      const before = origin.paths.length;
      let refetch!: Promise<unknown>;
      await act(async () => { refetch = queryClient.refetchQueries(); await new Promise(resolve => setTimeout(resolve, 50)); });
      await waitFor(() => origin.paths.length > before);
      expect(queryClient.getQueryCache().getAll().some(query => query.state.status === 'pending' && query.state.fetchStatus === 'fetching')).toBe(true);
      expect(panelOf(container, emptyTitle)).toEqual({ skeleton: false, error: true, empty: false });
      origin.release();
      await act(async () => { await refetch; });
      await waitFor(() => panelOf(container, emptyTitle).empty);

      // A background refetch with data present keeps the data.
      origin.hold();
      await act(async () => { refetch = queryClient.refetchQueries(); await new Promise(resolve => setTimeout(resolve, 50)); });
      expect(panelOf(container, emptyTitle)).toEqual({ skeleton: false, error: false, empty: true });
      origin.release();
      await act(async () => { await refetch; });
      record.stop();

      for (const seen of record.seen) expect(Number(seen.skeleton) + Number(seen.error) + Number(seen.empty)).toBeLessThanOrEqual(1);
      const errorAt = record.seen.findIndex(seen => seen.error);
      expect(errorAt).toBeGreaterThan(0);
      expect(record.seen.slice(errorAt).some(seen => seen.skeleton)).toBe(false);
      unmount(root, container, queryClient);
    }, 15000);
  }
});

describe('column header layout', () => {
  const column = { id: 'home-1', type: 'trending' as const, filter: '', density: 'comfortable' as const, view: 'list' as const };
  const noop = () => undefined;

  it('sizes the title trigger to its label so it sits beside the type icon', () => {
    const { root, container, queryClient } = mount(url0(), <ColumnChrome column={column} index={0} count={5} onMove={noop} onRemove={noop} onChangeType={noop} onPatch={noop} onDragStart={noop} onDragEnd={noop} />);
    const title = container.querySelector('h2 button');
    const classes = title?.className.split(/\s+/) ?? [];
    expect(title?.textContent).toBe('Trending');
    expect(classes).toContain('h-control-height-sm');
    expect(classes.some(name => name.startsWith('size-'))).toBe(false);
    expect(title?.getAttribute('aria-label')).toBe('Trending');
    const row = container.querySelector('h2')?.parentElement;
    const icon = row?.querySelector(':scope > svg');
    expect(icon?.nextElementSibling?.tagName).toBe('H2');
    expect(getComputedStyle(icon as Element).position).not.toBe('absolute');
    unmount(root, container, queryClient);
  });

  it('gives the visually hidden grip hint a positioned ancestor inside the column', () => {
    const { root, container, queryClient } = mount(url0(), <Column column={column} index={4} count={5} dragging={null} onDragStart={noop} onDragEnd={noop} onDrop={noop} onMove={noop} onRemove={noop} onChangeType={noop} onPatch={noop} />);
    const hint = Array.from(container.querySelectorAll('.sr-only')).find(node => node.textContent?.includes('Position 5 of 5'));
    expect(hint).toBeDefined();
    expect(hint?.parentElement?.className.split(/\s+/)).toContain('relative');
    expect(container.querySelector('section')?.className.split(/\s+/)).toContain('relative');
    unmount(root, container, queryClient);
  });
});

function url0(): string { return profiles.origin; }

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const classesOf = (root: ParentNode) => Array.from(root.querySelectorAll('[class]')).flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));

describe('column tab rows never clip a control', () => {
  it('Trending: ranking tabs and a change window that is a segmented group in a wide column, a compact pill in a narrow one', async () => {
    const { root, container, queryClient } = mount(url0(), <TrendingWidget {...props} />);
    const ranking = container.querySelector('[role="radiogroup"][aria-label="Ranking"]')!;
    const toolbar = ranking.parentElement!;
    const classes = toolbar.className.split(/\s+/);
    expect(classes).toContain('flex-wrap');
    expect(classes.some(name => /^overflow-/.test(name) || name === 'h-10')).toBe(false);
    // The widget root is the size container both forms query; exactly one of them is displayed at any column width.
    expect(toolbar.parentElement?.className.split(/\s+/)).toContain('@container');
    expect(timeframeSegmentsMinWidth).toBe(376);
    const segmentsGroup = toolbar.querySelector<HTMLElement>('[role="radiogroup"][aria-label="Change window"]')!;
    const segmentsSlot = segmentsGroup.parentElement!;
    expect(segmentsSlot.getAttribute('data-timeframe')).toBe('segments');
    expect(segmentsSlot.className.split(/\s+/)).toEqual(expect.arrayContaining(['hidden', '@min-[376px]:flex', 'ml-auto']));
    const segments = Array.from(segmentsGroup.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    expect(segments.map(segment => segment.textContent)).toEqual(timeframes);
    expect(segments.find(segment => segment.getAttribute('aria-checked') === 'true')?.textContent).toBe('24h');
    expect(segments.filter(segment => segment.getAttribute('aria-label')?.endsWith('coming soon')).map(segment => segment.textContent)).toEqual(['4h', '12h']);
    const trigger = toolbar.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!;
    expect(trigger.parentElement?.className.split(/\s+/)).toContain('@min-[376px]:hidden');
    // Both forms drive one window: a segment pick shows in the pill, and the unserved notice follows it.
    act(() => { segments[1].click(); });
    expect(trigger.textContent).toBe('4h');
    expect(container.textContent).toContain('4h change');
    act(() => { segments[3].click(); });
    expect(trigger.textContent).toBe('24h');
    expect(trigger.getAttribute('aria-label')).toBe('Change window');
    expect(trigger.textContent).toBe('24h');
    act(() => { trigger.click(); });
    await waitFor(() => document.querySelector('[role="listbox"][aria-label="Change window"]') !== null);
    const options = Array.from(document.querySelectorAll('[role="listbox"][aria-label="Change window"] [role="option"]'));
    expect(options.map(option => option.textContent?.replace('soon', '').trim())).toEqual(timeframes);
    expect(options.filter(option => option.textContent?.includes('soon')).map(option => option.textContent?.replace('soon', '').trim())).toEqual(['4h', '12h']);
    act(() => { (options[0] as HTMLElement).click(); });
    expect(trigger.textContent).toBe('1h');
    expect(classesOf(toolbar).filter(name => BORDER.test(name))).toEqual([]);
    unmount(root, container, queryClient);
  });

  it('Trending: ranking tabs and the window pill are sized for one line at the 288 px minimum column', () => {
    const { root, container, queryClient } = mount(url0(), <TrendingWidget {...props} />);
    const ranking = container.querySelector('[role="radiogroup"][aria-label="Ranking"]')!;
    const toolbar = ranking.parentElement!;
    const segments = Array.from(ranking.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    // Every ranking stays reachable under its full name; the visible label is the short form.
    expect(segments.map(segment => segment.getAttribute('aria-label'))).toEqual(['Trending', 'Unusual', 'Movers', 'Volume']);
    expect(segments.map(segment => segment.getAttribute('title'))).toEqual(['Trending', 'Unusual', 'Movers', 'Volume']);
    expect(segments.map(segment => segment.textContent)).toEqual(['Trend', 'Unusual', 'Movers', 'Vol']);
    for (const segment of segments) {
      const classes = segment.className.split(/\s+/);
      expect(classes).toContain('whitespace-nowrap');
      expect(classes).toContain('px-1.5');
      expect(classes.some(name => /^(truncate|overflow-|text-ellipsis|line-clamp)/.test(name))).toBe(false);
    }
    const trigger = toolbar.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!;
    // Value-only pill: no chevron, compact padding, the full name on hover, pinned to the row's right slot.
    expect(trigger.querySelector('svg')).toBeNull();
    expect(trigger.className.split(/\s+/)).toEqual(expect.arrayContaining(['px-2', 'whitespace-nowrap']));
    expect(trigger.getAttribute('title')).toBe('Change window: 24h');
    expect(trigger.parentElement?.className.split(/\s+/)).toContain('ml-auto');
    // Width budget at the 288 px minimum: 272 px inside the row's 8 px side padding. Text widths are bold 13 px
    // Manrope as measured in Chromium (canvas measureText); a label missing here must be measured before it ships.
    const measured: Record<string, number> = { Trend: 38.2, Unusual: 53.9, Movers: 48.4, Vol: 20, '1h': 14.4, '4h': 16.7, '12h': 22.5, '24h': 24.6 };
    const width = (text: string | null) => { expect(measured).toHaveProperty([text ?? '']); return measured[text ?? '']; };
    const tabs = segments.reduce((sum, segment) => sum + width(segment.textContent) + 12, 4);
    const pill = Math.max(...timeframes.map(frame => width(frame))) + 16;
    expect(tabs + 8 + pill).toBeLessThanOrEqual(288 - 16);
    width(trigger.textContent);
    // From the container-query width the four window segments sit on the same line as the ranking tabs.
    const windowSegments = timeframes.reduce((sum, frame) => sum + width(frame) + 12, 4);
    expect(tabs + 8 + windowSegments).toBeLessThanOrEqual(timeframeSegmentsMinWidth - 16);
    unmount(root, container, queryClient);
  });

  it('Leaderboard: tabs on the left and the window dropdown pinned right, in a wrapping row with no clipping', async () => {
    const { LeaderboardWidget } = await import('./leaderboard-widget');
    const { root, container, queryClient } = mount(url0(), <LeaderboardWidget {...props} />);
    const tabs = container.querySelector<HTMLElement>('[role="radiogroup"][aria-label="Leaderboard"]')!;
    expect(Array.from(tabs.querySelectorAll('[role="radio"]')).map(tab => tab.textContent)).toEqual(['Traders', 'Creators', 'Volume']);
    const toolbar = tabs.parentElement!;
    expect(toolbar.className.split(/\s+/)).toContain('flex-wrap');
    expect(toolbar.className.split(/\s+/).some(name => /^overflow-/.test(name))).toBe(false);
    const pill = toolbar.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!;
    expect(pill.textContent).toBe('24h');
    expect(pill.parentElement?.className.split(/\s+/)).toContain('ml-auto');
    // Chromium at 1024: tabs 198 px + gap 8 + pill 69 px = 275 px inside the 321 px column less 16 px row padding.
    expect(198 + 8 + 69).toBeLessThanOrEqual(321 - 16);
    expect(classesOf(toolbar).filter(name => BORDER.test(name))).toEqual([]);
    unmount(root, container, queryClient);
  });

  it('Screener: the risk legend button is the info icon with an accessible label, not a bare letter', () => {
    const { root, container, queryClient } = mount(url0(), <ScreenerWidget {...props} />);
    const legend = container.querySelector<HTMLButtonElement>('button[aria-label="What these numbers mean"]')!;
    expect(legend).not.toBeNull();
    expect(legend.textContent).toBe('');
    const icon = legend.querySelector('svg');
    expect(icon?.getAttribute('aria-hidden')).toBe('true');
    expect(icon?.getAttribute('class')).toContain('lucide-info');
    const toolbar = container.querySelector('[role="radiogroup"][aria-label="Screener list"]')!.parentElement!;
    expect(toolbar.className.split(/\s+/)).toContain('flex-wrap');
    // ml-auto sits on the tooltip anchor, the toolbar's direct child, so the icon pins to the right edge.
    expect(legend.parentElement?.className.split(/\s+/)).toContain('ml-auto');
    unmount(root, container, queryClient);
  });
});

describe('dashboard board fills the width with its columns and makes overflow usable', () => {
  /** Evaluate the slot width expression the board ships for a list width, visible count and column count (px). */
  function slotWidth(listWidth: number, visible: number, count: number, gap = 8): number {
    const expression = columnSlotStyle.width
      .replace(/^calc/, '')
      .replace('100%', String(listWidth))
      .replaceAll('var(--board-visible)', String(visible))
      .replaceAll('var(--board-count)', String(count))
      .replace('var(--sl-dashboard-column-gap)', String(gap))
      .replaceAll('min(', 'Math.min(');
    expect(expression).toMatch(/^[\d\s().*/+,Mathmin-]+$/);
    return Function(`return ${expression}`)() as number;
  }

  it('fills the main area with the three default columns at 1024, 1280, 1440 and 1920 (no empty tail, no rail)', () => {
    // Viewport - 56 sidebar - p-2 (16): the list width when nothing overflows.
    for (const [viewport, visible, expected] of [[1024, 3, 312], [1280, 3, 397.3], [1440, 3, 450.7], [1920, 4, 610.7]] as const) {
      const list = viewport - 56 - 16;
      const width = slotWidth(list, visible, 3);
      expect(width).toBeCloseTo(expected, 0);
      expect(width).toBeGreaterThanOrEqual(288);
      expect(3 * width + 2 * 8).toBeCloseTo(list, 5);
    }
    expect(boardVisibleClass).toContain('lg:[--board-visible:3]');
    expect(boardVisibleClass).toContain('min-[1800px]:[--board-visible:4]');
    // A board with more columns than fit keeps whole columns of the visible count: at 1920, 4 of 6.
    expect(slotWidth(1920 - 56 - 16 - 40, 4, 6)).toBeCloseTo((1808 - 24) / 4, 5);
    // Below lg home is the callouts feed, not the board.
    expect(compactHomeQuery).toBe('(max-width: 1023px)');
  });

  it('sizes every column slot from the board, shows a surface-2 scroll rail only on overflow, and pages by one column', async () => {
    resetLayoutStore();
    const { root, container, queryClient } = mount(url0(), <DashboardBoard />);
    const list = container.querySelector<HTMLElement>('[role="list"][aria-label="Dashboard columns"]')!;
    expect(list.style.getPropertyValue('--board-count')).toBe('3');
    let slots = Array.from(list.children) as HTMLElement[];
    expect(slots.length).toBe(3);
    for (const slot of slots) {
      expect(slot.style.width).toBe(columnSlotStyle.width);
      expect(slot.className.split(/\s+/)).toContain('min-w-[288px]');
    }
    for (const section of Array.from(list.querySelectorAll('section[data-column-id]'))) expect(section.className.split(/\s+/)).toContain('w-full');
    expect(Array.from(list.querySelectorAll('section[data-column-id]')).map(section => section.getAttribute('aria-label'))).toEqual(['Trending', 'Hot callouts', 'Leaderboard']);
    expect(list.getAttribute('data-board-overflow')).toBe('false');
    expect(container.querySelector('[data-board-rail]')).toBeNull();

    // Six columns on a 1440 board: three of 451 px fit, three more sit behind the rail.
    let six = defaultLayout();
    for (const type of ['callouts', 'screener', 'watchlist'] as const) six = addColumn(six, type);
    act(() => { setLayout(six); });
    slots = Array.from(list.children) as HTMLElement[];
    expect(slots.length).toBe(6);
    expect(list.style.getPropertyValue('--board-count')).toBe('6');
    Object.defineProperty(list, 'clientWidth', { configurable: true, value: 1328 });
    Object.defineProperty(list, 'scrollWidth', { configurable: true, value: 6 * 437 + 5 * 8 });
    slots.forEach((slot, index) => {
      Object.defineProperty(slot, 'offsetLeft', { configurable: true, value: index * 445 });
      Object.defineProperty(slot, 'offsetWidth', { configurable: true, value: 437 });
    });
    const scrolls: Array<{ left?: number }> = [];
    list.scrollBy = ((options: ScrollToOptions) => { scrolls.push({ left: options.left }); }) as typeof list.scrollBy;
    act(() => { list.dispatchEvent(new window.Event('scroll')); });

    expect(list.getAttribute('data-board-overflow')).toBe('true');
    const rail = container.querySelector<HTMLElement>('[data-board-rail]')!;
    expect(rail.className.split(/\s+/)).toContain('bg-surface-2');
    expect(classesOf(rail.parentElement!).concat(rail.className.split(/\s+/)).filter(name => BORDER.test(name))).toEqual([]);
    expect(rail.textContent).toContain('+3');
    const left = rail.querySelector<HTMLButtonElement>('button[aria-label="Scroll columns left"]')!;
    const right = rail.querySelector<HTMLButtonElement>('button[aria-label="Scroll columns right, 3 more"]')!;
    expect(left.disabled).toBe(true);
    expect(right.disabled).toBe(false);
    act(() => { right.click(); });
    expect(scrolls).toEqual([{ left: 437 + 8 }]);

    // Scrolled to the end: right disables, nothing hidden after.
    list.scrollLeft = 6 * 437 + 5 * 8 - 1328;
    act(() => { list.dispatchEvent(new window.Event('scroll')); });
    expect(rail.querySelector<HTMLButtonElement>('button[aria-label="Scroll columns right"]')?.disabled).toBe(true);
    expect(left.disabled).toBe(false);
    // The scrollbar stays visible with track contrast instead of being hidden.
    expect(list.className).toContain('[scrollbar-width:auto]');
    expect(list.className).not.toContain('scrollbar-none');

    // Callouts columns put the Live / Paused pill in the header's right cluster, not in a tab row.
    const hotSection = list.querySelector<HTMLElement>('section[data-column-type="hot-callouts"]')!;
    const actions = hotSection.querySelector<HTMLElement>('[data-column-actions]')!;
    const live = actions.querySelector<HTMLButtonElement>('button[aria-pressed]')!;
    expect(live.textContent).toBe('Live');
    expect(live.closest('h2')).toBeNull();
    expect(actions.parentElement?.querySelector('h2')).not.toBeNull();
    // Below 340 px of header the label is visually hidden (still the accessible name); the icon stays.
    expect(live.querySelector('span')?.className.split(/\s+/)).toEqual(expect.arrayContaining(['sr-only', '@min-[340px]:not-sr-only']));
    expect(Array.from(hotSection.querySelectorAll('button[aria-pressed]')).filter(button => /^(Live|Paused)/.test(button.textContent ?? ''))).toHaveLength(1);
    expect(hotSection.querySelector('[role="region"]')?.firstElementChild?.firstElementChild?.getAttribute('aria-busy')).not.toBeNull();
    act(() => { live.click(); });
    expect(live.getAttribute('aria-pressed')).toBe('true');
    expect(live.textContent).toBe('Paused');
    // Columns without a header control keep an empty, hidden slot.
    const trendingActions = list.querySelector<HTMLElement>('section[data-column-type="trending"] [data-column-actions]')!;
    expect(trendingActions.childElementCount).toBe(0);
    expect(trendingActions.className.split(/\s+/)).toContain('empty:hidden');
    // Flush, square columns.
    for (const section of Array.from(list.querySelectorAll('section[data-column-id]'))) expect(section.className).not.toMatch(/\brounded/);

    // Removing every column leaves an add-column card that can restore the default board.
    act(() => { setLayout({ version: 1, columns: [] }); });
    const empty = container.querySelector<HTMLElement>('[data-board-empty]')!;
    expect(empty.querySelector('button[aria-label="Add column"], button')?.textContent).toContain('Add column');
    expect(container.querySelector('[data-board-rail]')).toBeNull();
    act(() => { Array.from(empty.querySelectorAll('button')).find(button => button.textContent === 'Restore the default board')?.click(); });
    expect(Array.from(container.querySelectorAll('section[data-column-id]')).map(section => section.getAttribute('aria-label'))).toEqual(['Trending', 'Hot callouts', 'Leaderboard']);
    unmount(root, container, queryClient);
    resetLayoutStore();
  }, 15000);
});

describe('home below lg is a single callouts feed', () => {
  function withViewport(compact: boolean): () => void {
    const previous = window.matchMedia;
    window.matchMedia = ((query: string) => ({
      matches: query === compactHomeQuery ? compact : false,
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    })) as typeof window.matchMedia;
    return () => { window.matchMedia = previous; };
  }

  it('renders the feed, not the board or a widget switcher, at 768 and 390; the board from 1024', () => {
    resetLayoutStore();
    let restore = withViewport(true);
    let view = mount(url0(), <HomeModule />);
    expect(view.container.querySelector('[data-home-feed]')).not.toBeNull();
    expect(view.container.querySelector('[role="list"][aria-label="Dashboard columns"]')).toBeNull();
    expect(view.container.querySelector('[role="radiogroup"][aria-label="Dashboard column"]')).toBeNull();
    expect(view.container.textContent).not.toContain('Add column');
    unmount(view.root, view.container, view.queryClient);
    restore();

    restore = withViewport(false);
    view = mount(url0(), <HomeModule />);
    expect(view.container.querySelector('[data-home-feed]')).toBeNull();
    expect(view.container.querySelector('[role="list"][aria-label="Dashboard columns"]')).not.toBeNull();
    unmount(view.root, view.container, view.queryClient);
    restore();
  });

  it('switches between the chronological and relevance feeds with large tabs and keeps the feed frame on failure', async () => {
    const feedOrigin = await serve(() => ({ status: 503, body: { error: { code: 'DEPENDENCY_UNAVAILABLE', message: 'discovery runtime is not configured' } } }));
    const { root, container, queryClient } = mount(feedOrigin.origin, <HomeFeed />);
    const tablist = container.querySelector<HTMLElement>('[role="tablist"][aria-label="Callout feed"]')!;
    const tabs = Array.from(tablist.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
    expect(tabs.map(tab => tab.textContent)).toEqual(homeFeedTabs.map(tab => tab.label));
    expect(tabs.map(tab => tab.textContent)).toEqual(['Callouts', 'Top']);
    expect(tabs.map(tab => tab.getAttribute('aria-selected'))).toEqual(['true', 'false']);
    for (const tab of tabs) expect(tab.className.split(/\s+/)).toEqual(expect.arrayContaining(['text-lg', 'font-bold']));
    const panel = container.querySelector<HTMLElement>('[role="tabpanel"]')!;
    expect(panel.getAttribute('aria-labelledby')).toBe(tabs[0].id);
    await waitFor(() => feedOrigin.paths.some(path => path.startsWith('/v2/discovery/feed') && path.includes('order=chronological')));
    act(() => { tabs[1].click(); });
    expect(tabs.map(tab => tab.getAttribute('aria-selected'))).toEqual(['false', 'true']);
    expect(panel.getAttribute('aria-labelledby')).toBe(tabs[1].id);
    await waitFor(() => feedOrigin.paths.some(path => path.startsWith('/v2/discovery/feed') && path.includes('order=relevance')));
    // Arrow keys move between tabs.
    act(() => { tabs[1].dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })); });
    expect(tabs[0].getAttribute('aria-selected')).toBe('true');
    // The header and live toggle stay when the feed is unavailable; the body says so instead of collapsing the page.
    await waitFor(() => !/Loading callouts/.test(container.textContent ?? ''), 8000);
    expect(container.querySelector('[data-home-feed] [role="tablist"]')).not.toBeNull();
    expect(container.querySelector('button[aria-pressed]')?.textContent).toBe('Live');
    expect(classesOf(container).filter(name => BORDER.test(name))).toEqual([]);
    expect(container.innerHTML).not.toMatch(/[\u{1F300}-\u{1FAFF}]/u);
    unmount(root, container, queryClient);
    await new Promise<void>(resolve => feedOrigin.server.close(() => resolve()));
  }, 15000);
});
