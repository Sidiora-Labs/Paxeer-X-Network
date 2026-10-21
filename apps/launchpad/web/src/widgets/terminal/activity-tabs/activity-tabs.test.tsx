import { createServer, type IncomingHttpHeaders, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { ConnectedPortfolioV3Response } from '@kindle/sdk-v2';
import type { Order, Trade } from '../../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';

// Node fetch/AbortController plus the browser globals Node lacks, taken from a real jsdom window
// (same recipe as widgets/profile/portfolio-widgets.test.tsx) so SdkClient talks to a real local origin.
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

const me = '0x' + 'a1'.repeat(20);
const dev = '0x' + 'd3'.repeat(20);
const router = '0x' + 'e5'.repeat(20);
const stranger = '0x' + 'b2'.repeat(20);
const pool = '0x' + '33'.repeat(20);
const otherPool = '0x' + '44'.repeat(20);
const token = '0x' + '11'.repeat(20);
const otherToken = '0x' + '22'.repeat(20);
const watermark = { chainId: '125' as const, height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy' as const, observedAt: '2026-10-12T15:00:00.000Z' };
const orderHere = '7f1c2a3b-4d5e-4f60-8a1b-2c3d4e5f6a7b';
const orderElsewhere = '0a1b2c3d-4e5f-4a6b-9c7d-8e9f0a1b2c3d';

function trade(height: number, side: 'buy' | 'sell', sender: string, recipient: string | null, attribution: Trade['recipientAttribution']): Trade {
  return {
    ammInputAfterFeeRaw: null, codeEpoch: 'v1', emittingContract: pool, eventId: `ev-${height}`, feeAssetAddress: token, feeAssetDecimals: 6,
    feeDisposition: 'distributed_usdl', feeRaw: '0', inputDerivationQuality: 'exact', paidInputRaw: null, poolAddress: pool,
    position: { blockHash: '0x' + '7'.repeat(64), chainId: '125', height: String(height), logIndex: '0', transactionHash: '0x' + height.toString(16).padStart(64, '0'), transactionIndex: '0' },
    pricedInputRaw: null, recipient, recipientAttribution: attribution, recipientEvidenceRefs: [], sender, side, tokenAmountRaw: '2500000000', usdlAmountRaw: '1250000',
  };
}

const fills: Trade[] = [
  trade(100, 'buy', me, me, 'exact_receipt'),
  trade(101, 'sell', router, dev, 'qualified_calldata'),
  trade(102, 'buy', router, me, 'unavailable'),
  trade(103, 'buy', dev, dev, 'exact_receipt'),
  trade(104, 'sell', stranger, stranger, 'exact_receipt'),
];

type Position = ConnectedPortfolioV3Response['data']['positions'][number];
function position(tokenAddress: string, balanceRaw: string): Position {
  return {
    positionId: `pos-${tokenAddress.slice(-4)}`, chainId: '125', wallet: me, tokenAddress, balanceRaw,
    basisCoverage: { numerator: '1', denominator: '1' }, basisQuality: 'partial', knownQuantityRaw: balanceRaw,
    knownRealizedSubtotalUsdlRaw: '0', knownRemainingCostUsdlRaw: '4000000', markPrice: null, pricingQuality: 'unpriced',
    realizedBasisQuality: 'complete', realizedPnlRaw: '1500000', remainingCostUsdlRaw: '4000000', sourceWatermark: watermark,
    unknownQuantityRaw: '0', unrealizedPnlRaw: null,
  };
}
const portfolio: ConnectedPortfolioV3Response = {
  algorithm: 'average-cost-v1', decimals: 6, quoteAsset: 'USDL', version: 3, positionErrors: {},
  data: { positions: [position(token, '7000000'), position(otherToken, '9000000')], summary: { wallet: me, basisQuality: 'partial', completeValueUsdlRaw: null, pricedSubtotalUsdlRaw: '0', knownRealizedSubtotalUsdlRaw: '0', positionCount: 2, pricingQuality: 'partial', realizedPnlRaw: '1500000', snapshotId: 'snap-1', unpricedAssetCount: 2, watermark } },
};

function order(orderId: string, poolAddress: string, status: Order['status'] = 'active'): Order {
  return { chainOrderId: null, inputRaw: '5000000', kind: 'limit', orderId, poolAddress, status, triggerPrice: null, wallet: me };
}
function envelope<T>(data: T) {
  return { data, generatedAt: '2026-10-13T10:00:00.000Z', nextCursor: null, quality: 'complete', requestId: '5b8f0c1e-2d3a-4b5c-8d6e-7f8091a2b3c4', revision: null, watermark };
}

interface Seen { method: string; url: string; headers: IncomingHttpHeaders }
const seen: Seen[] = [];
let cancelled = false;
const server: Server = createServer((request, response) => {
  const url = request.url ?? '';
  seen.push({ method: request.method ?? 'GET', url, headers: request.headers });
  const reply = (status: number, body?: unknown) => {
    response.statusCode = status;
    if (body === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(body));
  };
  if (request.method === 'GET' && url.startsWith('/v3/me/portfolio')) return reply(200, portfolio);
  if (request.method === 'GET' && url.startsWith('/v2/me/orders?')) return reply(200, envelope({ items: cancelled ? [order(orderElsewhere, otherPool)] : [order(orderHere, pool), order(orderElsewhere, otherPool)] }));
  if (request.method === 'POST' && url === `/v2/me/orders/${orderHere}:cancel`) { cancelled = true; return reply(200, envelope({ order: order(orderHere, pool, 'cancelled'), transactionRequest: null })); }
  return reply(404);
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { accountEpoch, clearAccountSession, installAccountSession, readPortfolio } = await import('@/widgets/profile/account-transport');
const { fillFilterOptions, fillMaker, matchesFillFilter, visibleFills, arrivedKeys } = await import('./fills');
const { FillsTable } = await import('./trades-panel');
const { PositionsTable, positionsForToken } = await import('./positions-panel');
const { OrdersPanel, cancelOrder, ordersForPool } = await import('./orders-panel');
const { useInstantTrade, instantTradeEvent, instantTradeStorageKey } = await import('./instant-trade');
const { holderCountOf } = await import('./index');
const { operationsFor } = await import('@/core/market/operations');
const { browserSessionClient } = await import('@/core/market/client');

afterAll(() => { clearAccountSession(false); server.close(); });
beforeEach(() => { seen.length = 0; });

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function signIn() {
  installAccountSession({ accessExpiresAt: new Date(Date.now() + 3_600_000).toISOString(), deviceLabel: 'Web browser', refreshExpiresAt: new Date(Date.now() + 86_400_000).toISOString(), revokedAt: null, scopes: ['account'], sessionId: 'sess-terminal', wallet: me }, 'access-token-1', accountEpoch());
}

function mount(node: React.ReactElement) {
  const queryClient: QueryClientType = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => { root.render(<QueryClientProvider client={queryClient}>{node}</QueryClientProvider>); });
  return { root, container, queryClient };
}
function unmount(view: ReturnType<typeof mount>) {
  act(() => view.root.unmount());
  view.queryClient.clear();
  view.container.remove();
}
async function waitFor(check: () => boolean, timeoutMs = 4000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}
const classesOf = (element: Element) => Array.from(element.querySelectorAll('[class]')).flatMap(node => (node.getAttribute('class') ?? '').split(/\s+/));

describe('fill filters', () => {
  const wallets = { mine: me, dev };

  it('Mine keeps fills where the connected wallet is the sender or the attributed recipient', () => {
    const heights = fills.filter(row => matchesFillFilter(row, 'mine', wallets)).map(row => row.position.height);
    expect(heights).toEqual(['100']);
  });

  it('ignores an unattributed recipient, so a routed fill without proof is not counted as mine', () => {
    expect(matchesFillFilter(fills[2], 'mine', wallets)).toBe(false);
    expect(fillMaker(fills[2])).toBe(router);
  });

  it('Dev keeps fills where the pool creator is the sender or the attributed recipient', () => {
    expect(visibleFills(fills, 'dev', wallets, 'desc').map(row => row.position.height)).toEqual(['103', '101']);
    expect(fillMaker(fills[1])).toBe(dev);
  });

  it('All sorts by chain position in either direction; KOL and Tracked never match and are disabled with reasons', () => {
    expect(visibleFills(fills, 'all', wallets, 'asc').map(row => row.position.height)).toEqual(['100', '101', '102', '103', '104']);
    expect(visibleFills(fills, 'kol', wallets, 'desc')).toEqual([]);
    const options = fillFilterOptions({ mine: null, dev: null });
    expect(options.find(option => option.value === 'kol')?.unavailable).toBe('Coming soon');
    expect(options.find(option => option.value === 'tracked')?.unavailable).toBe('Coming soon');
    expect(options.find(option => option.value === 'mine')?.unavailable).not.toBeNull();
    expect(fillFilterOptions(wallets).find(option => option.value === 'dev')?.unavailable).toBeNull();
  });

  it('flags only fills that arrived after the first render', () => {
    expect(arrivedKeys(null, fills).size).toBe(0);
    const previous = new Set(fills.slice(1).map(row => `125:${row.position.transactionHash}:0`));
    expect([...arrivedKeys(previous, fills)]).toEqual([`125:${fills[0].position.transactionHash}:0`]);
  });

  it('FillsTable renders the Dev filter rows with side, scaled amount, USDL total and explorer link, without strokes', async () => {
    const view = mount(<FillsTable rows={fills} token={{ decimals: 6, decimalsQuality: 'verified' }} filter="dev" wallets={wallets} contiguous />);
    const rows = view.container.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('103');
    expect(rows[0].textContent).toContain('buy');
    expect(rows[0].textContent).toContain('2.5K');
    expect(rows[0].textContent).toContain('1.25 USDL');
    expect(rows[1].querySelector('a[href^="https://paxscan.paxeer.app/tx/0x"]')).not.toBeNull();
    await waitFor(() => seen.some(entry => entry.url.startsWith('/v3/profiles/')));
    expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
    unmount(view);
  });

  it('FillsTable renders an empty pair as an empty state, not an error', () => {
    const view = mount(<FillsTable rows={[]} token={{ decimals: 6, decimalsQuality: 'verified' }} filter="all" wallets={wallets} contiguous={false} />);
    expect(view.container.textContent).toContain('No trades yet');
    expect(view.container.textContent).toContain('Trades appear here as soon as someone buys or sells this coin.');
    expect(view.container.querySelector('[role="alert"]')).toBeNull();
    unmount(view);
  });

  it('FillsTable says when no retained fill matches', () => {
    const view = mount(<FillsTable rows={fills} token={{ decimals: 6, decimalsQuality: 'verified' }} filter="mine" wallets={{ mine: stranger.replace('b2', 'c9'), dev }} contiguous />);
    expect(view.container.textContent).toContain('No trades match this filter');
    unmount(view);
  });
});

describe('positions for this pair (real pnl-tracker read)', () => {
  it('reads the signed-in portfolio and keeps only the pool token, with quality tags', async () => {
    signIn();
    const page = await readPortfolio();
    const authorization = seen.find(entry => entry.url.startsWith('/v3/me/portfolio'))?.headers.authorization;
    expect(authorization).toBe('Bearer access-token-1');
    const matching = positionsForToken(page.data.positions, token.toUpperCase().replace('0X', '0x'));
    expect(matching.map(row => row.tokenAddress)).toEqual([token]);
    const view = mount(<PositionsTable positions={matching} token={{ decimals: 6, decimalsQuality: 'verified' }} symbol="LNTN" />);
    const text = view.container.textContent ?? '';
    expect(text).toContain('LNTN');
    expect(text).toContain('Cost incomplete');
    expect(text).toContain('No price yet');
    expect(text).toContain('4.00 USDL');
    expect(text).toContain('+1.50 USDL');
    expect(view.container.querySelectorAll('tbody tr')).toHaveLength(1);
    unmount(view);
    clearAccountSession(false);
  });
});

describe('orders for this pair (real session SdkClient)', () => {
  it('cancelOrder posts IF_ORDER_CANCEL with bearer and idempotency key', async () => {
    cancelled = false;
    signIn();
    const response = await cancelOrder(operationsFor(browserSessionClient()), orderHere, '11111111-2222-4333-8444-555555555555');
    expect(response.data.order.status).toBe('cancelled');
    const call = seen.find(entry => entry.method === 'POST');
    expect(call?.url).toBe(`/v2/me/orders/${orderHere}:cancel`);
    expect(call?.headers.authorization).toBe('Bearer access-token-1');
    expect(call?.headers['idempotency-key']).toBe('11111111-2222-4333-8444-555555555555');
    expect(ordersForPool([order(orderHere, pool), order(orderElsewhere, otherPool)], pool.toUpperCase().replace('0X', '0x')).map(row => row.orderId)).toEqual([orderHere]);
    clearAccountSession(false);
  });

  it('lists only this pool, confirms, cancels and refetches', async () => {
    cancelled = false;
    signIn();
    const view = mount(<OrdersPanel poolAddress={pool} />);
    await waitFor(() => view.container.querySelector(`[data-order-id="${orderHere}"]`) !== null);
    expect(seen.find(entry => entry.url.startsWith('/v2/me/orders?'))?.url).toBe('/v2/me/orders?status=active&limit=100');
    expect(view.container.querySelector(`[data-order-id="${orderElsewhere}"]`)).toBeNull();
    const cancel = Array.from(view.container.querySelectorAll('button')).find(button => button.textContent === 'Cancel') as HTMLButtonElement;
    act(() => cancel.click());
    expect(seen.some(entry => entry.method === 'POST')).toBe(false);
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('Cancel order')) as HTMLButtonElement;
    expect(confirm).toBeDefined();
    act(() => confirm.click());
    await waitFor(() => seen.some(entry => entry.method === 'POST'));
    const post = seen.find(entry => entry.method === 'POST');
    expect(post?.url).toBe(`/v2/me/orders/${orderHere}:cancel`);
    expect(post?.headers['idempotency-key']).toMatch(/^[0-9a-f-]{36}$/);
    await waitFor(() => view.container.querySelector(`[data-order-id="${orderHere}"]`) === null && (view.container.textContent?.includes('No open orders') ?? false));
    expect(seen.filter(entry => entry.url.startsWith('/v2/me/orders?')).length).toBeGreaterThanOrEqual(2);
    unmount(view);
    clearAccountSession(false);
  });

  it('signed out shows a sign-in empty state and makes no request', () => {
    const view = mount(<OrdersPanel poolAddress={pool} />);
    expect(view.container.textContent).toContain('Sign in to see your orders');
    expect(seen).toEqual([]);
    unmount(view);
  });
});

describe('instant trade toggle and holder count', () => {
  function Probe() {
    const [enabled, setEnabled] = useInstantTrade();
    return <button type="button" onClick={() => setEnabled(!enabled)}>{enabled ? 'on' : 'off'}</button>;
  }

  it('persists the toggle and announces every change on the window', () => {
    window.localStorage.removeItem(instantTradeStorageKey);
    const events: boolean[] = [];
    const listener = (event: Event) => events.push((event as CustomEvent<{ enabled: boolean }>).detail.enabled);
    window.addEventListener(instantTradeEvent, listener);
    const view = mount(<Probe />);
    const button = view.container.querySelector('button') as HTMLButtonElement;
    expect(button.textContent).toBe('off');
    act(() => button.click());
    expect(button.textContent).toBe('on');
    expect(window.localStorage.getItem(instantTradeStorageKey)).toBe('true');
    unmount(view);
    const again = mount(<Probe />);
    expect(again.container.querySelector('button')?.textContent).toBe('on');
    unmount(again);
    window.removeEventListener(instantTradeEvent, listener);
    expect(events).toEqual([false, true, true]);
  });

  it('shows the holder count only when the stats projection serves it', () => {
    expect(holderCountOf('3')).toBe(3);
    expect(holderCountOf(null)).toBeUndefined();
    expect(holderCountOf(undefined)).toBeUndefined();
  });
});
