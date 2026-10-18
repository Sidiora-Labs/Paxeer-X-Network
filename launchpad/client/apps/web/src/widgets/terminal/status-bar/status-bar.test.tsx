import { createServer, type IncomingMessage, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { Operations, SdkClient, type V2ContractTypes } from '@kindle/sdk';

// Same setup as widgets/profile/*.test.tsx: node environment plus the browser globals of a real
// jsdom window, so SDK requests run over Node's fetch against a real local HTTP origin.
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
// Node ships its own Event/CustomEvent, which jsdom's window.dispatchEvent rejects.
browserGlobals.Event = dom.window.Event;
browserGlobals.CustomEvent = dom.window.CustomEvent;
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

type Seen = { method: string; url: string; headers: IncomingMessage['headers']; body: string };
type Responder = (seen: Seen) => { status: number; body?: unknown };
type Served = { server: Server; origin: string; seen: Seen[] };

function serve(responder: Responder): Promise<Served> {
  const seen: Seen[] = [];
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', chunk => { body += String(chunk); });
    request.on('end', () => {
      const entry = { method: request.method ?? 'GET', url: request.url ?? '', headers: request.headers, body };
      seen.push(entry);
      const answer = responder(entry);
      response.statusCode = answer.status;
      if (answer.body === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(answer.body));
    });
  });
  return new Promise(resolve => server.listen(0, '127.0.0.1', () => resolve({ server, origin: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, seen })));
}

type Watermark = V2ContractTypes['Watermark'];
type RankingsResponse = V2ContractTypes['RankingsResponse'];
type AlertCreateResponse = V2ContractTypes['AlertCreateResponse'];
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };
const poolA = '0x' + 'a1'.repeat(20);
const poolB = '0x' + 'b2'.repeat(20);
const token = '0x' + 'c3'.repeat(20);
const usdl = '0x85fcd13735f4309833a503ee804ea32395851479';
const wallet = '0x' + 'd4'.repeat(20);

const rankings: RankingsResponse = {
  requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'complete', revision: '7', watermark,
  data: { snapshot: {
    snapshotId: '7b6f2b4e-3c1d-4e2f-9a8b-1c2d3e4f5a6b', category: 'trending', computedAt: '2026-10-12T15:00:00.000Z', modelVersion: 'trend-v1',
    replace: true, totalEligible: '2', truncated: false, watermark,
    items: [
      { category: 'trending', computedAt: '2026-10-12T15:00:00.000Z', modelVersion: 'trend-v1', poolAddress: poolA, rank: 1, score: '90' },
      { category: 'trending', computedAt: '2026-10-12T15:00:00.000Z', modelVersion: 'trend-v1', poolAddress: poolB, rank: 2, score: '80' },
    ],
  } },
};

// One local origin plays every host (core/api, gateway, metadata, user, pnl) so nothing leaves the machine.
const feedUnavailable = { code: 'DEPENDENCY_UNAVAILABLE', message: 'discovery runtime not configured', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null };
let rankingsServed = true;
let rankingsEmpty = false;
const emptyRankings: RankingsResponse = { ...rankings, data: { snapshot: { ...rankings.data.snapshot, totalEligible: '0', items: [] } } };
const api = await serve(({ url }) => {
  if (url === '/v2/rankings/trending' && rankingsServed) return { status: 200, body: rankingsEmpty ? emptyRankings : rankings };
  if (url.startsWith('/v2/discovery/feed')) return { status: 503, body: feedUnavailable };
  return { status: 404 };
});
for (const name of ['DATA', 'GATEWAY', 'USER', 'METADATA', 'SOCIAL', 'PNL']) process.env[`NEXT_PUBLIC_${name}_API_URL`] = api.origin;
process.env.NEXT_PUBLIC_GATEWAY_URL = api.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { defineChain } = await import('viem');
const { MarketClientContext } = await import('@/core/market/client');
const { toUnsupported } = await import('@/core/market/unsupported');
const statusBar = await import('./index');
const { alertCreateBody, connectionStatus, isTerminalPanel, priceRatio, terminalOpenPanelEvent } = await import('./model');
const { createAlert, deleteAlert } = await import('./alerts-panel');
const TerminalStatusBar = statusBar.default;
const { columnLimits, resetTerminalColumns, useResetColumnsEvent, useTerminalColumns } = await import('../column-store');

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: [api.origin] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http(api.origin) }, ssr: true, multiInjectedProviderDiscovery: false });
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function clientFor(origin: string, token?: string): SdkClient {
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false, ...(token ? { session: { mode: 'bearer' as const, token: () => token } } : {}) });
}

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement; queryClient: QueryClientType } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          <MarketClientContext.Provider value={clientFor(api.origin)}>{node}</MarketClientContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return { root, container, queryClient };
}

function unmount({ root, container, queryClient }: { root: Root; container: HTMLDivElement; queryClient: QueryClientType }) {
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

function tool(container: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll<HTMLButtonElement>('button[aria-haspopup="dialog"]')).find(button => button.textContent?.trim() === label);
  if (!found) throw new Error(`No tool ${label}`);
  return found;
}
const dialogs = (container: HTMLElement) => Array.from(container.querySelectorAll('[role="dialog"]'));

afterAll(async () => {
  await new Promise<void>(resolve => api.server.close(() => resolve()));
  dom.window.close();
});

describe('connectionStatus', () => {
  it('maps the browser flag, the stream state and freshness to the three labels', () => {
    expect(connectionStatus({ online: false, stream: 'live', freshness: 'current' })).toMatchObject({ label: 'Offline', tone: 'danger' });
    expect(connectionStatus({ online: true, stream: null, freshness: null })).toMatchObject({ label: 'Stable', tone: 'success' });
    expect(connectionStatus({ online: true, stream: 'live', freshness: 'current' })).toMatchObject({ label: 'Stable', tone: 'success' });
    expect(connectionStatus({ online: true, stream: 'live', freshness: 'delayed' })).toMatchObject({ label: 'Stable' });
    expect(connectionStatus({ online: true, stream: 'live', freshness: 'stale' })).toMatchObject({ label: 'Reconnecting', tone: 'warning' });
    expect(connectionStatus({ online: true, stream: 'live', freshness: 'reconnecting' })).toMatchObject({ label: 'Reconnecting' });
    expect(connectionStatus({ online: true, stream: 'resync-required', freshness: 'unavailable' })).toMatchObject({ label: 'Reconnecting' });
    expect(connectionStatus({ online: true, stream: 'loading', freshness: 'unavailable' }).detail).toBe('Reconnecting to live updates...');
    expect(connectionStatus({ online: true, stream: 'closed', freshness: 'unavailable' })).toMatchObject({ label: 'Offline', tone: 'danger' });
  });

  it('accepts only known panel ids from the window event', () => {
    expect(isTerminalPanel('alerts')).toBe(true);
    expect(isTerminalPanel('trenches')).toBe(false);
    expect(isTerminalPanel(3)).toBe(false);
  });
});

describe('price alert requests', () => {
  it('turns a decimal USDL price into an exact ratio', () => {
    expect(priceRatio('0.00012')).toEqual({ numerator: '12', denominator: '100000' });
    expect(priceRatio('15')).toEqual({ numerator: '15', denominator: '1' });
    expect(priceRatio('2.50')).toEqual({ numerator: '250', denominator: '100' });
    for (const bad of ['', '0', '0.000', '-1', '1e5', '1.', '.5', '0.' + '1'.repeat(19)]) expect(priceRatio(bad)).toBeNull();
  });

  it('sends IF_ALERT_CREATE with the contract body and an idempotency key, then deletes by id', async () => {
    const alertId = '3d6c1c4e-8a1b-4b7e-9f20-6f0e1d2c3b4a';
    const created: AlertCreateResponse = {
      requestId, generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'complete', revision: null, watermark: null,
      data: { alert: { alertId, wallet, poolAddress: poolA, direction: 'crosses_above', cooldownSeconds: 300, state: 'enabled',
        threshold: { numerator: '12', denominator: '100000', baseAsset: { kind: 'erc20', chainId: '125', address: token, currencyCode: null, decimals: 18 }, quoteAsset: { kind: 'erc20', chainId: '125', address: usdl, currencyCode: 'USDL', decimals: 6 },
          source: { kind: 'external_display', amountBasis: 'provider_display', observedAt: '2026-10-12T15:00:00.000Z', position: null, providerId: null, sourceEventId: null, sourceReference: null } } } },
    };
    const alerts = await serve(({ method, url }) => {
      if (method === 'POST' && url === '/v2/me/alerts') return { status: 200, body: created };
      if (method === 'DELETE' && url === `/v2/me/alerts/${alertId}`) return { status: 204 };
      return { status: 404 };
    });
    const operations = new Operations(clientFor(alerts.origin, 'session-token'));
    const body = alertCreateBody({ poolAddress: poolA.toUpperCase().replace('0X', '0x'), tokenAddress: token, tokenDecimals: 18, usdlAddress: usdl, direction: 'crosses_above', price: '0.00012', cooldownSeconds: 300, observedAt: new Date('2026-10-12T15:00:00.000Z') });
    expect(body).not.toBeNull();
    const alert = await createAlert(operations, body!, 'b0c1d2e3-f405-4617-8829-3a4b5c6d7e8f');
    expect(alert.alertId).toBe(alertId);
    const post = alerts.seen[0];
    expect(post.method).toBe('POST');
    expect(post.headers['idempotency-key']).toBe('b0c1d2e3-f405-4617-8829-3a4b5c6d7e8f');
    expect(post.headers.authorization).toBe('Bearer session-token');
    expect(JSON.parse(post.body)).toEqual({
      poolAddress: poolA, direction: 'crosses_above', cooldownSeconds: 300,
      threshold: { numerator: '12', denominator: '100000', baseAsset: { kind: 'erc20', chainId: '125', address: token, currencyCode: null, decimals: 18 }, quoteAsset: { kind: 'erc20', chainId: '125', address: usdl, currencyCode: 'USDL', decimals: 6 },
        source: { kind: 'external_display', amountBasis: 'provider_display', observedAt: '2026-10-12T15:00:00.000Z', position: null, providerId: null, sourceEventId: null, sourceReference: null } },
    });
    await deleteAlert(operations, alertId);
    expect(alerts.seen[1]).toMatchObject({ method: 'DELETE', url: `/v2/me/alerts/${alertId}` });
    await new Promise<void>(resolve => alerts.server.close(() => resolve()));
  });

  it('surfaces the real 404 of the unserved alert route as unsupported', async () => {
    const operations = new Operations(clientFor(api.origin, 'session-token'));
    const body = alertCreateBody({ poolAddress: poolA, tokenAddress: token, tokenDecimals: 18, usdlAddress: usdl, direction: 'crosses_below', price: '1', cooldownSeconds: 60, observedAt: new Date('2026-10-12T15:00:00.000Z') });
    const error = await createAlert(operations, body!, 'c0c1d2e3-f405-4617-8829-3a4b5c6d7e8f').then(() => null, (cause: unknown) => cause);
    expect(toUnsupported('IF_ALERT_CREATE', error)).toMatchObject({ state: 'unsupported', operation: 'IF_ALERT_CREATE', status: 404 });
  });
});

describe('TerminalStatusBar', () => {
  beforeAll(() => { rankingsServed = true; });

  it('renders the tools, the connection pill and the static language label without borders', () => {
    const mounted = mount(<TerminalStatusBar poolAddress={null} />);
    const text = mounted.container.textContent ?? '';
    for (const label of ['Stable', 'Watchlist', 'Wallets tracker', 'PnL tracker', 'Feed', 'Trenches', 'Alpha tracker', 'Price alerts', 'Trending', 'Callouts', 'English']) expect(text).toContain(label);
    expect(mounted.container.querySelector('a[href="/terminal/trenches"]')).not.toBeNull();
    expect(mounted.container.querySelector('a[href="/docs"]')).not.toBeNull();
    expect(mounted.container.querySelector('a[href="https://t.me/Sidiorafunsupport"]')).not.toBeNull();
    expect(text).toContain('No wallet connected');
    expect(dialogs(mounted.container)).toHaveLength(0);
    expect(mounted.container.innerHTML).not.toMatch(BORDER);
    unmount(mounted);
  });

  it('opens one panel at a time, names missing operations, closes on a second click and on Escape', async () => {
    const mounted = mount(<TerminalStatusBar poolAddress={null} />);
    const { container } = mounted;
    const wallets = tool(container, 'Wallets tracker');
    act(() => wallets.click());
    expect(dialogs(container)).toHaveLength(1);
    expect(wallets.getAttribute('aria-expanded')).toBe('true');
    expect(dialogs(container)[0].textContent).toContain('Wallets tracker is not available yet');
    expect(dialogs(container)[0].textContent).toContain('Wallet tracking is coming soon.');
    expect(dialogs(container)[0].textContent).not.toContain('no contract operation');
    expect(document.activeElement).toBe(dialogs(container)[0]);

    const alpha = tool(container, 'Alpha tracker');
    act(() => alpha.click());
    expect(dialogs(container)).toHaveLength(1);
    expect(dialogs(container)[0].textContent).toContain('Alpha tracking is coming soon.');
    expect(wallets.getAttribute('aria-expanded')).toBe('false');
    expect(alpha.getAttribute('aria-controls')).toBe(dialogs(container)[0].id);

    act(() => alpha.click());
    expect(dialogs(container)).toHaveLength(0);

    act(() => wallets.click());
    expect(dialogs(container)).toHaveLength(1);
    act(() => { document.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); });
    expect(dialogs(container)).toHaveLength(0);
    expect(document.activeElement).toBe(wallets);

    act(() => wallets.click());
    act(() => { document.body.dispatchEvent(new window.MouseEvent('pointerdown', { bubbles: true })); });
    expect(dialogs(container)).toHaveLength(0);
    unmount(mounted);
  });

  it('opens the panel named by the sidiora:terminal-open-panel event and ignores unknown ids', async () => {
    const mounted = mount(<TerminalStatusBar poolAddress={null} />);
    const { container } = mounted;
    act(() => { window.dispatchEvent(new window.CustomEvent(terminalOpenPanelEvent, { detail: 'nope' })); });
    expect(dialogs(container)).toHaveLength(0);
    act(() => { window.dispatchEvent(new window.CustomEvent(terminalOpenPanelEvent, { detail: 'alerts' })); });
    expect(dialogs(container)).toHaveLength(1);
    expect(tool(container, 'Price alerts').getAttribute('aria-expanded')).toBe('true');
    expect(dialogs(container)[0].textContent).toContain('Sign in to get notified');

    act(() => { window.dispatchEvent(new window.CustomEvent(terminalOpenPanelEvent, { detail: 'trending' })); });
    expect(dialogs(container)).toHaveLength(1);
    await waitFor(() => container.querySelectorAll('[role="dialog"] ol li a').length === 2);
    const hrefs = Array.from(container.querySelectorAll<HTMLAnchorElement>('[role="dialog"] ol li a')).map(link => link.getAttribute('href'));
    expect(hrefs).toEqual([`/terminal/${poolA}`, `/terminal/${poolB}`]);
    expect(api.seen.some(entry => entry.url === '/v2/rankings/trending')).toBe(true);
    unmount(mounted);
  });

  it('renders the real feed 503 as unavailable and the PnL tracker as a sign-in prompt', async () => {
    const mounted = mount(<TerminalStatusBar poolAddress={null} />);
    const { container } = mounted;
    act(() => tool(container, 'Feed').click());
    await waitFor(() => dialogs(container)[0]?.textContent?.includes('Feed is not available yet') === true);
    expect(dialogs(container)[0].textContent).toContain('Feed is not available yet');
    act(() => tool(container, 'PnL tracker').click());
    expect(dialogs(container)[0].textContent).toContain('Sign in to see your holdings value');
    expect(Array.from(dialogs(container)[0].querySelectorAll('button')).some(button => button.textContent === 'Sign in')).toBe(true);
    unmount(mounted);
  });

  it('renders an empty trending snapshot as an empty state, not an error', async () => {
    rankingsEmpty = true;
    try {
      const mounted = mount(<TerminalStatusBar poolAddress={null} />);
      act(() => tool(mounted.container, 'Trending').click());
      await waitFor(() => dialogs(mounted.container)[0]?.textContent?.includes('No coins trending yet') === true);
      const dialog = dialogs(mounted.container)[0];
      expect(dialog.textContent).toContain('Rankings appear as soon as coins trade.');
      expect(dialog.querySelector('[role="alert"]')).toBeNull();
      expect(dialog.textContent).not.toMatch(/IF_RANKINGS|did not load|failed/);
      unmount(mounted);
    } finally {
      rankingsEmpty = false;
    }
  });

  it('shows the trending unavailable state when IF_RANKINGS answers 404', async () => {
    rankingsServed = false;
    const mounted = mount(<TerminalStatusBar poolAddress={null} />);
    act(() => tool(mounted.container, 'Trending').click());
    await waitFor(() => dialogs(mounted.container)[0]?.textContent?.includes('Trending is not available yet') === true);
    expect(dialogs(mounted.container)[0].textContent).not.toContain('IF_RANKINGS');
    expect(mounted.container.querySelector('[role="dialog"] ol')).toBeNull();
    unmount(mounted);
    rankingsServed = true;
  });

  it('resets the frame column layout from the gear menu through the sidiora:terminal-reset-columns event', () => {
    let columns: ReturnType<typeof useTerminalColumns> | null = null;
    function FrameColumns() {
      useResetColumnsEvent();
      columns = useTerminalColumns();
      return null;
    }
    act(() => resetTerminalColumns());
    const mounted = mount(<><FrameColumns /><TerminalStatusBar poolAddress={null} /></>);
    act(() => { columns?.setLeftWidth(400); columns?.toggleRight(); });
    expect(columns).toMatchObject({ leftWidth: 400, rightCollapsed: true });
    const gear = mounted.container.querySelector<HTMLButtonElement>('button[aria-label="Terminal settings"]');
    expect(gear).not.toBeNull();
    act(() => gear?.click());
    const item = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(node => node.textContent === 'Reset column layout');
    expect(item).toBeDefined();
    act(() => item?.click());
    expect(columns).toMatchObject({ leftWidth: columnLimits.leftDefault, rightWidth: columnLimits.rightDefault, leftCollapsed: false, rightCollapsed: false });
    unmount(mounted);
  });
});
