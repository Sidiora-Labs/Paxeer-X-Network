/**
 * The chart card's degraded states, rendered with the real TvChart, the app providers and the real
 * replay datafeed against a local HTTP origin:
 *  - a candle history read that never answers: after CHART_ANSWER_BUDGET_MS of wall time the
 *    "Loading chart" spinner gives way to a compact unavailable chip on surface-1 inside the card
 *    frame, with a "Check again" retry; a late answer clears it;
 *  - a chart initialisation failure carrying transport text ("Failed to fetch") renders through
 *    humanErrorText, so the raw text never reaches the screen.
 */
import { createServer, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest';

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
/** Candle history reads held open (the datafeed "never answers") until the test releases them. */
let holdCandles = true;
const held: ServerResponse[] = [];
const candleReads: string[] = [];

function answerOutage(response: ServerResponse) {
  response.statusCode = 503;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(outage));
}

const server = createServer((request, response) => {
  response.setHeader('Access-Control-Allow-Origin', '*');
  if (request.method === 'OPTIONS') {
    response.setHeader('Access-Control-Allow-Methods', 'GET,POST');
    response.setHeader('Access-Control-Allow-Headers', '*');
    response.statusCode = 204; response.end(); return;
  }
  const url = request.url ?? '';
  if (url.startsWith(`/v2/pools/${pool}/candles`)) {
    candleReads.push(url);
    if (holdCandles) { held.push(response); return; }
  }
  answerOutage(response);
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL', 'RPC_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;
process.env.NEXT_PUBLIC_STREAM_ORIGIN = 'ws://127.0.0.1:1';

const { createRoot } = await import('react-dom/client');
const { SdkClient } = await import('@kindle/sdk');
const AppProviders = (await import('@/ui/providers/app-providers')).default;
const { MarketClientProvider } = await import('@/page-modules/token/market-session-context');
const { default: TvChart, CHART_ANSWER_BUDGET_MS } = await import('./tv-chart');
const { transientRetryBudgetMs } = await import('@/core/transient-retry');

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b|shadow/;
const stream = 'ws://127.0.0.1:1';

async function tick(ms: number) { await act(async () => { await vi.advanceTimersByTimeAsync(ms); }); }
async function waitFor(check: () => boolean, budgetMs = 5_000) {
  for (let spent = 0; !check(); spent += 25) {
    if (spent > budgetMs) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await tick(25);
  }
}

let root: Root | undefined;
let container: HTMLDivElement | undefined;

function mount() {
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => { root!.render(<AppProviders><MarketClientProvider client={client}><TvChart poolAddress={pool} /></MarketClientProvider></AppProviders>); });
  return container;
}

afterEach(() => {
  if (root) act(() => root!.unmount());
  root = undefined;
  container?.remove();
  container = undefined;
  delete (window as { TradingView?: unknown }).TradingView;
  vi.useRealTimers();
});
afterAll(() => { for (const response of held.splice(0)) answerOutage(response); server.close(); });

describe('TvChart degraded canvas', () => {
  it('bounds the wait on the candle datafeed by the transient-retry wall-time budget', () => {
    expect(CHART_ANSWER_BUDGET_MS).toBe(transientRetryBudgetMs);
    expect(CHART_ANSWER_BUDGET_MS).toBe(15_000);
  });

  it('replaces the spinner with a compact unavailable chip when the datafeed never answers, and clears it on a late answer', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'], shouldAdvanceTime: true });
    holdCandles = true;
    const view = mount();
    const card = () => view.querySelector('section[aria-label="Price chart"]') as HTMLElement | null;
    await waitFor(() => candleReads.length > 0);

    // Inside the budget: the loading overlay, no chip.
    await tick(CHART_ANSWER_BUDGET_MS - 1_000);
    expect(card()).not.toBeNull();
    expect(card()!.textContent).toContain('Loading chart');
    expect(card()!.querySelector('[data-state="unavailable"]')).toBeNull();

    // Past the budget with no answer: the chip on surface-1, inside the same card frame.
    await tick(1_500);
    const chip = card()!.querySelector('[data-state="unavailable"]') as HTMLElement | null;
    expect(chip).not.toBeNull();
    expect(chip!.textContent).toContain('Chart data is not available yet');
    expect(chip!.className).toContain('bg-surface-2');
    expect(chip!.closest('.absolute')?.className).toContain('bg-surface-1');
    expect(card()!.textContent).not.toContain('Loading chart');
    expect(card()!.textContent).not.toMatch(/Failed to fetch|unavailable right now/i);
    // The card keeps its toolbar and data disclosure around the chip.
    expect(card()!.querySelector('button[aria-controls="canonical-chart-table"]')).not.toBeNull();
    for (const node of Array.from(card()!.querySelectorAll('section, div, span'))) expect(node.className).not.toMatch(BORDER);

    // "Check again" restarts the read; it is still held, so the wait starts over from loading.
    const before = candleReads.length;
    const retry = Array.from(chip!.querySelectorAll('button')).find(button => button.textContent === 'Try again');
    expect(retry).toBeDefined();
    act(() => { retry!.click(); });
    await waitFor(() => candleReads.length > before);
    expect(card()!.querySelector('[data-state="unavailable"]')).toBeNull();
    await tick(CHART_ANSWER_BUDGET_MS + 500);
    expect(card()!.querySelector('[data-state="unavailable"]')).not.toBeNull();

    // A late answer (here the history read's real 503) settles the wait and clears the chip.
    holdCandles = false;
    for (const response of held.splice(0)) answerOutage(response);
    await waitFor(() => card()!.querySelector('[data-state="unavailable"]') === null);
  }, 30_000);

  it('renders a chart initialisation failure through humanErrorText, never the raw transport text', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'], shouldAdvanceTime: true });
    holdCandles = false;
    // The vendor library global (public/charting_library) failing to fetch its own bundle.
    (window as { TradingView?: unknown }).TradingView = { widget: class { constructor() { throw new TypeError('Failed to fetch'); } } };
    const view = mount();
    await waitFor(() => view.querySelector('section[aria-label="Price chart"] [role="alert"]') !== null);
    const alert = view.querySelector('section[aria-label="Price chart"] [role="alert"]') as HTMLElement;
    expect(alert.getAttribute('data-state')).toBe('error');
    expect(alert.textContent).toContain("Chart can't load right now");
    expect(view.textContent).not.toContain('Failed to fetch');
    expect(alert.closest('.absolute')?.className).toContain('bg-surface-1');
    expect(alert.className).not.toMatch(BORDER);
    expect(Array.from(alert.querySelectorAll('button')).map(button => button.textContent)).toContain('Try again');
    // The canonical data table opens alongside the error, as before.
    expect(view.querySelector('#canonical-chart-table')).not.toBeNull();
  }, 30_000);
});
