/** React hook — fetch paginated transactions for an address from Blockscout */

import { useState, useCallback, useRef } from 'react';
import { getAddressTransactions } from '../api/addresses';
import type { AddressTxsParams } from '../api/addresses';
import type { PaginatedResponse } from '../types/common';
import type { Transaction } from '../types/transaction';

export interface UseTransactionsResult {
  transactions: Transaction[];
  nextPageParams: Record<string, unknown> | null;
  loading: boolean;
  error: Error | null;
  fetch: (addressHash: string, params?: AddressTxsParams) => Promise<void>;
  fetchNextPage: () => Promise<void>;
  refresh: () => Promise<void>;
}

export function useTransactions(): UseTransactionsResult {
  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [nextPageParams, setNextPageParams] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const lastHashRef = useRef<string>('');
  const lastParamsRef = useRef<AddressTxsParams | undefined>(undefined);

  const fetchData = useCallback(async (
    addressHash: string,
    params?: AddressTxsParams,
  ) => {
    if (!addressHash) return;
    lastHashRef.current = addressHash;
    lastParamsRef.current = params;
    setLoading(true);
    setError(null);

    try {
      const result: PaginatedResponse<Transaction> = await getAddressTransactions(
        addressHash,
        params,
      );
      setTransactions(result.items);
      setNextPageParams(result.next_page_params);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchNextPage = useCallback(async () => {
    if (!lastHashRef.current || !nextPageParams) return;
    setLoading(true);
    setError(null);

    try {
      const result: PaginatedResponse<Transaction> = await getAddressTransactions(
        lastHashRef.current,
        { ...lastParamsRef.current, ...nextPageParams } as AddressTxsParams,
      );
      setTransactions((prev) => [...prev, ...result.items]);
      setNextPageParams(result.next_page_params);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [nextPageParams]);

  const refresh = useCallback(async () => {
    if (lastHashRef.current) {
      await fetchData(lastHashRef.current, lastParamsRef.current);
    }
  }, [fetchData]);

  return { transactions, nextPageParams, loading, error, fetch: fetchData, fetchNextPage, refresh };
}
