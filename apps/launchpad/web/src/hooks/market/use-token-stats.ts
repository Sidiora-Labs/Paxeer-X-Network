'use client';

import { useCallback } from 'react';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { Operations } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { cachePolicy } from '@/core/cache-policy';
import { useMarketClient } from '@/core/market/client';
import { useOperations } from '@/core/market/operations';
import { fetchStatsBatch, type PoolStats } from '@/core/market/use-stats-batch';
import { fetchPoolListing } from '@/core/market/pool-listings';
import { usdlRawToDecimal } from '@/core/market/market-units';
import { useMarketTopics } from '@/core/realtime/use-data-stream';
import { marketTopics } from '@/core/realtime/data-stream';
import { useRefetchOnAnyEvent } from '@/hooks/market/use-stream-refetch';
import { marketAddress, marketLimits } from '@/page-modules/token/market-session';
import { UserFacingError } from '@/core/user-facing-error';

export type { PoolStats };

/**
 * A v2 PoolStats row joined with its pool's token identity and a display
 * market cap. `marketCap` is the decimal-USDL form of
 * `totalSupplyValuationUsdlRaw` (price x total supply), null when the
 * projection has no verified valuation; the raw 6-decimal value is
 * `totalSupplyValuationUsdlRaw` itself.
 */
export interface TokenStatsRow extends PoolStats {
  tokenAddress: string;
  marketCap: string | null;
}

/**
 * IF_STATS `GET /v2/pools/{p}/stats` for the first paint; the public topic
 * `market.stats.<pool>` then replaces the cached row with every StatsEvent
 * payload (and the snapshot installed on each reconnect). No polling.
 */
export function useTokenStats(
  poolAddress: string | undefined | null,
  opts?: { enabled?: boolean },
) {
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const enabled = (opts?.enabled ?? true) && pool !== null;
  const operations = useOperations();
  const qc = useQueryClient();
  const key = queryKeys.tokenStats(pool ?? '');

  const install = useCallback((payload: unknown) => {
    const stats = payload as PoolStats;
    if (!pool || stats.poolAddress.toLowerCase() !== pool) return;
    if (stats.watermark.integrity === 'quarantined') { void qc.invalidateQueries({ queryKey: key }); return; }
    qc.setQueryData(key, stats);
    // key is derived from pool.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, pool]);

  useMarketTopics(pool ? [marketTopics.stats(pool)] : [], {
    enabled,
    onSnapshot: snapshot => { for (const entry of snapshot.topics) install(entry.payload); },
    onEvent: event => install(event.payload),
  });

  return useQuery<PoolStats | null>({
    queryKey: key,
    queryFn: async ({ signal }) => {
      if (!pool) return null;
      const response = await operations.call('IF_STATS', { path: { poolAddress: pool } }, { signal });
      if (response.quality === 'quarantined' || response.data.stats.watermark.integrity === 'quarantined') throw new UserFacingError('Coin stats are not loading right now, try again', new Error('Statistics integrity quarantined'));
      if (response.data.stats.poolAddress.toLowerCase() !== pool) throw new UserFacingError('Coin stats are not loading right now, try again', new Error('Statistics identity disagreement'));
      return response.data.stats;
    },
    enabled,
    // The stream is the freshness mechanism; no background poll.
    ...cachePolicy.REALTIME,
    refetchInterval: false,
  });
}

/** IF_STATS_BATCH rows joined with their token identities (session-cached IF_POOL_DETAIL), keyed by lowercased pool address. */
export async function fetchTokenStatsBatch(qc: QueryClient, operations: Operations, origin: string, pools: readonly string[], signal?: AbortSignal): Promise<Record<string, TokenStatsRow>> {
  const wanted = [...new Set(pools.map(marketAddress))].sort();
  if (wanted.length === 0) return {};
  const [batch, tokens] = await Promise.all([
    fetchStatsBatch(operations, wanted, signal),
    Promise.all(wanted.map(async pool => (await fetchPoolListing(qc, operations, origin, pool, signal)).tokenAddress)),
  ]);
  const tokenByPool = new Map(wanted.map((pool, index) => [pool, tokens[index]]));
  const out: Record<string, TokenStatsRow> = {};
  for (const stats of batch.data.items) {
    const pool = marketAddress(stats.poolAddress);
    out[pool] = { ...stats, tokenAddress: tokenByPool.get(pool) as string, marketCap: usdlRawToDecimal(stats.totalSupplyValuationUsdlRaw) };
  }
  return out;
}

/**
 * Stats for up to 50 pools in one IF_STATS_BATCH `POST /v2/pools/stats:batch`
 * request (core/api answers 503 when any requested pool has no projection; that
 * surfaces as the real error). Re-validated, throttled, whenever the trending
 * ranking projection on the public stream changes. No polling.
 */
export function useTokenStatsBatch(
  pools: string[],
  opts?: { enabled?: boolean },
) {
  const client = useMarketClient();
  const operations = useOperations();
  const qc = useQueryClient();
  const wanted = pools.filter(pool => /^0x[0-9a-fA-F]{40}$/.test(pool)).slice(0, marketLimits.rankingPage);
  const enabled = (opts?.enabled ?? true) && wanted.length > 0;

  useRefetchOnAnyEvent({ queryKeys: [queryKeys.tokenStatsBatch(wanted)], enabled });

  return useQuery<Record<string, TokenStatsRow>>({
    queryKey: queryKeys.tokenStatsBatch(wanted),
    queryFn: ({ signal }) => fetchTokenStatsBatch(qc, operations, client.config.apiOrigin, wanted, signal),
    enabled,
    ...cachePolicy.FAST,
    refetchInterval: false,
  });
}
