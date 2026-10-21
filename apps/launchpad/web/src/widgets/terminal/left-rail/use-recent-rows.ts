'use client';
import { useMemo } from 'react';
import { useStatsBatch, statsByPool } from '@/core/market/use-stats-batch';
import { usePoolDetailListings } from '@/core/market/pool-listings';
import { loadable, type Loadable } from '@/core/market/unsupported';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { buildExploreRow } from '@/widgets/explore/use-explore-rows';
import { toRecentRow, type RecentRow } from './rows';

export interface RecentRowsState {
  rows: RecentRow[];
  /** The price source (IF_POOL_DETAIL reserves). */
  prices: Loadable<unknown>;
  enrichmentPending: boolean;
}

/**
 * Stored recent pools joined with IF_STATS_BATCH (spotPriceWad, market cap),
 * IF_POOL_DETAIL (token, creation time, and the reserve price for a stats row
 * that predates spotPriceWad) and the metadata batch; rows render before
 * enrichment lands.
 */
export function useRecentRows(pools: readonly string[]): RecentRowsState {
  const stats = useStatsBatch(pools);
  const details = usePoolDetailListings(pools);
  const tokens = useMemo(() => pools.map(pool => details.data?.[pool]?.tokenAddress ?? null).filter((value): value is string => value !== null), [pools, details.data]);
  const metadata = useTokenMetadataBatch(tokens);
  const byPool = useMemo(() => statsByPool(stats.data), [stats.data]);

  const rows = useMemo(() => {
    const now = Date.now();
    return pools.map((pool, index) => {
      const listing = details.data?.[pool] ?? null;
      const token = listing?.tokenAddress ?? null;
      const stats = byPool.get(pool) ?? null;
      const row = buildExploreRow({ poolAddress: pool, rank: index + 1, stats, listing, metadata: token ? metadata.data?.[token] ?? null : null, now });
      // PoolStats.spotPriceWad when the row carries it (null: no trade yet); the reserve price only for a row without the field.
      const served = stats !== null && stats.spotPriceWad !== undefined;
      return served ? toRecentRow(row, row.price, row.priceReason) : toRecentRow(row, listing?.spotPrice ?? null);
    });
  }, [pools, byPool, details.data, metadata.data]);

  return {
    rows,
    prices: pools.length === 0 ? { state: 'ready', data: {} } : loadable(details, 'IF_POOL_DETAIL'),
    enrichmentPending: pools.length > 0 && (stats.isPending || details.isPending),
  };
}
