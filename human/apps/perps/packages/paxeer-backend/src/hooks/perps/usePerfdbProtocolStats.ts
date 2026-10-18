"use client";

// Polled wrapper around the PerfDB `protocolStats` query. All
// subscribers across the app share a single timer + fetch via
// `useSharedPolledQuery`. Polling pauses when the tab is hidden.

import { useSharedPolledQuery } from "./useGraphQLQuery";
import { PERFDB_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Interface (matching PerfDB GqlProtocolStats) ───────────────────────
export interface PerfdbProtocolStats {
  totalUsers: number;
  totalVolume: string;
  totalTrades: number;
  totalFees: string;
  netTraderPnl: string;
}

const POLL_MS = 30_000;

const PROTOCOL_STATS_QUERY = /* GraphQL */ `
  query {
    protocolStats {
      totalUsers
      totalVolume
      totalTrades
      totalFees
      netTraderPnl
    }
  }
`;

export function usePerfdbProtocolStats() {
  const { data, isLoading, error } = useSharedPolledQuery<
    { protocolStats: PerfdbProtocolStats },
    PerfdbProtocolStats
  >({
    endpoint: PERFDB_GRAPHQL,
    query: PROTOCOL_STATS_QUERY,
    intervalMs: POLL_MS,
    pick: (res) => res.protocolStats,
  });
  return { stats: data ?? null, isLoading, error };
}
