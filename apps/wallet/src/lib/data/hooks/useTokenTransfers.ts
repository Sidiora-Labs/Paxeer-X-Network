/** React hook — fetch paginated token transfers for an address from Blockscout */

import { useState, useCallback, useRef } from 'react';
import { getAddressTokenTransfers } from '../api/addresses';
import type { AddressTokenTransfersParams } from '../api/addresses';
import type { PaginatedResponse } from '../types/common';
import type { TokenTransfer } from '../types/token';

export interface UseTokenTransfersResult {
  transfers: TokenTransfer[];
  nextPageParams: Record<string, unknown> | null;
  loading: boolean;
  error: Error | null;
  fetch: (addressHash: string, params?: AddressTokenTransfersParams) => Promise<void>;
  fetchNextPage: () => Promise<void>;
  refresh: () => Promise<void>;
}

export function useTokenTransfers(): UseTokenTransfersResult {
  const [transfers, setTransfers] = useState<TokenTransfer[]>([]);
  const [nextPageParams, setNextPageParams] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const lastHashRef = useRef<string>('');
  const lastParamsRef = useRef<AddressTokenTransfersParams | undefined>(undefined);

  const fetchData = useCallback(async (
    addressHash: string,
    params?: AddressTokenTransfersParams,
  ) => {
    if (!addressHash) return;
    lastHashRef.current = addressHash;
    lastParamsRef.current = params;
    setLoading(true);
    setError(null);

    try {
      const result: PaginatedResponse<TokenTransfer> = await getAddressTokenTransfers(
        addressHash,
        params,
      );
      setTransfers(result.items);
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
      const result: PaginatedResponse<TokenTransfer> = await getAddressTokenTransfers(
        lastHashRef.current,
        { ...lastParamsRef.current, ...nextPageParams } as AddressTokenTransfersParams,
      );
      setTransfers((prev) => [...prev, ...result.items]);
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

  return { transfers, nextPageParams, loading, error, fetch: fetchData, fetchNextPage, refresh };
}
