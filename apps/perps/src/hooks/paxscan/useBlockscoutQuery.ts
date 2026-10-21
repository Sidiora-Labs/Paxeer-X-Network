/**
 * Paxscan typed query hook.
 *
 * Thin `useQuery` wrapper around `buildUrl` + `paxscanFetch`. Consumes the
 * `QueryClient` already provided by Hyperpax's `_app.tsx` — does NOT create
 * or own one.
 *
 * Query key shape: `['paxscan', resource, pathParams, queryParams]` so all
 * paxscan cache entries share a top-level namespace and can be invalidated
 * collectively via `queryClient.invalidateQueries({ queryKey: ['paxscan'] })`.
 */

import type { UseQueryOptions, UseQueryResult } from '@tanstack/react-query';
import { useQuery } from '@tanstack/react-query';

import {
  RESOURCES,
  buildUrl,
  paxscanFetch,
} from '@/core/paxscan/api';
import type {
  PathParams,
  QueryParams,
  ResourceName,
  ResourcePayload,
} from '@/core/paxscan/api';
import { PaxscanError } from '@/core/paxscan/api';

export interface UseBlockscoutQueryOptions<R extends ResourceName> {
  pathParams?: PathParams<R>;
  queryParams?: QueryParams<R>;
  queryOptions?: Omit<
    UseQueryOptions<ResourcePayload<R>, PaxscanError>,
    'queryKey' | 'queryFn'
  >;
}

export function useBlockscoutQuery<R extends ResourceName>(
  resource: R,
  opts: UseBlockscoutQueryOptions<R> = {},
): UseQueryResult<ResourcePayload<R>, PaxscanError> {
  const { pathParams, queryParams, queryOptions } = opts;

  return useQuery<ResourcePayload<R>, PaxscanError>({
    queryKey: [ 'paxscan', resource, pathParams ?? null, queryParams ?? null ],
    queryFn: ({ signal }) => {
      const url = buildUrl(resource, pathParams, queryParams);
      const def = RESOURCES[resource] as { headers?: Record<string, string> };
      const init: RequestInit = { signal };
      if (def.headers) init.headers = def.headers;
      return paxscanFetch<ResourcePayload<R>>(url, init);
    },
    ...queryOptions,
  });
}
