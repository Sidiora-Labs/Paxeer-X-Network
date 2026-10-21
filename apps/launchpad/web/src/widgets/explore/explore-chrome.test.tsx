import React, { act } from 'react';
import { createRequire } from 'node:module';
import type { Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it } from 'vitest';

// Node environment with a real jsdom window installed (same approach as widgets/terminal/views/views.test.tsx).
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

// jsdom has no layout engine, so the viewport width behind matchMedia is set per test.
let viewportWidth = 1440;
function installMatchMedia() {
  (dom.window as unknown as { matchMedia: (query: string) => MediaQueryList }).matchMedia = (query: string) => {
    const min = /min-width:\s*(\d+)px/.exec(query);
    return {
      matches: min ? viewportWidth >= Number(min[1]) : false,
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    } as MediaQueryList;
  };
}
installMatchMedia();

const { createRoot } = await import('react-dom/client');
const { PresetTabs, overflowPresets, presetTier, presets } = await import('./preset-tabs');
const { ExploreToolbar } = await import('./explore-toolbar');
const { ExploreTable } = await import('./explore-table');
const { columnSpecs, defaultSettings } = await import('./column-settings');
const { ExploreView } = await import('./explore-view');
const { StateNotice, UnavailableState } = await import('@/ui/primitives');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { paxeer } = await import('@/core/wagmi-config');

// Signed-out wallet context: a real wagmi config with no connectors and an unreachable RPC, so the watchlist stays local.
const signedOut = createConfig({ chains: [paxeer], connectors: [], transports: { [paxeer.id]: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, multiInjectedProviderDiscovery: false });
function Providers({ children }: { children: React.ReactNode }) {
  const [client] = React.useState(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }));
  return <WagmiProvider config={signedOut}><QueryClientProvider client={client}>{children}</QueryClientProvider></WagmiProvider>;
}

let root: Root | null = null;
let host: HTMLDivElement | null = null;

async function mount(node: React.ReactNode) {
  host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
  await act(async () => { root!.render(node); });
  return host;
}

afterEach(async () => {
  if (root) await act(async () => { root!.unmount(); });
  host?.remove();
  root = null;
  host = null;
  document.body.innerHTML = '';
  viewportWidth = 1440;
});

describe('preset tiers', () => {
  it('keeps the first preset inline, two more from md and the rest from xl', () => {
    expect(presets.map((_, index) => presetTier(index))).toEqual(['always', 'md', 'md', 'xl', 'xl', 'xl', 'xl']);
    expect(overflowPresets('wide')).toEqual([]);
    expect(overflowPresets('medium').map(preset => preset.id)).toEqual(['volume', 'breakout', 'unusual', 'mcap']);
    expect(overflowPresets('narrow').map(preset => preset.id)).toEqual(['new', 'trending', 'volume', 'breakout', 'unusual', 'mcap']);
  });
});

describe('PresetTabs', () => {
  it('renders one segmented container whose active segment is the only accent fill', () => {
    const html = renderToStaticMarkup(<PresetTabs active="movers" />);
    const doc = new dom.window.DOMParser().parseFromString(html, 'text/html');
    const nav = doc.querySelector('nav[aria-label="Explore presets"]')!;
    expect(nav.children).toHaveLength(1);
    expect(nav.firstElementChild!.className).toContain('bg-surface-1');
    const links = [...nav.querySelectorAll('a')];
    expect(links.map(link => link.textContent)).toEqual(presets.map(preset => preset.label));
    expect(links.filter(link => link.className.includes('bg-accent')).map(link => link.textContent)).toEqual(['Movers']);
    expect(links[0].getAttribute('aria-current')).toBe('page');
    expect(links[0].getAttribute('href')).toBe('/explore');
    expect(links[3].getAttribute('href')).toBe('/explore?preset=volume');
    // No border or ring separates the container; only the surface does.
    expect(html).not.toMatch(/\bborder\b|\bring-/);
  });

  it('hides overflow presets by breakpoint and keeps a More trigger below xl', () => {
    const html = renderToStaticMarkup(<PresetTabs active="movers" />);
    const doc = new dom.window.DOMParser().parseFromString(html, 'text/html');
    const items = [...doc.querySelectorAll('nav li')];
    expect(items[0].className).not.toContain('hidden');
    expect(items[1].className).toContain('hidden md:flex');
    expect(items[2].className).toContain('hidden md:flex');
    expect(items[3].className).toContain('hidden xl:flex');
    const more = doc.querySelector('button[aria-label="More presets"]')!;
    expect(more.textContent).toBe('More');
    expect(more.parentElement!.className).toContain('xl:hidden');
  });

  it('carries a folded active preset on the More trigger', () => {
    const html = renderToStaticMarkup(<PresetTabs active="volume" />);
    const doc = new dom.window.DOMParser().parseFromString(html, 'text/html');
    const more = doc.querySelector('button[aria-label="More presets"]')!;
    expect(more.textContent).toContain('Volume');
    expect(more.className).toContain('bg-accent');
    expect(more.className).toContain('xl:bg-transparent');
  });

  it('lists only the presets hidden at the current width in the More menu', async () => {
    viewportWidth = 390;
    const node = await mount(<PresetTabs active="movers" />);
    const trigger = node.querySelector<HTMLButtonElement>('button[aria-label="More presets"]')!;
    await act(async () => { trigger.click(); });
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
    const menu = document.querySelector('[role="menu"]')!;
    expect(menu).not.toBeNull();
    const entries = [...menu.querySelectorAll('[role="menuitemradio"]')];
    expect(entries.map(entry => entry.textContent)).toEqual(['New', 'Trending', 'Volume', 'Breakout', 'Unusual', 'Market cap']);
    expect(entries[2].getAttribute('href')).toBe('/explore?preset=volume');
  });

  it('marks the active preset inside the More menu at md', async () => {
    viewportWidth = 1024;
    const node = await mount(<PresetTabs active="unusual" />);
    const trigger = node.querySelector<HTMLButtonElement>('button[aria-label="More presets"]')!;
    await act(async () => { trigger.click(); });
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
    const entries = [...document.querySelectorAll('[role="menu"] [role="menuitemradio"]')];
    expect(entries.map(entry => entry.textContent)).toEqual(['Volume', 'Breakout', 'Unusual', 'Market cap']);
    expect(entries.map(entry => entry.getAttribute('aria-checked'))).toEqual(['false', 'false', 'true', 'false']);
  });
});

describe('ExploreToolbar', () => {
  function toolbar(filterCount: number, onToggle: () => void = () => undefined, onView: (view: 'grid' | 'table') => void = () => undefined) {
    return (
      <ExploreToolbar
        leading={<span data-testid="leading">presets</span>}
        filterOpen={false}
        filterCount={filterCount}
        filterPanelId="filters"
        onToggleFilter={onToggle}
        view="table"
        onViewChange={onView}
        settingsOpen={false}
        settingsPanelId="settings"
        onToggleSettings={() => undefined}
      />
    );
  }

  it('is a single non-wrapping row: presets, Filter icon, Grid/Table with text labels, settings', async () => {
    let toggled = 0;
    let picked: string | null = null;
    const node = await mount(toolbar(0, () => { toggled++; }, view => { picked = view; }));
    const row = node.firstElementChild!;
    expect(row.className).not.toContain('flex-wrap');
    expect(row.firstElementChild!.querySelector('[data-testid="leading"]')).not.toBeNull();
    const filter = node.querySelector<HTMLButtonElement>('button[aria-label="Filter"]')!;
    expect(filter.textContent).toBe('');
    await act(async () => { filter.click(); });
    expect(toggled).toBe(1);
    const layout = node.querySelector('[role="radiogroup"][aria-label="Layout"]')!;
    expect([...layout.querySelectorAll('[role="radio"]')].map(radio => radio.textContent)).toEqual(['Grid', 'Table']);
    expect(layout.className).toContain('md:inline-flex');
    await act(async () => { layout.querySelector<HTMLButtonElement>('[aria-label="Grid"]')!.click(); });
    expect(picked).toBe('grid');
    expect(node.querySelector('button[aria-label="Column settings"]')).not.toBeNull();
  });

  it('announces the active filter count on the icon button', async () => {
    const node = await mount(toolbar(2));
    const filter = node.querySelector<HTMLButtonElement>('button[aria-label="Filter, 2 active"]')!;
    expect(filter.getAttribute('aria-pressed')).toBe('true');
    expect(filter.textContent).toBe('2');
  });
});

describe('ExploreTable columns', () => {
  function table() {
    return (
      <ExploreTable
        rows={[]}
        sort={null}
        onSort={() => undefined}
        columns={defaultSettings.columns}
        density="default"
        isStarred={() => false}
        onToggleStar={() => undefined}
        loading
        hasMore={false}
        loadingMore={false}
        moreError={false}
        onLoadMore={() => undefined}
        empty={null}
        disclosure={null}
        caption="Movers coins"
      />
    );
  }

  it('has no separate rank column and never hides a column by breakpoint', () => {
    expect(columnSpecs.map(spec => spec.id)).not.toContain('rank');
    expect(columnSpecs.every(spec => spec.responsive === '')).toBe(true);
  });

  it('keeps every column at narrow widths and scrolls inside its card', async () => {
    viewportWidth = 390;
    const node = await mount(table());
    const headers = [...node.querySelectorAll('thead th')];
    expect(headers.map(th => th.className.includes('hidden'))).toEqual(headers.map(() => false));
    expect(headers[0].textContent).toBe('Coin');
    expect(headers.map(th => th.textContent ?? '')).toEqual(expect.arrayContaining([expect.stringContaining('Graph'), expect.stringContaining('ATH'), expect.stringContaining('TXNS'), expect.stringContaining('Traders')]));
    // Reference header weight: uppercase medium with sort carets.
    expect(headers.every(th => th.className.includes('font-medium') && !th.className.includes('font-bold') && !th.className.includes('font-semibold'))).toBe(true);
    const scroller = node.querySelector('table')!.parentElement!;
    expect(scroller.className).toContain('overflow-x-auto');
    expect(scroller.className).toContain('relative');
    expect(scroller.className).toContain('[contain:inline-size]');
    expect(node.querySelectorAll('tbody tr')).toHaveLength(8);
  });
});

describe('Explore frame when the ranking fails', () => {
  const failure = new TypeError('Failed to fetch');
  function view(empty: React.ReactNode) {
    return (
      <Providers>
      <ExploreView
        rows={[]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        moreError={null}
        onLoadMore={() => undefined}
        onRetryMore={() => undefined}
        defaultSort={null}
        sortResetKey="movers"
        sourceLabel="the movers ranking snapshot"
        leading={<PresetTabs active="movers" />}
        caption="Movers coins"
        empty={empty}
      />
      </Providers>
    );
  }

  for (const width of [1920, 1024, 768, 390]) {
    it(`keeps presets, toolbar, table headers and an in-card notice at ${width}px`, async () => {
      viewportWidth = width;
      const node = await mount(view(
        <div className="flex min-h-64 items-center justify-center p-6">
          <StateNotice kind="error" surface={1} wrap error={failure} onRetry={() => undefined}>Rankings did not load. We could not reach the server.</StateNotice>
        </div>,
      ));
      expect(node.querySelector('nav[aria-label="Explore presets"]')).not.toBeNull();
      expect(node.querySelector('button[aria-label="Filter"]')).not.toBeNull();
      expect(node.querySelector('button[aria-label="Column settings"]')).not.toBeNull();
      const headers = [...node.querySelectorAll('thead th')].map(th => th.textContent ?? '');
      expect(headers[0]).toBe('Coin');
      expect(headers).toEqual(expect.arrayContaining([expect.stringContaining('Graph'), expect.stringContaining('MCAP'), expect.stringContaining('24H VOL')]));
      expect(node.querySelectorAll('tbody tr')).toHaveLength(0);
      const notice = node.querySelector('[role="alert"][data-state="error"]')!;
      expect(notice).not.toBeNull();
      // The notice sits in the card but outside the horizontal scroller, so it stays centred in the visible width.
      expect(notice.closest('.overflow-x-auto')).toBeNull();
      expect(notice.closest('.bg-surface-1')).not.toBeNull();
      expect(notice.className).toContain('bg-surface-2');
      expect(node.textContent).not.toContain('Failed to fetch');
      // The coin header keeps the coin cell width when no rows render.
      expect(node.querySelector('thead th')!.className).toContain('min-w-42');
    });
  }

  it('words an unserved ranking as a plural "not available yet" chip inside the frame', async () => {
    const node = await mount(view(
      <UnavailableState variant="chip" plural="auto" surface={1} wrap title="Coin rankings" operation="IF_RANKINGS" status={404} onRetry={() => undefined} />,
    ));
    const chip = node.querySelector('[data-state="unavailable"]')!;
    expect(chip.textContent).toContain('Coin rankings are not available yet');
    expect(chip.closest('table')).toBeNull();
    expect(node.querySelectorAll('thead th').length).toBe(columnSpecs.length);
  });

  it('uses no border, ring, divide or shadow utilities in the frame', async () => {
    const node = await mount(view(<StateNotice kind="error" error={failure} />));
    const classes = [...node.querySelectorAll('[class]')].map(el => el.getAttribute('class') ?? '').join(' ');
    expect(classes).not.toMatch(/(^|\s)(border|ring|divide|shadow)(-|\s|$)/);
  });
});
