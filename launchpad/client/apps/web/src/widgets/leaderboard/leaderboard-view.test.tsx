/**
 * The real LeaderboardView against a real local HTTP origin that serves none
 * of the cup, ranking or feed routes (404 everywhere, as on a box with no
 * backend). Checks the parity structure survives: the cup hero frame always
 * renders with the period control inside it and no prize figure, the cup
 * segment is disabled and ?period=cup falls back to the day board, and Best
 * callouts is a column at xl but a fourth tab below it.
 */
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, describe, expect, it } from 'vitest';

interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000/leaderboard', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;
if (typeof navigator !== 'undefined' && navigator.onLine === undefined) Object.defineProperty(navigator, 'onLine', { value: true, configurable: true });

// jsdom has no layout engine and no matchMedia; evaluate min-width queries against a settable viewport width.
let viewportWidth = 1440;
(dom.window as unknown as { matchMedia: (query: string) => MediaQueryList }).matchMedia = (query: string) => {
  const min = Number(/min-width:\s*(\d+)px/.exec(query)?.[1] ?? 0);
  return { matches: viewportWidth >= min, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false } as MediaQueryList;
};

const seen: string[] = [];
const server = createServer((request, response) => {
  seen.push(request.url ?? '');
  response.writeHead(404, { 'content-type': 'application/json', 'access-control-allow-origin': '*' });
  response.end(JSON.stringify({ code: 'NOT_FOUND', requestId: 'req-miss' }));
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { injected } = await import('wagmi/connectors');
const { defineChain } = await import('viem');
const { LeaderboardView } = await import('./leaderboard-view');
const { CalloutsRail } = await import('@/widgets/social/callouts-rail');

const chain = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: false, multiInjectedProviderDiscovery: false });
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function mount(width: number, search = '') {
  viewportWidth = width;
  window.history.replaceState(null, '', `/leaderboard${search}`);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => { root.render(<WagmiProvider config={wagmiConfig}><QueryClientProvider client={queryClient}><LeaderboardView rail={<CalloutsRail />} /></QueryClientProvider></WagmiProvider>); });
  return { root, container, queryClient };
}
function unmount(view: ReturnType<typeof mount>) {
  act(() => view.root.unmount());
  view.queryClient.clear();
  view.container.remove();
}
async function waitFor(check: () => boolean, timeoutMs = 6000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 600)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}
const hero = (root: ParentNode) => root.querySelector<HTMLElement>('section[aria-label="Competition"], section[aria-labelledby="cup-hero-title"]');
const tabNames = (root: ParentNode) => Array.from(root.querySelectorAll('[role=tab]')).map(tab => tab.textContent);
const classesOf = (root: ParentNode) => Array.from(root.querySelectorAll('[class]')).flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));

afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));

describe('leaderboard view without a backend', () => {
  it('keeps the cup hero frame with the period control inside it while loading and after the cup route 404s', async () => {
    const view = mount(1440);
    const loading = hero(view.container);
    expect(loading).not.toBeNull();
    expect(loading!.getAttribute('data-cup-state')).toBe('loading');
    expect(loading!.querySelector('[role=radiogroup][aria-label="Ranking period"]')).not.toBeNull();

    await waitFor(() => hero(view.container)?.getAttribute('data-cup-state') === 'unsupported');
    expect(seen.some(url => url.startsWith('/v2/cups/current'))).toBe(true);
    const frame = hero(view.container)!;
    expect(frame.className).toContain('md:aspect-[16/7]');
    const control = frame.querySelector('[role=radiogroup][aria-label="Ranking period"]')!;
    expect(control.querySelectorAll('[role=radio]')).toHaveLength(4);
    expect(control.querySelector<HTMLButtonElement>('[aria-label="Competition"]')!.disabled).toBe(true);
    expect(control.querySelector('[aria-checked="true"]')!.textContent).toBe('1D');
    expect(frame.textContent).toContain('No competition scheduled');
    expect(frame.textContent).toContain('No prize pool announced');
    // Nothing fabricated: no USDL figure and no digits in the unavailable frame beyond the segment labels.
    expect(frame.textContent).not.toContain('USDL');
    expect(frame.textContent!.replace(/1[DWM]/g, '')).not.toMatch(/\d/);
    expect(frame.querySelector('button:not([role=radio])')!.textContent).toBe('Try again');
    // The rolling board follows the hero.
    expect(view.container.querySelector('#traders-board-title')).not.toBeNull();
    expect(classesOf(view.container).join(' ')).not.toMatch(BORDER);
    unmount(view);
  });

  it('puts Best callouts in its own column at xl with three tabs', async () => {
    const view = mount(1440);
    expect(tabNames(view.container)).toEqual(['Traders', 'Squads', 'Rewards']);
    const rail = view.container.querySelector('aside[aria-label="Best callouts"]');
    expect(rail).not.toBeNull();
    expect(rail!.closest('[role=tabpanel]')).toBeNull();
    expect(rail!.parentElement!.className).toContain('[&>aside>div:first-child_h2]:text-xl');
    unmount(view);
  });

  it('folds Best callouts into a fourth tab below xl', async () => {
    const view = mount(1024);
    expect(tabNames(view.container)).toEqual(['Traders', 'Squads', 'Rewards', 'Best callouts']);
    expect(view.container.querySelector('aside[aria-label="Best callouts"]')).toBeNull();
    const tab = Array.from(view.container.querySelectorAll<HTMLButtonElement>('[role=tab]')).find(node => node.textContent === 'Best callouts')!;
    act(() => { tab.click(); });
    const panel = view.container.querySelector('#leaderboard-panel-callouts');
    expect(panel).not.toBeNull();
    expect(panel!.querySelector('aside[aria-label="Best callouts"]')).not.toBeNull();
    expect(hero(view.container)).toBeNull();
    expect(window.location.search).toContain('tab=callouts');
    unmount(view);
  });

  it('keeps the hero first on a phone, with the control, the unavailable chip and the rules row inside it', async () => {
    const view = mount(390);
    expect(tabNames(view.container)).toEqual(['Traders', 'Squads', 'Rewards', 'Best callouts']);
    await waitFor(() => hero(view.container)?.getAttribute('data-cup-state') === 'unsupported');
    const frame = hero(view.container)!;
    const panel = view.container.querySelector('#leaderboard-panel-traders')!;
    expect(panel.firstElementChild).toBe(frame);
    expect(frame.querySelector('[role=radiogroup][aria-label="Ranking period"]')).not.toBeNull();
    expect(frame.querySelector('[data-state="unavailable"]')).not.toBeNull();
    expect(frame.textContent).toContain('Schedule, scoring, eligibility and rules');
    expect(frame.className).not.toContain('overflow-hidden');
    expect(view.container.querySelector('aside[aria-label="Best callouts"]')).toBeNull();
    unmount(view);
  });

  it('falls back from ?period=cup to the day board when no cup record exists', async () => {
    const view = mount(768, '?period=cup');
    await waitFor(() => hero(view.container)?.getAttribute('data-cup-state') === 'unsupported');
    const control = hero(view.container)!.querySelector('[role=radiogroup]')!;
    expect(control.querySelector('[aria-checked="true"]')!.textContent).toBe('1D');
    expect(view.container.querySelector('#traders-board-title')!.closest('section')!.textContent).toContain('Last 24 hours');
    unmount(view);
  });
});
