/**
 * The coin page when the snapshot fails (not a 404): the URL already names the pool, so the header
 * frame, chart card, trade panel, coin tabs and the mobile trade CTA must all render, with only the
 * values degraded. The freshness problem is a compact chip inside the header, not a page banner.
 * Runs the real TokenMarketLayout with the app providers against a real local HTTP origin that
 * answers every market route with a non-retryable 503.
 */
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, describe, expect, it, vi } from 'vitest';

// vitest.config.ts sets `deps.interopDefault: false`, so CJS `next/image` arrives as its exports object;
// re-expose the real component as the default (same recipe as coin/coin-units.render.test.tsx).
vi.mock('next/image', async importOriginal => {
  const mod = await importOriginal<{ default: unknown }>();
  const cjs = mod.default as { default?: unknown } | undefined;
  return { ...mod, default: cjs && typeof cjs === 'object' && 'default' in cjs ? cjs.default : mod.default };
});

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
browserGlobals.Event = dom.window.Event;
browserGlobals.CustomEvent = dom.window.CustomEvent;
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const outage = { code: 'DEPENDENCY_UNAVAILABLE', message: 'no backing service', requestId: '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', retryable: false, retryAfterMs: null, fieldErrors: [], details: null };
const seen: string[] = [];

const server = createServer((request, response) => {
  response.setHeader('Access-Control-Allow-Origin', '*');
  if (request.method === 'OPTIONS') {
    response.setHeader('Access-Control-Allow-Methods', 'GET,POST');
    response.setHeader('Access-Control-Allow-Headers', '*');
    response.statusCode = 204; response.end(); return;
  }
  seen.push(request.url ?? '');
  response.statusCode = 503;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(outage));
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL', 'RPC_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;
process.env.NEXT_PUBLIC_STREAM_ORIGIN = 'ws://127.0.0.1:1';

const { createRoot } = await import('react-dom/client');
const { SdkClient } = await import('@kindle/sdk');
const AppProviders = (await import('@/ui/providers/app-providers')).default;
const { MarketClientProvider } = await import('@/page-modules/token/market-session-context');
const { default: TokenMarketLayout } = await import('./token-market-layout');

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

async function settle(ms: number) { await act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); }); }
async function waitFor(check: () => boolean, timeoutMs = 10_000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await settle(25);
  }
}

let root: Root | undefined;
afterAll(() => { if (root) act(() => root!.unmount()); server.close(); });

describe('coin page when the snapshot fails', () => {
  it('keeps the header, chart, trade panel, tabs and trade CTA from the URL pool, with a compact freshness chip', async () => {
    const stream = 'ws://127.0.0.1:1';
    const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
    const container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    act(() => { root!.render(<AppProviders><MarketClientProvider client={client}><TokenMarketLayout poolAddress={pool} /></MarketClientProvider></AppProviders>); });

    await waitFor(() => container.querySelector('[data-market-snapshot="failed"]') !== null);
    await waitFor(() => container.querySelector('form[aria-label="Trade"]') !== null && container.querySelector('section[aria-label="Coin details"]') !== null);

    const page = container.querySelector('[data-market-pool]') as HTMLElement;
    expect(page.getAttribute('data-coin-state')).toBeNull();
    // Header frame from the URL: the short pool address as the title and the explorer link to Paxeer.
    await waitFor(() => container.querySelector('header[data-coin-header="degraded"]') !== null);
    const header = container.querySelector('header[data-coin-header="degraded"]') as HTMLElement;
    expect(header.querySelector('h1')?.textContent).toContain('0xce20...6263');
    expect(Array.from(header.querySelectorAll('a')).some(link => (link.getAttribute('href') ?? '').includes(`paxscan.paxeer.app/address/${pool}`))).toBe(true);
    expect(header.querySelector('button[aria-label^="Market cap not available yet"]')).not.toBeNull();
    // Chart card frame, the tab strip and the trade panel are present.
    expect(container.querySelector('section[aria-label="Price chart"]')).not.toBeNull();
    expect(Array.from(container.querySelectorAll('[role="tab"]')).map(tab => tab.textContent)).toEqual(expect.arrayContaining([expect.stringContaining('Holders'), 'Trades', 'About', 'Callouts']));
    // The mobile trade CTA (hidden at lg by CSS only) asks a signed-out viewer to sign in.
    expect(container.querySelector('[data-mobile-trade-cta="sign-in"]')?.textContent).toBe('Sign in to trade');
    // Rail order: trade panel, holders, stats, pool information, creator, market cap vs ATH, description.
    const rail = container.querySelector('aside[aria-label="Trade and coin details"]') as HTMLElement;
    const order = Array.from(rail.querySelectorAll('form[aria-label="Trade"], section[aria-label="Stats and audit"], h2')).map(node => node.getAttribute('aria-label') ?? node.textContent);
    expect(order.slice(0, 7)).toEqual(['Trade', 'Holders', 'Stats and audit', 'Pool information', 'Creator', 'Market cap vs ATH', 'Description']);

    // Freshness: a chip inside the header, never a page-wide notice bar above the columns.
    await waitFor(() => container.querySelector('[data-freshness-chip]') !== null);
    const chip = container.querySelector('[data-freshness-chip]') as HTMLElement;
    expect(header.contains(chip)).toBe(true);
    expect(chip.querySelector('[aria-hidden="true"].whitespace-nowrap')?.textContent?.length ?? 99).toBeLessThanOrEqual(14);
    expect(page.firstElementChild?.className).toContain('grid');
    expect(chip.className).not.toMatch(BORDER);

    // The degraded values carry the shared compact notice inside their frames, never a full error panel.
    const headerNotice = header.querySelector('[data-state="unavailable"]') as HTMLElement;
    expect(headerNotice?.textContent).toContain('This coin is not available yet');
    expect(headerNotice.className).toContain('flex-wrap'); // wraps to two lines at 390 instead of truncating
    await waitFor(() => container.querySelector('[data-tab-unavailable="Holders"]') !== null);
    const holdersFrame = container.querySelector('[data-tab-unavailable="Holders"]') as HTMLElement;
    expect(holdersFrame.textContent).toContain('Holders are not available yet');
    expect(container.querySelector('section[aria-label="Coin details"]')?.textContent).not.toMatch(/unavailable right now|Failed to fetch/i);
    for (const card of ['pool-info-title', 'creator-card-title', 'description-card-title']) {
      const section = rail.querySelector(`section[aria-labelledby="${card}"]`) as HTMLElement;
      await waitFor(() => section.querySelector('[data-state="unavailable"]') !== null);
    }
    expect(rail.textContent).toContain('Pool information is not available yet');
    expect(rail.textContent).not.toMatch(/Something went wrong|can't load right now|Failed to fetch/);
    // Trade panel is in the rail from lg (1024), not only from xl.
    const tradeSlot = rail.querySelector('form[aria-label="Trade"]')?.closest('.lg\\:block') as HTMLElement | null;
    expect(tradeSlot).not.toBeNull();
    expect(tradeSlot!.className).not.toContain('xl:block');
    // At md the left rail replaces the bottom nav, so the sticky CTA clears it instead of floating over it.
    expect(container.querySelector('[data-mobile-trade-cta]')?.className).toContain('md:left-rail-width');
    // Separation by surface contrast only: no strokes on the page frames.
    for (const node of Array.from(page.querySelectorAll('section, header, aside, [data-tab-unavailable], [data-state]'))) expect(node.className).not.toMatch(BORDER);

    // Stats keep three split-bar frames instead of prose.
    expect(rail.querySelectorAll('[data-split-frame]')).toHaveLength(3);

    // The frames mount snapshot observers; that must not turn into a refetch loop of the IF_POOL_DETAIL snapshot read.
    const snapshotReads = () => seen.filter(url => url === `/v2/pools/${pool}`).length;
    const before = snapshotReads();
    expect(before).toBeGreaterThan(0);
    await settle(1500);
    const after = snapshotReads();
    expect(after - before).toBeLessThanOrEqual(2);
    expect(container.querySelector('[data-market-snapshot="failed"]')).not.toBeNull();
  }, 30_000);
});
