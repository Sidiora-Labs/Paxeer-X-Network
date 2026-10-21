'use client';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useOperations } from '@/core/market/operations';
import { loadable, type Loadable } from '@/core/market/unsupported';
import { cupOperations, cupStandingsLimit, fetchCupResults, fetchCupStandings, fetchCurrentCup, type Cup, type CupResults, type CupStandingsPage } from './cups-contract';


/**
 * IF_CUP_CURRENT `GET /v2/cups/current`: typed in the SDK, no serving handler
 * yet. The real 404 becomes an Unsupported marker; a served `cup: null` is a
 * genuine "no competition scheduled" answer.
 */
export function useCurrentCup(): Loadable<Cup | null> {
  const operations = useOperations();
  const query = useQuery({
    queryKey: queryKeys.cupCurrent(),
    queryFn: ({ signal }) => fetchCurrentCup(operations, signal),
    ...marketCachePolicy.cups,
  });
  return loadable(query, cupOperations.current);
}

export function useCupResults(cupId: string): Loadable<CupResults> {
  const operations = useOperations();
  const query = useQuery({
    queryKey: queryKeys.cupResults(cupId),
    queryFn: ({ signal }) => fetchCupResults(operations, cupId, signal),
    ...marketCachePolicy.cups,
  });
  return loadable(query, cupOperations.results);
}

export function useCupStandings(cupId: string, cursor: string | null = null, limit: number = cupStandingsLimit): Loadable<CupStandingsPage> {
  const operations = useOperations();
  const query = useQuery({
    queryKey: queryKeys.cupStandings(cupId, cursor, limit),
    queryFn: ({ signal }) => fetchCupStandings(operations, cupId, cursor, limit, signal),
    ...marketCachePolicy.cups,
  });
  return loadable(query, cupOperations.standings);
}

/** Re-request every cup query (the Unsupported marker has no refetch handle of its own). */
export function useCupRetry(cupId: string | null = null): () => void {
  const client = useQueryClient();
  return () => {
    void client.invalidateQueries({ queryKey: queryKeys.cups() });
    if (cupId) void client.invalidateQueries({ queryKey: queryKeys.cup(cupId) });
  };
}
