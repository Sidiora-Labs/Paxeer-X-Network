// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRoot, hydrateRoot, type Root } from 'react-dom/client';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { mock } from 'wagmi/connectors';
import { defineChain } from 'viem';
import { PathnameContext } from 'next/dist/shared/lib/hooks-client-context.shared-runtime';
import { AppRouterContext, type AppRouterInstance } from 'next/dist/shared/lib/app-router-context.shared-runtime';

// vitest.config.ts sets deps.interopDefault: false, so CJS next/image arrives as its whole exports
// object; re-expose the real component as the default (pattern from src/ui/shared/token-image.test.tsx).
vi.mock('next/image', async importOriginal => {
  const mod = await importOriginal<{ default: unknown }>();
  const cjs = mod.default as { default?: unknown } | undefined;
  return { ...mod, default: cjs && typeof cjs === 'object' && 'default' in cjs ? cjs.default : mod.default };
});

// Every read in these suites goes to a closed local port, so nothing leaves the box.
process.env.NEXT_PUBLIC_DATA_API_URL = 'http://127.0.0.1:1';
process.env.NEXT_PUBLIC_METADATA_API_URL = 'http://127.0.0.1:1';
process.env.NEXT_PUBLIC_GATEWAY_URL = 'http://127.0.0.1:1';
process.env.NEXT_PUBLIC_PNL_API_URL = 'http://127.0.0.1:1';

const { default: SidebarNav, activeNavHref } = await import('./sidebar-nav');
const { default: TopBar } = await import('./top-bar');
const { default: TickerStrip } = await import('./ticker-strip');
const { ShellFooter } = await import('./platform-metrics');
const { navItems, SHELL_TOP_VARS } = await import('./nav-config');

let wide = true;
beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  window.matchMedia = (query: string) => ({ matches: wide && /min-width/.test(query), media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false }) as MediaQueryList;
});

const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const config = createConfig({ chains: [chain], connectors: [mock({ accounts: ['0x1111111111111111111111111111111111111111'] })], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, multiInjectedProviderDiscovery: false });
const router = { back() {}, forward() {}, refresh() {}, push() {}, replace() {}, prefetch() {} } as unknown as AppRouterInstance;

const mounted: Root[] = [];
async function render(node: React.ReactNode, pathname = '/') {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  mounted.push(root);
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <WagmiProvider config={config}>
        <QueryClientProvider client={queries}>
          <AppRouterContext.Provider value={router}>
            <PathnameContext.Provider value={pathname}>{node}</PathnameContext.Provider>
          </AppRouterContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return container;
}

afterEach(() => {
  for (const root of mounted.splice(0)) act(() => root.unmount());
  document.body.innerHTML = '';
  window.localStorage.clear();
  wide = true;
});

const classes = (element: Element | null | undefined) => (element?.getAttribute('class') ?? '').split(/\s+/);
const NO_STROKES = /\bborder\b|border-[a-z]|\bring-|divide-|gradient|glow/;

describe('navigation rail', () => {
  it('shows from md (768) up, matching the bottom bar, which hides at md', async () => {
    const container = await render(<SidebarNav />);
    const rail = container.querySelector('aside[aria-label="Navigation rail"]');
    expect(classes(rail)).toEqual(expect.arrayContaining(['hidden', 'md:flex']));
    expect(classes(rail)).not.toContain('lg:flex');
  });

  it('puts the expand chevron beside the logo, in the top bar row', async () => {
    const container = await render(<SidebarNav />);
    const logo = container.querySelector('a[aria-label="Sidiora Launch home"]')!;
    const expand = container.querySelector('button[aria-label="Expand navigation"]')!;
    expect(expand.parentElement).toBe(logo.parentElement);
    expect(classes(logo.parentElement)).toEqual(expect.arrayContaining(['h-header-height', 'items-center']));
    expect(classes(logo.parentElement)).not.toContain('flex-col');
  });

  it('separates primary and secondary destinations with a spacing gap, then create', async () => {
    const container = await render(<SidebarNav />);
    const primary = container.querySelector('[data-rail-group="primary"]')!;
    const secondary = container.querySelector('[data-rail-group="secondary"]')!;
    expect(Array.from(primary.querySelectorAll('a')).map(link => link.getAttribute('aria-label'))).toEqual(['Home', 'Explore', 'Leaderboard', 'Live']);
    expect(Array.from(secondary.querySelectorAll('a')).map(link => link.getAttribute('aria-label'))).toEqual(['Bounties', 'Holder rewards', 'Terminal']);
    expect(classes(secondary)).toContain('mt-4');
    expect(container.querySelector('nav[aria-label="Primary"]')!.outerHTML).not.toMatch(NO_STROKES);
  });

  it('pins a utility cluster to the rail bottom: help, legal and the sidebar toggle', async () => {
    const container = await render(<SidebarNav />);
    const utility = container.querySelector('[data-rail-group="utility"]')!;
    const spacer = utility.previousElementSibling;
    expect(classes(spacer)).toContain('flex-1');
    const controls = Array.from(utility.querySelectorAll('a, button'));
    expect(controls.map(control => control.getAttribute('aria-label'))).toEqual(['Help and docs', 'Terms and privacy', 'Toggle sidebar']);
    expect(controls.slice(0, 2).map(control => control.getAttribute('href'))).toEqual(['/docs', '/docs/terms']);
    expect(controls[2].getAttribute('aria-expanded')).toBe('false');
  });

  it('expands from the bottom toggle and collapses again, persisting the choice', async () => {
    const container = await render(<SidebarNav />);
    const toggle = () => container.querySelector<HTMLButtonElement>('button[aria-label="Toggle sidebar"]')!;
    await act(async () => { toggle().click(); });
    expect(window.localStorage.getItem('sidiora-rail-expanded')).toBe('true');
    expect(toggle().getAttribute('aria-expanded')).toBe('true');
    expect(toggle().textContent).toBe('Collapse sidebar');
    expect(container.querySelector('button[aria-label="Expand navigation"]')).toBeNull();
    await act(async () => { toggle().click(); });
    expect(window.localStorage.getItem('sidiora-rail-expanded')).toBe('false');
  });

  it('marks only the most specific matching destination as current', async () => {
    const container = await render(<SidebarNav />, '/docs/terms');
    const current = Array.from(container.querySelectorAll('[aria-current="page"]')).map(link => link.getAttribute('aria-label'));
    expect(current).toEqual(['Terms and privacy']);
    expect(activeNavHref('/docs', navItems.map(item => item.href))).toBe('/docs');
    expect(activeNavHref('/coin/0x00', navItems.map(item => item.href))).toBeNull();
    expect(activeNavHref('/explore', navItems.map(item => item.href))).toBe('/explore');
  });
});

describe('top bar anatomy', () => {
  const labels = (container: HTMLElement) => Array.from(container.querySelectorAll('header a, header button')).map(element => ({ label: element.getAttribute('aria-label') ?? element.textContent?.trim(), cls: classes(element.closest('header > *')) }));

  it('desktop: 32px subtle search field, ghost paste icon beside it, no back button on home', async () => {
    const container = await render(<TopBar />);
    const field = container.querySelector('[data-search-field]')!;
    expect(classes(field)).toEqual(expect.arrayContaining(['hidden', 'lg:block', '[&>button]:h-8', '[&>button]:bg-surface-1']));
    expect(field.nextElementSibling?.className).toContain('lg:hidden');
    const paste = container.querySelector('button[aria-label="Open a coin from a pasted address"]')!;
    expect(paste.className).not.toMatch(/bg-surface-2|bg-surface-3/);
    expect(container.querySelector('button[aria-label="Back to home"], button[aria-label="Back"]')).toBeNull();
  });

  it('touch: logo below md, icon-only search that opens the palette, create from md only', async () => {
    const container = await render(<TopBar />);
    expect(classes(container.querySelector('a[aria-label="Sidiora Launch home"]'))).toContain('md:hidden');
    const icon = container.querySelector('[data-search-icon]')!;
    expect(classes(icon)).toContain('lg:hidden');
    const trigger = icon.querySelector('button')!;
    expect(trigger.getAttribute('aria-haspopup')).toBe('dialog');
    expect(trigger.textContent?.trim()).toBe('');
    expect(classes(container.querySelector('a[aria-label="Create coin"]'))).toEqual(expect.arrayContaining(['hidden', 'md:inline-flex']));
    expect(container.querySelector('header')!.outerHTML).not.toMatch(NO_STROKES);
    // Reference order on the right: paste, search icon, create, sign in.
    const order = labels(container).map(entry => entry.label ?? '');
    const paste = order.indexOf('Open a coin from a pasted address');
    const searchIcon = order.lastIndexOf(order.filter(label => label.startsWith('Search coins and users')).at(-1)!);
    expect(paste).toBeGreaterThan(-1);
    expect(paste).toBeLessThan(searchIcon);
    expect(searchIcon).toBeLessThan(order.indexOf('Create coin'));
  });

  it('shows back only on tablet widths, away from home', async () => {
    const container = await render(<TopBar />, '/explore');
    const back = container.querySelector('button[aria-label="Back to home"]');
    expect(back).not.toBeNull();
    expect(classes(back)).toEqual(expect.arrayContaining(['hidden', 'md:inline-flex', 'lg:hidden']));
  });
});

describe('ticker strip', () => {
  it('is hidden below md and never shows placeholder pills while its feed loads', async () => {
    const container = await render(<TickerStrip />);
    const strip = container.querySelector('section[aria-label="Market ticker"]')!;
    expect(classes(strip)).toEqual(expect.arrayContaining(['hidden', 'md:flex']));
    expect(container.querySelector('[data-ticker-loading]')?.textContent).toBe('Loading top earners');
    expect(container.querySelectorAll('.sl-skeleton')).toHaveLength(0);
    expect(container.querySelector('[role="radiogroup"]')).not.toBeNull();
  });

  it('does not mount its feed on phones, so no ticker read starts there', async () => {
    wide = false;
    const container = await render(<TickerStrip />);
    expect(container.querySelector('[data-ticker-loading]')).toBeNull();
    expect(container.querySelector('[data-ticker-empty]')).toBeNull();
    expect(container.querySelector('section[aria-label="Market ticker"] [role="radiogroup"]')).not.toBeNull();
  });
});

describe('footer row', () => {
  it('is one slim canvas row: no surface card, no skeleton bars, legal links and copyright inline', async () => {
    const container = await render(<ShellFooter />);
    const footer = container.querySelector('footer')!;
    expect(classes(footer)).toEqual(expect.arrayContaining(['bg-canvas', 'py-3', 'flex', 'items-center']));
    expect(footer.outerHTML).not.toMatch(/bg-surface-|rounded-md|sl-skeleton/);
    expect(footer.outerHTML).not.toMatch(NO_STROKES);
    expect(Array.from(footer.querySelectorAll('nav[aria-label="Legal"] a')).map(link => link.textContent)).toEqual(['Terms', 'Privacy', 'Trademarks', 'DMCA', 'Do not sell my data']);
    expect(footer.textContent).toContain(`© ${new Date().getFullYear()} Sidiora`);
    // Loading is announced to screen readers only; the row keeps its one-line height.
    const busy = footer.querySelector('[aria-busy="true"]');
    if (busy) expect(busy.className).toContain('sr-only');
    else expect(footer.querySelector('[role="status"]')?.className).toContain('text-2xs');
  });
});

describe('footer placement', () => {
  it('anchors the footer row to the viewport bottom on short pages: main is one viewport tall and the page grows into it', async () => {
    const { default: AppShell } = await import('./app-shell');
    const html = renderToString(
      <WagmiProvider config={config}>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <AppRouterContext.Provider value={router}>
            <PathnameContext.Provider value="/live"><AppShell><p>short page</p></AppShell></PathnameContext.Provider>
          </AppRouterContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
    const host = document.createElement('div');
    host.innerHTML = html;
    const main = host.querySelector('main#main-content')!;
    expect(classes(main)).toEqual(expect.arrayContaining(['min-h-dvh', 'flex', 'flex-col', 'pt-[var(--shell-top)]', 'pb-14', 'md:pb-0']));
    const content = main.querySelector('[data-shell-content]')!;
    expect(classes(content)).toContain('flex-1');
    // No fixed viewport min-height on the content: that pushed the footer below the fold.
    expect(content.getAttribute('style') ?? '').not.toMatch(/min-height/);
    expect(main.lastElementChild?.tagName).toBe('FOOTER');
    expect(main.outerHTML).not.toMatch(NO_STROKES);
  });
});

describe('shell hydration', () => {
  const tree = (pathname: string, queries: QueryClient) => (
    <WagmiProvider config={config}>
      <QueryClientProvider client={queries}>
        <AppRouterContext.Provider value={router}>
          <PathnameContext.Provider value={pathname}>
            <SidebarNav />
            <div className={SHELL_TOP_VARS}><TopBar /><TickerStrip /></div>
            <ShellFooter />
          </PathnameContext.Provider>
        </AppRouterContext.Provider>
      </QueryClientProvider>
    </WagmiProvider>
  );

  // Persisted per-device choices (rail expanded, ticker mode) and a wide viewport differ from the server
  // snapshot; every one of them must be read after hydration, never during it.
  for (const pathname of ['/', '/coin/0xce20da50f92d21d8bee497910c782e9aa25b6263', '/profile/0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154']) {
    it(`hydrates ${pathname.split('/')[1] || 'home'} server markup without attribute or text mismatches`, async () => {
      const html = renderToString(tree(pathname, new QueryClient({ defaultOptions: { queries: { retry: false } } })));
      window.localStorage.setItem('sidiora-rail-expanded', 'true');
      window.localStorage.setItem('sidiora-ticker-mode', 'recent');
      const container = document.createElement('div');
      container.innerHTML = html;
      document.body.appendChild(container);
      const mismatches: string[] = [];
      const original = console.error;
      console.error = (...args: unknown[]) => { const text = args.map(String).join(' '); if (/hydrat|did not match|didn't match/i.test(text)) mismatches.push(text.slice(0, 400)); };
      try {
        const root = await act(async () => hydrateRoot(container, tree(pathname, new QueryClient({ defaultOptions: { queries: { retry: false } } })), {
          onRecoverableError: error => { mismatches.push(String(error).slice(0, 400)); },
        }));
        mounted.push(root);
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 50)); });
      } finally {
        console.error = original;
      }
      expect(mismatches).toEqual([]);
      // The persisted choices still apply once hydrated.
      expect(container.querySelector('button[aria-label="Toggle sidebar"]')?.getAttribute('aria-expanded')).toBe('true');
      expect(container.querySelector('[role="radio"][aria-checked="true"]')?.getAttribute('aria-label')).toBe('Recently viewed');
    });
  }
});

describe('shell top offset', () => {
  it('points the shared shell-top tokens at the measured header stack instead of a fixed 108px', () => {
    expect(SHELL_TOP_VARS).toContain('[--sl-shell-top-total:var(--shell-top)]');
    expect(SHELL_TOP_VARS).toContain('[--spacing-shell-top-total:var(--shell-top)]');
    expect(SHELL_TOP_VARS).toContain('md:[--shell-top:calc(var(--sl-header-height)+var(--sl-ticker-height))]');
  });
});
