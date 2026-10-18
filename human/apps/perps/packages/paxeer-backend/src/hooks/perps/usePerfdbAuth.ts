"use client";
import { fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbRest, PERFDB_URL } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
interface AuthState {
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  expiresAt: number | null; // unix ms
}

export interface UsePerfdbAuthReturn extends AuthState {
  authenticate: (
    address: string,
    signMessage: (message: string) => Promise<string>
  ) => Promise<void>;
  refresh: () => Promise<void>;
  logout: () => void;
}

interface ChallengeResponse {
  nonce: string;
  message: string;
}

interface VerifyResponse {
  token: string;
  expires_in: number; // seconds
}

interface RefreshResponse {
  token: string;
  expires_in: number;
}

// ── LocalStorage keys ───────────────────────────────────────────────
const JWT_KEY = "perfdb_jwt";
const JWT_EXPIRES_KEY = "perfdb_jwt_expires";

// ── Helpers (SSR-safe) ──────────────────────────────────────────────
function readStoredToken(): {
  token: string | null;
  expiresAt: number | null;
} {
  if (typeof window === "undefined") return { token: null, expiresAt: null };
  try {
    const token = window.localStorage.getItem(JWT_KEY);
    const rawExp = window.localStorage.getItem(JWT_EXPIRES_KEY);
    const expiresAt = rawExp ? parseInt(rawExp, 10) : null;
    if (!token) return { token: null, expiresAt: null };
    if (expiresAt && !isNaN(expiresAt) && Date.now() >= expiresAt) {
      // expired
      return { token: null, expiresAt: null };
    }
    return { token, expiresAt: expiresAt && !isNaN(expiresAt) ? expiresAt : null };
  } catch {
    return { token: null, expiresAt: null };
  }
}

function writeStoredToken(token: string, expiresAt: number): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(JWT_KEY, token);
    window.localStorage.setItem(JWT_EXPIRES_KEY, String(expiresAt));
  } catch {
    // ignore (private mode, quota, etc.)
  }
}

function clearStoredToken(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(JWT_KEY);
    window.localStorage.removeItem(JWT_EXPIRES_KEY);
  } catch {
    // ignore
  }
}

// ── Hook ─────────────────────────────────────────────────────────────

/**
 * SIWE-based authentication against PerfDB.
 * Signing is delegated — the caller passes a `signMessage` function
 * (e.g. wagmi's `useSignMessage().signMessageAsync`) so this hook
 * stays free of wallet imports.
 */
export function usePerfdbAuth(): UsePerfdbAuthReturn {
  const [state, setState] = useState<AuthState>(() => ({
    token: null,
    isAuthenticated: false,
    isLoading: true,
    expiresAt: null,
  }));

  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef<boolean>(true);

  const clearRefreshTimer = useCallback((): void => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }, []);

  const logout = useCallback((): void => {
    clearRefreshTimer();
    clearStoredToken();
    if (!mountedRef.current) return;
    setState({
      token: null,
      isAuthenticated: false,
      isLoading: false,
      expiresAt: null,
    });
  }, [clearRefreshTimer]);

  // Forward-declared ref so `scheduleRefresh` can safely call `refresh`
  // without introducing a cyclic dependency in useCallback deps.
  const refreshRef = useRef<() => Promise<void>>(async () => {});

  const scheduleRefresh = useCallback(
    (expiresInSec: number): void => {
      clearRefreshTimer();
      // Refresh 60s before expiry (but never schedule in the past)
      const delayMs = Math.max(10_000, (expiresInSec - 60) * 1000);
      refreshTimerRef.current = setTimeout(() => {
        refreshRef.current().catch(() => {
          // on failure, force logout
          logout();
        });
      }, delayMs);
    },
    [clearRefreshTimer, logout]
  );

  const applyNewToken = useCallback(
    (token: string, expiresInSec: number): void => {
      const expiresAt = Date.now() + expiresInSec * 1000;
      writeStoredToken(token, expiresAt);
      if (!mountedRef.current) return;
      setState({
        token,
        isAuthenticated: true,
        isLoading: false,
        expiresAt,
      });
      scheduleRefresh(expiresInSec);
    },
    [scheduleRefresh]
  );

  const authenticate = useCallback(
    async (
      address: string,
      signMessage: (message: string) => Promise<string>
    ): Promise<void> => {
      if (!address) throw new Error("Address is required for authentication");
      if (mountedRef.current) {
        setState((s) => ({ ...s, isLoading: true }));
      }
      try {
        const challenge = await perfdbRest<ChallengeResponse>(
          "/v2/auth/challenge",
          {
            method: "POST",
            body: JSON.stringify({ address }),
          }
        );
        const signature = await signMessage(challenge.message);
        const verify = await perfdbRest<VerifyResponse>("/v2/auth/verify", {
          method: "POST",
          body: JSON.stringify({
            address,
            signature,
            message: challenge.message,
          }),
        });
        applyNewToken(verify.token, verify.expires_in);
      } catch (err) {
        if (mountedRef.current) {
          setState((s) => ({ ...s, isLoading: false }));
        }
        throw err;
      }
    },
    [applyNewToken]
  );

  const refresh = useCallback(async (): Promise<void> => {
    const { token } = readStoredToken();
    if (!token) {
      logout();
      return;
    }
    try {
      const res = await fetch(`${PERFDB_URL}/v2/auth/refresh`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ token }),
      });
      if (!res.ok) throw new Error(`Refresh failed: ${res.status}`);
      const data = (await res.json()) as RefreshResponse;
      applyNewToken(data.token, data.expires_in);
    } catch {
      logout();
    }
  }, [applyNewToken, logout]);

  // Keep refresh ref in sync for scheduleRefresh
  useEffect(() => {
    refreshRef.current = refresh;
  }, [refresh]);

  // On mount: hydrate from localStorage
  useEffect(() => {
    mountedRef.current = true;
    const { token, expiresAt } = readStoredToken();
    if (token && expiresAt) {
      setState({
        token,
        isAuthenticated: true,
        isLoading: false,
        expiresAt,
      });
      const remainingSec = Math.floor((expiresAt - Date.now()) / 1000);
      if (remainingSec > 60) {
        scheduleRefresh(remainingSec);
      } else if (remainingSec > 0) {
        // Near-expiry: refresh immediately
        refreshRef.current().catch(() => logout());
      } else {
        logout();
      }
    } else {
      setState({
        token: null,
        isAuthenticated: false,
        isLoading: false,
        expiresAt: null,
      });
    }
    return () => {
      mountedRef.current = false;
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    ...state,
    authenticate,
    refresh,
    logout,
  };
}
