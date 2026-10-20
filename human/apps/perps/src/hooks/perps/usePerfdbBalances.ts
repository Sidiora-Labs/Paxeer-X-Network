"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQuery } from "@/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
export interface PerfdbBalance {
  token: string;
  balance: string;
  totalDeposited: string;
  totalWithdrawn: string;
  depositCount: number;
  withdrawCount: number;
}

// ── Query (schema confirms all fields on GqlUserBalance) ────────────
const USER_BALANCES_QUERY = /* GraphQL */ `
  query UserBalances($address: String!) {
    userBalances(address: $address) {
      token
      balance
      totalDeposited
      totalWithdrawn
      depositCount
      withdrawCount
    }
  }
`;

interface UserBalancesResponse {
  userBalances: PerfdbBalance[];
}

/**
 * Fetches PerfDB aggregated balances (per-token: balance, totals, counts).
 * Fetch once when address changes.
 */
export function usePerfdbBalances(address: string | undefined): {
  balances: PerfdbBalance[];
  isLoading: boolean;
  error: Error | null;
} {
  const [balances, setBalances] = useState<PerfdbBalance[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);

  const fetchBalances = useCallback(async (addr: string): Promise<void> => {
    try {
      const res = await perfdbQuery<UserBalancesResponse>(
        USER_BALANCES_QUERY,
        { address: addr }
      );
      if (!mountedRef.current) return;
      setBalances(res.userBalances ?? []);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setBalances([]);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!address) {
      setBalances([]);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchBalances(address);
    return () => {
      mountedRef.current = false;
    };
  }, [address, fetchBalances]);

  return { balances, isLoading, error };
}
