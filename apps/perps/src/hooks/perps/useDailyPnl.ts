"use client";

import { useState, useEffect } from "react";
import { perfdbQuery, perfdbRest } from "@/core/perfdb/client";
import { parseWei } from "@/ui/utils/format";

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

/** Every gateway DailyPnl field is nullable. */
type GatewayDailyPnl = { [K in keyof DailyPnl]: DailyPnl[K] | null };

interface DailyPnlQueryResponse {
  userDailyPnl: (GatewayDailyPnl | null)[] | null;
}

interface DailyPnlRestResponse {
  dailyPnl?: (GatewayDailyPnl | null)[] | null;
  data?: (GatewayDailyPnl | null)[] | null;
}

export interface DailyPnlResult {
  /** Complete rows, ascending by date. */
  rows: DailyPnl[];
  /** Rows dropped for a missing date or a null/unparseable pnl, volume or trades. */
  skippedDays: number;
}

function isExactWei(v: unknown): v is string {
  if (typeof v !== "string") return false;
  try {
    parseWei(v);
    return true;
  } catch {
    return false;
  }
}

function isComplete(r: GatewayDailyPnl | null): r is DailyPnl {
  return (
    typeof r?.date === "string" &&
    isExactWei(r.pnl) &&
    isExactWei(r.volume) &&
    typeof r.trades === "number" &&
    Number.isFinite(r.trades)
  );
}

/** A null or missing list is a failed read, not an empty history. */
function completeAscending(list: (GatewayDailyPnl | null)[] | null | undefined, source: string): DailyPnlResult {
  if (!Array.isArray(list)) throw new Error(`PerfDB ${source} returned no daily PnL list`);
  const rows = list.filter(isComplete).sort((a, b) => a.date.localeCompare(b.date));
  return { rows, skippedDays: list.length - rows.length };
}

/**
 * Fetches a trader's daily PnL history from PerfDB.
 * Tries GraphQL first; falls back to REST if GraphQL fails or returns no list; throws when both fail.
 */
export async function fetchDailyPnl(addr: string): Promise<DailyPnlResult> {
  try {
    const gqlRes = await perfdbQuery<DailyPnlQueryResponse>(USER_DAILY_PNL_QUERY, { address: addr, limit: 90 });
    return completeAscending(gqlRes?.userDailyPnl, "GraphQL");
  } catch {
    const restRes = await perfdbRest<DailyPnlRestResponse | (GatewayDailyPnl | null)[] | null>(`/v2/user/${addr}/daily-pnl`);
    return completeAscending(Array.isArray(restRes) ? restRes : (restRes?.dailyPnl ?? restRes?.data), "REST");
  }
}

/** Daily PnL for an address. No polling — refetches only when address changes. */
export function useDailyPnl(address: string | undefined): {
  dailyPnl: DailyPnl[];
  skippedDays: number;
  isLoading: boolean;
  error: Error | null;
} {
  const [dailyPnl, setDailyPnl] = useState<DailyPnl[]>([]);
  const [skippedDays, setSkippedDays] = useState(0);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    if (!address) {
      setDailyPnl([]);
      setSkippedDays(0);
      setIsLoading(false);
      setError(null);
      return;
    }
    // A superseded address's late response must not overwrite the current one.
    let cancelled = false;
    setIsLoading(true);
    fetchDailyPnl(address)
      .then(
        (result) => {
          if (cancelled) return;
          setDailyPnl(result.rows);
          setSkippedDays(result.skippedDays);
          setError(null);
        },
        (err: unknown) => {
          if (cancelled) return;
          setError(err instanceof Error ? err : new Error(String(err)));
          setDailyPnl([]);
          setSkippedDays(0);
        },
      )
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [address]);

  return { dailyPnl, skippedDays, isLoading, error };
}
