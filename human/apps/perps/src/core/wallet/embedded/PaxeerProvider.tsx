/**
 * PaxeerProvider — React context for the Paxeer Embedded Wallet.
 *
 * Wraps the new `@paxeer/wallet` SDK in a context so any component in the
 * tree can pull session state, kick off auth flows, and access the singleton
 * client. Backed by Supabase auth — all signing happens server-side via the
 * REST API (see `./sdk/index.ts`).
 *
 * The actual EIP-1193 provider used by wagmi is created in
 * `paxeerConnector.ts` from the same singleton, so the connector and this
 * provider stay in lockstep without prop drilling.
 */
"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { Session, User } from "@supabase/supabase-js";
import { PaxeerWallet, type OAuthProvider, type PublicWallet } from "./sdk";
import { getAuthRedirectUrl, getPaxeerWallet, resolvePaxeerConfig } from "./client";

// ─── Types ────────────────────────────────────────────────────────────────────

export type SignInProvider = OAuthProvider;

export interface EmbeddedSignInResult {
  pending: boolean; // true when an OAuth redirect is in progress
  emailSent: boolean; // true when a magic link was just dispatched
}

interface PaxeerContextValue {
  /** The SDK singleton, or null when env vars are missing. */
  wallet: PaxeerWallet | null;
  session: Session | null;
  user: User | null;
  publicWallet: PublicWallet | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  /** Set while an auth call is in flight. */
  authBusy: boolean;
  authError: string | null;
  signInWithEmail: (email: string) => Promise<{ ok: boolean; error?: string }>;
  signInWithOAuth: (provider: SignInProvider) => Promise<void>;
  signOut: () => Promise<void>;
  /** Force-refresh the cached wallet record (after a tx, etc.). */
  refresh: () => void;
}

const PaxeerContext = createContext<PaxeerContextValue | null>(null);

// ─── Provider ─────────────────────────────────────────────────────────────────

interface PaxeerProviderProps {
  children: ReactNode;
}

export function PaxeerProvider({ children }: PaxeerProviderProps) {
  const wallet = useMemo<PaxeerWallet | null>(() => getPaxeerWallet(), []);

  const [session, setSession] = useState<Session | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [publicWallet, setPublicWallet] = useState<PublicWallet | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [authBusy, setAuthBusy] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  // Hydrate session + subscribe to auth state changes.
  useEffect(() => {
    if (!wallet) {
      setIsLoading(false);
      return;
    }
    let alive = true;
    void (async () => {
      const s = await wallet.getSession();
      if (!alive) return;
      setSession(s);
      setUser(s?.user ?? null);
    })();
    const unsub = wallet.onAuthStateChange((_event, s) => {
      setSession(s);
      setUser(s?.user ?? null);
      if (!s) setPublicWallet(null);
    });
    return () => {
      alive = false;
      unsub();
    };
  }, [wallet]);

  // Provision/fetch the wallet record whenever we have an auth session.
  useEffect(() => {
    if (!wallet) {
      setIsLoading(false);
      return;
    }
    let alive = true;
    setIsLoading(true);
    void (async () => {
      try {
        if (!session) {
          if (alive) {
            setPublicWallet(null);
            setIsLoading(false);
          }
          return;
        }
        const r = await wallet.getWallet();
        if (!alive) return;
        setPublicWallet(r.wallet);
        setAuthError(null);
      } catch (err) {
        if (alive) setAuthError((err as Error).message);
      } finally {
        if (alive) setIsLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [wallet, session, refreshTick]);

  const signInWithEmail = useCallback(
    async (email: string): Promise<{ ok: boolean; error?: string }> => {
      if (!wallet) return { ok: false, error: "Embedded wallet not configured" };
      setAuthBusy(true);
      setAuthError(null);
      try {
        const { error } = await wallet.signInWithEmail(email, getAuthRedirectUrl());
        if (error) {
          setAuthError(error.message);
          return { ok: false, error: error.message };
        }
        return { ok: true };
      } finally {
        setAuthBusy(false);
      }
    },
    [wallet],
  );

  const signInWithOAuth = useCallback(
    async (provider: SignInProvider): Promise<void> => {
      if (!wallet) {
        setAuthError("Embedded wallet not configured");
        return;
      }
      setAuthBusy(true);
      setAuthError(null);
      try {
        await wallet.signInWithOAuth(provider, getAuthRedirectUrl());
        // Browser redirects away — control never returns here on success.
      } catch (err) {
        setAuthError((err as Error).message);
        setAuthBusy(false);
        throw err;
      }
    },
    [wallet],
  );

  const signOut = useCallback(async () => {
    if (!wallet) return;
    setAuthBusy(true);
    try {
      await wallet.signOut();
      setPublicWallet(null);
    } finally {
      setAuthBusy(false);
    }
  }, [wallet]);

  const refresh = useCallback(() => setRefreshTick((t) => t + 1), []);

  const value = useMemo<PaxeerContextValue>(
    () => ({
      wallet,
      session,
      user,
      publicWallet,
      isLoading,
      isAuthenticated: Boolean(session && publicWallet),
      authBusy,
      authError,
      signInWithEmail,
      signInWithOAuth,
      signOut,
      refresh,
    }),
    [
      wallet,
      session,
      user,
      publicWallet,
      isLoading,
      authBusy,
      authError,
      signInWithEmail,
      signInWithOAuth,
      signOut,
      refresh,
    ],
  );

  return <PaxeerContext.Provider value={value}>{children}</PaxeerContext.Provider>;
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function usePaxeer(): PaxeerContextValue {
  const ctx = useContext(PaxeerContext);
  if (!ctx) {
    throw new Error("usePaxeer must be used within <PaxeerProvider>");
  }
  return ctx;
}

/** Returns true when the embedded-wallet feature is configured (Supabase key set). */
export function useEmbeddedWalletAvailable(): boolean {
  return useMemo(() => resolvePaxeerConfig() !== null, []);
}
