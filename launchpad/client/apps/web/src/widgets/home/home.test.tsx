// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { defineChain } from 'viem';
import { addColumn, changeColumnType, defaultColumnTypes, defaultLayout, hydrateLayout, isLegacyDefaultLayout, layoutStorageKey, legacyDefaultLayout, moveColumn, parseLayout, parseSavedLayout, readStoredLayout, removeColumn, resetLayoutStore, serializeLayout, setLayout, updateColumn, useDashboardLayout, widgetTypes, type ColumnConfig } from './layout-store';
import { widgetOrder, widgetRegistry } from './widget-registry';
import { bundleLevel, devLevel, formatHolders, formatRiskPercent, holdersLevel, RiskRow, riskMetrics, sniperLevel, top10Level } from './risk-row';
import { changeWash, safeSocials, TrendingRow, type TrendingRowData } from './trending-row';
import { formatSpotPrice, statsChanges } from '@/core/market/pool-stats-display';
import { changeFor, matchesFilter, servedTimeframes, timeframes } from './trending-widget';
import { feesUnavailableReason, screenerStats } from './screener-widget';
import { leaderboardMetric } from './leaderboard-widget';
import { feedOrderFor } from './callouts-widget';
import { ColumnChrome } from './column-chrome';
import { DashboardBoard } from './dashboard-board';

declare global { var IS_REACT_ACT_ENVIRONMENT: boolean | undefined; }
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });

function render(node: React.ReactElement): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(<WagmiProvider config={wagmiConfig}><QueryClientProvider client={client}>{node}</QueryClientProvider></WagmiProvider>);
}

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement } {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => { root.render(node); });
  return { root, container };
}

const column: ColumnConfig = { id: 'trending-1', type: 'trending', filter: '', density: 'comfortable', view: 'list' };

describe('layout store', () => {
  beforeEach(() => { window.localStorage.clear(); resetLayoutStore(); });

  it('defaults to pump.fun\'s three board columns in order, with no watchlist column', () => {
    expect(defaultLayout().columns.map(c => c.type)).toEqual(['trending', 'hot-callouts', 'leaderboard']);
    expect(defaultLayout().columns.map(c => c.id)).toEqual(['trending-1', 'hot-callouts-2', 'leaderboard-3']);
    expect(defaultColumnTypes).not.toContain('watchlist');
    expect(parseLayout(null)).toEqual(defaultLayout());
    expect(parseLayout('not json')).toEqual(defaultLayout());
    expect(parseLayout(JSON.stringify({ version: 2, columns: [] }))).toEqual(defaultLayout());
  });

  it('drops malformed and duplicate columns while keeping valid ones', () => {
    const parsed = parseLayout(JSON.stringify({ version: 1, columns: [{ id: 'a', type: 'screener', density: 'compact', view: 'cards', filter: 'pepe' }, { id: 'a', type: 'trending' }, { id: 'b', type: 'nope' }, 'junk', { id: 'c', type: 'callouts' }] }));
    expect(parsed.columns).toEqual([
      { id: 'a', type: 'screener', density: 'compact', view: 'cards', filter: 'pepe' },
      { id: 'c', type: 'callouts', density: 'comfortable', view: 'list', filter: '' },
    ]);
  });

  it('moves, removes, adds and retypes columns immutably', () => {
    const base = defaultLayout();
    expect(moveColumn(base, 'leaderboard-3', 0).columns.map(c => c.id)[0]).toBe('leaderboard-3');
    expect(moveColumn(base, 'trending-1', 99).columns.map(c => c.id).at(-1)).toBe('trending-1');
    expect(moveColumn(base, 'missing', 0)).toBe(base);
    expect(removeColumn(base, 'hot-callouts-2').columns).toHaveLength(2);
    expect(removeColumn(base, 'missing')).toBe(base);
    const added = addColumn(base, 'trending');
    expect(added.columns).toHaveLength(4);
    expect(added.columns.at(-1)).toMatchObject({ type: 'trending', id: 'trending-4' });
    // A column header adds to its right: inserted at the given index, clamped to the board.
    expect(addColumn(base, 'watchlist', 1).columns.map(c => c.id)).toEqual(['trending-1', 'watchlist-4', 'hot-callouts-2', 'leaderboard-3']);
    expect(addColumn(base, 'screener', 99).columns.map(c => c.id).at(-1)).toBe('screener-4');
    expect(addColumn(base, 'screener', -3).columns.map(c => c.id)[0]).toBe('screener-4');
    const retyped = changeColumnType(updateColumn(base, 'leaderboard-3', { filter: 'dog' }), 'leaderboard-3', 'watchlist');
    expect(retyped.columns[2]).toEqual({ id: 'leaderboard-3', type: 'watchlist', filter: '', density: 'comfortable', view: 'list' });
    expect(updateColumn(base, 'leaderboard-3', { density: 'comfortable' })).toBe(base);
    let full = base;
    for (let i = 0; i < 10; i++) full = addColumn(full, 'callouts');
    expect(full.columns).toHaveLength(8);
    expect(addColumn(full, 'trending', 0)).toBe(full);
  });

  it('persists to localStorage and reads back after a reload', () => {
    const next = removeColumn(moveColumn(addColumn(defaultLayout(), 'callouts'), 'leaderboard-3', 0), 'hot-callouts-2');
    setLayout(next);
    expect(window.localStorage.getItem(layoutStorageKey)).toBe(serializeLayout(next));
    expect(readStoredLayout()).toEqual(next);
    expect(next.columns.map(c => c.type)).toEqual(['leaderboard', 'trending', 'callouts']);
    expect(widgetTypes).toHaveLength(6);
  });

  it('migrates the untouched first-release default board to Trending / Hot callouts / Leaderboard', () => {
    const legacy = legacyDefaultLayout();
    expect(legacy.columns.map(c => c.id)).toEqual(['trending-1', 'screener-2', 'watchlist-3', 'leaderboard-4', 'callouts-5']);
    expect(isLegacyDefaultLayout(legacy)).toBe(true);
    expect(parseSavedLayout(serializeLayout(legacy))).toEqual(legacy);
    expect(parseLayout(serializeLayout(legacy))).toEqual(defaultLayout());
    // Written back once on hydrate, so storage holds the board the user sees.
    window.localStorage.setItem(layoutStorageKey, serializeLayout(legacy));
    hydrateLayout();
    expect(window.localStorage.getItem(layoutStorageKey)).toBe(serializeLayout(defaultLayout()));
  });

  it('keeps every layout the user edited, even when it starts from the old default', () => {
    const legacy = legacyDefaultLayout();
    const edited = [
      updateColumn(legacy, 'trending-1', { density: 'compact' }),
      updateColumn(legacy, 'watchlist-3', { filter: 'pepe' }),
      moveColumn(legacy, 'leaderboard-4', 0),
      removeColumn(legacy, 'callouts-5'),
      addColumn(legacy, 'hot-callouts'),
    ];
    for (const layout of edited) {
      expect(isLegacyDefaultLayout(layout)).toBe(false);
      expect(parseLayout(serializeLayout(layout))).toEqual(layout);
    }
    const saved = serializeLayout(edited[0]);
    window.localStorage.setItem(layoutStorageKey, saved);
    hydrateLayout();
    expect(window.localStorage.getItem(layoutStorageKey)).toBe(saved);
  });
});

describe('layout store hydration', () => {
  beforeEach(() => { window.localStorage.clear(); resetLayoutStore(); });

  it('renders the migrated default board for a stored legacy default', () => {
    window.localStorage.setItem(layoutStorageKey, serializeLayout(legacyDefaultLayout()));
    let types: string[] = [];
    function Probe() { types = useDashboardLayout().columns.map(c => c.type); return null; }
    const { root, container } = mount(<Probe />);
    expect(types).toEqual(['trending', 'hot-callouts', 'leaderboard']);
    act(() => root.unmount());
    container.remove();
  });
});

describe('widget registry', () => {
  it('registers every widget type with a title, icon and component', () => {
    for (const type of widgetTypes) {
      expect(widgetRegistry[type].type).toBe(type);
      expect(widgetRegistry[type].title.length).toBeGreaterThan(0);
      expect(typeof widgetRegistry[type].Component).toBe('function');
    }
    expect(widgetOrder).toEqual([...widgetTypes]);
    expect(widgetRegistry['hot-callouts'].title).toBe('Hot callouts');
    expect(widgetRegistry.callouts.title).toBe('Latest callouts');
    // Menus list the default board first.
    expect(widgetOrder.slice(0, 3)).toEqual([...defaultColumnTypes]);
  });
});

describe('risk thresholds', () => {
  it('colours each metric by the documented thresholds', () => {
    expect(holdersLevel(49)).toBe('warn'); expect(holdersLevel(50)).toBe('ok'); expect(holdersLevel(null)).toBe('unknown');
    expect(top10Level(11.9)).toBe('ok'); expect(top10Level(12)).toBe('warn'); expect(top10Level(20)).toBe('warn'); expect(top10Level(20.1)).toBe('bad');
    expect(devLevel(4.9)).toBe('ok'); expect(devLevel(5)).toBe('warn'); expect(devLevel(10)).toBe('warn'); expect(devLevel(10.1)).toBe('bad');
    expect(sniperLevel(25)).toBe('ok'); expect(sniperLevel(25.1)).toBe('bad'); expect(sniperLevel(null)).toBe('unknown');
    expect(bundleLevel(15)).toBe('ok'); expect(bundleLevel(15.1)).toBe('bad'); expect(bundleLevel(null)).toBe('unknown');
    expect(formatRiskPercent(7.25)).toBe('7.3%'); expect(formatRiskPercent(23.4)).toBe('23%'); expect(formatRiskPercent(null)).toBe('—');
    expect(formatHolders(999)).toBe('999'); expect(formatHolders(1500)).toBe('1.5K'); expect(formatHolders(12000)).toBe('12K'); expect(formatHolders(2_000_000)).toBe('2M');
  });

  it('renders the row with hidden labels and dashes for unserved metrics', () => {
    const html = render(<RiskRow holders={30} sniperPct={null} bundlePct={null} devPct={12} top10Pct={15} />);
    expect(html).toContain('aria-label="Risk indicators"');
    expect(html).toContain('Holders 30, warning');
    expect(html).toContain('Sniper share not tracked yet');
    expect(html).toContain('Creator holding 12%, high risk');
    expect(html).toContain('Top 10 holders 15%, warning');
    expect(html).toContain('text-risk-bad');
    expect(html).toContain('text-risk-unknown');
    expect(html).not.toMatch(BORDER);
    expect(riskMetrics({ holders: null, sniperPct: null, bundlePct: null, devPct: null, top10Pct: null }).every(m => m.level === 'unknown')).toBe(true);
  });
});

describe('trending row', () => {
  it('formats spot prices with adaptive significant digits, never $0.00 for a sub-cent coin', () => {
    expect(formatSpotPrice('0.0001234567')).toBe('$0.0001234');
    expect(formatSpotPrice('0.000010098226981692')).toBe('$0.0₄1009');
    expect(formatSpotPrice('0.000000001')).toBe('$0.0₈1');
    expect(formatSpotPrice('0.1')).toBe('$0.1000');
    expect(formatSpotPrice('1234.5')).toBe('$1234.50');
    expect(formatSpotPrice(null)).toBe('—');
  });

  it('keeps only allowlisted socials and washes rows by direction', () => {
    expect(safeSocials({ twitter: 'https://x.com/sidiora', telegram: 'https://t.me/sidiora', website: 'https://example.com' })).toEqual([{ kind: 'twitter', url: 'https://x.com/sidiora' }, { kind: 'telegram', url: 'https://t.me/sidiora' }]);
    expect(safeSocials(null)).toEqual([]);
    expect(changeWash(5)).toBe('bg-[var(--sl-tint-accent-row-wash)]');
    expect(changeWash(-5)).toBe('bg-[var(--sl-tint-danger-row-wash)]');
    expect(changeWash(null)).toBe('');
  });

  it('renders image, name, price, socials, market cap and change without borders', () => {
    const row: TrendingRowData = { poolAddress: '0x' + '1'.repeat(40), href: '/token/0x' + '1'.repeat(40), rank: 1, name: 'Pepe Pax', symbol: 'PPX', image: null, price: '0.0004567', priceReason: null, mcapRaw: '29700000000', mcapReason: null, change: 12.5, changeReason: null, socials: [{ kind: 'twitter', url: 'https://x.com/pepe' }], stage: 'curve' };
    const html = render(<ol><TrendingRow row={row} starred={false} onToggleStar={() => undefined} /></ol>);
    expect(html).toContain('Pepe Pax');
    expect(html).toContain('$0.0004567');
    expect(html).toContain('29.7K MC');
    expect(html).toContain('+12.5%');
    expect(html).toContain('https://x.com/pepe');
    expect(html).toContain('bg-[var(--sl-tint-accent-row-wash)]');
    expect(html).toContain('Add Pepe Pax to watchlist');
    expect(html).toContain('h-list-row-lg');
    expect(html).not.toMatch(BORDER);
    const compact = render(<ol><TrendingRow row={{ ...row, change: null, changeReason: 'Not enough trading history for a 1-hour change yet.', price: null, priceReason: 'No trade yet, so there is no price.' }} density="compact" /></ol>);
    expect(compact).toContain('Change: Not enough trading history for a 1-hour change yet.');
    expect(compact).toContain('Price: No trade yet, so there is no price.');
    expect(compact).not.toContain('$0.00');
    expect(compact).toContain('h-list-row');
  });
});

describe('widget helpers', () => {
  it('serves only 1h and 24h change and filters by name, symbol or address', () => {
    expect(timeframes).toEqual(['1h', '4h', '12h', '24h']);
    expect([...servedTimeframes]).toEqual(['1h', '24h']);
    const changes = { ...statsChanges(null), '1h': { value: 1, reason: null }, '24h': { value: 2, reason: null } };
    const row = { changes, name: 'Doge Pax', symbol: 'DPX', poolAddress: '0xabc', tokenAddress: '0xdef' };
    expect(changeFor(row, '1h').value).toBe(1); expect(changeFor(row, '24h').value).toBe(2);
    expect(changeFor(row, '4h')).toEqual({ value: null, reason: '4h change coming soon, pick 1h or 24h' }); expect(changeFor(row, '12h').value).toBeNull();
    expect(matchesFilter(row, ' dpx ')).toBe(true); expect(matchesFilter(row, '0xDEF')).toBe(true); expect(matchesFilter(row, 'cat')).toBe(false); expect(matchesFilter(row, '')).toBe(true);
  });

  it('marks fees unavailable on screener cards and signs leaderboard metrics', () => {
    const stats = screenerStats({ poolAddress: '0x1', tokenAddress: null, rank: 1, name: 'A', symbol: 'A', image: null, href: '/token/0x1', mcapRaw: null, mcap: null, mcapReason: null, circulatingMcapRaw: null, volume24hRaw: '5000000', volume24h: 5, createdAtMs: null, ageMs: null, txns: 1234, traders: null, holders: null, price: null, priceReason: null, change5m: null, change1h: null, change6h: null, change24h: null, changes: statsChanges(null), top10Share: null, devShare: null, sharesReason: null, stage: null });
    expect(stats.map(s => s.label)).toEqual(['V', 'TX', 'F']);
    expect(stats[0].value).toBe('5.00');
    expect(stats[1].value).toBe('1.2K');
    expect(renderToStaticMarkup(<>{stats[2].value}</>)).toContain(feesUnavailableReason);
    expect(leaderboardMetric({ metricRaw: '1500000000', quality: 'complete' })).toEqual({ text: '1.5K', tone: 'neutral' });
    expect(leaderboardMetric({ metricRaw: '1500000000', metricSigned: '-1500000000', quality: 'complete' })).toEqual({ text: '-1.5K', tone: 'danger' });
    expect(leaderboardMetric({ metricRaw: '1500000000', metricSigned: '1500000000', quality: 'complete' })).toEqual({ text: '+1.5K', tone: 'success' });
    expect(leaderboardMetric({ metricRaw: '0', quality: 'unavailable' })).toEqual({ text: '—', tone: 'neutral' });
    expect(feedOrderFor('latest')).toBe('chronological'); expect(feedOrderFor('hot')).toBe('relevance');
  });
});

describe('column chrome', () => {
  afterEach(() => { document.body.innerHTML = ''; });

  it('moves with the arrow keys, confirms removal inline and reveals the filter input', () => {
    const calls: string[] = [];
    const view = mount(
      <ColumnChrome column={{ ...column, type: 'screener' }} index={1} count={3} onMove={to => calls.push(`move:${to}`)} onRemove={() => calls.push('remove')} onChangeType={type => calls.push(`type:${type}`)} onPatch={patch => calls.push(`patch:${JSON.stringify(patch)}`)} onDragStart={() => undefined} onDragEnd={() => undefined} />,
    );
    const grip = view.container.querySelector<HTMLButtonElement>('button[aria-label="Reorder column"]');
    expect(grip?.getAttribute('draggable')).toBe('true');
    act(() => { grip?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })); });
    act(() => { grip?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })); });
    expect(calls).toEqual(['move:0', 'move:2']);
    expect(view.container.querySelector('h2')?.textContent).toContain('Screener');
    act(() => { view.container.querySelector<HTMLButtonElement>('button[aria-label="Remove column"]')?.click(); });
    const dialog = view.container.querySelector('[role="alertdialog"]');
    expect(dialog?.textContent).toContain('Remove column?');
    act(() => { Array.from(dialog?.querySelectorAll('button') ?? []).find(b => b.textContent === 'Remove')?.click(); });
    expect(calls).toContain('remove');
    act(() => { view.container.querySelector<HTMLButtonElement>('button[aria-label="Show as cards"]')?.click(); });
    expect(calls).toContain('patch:{"view":"cards"}');
    act(() => { view.container.querySelector<HTMLButtonElement>('button[aria-label="Filter"]')?.click(); });
    expect(view.container.querySelector('input[type="search"]')).not.toBeNull();
    expect(view.container.innerHTML).not.toMatch(BORDER);
    act(() => view.root.unmount());
  });

  it('adds a column of the chosen type from the header, and has no add control without a handler', async () => {
    const added: string[] = [];
    const view = mount(<ColumnChrome column={column} index={0} count={3} onMove={() => undefined} onRemove={() => undefined} onChangeType={() => undefined} onPatch={() => undefined} onDragStart={() => undefined} onDragEnd={() => undefined} onAdd={type => added.push(type)} />);
    const trigger = view.container.querySelector<HTMLButtonElement>('button[aria-label="Add column"]');
    expect(trigger).not.toBeNull();
    act(() => { trigger?.click(); });
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
    const items = Array.from(document.querySelectorAll<HTMLElement>('[role="menu"][aria-label="Add column"] [role="menuitem"]'));
    expect(items.map(item => item.textContent)).toEqual(widgetOrder.map(type => widgetRegistry[type].title));
    act(() => { items.find(item => item.textContent === 'Latest callouts')?.click(); });
    expect(added).toEqual(['callouts']);
    act(() => view.root.unmount());
    document.body.innerHTML = '';
    const bare = mount(<ColumnChrome column={column} index={0} count={3} onMove={() => undefined} onRemove={() => undefined} onChangeType={() => undefined} onPatch={() => undefined} onDragStart={() => undefined} onDragEnd={() => undefined} />);
    expect(bare.container.querySelector('button[aria-label="Add column"]')).toBeNull();
    act(() => bare.root.unmount());
  });
});

describe('dashboard board', () => {
  beforeEach(() => { window.localStorage.clear(); resetLayoutStore(); });

  it('renders the three default columns with chrome and bodies, filling the board with no add-column slot', () => {
    const html = render(<DashboardBoard />);
    for (const title of ['Trending', 'Hot callouts', 'Leaderboard']) expect(html).toContain(`<section aria-label="${title}"`);
    for (const title of ['Watchlist', 'Screener', 'Latest callouts']) expect(html).not.toContain(`<section aria-label="${title}"`);
    expect(html).not.toContain('Your watchlist is empty');
    expect(html.match(/aria-label="Reorder column"/g)).toHaveLength(3);
    expect(html.match(/aria-label="Remove column"/g)).toHaveLength(3);
    // Adding is a header control on each column, not a ghost column that takes a slot.
    expect(html.match(/aria-label="Add column"/g)).toHaveLength(3);
    expect(html).not.toContain('Choose a widget to add it as a new column');
    // Slots divide the board by min(columns that fit, columns on the board), so three columns fill it.
    expect(html.match(/min-w-\[288px\]" style="width:calc\(\(100% - \(min\(var\(--board-visible\), var\(--board-count\)\) - 1\) \* var\(--sl-dashboard-column-gap\)\) \/ min\(var\(--board-visible\), var\(--board-count\)\)\)"/g)).toHaveLength(3);
    expect(html).toContain('--board-count:3');
    expect(html).toContain('lg:[--board-visible:3]');
    expect(html).toContain('min-[1800px]:[--board-visible:4]');
    // Flush columns: no board padding, square edges, a 2 px canvas seam (background contrast, not a stroke).
    expect(html).toContain('[--sl-dashboard-column-gap:2px]');
    expect(html).toContain('height:calc(100dvh - var(--sl-shell-top-total))');
    const sections = html.match(/<section [^>]*class="[^"]*"/g) ?? [];
    expect(sections).toHaveLength(3);
    for (const section of sections) expect(section).not.toMatch(/\brounded(-[a-z0-9]+)?\b/);
    // Callouts columns spend no tab row on the Live pill: on the server it is not rendered at all (no shift on hydration).
    const hot = html.slice(html.indexOf('<section aria-label="Hot callouts"'), html.indexOf('<section aria-label="Leaderboard"'));
    expect(hot).toContain('data-column-actions=""');
    expect(hot).not.toMatch(/>(Live|Paused)</);
    expect(html).toContain('gap-dashboard-column-gap');
    expect(html).toContain('bg-surface-1');
    expect(html).toContain('role="radiogroup"');
    expect(html).toContain('>24h<');
    expect(html).toContain('Loading coins');
    expect(html).toContain('Loading callouts');
    expect(html).toContain('aria-live="polite"');
    expect(html).not.toMatch(BORDER);
    expect(html).not.toMatch(/[\u{1F300}-\u{1FAFF}]/u);
  });
});
