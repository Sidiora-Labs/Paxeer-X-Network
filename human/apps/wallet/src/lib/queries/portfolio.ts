'use client';

import { useQuery, useQueryClient, type UseQueryOptions } from '@tanstack/react-query';
import {
  fetchPortfolio,
  fetchPortfolioFresh,
  fetchBalance,
  fetchBalanceFresh,
  fetchPaxPriceLatest,
} from '@/lib/api';
import type { BalanceResponse } from '@/lib/api';
import { queryKeys } from './keys';

type PortfolioData = Awaited<ReturnType<typeof fetchPortfolio>>;
type PaxPriceData = Awaited<ReturnType<typeof fetchPaxPriceLatest>>;

/**
 * Wallet portfolio (token holdings + native balance). Powers the bento grid
 * on `PortfolioPage`. Disabled when no address is connected.
 */
export function usePortfolioQuery(
  address: string | undefined,
  options?: Partial<UseQueryOptions<PortfolioData>>,
) {
  return useQuery<PortfolioData>({
    queryKey: queryKeys.portfolio(address ?? ''),
    queryFn: () => fetchPortfolio(address as string),
    enabled: Boolean(address),
    ...options,
  });
}

/**
 * Latest PAX price (USD). Refreshes every 60s while mounted so the hero
 * card stays accurate without flooding the price API.
 */
export function usePaxPriceQuery(symbol: string = 'PAX') {
  return useQuery<PaxPriceData>({
    queryKey: queryKeys.paxPrice(symbol),
    queryFn: () => fetchPaxPriceLatest(symbol),
    staleTime: 60_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  });
}

/**
 * Daily PnL + USD totals from the wallet balance endpoint.
 */
export function useBalanceQuery(
  address: string | undefined,
  options?: Partial<UseQueryOptions<BalanceResponse | null>>,
) {
  return useQuery<BalanceResponse | null>({
    queryKey: queryKeys.balance(address ?? ''),
    queryFn: () => fetchBalance(address as string),
    enabled: Boolean(address),
    ...options,
  });
}

/**
 * Manual reconcile helper used after a send: bypass any cache layers and
 * push the fresh result back into the query cache so the UI snaps to truth.
 *
 * Returned function is stable for the lifetime of the calling component.
 */
export function useReconcilePortfolio() {
  const qc = useQueryClient();

  return async (address: string) => {
    const [freshPortfolio, freshBalance] = await Promise.all([
      fetchPortfolioFresh(address).catch(() => null),
      fetchBalanceFresh(address).catch(() => null),
    ]);

    if (freshPortfolio) {
      qc.setQueryData(queryKeys.portfolio(address), freshPortfolio);
    }
    if (freshBalance) {
      qc.setQueryData(queryKeys.balance(address), freshBalance);
    }
  };
}

/**
 * Apply an optimistic mutation to the cached portfolio without refetching.
 * Caller supplies a producer that returns the new portfolio shape.
 */
export function useOptimisticPortfolioUpdate() {
  const qc = useQueryClient();

  return (address: string, updater: (prev: PortfolioData | undefined) => PortfolioData | undefined) => {
    qc.setQueryData<PortfolioData | undefined>(
      queryKeys.portfolio(address),
      (prev: PortfolioData | undefined) => updater(prev),
    );
  };
}
