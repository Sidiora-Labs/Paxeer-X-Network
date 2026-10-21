/**
 * The signed-in owner's portfolio reads go to the production pnl-tracker
 * routes behind identity + enrollment (core/pnl-tracker/internal/app/app.go:
 * GET /v3/me/portfolio, GET /v2/me/portfolio/history; l16 EnrollmentMiddleware).
 * These suites run the real widgets with a real wagmi connection and a real
 * account session against a local HTTP origin that answers the way those
 * handlers do: 200 with the l17 v3 body, 403 (plain text) when the sign-in
 * proof is older than 15 minutes, 404 NOT_FOUND while the first snapshot is
 * being built, and 401 when the gateway identity call fails.
 */
import { createServer, type IncomingMessage } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import type { ConnectedPortfolioV3Response } from '@kindle/sdk-v2';

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

const me = '0x1111111111111111111111111111111111111111';
const tokenA = '0x' + '11'.repeat(20).slice(0, 38) + 'aa';
const tokenB = '0x' + '22'.repeat(20);
const usdl = '0x85FcD13735F4309833A503EE804ea32395851479';
const blockHash = '0x' + 'a'.repeat(64);
const watermark = { chainId: '125', height: '33835096', blockHash, projectionRevision: '3', integrity: 'healthy', observedAt: '2026-10-13T09:00:00.000Z' };
type Position = ConnectedPortfolioV3Response['data']['positions'][number];

/** A position as l17 Derive leaves it (core/pnl-tracker/internal/production/l17/domain.go). */
const held: Position = {
  positionId: `125:${me}:${tokenA}`, chainId: '125', wallet: me, tokenAddress: tokenA, balanceRaw: '2000000000000000000',
  knownQuantityRaw: '2000000000000000000', unknownQuantityRaw: '0', knownRemainingCostUsdlRaw: '2500000', basisCoverage: { numerator: '2000000000000000000', denominator: '2000000000000000000' },
  remainingCostUsdlRaw: '2500000', realizedPnlRaw: '400000', knownRealizedSubtotalUsdlRaw: '400000', unrealizedPnlRaw: '500000',
  basisQuality: 'complete', realizedBasisQuality: 'complete', pricingQuality: 'priced',
  markPrice: {
    numerator: '3', denominator: '2',
    baseAsset: { kind: 'erc20', chainId: '125', address: tokenA, currencyCode: null, decimals: 18 },
    quoteAsset: { kind: 'erc20', chainId: '125', address: usdl, currencyCode: 'USDL', decimals: 6 },
    source: { kind: 'post_swap_spot', amountBasis: 'effective_reserves', sourceEventId: 'swap-33835090-1', observedAt: '2026-10-13T08:59:00.000Z', providerId: null, sourceReference: null, position: { chainId: '125', height: '33835090', transactionIndex: '0', logIndex: '1', blockHash: '0x' + 'b'.repeat(64), transactionHash: '0x' + 'c'.repeat(64) } },
  },
  sourceWatermark: watermark,
};
const closed: Position = {
  ...held, positionId: `125:${me}:${tokenB}`, tokenAddress: tokenB, balanceRaw: '0', knownQuantityRaw: '0', knownRemainingCostUsdlRaw: '0', remainingCostUsdlRaw: '0',
  basisCoverage: { numerator: '1', denominator: '1' }, realizedPnlRaw: '-1200000', knownRealizedSubtotalUsdlRaw: '-1200000', unrealizedPnlRaw: null, pricingQuality: 'unpriced', markPrice: null,
};
const portfolio: ConnectedPortfolioV3Response = {
  version: 3, algorithm: 'proportional_floor_v1', decimals: 6, quoteAsset: 'USDL', positionErrors: { [closed.positionId]: ['current_valuation_unavailable'] },
  data: { positions: [held, closed], summary: { wallet: me, pricedSubtotalUsdlRaw: '3000000', completeValueUsdlRaw: '3000000', realizedPnlRaw: '-800000', knownRealizedSubtotalUsdlRaw: '-800000', basisQuality: 'complete', pricingQuality: 'priced', unpricedAssetCount: 0, positionCount: 2, snapshotId: '0b7f3f4e-7a52-4c1b-9d55-5b6f1e0c2a11', watermark } },
};
const history = { requestId: 'AAAAAAAAAAAAAAAAAAAAAA', generatedAt: '2026-10-13T09:00:00.000Z', nextCursor: null, quality: 'complete', revision: '3', watermark, data: { items: [] } };
const notFound = { code: 'NOT_FOUND', message: 'Portfolio request unavailable', requestId: 'AAAAAAAAAAAAAAAAAAAAAA', retryable: false, fieldErrors: [], details: null, retryAfterMs: null };

interface Seen { method: string; url: string; headers: IncomingMessage['headers'] }
const seen: Seen[] = [];
const server = createServer((request, response) => {
  const url = request.url ?? '';
  const method = request.method ?? 'GET';
  seen.push({ method, url, headers: request.headers });
  response.setHeader('Access-Control-Allow-Origin', '*');
  const text = (status: number, body: string) => { response.statusCode = status; response.setHeader('Content-Type', 'text/plain; charset=utf-8'); response.end(`${body}\n`); };
  const json = (status: number, body: unknown) => { response.statusCode = status; response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify(body)); };
  if (method === 'OPTIONS') { response.setHeader('Access-Control-Allow-Headers', '*'); response.statusCode = 204; response.end(); return; }
  const isOwnerRoute = url.startsWith('/v3/me/portfolio') || url.startsWith('/v2/me/portfolio/history');
  if (!isOwnerRoute) return json(404, { code: 'NOT_FOUND' });
  // l16 EnrollmentMiddleware answers with http.Error (plain text) before the handler runs.
  const token = request.headers.authorization;
  if (token === 'Bearer access-gateway-down') return text(401, 'owner identity required');
  if (token === 'Bearer access-proof-stale') return text(403, 'portfolio enrollment unavailable');
  if (token === 'Bearer access-first-build' && url.startsWith('/v3/me/portfolio')) return json(404, notFound);
  if (token !== 'Bearer access-fresh' && token !== 'Bearer access-first-build') return text(401, 'owner identity required');
  if (url.startsWith('/v3/me/portfolio?')) return json(200, portfolio);
  if (url.startsWith('/v2/me/portfolio/history?')) return json(200, history);
  return json(404, notFound);
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['PNL_API_URL', 'METADATA_API_URL', 'GATEWAY_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { connect, disconnect } = await import('wagmi/actions');
const { injected } = await import('wagmi/connectors');
const { defineChain } = await import('viem');
const { accountEpoch, accountSession, clearAccountSession, installAccountSession } = await import('./account-transport');
const { BalanceVisibilityProvider } = await import('./balance-visibility');
const { PositionsOpenClosed } = await import('./positions-open-closed');
const { PortfolioBlock } = await import('./portfolio-block');
const { TopTrades } = await import('./top-trades');
const { PositionSummary } = await import('@/widgets/terminal/right-rail/position-summary');

/** A small EIP-1193 wallet on chain 125 holding `me` (the browser wallet is outside this app). */
class TestBrowserWallet {
  private readonly listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  async request({ method }: { method: string }): Promise<unknown> {
    if (method === 'eth_chainId') return '0x7d';
    if (method === 'eth_accounts' || method === 'eth_requestAccounts') return [me];
    if (method === 'wallet_requestPermissions') return [{ parentCapability: 'eth_accounts' }];
    throw Object.assign(new Error(`Unsupported method ${method}`), { code: 4200 });
  }
  on(event: string, listener: (...args: unknown[]) => void) { (this.listeners.get(event) ?? this.listeners.set(event, new Set()).get(event)!).add(listener); return this; }
  removeListener(event: string, listener: (...args: unknown[]) => void) { this.listeners.get(event)?.delete(listener); return this; }
}
(window as unknown as { ethereum: unknown }).ethereum = new TestBrowserWallet();
const chain = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: false, multiInjectedProviderDiscovery: false });

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function signIn(accessToken: string) {
  installAccountSession({ accessExpiresAt: new Date(Date.now() + 3_600_000).toISOString(), deviceLabel: 'Web browser', refreshExpiresAt: new Date(Date.now() + 86_400_000).toISOString(), revokedAt: null, scopes: ['account'], sessionId: `sess-${accessToken}`, wallet: me }, accessToken, accountEpoch());
}
function mount(node: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => { root.render(<WagmiProvider config={wagmiConfig}><QueryClientProvider client={queryClient}><BalanceVisibilityProvider>{node}</BalanceVisibilityProvider></QueryClientProvider></WagmiProvider>); });
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
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 600)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}
function classesOf(root: ParentNode): string[] {
  return Array.from(root.querySelectorAll('[class]')).flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));
}
const ownerReads = () => seen.filter(row => row.url.startsWith('/v3/me/portfolio') || row.url.startsWith('/v2/me/portfolio/history'));

beforeAll(async () => { await connect(wagmiConfig, { connector: wagmiConfig.connectors[0]! }); });
beforeEach(() => { seen.length = 0; });
afterAll(async () => { clearAccountSession(false); await disconnect(wagmiConfig).catch(() => undefined); server.close(); });

describe('owner portfolio on the production routes', () => {
  it('reads GET /v3/me/portfolio with the Bearer token and no cookies, and lists the open position', async () => {
    signIn('access-fresh');
    const view = mount(<PositionsOpenClosed wallet={me} mode="open" isOwner />);
    await waitFor(() => view.container.querySelectorAll('ul[aria-label="Open positions"] > li').length === 1);
    const row = view.container.querySelector('ul[aria-label="Open positions"] > li')?.textContent ?? '';
    expect(row).toContain('3.00 USDL');
    expect(row).toContain('+0.50 USDL');
    const read = ownerReads().find(entry => entry.url === '/v3/me/portfolio?limit=50');
    expect(read?.headers.authorization).toBe('Bearer access-fresh');
    expect(read?.headers.cookie).toBeUndefined();
    expect(seen.some(entry => entry.url.startsWith('/users/') || entry.url.startsWith('/pnl/'))).toBe(false);
    unmount(view);
  });

  it('ranks the owner closed position from the same snapshot in Top trades', async () => {
    signIn('access-fresh');
    const view = mount(<TopTrades wallet={me} isOwner />);
    await waitFor(() => view.container.querySelectorAll('ol[aria-label="Top trades"] > li').length === 1);
    expect(view.container.textContent).toContain('-1.20 USDL');
    unmount(view);
  });

  it('shows Value, Remaining, Realized and Total PNL for the token in the terminal rail', async () => {
    signIn('access-fresh');
    const view = mount(<PositionSummary poolAddress={'0x' + '33'.repeat(20)} tokenAddress={tokenA} />);
    await waitFor(() => view.container.textContent?.includes('Total PNL') === true && !(view.container.textContent ?? '').includes('...'));
    const labels = Array.from(view.container.querySelectorAll('dt')).map(node => node.textContent);
    expect(labels).toEqual(['Value', 'Remaining', 'Realized', 'Total PNL']);
    const values = Array.from(view.container.querySelectorAll('dd')).map(node => node.textContent);
    expect(values).toEqual(['3.00', '2.50', '+0.40', '+0.90']);
    expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
    unmount(view);
  });

  it('asks the owner to sign in again when the proof is older than 15 minutes (403)', async () => {
    signIn('access-proof-stale');
    const positions = mount(<PositionsOpenClosed wallet={me} mode="open" isOwner />);
    await waitFor(() => positions.container.textContent?.includes('Sign in again to see your portfolio') ?? false);
    expect(positions.container.textContent).toContain('For your security');
    expect(positions.container.querySelector('[role="alert"]')).toBeNull();
    expect(accountSession()?.accessToken).toBe('access-proof-stale');
    unmount(positions);

    signIn('access-proof-stale');
    const rail = mount(<PositionSummary poolAddress={'0x' + '33'.repeat(20)} tokenAddress={tokenA} />);
    await waitFor(() => rail.container.textContent?.includes('Sign in again to see your position') ?? false);
    expect(Array.from(rail.container.querySelectorAll('dd')).map(node => node.textContent)).toEqual(['-', '-', '-', '-']);
    unmount(rail);
  });

  it('tells the owner the first snapshot is being built (404 NOT_FOUND after enrollment)', async () => {
    signIn('access-first-build');
    const view = mount(<PortfolioBlock wallet={me} isOwner />);
    await waitFor(() => view.container.textContent?.includes('Your portfolio is being built') ?? false);
    expect(view.container.textContent).not.toMatch(/NOT_FOUND|404/);
    expect(ownerReads().every(entry => entry.headers.authorization === 'Bearer access-first-build')).toBe(true);
    unmount(view);
  });

  it('falls back to the sign-in prompt when the gateway identity call fails (401 clears the session)', async () => {
    signIn('access-gateway-down');
    const view = mount(<PositionsOpenClosed wallet={me} mode="closed" isOwner />);
    await waitFor(() => view.container.textContent?.includes('Sign in to see your positions') ?? false);
    expect(accountSession()).toBeNull();
    unmount(view);
  });
});
