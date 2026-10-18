"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQuery } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
export interface PerfdbUser {
  address: string;
  vaultAddress: string;
  referralCode: string | null;
  tier: string | null; // e.g. "bronze", "silver", "gold", "platinum", "diamond"
  feeDiscountBps: number; // basis points, e.g. 50 = 0.5%
}

// ── Query (schema confirms GqlUser fields) ──────────────────────────
const USER_QUERY = /* GraphQL */ `
  query User($address: String!) {
    user(address: $address) {
      address
      vaultAddress
      referralCode
      tier
      feeDiscountBps
    }
  }
`;

interface UserResponse {
  user: PerfdbUser | null;
}

/**
 * Fetches PerfDB user profile (vault, referral code, tier, fee discount).
 * Fetch once when address changes.
 */
export function usePerfdbUser(address: string | undefined): {
  user: PerfdbUser | null;
  isLoading: boolean;
  error: Error | null;
} {
  const [user, setUser] = useState<PerfdbUser | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);

  const fetchUser = useCallback(async (addr: string): Promise<void> => {
    try {
      const res = await perfdbQuery<UserResponse>(USER_QUERY, {
        address: addr,
      });
      if (!mountedRef.current) return;
      setUser(res.user ?? null);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setUser(null);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!address) {
      setUser(null);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchUser(address);
    return () => {
      mountedRef.current = false;
    };
  }, [address, fetchUser]);

  return { user, isLoading, error };
}
