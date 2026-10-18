'use client';

import { useCallback, useMemo } from 'react';
import { useInfiniteQuery, useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query';
import { usePublicClient } from 'wagmi';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useMarketClient } from '@/core/market/client';
import { useOperations } from '@/core/market/operations';
import { fetchStatsBatch } from '@/core/market/use-stats-batch';
import { toUnsupported, type Loadable, type Unsupported } from '@/core/market/unsupported';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import {
  chunkPools,
  classifyHolderRewardsFailure,
  fetchHolderRewardsPools,
  fetchHolderRewardsStats,
  holderRewardsOperations,
  holderRewardsPageSize,
  readAirdropBalance,
  type HolderRewardsAvailability,
  type HolderRewardsPoolsResponse,
  type HolderRewardsSort,
  type HolderRewardsStatsResponse,
  type PoolStats,
} from './holder-rewards-contract';
import { buildRows, type HolderRewardRow } from './holder-rewards-presentation';

const holderRewardsCachePolicy = marketCachePolicy.holderRewards;

export function useHolderRewardsStats(): Loadable<HolderRewardsStatsResponse> {
  const operations = useOperations();
  const query = useQuery({
    queryKey: queryKeys.holderRewardsStats(),
    queryFn: ({ signal }) => fetchHolderRewardsStats(operations, signal),
    ...holderRewardsCachePolicy,
  });
  if (query.status === 'pending') return { state: 'loading' };
  if (query.status === 'error') {
    const marker = toUnsupported(holderRewardsOperations.stats, query.error);
    if (marker) return marker;
    return { state: 'error', error: query.error, retry: () => { void query.refetch(); } };
  }
  return { state: 'ready', data: query.data };
}

export interface HolderRewardsPoolsState {
  items: HolderRewardsPoolsResponse['data']['items'];
  pages: HolderRewardsPoolsResponse[];
  loading: boolean;
  availability: HolderRewardsAvailability | null;
  hasMore: boolean;
  loadingMore: boolean;
  loadMoreFailed: boolean;
  loadMore(): void;
  retry(): void;
}

/** Cursor-paged IF_HOLDER_REWARDS_POOLS; the server orders by `sort`, the page keeps the cursor chain. */
export function useHolderRewardsPools(sort: HolderRewardsSort, limit: number = holderRewardsPageSize): HolderRewardsPoolsState {
  const operations = useOperations();
  const query = useInfiniteQuery({
    queryKey: queryKeys.holderRewardsPools(sort, limit),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => fetchHolderRewardsPools(operations, { sort, cursor: pageParam, limit }, signal),
    getNextPageParam: last => last.nextCursor,
    ...holderRewardsCachePolicy,
  });
  const data = query.data;
  const pages = useMemo(() => data?.pages ?? [], [data]);
  const items = useMemo(() => pages.flatMap(page => page.data.items), [pages]);
  const loadMore = useCallback(() => { if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage(); }, [query]);
  const retry = useCallback(() => { void query.refetch(); }, [query]);
  const availability = query.status === 'error' && pages.length === 0 ? classifyHolderRewardsFailure(holderRewardsOperations.pools, query.error) : null;
  return {
    items,
    pages,
    loading: query.status === 'pending',
    availability,
    hasMore: query.hasNextPage,
    loadingMore: query.isFetchingNextPage,
    loadMoreFailed: query.isFetchNextPageError,
    loadMore,
    retry,
  };
}

export interface PoolStatsLookup {
  byPool: ReadonlyMap<string, PoolStats>;
  pending: boolean;
  /** The first chunk that failed with a capability or dependency gap; genuine errors are rethrown by React Query and surface per chunk as missing prices. */
  unavailable: Unsupported | null;
}

/** IF_STATS_BATCH over chunks of at most 50 pools, sharing core/market/use-stats-batch.ts keys per chunk. */
export function usePoolStatsLookup(pools: readonly string[]): PoolStatsLookup {
  const client = useMarketClient();
  const operations = useOperations();
  const chunks = useMemo(() => chunkPools(pools), [pools]);
  return useQueries({
    queries: chunks.map(chunk => ({
      queryKey: queryKeys.statsBatch(client.config.apiOrigin, chunk),
      queryFn: ({ signal }: { signal: AbortSignal }) => fetchStatsBatch(operations, chunk, signal),
      ...marketCachePolicy.statsBatch,
    })),
    combine: results => {
      const byPool = new Map<string, PoolStats>();
      let pending = false;
      let unavailable: Unsupported | null = null;
      for (const result of results) {
        if (result.status === 'pending') pending = true;
        else if (result.status === 'error') unavailable ??= toUnsupported(holderRewardsOperations.statsBatch, result.error);
        else for (const item of result.data.data.items) byPool.set(item.poolAddress.toLowerCase(), item);
      }
      return { byPool, pending, unavailable };
    },
  });
}

export interface HolderRewardsRowsState extends Omit<HolderRewardsPoolsState, 'items' | 'pages'> {
  rows: HolderRewardRow[];
  pricesPending: boolean;
  pricesUnavailable: Unsupported | null;
  identityPending: boolean;
}

/** Rows joined with IF_STATS_BATCH (market cap) and the legacy metadata batch (name, symbol, logo, links). */
export function useHolderRewardsRows(sort: HolderRewardsSort, limit: number = holderRewardsPageSize): HolderRewardsRowsState {
  const pools = useHolderRewardsPools(sort, limit);
  const poolAddresses = useMemo(() => pools.items.map(item => item.poolAddress.toLowerCase()), [pools.items]);
  const tokenAddresses = useMemo(() => pools.items.map(item => item.tokenAddress.toLowerCase()), [pools.items]);
  const prices = usePoolStatsLookup(poolAddresses);
  const metadata = useTokenMetadataBatch(tokenAddresses);
  const rows = useMemo(() => buildRows(pools.items, prices.byPool, metadata.data), [pools.items, prices.byPool, metadata.data]);
  const { items: _items, pages: _pages, ...rest } = pools;
  return { ...rest, rows, pricesPending: prices.pending, pricesUnavailable: prices.unavailable, identityPending: metadata.isPending && tokenAddresses.length > 0 };
}

/** Pending holder airdrop for one pool read from IFeeAccumulator on chain 125; enabled only while a row is hovered or focused. */
export function useAirdropBalance(poolAddress: string | null): UseQueryResult<bigint> {
  const client = usePublicClient({ chainId: 125 });
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  return useQuery({
    queryKey: queryKeys.holderRewardsAirdropBalance(pool ?? ''),
    queryFn: () => {
      if (!client) throw new Error('Chain 125 public client unavailable');
      return readAirdropBalance(client, pool as string);
    },
    enabled: pool !== null && client !== undefined,
    staleTime: 15_000,
    gcTime: 60_000,
    retry: false,
  });
}
