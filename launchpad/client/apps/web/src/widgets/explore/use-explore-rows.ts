'use client';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { useRankings, type RankingCategory, type RankingsResponse } from '@/core/market/use-rankings';
import { useStatsBatch, statsByPool, type PoolStats } from '@/core/market/use-stats-batch';
import { usePoolListings, type PoolListing, type PoolListSort } from '@/core/market/pool-listings';
import { countNumber } from '@/core/market/market-units';
import { statsChanges, statsCirculatingMarketCap, statsMarketCap, statsShare, statsSpotPrice, type ChangeWindow, type StatValue } from '@/core/market/pool-stats-display';
import { loadable, type Loadable } from '@/core/market/unsupported';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import type { TokenMetadata } from '@/widgets/home/types';
import { appendRankingPage, marketLimits } from '@/page-modules/token/market-session';
import { usdlNumber } from '@/page-modules/token/market-presentation';
import { shortAddress } from '@/utils/format';
import { poolPath } from '@/core/app-foundation/navigation';
import type { Stage } from '@/ui/primitives';

export interface ExploreRow {
  poolAddress: string;
  tokenAddress: string | null;
  rank: number;
  name: string;
  symbol: string;
  image: string | null;
  href: string;
  /** Market cap in raw USDL (6 decimals): PoolStats.totalSupplyValuationUsdlRaw (price x total supply, pump.fun's market cap). */
  mcapRaw: string | null;
  mcap: number | null;
  /** Why the market cap is a dash (no trade yet, unverified supply, no stats); null with a value. */
  mcapReason: string | null;
  /** Secondary valuation: PoolStats.circulatingMarketCapUsdlRaw (price x circulating supply), raw USDL. */
  circulatingMcapRaw: string | null;
  volume24hRaw: string | null;
  volume24h: number | null;
  createdAtMs: number | null;
  ageMs: number | null;
  /** PoolStats.txCount24h, else buys24h + sells24h. */
  txns: number | null;
  /** Unique 24h traders (PoolStats.uniqueTraders24h). */
  traders: number | null;
  holders: number | null;
  /** Spot price in exact decimal USDL per whole token (PoolStats.spotPriceWad); null before the first trade. */
  price: string | null;
  priceReason: string | null;
  /** Window changes in percent, only on a complete baseline (the sortable values). */
  change5m: number | null;
  change1h: number | null;
  change6h: number | null;
  change24h: number | null;
  /** Every window's value or the reason it is a dash ('insufficient_history', unavailable, not served). */
  changes: Record<ChangeWindow, StatValue<number>>;
  /** Holder concentration in percent of total supply (top10HolderShareBps / creatorHoldingShareBps). */
  top10Share: number | null;
  devShare: number | null;
  sharesReason: string | null;
  stage: Stage | null;
}

const dayMs = 86_400_000;

function finite(value: string | number | null | undefined): number | null {
  if (value === null || value === undefined || value === '') return null;
  const n = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(n) ? n : null;
}

/** The only stage the served data can prove: a pool created less than a day ago. */
export function stageFor(ageMs: number | null): Stage | null {
  return ageMs !== null && ageMs < dayMs ? 'new' : null;
}

export interface RowSources {
  poolAddress: string;
  rank: number;
  stats: PoolStats | null;
  /** Registry facts from IF_POOL_LIST / IF_POOL_DETAIL (token, creation time). */
  listing?: PoolListing | null;
  metadata: TokenMetadata | null;
  now: number;
}

/** Join one pool's ranking entry with its v2 stats, v2 listing and metadata. */
export function buildExploreRow({ poolAddress, rank, stats, listing = null, metadata, now }: RowSources): ExploreRow {
  const pool = poolAddress.toLowerCase();
  const tokenAddress = listing?.tokenAddress ?? metadata?.token_address?.toLowerCase() ?? null;
  const mcap = statsMarketCap(stats);
  const volume24hRaw = stats?.volume24hRaw ?? null;
  const createdAtMs = listing?.createdAtMs ?? (metadata?.created_at ? metadata.created_at * 1000 : null);
  const ageMs = createdAtMs === null ? null : Math.max(0, now - createdAtMs);
  const buys = countNumber(stats?.buys24h), sells = countNumber(stats?.sells24h);
  const txns = countNumber(stats?.txCount24h) ?? (buys !== null && sells !== null ? buys + sells : null);
  const changes = statsChanges(stats);
  const price = statsSpotPrice(stats);
  const top10 = statsShare(stats, 'top10'), dev = statsShare(stats, 'creator');
  const name = metadata?.name?.trim() || shortAddress(pool, 6, 4);
  const symbol = metadata?.symbol?.trim() || shortAddress(tokenAddress ?? pool, 4, 4);
  return {
    poolAddress: pool,
    tokenAddress,
    rank,
    name,
    symbol,
    image: metadata?.images?.logo ?? null,
    href: poolPath(125, pool),
    mcapRaw: mcap.value,
    mcap: mcap.value === null ? null : usdlNumber(mcap.value),
    mcapReason: mcap.reason,
    circulatingMcapRaw: statsCirculatingMarketCap(stats).value,
    volume24hRaw,
    volume24h: volume24hRaw === null ? null : usdlNumber(volume24hRaw),
    createdAtMs,
    ageMs,
    txns,
    traders: countNumber(stats?.uniqueTraders24h),
    holders: finite(stats?.holderCount),
    price: price.value,
    priceReason: price.reason,
    change5m: changes['5m'].value,
    change1h: changes['1h'].value,
    change6h: changes['6h'].value,
    change24h: changes['24h'].value,
    changes,
    top10Share: top10.value,
    devShare: dev.value,
    sharesReason: top10.reason ?? dev.reason,
    stage: stageFor(ageMs),
  };
}

function chunk(pools: readonly string[], start: number): string[] {
  return pools.slice(start, start + marketLimits.rankingPage);
}

export interface ExploreRowsState {
  rows: ExploreRow[];
  snapshot: RankingsResponse | null;
  ranking: Loadable<RankingsResponse>;
  /** The first page is still loading and nothing has rendered yet. */
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  moreError: Error | null;
  loadMore(): void;
  retryMore(): void;
  reload(): void;
  stats: Loadable<ReadonlyMap<string, PoolStats>>;
  /** Pool registry facts (token, creation time): IF_POOL_LIST / IF_POOL_DETAIL. */
  listings: Loadable<Record<string, PoolListing>>;
  metadata: Loadable<Record<string, TokenMetadata | null>>;
  enrichmentPending: boolean;
}

function mergeLoadable<A, B>(a: Loadable<A>, b: Loadable<B> | null, merge: (x: A, y: B | null) => A): Loadable<A> {
  if (b === null) return a;
  if (a.state !== 'ready') return a;
  if (b.state === 'ready') return { state: 'ready', data: merge(a.data, b.data) };
  return b.state === 'loading' ? a : b;
}

const listSorts: ReadonlySet<string> = new Set<PoolListSort>(['new', 'volume', 'trending']);

/**
 * Ranking snapshot pages for one category (IF_RANKINGS, appended with
 * appendRankingPage) enriched with IF_STATS_BATCH (two fixed 50-pool
 * chunks, the snapshot cap is 100), the pool listings (IF_POOL_LIST for the
 * categories it orders, IF_POOL_DETAIL otherwise; session-cached) and the
 * metadata batch. Every source keeps its own Loadable so a 503 on one never
 * blanks the others.
 */
export function useExploreRows(category: RankingCategory): ExploreRowsState {
  const [cursor, setCursor] = useState<string | null>(null);
  const [snapshot, setSnapshot] = useState<RankingsResponse | null>(null);
  const [conflict, setConflict] = useState<Error | null>(null);
  const snapshotRef = useRef<RankingsResponse | null>(null);
  const query = useRankings(category, cursor);

  useEffect(() => {
    setCursor(null); setSnapshot(null); setConflict(null); snapshotRef.current = null;
  }, [category]);

  useEffect(() => {
    if (!query.data) return;
    try {
      const next = appendRankingPage(cursor === null ? null : snapshotRef.current, query.data);
      snapshotRef.current = next; setSnapshot(next); setConflict(null);
    } catch (error) {
      setConflict(error instanceof Error ? error : new Error(String(error)));
    }
  }, [query.data, cursor]);

  const pools = useMemo(() => snapshot?.data.snapshot.items.map(item => item.poolAddress.toLowerCase()) ?? [], [snapshot]);
  const first = useMemo(() => chunk(pools, 0), [pools]);
  const second = useMemo(() => chunk(pools, marketLimits.rankingPage), [pools]);
  const statsA = useStatsBatch(first);
  const statsB = useStatsBatch(second);
  const listings = usePoolListings(pools, listSorts.has(category) ? category as PoolListSort : undefined);
  const tokens = useMemo(() => pools.map(pool => listings.data?.[pool]?.tokenAddress ?? null).filter((value): value is string => value !== null), [pools, listings.data]);
  const metadata = useTokenMetadataBatch(tokens);

  const byPool = useMemo(() => {
    const map = new Map(statsByPool(statsA.data));
    for (const [address, row] of statsByPool(statsB.data)) map.set(address, row);
    return map as ReadonlyMap<string, PoolStats>;
  }, [statsA.data, statsB.data]);

  const rows = useMemo(() => {
    const now = Date.now();
    return (snapshot?.data.snapshot.items ?? []).map(item => {
      const pool = item.poolAddress.toLowerCase();
      const listing = listings.data?.[pool] ?? null;
      const token = listing?.tokenAddress ?? null;
      return buildExploreRow({ poolAddress: pool, rank: item.rank, stats: byPool.get(pool) ?? null, listing, metadata: token ? metadata.data?.[token] ?? null : null, now });
    });
  }, [snapshot, byPool, listings.data, metadata.data]);

  const loadMore = useCallback(() => {
    const next = snapshotRef.current?.nextCursor ?? null;
    if (next !== null && !query.isFetching) setCursor(next);
  }, [query.isFetching]);
  const reload = useCallback(() => {
    setCursor(null); setSnapshot(null); setConflict(null); snapshotRef.current = null;
    void query.refetch();
  }, [query]);
  const retryMore = useCallback(() => { void query.refetch(); }, [query]);

  const firstPageFailed = cursor === null && query.status === 'error';
  const ranking: Loadable<RankingsResponse> = conflict
    ? { state: 'error', error: conflict, retry: reload }
    : snapshot && !firstPageFailed
      ? { state: 'ready', data: snapshot }
      : loadable(query, 'IF_RANKINGS');
  const moreError = cursor !== null && query.status === 'error' ? (query.error instanceof Error ? query.error : new Error(String(query.error))) : null;

  const statsLoadable = pools.length === 0
    ? { state: 'ready' as const, data: byPool }
    : mergeLoadable(loadable(statsA as UseQueryResult<unknown>, 'IF_STATS_BATCH') as Loadable<unknown> as Loadable<ReadonlyMap<string, PoolStats>>, second.length > 0 ? loadable(statsB as UseQueryResult<unknown>, 'IF_STATS_BATCH') as Loadable<unknown> as Loadable<ReadonlyMap<string, PoolStats>> : null, () => byPool);
  const statsReady: Loadable<ReadonlyMap<string, PoolStats>> = statsLoadable.state === 'ready' ? { state: 'ready', data: byPool } : statsLoadable;

  return {
    rows,
    snapshot,
    ranking,
    // Until the first page lands in `snapshot` (one effect after the query resolves) the view is still loading, not empty.
    loading: snapshot === null && conflict === null && query.status !== 'error',
    hasMore: snapshot !== null && snapshot.nextCursor !== null && conflict === null,
    loadingMore: cursor !== null && query.isFetching,
    moreError,
    loadMore,
    retryMore,
    reload,
    stats: statsReady,
    listings: pools.length === 0 ? { state: 'ready', data: {} } : loadable(listings, 'IF_POOL_DETAIL'),
    metadata: tokens.length === 0 ? { state: 'ready', data: {} } : loadable(metadata, 'LEGACY_METADATA_BATCH'),
    enrichmentPending: pools.length > 0 && (statsA.isPending || (second.length > 0 && statsB.isPending) || listings.isPending || (tokens.length > 0 && metadata.isPending)),
  };
}
