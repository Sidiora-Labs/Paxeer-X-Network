import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { ConnectedPortfolioV3Response } from '@kindle/sdk-v2';
import { loadable } from '@/core/market/unsupported';

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

const wallet = '0x' + 'ab'.repeat(20);
const other = '0x' + 'cd'.repeat(20);
const unenrolled = '0x' + 'ee'.repeat(20);
const expired = '0x' + 'ef'.repeat(20);
const tokenA = '0x' + '11'.repeat(20);
const tokenB = '0x' + '22'.repeat(20);
const watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };

type Position = ConnectedPortfolioV3Response['data']['positions'][number];

function position(token: string, balanceRaw: string, unrealizedPnlRaw: string | null, extra: Partial<Position> = {}): Position {
  return {
    positionId: `125:${wallet}:${token}`, chainId: '125', wallet, tokenAddress: token, balanceRaw,
    basisCoverage: { numerator: '1', denominator: '1' }, basisQuality: 'complete', knownQuantityRaw: balanceRaw,
    knownRealizedSubtotalUsdlRaw: '0', knownRemainingCostUsdlRaw: '0', markPrice: null, pricingQuality: 'unpriced',
    realizedBasisQuality: 'complete', realizedPnlRaw: '0', remainingCostUsdlRaw: '0', sourceWatermark: watermark,
    unknownQuantityRaw: '0', unrealizedPnlRaw, ...extra,
  };
}

function portfolio(owner: string, positions: Position[]): ConnectedPortfolioV3Response {
  return {
    algorithm: 'proportional_floor_v1', decimals: 6, quoteAsset: 'USDL', version: 3, positionErrors: {},
    data: {
      positions,
      summary: { wallet: owner, basisQuality: 'complete', completeValueUsdlRaw: '1843410000', pricedSubtotalUsdlRaw: '1843410000', knownRealizedSubtotalUsdlRaw: '0', positionCount: positions.length, pricingQuality: 'priced', realizedPnlRaw: '-2500000', snapshotId: 'snap-1', unpricedAssetCount: 0, watermark },
    },
  };
}

/** interfaces.Error as l17 writeFailure encodes it (core/pnl-tracker/internal/production/l17/http_handlers.go). */
function failure(code: string, retryable: boolean) {
  return { code, message: 'Portfolio request unavailable', requestId: 'AAAAAAAAAAAAAAAAAAAAAA', retryable, fieldErrors: [], details: null, retryAfterMs: null };
}

const pnl = await serve(url => {
  if (url.startsWith(`/v3/wallets/${wallet}/portfolio`)) return { status: 200, body: portfolio(wallet, [position(tokenA, '5', '100')]) };
  if (url.startsWith(`/v3/wallets/${other}/portfolio`)) return { status: 200, body: portfolio(wallet, []) };
  if (url.startsWith(`/v3/wallets/${unenrolled}/portfolio`)) return { status: 404, body: failure('NOT_FOUND', false) };
  if (url.startsWith(`/v3/wallets/${expired}/portfolio`)) return { status: 409, body: failure('REVISION_MISMATCH', false) };
  return { status: 404 };
});
process.env.NEXT_PUBLIC_PNL_API_URL = pnl.origin;
const transport = await import('./portfolio-transport');

afterAll(() => { pnl.server.close(); });

describe('portfolio window math', () => {
  it('maps 1D to 24 hourly buckets and 1W/1M to daily buckets ending at the next UTC boundary', () => {
    const now = Date.parse('2026-10-13T10:17:00.000Z');
    expect(transport.windowInterval('1D', now)).toEqual({ from: '2026-10-12T11:00:00.000Z', to: '2026-10-13T11:00:00.000Z', interval: '1h' });
    expect(transport.windowInterval('1W', now)).toEqual({ from: '2026-10-07T00:00:00.000Z', to: '2026-10-14T00:00:00.000Z', interval: '1d' });
    expect(transport.windowInterval('1M', now)).toEqual({ from: '2026-09-14T00:00:00.000Z', to: '2026-10-14T00:00:00.000Z', interval: '1d' });
  });

  it('orders the series, prefers the complete value and falls back to the priced subtotal', () => {
    const entry = (time: string, complete: string | null, priced: string) => ({ time, completeValueUsdlRaw: complete, pricedSubtotalUsdlRaw: priced, basisQuality: 'complete' as const, knownRealizedSubtotalUsdlRaw: '0', unpricedAssetCount: complete === null ? 1 : 0, watermark: { ...watermark, chainId: '125' as const, integrity: 'healthy' as const } });
    const points = transport.seriesPoints({ requestId: 'r', generatedAt: '2026-10-13T00:00:00.000Z', nextCursor: null, quality: 'partial', revision: '7', watermark: null, data: { items: [entry('2026-10-13T02:00:00.000Z', null, '900'), entry('2026-10-13T01:00:00.000Z', '1000', '1000')] } });
    expect(points).toEqual([
      { time: '2026-10-13T01:00:00.000Z', valueRaw: 1000n, complete: true },
      { time: '2026-10-13T02:00:00.000Z', valueRaw: 900n, complete: false },
    ]);
  });

  it('computes delta as last minus first bucket with sign tone', () => {
    const at = (valueRaw: bigint) => ({ time: '2026-10-13T00:00:00.000Z', valueRaw, complete: true });
    expect(transport.windowDelta([])).toBeNull();
    expect(transport.windowDelta([at(2_000_000n), at(1_500_000n)])).toEqual({ absRaw: -500_000n, pct: -25, tone: 'loss' });
    expect(transport.windowDelta([at(1_000_000n), at(1_100_000n)])).toEqual({ absRaw: 100_000n, pct: 10, tone: 'gain' });
    expect(transport.windowDelta([at(0n), at(5n)])).toEqual({ absRaw: 5n, pct: null, tone: 'gain' });
    expect(transport.windowDelta([at(7n)])).toEqual({ absRaw: 0n, pct: 0, tone: 'flat' });
  });
});

describe('positions math', () => {
  it('splits open and closed by balance', () => {
    const open = position(tokenA, '10', '5'), closed = position(tokenB, '0', null);
    expect(transport.splitPositions([open, closed])).toEqual({ open: [open], closed: [closed] });
  });

  it('sums unrealized and marks partial for an unmarked open position or a further page', () => {
    expect(transport.sumUnrealized([position(tokenA, '10', '-300'), position(tokenB, '0', null)])).toEqual({ raw: -300n, partial: false, counted: 1 });
    expect(transport.sumUnrealized([position(tokenA, '10', '200'), position(tokenB, '4', null)])).toEqual({ raw: 200n, partial: true, counted: 1 });
    expect(transport.sumUnrealized([position(tokenA, '10', '200')], true).partial).toBe(true);
  });

  it('values a position with the l17 formula and refuses stale or undecimalled marks', () => {
    const asset = { address: null, chainId: '125', currencyCode: null, kind: 'erc20' };
    const markPrice = { numerator: '3', denominator: '2', baseAsset: { ...asset, decimals: 18 }, quoteAsset: { ...asset, decimals: 6 }, source: { amountBasis: 'reduced', kind: 'pool', observedAt: watermark.observedAt, position: null, providerId: null, sourceEventId: null, sourceReference: null } };
    expect(transport.positionValueRaw(position(tokenA, '2000000000000000000', null, { markPrice, pricingQuality: 'priced' }))).toBe(3_000_000n);
    expect(transport.positionValueRaw(position(tokenA, '2000000000000000000', null, { markPrice, pricingQuality: 'stale' }))).toBeNull();
    expect(transport.positionValueRaw(position(tokenA, '1', null, { markPrice: { ...markPrice, baseAsset: { ...asset, decimals: null } }, pricingQuality: 'priced' }))).toBeNull();
    expect(transport.positionValueRaw(position(tokenA, '1', null))).toBeNull();
  });

  it('ranks closed positions by served realized profit and skips withheld realized figures', () => {
    const rows = [
      position(tokenA, '0', null, { realizedPnlRaw: '2500000' }),
      position(tokenB, '0', null, { realizedPnlRaw: '-1000000' }),
      position(other, '0', null, { realizedPnlRaw: null, realizedBasisQuality: 'partial' }),
      position(unenrolled, '9', '10', { realizedPnlRaw: '9000000' }),
    ];
    expect(transport.topTrades(rows).map(trade => trade.realizedRaw)).toEqual([2_500_000n, -1_000_000n]);
    expect(transport.topTrades(rows, 1)).toHaveLength(1);
    expect(transport.topTrades([])).toEqual([]);
  });
});

describe('pnl-tracker reads against a real HTTP origin', () => {
  beforeAll(() => { pnl.paths.length = 0; });

  it('reads and validates the public portfolio projection', async () => {
    const value = await transport.readPublicPortfolio(wallet.toUpperCase().replace('0X', '0x'));
    expect(value.data.summary.completeValueUsdlRaw).toBe('1843410000');
    expect(value.data.positions).toHaveLength(1);
    expect(pnl.paths).toContain(`/v3/wallets/${wallet}/portfolio?limit=50`);
  });

  it('rejects a public portfolio that belongs to another wallet', async () => {
    await expect(transport.readPublicPortfolio(other)).rejects.toThrow('Portfolio ownership or page bound mismatch');
  });

  it('maps a 404 NOT_FOUND (no snapshot yet) and a 409 REVISION_MISMATCH (expired snapshot) onto honest states', async () => {
    const asLoadable = async (address: string) => {
      const error = await transport.readPublicPortfolio(address).catch((caught: unknown) => caught);
      return loadable({ status: 'error', data: undefined, error: error as Error, refetch: () => Promise.resolve({} as never) }, transport.publicPortfolioOperation);
    };
    const missing = await asLoadable(unenrolled);
    expect(missing).toMatchObject({ state: 'unsupported', status: 404, message: 'NOT_FOUND' });
    expect(transport.portfolioIssue(missing, false)).toMatchObject({ kind: 'no-snapshot', title: 'No portfolio yet' });
    expect(transport.portfolioIssue(missing, true)).toMatchObject({ kind: 'no-snapshot', title: 'Your portfolio is being built' });
    const stale = await asLoadable(expired);
    expect(stale.state).toBe('error');
    expect(transport.portfolioIssue(stale, false)).toMatchObject({ kind: 'stale-snapshot', title: 'Portfolio out of date' });
  });

  it('only treats 401 and 403 as sign-in prompts for the owner, and leaves other failures to the widget', () => {
    const failed = (status: number, code: string) => ({ state: 'error' as const, error: Object.assign(new Error(code), { status, code }), retry: () => undefined });
    expect(transport.portfolioIssue(failed(403, 'HTTP_403'), true)).toMatchObject({ kind: 'reauth' });
    expect(transport.portfolioIssue(failed(401, 'HTTP_401'), true)).toMatchObject({ kind: 'signed-out' });
    expect(transport.portfolioIssue(failed(403, 'SCOPE_DENIED'), false)).toBeNull();
    expect(transport.portfolioIssue(failed(500, 'HTTP_500'), true)).toBeNull();
    expect(transport.portfolioIssue({ state: 'unsupported', operation: 'x', status: 404, message: 'HTTP_404' }, false)).toBeNull();
    expect(transport.portfolioIssue({ state: 'loading' }, true)).toBeNull();
  });

  it('marks visitor history as unsupported without a request', () => {
    expect(transport.noPublicHistory).toMatchObject({ state: 'unsupported', status: null, operation: transport.historyOperation });
  });
});
