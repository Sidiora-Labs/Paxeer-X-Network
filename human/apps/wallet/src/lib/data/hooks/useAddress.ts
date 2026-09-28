/** React hook — fetch address info + counters from Blockscout */

import { useState, useCallback, useRef } from 'react';
import { getAddress, getAddressCounters } from '../api/addresses';
import type { AddressResponse, AddressCounters } from '../types/address';

export interface UseAddressResult {
  address: AddressResponse | null;
  counters: AddressCounters | null;
  loading: boolean;
  error: Error | null;
  fetch: (addressHash: string) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useAddress(): UseAddressResult {
  const [address, setAddress] = useState<AddressResponse | null>(null);
  const [counters, setCounters] = useState<AddressCounters | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const lastHashRef = useRef<string>('');

  const fetchData = useCallback(async (addressHash: string) => {
    if (!addressHash) return;
    lastHashRef.current = addressHash;
    setLoading(true);
    setError(null);

    try {
      const [addrRes, countersRes] = await Promise.all([
        getAddress(addressHash),
        getAddressCounters(addressHash),
      ]);
      setAddress(addrRes);
      setCounters(countersRes);
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

  return { address, counters, loading, error, fetch: fetchData, refresh };
}
