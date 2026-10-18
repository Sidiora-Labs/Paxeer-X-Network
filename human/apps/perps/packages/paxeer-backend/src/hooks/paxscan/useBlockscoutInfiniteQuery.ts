/**
 * Paxscan typed infinite-query hook (paginated endpoints only).
 *
 * Mirrors `useBlockscoutQuery` for the subset of resources flagged as
 * `paginated: true`. Honours Blockscout's `next_page_params` cursor protocol:
 * each successful response carries a `next_page_params` object (or `null` /
 * `undefined` when the last page has been reached).
 */

import type {
  InfiniteData,
  QueryKey,
  UseInfiniteQueryOptions,
  UseInfiniteQueryResult,
} from '@tanstack/react-query';
import { useInfiniteQuery } from '@tanstack/react-query';

import {
  RESOURCES,
  buildUrl,
  paxscanFetch,
} from '@paxeer-network/paxeer-backend/core/paxscan/api';
import type {
  PaginatedResourceName,
  PathParams,
  QueryParams,
  ResourcePayload,
} from '@paxeer-network/paxeer-backend/core/paxscan/api';
import { PaxscanError } from '@paxeer-network/paxeer-backend/core/paxscan/api';

// The page-param is Blockscout's `next_page_params` — an opaque cursor object
// whose exact shape is endpoint-specific. Kept loose in Phase 1 (see Agent 2
// plan §1 risks); per-resource tightening waits on Agent 1 filter-type freezes.
export type PaxscanPageParam =
  | Record<string, string | Array<string> | number | boolean | null | undefined>
  | null;

export interface UseBlockscoutInfiniteQueryOptions<R extends PaginatedResourceName> {
  pathParams?: PathParams<R>;
  queryParams?: QueryParams<R>;
  queryOptions?: Omit<
    UseInfiniteQueryOptions<
      ResourcePayload<R>,
      PaxscanError,
      InfiniteData<ResourcePayload<R>>,
      QueryKey,
      PaxscanPageParam
    >,
    'queryKey' | 'queryFn' | 'initialPageParam' | 'getNextPageParam'
  >;
}

export function useBlockscoutInfiniteQuery<R extends PaginatedResourceName>(
  resource: R,
  opts: UseBlockscoutInfiniteQueryOptions<R> = {},
): UseInfiniteQueryResult<InfiniteData<ResourcePayload<R>>, PaxscanError> {
  const { pathParams, queryParams, queryOptions } = opts;

  return useInfiniteQuery<
    ResourcePayload<R>,
    PaxscanError,
    InfiniteData<ResourcePayload<R>>,
    QueryKey,
    PaxscanPageParam
  >({
    queryKey: [ 'paxscan', resource, pathParams ?? null, queryParams ?? null ],
    queryFn: ({ signal, pageParam }) => {
      const cursor = (pageParam ?? {}) as Record<string, unknown>;
      const mergedQuery = {
        ...(queryParams ?? {}),
        ...cursor,
      } as QueryParams<R>;
      const url = buildUrl(resource, pathParams, mergedQuery);
      const def = RESOURCES[resource] as { headers?: Record<string, string> };
      const init: RequestInit = { signal };
      if (def.headers) init.headers = def.headers;
      return paxscanFetch<ResourcePayload<R>>(url, init);
    },
    initialPageParam: null,
    getNextPageParam: (lastPage) => {
      const maybe = lastPage as { next_page_params?: PaxscanPageParam } | null;
      return maybe?.next_page_params ?? null;
    },
    ...queryOptions,
  });
}
