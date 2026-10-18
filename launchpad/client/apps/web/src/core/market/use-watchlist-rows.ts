'use client';
import { useMemo } from 'react';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import type { TokenMetadata } from '@/widgets/home/types';
import { marketLimits } from '@/page-modules/token/market-session';
import { useStatsBatch, statsByPool, type PoolStats } from './use-stats-batch';
import { usePoolListings, type PoolListing } from './pool-listings';
import { loadable, type Loadable } from './unsupported';

function mapLoadable<T, U>(value: Loadable<T>, map: (data: T) => U): Loadable<U> {
  return value.state === 'ready' ? { state: 'ready', data: map(value.data) } : value;
}

export interface WatchlistRow {
  poolAddress: string;
  tokenAddress: string | null;
  stats: PoolStats | null;
  /** Registry facts (token, creation time) from IF_POOL_DETAIL. */
  listing: PoolListing | null;
  metadata: TokenMetadata | null;
}

export interface WatchlistRows {
  rows: WatchlistRow[];
  pools: readonly string[];
  isLocal: boolean;
  loading: boolean;
  stats: Loadable<ReadonlyMap<string, PoolStats>>;
  listings: Loadable<Record<string, PoolListing>>;
  metadata: Loadable<Record<string, TokenMetadata | null>>;
  toggle(poolAddress: string): void;
  check(poolAddress: string): boolean;
}

/**
 * Read-only join of the account (or local) watchlist with IF_STATS_BATCH,
 * the pool listings (IF_POOL_DETAIL, session-cached: pool -> token, creation
 * time) and the token metadata batch. Each source carries its own Loadable so
 * one unavailable source never blanks the others.
 */
export function useWatchlistRows(): WatchlistRows {
  const watchlist = useWatchlist();
  const pools = useMemo(() => [...new Set(watchlist.list.map(value => value.toLowerCase()))].filter(value => /^0x[0-9a-f]{40}$/.test(value)).slice(0, marketLimits.rankingPage), [watchlist.list]);
  const stats = useStatsBatch(pools);
  const listings = usePoolListings(pools);
  const tokens = useMemo(() => pools.map(pool => listings.data?.[pool]?.tokenAddress ?? null).filter((value): value is string => value !== null), [pools, listings.data]);
  const metadata = useTokenMetadataBatch(tokens);
  const byPool = statsByPool(stats.data);
  const rows: WatchlistRow[] = pools.map(pool => {
    const listing = listings.data?.[pool] ?? null;
    const token = listing?.tokenAddress ?? null;
    return { poolAddress: pool, tokenAddress: token, stats: byPool.get(pool) ?? null, listing, metadata: token ? metadata.data?.[token] ?? null : null };
  });
  return {
    rows, pools, isLocal: watchlist.isLocal, loading: watchlist.loading,
    stats: pools.length === 0 ? { state: 'ready', data: byPool } : mapLoadable(loadable(stats, 'IF_STATS_BATCH'), () => byPool),
    listings: pools.length === 0 ? { state: 'ready', data: {} } : loadable(listings, 'IF_POOL_DETAIL'),
    metadata: tokens.length === 0 ? { state: 'ready', data: {} } : loadable(metadata, 'LEGACY_METADATA_BATCH'),
    toggle: watchlist.toggle, check: watchlist.check,
  };
}
