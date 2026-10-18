'use client';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { V2ContractTypes } from '@kindle/sdk';
import type { ConnectedL17Position, ConnectedPortfolioV3Response } from '@kindle/sdk-v2';
import { queryKeys } from '@/core/query-keys';
import { loadable, unsupported, type Loadable, type Unsupported } from '@/core/market/unsupported';
import { pnlApiUrl } from '@/core/sdk-config';
import { retryTransientOnly } from '@/core/transient-retry';
import { normalizeAddress, rawInteger, uint256Revision } from './account-disclosure';
import { AccountApiError, accountRequest, accountSession, readPortfolio, requireAccount, type TrackedPortfolio } from './account-transport';
import { usePrivateAccountQuery } from './use-private-account-query';

export type PortfolioWindow = '1D' | '1W' | '1M';
export const portfolioWindows: readonly PortfolioWindow[] = ['1D', '1W', '1M'];
export type PnlHistoryResponse = V2ContractTypes['PnlHistoryResponse'];
export type PnlHistoryEntry = V2ContractTypes['PnlHistoryEntry'];
export type PortfolioPosition = ConnectedL17Position;

export const USDL_DECIMALS = 6;
const HOUR = 3_600_000;
const DAY = 86_400_000;

export interface HistoryInterval { from: string; to: string; interval: '1h' | '1d' }

/**
 * Canonical UTC bucket range per window: 1D = 24 hourly buckets ending at the
 * next full hour, 1W = 7 daily buckets, 1M = 30 daily buckets ending at the
 * next UTC midnight. Bounds use the millisecond ISO form the handler parses.
 */
export function windowInterval(window: PortfolioWindow, now: number = Date.now()): HistoryInterval {
  if (window === '1D') {
    const to = Math.floor(now / HOUR) * HOUR + HOUR;
    return { from: new Date(to - 24 * HOUR).toISOString(), to: new Date(to).toISOString(), interval: '1h' };
  }
  const to = Math.floor(now / DAY) * DAY + DAY;
  const days = window === '1W' ? 7 : 30;
  return { from: new Date(to - days * DAY).toISOString(), to: new Date(to).toISOString(), interval: '1d' };
}

export const historyOperation = 'IF_PNL_HISTORY GET /v2/me/portfolio/history';
export const publicPortfolioOperation = 'IF_CONNECTED_PUBLIC_PORTFOLIO_V3 GET /v3/wallets/{wallet}/portfolio';
export const privatePortfolioOperation = 'IF_CONNECTED_PORTFOLIO_V3 GET /v3/me/portfolio';
async function decodePublic<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body: unknown = await response.json().catch(() => null);
    const row = typeof body === 'object' && body !== null ? body as { code?: unknown; retryable?: unknown; retryAfterMs?: unknown } : {};
    const code = typeof row.code === 'string' ? row.code : `HTTP_${response.status}`;
    const retryAfterMs = typeof row.retryAfterMs === 'number' && Number.isFinite(row.retryAfterMs) && row.retryAfterMs >= 0 ? row.retryAfterMs : null;
    throw new AccountApiError(response.status, code, [], typeof row.retryable === 'boolean' ? row.retryable : null, retryAfterMs);
  }
  return response.json() as Promise<T>;
}

/** Portfolio value series for the signed-in wallet; entries are validated to lie inside the requested window. */
export async function readValueHistory(window: PortfolioWindow, now: number = Date.now()): Promise<PnlHistoryResponse> {
  const interval = windowInterval(window, now);
  const scope = requireAccount();
  const query = new URLSearchParams({ from: interval.from, to: interval.to, interval: interval.interval, limit: '1000' });
  const value = await accountRequest<PnlHistoryResponse>(pnlApiUrl(`/v2/me/portfolio/history?${query}`), {}, scope);
  if (value.quality === 'quarantined' || value.watermark?.integrity === 'quarantined') throw new Error('Portfolio history is being checked, try again soon');
  const from = Date.parse(interval.from), to = Date.parse(interval.to);
  if (!Array.isArray(value.data.items) || value.data.items.length > 1000) throw new Error('History page bound mismatch');
  for (const row of value.data.items) {
    const time = Date.parse(row.time);
    if (!Number.isFinite(time) || time < from || time > to) throw new Error('History row is outside the selected window');
    rawInteger(row.pricedSubtotalUsdlRaw);
    if (row.completeValueUsdlRaw !== null) rawInteger(row.completeValueUsdlRaw);
  }
  return value;
}

/** Public portfolio projection for any wallet (no session). Same shape as the private read. */
export async function readPublicPortfolio(wallet: string, cursor?: string, signal?: AbortSignal): Promise<TrackedPortfolio> {
  const address = normalizeAddress(wallet);
  const query = new URLSearchParams({ limit: '50' });
  if (cursor) query.set('cursor', cursor);
  const value = await decodePublic<ConnectedPortfolioV3Response>(await fetch(pnlApiUrl(`/v3/wallets/${address}/portfolio?${query}`), { signal, cache: 'no-store', credentials: 'omit' }));
  if (value.data.summary.wallet.toLowerCase() !== address || value.data.positions.length > 50 || value.data.positions.some(row => row.wallet.toLowerCase() !== address || row.chainId !== '125')) throw new Error('Portfolio ownership or page bound mismatch');
  uint256Revision(value.data.summary.watermark.projectionRevision);
  for (const row of value.data.positions) {
    normalizeAddress(row.tokenAddress);
    for (const amount of [row.balanceRaw, row.knownRemainingCostUsdlRaw]) rawInteger(amount);
    for (const amount of [row.remainingCostUsdlRaw, row.realizedPnlRaw, row.unrealizedPnlRaw]) if (amount !== null) rawInteger(amount, true);
  }
  return value;
}

/* ------------------------------------------------------------ pure math */

export interface SeriesPoint { time: string; valueRaw: bigint; complete: boolean }

/** One point per bucket: complete value when priced, else the priced subtotal (marked incomplete). */
export function seriesPoints(response: PnlHistoryResponse): SeriesPoint[] {
  return response.data.items
    .map(row => ({ time: row.time, valueRaw: BigInt(row.completeValueUsdlRaw ?? row.pricedSubtotalUsdlRaw), complete: row.completeValueUsdlRaw !== null }))
    .sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
}

export type DeltaTone = 'gain' | 'loss' | 'flat';
export interface WindowDelta { absRaw: bigint; pct: number | null; tone: DeltaTone }

/** Last bucket minus first bucket; percent against the first bucket when it is non-zero. */
export function windowDelta(points: readonly SeriesPoint[]): WindowDelta | null {
  if (points.length === 0) return null;
  const first = points[0].valueRaw, last = points[points.length - 1].valueRaw;
  const absRaw = last - first;
  const pct = first === 0n ? null : Number((absRaw * 10_000n) / first) / 100;
  return { absRaw, pct, tone: absRaw > 0n ? 'gain' : absRaw < 0n ? 'loss' : 'flat' };
}

export function isOpenPosition(position: PortfolioPosition): boolean {
  return BigInt(position.balanceRaw) > 0n;
}

export function splitPositions(positions: readonly PortfolioPosition[]): { open: PortfolioPosition[]; closed: PortfolioPosition[] } {
  const open: PortfolioPosition[] = [], closed: PortfolioPosition[] = [];
  for (const position of positions) (isOpenPosition(position) ? open : closed).push(position);
  return { open, closed };
}

export interface PartialSum { raw: bigint; partial: boolean; counted: number }

/** Sums the non-null unrealized figures; partial when any position lacks one or the page is not the whole book. */
export function sumUnrealized(positions: readonly PortfolioPosition[], morePages = false): PartialSum {
  let raw = 0n, counted = 0, partial = morePages;
  for (const position of positions) {
    if (position.unrealizedPnlRaw === null) { if (isOpenPosition(position)) partial = true; continue; }
    raw += BigInt(position.unrealizedPnlRaw); counted++;
  }
  return { raw, partial, counted };
}

/**
 * Current mark value in USDL raw: balance x price, where the frozen IF_PRICE
 * is reduced quote units per whole base token (mirrors l17 domain.go).
 */
export function positionValueRaw(position: PortfolioPosition): bigint | null {
  const price = position.markPrice;
  if (!price || position.pricingQuality === 'stale' || price.baseAsset.decimals === null) return null;
  const denominator = BigInt(price.denominator);
  if (denominator === 0n) return null;
  return (BigInt(position.balanceRaw) * BigInt(price.numerator) * 10n ** BigInt(USDL_DECIMALS)) / (denominator * 10n ** BigInt(price.baseAsset.decimals));
}

export interface TopTrade { position: PortfolioPosition; realizedRaw: bigint }

/**
 * Closed positions (zero balance) of the served snapshot page ranked by their
 * served realized PnL, highest first. Positions whose realized figure is
 * withheld (realized basis partial or unknown) are not ranked.
 */
export function topTrades(positions: readonly PortfolioPosition[], limit = 5): TopTrade[] {
  return positions
    .filter(position => !isOpenPosition(position) && position.realizedPnlRaw !== null)
    .map(position => ({ position, realizedRaw: BigInt(position.realizedPnlRaw as string) }))
    .sort((a, b) => (a.realizedRaw === b.realizedRaw ? (a.position.tokenAddress < b.position.tokenAddress ? -1 : 1) : a.realizedRaw > b.realizedRaw ? -1 : 1))
    .slice(0, limit);
}

/* --------------------------------------------------------- honest states */

export type PortfolioIssueKind = 'signed-out' | 'reauth' | 'no-snapshot' | 'stale-snapshot';
export interface PortfolioIssue { kind: PortfolioIssueKind; title: string; body: string }

function failureOf(state: Loadable<unknown>): { status: number | null; code: string | null } | null {
  if (state.state === 'unsupported') return { status: state.status, code: state.message };
  if (state.state !== 'error') return null;
  const carrier = state.error as { status?: unknown; code?: unknown };
  return { status: typeof carrier.status === 'number' ? carrier.status : null, code: typeof carrier.code === 'string' ? carrier.code : null };
}

/**
 * Maps the production pnl-tracker answers onto the states a portfolio widget
 * shows instead of a generic failure:
 *  - 404 NOT_FOUND: no snapshot exists for the wallet. Snapshots are built
 *    only after the owner signs in (l16 enrollment), so a visitor sees that,
 *    and the owner sees that theirs is being built.
 *  - 409 REVISION_MISMATCH: the latest snapshot is past its 24 h expiry.
 *  - 403 (owner): the sign-in proof is older than 15 minutes.
 *  - 401 (owner): no usable session.
 * Anything else returns null and keeps the widget's ordinary error path.
 */
export function portfolioIssue(state: Loadable<unknown>, owner: boolean): PortfolioIssue | null {
  const failure = failureOf(state);
  if (!failure) return null;
  if (owner && failure.status === 401) return { kind: 'signed-out', title: 'Sign in to see your positions', body: 'Sign in with this wallet to load your portfolio.' };
  if (owner && failure.status === 403) return { kind: 'reauth', title: 'Sign in again to see your portfolio', body: 'For your security, sign in again to load your positions.' };
  if (failure.status === 404 && failure.code === 'NOT_FOUND') {
    return owner
      ? { kind: 'no-snapshot', title: 'Your portfolio is being built', body: "We're loading your positions. Check back in a minute." }
      : { kind: 'no-snapshot', title: 'No portfolio yet', body: 'Positions show up after this wallet signs in once.' };
  }
  if (failure.status === 409 && failure.code === 'REVISION_MISMATCH') {
    return owner
      ? { kind: 'stale-snapshot', title: 'Updating your portfolio', body: "We're refreshing your positions. Check back in a minute." }
      : { kind: 'stale-snapshot', title: 'Portfolio out of date', body: 'It refreshes the next time this wallet signs in.' };
  }
  return null;
}

/* ---------------------------------------------------------------- hooks */

export interface PortfolioRead { loadable: Loadable<TrackedPortfolio>; source: 'private' | 'public'; owner: boolean; refetch(): void }

function sessionOwns(wallet: string): boolean {
  const scope = accountSession();
  return !!scope && scope.session.wallet.toLowerCase() === wallet.toLowerCase();
}

/**
 * Private portfolio for the signed-in owner, public projection for everyone
 * else. `isOwner` may be supplied by the page; otherwise it follows the session.
 */
export function usePortfolio(wallet: string, isOwner?: boolean): PortfolioRead {
  const address = wallet.toLowerCase();
  const privateQuery = usePrivateAccountQuery('portfolio', () => readPortfolio(), [''], wallet);
  const owner = isOwner ?? privateQuery.owner;
  const publicQuery = useQuery({
    queryKey: queryKeys.publicPortfolio(address, null),
    queryFn: ({ signal }) => readPublicPortfolio(address, undefined, signal),
    enabled: !owner && /^0x[0-9a-f]{40}$/.test(address),
    ...retryTransientOnly,
    staleTime: 15_000,
  });
  if (owner) {
    const refetch = () => { void privateQuery.refetch(); };
    // The page says this is the owner's view but no session for this wallet is installed: that is a sign-in prompt, not a load.
    if (!privateQuery.owner) return { loadable: { state: 'error', error: new AccountApiError(401, 'AUTH_REQUIRED'), retry: refetch }, source: 'private', owner: true, refetch };
    return { loadable: loadable({ status: privateQuery.status, data: privateQuery.data, error: privateQuery.error, refetch: privateQuery.refetch }, privatePortfolioOperation), source: 'private', owner: true, refetch };
  }
  return { loadable: loadable(publicQuery, publicPortfolioOperation), source: 'public', owner: false, refetch: () => { void publicQuery.refetch(); } };
}

export const noPublicHistory: Unsupported = unsupported(historyOperation, null, 'Only the owner of this wallet can see its value history.');

/** Window value series for the signed-in owner; visitors get the honest no-route marker. */
export function useValueHistory(wallet: string, window: PortfolioWindow, owner: boolean): Loadable<PnlHistoryResponse> {
  const query = usePrivateAccountQuery('portfolio-history', () => readValueHistory(window), [window], wallet);
  if (!owner) return noPublicHistory;
  if (!query.owner) return { state: 'loading' };
  return loadable({ status: query.status, data: query.data, error: query.error, refetch: query.refetch }, historyOperation);
}

export interface PositionPages { positions: PortfolioPosition[]; hasMore: boolean; loadingMore: boolean; moreError: Error | null; loadMore(): void }

/**
 * Accumulates further portfolio pages behind the first (cached) page using
 * the same read the first page used; the accumulation resets whenever the
 * first page's snapshot changes.
 */
export function usePositionPages(first: TrackedPortfolio | null, wallet: string, owner: boolean): PositionPages {
  const snapshot = first?.data.summary.snapshotId ?? null;
  const [extra, setExtra] = useState<{ snapshot: string | null; pages: TrackedPortfolio[] }>({ snapshot: null, pages: [] });
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<Error | null>(null);
  useEffect(() => { setExtra({ snapshot, pages: [] }); setMoreError(null); }, [snapshot]);
  const pages = useMemo(() => (first ? [first, ...(extra.snapshot === snapshot ? extra.pages : [])] : []), [first, extra, snapshot]);
  const next = pages.length ? pages[pages.length - 1].data.next : undefined;
  const loadMore = useCallback(() => {
    if (!next || loadingMore || !first) return;
    setLoadingMore(true); setMoreError(null);
    const read = owner && sessionOwns(wallet) ? readPortfolio(next) : readPublicPortfolio(wallet, next);
    read.then(page => {
      if (page.data.summary.snapshotId !== first.data.summary.snapshotId) throw new Error('Your positions changed. Refresh to see more');
      setExtra(previous => ({ snapshot, pages: previous.snapshot === snapshot ? [...previous.pages, page] : [page] }));
    }).catch((error: unknown) => setMoreError(error instanceof Error ? error : new Error("Couldn't load more positions"))).finally(() => setLoadingMore(false));
  }, [next, loadingMore, first, owner, wallet, snapshot]);
  return { positions: useMemo(() => pages.flatMap(page => page.data.positions), [pages]), hasMore: !!next, loadingMore, moreError, loadMore };
}
