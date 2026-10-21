'use client';

/**
 * Derive a flat sendable-tokens list from the cached portfolio.
 *
 * Native PAX is always first. ERC-20 holdings follow in portfolio order.
 * Returns `loading: true` while the underlying portfolio query is pending —
 * the orchestrator uses that to gate the token-selector sheet.
 */

import { useMemo } from 'react';
import { formatBalance } from '@/lib/format';
import { PAX_ICON_URL } from '@/lib/constants';
import { usePortfolioQuery } from '@/lib/queries';

export interface SendableToken {
  symbol: string;
  name: string;
  decimals: number;
  balance: string;
  balanceRaw: string;
  /** undefined = native PAX */
  address?: string;
  iconUrl?: string;
}

export interface UseSendableTokensResult {
  tokens: SendableToken[];
  loading: boolean;
}

export function useSendableTokens(address: string | undefined): UseSendableTokensResult {
  const portfolioQuery = usePortfolioQuery(address);

  const tokens = useMemo<SendableToken[]>(() => {
    const portfolio = portfolioQuery.data;
    if (!portfolio) {
      // Empty fallback — orchestrator can still show the native row.
      return [
        { symbol: 'PAX', name: 'Paxeer', decimals: 18, balance: '0', balanceRaw: '0', iconUrl: PAX_ICON_URL },
      ];
    }

    const list: SendableToken[] = [];

    const nativeRaw = portfolio.native_balance?.balance_raw || '0';
    list.push({
      symbol: 'PAX',
      name: 'Paxeer',
      decimals: 18,
      balance: formatBalance(nativeRaw, 18, 4),
      balanceRaw: nativeRaw,
      iconUrl: PAX_ICON_URL,
    });

    if (portfolio.token_holdings) {
      portfolio.token_holdings.forEach((h: any) => {
        const raw = h.balance_raw || '0';
        list.push({
          symbol: h.symbol || '???',
          name: h.name || 'Unknown',
          decimals: h.decimals || 18,
          balance: formatBalance(raw, h.decimals || 18, 4),
          balanceRaw: raw,
          address: h.contract_address,
          iconUrl: h.icon_url || undefined,
        });
      });
    }

    return list;
  }, [portfolioQuery.data]);

  return { tokens, loading: portfolioQuery.isPending };
}
