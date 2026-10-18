'use client';

import { useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { useOperations } from '@/core/market/operations';
import { useMarketTopics } from '@/core/realtime/use-data-stream';
import { isRankingCategory, marketTopics } from '@/core/realtime/data-stream';
import { appendRankingPage, marketAddress } from '@/page-modules/token/market-session';
import type { RankingsResponse } from '@/widgets/home/types';
import { UserFacingError } from '@/core/user-facing-error';

type RankingSnapshot = V2ContractTypes['RankingSnapshot'];

/**
 * Project a v2 RankingSnapshot onto the ranking list shape the home/search
 * widgets read: rows ordered by rank, the requested offset/limit window, the
 * eligible total. Scores stay the server's decimal strings turned into numbers
 * for ordering only; `stats` is not part of a ranking projection.
 */
export function rankingList(snapshot: RankingSnapshot, limit: number, offset: number): RankingsResponse {
  if (snapshot.watermark.integrity === 'quarantined') throw new UserFacingError('Rankings are not loading right now, try again', new Error('Ranking integrity quarantined'));
  const items = [...snapshot.items]
    .sort((a, b) => a.rank - b.rank)
    .slice(offset, offset + limit)
    .map(row => ({ poolAddress: marketAddress(row.poolAddress), score: Number(row.score), rank: row.rank, stats: null }));
  return { category: snapshot.category, items, total: Number(snapshot.totalEligible), limit, offset };
}

/**
 * Ranking list for a category: IF_RANKINGS `GET /v2/rankings/{category}`
 * (core/api compatibility.go projectionRankings) for the first paint, then the
 * public stream topic `market.rankings.<category>` replaces it in place on every
 * recompute (each RankingEvent carries the full snapshot, `replace: true`).
 * The served handler rejects query strings, so offset/limit apply client-side.
 */
export function useRanking(
  category: string,
  limit: number,
  offset: number,
  opts?: { enabled?: boolean },
) {
  const enabled = (opts?.enabled ?? true) && isRankingCategory(category);
  const operations = useOperations();
  const qc = useQueryClient();
  const key = queryKeys.ranking(category, limit, offset);

  const install = useCallback((payload: unknown) => {
    const snapshot = payload as RankingSnapshot;
    if (snapshot.category !== category) return;
    try {
      qc.setQueryData(key, rankingList(snapshot, limit, offset));
    } catch {
      void qc.invalidateQueries({ queryKey: key });
    }
    // key is derived from category/limit/offset.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, category, limit, offset]);

  useMarketTopics(enabled ? [marketTopics.rankings(category as Parameters<typeof marketTopics.rankings>[0])] : [], {
    enabled,
    onSnapshot: snapshot => { for (const entry of snapshot.topics) install(entry.payload); },
    onEvent: event => install(event.payload),
  });

  return useQuery<RankingsResponse | null>({
    queryKey: key,
    queryFn: async ({ signal }) => {
      if (!isRankingCategory(category)) return null;
      const response = appendRankingPage(null, await operations.call('IF_RANKINGS', { path: { category } }, { signal }));
      if (response.data.snapshot.category !== category) throw new UserFacingError('Rankings are not loading right now, try again', new Error('Ranking category disagreement'));
      return rankingList(response.data.snapshot, limit, offset);
    },
    enabled,
    staleTime: 10_000,
  });
}
