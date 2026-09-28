'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  parseRouteUrl,
  serializeRoute,
  SHELL_ROUTE_NAMES,
  type ShellRoute,
  type ShellRouteName,
} from '@/domains/shell';

export const APP_ROUTES = SHELL_ROUTE_NAMES;
export type AppRoute = ShellRouteName;

interface PaxportHistoryState {
  paxportRoute: ShellRouteName;
  paxportSession: string;
  paxportDepth: number;
}

function historyState(
  value: unknown,
  session: string,
): PaxportHistoryState | null {
  if (
    typeof value !== 'object' ||
    value === null ||
    (value as Partial<PaxportHistoryState>).paxportSession !== session ||
    !Number.isSafeInteger((value as Partial<PaxportHistoryState>).paxportDepth) ||
    ((value as Partial<PaxportHistoryState>).paxportDepth ?? -1) < 0
  ) {
    return null;
  }
  return value as PaxportHistoryState;
}

function defaultRoute(name: AppRoute): ShellRoute {
  switch (name) {
    case 'send':
      return { name };
    case 'token-detail':
    case 'tx-detail':
      return { name: 'portfolio' };
    default:
      return { name };
  }
}

function routeFromExternal(input: unknown): ShellRoute | null {
  if (typeof input !== 'string' || input.length > 2_048) return null;
  if ((SHELL_ROUTE_NAMES as readonly string[]).includes(input)) {
    return defaultRoute(input as AppRoute);
  }
  try {
    const incoming = new URL(input, window.location.origin);
    if (incoming.protocol === 'web+paxeer:') {
      const normalized = new URL('/', window.location.origin);
      incoming.searchParams.forEach((value, key) => {
        normalized.searchParams.append(key, value);
      });
      const parsed = parseRouteUrl(normalized);
      return parsed.ok ? parsed.value : null;
    }
    if (incoming.origin !== window.location.origin) return null;
    const parsed = parseRouteUrl(incoming);
    return parsed.ok ? parsed.value : null;
  } catch {
    return null;
  }
}

export interface AppRouteState {
  routeState: ShellRoute;
  route: AppRoute;
  setRoute: (route: AppRoute) => void;
  replaceRoute: (route: ShellRoute) => void;
  goBack: (fallback?: ShellRoute) => void;
  tokenDetailId: string;
  tokenDetailSymbol: string;
  txDetailHash: string;
  sendTokenId: string;
  navigateToSend: (tokenAddress?: string) => void;
  navigateToToken: (id: string, symbol?: string) => void;
  navigateToTx: (hash: string) => void;
}

export function useAppRoute(): AppRouteState {
  const [routeState, setRouteState] = useState<ShellRoute>({
    name: 'portfolio',
  });
  const historySession = useRef('');

  const getHistorySession = useCallback(() => {
    if (!historySession.current) {
      historySession.current =
        globalThis.crypto?.randomUUID?.() ??
        `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
    }
    return historySession.current;
  }, []);

  const commit = useCallback(
    (next: ShellRoute, mode: 'push' | 'replace' = 'push') => {
      const serialized = serializeRoute(next);
      const parsed = parseRouteUrl(new URL(serialized, window.location.origin));
      const safe = parsed.ok ? parsed.value : ({ name: 'portfolio' } as const);
      const session = getHistorySession();
      const current = historyState(window.history.state, session);
      const depth =
        mode === 'push' ? (current?.paxportDepth ?? 0) + 1 : current?.paxportDepth ?? 0;
      const state: PaxportHistoryState = {
        paxportRoute: safe.name,
        paxportSession: session,
        paxportDepth: depth,
      };
      if (mode === 'replace') {
        window.history.replaceState(state, '', serializeRoute(safe));
      } else {
        window.history.pushState(state, '', serializeRoute(safe));
      }
      setRouteState(safe);
    },
    [getHistorySession],
  );

  useEffect(() => {
    const initial = parseRouteUrl(window.location.href);
    commit(initial.ok ? initial.value : { name: 'portfolio' }, 'replace');

    const onPopState = () => {
      const parsed = parseRouteUrl(window.location.href);
      setRouteState(parsed.ok ? parsed.value : { name: 'portfolio' });
    };
    const onNavigate = (event: Event) => {
      const detail = (event as CustomEvent<{ route?: unknown; url?: unknown }>).detail;
      const next = routeFromExternal(detail?.route ?? detail?.url);
      if (next) commit(next);
    };
    const onWorkerMessage = (event: MessageEvent) => {
      if (event.data?.type !== 'PAXPORT_NAVIGATE') return;
      const next = routeFromExternal(event.data.route);
      if (next) commit(next);
    };
    window.addEventListener('popstate', onPopState);
    window.addEventListener('paxeer:deeplink', onNavigate);
    window.addEventListener('paxeer:push-navigate', onNavigate);
    navigator.serviceWorker?.addEventListener('message', onWorkerMessage);
    return () => {
      window.removeEventListener('popstate', onPopState);
      window.removeEventListener('paxeer:deeplink', onNavigate);
      window.removeEventListener('paxeer:push-navigate', onNavigate);
      navigator.serviceWorker?.removeEventListener('message', onWorkerMessage);
    };
  }, [commit]);

  const setRoute = useCallback(
    (name: AppRoute) => commit(defaultRoute(name)),
    [commit],
  );
  const replaceRoute = useCallback(
    (next: ShellRoute) => commit(next, 'replace'),
    [commit],
  );
  const goBack = useCallback(
    (fallback: ShellRoute = { name: 'portfolio' }) => {
      commit(fallback, 'replace');
    },
    [commit],
  );
  const navigateToSend = useCallback(
    (token?: string) => commit({ name: 'send', ...(token ? { token } : {}) }),
    [commit],
  );
  const navigateToToken = useCallback(
    (token: string, symbol?: string) =>
      commit({
        name: 'token-detail',
        token,
        ...(symbol ? { symbol } : {}),
      }),
    [commit],
  );
  const navigateToTx = useCallback(
    (hash: string) => commit({ name: 'tx-detail', hash }),
    [commit],
  );
  return useMemo(() => {
    const tokenDetail =
      routeState.name === 'token-detail' ? routeState : null;
    const transaction = routeState.name === 'tx-detail' ? routeState : null;
    return {
      routeState,
      route: routeState.name,
      setRoute,
      replaceRoute,
      goBack,
      tokenDetailId: tokenDetail?.token ?? '',
      tokenDetailSymbol: tokenDetail?.symbol ?? '',
      txDetailHash: transaction?.hash ?? '',
      sendTokenId: routeState.name === 'send' ? routeState.token ?? '' : '',
      navigateToSend,
      navigateToToken,
      navigateToTx,
    };
  }, [
    routeState,
    setRoute,
    replaceRoute,
    goBack,
    navigateToSend,
    navigateToToken,
    navigateToTx,
  ]);
}
