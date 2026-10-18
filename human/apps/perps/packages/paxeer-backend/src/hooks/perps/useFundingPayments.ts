"use client";

// One-shot funding-payment history fetch (no auto-poll). Goes through
// the shared in-flight dedup so multiple components asking for the
// same params share a single round-trip.

import { useSharedPolledQuery } from "./useGraphQLQuery";
import { INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";
import { FUNDING_PAYMENTS_QUERY } from "./useSubgraph";
import type { FundingPayment } from "@paxeer-network/paxeer-backend/types/perps";

export function useFundingPayments(params?: {
  userAddress?: string;
  marketId?: number;
  positionId?: string;
  limit?: number;
  offset?: number;
}) {
  return useSharedPolledQuery<{ fundingPayments: FundingPayment[] }, FundingPayment[]>({
    endpoint: INDEXER_GRAPHQL,
    query: FUNDING_PAYMENTS_QUERY,
    variables: {
      userAddress: params?.userAddress,
      marketId: params?.marketId,
      positionId: params?.positionId,
      limit: params?.limit ?? 50,
      offset: params?.offset,
    },
    // Mirror the legacy behavior: fetch on mount, no scheduled polling.
    intervalMs: 0,
    pick: (res) => res.fundingPayments,
  });
}
