'use client';

/**
 * Transaction-history queries.
 *
 * Backed by `fetchPortfolioTransactions`, which returns both native txns and
 * token transfers in one call. We split the response into two derived shapes
 * the UI consumes directly so widgets don't have to re-shape on render.
 */

import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import { fetchPortfolioTransactions } from '@/lib/api';
import { queryKeys } from './keys';

export interface TxHistoryRow {
  hash: string;
  fromAddress: string;
  toAddress: string;
  amountRaw: string;
  decimals: number;
  symbol: string;
  timestamp: string | null;
}

export interface TxHistoryResult {
  transactions: TxHistoryRow[];
  transfers: TxHistoryRow[];
}

const PAX_DECIMALS = 18;
const PAX_SYMBOL = 'PAX';

const normalizeTx = (tx: any): TxHistoryRow => ({
  hash: tx.tx_hash || '',
  fromAddress: tx.from_address || '',
  toAddress: tx.to_address || '',
  amountRaw: tx.value_raw || tx.value || '0',
  decimals: PAX_DECIMALS,
  symbol: PAX_SYMBOL,
  timestamp: tx.timestamp || null,
});

const normalizeTransfer = (t: any): TxHistoryRow => ({
  hash: t.tx_hash || '',
  fromAddress: t.from_address || '',
  toAddress: t.to_address || '',
  amountRaw: t.amount_raw || t.amount || '0',
  decimals: t.token_decimals || PAX_DECIMALS,
  symbol: t.token_symbol || '???',
  timestamp: t.timestamp || null,
});

/**
 * Native transactions + token transfers for the active wallet.
 * Disabled when no address is connected.
 */
export function useTxHistoryQuery(
  address: string | undefined,
  limit: number = 50,
  options?: Partial<UseQueryOptions<TxHistoryResult>>,
) {
  return useQuery<TxHistoryResult>({
    queryKey: queryKeys.txHistory(address ?? ''),
    queryFn: async () => {
      const data = await fetchPortfolioTransactions(address as string, limit);
      return {
        transactions: (data.transactions || []).map(normalizeTx),
        transfers: (data.token_transfers || []).map(normalizeTransfer),
      };
    },
    enabled: Boolean(address),
    ...options,
  });
}
