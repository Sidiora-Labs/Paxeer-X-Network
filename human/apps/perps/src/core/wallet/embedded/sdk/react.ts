/**
 * Vendored from `@paxeer/wallet/react` v0.1.0. See ./types.ts for vendoring policy.
 *
 * React bindings — opt-in. Pure functions, no context provider required at
 * this layer; Hyperpax-UI wires its own `PaxeerProvider` on top of these.
 */

import { useEffect, useMemo, useState } from "react";
import type { Session, User } from "@supabase/supabase-js";
import { PaxeerWallet } from "./index";
import type { ChainInfo, PaxeerWalletConfig, PublicWallet } from "./types";

export interface UseSessionResult {
  session: Session | null;
  user: User | null;
  loading: boolean;
}

export function useSession(wallet: PaxeerWallet): UseSessionResult {
  const [session, setSession] = useState<Session | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    void (async () => {
      const s = await wallet.getSession();
      if (!alive) return;
      setSession(s);
      setUser(s?.user ?? null);
      setLoading(false);
    })();
    const unsub = wallet.onAuthStateChange((_event, s) => {
      setSession(s);
      setUser(s?.user ?? null);
    });
    return () => {
      alive = false;
      unsub();
    };
  }, [wallet]);

  return { session, user, loading };
}

export interface UseWalletResult {
  wallet: PublicWallet | null;
  chain: ChainInfo | null;
  loading: boolean;
  error: Error | null;
  refresh: () => void;
}

export function useWallet(client: PaxeerWallet): UseWalletResult {
  const [wallet, setWallet] = useState<PublicWallet | null>(null);
  const [chain, setChain] = useState<ChainInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    void (async () => {
      try {
        const session = await client.getSession();
        if (!session) {
          if (alive) {
            setWallet(null);
            setChain(null);
            setLoading(false);
          }
          return;
        }
        const r = await client.getWallet();
        if (!alive) return;
        setWallet(r.wallet);
        setChain(r.chain);
        setError(null);
      } catch (err) {
        if (alive) setError(err as Error);
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [client, tick]);

  return useMemo(
    () => ({ wallet, chain, loading, error, refresh: () => setTick((t) => t + 1) }),
    [wallet, chain, loading, error],
  );
}

export function usePaxeerWallet(config: PaxeerWalletConfig): PaxeerWallet {
  return useMemo(
    () => new PaxeerWallet(config),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [config.apiUrl, config.supabaseUrl, config.supabaseAnonKey],
  );
}
