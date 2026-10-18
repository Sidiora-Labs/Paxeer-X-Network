"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQuery } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
export interface PerfdbTraderStats {
  address: string;
  totalPnl: string;
  totalVolume: string;
  totalFeesPaid: string;
  totalTrades: number;
  winCount: number;
  lossCount: number;
  liquidationCount: number;
  openPositions: number;
}

// ── Query (schema confirms all fields on GqlTraderStats) ────────────
const USER_STATS_QUERY = /* GraphQL */ `
  query UserStats($address: String!) {
    userStats(address: $address) {
      address
      totalPnl
      totalVolume
      totalFeesPaid
      totalTrades
      winCount
      lossCount
      liquidationCount
      openPositions
    }
  }
`;

interface UserStatsResponse {
  userStats: PerfdbTraderStats | null;
}

/**
 * Fetches enhanced trader stats from PerfDB.
 * Fetch once when address changes. No polling.
 */
export function usePerfdbTraderStats(address: string | undefined): {
  stats: PerfdbTraderStats | null;
  isLoading: boolean;
  error: Error | null;
} {
  const [stats, setStats] = useState<PerfdbTraderStats | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);

  const fetchStats = useCallback(async (addr: string): Promise<void> => {
    try {
      const res = await perfdbQuery<UserStatsResponse>(USER_STATS_QUERY, {
        address: addr,
      });
      if (!mountedRef.current) return;
      setStats(res.userStats ?? null);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setStats(null);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!address) {
      setStats(null);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchStats(address);
    return () => {
      mountedRef.current = false;
    };
  }, [address, fetchStats]);

  return { stats, isLoading, error };
}
