'use client';

import { useQuery } from '@tanstack/react-query';
import { PAXEER_CONFIG } from '@/lib/constants';
import { queryKeys } from './keys';

async function fetchTxCount(address: string): Promise<number> {
  const res = await fetch(
    `${PAXEER_CONFIG.indexerApiBase}/addresses/${address}/counters`,
  );
  if (!res.ok) throw new Error(`counters ${res.status}`);
  const data = await res.json();
  return Number(data.transactions_count || data.total || 0);
}

/**
 * Transaction count for a wallet address.
 * Used by notification-triggers to detect new incoming txs without a
 * raw setInterval — TanStack Query manages the 60-second poll window.
 */
export function useTxCountQuery(address: string | undefined) {
  return useQuery<number>({
    queryKey: queryKeys.txCount(address ?? ''),
    queryFn: () => fetchTxCount(address as string),
    enabled: Boolean(address),
    staleTime: 60_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  });
}
