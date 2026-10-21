import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act, Profiler } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, describe, expect, it } from 'vitest';
import { SdkClient } from '@kindle/sdk';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

// Node fetch plus browser globals from a real jsdom window (same recipe as activity-tabs.test.tsx) so the
// SDK, the legacy resolver fetch and wagmi's RPC transport (core/wagmi-config via the app's own
// AppProviders tree) all reach the local origin below.
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

// The real chain-125 pool, identified by core/api's recorded IF_POOL_DETAIL bytes (core-api-wire.json `v2Reads`).
const pool = v2Reads.pools.real;
const poolDetail = v2Reads.responses.poolDetailReal;

const seen: string[] = [];
const server: Server = createServer((request, response) => {
  const url = request.url ?? '';
  seen.push(`${request.method} ${url}`);
  request.resume();
  request.on('end', () => {
    if (request.method === 'GET' && url === `/v2/pools/${pool}`) {
      response.writeHead(poolDetail.status, poolDetail.headers);
      response.end(JSON.stringify(poolDetail.body));
      return;
    }
    response.statusCode = 404;
    response.end();
  });
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL', 'RPC_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;
process.env.NEXT_PUBLIC_STREAM_ORIGIN = 'ws://127.0.0.1:1';

const { createRoot } = await import('react-dom/client');
const AppProviders = (await import('@/ui/providers/app-providers')).default;
const { MarketClientContext } = await import('@/core/market/client');
const { TerminalLiveProvider, useTerminalLive } = await import('./live-context');
const TerminalLeftRail = (await import('./left-rail')).default;
const TerminalTokenStrip = (await import('./token-strip')).default;
const TerminalChartCard = (await import('./chart-card')).default;
const TerminalActivityTabs = (await import('./activity-tabs')).default;
const TerminalRightRail = (await import('./right-rail')).default;
const TerminalStatusBar = (await import('./status-bar')).default;

afterAll(() => { server.close(); });

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

interface Mounted { root: Root; container: HTMLDivElement; commits: () => number; errors: string[]; restore: () => void }

function mount(node: React.ReactElement): Mounted {
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const errors: string[] = [];
  let count = 0;
  const original = console.error;
  console.error = (...args: unknown[]) => { errors.push(args.map(String).join(' ')); original(...args); };
  act(() => {
    root.render(
      <Profiler id="terminal" onRender={() => { count += 1; }}>
        <AppProviders>
          <MarketClientContext.Provider value={client}>{node}</MarketClientContext.Provider>
        </AppProviders>
      </Profiler>,
    );
  });
  return { root, container, commits: () => count, errors, restore: () => { console.error = original; } };
}

function unmount({ root, container, restore }: Mounted) {
  act(() => root.unmount());
  restore();
  container.remove();
}

async function settle(ms: number) {
  await act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); });
}

async function waitFor(check: () => boolean, timeoutMs = 8000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await settle(25);
  }
}

function SessionProbe({ onLive }: { onLive: (session: unknown) => void }) {
  onLive(useTerminalLive(pool).session);
  return null;
}

describe('terminal coin view, rendered end to end with real providers', () => {
  it('renders the right rail inside the shared live session without crashing or looping', async () => {
    const view = mount(<TerminalLiveProvider poolAddress={pool}><TerminalRightRail poolAddress={pool} /></TerminalLiveProvider>);
    await waitFor(() => /Similar coins/.test(view.container.textContent ?? ''));
    const before = view.commits();
    await settle(1500);
    expect(view.commits() - before).toBeLessThan(60);
    expect(view.container.textContent).toContain('Coin info and safety');
    expect(view.container.querySelector('[aria-label="Active trade preset"]')).not.toBeNull();
    expect(view.container.innerHTML).not.toMatch(BORDER);
    expect(view.errors.filter(line => /Maximum update depth|is not mounted|Cannot read prop/.test(line))).toEqual([]);
    unmount(view);
  });

  it('composes every coin-view panel on one market session and resolves the pair from the served identity', async () => {
    const sessions = new Set<unknown>();
    const view = mount(
      <TerminalLiveProvider poolAddress={pool}>
        <SessionProbe onLive={session => sessions.add(session)} />
        <TerminalLeftRail activePool={pool} />
        <TerminalTokenStrip poolAddress={pool} />
        <TerminalChartCard poolAddress={pool} />
        <TerminalActivityTabs poolAddress={pool} />
        <TerminalRightRail poolAddress={pool} />
        <TerminalStatusBar poolAddress={pool} />
      </TerminalLiveProvider>,
    );
    await waitFor(() => seen.includes(`GET /v2/pools/${pool}`) && view.container.querySelector('section[aria-label="Pair activity"]') !== null);
    await waitFor(() => /Similar coins/.test(view.container.textContent ?? ''));
    const before = view.commits();
    await settle(1500);
    expect(view.commits() - before).toBeLessThan(80);
    expect(sessions.size).toBe(1);
    const text = view.container.textContent ?? '';
    expect(text).toContain('Liquidity');
    expect(text).toContain('Trades');
    expect(view.container.querySelector('[aria-label="Terminal status bar"]')).not.toBeNull();
    expect(view.container.querySelector('[role="status"][aria-label^="Connection"]')?.getAttribute('aria-label')).not.toContain('No pair stream is open');
    expect(view.container.innerHTML).not.toMatch(BORDER);
    expect(view.errors.filter(line => /Maximum update depth|is not mounted|Cannot read prop/.test(line))).toEqual([]);
    unmount(view);
  });
});
