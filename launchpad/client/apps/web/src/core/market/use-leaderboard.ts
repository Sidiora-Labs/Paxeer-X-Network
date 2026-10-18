'use client';
import { useQuery } from '@tanstack/react-query';
import type { OperationInput, V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useOperations } from './operations';
import { loadable, type Loadable } from './unsupported';

export type LeaderboardsResponse = V2ContractTypes['LeaderboardsResponse'];
export type LeaderboardCategory = OperationInput<'IF_LEADERBOARDS'>['path']['category'];
export type LeaderboardWindow = OperationInput<'IF_LEADERBOARDS'>['query']['window'];

export const leaderboardLimit = 100;

/** Quarantine, category/window and wallet/rank identity guards for an IF_LEADERBOARDS response. */
export function validateLeaderboard(response: LeaderboardsResponse, category: LeaderboardCategory, window: LeaderboardWindow, limit: number): LeaderboardsResponse {
  const snapshot = response.data.snapshot;
  const rows = snapshot.entries;
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined' || snapshot.watermark.integrity === 'quarantined') throw new Error('Leaderboard integrity quarantined');
  if (snapshot.category !== category || snapshot.window !== window) throw new Error('Leaderboard category or window disagreement');
  if (rows.length > limit || new Set(rows.map(row => row.wallet.toLowerCase())).size !== rows.length || new Set(rows.map(row => row.rank)).size !== rows.length) throw new Error('Leaderboard identity or rank disagreement');
  for (const row of rows) if (!/^0x[0-9a-fA-F]{40}$/.test(row.wallet) || !/^(0|[1-9][0-9]{0,77})$/.test(row.metricRaw)) throw new Error('Leaderboard entry shape disagreement');
  return response;
}

/**
 * IF_LEADERBOARDS `GET /v2/leaderboards/{category}` has no serving handler in
 * core/api today; the SDK turns the 404 into CapabilityUnavailableError and
 * this hook returns it as an Unsupported marker rather than synthetic rows.
 */
export function useLeaderboard(category: LeaderboardCategory, window: LeaderboardWindow, limit: number = leaderboardLimit): Loadable<LeaderboardsResponse> {
  const operations = useOperations();
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > leaderboardLimit) throw new RangeError('Invalid leaderboard page bound');
  const query = useQuery({
    queryKey: queryKeys.leaderboard(category, window, limit),
    queryFn: async ({ signal }) => validateLeaderboard(await operations.call('IF_LEADERBOARDS', { path: { category }, query: { window, limit } }, { signal }), category, window, limit),
    ...marketCachePolicy.leaderboard,
  });
  return loadable(query, 'IF_LEADERBOARDS');
}
