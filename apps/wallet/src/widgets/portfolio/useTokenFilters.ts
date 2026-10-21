'use client';

/**
 * Token visibility filter state, persisted to localStorage.
 *
 * Two filters compose: a global "hide dust under $1" toggle and a per-token
 * visibility map. Both survive page reloads. Apply via `applyFilters` against
 * the raw holdings array.
 */

import { useCallback, useState } from 'react';
import { portfolioFiltersRepository } from '@/platform/storage/repositories';

const DUST_THRESHOLD_USD = 1;

export interface TokenFiltersState {
  hideDust: boolean;
  hiddenTokens: Set<string>;
  toggleHideDust: () => void;
  toggleTokenVisibility: (address: string) => void;
  applyFilters: <T extends { contract_address?: string; value_usd?: string | number | null }>(
    holdings: T[],
  ) => T[];
}

export function useTokenFilters(): TokenFiltersState {
  const [hideDust, setHideDust] = useState(() => {
    if (typeof window === 'undefined') return false;
    return portfolioFiltersRepository.read().hideDust;
  });

  const [hiddenTokens, setHiddenTokens] = useState<Set<string>>(() => {
    if (typeof window === 'undefined') return new Set();
    return new Set(portfolioFiltersRepository.read().hiddenTokens);
  });

  const toggleHideDust = useCallback(() => {
    setHideDust((prev) => {
      const next = !prev;
      portfolioFiltersRepository.update((current) => ({
        ...current,
        hideDust: next,
      }));
      return next;
    });
  }, []);

  const toggleTokenVisibility = useCallback((address: string) => {
    setHiddenTokens((prev) => {
      const next = new Set(prev);
      if (next.has(address)) next.delete(address);
      else next.add(address);
      portfolioFiltersRepository.update((current) => ({
        ...current,
        hiddenTokens: [...next],
      }));
      return next;
    });
  }, []);

  const applyFilters = useCallback(
    <T extends { contract_address?: string; value_usd?: string | number | null }>(
      holdings: T[],
    ): T[] => {
      return holdings.filter((h) => {
        const addr = (h.contract_address || '').toLowerCase();
        if (hiddenTokens.has(addr)) return false;
        if (hideDust) {
          const val = h.value_usd != null ? Number(h.value_usd) : 0;
          if (val < DUST_THRESHOLD_USD && val >= 0) return false;
        }
        return true;
      });
    },
    [hideDust, hiddenTokens],
  );

  return { hideDust, hiddenTokens, toggleHideDust, toggleTokenVisibility, applyFilters };
}
