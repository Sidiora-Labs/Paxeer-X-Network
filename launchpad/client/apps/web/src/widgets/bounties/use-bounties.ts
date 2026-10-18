'use client';
import { useCallback, useMemo } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useGatewayOperations } from '@/core/market/operations';
import { toUnsupported, type Loadable } from '@/core/market/unsupported';
import { bountiesContract, type BountiesAvailability, type BountiesStatsResponse, type Bounty, type BountyGetResponse, type BountyListFilter, type BountyListResponse } from './bounties-contract';

const policy = marketCachePolicy.bounties;

type QueryShape<T> = { status: 'pending' | 'error' | 'success'; data: T | undefined; error: unknown; refetch(): unknown };

function toLoadable<T>(query: QueryShape<T>, operation: string): Loadable<T> {
  if (query.status === 'pending') return { state: 'loading' };
  if (query.status === 'error') {
    const marker = toUnsupported(operation, query.error);
    if (marker) return marker;
    const error = query.error instanceof Error ? query.error : new Error(String(query.error));
    return { state: 'error', error, retry: () => { void query.refetch(); } };
  }
  return { state: 'ready', data: query.data as T };
}

export interface BountiesAvailabilityResult {
  loadable: Loadable<{ generatedAt: string }>;
  /** The probe answer when it completed (either way). */
  availability: BountiesAvailability | null;
  retry(): void;
}

/**
 * Probes the bounties service once per session with the smallest real list
 * request. A 404/501/503 answer is kept as the Unsupported marker so the page
 * explains itself with the operation id and status instead of empty cards.
 */
export function useBountiesAvailability(): BountiesAvailabilityResult {
  const operations = useGatewayOperations();
  const query = useQuery({
    queryKey: queryKeys.bountiesProbe(),
    queryFn: ({ signal }) => bountiesContract.probe(operations, signal),
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: Number.POSITIVE_INFINITY,
  });
  const retry = useCallback(() => { void query.refetch(); }, [query]);
  const loadable = useMemo<Loadable<{ generatedAt: string }>>(() => {
    if (query.status === 'pending') return { state: 'loading' };
    if (query.status === 'error') {
      const error = query.error instanceof Error ? query.error : new Error(String(query.error));
      return { state: 'error', error, retry };
    }
    return query.data.state === 'available' ? { state: 'ready', data: { generatedAt: query.data.generatedAt } } : query.data;
  }, [query.status, query.data, query.error, retry]);
  return { loadable, availability: query.data ?? null, retry };
}

export interface BountiesResult {
  loadable: Loadable<Bounty[]>;
  items: Bounty[];
  /** Every loaded page reported quality complete and no further cursor exists: counts are real totals. */
  complete: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore(): void;
  refetch(): void;
}

/**
 * IF_BOUNTY_LIST `GET /v2/bounties?state=&poolAddress=&cursor=&limit=` paged by
 * the server cursor. The list carries no creator filter, so "Mine" callers pass
 * an empty filter and narrow the loaded pages with `bountiesContract.mine`.
 */
export function useBounties(filter: BountyListFilter, options: { enabled?: boolean; limit?: number } = {}): BountiesResult {
  const operations = useGatewayOperations();
  const enabled = options.enabled ?? true;
  const limit = options.limit ?? bountiesContract.pageLimit;
  const filterKey = bountiesContract.listFilterKey(filter);
  const query = useInfiniteQuery({
    queryKey: queryKeys.bounties(filterKey, null),
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => bountiesContract.validateList(
      await operations.call('IF_BOUNTY_LIST', { query: bountiesContract.listQuery(filter, pageParam, limit) }, { signal }),
      filter,
    ),
    getNextPageParam: (last: BountyListResponse) => last.nextCursor,
    enabled,
    ...policy,
  });
  const data = query.data;
  const items = useMemo(() => data?.pages.flatMap(page => page.data.items) ?? [], [data]);
  const complete = data !== undefined && data.pages.length > 0 && data.pages.every(page => page.quality === 'complete') && !query.hasNextPage;
  const loadable = useMemo<Loadable<Bounty[]>>(() => {
    if (!enabled) return { state: 'loading' };
    return toLoadable({ status: query.status, data: items, error: query.error, refetch: query.refetch }, bountiesContract.operations.list);
  }, [enabled, query.status, query.error, query.refetch, items]);
  return {
    loadable,
    items,
    complete,
    hasMore: query.hasNextPage,
    loadingMore: query.isFetchingNextPage,
    loadMore: () => { if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage(); },
    refetch: () => { void query.refetch(); },
  };
}

/** IF_BOUNTIES_STATS `GET /v2/bounties/stats`: the four header totals. */
export function useBountiesStats(enabled = true): Loadable<BountiesStatsResponse> {
  const operations = useGatewayOperations();
  const query = useQuery({
    queryKey: queryKeys.bountiesStats(),
    queryFn: async ({ signal }) => bountiesContract.validateStats(await operations.call('IF_BOUNTIES_STATS', {}, { signal })),
    enabled,
    ...policy,
  });
  if (!enabled) return { state: 'loading' };
  return toLoadable(query, bountiesContract.operations.stats);
}

/** IF_BOUNTY_GET `GET /v2/bounties/{bountyId}`; the detail page (BN5) builds on this. */
export function useBounty(bountyId: string | null): Loadable<BountyGetResponse> {
  const operations = useGatewayOperations();
  const query = useQuery({
    queryKey: queryKeys.bounty(bountyId ?? ''),
    queryFn: async ({ signal }) => bountiesContract.validateBountyResponse(await operations.call('IF_BOUNTY_GET', { path: { bountyId: bountyId as string } }, { signal }), bountyId as string),
    enabled: bountyId !== null,
    ...policy,
  });
  if (bountyId === null) return { state: 'loading' };
  return toLoadable(query, bountiesContract.operations.get);
}
