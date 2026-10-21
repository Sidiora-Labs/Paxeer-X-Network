'use client';

/**
 * Derive the swap **input** token list from the cached portfolio.
 *
 * Includes native PAX always, plus any ERC-20 holding with a non-zero
 * balance. Falls back to the static SWAP_TOKENS list if the portfolio query
 * returns no usable holdings.
 */

import { useMemo } from 'react';
import { PAX_ICON_URL } from '@/lib/constants';
import { usePortfolioQuery } from '@/lib/queries';
import { SWAP_TOKENS, type SwapToken } from '@/lib/swap';

export function useHeldTokens(address: string | undefined): SwapToken[] {
  const portfolioQuery = usePortfolioQuery(address);

  return useMemo<SwapToken[]>(() => {
    const portfolio = portfolioQuery.data;
    if (!portfolio) return SWAP_TOKENS;

    const list: SwapToken[] = [
      {
        symbol: 'PAX',
        name: 'Paxeer',
        address: '',
        decimals: 18,
        isNative: true,
        iconUrl: PAX_ICON_URL,
      },
    ];

    if (portfolio.token_holdings) {
      portfolio.token_holdings.forEach((h: any) => {
        const bal = BigInt(h.balance_raw || '0');
        if (bal > BigInt(0)) {
          list.push({
            symbol: h.symbol || '???',
            name: h.name || 'Unknown',
            address: (h.contract_address || '').toLowerCase(),
            decimals: h.decimals || 18,
            iconUrl: h.icon_url || undefined,
          });
        }
      });
    }

    return list.length > 1 ? list : SWAP_TOKENS;
  }, [portfolioQuery.data]);
}
