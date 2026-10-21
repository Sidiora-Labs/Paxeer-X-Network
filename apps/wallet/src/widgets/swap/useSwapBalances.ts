'use client';

/**
 * Derive from-token balance + display strings from the cached portfolio.
 *
 * The whole point of this hook: replace 3 separate `fetchPortfolio` calls in
 * the original SwapPage with a single shared query subscription. Re-keys
 * automatically when the user picks a new token.
 */

import { useMemo } from 'react';
import { usePortfolioQuery } from '@/lib/queries';
import type { SwapToken } from '@/lib/swap';
import { formatBalanceCompact } from './util';

export interface SwapBalances {
  fromBalance: string | null;
  toBalance: string | null;
  fromBalanceRaw: bigint | null;
  loading: boolean;
}

const findHolding = (portfolio: any, token: SwapToken) => {
  if (token.isNative) return null;
  return (portfolio?.token_holdings || []).find(
    (h: any) => (h.contract_address || '').toLowerCase() === token.address.toLowerCase(),
  );
};

const tokenDisplayBalance = (portfolio: any, token: SwapToken): string => {
  if (!portfolio) return '0';
  if (token.isNative) {
    const raw = BigInt(portfolio.native_balance?.balance_raw || '0');
    return formatBalanceCompact(raw, 18);
  }
  const holding = findHolding(portfolio, token);
  if (!holding) return '0';
  const raw = BigInt(holding.balance_raw || '0');
  return formatBalanceCompact(raw, holding.decimals || token.decimals);
};

const tokenRawBalance = (portfolio: any, token: SwapToken): bigint | null => {
  if (!portfolio) return null;
  if (token.isNative) return BigInt(portfolio.native_balance?.balance_raw || '0');
  const holding = findHolding(portfolio, token);
  return BigInt(holding?.balance_raw || '0');
};

export function useSwapBalances(
  address: string | undefined,
  fromToken: SwapToken,
  toToken: SwapToken,
): SwapBalances {
  const portfolioQuery = usePortfolioQuery(address);

  return useMemo<SwapBalances>(() => {
    const portfolio = portfolioQuery.data;
    if (!address) {
      return { fromBalance: null, toBalance: null, fromBalanceRaw: null, loading: false };
    }
    return {
      fromBalance: portfolio ? tokenDisplayBalance(portfolio, fromToken) : null,
      toBalance: portfolio ? tokenDisplayBalance(portfolio, toToken) : null,
      fromBalanceRaw: tokenRawBalance(portfolio, fromToken),
      loading: portfolioQuery.isPending,
    };
  }, [
    address,
    portfolioQuery.data,
    portfolioQuery.isPending,
    fromToken,
    toToken,
  ]);
}
