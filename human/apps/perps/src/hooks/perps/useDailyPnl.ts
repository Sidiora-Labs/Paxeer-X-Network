"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQuery, perfdbRest } from "@/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
export interface DailyPnl {
  date: string; // ISO date string, e.g. "2024-01-15"
  pnl: string; // decimal string, may be negative
  volume: string; // decimal string
  trades: number;
}

// ── Query (schema confirms: userDailyPnl(address, limit) -> [GqlDailyPnl]) ──
const USER_DAILY_PNL_QUERY = /* GraphQL */ `
  query UserDailyPnl($address: String!, $limit: Int!) {
    userDailyPnl(address: $address, limit: $limit) {
      date
      pnl
      volume
      trades
    }
  }
`;

interface DailyPnlQueryResponse {
  userDailyPnl: DailyPnl[];
}

interface DailyPnlRestResponse {
  dailyPnl?: DailyPnl[];
  data?: DailyPnl[];
}

/**
 * Fetches a trader's daily PnL history from PerfDB.
 * Tries GraphQL first; falls back to REST if GraphQL fails.
 * No polling — refetches only when address changes.
 */
export function useDailyPnl(address: string | undefined): {
  dailyPnl: DailyPnl[];
  isLoading: boolean;
  error: Error | null;
} {
  const [dailyPnl, setDailyPnl] = useState<DailyPnl[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);

  const fetchDailyPnl = useCallback(async (addr: string): Promise<void> => {
    try {
      // Try GraphQL first
      const gqlRes = await perfdbQuery<DailyPnlQueryResponse>(
        USER_DAILY_PNL_QUERY,
        { address: addr, limit: 90 }
      );
      if (!mountedRef.current) return;
      // Sort ascending by date so .slice(-7) gives the most recent 7 days
      const sorted = [...(gqlRes.userDailyPnl ?? [])].sort((a, b) =>
        a.date.localeCompare(b.date)
      );
      setDailyPnl(sorted);
      setError(null);
    } catch (gqlErr) {
      // Fall back to REST
      try {
        const restRes = await perfdbRest<DailyPnlRestResponse | DailyPnl[]>(
          `/v2/user/${addr}/daily-pnl`
        );
        if (!mountedRef.current) return;
        const list: DailyPnl[] = Array.isArray(restRes)
          ? restRes
          : (restRes.dailyPnl ?? restRes.data ?? []);
        const sorted = [...list].sort((a, b) => a.date.localeCompare(b.date));
        setDailyPnl(sorted);
        setError(null);
      } catch (restErr) {
        if (!mountedRef.current) return;
        const err =
          restErr instanceof Error ? restErr : new Error(String(restErr));
        setError(err);
        setDailyPnl([]);
      }
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!address) {
      setDailyPnl([]);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchDailyPnl(address);
    return () => {
      mountedRef.current = false;
    };
  }, [address, fetchDailyPnl]);

  return { dailyPnl, isLoading, error };
}
