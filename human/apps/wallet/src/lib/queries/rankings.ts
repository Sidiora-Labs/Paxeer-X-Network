'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchEnrichedRankings, type EnrichedRankedToken, type RankingCategory } from '@/lib/api';
import { queryKeys } from './keys';

export type { EnrichedRankedToken, RankingCategory };

export function useRankingsQuery(category: RankingCategory, limit = 20) {
  return useQuery<EnrichedRankedToken[]>({
    queryKey: queryKeys.rankings(category),
    queryFn: () => fetchEnrichedRankings(category, limit),
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
    placeholderData: (prev) => prev,
  });
}
