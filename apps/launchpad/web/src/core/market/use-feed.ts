'use client';
import { useQuery } from '@tanstack/react-query';
import type { OperationInput, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { marketAddress } from '@/page-modules/token/market-session';
import { useOperations } from './operations';
import { loadable, type Loadable } from './unsupported';

export type FeedPage = V2ContractTypes['ConnectedDiscoveryFeedPage'];
export type FeedCard = V2ContractTypes['ConnectedDiscoveryFeedCard'];
export type CalloutListResponse = V2ContractTypes['CalloutListResponse'];
export type Callout = V2ContractTypes['Callout'];
export type FeedOrder = NonNullable<NonNullable<OperationInput<'IF_CONNECTED_FEED'>['query']>['order']>;

export const feedLimit = 50;

/**
 * IF_CONNECTED_FEED `GET /v2/discovery/feed`: core/api answers 503
 * DEPENDENCY_UNAVAILABLE until a discovery runtime is configured
 * (core/api/internal/app/app.go:579-583); that status becomes Unsupported.
 */
export function useFeed(order: FeedOrder = 'relevance', cursor: string | null = null, limit: number = feedLimit): Loadable<FeedPage> {
  const operations = useOperations();
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > feedLimit) throw new RangeError('Invalid feed page bound');
  const query = useQuery({
    queryKey: queryKeys.feed(order, cursor, limit),
    queryFn: ({ signal }) => operations.call('IF_CONNECTED_FEED', { query: { order, limit, ...(cursor ? { cursor } : {}) } }, { signal }),
    ...marketCachePolicy.feed,
  });
  return loadable(query, 'IF_CONNECTED_FEED');
}

/** IF_CALLOUT_LIST `GET /v2/pools/{p}/callouts`: allowlisted but unimplemented; the real 404 becomes Unsupported. */
export function usePoolCallouts(poolAddress: string | null | undefined, cursor: string | null = null): Loadable<CalloutListResponse> {
  const operations = useOperations();
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const query = useQuery({
    queryKey: queryKeys.poolCallouts(pool ?? '', cursor),
    queryFn: async ({ signal }) => {
      const response = await operations.call('IF_CALLOUT_LIST', { path: { poolAddress: pool as string }, query: { limit: feedLimit, ...(cursor ? { cursor } : {}) } }, { signal });
      if (response.quality === 'quarantined' || response.data.items.some(row => marketAddress(row.poolAddress) !== pool)) throw new Error('Callout identity disagreement or integrity quarantine');
      return response;
    },
    enabled: pool !== null,
    ...marketCachePolicy.callouts,
  });
  return loadable(query, 'IF_CALLOUT_LIST');
}
