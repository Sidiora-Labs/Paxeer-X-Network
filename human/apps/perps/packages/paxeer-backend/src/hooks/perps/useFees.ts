"use client";

// One-shot fee history fetch (no auto-poll). Goes through the shared
// in-flight dedup so multiple components asking for the same params
// share a single round-trip.

import { useSharedPolledQuery } from "./useGraphQLQuery";
import { INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";
import { FEES_QUERY } from "./useSubgraph";
import type { Fee } from "@paxeer-network/paxeer-backend/types/perps";

export function useFees(params?: {
  userAddress?: string;
  marketId?: number;
  positionId?: string;
  feeType?: number;
  limit?: number;
  offset?: number;
}) {
  return useSharedPolledQuery<{ fees: Fee[] }, Fee[]>({
    endpoint: INDEXER_GRAPHQL,
    query: FEES_QUERY,
    variables: {
      userAddress: params?.userAddress,
      marketId: params?.marketId,
      positionId: params?.positionId,
      feeType: params?.feeType,
      limit: params?.limit ?? 50,
      offset: params?.offset,
    },
    // Mirror the legacy behavior: fetch on mount, no scheduled polling.
    intervalMs: 0,
    pick: (res) => res.fees,
  });
}
