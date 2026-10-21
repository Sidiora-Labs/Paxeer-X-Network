'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { Address } from 'viem';
import { failureReason } from './transaction-controls/runtime';

/** The one chain read the panel needs: ERC-20 `balanceOf` at the latest block (ChainTradeProvider.readBalance). */
export interface BalanceReader { readBalance(token: Address, account: Address): Promise<bigint> }

/**
 * A wallet balance may be read and shown only for an authenticated account
 * session (the header's sign-in) of exactly the connected wallet. A connected
 * or auto-reconnected wallet without that session is signed out.
 */
export function authenticatedFor(walletSelected: boolean, connected: string | null | undefined, sessionWallet: string | null | undefined): boolean {
  return walletSelected && !!connected && !!sessionWallet && connected.toLowerCase() === sessionWallet.toLowerCase();
}

/** Re-read cadence while signed in; stays inside the 10s freshness gate trade execution enforces. */
export const BALANCE_POLL_MS = 4000;

export interface LiveBalances {
  usdl: bigint | null;
  token: bigint | null;
  /** Time of the last successful chain read. */
  observedAt: number | undefined;
  /** Full reason of the most recent failed read; cleared by the next successful read. */
  error: string | undefined;
  refresh(): void;
}

export interface LiveBalanceArgs {
  reader: BalanceReader | null;
  usdl: Address | null;
  token: Address | null;
  account: Address | null;
  /** Only an authenticated session for `account` may read; otherwise nothing is held or shown. */
  enabled: boolean;
  /** Changes whenever previously read values must be discarded (pool, session epoch). */
  scope?: string;
  intervalMs?: number;
}

/**
 * Live USDL and token balances of the signed-in wallet, read straight from
 * chain 125 and re-read every BALANCE_POLL_MS while the page is visible. When
 * the session ends or the account changes, held values are dropped at once.
 */
export function useLiveBalances({ reader, usdl, token, account, enabled, scope = '', intervalMs = BALANCE_POLL_MS }: LiveBalanceArgs): LiveBalances {
  const [state, setState] = useState<Omit<LiveBalances, 'refresh'>>({ usdl: null, token: null, observedAt: undefined, error: undefined });
  const [tick, setTick] = useState(0);
  const generation = useRef(0);
  const active = enabled && !!reader && !!usdl && !!token && !!account;
  const key = active ? `${account!.toLowerCase()}|${usdl!.toLowerCase()}|${token!.toLowerCase()}|${scope}` : '';

  useEffect(() => {
    generation.current++;
    setState({ usdl: null, token: null, observedAt: undefined, error: undefined });
  }, [key]);

  useEffect(() => {
    if (!active) return;
    const mine = generation.current;
    let live = true;
    void Promise.all([reader!.readBalance(usdl!, account!), reader!.readBalance(token!, account!)]).then(([usdlRaw, tokenRaw]) => {
      if (live && mine === generation.current) setState({ usdl: usdlRaw, token: tokenRaw, observedAt: Date.now(), error: undefined });
    }).catch((error: unknown) => {
      if (live && mine === generation.current) setState(previous => ({ ...previous, error: failureReason(error) }));
    });
    return () => { live = false; };
    // `key` covers account/token/scope identity; `reader` identity changes with the wallet client.
  }, [active, key, reader, tick]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!active || intervalMs <= 0) return;
    const timer = setInterval(() => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return;
      setTick(value => value + 1);
    }, intervalMs);
    const onVisible = () => { if (document.visibilityState === 'visible') setTick(value => value + 1); };
    document.addEventListener('visibilitychange', onVisible);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', onVisible); };
  }, [active, key, intervalMs]);

  const refresh = useCallback(() => setTick(value => value + 1), []);
  return active ? { ...state, refresh } : { usdl: null, token: null, observedAt: undefined, error: undefined, refresh };
}
