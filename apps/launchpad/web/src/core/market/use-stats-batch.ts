'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { Operations, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { marketAddress, marketLimits } from '@/page-modules/token/market-session';
import { useMarketClient } from './client';
import { useOperations } from './operations';

export type StatsBatchResponse = V2ContractTypes['StatsBatchResponse'];
/**
 * The v2 PoolStats row (interface 2.2.0). The 2.2.0 additions (window changes
 * with their baseline qualities, spotPriceWad, unique traders, transaction
 * count, supplies and holder shares) are optional in the contract; read them
 * through core/market/pool-stats-display, which states the per-field null
 * handling.
 */
export type PoolStats = V2ContractTypes['PoolStats'];

/** Index a batch response by lowercased pool address. */
export function statsByPool(response: StatsBatchResponse | undefined): ReadonlyMap<string, PoolStats> {
  return new Map(response?.data.items.map(row => [row.poolAddress.toLowerCase(), row]) ?? []);
}

/**
 * One IF_STATS_BATCH request for up to `marketLimits.rankingPage` pools with
 * the identity guards: a quarantined response, a row for a pool that was not
 * requested, a duplicate row or a quarantined row rejects the whole batch.
 */
export async function fetchStatsBatch(operations: Operations, addresses: readonly string[], signal?: AbortSignal): Promise<StatsBatchResponse> {
  const pools = [...new Set(addresses.map(marketAddress))].sort();
  if (pools.length === 0 || pools.length > marketLimits.rankingPage) throw new RangeError('Stats batch bound exceeded');
  const response = await operations.call('IF_STATS_BATCH', { body: { poolAddresses: pools } }, { signal });
  if (response.quality === 'quarantined') throw new Error('Statistics integrity quarantined');
  const seen = new Set<string>();
  for (const item of response.data.items) {
    const address = marketAddress(item.poolAddress);
    if (!pools.includes(address) || seen.has(address) || item.watermark.integrity === 'quarantined') throw new Error('Statistics batch identity disagreement');
    seen.add(address);
  }
  return response;
}

/**
 * IF_STATS_BATCH `POST /v2/pools/stats:batch`
 * (core/api/internal/rest/compatibility.go:91). The handler answers 503 when
 * any requested pool has no projection; that failure surfaces as the real
 * error, never as a partial synthetic batch.
 */
export function useStatsBatch(addresses: readonly string[]): UseQueryResult<StatsBatchResponse> {
  const client = useMarketClient();
  const operations = useOperations();
  const pools = [...new Set(addresses.map(marketAddress))].sort();
  if (pools.length > marketLimits.rankingPage) throw new RangeError('Stats batch exceeds 50 pools');
  return useQuery({
    queryKey: queryKeys.statsBatch(client.config.apiOrigin, pools),
    queryFn: ({ signal }) => fetchStatsBatch(operations, pools, signal),
    enabled: pools.length > 0,
    ...marketCachePolicy.statsBatch,
  });
}
