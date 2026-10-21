'use client';
import { useMemo } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import type { V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { useGatewayOperations } from '@/core/market/operations';
import { toUnsupported, type Loadable } from '@/core/market/unsupported';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { bountiesContract, type Bounty } from './bounties-contract';

export type BountySubmission = V2ContractTypes['BountySubmission'];
export type BountySubmissionsResponse = V2ContractTypes['BountySubmissionsResponse'];

export const SUBMISSIONS_OPERATION = 'IF_BOUNTY_SUBMISSIONS';
const SUBMISSIONS_LIMIT = 24;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const UINT = /^(0|[1-9][0-9]{0,77})$/;
const SUBMISSION_STATES: ReadonlySet<string> = new Set<BountySubmission['state']>(['submitted', 'accepted', 'rejected', 'withdrawn']);

export function validateSubmission(submission: BountySubmission, bountyId: string): BountySubmission {
  if (!submission.submissionId || submission.bountyId !== bountyId || !SUBMISSION_STATES.has(submission.state) || !ADDRESS.test(submission.authorWallet)) throw new Error('Submission shape disagreement');
  if (submission.awardRaw !== null && !UINT.test(submission.awardRaw)) throw new Error('Submission award disagreement');
  if (!Array.isArray(submission.evidenceUrls) || submission.evidenceUrls.some(url => typeof url !== 'string')) throw new Error('Submission evidence disagreement');
  return submission;
}

export function validateSubmissions(response: BountySubmissionsResponse, bountyId: string): BountySubmissionsResponse {
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Submission list integrity quarantined');
  const seen = new Set<string>();
  for (const item of response.data.items) {
    validateSubmission(item, bountyId);
    if (seen.has(item.submissionId)) throw new Error('Submission identity disagreement');
    seen.add(item.submissionId);
  }
  return response;
}

export interface BountySubmissionsResult {
  loadable: Loadable<BountySubmission[]>;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore(): void;
}

/** IF_BOUNTY_SUBMISSIONS `GET /v2/bounties/{bountyId}/submissions` paged by the server cursor. */
export function useBountySubmissions(bountyId: string | null): BountySubmissionsResult {
  const operations = useGatewayOperations();
  const query = useInfiniteQuery({
    queryKey: queryKeys.bountySubmissions(bountyId ?? ''),
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => validateSubmissions(
      await operations.call(
        'IF_BOUNTY_SUBMISSIONS',
        { path: { bountyId: bountyId as string }, query: pageParam ? { limit: SUBMISSIONS_LIMIT, cursor: pageParam } : { limit: SUBMISSIONS_LIMIT } },
        { signal },
      ),
      bountyId as string,
    ),
    getNextPageParam: (last: BountySubmissionsResponse) => last.nextCursor,
    enabled: bountyId !== null,
    retry: false,
    staleTime: 15_000,
    gcTime: 60_000,
  });
  const items = useMemo(() => query.data?.pages.flatMap(page => page.data.items) ?? [], [query.data]);
  const loadable = useMemo<Loadable<BountySubmission[]>>(() => {
    if (bountyId === null || query.status === 'pending') return { state: 'loading' };
    if (query.status === 'error') {
      const marker = toUnsupported(SUBMISSIONS_OPERATION, query.error);
      if (marker) return marker;
      const error = query.error instanceof Error ? query.error : new Error(String(query.error));
      return { state: 'error', error, retry: () => { void query.refetch(); } };
    }
    return { state: 'ready', data: items };
  }, [bountyId, query, items]);
  return {
    loadable,
    hasMore: query.hasNextPage,
    loadingMore: query.isFetchingNextPage,
    loadMore: () => { if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage(); },
  };
}

export interface RewardAsset {
  kind: 'usdl' | 'token';
  symbol: string;
  /** Null while the linked pool resolves or when the token reports no verified decimals. */
  decimals: number | null;
  pending: boolean;
}

/** Symbol and decimals of a bounty's escrowed asset: USDL from the deployment, launched tokens from the linked pool snapshot. */
export function useRewardAsset(bounty: Pick<Bounty, 'assetAddress' | 'poolAddress'> | null): RewardAsset {
  const kind = bounty ? bountiesContract.rewardAssetKind(bounty.assetAddress) : 'usdl';
  const snapshot = usePoolSnapshot(bounty && kind === 'token' ? bounty.poolAddress : null);
  if (!bounty || kind === 'usdl') return { kind: 'usdl', symbol: 'USDL', decimals: bountiesContract.usdlDecimals, pending: false };
  const token = snapshot.data?.token ?? null;
  const matches = token !== null && token.address.toLowerCase() === bounty.assetAddress.toLowerCase();
  return {
    kind: 'token',
    symbol: matches ? token.symbol : 'token',
    decimals: matches && token.decimalsQuality === 'verified' ? token.decimals : null,
    pending: bounty.poolAddress !== null && snapshot.isPending,
  };
}
