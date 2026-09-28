/** React hook — fetch NFTs owned by an address from Blockscout */

import { useState, useCallback, useRef } from 'react';
import { getAddressNfts } from '../api/addresses';
import type { AddressNftParams } from '../api/addresses';
import type { PaginatedResponse } from '../types/common';
import type { TokenInstanceInList } from '../types/token';

export interface UseNftsResult {
  nfts: TokenInstanceInList[];
  nextPageParams: Record<string, unknown> | null;
  loading: boolean;
  error: Error | null;
  fetch: (addressHash: string, params?: AddressNftParams) => Promise<void>;
  fetchNextPage: () => Promise<void>;
  refresh: () => Promise<void>;
}

export function useNfts(): UseNftsResult {
  const [nfts, setNfts] = useState<TokenInstanceInList[]>([]);
  const [nextPageParams, setNextPageParams] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const lastHashRef = useRef<string>('');
  const lastParamsRef = useRef<AddressNftParams | undefined>(undefined);

  const fetchData = useCallback(async (
    addressHash: string,
    params?: AddressNftParams,
  ) => {
    if (!addressHash) return;
    lastHashRef.current = addressHash;
    lastParamsRef.current = params;
    setLoading(true);
    setError(null);

    try {
      const result: PaginatedResponse<TokenInstanceInList> = await getAddressNfts(
        addressHash,
        params,
      );
      setNfts(result.items);
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
      const result: PaginatedResponse<TokenInstanceInList> = await getAddressNfts(
        lastHashRef.current,
        { ...lastParamsRef.current, ...nextPageParams } as AddressNftParams,
      );
      setNfts((prev) => [...prev, ...result.items]);
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

  return { nfts, nextPageParams, loading, error, fetch: fetchData, fetchNextPage, refresh };
}
