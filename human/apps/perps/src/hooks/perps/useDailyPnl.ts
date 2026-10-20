"use client";

import { useState, useEffect } from "react";
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

/** Gateway DailyPnl fields are nullable; a row without a date is unusable. */
type GatewayDailyPnl = Omit<DailyPnl, "date"> & { date: string | null };

interface DailyPnlQueryResponse {
  userDailyPnl: (GatewayDailyPnl | null)[] | null;
}

interface DailyPnlRestResponse {
  dailyPnl?: GatewayDailyPnl[];
  data?: GatewayDailyPnl[];
}

// Ascending by date so .slice(-7) gives the most recent 7 days.
function datedAscending(rows: (GatewayDailyPnl | null)[] | null | undefined): DailyPnl[] {
  return (rows ?? [])
    .filter((r): r is DailyPnl => typeof r?.date === "string")
    .sort((a, b) => a.date.localeCompare(b.date));
}

/**
 * Fetches a trader's daily PnL history from PerfDB.
 * Tries GraphQL first; falls back to REST if GraphQL fails; throws when both fail.
 */
export async function fetchDailyPnl(addr: string): Promise<DailyPnl[]> {
  try {
    const gqlRes = await perfdbQuery<DailyPnlQueryResponse>(USER_DAILY_PNL_QUERY, { address: addr, limit: 90 });
    return datedAscending(gqlRes.userDailyPnl);
  } catch {
    const restRes = await perfdbRest<DailyPnlRestResponse | GatewayDailyPnl[]>(`/v2/user/${addr}/daily-pnl`);
    return datedAscending(Array.isArray(restRes) ? restRes : (restRes.dailyPnl ?? restRes.data));
  }
}

/** Daily PnL for an address. No polling — refetches only when address changes. */
export function useDailyPnl(address: string | undefined): {
  dailyPnl: DailyPnl[];
  isLoading: boolean;
  error: Error | null;
} {
  const [dailyPnl, setDailyPnl] = useState<DailyPnl[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    if (!address) {
      setDailyPnl([]);
      setIsLoading(false);
      setError(null);
      return;
    }
    // A superseded address's late response must not overwrite the current one.
    let cancelled = false;
    setIsLoading(true);
    fetchDailyPnl(address)
      .then(
        (rows) => {
          if (cancelled) return;
          setDailyPnl(rows);
          setError(null);
        },
        (err: unknown) => {
          if (cancelled) return;
          setError(err instanceof Error ? err : new Error(String(err)));
          setDailyPnl([]);
        },
      )
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [address]);

  return { dailyPnl, isLoading, error };
}
