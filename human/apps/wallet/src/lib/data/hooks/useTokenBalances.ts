/** React hook — fetch all token balances for an address from Blockscout */

import { useState, useCallback, useRef } from 'react';
import { getAddressTokenBalances } from '../api/addresses';
import type { TokenBalance } from '../types/token';

export interface UseTokenBalancesResult {
  balances: TokenBalance[];
  loading: boolean;
  error: Error | null;
  fetch: (addressHash: string) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useTokenBalances(): UseTokenBalancesResult {
  const [balances, setBalances] = useState<TokenBalance[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const lastHashRef = useRef<string>('');

  const fetchData = useCallback(async (addressHash: string) => {
    if (!addressHash) return;
    lastHashRef.current = addressHash;
    setLoading(true);
    setError(null);

    try {
      const result = await getAddressTokenBalances(addressHash);
      setBalances(result);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, []);

  const refresh = useCallback(async () => {
    if (lastHashRef.current) {
      await fetchData(lastHashRef.current);
    }
  }, [fetchData]);

  return { balances, loading, error, fetch: fetchData, refresh };
}
