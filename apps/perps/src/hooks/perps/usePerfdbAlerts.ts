"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQueryAuth, perfdbRestAuth, getAuthToken } from "@/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
export type AlertType =
  | "price_above"
  | "price_below"
  | "liquidation_warning"
  | "funding_threshold";

/**
 * Schema on server is `GqlAlert { id:Int!, alertType:String!, marketId:Int, threshold:String, enabled:Boolean!, triggeredAt:String }`.
 * We normalize `id` to string (Int → string) and expose `isActive` alias of `enabled`.
 */
export interface PerfdbAlert {
  id: string;
  alertType: AlertType;
  marketId: number | null;
  threshold: string | null;
  createdAt: string | null; // schema has no createdAt; triggeredAt used when available
  isActive: boolean;
}

interface RawAlert {
  id: number | string;
  alertType: string;
  marketId: number | null;
  threshold: string | null;
  enabled: boolean;
  triggeredAt: string | null;
}

// ── Query ────────────────────────────────────────────────────────────
const USER_ALERTS_QUERY = /* GraphQL */ `
  query UserAlerts($address: String!) {
    userAlerts(address: $address) {
      id
      alertType
      marketId
      threshold
      enabled
      triggeredAt
    }
  }
`;

interface UserAlertsResponse {
  userAlerts: RawAlert[];
}

function normalize(raw: RawAlert): PerfdbAlert {
  return {
    id: String(raw.id),
    alertType: raw.alertType as AlertType,
    marketId: raw.marketId,
    threshold: raw.threshold,
    createdAt: raw.triggeredAt,
    isActive: Boolean(raw.enabled),
  };
}

/**
 * Fetches & manages a user's alerts.
 * Reads require an address only (auth header is sent if present but server may allow read without it).
 * Mutations require JWT via `perfdbRestAuth`.
 */
export function usePerfdbAlerts(address: string | undefined): {
  alerts: PerfdbAlert[];
  isLoading: boolean;
  error: Error | null;
  createAlert: (
    alertType: AlertType,
    marketId?: number,
    threshold?: string
  ) => Promise<void>;
  deleteAlert: (id: string) => Promise<void>;
} {
  const [alerts, setAlerts] = useState<PerfdbAlert[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(address));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);
  const addressRef = useRef<string | undefined>(address);

  useEffect(() => {
    addressRef.current = address;
  }, [address]);

  const fetchAlerts = useCallback(async (addr: string): Promise<void> => {
    try {
      const res = await perfdbQueryAuth<UserAlertsResponse>(
        USER_ALERTS_QUERY,
        { address: addr }
      );
      if (!mountedRef.current) return;
      setAlerts((res.userAlerts ?? []).map(normalize));
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setAlerts([]);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!address) {
      setAlerts([]);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchAlerts(address);
    return () => {
      mountedRef.current = false;
    };
  }, [address, fetchAlerts]);

  const createAlert = useCallback(
    async (
      alertType: AlertType,
      marketId?: number,
      threshold?: string
    ): Promise<void> => {
      const token = getAuthToken();
      if (!token) {
        throw new Error("Not authenticated. Sign in first.");
      }
      const body: Record<string, unknown> = { alertType };
      if (typeof marketId === "number") body.marketId = marketId;
      if (threshold !== undefined && threshold !== "") body.threshold = threshold;
      await perfdbRestAuth<{ ok: boolean }>("/v2/alerts", {
        method: "POST",
        body: JSON.stringify(body),
      });
      // Refresh list
      if (addressRef.current) await fetchAlerts(addressRef.current);
    },
    [fetchAlerts]
  );

  const deleteAlert = useCallback(
    async (id: string): Promise<void> => {
      const token = getAuthToken();
      if (!token) {
        throw new Error("Not authenticated. Sign in first.");
      }
      await perfdbRestAuth<{ ok: boolean }>(
        `/v2/alerts/${encodeURIComponent(id)}`,
        { method: "DELETE" }
      );
      if (addressRef.current) await fetchAlerts(addressRef.current);
    },
    [fetchAlerts]
  );

  return { alerts, isLoading, error, createAlert, deleteAlert };
}
