'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useOperations } from './operations';
import { resolvePoolSnapshot, type PoolSnapshot } from './pool-resolver';

export type { PoolSnapshot, PoolIdentity } from './pool-resolver';

/**
 * Resolve a pool or token address to the pool identity and its snapshot from
 * served v2 routes (`/v2/pools/{p}`, `/v2/search`, `/v2/pools/{p}/stats`,
 * `/v2/tokens/{t}/metadata`). Unknown addresses fail with PoolNotFoundError
 * (status 404) which `toUnsupported` classifies.
 */
export function usePoolSnapshot(address: string | null | undefined): UseQueryResult<PoolSnapshot> {
  const operations = useOperations();
  const candidate = address && /^0x[0-9a-fA-F]{40}$/.test(address) ? address.toLowerCase() : null;
  return useQuery({
    queryKey: queryKeys.poolSnapshot(candidate ?? ''),
    queryFn: ({ signal }) => resolvePoolSnapshot(candidate as string, { operations }, signal),
    enabled: candidate !== null,
    ...marketCachePolicy.poolSnapshot,
  });
}
