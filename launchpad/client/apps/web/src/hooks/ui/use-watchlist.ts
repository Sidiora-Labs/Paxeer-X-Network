'use client';
import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { normalizeAddress } from '@/widgets/profile/account-disclosure';
import { accountSession, subscribeAccount } from '@/widgets/profile/account-transport';
import { dismissWatchOperation, mergeLocalWatchlist, mutateWatch, refreshWatchlist, retryWatchOperation, selectWatchScope, subscribeWatch, watchState } from '@/widgets/profile/watchlist-state';
const empty: readonly string[] = [];
export function useWatchlist() {
  const auth = useAccountSession();
  const state = useSyncExternalStore(subscribeWatch, watchState, watchState);
  const matches = state.scope === auth.session;
  const list = matches ? state.list : empty;
  useEffect(() => {
    const select = () => { selectWatchScope(); void refreshWatchlist(); };
    select(); const unsubscribe = subscribeAccount(select);
    const refresh = () => { void refreshWatchlist(); };
    window.addEventListener('focus', refresh); window.addEventListener('online', refresh); window.addEventListener('storage', refresh);
    const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('kindle-account-watchlist');
    if (channel) channel.onmessage = event => { if (event.data?.wallet?.toLowerCase() === accountSession()?.session.wallet.toLowerCase()) refresh(); };
    return () => { unsubscribe(); window.removeEventListener('focus', refresh); window.removeEventListener('online', refresh); window.removeEventListener('storage', refresh); channel?.close(); };
  }, [auth.session]);
  const toggle = useCallback((poolAddress: string) => { void mutateWatch(poolAddress, !list.includes(normalizeAddress(poolAddress))).catch(() => undefined); }, [list]);
  const check = useCallback((poolAddress: string) => list.includes(normalizeAddress(poolAddress)), [list]);
  return { list, toggle, check, pending: matches ? state.pending : [], localMerge: matches && !!auth.session ? state.local : [], error: matches ? state.error : null, loading: state.loading, isLocal: !auth.session, refresh: refreshWatchlist, merge: mergeLocalWatchlist, retry: retryWatchOperation, dismiss: dismissWatchOperation };
}
