'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchTxDetail, fetchTxTokenTransfers } from '@/lib/api';
import { queryKeys } from './keys';

export interface TxDetailResult {
  tx: Awaited<ReturnType<typeof fetchTxDetail>>;
  tokenTransfers: unknown[];
}

export function useTxDetailQuery(txHash: string | undefined) {
  return useQuery<TxDetailResult>({
    queryKey: queryKeys.txDetail(txHash ?? ''),
    queryFn: async () => {
      const [tx, transfersRes] = await Promise.all([
        fetchTxDetail(txHash as string),
        fetchTxTokenTransfers(txHash as string).catch(() => ({ items: [] as unknown[] })),
      ]);
      return { tx, tokenTransfers: (transfersRes as any).items ?? [] };
    },
    enabled: Boolean(txHash),
    staleTime: 30_000,
    retry: 1,
  });
}
