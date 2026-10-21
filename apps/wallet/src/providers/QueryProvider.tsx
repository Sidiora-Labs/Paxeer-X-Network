'use client';

import { QueryClient, QueryClientProvider, type DefaultOptions } from '@tanstack/react-query';
import { useState } from 'react';

/**
 * TanStack Query provider for the wallet app.
 *
 * Defaults are tuned for a wallet UI:
 *   - staleTime 15s     → fresh data for foreground bursts, no refetch storm
 *   - gcTime    5m      → caches survive route changes, freed if abandoned
 *   - retry     1       → recover from transient blockscout 5xx without thrashing
 *   - refetchOnWindowFocus + refetchOnReconnect → background sync when user returns
 *   - structuralSharing → re-render only when shape actually changes
 *
 * Polling is left OFF here. Individual queries opt in to refetchInterval
 * if they need live updates (e.g. price ticker), so most screens cost zero
 * background bandwidth.
 */
const defaultQueryOptions: DefaultOptions = {
  queries: {
    staleTime: 15_000,
    gcTime: 5 * 60_000,
    retry: 1,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchOnMount: true,
    structuralSharing: true,
  },
  mutations: {
    retry: 0,
  },
};

export function QueryProvider({ children }: { children: React.ReactNode }) {
  // QueryClient must live in component state so it survives HMR but does
  // not get recreated on each render. One client per browser tab.
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: defaultQueryOptions,
      }),
  );

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
