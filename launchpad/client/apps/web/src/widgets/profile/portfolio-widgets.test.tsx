import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, describe, expect, it } from 'vitest';
import type { ConnectedPortfolioV3Response } from '@kindle/sdk-v2';

// Same environment recipe as leaderboard-panel.test.tsx: node fetch/AbortController with the browser
// globals Node lacks taken from a real jsdom window.
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

type Responder = (url: string) => { status: number; body?: unknown };
function serve(responder: Responder): Promise<{ server: Server; origin: string; paths: string[] }> {
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
    server.listen(0, '127.0.0.1', () => resolve({ server, origin: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, paths }));
  });
}

const trader = '0x' + 'ab'.repeat(20);
const fresh = '0x' + 'ef'.repeat(20);
const missing = '0x' + '99'.repeat(20);
const expired = '0x' + 'cd'.repeat(20);
const unserved = '0x' + '88'.repeat(20);
const tokenA = '0x' + '11'.repeat(20);
const tokenB = '0x' + '22'.repeat(20);
const poolA = '0x' + '33'.repeat(20);
const poolB = '0x' + '44'.repeat(20);
const watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };
type Position = ConnectedPortfolioV3Response['data']['positions'][number];

function position(wallet: string, token: string, balanceRaw: string, realizedPnlRaw: string | null, unrealizedPnlRaw: string | null): Position {
  return {
    positionId: `125:${wallet}:${token}`, chainId: '125', wallet, tokenAddress: token, balanceRaw,
    basisCoverage: { numerator: '1', denominator: '1' }, basisQuality: 'complete', knownQuantityRaw: balanceRaw,
    knownRealizedSubtotalUsdlRaw: realizedPnlRaw ?? '0', knownRemainingCostUsdlRaw: '0', markPrice: null, pricingQuality: 'unpriced',
    realizedBasisQuality: 'complete', realizedPnlRaw, remainingCostUsdlRaw: '0', sourceWatermark: watermark,
    unknownQuantityRaw: '0', unrealizedPnlRaw,
  };
}

function portfolio(wallet: string, positions: Position[], completeValueUsdlRaw: string | null, realizedPnlRaw: string | null): ConnectedPortfolioV3Response {
  return {
    algorithm: 'proportional_floor_v1', decimals: 6, quoteAsset: 'USDL', version: 3, positionErrors: {},
    data: { positions, summary: { wallet, basisQuality: 'complete', completeValueUsdlRaw, pricedSubtotalUsdlRaw: completeValueUsdlRaw ?? '0', knownRealizedSubtotalUsdlRaw: '0', positionCount: positions.length, pricingQuality: 'priced', realizedPnlRaw, snapshotId: `snap-${wallet.slice(-4)}`, unpricedAssetCount: 0, watermark } },
  };
}

/** interfaces.Error as l17 writeFailure encodes it (core/pnl-tracker/internal/production/l17/http_handlers.go). */
function failure(code: string, retryable: boolean) {
  return { code, message: 'Portfolio request unavailable', requestId: 'AAAAAAAAAAAAAAAAAAAAAA', retryable, fieldErrors: [], details: null, retryAfterMs: null };
}

const pnl = await serve(url => {
  if (url.startsWith(`/v3/wallets/${trader}/portfolio`)) return { status: 200, body: portfolio(trader, [position(trader, tokenA, '5000000000000000000', '0', '-750000'), position(trader, tokenB, '0', '2500000', null)], '1843410000', '-2500000') };
  if (url.startsWith(`/v3/wallets/${fresh}/portfolio`)) return { status: 200, body: portfolio(fresh, [], '0', '0') };
  // No snapshot until the owner signs in once (l17 service.go Latest -> pgx.ErrNoRows -> 404 NOT_FOUND).
  if (url.startsWith(`/v3/wallets/${missing}/portfolio`)) return { status: 404, body: failure('NOT_FOUND', false) };
  // Snapshot past its 24 h expires_at (Latest -> ErrRestart -> 409 REVISION_MISMATCH).
  if (url.startsWith(`/v3/wallets/${expired}/portfolio`)) return { status: 409, body: failure('REVISION_MISMATCH', false) };
  return { status: 404 };
});
const metadata = await serve(url => {
  const match = /^\/metadata\/batch\?addresses=(.*)$/.exec(url);
  if (!match) return { status: 404 };
  const addresses = decodeURIComponent(match[1]).split(',');
  return { status: 200, body: Object.fromEntries(addresses.map(address => [address, address === tokenB ? { token_address: tokenB, pool_address: poolB, name: 'Lantern', symbol: 'LNTN', decimals: 18 } : address === tokenA ? { token_address: tokenA, pool_address: poolA, decimals: 18 } : null])) };
});
process.env.NEXT_PUBLIC_PNL_API_URL = pnl.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = metadata.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { defineChain } = await import('viem');
const { BalanceVisibilityProvider, useBalanceVisibility } = await import('./balance-visibility');
const { PortfolioBlock, splitAmount } = await import('./portfolio-block');
const { PnlSummary } = await import('./pnl-summary');
const { PositionsOpenClosed, positionsEmptyCopy } = await import('./positions-open-closed');
const { TopTrades } = await import('./top-trades');
const { chartGeometry } = await import('./portfolio-chart');
const { poolPath } = await import('@/core/app-foundation/navigation');

afterAll(() => { pnl.server.close(); metadata.server.close(); });

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });

function HideBalances() {
  const { setHidden } = useBalanceVisibility();
  return <button type="button" onClick={() => setHidden(true)}>hide</button>;
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
          <BalanceVisibilityProvider><HideBalances />{node}</BalanceVisibilityProvider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return { root, container, queryClient };
}

function unmount({ root, container, queryClient }: ReturnType<typeof mount>) {
  act(() => root.unmount());
  queryClient.clear();
  container.remove();
}

async function waitFor(check: () => boolean, timeoutMs = 4000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

function classesOf(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('[class]')).flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));
}

describe('pure helpers', () => {
  it('splits the formatted value into integer and fraction parts', () => {
    expect(splitAmount('1,843.41')).toEqual({ integer: '1,843', fraction: '.41' });
    expect(splitAmount('12')).toEqual({ integer: '12', fraction: null });
  });

  it('draws a single sample as a flat line across the chart', () => {
    const geometry = chartGeometry([{ time: '2026-10-13T00:00:00.000Z', valueRaw: 5_000_000n, complete: true }], 180);
    expect(geometry.coords).toHaveLength(2);
    expect(geometry.coords[0].y).toBe(geometry.coords[1].y);
    expect(geometry.coords[0].x).toBeLessThan(geometry.coords[1].x);
    expect(chartGeometry([], 180).line).toBe('');
  });
});

describe('portfolio widgets against a real pnl-tracker origin (visitor)', () => {
  it('PortfolioBlock shows split value typography, rank unavailable, the honest history state and no Deposit', async () => {
    const view = mount(<PortfolioBlock wallet={trader} isOwner={false} />);
    await waitFor(() => view.container.textContent?.includes('1,843') ?? false);
    const text = view.container.textContent ?? '';
    expect(text).toContain('.41');
    expect(text).not.toContain('Rank unavailable');
    expect(text).toContain('Portfolio chart is not available yet');
    expect(Array.from(view.container.querySelectorAll('button')).some(button => button.textContent?.includes('Deposit'))).toBe(false);
    expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
    expect(pnl.paths.some(path => path.includes('/portfolio/history'))).toBe(false);
    unmount(view);
  });

  it('PnlSummary shows realized from the summary and unrealized summed from positions, and no unserved buy volume', async () => {
    const view = mount(<PnlSummary wallet={trader} isOwner={false} />);
    await waitFor(() => view.container.textContent?.includes('-2.50 USDL') ?? false);
    const text = view.container.textContent ?? '';
    expect(text).toContain('-0.75 USDL');
    expect(text).not.toContain('Buy volume');
    expect(pnl.paths.some(path => path.startsWith('/users/'))).toBe(false);
    unmount(view);
  });

  it('PositionsOpenClosed lists only held tokens under Open and zero-balance tokens under Closed', async () => {
    const open = mount(<PositionsOpenClosed wallet={trader} mode="open" isOwner={false} />);
    await waitFor(() => open.container.querySelector(`a[href="${poolPath(125, poolA)}"]`) !== null);
    const openRows = open.container.querySelectorAll('ul[aria-label="Open positions"] > li');
    expect(openRows).toHaveLength(1);
    expect(openRows[0].textContent).toContain('Unknown');
    expect(Array.from(open.container.querySelectorAll('a')).some(link => link.textContent === 'Sell')).toBe(false);
    unmount(open);

    const closed = mount(<PositionsOpenClosed wallet={trader} mode="closed" isOwner={false} />);
    await waitFor(() => closed.container.textContent?.includes('Lantern') ?? false);
    const closedRows = closed.container.querySelectorAll('ul[aria-label="Closed positions"] > li');
    expect(closedRows).toHaveLength(1);
    expect(closedRows[0].textContent).toContain('+2.50 USDL');
    expect(closedRows[0].textContent).toContain('complete');
    expect(closed.container.querySelector(`a[href="${poolPath(125, poolB)}"]`)).not.toBeNull();
    unmount(closed);
  });

  it('TopTrades ranks closed positions and shows the empty sentence for a fresh wallet', async () => {
    const ranked = mount(<TopTrades wallet={trader} />);
    await waitFor(() => ranked.container.textContent?.includes('+2.50 USDL') ?? false);
    expect(ranked.container.querySelectorAll('ol[aria-label="Top trades"] > li')).toHaveLength(1);
    expect(ranked.container.textContent).toContain('Lantern');
    expect(ranked.container.textContent).not.toMatch(/\dx\b/);
    unmount(ranked);

    const empty = mount(<TopTrades wallet={fresh} />);
    await waitFor(() => empty.container.textContent?.includes('No closed trades yet.') ?? false);
    unmount(empty);
  });

  it('PositionsOpenClosed renders the empty-success branch with neutral visitor copy, never an error', async () => {
    for (const mode of ['open', 'closed'] as const) {
      const view = mount(<PositionsOpenClosed wallet={fresh} mode={mode} isOwner={false} />);
      const expected = positionsEmptyCopy(mode, false);
      await waitFor(() => view.container.textContent?.includes(expected.title) ?? false);
      const text = view.container.textContent ?? '';
      expect(text).toContain(expected.body);
      expect(text).not.toMatch(/did not load|could not load|failed|IF_|unavailable/i);
      expect(view.container.querySelector('[role="alert"]')).toBeNull();
      expect(Array.from(view.container.querySelectorAll('a')).some(link => link.textContent === 'Browse coins')).toBe(false);
      unmount(view);
    }
  });

  it('gives the owner a next step on an empty open book', () => {
    expect(positionsEmptyCopy('open', true).action).toEqual({ href: '/explore', label: 'Browse coins' });
    expect(positionsEmptyCopy('open', false).action).toBeNull();
    expect(positionsEmptyCopy('closed', false).body).toBe('This wallet has not closed a position yet.');
  });

  it('renders the honest no-snapshot state for a wallet whose owner never signed in (404 NOT_FOUND)', async () => {
    for (const node of [<PositionsOpenClosed key="o" wallet={missing} mode="open" isOwner={false} />, <TopTrades key="t" wallet={missing} isOwner={false} />, <PnlSummary key="p" wallet={missing} isOwner={false} />, <PortfolioBlock key="b" wallet={missing} isOwner={false} />]) {
      const view = mount(node);
      await waitFor(() => view.container.textContent?.includes('No portfolio yet') ?? false);
      expect(view.container.textContent).toContain('this wallet signs in once');
      expect(view.container.textContent).not.toMatch(/404|NOT_FOUND|unavailable right now/);
      expect(view.container.querySelector('[role="alert"]')).toBeNull();
      expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
      unmount(view);
    }
  });

  it('renders the stale-snapshot notice for an expired snapshot (409 REVISION_MISMATCH)', async () => {
    const view = mount(<PositionsOpenClosed wallet={expired} mode="open" isOwner={false} />);
    await waitFor(() => view.container.textContent?.includes('Portfolio out of date') ?? false);
    expect(view.container.textContent).toContain('next time this wallet signs in');
    expect(view.container.querySelector('[role="alert"]')).toBeNull();
    unmount(view);
  });

  it('keeps the generic unavailable state for a 404 that is not the NOT_FOUND envelope (route not served)', async () => {
    const view = mount(<TopTrades wallet={unserved} />);
    await waitFor(() => view.container.textContent?.includes('Top trades are not available yet') ?? false);
    expect(view.container.querySelector('[data-operation]')).toBeNull();
    unmount(view);
  });

  it('masks every amount while balances are hidden', async () => {
    const view = mount(<PnlSummary wallet={trader} isOwner={false} />);
    await waitFor(() => view.container.textContent?.includes('-2.50 USDL') ?? false);
    const hide = Array.from(view.container.querySelectorAll('button')).find(button => button.textContent === 'hide');
    act(() => { hide?.click(); });
    const masked = view.container.querySelectorAll('[data-masked="true"]');
    expect(masked.length).toBe(2);
    for (const element of masked) expect(element.querySelector('[aria-hidden="true"]')).not.toBeNull();
    unmount(view);
  });
});
