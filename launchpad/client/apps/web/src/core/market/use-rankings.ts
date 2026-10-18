'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { OperationInput, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { appendRankingPage, marketLimits } from '@/page-modules/token/market-session';
import { useMarketClient } from './client';
import { useOperations } from './operations';

export type RankingsResponse = V2ContractTypes['RankingsResponse'];
export type RankingCategory = OperationInput<'IF_RANKINGS'>['path']['category'];

/**
 * IF_RANKINGS `GET /v2/rankings/{category}`
 * (core/api/internal/rest/compatibility.go:92, projectionRankings). The served
 * handler rejects any query string, so the first page is requested bare and
 * `limit` is applied client-side; a non-null cursor is sent as the contract
 * query and surfaces the handler's real 422 until pagination is served.
 */
export function useRankings(category: RankingCategory, cursor: string | null = null, limit: number = marketLimits.rankingPage): UseQueryResult<RankingsResponse> {
  const client = useMarketClient();
  const operations = useOperations();
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > marketLimits.rankingPage) throw new RangeError('Invalid ranking page bound');
  return useQuery({
    queryKey: queryKeys.rankings(client.config.apiOrigin, category, cursor, limit),
    queryFn: async ({ signal }) => {
      const response = await operations.call('IF_RANKINGS', cursor ? { path: { category }, query: { cursor } } : { path: { category } }, { signal });
      if (response.data.snapshot.category !== category) throw new Error('Ranking category disagreement');
      const page = appendRankingPage(null, response);
      const snapshot = page.data.snapshot;
      return snapshot.items.length > limit ? { ...page, data: { snapshot: { ...snapshot, items: [...snapshot.items].sort((a, b) => a.rank - b.rank).slice(0, limit), truncated: true } } } : page;
    },
    ...marketCachePolicy.rankings,
  });
}
