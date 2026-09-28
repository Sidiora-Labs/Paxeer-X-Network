'use client';

/**
 * Apply a pending optimistic send on portfolio mount.
 *
 * Reads `consumePendingSend()` from sessionStorage once cache is populated
 * (`!loading`), applies an immediate subtraction to the cached portfolio so
 * the UI reflects the outgoing transfer, then schedules a cache-busted
 * reconcile after `RECONCILE_DELAY_MS` to snap to on-chain truth.
 *
 * The `optimisticBlockUntilRef` is owned by the caller so query hooks can
 * skip background refetches while the optimistic delta is in flight —
 * otherwise a stale read would stomp the optimistic value.
 *
 * Why split from the orchestrator: the ref needs to be visible to the
 * `usePortfolioQuery({ refetchOnWindowFocus: ... })` callbacks BEFORE the
 * effect runs. Owning the ref upstream avoids declaration-order tangles.
 */

import { useEffect, useRef } from 'react';
import {
  consumePendingSend,
  subtractFromRawBalance,
  RECONCILE_DELAY_MS,
} from '@/lib/optimistic';
import { formatBalance } from '@/lib/format';
import { useReconcilePortfolio, useOptimisticPortfolioUpdate } from '@/lib/queries';

export interface UseApplyPendingSendOptions {
  address: string | undefined;
  loading: boolean;
  /** Mutated by this hook; read by query refetch guards in the parent. */
  optimisticBlockUntilRef: React.MutableRefObject<number>;
}

export function useApplyPendingSend({
  address,
  loading,
  optimisticBlockUntilRef,
}: UseApplyPendingSendOptions) {
  const reconcilePortfolio = useReconcilePortfolio();
  const updatePortfolioCache = useOptimisticPortfolioUpdate();

  const optimisticAppliedRef = useRef(false);
  const reconcileTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!address || loading || optimisticAppliedRef.current) return;

    const pending = consumePendingSend();
    if (!pending) return;

    optimisticAppliedRef.current = true;
    optimisticBlockUntilRef.current = Date.now() + RECONCILE_DELAY_MS + 5_000;

    updatePortfolioCache(address, (prev: any) => {
      if (!prev) return prev;
      const next = { ...prev };

      if (!pending.tokenAddress) {
        const newRaw = subtractFromRawBalance(
          prev.native_balance?.balance_raw || '0',
          pending.amount,
          18,
        );
        next.native_balance = {
          ...prev.native_balance,
          balance_raw: newRaw,
          balance: formatBalance(newRaw, 18, 8),
        };
      } else {
        next.token_holdings = (prev.token_holdings || []).map((h: any) => {
          if (h.contract_address?.toLowerCase() === pending.tokenAddress?.toLowerCase()) {
            const newRaw = subtractFromRawBalance(
              h.balance_raw || '0',
              pending.amount,
              h.decimals || 18,
            );
            return {
              ...h,
              balance_raw: newRaw,
              balance: formatBalance(newRaw, h.decimals || 18, 8),
            };
          }
          return h;
        });
      }
      return next;
    });

    reconcileTimerRef.current = setTimeout(async () => {
      optimisticBlockUntilRef.current = 0;
      try {
        await reconcilePortfolio(address);
      } catch {
        /* reconcile failed — next focus refetch will catch up */
      }
    }, RECONCILE_DELAY_MS);

    return () => {
      if (reconcileTimerRef.current) clearTimeout(reconcileTimerRef.current);
    };
  }, [address, loading, reconcilePortfolio, updatePortfolioCache, optimisticBlockUntilRef]);
}
