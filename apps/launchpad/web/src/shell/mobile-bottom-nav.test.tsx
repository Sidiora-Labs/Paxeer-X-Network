// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { connect } from 'wagmi/actions';
import { mock } from 'wagmi/connectors';
import { defineChain } from 'viem';
import { PathnameContext } from 'next/dist/shared/lib/hooks-client-context.shared-runtime';
import MobileBottomNav, { BOTTOM_NAV_BREAKPOINT_PX, BOTTOM_NAV_IDS, moreSheetGroups } from './mobile-bottom-nav';
import { createItem, navItems } from './nav-config';

vi.mock('next/image', async importOriginal => ({ default: ((await importOriginal()) as { default: unknown }).default }));

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  // jsdom has no matchMedia; a phone-width implementation that never matches min-width queries.
  window.matchMedia = (query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false }) as MediaQueryList;
});

const ACCOUNT = '0x1111111111111111111111111111111111111111';
const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const makeConfig = () => createConfig({ chains: [chain], connectors: [mock({ accounts: [ACCOUNT] })], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, multiInjectedProviderDiscovery: false });

const mounted: Root[] = [];

async function render(pathname: string, config = makeConfig()) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  mounted.push(root);
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (path: string) => (
    <WagmiProvider config={config}>
      <QueryClientProvider client={queries}>
        <PathnameContext.Provider value={path}>
          <MobileBottomNav />
        </PathnameContext.Provider>
      </QueryClientProvider>
    </WagmiProvider>
  );
  await act(async () => { root.render(tree(pathname)); });
  return { container, rerender: async (path: string) => { await act(async () => { root.render(tree(path)); }); } };
}

const nav = () => document.querySelector('nav[aria-label="Mobile navigation"]') as HTMLElement;
const targets = () => Array.from(nav().querySelectorAll<HTMLElement>('a, button'));
const sheet = () => document.querySelector<HTMLElement>('[role="dialog"][aria-label="More"]');
const moreButton = () => nav().querySelector<HTMLButtonElement>('button[aria-label="More"]')!;

afterEach(() => {
  for (const root of mounted.splice(0)) act(() => root.unmount());
  document.body.innerHTML = '';
});

describe('MobileBottomNav bar', () => {
  it('renders five icon-only targets in reference order with accessible names', async () => {
    await render('/');
    const items = targets();
    expect(items.map(item => item.getAttribute('aria-label'))).toEqual(['Home', 'Explore', 'Create', 'Bounties', 'More']);
    for (const item of items) {
      expect(item.textContent?.trim()).toBe('');
      expect(item.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
    }
    expect(items.slice(0, 4).map(item => item.getAttribute('href'))).toEqual(['/', '/explore', '/create', '/bounties']);
  });

  it('is phone-only: hidden from md (768) up, where the rail takes over', async () => {
    await render('/');
    expect(BOTTOM_NAV_BREAKPOINT_PX).toBe(768);
    expect(nav().className).toContain('md:hidden');
    expect(nav().className).not.toContain('lg:hidden');
  });

  it('separates by background contrast only: no border or ring classes, create is not a filled accent tile', async () => {
    await render('/');
    expect(nav().className).toContain('bg-surface-1');
    expect(nav().outerHTML).not.toMatch(/\bborder\b|border-[a-z]|\bring-|shadow-|bg-accent\b|gradient/);
  });

  it('tints only the active target and marks it as the current page', async () => {
    await render('/explore');
    const [home, explore] = targets();
    expect(explore.getAttribute('aria-current')).toBe('page');
    expect(explore.className).toContain('text-accent');
    expect(home.getAttribute('aria-current')).toBeNull();
    expect(home.className).not.toContain('text-accent');
  });

  it('tints More when the current route lives in the sheet', async () => {
    await render('/leaderboard');
    expect(moreButton().className).toContain('text-accent');
    expect(targets().filter(item => item.getAttribute('aria-current') === 'page')).toHaveLength(0);
  });
});

describe('More sheet', () => {
  it('opens a modal sheet listing every rail destination not on the bar, and closes on Escape', async () => {
    await render('/');
    expect(sheet()).toBeNull();
    await act(async () => { moreButton().click(); });
    const dialog = sheet()!;
    expect(dialog).not.toBeNull();
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(moreButton().getAttribute('aria-expanded')).toBe('true');
    const links = Array.from(dialog.querySelectorAll('a')).map(link => link.textContent?.trim());
    const pinned = new Set<string>(BOTTOM_NAV_IDS);
    const expected = [...navItems.filter(item => item.section === 'main'), ...navItems.filter(item => item.section === 'bottom')]
      .filter(item => item.requires === 'public' && !pinned.has(item.id)).map(item => item.label);
    expect(links).toEqual(expected);
    expect(links.slice(0, 4)).toEqual(['Leaderboard', 'Live', 'Holder rewards', 'Terminal']);
    expect(dialog.className).toContain('bg-surface-2');
    expect(dialog.outerHTML).not.toMatch(/\bborder\b|border-[a-z]|\bring-|shadow-|gradient/);
    await act(async () => { dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); });
    expect(sheet()).toBeNull();
    expect(moreButton().getAttribute('aria-expanded')).toBe('false');
  });

  it('closes from the scrim and from the close button', async () => {
    await render('/');
    await act(async () => { moreButton().click(); });
    const closers = Array.from(document.querySelectorAll<HTMLButtonElement>('button[aria-label="Close menu"]'));
    expect(closers).toHaveLength(2);
    await act(async () => { closers[0].click(); });
    expect(sheet()).toBeNull();
    await act(async () => { moreButton().click(); });
    await act(async () => { document.querySelectorAll<HTMLButtonElement>('button[aria-label="Close menu"]')[1].click(); });
    expect(sheet()).toBeNull();
  });

  it('closes when the route changes', async () => {
    const view = await render('/');
    await act(async () => { moreButton().click(); });
    expect(sheet()).not.toBeNull();
    await view.rerender('/live');
    expect(sheet()).toBeNull();
  });

  it('marks the current route inside the sheet', async () => {
    await render('/holder-rewards');
    await act(async () => { moreButton().click(); });
    const current = sheet()!.querySelector('a[aria-current="page"]');
    expect(current?.textContent?.trim()).toBe('Holder rewards');
  });

  it('adds the account group once a wallet is connected', async () => {
    const config = makeConfig();
    await render('/', config);
    expect(sheet()).toBeNull();
    await act(async () => { await connect(config, { connector: config.connectors[0] }); });
    await act(async () => { moreButton().click(); });
    const account = sheet()!.querySelector('section[aria-label="Your account"]');
    expect(Array.from(account!.querySelectorAll('a')).map(link => link.textContent?.trim())).toEqual(navItems.filter(item => item.section === 'signed-in').map(item => item.label));
    expect(account!.textContent).toContain('Rewards');
  });
});

describe('moreSheetGroups', () => {
  it('covers every configured destination exactly once across bar and sheet when signed in', () => {
    const sheetIds = moreSheetGroups(true).flatMap(group => group.items.map(item => item.id));
    const all = [...navItems.map(item => item.id), createItem.id].sort();
    expect([...BOTTOM_NAV_IDS, ...sheetIds].sort()).toEqual(all);
  });

  it('hides wallet and session gated items for guests', () => {
    const groups = moreSheetGroups(false);
    expect(groups.map(group => group.id)).toEqual(['browse', 'resources']);
    expect(groups.flatMap(group => group.items).every(item => item.requires === 'public')).toBe(true);
  });
});
