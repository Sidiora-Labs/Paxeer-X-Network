'use client';
import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useMarketClient } from '@/core/market/client';
import { useOperations } from '@/core/market/operations';
import { useStatsBatch, statsByPool, type PoolStats } from '@/core/market/use-stats-batch';
import { usePoolListings, type PoolListing } from '@/core/market/pool-listings';
import { loadable, type Loadable } from '@/core/market/unsupported';
import { marketCachePolicy } from '@/core/cache-policy';
import { queryKeys } from '@/core/query-keys';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import type { TokenMetadata } from '@/widgets/home/types';
import { buildExploreRow } from '@/widgets/explore/use-explore-rows';
import { terminalPoolPath } from '../sub-nav';
import type { SearchResult } from '../../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';

export const similarLimit = 10;
export type SimilarSort = 'liq' | 'vol' | 'mc';

export interface SimilarRow {
  poolAddress: string;
  tokenAddress: string | null;
  symbol: string;
  name: string;
  image: string | null;
  href: string;
  createdAtMs: number | null;
  liquidityRaw: string | null;
  volumeRaw: string | null;
  mcapRaw: string | null;
}

/** Pool hits for the symbol, minus the open pool, deduplicated, in served order. */
export function similarPools(results: readonly SearchResult[], current: string): Array<{ poolAddress: string; tokenAddress: string | null; label: string }> {
  const seen = new Set<string>([current.toLowerCase()]);
  const pools: Array<{ poolAddress: string; tokenAddress: string | null; label: string }> = [];
  for (const result of results) {
    const pool = result.poolAddress?.toLowerCase() ?? null;
    if (result.kind !== 'pool' || !pool || !/^0x[0-9a-f]{40}$/.test(pool) || seen.has(pool)) continue;
    seen.add(pool);
    pools.push({ poolAddress: pool, tokenAddress: result.tokenAddress?.toLowerCase() ?? null, label: result.label });
  }
  return pools.slice(0, similarLimit);
}

export function similarRow(hit: { poolAddress: string; tokenAddress: string | null; label: string }, stats: PoolStats | null, listing: PoolListing | null, metadata: TokenMetadata | null, now: number): SimilarRow {
  const base = buildExploreRow({ poolAddress: hit.poolAddress, rank: 0, stats, listing, metadata, now });
  const label = hit.label.trim();
  return {
    poolAddress: base.poolAddress,
    tokenAddress: hit.tokenAddress ?? base.tokenAddress,
    symbol: metadata?.symbol?.trim() || base.symbol,
    name: metadata?.name?.trim() || label || base.name,
    image: base.image,
    href: terminalPoolPath(base.poolAddress),
    createdAtMs: base.createdAtMs,
    liquidityRaw: stats?.realUsdlBalanceRaw ?? null,
    volumeRaw: base.volume24hRaw,
    mcapRaw: base.mcapRaw,
  };
}

function sortValue(row: SimilarRow, sort: SimilarSort): bigint | null {
  const raw = sort === 'liq' ? row.liquidityRaw : sort === 'vol' ? row.volumeRaw : row.mcapRaw;
  return raw !== null && /^-?\d+$/.test(raw) ? BigInt(raw) : null;
}

/** Descending by the chosen figure; rows without it keep served order at the end. */
export function sortSimilar(rows: readonly SimilarRow[], sort: SimilarSort): SimilarRow[] {
  return rows.map((row, index) => ({ row, index, value: sortValue(row, sort) })).sort((a, b) => {
    if (a.value === null || b.value === null) return a.value === null && b.value === null ? a.index - b.index : a.value === null ? 1 : -1;
    return a.value === b.value ? a.index - b.index : a.value > b.value ? -1 : 1;
  }).map(entry => entry.row);
}

export interface SimilarState {
  search: Loadable<SearchResult[]>;
  rows: SimilarRow[];
  stats: Loadable<unknown>;
  enrichmentPending: boolean;
}

/** IF_SEARCH type=pool q=<symbol>, then IF_STATS_BATCH, the pool listings (IF_POOL_DETAIL, for hits search left without a token) and metadata. */
export function useSimilarTokens(poolAddress: string, symbol: string | null): SimilarState {
  const client = useMarketClient();
  const operations = useOperations();
  const q = symbol?.trim() ?? '';
  const query = useQuery({
    queryKey: queryKeys.similarPools(client.config.apiOrigin, q),
    queryFn: async ({ signal }) => {
      const response = await operations.call('IF_SEARCH', { query: { q, type: 'pool', limit: similarLimit } }, { signal });
      if (response.quality === 'quarantined') throw new Error('Search integrity quarantined');
      return response.data.items;
    },
    enabled: q.length > 0,
    ...marketCachePolicy.rankings,
  });
  const hits = useMemo(() => similarPools(query.data ?? [], poolAddress), [query.data, poolAddress]);
  const pools = useMemo(() => hits.map(hit => hit.poolAddress), [hits]);
  const stats = useStatsBatch(pools);
  const listings = usePoolListings(pools);
  const tokens = useMemo(() => hits.map(hit => hit.tokenAddress ?? listings.data?.[hit.poolAddress]?.tokenAddress ?? null).filter((value): value is string => value !== null), [hits, listings.data]);
  const metadata = useTokenMetadataBatch(tokens);
  const rows = useMemo(() => {
    const now = Date.now();
    const byPool = statsByPool(stats.data);
    return hits.map(hit => {
      const listing = listings.data?.[hit.poolAddress] ?? null;
      const token = hit.tokenAddress ?? listing?.tokenAddress ?? null;
      return similarRow(hit, byPool.get(hit.poolAddress) ?? null, listing, token ? metadata.data?.[token] ?? null : null, now);
    });
  }, [hits, stats.data, listings.data, metadata.data]);
  return {
    search: loadable(query, 'IF_SEARCH'),
    rows,
    stats: pools.length === 0 ? { state: 'ready', data: null } : loadable(stats, 'IF_STATS_BATCH'),
    enrichmentPending: pools.length > 0 && (stats.isPending || listings.isPending || (tokens.length > 0 && metadata.isPending)),
  };
}
